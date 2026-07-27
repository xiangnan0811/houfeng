package migrate

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

type appACLRuntimeAdmissionBeginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)

type appACLRuntimeAdmissionDependencies struct {
	embeddedMigrations fs.FS
	beginTx            appACLRuntimeAdmissionBeginTx
	readManifest       func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error)
	verifyManifest     func(AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error)
	readCatalog        func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error)
	verifyCatalog      func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error
}

// AdmitAppACLRuntime verifies that a direct runtime login may use the APP
// schema. It performs only read-only catalog/manifest checks in one
// REPEATABLE READ snapshot and never applies migrations or ACL changes.
func AdmitAppACLRuntime(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return fmt.Errorf("app ACL runtime admission has no PostgreSQL pool")
	}
	return admitAppACLRuntimeWithDependencies(ctx, appACLRuntimeAdmissionDependencies{
		embeddedMigrations: migrations.FS,
		beginTx: func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return db.BeginTx(ctx, options)
		},
		readManifest: readAppACLManifestRuntimeSnapshotInTxV1,
		verifyManifest: func(snapshot AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error) {
			return verifyAppACLManifestRuntimeSnapshotV1(snapshot, migrations.FS)
		},
		readCatalog:   readAppACLEffectiveCatalogSnapshotInTxR1,
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
}

func admitAppACLRuntimeWithDependencies(
	ctx context.Context,
	dependencies appACLRuntimeAdmissionDependencies,
) error {
	if err := dependencies.validate(); err != nil {
		return err
	}
	tx, err := dependencies.beginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("begin app ACL runtime admission transaction: %w", err)
	}
	if tx == nil {
		return fmt.Errorf("begin app ACL runtime admission transaction returned nil transaction")
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	manifestSnapshot, err := dependencies.readManifest(ctx, tx)
	if err != nil {
		return fmt.Errorf("read app ACL runtime manifest: %w", err)
	}
	manifest, err := dependencies.verifyManifest(manifestSnapshot)
	if err != nil {
		return fmt.Errorf("verify app ACL runtime manifest: %w", err)
	}
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet)
	if err != nil {
		return fmt.Errorf("parse admitted app ACL privilege set: %w", err)
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1(manifestSnapshot.DatabaseName, privilegeSet.RoleBindings)
	if err != nil {
		return fmt.Errorf("compile admitted app ACL catalog contract: %w", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, manifest.MigratorCatalogRole)
	if err != nil {
		return fmt.Errorf("build admitted app ACL catalog verifier input: %w", err)
	}
	catalogSnapshot, err := dependencies.readCatalog(ctx, tx, input)
	if err != nil {
		return fmt.Errorf("read app ACL runtime catalog: %w", err)
	}
	if err := validateAppACLRuntimeAdmissionSnapshotIdentity(manifestSnapshot, catalogSnapshot); err != nil {
		return err
	}
	if err := dependencies.verifyCatalog(catalogSnapshot, input); err != nil {
		return fmt.Errorf("verify app ACL runtime catalog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit app ACL runtime admission transaction: %w", err)
	}
	return nil
}

func (dependencies appACLRuntimeAdmissionDependencies) validate() error {
	if dependencies.embeddedMigrations == nil {
		return fmt.Errorf("app ACL runtime admission embedded migration filesystem is nil")
	}
	if dependencies.beginTx == nil {
		return fmt.Errorf("app ACL runtime admission transaction opener is nil")
	}
	if dependencies.readManifest == nil {
		return fmt.Errorf("app ACL runtime admission manifest reader is nil")
	}
	if dependencies.verifyManifest == nil {
		return fmt.Errorf("app ACL runtime admission manifest verifier is nil")
	}
	if dependencies.readCatalog == nil {
		return fmt.Errorf("app ACL runtime admission catalog reader is nil")
	}
	if dependencies.verifyCatalog == nil {
		return fmt.Errorf("app ACL runtime admission catalog verifier is nil")
	}
	return nil
}

func validateAppACLRuntimeAdmissionSnapshotIdentity(
	manifest AppACLManifestRuntimeSnapshotV1,
	catalog AppACLEffectiveCatalogSnapshotR1,
) error {
	if catalog.DatabaseName != manifest.DatabaseName {
		return fmt.Errorf("app ACL runtime catalog database %q does not match manifest database %q", catalog.DatabaseName, manifest.DatabaseName)
	}
	if catalog.SessionUser != manifest.SessionUser {
		return fmt.Errorf("app ACL runtime catalog session user %q does not match manifest session user %q", catalog.SessionUser, manifest.SessionUser)
	}
	if catalog.CurrentUser != manifest.CurrentUser {
		return fmt.Errorf("app ACL runtime catalog current user %q does not match manifest current user %q", catalog.CurrentUser, manifest.CurrentUser)
	}
	return nil
}
