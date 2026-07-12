package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/store"
)

const postgresIntegrationFlag = "HOUFENG_POSTGRES_INTEGRATION"

func TestPostgresIntegrationAppliesFreshMigrations(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() on fresh postgres error = %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() second run on fresh postgres error = %v", err)
	}

	assertSingleStringValue(t, ctx, db, "select to_regclass('public.monitoring_instances')::text", "monitoring_instances")
	assertSingleStringValue(t, ctx, db, "select to_regclass('public.vps_monitoring_instance_links')::text", "vps_monitoring_instance_links")
	assertSingleIntValue(t, ctx, db, "select count(*)::int from schema_migrations where name = '0030_vps_first_status_semantics.sql'", 1)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from schema_migrations where name = '0050_extend_command_action_audit.sql'", 1)
	for _, indexName := range []string{
		"idx_monitoring_instance_command_action_audit_instance_time",
		"idx_monitoring_instance_command_action_audit_action_time",
		"idx_monitoring_instance_command_action_audit_global_time",
	} {
		assertSingleStringValue(t, ctx, db, "select to_regclass('public."+indexName+"')::text", indexName)
	}
}

func TestPostgresIntegrationCommandActionAuditUpgrade(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresSchema(t, ctx)

	execSQL(t, ctx, db, `
		create table users (
			user_id text primary key,
			username text not null unique,
			password_hash text not null,
			display_name text not null default '',
			role text not null default 'admin',
			created_at timestamptz not null default now(),
			password_changed_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table monitoring_instances (
			monitoring_instance_id text primary key,
			display_name text not null
		)
	`)
	execSQL(t, ctx, db, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_audit', 'audit-admin', 'hash', '审计管理员')
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instances (monitoring_instance_id, display_name)
		values ('mi_audit', 'Tokyo Audit')
	`)

	legacyMigration, err := fs.ReadFile(migrations.FS, "0046_create_command_action_audit.sql")
	if err != nil {
		t.Fatalf("read 0046 migration: %v", err)
	}
	execSQL(t, ctx, db, string(legacyMigration))
	execSQL(t, ctx, db, `
		create table command_action_audit_external_refs (
			external_ref_id text primary key
		)
	`)
	execSQL(t, ctx, db, `
		alter table monitoring_instance_command_action_audit
			add column external_ref_id text,
			add constraint command_action_audit_external_ref_fkey
			foreign key (external_ref_id)
			references command_action_audit_external_refs(external_ref_id)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at
		) values (
			'cmd_aud_legacy', 'act_legacy', 'mi_audit', 'uptime',
			'standard', 'queued', 'usr_audit', 'web', '2026-07-01T00:00:00Z'
		)
	`)

	extensionMigration, err := fs.ReadFile(migrations.FS, "0050_extend_command_action_audit.sql")
	if err != nil {
		t.Fatalf("read 0050 migration: %v", err)
	}
	execSQL(t, ctx, db, string(extensionMigration))
	execSQL(t, ctx, db, string(extensionMigration))

	var instanceName, actorUsername, actorDisplayName string
	if err := db.QueryRow(ctx, `
		select monitoring_instance_name_snapshot, actor_username_snapshot, actor_display_name_snapshot
		from monitoring_instance_command_action_audit
		where audit_id = 'cmd_aud_legacy'
	`).Scan(&instanceName, &actorUsername, &actorDisplayName); err != nil {
		t.Fatalf("query backfilled command audit snapshots: %v", err)
	}
	if instanceName != "Tokyo Audit" || actorUsername != "audit-admin" || actorDisplayName != "审计管理员" {
		t.Fatalf("backfilled snapshots = (%q, %q, %q)", instanceName, actorUsername, actorDisplayName)
	}

	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at
		) values (
			'cmd_aud_rollback', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'queued', 'usr_audit', 'web', '2026-07-01T00:01:00Z'
		)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, occurred_at
		) values (
			'cmd_aud_rollback_dispatched', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'dispatched', 'agent_sync', '2026-07-01T00:01:01Z'
		)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, exit_code, occurred_at
		) values (
			'cmd_aud_rollback_completed', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'completed', 'agent_sync', 0, '2026-07-01T00:01:02Z'
		)
	`)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from monitoring_instance_command_action_audit
		where action_id = 'act_rollback'
			and monitoring_instance_name_snapshot = ''
			and actor_username_snapshot = ''
			and actor_display_name_snapshot = ''
	`, 3)

	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at, details
		) values (
			'cmd_aud_rejected', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'usr_audit', 'web', '2026-07-01T00:02:00Z',
			'{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)

	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_action_identity_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_action', 'act_bad', 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'web', '{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_action_identity_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source
		) values ('cmd_aud_bad_queued_action', null, 'mi_audit', 'uptime', 'standard', 'queued', 'web')
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_rejected_source_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_source', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'agent_sync', '{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_rejected_reason_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_reason', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'web', '{"reason":"other"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_details_metadata_only", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_output', 'act_output', 'mi_audit', 'uptime',
			'standard', 'queued', 'web', '{"nested":{"stdout":"must-not-persist"}}'::jsonb
		)
	`)

	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_constraint
		where conrelid = 'monitoring_instance_command_action_audit'::regclass
			and contype = 'f'
			and confrelid in ('monitoring_instances'::regclass, 'users'::regclass)
	`, 0)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_constraint
		where conrelid = 'monitoring_instance_command_action_audit'::regclass
			and conname = 'command_action_audit_external_ref_fkey'
	`, 1)
	execSQL(t, ctx, db, `delete from users where user_id = 'usr_audit'`)
	execSQL(t, ctx, db, `delete from monitoring_instances where monitoring_instance_id = 'mi_audit'`)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from monitoring_instance_command_action_audit`, 5)
	assertSingleStringValue(t, ctx, db, `
		select actor_user_id
		from monitoring_instance_command_action_audit
		where audit_id = 'cmd_aud_legacy'
	`, "usr_audit")
}

func TestPostgresIntegrationVPSFirstUpgradeNormalizesLegacyState(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresSchema(t, ctx)

	createMinimalPost0029Schema(t, ctx, db)
	seedVPSFirstLegacyData(t, ctx, db)

	migrationSQL, err := fs.ReadFile(migrations.FS, "0030_vps_first_status_semantics.sql")
	if err != nil {
		t.Fatalf("read 0030 migration: %v", err)
	}
	if _, err := db.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("exec 0030 upgrade error = %v", err)
	}
	if _, err := db.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("exec 0030 upgrade second run error = %v", err)
	}

	assertVPSBusinessState(t, ctx, db, "vps_auto_cancel", "to_cancel", "unknown", "auto_renew_cancelled")
	assertVPSBusinessState(t, ctx, db, "vps_expired_running", "to_cancel", "unknown", "cancel")
	assertVPSBusinessState(t, ctx, db, "vps_expired_stopped", "cancelled", "unknown", "cancel")
	assertVPSBusinessState(t, ctx, db, "vps_paused_review", "active", "unknown", "observe")
	assertVPSBusinessState(t, ctx, db, "vps_invalid_review", "active", "unknown", "observe")
	assertVPSBusinessState(t, ctx, db, "vps_active_clean", "active", "unknown", "unreviewed")

	assertSingleStringValue(t, ctx, db, "select status from subscriptions where subscription_id = 'sub_invalid'", "unknown")
	assertSingleStringValue(t, ctx, db, "select lifecycle_status from monitoring_instances where monitoring_instance_id = 'mi_invalid'", "待接入")

	assertSingleIntValue(t, ctx, db, "select count(*)::int from asset_lifecycle_actions where action_id like 'ala_mig0030_%'", 5)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from asset_lifecycle_action_steps where step_id like 'als_mig0030_%'", 5)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from renewal_decisions where decision_id like 'rdec_mig0030_%'", 5)
}

func TestPostgresIntegrationUpgradePreservesExistingLogin(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	const (
		existingPassword    = "Legacy-Login-42!"
		replacementPassword = "Replacement-Seed-42!"
	)

	applyPostgresMigrationsThrough(t, ctx, db, "0029_rename_nodes_to_monitoring_instances.sql")
	legacyHash, err := auth.HashPassword(existingPassword)
	if err != nil {
		t.Fatalf("HashPassword legacy: %v", err)
	}
	execSQL(t, ctx, db, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ('usr_existing', 'admin', $1, '管理员', 'admin', now() - interval '1 day', now() - interval '1 day')
	`, legacyHash)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() upgrade with existing user error = %v", err)
	}

	users := store.NewPostgresUserRepository(db)
	if err := auth.SeedInitialUser(ctx, users, "admin", replacementPassword, "管理员", func() time.Time {
		return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	}); err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	u, err := users.FindByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("FindByUsername admin: %v", err)
	}
	if u.PasswordHash != legacyHash {
		t.Fatal("existing password hash changed during upgrade/bootstrap")
	}

	sessions, err := store.NewPostgresSessionRepository(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	svc := auth.New(users, sessions, auth.Options{
		SessionTTL: time.Hour,
		Now: func() time.Time {
			return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
		},
	})
	sess, err := svc.Login(ctx, "admin", existingPassword, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login with existing password after upgrade: %v", err)
	}
	if sess.UserID != "usr_existing" {
		t.Fatalf("session user = %q, want usr_existing", sess.UserID)
	}
	_, err = svc.Login(ctx, "admin", replacementPassword, "", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Login with seed replacement password = %v, want ErrInvalidCredentials", err)
	}
}

func applyPostgresMigrationsThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, throughName string) {
	t.Helper()
	names, err := Names()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	files := fstest.MapFS{}
	found := false
	for _, name := range names {
		payload, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		files[name] = &fstest.MapFile{Data: payload}
		if name == throughName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration %q not found", throughName)
	}
	if err := applyFS(ctx, poolStore{db: db}, files); err != nil {
		t.Fatalf("apply migrations through %s: %v", throughName, err)
	}
}

func openTemporaryPostgresDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for postgres integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	testDatabaseName := fmt.Sprintf("houfeng_test_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafePostgresIdentifier(testDatabaseName) {
		t.Fatalf("unsafe generated database name %q", testDatabaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open admin postgres pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `create database `+quotePostgresIdentifier(testDatabaseName)); err != nil {
		if isPostgresInsufficientPrivilege(err) {
			t.Skipf("temporary database creation requires CREATEDB privilege: %v", err)
		}
		t.Fatalf("create temporary postgres database %q: %v", testDatabaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotePostgresIdentifier(testDatabaseName)+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", testDatabaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = testDatabaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", testDatabaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres database %q: %v", testDatabaseName, err)
	}
	return testPool
}

func openTemporaryPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for postgres integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	schemaName := fmt.Sprintf("houfeng_it_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafePostgresIdentifier(schemaName) {
		t.Fatalf("unsafe generated schema name %q", schemaName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `create schema `+quotePostgresIdentifier(schemaName)); err != nil {
		t.Fatalf("create temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop schema if exists `+quotePostgresIdentifier(schemaName)+` cascade`); err != nil {
			t.Logf("drop temporary postgres schema %q: %v", schemaName, err)
		}
	})

	testConfig := adminConfig.Copy()
	if testConfig.ConnConfig.RuntimeParams == nil {
		testConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schemaName

	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres schema %q: %v", schemaName, err)
	}
	return testPool
}

