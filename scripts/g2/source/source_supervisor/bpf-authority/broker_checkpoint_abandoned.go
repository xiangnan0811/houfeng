//go:build linux && amd64 && !cgo

package main

// drainAbandonedCheckpointReader consumes the original already-issued reader
// request after owner death. The genuine response is recorded but never becomes
// a successful checkpoint, audit cut, or ACK.
func (b *authorityBroker) drainAbandonedCheckpointReader(q *pendingCheckpointRequest) (bool, error) {
	if b == nil || b.checkpoint == nil || b.reader == nil || q == nil || q.principal == nil || !q.principal.Terminal || !b.checkpoint.ownerDead {
		return false, errGuardProtocol
	}
	if !q.readbackStarted || q.readbackDone {
		return true, nil
	}
	ready, err := b.reader.service()
	if err != nil {
		b.checkpoint.markFault()
		return false, err
	}
	if !ready {
		return false, nil
	}
	status, evidence, err := b.reader.result()
	if err != nil {
		b.checkpoint.markFault()
		return false, err
	}
	c := b.checkpoint
	if q.op == "AUDIT_CUT" {
		if _, err = validateAuditConfiguration(evidence, true, q.principal.PID); err != nil {
			c.markFault()
			return false, err
		}
	}
	if !c.auditBaselineSet || status.enabled != 2 || status.failure != 1 || status.rate != 0 || status.backlogLimit != 16384 || status.lost != c.auditLost || c.auditOwner != 0 && status.pid != c.auditOwner || c.auditOwner == 0 && status.pid != 0 && (q.principal.Role != "REALM_ROOT" && q.principal.Role != "RECOVERY_OBSERVER" || status.pid != q.principal.PID) {
		c.markFault()
		return false, errGuardProtocol
	}
	q.readbackSequence, _, err = b.transcript.append("audit_readback", evidence)
	if err != nil {
		c.markFault()
		return false, err
	}
	q.readbackDone = true
	return true, nil
}
