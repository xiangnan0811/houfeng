package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
)

func TestImportJobQueriesSelectActorID(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("record_portability_import.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := "select import_job_id, actor_id, job_state, lock_version, archive_digest, expires_at"
	if strings.Count(string(source), want) < 2 {
		t.Fatalf("ClaimImportJob and LoadImportJob must both select actor_id")
	}
}

func TestClaimExportJobAdmitsThenInsertsAndReplaysIdempotentFingerprint(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPortabilityTx{}
	repository := newRecordPortabilityTestRepository(tx)
	input := testClaimRecordExportJobInput()

	first, err := repository.ClaimExportJob(context.Background(), input)
	if err != nil {
		t.Fatalf("ClaimExportJob() error = %v", err)
	}
	if first.ExportJobID != "rej_testjob" || first.LockVersion != 1 || first.JobState != RecordExportJobStatePreviewed {
		t.Fatalf("first claim = %#v", first)
	}
	if !containsStringSlice(tx.steps, "admission") || !containsStringSlice(tx.steps, "insert_export_job") {
		t.Fatalf("steps = %v, want admission then insert", tx.steps)
	}

	tx.existing = first
	tx.steps = nil
	replay, err := repository.ClaimExportJob(context.Background(), input)
	if err != nil {
		t.Fatalf("idempotent ClaimExportJob() error = %v", err)
	}
	if replay.ExportJobID != first.ExportJobID {
		t.Fatalf("replay job = %s, want %s", replay.ExportJobID, first.ExportJobID)
	}
	if containsStringSlice(tx.steps, "insert_export_job") {
		t.Fatalf("idempotent replay inserted again: %v", tx.steps)
	}
	if containsStringSlice(tx.sql, "record_import_jobs") {
		t.Fatalf("export claim wrote an import table: %v", tx.sql)
	}
}

func TestClaimExportJobRejectsFingerprintDriftAndUnavailableAdmission(t *testing.T) {
	t.Parallel()

	existing := testClaimedRecordExportJob()
	tx := &fakeRecordPortabilityTx{existing: existing}
	repository := newRecordPortabilityTestRepository(tx)
	input := testClaimRecordExportJobInput()
	input.RequestFingerprint[0] ^= 0xff
	if _, err := repository.ClaimExportJob(context.Background(), input); !errors.Is(err, ErrRecordExportJobConflict) {
		t.Fatalf("fingerprint drift error = %v, want ErrRecordExportJobConflict", err)
	}

	denied := newRecordPortabilityTestRepository(&fakeRecordPortabilityTx{})
	denied.platform.gate = AdmissionGateFunc(func(context.Context, pgx.Tx) error {
		return ErrRecordPlatformAdmissionUnavailable
	})
	if _, err := denied.ClaimExportJob(context.Background(), testClaimRecordExportJobInput()); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("denied ClaimExportJob() error = %v, want admission unavailable", err)
	}
}

func TestAdvanceExportJobCASRequiresExactLockVersion(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPortabilityTx{affected: 1}
	repository := newRecordPortabilityTestRepository(tx)
	err := repository.AdvanceExportJob(context.Background(), AdvanceRecordExportJobInput{
		ExportJobID: "rej_testjob",
		LockVersion: 1,
		JobState:    RecordExportJobStateStaging,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AdvanceExportJob() error = %v", err)
	}
	if !containsStringSlice(tx.steps, "admission") || !containsStringSlice(tx.steps, "advance_export_job") {
		t.Fatalf("steps = %v, want admission then CAS update", tx.steps)
	}

	tx.affected = 0
	if err := repository.AdvanceExportJob(context.Background(), AdvanceRecordExportJobInput{
		ExportJobID: "rej_testjob",
		LockVersion: 1,
		JobState:    RecordExportJobStatePublished,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); !errors.Is(err, ErrRecordExportJobCASConflict) {
		t.Fatalf("stale CAS error = %v, want ErrRecordExportJobCASConflict", err)
	}
}

func newRecordPortabilityTestRepository(tx *fakeRecordPortabilityTx) *PostgresRecordPortabilityRepository {
	return &PostgresRecordPortabilityRepository{
		platform: &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
				tx.steps = append(tx.steps, "admission")
				return nil
			}),
		},
		newExportJobID: func() (string, error) { return "rej_testjob", nil },
	}
}

