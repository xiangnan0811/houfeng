package migrate

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

type appACLCurrentRuntimeAdmissionDependencies struct {
	beginTx       appACLRuntimeAdmissionBeginTx
	readManifest  func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error)
	readCatalog   func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error)
	verifyCatalog func(AppACLEffectiveCatalogSnapshotR1, appACLEffectiveCatalogVerifierInput) error
}

// AdmitAppACLCurrentRuntime admits a direct runtime login only when the
// manifest, ledger, and managed catalog exactly match this development build.
func AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return fmt.Errorf("current app ACL runtime admission has no PostgreSQL pool")
	}
	return admitAppACLCurrentRuntimeWithDependencies(
		ctx,
		migrations.FS,
		appACLCurrentMigrationFragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				return db.BeginTx(ctx, options)
			},
			readManifest:  readAppACLManifestRuntimeSnapshotInTxV1,
			readCatalog:   readAppACLEffectiveCatalogSnapshotInTx,
			verifyCatalog: verifyAppACLEffectiveCatalogSnapshot,
		},
	)
}

func admitAppACLCurrentRuntimeWithDependencies(
	ctx context.Context,
	migrationFS fs.FS,
	fragments []AppACLCurrentMigrationFragment,
	dependencies appACLCurrentRuntimeAdmissionDependencies,
) error {
	if migrationFS == nil {
		return fmt.Errorf("current app ACL runtime admission embedded migration filesystem is nil")
	}
	source, err := compileAppACLCurrentSourceContract(migrationFS, fragments)
	if err != nil {
		return fmt.Errorf("compile current app ACL runtime source contract: %w", err)
	}
	if err := dependencies.validate(); err != nil {
		return err
	}

	tx, err := dependencies.beginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("begin current app ACL runtime admission transaction: %w", err)
	}
	if tx == nil {
		return fmt.Errorf("begin current app ACL runtime admission transaction returned nil transaction")
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	manifestSnapshot, err := dependencies.readManifest(ctx, tx)
	if err != nil {
		return fmt.Errorf("read current app ACL runtime manifest: %w", err)
	}
	manifest, contract, err := verifyAppACLCurrentManifestRuntimeSnapshot(manifestSnapshot, source)
	if err != nil {
		return fmt.Errorf("verify current app ACL runtime manifest: %w", err)
	}
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, manifest.MigratorCatalogRole)
	if err != nil {
		return fmt.Errorf("build current app ACL runtime catalog verifier input: %w", err)
	}
	catalogSnapshot, err := dependencies.readCatalog(ctx, tx, input)
	if err != nil {
		return fmt.Errorf("read current app ACL runtime catalog: %w", err)
	}
	if err := validateAppACLRuntimeAdmissionSnapshotIdentity(manifestSnapshot, catalogSnapshot); err != nil {
		return err
	}
	if err := dependencies.verifyCatalog(catalogSnapshot, input); err != nil {
		return fmt.Errorf("verify current app ACL runtime catalog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit current app ACL runtime admission transaction: %w", err)
	}
	return nil
}

func (dependencies appACLCurrentRuntimeAdmissionDependencies) validate() error {
	checks := []struct {
		name    string
		missing bool
	}{
		{name: "transaction opener", missing: dependencies.beginTx == nil},
		{name: "manifest reader", missing: dependencies.readManifest == nil},
		{name: "catalog reader", missing: dependencies.readCatalog == nil},
		{name: "catalog verifier", missing: dependencies.verifyCatalog == nil},
	}
	for _, check := range checks {
		if check.missing {
			return fmt.Errorf("current app ACL runtime admission %s is nil", check.name)
		}
	}
	return nil
}
