package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

func TestVPSStateRepairMISyncWaitsForRetirementGraphAndSuppressesWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	record := createVPSStateRepairSyncMI(t, ctx, miRepo, "MI sync race")
	syncRepo := NewPostgresSyncRepository(pool)
	token := "sync-retirement-race-token"
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set binding_status = $2, binding_fingerprint = $3, sync_token_hash = $4
		where monitoring_instance_id = $1`,
		record.MonitoringInstanceID,
		monitoringinstances.BindingBound,
		"fingerprint-race",
		syncRepo.tokenHasher.hashSyncToken(token)); err != nil {
		t.Fatalf("prepare accepted sync state: %v", err)
	}

	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin exclusive graph holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	resultCh := make(chan struct {
		result syncing.Result
		err    error
	}, 1)
	go func() {
		result, syncErr := syncRepo.ApplyBatch(ctx, vpsStateRepairSyncBatch(record.MonitoringInstanceID, token, "fingerprint-race", "sync_after_retirement"))
		resultCh <- struct {
			result syncing.Result
			err    error
		}{result: result, err: syncErr}
	}()
	if err := waitForAssetGraphSyncLockWaiter(ctx, pool); err != nil {
		t.Fatalf("sync did not wait on the shared asset graph lock: %v", err)
	}
	select {
	case result := <-resultCh:
		t.Fatalf("sync completed before retirement graph transaction: result=%#v error=%v", result.result, result.err)
	default:
	}

	if _, err := holder.Exec(ctx, `
		update monitoring_instances
		set lifecycle_status = $2, monitoring_status = $3
		where monitoring_instance_id = $1`,
		record.MonitoringInstanceID,
		monitoringinstances.LifecycleRetired,
		monitoringinstances.MonitoringPaused); err != nil {
		t.Fatalf("commit retirement state: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit graph holder: %v", err)
	}
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("sync after retirement graph commit: %v", result.err)
	}
	if result.result.Disposition != syncing.ResultDispositionSuppressed {
		t.Fatalf("sync disposition after retirement = %q, want suppressed", result.result.Disposition)
	}
	assertVPSStateRepairMIIntValue(t, ctx, pool, `select count(*)::int from monitoring_instance_heartbeats where monitoring_instance_id = $1`, record.MonitoringInstanceID, 0)
	assertVPSStateRepairMIIntValue(t, ctx, pool, `select count(*)::int from agent_sync_batches where monitoring_instance_id = $1`, record.MonitoringInstanceID, 0)
}

func TestVPSStateRepairMIIndependentSyncBatchesShareGraphLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	syncRepo := NewPostgresSyncRepository(pool)
	type instance struct {
		id          string
		token       string
		fingerprint string
		batchID     string
	}
	instances := make([]instance, 2)
	for index := range instances {
		record := createVPSStateRepairSyncMI(t, ctx, miRepo, fmt.Sprintf("MI shared sync %d", index))
		token := fmt.Sprintf("sync-shared-token-%d", index)
		fingerprint := fmt.Sprintf("fingerprint-shared-%d", index)
		if _, err := pool.Exec(ctx, `
			update monitoring_instances
			set binding_status = $2, binding_fingerprint = $3, sync_token_hash = $4
			where monitoring_instance_id = $1`,
			record.MonitoringInstanceID,
			monitoringinstances.BindingBound,
			fingerprint,
			syncRepo.tokenHasher.hashSyncToken(token)); err != nil {
			t.Fatalf("prepare accepted sync state: %v", err)
		}
		instances[index] = instance{id: record.MonitoringInstanceID, token: token, fingerprint: fingerprint, batchID: fmt.Sprintf("sync_shared_%d", index)}
	}

	holder, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin shared graph holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if err := lockAssetGraphForSync(ctx, holder); err != nil {
		t.Fatalf("lock shared graph holder: %v", err)
	}
	results := make(chan error, len(instances))
	for _, item := range instances {
		item := item
		go func() {
			result, syncErr := syncRepo.ApplyBatch(ctx, vpsStateRepairSyncBatch(item.id, item.token, item.fingerprint, item.batchID))
			if syncErr != nil {
				results <- syncErr
				return
			}
			if result.Disposition != syncing.ResultDispositionRecorded {
				results <- fmt.Errorf("MI %s sync disposition = %q, want recorded", item.id, result.Disposition)
				return
			}
			results <- nil
		}()
	}
	for range instances {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("sync while another shared lock is held: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("independent sync batches blocked behind a shared graph lock: %v", ctx.Err())
		}
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit shared graph holder: %v", err)
	}
}

func createVPSStateRepairSyncMI(t *testing.T, ctx context.Context, repo *PostgresMonitoringInstanceRepository, name string) monitoringinstances.Record {
	t.Helper()
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     name,
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance: %v", err)
	}
	return record
}

func vpsStateRepairSyncBatch(monitoringInstanceID, token, fingerprint, batchID string) syncing.Batch {
	return syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            token,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   time.Now().UTC().Truncate(time.Microsecond),
			AgentVersion: "agent/v0.1.0",
			Fingerprint:  fingerprint,
			SyncBatchID:  batchID,
		}},
		Observations: observations.BatchWrite{MonitoringInstanceID: monitoringInstanceID},
	}
}

func waitForAssetGraphSyncLockWaiter(ctx context.Context, pool *pgxpool.Pool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := pool.QueryRow(ctx, `
			select count(*)
			from pg_stat_activity
			where datname = current_database()
			  and pid <> pg_backend_pid()
			  and wait_event_type = 'Lock'
			  and query like '%pg_advisory_xact_lock_shared%'`).Scan(&waiting)
		if err != nil {
			return err
		}
		if waiting > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for sync to block on the shared asset graph lock")
}

func assertVPSStateRepairMIIntValue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query, monitoringInstanceID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query, monitoringInstanceID).Scan(&got); err != nil {
		t.Fatalf("query MI invariant count: %v", err)
	}
	if got != want {
		t.Fatalf("MI invariant count = %d, want %d", got, want)
	}
}
