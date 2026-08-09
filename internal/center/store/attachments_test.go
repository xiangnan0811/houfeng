package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

var _ attachments.ProcessorRepository = (*PostgresAttachmentRepository)(nil)

func TestPostgresAttachmentRepositoryReserveUploadLocksOwnersBeforeQuota(t *testing.T) {
	t.Parallel()

	recordID := "rec_reserve1"
	tx := &fakeAttachmentTx{
		draftRecordID:        &recordID,
		quotaUsage:           attachments.QuotaUsage{LogicalBytes: 100, ReservedBytes: 200, PhysicalBytes: 80},
		effectiveRecordBytes: 300,
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	command := validReserveUploadCommand()

	result, err := repository.ReserveUpload(context.Background(), command)
	if err != nil {
		t.Fatalf("ReserveUpload() error = %v", err)
	}
	if result.UploadID != command.UploadID || result.AttachmentID != command.AttachmentID ||
		result.State != attachments.UploadStateCreated || result.Quota.Usage.ReservedBytes != 1224 ||
		result.Quota.EffectiveRecordBytes != 1324 {
		t.Fatalf("ReserveUpload() = %#v", result)
	}
	wantSteps := []string{
		"draft_route", "record_lock", "draft_lock", "quota_seed", "quota_lock",
		"effective_usage", "upload_reservation_exists", "attachment_insert", "upload_insert",
		"quota_update", "commit", "rollback",
	}
	if !reflect.DeepEqual(tx.steps, wantSteps) {
		t.Fatalf("ReserveUpload() steps = %#v, want %#v", tx.steps, wantSteps)
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

func TestPostgresAttachmentRepositoryReserveUploadRollsBackBeforeWritesOnQuotaFailure(t *testing.T) {
	t.Parallel()

	tx := &fakeAttachmentTx{
		quotaUsage: attachments.QuotaUsage{LogicalBytes: attachments.DefaultLimits().MaxProjectBytes - 512},
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	command := validReserveUploadCommand()

	_, err := repository.ReserveUpload(context.Background(), command)
	if !errors.Is(err, attachments.ErrQuotaExceeded) {
		t.Fatalf("ReserveUpload() error = %v, want ErrQuotaExceeded", err)
	}
	for _, step := range tx.steps {
		if strings.HasSuffix(step, "_insert") || step == "quota_update" || step == "commit" {
			t.Fatalf("ReserveUpload() quota failure performed write step %q: %#v", step, tx.steps)
		}
	}
	if len(tx.steps) == 0 || tx.steps[len(tx.steps)-1] != "rollback" {
		t.Fatalf("ReserveUpload() quota failure steps = %#v, want rollback", tx.steps)
	}
}

func TestPostgresAttachmentRepositoryCopyAttachmentLocksRecordsInSortedOrder(t *testing.T) {
	t.Parallel()

	tx := &fakeAttachmentTx{
		quotaUsage:           attachments.QuotaUsage{LogicalBytes: 100, PhysicalBytes: 80},
		effectiveRecordBytes: 200,
		copySource: fakeCopyAttachmentSource{
			displayName:  "source.txt",
			mediaType:    "text/plain",
			logicalBytes: 32,
			blobKey:      "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			blobVersion:  "local-v1",
		},
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	command := attachments.CopyAttachmentCommand{
		ProjectID:          "default",
		SourceRecordID:     "rec_zsource",
		TargetRecordID:     "rec_atarget",
		SourceAttachmentID: "att_source1",
		TargetAttachmentID: "att_target1",
		ActorID:            "usr_copy1",
		Limits:             attachments.DefaultLimits(),
	}

	result, err := repository.CopyAttachment(context.Background(), command)
	if err != nil {
		t.Fatalf("CopyAttachment() error = %v", err)
	}
	if !reflect.DeepEqual(tx.recordLockIDs, []string{"rec_atarget", "rec_zsource"}) {
		t.Fatalf("CopyAttachment() record lock order = %#v", tx.recordLockIDs)
	}
	if result.AttachmentID != command.TargetAttachmentID ||
		result.CopiedFromAttachmentID != command.SourceAttachmentID ||
		result.Quota.Usage != (attachments.QuotaUsage{LogicalBytes: 132, PhysicalBytes: 80}) ||
		result.Quota.EffectiveRecordBytes != 232 {
		t.Fatalf("CopyAttachment() = %#v", result)
	}
}

func TestPostgresAttachmentRepositoryAdmitUploadLocksQuotaBeforeUpload(t *testing.T) {
	t.Parallel()

	recordID := "rec_admit1"
	tx := validLifecycleAttachmentTx(attachments.UploadStateQuarantined)
	tx.draftRecordID = &recordID
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	if _, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand()); err != nil {
		t.Fatalf("AdmitUpload() error = %v", err)
	}
	wantPrefix := []string{
		"upload_route", "draft_route", "record_lock", "draft_lock",
		"quota_seed", "quota_lock", "upload_lock",
	}
	if len(tx.steps) < len(wantPrefix) || !reflect.DeepEqual(tx.steps[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("AdmitUpload() lock steps = %#v, want prefix %#v", tx.steps, wantPrefix)
	}
}

func TestPostgresAttachmentRepositoryRejectsUploadRoutingDriftBeforeWrites(t *testing.T) {
	t.Parallel()

	baseTx := validLifecycleAttachmentTx(attachments.UploadStateCreated)
	tx := &routeDriftAttachmentTx{fakeAttachmentTx: baseTx}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	_, err := repository.StartUpload(context.Background(), attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: "aup_lifecycle1", AuthorID: "usr_lifecycle1",
	})
	if !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("StartUpload() routing drift error = %v, want ErrAttachmentConflict", err)
	}
	if slicesContain(baseTx.steps, "upload_state_update") || slicesContain(baseTx.steps, "commit") ||
		len(baseTx.steps) == 0 || baseTx.steps[len(baseTx.steps)-1] != "rollback" {
		t.Fatalf("StartUpload() routing drift steps = %#v", baseTx.steps)
	}
}

func TestPostgresAttachmentRepositoryLegacyCompletionCannotBypassPartAndProcessorEnqueue(t *testing.T) {
	t.Parallel()

	tx := validLifecycleAttachmentTx(attachments.UploadStateUploading)
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	_, err := repository.CompleteUploadContent(context.Background(), validCompleteUploadContentCommand())
	if !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CompleteUploadContent(initial) error = %v, want ErrAttachmentConflict", err)
	}
	if slicesContain(tx.steps, "upload_content_complete") || slicesContain(tx.steps, "logical_state_update") ||
		slicesContain(tx.steps, "commit") {
		t.Fatalf("CompleteUploadContent(initial) bypassed atomic completion: %#v", tx.steps)
	}
}

func TestPostgresAttachmentRepositoryLifecycleWriteCutpointsRollBack(t *testing.T) {
	t.Parallel()

	cutpointError := errors.New("attachment lifecycle cutpoint")
	tests := []struct {
		name     string
		state    attachments.UploadState
		cutpoint string
		run      func(*PostgresAttachmentRepository) error
	}{
		{name: "admit Blob row", state: attachments.UploadStateQuarantined, cutpoint: "blob_insert", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand())
			return err
		}},
		{name: "admit attachment row", state: attachments.UploadStateQuarantined, cutpoint: "attachment_admit", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand())
			return err
		}},
		{name: "admit upload row", state: attachments.UploadStateQuarantined, cutpoint: "upload_state_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand())
			return err
		}},
		{name: "admit quota row", state: attachments.UploadStateQuarantined, cutpoint: "quota_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand())
			return err
		}},
		{name: "admit result read", state: attachments.UploadStateQuarantined, cutpoint: "effective_usage", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand())
			return err
		}},
		{name: "reject upload row", state: attachments.UploadStateUploading, cutpoint: "upload_state_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.FailUpload(context.Background(), validFailUploadCommand())
			return err
		}},
		{name: "reject attachment row", state: attachments.UploadStateUploading, cutpoint: "logical_state_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.FailUpload(context.Background(), validFailUploadCommand())
			return err
		}},
		{name: "reject quota row", state: attachments.UploadStateUploading, cutpoint: "quota_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.FailUpload(context.Background(), validFailUploadCommand())
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := validLifecycleAttachmentTx(test.state)
			if test.cutpoint == "effective_usage" {
				tx.queryErrors = map[string]error{test.cutpoint: cutpointError}
			} else {
				tx.execErrors = map[string]error{test.cutpoint: cutpointError}
			}
			repository := &PostgresAttachmentRepository{
				beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
			}
			if err := test.run(repository); !errors.Is(err, cutpointError) {
				t.Fatalf("lifecycle cutpoint error = %v, want injected error", err)
			}
			if slicesContain(tx.steps, "commit") || len(tx.steps) == 0 || tx.steps[len(tx.steps)-1] != "rollback" {
				t.Fatalf("lifecycle cutpoint steps = %#v, want rollback without commit", tx.steps)
			}
		})
	}
}

