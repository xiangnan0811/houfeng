package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresSyncRepositoryRollsBackHeartbeatWhenObservationWriteFails(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
		execErrForSQLSubstring:          "insert into probe_observations",
		execErr:                         errors.New("probe write boom"),
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err == nil {
		t.Fatal("ApplyBatch() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "insert probe observation") {
		t.Fatalf("ApplyBatch() error = %q, want insert probe observation context", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on observation write failure")
	}
	if !containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") {
		t.Fatal("expected heartbeat insert before probe write failure")
	}
	if containsSQL(tx.execSQL, "update monitoring_instances") {
		t.Fatal("monitoringInstance sync state should not update when probe write fails")
	}
}

func TestPostgresSyncRepositoryRejectsProbeMetadataMismatchBeforeWritingBatch(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_wrong", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if !errors.Is(err, observations.ErrInvalidProbeObservation) {
		t.Fatalf("ApplyBatch() error = %v, want ErrInvalidProbeObservation", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on probe metadata mismatch")
	}
	if containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") {
		t.Fatal("probe metadata mismatch should fail before writing heartbeats")
	}
}

func TestPostgresSyncRepositoryRejectsInvalidProbeObservationSemanticsBeforeWritingBatch(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Observations.ProbeObservations[0].ResultKind = "maybe"

	_, err := repo.ApplyBatch(context.Background(), batch)
	if !errors.Is(err, observations.ErrInvalidProbeObservation) {
		t.Fatalf("ApplyBatch() error = %v, want ErrInvalidProbeObservation", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls == 0 {
		t.Fatal("rollbackCalls = 0, want rollback on invalid probe observation semantics")
	}
	if containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") {
		t.Fatal("invalid probe observation semantics should fail before writing heartbeats")
	}
}

func TestPostgresSyncRepositoryRejectsObservationBatchWithoutHeartbeatCarrier(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Heartbeats = nil

	_, err := repo.ApplyBatch(context.Background(), batch)
	if !errors.Is(err, syncing.ErrHeartbeatRequired) {
		t.Fatalf("ApplyBatch() error = %v, want ErrHeartbeatRequired", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls != 0 {
		t.Fatalf("rollbackCalls = %d, want 0", tx.rollbackCalls)
	}
	if len(tx.execSQL) != 0 {
		t.Fatalf("len(execSQL) = %d, want 0", len(tx.execSQL))
	}
}

func TestSyncBatchSourceUsesConstantTimeSyncTokenHashCompare(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("sync_batches.go")
	if err != nil {
		t.Fatalf("ReadFile(sync_batches.go) error = %v", err)
	}

	validationPath := sourceBetween(t, string(source), "func (r *PostgresSyncRepository) validateAcceptedSyncBatch", "func lifecycleStatusAfterAcceptedSync")
	if !strings.Contains(validationPath, "r.tokenHasher.syncTokenMatches(storedSyncTokenHash, batch.SyncToken)") {
		t.Fatal("validateAcceptedSyncBatch() should compare sync token hashes with the shared versioned token hasher")
	}
	if strings.Contains(validationPath, "storedSyncTokenHash != hashSyncToken(batch.SyncToken)") {
		t.Fatal("validateAcceptedSyncBatch() should not use plain string inequality for sync token hashes")
	}
}

func TestPostgresSyncRepositoryMigratesLegacySyncTokenHashAfterSuccessfulValidation(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashOpaqueToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatch()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("sync_token_hash = $2")
	if len(args) != 2 || args[0] != "mi_001" {
		t.Fatalf("sync token migration args = %#v, want monitoring instance id and hash", args)
	}
	migratedHash, ok := args[1].(string)
	if !ok || !isHMACAgentTokenHash(migratedHash) {
		t.Fatalf("sync token migration hash = %#v, want versioned hmac hash", args[1])
	}
	if migratedHash != hashSyncToken("sync-token-001") {
		t.Fatalf("sync token migration hash = %q, want current sync token hash", migratedHash)
	}
}

func TestPostgresSyncRepositoryRecordsBatchIDBeforeWritingFacts(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatch()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	recordIndex := sqlIndex(tx.execSQL, "insert into agent_sync_batches")
	heartbeatIndex := sqlIndex(tx.execSQL, "insert into monitoring_instance_heartbeats")
	if recordIndex == -1 {
		t.Fatalf("agent sync batch id was not recorded; execSQL=%#v", tx.execSQL)
	}
	if heartbeatIndex == -1 {
		t.Fatalf("heartbeat insert missing; execSQL=%#v", tx.execSQL)
	}
	if recordIndex > heartbeatIndex {
		t.Fatalf("agent sync batch id insert index %d should run before heartbeat insert index %d", recordIndex, heartbeatIndex)
	}
	args := tx.argsForSQL("insert into agent_sync_batches")
	if len(args) != 2 || args[0] != "mi_001" || args[1] != "sync_001" {
		t.Fatalf("agent sync batch insert args = %#v, want monitoring instance id and sync batch id", args)
	}
	batchSQL := strings.ToLower(tx.execSQL[recordIndex])
	if !strings.Contains(batchSQL, "on conflict do nothing") {
		t.Fatal("agent sync batch SQL must use targetless ON CONFLICT DO NOTHING")
	}
	if strings.Contains(batchSQL, "on conflict (") {
		t.Fatal("agent sync batch SQL must not name conflict columns under INSERT-only ACL")
	}
}

func TestPostgresSyncRepositoryPreservesAgentSyncBatchPostgresCause(t *testing.T) {
	t.Parallel()

	wantPostgresError := &pgconn.PgError{Code: "42501"}
	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
		execErrForSQLSubstring:          "insert into agent_sync_batches",
		execErr:                         wantPostgresError,
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	var gotPostgresError *pgconn.PgError
	if !errors.As(err, &gotPostgresError) || gotPostgresError != wantPostgresError {
		t.Fatal("ApplyBatch() did not preserve the agent sync batch PostgreSQL typed cause")
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") {
		t.Fatal("agent sync batch insert failure wrote heartbeat facts")
	}
}

func TestPostgresSyncRepositoryDuplicateBatchCommitsWithoutRewritingFacts(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		duplicateSyncBatch:              true,
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	result, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if result.Disposition != syncing.ResultDispositionExactDuplicate {
		t.Fatalf("Disposition = %q, want exact duplicate", result.Disposition)
	}
	if result.AcceptedAt.IsZero() {
		t.Fatal("AcceptedAt is zero, want accepted duplicate sync")
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1 for idempotent duplicate", tx.commitCalls)
	}
	if containsSQL(tx.execSQL, "insert into monitoring_instance_heartbeats") ||
		containsSQL(tx.execSQL, "insert into host_samples") ||
		containsSQL(tx.execSQL, "insert into probe_observations") ||
		containsSQL(tx.execSQL, "insert into ip_quality_reports") ||
		containsSQL(tx.execSQL, "last_heartbeat_at = greatest") {
		t.Fatalf("duplicate sync batch rewrote facts; execSQL=%#v", tx.execSQL)
	}
	if result.Plan.HostSampleFrequencyTier != "" ||
		result.Plan.HostSampleMaintenanceContext ||
		result.Plan.IPQualityPlan != nil ||
		result.Plan.PendingAction != nil ||
		result.Plan.ProbeAssignments == nil ||
		len(result.Plan.ProbeAssignments) != 0 {
		t.Fatal("duplicate sync batch should return an exact empty plan with a non-nil probe assignment slice")
	}
}

func TestSyncBatchPromotesPendingEnrollmentLifecycleAfterHostSample(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		monitoringInstanceLifecycle:     monitoringinstances.LifecyclePendingEnrollment,
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatchWithHostSample()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("last_heartbeat_at = greatest")
	if len(args) != 4 {
		t.Fatalf("sync state args = %#v, want monitoringInstance id, heartbeat, sync time, lifecycle", args)
	}
	if got := args[3]; got != monitoringinstances.LifecycleInUse {
		t.Fatalf("lifecycle update arg = %#v, want %q", got, monitoringinstances.LifecycleInUse)
	}
}

func TestSyncBatchKeepsPendingEnrollmentLifecycleWithoutHostSample(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		monitoringInstanceLifecycle:     monitoringinstances.LifecyclePendingEnrollment,
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatch()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("last_heartbeat_at = greatest")
	if len(args) != 4 {
		t.Fatalf("sync state args = %#v, want monitoringInstance id, heartbeat, sync time, lifecycle", args)
	}
	if got := args[3]; got != monitoringinstances.LifecyclePendingEnrollment {
		t.Fatalf("lifecycle update arg = %#v, want %q", got, monitoringinstances.LifecyclePendingEnrollment)
	}
}

func TestSyncBatchRecordsIPQualityReportsInSameTransaction(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
		newIPQualityReportID: func() (string, error) {
			return "ipq_001", nil
		},
	}
	batch := testSyncBatch()
	batch.IPQualityReports = []ipquality.ReportWrite{ipQualityReportWrite()}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_reports") {
		t.Fatalf("execSQL = %#v, want ip quality report insert", tx.execSQL)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_provider_results") {
		t.Fatalf("execSQL = %#v, want provider result insert", tx.execSQL)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_service_unlocks") {
		t.Fatalf("execSQL = %#v, want service unlock insert", tx.execSQL)
	}
	args := tx.argsForSQL("insert into ip_quality_reports")
	if args[0] != "ipq_001" {
		t.Fatalf("report_id arg = %#v, want ipq_001", args[0])
	}
	if args[1] != "mi_001" {
		t.Fatalf("monitoring_instance_id arg = %#v, want mi_001", args[1])
	}
	if args[6] != "sync_001" {
		t.Fatalf("sync_batch_id tracking arg = %#v, want sync_001", args[6])
	}
}

func TestSyncBatchDoesNotOverrideNonPendingLifecycleAfterHostSample(t *testing.T) {
	t.Parallel()

	tests := []string{
		monitoringinstances.LifecycleInUse,
		monitoringinstances.LifecycleObserving,
		monitoringinstances.LifecycleNoRenewal,
	}

	for _, lifecycle := range tests {
		lifecycle := lifecycle
		t.Run(lifecycle, func(t *testing.T) {
			t.Parallel()

			tx := &fakeSyncBatchTx{
				monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
				monitoringInstanceFingerprint:   "fp-001",
				monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
				monitoringInstanceLifecycle:     lifecycle,
				probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
			}
			repo := &PostgresSyncRepository{
				beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
					return tx, nil
				},
			}

			if _, err := repo.ApplyBatch(context.Background(), testSyncBatchWithHostSample()); err != nil {
				t.Fatalf("ApplyBatch() error = %v", err)
			}

			args := tx.argsForSQL("last_heartbeat_at = greatest")
			if len(args) != 4 {
				t.Fatalf("sync state args = %#v, want monitoringInstance id, heartbeat, sync time, lifecycle", args)
			}
			if got := args[3]; got != lifecycle {
				t.Fatalf("lifecycle update arg = %#v, want %q", got, lifecycle)
			}
		})
	}
}

