package platformmigrate

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
)

const ensureSchemaMigrationsSQL = `
create table if not exists schema_migrations (
  name text primary key,
  checksum text not null check (checksum ~ '^[0-9a-f]{64}$'),
  applied_at timestamptz not null default now()
)`

type Store interface {
	EnsureLedger(context.Context) error
	Applied(context.Context) (map[string]string, error)
	Apply(ctx context.Context, name, checksum, sql string) error
}

type poolStore struct {
	db *pgxpool.Pool
}

func Apply(ctx context.Context, db *pgxpool.Pool, fsys fs.FS) error {
	return ApplyFS(ctx, poolStore{db: db}, fsys)
}

func Names(fsys fs.FS) ([]string, error) {
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

func ApplyFS(ctx context.Context, store Store, fsys fs.FS) error {
	if err := store.EnsureLedger(ctx); err != nil {
		return fmt.Errorf("ensure migration ledger: %w", err)
	}

	sources, err := migrationSources(fsys)
	if err != nil {
		return err
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

	names, err := Names(fsys)
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
			return fmt.Errorf("apply migration %q: %w", name, err)
		}
	}

	return nil
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

func (s poolStore) EnsureLedger(ctx context.Context) error {
	_, err := s.db.Exec(ctx, ensureSchemaMigrationsSQL)
	return err
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
	if _, err := tx.Exec(ctx,
		`insert into schema_migrations (name, checksum) values ($1, $2)`, name, checksum); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
