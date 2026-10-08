package migrate

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const accessManagementMigrationName = "0071_add_access_management.sql"

func TestPostgresIntegrationAccessManagementMigrationZeroUsers(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0070_add_record_import_destination_subject.sql")

	if err := applyAccessManagementMigration(ctx, db); err != nil {
		t.Fatalf("apply access-management migration with zero users: %v", err)
	}

	var supervisors int
	if err := db.QueryRow(ctx, `select count(*) from public.users where is_supervisor`).Scan(&supervisors); err != nil {
		t.Fatalf("count supervisors after zero-user migration: %v", err)
	}
	if supervisors != 0 {
		t.Fatalf("zero-user migration created %d supervisors, want 0", supervisors)
	}
	assertAccessManagementMigrationRecorded(t, ctx, db)
}

func TestPostgresIntegrationAccessManagementMigrationSingleUserSeedsSupervisor(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0070_add_record_import_destination_subject.sql")

	if _, err := db.Exec(ctx, `
		insert into public.users (user_id, username, password_hash, role)
		values ('usr_access_management_seed', 'seed-admin', 'hash', 'admin')
	`); err != nil {
		t.Fatalf("insert single pre-C71 user: %v", err)
	}
	if err := applyAccessManagementMigration(ctx, db); err != nil {
		t.Fatalf("apply access-management migration with one admin: %v", err)
	}

	var supervisor bool
	if err := db.QueryRow(ctx, `select is_supervisor from public.users where user_id = 'usr_access_management_seed'`).Scan(&supervisor); err != nil {
		t.Fatalf("read seeded supervisor: %v", err)
	}
	if !supervisor {
		t.Fatal("single existing admin was not marked supervisor")
	}
	assertAccessManagementMigrationRecorded(t, ctx, db)
}

func TestPostgresIntegrationAccessManagementMigrationAmbiguousUsersRejectsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0070_add_record_import_destination_subject.sql")

	if _, err := db.Exec(ctx, `
		insert into public.users (user_id, username, password_hash, role)
		values
		  ('usr_access_management_one', 'first-admin', 'hash', 'admin'),
		  ('usr_access_management_two', 'second-admin', 'hash', 'admin')
	`); err != nil {
		t.Fatalf("insert ambiguous pre-C71 users: %v", err)
	}
	if err := applyAccessManagementMigration(ctx, db); err == nil {
		t.Fatal("ambiguous existing users unexpectedly allowed access-management migration")
	}

	var migrationRows int
	if err := db.QueryRow(ctx, `select count(*) from public.schema_migrations where name = $1`, accessManagementMigrationName).Scan(&migrationRows); err != nil {
		t.Fatalf("check failed migration ledger row: %v", err)
	}
	if migrationRows != 0 {
		t.Fatalf("failed access-management migration recorded %d ledger rows, want 0", migrationRows)
	}
	var supervisorColumnRows int
	if err := db.QueryRow(ctx, `
		select count(*)
		from information_schema.columns
		where table_schema = 'public' and table_name = 'users' and column_name = 'is_supervisor'
	`).Scan(&supervisorColumnRows); err != nil {
		t.Fatalf("check rolled-back supervisor column: %v", err)
	}
	if supervisorColumnRows != 0 {
		t.Fatalf("failed access-management migration left is_supervisor column count %d, want 0", supervisorColumnRows)
	}
}

func applyAccessManagementMigration(ctx context.Context, db *pgxpool.Pool) error {
	sources, err := migrationSources(migrations.FS)
	if err != nil {
		return err
	}
	source, ok := sources[accessManagementMigrationName]
	if !ok {
		return fmt.Errorf("missing %s source", accessManagementMigrationName)
	}
	return (poolStore{db: db}).Apply(ctx, accessManagementMigrationName, source.checksum, source.sql)
}

func assertAccessManagementMigrationRecorded(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var migrationRows int
	if err := db.QueryRow(ctx, `select count(*) from public.schema_migrations where name = $1`, accessManagementMigrationName).Scan(&migrationRows); err != nil {
		t.Fatalf("check access-management migration ledger row: %v", err)
	}
	if migrationRows != 1 {
		t.Fatalf("access-management migration ledger rows = %d, want 1", migrationRows)
	}
}