func TestSyncBatchShortCircuitsSuppressedMonitoringInstanceWithoutWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		lifecycleStatus  string
		monitoringStatus string
		archived         bool
	}{
		{
			name:             "paused",
			lifecycleStatus:  monitoringinstances.LifecycleInUse,
			monitoringStatus: monitoringinstances.MonitoringPaused,
		},
		{
			name:             "retired",
			lifecycleStatus:  monitoringinstances.LifecycleRetired,
			monitoringStatus: monitoringinstances.MonitoringEnabled,
		},
		{
			name:             "archived",
			lifecycleStatus:  monitoringinstances.LifecycleRetired,
			monitoringStatus: monitoringinstances.MonitoringPaused,
			archived:         true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &fakeSyncBatchTx{
				monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
				monitoringInstanceFingerprint:   "fp-001",
				monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
				monitoringInstanceLifecycle:     tt.lifecycleStatus,
				monitoringInstanceMonitoring:    tt.monitoringStatus,
				monitoringInstanceArchived:      tt.archived,
				pendingActionID:                 "act_001",
				pendingCommandID:                "uptime",
				probeMetadataByItemID:           map[string]observations.ProbeMetadata{"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP}},
			}
			repo := &PostgresSyncRepository{
				beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
					return tx, nil
				},
				newIPQualityReportID: func() (string, error) {
					return "ipq_001", nil
				},
			}

			batch := testSyncBatchWithHostSample()
			batch.IPQualityReports = []ipquality.ReportWrite{ipQualityReportWrite()}
			batch.CommandResults = []syncing.CommandResult{{ActionID: "act_001", CommandID: "uptime", Stdout: "up", ExitCode: 0}}

			result, err := repo.ApplyBatch(context.Background(), batch)
			if err != nil {
				t.Fatalf("ApplyBatch() error = %v", err)
			}
			if result.AcceptedAt.IsZero() {
				t.Fatal("AcceptedAt is zero, want accepted short-circuit sync")
			}
			if result.Disposition != syncing.ResultDispositionSuppressed {
				t.Fatalf("Disposition = %q, want suppressed", result.Disposition)
			}
			if result.Plan.HostSampleFrequencyTier != "" || result.Plan.HostSampleMaintenanceContext || result.Plan.IPQualityPlan != nil || result.Plan.PendingAction != nil || len(result.Plan.ProbeAssignments) != 0 {
				t.Fatalf("Plan = %#v, want empty plan", result.Plan)
			}
			if tx.commitCalls != 1 {
				t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
			}
			for _, blocked := range []string{
				"insert into monitoring_instance_heartbeats",
				"insert into host_samples",
				"insert into probe_observations",
				"insert into ip_quality_reports",
				"last_heartbeat_at = greatest",
				"last_action->>'status'",
				"pending_action_id = NULL",
			} {
				if containsSQL(tx.execSQL, blocked) {
					t.Fatalf("execSQL contains %q for suppressed sync: %#v", blocked, tx.execSQL)
				}
			}
		})
	}
}