func TestPostgresAttachmentRepositoryReserveUploadRejectsInvalidPersistedQuotaStateBeforeWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		usage        attachments.QuotaUsage
		quotaVersion int64
		want         error
	}{
		{name: "negative logical", usage: attachments.QuotaUsage{LogicalBytes: -1}, want: attachments.ErrInvalidQuotaUsage},
		{name: "negative reserved", usage: attachments.QuotaUsage{ReservedBytes: -1}, want: attachments.ErrInvalidQuotaUsage},
		{name: "negative physical", usage: attachments.QuotaUsage{PhysicalBytes: -1}, want: attachments.ErrInvalidQuotaUsage},
		{name: "negative version", quotaVersion: -1, want: attachments.ErrInvalidQuotaUsage},
		{name: "version overflow", quotaVersion: math.MaxInt64, want: attachments.ErrQuotaOverflow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeAttachmentTx{quotaUsage: test.usage, quotaVersion: test.quotaVersion}
			repository := &PostgresAttachmentRepository{
				beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
			}
			if _, err := repository.ReserveUpload(context.Background(), validReserveUploadCommand()); !errors.Is(err, test.want) {
				t.Fatalf("ReserveUpload() error = %v, want %v", err, test.want)
			}
			for _, step := range tx.steps {
				if step == "attachment_insert" || step == "upload_insert" || step == "quota_update" || step == "commit" {
					t.Fatalf("ReserveUpload() invalid persisted state performed %q: %#v", step, tx.steps)
				}
			}
		})
	}
}

