//go:build linux && amd64 && !cgo

package main

import (
	"math"

	"golang.org/x/sys/unix"
)

// denyRuntimeNotification records only an actually sent EPERM response. The
// guard task is the notifying TID, even when it is not the authorized runc
// task. No BPF transaction or cleanup authority is created by this record.
func (b *authorityBroker) denyRuntimeNotification(inv *runtimeInvocation, n seccompNotification) error {
	if b == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.lifecycle == nil || b.join == nil || b.audit == nil || inv == nil || inv.id == 0 || inv.listener < 0 || b.runtimeInvocations[inv.listener] != inv || n.ID == 0 || n.TID == 0 || n.Syscall != unix.SYS_BPF || n.Arch != auditArchX8664 || n.Flags != 0 || n.Args[0] > math.MaxUint32 {
		return errGuardProtocol
	}
	if err := notificationValid(inv.listener, n.ID); err != nil {
		return err
	}
	task, err := b.authority.guard.pinTask(n.TID)
	if err != nil {
		return err
	}
	defer func() {
		if b.authority.guard.dropTask(task.Cookie) != nil {
			b.checkpoint.markFault()
		}
	}()
	if task.TID != n.TID || task.Cookie == 0 || task.StartBoottimeNS == 0 {
		return errGuardProtocol
	}
	c := b.checkpoint
	if err = drainGuardEvents(c.guard, c.transcript, c.join, &c.cursor, c.lifecycle); err != nil {
		c.markFault()
		return err
	}
	lineage := c.lifecycle.lineage
	if lineage == nil {
		return errGuardProtocol
	}
	node := lineage.nodes[task.Cookie]
	if node == nil || node.exited || node.startNS != task.StartBoottimeNS || node.tid != task.TID || node.tgid != task.TGID {
		return errGuardProtocol
	}
	seen := make(map[uint64]bool)
	for node.cookie != inv.taskCookie {
		if seen[node.cookie] || len(seen) >= 256 || node.parent == 0 {
			return errGuardProtocol
		}
		seen[node.cookie] = true
		node = lineage.nodes[node.parent]
		if node == nil {
			return errGuardProtocol
		}
	}
	if node.startNS != inv.startNS || node.tgid != inv.tgid {
		return errGuardProtocol
	}
	status, err := c.guard.status()
	if err != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != c.cursor || status.AckCursor != c.cursor {
		c.markFault()
		return errGuardProtocol
	}
	before := c.cursor
	body := map[string]any{"invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(task.Cookie), "notification_id": jsonU64(n.ID), "command": jsonU64(n.Args[0]), "result": jsonI64(-int64(unix.EPERM)), "guard_before": jsonU64(before), "guard_after": jsonU64(math.MaxUint64)}
	if err = preflightRuntimeRecord(b, "notification_denial", body); err != nil {
		c.markFault()
		return err
	}
	if err = notificationValid(inv.listener, n.ID); err != nil {
		return err
	}
	if err = replyNotification(inv.listener, n.ID, 0, unix.EPERM); err != nil {
		// Even an interrupted SEND may have reached the requester.
		c.markFault()
		return err
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
	if err = appendRuntimeRecord(b, "notification_denial", body); err != nil {
		c.markFault()
		return err
	}
	return nil
}