func testSyncBatch() syncing.Batch {
	observedAt := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	return syncing.Batch{
		MonitoringInstanceID: "mi_001",
		SyncToken:            "sync-token-001",
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   observedAt,
			AgentVersion: "agent/v0.1.0",
			Fingerprint:  "fp-001",
			SyncBatchID:  "sync_001",
		}},
		Observations: observations.BatchWrite{
			MonitoringInstanceID: "mi_001",
			ProbeObservations: []observations.ProbeObservationWrite{{
				MonitoringInstanceID: "mi_001",
				TargetID:             "tg_001",
				ProbeItemID:          "pb_001",
				ProbeKind:            agentapi.ProbeKindHTTP,
				ObservedAt:           observedAt,
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-001",
				ResultKind:           agentapi.ProbeResultSuccess,
				SyncBatchID:          "sync_001",
			}},
		},
	}
}

func testSyncBatchWithHostSample() syncing.Batch {
	batch := testSyncBatch()
	observedAt := batch.Heartbeats[0].ObservedAt
	batch.Observations.HostSamples = []observations.HostSampleWrite{{
		MonitoringInstanceID: "mi_001",
		ObservedAt:           observedAt,
		AgentVersion:         "agent/v0.1.0",
		Fingerprint:          "fp-001",
		CPUUsagePct:          42,
		Load5:                0.8,
		MemUsedPct:           64,
		DiskUsedPct:          51,
		InodeUsedPct:         18,
		NetInBytesPerSec:     1024,
		NetOutBytesPerSec:    2048,
		SyncBatchID:          "sync_001",
	}}
	return batch
}