func TestPostgresAttachmentRepositoryAdmitUploadRejectsQuotaVersionOverflowBeforeWrites(t *testing.T) {
	t.Parallel()

	tx := validLifecycleAttachmentTx(attachments.UploadStateQuarantined)
	tx.quotaVersion = math.MaxInt64
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	if _, err := repository.AdmitUpload(context.Background(), validAdmitUploadCommand()); !errors.Is(err, attachments.ErrQuotaOverflow) {
		t.Fatalf("AdmitUpload() error = %v, want ErrQuotaOverflow", err)
	}
	for _, step := range tx.steps {
		if step == "blob_insert" || step == "attachment_admit" || step == "upload_state_update" ||
			step == "quota_update" || step == "commit" {
			t.Fatalf("AdmitUpload() quota version overflow performed %q: %#v", step, tx.steps)
		}
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorClaimUsesDeterministicSkipLockedAndReturnsExactToken(t *testing.T) {
	t.Parallel()

	want := testAttachmentProcessorClaim()
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		want.ProjectID,
		want.ProcessorJobID,
		want.UploadID,
		want.AttachmentID,
		want.DisplayName,
		want.DeclaredMediaType,
		want.Source.SHA256[:],
		want.Source.ObjectVersion,
		want.Source.SizeBytes,
		want.Source.BackendKind,
		want.Profile,
		want.Attempt,
		want.MaxAttempts,
		want.OwnerID,
		want.OwnerGeneration,
		want.LeaseExpiresAt,
		want.ExpiresAt,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	claim, err := repository.ClaimProcessorJob(context.Background(), attachments.ProcessorClaimInput{
		OwnerID: "processor_worker_1", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimProcessorJob() error = %v", err)
	}
	if claim == nil || *claim != want {
		t.Fatalf("ClaimProcessorJob() = %#v, want %#v", claim, want)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("ClaimProcessorJob() transaction = committed %t rollbacks %d queries %d execs %d",
			tx.committed, tx.rollbackCount, len(tx.querySQL), len(tx.execSQL))
	}
	claimSQL := strings.ToLower(tx.querySQL[0])
	for _, fragment := range []string{
		"from public.attachment_processor_jobs",
		"join public.attachment_uploads",
		"join public.record_attachments",
		"join public.attachment_upload_parts",
		"attachment.display_name",
		"attachment.media_type",
		"part_number = 1",
		"processor_state = 'queued'",
		"processor_state = 'retry_wait'",
		"retry_at <= transaction_timestamp()",
		"processor_state = 'claimed'",
		"lease_expires_at <= transaction_timestamp()",
		"attempt < max_attempts",
		"expires_at > transaction_timestamp()",
		"order by",
		"created_at",
		"processor_job_id",
		"for update skip locked",
		"attempt =",
		"attempt + 1",
		"owner_generation =",
		"owner_generation + 1",
		"result_owner_id = ''",
		"result_lease_expires_at = null",
		"least(",
		"transaction_timestamp()",
		"returning",
	} {
		if !strings.Contains(claimSQL, fragment) {
			t.Errorf("claim SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
	if len(tx.queryArgs) != 1 || len(tx.queryArgs[0]) < 2 ||
		tx.queryArgs[0][0] != "processor_worker_1" || tx.queryArgs[0][1] != (5*time.Minute).Microseconds() {
		t.Fatalf("claim SQL arguments = %#v, want owner and lease microseconds", tx.queryArgs)
	}
	if !claim.LeaseExpiresAt.Before(claim.ExpiresAt) && !claim.LeaseExpiresAt.Equal(claim.ExpiresAt) {
		t.Fatalf("claim lease expiry %s exceeds overall expiry %s", claim.LeaseExpiresAt, claim.ExpiresAt)
	}
	if claim.Source != want.Source || claim.Attempt != 3 || claim.OwnerGeneration != 8 {
		t.Fatalf("claim durable identity = %#v, want persisted source and incremented attempt/generation", claim)
	}
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

func TestPostgresAttachmentRepositoryExpiresAbandonedUploadAndReleasesReservation(t *testing.T) {
	t.Parallel()

	const reservedBytes = int64(9)
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{
		fakeAttachmentRow{values: []any{int64(0), reservedBytes, int64(0), int64(4)}},
		fakeAttachmentRow{values: []any{
			"default", "aup_abandoned1", "att_abandoned1", "rdf_abandoned1", "usr_abandoned1",
			attachments.UploadStateUploading, reservedBytes, reservedBytes,
			(*int64)(nil), []byte(nil),
			(*string)(nil), (*string)(nil), []byte(nil),
			attachments.UploadStateUploading,
		}},
		fakeAttachmentRow{values: []any{int64(0)}},
	}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.ExpireAbandonedUpload(context.Background(), attachments.AbandonedUploadExpiryInput{
		ProjectID: "default", Limits: attachments.DefaultLimits(),
	})
	if err != nil || result == nil {
		t.Fatalf("ExpireAbandonedUpload() = (%#v, %v), want expired result", result, err)
	}
	if result.UploadID != "aup_abandoned1" || result.AttachmentID != "att_abandoned1" ||
		result.State != attachments.UploadStateExpired || result.Quota.Usage != (attachments.QuotaUsage{}) {
		t.Fatalf("ExpireAbandonedUpload() = %#v", result)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 3 || len(tx.execSQL) != 3 {
		t.Fatalf("abandoned expiry transaction = committed %t rollbacks %d queries %d execs %d",
			tx.committed, tx.rollbackCount, len(tx.querySQL), len(tx.execSQL))
	}
	candidateSQL := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[1]), " "))
	for _, fragment := range []string{
		"from public.attachment_uploads as upload",
		"join public.record_attachments as attachment",
		"upload.upload_state in ('created', 'uploading')",
		"upload.expires_at <= transaction_timestamp()",
		"not exists",
		"from public.attachment_processor_jobs",
		"for update of upload skip locked",
	} {
		if !strings.Contains(candidateSQL, fragment) {
			t.Errorf("abandoned expiry candidate SQL missing %q:\n%s", fragment, tx.querySQL[1])
		}
	}
	quotaArgs := tx.execArgs[2]
	if len(quotaArgs) != 6 || quotaArgs[0] != "default" || quotaArgs[2] != int64(0) ||
		quotaArgs[4] != int64(5) || quotaArgs[5] != int64(4) {
		t.Fatalf("abandoned expiry quota update args = %#v", quotaArgs)
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorBoundedExpiryNoWorkIsReadOnly(t *testing.T) {
	t.Parallel()

	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{
		fakeAttachmentRow{values: []any{int64(0), int64(0), int64(0), int64(0)}},
		fakeAttachmentRow{err: pgx.ErrNoRows},
	}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.ExpireBoundedProcessorJob(context.Background(), attachments.ProcessorExpiryInput{
		ProjectID: "default", OwnerID: "processor_expiry_reaper", Limits: attachments.DefaultLimits(),
	})
	if err != nil || result != nil {
		t.Fatalf("ExpireBoundedProcessorJob(no work) = (%#v, %v), want nil, nil", result, err)
	}
	if !tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 2 || len(tx.execSQL) != 0 {
		t.Fatalf("ExpireBoundedProcessorJob(no work) transaction = committed %t rollbacks %d queries %d execs %#v",
			tx.committed, tx.rollbackCount, len(tx.querySQL), tx.execSQL)
	}
	quotaSQL := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	if !strings.Contains(quotaSQL, "from public.attachment_quota_accounts") ||
		!strings.Contains(quotaSQL, "for update") || strings.Contains(quotaSQL, "insert") {
		t.Fatalf("bounded expiry quota lock is not read-only on no work:\n%s", tx.querySQL[0])
	}
	candidateSQL := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[1]), " "))
	for _, fragment := range []string{
		"from public.attachment_processor_jobs as job",
		"join public.attachment_uploads as upload",
		"upload.project_id = $",
		"job.processor_state in ('queued', 'retry_wait', 'claimed')",
		"job.expires_at <= transaction_timestamp()",
		"job.attempt >= job.max_attempts",
		"job.processor_state <> 'claimed'",
		"job.lease_expires_at <= transaction_timestamp()",
		"upload.upload_state = 'quarantined'",
		"attachment.attachment_state = 'quarantined'",
		"order by job.created_at, job.processor_job_id",
		"for update of job skip locked",
	} {
		if !strings.Contains(candidateSQL, fragment) {
			t.Errorf("bounded expiry candidate SQL missing %q:\n%s", fragment, tx.querySQL[1])
		}
	}
}

func TestPostgresAttachmentRepositoryAttachmentProcessorBoundedExpiryFailsClosedWithoutQuotaAccount(t *testing.T) {
	t.Parallel()

	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{
		fakeAttachmentRow{err: pgx.ErrNoRows},
		fakeAttachmentRow{values: []any{true}},
	}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}

	result, err := repository.ExpireBoundedProcessorJob(context.Background(), attachments.ProcessorExpiryInput{
		ProjectID: "default", OwnerID: "processor_expiry_reaper", Limits: attachments.DefaultLimits(),
	})
	if !errors.Is(err, attachments.ErrAttachmentConflict) || result != nil {
		t.Fatalf("ExpireBoundedProcessorJob(missing quota account) = (%#v, %v), want ErrAttachmentConflict",
			result, err)
	}
	if tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 2 || len(tx.execSQL) != 0 {
		t.Fatalf("missing-quota bounded expiry transaction = committed %t rollbacks %d queries %d execs %#v",
			tx.committed, tx.rollbackCount, len(tx.querySQL), tx.execSQL)
	}
	activeJobSQL := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[1]), " "))
	for _, fragment := range []string{
		"select exists",
		"from public.attachment_processor_jobs as job",
		"join public.attachment_uploads as upload",
		"upload.project_id = $",
		"job.processor_state in ('queued', 'retry_wait', 'claimed')",
	} {
		if !strings.Contains(activeJobSQL, fragment) {
			t.Errorf("missing-quota active-job check missing %q:\n%s", fragment, tx.querySQL[1])
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
	if !tx.committed || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("RenewProcessorClaim() transaction = committed %t queries %d execs %d",
			tx.committed, len(tx.querySQL), len(tx.execSQL))
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

func TestPostgresAttachmentRepositoryAttachmentProcessorClaimOwnedMutationsRejectStaleFenceWithoutWrites(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	result := attachments.ProcessorResult{
		Source: claim.Source, Profile: claim.Profile,
		Code: attachments.ProcessorResultCodeScannerUnavailable,
	}
	pathDigest := sha256.Sum256([]byte("stale processor workspace"))
	tests := []struct {
		name string
		run  func(*PostgresAttachmentRepository) error
	}{
		{name: "renew", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.RenewProcessorClaim(context.Background(), attachments.ProcessorRenewInput{
				Claim: claim, OwnerLeaseDuration: time.Minute,
			})
			return err
		}},
		{name: "workspace registration", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.RegisterProcessorWorkspace(context.Background(), attachments.ProcessorWorkspaceRegistration{
				Claim: claim, WorkspaceID: "cpw_staleprocessor1",
				WorkspacePathDigest: pathDigest, ExpiresAt: claim.ExpiresAt.Add(-time.Minute),
			})
			return err
		}},
		{name: "completion", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
				Claim: claim, Result: result, RetryAt: claim.ExpiresAt.Add(time.Minute),
				Limits: attachments.DefaultLimits(),
			})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeAttachmentProcessorTx{}
			repository := &PostgresAttachmentRepository{
				beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
			}
			if err := test.run(repository); !errors.Is(err, attachments.ErrProcessorClaimLost) {
				t.Fatalf("claim-owned mutation error = %v, want ErrProcessorClaimLost", err)
			}
			if tx.committed || tx.rollbackCount == 0 || len(tx.execSQL) != 0 {
				t.Fatalf("stale mutation transaction = committed %t rollbacks %d execs %#v",
					tx.committed, tx.rollbackCount, tx.execSQL)
			}
			fencedSQL := ""
			for _, sql := range tx.querySQL {
				if attachmentProcessorSQLHasOwnerFence(sql) {
					fencedSQL = sql
					break
				}
			}
			if fencedSQL == "" {
				t.Fatalf("stale mutation queries contain no exact live owner fence: %#v", tx.querySQL)
			}
			assertAttachmentProcessorOwnerFenceSQL(t, fencedSQL)
			for _, arguments := range tx.queryArgs {
				if attachmentProcessorArgumentsContainTime(arguments, claim.LeaseExpiresAt) {
					return
				}
			}
			t.Fatalf("stale mutation arguments do not contain DB-observed expiry %s: %#v",
				claim.LeaseExpiresAt, tx.queryArgs)
		})
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

