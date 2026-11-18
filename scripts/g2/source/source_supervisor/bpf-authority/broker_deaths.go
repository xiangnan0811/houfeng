//go:build linux && amd64 && !cgo

package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

// pidfdGroupExited checks the original held pidfd. A task tracepoint alone is
// not evidence that the process and its other threads have exited.
func pidfdGroupExited(fd int) (bool, error) {
	if fd < 0 {
		return false, errGuardProtocol
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err := unix.Poll(fds, 0)
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	if err != nil || fds[0].Revents&unix.POLLNVAL != 0 {
		return false, errGuardProtocol
	}
	return fds[0].Revents&unix.POLLIN != 0, nil
}

func (b *authorityBroker) updateExistingIdentity(kind string, id uint64, field string, value any) error {
	if b == nil || b.transcript == nil || id == 0 {
		return errGuardProtocol
	}
	for i := len(b.transcript.entries) - 1; i >= 0; i-- {
		record := b.transcript.entries[i].record
		if record["kind"] != kind {
			continue
		}
		old, ok := record["body"].(map[string]any)
		if !ok || old["id"] != jsonU64(id) {
			continue
		}
		if _, ok = old[field]; !ok {
			return errGuardProtocol
		}
		body := make(map[string]any, len(old))
		for key, item := range old {
			body[key] = item
		}
		body[field] = value
		return appendRuntimeRecord(b, kind, body)
	}
	return errGuardProtocol
}

func (b *authorityBroker) invocationEffectsSettled(inv *runtimeInvocation) bool {
	if b == nil || b.effects == nil || inv == nil || b.join == nil || !b.join.settled() {
		return false
	}
	for _, tx := range b.effects.transactions {
		if tx.invocationID != inv.id {
			continue
		}
		if tx.state != "retired" || tx.cleanup["state"] == "pending" || tx.cleanup["state"] == "unknown" {
			return false
		}
	}
	return true
}

func (b *authorityBroker) invocationCreateProven(inv *runtimeInvocation) bool {
	if inv == nil || inv.grant == nil || !inv.grant.actualQueryConfirmed {
		return false
	}
	for _, tx := range b.effects.transactions {
		if tx.invocationID == inv.id && tx.state == "retired" && tx.result == jsonI64(0) && tx.command == "PROG_ATTACH" && tx.output != nil {
			return true
		}
	}
	return false
}

// serviceRuntimeDeaths consumes only guard-observed lineage and original
// pidfds. It never treats process disappearance as a successful operation.
func (b *authorityBroker) serviceRuntimeDeaths() error {
	if b == nil || b.effects == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.lineage == nil || b.transcript == nil {
		return errGuardProtocol
	}
	if err := b.serviceOwnerDeaths(); err != nil {
		return err
	}
	if err := b.serviceUndeliveredAttachmentCleanup(); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if err := b.serviceFixedProbeCleanup(); err != nil {
		b.checkpoint.markFault()
		return err
	}
	lineage := b.checkpoint.lifecycle.lineage
	for _, inv := range b.runtimeInvocations {
		if err := b.serviceRuntimeInvocation(inv, lineage); err != nil {
			return err
		}
	}
	for _, inv := range b.featureInvocations {
		if err := b.serviceRuntimeInvocation(inv, lineage); err != nil {
			return err
		}
	}
	if err := b.serviceActiveExecAdmissions(lineage); err != nil {
		return err
	}
	if err := b.retireRevokedBundleCustody(); err != nil {
		b.checkpoint.markFault()
		return err
	}
	return b.serviceRevokedCleanup()
}

func (b *authorityBroker) serviceRuntimeInvocation(inv *runtimeInvocation, lineage *daemonLineage) error {
	if inv == nil {
		return errGuardProtocol
	}
	if inv.state == "settled" || inv.state == "failed" {
		if inv.command == "delete" && b.runtimeBinds[inv.taskCookie] != nil {
			if b.effects.pending != nil || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.invalid && !b.canObserveOwnerDeath() {
				return nil
			}
			if err := b.retireBoundDeleteActor(inv); err != nil {
				return err
			}
		}
		if err := b.retireRuntimeBinding(inv); err != nil {
			return err
		}
		return b.retireRuntimeListener(inv, lineage)
	}
	node := lineage.nodes[inv.taskCookie]
	if node == nil {
		return nil
	}
	if node.cookie != inv.taskCookie || node.startNS != inv.startNS || node.tid != inv.pid || node.tgid != inv.tgid || node.tainted {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	runc := node.role == "runc" && node.digest == runtimeRuncSHA256 && node.execSeq > inv.wrapperExecSequence
	if runc && !inv.runtimeExecRecorded && node.exited {
		dead, err := pidfdGroupExited(inv.pidfd)
		if err != nil {
			b.checkpoint.markFault()
			return err
		}
		if !dead {
			return nil
		}
	}
	if runc && !inv.runtimeExecRecorded {
		if err := b.recordRuntimeExec(inv); err != nil {
			if !node.exited {
				dead, e := pidfdGroupExited(inv.pidfd)
				if e == nil && dead {
					// The native exit may be queued after this drain; attest on
					// its next observation using the retained package file.
					return nil
				}
			}
			b.checkpoint.markFault()
			return err
		}
	}
	if inv.runtimeExecRecorded && (!runc || node.execSeq != inv.runtimeExecSequence || node.generation != inv.runtimeExecGeneration) {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	if inv.state == "bound" && runc {
		if err := b.updateExistingIdentity("invocation", inv.id, "state", "running"); err != nil {
			b.checkpoint.markFault()
			return err
		}
		inv.state = "running"
	}
	if !node.exited {
		return nil
	}
	dead, err := pidfdGroupExited(inv.pidfd)
	if err != nil {
		b.checkpoint.markFault()
		return err
	}
	if !dead {
		return nil
	}
	if inv.grantID != 0 {
		for _, alias := range b.effects.aliases {
			if alias != nil && alias.invocationID == inv.id && !alias.closed && (alias.transaction == nil || !alias.transaction.executionReleased) {
				return nil
			}
		}
		if err = b.closeInvocationAliases(inv); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	if !b.invocationEffectsSettled(inv) {
		return nil
	}
	raw := uint32(node.exitCode)
	if raw > 0xffff || raw&0x7f == 0x7f || raw&0x7f == 0 && raw&0xff != 0 {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	success := raw == 0 && runc
	if success {
		switch inv.command {
		case "features":
			success = inv.grant == nil && inv.listener == -1
		case "create":
			success = b.invocationCreateProven(inv)
		case "start":
			success = inv.grant != nil && inv.grant.createSettled && inv.grant.actualQueryConfirmed
		case "exec":
			success = inv.grant != nil && inv.grant.exec != nil && inv.grant.exec.id == inv.execID && inv.grant.exec.consumed
		case "state", "kill", "delete":
			success = inv.grant != nil && inv.grant.createBound
		default:
			success = false
		}
	}
	state := "failed"
	if success {
		state = "settled"
	}
	if err := b.updateExistingIdentity("invocation", inv.id, "state", state); err != nil {
		b.checkpoint.markFault()
		return err
	}
	inv.state = state
	if success && inv.command == "create" {
		inv.grant.createSettled = true
		if inv.grant.phase != "revoked" {
			if inv.grant.phase != "admitted" {
				b.checkpoint.markFault()
				return errGuardProtocol
			}
			inv.grant.phase = "active"
			if err := b.emitGrantPhase(inv.grant); err != nil {
				b.checkpoint.markFault()
				return err
			}
		}
	}
	if success && inv.command == "start" {
		inv.grant.startObserved = true
	}
	principal := b.principals[inv.principalID]
	if principal == nil || principal.TaskCookie != inv.taskCookie || principal.StartNS != inv.startNS {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	if !principal.Terminal {
		if err := b.updateExistingIdentity("principal", principal.ID, "terminal", "exit"); err != nil {
			b.checkpoint.markFault()
			return err
		}
		if err := principal.retire(); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	return nil
}
