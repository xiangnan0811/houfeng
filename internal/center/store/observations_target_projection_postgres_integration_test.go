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
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationObservationBatchProjectsEligibleTargetTimestamps(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "observation-target-projection", 2)

	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values ('vps_observation_projection', 'Observation projection', 'active', 'in_use')
	`); err != nil {
		t.Fatalf("insert observation projection VPS: %v", err)
	}
	observedBase := time.Date(2026, time.August, 30, 6, 0, 0, 0, time.UTC)
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id, vps_id, display_name, region, city, provider,
			lifecycle_status, binding_status, binding_fingerprint, binding_epoch_started_at
		) values ('mi_observation_projection', 'vps_observation_projection', 'Observation projection', '', '', '', '已接入', '已绑定', 'fixture', $1)
	`, observedBase.Add(-time.Hour)); err != nil {
		t.Fatalf("insert observation projection monitoring instance: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(target_id, name, target_type, host, run_status, lifecycle_status)
		values
			('tg_observation_projection', 'Projected target', 'service', 'projection.example.test', '启用', 'active'),
			('tg_observation_excluded', 'Excluded target', 'service', 'excluded.example.test', '启用', 'active')
	`); err != nil {
		t.Fatalf("insert observation projection targets: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values
			('pb_observation_projection', 'tg_observation_projection', 'http', '5m', 10),
			('pb_observation_excluded', 'tg_observation_excluded', 'http', '5m', 10)
	`); err != nil {
		t.Fatalf("insert observation projection probe items: %v", err)
	}

	repository := NewPostgresObservationRepository(pool)
	successAt := observedBase.Add(10 * time.Minute)
	failureAt := observedBase.Add(11 * time.Minute)
	if err := repository.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			observationProjectionProbe("tg_observation_projection", "pb_observation_projection", successAt, agentapi.ProbeResultSuccess, false, false),
			observationProjectionProbe("tg_observation_projection", "pb_observation_projection", failureAt, agentapi.ProbeResultFailure, false, false),
		},
	}); err != nil {
		t.Fatalf("record eligible observation batch: %v", err)
	}
	assertTargetObservationTimestamps(t, ctx, pool, "tg_observation_projection", &successAt, &failureAt)
	assertProbeLiveObservationTimestamp(t, ctx, pool, "tg_observation_projection", "pb_observation_projection", &failureAt)

	staleSuccessAt := successAt.Add(-time.Minute)
	staleFailureAt := failureAt.Add(-time.Minute)
	if err := repository.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			observationProjectionProbe("tg_observation_projection", "pb_observation_projection", staleSuccessAt, agentapi.ProbeResultSuccess, false, false),
			observationProjectionProbe("tg_observation_projection", "pb_observation_projection", staleFailureAt, agentapi.ProbeResultFailure, false, false),
		},
	}); err != nil {
		t.Fatalf("record out-of-order observation batch: %v", err)
	}
	assertTargetObservationTimestamps(t, ctx, pool, "tg_observation_projection", &successAt, &failureAt)
	assertProbeLiveObservationTimestamp(t, ctx, pool, "tg_observation_projection", "pb_observation_projection", &failureAt)

	if err := repository.RecordBatch(ctx, observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{
			observationProjectionProbe("tg_observation_excluded", "pb_observation_excluded", observedBase.Add(20*time.Minute), agentapi.ProbeResultSuccess, true, false),
			observationProjectionProbe("tg_observation_excluded", "pb_observation_excluded", observedBase.Add(21*time.Minute), agentapi.ProbeResultFailure, false, true),
		},
	}); err != nil {
		t.Fatalf("record excluded observation batch: %v", err)
	}
	assertTargetObservationTimestamps(t, ctx, pool, "tg_observation_excluded", nil, nil)
	assertProbeLiveObservationTimestamp(t, ctx, pool, "tg_observation_excluded", "pb_observation_excluded", nil)

	var rawExcludedCount int
	if err := pool.QueryRow(ctx, `
		select count(*)::int
		from probe_observations
		where target_id = $1
	`, "tg_observation_excluded").Scan(&rawExcludedCount); err != nil {
		t.Fatalf("count excluded raw observations: %v", err)
	}
	if rawExcludedCount != 2 {
		t.Fatalf("excluded raw observation count = %d, want 2", rawExcludedCount)
	}
}