func TestPostgresAttachmentRepositoryAttachmentProcessorCompletionRejectsPastRetryBeforeExec(t *testing.T) {
	t.Parallel()

	claim := testAttachmentProcessorClaim()
	databaseNow := claim.LeaseExpiresAt.Add(-time.Minute)
	leaseExpiresAt := claim.LeaseExpiresAt
	var retryAt *time.Time
	var resultCode *attachments.ProcessorResultCode
	tx := &fakeAttachmentProcessorTx{queryRows: []pgx.Row{fakeAttachmentRow{values: []any{
		claim.ProcessorJobID,
		claim.UploadID,
		claim.AttachmentID,
		attachments.ProcessorStateClaimed,
		claim.Profile,
		claim.Attempt,
		claim.MaxAttempts,
		claim.OwnerID,
		claim.OwnerGeneration,
		&leaseExpiresAt,
		retryAt,
		resultCode,
		[]byte(nil),
		"",
		(*time.Time)(nil),
		claim.ExpiresAt,
		databaseNow,
	}}}}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	result := attachments.ProcessorResult{
		Source: claim.Source, Profile: claim.Profile,
		Code: attachments.ProcessorResultCodeScannerUnavailable,
	}

	_, err := repository.CompleteProcessorJob(context.Background(), attachments.ProcessorCompletionInput{
		Claim: claim, Result: result, RetryAt: databaseNow.Add(-time.Second),
		Limits: attachments.DefaultLimits(),
	})
	if !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
		t.Fatalf("CompleteProcessorJob(past RetryAt) error = %v, want ErrInvalidProcessorCommand", err)
	}
	if tx.committed || tx.rollbackCount == 0 || len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
		t.Fatalf("past RetryAt transaction = committed %t rollbacks %d queries %d execs %#v",
			tx.committed, tx.rollbackCount, len(tx.querySQL), tx.execSQL)
	}
}

