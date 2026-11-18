//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"

	"golang.org/x/sys/unix"
)

// executeBrokerCreation executes one already-authorized, privately captured
// creation. The caller must finish actor, invocation, grant and per-invocation
// policy checks before calling it. A successful call leaves the transaction in
// response_settled; the event loop must still join audit and, where applicable,
// observe removal before retiring it.
func (b *authorityBroker) executeBrokerCreation(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect, nullFD int) (*runtimeEffectTransaction, error) {
	if b == nil || b.checkpoint == nil || b.checkpoint.invalid || b.effects == nil || b.resolver == nil || inv == nil || h == nil || h.memory == nil || captured == nil {
		return nil, errGuardProtocol
	}
	if err := authorizeCreationCapture(inv, captured); err != nil {
		return nil, err
	}
	var name string
	var program, license, maximum any
	switch captured.command {
	case unix.BPF_MAP_CREATE:
		if !featureMapAttribute(captured.attribute) {
			return nil, errGuardProtocol
		}
		name = "MAP_CREATE"
		maximum = map[string]any{"object_cookie": jsonU64(math.MaxUint64)}
	case unix.BPF_PROG_LOAD:
		if captured.program == nil || len(captured.program.license) == 0 || captured.program.license[len(captured.program.license)-1] != 0 {
			return nil, errGuardProtocol
		}
		name = "PROG_LOAD"
		program = hex.EncodeToString(captured.program.instructions)
		license = string(captured.program.license[:len(captured.program.license)-1])
		maximum = map[string]any{"object_cookie": jsonU64(math.MaxUint64), "log_true_size": jsonU64(math.MaxUint32)}
	default:
		return nil, errGuardProtocol
	}
	rtStage := b.selftestRuntime
	isRuntimeStageTarget := rtStage != nil && rtStage.grantID == inv.grantID && name == "PROG_LOAD" && captured.programKind == "device" && rtStage.phase == "armed"
	var insnAddr uint64
	var origByte byte
	if isRuntimeStageTarget {
		rtStage.invocationID = inv.id
		rtStage.notifyingCookie = h.task.Cookie
		rtStage.notificationID = h.notice.ID
		if err := b.emitSelftestRuntimeStage(rtStage, "captured"); err != nil {
			b.checkpoint.markFault()
			return nil, err
		}
		if rtStage.mode == "nested-buffer-race" {
			insnAddr = guardU64(captured.program.outer, 8)
			if len(captured.program.instructions) == 0 || insnAddr == 0 {
				b.checkpoint.markFault()
				return nil, errGuardProtocol
			}
			origByte = captured.program.instructions[0]
			if err := runBoundedMutationWorker(h.memory, insnAddr, origByte, 0xff, false); err != nil {
				b.checkpoint.markFault()
				return nil, err
			}
			rtStage.mutation = map[string]any{
				"captured_sha256": hashBytes(captured.program.instructions),
				"changed_offset":  jsonU64(0),
				"original_byte":   jsonU64(uint64(origByte)),
				"mutated_byte":    jsonU64(0xff),
				"write_complete":  true,
				"restored":        false,
			}
			if err := b.emitSelftestRuntimeStage(rtStage, "mutated"); err != nil {
				b.checkpoint.markFault()
				return nil, err
			}
		}
	}
	if err := notificationValid(h.memory.listener, h.notice.ID); err != nil {
		return nil, err
	}
	t, err := b.prepareRuntimeEffect(inv, h, name, captured.normalized, program, license, []any{}, maximum)
	if err != nil {
		return nil, err
	}
	if isRuntimeStageTarget {
		rtStage.transactionID = t.id
		if rtStage.mode == "commit-before-cancel" {
			sigMap, sigErr := runBoundedSignalWorker(inv.pidfd, inv.taskCookie)
			if sigErr != nil {
				b.checkpoint.markFault()
				return t, sigErr
			}
			rtStage.signal = sigMap
			if err := b.emitSelftestRuntimeStage(rtStage, "cancelled"); err != nil {
				b.checkpoint.markFault()
				return t, err
			}
		}
	}
	// Once committed, any transport or copyout failure is unresolved. Never
	// repeat the syscall or convert an uncertain response into a retirement.
	if err = notificationValid(h.memory.listener, h.notice.ID); err != nil {
		if cancelErr := b.cancelPreparedRuntimeEffect(t, inv, h, err); cancelErr == nil {
			return t, nil
		}
		b.checkpoint.markFault()
		return t, err
	}
	if err = b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	if isRuntimeStageTarget {
		if err := b.emitSelftestRuntimeStage(rtStage, "committed"); err != nil {
			b.checkpoint.markFault()
			return t, err
		}
	}
	if err = noteCreationCommitted(inv, captured); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	var effect bpfEffect
	stage := b.selftestFD
	if name == "MAP_CREATE" {
		if stage != nil && stage.class == "map" && stage.grantID == inv.grantID {
			effect, err = b.executeSelftestMap(t, h, captured.attribute, nullFD)
		} else {
			effect, err = executeFeatureMap(h.memory, h.notice, captured.attribute)
		}
	} else if stage != nil && stage.class == "program" && stage.grantID == inv.grantID && captured.programKind == "device" {
		effect, err = b.executeSelftestProgram(t, h, captured.program, nullFD)
	} else {
		effect, err = executeProgramLoad(h.memory, h.notice, captured.program)
	}
	if err != nil {
		if name == "PROG_LOAD" && captured.program != nil && len(captured.program.log) > 0 {
			clear(captured.program.log)
		}
		b.checkpoint.markFault()
		return t, err
	}
	t.effectExecuted, t.effectFD, t.effectErrno = true, effect.FD, effect.Errno
	t.policyErr = noteCreationResult(inv, captured, effect)
	result := int64(effect.FD)
	if effect.Errno != 0 {
		result = -int64(effect.Errno)
	}
	if err = b.resolver.recordTransaction(auditedTransaction{ID: t.id, ExecutorCookie: b.resolver.brokerCookie, Command: name, Number: uint64(captured.command), Result: result}); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	if captured.programKind == "extended_info_probe" && t.publication != nil {
		t.policyErr = errGuardProtocol
	}
	if t.policyErr != nil && effect.FD >= 0 {
		if err = b.beginCreationCleanup(t); err != nil {
			b.checkpoint.markFault()
			return t, err
		}
	}
	var cookie any
	if t.publication != nil {
		cookie = jsonU64(t.publication.ObjectCookie)
	}
	var output map[string]any
	if name == "MAP_CREATE" {
		output = map[string]any{"object_cookie": cookie}
	} else {
		var logTrueSize uint32
		if len(captured.program.returnedOuter) >= 144 {
			logTrueSize = guardU32(captured.program.returnedOuter, 140)
		}
		output = map[string]any{"object_cookie": cookie, "log_true_size": jsonU64(uint64(logTrueSize))}
	}
	if err = b.transitionRuntimeEffect(t, "effect_observed", jsonI64(result), nil, output); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	failureCleanup := b.selftestBaseline != nil && b.selftestBaseline.name == "failure-cleanup" && b.selftestBaseline.grantID == inv.grantID && name == "PROG_LOAD" && captured.programKind == "device" && t.publication != nil && effect.Errno == 0
	if failureCleanup {
		s := b.selftestBaseline
		if s.transactionID != 0 {
			return t, errGuardProtocol
		}
		if _, err = runBoundedSignalWorker(inv.pidfd, inv.taskCookie); err != nil {
			return t, err
		}
		if err = notificationValid(h.memory.listener, h.notice.ID); err != unix.ENOENT {
			return t, errGuardProtocol
		}
		s.invocationID, s.transactionID, s.notificationID = inv.id, t.id, h.notice.ID
	}
	if isRuntimeStageTarget {
		if err := b.emitSelftestRuntimeStage(rtStage, "effect_observed"); err != nil {
			b.checkpoint.markFault()
			return t, err
		}
		if rtStage.mode == "after-effect-cancel" {
			sigMap, sigErr := runBoundedSignalWorker(inv.pidfd, inv.taskCookie)
			if sigErr != nil {
				b.checkpoint.markFault()
				return t, sigErr
			}
			rtStage.signal = sigMap
			if err := b.emitSelftestRuntimeStage(rtStage, "cancelled"); err != nil {
				b.checkpoint.markFault()
				return t, err
			}
		} else if rtStage.mode == "nested-buffer-race" {
			if err := runBoundedMutationWorker(h.memory, insnAddr, origByte, 0xff, true); err != nil {
				b.checkpoint.markFault()
				return t, err
			}
			rtStage.mutation["restored"] = true
		}
	}
	if name == "PROG_LOAD" {
		if err = copyProgramLoadResult(h.memory, h.notice, captured.program); err != nil {
			if !(failureCleanup || isRuntimeStageTarget && rtStage.mode == "after-effect-cancel") {
				b.checkpoint.markFault()
				return t, err
			}
		}
	}
	var returned any
	var undelivered bool
	if t.policyErr != nil && effect.FD >= 0 {
		returned, undelivered, err = b.settleNotification(t, h, 0, unix.EPERM, -1)
	} else if effect.Errno != 0 {
		returned, undelivered, err = b.settleNotification(t, h, 0, effect.Errno, -1)
	} else {
		returned, undelivered, err = b.settleNotification(t, h, 0, 0, effect.FD)
	}
	if err != nil {
		return t, err
	}
	if undelivered && effect.FD >= 0 && !t.cleanupActive {
		if err = b.beginCreationCleanup(t); err != nil {
			b.checkpoint.markFault()
			return t, err
		}
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), returned, output); err != nil {
		b.checkpoint.markFault()
		return t, err
	}
	if isRuntimeStageTarget && !undelivered {
		if err := b.emitSelftestRuntimeStage(rtStage, "replied"); err != nil {
			b.checkpoint.markFault()
			return t, err
		}
		if rtStage.mode == "after-reply-cancel" {
			sigMap, sigErr := runBoundedSignalWorker(inv.pidfd, inv.taskCookie)
			if sigErr != nil {
				b.checkpoint.markFault()
				return t, sigErr
			}
			rtStage.signal = sigMap
			if err := b.emitSelftestRuntimeStage(rtStage, "cancelled"); err != nil {
				b.checkpoint.markFault()
				return t, err
			}
		}
	}
	return t, nil
}

