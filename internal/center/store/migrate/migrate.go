package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const ensureSchemaMigrationsSQL = `
create table if not exists schema_migrations (
  name text primary key,
	checksum text not null check (checksum ~ '^[0-9a-f]{64}$'),
  applied_at timestamptz not null default now()
)`

type migrationStore interface {
	EnsureLedger(ctx context.Context, sources map[string]migrationSource) error
	Applied(ctx context.Context) (map[string]string, error)
	Apply(ctx context.Context, name, checksum, sql string) error
}

type poolStore struct {
	db *pgxpool.Pool
}

func Names() ([]string, error) {
	return namesFromSources(migrations.FS)
}

func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return applyFS(ctx, poolStore{db: db}, migrations.FS)
}

func namesFromSources(fsys fs.FS) ([]string, error) {
	sources, err := migrationSources(fsys)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

type migrationSource struct {
	checksum string
	sql      string
}

func migrationSources(fsys fs.FS) (map[string]migrationSource, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	sources := make(map[string]migrationSource, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		payload, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(payload)
		sources[entry.Name()] = migrationSource{
			checksum: hex.EncodeToString(digest[:]),
			sql:      string(payload),
		}
	}
	return sources, nil
}

func applyFS(ctx context.Context, store migrationStore, fsys fs.FS) error {
	sources, err := migrationSources(fsys)
	if err != nil {
		return err
	}
	if err := store.EnsureLedger(ctx, sources); err != nil {
		return fmt.Errorf("ensure migration ledger: %w", err)
	}
	applied, err := store.Applied(ctx)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	for name := range applied {
		if _, ok := sources[name]; !ok {
			return fmt.Errorf("unknown applied migration %q", name)
		}
	}

	names, err := namesFromSources(fsys)
	if err != nil {
		return err
	}
	for _, name := range names {
		source := sources[name]
		if checksum, ok := applied[name]; ok {
			if checksum != source.checksum {
				return fmt.Errorf("migration checksum mismatch for %q", name)
			}
			continue
		}
		if err := store.Apply(ctx, name, source.checksum, source.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}

	return nil
}

func (s poolStore) EnsureLedger(ctx context.Context, sources map[string]migrationSource) (err error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, ensureSchemaMigrationsSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `alter table schema_migrations add column if not exists checksum text`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `lock table schema_migrations in share row exclusive mode`); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `select name, checksum from schema_migrations order by name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	checksumsToBackfill := make([]migrationLedgerChecksum, 0)
	for rows.Next() {
		var name string
		var checksum *string
		if err := rows.Scan(&name, &checksum); err != nil {
			return err
		}
		source, ok := sources[name]
		if !ok {
			return fmt.Errorf("unknown applied migration %q", name)
		}
		if checksum == nil {
			checksumsToBackfill = append(checksumsToBackfill, migrationLedgerChecksum{name: name, checksum: source.checksum})
			continue
		}
		if *checksum != source.checksum {
			return fmt.Errorf("migration checksum mismatch for %q", name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, backfill := range checksumsToBackfill {
		if _, err := tx.Exec(ctx, `update schema_migrations set checksum = $2 where name = $1`, backfill.name, backfill.checksum); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `alter table schema_migrations alter column checksum set not null`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		do $$
		begin
		  if not exists (
		    select 1
		    from pg_constraint
		    where conrelid = 'schema_migrations'::regclass
		      and conname = 'schema_migrations_checksum_format'
		  ) then
		    alter table schema_migrations
		      add constraint schema_migrations_checksum_format
		      check (checksum ~ '^[0-9a-f]{64}$');
		  end if;
		end
		$$
	`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type migrationLedgerChecksum struct {
	name     string
	checksum string
}

func (s poolStore) Applied(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Query(ctx, `select name, checksum from schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

func (s poolStore) Apply(ctx context.Context, name, checksum, sql string) (err error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (name, checksum) values ($1, $2)`, name, checksum); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
