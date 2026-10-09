package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationTargetFreshnessPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "target-freshness-writer", 4)

	const (
		vpsID       = "vps_target_freshness_writer"
		instanceID  = "mi_target_freshness_writer"
		targetID    = "tg_target_freshness_writer"
		probeItemID = "pb_target_freshness_writer"
	)
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	initialConfig := `{"headers":{"x-fixture":"one"},"url":"https://freshness.example.test"}`

	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'Target freshness writer VPS', 'active', 'in_use')
	`, vpsID); err != nil {
		t.Fatalf("insert freshness VPS: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id, vps_id, display_name, region, city, provider,
			labels, lifecycle_status, binding_status, binding_fingerprint, binding_epoch_started_at
		) values ($1, $2, 'Target freshness writer instance', '', '', '', array['freshness'], '已接入', '已绑定', 'freshness-fixture', $3)
	`, instanceID, vpsID, base); err != nil {
		t.Fatalf("insert freshness monitoring instance: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(
			target_id, name, target_type, host, execution_monitoring_instance_labels,
			lifecycle_status, run_status, "group", labels, note,
			current_health_status, current_active_incident_count, current_primary_issue_summary,
			freshness_reset_at
		) values ($1, 'Target freshness writer target', 'service', 'freshness.example.test', array['freshness'],
			  'active', '暂停', 'freshness', array['initial'], 'initial note', '正常', 0, '', $2)
	`, targetID, base); err != nil {
		t.Fatalf("insert freshness target: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(
			probe_item_id, target_id, probe_kind, enabled, frequency_tier, timeout_seconds,
			config, freshness_reset_at
		) values ($1, $2, 'http', true, '5m', 5, $3::jsonb, $4)
	`, probeItemID, targetID, initialConfig, base); err != nil {
		t.Fatalf("insert freshness probe: %v", err)
	}

	targetRepo := NewPostgresTargetRepository(pool)
	observationRepo := NewPostgresObservationRepository(pool)

	resumed, err := targetRepo.ResumeTargetRun(ctx, targetID)
	if err != nil {
		t.Fatalf("ResumeTargetRun() from paused: %v", err)
	}
	if resumed.RunStatus != targets.RunStatusEnabled {
		t.Fatalf("resumed RunStatus = %q, want %q", resumed.RunStatus, targets.RunStatusEnabled)
	}
	firstResumeReset := readTargetFreshnessReset(t, ctx, pool, targetID)
	if !firstResumeReset.After(base) {
		t.Fatalf("first resume freshness reset = %v, want after %v", firstResumeReset, base)
	}

	if _, err := targetRepo.ResumeTargetRun(ctx, targetID); err != nil {
		t.Fatalf("duplicate ResumeTargetRun(): %v", err)
	}
	duplicateResumeReset := readTargetFreshnessReset(t, ctx, pool, targetID)
	if !duplicateResumeReset.Equal(firstResumeReset) {
		t.Fatalf("duplicate resume reset = %v, want unchanged %v", duplicateResumeReset, firstResumeReset)
	}

	if _, err := targetRepo.SetTargetMaintenance(ctx, targetID); err != nil {
		t.Fatalf("SetTargetMaintenance(): %v", err)
	}
	maintenanceReset := readTargetFreshnessReset(t, ctx, pool, targetID)
	if !maintenanceReset.Equal(firstResumeReset) {
		t.Fatalf("maintenance reset = %v, want unchanged %v", maintenanceReset, firstResumeReset)
	}

	maintenanceExitBase := base.Add(2 * time.Hour)
	if _, err := fixture.db.Exec(ctx, `update targets set freshness_reset_at = $2 where target_id = $1`, targetID, maintenanceExitBase); err != nil {
		t.Fatalf("set maintenance exit reset baseline: %v", err)
	}
	if _, err := targetRepo.ResumeTargetRun(ctx, targetID); err != nil {
		t.Fatalf("ResumeTargetRun() from maintenance: %v", err)
	}
	maintenanceExitReset := readTargetFreshnessReset(t, ctx, pool, targetID)
	if !maintenanceExitReset.After(maintenanceExitBase) {
		t.Fatalf("maintenance exit reset = %v, want after %v", maintenanceExitReset, maintenanceExitBase)
	}
	if _, err := targetRepo.ResumeTargetRun(ctx, targetID); err != nil {
		t.Fatalf("duplicate maintenance-exit ResumeTargetRun(): %v", err)
	}
	if duplicate := readTargetFreshnessReset(t, ctx, pool, targetID); !duplicate.Equal(maintenanceExitReset) {
		t.Fatalf("duplicate maintenance-exit reset = %v, want unchanged %v", duplicate, maintenanceExitReset)
	}

	metadataReset := readTargetFreshnessReset(t, ctx, pool, targetID)
	group := "metadata-updated"
	if _, err := targetRepo.UpdateTargetMetadata(ctx, targetID, targets.UpdateMetadataInput{
		Group:  &group,
		Labels: []string{"metadata"},
		Note:   "metadata-only update",
	}); err != nil {
		t.Fatalf("UpdateTargetMetadata(): %v", err)
	}
	if got := readTargetFreshnessReset(t, ctx, pool, targetID); !got.Equal(metadataReset) {
		t.Fatalf("metadata update reset = %v, want unchanged %v", got, metadataReset)
	}

	probeReset := readProbeFreshnessReset(t, ctx, pool, probeItemID)
	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier5m,
		TimeoutSeconds: 5,
		Config:         json.RawMessage(`{"url":"https://freshness.example.test","headers":{"x-fixture":"one"}}`),
	}); err != nil {
		t.Fatalf("same-config UpdateProbeItem(): %v", err)
	}
	if got := readProbeFreshnessReset(t, ctx, pool, probeItemID); !got.Equal(probeReset) {
		t.Fatalf("same-config JSONB reset = %v, want unchanged %v", got, probeReset)
	}

	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 9,
		Config:         json.RawMessage(initialConfig),
	}); err != nil {
		t.Fatalf("cadence-timeout-only UpdateProbeItem(): %v", err)
	}
	if got := readProbeFreshnessReset(t, ctx, pool, probeItemID); !got.Equal(probeReset) {
		t.Fatalf("cadence-timeout-only reset = %v, want unchanged %v", got, probeReset)
	}

	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTLS,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 9,
		Config:         json.RawMessage(initialConfig),
	}); err != nil {
		t.Fatalf("kind-change UpdateProbeItem(): %v", err)
	}
	kindReset := readProbeFreshnessReset(t, ctx, pool, probeItemID)
	if !kindReset.After(probeReset) {
		t.Fatalf("kind-change reset = %v, want after %v", kindReset, probeReset)
	}

	configResetBaseline := base.Add(3 * time.Hour)
	if _, err := fixture.db.Exec(ctx, `update probe_items set freshness_reset_at = $2 where probe_item_id = $1`, probeItemID, configResetBaseline); err != nil {
		t.Fatalf("set config reset baseline: %v", err)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTLS,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 9,
		Config:         json.RawMessage(`{"url":"https://changed.example.test","headers":{"x-fixture":"one"}}`),
	}); err != nil {
		t.Fatalf("config-change UpdateProbeItem(): %v", err)
	}
	configReset := readProbeFreshnessReset(t, ctx, pool, probeItemID)
	if !configReset.After(configResetBaseline) {
		t.Fatalf("config-change reset = %v, want after %v", configReset, configResetBaseline)
	}

	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTLS,
		Enabled:        false,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 9,
		Config:         json.RawMessage(`{"url":"https://changed.example.test","headers":{"x-fixture":"one"}}`),
	}); err != nil {
		t.Fatalf("disable probe UpdateProbeItem(): %v", err)
	}
	if got := readProbeFreshnessReset(t, ctx, pool, probeItemID); !got.Equal(configReset) {
		t.Fatalf("disable probe reset = %v, want unchanged %v", got, configReset)
	}
	if _, err := targetRepo.UpdateProbeItem(ctx, targetID, probeItemID, targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTLS,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 9,
		Config:         json.RawMessage(`{"url":"https://changed.example.test","headers":{"x-fixture":"one"}}`),
	}); err != nil {
		t.Fatalf("re-enable probe UpdateProbeItem(): %v", err)
	}
	reenabledReset := readProbeFreshnessReset(t, ctx, pool, probeItemID)
	if !reenabledReset.After(configReset) {
		t.Fatalf("re-enable reset = %v, want after %v", reenabledReset, configReset)
	}

	observedBase := base.Add(10 * time.Hour)
	writeObservation := func(id string, observedAt, receivedAt time.Time, resultKind string, maintenance, backfilled bool) {
		t.Helper()
		observation := observations.ProbeObservationWrite{
			MonitoringInstanceID: instanceID,
			TargetID:             targetID,
			ProbeItemID:          probeItemID,
			ProbeKind:            agentapi.ProbeKindTLS,
			ObservedAt:           observedAt,
			ReceivedAt:           receivedAt,
			AgentVersion:         "freshness-writer-fixture",
			Fingerprint:          "freshness-writer-fixture",
			ResultKind:           resultKind,
			MaintenanceContext:   maintenance,
			IsBackfilled:         backfilled,
			SyncBatchID:          id,
		}
		if resultKind == agentapi.ProbeResultFailure {
			observation.ErrorCode = agentapi.ProbeErrorHTTPStatus
			observation.ErrorSummary = "503"
		}
		if err := observationRepo.RecordBatch(ctx, observations.BatchWrite{ProbeObservations: []observations.ProbeObservationWrite{observation}}); err != nil {
			t.Fatalf("RecordBatch(%q): %v", id, err)
		}
	}

	writeObservation("freshness-live-1", observedBase, observedBase.Add(5*time.Second), agentapi.ProbeResultSuccess, false, false)
	if got := readProbeLiveObservedAt(t, ctx, pool, probeItemID); !got.Equal(observedBase) {
		t.Fatalf("first live projection = %v, want %v", got, observedBase)
	}
	futureObserved := observedBase.Add(10 * time.Hour)
	futureReceived := observedBase.Add(5 * time.Hour)
	writeObservation("freshness-live-future", futureObserved, futureReceived, agentapi.ProbeResultFailure, false, false)
	if got := readProbeLiveObservedAt(t, ctx, pool, probeItemID); !got.Equal(futureReceived) {
		t.Fatalf("future-clock live projection = %v, want capped receipt %v", got, futureReceived)
	}
	writeObservation("freshness-live-late", observedBase.Add(-time.Hour), observedBase.Add(-30*time.Minute), agentapi.ProbeResultSuccess, false, false)
	if got := readProbeLiveObservedAt(t, ctx, pool, probeItemID); !got.Equal(futureReceived) {
		t.Fatalf("late live projection = %v, want unchanged %v", got, futureReceived)
	}
	writeObservation("freshness-live-maintenance", observedBase.Add(20*time.Hour), observedBase.Add(20*time.Hour+time.Second), agentapi.ProbeResultSuccess, true, false)
	writeObservation("freshness-live-backfill", observedBase.Add(21*time.Hour), observedBase.Add(21*time.Hour+time.Second), agentapi.ProbeResultFailure, false, true)
	if got := readProbeLiveObservedAt(t, ctx, pool, probeItemID); !got.Equal(futureReceived) {
		t.Fatalf("maintenance/backfill live projection = %v, want unchanged %v", got, futureReceived)
	}

	var rawCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from probe_observations
		where target_id = $1 and probe_item_id = $2
	`, targetID, probeItemID).Scan(&rawCount); err != nil {
		t.Fatalf("count raw freshness observations: %v", err)
	}
	if rawCount != 5 {
		t.Fatalf("raw freshness observations = %d, want 5", rawCount)
	}
	if _, err := fixture.db.Exec(ctx, `delete from probe_observations where target_id = $1 and probe_item_id = $2`, targetID, probeItemID); err != nil {
		t.Fatalf("delete retained raw freshness observations: %v", err)
	}
	if got := readProbeLiveObservedAt(t, ctx, pool, probeItemID); !got.Equal(futureReceived) {
		t.Fatalf("live projection after raw retention cleanup = %v, want %v", got, futureReceived)
	}
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from probe_observations
		where target_id = $1 and probe_item_id = $2
	`, targetID, probeItemID).Scan(&rawCount); err != nil {
		t.Fatalf("count cleaned raw freshness observations: %v", err)
	}
	if rawCount != 0 {
		t.Fatalf("raw freshness observations after cleanup = %d, want 0", rawCount)
	}
}