type fakeSyncBatchTx struct {
	monitoringInstanceBindingStatus string
	monitoringInstanceFingerprint   string
	monitoringInstanceSyncTokenHash string
	monitoringInstanceLifecycle     string
	monitoringInstanceMonitoring    string
	monitoringInstanceArchived      bool
	monitoringInstanceLabels        []string
	pendingActionID                 string
	pendingCommandID                string
	pendingLastActionRaw            []byte
	duplicateSyncBatch              bool

	probeMetadataByItemID map[string]observations.ProbeMetadata
	probeMetadataErr      map[string]error
	planRows              []fakeAgentPlanScan

	execErrForSQLSubstring    string
	execErr                   error
	commandResultRowsAffected *int
	execSQL                   []string
	execArgs                  [][]any
	commitCalls               int
	rollbackCalls             int
}

func (f *fakeSyncBatchTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	if f.execErr != nil && strings.Contains(sql, f.execErrForSQLSubstring) {
		return pgconn.CommandTag{}, f.execErr
	}
	lowerSQL := strings.ToLower(sql)
	if strings.Contains(lowerSQL, "insert into agent_sync_batches") {
		if f.duplicateSyncBatch {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(lowerSQL, "insert into monitoring_instance_command_action_audit") {
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(lowerSQL, "last_action->>'status'") {
		if f.commandResultRowsAffected != nil && *f.commandResultRowsAffected == 0 {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if strings.Contains(lowerSQL, "update monitoring_instances") {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeSyncBatchTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "select labels"):
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(*[]string)) = append([]string(nil), f.monitoringInstanceLabels...)
			return nil
		}}
	case strings.Contains(sql, "pending_action_id"):
		if f.pendingActionID == "" {
			return fakeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		actionID := f.pendingActionID
		commandID := f.pendingCommandID
		lastActionRaw := append([]byte(nil), f.pendingLastActionRaw...)
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(**string)) = &actionID
			*(dest[1].(**string)) = &commandID
			if len(dest) > 2 {
				*(dest[2].(*[]byte)) = lastActionRaw
			}
			return nil
		}}
	case strings.Contains(sql, "from monitoring_instances"):
		return fakeRow{scan: func(dest ...any) error {
			lifecycle := f.monitoringInstanceLifecycle
			if lifecycle == "" {
				lifecycle = monitoringinstances.LifecycleInUse
			}
			monitoringStatus := f.monitoringInstanceMonitoring
			if monitoringStatus == "" {
				monitoringStatus = monitoringinstances.MonitoringEnabled
			}
			*(dest[0].(*string)) = f.monitoringInstanceBindingStatus
			*(dest[1].(*string)) = f.monitoringInstanceFingerprint
			*(dest[2].(*string)) = f.monitoringInstanceSyncTokenHash
			*(dest[3].(*string)) = lifecycle
			if len(dest) > 4 {
				*(dest[4].(*string)) = monitoringStatus
			}
			if len(dest) > 5 {
				*(dest[5].(*bool)) = f.monitoringInstanceArchived
			}
			return nil
		}}
	case strings.Contains(sql, "from probe_items"):
		probeItemID, _ := args[0].(string)
		if err := f.probeMetadataErr[probeItemID]; err != nil {
			return fakeRow{scan: func(dest ...any) error { return err }}
		}
		metadata, ok := f.probeMetadataByItemID[probeItemID]
		if !ok {
			return fakeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		return fakeRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = metadata.TargetID
			*(dest[1].(*string)) = metadata.ProbeKind
			return nil
		}}
	default:
		return fakeRow{scan: func(dest ...any) error { return errors.New("unexpected query") }}
	}
}

