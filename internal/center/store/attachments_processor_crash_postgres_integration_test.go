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
	fixture := newRecordsPostgresFixture(t, ctx)
	helperBinary := buildContentProcessorCrashHelper(t)
	blobRoot := filepath.Join(t.TempDir(), "blob-root")
	blob, err := attachments.NewLocalBlobStore(blobRoot)
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	databaseURL := contentProcessorCrashDatabaseURL(
		fixture.directRoleConfig(t, fixture.runtime, "content-processor-crash-helper"),
	)

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
			content := []byte(fmt.Sprintf("content processor crash cutpoint %d\n", index))
			digest := sha256.Sum256(content)
			version, err := blob.Put(ctx, attachments.PutRequest{
				ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
			}, bytes.NewReader(content))
			if err != nil {
				t.Fatalf("LocalBlobStore.Put(source) error = %v", err)
			}
			source := attachments.BlobObject{
				Key: version.Key, ObjectVersion: version.VersionID, SHA256: version.SHA256,
				SizeBytes: version.SizeBytes, BackendKind: attachments.BackendKindLocal,
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			seed := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
				Suffix: fmt.Sprintf("processcrash%d", index), Source: source,
				State: attachments.ProcessorStateQueued, Profile: attachments.ProcessorProfileText,
				CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				MaxAttempts: 3, ReservedQuotaBytes: source.SizeBytes,
			})
			workspaceRoot := filepath.Join(t.TempDir(), "processor-root")
			environment := contentProcessorCrashEnvironment(databaseURL, blobRoot, workspaceRoot)

			output, err := runContentProcessorCrashHelper(helperBinary, environment, "crash", cutpoint)
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != contentProcessorCrashExitCode {
				t.Fatalf("crash helper error = %v, output = %s", err, output)
			}
			expireAttachmentProcessorCrashState(t, ctx, fixture, seed.ProcessorJobID)
			if output, err := runContentProcessorCrashHelper(helperBinary, environment, "recover", ""); err != nil {
				t.Fatalf("recovery helper error = %v, output = %s", err, output)
			}
			if output, err := runContentProcessorCrashHelper(helperBinary, environment, "recover", ""); err != nil {
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
		})
	}
	assertNoLocalBlobTemporaryResidue(t, blobRoot)
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
	command := exec.Command("go", "test", "-c", "-o", binary, "./cmd/houfeng-content-processor")
	command.Dir = repositoryRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build content processor crash helper: %v\n%s", err, output)
	}
	return binary
}

func contentProcessorCrashEnvironment(databaseURL, blobRoot, workspaceRoot string) []string {
	return append(os.Environ(),
		"HOUFENG_CONTENT_PROCESSOR_CRASH_HELPER=1",
		"HOUFENG_DATABASE_URL="+databaseURL,
		"HOUFENG_ATTACHMENT_BLOB_BACKEND=local",
		"HOUFENG_ATTACHMENT_BLOB_ROOT="+blobRoot,
		"HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT="+workspaceRoot,
		"HOUFENG_CONTENT_PROCESSOR_OWNER_ID=processor_crash_helper",
		"HOUFENG_CONTENT_PROCESSOR_LEASE_DURATION=100ms",
		"HOUFENG_CONTENT_PROCESSOR_CLEANUP_TIMEOUT=5s",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS=10",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME=5s",
		"HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_RETRY_DELAY=1us",
	)
}

func runContentProcessorCrashHelper(binary string, environment []string, phase, cutpoint string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestContentProcessorCrashHelper$", "-test.v")
	command.Env = append(environment,
		"HOUFENG_CONTENT_PROCESSOR_CRASH_PHASE="+phase,
		"HOUFENG_CONTENT_PROCESSOR_CRASH_CUTPOINT="+cutpoint,
	)
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
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			return fmt.Errorf("temporary Blob residue at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
