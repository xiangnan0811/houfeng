package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
)

func TestPostgresIntegrationAgentSyncBatchRuntimeACL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	assertAgentSyncBatchPostgresSchemaContract(t, ctx, fixture)
	assertAgentSyncBatchRuntimePrivileges(t, ctx, fixture)

	const (
		monitoringInstanceID = "mi_sync_batch_acl"
		syncBatchID          = "sync_batch_acl"
		syncToken            = "sync-token-acl-fixture"
		fingerprint          = "fingerprint-acl-fixture"
	)
	firstReceivedAt := time.Date(2026, time.August, 30, 3, 30, 0, 0, time.UTC)
	duplicateReceivedAt := firstReceivedAt.Add(time.Minute)
	heartbeatAt := firstReceivedAt.Add(-time.Minute)

	if _, err := fixture.db.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id,
			display_name,
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			binding_status,
			binding_fingerprint,
			sync_token_hash
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		monitoringInstanceID,
		"Sync batch ACL fixture",
		"",
		"",
		"",
		monitoringinstances.LifecyclePendingEnrollment,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound,
		fingerprint,
		hashSyncToken(syncToken),
	); err != nil {
		t.Fatal("seed bound monitoring instance")
	}

	runtimePool := fixture.openDirectRuntimePool(t, ctx, "sync-batch-runtime-acl", 1)
	assertExplicitAgentSyncBatchConflictTargetRejected(t, ctx, runtimePool, monitoringInstanceID)
	repository := NewPostgresSyncRepository(runtimePool)
	receivedAt := firstReceivedAt
	repository.now = func() time.Time { return receivedAt }
	batch := syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   heartbeatAt,
			AgentVersion: "agent/sync-batch-acl",
			Fingerprint:  fingerprint,
			SyncBatchID:  syncBatchID,
		}},
	}

	requireAgentSyncBatchApplySuccess(t, ctx, repository, batch, "first")
	receivedAt = duplicateReceivedAt
	duplicateResult := requireAgentSyncBatchApplySuccess(t, ctx, repository, batch, "duplicate")
	if duplicateResult.Plan.HostSampleFrequencyTier != "" ||
		duplicateResult.Plan.HostSampleMaintenanceContext ||
		duplicateResult.Plan.IPQualityPlan != nil ||
		duplicateResult.Plan.PendingAction != nil ||
		duplicateResult.Plan.ProbeAssignments == nil ||
		len(duplicateResult.Plan.ProbeAssignments) != 0 {
		t.Fatal("duplicate ApplyBatch returned a non-empty or nil-normalized plan")
	}

	var batchCount, heartbeatCount int64
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*) from public.agent_sync_batches where monitoring_instance_id = $1 and sync_batch_id = $2),
			(select count(*) from public.monitoring_instance_heartbeats where monitoring_instance_id = $1 and sync_batch_id = $2)`,
		monitoringInstanceID,
		syncBatchID,
	).Scan(&batchCount, &heartbeatCount); err != nil {
		t.Fatal("read sync batch and heartbeat counts")
	}
	if batchCount != 1 || heartbeatCount != 1 {
		t.Fatalf("persisted row counts = batch:%d heartbeat:%d, want batch:1 heartbeat:1", batchCount, heartbeatCount)
	}

	var storedHeartbeatAt, storedSyncAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select last_heartbeat_at, last_sync_at
		from public.monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&storedHeartbeatAt, &storedSyncAt); err != nil {
		t.Fatal("read monitoring instance sync timestamps")
	}
	if !storedHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("last_heartbeat_at = %s, want %s", storedHeartbeatAt.UTC().Format(time.RFC3339Nano), heartbeatAt.Format(time.RFC3339Nano))
	}
	if !storedSyncAt.Equal(firstReceivedAt) {
		t.Fatalf("last_sync_at = %s, want first receive time %s after duplicate at %s", storedSyncAt.UTC().Format(time.RFC3339Nano), firstReceivedAt.Format(time.RFC3339Nano), duplicateReceivedAt.Format(time.RFC3339Nano))
	}

	assertAgentSyncBatchRuntimePrivileges(t, ctx, fixture)
}

func assertAgentSyncBatchPostgresSchemaContract(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()

	var serverMajor, uniqueIndexCount, expectedPrimaryKeyCount int
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.current_setting('server_version_num')::int / 10000,
			(select count(*)::int
			 from pg_catalog.pg_index index_catalog
			 where index_catalog.indrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and index_catalog.indisunique),
			(select count(*)::int
			 from pg_catalog.pg_constraint constraint_catalog
			 where constraint_catalog.conrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and constraint_catalog.contype = 'p'
			   and pg_catalog.pg_get_constraintdef(constraint_catalog.oid) =
			       'PRIMARY KEY (monitoring_instance_id, sync_batch_id)')`,
	).Scan(&serverMajor, &uniqueIndexCount, &expectedPrimaryKeyCount); err != nil {
		t.Fatal("read agent_sync_batches PostgreSQL schema contract")
	}
	if serverMajor != 16 {
		t.Fatalf("PostgreSQL server major = %d, want 16", serverMajor)
	}
	if uniqueIndexCount != 1 || expectedPrimaryKeyCount != 1 {
		t.Fatalf(
			"agent_sync_batches unique arbiter catalog = indexes:%d expected-primary-key:%d, want indexes:1 expected-primary-key:1",
			uniqueIndexCount,
			expectedPrimaryKeyCount,
		)
	}
}

