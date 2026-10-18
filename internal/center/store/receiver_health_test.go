package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"houfeng/internal/center/assetlifecycle"
)

// These fakes exercise the production observation and archive-evidence paths;
// assertions concern writes, continuity and fault acknowledgement, not SQL text.
type receiverHealthTestDB struct {
	writes       int
	healthy      bool
	healthySince *time.Time
	checkedAt    time.Time
	reason       string
	beginErr     error
	commitErr    error
	onCommit     func()
}

func (db *receiverHealthTestDB) beginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	return &receiverHealthTestTx{db: db}, nil
}

func (db *receiverHealthTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &receiverHealthTestRows{db: db}, nil
}

func (db *receiverHealthTestDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{scan: func(dest ...any) error {
		*(dest[0].(*time.Time)) = time.Now()
		return nil
	}}
}

type receiverHealthTestTx struct {
	pgx.Tx
	db        *receiverHealthTestDB
	healthy   bool
	reason    string
	checkedAt time.Time
}

func (tx *receiverHealthTestTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.db.QueryRow(ctx, sql, args...)
}

func (tx *receiverHealthTestTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.db.writes++
	tx.healthy = args[1].(bool)
	tx.reason = args[2].(string)
	tx.checkedAt = args[3].(time.Time)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *receiverHealthTestTx) Commit(context.Context) error {
	if tx.db.commitErr != nil {
		return tx.db.commitErr
	}
	if !tx.healthy {
		tx.db.healthySince = nil
	} else if !tx.db.healthy {
		at := tx.checkedAt
		tx.db.healthySince = &at
	}
	tx.db.healthy, tx.db.reason, tx.db.checkedAt = tx.healthy, tx.reason, tx.checkedAt
	if tx.db.onCommit != nil {
		tx.db.onCommit()
	}
	return nil
}

func (*receiverHealthTestTx) Rollback(context.Context) error { return nil }

type receiverHealthTestRows struct {
	pgx.Rows
	db   *receiverHealthTestDB
	read bool
}

func (rows *receiverHealthTestRows) Next() bool {
	if rows.read {
		return false
	}
	rows.read = true
	return true
}

func (rows *receiverHealthTestRows) Scan(dest ...any) error {
	*(dest[0].(*string)) = "receiver-test"
	*(dest[1].(**time.Time)) = rows.db.healthySince
	*(dest[2].(**time.Time)) = &rows.db.checkedAt
	*(dest[3].(*bool)) = rows.db.healthy
	return nil
}

func (*receiverHealthTestRows) Close()     {}
func (*receiverHealthTestRows) Err() error { return nil }

