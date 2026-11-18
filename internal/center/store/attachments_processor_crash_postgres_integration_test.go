package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
)

const contentProcessorCrashExitCode = 86

func TestPostgresIntegrationAttachmentProcessorCrashCutpointsConvergeAfterRestart(t *testing.T) {
	ctx := context.Background()
	helperBinary := buildContentProcessorCrashHelper(t)
	cutpoints := []string{
		string(attachments.ProcessorWorkerCutpointAfterClaim),
		string(attachments.ProcessorWorkspaceCutpointAfterMkdir),
		string(attachments.ProcessorWorkspaceCutpointAfterSourceMaterialization),
		string(attachments.ProcessorWorkspaceCutpointAfterProcessing),
		string(attachments.ProcessorWorkerCutpointAfterResultCommit),
		string(attachments.ProcessorWorkspaceCutpointAfterPhysicalPurge),
	}
	for index, cutpoint := range cutpoints {
		t.Run(cutpoint, func(t *testing.T) {
			signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
			fixture := signed.platform
			blobRoot := filepath.Join(t.TempDir(), "blob-root")
			blob, err := newPostgresLocalBlobStore(t, ctx, fixture, blobRoot)
			if err != nil {
				t.Fatal(err)
			}
			databaseURL := contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "content-processor-crash-helper"))
			content := []byte(fmt.Sprintf("content processor crash cutpoint %d\n", index))
			actor := attachmentUploadWorkflowActor(t)
			suffix := fmt.Sprintf("processcrash%d", index)
			draftID, uploadID, attachmentID := "rdf_"+suffix, "aup_"+suffix, "att_"+suffix
			seedAttachmentDraft(t, ctx, fixture, draftID, actor.UserID)
			repository := NewPostgresAttachmentRepository(fixture.openDirectRuntimePool(t, ctx, "processor-crash-upload", 2))
			service := newAttachmentUploadWorkflowService(t, fixture, repository, blob, attachments.TransportKindLocal, actor, draftID, time.Now)
			expiresAt := time.Now().UTC().Add(time.Hour)
			if _, err := service.CreateUpload(ctx, attachments.CreateUploadRequest{Actor: actor, UploadID: uploadID, AttachmentID: attachmentID,
				DraftID: draftID, DisplayName: suffix + ".txt", MediaType: "text/plain", DeclaredSizeBytes: int64(len(content)), ExpiresAt: expiresAt}); err != nil {
				t.Fatal(err)
			}
			put, err := service.PutContent(ctx, attachments.PutUploadContentRequest{Actor: actor, DraftID: draftID, UploadID: uploadID,
				ExpectedSHA256: sha256.Sum256(content), Content: bytes.NewReader(content)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{Actor: actor, DraftID: draftID, UploadID: uploadID}); err != nil {
				t.Fatal(err)
			}
			source := attachments.BlobObject{Key: put.Object.Key, ObjectVersion: put.Object.VersionID, SHA256: put.Object.SHA256,
				SizeBytes: put.Object.SizeBytes, BackendKind: attachments.BackendKindLocal, NamespaceID: blob.Namespace().NamespaceID}
			seed := attachmentProcessorSeedResult{UploadID: uploadID, AttachmentID: attachmentID, DraftID: draftID, Source: source}
			if err := fixture.db.QueryRow(ctx, `select processor_job_id from public.attachment_processor_jobs where upload_id=$1`, uploadID).Scan(&seed.ProcessorJobID); err != nil {
				t.Fatal(err)
			}
			// This fixture directly seeds a worker row for the active signed
			// contract; membership issuer acceptance is tested separately.
			if _, err := fixture.db.Exec(ctx, `insert into public.deployment_membership
				(instance_id,deployment_id,project_id,instance_kind,capability,deployment_epoch,fence_contract_version,load_balancer_admitted,queue_admitted,heartbeat_expires_at)
				select 'compose-processor',deployment_id,project_id,'worker','records.runtime',active_domain_identity_epoch,minimum_fence_contract_version,false,true,clock_timestamp()+interval '1 hour'
				from public.deployment_contract_state where project_id='default'`); err != nil {
				t.Fatal(err)
			}
			workspaceRoot := filepath.Join(t.TempDir(), "processor-root")
			container, deploymentID := newContentProcessorLaunchFixture(t, ctx, fixture, helperBinary, blobRoot, workspaceRoot)
			environment := contentProcessorCrashEnvironment(databaseURL, blobRoot, workspaceRoot)
			environment = append(environment,
				"HOUFENG_RECORD_BIRTH_LAUNCH_FILE=/run/houfeng/records-authority/birth-launch-processor.json",
				"HOUFENG_RECORD_DEPLOYMENT_ID="+deploymentID,
				"HOUFENG_RECORD_INSTANCE_ID=compose-processor", "HOUFENG_RECORD_INSTANCE_KIND=worker",
				"HOUFENG_RECORD_INSTANCE_CAPABILITY=records.runtime")

			output, err := runContentProcessorCrashHelper(container, environment, "crash", cutpoint)
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != contentProcessorCrashExitCode {
				t.Fatalf("crash helper error = %v, output = %s", err, output)
			}
			expireAttachmentProcessorCrashState(t, ctx, fixture, seed.ProcessorJobID)
			if output, err := runContentProcessorCrashHelper(container, environment, "recover", ""); err != nil {
				t.Fatalf("recovery helper error = %v, output = %s", err, output)
			}
			if output, err := runContentProcessorCrashHelper(container, environment, "recover", ""); err != nil {
				t.Fatalf("recovery replay helper error = %v, output = %s", err, output)
			}

			wantWorkspaces := int64(2)
			if cutpoint == string(attachments.ProcessorWorkerCutpointAfterClaim) ||
				cutpoint == string(attachments.ProcessorWorkerCutpointAfterResultCommit) {
				wantWorkspaces = 1
			}
			assertAttachmentProcessorCrashConverged(
				t, ctx, fixture, seed, source, workspaceRoot, wantWorkspaces,
			)
			assertNoLocalBlobTemporaryResidue(t, blobRoot)
		})
	}
}