func (f *fakeSyncBatchTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "from targets t") {
		return &fakeAgentPlanRows{rows: f.planRows}, nil
	}
	return nil, errors.New("unexpected query")
}

func (f *fakeSyncBatchTx) Commit(context.Context) error {
	f.commitCalls++
	return nil
}

func (f *fakeSyncBatchTx) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

func TestPostgresSyncRepositoryPersistsBackfilledHeartbeatFlag(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		monitoringInstanceLabels:        []string{"核心", "edge"},
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		planRows: []fakeAgentPlanScan{{
			scan: func(dest ...any) error {
				*(dest[0].(*string)) = "tg_001"
				*(dest[1].(*string)) = "api.example.test"
				port := 443
				*(dest[2].(**int)) = &port
				*(dest[3].(*string)) = "启用"
				*(dest[4].(*string)) = "pb_001"
				*(dest[5].(*string)) = agentapi.ProbeKindHTTP
				*(dest[6].(*string)) = agentapi.FrequencyTier1m
				*(dest[7].(*int)) = 5
				*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
				return nil
			},
		}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.Heartbeats[0].IsBackfilled = true

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	heartbeatArgs := tx.argsForSQL("insert into monitoring_instance_heartbeats")
	if got, ok := heartbeatArgs[6].(bool); !ok || !got {
		t.Fatalf("heartbeat is_backfilled arg = %#v, want true", heartbeatArgs[6])
	}
}

func TestSyncBatchPlanReturnsAcceptedAtAndDerivedPlan(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		monitoringInstanceLabels:        []string{"核心", "edge"},
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		planRows: []fakeAgentPlanScan{{
			scan: func(dest ...any) error {
				*(dest[0].(*string)) = "tg_001"
				*(dest[1].(*string)) = "api.example.test"
				port := 443
				*(dest[2].(**int)) = &port
				*(dest[3].(*string)) = "启用"
				*(dest[4].(*string)) = "pb_001"
				*(dest[5].(*string)) = agentapi.ProbeKindHTTP
				*(dest[6].(*string)) = agentapi.FrequencyTier1m
				*(dest[7].(*int)) = 5
				*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
				return nil
			},
		}},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	result, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if result.AcceptedAt.IsZero() {
		t.Fatal("AcceptedAt is zero, want non-zero")
	}
	if result.Disposition != syncing.ResultDispositionRecorded {
		t.Fatalf("Disposition = %q, want recorded", result.Disposition)
	}
	if result.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier5s {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", result.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier5s)
	}
	if len(result.Plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(result.Plan.ProbeAssignments))
	}
	if result.Plan.ProbeAssignments[0].TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", result.Plan.ProbeAssignments[0].TargetID, "tg_001")
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
	resultIndex := sqlIndex(tx.execSQL, "last_action->>'status'")
	auditIndex := sqlIndexAfter(tx.execSQL, "insert into monitoring_instance_command_action_audit", resultIndex)
	if auditIndex != -1 && strings.Contains(tx.execSQL[auditIndex], "'completed'") {
		t.Fatalf("stale command result wrote completion audit: %#v", tx.execSQL)
	}
}

