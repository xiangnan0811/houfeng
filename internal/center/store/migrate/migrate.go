package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const ensureSchemaMigrationsSQL = `
create table if not exists schema_migrations (
  name text primary key,
  applied_at timestamptz not null default now()
)`

const hasSchemaMigrationSQL = `
select exists (
  select 1
  from schema_migrations
  where name = $1
)`

const recordSchemaMigrationSQL = `
insert into schema_migrations (name)
values ($1)
`

type migrationStore interface {
	EnsureLedger(ctx context.Context) error
	HasMigration(ctx context.Context, name string) (bool, error)
	ExecMigration(ctx context.Context, sql string) error
	RecordMigration(ctx context.Context, name string) error
}

type poolStore struct {
	db *pgxpool.Pool
}

func Names() ([]string, error) {
	return namesFromFS(migrations.FS)
}

func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return applyFS(ctx, poolStore{db: db}, migrations.FS)
}

func namesFromFS(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}

	sort.Strings(names)
	return names, nil
}

func applyFS(ctx context.Context, store migrationStore, fsys fs.FS) error {
	if err := store.EnsureLedger(ctx); err != nil {
		return fmt.Errorf("ensure migration ledger: %w", err)
	}

	names, err := namesFromFS(fsys)
	if err != nil {
		return err
	}

	for _, name := range names {
		applied, err := store.HasMigration(ctx, name)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := store.ExecMigration(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if err := store.RecordMigration(ctx, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}

	return nil
}

func (s poolStore) EnsureLedger(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, ensureSchemaMigrationsSQL); err != nil {
		return err
	}
	return nil
}

func (s poolStore) HasMigration(ctx context.Context, name string) (bool, error) {
	var applied bool
	if err := s.db.QueryRow(ctx, hasSchemaMigrationSQL, name).Scan(&applied); err != nil {
		return false, err
	}
	return applied, nil
}

func (s poolStore) ExecMigration(ctx context.Context, sql string) error {
	if _, err := s.db.Exec(ctx, sql); err != nil {
		return err
	}
	return nil
}

func (s poolStore) RecordMigration(ctx context.Context, name string) error {
	if _, err := s.db.Exec(ctx, recordSchemaMigrationSQL, name); err != nil {
		return err
	}
	return nil
}
