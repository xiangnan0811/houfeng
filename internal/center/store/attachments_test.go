package store

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

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
		"effective_usage", "attachment_insert", "upload_insert", "quota_update", "commit", "rollback",
	}
	if !reflect.DeepEqual(tx.steps, wantSteps) {
		t.Fatalf("ReserveUpload() steps = %#v, want %#v", tx.steps, wantSteps)
	}
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

func TestPostgresAttachmentRepositoryLifecycleWriteCutpointsRollBack(t *testing.T) {
	t.Parallel()

	cutpointError := errors.New("attachment lifecycle cutpoint")
	tests := []struct {
		name     string
		state    attachments.UploadState
		cutpoint string
		run      func(*PostgresAttachmentRepository) error
	}{
		{name: "complete upload row", state: attachments.UploadStateUploading, cutpoint: "upload_content_complete", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.CompleteUploadContent(context.Background(), validCompleteUploadContentCommand())
			return err
		}},
		{name: "complete attachment row", state: attachments.UploadStateUploading, cutpoint: "logical_state_update", run: func(repository *PostgresAttachmentRepository) error {
			_, err := repository.CompleteUploadContent(context.Background(), validCompleteUploadContentCommand())
			return err
		}},
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
	case strings.Contains(compact, "from public.attachment_uploads") && strings.Contains(compact, "for update"):
		return "upload_lock"
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
	case strings.Contains(compact, "insert into public.blob_objects"):
		return "blob_insert"
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