func TestSyncBatchDispatchesPendingActionAsDurableLastAction(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:                 "act_001",
		pendingCommandID:                "uptime",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	result, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if result.Plan.PendingAction == nil {
		t.Fatal("PendingAction = nil, want dispatched action")
	}
	if result.Plan.PendingAction.ActionID != "act_001" || result.Plan.PendingAction.CommandID != "uptime" {
		t.Fatalf("PendingAction = %#v, want act_001/uptime", result.Plan.PendingAction)
	}

	args := tx.argsForSQL("pending_action_id = NULL")
	if len(args) != 4 {
		t.Fatalf("dispatch args = %#v, want monitoringInstance id, payload, action id, command id", args)
	}
	payload, ok := args[1].([]byte)
	if !ok {
		t.Fatalf("pending payload arg = %#v, want []byte JSON", args[1])
	}
	for _, want := range []string{`"action_id":"act_001"`, `"command_id":"uptime"`, `"status":"pending"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("pending last_action = %s, missing %s", payload, want)
		}
	}
	if args[2] != "act_001" || args[3] != "uptime" {
		t.Fatalf("dispatch guard args = %#v, want act_001/uptime", args[2:4])
	}
}

func TestSyncBatchDispatchPreservesQueuedPendingActionMetadata(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:                 "act_sensitive",
		pendingCommandID:                "systemctl_status",
		pendingLastActionRaw:            []byte(`{"action_id":"act_sensitive","command_id":"systemctl_status","status":"pending","sensitivity":"sensitive","queued_at":"2026-06-26T11:00:00Z"}`),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
		now: func() time.Time {
			return time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatch()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("pending_action_id = NULL")
	if len(args) != 4 {
		t.Fatalf("dispatch args = %#v, want monitoringInstance id, payload, action id, command id", args)
	}
	payload, ok := args[1].([]byte)
	if !ok {
		t.Fatalf("pending payload arg = %#v, want []byte JSON", args[1])
	}
	for _, want := range []string{
		`"action_id":"act_sensitive"`,
		`"command_id":"systemctl_status"`,
		`"status":"pending"`,
		`"sensitivity":"sensitive"`,
		`"queued_at":"2026-06-26T11:00:00Z"`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("pending last_action = %s, missing %s", payload, want)
		}
	}
}

func TestSyncBatchDispatchesPendingActionWritesAudit(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:                 "act_001",
		pendingCommandID:                "systemctl_status",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
		now: func() time.Time {
			return time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
		},
	}

	if _, err := repo.ApplyBatch(context.Background(), testSyncBatch()); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	dispatchIndex := sqlIndex(tx.execSQL, "pending_action_id = NULL")
	auditIndex := sqlIndex(tx.execSQL, "insert into monitoring_instance_command_action_audit")
	if dispatchIndex == -1 || auditIndex == -1 {
		t.Fatalf("execSQL = %#v, want dispatch update and audit insert", tx.execSQL)
	}
	if auditIndex < dispatchIndex {
		t.Fatalf("dispatch audit index %d should run after dispatch update index %d", auditIndex, dispatchIndex)
	}
	auditArgs := tx.execArgs[auditIndex]
	if len(auditArgs) != 9 {
		t.Fatalf("audit args = %#v, want dispatch metadata", auditArgs)
	}
	if !strings.Contains(tx.execSQL[auditIndex], "'dispatched'") {
		t.Fatalf("audit SQL = %q, want dispatched event type", tx.execSQL[auditIndex])
	}
	if auditArgs[1] != "act_001" || auditArgs[2] != "mi_001" || auditArgs[3] != "systemctl_status" || auditArgs[4] != "sensitive" || auditArgs[5] != "" || auditArgs[6] != monitoringinstances.CommandActionSourceAgentSync || auditArgs[7] != nil {
		t.Fatalf("audit args = %#v, want dispatched metadata", auditArgs)
	}
	if !auditArgs[8].(time.Time).Equal(time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("audit occurred_at = %v, want sync timestamp", auditArgs[8])
	}
}

func TestSyncBatchRollsBackDispatchWhenAuditFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("dispatch audit failed")
	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:                 "act_001",
		pendingCommandID:                "uptime",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		execErrForSQLSubstring: "insert into monitoring_instance_command_action_audit",
		execErr:                wantErr,
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	_, err := repo.ApplyBatch(context.Background(), testSyncBatch())
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyBatch() error = %v, want wrapped dispatch audit error", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("rollbackCalls = %d, want 1", tx.rollbackCalls)
	}
	assertSQLOrder(t, tx.execSQL,
		"pending_action_id = NULL",
		"insert into monitoring_instance_command_action_audit",
	)
}

func TestSyncBatchStoresMatchingCommandResultWithCommandID(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("last_action->>'status'")
	if len(args) != 5 {
		t.Fatalf("result args = %#v, want payload, monitoringInstance id, status, action id, command id", args)
	}
	payload, ok := args[0].([]byte)
	if !ok {
		t.Fatalf("result payload arg = %#v, want []byte JSON", args[0])
	}
	for _, want := range []string{`"action_id":"act_001"`, `"command_id":"uptime"`, `"status":"done"`, `"stdout":"up 1 day"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("result last_action = %s, missing %s", payload, want)
		}
	}
	if args[1] != "mi_001" || args[2] != commandActionStatusPending || args[3] != "act_001" || args[4] != "uptime" {
		t.Fatalf("result guard args = %#v, want monitoringInstance/status/action/command guard", args[1:5])
	}
}

