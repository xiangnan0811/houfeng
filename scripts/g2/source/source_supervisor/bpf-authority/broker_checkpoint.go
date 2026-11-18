//go:build linux && amd64 && !cgo

package main

import (
	"crypto/rand"
	"encoding/hex"
	"math"

	"golang.org/x/sys/unix"
)

type pendingCheckpointRequest struct {
	principal                                        *controlPrincipal
	requestID, deadline, watermark, readbackSequence uint64
	op, nonce                                        string
	raw                                              []byte
	readbackStarted, readbackDone                    bool
	beginCursor                                      uint64
	beginTail                                        string
	nested                                           bool
}

// deferredCheckpointReady admits only the fixed extra-FD witness. The retained
// child reference is evidence for this checkpoint, not grant retirement.
func (b *authorityBroker) deferredCheckpointReady(p *controlPrincipal) error {
	if b == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil {
		return errGuardProtocol
	}
	s := b.selftestBaseline
	var cookie uint64
	for id, item := range b.checkpoint.lifecycle.objects {
		if item != nil && item.deferredRemovalOnNextRelease {
			if cookie != 0 {
				return errGuardProtocol
			}
			cookie = id
		}
	}
	if cookie == 0 && (s == nil || s.name != "extra-fd-delays-removal") {
		return nil
	}
	if p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || s == nil || s.name != "extra-fd-delays-removal" || s.owner != p.ID || !validCheckpointNonce(s.nonce) || !s.observed || cookie == 0 || cookie != s.referenceCookie || s.notificationID == 0 || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken || (b.caseFacts["bpf_variant"] != nil && b.caseFacts["bpf_variant"] != "" && b.caseFacts["bpf_variant"] != "baseline") || b.effects == nil || b.effects.objects == nil || b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) || b.pendingSettlement != nil || b.pendingAdmit != nil || b.pendingStart != nil || b.pendingExecOp != nil || b.pendingObservation != nil || b.pendingContainerProbe != nil || b.pendingEndpoint != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.effects.pending != nil || b.checkpoint.activeTransactions != 0 {
		return errGuardProtocol
	}
	grant := b.grants[s.grantID]
	object := b.effects.objects.byCookie[cookie]
	item := b.checkpoint.lifecycle.objects[cookie]
	a := b.effects.attachments[s.extraAttachmentID]
	cleanup := b.effects.grantCleanup[cookie]
	load, detach := b.effects.transactions[s.transactionID], b.effects.transactions[s.secondTransactionID]
	if grant == nil || grant.id != s.grantID || grant.owner != p.ID || grant.phase != "active" || grant.settlementRequests != 0 || !grant.startObserved || !grant.createSettled || !grant.actualQueryConfirmed || object == nil || object.cookie != cookie || object.class != "program" || object.state != "live" || object.removalSeq != 0 || !object.removalAuthorized || object.ownerTransaction != s.transactionID || object.publicationSeq != s.publicationSeq || b.effects.objectGrant[cookie] != grant.id || item == nil || item.class != "program" || item.kernelID != object.id || item.removed || item.removalAllowed || item.deferredRemovalOwner != p.ID || a == nil || a.id != s.extraAttachmentID || a.grantID != grant.id || a.programCookie != cookie || (a.state != "detached" && a.state != "offlining" && a.state != "released") || cleanup == nil || cleanup.grantID != grant.id || cleanup.creatorID != s.transactionID || cleanup.cookie != cookie || cleanup.closeResult != 0 || cleanup.removalSequence != 0 || load == nil || load.id != s.transactionID || load.grantID != grant.id || load.invocationID != s.invocationID || load.command != "PROG_LOAD" || load.state != "retired" || detach == nil || detach.id != s.secondTransactionID || detach.grantID != grant.id || detach.command != "PROG_DETACH" || !detach.internalSelftest || detach.state != "retired" || detach.result != jsonI64(0) {
		return errGuardProtocol
	}
	if _, held := b.effects.objectFD[cookie]; held {
		return errGuardProtocol
	}
	attach := b.effects.transactions[a.transactionID]
	if attach == nil || attach.grantID != grant.id || attach.invocationID != s.invocationID || attach.command != "PROG_ATTACH" || attach.state != "retired" {
		return errGuardProtocol
	}
	if !sameJSON(s.transactionIDs, []any{jsonU64(attach.id), jsonU64(detach.id)}) || !sameJSON(detach.output, map[string]any{"attachment_id": jsonU64(a.id)}) {
		return errGuardProtocol
	}
	for _, tx := range b.effects.transactions {
		if tx != nil && tx.state != "retired" {
			return errGuardProtocol
		}
	}
	var actor *selftestProbeActor
	for _, candidate := range b.probeActors {
		if candidate != nil && candidate.taskCookie == s.actorCookie {
			if actor != nil {
				return errGuardProtocol
			}
			actor = candidate
		}
	}
	if actor == nil || actor.name != s.name || actor.parentCookie != p.TaskCookie || actor.listenerFD < 0 || actor.listenerID == 0 || actor.tid != actor.tgid || actor.startNS == 0 || !pidfdSelects(actor.pidfd, actor.tgid) || b.probeListeners[actor.listenerFD] != actor {
		return errGuardProtocol
	}
	var invocation *runtimeInvocation
	for _, candidate := range b.runtimeInvocations {
		if candidate != nil && candidate.id == s.invocationID {
			if invocation != nil {
				return errGuardProtocol
			}
			invocation = candidate
		}
	}
	if invocation == nil || invocation.grant != grant || invocation.grantID != grant.id || invocation.generation != grant.generation || invocation.command != "create" {
		return errGuardProtocol
	}
	var injected, detached, closed, retired, observed bool
	for _, entry := range b.transcript.entries[s.begin:] {
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		switch entry.record["kind"] {
		case "selftest_runtime_stage":
			if body["mode"] != s.name || body["nonce"] != s.nonce || body["phase"] != "injected" {
				continue
			}
			mutation, ok := body["mutation"].(map[string]any)
			fd, valid := exactNumber(mutation["installed_fd"])
			if injected || !ok || !exactKeys(mutation, "program_cookie", "listener_id", "installed_fd") || !valid || fd > math.MaxInt32 || body["grant_id"] != jsonU64(grant.id) || body["invocation_id"] != jsonU64(s.invocationID) || body["notifying_cookie"] != jsonU64(actor.taskCookie) || body["notification_id"] != jsonU64(s.notificationID) || mutation["program_cookie"] != jsonU64(cookie) || mutation["listener_id"] != jsonU64(actor.listenerID) {
				return errGuardProtocol
			}
			injected = true
		case "attachment":
			if body["id"] != jsonU64(a.id) || body["state"] != "detached" {
				continue
			}
			if detached || !injected || body["grant_id"] != jsonU64(grant.id) || body["program_cookie"] != jsonU64(cookie) || body["transaction_id"] != jsonU64(attach.id) {
				return errGuardProtocol
			}
			detached = true
		case "transaction":
			if body["id"] != jsonU64(detach.id) || body["state"] != "retired" {
				continue
			}
			if retired || !detached || body["grant_id"] != jsonU64(grant.id) || body["command"] != "PROG_DETACH" || body["result"] != jsonI64(0) || !sameJSON(body["output"], detach.output) {
				return errGuardProtocol
			}
			retired = true
		case "grant_object_cleanup":
			if body["object_cookie"] != jsonU64(cookie) || body["step"] != jsonU64(1) {
				continue
			}
			if closed || !detached || !sameJSON(body, grantCleanupBody(cleanup, 1, "close_ref", 0, nil)) {
				return errGuardProtocol
			}
			closed = true
		case "selftest_case":
			if body["name"] != s.name || body["nonce"] != s.nonce || body["phase"] != "observed" {
				continue
			}
			if observed || !closed || !retired || !sameJSON(body, s.record("observed", jsonU64(actor.taskCookie), nil, nil)) {
				return errGuardProtocol
			}
			observed = true
		}
	}
	if !injected || !detached || !closed || !retired || !observed {
		return errGuardProtocol
	}
	return nil
}

