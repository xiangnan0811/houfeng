package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationVPSLifecycleSchema(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into vps_assets(vps_id,display_name,lifecycle_status) values ('vps_lifecycle_one','One','active'),('vps_lifecycle_two','Two','active')`)
	const insertMI = `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status) values ($1,$2,'Agent','','','','待接入')`
	execLifecycleSchemaSQL(t, ctx, runtime, insertMI, "mi_lifecycle_one", "vps_lifecycle_one")
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into vps_monitoring_instance_links(link_id,vps_id,monitoring_instance_id) values ('link_owner','vps_lifecycle_one','mi_lifecycle_one')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23503", `insert into vps_monitoring_instance_links(link_id,vps_id,monitoring_instance_id) values ('link_wrong_owner','vps_lifecycle_two','mi_lifecycle_one')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23505", insertMI, "mi_lifecycle_duplicate", "vps_lifecycle_one")
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23502", insertMI, "mi_lifecycle_orphan", nil)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23514", `update monitoring_instances set vps_id='vps_lifecycle_two' where monitoring_instance_id='mi_lifecycle_one'`)
	execLifecycleSchemaSQL(t, ctx, runtime, `update monitoring_instances set lifecycle_status='已退役' where monitoring_instance_id='mi_lifecycle_one'`)
	execLifecycleSchemaSQL(t, ctx, runtime, insertMI, "mi_lifecycle_replacement", "vps_lifecycle_one")
	for _, sql := range []string{
		`update vps_assets set lifecycle_status='cancelled' where vps_id='vps_lifecycle_one'`,
		`update vps_assets set renewal_decision='observe' where vps_id='vps_lifecycle_one'`,
		`update vps_assets set validity_mode='fixed' where vps_id='vps_lifecycle_one'`,
		`update vps_assets set expires_at='2030-01-01' where vps_id='vps_lifecycle_one'`,
		`update vps_assets set auto_renew_check='disabled' where vps_id='vps_lifecycle_one'`,
		`update monitoring_instances set lifecycle_status='在用' where monitoring_instance_id='mi_lifecycle_replacement'`,
	} {
		assertLifecycleSchemaSQLState(t, ctx, runtime, "23514", sql)
	}
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash) values ('session_lifecycle','mi_lifecycle_one','hash_lifecycle')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23514", `update monitoring_agent_sessions set monitoring_instance_id='mi_lifecycle_replacement' where session_id='session_lifecycle'`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23505", `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash) values ('session_duplicate','mi_lifecycle_one','hash_duplicate')`)
	execLifecycleSchemaSQL(t, ctx, runtime, `update monitoring_agent_sessions set capability='evidence_only' where session_id='session_lifecycle'`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23514", `update monitoring_agent_sessions set capability='full' where session_id='session_lifecycle'`)
	waitingTx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waitingTx.Rollback(ctx) }()
	if _, err := waitingTx.Exec(ctx, `select pg_sleep(0.01)`); err != nil {
		t.Fatal(err)
	}
	var issuedAfterWait bool
	if err := waitingTx.QueryRow(ctx, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash) values ('session_after_wait','mi_lifecycle_one','hash_after_wait') returning started_at > transaction_timestamp() + interval '5 milliseconds'`).Scan(&issuedAfterWait); err != nil || !issuedAfterWait {
		t.Fatalf("session issuance clock includes transaction wait: actual-clock=%v err=%v", issuedAfterWait, err)
	}
	if err := waitingTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into agent_live_signals(session_id,signal_id) values ('session_lifecycle','signal_one')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23505", `insert into agent_live_signals(session_id,signal_id) values ('session_lifecycle','signal_one')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "42501", `delete from monitoring_agent_sessions where session_id='session_lifecycle'`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "42501", `update agent_live_signals set received_at=now()`)
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into vps_followups(followup_id,vps_id,kind,dedupe_key,summary) values ('followup_one','vps_lifecycle_one','archived_online','session_lifecycle','Online')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23505", `insert into vps_followups(followup_id,vps_id,kind,dedupe_key,summary) values ('followup_two','vps_lifecycle_one','archived_online','session_lifecycle','Online')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "23514", `update vps_followups set status='ignored' where followup_id='followup_one'`)
	execLifecycleSchemaSQL(t, ctx, runtime, `update vps_followups set status='ignored',resolution_reason='Checked',resolved_by='tester',resolved_at=now() where followup_id='followup_one'`)
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into asset_services(service_id,name,service_type) values ('service_lifecycle','Shared','web')`)
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into asset_service_associations(id,service_id,vps_id) values ('association_one','service_lifecycle','vps_lifecycle_one'),('association_two','service_lifecycle','vps_lifecycle_two')`)
	execLifecycleSchemaSQL(t, ctx, runtime, `update asset_service_associations set ended_at=now(),snapshot='{"name":"Shared"}' where id='association_one'`)
	var count int
	if err := runtime.QueryRow(ctx, `select count(*) from asset_service_associations where ended_at is null`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("shared association count = %d, err = %v", count, err)
	}
	execLifecycleSchemaSQL(t, ctx, runtime, `insert into vps_archive_requests(vps_id,idempotency_key,request_digest,response) values ('vps_lifecycle_one','archive-key','digest','{}')`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "42501", `update vps_archive_requests set response='{}'`)
	assertLifecycleSchemaSQLState(t, ctx, runtime, "42501", `delete from vps_archive_requests`)
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("runtime admission after lifecycle data: %v", err)
	}
}

func TestPostgresIntegrationVPSLifecycleRejectsLegacyData(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0066_constrain_monitoring_and_target_state_values.sql")
	execLifecycleSchemaSQL(t, ctx, db, `insert into vps_assets(vps_id,display_name,lifecycle_status,usage_status) values ('legacy','Legacy','active','in_use')`)
	if err := Apply(ctx, db); err == nil {
		t.Fatal("fresh-only migration accepted legacy business data")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("legacy migration rejection = %v", err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, `select count(*) from vps_assets where vps_id='legacy' and usage_status='in_use'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("legacy data changed: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema='public' and table_name='vps_assets' and column_name='usage_tags'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected migration partially applied: count=%d err=%v", count, err)
	}
}

func execLifecycleSchemaSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("lifecycle SQL: %v", err)
	}
}

func assertLifecycleSchemaSQLState(t *testing.T, ctx context.Context, db *pgxpool.Pool, code, sql string, args ...any) {
	t.Helper()
	_, err := db.Exec(ctx, sql, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("SQLSTATE = %v, want %s", err, code)
	}
}