// retireBrokerCreation waits for actual audit completion and, for a failed
// creation which nevertheless published an incarnation, the matching removal.
func (b *authorityBroker) retireBrokerCreation(t *runtimeEffectTransaction) (bool, error) {
	if b == nil || b.checkpoint == nil || b.effects == nil || b.effects.pending != t || t == nil || t.state != "response_settled" || b.checkpoint.invalid && !b.canDrainCommitted(t) {
		return false, errGuardProtocol
	}
	_, ready, err := pollAuditWatermark(b.checkpoint.guard, b.checkpoint.audit, b.checkpoint.transcript, b.checkpoint.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
	if err != nil {
		b.checkpoint.markFault()
		return false, err
	}
	if !ready {
		return false, nil
	}
	if t.command == "PROG_GET_FD_BY_ID" && t.cleanup["policy"] == "close_borrowed_ref" {
		alias := b.effects.aliases[t.id]
		if alias == nil || alias.transaction != t || !alias.responseKnown || alias.closed || alias.fd < 0 || t.executionReleased || b.checkpoint.activeTransactions == 0 {
			b.checkpoint.markFault()
			return false, errGuardProtocol
		}
		t.executionReleased = true
		b.effects.pending = nil
		b.checkpoint.activeTransactions--
		if !alias.delivered {
			if err = b.closeBorrowedAliases(alias.grantID, alias.invocationID, t.id); err != nil {
				b.checkpoint.markFault()
				return false, err
			}
		}
		return true, nil
	}
	if t.cleanup["state"] == "pending" {
		return false, nil
	}
	if err = b.transitionRuntimeEffect(t, "retired", t.result, t.returnedFD, t.output); err != nil {
		b.checkpoint.markFault()
		return false, err
	}
	if t.policyErr != nil {
		b.checkpoint.markFault()
		return false, t.policyErr
	}
	return true, nil
}

// importBrokerObject matches the file description imported from the actual
// notifying thread to one live object held for this grant. The descriptor slot
// is evidence only; all subsequent operations use the imported description.
func (b *authorityBroker) importBrokerObject(inv *runtimeInvocation, h *heldNotification, slot uint32, class string) (int, *objectIncarnation, error) {
	if b == nil || b.effects == nil || b.effects.objects == nil || inv == nil || inv.grant == nil || h == nil || (class != "" && class != "program" && class != "map") {
		return -1, nil, errGuardProtocol
	}
	fd, err := h.importFD(slot)
	if err != nil {
		return -1, nil, err
	}
	var match *objectIncarnation
	for cookie, held := range b.effects.objectFD {
		if b.effects.objectGrant[cookie] != inv.grantID || held < 0 {
			continue
		}
		object := b.effects.objects.byCookie[cookie]
		if object == nil || (class != "" && object.class != class) || object.state != "live" || object.removalSeq != 0 || object.removalAuthorized {
			continue
		}
		if sameHeldDescription(fd, held) {
			if match != nil {
				_ = unix.Close(fd)
				return -1, nil, errGuardProtocol
			}
			match = object
		}
	}
	for _, alias := range b.effects.aliases {
		if alias == nil || !alias.delivered || alias.closed || alias.fd < 0 || alias.grantID != inv.grantID {
			continue
		}
		object := b.effects.objects.byCookie[alias.cookie]
		if object == nil || (class != "" && object.class != class) || object.state != "live" || object.removalSeq != 0 || object.removalAuthorized {
			continue
		}
		if sameHeldDescription(fd, alias.fd) {
			if match != nil && match != object {
				_ = unix.Close(fd)
				return -1, nil, errGuardProtocol
			}
			match = object
		}
	}
	if match == nil || notificationValid(h.memory.listener, h.notice.ID) != nil {
		_ = unix.Close(fd)
		return -1, nil, errGuardProtocol
	}
	return fd, match, nil
}

// beginBrokerOperation records a fully checked operation and makes the final
// notification validity check before committing any SYS_BPF effect.
func (b *authorityBroker) beginBrokerOperation(inv *runtimeInvocation, h *heldNotification, command string, attr []byte, inputs []any, maximum any) (*runtimeEffectTransaction, bool, error) {
	t, err := b.prepareRuntimeEffect(inv, h, command, attr, nil, nil, inputs, maximum)
	if err != nil {
		return nil, false, err
	}
	if err = notificationValid(h.memory.listener, h.notice.ID); err != nil {
		if e := b.cancelPreparedRuntimeEffect(t, inv, h, err); e == nil {
			return t, false, nil
		}
		b.checkpoint.markFault()
		return t, false, err
	}
	if err = b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		b.checkpoint.markFault()
		return t, false, err
	}
	return t, true, nil
}

