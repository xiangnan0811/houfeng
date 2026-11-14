//go:build linux

package deploy

import (
	"fmt"
	"reflect"
	"strings"

	"houfeng/internal/center/store/migrate"
)

// Observation records are private maintenance records. The normal record is a
// fresh catalog observation, never a replacement for the original normal.
type ledgerMigrationObservationIntent struct {
	Version                      uint64                          `json:"version"`
	PhaseGeneration              uint64                          `json:"phase_generation"`
	Attempt                      uint64                          `json:"attempt"`
	RecoveryID                   string                          `json:"recovery_id"`
	Application                  RecordLedgerApplicationIdentity `json:"application"`
	IntentDigest                 string                          `json:"intent_digest"`
	PreparationDigest            string                          `json:"preparation_digest"`
	PreviousAttemptSettledDigest *string                         `json:"previous_attempt_settled_digest"`
	Bootstrap                    ledgerPredecessorBackend        `json:"bootstrap"`
	WorkerOID                    uint32                          `json:"worker_oid"`
	ManifestDigest               string                          `json:"manifest_digest"`
	CatalogDigest                string                          `json:"catalog_digest"`
	From                         ledgerAppRoleSnapshot           `json:"from"`
	To                           ledgerAppRoleSnapshot           `json:"to"`
	FinalTarget                  ledgerAppRoleSnapshot           `json:"final_target"`
	EmergencyTarget              ledgerAppRoleSnapshot           `json:"emergency_target"`
}

type ledgerMigrationObservationNormal struct {
	Version                 uint64             `json:"version"`
	PhaseGeneration         uint64             `json:"phase_generation"`
	Attempt                 uint64             `json:"attempt"`
	IntentDigest            string             `json:"intent_digest"`
	ObservationIntentDigest string             `json:"observation_intent_digest"`
	Inventory               ledgerAppInventory `json:"inventory"`
}

type ledgerMigrationObservationRestriction struct {
	Version                 uint64                `json:"version"`
	PhaseGeneration         uint64                `json:"phase_generation"`
	Attempt                 uint64                `json:"attempt"`
	IntentDigest            string                `json:"intent_digest"`
	ObservationIntentDigest string                `json:"observation_intent_digest"`
	From                    ledgerAppRoleSnapshot `json:"from"`
	To                      ledgerAppRoleSnapshot `json:"to"`
}

type ledgerMigrationObservationSettled struct {
	Version                 uint64  `json:"version"`
	PhaseGeneration         uint64  `json:"phase_generation"`
	Attempt                 uint64  `json:"attempt"`
	IntentDigest            string  `json:"intent_digest"`
	ObservationIntentDigest string  `json:"observation_intent_digest"`
	RestrictionDigest       string  `json:"restriction_digest"`
	Outcome                 string  `json:"outcome"`
	NormalDigest            *string `json:"normal_digest"`
}

func ledgerMigrationObservationName(kind string, generation, attempt uint64) string {
	return fmt.Sprintf("migration-observe-%s-%020d-%020d.json", kind, generation, attempt)
}

func ledgerMigrationReadObservation(control int, kind string, generation, attempt uint64, target any) (bool, error) {
	if generation == 0 || attempt == 0 || (kind != "intent" && kind != "normal" && kind != "restrict" && kind != "settled") {
		return false, ErrRecordLedgerFence
	}
	body, found, err := fenceReadFile(control, ledgerMigrationObservationName(kind, generation, attempt))
	if err != nil || !found {
		return found, err
	}
	if fenceDecode(body, target) != nil {
		return false, ErrRecordLedgerFence
	}
	return true, nil
}

