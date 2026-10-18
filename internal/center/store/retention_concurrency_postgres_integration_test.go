package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/retention"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
)

const retentionTestInsertHostSQL = `insert into host_samples (monitoring_instance_id,observed_at,received_at,cpu_usage_pct,load_1,load_5,load_15,mem_used_pct,mem_available_bytes,swap_used_pct,disk_used_pct,inode_used_pct,net_in_bytes_per_sec,net_out_bytes_per_sec,cpu_iowait_pct,cpu_steal_pct,disk_read_bytes_per_sec,disk_write_bytes_per_sec,disk_busy_pct,uptime_seconds,sync_batch_id) values ($1,$2,$2,$3,1,1,1,20,1,0,10,10,0,0,0,0,0,0,0,1,'retention_test')`

// Interleavings are fixed with transaction events and observed PostgreSQL
// lock waits (pg_blocking_pids), never with sleeps guessing statement order.
func TestPostgresIntegrationRetentionConcurrentSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "retention-concurrent-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("converge current retention schema: %v", err)
	}
	runtime := fixture.openDirectRuntimePool(t, ctx, "retention-concurrent-runtime", 6)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("admit retention runtime role: %v", err)
	}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	policy := retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec fixture SQL: %v", err)
		}
	}
	seed := func(prefix string) {
		t.Helper()
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		exec(retentionTestInsertHostSQL, "mi_"+prefix, now.AddDate(0, 0, -31), 10.0)
	}
	syncRepo := NewPostgresSyncRepository(runtime)
	identity := func(prefix string) syncing.Batch {
		return syncing.Batch{SessionID: "mas_" + prefix, MonitoringInstanceID: "mi_" + prefix, SyncToken: "token_" + prefix}
	}
	validate := func(tx pgx.Tx, prefix string) acceptedSyncBatchState {
		t.Helper()
		state, err := syncRepo.validateAcceptedSyncBatch(ctx, tx, identity(prefix))
		if err != nil {
			t.Fatalf("validateAcceptedSyncBatch: %v", err)
		}
		return state
	}

	// The RR retention snapshot precedes a sync writer that updates the
	// monitoring instance's non-key columns; the aggregate insert then needs a
	// FK KEY SHARE lock on that parent row. The old FOR UPDATE lock turns the
	// update into a key-updating xmax and must reproduce 40001.
	for _, mode := range []string{"old_for_update_control", "committed_nonkey", "pending_nonkey"} {
		t.Run(mode, func(t *testing.T) {
			prefix := "ret_" + mode
			seed(prefix)
			readTx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = readTx.Rollback(ctx) }()
			var snapshot int
			if err := readTx.QueryRow(ctx, `select 1 from monitoring_instances where monitoring_instance_id=$1`, "mi_"+prefix).Scan(&snapshot); err != nil {
				t.Fatal(err)
			}
			writer, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = writer.Rollback(ctx) }()
			validate(writer, prefix)
			if mode == "old_for_update_control" {
				if _, err := writer.Exec(ctx, `select 1 from monitoring_instances where monitoring_instance_id=$1 for update`, "mi_"+prefix); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := writer.Exec(ctx, `update monitoring_instances set updated_at=clock_timestamp() where monitoring_instance_id=$1`, "mi_"+prefix); err != nil {
				t.Fatal(err)
			}
			if mode != "pending_nonkey" {
				if err := writer.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			// Bounded so a KEY SHARE incompatibility with the pending writer
			// fails the test instead of hanging it.
			insertCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			defer stop()
			_, err = readTx.Exec(insertCtx, upsertMonitoringInstanceHostDailyAggregatesSQL, startOfUTCDay(now), now.AddDate(0, 0, -30))
			if mode == "old_for_update_control" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
					t.Fatalf("old FOR UPDATE control = %v, want 40001", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NO KEY UPDATE did not remove the FK serialization conflict (%s): %v", mode, err)
			}
			if err := readTx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if mode == "pending_nonkey" {
				if err := writer.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var buckets int
			if err := fixture.db.QueryRow(ctx, `select count(*) from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id=$1`, "mi_"+prefix).Scan(&buckets); err != nil || buckets != 1 {
				t.Fatalf("bucket count=%d err=%v", buckets, err)
			}
		})
	}

	t.Run("sync_and_capability_transitions_still_serialize", func(t *testing.T) {
		prefix := "ret_serial"
		seed(prefix)
		first, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = first.Rollback(ctx) }()
		validate(first, prefix)
		second, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = second.Rollback(ctx) }()
		var pid int32
		if err := second.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := syncRepo.validateAcceptedSyncBatch(ctx, second, identity(prefix))
			done <- err
		}()
		retentionWaitForBlocked(t, ctx, fixture.db, pid)
		if _, err := first.Exec(ctx, `update monitoring_instances set lifecycle_status=$2, monitoring_status=$3 where monitoring_instance_id=$1`, "mi_"+prefix, monitoringinstances.LifecycleRetired, monitoringinstances.MonitoringPaused); err != nil {
			t.Fatal(err)
		}
		if _, err := first.Exec(ctx, `update monitoring_agent_sessions set capability='evidence_only' where session_id=$1`, "mas_"+prefix); err != nil {
			t.Fatal(err)
		}
		if err := first.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("blocked sync validation: %v", err)
		}
		state := validate(second, prefix)
		if state.Capability != "evidence_only" || state.LifecycleStatus != monitoringinstances.LifecycleRetired {
			t.Fatalf("serialized sync observed stale state %+v", state)
		}
	})

	t.Run("overlap_skips_and_raw_arriving_after_aggregation_survives", func(t *testing.T) {
		prefix := "ret_overlap"
		seed(prefix)
		repo := &PostgresRetentionRepository{beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) {
			tx, err := runtime.BeginTx(ctx, opts)
			if err != nil {
				return nil, err
			}
			// Exec 2 is the probe aggregate upsert: aggregation has finished
			// and no raw delete has run yet.
			return &retentionBarrierTx{Tx: tx, afterExec: func(call int) {
				if call != 2 {
					return
				}
				other, err := NewPostgresRetentionRepository(runtime).ApplyRetention(ctx, policy, now)
				if err != nil || !other.Skipped || other != (retention.Result{Skipped: true}) {
					t.Fatalf("overlapping pass = %+v, %v; want skipped", other, err)
				}
				exec(retentionTestInsertHostSQL, "mi_"+prefix, now.AddDate(0, 0, -31).Add(time.Second), 90.0)
			}}, nil
		}}
		result, err := repo.ApplyRetention(ctx, policy, now)
		if err != nil || result.Skipped {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		var count int
		if err := fixture.db.QueryRow(ctx, `select count(*) from host_samples where monitoring_instance_id=$1 and cpu_usage_pct=90`, "mi_"+prefix).Scan(&count); err != nil || count != 1 {
			t.Fatalf("raw arriving after aggregation was deleted unaggregated: count=%d err=%v", count, err)
		}
		var avg float64
		if err := fixture.db.QueryRow(ctx, `select sample_count,avg_cpu_usage_pct from monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id=$1`, "mi_"+prefix).Scan(&count, &avg); err != nil || count != 1 || avg != 10 {
			t.Fatalf("aggregate count=%d avg=%v err=%v", count, avg, err)
		}
	})

	for _, code := range []string{"40001", "40P01"} {
		for _, succeeds := range []bool{false, true} {
			t.Run(fmt.Sprintf("retry_%s_success_%t", code, succeeds), func(t *testing.T) {
				prefix := fmt.Sprintf("ret_retry_%s_%t", code, succeeds)
				seed(prefix)
				exec(`create sequence retention_attempts`)
				exec(`grant usage, select on sequence retention_attempts to ` + pgx.Identifier{fixture.runtime}.Sanitize())
				failures := 3
				if succeeds {
					failures = 2
				}
				exec(fmt.Sprintf(`create function retention_retry_failure() returns trigger language plpgsql as $$ begin if nextval('retention_attempts') <= %d then raise exception 'injected retry' using errcode='%s'; end if; return null; end $$`, failures, code))
				exec(`create trigger retention_retry_failure before insert on monitoring_instance_host_sample_daily_aggregates for each statement execute function retention_retry_failure()`)
				defer exec(`drop sequence retention_attempts`)
				defer exec(`drop function retention_retry_failure() cascade`)
				result, err := NewPostgresRetentionRepository(runtime).ApplyRetention(ctx, policy, now)
				if (err == nil) != succeeds || (!succeeds && result != (retention.Result{})) || result.Skipped {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				var pgErr *pgconn.PgError
				if !succeeds && (!errors.As(err, &pgErr) || pgErr.Code != code) {
					t.Fatalf("exhausted retry error = %v, want %s", err, code)
				}
				var attempts int
				if err := fixture.db.QueryRow(ctx, `select last_value from retention_attempts`).Scan(&attempts); err != nil || attempts != 3 {
					t.Fatalf("attempts=%d err=%v, want exactly 3", attempts, err)
				}
				var raw int
				wantRaw := 1
				if succeeds {
					wantRaw = 0
				}
				if err := fixture.db.QueryRow(ctx, `select count(*) from host_samples where monitoring_instance_id=$1`, "mi_"+prefix).Scan(&raw); err != nil || raw != wantRaw {
					t.Fatalf("raw=%d err=%v, want %d", raw, err, wantRaw)
				}
			})
		}
	}

	t.Run("cancel_after_real_serialization_failure_rolls_back_before_wait", func(t *testing.T) {
		seed("ret_cancel")
		exec(`create function retention_cancel_failure() returns trigger language plpgsql as $$ begin raise exception 'injected cancellation retry' using errcode='40001'; end $$`)
		exec(`create trigger retention_cancel_failure before insert on monitoring_instance_host_sample_daily_aggregates for each statement execute function retention_cancel_failure()`)
		defer exec(`drop function retention_cancel_failure() cascade`)
		attemptCtx, stop := context.WithCancel(ctx)
		defer stop()
		attempts := 0
		repo := &PostgresRetentionRepository{beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) {
			attempts++
			tx, err := runtime.BeginTx(ctx, opts)
			if err != nil {
				return nil, err
			}
			return &retentionBarrierTx{Tx: tx, afterRollback: stop}, nil
		}}
		result, err := repo.ApplyRetention(attemptCtx, policy, now)
		if !errors.Is(err, context.Canceled) || attempts != 1 || result != (retention.Result{}) {
			t.Fatalf("attempts=%d result=%+v err=%v", attempts, result, err)
		}
		// The rolled-back attempt released its transaction advisory lock.
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var locked bool
		if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended('houfeng:retention',0))`).Scan(&locked); err != nil || !locked {
			t.Fatalf("retention lock available=%t err=%v", locked, err)
		}
	})

	t.Run("command_replacement_rechecks_current_row", func(t *testing.T) {
		prefix := "ret_command"
		seed(prefix)
		exec(`update monitoring_instances set last_action=jsonb_build_object('status','done','stdout','old','stderr','old error','output_expired',false,'output_expires_at',$2::timestamptz) where monitoring_instance_id=$1`, "mi_"+prefix, now)
		writer, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Rollback(ctx) }()
		if _, err := writer.Exec(ctx, `update monitoring_instances set last_action=jsonb_build_object('status','done','action_id','replacement','stdout','new output','stderr','new error','output_expired',false,'output_expires_at',$2::timestamptz) where monitoring_instance_id=$1`, "mi_"+prefix, now.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		cleanupConn, err := runtime.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanupConn.Release()
		pid := int32(cleanupConn.Conn().PgConn().PID())
		repo := &PostgresRetentionRepository{beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) {
			return cleanupConn.BeginTx(ctx, opts)
		}}
		type outcome struct {
			count int64
			err   error
		}
		done := make(chan outcome, 1)
		go func() {
			count, err := repo.ClearExpiredCommandOutputs(ctx, now)
			done <- outcome{count: count, err: err}
		}()
		retentionWaitForBlocked(t, ctx, fixture.db, pid)
		if err := writer.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		got := <-done
		if got.err != nil || got.count != 0 {
			t.Fatalf("cleanup with replaced command = %+v", got)
		}
		var stdout, stderr string
		if err := fixture.db.QueryRow(ctx, `select last_action->>'stdout',last_action->>'stderr' from monitoring_instances where monitoring_instance_id=$1`, "mi_"+prefix).Scan(&stdout, &stderr); err != nil || stdout != "new output" || stderr != "new error" {
			t.Fatalf("new output cleared by old snapshot: stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
		count, err := NewPostgresRetentionRepository(runtime).ClearExpiredCommandOutputs(ctx, now.Add(24*time.Hour))
		if err != nil || count != 1 {
			t.Fatalf("expired replacement cleanup count=%d err=%v", count, err)
		}
	})
}

type retentionBarrierTx struct {
	pgx.Tx
	calls         int
	afterExec     func(int)
	afterRollback func()
}

func (tx *retentionBarrierTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	tx.calls++
	if err == nil && tx.afterExec != nil {
		tx.afterExec(tx.calls)
	}
	return tag, err
}

func (tx *retentionBarrierTx) Rollback(ctx context.Context) error {
	err := tx.Tx.Rollback(ctx)
	if tx.afterRollback != nil {
		tx.afterRollback()
	}
	return err
}

func retentionWaitForBlocked(t *testing.T, ctx context.Context, db *pgxpool.Pool, pid int32) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRow(deadline, `select cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatalf("query lock wait for backend %d: %v", pid, err)
		}
		if blocked {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("backend %d did not reach lock barrier: %v", pid, deadline.Err())
		case <-ticker.C:
		}
	}
}