func newReceiverHealthTestObserver(t *testing.T) (*ReceiverHealthObserver, *receiverHealthTestDB) {
	t.Helper()
	anchor := receiverClockAnchor.Load()
	t.Cleanup(func() {
		receiverClockAnchor.Store(anchor)
		NoteReceiverHealthFault("reception_fault")
	})
	db := &receiverHealthTestDB{}
	o := &ReceiverHealthObserver{beginTx: db.beginTx, bootID: "receiver-test", faultGeneration: receiverFaultGeneration.Load()}
	o.SetReady(true)
	if err := o.observeTick(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	return o, db
}

func receiverHealthTestEvidence(t *testing.T, db *receiverHealthTestDB) assetlifecycle.ArchiveOnlineEvidence {
	t.Helper()
	var evidence assetlifecycle.ArchiveOnlineEvidence
	if err := loadArchiveReceiverEvidence(context.Background(), db, &evidence, false); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func TestReceiverHealthFaultStormCoalescesWithoutHealthyTicks(t *testing.T) {
	o, db := newReceiverHealthTestObserver(t)
	if !receiverHealthTestEvidence(t, db).ReceiverHealthy {
		t.Fatal("initial receiver evidence is not healthy")
	}
	beforeGeneration := receiverFaultGeneration.Load()
	beforeWrites := db.writes
	for range 1000 {
		NoteReceiverHealthFault("sync_rate_limited")
	}
	NoteReceiverHealthFault("sync_capacity_exhausted")
	if receiverFaultGeneration.Load() != beforeGeneration+1001 {
		t.Fatal("fault notifications were not immediately generation-counted")
	}
	if db.writes != beforeWrites {
		t.Fatal("reject storm wrote to the database before an observer tick")
	}
	if receiverHealthTestEvidence(t, db).ReceiverHealthy {
		t.Fatal("stale healthy database row bypassed pending in-memory faults")
	}
	for tick := range 3 {
		if tick > 0 {
			for range 1000 {
				NoteReceiverHealthFault("sync_capacity_exhausted")
			}
		}
		if err := o.observeTick(context.Background(), time.Now()); err != nil {
			t.Fatal(err)
		}
		if db.writes != beforeWrites+tick+1 || db.healthy || db.healthySince != nil || db.reason != "sync_capacity_exhausted" {
			t.Fatalf("continuous faults tick=%d writes=%d healthy=%t since=%v reason=%q", tick, db.writes, db.healthy, db.healthySince, db.reason)
		}
		if receiverHealthTestEvidence(t, db).ReceiverHealthy {
			t.Fatal("continuous rejection storm produced healthy archive evidence")
		}
	}
	if err := o.observeTick(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if !db.healthy || db.healthySince == nil || db.reason != "" {
		t.Fatal("fault-free recovery did not start a fresh healthy window")
	}
}

func TestReceiverHealthFaultReasonsAreAllowlisted(t *testing.T) {
	o, db := newReceiverHealthTestObserver(t)
	for _, reason := range []string{"sync_rate_limited", "sync_capacity_exhausted", "sync_internal_error", "sync_persistence_failure", "untrusted secret-bearing reason"} {
		NoteReceiverHealthFault(reason)
		if err := o.observeTick(context.Background(), time.Now()); err != nil {
			t.Fatal(err)
		}
		want := reason
		if reason == "untrusted secret-bearing reason" {
			want = "reception_fault"
		}
		if db.healthy || db.reason != want {
			t.Fatalf("observed healthy=%t reason=%q, want false/%q", db.healthy, db.reason, want)
		}
	}
}

func TestReceiverHealthFaultDuringCommitIsNotAcknowledged(t *testing.T) {
	o, db := newReceiverHealthTestObserver(t)
	db.onCommit = func() { NoteReceiverHealthFault("sync_internal_error") }
	if err := o.observeTick(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if receiverHealthTestEvidence(t, db).ReceiverHealthy {
		t.Fatal("observation acknowledged a fault arriving during commit")
	}
	db.onCommit = nil
	if err := o.observeTick(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if db.healthy || db.reason != "sync_internal_error" || db.healthySince != nil {
		t.Fatal("next observation retained continuity after an in-flight fault")
	}
}

func TestReceiverHealthDatabaseFailureRecoveryResetsContinuity(t *testing.T) {
	for _, stage := range []string{"begin", "commit"} {
		t.Run(stage, func(t *testing.T) {
			o, db := newReceiverHealthTestObserver(t)
			failure := errors.New("database unavailable")
			if stage == "begin" {
				db.beginErr = failure
			} else {
				db.commitErr = failure
			}
			if err := o.observeTick(context.Background(), time.Now()); !errors.Is(err, failure) {
				t.Fatalf("observation error=%v, want unavailable", err)
			}
			if receiverHealthTestEvidence(t, db).ReceiverHealthy {
				t.Fatal("database failure retained healthy in-process evidence")
			}
			db.beginErr, db.commitErr = nil, nil
			if err := o.observeTick(context.Background(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if db.healthy || db.healthySince != nil || db.reason != "health_observation_failed" {
				t.Fatal("database recovery skipped the continuity reset")
			}
			if err := o.observeTick(context.Background(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if !receiverHealthTestEvidence(t, db).ReceiverHealthy || db.healthySince == nil {
				t.Fatal("healthy observation did not restart continuity after reset")
			}
		})
	}
}
