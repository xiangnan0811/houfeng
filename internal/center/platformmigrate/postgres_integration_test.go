package platformmigrate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
	witnessMigrations "houfeng/db/deletionwitness/migrations"
	"houfeng/db/recoverycontrol/migrations"
)

const postgresIntegrationFlag = "HOUFENG_POSTGRES_INTEGRATION"

func TestPostgresIntegrationIndependentMigrationDomains(t *testing.T) {
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}

	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		env       string
		apply     func(context.Context, *pgxpool.Pool) error
		tables    []string
		immutable string
	}{
		{
			name: "ledger",
			env:  "HOUFENG_DELETION_LEDGER_DATABASE_URL",
			apply: func(ctx context.Context, db *pgxpool.Pool) error {
				return Apply(ctx, db, ledgerMigrations.FS)
			},
			tables:    []string{"deletion_ledger_entries", "deletion_ledger_head"},
			immutable: "deletion_ledger_entries",
		},
		{
			name: "witness",
			env:  "HOUFENG_DELETION_WITNESS_DATABASE_URL",
			apply: func(ctx context.Context, db *pgxpool.Pool) error {
				return Apply(ctx, db, witnessMigrations.FS)
			},
			tables:    []string{"deletion_witness_entries", "deletion_witness_head"},
			immutable: "deletion_witness_entries",
		},
		{
			name: "recovery control",
			env:  "HOUFENG_RECOVERY_CONTROL_DATABASE_URL",
			apply: func(ctx context.Context, db *pgxpool.Pool) error {
				return Apply(ctx, db, migrations.FS)
			},
			tables:    []string{"recovery_trust_entries", "recovery_trust_head", "recovery_point_manifests"},
			immutable: "recovery_trust_entries",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := strings.TrimSpace(os.Getenv(tc.env))
			if url == "" {
				t.Fatalf("%s is required when %s=1", tc.env, postgresIntegrationFlag)
			}
			db, err := pgxpool.New(ctx, url)
			if err != nil {
				t.Fatalf("open %s database: %v", tc.name, err)
			}
			t.Cleanup(db.Close)

			if err := tc.apply(ctx, db); err != nil {
				t.Fatalf("fresh apply %s migrations: %v", tc.name, err)
			}
			if err := tc.apply(ctx, db); err != nil {
				t.Fatalf("repeat apply %s migrations: %v", tc.name, err)
			}

			for _, table := range tc.tables {
				var actual string
				if err := db.QueryRow(ctx, "select to_regclass('public."+table+"')::text").Scan(&actual); err != nil {
					t.Fatalf("look up %s table %s: %v", tc.name, table, err)
				}
				if actual != table {
					t.Fatalf("%s table %s = %q", tc.name, table, actual)
				}
			}

			var extensionSchema string
			if err := db.QueryRow(ctx, `
				select n.nspname
				from pg_extension e
				join pg_namespace n on n.oid = e.extnamespace
				where e.extname = 'pgcrypto'
			`).Scan(&extensionSchema); err != nil {
				t.Fatalf("read %s pgcrypto schema: %v", tc.name, err)
			}
			if extensionSchema != "record_platform_internal" {
				t.Fatalf("%s pgcrypto schema = %q, want record_platform_internal", tc.name, extensionSchema)
			}

			_, err = db.Exec(ctx, "delete from public."+tc.immutable)
			if err == nil || !isImmutableMutationError(err) {
				t.Fatalf("delete immutable %s table error = %v, want SQLSTATE 55000", tc.name, err)
			}
		})
	}
}

func isImmutableMutationError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55000"
}
