//go:build linux

package deploy

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"
)

var errLedgerMigrationNormalUnresolved = errors.New("migration normal catalog observation is unresolved")

type ledgerMigrationAdmission struct {
	installation ledgerMigrationInstallation
	fault        *ledgerMaintenanceFault
	checkpointer ledgerMaintenanceCheckpointer
	recovered    *LegacyC81MigrationResult
	pending      bool
}

func ledgerMigrationFenceBackends(ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory, old ledgerPredecessorBackend, child *ledgerPredecessorBackend, recovering bool) error {
	current, err := ledgerCurrentAppSession(ctx, app)
	if err != nil || current.Role != inventory.Bootstrap {
		return ErrRecordLedgerFence
	}
	same := current.PID == old.PID && current.Started.Equal(old.Started) && current.Role == old.Role
	if recovering == same {
		return ErrRecordLedgerFence
	}
	effective := inventory
	effective.Previous = append(append([]string{}, inventory.Previous...), composeDeletionWorkerRole)
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	var actual ledgerAppSession
	if tx.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&actual.PID, &actual.Started, &actual.Role) != nil ||
		actual.PID != current.PID || !actual.Started.Equal(current.Started) || actual.Role != current.Role {
		return ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, effective); err != nil {
		return err
	}
	if recovering {
		var oldActive bool
		if tx.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3)`, old.PID, old.Started, old.Role).Scan(&oldActive) != nil || oldActive {
			return ErrRecordLedgerFence
		}
	}
	if child != nil {
		var active bool
		if tx.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3)`, child.PID, child.Started, child.Role).Scan(&active) != nil || active {
			return ErrRecordLedgerFence
		}
	}
	if tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerMigrationPublishCompensation(ctx context.Context, control int, intent ledgerMigrationIntent, original ledgerAppInventory, preparation *ledgerMigrationPreparation, checkpointer ledgerMaintenanceCheckpointer) error {
	intentDigest, e1 := ledgerMigrationDigest(intent)
	targetDigest, e2 := ledgerMigrationDigest(ledgerAppExpectedRoles(original, ledgerAppTarget))
	if e1 != nil || e2 != nil {
		return ErrRecordLedgerFence
	}
	record := ledgerMigrationCompensation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, TargetDigest: targetDigest}
	if preparation != nil {
		if ledgerMigrationValidatePreparation(*preparation, intent, original) != nil {
			return ErrRecordLedgerFence
		}
		worker, found := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
		if !found || worker.OID == 0 {
			return ErrRecordLedgerFence
		}
		var err error
		record.PreparationDigest, err = ledgerMigrationDigest(*preparation)
		if err != nil {
			return err
		}
		record.WorkerOID = worker.OID
	}
	if _, err := ledgerPublishExact(control, ledgerMigrationName("compensate", intent.PhaseGeneration), record); err != nil {
		return err
	}
	if ledgerReadExact(control, ledgerMigrationName("compensate", intent.PhaseGeneration), record) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerMigrationCheckpointRecord(ctx, checkpointer, intent, 0, "compensate", record)
}