// startCheckpointRequest reserves the one checkpoint exchange. The caller has
// already authenticated the principal and processed exact replay. The event
// loop must stop admitting new effects as soon as this reservation exists.
func (b *authorityBroker) startCheckpointRequest(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || b.checkpoint == nil || b.checkpoint.invalid || b.reader == nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	id, ok := exactNumber(request["request_id"])
	if !ok || id == 0 || id == math.MaxUint64 || id != p.NextRequest {
		return errControlFrame
	}
	op, ok := request["operation"].(string)
	if !ok || !allowedOperation(p.Role, op) {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok {
		return errControlFrame
	}
	pending := &pendingCheckpointRequest{principal: p, requestID: id, op: op, raw: append([]byte(nil), raw...), beginCursor: b.checkpoint.cursor, beginTail: b.transcript.tail}
	switch op {
	case "QUIESCE":
		if !exactKeys(body, "nonce", "deadline_mono_ns") {
			return errControlFrame
		}
		pending.nonce, ok = body["nonce"].(string)
		if !ok || !validCheckpointNonce(pending.nonce) {
			return errControlFrame
		}
		if active := b.checkpoint.active; active != nil {
			if active.principalID != p.ID || active.nonce != pending.nonce || active.depth == 0 || active.depth >= 64 {
				return errControlFrame
			}
			pending.nested = true
		}
		pending.deadline, ok = exactNumber(body["deadline_mono_ns"])
		if !ok || pending.deadline == 0 {
			return errControlFrame
		}
		var err error
		pending.deadline, err = clampGuestDeadlineNS(pending.deadline)
		if err != nil {
			return err
		}
		if pending.nested && pending.deadline > b.checkpoint.active.deadlineNS {
			pending.deadline = b.checkpoint.active.deadlineNS
		}
	case "RELEASE":
		if !exactKeys(body, "nonce", "watermark") || b.checkpoint.active == nil {
			return errControlFrame
		}
		pending.nonce, ok = body["nonce"].(string)
		if !ok || !validCheckpointNonce(pending.nonce) {
			return errControlFrame
		}
		pending.watermark, ok = exactNumber(body["watermark"])
		if !ok || b.checkpoint.active.principalID != p.ID || b.checkpoint.active.nonce != pending.nonce || b.checkpoint.active.watermark != pending.watermark {
			return errControlFrame
		}
		pending.deadline = b.checkpoint.active.deadlineNS
	case "AUDIT_CUT":
		if !exactKeys(body, "nonce") || b.auditCutDone || b.checkpoint.active == nil || (p.Role != "REALM_ROOT" && p.Role != "RECOVERY_OBSERVER") {
			return errControlFrame
		}
		pending.nonce, ok = body["nonce"].(string)
		if !ok || !validCheckpointNonce(pending.nonce) || b.checkpoint.active.principalID != p.ID || b.checkpoint.active.nonce != pending.nonce {
			return errControlFrame
		}
		pending.deadline = b.checkpoint.active.deadlineNS
	default:
		return errControlFrame
	}
	b.pendingCheckpoint = pending
	return nil
}

// advanceCheckpointRequest performs bounded nonblocking work. A false done
// result means the event loop must continue servicing guard, audit, reader,
// notifications, committed cleanup and pidfd deaths before calling again.
func (b *authorityBroker) advanceCheckpointRequest() (p *controlPrincipal, ack []byte, done bool, err error) {
	if b == nil || b.pendingCheckpoint == nil || b.checkpoint == nil || b.reader == nil {
		return nil, nil, false, errGuardProtocol
	}
	q := b.pendingCheckpoint
	fail := func(e error) (*controlPrincipal, []byte, bool, error) {
		b.checkpoint.markFault()
		return q.principal, nil, true, e
	}
	now, e := monoNowNS()
	if e != nil {
		return fail(e)
	}
	if now >= q.deadline {
		return fail(unix.ETIMEDOUT)
	}
	if !livePIDFD(q.principal.PIDFD) || !pidfdSelects(b.reader.pidfd, b.reader.pid) || b.reader.invalid {
		return fail(errGuardProtocol)
	}
	if b.checkpoint.activeTransactions != 0 {
		return q.principal, nil, false, nil
	}
	if q.op == "QUIESCE" {
		if e = b.deferredCheckpointReady(q.principal); e != nil {
			return fail(e)
		}
		b.checkpoint.lifecycle.paused = true
	}
	status, ready, e := pollAuditWatermark(b.checkpoint.guard, b.checkpoint.audit, b.checkpoint.transcript, b.checkpoint.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
	if e != nil {
		return fail(e)
	}
	if !ready {
		return q.principal, nil, false, nil
	}
	if !q.readbackStarted {
		var nonceBytes [16]byte
		if _, e = rand.Read(nonceBytes[:]); e != nil {
			return fail(e)
		}
		nonce := hex.EncodeToString(nonceBytes[:])
		if !validCheckpointNonce(nonce) {
			return fail(errGuardProtocol)
		}
		check := "status"
		if q.op == "AUDIT_CUT" {
			check = "configuration"
		}
		if e = b.reader.begin(check, nonce, q.deadline); e != nil {
			return fail(e)
		}
		q.readbackStarted = true
	}
	if !q.readbackDone {
		ready, e := b.reader.service()
		if e != nil {
			return fail(e)
		}
		if !ready {
			return q.principal, nil, false, nil
		}
		status, evidence, e := b.reader.result()
		if e != nil {
			return fail(e)
		}
		c := b.checkpoint
		if q.op == "AUDIT_CUT" {
			if _, e = validateAuditConfiguration(evidence, true, q.principal.PID); e != nil {
				return fail(e)
			}
		}
		if !c.auditBaselineSet || status.enabled != 2 || status.failure != 1 || status.rate != 0 || status.backlogLimit != 16384 || status.lost != c.auditLost {
			return fail(errGuardProtocol)
		}
		if c.auditOwner != 0 {
			if status.pid != c.auditOwner {
				return fail(errGuardProtocol)
			}
		} else if status.pid != 0 && (q.principal.Role != "REALM_ROOT" && q.principal.Role != "RECOVERY_OBSERVER" || status.pid != q.principal.PID) {
			return fail(errGuardProtocol)
		}
		q.readbackSequence, _, e = b.transcript.append("audit_readback", evidence)
		if e != nil {
			return fail(e)
		}
		q.readbackDone = true
	}
	status, ready, e = pollAuditWatermark(b.checkpoint.guard, b.checkpoint.audit, b.checkpoint.transcript, b.checkpoint.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
	if e != nil {
		return fail(e)
	}
	if !ready {
		return q.principal, nil, false, nil
	}
	var response map[string]any
	if q.op == "QUIESCE" {
		if e = b.deferredCheckpointReady(q.principal); e != nil {
			return fail(e)
		}
		var watermark uint64
		var tail string
		if q.nested {
			active := b.checkpoint.active
			if active == nil || active.principalID != q.principal.ID || active.nonce != q.nonce || active.depth == 0 || active.depth >= 64 {
				return fail(errGuardProtocol)
			}
			active.depth++
			if q.deadline < active.deadlineNS {
				active.deadlineNS = q.deadline
			}
			watermark, tail = active.watermark, active.replyTail
		} else {
			watermark, tail, e = b.checkpoint.begin(q.principal.ID, q.nonce, q.deadline, status)
			if e != nil {
				return fail(e)
			}
		}
		response = map[string]any{"watermark": jsonU64(watermark), "tail_sha256": tail}
	} else if q.op == "AUDIT_CUT" {
		if !b.join.auditControlsSettled() {
			return q.principal, nil, false, nil
		}
		count, e := b.join.auditCutReady()
		if e != nil {
			return fail(e)
		}
		boundary, tail := uint64(len(b.transcript.entries)), b.transcript.tail
		if boundary == 0 || q.readbackSequence == 0 || q.readbackSequence > boundary {
			return fail(errGuardProtocol)
		}
		if _, _, e = b.transcript.append("audit_cut", map[string]any{"principal_id": jsonU64(q.principal.ID), "readback_sequence": jsonU64(q.readbackSequence), "boundary_sequence": jsonU64(boundary), "boundary_sha256": tail, "wire_count": jsonU64(count)}); e != nil {
			return fail(e)
		}
		if e = b.join.beginCoverage(); e != nil {
			return fail(e)
		}
		b.auditCutDone = true
		b.checkpoint.auditOwner = q.principal.PID
		response = map[string]any{"boundary_sequence": jsonU64(boundary), "boundary_sha256": tail, "wire_count": jsonU64(count)}
	} else {
		if e = b.deferredCheckpointReady(q.principal); e != nil {
			return fail(e)
		}
		watermark, tail, e := b.checkpoint.release(q.principal.ID, q.nonce, q.watermark, status)
		if e != nil {
			return fail(e)
		}
		response = map[string]any{"released": true, "watermark": jsonU64(watermark), "tail_sha256": tail}
	}
	ack, e = controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", response)
	if e != nil {
		return fail(e)
	}
	if e = q.principal.commitReply(q.requestID, q.raw, ack, nil); e != nil {
		return fail(e)
	}
	b.pendingCheckpoint = nil
	return q.principal, ack, true, nil
}

// expectPrincipalAuditClaim is installed only for the newly pinned root or
// recovery observer. These Python tasks are created after audit was enabled;
// their PID-only AUDIT_SET therefore requires its real syscall and EOE.
func (b *authorityBroker) expectPrincipalAuditClaim(p *controlPrincipal) error {
	if b == nil || b.join == nil || p == nil || p.TaskCookie == 0 || p.PID <= 1 || p.StartNS == 0 || !livePIDFD(p.PIDFD) {
		return errGuardProtocol
	}
	old, next := uint32(0), p.PID
	return b.join.expectControl(auditControlExpectation{Actor: auditControlActor{TaskCookie: p.TaskCookie, TGID: uint64(p.PID), TID: uint64(p.PID), StartNS: p.StartNS, ExeSHA256: p.ExecutableSHA256}, Phase: "unicast_claim", Change: "audit_pid", OldValue: &old, NewValue: &next, RequireSyscall: true})
}