func TestPostgresIntegrationObservationBatchConcurrentTargetProjectionOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "observation-target-projection-order", 8)
	seedConcurrentObservationProjectionFixture(t, ctx, fixture)

	const (
		targetA = "tg_observation_projection_order_a"
		targetB = "tg_observation_projection_order_b"
	)
	base := time.Date(2026, time.August, 30, 7, 0, 0, 0, time.UTC)
	successA := base.Add(1 * time.Minute)
	failureB := base.Add(2 * time.Minute)
	successB := base.Add(3 * time.Minute)
	failureA := base.Add(4 * time.Minute)
	fingerprintA := "fingerprint_observation_projection_order_a"
	fingerprintB := "fingerprint_observation_projection_order_b"
	tokenA := "mas_observation_projection_order_a.secret"
	tokenB := "mas_observation_projection_order_b.secret"

	batchA := observationProjectionConcurrentBatch(
		"mi_observation_projection_order_a",
		"mas_observation_projection_order_a",
		tokenA,
		fingerprintA,
		"sync_observation_projection_order_a",
		base,
		observationProjectionConcurrentProbe(targetA, "pb_observation_projection_order_a", successA, agentapi.ProbeResultSuccess),
		observationProjectionConcurrentProbe(targetB, "pb_observation_projection_order_b", failureB, agentapi.ProbeResultFailure),
	)
	batchB := observationProjectionConcurrentBatch(
		"mi_observation_projection_order_b",
		"mas_observation_projection_order_b",
		tokenB,
		fingerprintB,
		"sync_observation_projection_order_b",
		base.Add(time.Second),
		observationProjectionConcurrentProbe(targetB, "pb_observation_projection_order_b", successB, agentapi.ProbeResultSuccess),
		observationProjectionConcurrentProbe(targetA, "pb_observation_projection_order_a", failureA, agentapi.ProbeResultFailure),
	)

	firstPool := fixture.openDirectRuntimePool(t, ctx, "observation-target-projection-order-first", 1)
	secondPool := fixture.openDirectRuntimePool(t, ctx, "observation-target-projection-order-second", 1)
	var firstPID, secondPID int32
	if err := firstPool.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&firstPID); err != nil {
		t.Fatalf("read first observation projection backend PID: %v", err)
	}
	if err := secondPool.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatalf("read second observation projection backend PID: %v", err)
	}

	holder, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin target projection row-lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var holderPID int32
	if err := holder.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatalf("read target projection holder backend PID: %v", err)
	}
	if _, err := holder.Exec(ctx, `select target_id from targets where target_id = $1 for share`, targetA); err != nil {
		t.Fatalf("lock target A before concurrent batches: %v", err)
	}

	repositories := []*PostgresSyncRepository{
		NewPostgresSyncRepository(firstPool),
		NewPostgresSyncRepository(secondPool),
	}
	batches := []syncing.Batch{batchA, batchB}
	batchPIDs := []int32{firstPID, secondPID}
	results := make(chan error, len(batches))
	apply := func(repository *PostgresSyncRepository, batch syncing.Batch) {
		go func() {
			_, applyErr := repository.ApplyBatch(ctx, batch)
			results <- applyErr
		}()
	}
	apply(repositories[0], batches[0])
	if err := waitForObservationProjectionTargetWaiter(ctx, fixture.db, batchPIDs[0], holderPID); err != nil {
		_ = holder.Rollback(ctx)
		<-results
		t.Fatalf("first concurrent batch did not wait on target A: %v", err)
	}
	apply(repositories[1], batches[1])
	if err := waitForObservationProjectionTargetWaiter(ctx, fixture.db, batchPIDs[1], holderPID); err != nil {
		_ = holder.Rollback(ctx)
		for range batches {
			<-results
		}
		t.Fatalf("concurrent batches did not both wait on target A: %v", err)
	}
	for _, pid := range batchPIDs {
		var statement, waitEventType, waitEvent string
		var blockers []int32
		if err := fixture.db.QueryRow(ctx, `
			select coalesce(query, ''), coalesce(wait_event_type, ''), coalesce(wait_event, ''),
			       pg_catalog.pg_blocking_pids(pid)
			from pg_catalog.pg_stat_activity
			where pid = $1
		`, pid).Scan(&statement, &waitEventType, &waitEvent, &blockers); err != nil {
			t.Fatalf("read observation projection backend %d lock state: %v", pid, err)
		}
		t.Logf("observation projection backend pid=%d statement=%q wait=%s/%s blockers=%v", pid, statement, waitEventType, waitEvent, blockers)
	}
	probeTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin target B lock probe: %v", err)
	}
	var probedTarget string
	probeErr := probeTx.QueryRow(ctx, `
		select target_id
		from targets
		where target_id = $1
		for no key update nowait
	`, targetB).Scan(&probedTarget)
	if probeErr == nil {
		t.Logf("target B FOR NO KEY UPDATE NOWAIT probe succeeded before target A release; target B row was not locked")
	} else {
		var postgresErr *pgconn.PgError
		if !errors.As(probeErr, &postgresErr) || postgresErr.Code != "55P03" {
			_ = probeTx.Rollback(ctx)
			t.Fatalf("target B FOR NO KEY UPDATE NOWAIT probe: %v", probeErr)
		}
		t.Logf("target B FOR NO KEY UPDATE NOWAIT probe returned lock_not_available (55P03); reversed batch holds target B")
	}
	if err := probeTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback target B lock probe: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release target A row lock: %v", err)
	}

	var applyErrors []error
	for range batches {
		select {
		case applyErr := <-results:
			if applyErr != nil {
				applyErrors = append(applyErrors, applyErr)
			}
		case <-ctx.Done():
			t.Fatalf("concurrent observation batches did not finish: %v", ctx.Err())
		}
	}
	if len(applyErrors) != 0 {
		t.Fatalf("concurrent observation batches failed: %v", applyErrors)
	}

	assertTargetObservationTimestamps(t, ctx, pool, targetA, &successA, &failureA)
	assertTargetObservationTimestamps(t, ctx, pool, targetB, &successB, &failureB)
	assertProbeLiveObservationTimestamp(t, ctx, pool, targetA, "pb_observation_projection_order_a", &failureA)
	assertProbeLiveObservationTimestamp(t, ctx, pool, targetB, "pb_observation_projection_order_b", &successB)

	var rawCount int
	if err := pool.QueryRow(ctx, `
		select count(*)::int
		from probe_observations
		where target_id in ($1, $2)
	`, targetA, targetB).Scan(&rawCount); err != nil {
		t.Fatalf("count concurrent target projection raw facts: %v", err)
	}
	if rawCount != 4 {
		t.Fatalf("concurrent target projection raw fact count = %d, want 4", rawCount)
	}
}