func contentProcessorCrashDatabaseURL(config *pgxpool.Config) string {
	databaseURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.ConnConfig.User, config.ConnConfig.Password),
		Host:   net.JoinHostPort(config.ConnConfig.Host, strconv.Itoa(int(config.ConnConfig.Port))),
		Path:   "/" + config.ConnConfig.Database,
	}
	query := databaseURL.Query()
	query.Set("sslmode", "disable")
	databaseURL.RawQuery = query.Encode()
	return databaseURL.String()
}

func buildContentProcessorCrashHelper(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "houfeng-content-processor.test")
	arguments := []string{"test", "-c", "-o", binary}
	cgo := "CGO_ENABLED=0"
	for _, flag := range strings.Fields(os.Getenv("GOFLAGS")) {
		if flag == "-race" || flag == "-race=true" {
			// Keep the inherited race instrumentation while avoiding a host
			// glibc dependency in the Alpine fixture. Go handles DNS/user lookups.
			cgo = "CGO_ENABLED=1"
			arguments = append(arguments, "-tags=netgo,osusergo", "-ldflags=-linkmode=external -extldflags=-static")
			break
		}
	}
	arguments = append(arguments, "./cmd/houfeng-content-processor")
	command := exec.Command("go", arguments...)
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(), cgo)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build content processor crash helper: %v\n%s", err, output)
	}
	return binary
}

func contentProcessorCrashEnvironment(databaseURL, blobRoot, workspaceRoot string) []string {
	// Only this isolated APP's explicit configuration reaches the helper.
	return []string{
		"HOUFENG_CONTENT_PROCESSOR_CRASH_HELPER=1",
		"HOUFENG_DATABASE_URL=" + databaseURL,
		"HOUFENG_ATTACHMENT_BLOB_BACKEND=local",
		"HOUFENG_ATTACHMENT_BLOB_ROOT=" + blobRoot,
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT=" + workspaceRoot,
		"HOUFENG_CONTENT_PROCESSOR_OWNER_ID=processor_crash_helper",
		"HOUFENG_CONTENT_PROCESSOR_LEASE_DURATION=5s",
		"HOUFENG_CONTENT_PROCESSOR_CLEANUP_TIMEOUT=5s",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS=10",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME=5s",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_RETRY_DELAY=1us",
	}
}

