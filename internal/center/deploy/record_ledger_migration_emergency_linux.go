//go:build linux

package deploy

import (
	"context"
	"os"
	"reflect"
	"sort"
)

// This is authority-decreasing only. It is used when root acknowledgement or
// phase publication has failed and never publishes a terminal record.
func ledgerMigrationEmergencyCleanup(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, original ledgerAppInventory, recovering bool) error {
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var ready ledgerMigrationReady
	readyFound, err := ledgerMigrationRead(f.control, "ready", intent.PhaseGeneration, &ready)
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var child *ledgerPredecessorBackend
	if readyFound {
		if ready.Version != 1 || ready.PhaseGeneration != intent.PhaseGeneration || ready.IntentDigest != intentDigest || ready.Backend.PID <= 0 || ready.Backend.Started.IsZero() || ready.Backend.Role != intent.MigratorRole {
			return ledgerMigrationRevocationFailure(f.control, intent)
		}
		child = &ready.Backend
	}
	if ledgerMigrationScope(intent.Cgroup, os.Getpid()) != nil || ledgerMigrationFenceBackends(ctx, f.app, original, intent.Bootstrap, child, recovering) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var preparation ledgerMigrationPreparation
	prepared, err := ledgerMigrationRead(f.control, "preparation", intent.PhaseGeneration, &preparation)
	if err != nil || prepared && ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var worker bool
	if f.app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&worker) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if !worker {
		actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(original))
		target := ledgerAppExpectedRoles(original, ledgerAppTarget)
		emergency := ledgerMigrationEmergencyTarget(target, original.Admin)
		if err != nil {
			return ledgerMigrationRevocationFailure(f.control, intent)
		}
		if reflect.DeepEqual(actual, emergency) {
			return nil
		}
		if !reflect.DeepEqual(actual, target) && !reflect.DeepEqual(actual, ledgerAppExpectedRoles(original, ledgerAppTargetAdminClosed)) {
			return ledgerMigrationRevocationFailure(f.control, intent)
		}
		if ledgerPredecessorApplyRoles(ctx, f.app, original, actual, emergency) != nil {
			return ledgerMigrationRevocationFailure(f.control, intent)
		}
		return nil
	}
	if !prepared {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	previous, _, _, _, err := ledgerMigrationObservationPrefix(f.control, intent, preparation, original)
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if len(previous) != 0 {
		return ledgerMigrationObservationEmergency(ctx, f, intent, original, preparation, previous[len(previous)-1])
	}
	names := append(ledgerAppRoleNames(original), composeDeletionWorkerRole)
	sort.Strings(names)
	actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, names)
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if reflect.DeepEqual(actual, preparation.EmergencyTarget) {
		return nil
	}
	if !reflect.DeepEqual(actual, preparation.Normal) && ledgerMigrationPostRoles(preparation.Normal, actual) != nil && !reflect.DeepEqual(actual, preparation.FinalTarget) {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, actual)
	if ledgerPredecessorApplyRoles(ctx, f.app, inventory, actual, preparation.EmergencyTarget) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	return nil
}