func createLegacyAuthSchema(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		create table users (
		  user_id              text primary key,
		  username             text not null unique,
		  password_hash        text not null,
		  display_name         text not null default '',
		  role                 text not null default 'admin',
		  created_at           timestamptz not null default now(),
		  password_changed_at  timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table sessions (
		  session_id    text primary key,
		  user_id       text not null references users(user_id) on delete cascade,
		  issued_at     timestamptz not null default now(),
		  last_seen_at  timestamptz not null default now(),
		  expires_at    timestamptz not null,
		  user_agent    text not null default '',
		  client_ip     text not null default ''
		)
	`)
	execSQL(t, ctx, db, `create index sessions_user_idx on sessions(user_id)`)
	execSQL(t, ctx, db, `create index sessions_expires_idx on sessions(expires_at)`)
}

func markMigrationsAppliedThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, throughName string) {
	t.Helper()

	if _, err := db.Exec(ctx, ensureSchemaMigrationsSQL); err != nil {
		t.Fatalf("ensure schema_migrations: %v", err)
	}
	names, err := Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	for _, name := range names {
		execSQL(t, ctx, db, `insert into schema_migrations (name) values ($1)`, name)
		if name == throughName {
			return
		}
	}
	t.Fatalf("migration %q not found", throughName)
}

func createMinimalPost0029Schema(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		create table providers (
			provider_id text primary key,
			name text not null,
			labels text[] not null default '{}',
			note text not null default ''
		)
	`)
	execSQL(t, ctx, db, `
		create table vps_assets (
			vps_id text primary key,
			display_name text not null,
			provider_id text references providers(provider_id) on delete set null,
			provider_name text not null default '',
			lifecycle_status text not null check (lifecycle_status in ('active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived')),
			usage_status text not null check (usage_status in ('in_use', 'idle', 'standby', 'testing', 'unknown')),
			renewal_decision text not null default 'unreviewed' check (renewal_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')),
			ssh_port integer not null default 22,
			labels text[] not null default '{}',
			note text not null default '',
			updated_at timestamptz not null default now(),
			archived_at timestamptz
		)
	`)
	execSQL(t, ctx, db, `
		create table subscriptions (
			subscription_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			price numeric(12, 2) not null,
			currency text not null,
			billing_cycle text not null default '',
			billing_months integer not null,
			monthly_price numeric(12, 4) not null,
			renew_at date,
			auto_renew boolean not null default false,
			auto_renew_cancelled boolean not null default false,
			status text not null default 'active',
			payment_method text not null default '',
			note text not null default '',
			updated_at timestamptz not null default now(),
			constraint subscriptions_status_allowed check (status in ('active', 'paused', 'cancelled', 'expired', 'unknown'))
		)
	`)
	execSQL(t, ctx, db, `
		create table monitoring_instances (
			monitoring_instance_id text primary key,
			display_name text not null,
			region text not null,
			city text not null,
			provider text not null,
			lifecycle_status text not null,
			monitoring_status text not null default '启用',
			binding_status text not null default '未绑定',
			labels text[] not null default '{}',
			note text not null default '',
			current_health_status text not null default '正常',
			current_active_incident_count integer not null default 0,
			current_primary_issue_summary text not null default '',
			updated_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table vps_monitoring_instance_links (
			link_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
			linked_at timestamptz not null default now(),
			unlinked_at timestamptz,
			note text not null default ''
		)
	`)
	execSQL(t, ctx, db, `
		create table asset_lifecycle_actions (
			action_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			action_type text not null check (action_type in ('cancel_vps')),
			status text not null default 'completed' check (status in ('pending', 'completed', 'failed')),
			reason text not null default '',
			summary jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			confirmed_at timestamptz,
			completed_at timestamptz
		)
	`)
	execSQL(t, ctx, db, `
		create table asset_lifecycle_action_steps (
			step_id text primary key,
			action_id text not null references asset_lifecycle_actions(action_id) on delete cascade,
			object_type text not null check (object_type in ('vps', 'subscription', 'monitoring_instance', 'target')),
			object_id text not null,
			step_type text not null check (step_type in ('vps_lifecycle', 'subscription_status', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status')),
			status text not null check (status in ('completed', 'skipped', 'failed')),
			before_state jsonb not null default '{}'::jsonb,
			after_state jsonb not null default '{}'::jsonb,
			message text not null default '',
			executed_at timestamptz,
			created_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table renewal_decisions (
			decision_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			from_decision text,
			to_decision text not null check (to_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')),
			reason text not null default '',
			decided_at timestamptz not null default now(),
			created_at timestamptz not null default now()
		)
	`)
}

