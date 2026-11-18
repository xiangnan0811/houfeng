//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

// dispatchBrokerControl consumes exactly one credentialled packet. A true
// pending result transfers responsibility for the connection to the caller:
// asynchronous checkpoint and admission handlers must finish before it closes.
func (b *authorityBroker) dispatchBrokerControl(fd int, diagnostic *brokerControlDiagnostic) (pending bool, err error) {
	if diagnostic != nil {
		*diagnostic = brokerControlDiagnostic{}
	}
	defer func() { diagnostic.finish(err) }()
	raw, cred, rights, err := receiveControl(fd, -1)
	if err != nil {
		return false, diagnostic.reject(brokerControlPointReceive, err)
	}
	retained := false
	defer func() {
		if !retained {
			for _, right := range rights {
				_ = unix.Close(right)
			}
		}
	}()
	var heldTask guardTask
	taskHeld := false
	defer func() {
		e := b.finishBrokerControlTask(heldTask, &taskHeld, diagnostic)
		err = diagnostic.afterTaskDrop(err, e)
	}()
	request, err := parseCanonicalControl(raw)
	if err != nil {
		return false, diagnostic.reject(brokerControlPointParse, err)
	}
	op, ok := request["operation"].(string)
	if !ok {
		return false, diagnostic.reject(brokerControlPointOperation, errControlFrame)
	}
	if diagnostic != nil {
		diagnostic.operation = brokerControlOperationFor(op)
		diagnostic.command = brokerControlCommandFor(request, diagnostic.operation)
	}
	if b.pendingObservation != nil && (op == "REGISTER_LAUNCH" || op == "BIND_RUNTIME") {
		return false, diagnostic.reject(brokerControlPointObservationGate, errControlFrame)
	}
	if b.runtimeRootState != runtimeRootReady {
		switch op {
		case "BIND_RUNTIME", "ADMIT", "SETTLE", "ADMIT_EXEC", "TRACK_EXEC", "CANCEL_EXEC", "END_ENDPOINT_TRANSITION", "SELFTEST_RUNTIME_ARM":
			return false, diagnostic.reject(brokerControlPointRootGate, errControlFrame)
		}
	}
	if op == "BIND_RUNTIME" || op == "ADMIT" || op == "ADMIT_EXEC" {
		if err = b.revalidateRuntimeRoot(); err != nil {
			b.runtimeRootState = runtimeRootFailed
			b.checkpoint.markFault()
			return false, diagnostic.reject(brokerControlPointRootRecheck, err)
		}
	}
	if b.pendingContainerProbe != nil {
		return false, diagnostic.reject(brokerControlPointContainerGate, errControlFrame)
	}
	preprincipal := op == "REGISTER_LAUNCH" || op == "BIND_RUNTIME"
	var before guardStatus
	var beforeErr error
	beforeCount := len(b.transcript.entries)
	if preprincipal {
		before, beforeErr = b.authority.guard.status()
	}
	var ack []byte
	var armSuccessPrincipal *controlPrincipal
	var daemonRegistrationACK bool
	var armSuccessBefore guardStatus
	switch op {
	case "REGISTER_LAUNCH":
		if b.selftestFD != nil {
			return false, diagnostic.reject(brokerControlPointPendingReply, b.sendBrokerControlError(fd, 0, 1, raw, "BPF_AUTH_PENDING"))
		}
		ack, retained, err = b.registerLaunch(fd, raw, request, cred, rights)
		if err != nil {
			_ = diagnostic.reject(brokerControlPointLaunchHandler, err)
		}
	case "BIND_RUNTIME":
		if b.selftestFD != nil {
			body, valid := request["body"].(map[string]any)
			grant := b.grants[b.selftestFD.grantID]
			if !valid || grant == nil || body["container_id"] != grant.containerID || body["command"] != "create" {
				return false, diagnostic.reject(brokerControlPointPendingReply, b.sendBrokerControlError(fd, 0, 1, raw, "BPF_AUTH_PENDING"))
			}
		}
		if b.pendingCheckpoint == nil && b.pendingAdmit == nil && b.pendingSettlement == nil && b.pendingExecOp == nil {
			var queued bool
			queued, err = b.queueRuntimeStart(fd, raw, request, cred, rights)
			if err != nil {
				return false, diagnostic.reject(brokerControlPointStartQueue, err)
			}
			if queued {
				retained = true
				return true, nil
			}
		}
		var bindDiagnostic *brokerBindDiagnostic
		if diagnostic != nil {
			bindDiagnostic = &diagnostic.bind
		}
		ack, retained, err = b.bindRuntimeRequest(fd, raw, request, cred, rights, bindDiagnostic)
		if err != nil {
			_ = diagnostic.reject(brokerControlPointBindHandler, err)
		}
	default:
		var rejectionBefore guardStatus
		var rejectionBeforeErr error = errGuardProtocol
		if op == "SELFTEST_FD_ARM" && recordableFDArm(request) && len(rights) == 0 && b.caseFacts != nil && b.caseFacts["case"] == "realm-negative" && b.caseFacts["scenario"] == "bpf-authorization" && b.caseFacts["run_token"] == authorityRunToken && (b.caseFacts["bpf_variant"] == nil || b.caseFacts["bpf_variant"] == "" || b.caseFacts["bpf_variant"] == "baseline") {
			rejectionBefore, rejectionBeforeErr = b.authority.guard.status()
		}
		p, replay, task, e := b.authenticatePrincipal(fd, raw, request, cred, rights)
		if e != nil {
			_ = diagnostic.reject(brokerControlPointAuthenticate, e)
			if rejectionBeforeErr == nil {
				if deniedPrincipal, deniedTask, fresh := b.authenticateRejectedFDArm(fd, request, raw, cred); deniedPrincipal != nil {
					after, statusErr := b.authority.guard.status()
					if statusErr == nil && after.Sequence == rejectionBefore.Sequence && after.Sticky == 0 && after.Lost == 0 && after.ProbeMissed == 0 && !b.checkpoint.invalid {
						requestID, _ := exactNumber(request["request_id"])
						if recordErr := b.appendBaselineControlRejection(deniedPrincipal, requestID, op, request, raw, rejectionBefore.Sequence, after.Sequence); recordErr != nil {
							_ = b.authority.guard.dropTask(deniedTask.Cookie)
							b.checkpoint.markFault()
							return false, diagnostic.reject(brokerControlPointAuthDenial, recordErr)
						}
						response, ackErr := controlACK(b.reads, deniedPrincipal, requestID, raw, "BPF_AUTH_DENIED", map[string]any{"code": "BPF_AUTH_DENIED", "sequence": jsonU64(uint64(len(b.transcript.entries)))})
						if ackErr == nil && fresh {
							if deniedPrincipal.Transferred {
								ackErr = deniedPrincipal.commitTransferredFDArmDenial(requestID, raw, response)
							} else {
								ackErr = deniedPrincipal.commitReply(requestID, raw, response, nil)
							}
						}
						if dropErr := b.authority.guard.dropTask(deniedTask.Cookie); ackErr == nil {
							ackErr = dropErr
						}
						if ackErr != nil {
							b.checkpoint.markFault()
							return false, diagnostic.reject(brokerControlPointAuthDenial, ackErr)
						}
						if sendErr := sendControl(fd, response, nil); sendErr != nil {
							b.checkpoint.markFault()
							return false, diagnostic.reject(brokerControlPointAuthDenial, sendErr)
						}
						return false, nil
					}
					_ = b.authority.guard.dropTask(deniedTask.Cookie)
				}
			}
			id, idOK := exactNumber(request["principal_id"])
			requestID, requestOK := exactNumber(request["request_id"])
			if idOK && id != 0 && requestOK && requestID != 0 {
				return false, diagnostic.reject(brokerControlPointAuthDenial, b.sendBrokerControlError(fd, id, requestID, raw, "BPF_AUTH_DENIED"))
			}
			return false, e
		}
		heldTask, taskHeld = task, true
		if replay == nil {
			before := len(b.transcript.entries)
			status, statusErr := b.authority.guard.status()
			requestID, requestOK := exactNumber(request["request_id"])
			defer func() {
				if err == nil || !errors.Is(err, errControlFrame) || statusErr != nil || !requestOK || p.NextRequest != requestID || p.Transferred || p.Terminal || b.checkpoint.invalid || len(b.transcript.entries) != before {
					return
				}
				after, e := b.authority.guard.status()
				if e != nil || after.Sequence != status.Sequence || after.Sticky != 0 || after.Lost != 0 || after.ProbeMissed != 0 {
					return
				}
				if op == "SELFTEST_FD_ARM" && recordableFDArm(request) && b.caseFacts != nil && b.caseFacts["case"] == "realm-negative" && b.caseFacts["scenario"] == "bpf-authorization" && b.caseFacts["run_token"] == authorityRunToken && (b.caseFacts["bpf_variant"] == nil || b.caseFacts["bpf_variant"] == "" || b.caseFacts["bpf_variant"] == "baseline") {
					if e = b.appendBaselineControlRejection(p, requestID, op, request, raw, status.Sequence, after.Sequence); e != nil {
						b.checkpoint.markFault()
						err = diagnostic.reject(brokerControlPointDeferDenialAppend, e)
						return
					}
				}
				body := map[string]any{"code": "BPF_AUTH_DENIED", "sequence": jsonU64(uint64(len(b.transcript.entries)))}
				ack, e := controlACK(b.reads, p, requestID, raw, "BPF_AUTH_DENIED", body)
				if e != nil {
					return
				}
				if e = p.commitReply(requestID, raw, ack, rights); e != nil {
					return
				}
				retained = true
				if e = b.finishBrokerControlTask(heldTask, &taskHeld, diagnostic); e != nil {
					err = diagnostic.reject(brokerControlPointTaskDrop, e)
					return
				}
				err = sendControl(fd, ack, nil)
				if err != nil {
					_ = diagnostic.reject(brokerControlPointDeferDenialSend, err)
				}
				if err != nil && op == "SELFTEST_FD_ARM" && b.selftestBaseline != nil {
					b.checkpoint.markFault()
				}
			}()
		}
		if replay != nil {
			if e := b.finishBrokerControlTask(heldTask, &taskHeld, diagnostic); e != nil {
				return false, diagnostic.reject(brokerControlPointTaskDrop, e)
			}
			if e := b.deliverReplayedBrokerACK(fd, replay.Ack, op, p); e != nil {
				return false, diagnostic.reject(brokerControlPointReplayACK, e)
			}
			if op == "SELFTEST_FD_ARM" && b.selftestBaseline != nil && b.selftestBaseline.name == "fd-arm-replay" && b.selftestBaseline.owner == p.ID && rejectionBeforeErr == nil {
				after, e := b.authority.guard.status()
				if e != nil || after.Sequence != rejectionBefore.Sequence || after.Sticky != 0 || after.Lost != 0 || after.ProbeMissed != 0 {
					b.checkpoint.markFault()
					return false, diagnostic.reject(brokerControlPointReplayPostACK, errGuardProtocol)
				}
				requestID, _ := exactNumber(request["request_id"])
				if e = b.appendBaselineControlSuccess(p, requestID, request, raw, rejectionBefore.Sequence, after.Sequence); e != nil {
					b.checkpoint.markFault()
					return false, diagnostic.reject(brokerControlPointReplayPostACK, e)
				}
			}
			return false, nil
		}
		if b.pendingObservation != nil && op != "READ_STATUS" && op != "READ_EVIDENCE" {
			return false, diagnostic.reject(brokerControlPointAuthenticatedGate, errControlFrame)
		}
		if b.selftestFD != nil {
			switch op {
			case "REGISTER_REALM", "REGISTER_RECOVERY", "REGISTER_DAEMON", "QUIESCE", "RELEASE", "AUDIT_CUT":
				return false, diagnostic.reject(brokerControlPointAuthenticatedGate, errControlFrame)
			}
		}
		if b.pendingStart != nil {
			switch op {
			case "ADMIT", "SETTLE", "QUIESCE", "RELEASE", "AUDIT_CUT":
				return false, diagnostic.reject(brokerControlPointAuthenticatedGate, errControlFrame)
			}
		}
		switch op {
		case "REGISTER_REALM":
			ack, retained, err = b.registerRealm(p, request, raw, rights)
		case "REGISTER_RECOVERY":
			ack, retained, err = b.registerRecovery(p, request, raw, rights)
		case "REGISTER_DAEMON":
			if err = b.installBrokerLineage(); err != nil {
				return false, diagnostic.reject(brokerControlPointLineageInstall, err)
			}
			ack, err = b.registerDaemon(p, request, raw, rights)
			retained = err == nil
			daemonRegistrationACK = err == nil
		case "ADMIT":
			if len(rights) != 0 || b.pendingSettlement != nil || b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startAdmitContainer(p, request, raw); err == nil {
				return true, nil
			}
		case "SETTLE":
			if len(rights) != 0 {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startBrokerSettlement(p, request, raw); err == nil {
				return true, nil
			}
		case "ADMIT_EXEC":
			if len(rights) != 0 || b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startAdmitExec(p, request, raw); err == nil {
				return true, nil
			}
		case "TRACK_EXEC":
			if len(rights) != 0 || b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startTrackExec(p, request, raw); err == nil {
				return true, nil
			}
		case "CANCEL_EXEC":
			if len(rights) != 0 || b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			var pendingCancel bool
			ack, pendingCancel, err = b.startCancelExec(p, request, raw)
			if err == nil && pendingCancel {
				return true, nil
			}
		case "BEGIN_ENDPOINT_TRANSITION":
			if b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			ack, retained, err = b.beginEndpointTransition(p, request, raw, rights)
		case "END_ENDPOINT_TRANSITION":
			if len(rights) != 0 || b.selftestFD != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startEndEndpointTransition(p, request, raw); err == nil {
				return true, nil
			}
		case "SELFTEST_FD_ARM":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			ack, err = b.armSelftestFD(p, request, raw)
			if err == nil && b.selftestBaseline != nil && b.selftestBaseline.name == "fd-arm-replay" && b.selftestBaseline.owner == p.ID && rejectionBeforeErr == nil {
				armSuccessPrincipal, armSuccessBefore = p, rejectionBefore
			}
		case "REGISTER_SELFTEST_PROBE":
			if b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			body, _ := request["body"].(map[string]any)
			if body["name"] == "container-raw-denial" {
				if err = b.startContainerSelftestProbe(p, request, raw, rights); err == nil {
					retained = true
					return true, nil
				}
				break
			}
			ack, retained, err = b.registerSelftestProbe(p, request, raw, rights)
		case "SELFTEST_RUNTIME_ARM":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			ack, err = b.armSelftestRuntime(p, request, raw)
		case "SELFTEST_BASELINE_STEP":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			ack, err = b.startSelftestBaseline(p, request, raw)
		case "SELFTEST_REVOKE_AND_CLOSE":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			ack, err = b.selftestRevokeAndClose(p, request, raw)
		case "SELFTEST_OBSERVATION_FAULT":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			body, _ := request["body"].(map[string]any)
			variant, _ := body["variant"].(string)
			switch variant {
			case "reader-death", "reader-endpoint", "reader-timeout", "reader-replay", "reader-credential", "reader-extra-fd", "reader-frame", "reader-done":
				if err = b.startReaderObservationFault(p, request, raw, fd); err == nil {
					return true, nil
				}
				return false, diagnostic.reject(brokerControlPointHandler, err)
			}
			ack, err = b.selftestObservationFault(p, request, raw)
		case "QUIESCE", "RELEASE", "AUDIT_CUT":
			if len(rights) != 0 || b.pendingSettlement != nil {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if err = b.startCheckpointRequest(p, request, raw); err == nil {
				return true, nil
			}
		case "READ_STATUS", "READ_EVIDENCE":
			if len(rights) != 0 {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			body, valid := request["body"].(map[string]any)
			id, idOK := exactNumber(request["request_id"])
			if !valid || !idOK {
				return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
			}
			if b.checkpoint.invalid {
				ack, err = controlACK(b.reads, p, id, raw, "BPF_AUTH_UNAVAILABLE", map[string]any{"code": "BPF_AUTH_UNAVAILABLE", "sequence": jsonU64(uint64(len(b.transcript.entries)))})
			} else if op == "READ_STATUS" {
				if !exactKeys(body) {
					return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
				}
				response, status := b.reads.readStatus()
				ack, err = controlACK(b.reads, p, id, raw, status, response)
			} else {
				ack, err = b.reads.readEvidenceACK(p, id, raw, body)
			}
			if err == nil {
				err = p.commitReply(id, raw, ack, nil)
			}
		default:
			return false, diagnostic.reject(brokerControlPointHandlerGuard, errControlFrame)
		}
		if err != nil {
			_ = diagnostic.reject(brokerControlPointHandler, err)
		}
	}
	if err != nil {
		if op == "BIND_RUNTIME" && beforeErr == nil && !retained && b.selftestBaseline != nil && b.selftestBaseline.witnessed && !b.selftestBaseline.ackDelivered && b.selftestBaseline.requestHash == hashBytes(raw) {
			after, e := b.authority.guard.status()
			if e != nil || after.Sequence < before.Sequence || after.Sticky != 0 || after.Lost != 0 || after.ProbeMissed != 0 || b.checkpoint.invalid {
				b.checkpoint.markFault()
				return false, diagnostic.reject(brokerControlPointDenialGuard, errGuardProtocol)
			}
			if e = b.appendBaselineControlRejection(nil, 1, op, request, raw, before.Sequence, after.Sequence); e != nil {
				b.checkpoint.markFault()
				return false, diagnostic.reject(brokerControlPointDenialAppend, e)
			}
			if e = b.sendBrokerControlError(fd, 0, 1, raw, "BPF_AUTH_DENIED"); e != nil {
				b.checkpoint.markFault()
				return false, diagnostic.reject(brokerControlPointDenialReply, e)
			}
			b.selftestBaseline.ackDelivered = true
			return false, nil
		}
		if preprincipal && beforeErr == nil && !retained && !b.checkpoint.invalid && len(b.transcript.entries) == beforeCount {
			after, e := b.authority.guard.status()
			if e == nil && after.Sequence == before.Sequence && after.Sticky == 0 && after.Lost == 0 && after.ProbeMissed == 0 {
				return false, diagnostic.reject(brokerControlPointDenialReply, b.sendBrokerControlError(fd, 0, 1, raw, "BPF_AUTH_DENIED"))
			}
		}
		return false, err
	}
	if len(ack) == 0 {
		return false, diagnostic.reject(brokerControlPointACKEmpty, errControlFrame)
	}
	if err = b.finishBrokerControlTask(heldTask, &taskHeld, diagnostic); err != nil {
		return false, diagnostic.reject(brokerControlPointTaskDrop, err)
	}
	if err = b.deliverBrokerACK(fd, ack, daemonRegistrationACK); err != nil {
		return false, diagnostic.reject(brokerControlPointACKDeliver, err)
	}
	if armSuccessPrincipal != nil {
		after, e := b.authority.guard.status()
		if e != nil || after.Sequence != armSuccessBefore.Sequence || after.Sticky != 0 || after.Lost != 0 || after.ProbeMissed != 0 {
			b.checkpoint.markFault()
			return false, diagnostic.reject(brokerControlPointPostACK, errGuardProtocol)
		}
		requestID, _ := exactNumber(request["request_id"])
		if e = b.appendBaselineControlSuccess(armSuccessPrincipal, requestID, request, raw, armSuccessBefore.Sequence, after.Sequence); e != nil {
			b.checkpoint.markFault()
			return false, diagnostic.reject(brokerControlPointPostACK, e)
		}
	}
	return false, nil
}

// deliverBrokerACK closes the watched epoch if its successful registration
// response cannot reach the registering principal, including exact replays.
func (b *authorityBroker) deliverBrokerACK(fd int, ack []byte, daemonRegistration bool) error {
	err := sendControl(fd, ack, nil)
	if err != nil && daemonRegistration {
		b.checkpoint.markFault()
	}
	return err
}

func (b *authorityBroker) deliverReplayedBrokerACK(fd int, ack []byte, op string, p *controlPrincipal) error {
	err := sendControl(fd, ack, nil)
	if err != nil && op == "REGISTER_DAEMON" && b.matchesDaemonRegistrationACK(p, ack) {
		b.checkpoint.markFault()
	}
	return err
}
func (b *authorityBroker) matchesDaemonRegistrationACK(p *controlPrincipal, ack []byte) bool {
	if b == nil || p == nil {
		return false
	}
	reg := b.daemons[p.ID]
	if reg == nil {
		return false
	}
	decoded, err := parseCanonicalControl(ack)
	if err != nil || decoded["status"] != "OK" {
		return false
	}
	body, ok := decoded["body"].(map[string]any)
	return ok && exactKeys(body, "daemon_cookie", "watch_sequence") &&
		body["daemon_cookie"] == jsonU64(reg.cookie) && body["watch_sequence"] == jsonU64(reg.watchSequence)
}

func recordableFDArm(request map[string]any) bool {
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "nonce", "grant_id", "class") {
		return false
	}
	nonce, ok := body["nonce"].(string)
	grant, valid := exactNumber(body["grant_id"])
	class, classOK := body["class"].(string)
	return ok && validCheckpointNonce(nonce) && valid && grant != 0 && classOK && (class == "map" || class == "program")
}

// authenticateRejectedFDArm identifies the sender independently of the arm
// authorization. It never executes the denied operation or accepts rights.
func (b *authorityBroker) authenticateRejectedFDArm(fd int, request map[string]any, raw []byte, cred *unix.Ucred) (*controlPrincipal, guardTask, bool) {
	var empty guardTask
	if !exactKeys(request, "version", "boot_id", "epoch", "run_token", "principal_id", "request_id", "operation", "body") || request["version"] != jsonU64(1) || request["boot_id"] != b.bootID || request["epoch"] != b.epoch || request["run_token"] != authorityRunToken || request["operation"] != "SELFTEST_FD_ARM" {
		return nil, empty, false
	}
	principalID, validID := exactNumber(request["principal_id"])
	requestID, validRequest := exactNumber(request["request_id"])
	p := b.principals[principalID]
	body, validBody := request["body"].(map[string]any)
	if !validID || !validRequest || p == nil || !validBody || !exactKeys(body, "nonce", "grant_id", "class") {
		return nil, empty, false
	}
	if requestID < p.NextRequest {
		old, ok := p.Replay[requestID]
		if !ok || len(old.HeldFDs) != 0 || bytes.Equal(old.Request, raw) {
			return nil, empty, false
		}
		oldRequest, err := parseCanonicalControl(old.Request)
		if err != nil || oldRequest["operation"] != "SELFTEST_FD_ARM" {
			return nil, empty, false
		}
		authenticated, replay, task, err := b.authenticatePrincipal(fd, old.Request, oldRequest, cred, nil)
		if err != nil || authenticated != p || replay == nil {
			return nil, empty, false
		}
		return p, task, false
	}
	if s := b.selftestBaseline; s != nil && s.name == "fd-arm-wrong-role" && p.ID == b.bootOwner && p.Role == "BOOT_OWNER" && p.Transferred && requestID == p.NextRequest && requestID != math.MaxUint64 && livePIDFD(p.TransferChildPIDFD) {
		if cred == nil || cred.Pid <= 0 {
			return nil, empty, false
		}
		peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
			return nil, empty, false
		}
		task, err := b.authority.guard.pinTask(uint32(cred.Pid))
		if err != nil {
			return nil, empty, false
		}
		probe := make(map[string]any, len(request))
		for key, value := range request {
			probe[key] = value
		}
		probe["operation"] = "READ_STATUS"
		probeRaw, err := canonicalControl(probe)
		check := *p
		check.Transferred = false
		if err == nil {
			var replay *controlReplay
			replay, err = check.authenticate(probe, probeRaw, cred, task, b.bootID, b.epoch, authorityRunToken, nil)
			if replay != nil {
				err = errControlFrame
			}
		}
		if err != nil {
			_ = b.authority.guard.dropTask(task.Cookie)
			return nil, empty, false
		}
		return p, task, true
	}
	if p.Role == "REALM_ROOT" || requestID != p.NextRequest || !allowedOperation(p.Role, "READ_STATUS") {
		return nil, empty, false
	}
	probe := make(map[string]any, len(request))
	for key, value := range request {
		probe[key] = value
	}
	probe["operation"] = "READ_STATUS"
	probeRaw, err := canonicalControl(probe)
	if err != nil {
		return nil, empty, false
	}
	authenticated, replay, task, err := b.authenticatePrincipal(fd, probeRaw, probe, cred, nil)
	if err != nil || authenticated != p || replay != nil {
		return nil, empty, false
	}
	return p, task, true
}

// appendBaselineControlRejection records a real rejected control frame and
// the guard bounds measured by the dispatcher around its processing.
func (b *authorityBroker) appendBaselineControlRejection(p *controlPrincipal, requestID uint64, op string, request map[string]any, raw []byte, before, after uint64) error {
	if requestID == 0 || op == "" || request == nil || len(raw) == 0 || after < before {
		return errGuardProtocol
	}
	var principal, nonce any
	if p != nil {
		if p.ID == 0 {
			return errGuardProtocol
		}
		principal = jsonU64(p.ID)
		if body, ok := request["body"].(map[string]any); ok {
			nonce = body["nonce"]
		}
	}
	return appendRuntimeRecord(b, "control_rejection", map[string]any{
		"principal_id": principal, "request_id": jsonU64(requestID), "operation": op,
		"nonce": nonce, "body": request["body"], "request_sha256": hashBytes(raw),
		"status": "BPF_AUTH_DENIED", "code": "BPF_AUTH_DENIED",
		"guard_before": jsonU64(before), "guard_after": jsonU64(after),
	})
}

func (b *authorityBroker) appendBaselineControlSuccess(p *controlPrincipal, requestID uint64, request map[string]any, raw []byte, before, after uint64) error {
	if p == nil || p.ID == 0 || requestID == 0 || !recordableFDArm(request) || len(raw) == 0 || after < before {
		return errGuardProtocol
	}
	body := request["body"].(map[string]any)
	return appendRuntimeRecord(b, "control_rejection", map[string]any{
		"principal_id": jsonU64(p.ID), "request_id": jsonU64(requestID), "operation": "SELFTEST_FD_ARM",
		"nonce": body["nonce"], "body": body, "request_sha256": hashBytes(raw), "status": "OK", "code": nil,
		"guard_before": jsonU64(before), "guard_after": jsonU64(after),
	})
}

func (b *authorityBroker) sendBrokerControlError(fd int, principalID, requestID uint64, raw []byte, code string) error {
	if b == nil || b.reads == nil || b.transcript == nil || requestID == 0 || (code != "BPF_AUTH_DENIED" && code != "BPF_AUTH_UNAVAILABLE" && code != "BPF_AUTH_PENDING" && code != "BPF_AUTH_INCOMPATIBLE") {
		return errControlFrame
	}
	p := &controlPrincipal{ID: principalID}
	body := map[string]any{"code": code, "sequence": jsonU64(uint64(len(b.transcript.entries)))}
	ack, err := controlACK(b.reads, p, requestID, raw, code, body)
	if err != nil {
		return err
	}
	return sendControl(fd, ack, nil)
}