func TestPostgresIntegrationTargetFreshnessResetUsesPostTransitionWallClock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	resumePool := fixture.openDirectRuntimePool(t, ctx, "target-freshness-resume-clock", 1)
	ingestPool := fixture.openDirectRuntimePool(t, ctx, "target-freshness-ingest-clock", 1)
	rowPool := fixture.openDirectRuntimePool(t, ctx, "target-freshness-row-holder", 1)

	const (
		vpsID       = "vps_target_freshness_resume_clock"
		instanceID  = "mi_target_freshness_resume_clock"
		sessionID   = "mas_target_freshness_resume_clock"
		targetID    = "tg_target_freshness_resume_clock"
		probeItemID = "pb_target_freshness_resume_clock"
		syncToken   = "target-freshness-resume-clock.secret"
		fingerprint = "target-freshness-resume-clock"
		syncBatchID = "sync_target_freshness_resume_clock"
	)
	if _, err := fixture.db.Exec(ctx, `
		insert into vps_assets(vps_id,display_name,lifecycle_status,usage_status)
		values ($1,'Target freshness resume clock VPS','active','in_use')
	`, vpsID); err != nil {
		t.Fatalf("seed freshness resume-clock VPS: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_instances(
			monitoring_instance_id,vps_id,display_name,region,city,provider,
			lifecycle_status,monitoring_status,binding_status,binding_fingerprint,
			sync_token_hash
		) values (
			$1,$2,'Target freshness resume clock instance','','','',
			$3,$4,$5,$6,$7
		)
	`, instanceID, vpsID,
		monitoringinstances.LifecycleInUse,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound,
		fingerprint,
		hashSyncToken(syncToken),
	); err != nil {
		t.Fatalf("seed freshness resume-clock monitoring instance: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into monitoring_agent_sessions(
			session_id,monitoring_instance_id,token_hash,fingerprint_hash,capability
		) values ($1,$2,$3,$4,'full')
	`, sessionID, instanceID, hashSyncToken(syncToken), fingerprint); err != nil {
		t.Fatalf("seed freshness resume-clock session: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into targets(
			target_id,name,target_type,host,execution_monitoring_instance_labels,
			lifecycle_status,run_status,"group",current_health_status
		) values ($1,'Target freshness resume clock target','service','resume-clock.example.test',
			array[]::text[],'active','暂停','freshness','正常')
	`, targetID); err != nil {
		t.Fatalf("seed freshness resume-clock target: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into probe_items(
			probe_item_id,target_id,probe_kind,enabled,frequency_tier,timeout_seconds,config
		) values ($1,$2,'http',true,'5s',3,'{}'::jsonb)
	`, probeItemID, targetID); err != nil {
		t.Fatalf("seed freshness resume-clock probe: %v", err)
	}

	rowHolderPID := targetFreshnessBackendPID(t, ctx, rowPool)
	rowHolder, err := rowPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin freshness target row holder: %v", err)
	}
	defer func() { _ = rowHolder.Rollback(ctx) }()
	var lockedTargetID string
	if err := rowHolder.QueryRow(ctx, `select target_id from targets where target_id=$1 for share`, targetID).Scan(&lockedTargetID); err != nil {
		t.Fatalf("hold freshness target row: %v", err)
	}
	if lockedTargetID != targetID {
		t.Fatalf("locked target id=%q want %q", lockedTargetID, targetID)
	}

	futureObservedAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	ingestPID := targetFreshnessBackendPID(t, ctx, ingestPool)
	receivedAtReady := make(chan struct{})
	allowReceivedAt := make(chan struct{})
	var receivedAtOnce sync.Once
	ingestRepository := NewPostgresSyncRepository(ingestPool)
	ingestRepository.receptionFault = nil
	ingestRepository.now = func() time.Time {
		receivedAtOnce.Do(func() { close(receivedAtReady) })
		select {
		case <-allowReceivedAt:
		case <-ctx.Done():
		}
		return time.Now().UTC()
	}
	ingestDone := make(chan error, 1)
	go func() {
		_, ingestErr := ingestRepository.ApplyBatch(ctx, syncing.Batch{
			SessionID:            sessionID,
			MonitoringInstanceID: instanceID,
			SyncToken:            syncToken,
			LiveSignal:           &agentapi.LiveSignal{ID: "live_" + syncBatchID, Fingerprint: fingerprint},
			Heartbeats: []syncing.HeartbeatPayload{{
				ObservedAt:   futureObservedAt,
				AgentVersion: "freshness-resume-clock-fixture",
				Fingerprint:  fingerprint,
				SyncBatchID:  syncBatchID,
			}},
			Observations: observations.BatchWrite{
				MonitoringInstanceID: instanceID,
				ProbeObservations: []observations.ProbeObservationWrite{{
					MonitoringInstanceID: instanceID,
					TargetID:             targetID,
					ProbeItemID:          probeItemID,
					ProbeKind:            agentapi.ProbeKindHTTP,
					ObservedAt:           futureObservedAt,
					AgentVersion:         "freshness-resume-clock-fixture",
					Fingerprint:          fingerprint,
					ResultKind:           agentapi.ProbeResultSuccess,
					SyncBatchID:          syncBatchID,
				}},
			},
		})
		ingestDone <- ingestErr
	}()
	select {
	case <-receivedAtReady:
	case <-ctx.Done():
		t.Fatalf("freshness ingestion did not reach receive-time barrier: %v", ctx.Err())
	}

	resumePID := targetFreshnessBackendPID(t, ctx, resumePool)
	resumeDone := make(chan error, 1)
	go func() {
		_, resumeErr := NewPostgresTargetRepository(resumePool).ResumeTargetRun(ctx, targetID)
		resumeDone <- resumeErr
	}()
	if err := waitForTargetFreshnessLockWaiter(ctx, fixture.db, resumePID, "pg_advisory_xact_lock"); err != nil {
		t.Fatalf("resume did not wait for ingestion graph lock: %v", err)
	}
	close(allowReceivedAt)
	rowWaitDone := make(chan error, 1)
	go func() {
		rowWaitDone <- waitForTargetFreshnessRowWaiter(ctx, fixture.db, ingestPID, rowHolderPID)
	}()
	select {
	case ingestErr := <-ingestDone:
		if ingestErr != nil {
			t.Fatalf("freshness ingestion exited before target-row wait: %v", ingestErr)
		}
		t.Fatal("freshness ingestion committed before target-row wait")
	case rowWaitErr := <-rowWaitDone:
		if rowWaitErr != nil {
			select {
			case ingestErr := <-ingestDone:
				t.Fatalf("ingestion did not reach target-row wait: %v (ingestion result: %v)", rowWaitErr, ingestErr)
			default:
			}
			t.Fatalf("ingestion did not reach target-row wait: %v", rowWaitErr)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for ingestion target-row wait: %v", ctx.Err())
	}

	if err := rowHolder.Commit(ctx); err != nil {
		t.Fatalf("release freshness target row holder: %v", err)
	}
	select {
	case ingestErr := <-ingestDone:
		if ingestErr != nil {
			t.Fatalf("delayed freshness ingestion: %v", ingestErr)
		}
	case <-ctx.Done():
		t.Fatalf("delayed freshness ingestion did not finish: %v", ctx.Err())
	}
	select {
	case resumeErr := <-resumeDone:
		if resumeErr != nil {
			t.Fatalf("delayed freshness resume: %v", resumeErr)
		}
	case <-ctx.Done():
		t.Fatalf("delayed freshness resume did not finish: %v", ctx.Err())
	}

	var observedAt, receivedAt, resetAt, liveObservedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select observed_at,received_at from probe_observations where sync_batch_id=$1
	`, syncBatchID).Scan(&observedAt, &receivedAt); err != nil {
		t.Fatalf("read delayed freshness observation timestamps: %v", err)
	}
	if !observedAt.Equal(futureObservedAt) {
		t.Fatalf("delayed freshness observed_at=%v, want future timestamp %v", observedAt, futureObservedAt)
	}
	if err := fixture.db.QueryRow(ctx, `
		select t.freshness_reset_at,p.last_live_observed_at
		from targets t
		join probe_items p on p.target_id=t.target_id
		where t.target_id=$1 and p.probe_item_id=$2
	`, targetID, probeItemID).Scan(&resetAt, &liveObservedAt); err != nil {
		t.Fatalf("read delayed freshness reset and projection: %v", err)
	}
	if !resetAt.After(receivedAt) {
		t.Fatalf("resume freshness reset=%v, want after earlier received_at=%v", resetAt, receivedAt)
	}
	if !liveObservedAt.Equal(receivedAt) {
		t.Fatalf("future-clock live observation=%v, want receipt-capped %v", liveObservedAt, receivedAt)
	}
	if !liveObservedAt.Before(resetAt) {
		t.Fatalf("future-clock live observation=%v crossed resume reset=%v", liveObservedAt, resetAt)
	}
	record, err := NewPostgresTargetRepository(resumePool).GetTarget(ctx, targetID)
	if err != nil {
		t.Fatalf("read delayed freshness target: %v", err)
	}
	if record.ObservationFreshness.State != "pending" {
		t.Fatalf("delayed freshness state=%q, want pending after post-transition reset", record.ObservationFreshness.State)
	}
}

func targetFreshnessBackendPID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int32 {
	t.Helper()
	var pid int32
	if err := pool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read target freshness backend pid: %v", err)
	}
	return pid
}

func waitForTargetFreshnessLockWaiter(ctx context.Context, pool *pgxpool.Pool, pid int32, queryFragment string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			select exists (
				select 1
				from pg_catalog.pg_stat_activity
				where pid=$1
				  and state='active'
				  and wait_event_type='Lock'
				  and query ilike '%' || $2 || '%'
			)
		`, pid, queryFragment).Scan(&waiting); err != nil {
			return err
		}
		if waiting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for backend %d on %q", pid, queryFragment)
}

