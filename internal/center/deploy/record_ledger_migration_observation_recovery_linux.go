//go:build linux

package deploy

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

// Observation files are an append-only, contiguous attempt chain. Unknown
// names and partial earlier attempts cannot be skipped to start a new grant.
func ledgerMigrationObservationPrefix(control int, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) ([]ledgerMigrationObservationIntent, *string, *ledgerMigrationObservationNormal, bool, error) {
	names, err := ledgerNames(control)
	if err != nil {
		return nil, nil, nil, false, err
	}
	var max uint64
	for _, name := range names {
		if !strings.HasPrefix(name, "migration-observe-") {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(name, ".json"), "-")
		if len(parts) != 5 || !strings.HasSuffix(name, ".json") {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		kind := parts[2]
		generation, e1 := strconv.ParseUint(parts[3], 10, 64)
		attempt, e2 := strconv.ParseUint(parts[4], 10, 64)
		if e1 != nil || e2 != nil || attempt == 0 || generation == 0 || (kind != "intent" && kind != "normal" && kind != "restrict" && kind != "settled") || name != ledgerMigrationObservationName(kind, generation, attempt) {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		if generation != intent.PhaseGeneration {
			continue
		}
		if attempt > uint64(len(names)) {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		if attempt > max {
			max = attempt
		}
	}
	previous := make([]ledgerMigrationObservationIntent, 0, max)
	var previousDigest *string
	var selected *ledgerMigrationObservationNormal
	seen := map[string]bool{}
	for n := uint64(1); n <= max; n++ {
		var observation ledgerMigrationObservationIntent
		found, err := ledgerMigrationReadObservation(control, "intent", intent.PhaseGeneration, n, &observation)
		if err != nil || !found || ledgerMigrationValidateObservationIntent(observation, intent, preparation, original) != nil || !reflect.DeepEqual(observation.PreviousAttemptSettledDigest, previousDigest) {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		for _, role := range observation.To.Roles {
			if role.Name == original.Bootstrap {
				continue
			}
			if role.Verifier == nil || seen[*role.Verifier] {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
			seen[*role.Verifier] = true
		}
		previous = append(previous, observation)
		var settled ledgerMigrationObservationSettled
		found, err = ledgerMigrationReadObservation(control, "settled", intent.PhaseGeneration, n, &settled)
		if err != nil {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		if !found {
			if n != max {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
			return previous, previousDigest, selected, true, nil
		}
		observationDigest, e1 := ledgerMigrationDigest(observation)
		var restriction ledgerMigrationObservationRestriction
		found, err = ledgerMigrationReadObservation(control, "restrict", intent.PhaseGeneration, n, &restriction)
		restrictionDigest, e2 := ledgerMigrationDigest(restriction)
		if err != nil || !found || e1 != nil || e2 != nil || restriction.Version != 1 || restriction.PhaseGeneration != intent.PhaseGeneration || restriction.Attempt != n || restriction.IntentDigest != observation.IntentDigest || restriction.ObservationIntentDigest != observationDigest || !reflect.DeepEqual(restriction.From, observation.To) || (!reflect.DeepEqual(restriction.To, observation.FinalTarget) && !reflect.DeepEqual(restriction.To, observation.EmergencyTarget)) || settled.Version != 1 || settled.PhaseGeneration != intent.PhaseGeneration || settled.Attempt != n || settled.IntentDigest != observation.IntentDigest || settled.ObservationIntentDigest != observationDigest || settled.RestrictionDigest != restrictionDigest {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		var normal ledgerMigrationObservationNormal
		normalFound, err := ledgerMigrationReadObservation(control, "normal", intent.PhaseGeneration, n, &normal)
		if err != nil {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		switch settled.Outcome {
		case "grant_not_committed", "refenced_without_normal":
			if normalFound || settled.NormalDigest != nil {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
		case "normal_observed_refenced":
			if n != max {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
			if !normalFound || settled.NormalDigest == nil || ledgerMigrationValidateObservationNormal(normal, observation, intent, preparation, original) != nil {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
			digest, err := ledgerMigrationDigest(normal)
			if err != nil || digest != *settled.NormalDigest {
				return nil, nil, nil, false, ErrRecordLedgerFence
			}
			selected = &normal
		default:
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		digest, err := ledgerMigrationDigest(settled)
		if err != nil {
			return nil, nil, nil, false, ErrRecordLedgerFence
		}
		previousDigest = &digest
	}
	return previous, previousDigest, selected, false, nil
}

// Re-entry settles the old grant before considering any next attempt. It never
// replays the grant and never manufactures the missing normal observation.
func ledgerMigrationSettleObservation(ctx, cleanup context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory, observation ledgerMigrationObservationIntent) error {
	attempt := observation.Attempt
	current, sessionErr := ledgerCurrentAppSession(cleanup, f.app)
	if sessionErr != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	same := current.PID == observation.Bootstrap.PID && current.Role == observation.Bootstrap.Role && current.Started.Equal(observation.Bootstrap.Started)
	if ledgerMigrationFenceBackends(cleanup, f.app, original, observation.Bootstrap, nil, !same) != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "intent", observation); err != nil {
		return errorsJoinObservation(err, ledgerMigrationObservationEmergency(cleanup, f, intent, original, preparation, observation))
	}
	digest, err := ledgerMigrationDigest(observation)
	if err != nil {
		return err
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, observation.To)
	actual, err := ledgerReadAppRoleSnapshot(cleanup, f.app, ledgerAppRoleNames(inventory))
	if err != nil {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var normal ledgerMigrationObservationNormal
	normalFound, err := ledgerMigrationReadObservation(f.control, "normal", intent.PhaseGeneration, attempt, &normal)
	if err != nil {
		return err
	}
	if normalFound {
		if ledgerMigrationValidateObservationNormal(normal, observation, intent, preparation, original) != nil {
			return ErrRecordLedgerFence
		}
		if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "normal", normal); err != nil {
			return errorsJoinObservation(err, ledgerMigrationObservationEmergency(cleanup, f, intent, original, preparation, observation))
		}
	}
	var restriction ledgerMigrationObservationRestriction
	found, err := ledgerMigrationReadObservation(f.control, "restrict", intent.PhaseGeneration, attempt, &restriction)
	if err != nil {
		return err
	}
	restrictionAlreadyPublished := found
	if !found {
		target := observation.FinalTarget
		if reflect.DeepEqual(actual, observation.EmergencyTarget) {
			target = observation.EmergencyTarget
		}
		restriction = ledgerMigrationObservationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, IntentDigest: observation.IntentDigest, ObservationIntentDigest: digest, From: observation.To, To: target}
	}
	if restriction.Version != 1 || restriction.PhaseGeneration != intent.PhaseGeneration || restriction.Attempt != attempt || restriction.IntentDigest != observation.IntentDigest || restriction.ObservationIntentDigest != digest || !reflect.DeepEqual(restriction.From, observation.To) || (!reflect.DeepEqual(restriction.To, observation.FinalTarget) && !reflect.DeepEqual(restriction.To, observation.EmergencyTarget)) {
		return ErrRecordLedgerFence
	}
	if err := ledgerMigrationPublishObservation(ctx, f, intent, attempt, "restrict", restriction); err != nil {
		return errorsJoinObservation(err, ledgerMigrationObservationEmergency(cleanup, f, intent, original, preparation, observation))
	}
	outcome := "refenced_without_normal"
	if !restrictionAlreadyPublished && reflect.DeepEqual(actual, observation.From) && !reflect.DeepEqual(actual, observation.EmergencyTarget) {
		outcome = "grant_not_committed"
	}
	if reflect.DeepEqual(actual, observation.To) {
		outcome = "refenced_without_normal"
		if err := ledgerPredecessorApplyRoles(cleanup, f.app, inventory, observation.To, restriction.To); err != nil {
			return ledgerMigrationRevocationFailure(f.control, intent)
		}
	} else if !reflect.DeepEqual(actual, restriction.To) || normalFound && reflect.DeepEqual(actual, observation.From) && !reflect.DeepEqual(actual, restriction.To) {
		return ledgerMigrationRevocationFailure(f.control, intent)
	} else if !reflect.DeepEqual(actual, observation.From) {
		outcome = "refenced_without_normal"
	}
	actual, err = ledgerReadAppRoleSnapshot(cleanup, f.app, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, restriction.To) {
		return ledgerMigrationRevocationFailure(f.control, intent)
	}
	var normalDigest *string
	if normalFound {
		value, err := ledgerMigrationDigest(normal)
		if err != nil {
			return err
		}
		normalDigest, outcome = &value, "normal_observed_refenced"
	}
	restrictionDigest, err := ledgerMigrationDigest(restriction)
	if err != nil {
		return err
	}
	settled := ledgerMigrationObservationSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, IntentDigest: observation.IntentDigest, ObservationIntentDigest: digest, RestrictionDigest: restrictionDigest, Outcome: outcome, NormalDigest: normalDigest}
	return ledgerMigrationPublishObservation(ctx, f, intent, attempt, "settled", settled)
}

func errorsJoinObservation(first, second error) error {
	return errors.Join(first, second)
}
