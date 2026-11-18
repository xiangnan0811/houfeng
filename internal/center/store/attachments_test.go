package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

var _ attachments.ProcessorRepository = (*PostgresAttachmentRepository)(nil)

func TestAttachmentProcessorAdmissionRequiresWorkerGate(t *testing.T) {
	repository := &PostgresAttachmentRepository{}
	if err := repository.ConfigureProcessorAdmission(nil); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("nil gate = %v", err)
	}
	if err := repository.ConfigureProcessorAdmission(mustDeploymentMembershipAdmissionGate(t)); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("API gate = %v", err)
	}
	identity := testDeploymentMembershipIdentity()
	identity.InstanceKind = "worker"
	gate, err := NewDeploymentMembershipAdmissionGate(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProcessorAdmission(gate); err != nil {
		t.Fatalf("worker gate = %v", err)
	}
}

func TestPostgresAttachmentRepositoryReadsExactDownloadObjectAndOwnerRoute(t *testing.T) {
	t.Parallel()

	digest := bytes.Repeat([]byte{0x11}, sha256.Size)
	previewDigest := bytes.Repeat([]byte{0x22}, sha256.Size)
	row := &attachmentDownloadRow{values: []any{
		"default", "att_download1", "rdf_download1", "", "usr_0123456789abcdef01234567",
		"available", "notes.txt", "text/plain", int64(10),
		"sha256/" + strings.Repeat("11", sha256.Size), "original-v1", digest, int64(10),
		"sha256/" + strings.Repeat("22", sha256.Size), "preview-v1", previewDigest, int64(10),
		"text/plain; charset=utf-8",
	}}
	var expectedDigest [sha256.Size]byte
	for index := range expectedDigest {
		expectedDigest[index] = 0x11
	}
	expectedContent := attachments.AttachmentContent{ProjectID: "default", AttachmentID: "att_download1", RecordID: "rec_download1", AuthorID: "usr_0123456789abcdef01234567", State: attachments.UploadStateAvailable, DisplayName: "notes.txt", MediaType: "text/plain", LogicalSizeBytes: 10, Original: attachments.ObjectVersion{Key: "sha256/" + strings.Repeat("11", sha256.Size), VersionID: "original-v1", SHA256: expectedDigest, SizeBytes: 10}}
	if validationErr := expectedContent.Validate(); validationErr != nil {
		t.Logf("expected content validation = %v", validationErr)
	}
	tx := &downloadAttachmentTx{row: row}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	content, err := repository.GetAttachmentForDownload(context.Background(), attachments.ContentLookup{
		ProjectID: "default", AttachmentID: "att_download1",
	})
	if err != nil {
		t.Fatalf("GetAttachmentForDownload() error = %v", err)
	}
	if content.ProjectID != "default" || content.DraftID != "rdf_download1" || content.RecordID != "" ||
		content.AuthorID != "usr_0123456789abcdef01234567" || content.State != attachments.UploadStateAvailable ||
		content.Original.VersionID != "original-v1" || content.Original.SizeBytes != 10 ||
		content.Preview == nil || content.Preview.Object.VersionID != "preview-v1" ||
		content.Preview.MediaType != attachments.ManagedPreviewMediaTypeTextUTF8 {
		t.Fatalf("GetAttachmentForDownload() = %#v", content)
	}
	if !tx.committed || tx.rollbackCount != 1 {
		t.Fatalf("download read transaction committed=%t rollback=%d, want commit plus deferred rollback", tx.committed, tx.rollbackCount)
	}
}

func TestPostgresAttachmentRepositoryDownloadAssertionRejectsObjectDrift(t *testing.T) {
	t.Parallel()

	row := &attachmentDownloadRow{values: []any{
		"default", "att_download1", "", "rec_download1", "usr_0123456789abcdef01234567",
		"available", "notes.txt", "text/plain", int64(10),
		"sha256/" + strings.Repeat("11", sha256.Size), "original-v1", bytes.Repeat([]byte{0x11}, sha256.Size), int64(10),
		nil, nil, nil, nil, nil,
	}}
	tx := &downloadAttachmentTx{row: row}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	var assertionDigest [sha256.Size]byte
	for index := range assertionDigest {
		assertionDigest[index] = 0x11
	}
	assertion := attachments.ContentAssertion{
		ProjectID: "default", AttachmentID: "att_download1", RecordID: "rec_download1",
		AuthorID: "usr_0123456789abcdef01234567", Variant: attachments.ContentVariantOriginal,
		Object: attachments.ObjectVersion{
			Key: "sha256/" + strings.Repeat("11", sha256.Size), VersionID: "newer-v2",
			SHA256: assertionDigest, SizeBytes: 10,
		},
	}
	if err := repository.AssertAttachmentContent(context.Background(), assertion); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("AssertAttachmentContent() error = %v, want ErrAttachmentConflict", err)
	}
}

