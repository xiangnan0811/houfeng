package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/retention"
)

// Transaction orchestration is tested here; SQL results and time boundaries
// are exercised against PostgreSQL in the retention integration tests.
func TestRetentionRetriesOnlyWholeRetryableTransactions(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "23514"} {
		t.Run(code, func(t *testing.T) {
			var txs []*fakeRetentionTx
			repo := &PostgresRetentionRepository{beginTx: func(_ context.Context, opts pgx.TxOptions) (retentionTx, error) {
				if opts.IsoLevel != pgx.RepeatableRead {
					t.Fatalf("isolation = %s", opts.IsoLevel)
				}
				if len(txs) > 0 && txs[len(txs)-1].rollbacks == 0 {
					t.Fatal("retry started before rollback")
				}
				tx := &fakeRetentionTx{locked: true, failAt: 4, err: &pgconn.PgError{Code: code}}
				txs = append(txs, tx)
				return tx, nil
			}}
			result, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}, time.Now())
			want := 3
			if code == "23514" {
				want = 1
			}
			if err == nil || len(txs) != want || result != (retention.Result{}) {
				t.Fatalf("attempts=%d result=%+v err=%v", len(txs), result, err)
			}
			for _, tx := range txs {
				if tx.commits != 0 || tx.rollbacks != 1 {
					t.Fatalf("transaction = %+v", tx)
				}
			}
		})
	}
}

func TestRetentionSuccessfulRetryReportsOnlyCommittedCounts(t *testing.T) {
	var txs []*fakeRetentionTx
	now := time.Date(2026, 10, 1, 0, 30, 0, 0, time.FixedZone("west", -7*3600))
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) {
		tx := &fakeRetentionTx{locked: true}
		if len(txs) == 0 {
			tx.failAt = 4
			tx.err = &pgconn.PgError{Code: "40001"}
		}
		txs = append(txs, tx)
		return tx, nil
	}}
	result, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}, now)
	if err != nil || len(txs) != 2 || txs[1].commits != 1 {
		t.Fatalf("result=%+v attempts=%d err=%v", result, len(txs), err)
	}
	// Each committed counter is a single command tag (1), never a sum across
	// the failed first attempt.
	if result.MonitoringInstanceAggregateRows != 1 || result.DeletedHostSamples != 1 || result.DeletedTargetAggregates != 1 {
		t.Fatalf("result = %+v", result)
	}
	for _, tx := range txs {
		if got := tx.args[0][0].(time.Time); !got.Equal(startOfUTCDay(now)) {
			t.Fatalf("stable cutoff = %v", got)
		}
		if got := tx.args[0][1].(time.Time); !got.Equal(now.UTC().AddDate(0, 0, -30)) {
			t.Fatalf("raw cutoff = %v", got)
		}
	}
}

func TestRetentionCancellationAfterRollbackStopsRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx := &fakeRetentionTx{locked: true, failAt: 1, err: &pgconn.PgError{Code: "40001"}, afterRollback: cancel}
	calls := 0
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) {
		calls++
		return tx, nil
	}}
	result, err := repo.ApplyRetention(ctx, retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}, time.Now())
	if !errors.Is(err, context.Canceled) || calls != 1 || tx.rollbacks != 1 || result != (retention.Result{}) {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestRetentionOverlapSkipsWithoutWritesOrCommit(t *testing.T) {
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}
	result, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}, time.Now())
	if err != nil || !result.Skipped || tx.execs != 0 || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("result=%+v tx=%+v err=%v", result, tx, err)
	}
}

func TestCommandOutputCleanupUsesIndependentReadCommittedCommit(t *testing.T) {
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(_ context.Context, opts pgx.TxOptions) (retentionTx, error) {
		if opts.IsoLevel != pgx.ReadCommitted {
			t.Fatalf("isolation = %s", opts.IsoLevel)
		}
		return tx, nil
	}}
	count, err := repo.ClearExpiredCommandOutputs(context.Background(), time.Now())
	if err != nil || count != 1 || tx.commits != 1 || tx.execs != 1 {
		t.Fatalf("count=%d tx=%+v err=%v", count, tx, err)
	}
}

type fakeRetentionTx struct {
	locked        bool
	failAt        int
	err           error
	execs         int
	commits       int
	rollbacks     int
	args          [][]any
	afterRollback func()
}

func (f *fakeRetentionTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	f.execs++
	f.args = append(f.args, args)
	if f.execs == f.failAt {
		return pgconn.CommandTag{}, f.err
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeRetentionTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return retentionBoolRow(f.locked)
}

func (f *fakeRetentionTx) Commit(context.Context) error {
	f.commits++
	return nil
}

func (f *fakeRetentionTx) Rollback(context.Context) error {
	f.rollbacks++
	if f.afterRollback != nil {
		f.afterRollback()
	}
	return nil
}

type retentionBoolRow bool

func (r retentionBoolRow) Scan(dest ...any) error {
	*dest[0].(*bool) = bool(r)
	return nil
}