func assertExplicitAgentSyncBatchConflictTargetRejected(
	t *testing.T,
	ctx context.Context,
	runtimePool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	monitoringInstanceID string,
) {
	t.Helper()

	_, err := runtimePool.Exec(ctx, `
		insert into public.agent_sync_batches (monitoring_instance_id, sync_batch_id)
		values ($1, 'sync_batch_explicit_target_probe')
		on conflict (monitoring_instance_id, sync_batch_id) do nothing`, monitoringInstanceID)
	if err == nil {
		t.Fatal("explicit agent_sync_batches conflict target succeeded under INSERT-only runtime, want SQLSTATE 42501")
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatal("explicit agent_sync_batches conflict target failed without PostgreSQL typed cause")
	}
	if postgresError.Code != "42501" {
		t.Fatalf("explicit agent_sync_batches conflict target SQLSTATE = %s, want 42501", postgresError.Code)
	}
}

func assertAgentSyncBatchRuntimePrivileges(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()

	var insertPrivilege, selectPrivilege, updatePrivilege, deletePrivilege bool
	var runtimeColumnACLCount int
	if err := fixture.db.QueryRow(ctx, `
		select
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'INSERT'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'SELECT'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'UPDATE'),
			pg_catalog.has_table_privilege($1::name, 'public.agent_sync_batches', 'DELETE'),
			(select count(*)::int
			 from pg_catalog.pg_attribute attribute
			 cross join lateral pg_catalog.aclexplode(attribute.attacl) acl_entry
			 where attribute.attrelid = 'public.agent_sync_batches'::pg_catalog.regclass
			   and attribute.attnum > 0
			   and not attribute.attisdropped
			   and acl_entry.grantee = (
			       select role.oid from pg_catalog.pg_roles role where role.rolname = $1::name
			   ))`,
		fixture.runtime,
	).Scan(&insertPrivilege, &selectPrivilege, &updatePrivilege, &deletePrivilege, &runtimeColumnACLCount); err != nil {
		t.Fatal("read agent_sync_batches runtime privilege vector")
	}
	if !insertPrivilege || selectPrivilege || updatePrivilege || deletePrivilege || runtimeColumnACLCount != 0 {
		t.Fatalf(
			"agent_sync_batches runtime privileges = insert:%t select:%t update:%t delete:%t column-acl-entries:%d, want insert:true select:false update:false delete:false column-acl-entries:0",
			insertPrivilege,
			selectPrivilege,
			updatePrivilege,
			deletePrivilege,
			runtimeColumnACLCount,
		)
	}
}

func requireAgentSyncBatchApplySuccess(
	t *testing.T,
	ctx context.Context,
	repository *PostgresSyncRepository,
	batch syncing.Batch,
	phase string,
) syncing.Result {
	t.Helper()

	result, err := repository.ApplyBatch(ctx, batch)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf("%s ApplyBatch PostgreSQL SQLSTATE = %s, want success", phase, postgresError.Code)
		}
		t.Fatalf("%s ApplyBatch failed without PostgreSQL typed cause", phase)
	}
	return result
}