func TestPostgresAttachmentRepositoryDownloadAssertionRejectsOwnerRouteDrift(t *testing.T) {
	t.Parallel()

	digest := bytes.Repeat([]byte{0x11}, sha256.Size)
	row := &attachmentDownloadRow{values: []any{
		"default", "att_downloadroute", "rdf_downloadroute", "", "usr_0123456789abcdef01234567",
		"available", "notes.txt", "text/plain", int64(10),
		"sha256/" + strings.Repeat("11", sha256.Size), "original-v1", digest, int64(10),
		nil, nil, nil, nil, nil,
	}}
	tx := &downloadAttachmentTx{row: row}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	var assertionDigest [sha256.Size]byte
	for index := range assertionDigest {
		assertionDigest[index] = 0x11
	}
	assertion := attachments.ContentAssertion{
		ProjectID: "default", AttachmentID: "att_downloadroute",
		RecordID: "rec_downloadroute", AuthorID: "usr_0123456789abcdef01234567",
		Variant: attachments.ContentVariantOriginal,
		Object: attachments.ObjectVersion{
			Key: "sha256/" + strings.Repeat("11", sha256.Size), VersionID: "original-v1",
			SHA256: assertionDigest, SizeBytes: 10,
		},
	}
	if err := repository.AssertAttachmentContent(context.Background(), assertion); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("AssertAttachmentContent(owner route drift) error = %v, want ErrAttachmentConflict", err)
	}
	if !tx.committed || tx.rollbackCount != 1 {
		t.Fatalf("owner route assertion transaction committed=%t rollback=%d, want commit plus deferred rollback",
			tx.committed, tx.rollbackCount)
	}
}

type downloadAttachmentTx struct {
	pgx.Tx
	row           pgx.Row
	committed     bool
	rollbackCount int
}

func (tx *downloadAttachmentTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return tx.row
}

func (tx *downloadAttachmentTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected download Exec")
}

func (tx *downloadAttachmentTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *downloadAttachmentTx) Rollback(context.Context) error {
	tx.rollbackCount++
	return nil
}

type attachmentDownloadRow struct {
	values []any
}

func (row *attachmentDownloadRow) Scan(dest ...any) error {
	if len(dest) != len(row.values) {
		return fmt.Errorf("download scan destination count %d, want %d", len(dest), len(row.values))
	}
	for index, value := range row.values {
		if err := assignDownloadScanValue(dest[index], value); err != nil {
			return fmt.Errorf("download scan column %d: %w", index, err)
		}
	}
	return nil
}

func assignDownloadScanValue(destination any, value any) error {
	target := reflect.ValueOf(destination)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("invalid destination")
	}
	if value == nil {
		target.Elem().Set(reflect.Zero(target.Elem().Type()))
		return nil
	}
	if target.Elem().Kind() == reflect.Pointer {
		inner := reflect.New(target.Elem().Type().Elem())
		if err := assignDownloadScanValue(inner.Interface(), value); err != nil {
			return err
		}
		target.Elem().Set(inner)
		return nil
	}
	source := reflect.ValueOf(value)
	if !source.Type().AssignableTo(target.Elem().Type()) {
		return fmt.Errorf("%T is not assignable to %s", value, target.Elem().Type())
	}
	target.Elem().Set(source)
	return nil
}

