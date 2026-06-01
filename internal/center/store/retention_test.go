package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/retention"
)

func TestPostgresRetentionRepositoryAppliesAggregatesAndCleanupInTransaction(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 28, 12, 30, 0, 0, time.UTC)

	result, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, now)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}
	for _, want := range []string{
		"insert into monitoring_instance_host_sample_daily_aggregates",
		"insert into target_probe_daily_aggregates",
		"delete from monitoring_instance_heartbeats",
		"delete from host_samples",
		"delete from probe_observations",
		"delete from monitoring_instance_host_sample_daily_aggregates",
		"delete from target_probe_daily_aggregates",
		"delete from state_change_events",
		"delete from notification_records",
	} {
		if !containsSQL(tx.execSQL, want) {
			t.Fatalf("execSQL = %#v, want %q", tx.execSQL, want)
		}
	}
	if containsSQL(tx.execSQL, "delete from active_incidents") {
		t.Fatalf("execSQL = %#v, must not delete active_incidents", tx.execSQL)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls == 0 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want commit and deferred rollback", tx.commitCalls, tx.rollbackCalls)
	}
	if result.MonitoringInstanceAggregateRows != 1 || result.TargetAggregateRows != 1 || result.DeletedHeartbeats != 1 || result.DeletedNotifications != 1 {
		t.Fatalf("result = %#v, want command-tag counts", result)
	}
}

func TestPostgresRetentionRepositoryUsesRepeatableReadTransaction(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{}
	var options pgx.TxOptions
	repo := &PostgresRetentionRepository{
		beginTx: func(_ context.Context, opts pgx.TxOptions) (retentionTx, error) {
			options = opts
			return tx, nil
		},
	}

	_, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}

	if options.IsoLevel != pgx.RepeatableRead {
		t.Fatalf("transaction isolation = %q, want %q", options.IsoLevel, pgx.RepeatableRead)
	}
}

func TestPostgresRetentionRepositoryUsesExpectedCutoffs(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 28, 12, 30, 0, 0, time.UTC)

	_, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, now)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}
	if got := tx.argsForSQL("insert into monitoring_instance_host_sample_daily_aggregates")[0].(time.Time); !got.Equal(time.Date(2026, time.April, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("aggregate stable cutoff = %s, want start of current UTC day", got)
	}
	if got := tx.argsForSQL("delete from monitoring_instance_heartbeats")[0].(time.Time); !got.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("raw cutoff = %s, want %s", got, now.AddDate(0, 0, -7))
	}
	if got := tx.argsForSQL("delete from state_change_events")[0].(time.Time); !got.Equal(now.AddDate(0, 0, -90)) {
		t.Fatalf("event cutoff = %s, want %s", got, now.AddDate(0, 0, -90))
	}
}

func TestPostgresRetentionRepositoryRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{execErrForSQLSubstring: "delete from host_samples", execErr: errors.New("delete boom")}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}

	_, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "delete expired host samples") {
		t.Fatalf("ApplyRetention() error = %v, want host sample context", err)
	}
	if tx.commitCalls != 0 || tx.rollbackCalls == 0 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want rollback without commit", tx.commitCalls, tx.rollbackCalls)
	}
}

type fakeRetentionTx struct {
	execSQL                []string
	execArgs               [][]any
	execErrForSQLSubstring string
	execErr                error
	commitCalls            int
	rollbackCalls          int
}

func (f *fakeRetentionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	if f.execErr != nil && strings.Contains(sql, f.execErrForSQLSubstring) {
		return pgconn.CommandTag{}, f.execErr
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (f *fakeRetentionTx) Commit(context.Context) error   { f.commitCalls++; return nil }
func (f *fakeRetentionTx) Rollback(context.Context) error { f.rollbackCalls++; return nil }

func (f *fakeRetentionTx) argsForSQL(substring string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, substring) {
			return f.execArgs[i]
		}
	}
	return nil
}