func testClaimRecordExportJobInput() ClaimRecordExportJobInput {
	return ClaimRecordExportJobInput{
		ActorID:            testStoreRecordUserID,
		IdempotencyKey:     "export-1",
		ExportKind:         RecordExportKindMarkdown,
		ExportMode:         RecordExportModeSafe,
		RequestFingerprint: sha256.Sum256([]byte("export-fingerprint")),
		InventoryDigest:    sha256.Sum256([]byte("export-inventory")),
		AuthorizationEpoch: 1,
		RecordID:           "rec_portability1",
		ExpiresAt:          time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
	}
}

func testClaimedRecordExportJob() RecordExportJob {
	input := testClaimRecordExportJobInput()
	return RecordExportJob{
		ExportJobID:        "rej_testjob",
		ProjectID:          "default",
		ActorID:            input.ActorID,
		IdempotencyKey:     input.IdempotencyKey,
		ExportKind:         input.ExportKind,
		ExportMode:         input.ExportMode,
		JobState:           RecordExportJobStatePreviewed,
		LockVersion:        1,
		RequestFingerprint: input.RequestFingerprint,
		InventoryDigest:    input.InventoryDigest,
		AuthorizationEpoch: input.AuthorizationEpoch,
		RecordID:           input.RecordID,
		ExpiresAt:          input.ExpiresAt,
		CreatedAt:          input.ExpiresAt.Add(-time.Hour),
		UpdatedAt:          input.ExpiresAt.Add(-time.Hour),
	}
}

type fakeRecordPortabilityTx struct {
	pgx.Tx
	steps    []string
	sql      []string
	existing RecordExportJob
	affected int64
}

func (tx *fakeRecordPortabilityTx) Commit(context.Context) error { return nil }
func (tx *fakeRecordPortabilityTx) Rollback(context.Context) error {
	return nil
}

func (tx *fakeRecordPortabilityTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.sql = append(tx.sql, sql)
	normalized := strings.ToLower(sql)
	if strings.Contains(normalized, "insert into public.record_export_jobs") {
		tx.steps = append(tx.steps, "insert_export_job")
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(normalized, "update public.record_export_jobs") {
		tx.steps = append(tx.steps, "advance_export_job")
		return pgconn.NewCommandTag("UPDATE " + itoaPortability(tx.affected)), nil
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *fakeRecordPortabilityTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.sql = append(tx.sql, sql)
	tx.steps = append(tx.steps, "load_export_job")
	if tx.existing.ExportJobID == "" {
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}
	job := tx.existing
	return fakeRecordPlatformRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = job.ExportJobID
		*(dest[1].(*recordauth.ProjectID)) = job.ProjectID
		*(dest[2].(*string)) = job.ActorID
		*(dest[3].(*string)) = job.IdempotencyKey
		*(dest[4].(*string)) = job.ExportKind
		*(dest[5].(*string)) = job.ExportMode
		*(dest[6].(*string)) = job.JobState
		*(dest[7].(*string)) = job.FailureCode
		*(dest[8].(*int64)) = int64(job.LockVersion)
		*(dest[9].(*[]byte)) = append([]byte(nil), job.RequestFingerprint[:]...)
		*(dest[10].(*[]byte)) = append([]byte(nil), job.InventoryDigest[:]...)
		*(dest[11].(*int64)) = int64(job.AuthorizationEpoch)
		*(dest[12].(*string)) = job.RecordID
		*(dest[13].(*string)) = job.RevisionID
		*(dest[14].(*time.Time)) = job.ExpiresAt
		*(dest[15].(*time.Time)) = job.CreatedAt
		*(dest[16].(*time.Time)) = job.UpdatedAt
		return nil
	}}
}

func itoaPortability(value int64) string {
	if value == 0 {
		return "0"
	}
	return "1"
}

func containsStringSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
