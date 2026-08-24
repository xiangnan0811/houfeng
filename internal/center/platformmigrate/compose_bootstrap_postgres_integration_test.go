package platformmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/store"
)

func TestPostgresIntegrationComposeBootstrapRollback(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("Compose bootstrap rollback integration requires the ownership-checked ephemeral PostgreSQL runner")
	}
	ctx := context.Background()
	baseURL := os.Getenv("HOUFENG_DATABASE_URL")
	baseConfig, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("parse strict PostgreSQL fixture URL: %v", err)
	}
	if baseConfig.ConnConfig.User != "postgres" {
		t.Fatalf("strict PostgreSQL fixture user = %q, want postgres", baseConfig.ConnConfig.User)
	}
	bootstrapPool, err := store.OpenPostgres(ctx, baseURL)
	if err != nil {
		t.Fatalf("open strict PostgreSQL fixture: %v", err)
	}
	t.Cleanup(bootstrapPool.Close)

	suffixBytes := make([]byte, 8)
	if _, err := rand.Read(suffixBytes); err != nil {
		t.Fatalf("generate Compose rollback suffix: %v", err)
	}
	suffix := hex.EncodeToString(suffixBytes)
	config := ComposeBootstrapConfig{
		DatabaseName:  "hf_rollback_" + suffix,
		BootstrapRole: "postgres",
		AuthorityRole: "hf_rb_authority_" + suffix,
		Roles: AppRoleSetV1{
			CenterRuntime: "hf_rb_runtime_" + suffix,
			PlatformAdmin: "hf_rb_admin_" + suffix,
			Migrator:      "hf_rb_migrator_" + suffix,
		},
		Passwords: ComposeRolePasswords{
			Runtime:       "runtime rollback secret",
			PlatformAdmin: "admin rollback secret",
			Migrator:      "migrator rollback secret",
			Authority:     "authority rollback secret",
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("validate Compose rollback config: %v", err)
	}
	createDatabase := formatComposeBootstrapIntegrationDDL(t, ctx, bootstrapPool, `CREATE DATABASE %I`, config.DatabaseName)
	if _, err := bootstrapPool.Exec(ctx, createDatabase); err != nil {
		t.Fatalf("create Compose rollback database: %v", err)
	}
	t.Cleanup(func() {
		cleanupComposeBootstrapIntegration(t, context.Background(), bootstrapPool, config)
	})

	targetConfig := baseConfig.Copy()
	targetConfig.ConnConfig.Database = config.DatabaseName
	targetPool, err := pgxpool.NewWithConfig(ctx, targetConfig)
	if err != nil {
		t.Fatalf("open Compose rollback database: %v", err)
	}
	if err := targetPool.Ping(ctx); err != nil {
		t.Fatalf("ping Compose rollback database: %v", err)
	}
	t.Cleanup(targetPool.Close)

	ownerBefore := readComposeBootstrapIntegrationOwner(t, ctx, bootstrapPool, config.DatabaseName)
	aclBefore := readComposeBootstrapIntegrationPGControlACL(t, ctx, bootstrapPool)
	tx, err := targetPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("begin Compose rollback transaction: %v", err)
	}
	if err := provisionComposeBootstrapInTx(ctx, tx, config); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("apply Compose bootstrap before synthetic failure: %v", err)
	}
	syntheticFailure := errors.New("synthetic post-mutation failure")
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback after %v: %v", syntheticFailure, err)
	}

	var roleCount int
	if err := bootstrapPool.QueryRow(ctx, `select count(*)::integer from pg_catalog.pg_roles where rolname = any($1::name[])`, config.roleNames()).Scan(&roleCount); err != nil {
		t.Fatalf("read roles after Compose rollback: %v", err)
	}
	if roleCount != 0 {
		t.Fatalf("Compose rollback retained %d created roles", roleCount)
	}
	if ownerAfter := readComposeBootstrapIntegrationOwner(t, ctx, bootstrapPool, config.DatabaseName); ownerAfter != ownerBefore {
		t.Fatalf("Compose rollback database owner = %q, want pre-failure %q", ownerAfter, ownerBefore)
	}
	if aclAfter := readComposeBootstrapIntegrationPGControlACL(t, ctx, bootstrapPool); aclAfter != aclBefore {
		t.Fatalf("Compose rollback pg_control_system ACL = %q, want pre-failure %q", aclAfter, aclBefore)
	}
}

func readComposeBootstrapIntegrationOwner(t *testing.T, ctx context.Context, pool *pgxpool.Pool, database string) string {
	t.Helper()
	var owner string
	if err := pool.QueryRow(ctx, `
		select owner.rolname
		from pg_catalog.pg_database database
		join pg_catalog.pg_roles owner on owner.oid = database.datdba
		where database.datname = $1
	`, database).Scan(&owner); err != nil {
		t.Fatalf("read Compose rollback database owner: %v", err)
	}
	return owner
}

func readComposeBootstrapIntegrationPGControlACL(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var acl string
	if err := pool.QueryRow(ctx, `
		select coalesce(procedure.proacl::text, '<null>')
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = 'pg_catalog'
		  and procedure.proname = 'pg_control_system'
		  and procedure.pronargs = 0
	`).Scan(&acl); err != nil {
		t.Fatalf("read Compose rollback pg_control_system ACL: %v", err)
	}
	return acl
}

func formatComposeBootstrapIntegrationDDL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, format string, arguments ...any) string {
	t.Helper()
	queryArguments := append([]any{format}, arguments...)
	placeholders := make([]string, len(arguments))
	for index := range arguments {
		placeholders[index] = "$" + string(rune('2'+index)) + "::text"
	}
	var ddl string
	if err := pool.QueryRow(ctx, `select pg_catalog.format($1::text, `+strings.Join(placeholders, ", ")+`)`, queryArguments...).Scan(&ddl); err != nil {
		t.Fatalf("format Compose rollback DDL: %v", err)
	}
	return ddl
}

func cleanupComposeBootstrapIntegration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, config ComposeBootstrapConfig) {
	t.Helper()
	_, _ = pool.Exec(ctx, `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity where datname = $1 and pid <> pg_backend_pid()`, config.DatabaseName)
	for _, statement := range []string{
		formatComposeBootstrapIntegrationDDL(t, ctx, pool, `DROP DATABASE IF EXISTS %I`, config.DatabaseName),
		formatComposeBootstrapIntegrationDDL(t, ctx, pool, `DROP ROLE IF EXISTS %I, %I, %I, %I`, config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.Roles.Migrator, config.AuthorityRole),
	} {
		if _, err := pool.Exec(ctx, statement); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("cleanup Compose rollback resource: %v", err)
		}
	}
}
