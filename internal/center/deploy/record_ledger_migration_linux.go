//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"time"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/store/migrate"
)

const ledgerMigrationObservationLimit = 3 * time.Second

type ledgerMigrationCall struct {
	fence                     *RecordLedgerFence
	admission                 *ledgerMigrationAdmission
	intent                    ledgerMigrationIntent
	preparation               ledgerMigrationPreparation
	child                     *ledgerPredecessorChild
	backend                   ledgerPredecessorBackend
	readyPublicationAttempted bool
	rootReadyAcknowledged     bool
	ready                     bool
	committed                 bool
}

// RunLegacyC81MigrationMaintenance is the only admission that may settle an
// interrupted migration before the regular strict maintenance loader runs.
func RunLegacyC81MigrationMaintenance(ctx context.Context, maintenance RecordLedgerMaintenanceRequest) (result LegacyC81MigrationResult, resultErr error) {
	if ctx == nil {
		return result, ErrRecordLedgerFence
	}
	ctx, cancel, err := ledgerMaintenanceWorkContext(ctx)
	if err != nil {
		return result, err
	}
	defer cancel()
	if PrepareLegacyC81MigrationCoordinator() != nil {
		return result, ErrRecordLedgerFence
	}
	fault, err := ledgerCaptureMaintenanceFault(ctx, "c81-convergence-commit")
	if err != nil {
		return result, err
	}
	defer fault.close()
	installation, err := ledgerMigrationInstall()
	if err != nil {
		return result, err
	}
	root, err := ledgerOpenRealm(5)
	if err != nil {
		return result, err
	}
	defer root.close()
	checkpointer := &ledgerMaintenanceChannel{realm: root}
	stopRootWatch, err := checkpointer.watch(ctx, cancel)
	if err != nil {
		return result, err
	}
	defer stopRootWatch()
	admission := &ledgerMigrationAdmission{installation: installation, fault: fault, checkpointer: checkpointer}
	err = withRecordLedgerMaintenanceMode(ctx, maintenance, nil, admission, func(f *RecordLedgerFence) error {
		f.maintenanceCheckpointer = admission.checkpointer
		if admission.recovered != nil {
			result = *admission.recovered
			return nil
		}
		var callErr error
		result, callErr = f.runLegacyC81Migration(ctx, admission)
		return callErr
	})
	if err != nil {
		return LegacyC81MigrationResult{}, err
	}
	return result, nil
}

