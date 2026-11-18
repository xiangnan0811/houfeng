package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

// ProjectComposeMembershipV2 verifies the executable's current APP contract
// and projects the signed Compose successor in the same serializable transaction.
// A returned receipt is usable only because the transaction has committed.
func ProjectComposeMembershipV2(ctx context.Context, db *pgxpool.Pool, roles platformmigrate.AppRoleSetV2, activation, rotation []byte) ([]byte, error) {
	if db == nil {
		return nil, fmt.Errorf("Compose inventory has no PostgreSQL pool")
	}
	if err := roles.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Compose inventory roles: %w", err)
	}
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		return nil, fmt.Errorf("compile Compose inventory source: %w", err)
	}
	transitions, err := compileAppACLCurrentTransitions(source, appACLCurrentTransitionDefinitions)
	if err != nil {
		return nil, fmt.Errorf("compile Compose inventory transitions: %w", err)
	}
	var receipt []byte
	err = retryAppACLConvergence(ctx, func() error {
		tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("begin Compose inventory transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, appACLConvergenceHardenedSearchPathSQL); err != nil {
			return fmt.Errorf("set Compose inventory search path: %w", err)
		}
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, appACLSchemaAdvisoryLockV1); err != nil {
			return fmt.Errorf("lock Compose inventory schema: %w", err)
		}
		if err := lockAppACLConvergenceLedgerInTx(ctx, tx); err != nil {
			return fmt.Errorf("lock Compose inventory migration ledger: %w", err)
		}
		if _, err := readAppACLManifestHeadForUpdateV1(ctx, tx); err != nil {
			return fmt.Errorf("lock Compose inventory manifest: %w", err)
		}
		snapshot, err := readAppACLManifestRuntimeSnapshotInTxV1(ctx, tx)
		if err != nil {
			return err
		}
		if snapshot.SessionUser != roles.Migrator || snapshot.CurrentUser != roles.Migrator {
			return fmt.Errorf("Compose inventory requires direct migrator login")
		}
		envelope, err := validateAppACLManifestRuntimeEnvelope(snapshot)
		if err != nil {
			return err
		}
		if envelope.Latest.MigratorCatalogRole != roles.Migrator || envelope.Version != AppACLPrivilegeSetV2 {
			return fmt.Errorf("Compose inventory manifest migrator or privilege version mismatch")
		}
		bindings := []AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: roles.CenterRuntime},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: roles.PlatformAdmin},
			{Subject: AppACLSubjectRecordDeletionWorker, CatalogRole: roles.DeletionWorker},
		}
		contract, err := compileAppACLCurrentCatalogContract(source, snapshot.DatabaseName, bindings, roles.Migrator)
		if err != nil {
			return fmt.Errorf("compile Compose inventory catalog: %w", err)
		}
		privileges, err := canonicalPrivilegeSetBodyForBindings(contract.RoleBindings, contract.Privileges)
		if err != nil {
			return fmt.Errorf("compile Compose inventory privileges: %w", err)
		}
		shape, err := classifyAppACLCurrentManifestShape(source, transitions, snapshot.AppliedMigrations, snapshot.Manifests, snapshot.Head, privileges, roles.Migrator, snapshot.DatabaseName, contract.RoleBindings)
		if err != nil {
			return fmt.Errorf("verify Compose inventory manifest: %w", err)
		}
		if shape.kind == appACLCurrentManifestShapePredecessor {
			return fmt.Errorf("Compose inventory requires current successor manifest")
		}
		input, err := newAppACLEffectiveCatalogVerifierInput(contract, roles.Migrator)
		if err != nil {
			return err
		}
		catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
		if err != nil {
			return fmt.Errorf("read Compose inventory catalog: %w", err)
		}
		if err := verifyAppACLEffectiveCatalogSnapshot(catalog, input); err != nil {
			return fmt.Errorf("verify Compose inventory catalog: %w", err)
		}
		var tentative []byte
		if err := tx.QueryRow(ctx, `select public.record_platform_compose_inventory_upgrade($1::bytea,$2::bytea)`, activation, rotation).Scan(&tentative); err != nil {
			return fmt.Errorf("project Compose inventory: %w", err)
		}
		if len(tentative) != 32 {
			return fmt.Errorf("Compose inventory returned invalid receipt")
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit Compose inventory transaction: %w", err)
		}
		receipt = tentative
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