func seedConcurrentObservationProjectionFixture(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status)
		values
			('vps_observation_projection_order_a', 'Observation projection order A', 'active'),
			('vps_observation_projection_order_b', 'Observation projection order B', 'active')
	`); err != nil {
		t.Fatalf("insert concurrent observation projection VPS: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id, vps_id, display_name, region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
			binding_epoch_started_at, sync_token_hash
		) values
			('mi_observation_projection_order_a', 'vps_observation_projection_order_a', 'Observation projection order A', '', '', '',
			 $1, $2, $3, $4, $5, $6),
			('mi_observation_projection_order_b', 'vps_observation_projection_order_b', 'Observation projection order B', '', '', '',
			 $1, $2, $3, $7, $5, $8)
	`, monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringEnabled, monitoringinstances.BindingBound,
		"fingerprint_observation_projection_order_a", time.Date(2026, time.August, 30, 6, 0, 0, 0, time.UTC),
		hashSyncToken("mas_observation_projection_order_a.secret"),
		"fingerprint_observation_projection_order_b",
		hashSyncToken("mas_observation_projection_order_b.secret")); err != nil {
		t.Fatalf("insert concurrent observation projection monitoring instances: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_agent_sessions(session_id, monitoring_instance_id, token_hash, fingerprint_hash)
		values
			('mas_observation_projection_order_a', 'mi_observation_projection_order_a', $1, $2),
			('mas_observation_projection_order_b', 'mi_observation_projection_order_b', $3, $4)
	`,
		hashSyncToken("mas_observation_projection_order_a.secret"),
		"fingerprint_observation_projection_order_a",
		hashSyncToken("mas_observation_projection_order_b.secret"),
		"fingerprint_observation_projection_order_b"); err != nil {
		t.Fatalf("insert concurrent observation projection sessions: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(target_id, name, target_type, host, run_status, lifecycle_status)
		values
			('tg_observation_projection_order_a', 'Observation projection order A', 'service', 'order-a.example.test', '启用', 'active'),
			('tg_observation_projection_order_b', 'Observation projection order B', 'service', 'order-b.example.test', '启用', 'active')
	`); err != nil {
		t.Fatalf("insert concurrent observation projection targets: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds)
		values
			('pb_observation_projection_order_a', 'tg_observation_projection_order_a', 'http', '5m', 10),
			('pb_observation_projection_order_b', 'tg_observation_projection_order_b', 'http', '5m', 10)
	`); err != nil {
		t.Fatalf("insert concurrent observation projection probe items: %v", err)
	}
}

func observationProjectionConcurrentBatch(
	monitoringInstanceID, sessionID, syncToken, fingerprint, syncBatchID string,
	heartbeatAt time.Time,
	first, second observations.ProbeObservationWrite,
) syncing.Batch {
	probes := []observations.ProbeObservationWrite{first, second}
	for index := range probes {
		probes[index].MonitoringInstanceID = monitoringInstanceID
		probes[index].Fingerprint = fingerprint
		probes[index].SyncBatchID = syncBatchID
	}
	return syncing.Batch{
		SessionID:            sessionID,
		LiveSignal:           &agentapi.LiveSignal{ID: "live_" + syncBatchID, Fingerprint: fingerprint},
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   heartbeatAt,
			AgentVersion: "fixture",
			Fingerprint:  fingerprint,
			SyncBatchID:  syncBatchID,
		}},
		Observations: observations.BatchWrite{
			MonitoringInstanceID: monitoringInstanceID,
			ProbeObservations:    probes,
		},
	}
}

func observationProjectionConcurrentProbe(targetID, probeItemID string, observedAt time.Time, resultKind string) observations.ProbeObservationWrite {
	observation := observations.ProbeObservationWrite{
		TargetID:    targetID,
		ProbeItemID: probeItemID,
		ProbeKind:   agentapi.ProbeKindHTTP,
		ObservedAt:  observedAt,
		ResultKind:  resultKind,
	}
	if resultKind == agentapi.ProbeResultFailure {
		observation.ErrorCode = agentapi.ProbeErrorHTTPStatus
		observation.ErrorSummary = "503"
	}
	return observation
}

func waitForObservationProjectionTargetWaiter(ctx context.Context, pool *pgxpool.Pool, blockedPID, blockerPID int32) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			select exists (
				select 1
				from pg_catalog.pg_stat_activity
				where pid = $1
				  and state = 'active'
				  and wait_event_type = 'Lock'
				  and query like '%update targets%'
				  and $2 = any(pg_catalog.pg_blocking_pids(pid))
			)
		`, blockedPID, blockerPID).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for backend %d to block on target projection holder %d", blockedPID, blockerPID)
}

func observationProjectionProbe(targetID, probeItemID string, observedAt time.Time, resultKind string, maintenance, backfilled bool) observations.ProbeObservationWrite {
	observation := observations.ProbeObservationWrite{
		MonitoringInstanceID: "mi_observation_projection",
		TargetID:             targetID,
		ProbeItemID:          probeItemID,
		ProbeKind:            agentapi.ProbeKindHTTP,
		ObservedAt:           observedAt,
		ReceivedAt:           observedAt.Add(time.Second),
		AgentVersion:         "fixture",
		Fingerprint:          "fixture",
		ResultKind:           resultKind,
		MaintenanceContext:   maintenance,
		IsBackfilled:         backfilled,
		SyncBatchID:          "sync_" + targetID + "_" + observedAt.Format("150405"),
	}
	if resultKind == agentapi.ProbeResultFailure {
		observation.ErrorCode = agentapi.ProbeErrorHTTPStatus
		observation.ErrorSummary = "503"
	}
	return observation
}

func assertTargetObservationTimestamps(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID string, wantSuccess, wantFailure *time.Time) {
	t.Helper()
	var gotSuccess, gotFailure *time.Time
	if err := pool.QueryRow(ctx, `
		select last_success_at, last_failure_at
		from targets
		where target_id = $1
	`, targetID).Scan(&gotSuccess, &gotFailure); err != nil {
		t.Fatalf("read target %q observation timestamps: %v", targetID, err)
	}
	if !sameOptionalTimestamp(gotSuccess, wantSuccess) || !sameOptionalTimestamp(gotFailure, wantFailure) {
		t.Fatalf("target %q timestamps = (%v, %v), want (%v, %v)", targetID, gotSuccess, gotFailure, wantSuccess, wantFailure)
	}
}

func assertProbeLiveObservationTimestamp(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID, probeItemID string, want *time.Time) {
	t.Helper()
	var got *time.Time
	if err := pool.QueryRow(ctx, `
		select last_live_observed_at
		from probe_items
		where target_id = $1
		  and probe_item_id = $2
	`, targetID, probeItemID).Scan(&got); err != nil {
		t.Fatalf("read probe %q live observation timestamp: %v", probeItemID, err)
	}
	if !sameOptionalTimestamp(got, want) {
		t.Fatalf("probe %q live observation timestamp = %v, want %v", probeItemID, got, want)
	}
}

func sameOptionalTimestamp(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}
