package migrate

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const appACLSchemaAdvisoryLockV1 = "houfeng-app-schema-acl-v1"

// EnsureAppACLManifestGenesisV1 writes the only allowed r1 manifest genesis.
// It is deliberately narrower than the later scoped migration path: it refuses
// ledger, manifest, or privilege drift instead of trying to repair any state.
func EnsureAppACLManifestGenesisV1(
	ctx context.Context,
	db *pgxpool.Pool,
	embeddedMigrations fs.FS,
	compiledPrivilegeSet []byte,
	migratorCatalogRole string,
) (AppACLManifestPersistedV1, error) {
	if db == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest genesis has no PostgreSQL pool")
	}
	if embeddedMigrations == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("embedded migration filesystem is nil")
	}
	if len(compiledPrivilegeSet) < 1 || len(compiledPrivilegeSet) > maxCanonicalACLManifestBodyBytes {
		return AppACLManifestPersistedV1{}, fmt.Errorf("compiled app ACL privilege set size is outside v1 bounds")
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(compiledPrivilegeSet); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("validate compiled app ACL privilege set: %w", err)
	}
	if !validCatalogRoleName(migratorCatalogRole) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("invalid app ACL migrator catalog role")
	}

	embeddedMigrationSet, err := CanonicalMigrationSetFromFS(embeddedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build embedded application migration set: %w", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("begin app ACL manifest genesis transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, appACLSchemaAdvisoryLockV1); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("lock app ACL manifest genesis: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("lock application migration ledger: %w", err)
	}

	appliedMigrations, err := readAppliedAppMigrationsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	appliedMigrationSet, err := CanonicalMigrationSetBodyV1(appliedMigrations)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("encode applied application migration ledger: %w", err)
	}
	if !bytes.Equal(appliedMigrationSet, embeddedMigrationSet) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("applied application migration ledger does not match embedded migrations")
	}

	manifests, err := readAppACLManifestRevisionsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	head, err := readAppACLManifestHeadForUpdateV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	existing, err := checkAppACLManifestGenesisStateV1(
		manifests,
		head,
		embeddedMigrationSet,
		compiledPrivilegeSet,
		migratorCatalogRole,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if existing == nil {
		genesis, err := insertAppACLManifestGenesisV1(ctx, tx, embeddedMigrationSet, compiledPrivilegeSet, migratorCatalogRole)
		if err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("commit app ACL manifest genesis transaction: %w", err)
		}
		return genesis, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("commit read-only app ACL manifest genesis transaction: %w", err)
	}
	return *existing, nil
}

// checkAppACLManifestGenesisStateV1 determines whether a manifest can be
// created, or whether the persisted state is the exact immutable r1 result.
// It performs no writes so a caller can validate its catalog before inserting
// a revision or advancing the singleton head.
func checkAppACLManifestGenesisStateV1(
	manifests []AppACLManifestPersistedV1,
	head *AppACLManifestHeadV1,
	embeddedMigrationSet []byte,
	compiledPrivilegeSet []byte,
	migratorCatalogRole string,
) (*AppACLManifestPersistedV1, error) {
	if head == nil {
		if len(manifests) != 0 {
			return nil, fmt.Errorf("app ACL manifest has revisions with a null head")
		}
		return nil, nil
	}
	if err := ValidateAppACLManifestChainV1(manifests, *head); err != nil {
		return nil, fmt.Errorf("validate persisted app ACL manifest chain: %w", err)
	}
	if head.ManifestRevision != 1 || len(manifests) != 1 {
		return nil, fmt.Errorf("app ACL manifest chain is already advanced")
	}
	genesis := manifests[0]
	if !bytes.Equal(genesis.CanonicalMigrationSet, embeddedMigrationSet) {
		return nil, fmt.Errorf("persisted app ACL manifest migration set does not match embedded migrations")
	}
	if !bytes.Equal(genesis.CanonicalPrivilegeSet, compiledPrivilegeSet) {
		return nil, fmt.Errorf("persisted app ACL manifest privilege set does not match compiled privilege set")
	}
	if genesis.MigratorCatalogRole != migratorCatalogRole {
		return nil, fmt.Errorf("persisted app ACL manifest migrator catalog role does not match expected role")
	}
	return &genesis, nil
}

func insertAppACLManifestGenesisV1(
	ctx context.Context,
	tx pgx.Tx,
	embeddedMigrationSet []byte,
	compiledPrivilegeSet []byte,
	migratorCatalogRole string,
) (AppACLManifestPersistedV1, error) {
	genesis, err := NewAppACLManifestPersistedV1(1, migratorCatalogRole, [32]byte{}, embeddedMigrationSet, compiledPrivilegeSet)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build app ACL manifest genesis: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.app_acl_manifest_revisions (
			manifest_revision,
			migrator_catalog_role,
			previous_manifest_digest,
			canonical_migration_set,
			sorted_migration_set_digest,
			canonical_privilege_set,
			privilege_set_digest,
			manifest_digest
		) values ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		int64(genesis.ManifestRevision),
		genesis.MigratorCatalogRole,
		genesis.PreviousManifestDigest[:],
		genesis.CanonicalMigrationSet,
		genesis.MigrationSetDigest[:],
		genesis.CanonicalPrivilegeSet,
		genesis.PrivilegeSetDigest[:],
		genesis.ManifestDigest[:],
	); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("insert app ACL manifest genesis revision: %w", err)
	}
	result, err := tx.Exec(ctx, `
		update public.app_acl_manifest_head
		set manifest_revision = $1, manifest_digest = $2
		where singleton
		  and manifest_revision is null
		  and manifest_digest is null
	`, int64(genesis.ManifestRevision), genesis.ManifestDigest[:])
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("cas app ACL manifest genesis head: %w", err)
	}
	if result.RowsAffected() != 1 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest genesis head changed concurrently")
	}
	return genesis, nil
}
