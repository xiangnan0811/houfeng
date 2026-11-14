//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func detachCleanupStep(a *runtimeAttachment, result any, state string) map[string]any {
	return map[string]any{"policy": "detach_then_close", "state": state, "steps": []any{map[string]any{"sequence": jsonU64(1), "action": "detach", "object_cookie": jsonU64(a.programCookie), "attachment_id": jsonU64(a.id), "guard_sequence": nil, "result": result}}}
}

// serviceUndeliveredAttachmentCleanup retains the original response-settled
// transaction while executing the uniquely preauthorized broker detach. The
// original cannot retire before the child syscall and real audit have settled.
func (b *authorityBroker) serviceUndeliveredAttachmentCleanup() error {
	if b == nil || b.effects == nil || b.effects.objects == nil || b.checkpoint == nil || b.resolver == nil || b.join == nil {
		return errGuardProtocol
	}
	if b.checkpoint.invalid && !b.canObserveOwnerDeath() {
		return nil
	}
	if child := b.effects.cleanupChild; child != nil {
		parent := child.cleanupParent
		a := b.effects.attachments[child.cleanupAttachment]
		if parent == nil || b.effects.pending != parent || parent.state != "response_settled" || child.state != "response_settled" || !child.internalDetach || a == nil {
			return errGuardProtocol
		}
		_, ready, err := pollAuditWatermark(b.checkpoint.guard, b.checkpoint.audit, b.checkpoint.transcript, b.checkpoint.join, &b.checkpoint.cursor, b.checkpoint.lifecycle)
		if err != nil {
			b.checkpoint.markFault()
			return err
		}
		if !ready {
			return nil
		}
		if child.result != jsonI64(0) || a.state != "detached" {
			b.checkpoint.markFault()
			return fmt.Errorf("BPF_AUTH_PENDING: attachment cleanup failed")
		}
		if b.checkpoint.activeTransactions == 0 {
			return errGuardProtocol
		}
		if err = appendRuntimeRecord(b, "transaction", child.body(b.resolver.brokerCookie, "retired", child.result, nil, child.output)); err != nil {
			b.checkpoint.markFault()
			return err
		}
		child.state = "retired"
		b.checkpoint.activeTransactions--
		parent.cleanup = detachCleanupStep(a, jsonI64(0), "settled")
		b.effects.cleanupChild = nil
		return nil
	}
	parent := b.effects.pending
	if parent == nil || parent.state != "response_settled" || parent.command != "PROG_ATTACH" || !parent.responseUndelivered {
		return nil
	}
	if b.pendingCheckpoint != nil || b.checkpoint.active != nil || b.selftestFD != nil {
		return nil
	}
	if parent.result != jsonI64(0) || parent.cleanup["policy"] != "detach_then_close" || parent.cleanup["state"] != "pending" {
		return errGuardProtocol
	}
	var a *runtimeAttachment
	for _, candidate := range b.effects.attachments {
		if candidate == nil || candidate.transactionID != parent.id || candidate.state != "attached" {
			continue
		}
		if a != nil {
			return errGuardProtocol
		}
		a = candidate
	}
	if a == nil || a.grantID != parent.grantID {
		return errGuardProtocol
	}
	grant := b.grants[a.grantID]
	group := b.effects.cgroups[a.grantID]
	object := b.effects.objects.byCookie[a.programCookie]
	fd, held := b.effects.objectFD[a.programCookie]
	if grant == nil || group == nil || group.Retired || group.HeldFD < 0 || group.Enrollment != a.enrollment || group.RootCookie != a.rootCookie || group.KernfsID != a.cgroupID || object == nil || object.class != "program" || object.state != "live" || object.removalSeq != 0 || b.effects.objectGrant[a.programCookie] != a.grantID || !held || fd < 0 {
		return errGuardProtocol
	}
	if grant.phase != "admitted" && grant.phase != "active" && grant.phase != "settling" && !b.revokedCleanupAllowed(grant) {
		return errGuardProtocol
	}
	if b.checkpoint.invalid && (!b.revokedCleanupAllowed(grant) || !b.canDrainCommitted(parent)) {
		return errGuardProtocol
	}
	for _, other := range b.effects.attachments {
		if other != a && other != nil && other.state == "attached" && other.grantID == a.grantID && other.enrollment == a.enrollment && other.programCookie == a.programCookie {
			return errGuardProtocol
		}
	}
	if b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 || len(b.effects.transactions) >= 4096 {
		return errGuardProtocol
	}
	attr := make([]byte, 16)
	guardPut32(attr, 8, unix.BPF_CGROUP_DEVICE)
	inputs := []any{
		map[string]any{"field": "target_fd", "kind": "cgroup", "cookie": jsonU64(group.Enrollment), "requester_fd": nil},
		map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(a.programCookie), "requester_fd": nil},
	}
	id := b.effects.nextTransaction
	t := &runtimeEffectTransaction{id: id, grantID: a.grantID, command: "PROG_DETACH", attributes: attr, inputs: inputs, state: "prepared", effectFD: -1, internalDetach: true, cleanupParent: parent, cleanupAttachment: a.id, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	maximum := map[string]any{"attachment_id": jsonU64(math.MaxUint64)}
	if err := preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), nil, maximum)); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id], b.effects.cleanupChild = t, t
	b.checkpoint.activeTransactions++
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "committed", nil, nil, nil)); err != nil {
		b.checkpoint.markFault()
		return err
	}
	t.state = "committed"
	actual := append([]byte(nil), attr...)
	guardPut32(actual, 0, uint32(group.HeldFD))
	guardPut32(actual, 4, uint32(fd))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_DETACH, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "PROG_DETACH", Number: unix.BPF_PROG_DETACH, Result: signed}); err != nil {
		b.checkpoint.markFault()
		return err
	}
	var attachmentID any
	if errno == 0 && result == 0 {
		a.state = "detached"
		if err := b.emitRuntimeAttachment(a); err != nil {
			b.checkpoint.markFault()
			return err
		}
		attachmentID = jsonU64(a.id)
	}
	output := map[string]any{"attachment_id": attachmentID}
	for _, state := range []string{"effect_observed", "response_settled"} {
		if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, state, jsonI64(signed), nil, output)); err != nil {
			b.checkpoint.markFault()
			return err
		}
		t.state = state
	}
	t.result, t.output = jsonI64(signed), output
	return nil
}
