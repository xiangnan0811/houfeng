//go:build linux

package deploy

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"
)

type ledgerPredecessorAdmission struct {
	request   LegacyC80ServiceRequest
	realm     *ledgerRealmChannel
	fault     *ledgerPredecessorFault
	pending   *ledgerPredecessorIntent
	phase     ledgerMaintenancePhase
	ready     *ledgerPredecessorReady
	recovered *LegacyC80ServiceResult
}

var errLedgerPredecessorRealm = errors.New("predecessor realm unavailable")

// PrepareLegacyC80ServiceCoordinator must run before the CLI reads credentials.
func PrepareLegacyC80ServiceCoordinator() error {
	if os.Geteuid() != 10002 || os.Getegid() != 10002 || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return ErrRecordLedgerFence
	}
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || nnp != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

// RunLegacyC80ServiceMaintenance is the restricted CLI composition. Only this
// entrypoint may resolve an interrupted predecessor before the general strict
// quiescent phase loader. It never grants recovery to other maintenance commands.
func RunLegacyC80ServiceMaintenance(ctx context.Context, maintenance RecordLedgerMaintenanceRequest, request LegacyC80ServiceRequest) (result LegacyC80ServiceResult, resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		reason := "service_unavailable"
		if errors.Is(resultErr, errLedgerPredecessorRealm) {
			reason = "realm_unavailable"
		} else if errors.Is(resultErr, ErrRecordLedgerUnexpectedSession) {
			reason = "unexpected_session"
		}
		result = LegacyC80ServiceResult{Status: "unavailable", Reason: reason}
	}()
	if ctx == nil {
		return result, ErrRecordLedgerFence
	}
	ctx, cancel, err := ledgerMaintenanceWorkContext(ctx)
	if err != nil {
		return result, err
	}
	defer cancel()
	if PrepareLegacyC80ServiceCoordinator() != nil {
		return result, ErrRecordLedgerFence
	}
	// Capture optional FD7 before opening pools or converting sockets can reuse
	// that descriptor number. It is never inherited by the predecessor.
	faultFD := -1
	if kind, err := unix.GetsockoptInt(7, unix.SOL_SOCKET, unix.SO_TYPE); err == nil {
		if kind != unix.SOCK_SEQPACKET {
			return LegacyC80ServiceResult{}, ErrRecordLedgerFence
		}
		faultFD, err = unix.FcntlInt(7, unix.F_DUPFD_CLOEXEC, 64)
		if err != nil {
			return LegacyC80ServiceResult{}, ErrRecordLedgerFence
		}
		defer unix.Close(faultFD)
	} else if err != unix.EBADF {
		return LegacyC80ServiceResult{}, ErrRecordLedgerFence
	}
	realm, err := ledgerOpenRealm(request.RealmFD)
	if err != nil {
		return result, errors.Join(err, errLedgerPredecessorRealm)
	}
	defer realm.close()
	admission := &ledgerPredecessorAdmission{request: request, realm: realm}
	if faultFD >= 0 {
		admission.fault, err = ledgerOpenPredecessorFaultFD(ctx, realm, faultFD)
		if err != nil {
			return LegacyC80ServiceResult{}, err
		}
		defer admission.fault.channel.close()
	}
	err = withRecordLedgerMaintenance(ctx, maintenance, admission, func(f *RecordLedgerFence) error {
		if admission.recovered != nil {
			result = *admission.recovered
			return nil
		}
		var err error
		result, err = f.runLegacyC80Service(ctx, request, realm, admission.fault)
		return err
	})
	if err != nil {
		return LegacyC80ServiceResult{}, err
	}
	return result, nil
}