func ledgerMigrationAbortRoles(ctx context.Context, app *pgxpool.Pool, control int, intent ledgerMigrationIntent, original ledgerAppInventory, preparation *ledgerMigrationPreparation, restriction *ledgerMigrationRestriction, checkpointer ledgerMaintenanceCheckpointer) error {
	var exists bool
	if app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&exists) != nil {
		return ErrRecordLedgerFence
	}
	if !exists {
		actual, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(original))
		if err != nil {
			return ErrRecordLedgerFence
		}
		target := ledgerAppExpectedRoles(original, ledgerAppTarget)
		emergency := ledgerMigrationEmergencyTarget(target, original.Admin)
		legacyClosed := ledgerAppExpectedRoles(original, ledgerAppTargetAdminClosed)
		if !reflect.DeepEqual(actual, target) && !reflect.DeepEqual(actual, legacyClosed) && !reflect.DeepEqual(actual, emergency) {
			return ErrRecordLedgerFence
		}
		if err := ledgerMigrationPublishCompensation(ctx, control, intent, original, preparation, checkpointer); err != nil {
			cleanup, cancel, cleanupErr := ledgerMaintenanceCallbackCleanupContext(ctx)
			if cleanupErr != nil {
				return errors.Join(err, ledgerMigrationRevocationFailure(control, intent))
			}
			defer cancel()
			if !reflect.DeepEqual(actual, emergency) && ledgerPredecessorApplyRoles(cleanup, app, original, actual, emergency) != nil {
				return errors.Join(err, ledgerMigrationRevocationFailure(control, intent))
			}
			return err
		}
		if !reflect.DeepEqual(actual, target) && ledgerPredecessorApplyRoles(ctx, app, original, actual, target) != nil {
			return ErrRecordLedgerFence
		}
		return nil
	}
	if preparation == nil || restriction == nil || ledgerMigrationValidateRestriction(*restriction, intent, *preparation, original) != nil {
		return ErrRecordLedgerFence
	}
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	names := append(ledgerAppRoleNames(original), composeDeletionWorkerRole)
	sort.Strings(names)
	actual, err := ledgerReadAppRoles(ctx, tx, names)
	if err != nil || !reflect.DeepEqual(actual, restriction.To) && !reflect.DeepEqual(actual, preparation.EmergencyTarget) {
		return ErrRecordLedgerFence
	}
	worker, ok := ledgerAppRoleByName(actual, composeDeletionWorkerRole)
	if !ok || worker.OID == 0 || worker.Connect {
		return ErrRecordLedgerFence
	}
	var dependencies int
	if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_shdepend where refclassid='pg_catalog.pg_authid'::pg_catalog.regclass and refobjid=$1`, worker.OID).Scan(&dependencies) != nil || dependencies != 0 {
		return ErrRecordLedgerFence
	}
	if err := ledgerMigrationPublishCompensation(ctx, control, intent, original, preparation, checkpointer); err != nil {
		return err
	}
	currentOriginal := actual
	currentOriginal.Roles = make([]ledgerAppRole, 0, len(actual.Roles)-1)
	for _, role := range actual.Roles {
		if role.Name != composeDeletionWorkerRole {
			currentOriginal.Roles = append(currentOriginal.Roles, role)
		}
	}
	if err := ledgerMigrationSetRoles(ctx, tx, original, currentOriginal, ledgerAppExpectedRoles(original, ledgerAppTarget)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "drop role "+pgx.Identifier{composeDeletionWorkerRole}.Sanitize()); err != nil {
		return ErrRecordLedgerFence
	}
	settled, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(original))
	if err != nil || !reflect.DeepEqual(settled, ledgerAppExpectedRoles(original, ledgerAppTarget)) || ledgerMigrationWorkerAbsent(ctx, tx) != nil {
		return ErrRecordLedgerFence
	}
	if tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerVerifyAppRoleState(ctx, app, original, original.Credential, ledgerAppTarget)
}

// Only the root's authenticated decision distinguishes a witnessed ready from
// a retained ready whose original scope has retired. Hash the stored bytes,
// including on replay; a re-encoded Go value is not the record root observed.
func ledgerMigrationReadyDisposition(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, body []byte, abandonedFound bool) (string, error) {
	if f.maintenanceCheckpointer == nil || len(body) == 0 {
		return "", ErrRecordLedgerFence
	}
	application, err := ledgerMigrationDigest(intent.Application)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	request := ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: intent.RecoveryID, ApplicationDigest: application,
		PhaseGeneration: intent.PhaseGeneration, Kind: "ready", RecordSHA256: ledgerPredecessorHash(body)}
	decision, err := f.maintenanceCheckpointer.ReadyDisposition(ctx, request)
	if err != nil || decision.DecisionSequence == 0 {
		return "", ErrRecordLedgerFence
	}
	if _, ok := ledgerC80DigestBytes(decision.DecisionDigest); !ok {
		return "", ErrRecordLedgerFence
	}
	switch decision.Disposition {
	case "witnessed":
		if abandonedFound || decision.ReadyWitnessSequence == nil || *decision.ReadyWitnessSequence == 0 || *decision.ReadyWitnessSequence >= decision.DecisionSequence || decision.ReadyWitnessDigest == nil {
			return "", ErrRecordLedgerFence
		}
		if _, ok := ledgerC80DigestBytes(*decision.ReadyWitnessDigest); !ok {
			return "", ErrRecordLedgerFence
		}
		current, found, readErr := fenceReadFile(f.control, ledgerMigrationName("ready", intent.PhaseGeneration))
		if readErr != nil || !found || ledgerPredecessorHash(current) != request.RecordSHA256 {
			return "", ErrRecordLedgerFence
		}
		ack, ackErr := f.maintenanceCheckpointer.Checkpoint(ctx, request)
		if ackErr != nil || ack.WitnessSequence != *decision.ReadyWitnessSequence || ack.WitnessDigest != *decision.ReadyWitnessDigest {
			return "", ErrRecordLedgerFence
		}
	case "unwitnessed_retired":
		if decision.ReadyWitnessSequence != nil || decision.ReadyWitnessDigest != nil {
			return "", ErrRecordLedgerFence
		}
	default:
		return "", ErrRecordLedgerFence
	}
	return decision.Disposition, nil
}

// A normal record can have reached disk before root acknowledged it. Root
// must witness that original observation before a replacement publishes or
// replays refencing. Its later catalog and backend checks remain mandatory.
func ledgerMigrationReplayRetainedNormal(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, original ledgerAppInventory) error {
	var normal ledgerMigrationNormal
	found, err := ledgerMigrationRead(f.control, "normal", intent.PhaseGeneration, &normal)
	if err != nil {
		return ErrRecordLedgerFence
	}
	if !found {
		return nil
	}
	var preparation ledgerMigrationPreparation
	prepared, err := ledgerMigrationRead(f.control, "preparation", intent.PhaseGeneration, &preparation)
	if err != nil || !prepared || ledgerMigrationValidateNormal(normal, intent, preparation, original) != nil ||
		ledgerReadExact(f.control, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation) != nil ||
		ledgerMigrationCheckpoint(ctx, f, intent, 0, "normal", normal) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

// Abandonment proves an exact C80 emergency state before its first witness.
// Once a later refencing phase exists, root permits replay only if it has
// already witnessed the abandonment; compensation may have changed the roles.
func ledgerMigrationSettleUnwitnessedReady(ctx, cleanup context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, original ledgerAppInventory, ready ledgerMigrationReady, readyBody []byte, continuity func(context.Context) error) error {
	names, err := ledgerNames(f.control)
	if err != nil {
		return ErrRecordLedgerFence
	}
	for _, name := range names {
		if strings.HasPrefix(name, "migration-observe-") {
			_, generation, _, ok := ledgerMigrationObservationNameParts(name)
			if !ok || generation == intent.PhaseGeneration {
				return ErrRecordLedgerFence
			}
		}
	}
	for _, kind := range []string{"normal", "successor", "activation"} {
		if _, found, readErr := fenceReadFile(f.control, ledgerMigrationName(kind, intent.PhaseGeneration)); readErr != nil || found {
			return ErrRecordLedgerFence
		}
	}
	preparationBody, found, err := fenceReadFile(f.control, ledgerMigrationName("preparation", intent.PhaseGeneration))
	var preparation ledgerMigrationPreparation
	if err != nil || !found || fenceDecode(preparationBody, &preparation) != nil || ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return ErrRecordLedgerFence
	}
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		return ErrRecordLedgerFence
	}
	var abandoned ledgerMigrationReadyAbandoned
	abandonedFound, err := ledgerMigrationRead(f.control, "ready-abandoned", intent.PhaseGeneration, &abandoned)
	if err != nil {
		return ErrRecordLedgerFence
	}
	if abandonedFound && (abandoned.Version != 1 || abandoned.PhaseGeneration != intent.PhaseGeneration || abandoned.RecoveryID != intent.RecoveryID || abandoned.Application != intent.Application || abandoned.IntentDigest != intentDigest || abandoned.PreparationDigest != ledgerPredecessorHash(preparationBody) || abandoned.ReadyDigest != ledgerPredecessorHash(readyBody) || abandoned.Outcome != "unwitnessed_ready_c80" || abandoned.Coordinator.PID <= 0 || abandoned.Coordinator.Starttime == 0 || !ledgerMaintenanceBackendValid(abandoned.Bootstrap) || !reflect.DeepEqual(abandoned.Restricted, preparation.EmergencyTarget)) {
		return ErrRecordLedgerFence
	}
	if abandonedFound && (abandoned.Coordinator.ExecutableSHA256 != intent.Coordinator.ExecutableSHA256 || abandoned.Bootstrap.Role != intent.Bootstrap.Role || abandoned.Coordinator.PID == intent.Coordinator.PID && abandoned.Coordinator.Starttime == intent.Coordinator.Starttime || abandoned.Bootstrap.PID == intent.Bootstrap.PID && abandoned.Bootstrap.Started.Equal(intent.Bootstrap.Started)) {
		return ErrRecordLedgerFence
	}
	if abandonedFound {
		if _, ok := ledgerC80DigestBytes(abandoned.Coordinator.ExecutableSHA256); !ok {
			return ErrRecordLedgerFence
		}
		if _, ok := ledgerC80DigestBytes(abandoned.CatalogDigest); !ok {
			return ErrRecordLedgerFence
		}
	}
	advanced := f.phase.Phase == ledgerPhaseRefencing && f.phase.Generation == intent.PhaseGeneration+1
	if !advanced {
		if ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, true) != nil || continuity == nil || continuity(ctx) != nil || ledgerAppIdentity(ctx, f.app, intent.Application) != nil {
			return ErrRecordLedgerFence
		}
		catalog, roles, err := ledgerMigrationFencedCatalog(ctx, f.app, intent, &preparation, original)
		if err != nil || !catalog.LegacyC80 || !reflect.DeepEqual(roles, preparation.EmergencyTarget) {
			return ErrRecordLedgerFence
		}
		catalogDigest, err := ledgerMigrationDigest(catalog)
		if err != nil {
			return ErrRecordLedgerFence
		}
		if abandonedFound {
			if abandoned.CatalogDigest != catalogDigest {
				return ErrRecordLedgerFence
			}
		} else {
			self, selfErr := ledgerPredecessorSelf()
			current, currentErr := ledgerCurrentAppSession(ctx, f.app)
			if selfErr != nil || currentErr != nil || current.Role != original.Bootstrap {
				return ErrRecordLedgerFence
			}
			abandoned = ledgerMigrationReadyAbandoned{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: intent.Application,
				IntentDigest: intentDigest, PreparationDigest: ledgerPredecessorHash(preparationBody), ReadyDigest: ledgerPredecessorHash(readyBody), Coordinator: self,
				Bootstrap: ledgerPredecessorBackend{PID: current.PID, Started: current.Started, Role: current.Role}, Restricted: roles, CatalogDigest: catalogDigest, Outcome: "unwitnessed_ready_c80"}
			if _, err := ledgerPublishExact(f.control, ledgerMigrationName("ready-abandoned", intent.PhaseGeneration), abandoned); err != nil {
				return err
			}
		}
	} else if !abandonedFound || ledgerMigrationScope(intent.Cgroup, os.Getpid()) != nil || ledgerMigrationFenceBackends(cleanup, f.app, original, intent.Bootstrap, &ready.Backend, true) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerMigrationSettlementCheckpoint(ctx, f, intent.RecoveryID, intent.Application, intent.PhaseGeneration, "ready-abandoned", abandoned)
}

// Resolve with the same guard and root-owned scope, after every older member
// and database transaction has settled. No PID number is used as retirement
// evidence; scope emptiness and exact backend start times are independent facts.
func ledgerMigrationResolve(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, original ledgerAppInventory, recovering bool, continuity func(context.Context) error) (result *LegacyC81MigrationResult, resultErr error) {
	cleanup, cancelCleanup, err := ledgerMaintenanceCallbackCleanupContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancelCleanup()
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
		}
	}()
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		return nil, err
	}
	var unresolved ledgerMigrationRevocationUnresolved
	unresolvedFound, err := ledgerMigrationRead(f.control, "revocation-unresolved", intent.PhaseGeneration, &unresolved)
	if err != nil || unresolvedFound && (unresolved.Version != 1 || unresolved.PhaseGeneration != intent.PhaseGeneration || unresolved.IntentDigest != intentDigest || unresolved.Outcome != "revocation_unresolved") {
		return nil, errors.Join(ErrRecordLedgerFence, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
	}
	abandoning := false
	if recovering {
		readyBody, readyFound, readErr := fenceReadFile(f.control, ledgerMigrationName("ready", intent.PhaseGeneration))
		if readErr != nil {
			return nil, ErrRecordLedgerFence
		}
		var abandoned ledgerMigrationReadyAbandoned
		abandonedFound, readErr := ledgerMigrationRead(f.control, "ready-abandoned", intent.PhaseGeneration, &abandoned)
		if readErr != nil || abandonedFound && !readyFound {
			return nil, ErrRecordLedgerFence
		}
		if readyFound {
			var ready ledgerMigrationReady
			if fenceDecode(readyBody, &ready) != nil || ready.Version != 1 || ready.PhaseGeneration != intent.PhaseGeneration || ready.IntentDigest != intentDigest || ready.Child.PID <= 0 || ready.Child.Starttime == 0 || ready.Child.ExecutableSHA256 != intent.ProgramSHA256 || !ledgerMaintenanceBackendValid(ready.Backend) || ready.Backend.Role != intent.MigratorRole {
				return nil, ErrRecordLedgerFence
			}
			disposition, err := ledgerMigrationReadyDisposition(ctx, f, intent, readyBody, abandonedFound)
			if err != nil {
				return nil, ErrRecordLedgerFence
			}
			if disposition == "witnessed" {
				if err := ledgerMigrationReplayRetainedNormal(ctx, f, intent, original); err != nil {
					return nil, err
				}
			}
			if disposition == "unwitnessed_retired" {
				if err := ledgerMigrationSettleUnwitnessedReady(ctx, cleanup, f, intent, original, ready, readyBody, continuity); err != nil {
					return nil, err
				}
				abandoning = true
			}
		}
	}
	if recovering && f.phase.Phase == ledgerPhaseRefencing && f.phase.Generation == intent.PhaseGeneration+1 {
		// A durable phase may have lost its ACK. Root must reconcile that
		// existing phase before any later unresolved record is replayed.
		if err := f.checkpointMaintenancePhase(ctx, f.phase); err != nil {
			return nil, ErrRecordLedgerFence
		}
	}
	if unresolvedFound && (ledgerReadExact(f.control, ledgerMigrationName("revocation-unresolved", intent.PhaseGeneration), unresolved) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "revocation-unresolved", unresolved) != nil) {
		return nil, errors.Join(ErrRecordLedgerFence, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
	}
	if f.phase.Generation+1 == intent.PhaseGeneration && f.phase.Phase == ledgerPhaseQuiescent {
		if err := f.transitionMaintenancePhase(ledgerPhaseMigration); err != nil {
			return nil, errors.Join(err, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
		}
	}
	if f.phase.Phase == ledgerPhaseMigration && f.phase.Generation == intent.PhaseGeneration {
		if err := f.transitionMaintenancePhase(ledgerPhaseRefencing); err != nil {
			return nil, errors.Join(err, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
		}
	} else if f.phase.Phase != ledgerPhaseRefencing || f.phase.Generation != intent.PhaseGeneration+1 {
		return nil, errors.Join(ErrRecordLedgerFence, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
	}
	var ready ledgerMigrationReady
	readyFound, err := ledgerMigrationRead(f.control, "ready", intent.PhaseGeneration, &ready)
	var evidenceErr error
	if err != nil {
		evidenceErr, readyFound = ErrRecordLedgerFence, false
	}
	var oldChild *ledgerPredecessorBackend
	if readyFound {
		if ready.Version != 1 || ready.PhaseGeneration != intent.PhaseGeneration || ready.IntentDigest != intentDigest ||
			ready.Child.PID <= 0 || ready.Child.Starttime == 0 || ready.Child.ExecutableSHA256 != intent.ProgramSHA256 ||
			ready.Backend.PID <= 0 || ready.Backend.Started.IsZero() || ready.Backend.Started.Nanosecond()%1000 != 0 || ready.Backend.Role != intent.MigratorRole {
			evidenceErr, readyFound = ErrRecordLedgerFence, false
		} else {
			oldChild = &ready.Backend
		}
	}
	if ledgerMigrationScope(intent.Cgroup, os.Getpid()) != nil || ledgerMigrationFenceBackends(cleanup, f.app, original, intent.Bootstrap, oldChild, recovering) != nil {
		return nil, ledgerMigrationRevocationFailure(f.control, intent)
	}
	var preparation ledgerMigrationPreparation
	prepared, err := ledgerMigrationRead(f.control, "preparation", intent.PhaseGeneration, &preparation)
	if err != nil || prepared && ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return nil, ledgerMigrationRevocationFailure(f.control, intent)
	}
	if prepared && (ledgerReadExact(f.control, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "preparation", preparation) != nil) {
		evidenceErr = ErrRecordLedgerFence
	}
	var normal ledgerMigrationNormal
	normalFound, err := ledgerMigrationRead(f.control, "normal", intent.PhaseGeneration, &normal)
	if err != nil || normalFound && (!prepared || !readyFound) || normalFound && ledgerMigrationValidateNormal(normal, intent, preparation, original) != nil {
		evidenceErr, normalFound = ErrRecordLedgerFence, false
	}
	if normalFound && (ledgerReadExact(f.control, ledgerMigrationName("normal", intent.PhaseGeneration), normal) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "normal", normal) != nil) {
		evidenceErr = ErrRecordLedgerFence
	}
	// A post-COMMIT catalog observation cannot be reconstructed as the child's
	// original normal. Recovery must refence and publish a new observation.
	var observationErr error
	if evidenceErr == nil && !normalFound && prepared && readyFound {
		observationErr = errLedgerMigrationNormalUnresolved
	}
	var preparedRoles *ledgerMigrationPreparation
	if prepared {
		preparedRoles = &preparation
	}
	current, err := ledgerCurrentAppSession(cleanup, f.app)
	if err != nil {
		return nil, ledgerMigrationRevocationFailure(f.control, intent)
	}
	sameBootstrap := current.PID == intent.Bootstrap.PID && current.Role == intent.Bootstrap.Role && current.Started.Equal(intent.Bootstrap.Started)
	if ledgerMigrationFenceBackends(cleanup, f.app, original, intent.Bootstrap, oldChild, !sameBootstrap) != nil {
		return nil, ledgerMigrationRevocationFailure(f.control, intent)
	}
	var previous []ledgerMigrationObservationIntent
	var previousSettled *string
	var selected *ledgerMigrationObservationNormal
	if prepared {
		var pending bool
		previous, previousSettled, selected, pending, err = ledgerMigrationObservationPrefix(f.control, intent, preparation, original)
		if err != nil || normalFound && len(previous) != 0 {
			return nil, ErrRecordLedgerFence
		}
		if pending {
			if !readyFound {
				return nil, ErrRecordLedgerFence
			}
			if err := ledgerMigrationSettleObservation(ctx, cleanup, f, intent, preparation, original, previous[len(previous)-1]); err != nil {
				return nil, err
			}
			previous, previousSettled, selected, pending, err = ledgerMigrationObservationPrefix(f.control, intent, preparation, original)
			if err != nil || pending {
				return nil, ErrRecordLedgerFence
			}
		}
		if len(previous) != 0 {
			attempt := previous[len(previous)-1].Attempt
			var settled ledgerMigrationObservationSettled
			found, readErr := ledgerMigrationReadObservation(f.control, "settled", intent.PhaseGeneration, attempt, &settled)
			if readErr != nil || !found || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, attempt, "observe-settled", settled) != nil {
				return nil, errors.Join(ErrRecordLedgerFence, ledgerMigrationEmergencyCleanup(cleanup, f, intent, original, recovering))
			}
		}
	}
	restriction, err := ledgerMigrationRestrict(cleanup, f.app, f.control, intent, original, preparedRoles, f.maintenanceCheckpointer)
	if err != nil {
		return nil, err
	}
	if evidenceErr != nil {
		return nil, errors.Join(ErrRecordLedgerFence, errLedgerMigrationNormalUnresolved, evidenceErr)
	}
	if continuity == nil || continuity(ctx) != nil {
		return nil, errors.Join(ErrRecordLedgerFence, observationErr)
	}
	observedCatalog, observedRoles, err := ledgerMigrationFencedCatalog(ctx, f.app, intent, preparedRoles, original)
	if err != nil {
		return nil, errors.Join(ErrRecordLedgerFence, errLedgerMigrationNormalUnresolved, err)
	}
	if abandoning && !observedCatalog.LegacyC80 {
		return nil, ErrRecordLedgerFence
	}
	if !normalFound && observedCatalog.LegacyC80 {
		if len(previous) != 0 {
			return nil, ErrRecordLedgerFence
		}
		if err := ledgerMigrationAbortRoles(ctx, f.app, f.control, intent, original, preparedRoles, restriction, f.maintenanceCheckpointer); err != nil {
			return nil, errors.Join(err, observationErr)
		}
		var compensation ledgerMigrationCompensation
		found, err := ledgerMigrationRead(f.control, "compensate", intent.PhaseGeneration, &compensation)
		compensationDigest, digestErr := ledgerMigrationDigest(compensation)
		if err != nil || !found || digestErr != nil {
			return nil, ErrRecordLedgerFence
		}
		abort := ledgerMigrationAbort{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, CompensationDigest: compensationDigest, Outcome: "restored_c80"}
		if _, err := ledgerPublishExact(f.control, ledgerMigrationName("abort", intent.PhaseGeneration), abort); err != nil {
			return nil, err
		}
		if ledgerReadExact(f.control, ledgerMigrationName("abort", intent.PhaseGeneration), abort) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "abort", abort) != nil {
			return nil, ErrRecordLedgerFence
		}
		if err := f.transitionMaintenancePhase(ledgerPhaseQuiescent); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if observedCatalog.LegacyC80 {
		return nil, ErrRecordLedgerFence
	}
	var inv ledgerAppInventory
	normalSource := "original"
	var normalAttempt uint64
	var normalDigest string
	if normalFound {
		inv = normal.Inventory
		normalDigest, err = ledgerMigrationDigest(normal)
	} else {
		if !prepared || !readyFound {
			return nil, errLedgerMigrationNormalUnresolved
		}
		if selected == nil {
			fresh, freshErr := ledgerMigrationFreshObservation(ctx, cleanup, f, intent, preparation, original, observedCatalog, observedRoles, previous, previousSettled)
			if freshErr != nil {
				return nil, freshErr
			}
			selected = &fresh
		} else {
			var settled ledgerMigrationObservationSettled
			found, readErr := ledgerMigrationReadObservation(f.control, "settled", intent.PhaseGeneration, selected.Attempt, &settled)
			if readErr != nil || !found || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, selected.Attempt, "observe-settled", settled) != nil {
				return nil, ErrRecordLedgerFence
			}
		}
		inv, normalSource, normalAttempt = selected.Inventory, "observation", selected.Attempt
		normalDigest, err = ledgerMigrationDigest(*selected)
	}
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	emergencyObserved := prepared && reflect.DeepEqual(observedRoles, preparation.EmergencyTarget)
	if !reflect.DeepEqual(observedCatalog.Manifest, inv.Baseline.Manifest) || !reflect.DeepEqual(observedCatalog.Catalog, ledgerMigrationExpectedFencedCatalog(inv, emergencyObserved)) {
		return nil, ErrRecordLedgerFence
	}
	actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(inv))
	if err != nil {
		return nil, err
	}
	target := ledgerAppExpectedRoles(inv, ledgerAppTarget)
	switch {
	case reflect.DeepEqual(actual, inv.RoleBaseline):
		if err := ledgerPredecessorApplyRoles(ctx, f.app, inv, actual, target); err != nil {
			return nil, err
		}
	case reflect.DeepEqual(actual, ledgerAppExpectedRoles(inv, ledgerAppTargetAdminClosed)) || prepared && reflect.DeepEqual(actual, preparation.EmergencyTarget):
		if err := ledgerPredecessorApplyRoles(ctx, f.app, inv, actual, target); err != nil {
			return nil, err
		}
	case reflect.DeepEqual(actual, target):
	default:
		return nil, ErrRecordLedgerFence
	}
	if ledgerCheckAppFenced(ctx, f.app, inv.Bootstrap, inv, inv.Credential) != nil {
		return nil, ErrRecordLedgerFence
	}
	current, err = ledgerCurrentAppSession(ctx, f.app)
	if err != nil || ledgerCheckAppSessions(ctx, f.app, inv, map[int32]ledgerAppSession{current.PID: current}) != nil {
		return nil, ErrRecordLedgerFence
	}
	if _, err := ledgerPublishExact(f.control, ledgerMigrationName("successor", intent.PhaseGeneration), inv); err != nil {
		return nil, err
	}
	if ledgerReadExact(f.control, ledgerMigrationName("successor", intent.PhaseGeneration), inv) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "successor", inv) != nil {
		return nil, ErrRecordLedgerFence
	}
	successorDigest, e1 := ledgerMigrationDigest(inv)
	if e1 != nil {
		return nil, ErrRecordLedgerFence
	}
	activation := ledgerMigrationActivation{Version: 1, PhaseGeneration: intent.PhaseGeneration, TerminalPhaseGeneration: intent.PhaseGeneration + 2,
		RecoveryID: intent.RecoveryID, Application: intent.Application, IntentDigest: intentDigest, OriginalInventoryDigest: intent.OriginalInventoryDigest,
		SuccessorDigest: successorDigest, NormalDigest: normalDigest, NormalSource: normalSource, NormalAttempt: normalAttempt, ManifestDigest: hexDigest(inv.ManifestDigest)}
	if _, err := ledgerPublishExact(f.control, ledgerMigrationName("activation", intent.PhaseGeneration), activation); err != nil {
		return nil, err
	}
	if ledgerReadExact(f.control, ledgerMigrationName("activation", intent.PhaseGeneration), activation) != nil || ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, 0, "activation", activation) != nil {
		return nil, ErrRecordLedgerFence
	}
	if err := f.transitionMaintenancePhase(ledgerPhaseQuiescent); err != nil {
		return nil, err
	}
	f.appInventory = inv
	f.appCredential = inv.Credential
	return &LegacyC81MigrationResult{ManifestDigest: activation.ManifestDigest, Recovered: recovering}, nil
}

func (admission *ledgerMigrationAdmission) prepare(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, control, session int, address string, guardFD int, cfg SourceMaintenanceConfig, primary, witness *ledgerDatabase) error {
	continuity := func(c context.Context) error {
		return ledgerMigrationCheckOriginalContinuity(c, app, request, control, session, cfg, primary, witness)
	}
	journal, err := ledgerMigrationLoadJournal(control, request.AppIdentity, true)
	if err != nil {
		return err
	}
	if err := ledgerMigrationAuthorizeJournal(ctx, control, journal, admission.checkpointer); err != nil {
		return err
	}
	if !journal.Pending && continuity(ctx) != nil {
		return ErrRecordLedgerFence
	}
	if !journal.Pending {
		if journal.Activation == nil {
			return nil
		}
		if journal.Intent == nil || journal.Intent.RecoveryID != request.Compose.RecoveryID || journal.Intent.BootID != admission.installation.BootID ||
			journal.Intent.Cgroup != admission.installation.Cgroup || journal.Intent.ProgramSHA256 != admission.installation.ProgramSHA256 || journal.Intent.Coordinator.ExecutableSHA256 != admission.installation.CoordinatorSHA256 {
			return ErrRecordLedgerFence
		}
		admission.recovered = &LegacyC81MigrationResult{ManifestDigest: journal.Activation.ManifestDigest, Recovered: true}
		return nil
	}
	admission.pending = true
	if journal.Intent == nil {
		if journal.Begin == nil || journal.Begin.RecoveryID != request.Compose.RecoveryID {
			return ErrRecordLedgerFence
		}
		begin := *journal.Begin
		if begin.BootID != admission.installation.BootID || begin.Cgroup != admission.installation.Cgroup || begin.Coordinator.ExecutableSHA256 != admission.installation.CoordinatorSHA256 || ledgerMigrationScope(begin.Cgroup, os.Getpid()) != nil {
			return ErrRecordLedgerFence
		}
		var guard unix.Stat_t
		if unix.Fstat(guardFD, &guard) != nil || guard.Dev != begin.Guard.Dev || guard.Ino != begin.Guard.Ino {
			return ErrRecordLedgerFence
		}
		body, found, err := fenceReadFile(session, "app-inventory.json")
		var original ledgerAppInventory
		if err != nil || !found || fenceDecode(body, &original) != nil || ledgerPredecessorHash(body) != begin.OriginalInventoryDigest || !original.LegacyC80 || ledgerValidateAppRequest(original, request, original.Bootstrap, address) != nil {
			return ErrRecordLedgerFence
		}
		phases, err := ledgerReadMaintenancePhases(control)
		if err != nil || len(phases) == 0 {
			return ErrRecordLedgerFence
		}
		phase := phases[len(phases)-1]
		temporary := &RecordLedgerFence{ctx: ctx, uses: 1, phase: phase, control: control, request: request, app: app, appInventory: original, appCredential: original.Credential, maintenanceCheckpointer: admission.checkpointer}
		temporary.condition = sync.NewCond(&temporary.mu)
		if err := ledgerMigrationResolveBegin(ctx, temporary, begin, original, continuity); err != nil {
			return err
		}
		admission.pending = false
		return nil
	}
	if journal.Intent == nil || journal.Intent.RecoveryID != request.Compose.RecoveryID {
		return ErrRecordLedgerFence
	}
	intent := *journal.Intent
	if intent.BootID != admission.installation.BootID || intent.Cgroup != admission.installation.Cgroup || intent.ProgramSHA256 != admission.installation.ProgramSHA256 ||
		intent.Coordinator.ExecutableSHA256 != admission.installation.CoordinatorSHA256 || ledgerMigrationScope(intent.Cgroup, os.Getpid()) != nil {
		return ErrRecordLedgerFence
	}
	var guard unix.Stat_t
	if unix.Fstat(guardFD, &guard) != nil || guard.Dev != intent.Guard.Dev || guard.Ino != intent.Guard.Ino {
		return ErrRecordLedgerFence
	}
	body, found, err := fenceReadFile(session, "app-inventory.json")
	var original ledgerAppInventory
	if err != nil || !found || fenceDecode(body, &original) != nil || ledgerPredecessorHash(body) != intent.OriginalInventoryDigest ||
		ledgerValidateAppRequest(original, request, intent.Bootstrap.Role, address) != nil || !original.LegacyC80 {
		return ErrRecordLedgerFence
	}
	runtime, migrator, err := ledgerMigrationRuntimeRoles(original)
	if err != nil || runtime != intent.RuntimeRole || migrator != intent.MigratorRole || intent.AdminRole != original.Admin || intent.Bootstrap.Role != original.Bootstrap {
		return ErrRecordLedgerFence
	}
	phases, err := ledgerReadMaintenancePhases(control)
	if err != nil || len(phases) == 0 {
		return ErrRecordLedgerFence
	}
	phase := phases[len(phases)-1]
	if phase.Application != intent.Application || phase.RecoveryID != intent.RecoveryID {
		return ErrRecordLedgerFence
	}
	temporary := &RecordLedgerFence{ctx: ctx, uses: 1, phase: phase, control: control, request: request, app: app, appInventory: original, appCredential: original.Credential, maintenanceCheckpointer: admission.checkpointer}
	temporary.condition = sync.NewCond(&temporary.mu)
	result, err := ledgerMigrationResolve(ctx, temporary, intent, original, true, continuity)
	if err != nil {
		return err
	}
	admission.recovered = result
	admission.pending = false
	return nil
}