func validReserveUploadCommand() attachments.ReserveUploadCommand {
	return attachments.ReserveUploadCommand{
		ProjectID:         "default",
		UploadID:          "aup_reserve1",
		AttachmentID:      "att_reserve1",
		DraftID:           "rdf_reserve1",
		AuthorID:          "usr_reserve1",
		DisplayName:       "report.txt",
		MediaType:         "text/plain",
		TransportKind:     attachments.TransportKindLocal,
		DeclaredSizeBytes: 1024,
		ExpiresAt:         time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		Limits:            attachments.DefaultLimits(),
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

func attachmentProcessorSQLHasOwnerFence(sql string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	return strings.Contains(compact, "processor_job_id = $") &&
		strings.Contains(compact, "owner_id = $") &&
		strings.Contains(compact, "owner_generation = $") &&
		strings.Contains(compact, "attempt = $") &&
		strings.Contains(compact, "lease_expires_at = $") &&
		strings.Contains(compact, "lease_expires_at > transaction_timestamp()")
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

type fakeAttachmentTx struct {
	pgx.Tx
	steps                  []string
	recordLockIDs          []string
	draftRecordID          *string
	quotaUsage             attachments.QuotaUsage
	quotaVersion           int64
	effectiveRecordBytes   int64
	copySource             fakeCopyAttachmentSource
	uploadState            attachments.UploadState
	declaredSizeBytes      int64
	reservedSizeBytes      int64
	actualSizeBytes        *int64
	actualSHA256           []byte
	temporaryObjectKey     *string
	temporaryObjectVersion *string
	completionFingerprint  []byte
	queryErrors            map[string]error
	execErrors             map[string]error
}

type fakeCopyAttachmentSource struct {
	displayName  string
	mediaType    string
	logicalBytes int64
	blobKey      string
	blobVersion  string
}

type routeDriftAttachmentTx struct {
	*fakeAttachmentTx
}

func (tx *routeDriftAttachmentTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	step := attachmentSQLStep(sql)
	if step != "upload_lock" {
		return tx.fakeAttachmentTx.QueryRow(ctx, sql, args...)
	}
	tx.steps = append(tx.steps, step)
	return routeDriftAttachmentRow{tx: tx.fakeAttachmentTx}
}

type routeDriftAttachmentRow struct {
	tx *fakeAttachmentTx
}

func (row routeDriftAttachmentRow) Scan(dest ...any) error {
	stateValues := []any{
		row.tx.uploadState,
		row.tx.declaredSizeBytes,
		row.tx.reservedSizeBytes,
		row.tx.actualSizeBytes,
		row.tx.actualSHA256,
		row.tx.temporaryObjectKey,
		row.tx.temporaryObjectVersion,
		row.tx.completionFingerprint,
	}
	if len(dest) == len(stateValues) {
		return fakeAttachmentRow{values: stateValues}.Scan(dest...)
	}
	lockedValues := append([]any{
		"default", "aup_lifecycle1", "att_drifted1", "rdf_lifecycle1", "usr_lifecycle1",
	}, stateValues...)
	return fakeAttachmentRow{values: lockedValues}.Scan(dest...)
}

func (tx *fakeAttachmentTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	step := attachmentSQLStep(sql)
	tx.steps = append(tx.steps, step)
	if err := tx.queryErrors[step]; err != nil {
		return fakeAttachmentRow{err: err}
	}
	switch step {
	case "upload_route":
		return fakeAttachmentRow{values: []any{"default", "aup_lifecycle1", "att_lifecycle1", "rdf_lifecycle1", "usr_lifecycle1"}}
	case "draft_route", "draft_lock":
		return fakeAttachmentRow{values: []any{"default", tx.draftRecordID}}
	case "record_lock":
		recordID := args[1].(string)
		tx.recordLockIDs = append(tx.recordLockIDs, recordID)
		return fakeAttachmentRow{values: []any{recordID}}
	case "quota_lock":
		return fakeAttachmentRow{values: []any{
			tx.quotaUsage.LogicalBytes,
			tx.quotaUsage.ReservedBytes,
			tx.quotaUsage.PhysicalBytes,
			tx.quotaVersion,
		}}
	case "upload_lock":
		return fakeAttachmentRow{values: []any{
			"default",
			"aup_lifecycle1",
			"att_lifecycle1",
			"rdf_lifecycle1",
			"usr_lifecycle1",
			tx.uploadState,
			tx.declaredSizeBytes,
			tx.reservedSizeBytes,
			tx.actualSizeBytes,
			tx.actualSHA256,
			tx.temporaryObjectKey,
			tx.temporaryObjectVersion,
			tx.completionFingerprint,
		}}
	case "effective_usage":
		return fakeAttachmentRow{values: []any{tx.effectiveRecordBytes}}
	case "upload_reservation_exists":
		return fakeAttachmentRow{values: []any{false}}
	case "blob_gc_fence":
		return fakeAttachmentRow{values: []any{false}}
	case "copy_source":
		return fakeAttachmentRow{values: []any{
			tx.copySource.displayName,
			tx.copySource.mediaType,
			tx.copySource.logicalBytes,
			tx.copySource.blobKey,
			tx.copySource.blobVersion,
		}}
	default:
		return fakeAttachmentRow{err: errors.New("unexpected attachment query step " + step)}
	}
}

func (tx *fakeAttachmentTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	step := attachmentSQLStep(sql)
	tx.steps = append(tx.steps, step)
	if err := tx.execErrors[step]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *fakeAttachmentTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	return nil
}

func (tx *fakeAttachmentTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
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

func attachmentSQLStep(sql string) string {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.HasPrefix(compact, "lock table public.blob_objects"):
		return "blob_publication_lock"
	case strings.HasPrefix(compact, "lock table public.attachment_upload_parts"):
		return "upload_part_publication_lock"
	case strings.Contains(compact, "insert into public.blob_objects"):
		return "blob_insert"
	case strings.Contains(compact, "from public.blob_gc_deletions"):
		return "blob_gc_fence"
	case strings.Contains(compact, "from public.attachment_uploads") && strings.Contains(compact, "for update"):
		return "upload_lock"
	case strings.Contains(compact, "select exists(") && strings.Contains(compact, "from public.attachment_uploads"):
		return "upload_reservation_exists"
	case strings.Contains(compact, "from public.attachment_uploads"):
		return "upload_route"
	case strings.Contains(compact, "from public.record_drafts") && strings.Contains(compact, "for update"):
		return "draft_lock"
	case strings.Contains(compact, "from public.record_drafts"):
		return "draft_route"
	case strings.Contains(compact, "from public.records") && strings.Contains(compact, "for update"):
		return "record_lock"
	case strings.Contains(compact, "insert into public.attachment_quota_accounts"):
		return "quota_seed"
	case strings.Contains(compact, "from public.attachment_quota_accounts") && strings.Contains(compact, "for update"):
		return "quota_lock"
	case strings.Contains(compact, "coalesce(sum(logical_size_bytes)"):
		return "effective_usage"
	case strings.Contains(compact, "from public.record_attachments") && strings.Contains(compact, "join public.blob_objects"):
		return "copy_source"
	case strings.Contains(compact, "insert into public.record_attachments"):
		return "attachment_insert"
	case strings.Contains(compact, "insert into public.attachment_uploads"):
		return "upload_insert"
	case strings.Contains(compact, "update public.attachment_uploads") && strings.Contains(compact, "actual_size_bytes"):
		return "upload_content_complete"
	case strings.Contains(compact, "update public.attachment_uploads"):
		return "upload_state_update"
	case strings.Contains(compact, "update public.record_attachments") && strings.Contains(compact, "blob_key"):
		return "attachment_admit"
	case strings.Contains(compact, "update public.record_attachments"):
		return "logical_state_update"
	case strings.Contains(compact, "update public.attachment_quota_accounts"):
		return "quota_update"
	default:
		return "unknown"
	}
}

func validLifecycleAttachmentTx(state attachments.UploadState) *fakeAttachmentTx {
	actualSize := int64(9)
	temporaryKey := "tmp/lifecycle1"
	temporaryVersion := "tmp-v1"
	return &fakeAttachmentTx{
		quotaUsage:             attachments.QuotaUsage{ReservedBytes: 9},
		quotaVersion:           3,
		effectiveRecordBytes:   0,
		uploadState:            state,
		declaredSizeBytes:      9,
		reservedSizeBytes:      9,
		actualSizeBytes:        &actualSize,
		actualSHA256:           bytesOf(0xbb, 32),
		temporaryObjectKey:     &temporaryKey,
		temporaryObjectVersion: &temporaryVersion,
		completionFingerprint:  bytesOf(0xcc, 32),
	}
}

func validCompleteUploadContentCommand() attachments.CompleteUploadContentCommand {
	var digest, fingerprint [32]byte
	copy(digest[:], bytesOf(0xbb, 32))
	copy(fingerprint[:], bytesOf(0xcc, 32))
	return attachments.CompleteUploadContentCommand{
		ProjectID: "default", UploadID: "aup_lifecycle1", AuthorID: "usr_lifecycle1",
		ActualSizeBytes: 9, ActualSHA256: digest, TemporaryObjectKey: "tmp/lifecycle1",
		TemporaryObjectVersion: "tmp-v1", CompletionFingerprint: fingerprint,
	}
}

func validAdmitUploadCommand() attachments.AdmitUploadCommand {
	var digest [32]byte
	copy(digest[:], bytesOf(0xbb, 32))
	return attachments.AdmitUploadCommand{
		ProjectID: "default", UploadID: "aup_lifecycle1", AuthorID: "usr_lifecycle1",
		Blob: attachments.BlobObject{
			Key:    "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SHA256: digest, ObjectVersion: "local-v1", SizeBytes: 9, BackendKind: attachments.BackendKindLocal,
		},
		Limits: attachments.DefaultLimits(),
	}
}

func validFailUploadCommand() attachments.FailUploadCommand {
	return attachments.FailUploadCommand{
		ProjectID: "default", UploadID: "aup_lifecycle1", AuthorID: "usr_lifecycle1",
		TargetState: attachments.UploadStateRejected, Limits: attachments.DefaultLimits(),
	}
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