func runContentProcessorCrashHelper(container string, environment []string, phase, cutpoint string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	arguments := []string{"exec"}
	for _, entry := range append(append([]string{}, environment...),
		"HOUFENG_CONTENT_PROCESSOR_CRASH_PHASE="+phase,
		"HOUFENG_CONTENT_PROCESSOR_CRASH_CUTPOINT="+cutpoint) {
		arguments = append(arguments, "--env", entry)
	}
	arguments = append(arguments, container, "/processor-helper", "-test.run=^TestContentProcessorCrashHelper$", "-test.v")
	command := exec.CommandContext(ctx, "docker", arguments...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func expireAttachmentProcessorCrashState(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	processorJobID string,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1 and processor_state = 'claimed'`, processorJobID); err != nil {
		t.Fatalf("expire crashed processor claim: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.content_processor_workspaces
		set updated_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, processorJobID); err != nil {
		t.Fatalf("age crashed processor workspaces: %v", err)
	}
}

func assertAttachmentProcessorCrashConverged(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	seed attachmentProcessorSeedResult,
	source attachments.BlobObject,
	workspaceRoot string,
	wantWorkspaces int64,
) {
	t.Helper()
	var processorState attachments.ProcessorState
	var uploadState, attachmentState attachments.UploadState
	var previewKey, previewVersion, previewMediaType string
	var previewSize, workspaceCount, purgedWorkspaceCount, receiptCount, invalidReceiptCount int64
	if err := fixture.db.QueryRow(ctx, `
		select job.processor_state, upload.upload_state, attachment.attachment_state,
		       attachment.preview_blob_key, attachment.preview_blob_object_version,
		       attachment.preview_media_type, attachment.preview_size_bytes,
		       (select count(*) from public.content_processor_workspaces as workspace
		        where workspace.processor_job_id = job.processor_job_id),
		       (select count(*) from public.content_processor_workspaces as workspace
		        where workspace.processor_job_id = job.processor_job_id
		          and workspace.workspace_state = 'purged'),
		       (select count(*) from public.content_workspace_purge_receipts as receipt
		        join public.content_processor_workspaces as workspace
		          on workspace.workspace_id = receipt.workspace_id
		        where workspace.processor_job_id = job.processor_job_id),
		       (select count(*) from (
		          select workspace.workspace_id
		          from public.content_processor_workspaces as workspace
		          left join public.content_workspace_purge_receipts as receipt
		            on receipt.workspace_id = workspace.workspace_id
		          where workspace.processor_job_id = job.processor_job_id
		          group by workspace.workspace_id
		          having count(receipt.workspace_id) <> 1
		       ) as invalid_receipts)
		from public.attachment_processor_jobs as job
		join public.attachment_uploads as upload on upload.upload_id = job.upload_id
		join public.record_attachments as attachment on attachment.attachment_id = job.attachment_id
		where job.processor_job_id = $1`, seed.ProcessorJobID).Scan(
		&processorState, &uploadState, &attachmentState,
		&previewKey, &previewVersion, &previewMediaType, &previewSize,
		&workspaceCount, &purgedWorkspaceCount, &receiptCount, &invalidReceiptCount,
	); err != nil {
		t.Fatalf("read crash convergence result: %v", err)
	}
	if processorState != attachments.ProcessorStateSucceeded ||
		uploadState != attachments.UploadStateAvailable || attachmentState != attachments.UploadStateAvailable {
		t.Fatalf("crash convergence states = %q/%q/%q", processorState, uploadState, attachmentState)
	}
	if previewKey != source.Key || previewVersion != source.ObjectVersion ||
		previewMediaType != attachments.ManagedPreviewMediaTypeTextUTF8 || previewSize != source.SizeBytes {
		t.Fatalf("crash convergence preview = %q/%q/%q/%d, source = %#v",
			previewKey, previewVersion, previewMediaType, previewSize, source)
	}
	if workspaceCount != wantWorkspaces || purgedWorkspaceCount != wantWorkspaces ||
		receiptCount != wantWorkspaces || invalidReceiptCount != 0 {
		t.Fatalf("crash convergence workspaces/purged/receipts/invalid = %d/%d/%d/%d, want %d/%d/%d/0",
			workspaceCount, purgedWorkspaceCount, receiptCount, invalidReceiptCount,
			wantWorkspaces, wantWorkspaces, wantWorkspaces)
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatalf("read crash workspace root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("crash workspace residue = %#v", entries)
	}
}

func assertNoLocalBlobTemporaryResidue(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".blob-tmp-") || strings.HasPrefix(entry.Name(), ".tmp-") {
			return fmt.Errorf("temporary Blob residue at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