func seedVPSFirstLegacyData(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		insert into providers (provider_id, name, labels, note)
		values ('pv_pgtest', 'Postgres Test Provider', '{}', '')
	`)
	execSQL(t, ctx, db, `
		insert into vps_assets (
			vps_id, display_name, provider_id, provider_name, lifecycle_status,
			usage_status, renewal_decision, ssh_port, labels, note
		) values
			('vps_auto_cancel', 'Auto Cancel', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_expired_running', 'Expired Running', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_expired_stopped', 'Expired Stopped', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_paused_review', 'Paused Review', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_invalid_review', 'Invalid Review', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_active_clean', 'Active Clean', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', '')
	`)
	execSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, renew_at, auto_renew, auto_renew_cancelled, status, payment_method, note
		) values
			('sub_auto_cancel', 'vps_auto_cancel', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, true, 'active', '', ''),
			('sub_expired_running', 'vps_expired_running', 12, 'USD', 'monthly', 1, 12, current_date - 1, false, false, 'expired', '', ''),
			('sub_expired_stopped', 'vps_expired_stopped', 12, 'USD', 'monthly', 1, 12, current_date - 1, false, false, 'expired', '', ''),
			('sub_paused_review', 'vps_paused_review', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, false, 'paused', '', ''),
			('sub_invalid', 'vps_invalid_review', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, false, 'active', '', ''),
			('sub_active_clean', 'vps_active_clean', 12, 'USD', 'monthly', 1, 12, current_date + 20, true, false, 'active', '', '')
	`)
	execSQL(t, ctx, db, `alter table subscriptions drop constraint subscriptions_status_allowed`)
	execSQL(t, ctx, db, `update subscriptions set status = 'legacy-bad' where subscription_id = 'sub_invalid'`)

	execSQL(t, ctx, db, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, labels, note, current_health_status,
			current_active_incident_count, current_primary_issue_summary
		) values
			('mi_running', 'Running MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '在用', '启用', '未绑定', '{}', '', '正常', 0, ''),
			('mi_retired', 'Retired MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '已退役', '暂停', '未绑定', '{}', '', '正常', 0, ''),
			('mi_invalid', 'Invalid MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '在用', '启用', '未绑定', '{}', '', '正常', 0, '')
	`)
	execSQL(t, ctx, db, `update monitoring_instances set lifecycle_status = 'legacy-bad' where monitoring_instance_id = 'mi_invalid'`)
	execSQL(t, ctx, db, `
		insert into vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values
			('vnl_running', 'vps_expired_running', 'mi_running', ''),
			('vnl_retired', 'vps_expired_stopped', 'mi_retired', ''),
			('vnl_invalid', 'vps_active_clean', 'mi_invalid', '')
	`)
}

func execSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec sql %q error = %v", oneLineSQL(sql), err)
	}
}

func expectSQLConstraintFailure(t *testing.T, ctx context.Context, db *pgxpool.Pool, constraintName, sql string) {
	t.Helper()
	_, err := db.Exec(ctx, sql)
	if err == nil {
		t.Fatalf("exec sql %q succeeded, want constraint %q failure", oneLineSQL(sql), constraintName)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("exec sql %q error = %T %v, want postgres error", oneLineSQL(sql), err, err)
	}
	if pgErr.ConstraintName != constraintName {
		t.Fatalf("exec sql %q constraint = %q, want %q", oneLineSQL(sql), pgErr.ConstraintName, constraintName)
	}
}

func assertVPSBusinessState(t *testing.T, ctx context.Context, db *pgxpool.Pool, vpsID, lifecycle, usage, renewal string) {
	t.Helper()
	var gotLifecycle, gotUsage, gotRenewal string
	if err := db.QueryRow(ctx, `
		select lifecycle_status, usage_status, renewal_decision
		from vps_assets
		where vps_id = $1`, vpsID).Scan(&gotLifecycle, &gotUsage, &gotRenewal); err != nil {
		t.Fatalf("query vps %q business state: %v", vpsID, err)
	}
	if gotLifecycle != lifecycle || gotUsage != usage || gotRenewal != renewal {
		t.Fatalf("vps %q state = (%q, %q, %q), want (%q, %q, %q)", vpsID, gotLifecycle, gotUsage, gotRenewal, lifecycle, usage, renewal)
	}
}

func assertSingleStringValue(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(ctx, sql).Scan(&got); err != nil {
		t.Fatalf("query %q error = %v", oneLineSQL(sql), err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", oneLineSQL(sql), got, want)
	}
}

func assertSingleIntValue(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(ctx, sql).Scan(&got); err != nil {
		t.Fatalf("query %q error = %v", oneLineSQL(sql), err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", oneLineSQL(sql), got, want)
	}
}

func isSafePostgresIdentifier(value string) bool {
	return regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(value)
}

func quotePostgresIdentifier(value string) string {
	return pgx.Identifier{value}.Sanitize()
}

func isPostgresInsufficientPrivilege(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func oneLineSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
