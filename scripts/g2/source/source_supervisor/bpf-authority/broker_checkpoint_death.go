//go:build linux && amd64 && !cgo

package main

// failOwnerCheckpoint records only the accepted checkpoint's original nonce,
// actual beginning cursor/tail and last guard cursor after owner death. No ACK
// or quiesced/released claim follows this failure.
func (b *authorityBroker) failOwnerCheckpoint(p *controlPrincipal) error {
	if b == nil || b.checkpoint == nil || b.transcript == nil || p == nil || !p.Terminal {
		return errGuardProtocol
	}
	dead, err := pidfdGroupExited(p.PIDFD)
	if err != nil || !dead {
		return errGuardProtocol
	}
	c, q := b.checkpoint, b.pendingCheckpoint
	var nonce, tail string
	var begin uint64
	if c.active != nil && c.active.principalID == p.ID {
		nonce, tail, begin = c.active.nonce, c.active.tailBefore, c.active.guardWatermark
	} else if q != nil && q.principal == p && q.op == "QUIESCE" {
		nonce, tail, begin = q.nonce, q.beginTail, q.beginCursor
	} else {
		return nil
	}
	if !validCheckpointNonce(nonce) || tail == "" || c.cursor < begin {
		c.markFault()
		return errGuardProtocol
	}
	c.markOwnerDeath()
	body := map[string]any{"nonce": nonce, "begin_watermark": jsonU64(begin), "end_watermark": jsonU64(c.cursor), "tail_before": tail, "principal_id": jsonU64(p.ID), "state": "failed"}
	if err = appendRuntimeRecord(b, "checkpoint", body); err != nil {
		c.markFault()
		return err
	}
	c.active = nil
	if c.lifecycle != nil {
		c.lifecycle.paused = false
	}
	return nil
}