func (f *RecordLedgerFence) runLegacyC81Migration(ctx context.Context, admission *ledgerMigrationAdmission) (result LegacyC81MigrationResult, resultErr error) {
	if err := f.enter(ctx); err != nil {
		return result, err
	}
	defer f.leave()
	f.sourceWork.Lock()
	defer f.sourceWork.Unlock()
	if err := f.verifyExternal(ctx); err != nil {
		return result, err
	}
	if !f.appInventory.LegacyC80 || f.phase.Generation >= math.MaxInt64-2 || len(f.appSessions) != 1 || f.sourceRequestsSettled(ctx, false) != nil ||
		ledgerMigrationScope(admission.installation.Cgroup, os.Getpid()) != nil || ledgerMigrationWorkerAbsent(ctx, f.app) != nil {
		return result, ErrRecordLedgerFence
	}
	runtimeRole, migratorRole, err := ledgerMigrationRuntimeRoles(f.appInventory)
	if err != nil {
		return result, err
	}
	self, err := ledgerPredecessorSelf()
	if err != nil || self.ExecutableSHA256 != admission.installation.CoordinatorSHA256 {
		return result, ErrRecordLedgerFence
	}
	bootstrap, err := ledgerCurrentAppSession(ctx, f.app)
	registered, present := f.appSessions[bootstrap.PID]
	if err != nil || bootstrap.Role != f.appBootstrapRole || !present || registered.Role != bootstrap.Role || !registered.Started.Equal(bootstrap.Started) {
		return result, ErrRecordLedgerFence
	}
	body, found, err := fenceReadFile(f.session, "app-inventory.json")
	if err != nil || !found || ledgerReadExact(f.session, "app-inventory.json", f.appInventory) != nil {
		return result, ErrRecordLedgerFence
	}
	var guard unix.Stat_t
	if unix.Fstat(f.guardFD, &guard) != nil || guard.Mode&unix.S_IFMT != unix.S_IFREG || guard.Dev == 0 || guard.Ino == 0 {
		return result, ErrRecordLedgerFence
	}
	intent := ledgerMigrationIntent{Version: 1, PhaseGeneration: f.phase.Generation + 1, PreviousPhaseGeneration: f.phase.Generation,
		RecoveryID: f.request.Compose.RecoveryID, Application: f.request.AppIdentity, OriginalInventoryDigest: ledgerPredecessorHash(body),
		Guard: ledgerPredecessorGuard{Dev: guard.Dev, Ino: guard.Ino}, BootID: admission.installation.BootID, Cgroup: admission.installation.Cgroup,
		Coordinator: self, ProgramSHA256: admission.installation.ProgramSHA256, RuntimeRole: runtimeRole, AdminRole: f.appInventory.Admin,
		MigratorRole: migratorRole, WorkerRole: composeDeletionWorkerRole, WorkerAbsent: true,
		Bootstrap: ledgerPredecessorBackend{PID: bootstrap.PID, Started: bootstrap.Started, Role: bootstrap.Role}}
	// Publication may succeed even when its final fsync or ACK fails. Preserve
	// the journal-bound roles until a witnessed terminal settles that uncertainty.
	admission.pending = true
	if err := ledgerPublishMigrationBegin(ctx, f, intent); err != nil {
		return result, err
	}
	if _, err := ledgerPublishExact(f.control, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		return result, err
	}
	if err := ledgerMigrationCheckpoint(ctx, f, intent, 0, "intent", intent); err != nil {
		return result, err
	}
	call := &ledgerMigrationCall{fence: f, admission: admission, intent: intent}
	cleanup, cancelCleanup, err := ledgerMaintenanceCallbackCleanupContext(ctx)
	if err != nil {
		return result, err
	}
	defer cancelCleanup()
	defer func() {
		if cleanup.Err() != nil {
			result = LegacyC81MigrationResult{}
			resultErr = errors.Join(resultErr, ErrRecordLedgerFence)
			if call.child != nil {
				call.child.close()
			}
			return
		}
		// A failed ready publication or root exchange is ambiguous. The original
		// coordinator cannot decide whether root witnessed the ready record; only
		// a replacement after scope retirement can request that decision.
		if call.readyPublicationAttempted && !call.rootReadyAcknowledged || admission.checkpointer == nil || !admission.checkpointer.Healthy() {
			result = LegacyC81MigrationResult{}
			if call.child != nil {
				resultErr = errors.Join(resultErr, ledgerMigrationJoin(cleanup, call.child))
				call.child.close()
			}
			resultErr = errors.Join(resultErr, ErrRecordLedgerFence, ledgerMigrationEmergencyCleanup(cleanup, f, intent, f.appInventory, false))
			return
		}
		var phaseErr error
		if f.phase.Phase == ledgerPhaseQuiescent && f.phase.Generation+1 == intent.PhaseGeneration {
			phaseErr = f.transitionMaintenancePhase(ledgerPhaseMigration)
		}
		if phaseErr == nil && f.phase.Phase == ledgerPhaseMigration && f.phase.Generation == intent.PhaseGeneration {
			phaseErr = f.transitionMaintenancePhase(ledgerPhaseRefencing)
		}
		if call.child != nil {
			if err := ledgerMigrationJoin(cleanup, call.child); err != nil {
				result = LegacyC81MigrationResult{}
				resultErr = errors.Join(resultErr, err, phaseErr, ledgerMigrationEmergencyCleanup(cleanup, f, intent, f.appInventory, false))
				if phaseErr == nil && f.phase.Phase == ledgerPhaseRefencing {
					resultErr = errors.Join(resultErr, ledgerMigrationRevocationFailure(f.control, intent))
				}
				call.child.close()
				return
			}
			call.child.close()
		}
		if phaseErr != nil {
			result = LegacyC81MigrationResult{}
			resultErr = errors.Join(resultErr, phaseErr, ledgerMigrationEmergencyCleanup(cleanup, f, intent, f.appInventory, false))
			return
		}
		if call.backend.PID != 0 {
			delete(f.appSessions, call.backend.PID)
		}
		continuity := func(c context.Context) error {
			return ledgerMigrationCheckOriginalContinuity(c, f.app, f.request, f.control, f.session, f.config, f.primary, f.witness)
		}
		resolved, err := ledgerMigrationResolve(ctx, f, intent, f.appInventory, false, continuity)
		if err != nil {
			result = LegacyC81MigrationResult{}
			resultErr = errors.Join(resultErr, err)
			return
		}
		if err := f.verifyExternal(cleanup); err != nil {
			result = LegacyC81MigrationResult{}
			resultErr = errors.Join(resultErr, err)
			return
		}
		admission.pending = false
		if resultErr == nil {
			if resolved == nil || !call.committed || call.child == nil || !call.child.waitSeen || call.child.waitError != nil {
				result = LegacyC81MigrationResult{}
				resultErr = ErrRecordLedgerFence
				return
			}
			result = *resolved
			result.Recovered = false
		}
	}()
	if err := f.transitionMaintenancePhase(ledgerPhaseMigration); err != nil {
		return result, err
	}
	var migratorURL string
	call.preparation, migratorURL, err = ledgerMigrationPrepareRoles(ctx, f.app, f.control, intent, f.appInventory, f.compose.DatabaseAddress, admission.checkpointer)
	if err != nil {
		return result, err
	}
	config := ledgerMigrationConfig{Version: 1, PhaseGeneration: intent.PhaseGeneration, MigratorURL: migratorURL,
		RuntimeRole: runtimeRole, AdminRole: intent.AdminRole, WorkerRole: intent.WorkerRole, Application: intent.Application}
	file, err := ledgerMigrationConfigFile(config)
	config.MigratorURL, migratorURL = "", ""
	if err != nil {
		return result, err
	}
	defer file.Close()
	if ledgerMigrationScope(intent.Cgroup, os.Getpid()) != nil {
		return result, ErrRecordLedgerFence
	}
	call.child, err = ledgerLaunchMigration(ctx, f.guardFD, file, intent.ProgramSHA256)
	if err != nil {
		return result, err
	}
	if err := call.run(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (call *ledgerMigrationCall) verifyEnvelope(ctx context.Context) error {
	f := call.fence
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil || len(phases) == 0 || phases[len(phases)-1] != f.phase || f.phase.Phase != ledgerPhaseMigration || f.phase.Generation != call.intent.PhaseGeneration ||
		!ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) || !ledgerSessionMatches(f.control, f.session, f.request.Compose.RecoveryID) ||
		ledgerVerifyCgroups(f.groups) != nil || f.verifyCompose(ctx) != nil || f.primary.verifyFenced(ctx) != nil || f.witness.verifyFenced(ctx) != nil ||
		ledgerAppIdentity(ctx, f.app, f.request.AppIdentity) != nil || ledgerVerifyMigrationChild(call.child, call.intent.Cgroup) != nil {
		return ErrRecordLedgerFence
	}
	boot, err := ledgerMigrationBootID()
	if err != nil || boot != call.intent.BootID {
		return ErrRecordLedgerFence
	}
	inventory := f.appInventory
	inventory.Previous = append(append([]string{}, inventory.Previous...), composeDeletionWorkerRole)
	if ledgerCheckAppSessions(ctx, f.app, inventory, f.appSessions) != nil {
		return ErrRecordLedgerFence
	}
	var prepared int
	if f.app.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, ledgerAppRoleNames(inventory)).Scan(&prepared) != nil || prepared != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

func (call *ledgerMigrationCall) receiveReady(ctx context.Context, body []byte) error {
	fields, err := ledgerCallbackFields(body, "op", "phase_generation", "backend_pid", "backend_started", "role")
	if err != nil || call.ready {
		return ErrRecordLedgerFence
	}
	var op, started string
	var generation uint64
	var backend ledgerPredecessorBackend
	if ledgerCallbackDecode(fields, "op", &op) != nil || op != "ready" || ledgerCallbackDecode(fields, "phase_generation", &generation) != nil || generation != call.intent.PhaseGeneration ||
		ledgerCallbackDecode(fields, "backend_pid", &backend.PID) != nil || backend.PID <= 0 || ledgerCallbackDecode(fields, "backend_started", &started) != nil ||
		ledgerCallbackDecode(fields, "role", &backend.Role) != nil || backend.Role != call.intent.MigratorRole {
		return ErrRecordLedgerFence
	}
	backend.Started, err = time.Parse(time.RFC3339Nano, started)
	if err != nil || backend.Started.IsZero() || backend.Started.Location() != time.UTC || backend.Started.Nanosecond()%1000 != 0 || backend.Started.Format(time.RFC3339Nano) != started {
		return ErrRecordLedgerFence
	}
	var actual ledgerAppSession
	if call.fence.app.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=$1 and backend_type='client backend' and datname=pg_catalog.current_database()`, backend.PID).Scan(&actual.PID, &actual.Started, &actual.Role) != nil ||
		actual.PID != backend.PID || actual.Role != backend.Role || !actual.Started.Equal(backend.Started) {
		return ErrRecordLedgerFence
	}
	if _, found := call.fence.appSessions[backend.PID]; found {
		return ErrRecordLedgerFence
	}
	call.backend = backend
	call.fence.appSessions[backend.PID] = actual
	if err := call.verifyEnvelope(ctx); err != nil {
		return err
	}
	names := append(ledgerAppRoleNames(call.fence.appInventory), composeDeletionWorkerRole)
	// ledgerReadAppRoles orders by role name; its caller list is canonical too.
	sort.Strings(names)
	roles, err := ledgerReadAppRoleSnapshot(ctx, call.fence.app, names)
	if err != nil || !reflect.DeepEqual(roles, call.preparation.Normal) {
		return ErrRecordLedgerFence
	}
	old, err := migrate.ReadAppACLMaintenanceSnapshot(ctx, call.fence.app, nil)
	if err != nil || !old.LegacyC80 || !reflect.DeepEqual(old, call.fence.appInventory.Baseline) {
		return ErrRecordLedgerFence
	}
	digest, err := ledgerMigrationDigest(call.intent)
	if err != nil {
		return err
	}
	ready := ledgerMigrationReady{Version: 1, PhaseGeneration: generation, IntentDigest: digest, Child: call.child.process, Backend: backend}
	call.readyPublicationAttempted = true
	if _, err := ledgerPublishExact(call.fence.control, ledgerMigrationName("ready", generation), ready); err != nil {
		return err
	}
	if err := ledgerMigrationCheckpoint(ctx, call.fence, call.intent, 0, "ready", ready); err != nil {
		return err
	}
	call.rootReadyAcknowledged = true
	if err := call.child.channel.send(ctx, map[string]any{"op": "ready_ack", "phase_generation": generation, "backend_pid": backend.PID, "backend_started": started, "role": backend.Role}); err != nil {
		return err
	}
	call.ready = true
	return nil
}

func (call *ledgerMigrationCall) receiveResult(ctx context.Context, body []byte) error {
	fields, err := ledgerCallbackFields(body, "op", "phase_generation", "manifest_digest")
	if err != nil || !call.ready || call.committed {
		return ErrRecordLedgerFence
	}
	var op string
	var generation uint64
	if ledgerCallbackDecode(fields, "op", &op) != nil || op != "migration_result" || ledgerCallbackDecode(fields, "phase_generation", &generation) != nil || generation != call.intent.PhaseGeneration {
		return ErrRecordLedgerFence
	}
	digest, err := ledgerCallbackDigest(fields, "manifest_digest")
	if err != nil || call.verifyEnvelope(ctx) != nil {
		return ErrRecordLedgerFence
	}
	normal, err := ledgerMigrationObserveNormal(ctx, call.fence.app, call.intent, call.preparation, call.fence.appInventory)
	if err != nil || normal.Inventory.ManifestDigest != digest {
		return ErrRecordLedgerFence
	}
	if _, err := ledgerPublishExact(call.fence.control, ledgerMigrationName("normal", generation), normal); err != nil {
		return err
	}
	if err := ledgerMigrationCheckpoint(ctx, call.fence, call.intent, 0, "normal", normal); err != nil {
		return err
	}
	// Root's fault barrier executes after actual commit/catalog readback and
	// before the child receives the result ACK. The child still holds FD3.
	if err := call.admission.fault.migrationCommitted(ctx, call.fence, call.child.process, call.child.pidfd, call.backend); err != nil {
		return err
	}
	if err := call.child.channel.send(ctx, map[string]any{"op": "migration_result_ack", "phase_generation": generation, "manifest_digest": hexDigest(digest)}); err != nil {
		return err
	}
	call.committed = true
	return nil
}

func (call *ledgerMigrationCall) run(ctx context.Context) error {
	eofs := map[string]bool{}
	wait := call.child.waited
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if call.committed && len(eofs) == 3 && call.child.waitSeen {
			if call.child.waitError != nil || ledgerHeldProcessLive(call.child.pidfd) {
				return ErrRecordLedgerFence
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrRecordLedgerFence
		case err := <-wait:
			call.child.waitSeen, call.child.waitError, wait = true, err, nil
			if err != nil || !call.committed {
				return ErrRecordLedgerFence
			}
		case <-ticker.C:
			if !call.committed && call.ready && call.verifyEnvelope(ctx) != nil {
				return ErrRecordLedgerFence
			}
		case message := <-call.child.output:
			if eofs[message.Stream] {
				return ErrRecordLedgerFence
			}
			if message.Err != nil {
				if !errors.Is(message.Err, io.EOF) || message.Stream == "ipc" && !call.committed {
					return ErrRecordLedgerFence
				}
				eofs[message.Stream] = true
			}
			if len(message.Body) == 0 {
				continue
			}
			if message.Stream != "ipc" {
				return ErrRecordLedgerFence
			}
			var envelope struct {
				Op string `json:"op"`
			}
			if json.Unmarshal(message.Body, &envelope) != nil {
				return ErrRecordLedgerFence
			}
			switch envelope.Op {
			case "ready":
				err := call.receiveReady(ctx, message.Body)
				if err != nil {
					return err
				}
			case "migration_result":
				err := call.receiveResult(ctx, message.Body)
				if err != nil {
					return err
				}
			default:
				return ErrRecordLedgerFence
			}
		}
	}
}
