//go:build linux

package deploy

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errLedgerMigrationRevocationUnresolved = errors.New("migration callback credential revocation is unresolved")

type ledgerMigrationRestriction struct {
	Version           uint64                `json:"version"`
	PhaseGeneration   uint64                `json:"phase_generation"`
	IntentDigest      string                `json:"intent_digest"`
	PreparationDigest string                `json:"preparation_digest"`
	From              ledgerAppRoleSnapshot `json:"from"`
	To                ledgerAppRoleSnapshot `json:"to"`
}

type ledgerMigrationRevocationUnresolved struct {
	Version         uint64 `json:"version"`
	PhaseGeneration uint64 `json:"phase_generation"`
	IntentDigest    string `json:"intent_digest"`
	Outcome         string `json:"outcome"`
}

func ledgerMigrationRevocationFailure(control int, intent ledgerMigrationIntent) error {
	digest, err := ledgerMigrationDigest(intent)
	if err == nil {
		_, err = ledgerPublishExact(control, ledgerMigrationName("revocation-unresolved", intent.PhaseGeneration), ledgerMigrationRevocationUnresolved{
			Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: digest, Outcome: "revocation_unresolved"})
	}
	return errors.Join(ErrRecordLedgerFence, errLedgerMigrationRevocationUnresolved, err)
}

func ledgerMigrationRestrictionInventory(original ledgerAppInventory, preparation ledgerMigrationPreparation, from ledgerAppRoleSnapshot) ledgerAppInventory {
	inventory := original
	inventory.Previous = append(append([]string{}, original.Previous...), composeDeletionWorkerRole)
	sort.Strings(inventory.Previous)
	inventory.RoleBaseline = from
	inventory.Targets = append(append([]ledgerAppRoleTarget{}, original.Targets...), preparation.WorkerTarget)
	sort.Slice(inventory.Targets, func(i, j int) bool { return inventory.Targets[i].Role < inventory.Targets[j].Role })
	return inventory
}

func ledgerMigrationValidateRestriction(restriction ledgerMigrationRestriction, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) error {
	intentDigest, e1 := ledgerMigrationDigest(intent)
	preparationDigest, e2 := ledgerMigrationDigest(preparation)
	if e1 != nil || e2 != nil || restriction.Version != 1 || restriction.PhaseGeneration != intent.PhaseGeneration || restriction.IntentDigest != intentDigest ||
		restriction.PreparationDigest != preparationDigest || ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return ErrRecordLedgerFence
	}
	if !reflect.DeepEqual(restriction.From, preparation.Normal) && ledgerMigrationPostRoles(preparation.Normal, restriction.From) != nil {
		return ErrRecordLedgerFence
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, restriction.From)
	if !reflect.DeepEqual(restriction.To, ledgerAppExpectedRoles(inventory, ledgerAppTarget)) {
		return ErrRecordLedgerFence
	}
	return nil
}

