//go:build linux

package deploy

import (
	"context"
	"errors"
	"reflect"

	"houfeng/internal/center/store/migrate"
)

// An unknown grant outcome is never replayed. Cleanup recognizes only the
// journaled from/to snapshots and retains the original attempt for recovery.
func ledgerMigrationObservationEmergency(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, original ledgerAppInventory, preparation ledgerMigrationPreparation, observation ledgerMigrationObservationIntent) error {
	current, err := ledgerCurrentAppSession(ctx, f.app)
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	same := current.PID == observation.Bootstrap.PID && current.Role == observation.Bootstrap.Role && current.Started.Equal(observation.Bootstrap.Started)
	if ledgerMigrationFenceBackends(ctx, f.app, original, observation.Bootstrap, nil, !same) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, observation.To)
	actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(inventory))
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if reflect.DeepEqual(actual, observation.EmergencyTarget) {
		return nil
	}
	if !reflect.DeepEqual(actual, observation.To) && !reflect.DeepEqual(actual, observation.From) && !reflect.DeepEqual(actual, observation.FinalTarget) {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if err := ledgerPredecessorApplyRoles(ctx, f.app, inventory, actual, observation.EmergencyTarget); err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	return nil
}

func ledgerMigrationFreshObservation(ctx, cleanup context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory, catalog migrate.AppACLMaintenanceSnapshot, from ledgerAppRoleSnapshot, previous []ledgerMigrationObservationIntent, previousSettled *string) (result ledgerMigrationObservationNormal, resultErr error) {
	if catalog.LegacyC80 || catalog.Manifest.Head == nil || (!reflect.DeepEqual(from, preparation.FinalTarget) && !reflect.DeepEqual(from, preparation.EmergencyTarget)) {
		return result, ErrRecordLedgerFence
	}
	attempt := uint64(len(previous)) + 1
	to, err := ledgerMigrationFreshRoles(preparation, original, previous)
	if err != nil {
		return result, err
	}
	bootstrap, err := ledgerCurrentAppSession(ctx, f.app)
	if err != nil || bootstrap.Role != original.Bootstrap {
		return result, ErrRecordLedgerFence
	}
	intentDigest, e1 := ledgerMigrationDigest(intent)
	preparationDigest, e2 := ledgerMigrationDigest(preparation)
	catalogDigest, e3 := ledgerMigrationDigest(catalog.Catalog)
	worker, found := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
	if e1 != nil || e2 != nil || e3 != nil || !found {
		return result, ErrRecordLedgerFence
	}
	observation := ledgerMigrationObservationIntent{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, RecoveryID: intent.RecoveryID, Application: intent.Application,
		IntentDigest: intentDigest, PreparationDigest: preparationDigest, PreviousAttemptSettledDigest: previousSettled,
		Bootstrap: ledgerPredecessorBackend{PID: bootstrap.PID, Started: bootstrap.Started, Role: bootstrap.Role}, WorkerOID: worker.OID,
		ManifestDigest: hexDigest(catalog.Manifest.Head.ManifestDigest), CatalogDigest: catalogDigest, From: from, To: to,
		FinalTarget: preparation.FinalTarget, EmergencyTarget: preparation.EmergencyTarget}
	if ledgerMigrationValidateObservationIntent(observation, intent, preparation, original) != nil {
		return result, ErrRecordLedgerFence
	}
	possiblyGranted := true
	defer func() {
		if possiblyGranted && resultErr != nil {
			if err := ledgerMigrationObservationEmergency(cleanup, f, intent, original, preparation, observation); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "intent", observation); err != nil {
		return result, err
	}
	if err := ledgerMigrationObservationGrant(ctx, f, original, preparation, observation, catalog); err != nil {
		return result, err
	}
	actual, err := ledgerMigrationObserveNormalRoles(ctx, f.app, intent, preparation, original, &observation.To)
	if err != nil || actual.Inventory.ManifestDigest != catalog.Manifest.Head.ManifestDigest {
		return result, errors.Join(ErrRecordLedgerFence, err)
	}
	observationDigest, err := ledgerMigrationDigest(observation)
	if err != nil {
		return result, err
	}
	result = ledgerMigrationObservationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, Inventory: actual.Inventory}
	if ledgerMigrationValidateObservationNormal(result, observation, intent, preparation, original) != nil {
		return ledgerMigrationObservationNormal{}, ErrRecordLedgerFence
	}
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "normal", result); err != nil {
		return ledgerMigrationObservationNormal{}, err
	}
	restriction := ledgerMigrationObservationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, From: observation.To, To: observation.FinalTarget}
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "restrict", restriction); err != nil {
		return ledgerMigrationObservationNormal{}, err
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, observation.To)
	if err := ledgerPredecessorApplyRoles(ctx, f.app, inventory, restriction.From, restriction.To); err != nil {
		return ledgerMigrationObservationNormal{}, err
	}
	actualRoles, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actualRoles, restriction.To) {
		return ledgerMigrationObservationNormal{}, ErrRecordLedgerFence
	}
	restrictionDigest, e1 := ledgerMigrationDigest(restriction)
	normalDigest, e2 := ledgerMigrationDigest(result)
	if e1 != nil || e2 != nil {
		return ledgerMigrationObservationNormal{}, ErrRecordLedgerFence
	}
	settled := ledgerMigrationObservationSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, RestrictionDigest: restrictionDigest, Outcome: "normal_observed_refenced", NormalDigest: &normalDigest}
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "settled", settled); err != nil {
		return ledgerMigrationObservationNormal{}, err
	}
	possiblyGranted = false
	return result, nil
}
