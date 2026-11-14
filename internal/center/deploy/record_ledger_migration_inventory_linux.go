//go:build linux

package deploy

import (
	"context"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/store/migrate"
)

// Capture the actual normal current catalog while the child is waiting for its
// result ACK. The catalog verifier also checks the compiled successor and its
// exact registered predecessor chain in one repeatable-read snapshot.
func ledgerMigrationObserveNormal(ctx context.Context, app *pgxpool.Pool, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) (ledgerMigrationNormal, error) {
	return ledgerMigrationObserveNormalRoles(ctx, app, intent, preparation, original, nil)
}

func ledgerMigrationObserveNormalRoles(ctx context.Context, app *pgxpool.Pool, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory, expected *ledgerAppRoleSnapshot) (ledgerMigrationNormal, error) {
	var result ledgerMigrationNormal
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil || ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return result, ErrRecordLedgerFence
	}
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	observed, err := migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, nil)
	if err != nil || observed.LegacyC80 || observed.Manifest.Head == nil || observed.Manifest.DatabaseName != intent.Application.DatabaseName ||
		observed.Manifest.SessionUser != intent.Bootstrap.Role || observed.Manifest.CurrentUser != intent.Bootstrap.Role {
		return result, ErrRecordLedgerFence
	}
	before := original.Baseline.Manifest.Manifests
	after := observed.Manifest.Manifests
	if len(before) == 0 || len(after) != len(before)+1 || !reflect.DeepEqual(after[:len(before)], before) {
		return result, ErrRecordLedgerFence
	}
	latest := after[len(after)-1]
	if latest.PreviousManifestDigest != original.ManifestDigest || latest.MigratorCatalogRole != intent.MigratorRole || latest.ManifestDigest == ([32]byte{}) {
		return result, ErrRecordLedgerFence
	}
	inventory := original
	inventory.LegacyC80 = false
	inventory.OriginalPrevious = append([]string{}, original.Previous...)
	inventory.Previous = append(append([]string{}, original.Previous...), intent.WorkerRole)
	sort.Strings(inventory.Previous)
	inventory.Baseline = observed
	inventory.ManifestDigest = latest.ManifestDigest
	inventory.Targets = append(append([]ledgerAppRoleTarget{}, original.Targets...), preparation.WorkerTarget)
	sort.Slice(inventory.Targets, func(i, j int) bool { return inventory.Targets[i].Role < inventory.Targets[j].Role })
	inventory.RoleBaseline, err = ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || expected == nil && ledgerMigrationPostRoles(preparation.Normal, inventory.RoleBaseline) != nil ||
		expected != nil && !reflect.DeepEqual(*expected, inventory.RoleBaseline) ||
		ledgerValidateAppJournal(inventory, inventory.Credential) != nil || ledgerAppPrincipalMatches(ctx, tx, inventory, observed) != nil {
		return result, ErrRecordLedgerFence
	}
	if tx.Commit(ctx) != nil {
		return result, ErrRecordLedgerFence
	}
	preparationDigest, err := ledgerMigrationDigest(preparation)
	if err != nil {
		return result, err
	}
	return ledgerMigrationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, PreparationDigest: preparationDigest, Inventory: inventory}, nil
}

func ledgerMigrationValidateNormal(normal ledgerMigrationNormal, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) error {
	intentDigest, e1 := ledgerMigrationDigest(intent)
	preparationDigest, e2 := ledgerMigrationDigest(preparation)
	inv := normal.Inventory
	if e1 != nil || e2 != nil || ledgerMigrationValidatePreparation(preparation, intent, original) != nil || normal.Version != 1 || normal.PhaseGeneration != intent.PhaseGeneration || normal.IntentDigest != intentDigest || normal.PreparationDigest != preparationDigest ||
		inv.LegacyC80 || inv.RecoveryID != original.RecoveryID || inv.Application != original.Application || inv.Bootstrap != original.Bootstrap || inv.Admin != original.Admin ||
		!reflect.DeepEqual(inv.OriginalPrevious, original.Previous) || !reflect.DeepEqual(inv.Credential, original.Credential) ||
		ledgerValidateAppJournal(inv, inv.Credential) != nil || ledgerMigrationPostRoles(preparation.Normal, inv.RoleBaseline) != nil {
		return ErrRecordLedgerFence
	}
	targets := append(append([]ledgerAppRoleTarget{}, original.Targets...), preparation.WorkerTarget)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Role < targets[j].Role })
	if !reflect.DeepEqual(inv.Targets, targets) {
		return ErrRecordLedgerFence
	}
	before, after := original.Baseline.Manifest.Manifests, inv.Baseline.Manifest.Manifests
	if len(before) == 0 || len(after) != len(before)+1 || !reflect.DeepEqual(after[:len(before)], before) ||
		after[len(after)-1].PreviousManifestDigest != original.ManifestDigest || after[len(after)-1].ManifestDigest != inv.ManifestDigest ||
		after[len(after)-1].MigratorCatalogRole != intent.MigratorRole {
		return ErrRecordLedgerFence
	}
	return nil
}