func TestSyncBatchStoresCommandResultOutputTTLMetadata(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
		now: func() time.Time {
			return time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	args := tx.argsForSQL("last_action->>'status'")
	payload, ok := args[0].([]byte)
	if !ok {
		t.Fatalf("result payload arg = %#v, want []byte JSON", args[0])
	}
	for _, want := range []string{
		`"completed_at":"2026-06-26T12:00:00Z"`,
		`"output_expires_at":"2026-06-27T12:00:00Z"`,
		`"output_expired":false`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("result last_action = %s, missing %s", payload, want)
		}
	}
}

func TestSyncBatchStoresMatchingCommandResultWritesMetadataOnlyCompletionAudit(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
		now: func() time.Time {
			return time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		Stderr:    "diagnostic",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	resultIndex := sqlIndex(tx.execSQL, "last_action->>'status'")
	auditIndex := sqlIndexAfter(tx.execSQL, "insert into monitoring_instance_command_action_audit", resultIndex)
	if resultIndex == -1 || auditIndex == -1 {
		t.Fatalf("execSQL = %#v, want result update and completion audit", tx.execSQL)
	}
	if !strings.Contains(tx.execSQL[auditIndex], "'completed'") {
		t.Fatalf("audit SQL = %q, want completed event type", tx.execSQL[auditIndex])
	}
	auditArgs := tx.execArgs[auditIndex]
	if len(auditArgs) != 9 {
		t.Fatalf("audit args = %#v, want completion metadata", auditArgs)
	}
	if auditArgs[1] != "act_001" || auditArgs[2] != "mi_001" || auditArgs[3] != "uptime" || auditArgs[4] != "standard" || auditArgs[5] != "" || auditArgs[6] != monitoringinstances.CommandActionSourceAgentSync || auditArgs[7] != 0 {
		t.Fatalf("audit args = %#v, want completion metadata", auditArgs)
	}
	if !auditArgs[8].(time.Time).Equal(time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("audit occurred_at = %v, want sync timestamp", auditArgs[8])
	}
	sql := strings.ToLower(tx.execSQL[auditIndex])
	if strings.Contains(sql, "stdout") || strings.Contains(sql, "stderr") {
		t.Fatalf("completion audit SQL must not store stdout/stderr: %s", tx.execSQL[auditIndex])
	}
}

func TestSyncBatchRollsBackCompletionWhenAuditFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("completion audit failed")
	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
		execErrForSQLSubstring: "insert into monitoring_instance_command_action_audit",
		execErr:                wantErr,
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}
	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	_, err := repo.ApplyBatch(context.Background(), batch)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyBatch() error = %v, want wrapped completion audit error", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("rollbackCalls = %d, want 1", tx.rollbackCalls)
	}
	assertSQLOrder(t, tx.execSQL,
		"last_action->>'status'",
		"insert into monitoring_instance_command_action_audit",
	)
}

