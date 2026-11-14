//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"math"
	"reflect"

	"golang.org/x/sys/unix"
)

type grantObjectCleanup struct {
	grantID, creatorID, cookie uint64
	closeResult                int64
	removalSequence            uint64
}

func grantCleanupBody(g *grantObjectCleanup, step uint64, action string, result int64, sequence any) map[string]any {
	return map[string]any{"grant_id": jsonU64(g.grantID), "creator_transaction_id": jsonU64(g.creatorID), "object_cookie": jsonU64(g.cookie), "step": jsonU64(step), "action": action, "result": jsonI64(result), "guard_sequence": sequence}
}

// beginGrantObjectCleanup closes the original broker-held creator FD only
// after revalidating its real object information and the active grant cleanup.
// The requester or kernel may retain other descriptions; close is not removal.
func (b *authorityBroker) beginGrantObjectCleanup(grant *brokerGrant, cookie uint64) error {
	if b == nil || b.effects == nil || b.effects.objects == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.resolver == nil || b.join == nil || grant == nil || b.grants[grant.id] != grant || grant.phase != "settling" && !b.revokedCleanupAllowed(grant) && !b.fixedProbeCleanupAllowed(grant, cookie) || b.checkpoint.invalid && (!b.revokedCleanupAllowed(grant) || !b.canObserveOwnerDeath()) || cookie == 0 || b.effects.pending != nil || b.effects.objectGrant[cookie] != grant.id {
		return errGuardProtocol
	}
	if b.effects.grantCleanup[cookie] != nil {
		return errGuardProtocol
	}
	object := b.effects.objects.byCookie[cookie]
	item := b.checkpoint.lifecycle.objects[cookie]
	fd, ok := b.effects.objectFD[cookie]
	if !ok || fd < 0 || object == nil || item == nil || object.state != "live" || object.removalSeq != 0 || object.removalAuthorized || item.removalAllowed || item.removed {
		return errGuardProtocol
	}
	creator := b.effects.transactions[object.ownerTransaction]
	if creator == nil || creator.grantID != grant.id || creator.state != "retired" || !creator.effectExecuted || creator.effectErrno != 0 || creator.effectFD != fd || creator.returnedFD == nil || creator.policyErr != nil {
		return errGuardProtocol
	}
	// Drain actual guard facts before reusing the saved descriptor number. The
	// held-FD info syscall independently checks object ID, type and snapshot.
	if err := drainGuardEvents(b.checkpoint.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if object.removalSeq != 0 || object.state != "live" {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	id, attrs, err := b.observeRuntimeObjectInfo(fd, cookie, object.class, object.objectType)
	if err != nil || uint64(id) != object.id || !reflect.DeepEqual(attrs, object.attributes) {
		b.checkpoint.markFault()
		return errGuardProtocol
	}
	cleanup := &grantObjectCleanup{grantID: grant.id, creatorID: creator.id, cookie: cookie}
	max := jsonU64(math.MaxUint64)
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 2, "await_id_removed", math.MinInt32, max)); err != nil {
		return err
	}
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", math.MinInt32, nil)); err != nil {
		return err
	}
	if err = b.effects.objects.authorizeRemoval(cookie); err != nil {
		return err
	}
	if object.class == "program" {
		if err = b.resolver.authorizeUnload(cookie, creator.id); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	item.removalAllowed = true
	if b.effects.grantCleanup == nil {
		b.effects.grantCleanup = make(map[uint64]*grantObjectCleanup)
	}
	b.effects.grantCleanup[cookie] = cleanup
	closeErr := unix.Close(fd)
	if closeErr != nil {
		var errno unix.Errno
		if !errors.As(closeErr, &errno) || errno == 0 {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		cleanup.closeResult = -int64(errno)
	}
	if err = appendRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", cleanup.closeResult, nil)); err != nil {
		b.checkpoint.markFault()
		return err
	}
	if closeErr != nil {
		b.checkpoint.markFault()
		return closeErr
	}
	delete(b.effects.objectFD, cookie)
	return nil
}

func (b *authorityBroker) recordGrantObjectRemoval(r guardRecord) error {
	if b == nil || b.effects == nil {
		return errGuardProtocol
	}
	cleanup := b.effects.grantCleanup[r.ObjectCookie]
	if cleanup == nil {
		return nil
	}
	if cleanup.closeResult != 0 || cleanup.removalSequence != 0 || (r.Kind != 2 && r.Kind != 4) || r.Sequence == 0 {
		return errGuardProtocol
	}
	cleanup.removalSequence = r.Sequence
	return appendRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 2, "await_id_removed", int64(r.Result), jsonU64(r.Sequence)))
}
