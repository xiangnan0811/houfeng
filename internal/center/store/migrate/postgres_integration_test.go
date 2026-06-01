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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
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
	defer adminPool.Close()

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
			t.Logf("drop temporary postgres database %q: %v", testDatabaseName, err)
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