func TestSyncBatchRedactsCommandResultsBeforePersisting(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}
	batch := testSyncBatch()
	batch.Observations.HostSamples = nil
	batch.Observations.ProbeObservations = nil
	batch.IPQualityReports = nil
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_001",
		CommandID: "uptime",
		Stdout:    "token=stdout-secret",
		Stderr:    "Authorization: Bearer stderr-secret",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	args := tx.argsForSQL("last_action->>'status'")
	if len(args) == 0 {
		t.Fatal("command result update not executed")
	}
	payload := string(args[0].([]byte))
	for _, leaked := range []string{"stdout-secret", "stderr-secret"} {
		if strings.Contains(payload, leaked) {
			t.Fatalf("last_action leaked %q: %s", leaked, payload)
		}
	}
	if !strings.Contains(payload, "[redacted]") {
		t.Fatalf("last_action = %s, want redaction marker", payload)
	}
}

func TestSyncBatchIgnoresMismatchedCommandResultRows(t *testing.T) {
	t.Parallel()

	zeroRows := 0
	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		commandResultRowsAffected:       &zeroRows,
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_stale",
		CommandID: "uptime",
		Stdout:    "stale",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v, want mismatch to be ignored", err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
}

func TestSyncBatchStoresCommandResultBeforeDispatchingQueuedAction(t *testing.T) {
	t.Parallel()

	tx := &fakeSyncBatchTx{
		monitoringInstanceBindingStatus: agentapi.BindingStatusBound,
		monitoringInstanceFingerprint:   "fp-001",
		monitoringInstanceSyncTokenHash: hashSyncToken("sync-token-001"),
		pendingActionID:                 "act_next",
		pendingCommandID:                "df_h",
		probeMetadataByItemID: map[string]observations.ProbeMetadata{
			"pb_001": {TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP},
		},
	}
	repo := &PostgresSyncRepository{
		beginTx: func(context.Context, pgx.TxOptions) (syncBatchTx, error) {
			return tx, nil
		},
	}

	batch := testSyncBatch()
	batch.CommandResults = []syncing.CommandResult{{
		ActionID:  "act_prev",
		CommandID: "uptime",
		Stdout:    "up 1 day",
		ExitCode:  0,
	}}

	if _, err := repo.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}

	resultIndex := sqlIndex(tx.execSQL, "last_action->>'status'")
	dispatchIndex := sqlIndex(tx.execSQL, "pending_action_id = NULL")
	if resultIndex == -1 || dispatchIndex == -1 {
		t.Fatalf("exec SQL missing result or dispatch update: %#v", tx.execSQL)
	}
	if resultIndex > dispatchIndex {
		t.Fatalf("command result update index %d should run before dispatch index %d", resultIndex, dispatchIndex)
	}
}

type fakeRow struct {
	scan func(dest ...any) error
}

func (f fakeRow) Scan(dest ...any) error {
	return f.scan(dest...)
}

func containsSQL(sqls []string, want string) bool {
	return sqlIndex(sqls, want) != -1
}

func sqlIndex(sqls []string, want string) int {
	for i, sql := range sqls {
		if strings.Contains(sql, want) {
			return i
		}
	}
	return -1
}

func sqlIndexAfter(sqls []string, want string, after int) int {
	if after < -1 {
		after = -1
	}
	for i := after + 1; i < len(sqls); i++ {
		if strings.Contains(sqls[i], want) {
			return i
		}
	}
	return -1
}

func (f *fakeSyncBatchTx) argsForSQL(want string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, want) {
			return f.execArgs[i]
		}
	}
	return nil
}