func ledgerMigrationValidateObservationIntent(observation ledgerMigrationObservationIntent, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) error {
	intentDigest, e1 := ledgerMigrationDigest(intent)
	preparationDigest, e2 := ledgerMigrationDigest(preparation)
	worker, ok := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
	if e1 != nil || e2 != nil || ledgerMigrationValidatePreparation(preparation, intent, original) != nil || !ok ||
		observation.Version != 1 || observation.PhaseGeneration != intent.PhaseGeneration || observation.Attempt == 0 ||
		observation.RecoveryID != intent.RecoveryID || observation.Application != intent.Application ||
		observation.IntentDigest != intentDigest || observation.PreparationDigest != preparationDigest ||
		observation.Bootstrap.Role != intent.Bootstrap.Role || observation.Bootstrap.PID <= 0 || observation.Bootstrap.Started.IsZero() || observation.Bootstrap.Started.Nanosecond()%1000 != 0 ||
		observation.WorkerOID != worker.OID || (observation.Attempt == 1) != (observation.PreviousAttemptSettledDigest == nil) ||
		!reflect.DeepEqual(observation.From, preparation.FinalTarget) && !reflect.DeepEqual(observation.From, preparation.EmergencyTarget) ||
		!reflect.DeepEqual(observation.FinalTarget, preparation.FinalTarget) || !reflect.DeepEqual(observation.EmergencyTarget, preparation.EmergencyTarget) {
		return ErrRecordLedgerFence
	}
	for _, digest := range []string{observation.ManifestDigest, observation.CatalogDigest} {
		if _, valid := ledgerC80DigestBytes(digest); !valid {
			return ErrRecordLedgerFence
		}
	}
	if observation.PreviousAttemptSettledDigest != nil {
		if _, valid := ledgerC80DigestBytes(*observation.PreviousAttemptSettledDigest); !valid {
			return ErrRecordLedgerFence
		}
	}
	// Only the post-convergence worker CONNECT differs structurally from the
	// preparation's normal state; every non-bootstrap verifier must be fresh.
	comparison := observation.To
	comparison.Roles = append([]ledgerAppRole(nil), observation.To.Roles...)
	seen := map[string]bool{}
	for i := range comparison.Roles {
		role := &comparison.Roles[i]
		prepared, found := ledgerAppRoleByName(preparation.Normal, role.Name)
		if !found {
			return ErrRecordLedgerFence
		}
		if role.Name != original.Bootstrap {
			if role.Verifier == nil || !strings.HasPrefix(*role.Verifier, "SCRAM-SHA-256$") || seen[*role.Verifier] || reflect.DeepEqual(role.Verifier, prepared.Verifier) {
				return ErrRecordLedgerFence
			}
			for _, baseline := range original.RoleBaseline.Roles {
				if reflect.DeepEqual(role.Verifier, baseline.Verifier) {
					return ErrRecordLedgerFence
				}
			}
			for _, target := range append(append([]ledgerAppRoleTarget{}, original.Targets...), preparation.WorkerTarget) {
				if *role.Verifier == target.Verifier || ledgerMatchesSCRAM(*role.Verifier, target.Secret) {
					return ErrRecordLedgerFence
				}
			}
			seen[*role.Verifier] = true
			role.Verifier = prepared.Verifier
		}
	}
	if ledgerMigrationPostRoles(preparation.Normal, comparison) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerMigrationExpectedFencedCatalog(inventory ledgerAppInventory, emergency bool) migrate.AppACLEffectiveCatalogSnapshotR1 {
	state := ledgerAppTarget
	if emergency {
		state = ledgerAppTargetAdminClosed
	}
	fenced := ledgerAppExpectedCatalog(inventory, state)
	if emergency {
		filter := func(input []migrate.AppACLEffectiveCatalogPrivilegeObservationR1) []migrate.AppACLEffectiveCatalogPrivilegeObservationR1 {
			output := make([]migrate.AppACLEffectiveCatalogPrivilegeObservationR1, 0, len(input))
			for _, privilege := range input {
				if privilege.Grantee != inventory.Admin || privilege.ObjectClass != migrate.AppACLObjectClassDatabase || privilege.Privilege != migrate.AppACLPrivilegeConnect {
					output = append(output, privilege)
				}
			}
			return output
		}
		fenced.DirectPrivileges = filter(fenced.DirectPrivileges)
		fenced.EffectivePrivileges = filter(fenced.EffectivePrivileges)
	}
	return fenced
}

func ledgerMigrationValidateObservationNormal(normal ledgerMigrationObservationNormal, observation ledgerMigrationObservationIntent, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) error {
	digest, err := ledgerMigrationDigest(observation)
	if err != nil || normal.Version != 1 || normal.PhaseGeneration != intent.PhaseGeneration || normal.Attempt != observation.Attempt || normal.IntentDigest != observation.IntentDigest || normal.ObservationIntentDigest != digest || !reflect.DeepEqual(normal.Inventory.RoleBaseline, observation.To) {
		return ErrRecordLedgerFence
	}
	if normal.Inventory.Baseline.Manifest.Head == nil || hexDigest(normal.Inventory.Baseline.Manifest.Head.ManifestDigest) != observation.ManifestDigest {
		return ErrRecordLedgerFence
	}
	fencedCatalog := ledgerMigrationExpectedFencedCatalog(normal.Inventory, reflect.DeepEqual(observation.From, observation.EmergencyTarget))
	catalogDigest, err := ledgerMigrationDigest(fencedCatalog)
	if err != nil || catalogDigest != observation.CatalogDigest {
		return ErrRecordLedgerFence
	}
	canonical := normal.Inventory
	canonical.RoleBaseline.Roles = append([]ledgerAppRole(nil), canonical.RoleBaseline.Roles...)
	for i := range canonical.RoleBaseline.Roles {
		role, ok := ledgerAppRoleByName(preparation.Normal, canonical.RoleBaseline.Roles[i].Name)
		if !ok {
			return ErrRecordLedgerFence
		}
		canonical.RoleBaseline.Roles[i].Verifier = role.Verifier
	}
	legacy := ledgerMigrationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: observation.IntentDigest, PreparationDigest: observation.PreparationDigest, Inventory: canonical}
	return ledgerMigrationValidateNormal(legacy, intent, preparation, original)
}