// A missing normal record is never evidence that the catalog is still C80.
func ledgerMigrationFencedCatalog(ctx context.Context, app *pgxpool.Pool, intent ledgerMigrationIntent, preparation *ledgerMigrationPreparation, original ledgerAppInventory) (migrate.AppACLMaintenanceSnapshot, ledgerAppRoleSnapshot, error) {
	var empty migrate.AppACLMaintenanceSnapshot
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	var workerExists bool
	if tx.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&workerExists) != nil || workerExists && preparation == nil {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	names := ledgerAppRoleNames(original)
	if workerExists {
		names = append(names, composeDeletionWorkerRole)
		sort.Strings(names)
	}
	roles, err := ledgerReadAppRoles(ctx, tx, names)
	if err != nil {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	if !workerExists {
		if !reflect.DeepEqual(roles, ledgerAppExpectedRoles(original, ledgerAppTarget)) && !reflect.DeepEqual(roles, ledgerAppExpectedRoles(original, ledgerAppTargetAdminClosed)) && !reflect.DeepEqual(roles, ledgerMigrationEmergencyTarget(ledgerAppExpectedRoles(original, ledgerAppTarget), original.Admin)) {
			return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
	} else if !reflect.DeepEqual(roles, preparation.FinalTarget) && !reflect.DeepEqual(roles, preparation.EmergencyTarget) {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	states := make([]migrate.AppACLMaintenanceRoleState, 0, len(original.Baseline.Catalog.Roles)+1)
	for _, catalogRole := range original.Baseline.Catalog.Roles {
		if catalogRole.Name == original.Bootstrap {
			continue
		}
		role, ok := ledgerAppRoleByName(roles, catalogRole.Name)
		if !ok {
			return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		states = append(states, migrate.AppACLMaintenanceRoleState{Role: role.Name, Login: role.Login, Connect: role.Connect})
	}
	observed, err := migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, states)
	if err != nil && workerExists {
		worker, ok := ledgerAppRoleByName(roles, composeDeletionWorkerRole)
		if !ok {
			return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		states = append(states, migrate.AppACLMaintenanceRoleState{Role: worker.Name, Login: worker.Login, Connect: worker.Connect})
		observed, err = migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, states)
	}
	if err != nil || observed.Manifest.DatabaseName != intent.Application.DatabaseName || observed.Manifest.SessionUser != original.Bootstrap || observed.Manifest.CurrentUser != original.Bootstrap {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	before, after := original.Baseline.Manifest.Manifests, observed.Manifest.Manifests
	if len(before) == 0 || observed.LegacyC80 && !reflect.DeepEqual(after, before) || !observed.LegacyC80 && (len(after) != len(before)+1 || !reflect.DeepEqual(after[:len(before)], before) || after[len(after)-1].PreviousManifestDigest != original.ManifestDigest || after[len(after)-1].MigratorCatalogRole != intent.MigratorRole) {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	principalInventory := original
	if !observed.LegacyC80 {
		if !workerExists {
			return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		principalInventory.Previous = append(append([]string{}, original.Previous...), composeDeletionWorkerRole)
		sort.Strings(principalInventory.Previous)
	}
	if ledgerAppPrincipalMatches(ctx, tx, principalInventory, observed) != nil {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	if tx.Commit(ctx) != nil {
		return empty, ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	return observed, roles, nil
}