func (b *authorityBroker) recordBrokerOperation(t *runtimeEffectTransaction, number uint64, result int64, output any) error {
	if err := b.resolver.recordTransaction(auditedTransaction{ID: t.id, ExecutorCookie: b.resolver.brokerCookie, Command: t.command, Number: number, Result: result}); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if err := b.transitionRuntimeEffect(t, "effect_observed", jsonI64(result), nil, output); err != nil {
		b.checkpoint.markFault()
		return err
	}
	return nil
}

// dispatchBrokerInfo imports the actual caller's descriptor, identifies the
// live object held for this grant and uses only the imported file description.
func (b *authorityBroker) dispatchBrokerInfo(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect) (resultErr error) {
	if captured == nil || len(captured.attribute) < 16 {
		return errGuardProtocol
	}
	slot := guardU32(captured.attribute, 0)
	fd, object, err := b.importBrokerObject(inv, h, slot, "")
	if err != nil {
		return err
	}
	release := true
	defer func() {
		if release {
			if e := unix.Close(fd); e != nil {
				b.checkpoint.markFault()
				if resultErr == nil {
					resultErr = e
				}
			}
		}
	}()
	if b.selftestFD != nil {
		creator := b.effects.transactions[object.ownerTransaction]
		valid := creator != nil && creator.grantID == inv.grantID && creator.invocationID == inv.id && creator.state == "retired" && creator.effectExecuted && creator.effectErrno == 0
		if valid && object.class == "map" {
			valid = creator.command == "MAP_CREATE"
		} else if valid && object.class == "program" {
			probe, e := probeProgram()
			valid = e == nil && creator.command == "PROG_LOAD" && object.programBytes == hex.EncodeToString(probe)
		} else {
			valid = false
		}
		if !valid {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
	}
	in, err := captureScalarInfo(h, captured.attribute, object.class)
	if err != nil {
		return err
	}
	inputs := []any{map[string]any{"field": "info.bpf_fd", "kind": object.class, "cookie": jsonU64(object.cookie), "requester_fd": jsonU64(uint64(slot))}}
	maximum := map[string]any{"info_len": jsonU64(65536), "object_cookie": jsonU64(object.cookie), "type": jsonU64(math.MaxUint32), "kernel_id": jsonU64(math.MaxUint32), "attributes": object.attributes}
	t, committed, err := b.beginBrokerOperation(inv, h, "OBJ_GET_INFO_BY_FD", in.normalized, inputs, maximum)
	if err != nil || !committed {
		return err
	}
	release = false
	effect, effectErr := executeScalarInfo(h, in, fd, object)
	if effect == nil {
		b.checkpoint.markFault()
		if effectErr == nil {
			return errGuardProtocol
		}
		return effectErr
	}
	var output any
	if effect.result == 0 && effect.attributes != nil {
		output = map[string]any{"info_len": jsonU64(uint64(effect.length)), "object_cookie": jsonU64(object.cookie), "type": jsonU64(uint64(effect.objectType)), "kernel_id": jsonU64(uint64(effect.kernelID)), "attributes": effect.attributes}
	}
	if err = b.recordBrokerOperation(t, unix.BPF_OBJ_GET_INFO_BY_FD, effect.result, output); err != nil {
		return err
	}
	if effectErr == nil {
		err = copyScalarInfo(h, in, effect)
		if err != nil {
			b.checkpoint.markFault()
			return err
		}
	} else {
		t.policyErr = effectErr
	}
	errno := unix.Errno(0)
	if effectErr != nil {
		errno = unix.EPERM
	} else if effect.result < 0 {
		errno = unix.Errno(-effect.result)
	}
	if _, _, err = b.settleNotification(t, h, 0, errno, -1); err != nil {
		return err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(effect.result), nil, output); err != nil {
		b.checkpoint.markFault()
	}
	if err == nil {
		release = true
	}
	return err
}

func (b *authorityBroker) dispatchBrokerGetID(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect) error {
	if b == nil || b.effects == nil || captured == nil || captured.getID == nil {
		return errGuardProtocol
	}
	object := b.effects.objects.latestByID[objectID{class: "program", id: uint64(captured.getID.programID)}]
	if object == nil || object.state != "live" || object.removalSeq != 0 || object.removalAuthorized || b.effects.objectGrant[object.cookie] != inv.grantID || b.effects.objectFD[object.cookie] < 0 {
		return errGuardProtocol
	}
	if _, present := b.effects.objectFD[object.cookie]; !present {
		return errGuardProtocol
	}
	maximum := map[string]any{"object_cookie": jsonU64(object.cookie)}
	step := map[string]any{"sequence": jsonU64(1), "action": "close_ref", "object_cookie": jsonU64(object.cookie), "attachment_id": nil, "guard_sequence": nil, "result": jsonI64(-4095)}
	shadow := &runtimeEffectTransaction{id: math.MaxUint64, grantID: inv.grantID, invocationID: inv.id, requesterCookie: h.task.Cookie, notificationID: h.notice.ID, command: "PROG_GET_FD_BY_ID", attributes: captured.normalized, inputs: []any{}, cleanup: map[string]any{"policy": "close_borrowed_ref", "state": "unknown", "steps": []any{step}}}
	if err := preflightRuntimeRecord(b, "transaction", shadow.body(b.resolver.brokerCookie, "retired", jsonI64(math.MinInt64), jsonU64(math.MaxUint32), maximum)); err != nil {
		return err
	}
	t, committed, err := b.beginBrokerOperation(inv, h, "PROG_GET_FD_BY_ID", captured.normalized, []any{}, maximum)
	if err != nil || !committed {
		return err
	}
	effect, err := openKnownProgram(h, captured.getID, object)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	result := int64(effect.FD)
	var output any
	if effect.Errno != 0 {
		result = -int64(effect.Errno)
	} else {
		output = map[string]any{"object_cookie": jsonU64(object.cookie)}
		if b.effects.aliases == nil {
			b.effects.aliases = make(map[uint64]*brokerBorrowedAlias)
		}
		if b.effects.aliases[t.id] != nil {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		t.effectExecuted, t.effectFD = true, effect.FD
		t.cleanup = map[string]any{"policy": "close_borrowed_ref", "state": "pending", "steps": []any{}}
		b.effects.aliases[t.id] = &brokerBorrowedAlias{transaction: t, grantID: inv.grantID, invocationID: inv.id, cookie: object.cookie, fd: effect.FD}
	}
	if err = b.recordBrokerOperation(t, unix.BPF_PROG_GET_FD_BY_ID, result, output); err != nil {
		return err
	}
	var returned any
	var undelivered bool
	if effect.Errno != 0 {
		returned, undelivered, err = b.settleNotification(t, h, 0, effect.Errno, -1)
	} else {
		returned, undelivered, err = b.settleNotification(t, h, 0, 0, effect.FD)
		if err == nil {
			b.effects.aliases[t.id].responseKnown = true
			b.effects.aliases[t.id].delivered = !undelivered
		}
	}
	if err != nil {
		return err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), returned, output); err != nil {
		b.checkpoint.markFault()
	}
	return err
}

func (b *authorityBroker) emitBrokerInvocation(inv *runtimeInvocation, state string) error {
	if inv == nil || inv.grant == nil || state == "" {
		return errGuardProtocol
	}
	var bundle, process, exec any
	if inv.bundleSHA != "" {
		bundle = inv.bundleSHA
	}
	if inv.processSHA != "" {
		process = inv.processSHA
	}
	if inv.execID != 0 {
		exec = jsonU64(inv.execID)
	}
	return appendRuntimeRecord(b, "invocation", map[string]any{"id": jsonU64(inv.id), "principal_id": jsonU64(inv.principalID), "task_cookie": jsonU64(inv.taskCookie), "daemon_cookie": jsonU64(inv.daemonCookie), "grant_id": jsonU64(inv.grantID), "container_id": inv.containerID, "generation": jsonU64(inv.generation), "command": inv.command, "config_sha256": inv.configSHA, "bundle_sha256": bundle, "process_sha256": process, "exec_id": exec, "listener_id": jsonU64(inv.listenerID), "ancestry": inv.ancestry, "state": state})
}

func (b *authorityBroker) denyRuntimeHandlerNotification(inv *runtimeInvocation, n seccompNotification, command uint32, handlerErr error) error {
	stage := b.reserveOwnerProbeHandlerDiagnostic(inv, command, handlerErr)
	denialErr := b.denyRuntimeNotification(inv, n)
	if stage != 0 {
		sendOwnerProbeRuntimeDiagnostic(inv.containerID, ownerProbeRuntimeDiagnostic{stage: stage}, handlerErr)
	}
	return denialErr
}

// dispatchBrokerNotification authenticates the actual notifying TID and holds
// its mm for every request. The caller keeps the sole broker OS thread locked.
func (b *authorityBroker) dispatchBrokerNotification(inv *runtimeInvocation, n seccompNotification, nullFD int) (err error) {
	if b.selftestFD != nil && b.selftestFD.replayOnly {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	identity, err := b.runtimeNotificationIdentity(inv)
	if err != nil {
		if b.selftestFD != nil {
			b.checkpoint.markFault()
			return err
		}
		return b.denyRuntimeNotification(inv, n)
	}
	h, err := holdRuntimeNotification(b.authority.guard, inv.listener, n, identity)
	if err != nil {
		if b.selftestFD != nil {
			b.checkpoint.markFault()
			return err
		}
		return b.denyRuntimeNotification(inv, n)
	}
	defer func() {
		if closeErr := h.close(); closeErr != nil {
			b.checkpoint.markFault()
			if err == nil {
				err = closeErr
			}
		}
	}()
	if err = b.recordRuntimeExec(inv); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if inv.state == "bound" {
		if e := b.emitBrokerInvocation(inv, "running"); e != nil {
			return e
		}
		inv.state = "running"
	}
	if inv.state != "running" {
		if b.selftestFD != nil {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		return b.denyRuntimeNotification(inv, n)
	}
	mutated, mutationErr := b.mutateSelftestRequest(inv, h)
	if mutationErr != nil {
		b.checkpoint.markFault()
		return mutationErr
	}
	captured, err := captureRuntimeEffect(inv, h, b.programLogScratch)
	if mutated && err == nil && b.selftestBaseline.name != "wrong-target" {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	if err != nil {
		if b.selftestFD != nil {
			b.checkpoint.markFault()
			return err
		}
		return b.denyRuntimeNotification(inv, n)
	}
	if stage := b.selftestFD; stage != nil {
		allowed := inv.grantID == stage.grantID && inv.command == "create"
		if allowed {
			switch captured.command {
			case unix.BPF_MAP_CREATE:
				// The fixed feature map is the map target or a program prerequisite.
			case unix.BPF_PROG_LOAD:
				allowed = captured.programKind == "extended_info_probe" || captured.programKind == "replace_probe" || stage.class == "program" && captured.programKind == "device"
			case unix.BPF_OBJ_GET_INFO_BY_FD, unix.BPF_PROG_QUERY:
				// The operation-specific handlers check the actual owned inputs.
			case unix.BPF_PROG_ATTACH:
				allowed = captured.attach != nil && captured.attach.attach && captured.attach.replace
			default:
				allowed = false
			}
		}
		if !allowed {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
	}
	switch captured.command {
	case unix.BPF_MAP_CREATE, unix.BPF_PROG_LOAD:
		_, err = b.executeBrokerCreation(inv, h, captured, nullFD)
	case unix.BPF_OBJ_GET_INFO_BY_FD:
		err = b.dispatchBrokerInfo(inv, h, captured)
	case unix.BPF_PROG_GET_FD_BY_ID:
		err = b.dispatchBrokerGetID(inv, h, captured)
	case unix.BPF_PROG_QUERY:
		err = b.dispatchBrokerQuery(inv, h, captured)
	case unix.BPF_PROG_ATTACH, unix.BPF_PROG_DETACH:
		err = b.dispatchBrokerAttach(inv, h, captured)
	default:
		return b.denyRuntimeNotification(inv, n)
	}
	if err != nil && b.selftestFD != nil {
		b.checkpoint.markFault()
		return err
	}
	if err != nil && !b.checkpoint.invalid && (b.effects == nil || b.effects.pending == nil) {
		return b.denyRuntimeHandlerNotification(inv, n, captured.command, err)
	}
	return err
}

func (b *authorityBroker) dispatchBrokerQuery(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect) error {
	if b == nil || b.effects == nil || inv == nil || inv.grant == nil || inv.command != "create" || captured == nil || captured.query == nil {
		return errGuardProtocol
	}
	owned := make(map[uint32]uint64)
	for cookie, owner := range b.effects.objectGrant {
		object := b.effects.objects.byCookie[cookie]
		if owner == inv.grantID && object != nil && object.class == "program" && object.state == "live" && !object.removalAuthorized && object.id <= math.MaxUint32 {
			if _, held := b.effects.objectFD[cookie]; !held {
				return errGuardProtocol
			}
			id := uint32(object.id)
			if id == 0 || owned[id] != 0 {
				return errGuardProtocol
			}
			owned[id] = cookie
		}
	}
	group, err := b.importGrantCgroup(inv, h, captured)
	if err != nil {
		return err
	}
	slot := guardU32(captured.attribute, 0)
	inputs := []any{map[string]any{"field": "query.target_fd", "kind": "cgroup", "cookie": jsonU64(group.Enrollment), "requester_fd": jsonU64(uint64(slot))}}
	idsMax, flagsMax := make([]any, 64), make([]any, 64)
	for i := range idsMax {
		idsMax[i], flagsMax[i] = jsonU64(math.MaxUint32), jsonU64(math.MaxUint32)
	}
	maximum := map[string]any{"attach_flags": jsonU64(math.MaxUint32), "program_ids": idsMax, "program_attach_flags": flagsMax}
	t, committed, err := b.beginBrokerOperation(inv, h, "PROG_QUERY", captured.normalized, inputs, maximum)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	if !committed {
		return nil
	}
	out, effectErr := executeQuery(h, captured.query, group, owned)
	if !out.Executed {
		b.checkpoint.markFault()
		if effectErr == nil {
			return errGuardProtocol
		}
		return effectErr
	}
	var output any
	if out.Errno == 0 || out.Errno == unix.ENOSPC {
		ids, flags := make([]any, len(out.ProgramIDs)), make([]any, len(out.ProgramAttachFlags))
		for i, value := range out.ProgramIDs {
			ids[i] = jsonU64(uint64(value))
		}
		for i, value := range out.ProgramAttachFlags {
			flags[i] = jsonU64(uint64(value))
		}
		output = map[string]any{"attach_flags": jsonU64(uint64(out.AttachFlags)), "program_ids": ids, "program_attach_flags": flags}
	}
	if err = b.recordBrokerOperation(t, unix.BPF_PROG_QUERY, out.Result, output); err != nil {
		return err
	}
	if effectErr == nil && out.Errno == 0 && len(out.ProgramIDs) != 0 {
		effectErr = b.confirmGrantQuery(inv, group, out)
	}
	if effectErr == nil {
		if err = copyQueryResult(h, captured.query, out); err != nil {
			b.checkpoint.markFault()
			return err
		}
	} else {
		t.policyErr = effectErr
	}
	errno := out.Errno
	if effectErr != nil {
		errno = unix.EPERM
	}
	if _, _, err = b.settleNotification(t, h, 0, errno, -1); err != nil {
		return err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(out.Result), nil, output); err != nil {
		b.checkpoint.markFault()
	}
	return err
}

// dispatchBrokerReplaceProbe executes the fixed source-backed negative probe
// against the actual imported /dev/null description, never a cgroup target.
func (b *authorityBroker) dispatchBrokerReplaceProbe(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect, programFD int, program *objectIncarnation, replacementFD int, replacement *objectIncarnation) (resultErr error) {
	if b == nil || b.effects == nil || inv == nil || inv.command != "create" || inv.replaceAttachCommitted || captured == nil || captured.attach == nil || !captured.attach.attach || !captured.attach.replace || captured.attach.flags != unix.BPF_F_ALLOW_MULTI|unix.BPF_F_REPLACE || program == nil || replacement == nil || program.cookie != replacement.cookie || program.objectType != unix.BPF_PROG_TYPE_CGROUP_DEVICE {
		return errGuardProtocol
	}
	probe, err := probeProgram()
	if err != nil || program.programBytes != hex.EncodeToString(probe) {
		return errGuardProtocol
	}
	creator := b.effects.transactions[program.ownerTransaction]
	if creator == nil || creator.invocationID != inv.id || creator.command != "PROG_LOAD" || creator.state != "retired" {
		return errGuardProtocol
	}
	target, targetCookie, err := b.importRuntimeDevNull(inv, h, captured.attach.targetSlot)
	if err != nil {
		return err
	}
	release := true
	defer func() {
		if release {
			if e := unix.Close(target); e != nil {
				b.checkpoint.markFault()
				if resultErr == nil {
					resultErr = e
				}
			}
		}
	}()
	inputs := []any{
		map[string]any{"field": "target_fd", "kind": "dev_null", "cookie": jsonU64(targetCookie), "requester_fd": jsonU64(uint64(captured.attach.targetSlot))},
		map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(program.cookie), "requester_fd": jsonU64(uint64(captured.attach.programSlot))},
		map[string]any{"field": "replace_bpf_fd", "kind": "program", "cookie": jsonU64(replacement.cookie), "requester_fd": jsonU64(uint64(captured.attach.replacementSlot))},
	}
	maximum := map[string]any{"attachment_id": jsonU64(math.MaxUint64)}
	t, committed, err := b.beginBrokerOperation(inv, h, "PROG_ATTACH", captured.normalized, inputs, maximum)
	if err != nil || !committed {
		return err
	}
	inv.replaceAttachCommitted = true
	release = false
	result, err := performAttach(h, captured.attach, target, programFD, replacementFD)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	output := map[string]any{"attachment_id": nil}
	if err = b.recordBrokerOperation(t, unix.BPF_PROG_ATTACH, result, output); err != nil {
		return err
	}
	if result != -int64(unix.EBADF) {
		t.policyErr = errGuardProtocol
		if result >= 0 {
			// An unexpected successful effect cannot be represented as the
			// expected negative probe or assumed to have no attachment.
			b.checkpoint.markFault()
			return errGuardProtocol
		}
	}
	if _, _, err = b.settleNotification(t, h, 0, unix.Errno(-result), -1); err != nil {
		return err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), nil, output); err != nil {
		b.checkpoint.markFault()
	}
	if err == nil {
		release = true
	}
	return err
}

// recordRuntimeExec binds native exec/incarnation evidence to the held package
// executable. Live tasks additionally require the executable readback below.
func (b *authorityBroker) recordRuntimeExec(inv *runtimeInvocation) (err error) {
	if b == nil || inv == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.lineage == nil || b.transcript == nil || b.checkpoint.invalid && !b.canObserveOwnerDeath() {
		return errGuardProtocol
	}
	lineage := b.checkpoint.lifecycle.lineage
	node := lineage.nodes[inv.taskCookie]
	if node == nil || node.cookie != inv.taskCookie || node.startNS != inv.startNS || node.tgid != inv.tgid || node.tid != inv.pid || node.tainted || node.role != "runc" || node.execSeq <= inv.wrapperExecSequence || node.generation == 0 {
		return errGuardProtocol
	}
	if inv.runtimeExecRecorded {
		if inv.runtimeExecGeneration != node.generation || inv.runtimeExecSequence != node.execSeq {
			return errGuardProtocol
		}
		return nil
	}
	if b.reportsOwnerProbeCreate(inv.grant, inv.containerID, inv.command) && inv.grant.id == inv.grantID && inv.grant.generation == inv.generation {
		defer func() {
			if err == nil && inv.runtimeExecRecorded {
				sendOwnerProbeRuntimeDiagnostic(inv.containerID, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageRuncExecObserved}, nil)
			}
		}()
	}
	if !node.exited && !pidfdSelects(inv.pidfd, inv.pid) {
		return errGuardProtocol
	}
	matched := false
	for _, entry := range b.transcript.entries {
		if entry.record["kind"] != "guard" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		record, ok := body["record"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		sequence, ok := exactNumber(record["sequence"])
		if !ok || sequence != node.execSeq {
			continue
		}
		kind, k := exactNumber(record["kind"])
		cookie, c := exactNumber(record["task_cookie"])
		start, s := exactNumber(record["task_start_ns"])
		detail, d := record["detail"].(map[string]any)
		if !k || !c || !s || !d || kind != 10 || cookie != inv.taskCookie || start != inv.startNS || matched {
			return errGuardProtocol
		}
		tgid, a := exactNumber(detail["tgid"])
		tid, z := exactNumber(detail["tid"])
		generation, g := exactNumber(detail["exec_generation"])
		dev, v := exactNumber(detail["exe_dev"])
		ino, n := exactNumber(detail["exe_ino"])
		if !a || !z || !g || !v || !n || tgid != uint64(inv.tgid) || tid != uint64(inv.pid) || generation != node.generation || dev != node.executable.dev || ino != node.executable.ino {
			return errGuardProtocol
		}
		matched = true
	}
	if !matched {
		return errGuardProtocol
	}
	pinned, ok := lineage.executables[node.executable]
	packageFile := b.manifest.Executables["runc"]
	if !ok || pinned.role != "runc" || pinned.file == nil || pinned.digest != packageFile.SHA256 || node.digest != packageFile.SHA256 {
		return errGuardProtocol
	}
	if node.exited {
		dead, e := pidfdGroupExited(inv.pidfd)
		if e != nil || !dead {
			return errGuardProtocol
		}
		digest, held, e := heldRegularDigest(int(pinned.file.Fd()), 128<<20)
		if e != nil || digest != packageFile.SHA256 {
			return errGuardProtocol
		}
		dev, e := kernelDevice(uint64(held.Dev))
		if e != nil || dev != node.executable.dev || held.Ino != node.executable.ino {
			return errGuardProtocol
		}
		body := map[string]any{"invocation_id": jsonU64(inv.id), "task_cookie": jsonU64(inv.taskCookie), "exec_generation": jsonU64(node.generation), "guard_sequence": jsonU64(node.execSeq), "exe_sha256": digest}
		if e = appendRuntimeRecord(b, "runtime_exec", body); e != nil {
			return e
		}
		inv.runtimeExecGeneration, inv.runtimeExecSequence, inv.runtimeExecRecorded = node.generation, node.execSeq, true
		return nil
	}
	task, err := b.authority.guard.pinTask(inv.pid)
	if err != nil {
		return err
	}
	defer func() {
		if e := b.authority.guard.dropTask(task.Cookie); e != nil {
			b.checkpoint.markFault()
			if err == nil {
				err = e
			}
		}
	}()
	if task.Cookie != inv.taskCookie || task.StartBoottimeNS != inv.startNS || task.TGID != inv.tgid || task.TID != inv.pid || !pidfdSelects(inv.pidfd, inv.pid) {
		return errGuardProtocol
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/exe", inv.pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	digest, actual, err := heldRegularDigest(fd, 128<<20)
	if err != nil || digest != packageFile.SHA256 {
		return errGuardProtocol
	}
	pinnedDigest, held, err := heldRegularDigest(int(pinned.file.Fd()), 128<<20)
	if err != nil || pinnedDigest != digest || !sameTrustedStat(actual, held) {
		return errGuardProtocol
	}
	dev, err := kernelDevice(uint64(actual.Dev))
	if err != nil || dev != node.executable.dev || actual.Ino != node.executable.ino || !pidfdSelects(inv.pidfd, inv.pid) {
		return errGuardProtocol
	}
	check, err := unix.Open(fmt.Sprintf("/proc/%d/exe", inv.pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var after unix.Stat_t
	err = unix.Fstat(check, &after)
	_ = unix.Close(check)
	if err != nil || !sameTrustedStat(actual, after) || !pidfdSelects(inv.pidfd, inv.pid) {
		return errGuardProtocol
	}
	body := map[string]any{"invocation_id": jsonU64(inv.id), "task_cookie": jsonU64(inv.taskCookie), "exec_generation": jsonU64(node.generation), "guard_sequence": jsonU64(node.execSeq), "exe_sha256": digest}
	if err = appendRuntimeRecord(b, "runtime_exec", body); err != nil {
		return err
	}
	inv.runtimeExecGeneration, inv.runtimeExecSequence, inv.runtimeExecRecorded = node.generation, node.execSeq, true
	return nil
}

// dispatchBrokerAttach validates all copied program inputs and actual imported
// descriptions before enrolling a cgroup or committing an attachment.
func (b *authorityBroker) dispatchBrokerAttach(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect) (resultErr error) {
	if b == nil || b.effects == nil || inv == nil || inv.grant == nil || h == nil || captured == nil || captured.attach == nil {
		return errGuardProtocol
	}
	in := captured.attach
	programFD, program, err := b.importBrokerObject(inv, h, in.programSlot, "program")
	if err != nil {
		return err
	}
	replacementFD := -1
	defer func() {
		if b.effects.pending != nil && b.effects.pending.state != "response_settled" && b.effects.pending.state != "retired" {
			return
		}
		for _, fd := range []int{replacementFD, programFD} {
			if fd >= 0 {
				if e := unix.Close(fd); e != nil {
					b.checkpoint.markFault()
					if resultErr == nil {
						resultErr = e
					}
				}
			}
		}
	}()
	var replacement *objectIncarnation
	if in.replace {
		replacementFD, replacement, err = b.importBrokerObject(inv, h, in.replacementSlot, "program")
		if err != nil {
			return err
		}
	}
	probe, err := probeProgram()
	if err != nil {
		return err
	}
	if program.programBytes == hex.EncodeToString(probe) {
		return b.dispatchBrokerReplaceProbe(inv, h, captured, programFD, program, replacementFD, replacement)
	}
	if b.selftestFD != nil {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	device, err := deviceProgram()
	if err != nil || program.objectType != unix.BPF_PROG_TYPE_CGROUP_DEVICE || program.programBytes != hex.EncodeToString(device) {
		return errGuardProtocol
	}
	var active *runtimeAttachment
	for _, attachment := range b.effects.attachments {
		if attachment.grantID == inv.grantID && attachment.state == "attached" {
			if active != nil {
				return errGuardProtocol
			}
			active = attachment
		}
	}
	if active != nil {
		current := b.effects.cgroups[inv.grantID]
		if current == nil || current.Retired || current.Enrollment != active.enrollment {
			return errGuardProtocol
		}
	}
	var replacementCookie uint64
	if replacement != nil {
		replacementCookie = replacement.cookie
	}
	if in.attach {
		if inv.command != "create" || inv.grant.phase != "admitted" {
			return errGuardProtocol
		}
		if b.effects.nextAttachment == 0 || b.effects.nextAttachment == math.MaxUint64 || len(b.effects.attachments) >= 4096 {
			return errGuardProtocol
		}
		maximumID := jsonU64(math.MaxUint64)
		if err = preflightRuntimeRecord(b, "attachment", map[string]any{"id": maximumID, "grant_id": maximumID, "enrollment": maximumID, "root_cookie": maximumID, "cgroup_id": maximumID, "program_cookie": maximumID, "attach_type": jsonU64(unix.BPF_CGROUP_DEVICE), "flags": jsonU64(math.MaxUint32), "replaces": maximumID, "transaction_id": maximumID, "offline_seq": maximumID, "release_seq": maximumID, "state": "offlining"}); err != nil {
			return err
		}
		if in.replace {
			if active == nil || replacement == nil || active.programCookie != replacement.cookie || program.cookie == replacement.cookie {
				return errGuardProtocol
			}
		} else if active != nil {
			return errGuardProtocol
		}
	} else {
		if active == nil || active.programCookie != program.cookie || b.effects.cgroups[inv.grantID] == nil {
			return errGuardProtocol
		}
	}
	group, err := b.importGrantCgroup(inv, h, captured)
	if err != nil {
		return err
	}
	if active != nil && active.enrollment != group.Enrollment {
		return errGuardProtocol
	}
	inputs := []any{
		map[string]any{"field": "target_fd", "kind": "cgroup", "cookie": jsonU64(group.Enrollment), "requester_fd": jsonU64(uint64(in.targetSlot))},
		map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(program.cookie), "requester_fd": jsonU64(uint64(in.programSlot))},
	}
	if replacement != nil {
		inputs = append(inputs, map[string]any{"field": "replace_bpf_fd", "kind": "program", "cookie": jsonU64(replacement.cookie), "requester_fd": jsonU64(uint64(in.replacementSlot))})
	}
	command := "PROG_DETACH"
	if in.attach {
		command = "PROG_ATTACH"
	}
	maximum := map[string]any{"attachment_id": jsonU64(math.MaxUint64)}
	if in.attach {
		step := map[string]any{"sequence": jsonU64(1), "action": "detach", "object_cookie": jsonU64(program.cookie), "attachment_id": jsonU64(math.MaxUint64), "guard_sequence": nil, "result": jsonI64(math.MinInt64)}
		shadow := &runtimeEffectTransaction{id: math.MaxUint64, grantID: inv.grantID, invocationID: inv.id, requesterCookie: h.task.Cookie, notificationID: h.notice.ID, command: command, attributes: captured.normalized, inputs: inputs, cleanup: map[string]any{"policy": "detach_then_close", "state": "settled", "steps": []any{step}}}
		if err = preflightRuntimeRecord(b, "transaction", shadow.body(b.resolver.brokerCookie, "retired", jsonI64(math.MinInt64), nil, maximum)); err != nil {
			return err
		}
	}
	t, committed, err := b.beginBrokerOperation(inv, h, command, captured.normalized, inputs, maximum)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	if !committed {
		return nil
	}
	if in.attach {
		err = b.activateEffectCgroup(group, inv.grant, inv, h, t)
		if err == nil {
			_, err = b.authorizeGrantAttachment(inv, group, program.cookie, replacementCookie, in.flags)
		}
	} else {
		err = b.authorizeEffectRuntimeRetirement(group, inv.grant, inv, t)
		if err == nil {
			_, err = b.authorizeGrantDetach(inv, group, program.cookie)
		}
	}
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	result, err := performAttach(h, in, group.HeldFD, programFD, replacementFD)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	if err = b.resolver.recordTransaction(auditedTransaction{ID: t.id, ExecutorCookie: b.resolver.brokerCookie, Command: command, Number: uint64(captured.command), Result: result}); err != nil {
		b.checkpoint.markFault()
		return err
	}
	var attachmentID any
	if result == 0 {
		var id uint64
		if in.attach {
			id, err = b.recordGrantAttachment(inv, t, group, program.cookie, replacementCookie, in.flags, result)
		} else {
			id, err = b.recordGrantDetach(inv, t, group, program.cookie, result)
		}
		if err != nil {
			b.checkpoint.markFault()
			return err
		}
		attachmentID = jsonU64(id)
	} else if result > 0 {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	output := map[string]any{"attachment_id": attachmentID}
	if err = b.transitionRuntimeEffect(t, "effect_observed", jsonI64(result), nil, output); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if in.attach && result == 0 {
		if err = b.observeAttachedProgram(inv, group, program); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	errno := unix.Errno(0)
	if result < 0 {
		errno = unix.Errno(-result)
	}
	_, undelivered, responseErr := b.settleNotification(t, h, 0, errno, -1)
	if responseErr != nil {
		return responseErr
	}
	t.responseUndelivered = undelivered && in.attach && result == 0
	if t.responseUndelivered {
		step := map[string]any{"sequence": jsonU64(1), "action": "detach", "object_cookie": jsonU64(program.cookie), "attachment_id": attachmentID, "guard_sequence": nil, "result": nil}
		t.cleanup = map[string]any{"policy": "detach_then_close", "state": "pending", "steps": []any{step}}
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), nil, output); err != nil {
		b.checkpoint.markFault()
	}
	return err
}

type brokerBorrowedAlias struct {
	transaction                      *runtimeEffectTransaction
	grantID, invocationID, cookie    uint64
	fd                               int
	responseKnown, delivered, closed bool
}

// closeBorrowedAliases closes only aliases with a known response and settled
// execution/audit. The original creator remains held; no absence is inferred.
func (b *authorityBroker) closeBorrowedAliases(grantID, invocationID, onlyID uint64) error {
	if b == nil || b.effects == nil || b.effects.objects == nil || grantID == 0 {
		return errGuardProtocol
	}
	ids := make([]uint64, 0)
	for id, alias := range b.effects.aliases {
		if alias != nil && (onlyID == 0 || id == onlyID) && alias.grantID == grantID && (invocationID == 0 || alias.invocationID == invocationID) && !alias.closed {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		alias := b.effects.aliases[id]
		t := alias.transaction
		object := b.effects.objects.byCookie[alias.cookie]
		creatorFD, held := b.effects.objectFD[alias.cookie]
		if t == nil || t.id != id || t.state != "response_settled" || !t.executionReleased || !alias.responseKnown || alias.fd < 0 || object == nil || object.state != "live" || object.removalSeq != 0 || b.effects.objectGrant[alias.cookie] != grantID || !held || creatorFD < 0 {
			return errGuardProtocol
		}
		if _, err := unix.FcntlInt(uintptr(creatorFD), unix.F_GETFD, 0); err != nil {
			return errGuardProtocol
		}
		err := unix.Close(alias.fd)
		alias.closed, alias.fd = true, -1
		var result any = jsonI64(0)
		state := "settled"
		if err != nil {
			state, result = "unknown", nil
			var errno unix.Errno
			if errors.As(err, &errno) && errno != 0 {
				result = jsonI64(-int64(errno))
			}
		}
		step := map[string]any{"sequence": jsonU64(1), "action": "close_ref", "object_cookie": jsonU64(alias.cookie), "attachment_id": nil, "guard_sequence": nil, "result": result}
		t.cleanup = map[string]any{"policy": "close_borrowed_ref", "state": state, "steps": []any{step}}
		if err != nil {
			_ = appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "response_settled", t.result, t.returnedFD, t.output))
			b.checkpoint.markFault()
			return err
		}
		if err = b.transitionRuntimeEffect(t, "retired", t.result, t.returnedFD, t.output); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	return nil
}

func (b *authorityBroker) closeInvocationAliases(inv *runtimeInvocation) error {
	if inv == nil {
		return errGuardProtocol
	}
	return b.closeBorrowedAliases(inv.grantID, inv.id, 0)
}

func (b *authorityBroker) closeGrantAliases(grant *brokerGrant) error {
	if grant == nil {
		return errGuardProtocol
	}
	return b.closeBorrowedAliases(grant.id, 0, 0)
}