// Select only a complete settled observation and validate every prior attempt.
func ledgerMigrationSelectObservation(control int, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory, attempt uint64) (ledgerMigrationObservationNormal, error) {
	var selected ledgerMigrationObservationNormal
	names, err := ledgerNames(control)
	if err != nil || attempt == 0 || attempt > uint64(len(names)) {
		return selected, ErrRecordLedgerFence
	}
	var previous *string
	seenVerifiers := map[string]bool{}
	for n := uint64(1); n <= attempt; n++ {
		var observation ledgerMigrationObservationIntent
		found, err := ledgerMigrationReadObservation(control, "intent", intent.PhaseGeneration, n, &observation)
		if err != nil || !found || ledgerMigrationValidateObservationIntent(observation, intent, preparation, original) != nil || !reflect.DeepEqual(observation.PreviousAttemptSettledDigest, previous) {
			return selected, ErrRecordLedgerFence
		}
		for _, role := range observation.To.Roles {
			if role.Name == original.Bootstrap {
				continue
			}
			if role.Verifier == nil || seenVerifiers[*role.Verifier] {
				return selected, ErrRecordLedgerFence
			}
			seenVerifiers[*role.Verifier] = true
		}
		observationDigest, err := ledgerMigrationDigest(observation)
		if err != nil {
			return selected, ErrRecordLedgerFence
		}
		var restriction ledgerMigrationObservationRestriction
		found, err = ledgerMigrationReadObservation(control, "restrict", intent.PhaseGeneration, n, &restriction)
		if err != nil || !found || restriction.Version != 1 || restriction.PhaseGeneration != intent.PhaseGeneration || restriction.Attempt != n || restriction.IntentDigest != observation.IntentDigest || restriction.ObservationIntentDigest != observationDigest || !reflect.DeepEqual(restriction.From, observation.To) || (!reflect.DeepEqual(restriction.To, observation.FinalTarget) && !reflect.DeepEqual(restriction.To, observation.EmergencyTarget)) {
			return selected, ErrRecordLedgerFence
		}
		restrictionDigest, err := ledgerMigrationDigest(restriction)
		if err != nil {
			return selected, ErrRecordLedgerFence
		}
		var settled ledgerMigrationObservationSettled
		found, err = ledgerMigrationReadObservation(control, "settled", intent.PhaseGeneration, n, &settled)
		if err != nil || !found || settled.Version != 1 || settled.PhaseGeneration != intent.PhaseGeneration || settled.Attempt != n || settled.IntentDigest != observation.IntentDigest || settled.ObservationIntentDigest != observationDigest || settled.RestrictionDigest != restrictionDigest {
			return selected, ErrRecordLedgerFence
		}
		var normal ledgerMigrationObservationNormal
		normalFound, err := ledgerMigrationReadObservation(control, "normal", intent.PhaseGeneration, n, &normal)
		if err != nil {
			return selected, ErrRecordLedgerFence
		}
		switch settled.Outcome {
		case "grant_not_committed", "refenced_without_normal":
			if settled.NormalDigest != nil || normalFound {
				return selected, ErrRecordLedgerFence
			}
		case "normal_observed_refenced":
			if n != attempt || !normalFound || settled.NormalDigest == nil || ledgerMigrationValidateObservationNormal(normal, observation, intent, preparation, original) != nil {
				return selected, ErrRecordLedgerFence
			}
			digest, e := ledgerMigrationDigest(normal)
			if e != nil || digest != *settled.NormalDigest {
				return selected, ErrRecordLedgerFence
			}
			selected = normal
		default:
			return selected, ErrRecordLedgerFence
		}
		settledDigest, err := ledgerMigrationDigest(settled)
		if err != nil {
			return selected, ErrRecordLedgerFence
		}
		previous = &settledDigest
	}
	if selected.Attempt != attempt {
		return ledgerMigrationObservationNormal{}, ErrRecordLedgerFence
	}
	return selected, nil
}
