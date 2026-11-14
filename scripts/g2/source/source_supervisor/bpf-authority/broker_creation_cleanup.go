//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

// beginCreationCleanup releases only the broker's held description of this
// committed, published but undeliverable object. ID removal remains pending
// until the guard observes this exact incarnation; close is not removal.
func (b *authorityBroker) beginCreationCleanup(t *runtimeEffectTransaction) error {
	if b == nil || b.effects == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.resolver == nil || t == nil || !t.effectExecuted || t.effectErrno != 0 || t.effectFD < 0 || t.publication == nil || t.cleanupActive || t.state == "prepared" || t.state == "retired" {
		return errGuardProtocol
	}
	cookie := t.publication.ObjectCookie
	object := b.effects.objects.byCookie[cookie]
	item := b.checkpoint.lifecycle.objects[cookie]
	fd, ok := b.effects.objectFD[cookie]
	if !ok || fd != t.effectFD || object == nil || item == nil || object.ownerTransaction != t.id || object.removalSeq != 0 || item.removalAllowed || item.removed {
		return errGuardProtocol
	}
	if err := b.effects.objects.authorizeRemoval(cookie); err != nil {
		return err
	}
	if object.class == "program" {
		if err := b.resolver.authorizeUnload(cookie, t.id); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	item.removalAllowed = true
	t.cleanupActive = true
	if err := unix.Close(fd); err != nil {
		b.checkpoint.markFault()
		return err
	}
	delete(b.effects.objectFD, cookie)
	t.cleanup = creationCloseCleanup(cookie, 0)
	return nil
}

func creationCloseCleanup(cookie, removalSequence uint64) map[string]any {
	state := "pending"
	var guard any
	if removalSequence != 0 {
		state = "settled"
		guard = jsonU64(removalSequence)
	}
	return map[string]any{"policy": "close_unattached", "state": state, "steps": []any{
		map[string]any{"sequence": jsonU64(1), "action": "close_ref", "object_cookie": jsonU64(cookie), "attachment_id": nil, "guard_sequence": nil, "result": jsonI64(0)},
		map[string]any{"sequence": jsonU64(2), "action": "await_id_removed", "object_cookie": jsonU64(cookie), "attachment_id": nil, "guard_sequence": guard, "result": nil},
	}}
}