func ledgerPredecessorPending(control int, request RecordLedgerMaintenanceRequest, service LegacyC80ServiceRequest) (*ledgerPredecessorIntent, ledgerMaintenancePhase, error) {
	phases, err := ledgerReadMaintenancePhases(control)
	if err != nil {
		return nil, ledgerMaintenancePhase{}, err
	}
	if len(phases) == 0 {
		return nil, ledgerMaintenancePhase{}, nil
	}
	phase := phases[len(phases)-1]
	if phase.Application != request.AppIdentity || phase.RecoveryID != request.Compose.RecoveryID {
		return nil, phase, ErrRecordLedgerFence
	}
	names, err := ledgerNames(control)
	if err != nil {
		return nil, phase, err
	}
	var pending *ledgerPredecessorIntent
	for _, name := range names {
		if !strings.HasPrefix(name, "predecessor-callback-intent-") {
			continue
		}
		data, found, err := fenceReadFile(control, name)
		var intent ledgerPredecessorIntent
		if err != nil || !found || ledgerPredecessorDecode(data, &intent) != nil || intent.Version != 1 || intent.PhaseGeneration == 0 || name != ledgerPredecessorName("callback-intent", intent.PhaseGeneration) || intent.Application != request.AppIdentity || intent.RecoveryID != request.Compose.RecoveryID {
			return nil, phase, ErrRecordLedgerFence
		}
		var completed ledgerPredecessorCompletion
		complete, err := ledgerPredecessorRead(control, "callback-result", intent.PhaseGeneration, &completed)
		if err != nil {
			return nil, phase, err
		}
		if complete {
			if completed.Version != 1 || completed.PhaseGeneration != intent.PhaseGeneration || completed.FinalPhaseGeneration != intent.PhaseGeneration+2 || completed.Application != intent.Application || completed.RecoveryID != intent.RecoveryID || completed.ActorUserID != intent.ActorUserID || completed.RecordID != intent.RecordID || completed.Coordinator != intent.Coordinator || !completed.Exit.PidfdExited || completed.Exit.WaitStatus != 0 || completed.ChildResult.PhaseGeneration != intent.PhaseGeneration {
				return nil, phase, ErrRecordLedgerFence
			}
			if !ledgerPredecessorOutputValid(completed.ChildResult, intent.PhaseGeneration) {
				return nil, phase, ErrRecordLedgerFence
			}
			if completed.ChildResult.Status != "unavailable" {
				continue
			}
		}
		var resolution ledgerPredecessorResolution
		resolved, err := ledgerPredecessorRead(control, "resolution", intent.PhaseGeneration, &resolution)
		if err != nil {
			return nil, phase, err
		}
		if resolved && resolution.Outcome != "reserve_committed" {
			if resolution.Outcome != "preview_not_created" && resolution.Outcome != "preview_abandoned" && resolution.Outcome != "reserve_not_committed_preview_abandoned" {
				return nil, phase, ErrRecordLedgerFence
			}
			if resolution.PhaseGeneration != intent.PhaseGeneration || resolution.Application != intent.Application || resolution.RealmDigest != intent.RealmDigest || resolution.OperationID != "" {
				return nil, phase, ErrRecordLedgerFence
			}
			continue
		}
		if intent.ActorUserID != service.ActorUserID || intent.RecordID != service.RecordID {
			return nil, phase, ErrRecordLedgerFence
		}
		if pending != nil {
			return nil, phase, ErrRecordLedgerFence
		}
		copy := intent
		pending = &copy
	}
	if pending == nil {
		if phase.Phase != ledgerPhaseQuiescent {
			return nil, phase, ErrRecordLedgerFence
		}
		return nil, phase, nil
	}
	generation := pending.PhaseGeneration
	valid := phase.Phase == ledgerPhasePredecessor && phase.Generation == generation || phase.Phase == ledgerPhaseRefencing && phase.Generation == generation+1 || phase.Phase == ledgerPhaseQuiescent && (phase.Generation+1 == generation || phase.Generation == generation+2)
	if !valid {
		return nil, phase, ErrRecordLedgerFence
	}
	return pending, phase, nil
}

