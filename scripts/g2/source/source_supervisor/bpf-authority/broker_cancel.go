//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

// cancelPreparedRuntimeEffect performs the final ID_VALID check itself. Only
// the kernel's ENOENT can settle a prepared operation without a syscall.
func (b *authorityBroker) cancelPreparedRuntimeEffect(t *runtimeEffectTransaction, inv *runtimeInvocation, h *heldNotification, validationErr error) error {
	if b == nil || b.effects == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.lifecycle == nil || b.join == nil || b.audit == nil || t == nil || inv == nil || h == nil || h.memory == nil || !errors.Is(validationErr, unix.ENOENT) || b.effects.pending != t || t.state != "prepared" || t.effectExecuted || inv.id != t.invocationID || inv.listener != h.memory.listener || b.runtimeInvocations[inv.listener] != inv || h.notice.ID != t.notificationID || h.task.Cookie != t.requesterCookie || h.notice.Syscall != unix.SYS_BPF || h.notice.Arch != auditArchX8664 || h.notice.Args[0] > math.MaxUint32 || h.memory.id != h.notice.ID || !exactKeys(t.cleanup, "policy", "state", "steps") || t.cleanup["policy"] != "none" || t.cleanup["state"] != "not_needed" {
		return errGuardProtocol
	}
	steps, ok := t.cleanup["steps"].([]any)
	if !ok || len(steps) != 0 {
		return errGuardProtocol
	}
	c := b.checkpoint
	if err := drainGuardEvents(c.guard, c.transcript, c.join, &c.cursor, c.lifecycle); err != nil {
		c.markFault()
		return err
	}
	status, err := c.guard.status()
	if err != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != c.cursor || status.AckCursor != c.cursor {
		c.markFault()
		return errGuardProtocol
	}
	before := c.cursor
	body := map[string]any{"invocation_id": jsonU64(t.invocationID), "requester_cookie": jsonU64(t.requesterCookie), "notification_id": jsonU64(t.notificationID), "command": jsonU64(h.notice.Args[0]), "validation_errno": jsonU64(uint64(unix.ENOENT)), "guard_before": jsonU64(before), "guard_after": jsonU64(math.MaxUint64)}
	if err = preflightRuntimeRecord(b, "notification_cancel", body); err != nil {
		c.markFault()
		return err
	}
	if err = notificationValid(h.memory.listener, h.notice.ID); !errors.Is(err, unix.ENOENT) {
		c.markFault()
		return errGuardProtocol
	}
	if err = drainGuardEvents(c.guard, c.transcript, c.join, &c.cursor, c.lifecycle); err != nil {
		c.markFault()
		return err
	}
	status, err = c.guard.status()
	if err != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != c.cursor || status.AckCursor != c.cursor || c.cursor < before {
		c.markFault()
		return errGuardProtocol
	}
	body["guard_after"] = jsonU64(c.cursor)
	if err = appendRuntimeRecord(b, "notification_cancel", body); err != nil {
		c.markFault()
		return err
	}
	t.cancelRecorded = true
	return b.transitionRuntimeEffect(t, "retired", nil, nil, nil)
}