func TestPostgresAttachmentRepositoryAttachmentProcessorClaimNoWorkCommitsWithoutWrites(t *testing.T) {
	t.Parallel()

	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{err: pgx.ErrNoRows}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	claim, err := repository.ClaimProcessorJob(context.Background(), attachments.ProcessorClaimInput{
		OwnerID: "processor_worker_1", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || claim != nil {
		t.Fatalf("ClaimProcessorJob(no work) = (%#v, %v), want nil, nil", claim, err)
	}
	if !tx.committed || len(tx.execSQL) != 0 {
		t.Fatalf("ClaimProcessorJob(no work) committed=%t execs=%#v", tx.committed, tx.execSQL)
	}
}

func TestPostgresAttachmentRepositoryClaimsTemporaryObjectCleanupByKnownKey(t *testing.T) {
	t.Parallel()

	candidate := testTemporaryObjectCleanupCandidate()
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		candidate.ProjectID, candidate.UploadID, candidate.AuthorID,
		candidate.TemporaryObjectKey, candidate.TemporaryObjectVersion,
		candidate.State, candidate.ExpiresAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	claimed, err := repository.ClaimTemporaryObjectCleanup(context.Background(), attachments.TemporaryObjectCleanupClaimInput{
		ProjectID: "default", RetryDelay: time.Minute,
	})
	if err != nil || claimed == nil || *claimed != candidate {
		t.Fatalf("ClaimTemporaryObjectCleanup() = (%#v, %v), want %#v", claimed, err, candidate)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("cleanup claim transaction = committed:%t rollback:%d query:%d exec:%d", tx.committed, tx.rollbackCount, len(tx.querySQL), len(tx.execSQL))
	}
	cleanupSQL := strings.ToLower(tx.querySQL[0])
	for _, fragment := range []string{
		"transport_kind = 's3'", "temporary_object_key is not null",
		"temporary_object_deleted_at is null", "temporary_object_cleanup_retry_at",
		"for update skip locked", "update public.attachment_uploads",
		"temporary_object_version", "$2 * interval '1 microsecond'",
	} {
		if !strings.Contains(cleanupSQL, fragment) {
			t.Errorf("cleanup claim SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
}

func TestPostgresAttachmentRepositoryMarksTemporaryObjectCleanupByExactVersion(t *testing.T) {
	t.Parallel()

	candidate := testTemporaryObjectCleanupCandidate()
	candidate.TemporaryObjectVersion = "observed-v1"
	tx := &fakeAttachmentProcessorTx{}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	if err := repository.MarkTemporaryObjectCleaned(context.Background(), candidate); err != nil {
		t.Fatalf("MarkTemporaryObjectCleaned() error = %v", err)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.execSQL) != 1 || len(tx.querySQL) != 0 {
		t.Fatalf("cleanup mark transaction = committed:%t rollback:%d query:%d exec:%d", tx.committed, tx.rollbackCount, len(tx.querySQL), len(tx.execSQL))
	}
	cleanupSQL := strings.ToLower(tx.execSQL[0])
	for _, fragment := range []string{
		"update public.attachment_uploads", "temporary_object_deleted_at",
		"temporary_object_cleanup_retry_at = null", "temporary_object_key = $",
		"temporary_object_version = $", "temporary_object_deleted_at is null",
	} {
		if !strings.Contains(cleanupSQL, fragment) {
			t.Errorf("cleanup mark SQL missing %q:\n%s", fragment, tx.execSQL[0])
		}
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorRenewalBindsObservedExpiry(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	renewedExpiry := claim.LeaseExpiresAt.Add(45 * time.Second)
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{renewedExpiry}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	renewed, err := repository.RenewProcessorClaim(context.Background(), attachments.ProcessorRenewInput{
		Claim: claim, OwnerLeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("RenewProcessorClaim() error = %v", err)
	}
	want := claim
	want.LeaseExpiresAt = renewedExpiry
	if renewed != want {
		t.Fatalf("RenewProcessorClaim() = %#v, want %#v", renewed, want)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 1 {
		t.Fatalf("RenewProcessorClaim() transaction = committed %t rollbacks %d queries %d",
			tx.committed, tx.rollbackCount, len(tx.querySQL))
	}
	assertAttachmentProcessorOwnerFenceSQL(t, tx.querySQL[0])
	assertAttachmentProcessorFullClaimFenceSQL(t, tx.querySQL[0], tx.queryArgs[0], claim)
	assertAttachmentProcessorObservedExpiryArgument(t, tx.queryArgs[0], claim.LeaseExpiresAt)
	if !strings.Contains(strings.ToLower(tx.querySQL[0]), "least(") ||
		!strings.Contains(strings.ToLower(tx.querySQL[0]), "expires_at") {
		t.Fatalf("renew SQL does not bound the lease by the overall expiry:\n%s", tx.querySQL[0])
	}
}

func TestPostgresAttachmentRepositoryProcessorWorkspaceRegistrationUsesExactLiveClaim(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	pathDigest := sha256.Sum256([]byte("processor workspace path"))
	want := attachments.ProcessorWorkspace{
		WorkspaceID: "cpw_processor1", ProcessorJobID: claim.ProcessorJobID,
		Attempt: claim.Attempt, State: attachments.ProcessorWorkspaceStateRegistered,
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.ExpiresAt.Add(-time.Minute),
	}
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		want.WorkspaceID,
		want.ProcessorJobID,
		want.Attempt,
		want.State,
		want.WorkspacePathDigest[:],
		want.ExpiresAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	workspace, err := repository.RegisterProcessorWorkspace(context.Background(), attachments.ProcessorWorkspaceRegistration{
		Claim: claim, WorkspaceID: want.WorkspaceID,
		WorkspacePathDigest: pathDigest, ExpiresAt: want.ExpiresAt,
	})
	if err != nil {
		t.Fatalf("RegisterProcessorWorkspace() error = %v", err)
	}
	if workspace != want {
		t.Fatalf("RegisterProcessorWorkspace() = %#v, want %#v", workspace, want)
	}
	if !tx.committed || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("RegisterProcessorWorkspace() transaction = committed %t queries %d execs %d",
			tx.committed, len(tx.querySQL), len(tx.execSQL))
	}
	registrationSQL := strings.ToLower(tx.querySQL[0])
	for _, fragment := range []string{
		"insert into public.content_processor_workspaces",
		"workspace_state",
		"registered",
		"workspace_path_digest",
		"from public.attachment_processor_jobs",
	} {
		if !strings.Contains(registrationSQL, fragment) {
			t.Errorf("workspace registration SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
	assertAttachmentProcessorOwnerFenceSQL(t, tx.querySQL[0])
	assertAttachmentProcessorFullClaimFenceSQL(t, tx.querySQL[0], tx.queryArgs[0], claim)
	assertAttachmentProcessorObservedExpiryArgument(t, tx.queryArgs[0], claim.LeaseExpiresAt)
	if !attachmentProcessorArgumentsContain(tx.queryArgs[0], want.WorkspaceID) ||
		!attachmentProcessorArgumentsContain(tx.queryArgs[0], want.Attempt) ||
		!attachmentProcessorArgumentsContain(tx.queryArgs[0], want.ExpiresAt) ||
		!attachmentProcessorArgumentsContainBytes(tx.queryArgs[0], pathDigest[:]) {
		t.Fatalf("workspace registration arguments = %#v, want exact ID/attempt/path/expiry", tx.queryArgs[0])
	}
}

func TestPostgresAttachmentRepositoryProcessorWorkspaceMaterializeIsExactAndIdempotent(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	pathDigest := sha256.Sum256([]byte("processor workspace path"))
	want := attachments.ProcessorWorkspace{
		WorkspaceID: "cpw_materialized1", ProcessorJobID: claim.ProcessorJobID,
		Attempt: claim.Attempt, State: attachments.ProcessorWorkspaceStateMaterialized,
		WorkspacePathDigest: pathDigest, ExpiresAt: claim.ExpiresAt.Add(-time.Minute),
	}
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		want.WorkspaceID, want.ProcessorJobID, want.Attempt, want.State,
		want.WorkspacePathDigest[:], want.ExpiresAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	transition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: want.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(claim),
	}
	workspace, err := repository.MaterializeProcessorWorkspace(context.Background(), transition)
	if err != nil {
		t.Fatalf("MaterializeProcessorWorkspace() error = %v", err)
	}
	if workspace != want || !tx.committed || len(tx.querySQL) != 1 {
		t.Fatalf("MaterializeProcessorWorkspace() = %#v committed=%t queries=%d, want %#v",
			workspace, tx.committed, len(tx.querySQL), want)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, fragment := range []string{
		"update public.content_processor_workspaces", "workspace_state = 'materialized'",
		"workspace_state = 'registered'", "workspace_path_digest", "not exists",
	} {
		if !strings.Contains(compact, fragment) {
			t.Errorf("materialize SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
}

func TestPostgresAttachmentRepositoryClaimsProcessorWorkspaceCleanupWithBoundedRetry(t *testing.T) {
	t.Parallel()

	pathDigest := sha256.Sum256([]byte("processor workspace cleanup path"))
	want := attachments.ProcessorWorkspaceCleanupCandidate{
		WorkspaceID: "cpw_cleanupclaim1", WorkspacePathDigest: pathDigest,
	}
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		want.WorkspaceID, want.WorkspacePathDigest[:],
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	retryDelay := 5 * time.Minute
	candidate, err := repository.ClaimProcessorWorkspaceCleanup(context.Background(), attachments.ProcessorWorkspaceCleanupClaimInput{
		ProjectID: "default", RetryDelay: retryDelay,
	})
	if err != nil {
		t.Fatalf("ClaimProcessorWorkspaceCleanup() error = %v", err)
	}
	if candidate == nil || *candidate != want || !tx.committed || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("ClaimProcessorWorkspaceCleanup() = %#v committed=%t queries=%d execs=%d, want %#v",
			candidate, tx.committed, len(tx.querySQL), len(tx.execSQL), want)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, fragment := range []string{
		"from public.content_processor_workspaces as workspace",
		"join public.attachment_processor_jobs as job",
		"join public.attachment_uploads as upload",
		"left join public.content_workspace_purge_receipts as receipt",
		"receipt.workspace_id is null",
		"workspace.workspace_state in ('registered', 'materialized', 'purging')",
		"job.processor_state in ('succeeded', 'rejected', 'expired')",
		"workspace.attempt < job.attempt",
		"for update of workspace skip locked",
		"update public.content_processor_workspaces as workspace",
		"workspace_state = 'purging'",
		"updated_at <= transaction_timestamp() - ($2 * interval '1 microsecond')",
		"order by workspace.expires_at, workspace.workspace_id",
		"limit 1",
	} {
		if !strings.Contains(compact, fragment) {
			t.Errorf("workspace cleanup claim SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
	if !attachmentProcessorArgumentsContain(tx.queryArgs[0], "default") ||
		!attachmentProcessorArgumentsContain(tx.queryArgs[0], retryDelay.Microseconds()) {
		t.Fatalf("workspace cleanup claim arguments = %#v", tx.queryArgs[0])
	}

	emptyTx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{err: pgx.ErrNoRows}}}
	repository.beginTx = func(context.Context, pgx.TxOptions) (attachmentTx, error) { return emptyTx, nil }
	candidate, err = repository.ClaimProcessorWorkspaceCleanup(context.Background(), attachments.ProcessorWorkspaceCleanupClaimInput{
		ProjectID: "default", RetryDelay: retryDelay,
	})
	if err != nil || candidate != nil || !emptyTx.committed {
		t.Fatalf("ClaimProcessorWorkspaceCleanup(empty) = (%#v, %v), committed=%t", candidate, err, emptyTx.committed)
	}
}

func TestPostgresAttachmentRepositoryProcessorWorkspacePurgeTransitionsAndImmutableReceipt(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	pathDigest := sha256.Sum256([]byte("processor workspace purge path"))
	expiresAt := claim.ExpiresAt.Add(-time.Minute)
	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	receipt, err := attachments.NewProcessorWorkspacePurgeReceipt("cpw_purge1", 2, verifiedAt)
	if err != nil {
		t.Fatalf("NewProcessorWorkspacePurgeReceipt() error = %v", err)
	}
	workspace := attachments.ProcessorWorkspace{
		WorkspaceID: receipt.WorkspaceID, ProcessorJobID: claim.ProcessorJobID,
		Attempt: claim.Attempt, State: attachments.ProcessorWorkspaceStatePurging,
		WorkspacePathDigest: pathDigest, ExpiresAt: expiresAt,
	}
	beginTx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		true,
		workspace.WorkspaceID, workspace.ProcessorJobID, workspace.Attempt, workspace.State,
		workspace.WorkspacePathDigest[:], workspace.ExpiresAt,
		false, "", []byte{}, []byte{}, int64(0), workspace.ExpiresAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return beginTx, nil },
	}
	transition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID: workspace.WorkspaceID, WorkspacePathDigest: pathDigest,
		Authorization: attachments.NewProcessorWorkspaceWorkerAuthorization(claim),
	}
	plan, err := repository.BeginProcessorWorkspacePurge(context.Background(), transition)
	if err != nil {
		t.Fatalf("BeginProcessorWorkspacePurge() error = %v", err)
	}
	if plan.Workspace != workspace || plan.Receipt != nil || !beginTx.committed {
		t.Fatalf("BeginProcessorWorkspacePurge() = %#v committed=%t", plan, beginTx.committed)
	}
	beginSQL := strings.ToLower(strings.Join(strings.Fields(beginTx.querySQL[0]), " "))
	for _, fragment := range []string{
		"workspace_state = 'purging'", "workspace_state in ('registered', 'materialized')",
		"content_workspace_purge_receipts", "workspace_path_digest",
	} {
		if !strings.Contains(beginSQL, fragment) {
			t.Errorf("begin purge SQL missing %q:\n%s", fragment, beginTx.querySQL[0])
		}
	}

	completeWorkspace := workspace
	completeWorkspace.State = attachments.ProcessorWorkspaceStatePurged
	completeTx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		true,
		completeWorkspace.WorkspaceID, completeWorkspace.ProcessorJobID, completeWorkspace.Attempt,
		completeWorkspace.State, completeWorkspace.WorkspacePathDigest[:], completeWorkspace.ExpiresAt,
		true, receipt.WorkspaceID, receipt.RemovedSurfaceDigest[:], receipt.ReceiptDigest[:],
		receipt.RemovedRowCount, receipt.VerifiedAbsentAt,
	}}}}
	repository.beginTx = func(context.Context, pgx.TxOptions) (attachmentTx, error) { return completeTx, nil }
	completed, err := repository.CompleteProcessorWorkspacePurge(context.Background(), attachments.ProcessorWorkspacePurgeCompletion{
		Workspace: transition, Receipt: receipt,
	})
	if err != nil {
		t.Fatalf("CompleteProcessorWorkspacePurge() error = %v", err)
	}
	if completed != receipt || !completeTx.committed {
		t.Fatalf("CompleteProcessorWorkspacePurge() = %#v committed=%t, want %#v", completed, completeTx.committed, receipt)
	}
	completeSQL := strings.ToLower(strings.Join(strings.Fields(completeTx.querySQL[0]), " "))
	for _, fragment := range []string{
		"insert into public.content_workspace_purge_receipts", "on conflict (workspace_id) do nothing",
		"workspace_state = 'purged'", "workspace_state = 'purging'", "workspace_path_digest",
		"from inserted_receipt", "where not exists (select 1 from inserted_receipt)",
	} {
		if !strings.Contains(completeSQL, fragment) {
			t.Errorf("complete purge SQL missing %q:\n%s", fragment, completeTx.querySQL[0])
		}
	}
}

func TestPostgresAttachmentRepositoryProcessorWorkspacePurgeReceiptOnlyReplay(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	receipt, err := attachments.NewProcessorWorkspacePurgeReceipt("cpw_receiptonly1", 2, verifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		false,
		"", "", int64(0), attachments.ProcessorWorkspaceState(""), []byte{}, verifiedAt,
		true, receipt.WorkspaceID, receipt.RemovedSurfaceDigest[:], receipt.ReceiptDigest[:],
		receipt.RemovedRowCount, receipt.VerifiedAbsentAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	transition := attachments.ProcessorWorkspaceTransition{
		WorkspaceID:         receipt.WorkspaceID,
		WorkspacePathDigest: sha256.Sum256([]byte("derived receipt-only workspace path")),
		Authorization:       attachments.NewProcessorWorkspaceReconciliationAuthorization(),
	}

	plan, err := repository.BeginProcessorWorkspacePurge(context.Background(), transition)
	if err != nil {
		t.Fatalf("BeginProcessorWorkspacePurge(receipt-only) error = %v", err)
	}
	if plan.Workspace != (attachments.ProcessorWorkspace{}) || plan.Receipt == nil || *plan.Receipt != receipt {
		t.Fatalf("BeginProcessorWorkspacePurge(receipt-only) = %#v, want zero workspace and %#v", plan, receipt)
	}
	if !tx.committed || len(tx.querySQL) != 1 {
		t.Fatalf("receipt-only replay transaction committed=%t queries=%d", tx.committed, len(tx.querySQL))
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	if !strings.Contains(compact, "not exists (select 1 from selected_workspace)") {
		t.Fatalf("begin purge SQL has no receipt-only branch:\n%s", tx.querySQL[0])
	}

	missingTx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{err: pgx.ErrNoRows}}}
	repository.beginTx = func(context.Context, pgx.TxOptions) (attachmentTx, error) { return missingTx, nil }
	if _, err := repository.BeginProcessorWorkspacePurge(context.Background(), transition); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("BeginProcessorWorkspacePurge(missing workspace and receipt) error = %v, want ErrAttachmentConflict", err)
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorCompletionBindsClaimedSourceAndProfileBeforeSQL(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	otherSource := attachmentProcessorBlob(0x4c, claim.Source.SizeBytes, "source-other-v1")
	preview := attachmentProcessorBlob(0x5d, 7, "preview-v1")
	tests := []struct {
		name   string
		result attachments.ProcessorResult
	}{
		{
			name: "source",
			result: attachments.ProcessorResult{
				Source: otherSource, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean,
				HasPreview: true, Preview: attachments.ManagedPreview{
					Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
				},
			},
		},
		{
			name: "profile",
			result: attachments.ProcessorResult{
				Source: claim.Source, Profile: attachments.ProcessorProfileArchive,
				Code: attachments.ProcessorResultCodeClean,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.result.Validate(); err != nil {
				t.Fatalf("test ProcessorResult.Validate() error = %v", err)
			}
			repository := &PostgresAttachmentRepository{beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) {
				t.Fatal("mismatched processor result must fail before opening a transaction")
				return nil, errors.New("unreachable")
			}}
			_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
				Claim: claim, Result: test.result, Limits: attachments.DefaultLimits(),
			})
			if !errors.Is(err, attachments.ErrAttachmentConflict) {
				t.Fatalf("CompleteProcessorJob(mismatched %s) error = %v, want ErrAttachmentConflict", test.name, err)
			}
		})
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorCompletionRejectsOversizedPreviewBeforeTransaction(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	result := attachments.ProcessorResult{
		Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean,
		HasPreview: true,
		Preview: attachments.ManagedPreview{
			Blob:      attachmentProcessorBlob(0x6d, attachments.DefaultLimits().MaxInlineTextPreviewBytes+1, "preview-oversized-v1"),
			MediaType: attachments.ManagedPreviewMediaTypeTextUTF8,
		},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("oversized test result shape error = %v", err)
	}
	opened := false
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) {
			opened = true
			return &fakeAttachmentProcessorTx{}, nil
		},
	}
	_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
		Claim: claim, Result: result, Limits: attachments.DefaultLimits(),
	})
	if !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(oversized preview) error = %v, want ErrInvalidProcessorCommand", err)
	}
	if opened {
		t.Fatal("CompleteProcessorJob(oversized preview) opened a transaction before durable-boundary rejection")
	}
}

func testAttachmentProcessorClaim() attachments.ProcessorClaim {
	expiresAt := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	return attachments.ProcessorClaim{
		ProjectID:         "default",
		ProcessorJobID:    "apj_processor1",
		UploadID:          "aup_processor1",
		AttachmentID:      "att_processor1",
		DisplayName:       "report.txt",
		DeclaredMediaType: "text/plain",
		Source:            attachmentProcessorBlob(0x3b, 19, "source-v1"),
		Profile:           attachments.ProcessorProfileText,
		Attempt:           3,
		MaxAttempts:       4,
		OwnerID:           "processor_worker_1",
		OwnerGeneration:   8,
		LeaseExpiresAt:    expiresAt.Add(-2 * time.Minute),
		ExpiresAt:         expiresAt,
	}
}

func testTemporaryObjectCleanupCandidate() attachments.TemporaryObjectCleanupCandidate {
	return attachments.TemporaryObjectCleanupCandidate{
		ProjectID: "default", UploadID: "aup_cleanup1", AuthorID: "usr_cleanup1",
		TemporaryObjectKey:     "temporary/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TemporaryObjectVersion: "observed-v1", State: attachments.UploadStateExpired,
		ExpiresAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	}
}

func attachmentProcessorBlob(value byte, size int64, version string) attachments.BlobObject {
	var digest [sha256.Size]byte
	for index := range digest {
		digest[index] = value
	}
	return attachments.BlobObject{
		Key: "sha256/" + hex.EncodeToString(digest[:]), SHA256: digest,
		ObjectVersion: version, SizeBytes: size, BackendKind: attachments.BackendKindLocal,
	}
}

type fakeAttachmentProcessorTx struct {
	pgx.Tx
	queryRows     []pgx.Row
	querySQL      []string
	queryArgs     [][]any
	execSQL       []string
	execArgs      [][]any
	execErr       error
	committed     bool
	rollbackCount int
}

func (tx *fakeAttachmentProcessorTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.querySQL = append(tx.querySQL, sql)
	tx.queryArgs = append(tx.queryArgs, append([]any(nil), args...))
	if len(tx.queryRows) == 0 {
		return fakeAttachmentRow{err: pgx.ErrNoRows}
	}
	row := tx.queryRows[0]
	tx.queryRows = tx.queryRows[1:]
	return row
}

func (tx *fakeAttachmentProcessorTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	tx.execArgs = append(tx.execArgs, append([]any(nil), args...))
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeAttachmentProcessorTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *fakeAttachmentProcessorTx) Rollback(context.Context) error {
	tx.rollbackCount++
	return nil
}

func assertAttachmentProcessorOwnerFenceSQL(t *testing.T, sql string) {
	t.Helper()
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	for _, fragment := range []string{
		"processor_job_id = $",
		"owner_id = $",
		"owner_generation = $",
		"attempt = $",
		"lease_expires_at = $",
		"lease_expires_at > transaction_timestamp()",
	} {
		if !strings.Contains(compact, fragment) {
			t.Errorf("processor owner fence SQL missing %q:\n%s", fragment, sql)
		}
	}
}

func assertAttachmentProcessorFullClaimFenceSQL(
	t *testing.T,
	sql string,
	arguments []any,
	claim attachments.ProcessorClaim,
) {
	t.Helper()
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	for _, fragment := range []string{
		"upload_id = $",
		"attachment_id = $",
		"processor_profile = $",
		"max_attempts = $",
		"expires_at = $",
		"from public.attachment_uploads",
		"join public.attachment_upload_parts",
		"part_number = 1",
		"project_id = $",
		"sha256_digest = $",
		"object_version = $",
		"size_bytes = $",
		"transport_kind = $",
	} {
		if !strings.Contains(compact, fragment) {
			t.Errorf("processor full claim fence SQL missing %q:\n%s", fragment, sql)
		}
	}
	for _, want := range []any{
		claim.UploadID,
		claim.AttachmentID,
		claim.Profile,
		claim.MaxAttempts,
		claim.ProjectID,
		claim.Source.ObjectVersion,
		claim.Source.SizeBytes,
		claim.Source.BackendKind,
	} {
		if !attachmentProcessorArgumentsContain(arguments, want) {
			t.Errorf("processor full claim fence arguments = %#v, missing %#v", arguments, want)
		}
	}
	if !attachmentProcessorArgumentsContainTime(arguments, claim.ExpiresAt) ||
		!attachmentProcessorArgumentsContainBytes(arguments, claim.Source.SHA256[:]) {
		t.Errorf("processor full claim fence arguments = %#v, missing expiry/source digest", arguments)
	}
}

func assertAttachmentProcessorObservedExpiryArgument(t *testing.T, arguments []any, want time.Time) {
	t.Helper()
	if !attachmentProcessorArgumentsContainTime(arguments, want) {
		t.Fatalf("processor SQL arguments = %#v, want DB-observed expiry %s", arguments, want)
	}
}

func attachmentProcessorArgumentsContain(arguments []any, want any) bool {
	for _, argument := range arguments {
		if reflect.DeepEqual(argument, want) {
			return true
		}
	}
	return false
}

func attachmentProcessorArgumentsContainBytes(arguments []any, want []byte) bool {
	for _, argument := range arguments {
		value, ok := argument.([]byte)
		if ok && reflect.DeepEqual(value, want) {
			return true
		}
	}
	return false
}

func attachmentProcessorArgumentsContainTime(arguments []any, want time.Time) bool {
	for _, argument := range arguments {
		value, ok := argument.(time.Time)
		if ok && value.Equal(want) {
			return true
		}
	}
	return false
}

type fakeAttachmentRow struct {
	values []any
	err    error
}

func (row fakeAttachmentRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected attachment scan destination count")
	}
	for index := range dest {
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(row.values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected attachment scan destination type")
		}
		target.Elem().Set(value)
	}
	return nil
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
