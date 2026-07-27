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

const ensurePublicSchemaMigrationsSQL = `
create table if not exists public.schema_migrations (
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
	return namesFromSources(legacyMigrationSources())
}

func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return applyLegacyFS(ctx, poolStore{db: db}, legacyMigrationSources())
}

// legacyMigrationSources is the flags-off APP runner's explicit source
// boundary. applyFS remains parameterized so a separately admitted future
// source revision can be introduced without widening the legacy r1 runner.
func legacyMigrationSources() fs.FS {
	return newAppACLR1MigrationFS(migrations.FS)
}

// applyLegacyFS closes the flags-off runner over the frozen r1 source bytes
// before it creates or adopts the ambient migration ledger.
func applyLegacyFS(ctx context.Context, store migrationStore, fsys fs.FS) error {
	snapshot, err := snapshotMigrationSources(fsys)
	if err != nil {
		return fmt.Errorf("snapshot legacy r1 migration sources: %w", err)
	}
	if err := validateAppACLR1FrozenSourceSnapshot(snapshot); err != nil {
		return fmt.Errorf("validate legacy r1 migration sources: %w", err)
	}
	return applyMigrationSourceSnapshot(ctx, store, snapshot)
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

// migrationSourceSnapshot is a caller-owned immutable view of one embedded
// migration filesystem. Scoped convergence creates it before retrying so every
// SERIALIZABLE attempt sees the same source names, bytes, checksums, and
// canonical ledger body.
type migrationSourceSnapshot struct {
	sources      map[string]migrationSource
	names        []string
	canonicalSet []byte
}

func snapshotMigrationSources(fsys fs.FS) (migrationSourceSnapshot, error) {
	sources, err := migrationSources(fsys)
	if err != nil {
		return migrationSourceSnapshot{}, err
	}
	names := make([]string, 0, len(sources))
	entries := make([]MigrationChecksumEntry, 0, len(sources))
	for name, source := range sources {
		names = append(names, name)
		checksumBytes, err := hex.DecodeString(source.checksum)
		if err != nil || len(checksumBytes) != 32 {
			return migrationSourceSnapshot{}, fmt.Errorf("decode migration checksum for %q", name)
		}
		var checksum [32]byte
		copy(checksum[:], checksumBytes)
		entries = append(entries, MigrationChecksumEntry{Filename: name, Checksum: checksum})
	}
	sort.Strings(names)
	canonicalSet, err := CanonicalMigrationSetBodyV1(entries)
	if err != nil {
		return migrationSourceSnapshot{}, fmt.Errorf("build canonical migration source snapshot: %w", err)
	}
	return migrationSourceSnapshot{
		sources:      sources,
		names:        names,
		canonicalSet: canonicalSet,
	}, nil
}

func applyFS(ctx context.Context, store migrationStore, fsys fs.FS) error {
	snapshot, err := snapshotMigrationSources(fsys)
	if err != nil {
		return err
	}
	return applyMigrationSourceSnapshot(ctx, store, snapshot)
}

func applyMigrationSourceSnapshot(ctx context.Context, store migrationStore, snapshot migrationSourceSnapshot) error {
	sources := snapshot.sources
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

	for _, name := range snapshot.names {
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
		_ = tx.Rollback(ctx)
	}()

	if err := ensureLegacyMigrationLedgerInTx(ctx, tx, sources); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ensureMigrationLedgerInTx retains the scoped convergence dependency shape.
// Its ledger is deliberately public-qualified; legacy Apply uses the separate
// ambient-search-path helper below.
func ensureMigrationLedgerInTx(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource) error {
	return ensurePublicMigrationLedgerInTx(ctx, tx, sources)
}

func ensurePublicMigrationLedgerInTx(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource) error {
	return ensureMigrationLedgerAt(ctx, tx, sources, publicMigrationLedgerTarget)
}

func ensureLegacyMigrationLedgerInTx(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource) error {
	return ensureMigrationLedgerAt(ctx, tx, sources, legacyMigrationLedgerTarget)
}

// migrationLedgerTarget is selected only by the fixed legacy/scoped helpers;
// it is never built from configuration or other caller input.
type migrationLedgerTarget string

const (
	legacyMigrationLedgerTarget migrationLedgerTarget = "schema_migrations"
	publicMigrationLedgerTarget migrationLedgerTarget = "public.schema_migrations"
)

// ensureMigrationLedgerAt creates, adopts, validates, and locks the selected
// application migration ledger on a caller-owned transaction. It deliberately
// never begins or commits a transaction so scoped convergence can retain its
// public ledger lock through pending DDL, ACL convergence, and manifest
// publication.
func ensureMigrationLedgerAt(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource, target migrationLedgerTarget) error {
	if tx == nil {
		return fmt.Errorf("migration ledger has no PostgreSQL transaction")
	}
	var ensureSQL string
	switch target {
	case legacyMigrationLedgerTarget:
		ensureSQL = ensureSchemaMigrationsSQL
	case publicMigrationLedgerTarget:
		ensureSQL = ensurePublicSchemaMigrationsSQL
	default:
		return fmt.Errorf("unknown migration ledger target %q", target)
	}
	if _, err := tx.Exec(ctx, ensureSQL); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	ledgerTable := string(target)
	if _, err := tx.Exec(ctx, `alter table `+ledgerTable+` add column if not exists checksum text`); err != nil {
		return fmt.Errorf("add migration ledger checksum column: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table `+ledgerTable+` in share row exclusive mode`); err != nil {
		return fmt.Errorf("lock migration ledger: %w", err)
	}

	rows, err := tx.Query(ctx, `select name, checksum from `+ledgerTable+` order by name::text COLLATE "C"`)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()
	checksumsToBackfill := make([]migrationLedgerChecksum, 0)
	for rows.Next() {
		var name string
		var checksum *string
		if err := rows.Scan(&name, &checksum); err != nil {
			return fmt.Errorf("scan migration ledger: %w", err)
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
		return fmt.Errorf("iterate migration ledger: %w", err)
	}
	rows.Close()
	for _, backfill := range checksumsToBackfill {
		if _, err := tx.Exec(ctx, `update `+ledgerTable+` set checksum = $2 where name = $1`, backfill.name, backfill.checksum); err != nil {
			return fmt.Errorf("backfill migration ledger checksum for %q: %w", backfill.name, err)
		}
	}
	if _, err := tx.Exec(ctx, `alter table `+ledgerTable+` alter column checksum set not null`); err != nil {
		return fmt.Errorf("require migration ledger checksum: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		do $$
		begin
		  if not exists (
		    select 1
		    from pg_constraint
		    where conrelid = '%s'::regclass
		      and conname = 'schema_migrations_checksum_format'
		  ) then
		    alter table %s
		      add constraint schema_migrations_checksum_format
		      check (checksum ~ '^[0-9a-f]{64}$');
		  end if;
		end
		$$
	`, ledgerTable, ledgerTable)); err != nil {
		return fmt.Errorf("require migration ledger checksum format: %w", err)
	}
	return nil
}

type migrationLedgerChecksum struct {
	name     string
	checksum string
}

func (s poolStore) Applied(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Query(ctx, `select name, checksum from schema_migrations order by name::text COLLATE "C"`)
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