func (admission *ledgerPredecessorAdmission) prepare(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, control, session int, address string) error {
	intent, phase, err := ledgerPredecessorPending(control, request, admission.request)
	if err != nil || intent == nil {
		return err
	}
	var inventory ledgerAppInventory
	raw, found, err := fenceReadFile(session, "app-inventory.json")
	if err != nil || !found || fenceDecode(raw, &inventory) != nil || ledgerValidateAppRequest(inventory, request, inventory.Bootstrap, address) != nil || !inventory.LegacyC80 {
		return ErrRecordLedgerFence
	}
	var roles ledgerPredecessorRoles
	if found, err := ledgerPredecessorRead(control, "roles", intent.PhaseGeneration, &roles); err != nil || !found || ledgerPredecessorValidateRoles(roles, inventory, *intent) != nil {
		return ErrRecordLedgerFence
	}
	// No serialized receipt replaces this live root channel. Root still retains
	// the original coordinator/child pidfds and independently settles that history.
	var ready ledgerPredecessorReady
	readyFound, err := ledgerPredecessorRead(control, "ready", intent.PhaseGeneration, &ready)
	if err != nil {
		return err
	}
	if readyFound {
		if ready.Version != 1 || ready.PhaseGeneration != intent.PhaseGeneration || ready.RecoveryID != intent.RecoveryID || ready.Application != intent.Application || ready.RealmDigest != intent.RealmDigest || ready.Backend.PID <= 0 || ready.Backend.Started.IsZero() || ready.Child.PID <= 0 || ready.Child.Starttime == 0 || ready.Child.ExecutableSHA256 != intent.PredecessorSHA256 {
			return ErrRecordLedgerFence
		}
		admission.ready = &ready
	}
	temporary := &RecordLedgerFence{uses: 1, phase: phase, control: control, request: request}
	temporary.condition = sync.NewCond(&temporary.mu)
	if phase.Phase == ledgerPhaseQuiescent && phase.Generation+1 == intent.PhaseGeneration {
		if err := temporary.transitionMaintenancePhase(ledgerPhasePredecessor); err != nil {
			return err
		}
	}
	if temporary.phase.Phase == ledgerPhasePredecessor {
		if err := temporary.transitionMaintenancePhase(ledgerPhaseRefencing); err != nil {
			return err
		}
	}
	actual, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
	if err != nil {
		return err
	}
	target := ledgerAppExpectedRoles(inventory, ledgerAppTarget)
	switch {
	case reflect.DeepEqual(actual, roles.Normal):
		if err := ledgerPredecessorApplyRoles(ctx, app, inventory, roles.Normal, target); err != nil {
			return err
		}
	case reflect.DeepEqual(actual, target), reflect.DeepEqual(actual, ledgerAppExpectedRoles(inventory, ledgerAppTargetAdminClosed)):
		bootstrap, err := ledgerCurrentAppSession(ctx, app)
		if err != nil {
			return err
		}
		state := ledgerAppTarget
		if !reflect.DeepEqual(actual, target) {
			state = ledgerAppTargetAdminClosed
		}
		if err := ledgerApplyAppRoles(ctx, app, inventory, state, bootstrap); err != nil {
			return err
		}
	default:
		return ErrRecordLedgerFence
	}
	observed, err := admission.realm.observe(ctx, temporary.phase.Generation)
	if err != nil || hexDigest(observed.Realm) != intent.RealmDigest {
		return ErrRecordLedgerFence
	}
	if ledgerPredecessorGroupEmpty(intent.ChildCgroup) != nil {
		return ErrRecordLedgerFence
	}
	if err := ledgerCheckAppFenced(ctx, app, inventory.Bootstrap, inventory, inventory.Credential); err != nil {
		return err
	}
	admission.pending, admission.phase = intent, temporary.phase
	return nil
}

func (admission *ledgerPredecessorAdmission) resume(ctx context.Context, f *RecordLedgerFence) error {
	if admission.pending == nil {
		return nil
	}
	intent := *admission.pending
	f.phase = admission.phase
	f.uses = 1
	defer func() { f.uses = 0 }()
	// Existing anchors are required here: an interrupted callback can never
	// establish a replacement genesis or adopt another APP's history.
	anchors, err := ledgerReadAnchors(f.control)
	if err != nil || len(anchors) == 0 {
		return ErrRecordLedgerFence
	}
	if err := f.loadAnchors(ctx); err != nil {
		return err
	}
	binding := ledgerPredecessorEffectBinding{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: intent.Application, RealmDigest: intent.RealmDigest}
	if admission.ready != nil {
		binding.Backend = admission.ready.Backend
	}
	resolution, err := ledgerResolvePredecessor(ctx, f, intent, binding)
	if err != nil {
		resolution.Outcome = "unresolved"
		_, persistErr := ledgerPublishExact(f.control, ledgerPredecessorName("resolution", intent.PhaseGeneration), resolution)
		return errors.Join(err, persistErr)
	}
	if _, err := ledgerPublishExact(f.control, ledgerPredecessorName("resolution", intent.PhaseGeneration), resolution); err != nil {
		return err
	}
	if f.phase.Phase == ledgerPhaseRefencing {
		if err := f.transitionMaintenancePhase(ledgerPhaseQuiescent); err != nil {
			return err
		}
	}
	if f.phase.Phase != ledgerPhaseQuiescent {
		return ErrRecordLedgerFence
	}
	if resolution.Outcome == "reserve_committed" {
		admission.recovered = &LegacyC80ServiceResult{Status: "recovered_committed", OperationID: resolution.OperationID, Reason: "reserve_committed"}
	}
	admission.pending = nil
	return nil
}