// This transition uses only the exact journaled role/OID snapshot. It revokes
// callback verifiers and LOGIN/CONNECT even if catalog classification fails;
// the transition does not admit a catalog or grant business authority.
func ledgerMigrationRestrict(ctx context.Context, app *pgxpool.Pool, control int, intent ledgerMigrationIntent, original ledgerAppInventory, preparation *ledgerMigrationPreparation, checkpointer ledgerMaintenanceCheckpointer) (*ledgerMigrationRestriction, error) {
	var workerExists bool
	if app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&workerExists) != nil {
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	if !workerExists {
		actual, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(original))
		if err != nil || !reflect.DeepEqual(actual, ledgerAppExpectedRoles(original, ledgerAppTarget)) && !reflect.DeepEqual(actual, ledgerAppExpectedRoles(original, ledgerAppTargetAdminClosed)) && !reflect.DeepEqual(actual, ledgerMigrationEmergencyTarget(ledgerAppExpectedRoles(original, ledgerAppTarget), original.Admin)) {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
		return nil, nil
	}
	if preparation == nil || ledgerMigrationValidatePreparation(*preparation, intent, original) != nil {
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	names := append(ledgerAppRoleNames(original), composeDeletionWorkerRole)
	sort.Strings(names)
	actual, err := ledgerReadAppRoleSnapshot(ctx, app, names)
	if err != nil {
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	var restriction ledgerMigrationRestriction
	found, err := ledgerMigrationRead(control, "restrict", intent.PhaseGeneration, &restriction)
	if err != nil {
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	if found {
		if ledgerMigrationValidateRestriction(restriction, intent, *preparation, original) != nil {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
	} else {
		from := actual
		if reflect.DeepEqual(actual, preparation.FinalTarget) || reflect.DeepEqual(actual, preparation.EmergencyTarget) {
			catalog, roles, classifyErr := ledgerMigrationFencedCatalog(ctx, app, intent, preparation, original)
			if classifyErr != nil || !reflect.DeepEqual(roles, actual) {
				return nil, ledgerMigrationRevocationFailure(control, intent)
			}
			from = preparation.Normal
			if !catalog.LegacyC80 {
				from, classifyErr = ledgerMigrationConvergedRoles(*preparation)
				if classifyErr != nil {
					return nil, ledgerMigrationRevocationFailure(control, intent)
				}
			}
		} else if !reflect.DeepEqual(actual, preparation.Normal) && ledgerMigrationPostRoles(preparation.Normal, actual) != nil {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
		intentDigest, e1 := ledgerMigrationDigest(intent)
		preparationDigest, e2 := ledgerMigrationDigest(*preparation)
		if e1 != nil || e2 != nil {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
		inventory := ledgerMigrationRestrictionInventory(original, *preparation, from)
		restriction = ledgerMigrationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, PreparationDigest: preparationDigest,
			From: from, To: ledgerAppExpectedRoles(inventory, ledgerAppTarget)}
		if ledgerMigrationValidateRestriction(restriction, intent, *preparation, original) != nil {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
		if _, err := ledgerPublishExact(control, ledgerMigrationName("restrict", intent.PhaseGeneration), restriction); err != nil {
			return nil, ledgerMigrationRevocationFailure(control, intent)
		}
	}
	// Restriction is pre-authorized; if the root channel is unavailable, close
	// even the management login/CONNECT and leave the invocation unresolved.
	if ledgerReadExact(control, ledgerMigrationName("restrict", intent.PhaseGeneration), restriction) != nil || ledgerMigrationCheckpointRecord(ctx, checkpointer, intent, 0, "restrict", restriction) != nil {
		inventory := ledgerMigrationRestrictionInventory(original, *preparation, restriction.From)
		emergency := preparation.EmergencyTarget
		if reflect.DeepEqual(actual, restriction.From) {
			_ = ledgerPredecessorApplyRoles(ctx, app, inventory, actual, emergency)
		} else if reflect.DeepEqual(actual, restriction.To) && !reflect.DeepEqual(actual, emergency) {
			_ = ledgerPredecessorApplyRoles(ctx, app, inventory, actual, emergency)
		}
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	if reflect.DeepEqual(actual, restriction.From) {
		inventory := ledgerMigrationRestrictionInventory(original, *preparation, restriction.From)
		if err := ledgerPredecessorApplyRoles(ctx, app, inventory, restriction.From, restriction.To); err != nil {
			// A lost COMMIT response is settled against the exact target snapshot.
			actual, err = ledgerReadAppRoleSnapshot(ctx, app, names)
			if err != nil || !reflect.DeepEqual(actual, restriction.To) {
				return nil, ledgerMigrationRevocationFailure(control, intent)
			}
		}
	} else if !reflect.DeepEqual(actual, restriction.To) && !reflect.DeepEqual(actual, preparation.EmergencyTarget) {
		return nil, ledgerMigrationRevocationFailure(control, intent)
	}
	return &restriction, nil
}