func waitForTargetFreshnessRowWaiter(ctx context.Context, pool *pgxpool.Pool, blockedPID, blockerPID int32) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			select exists (
				select 1
				from pg_catalog.pg_stat_activity
				where pid=$1
				  and state='active'
				  and wait_event_type='Lock'
				  and $2 = any(pg_catalog.pg_blocking_pids(pid))
				  and query ilike '%update targets%'
			)
		`, blockedPID, blockerPID).Scan(&waiting); err != nil {
			return err
		}
		if waiting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	var statement, state, waitEventType, waitEvent string
	var blockers []int32
	if err := pool.QueryRow(ctx, `
		select coalesce(query,''),coalesce(state,''),coalesce(wait_event_type,''),
		       coalesce(wait_event,''),pg_catalog.pg_blocking_pids(pid)
		from pg_catalog.pg_stat_activity
		where pid=$1
	`, blockedPID).Scan(&statement, &state, &waitEventType, &waitEvent, &blockers); err != nil {
		return fmt.Errorf("timed out waiting for backend %d on target row held by %d (read state: %w)", blockedPID, blockerPID, err)
	}
	return fmt.Errorf("timed out waiting for backend %d on target row held by %d: state=%q wait=%s/%s blockers=%v query=%q", blockedPID, blockerPID, state, waitEventType, waitEvent, blockers, statement)
}

func readTargetFreshnessReset(t *testing.T, ctx context.Context, pool *pgxpool.Pool, targetID string) time.Time {
	t.Helper()
	var resetAt time.Time
	if err := pool.QueryRow(ctx, `select freshness_reset_at from targets where target_id = $1`, targetID).Scan(&resetAt); err != nil {
		t.Fatalf("read target %q freshness reset: %v", targetID, err)
	}
	return resetAt
}

func readProbeFreshnessReset(t *testing.T, ctx context.Context, pool *pgxpool.Pool, probeItemID string) time.Time {
	t.Helper()
	var resetAt time.Time
	if err := pool.QueryRow(ctx, `select freshness_reset_at from probe_items where probe_item_id = $1`, probeItemID).Scan(&resetAt); err != nil {
		t.Fatalf("read probe %q freshness reset: %v", probeItemID, err)
	}
	return resetAt
}

func readProbeLiveObservedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, probeItemID string) time.Time {
	t.Helper()
	var observedAt time.Time
	if err := pool.QueryRow(ctx, `select last_live_observed_at from probe_items where probe_item_id = $1`, probeItemID).Scan(&observedAt); err != nil {
		t.Fatalf("read probe %q live observation: %v", probeItemID, err)
	}
	return observedAt
}