func ledgerResolvePredecessor(ctx context.Context, f *RecordLedgerFence, intent ledgerPredecessorIntent, binding ledgerPredecessorEffectBinding) (ledgerPredecessorResolution, error) {
	result := ledgerPredecessorResolution{ledgerPredecessorEffectBinding: binding, Outcome: "preview_not_created"}
	var create ledgerPredecessorCreateIntent
	var preview ledgerPredecessorCreateResult
	var reserve ledgerPredecessorReserveIntent
	var committed ledgerPredecessorReserveResult
	var written ledgerPredecessorWrittenResult
	creates, e1 := ledgerPredecessorRead(f.control, "create-intent", intent.PhaseGeneration, &create)
	previews, e2 := ledgerPredecessorRead(f.control, "create-result", intent.PhaseGeneration, &preview)
	reserves, e3 := ledgerPredecessorRead(f.control, "reserve-intent", intent.PhaseGeneration, &reserve)
	commits, e4 := ledgerPredecessorRead(f.control, "reserve-result", intent.PhaseGeneration, &committed)
	outputs, e5 := ledgerPredecessorRead(f.control, "result-written", intent.PhaseGeneration, &written)
	carrier, original, captured, e6 := ledgerPredecessorReadStdout(f.control, intent)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || (outputs && !captured) {
		return result, ErrRecordLedgerFence
	}
	if outputs {
		if written.Version != 1 || written.PhaseGeneration != intent.PhaseGeneration || written.Application != intent.Application || written.RecoveryID != intent.RecoveryID || written.RealmDigest != intent.RealmDigest || !ledgerPredecessorOutputValid(written.Result, intent.PhaseGeneration) {
			return result, ErrRecordLedgerFence
		}
		// Only persisted original bytes can corroborate the written receipt.
		if written.ResultDigest != carrier.ResultDigest || written.Result != original {
			return result, ErrRecordLedgerFence
		}
		if written.Result.Status == "committed" && !commits || written.Result.Status == "rejected" && creates {
			return result, ErrRecordLedgerFence
		}
	}
	if captured && (original.Status == "committed" && !commits || original.Status == "rejected" && creates) {
		return result, ErrRecordLedgerFence
	}
	if !creates {
		if previews || reserves || commits || (outputs && written.Result.Status == "committed") {
			return result, ErrRecordLedgerFence
		}
		return result, nil
	}
	if binding.Backend.PID <= 0 || create.ledgerPredecessorEffectBinding != binding || !ledgerC80ValidCreate(create.Command) || create.Command.ObjectID != intent.RecordID {
		return result, ErrRecordLedgerFence
	}
	digest, err := ledgerC80CommandDigest(create.Command)
	if err != nil || digest != create.CommandDigest {
		return result, ErrRecordLedgerFence
	}
	result.CreateCommandDigest = digest
	if previews && (preview.ledgerPredecessorEffectBinding != binding || preview.CommandDigest != digest) {
		return result, ErrRecordLedgerFence
	}
	if reserves {
		reserveDigest, err := ledgerC80CommandDigest(reserve.Command)
		if err != nil || !previews || !ledgerC80ValidReserve(reserve.Command) || reserve.ledgerPredecessorEffectBinding != binding || reserveDigest != reserve.CommandDigest || reserve.Command.CreateCommandDigest != digest || reserve.Command.ReservationID != preview.ReservationID || reserve.Command.ActorID != intent.ActorUserID || reserve.Command.RequestFingerprint != create.Command.RequestFingerprint || reserve.Command.ExpectedBindingDigest != create.Command.BindingDigest {
			return result, ErrRecordLedgerFence
		}
		result.ReserveCommandDigest = reserveDigest
	} else if commits {
		return result, ErrRecordLedgerFence
	}
	err = ledgerPredecessorReadonly(ctx, f, func(tx pgx.Tx) error {
		original, found, err := ledgerC80ReadPreview(ctx, tx, create.Command)
		if err != nil {
			return err
		}
		if !found {
			if previews || reserves {
				return ErrRecordLedgerFence
			}
			return nil
		}
		result.ReservationID = original.ReservationID
		if previews && (original.ReservationID != preview.ReservationID || !original.ExpiresAt.Equal(preview.ExpiresAt)) {
			return ErrRecordLedgerFence
		}
		if reserves {
			operation, found, err := ledgerC80ReadOperation(ctx, tx, create.Command, reserve.Command)
			if err != nil {
				return err
			}
			if found {
				result.OperationID = operation
				result.Outcome = "reserve_committed"
				return nil
			}
			if commits {
				return ErrRecordLedgerFence
			}
			result.Outcome = "reserve_not_committed_preview_abandoned"
		} else {
			result.Outcome = "preview_abandoned"
		}
		return ledgerC80PreviewAbandonable(ctx, tx, create.Command, original)
	})
	if err != nil {
		return result, err
	}
	if commits && (committed.ledgerPredecessorEffectBinding != binding || committed.CreateCommandDigest != create.CommandDigest || committed.CommandDigest != reserve.CommandDigest || committed.ReservationID != result.ReservationID || committed.OperationID != result.OperationID || result.Outcome != "reserve_committed") {
		return result, ErrRecordLedgerFence
	}
	if outputs {
		if written.Result.Status == "committed" && (written.Result.OperationID != result.OperationID || result.Outcome != "reserve_committed" || written.Result.Reason != "app_reserved") {
			return result, ErrRecordLedgerFence
		}
	}
	return result, nil
}
