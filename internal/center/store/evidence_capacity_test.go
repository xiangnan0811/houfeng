package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

func TestPostgresEvidenceRepositoryReadsProjectScopedLogicalCapacityAfterAdmission(t *testing.T) {
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
		for _, required := range []string{
			"from public.evidence_snapshots", "join public.records", "record.project_id = $1",
			"select distinct snapshot.payload_digest", "not exists",
		} {
			if !strings.Contains(compact, required) {
				t.Fatalf("capacity SQL missing %q:\n%s", required, sql)
			}
		}
		if strings.Contains(compact, "attachment") {
			t.Fatalf("evidence capacity queried attachment quota:\n%s", sql)
		}
		if !reflect.DeepEqual(args, []any{string(recordauth.ProjectIDDefault)}) {
			t.Fatalf("capacity arguments = %#v", args)
		}
		return evidenceCapacityInt64Row(2, 300, 1, 200, 100, 3, 600, 350)
	}}
	repository := newFakePostgresEvidenceRepository(tx, AdmissionGateFunc(func(_ context.Context, got pgx.Tx) error {
		if got != tx {
			t.Fatal("AdmissionGate received a different transaction")
		}
		tx.calls = append(tx.calls, "gate")
		return nil
	}))

	usage, err := repository.ReadProjectEvidenceCapacity(context.Background(), string(recordauth.ProjectIDDefault))
	if err != nil {
		t.Fatalf("ReadProjectEvidenceCapacity() error = %v", err)
	}
	want := evidence.ProjectCapacityUsage{
		ProjectID:            string(recordauth.ProjectIDDefault),
		LogicalSnapshotCount: 2, LogicalSnapshotBytes: 300,
		PhysicalPayloadCount: 1, PhysicalCanonicalBytes: 200, PhysicalCompressedBytes: 100,
		OrphanPayloadCount: 3, OrphanCanonicalBytes: 600, OrphanCompressedBytes: 350,
	}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("ReadProjectEvidenceCapacity() = %#v, want %#v", usage, want)
	}
	if !tx.committed || !reflect.DeepEqual(tx.calls, []string{"gate", "query"}) {
		t.Fatalf("capacity transaction = committed:%t calls:%#v", tx.committed, tx.calls)
	}
}

func TestPostgresEvidenceRepositoryCapacityFailsClosedForUnknownProjectAndInconsistentRows(t *testing.T) {
	t.Run("unknown project", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{}
		repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)
		_, err := repository.ReadProjectEvidenceCapacity(context.Background(), "future-project")
		if !errors.Is(err, ErrInvalidEvidencePersistence) {
			t.Fatalf("ReadProjectEvidenceCapacity() error = %v, want invalid persistence", err)
		}
		if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
			t.Fatalf("unknown project performed DB work: queries:%d execs:%d committed:%t", tx.queryCount, tx.execCount, tx.committed)
		}
	})

	t.Run("inconsistent accounting", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
			return evidenceCapacityInt64Row(0, 1, 0, 0, 0, 0, 0, 0)
		}}
		repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)
		_, err := repository.ReadProjectEvidenceCapacity(context.Background(), string(recordauth.ProjectIDDefault))
		if !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("ReadProjectEvidenceCapacity() error = %v, want conflict", err)
		}
		if tx.committed {
			t.Fatal("inconsistent accounting committed")
		}
	})
}

func TestPostgresEvidenceRepositoryReadsAggregateCapacityWithoutHighCardinalityDimensions(t *testing.T) {
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
		for _, required := range []string{
			"group by record.project_id", "select distinct snapshot.payload_digest", "not exists",
		} {
			if !strings.Contains(compact, required) {
				t.Fatalf("aggregate SQL missing %q:\n%s", required, sql)
			}
		}
		if len(args) != 0 || strings.Contains(compact, "attachment") {
			t.Fatalf("aggregate query leaked a dimension or attachment dependency: args=%#v sql=%s", args, sql)
		}
		return evidenceCapacityInt64Row(2, 500, 300, 400, 220, 1, 50, 25)
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	aggregate, err := repository.ReadEvidenceCapacityAggregate(context.Background())
	if err != nil {
		t.Fatalf("ReadEvidenceCapacityAggregate() error = %v", err)
	}
	want := evidence.EvidenceCapacityAggregate{
		ProjectCount: 2, LogicalSnapshotBytes: 500, HighestProjectLogicalBytes: 300,
		PhysicalCanonicalBytes: 400, PhysicalCompressedBytes: 220,
		OrphanPayloadCount: 1, OrphanCanonicalBytes: 50, OrphanCompressedBytes: 25,
	}
	if !reflect.DeepEqual(aggregate, want) || !tx.committed {
		t.Fatalf("ReadEvidenceCapacityAggregate() = %#v committed:%t, want %#v", aggregate, tx.committed, want)
	}
}

func TestPostgresEvidenceRepositoryReadsBoundedLifecycleBacklogWithDatabaseTime(t *testing.T) {
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
		for _, required := range []string{
			"valid_until <= transaction_timestamp()", "payload.created_at <= transaction_timestamp()",
			"interval '1 microsecond'", "limit $1", "not exists",
		} {
			if !strings.Contains(compact, required) {
				t.Fatalf("backlog SQL missing %q:\n%s", required, sql)
			}
		}
		if strings.Contains(compact, "attachment") {
			t.Fatalf("evidence backlog queried attachments:\n%s", sql)
		}
		if !reflect.DeepEqual(args, []any{int64(101), EvidencePayloadOrphanGracePeriod.Microseconds()}) {
			t.Fatalf("backlog arguments = %#v", args)
		}
		return evidenceCapacityInt64Row(101, 40)
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	backlog, err := repository.ReadEvidenceLifecycleBacklog(context.Background(), 100)
	if err != nil {
		t.Fatalf("ReadEvidenceLifecycleBacklog() error = %v", err)
	}
	want := evidence.EvidenceLifecycleBacklog{
		ExpiredIntentCount: 100, EligibleOrphanPayloadCount: 40,
		MoreExpiredIntents: true, MoreEligibleOrphanPayloads: false,
	}
	if !reflect.DeepEqual(backlog, want) || !tx.committed {
		t.Fatalf("ReadEvidenceLifecycleBacklog() = %#v committed:%t, want %#v", backlog, tx.committed, want)
	}
}

func evidenceCapacityInt64Row(values ...int64) pgx.Row {
	return fakeRecordPlatformRow{scan: func(dest ...any) error {
		if len(dest) != len(values) {
			return errors.New("unexpected evidence capacity scan destination count")
		}
		for index, value := range values {
			target, ok := dest[index].(*int64)
			if !ok {
				return errors.New("unexpected evidence capacity scan destination type")
			}
			*target = value
		}
		return nil
	}}
}
