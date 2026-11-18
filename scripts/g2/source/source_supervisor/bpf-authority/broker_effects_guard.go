//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"math"
	"reflect"
)

var errGuardSnapshotMismatch = fmt.Errorf("%w: publication snapshot differs from live object", errGuardProtocol)

// observeRuntimeGuard runs in guard sequence order before audit resolution.
// It binds each actual publication to the one currently committed syscall;
// an unowned or already-removed incarnation invalidates the observation.
func (b *authorityBroker) observeRuntimeGuard(r guardRecord) error {
	if b == nil || b.effects == nil || b.resolver == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || r.Sequence == 0 {
		return errGuardProtocol
	}
	switch r.Kind {
	case 1, 3:
		t := b.effects.pending
		if t == nil || t.state != "committed" || t.publication != nil || r.ObjectCookie == 0 || r.KernelID == 0 || r.KernelID > math.MaxUint32 || r.TaskCookie != b.resolver.brokerCookie || len(t.attributes) < 4 {
			return errGuardProtocol
		}
		if !t.effectExecuted || t.effectFD < 0 && t.effectErrno == 0 || t.effectFD >= 0 && t.effectErrno != 0 {
			return errGuardProtocol
		}
		class := "program"
		if r.Kind == 3 {
			class = "map"
			if t.command != "MAP_CREATE" {
				return errGuardProtocol
			}
		} else if t.command != "PROG_LOAD" {
			return errGuardProtocol
		}
		if len(b.effects.objects.byCookie) >= 384 {
			return errGuardProtocol
		}
		if class == "program" {
			if err := b.resolver.recordObject(auditedObject{Cookie: r.ObjectCookie, LoadTransactionID: t.id, ProgramID: r.KernelID, PublicationSequence: r.Sequence, LoadAuthorized: true}); err != nil {
				return err
			}
		}
		snapshot, err := r.objectSnapshot()
		if err != nil || snapshot == nil || snapshot["type"] != jsonU64(uint64(guardU32(t.attributes, 0))) {
			return errGuardProtocol
		}
		attrs, ok := snapshot["attributes"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		program := ""
		if class == "program" {
			var ok bool
			program, ok = t.program.(string)
			if !ok {
				return errGuardProtocol
			}
		}
		err = b.effects.objects.publish(r, t.id, guardU32(t.attributes, 0), attrs, program)
		if b.diagnostic != nil {
			b.diagnostic.trackerThrough = r.Sequence
		}
		if err != nil {
			return err
		}
		if t.effectFD >= 0 {
			id, observed, err := b.observeRuntimeObjectInfo(t.effectFD, r.ObjectCookie, class, guardU32(t.attributes, 0))
			if err != nil {
				return err
			}
			if uint64(id) != r.KernelID {
				return errGuardProtocol
			}
			if !reflect.DeepEqual(attrs, observed) {
				return errGuardSnapshotMismatch
			}
		}
		if b.checkpoint.lifecycle.objects == nil {
			b.checkpoint.lifecycle.objects = make(map[uint64]*deferredObject)
		}
		if b.checkpoint.lifecycle.objects[r.ObjectCookie] != nil {
			return errGuardProtocol
		}
		b.checkpoint.lifecycle.objects[r.ObjectCookie] = &deferredObject{kernelID: r.KernelID, class: class}
		if t.effectFD < 0 {
			if err := b.effects.objects.authorizeRemoval(r.ObjectCookie); err != nil {
				return err
			}
			if class == "program" {
				if err := b.resolver.authorizeUnload(r.ObjectCookie, t.id); err != nil {
					return err
				}
			}
			b.checkpoint.lifecycle.objects[r.ObjectCookie].removalAllowed = true
			t.cleanup = map[string]any{"policy": "close_unattached", "state": "pending", "steps": []any{map[string]any{"sequence": jsonU64(1), "action": "await_id_removed", "object_cookie": jsonU64(r.ObjectCookie), "attachment_id": nil, "guard_sequence": nil, "result": nil}}}
		}
		if t.effectFD >= 0 {
			b.effects.objectFD[r.ObjectCookie] = t.effectFD
		}
		b.effects.objectGrant[r.ObjectCookie] = t.grantID
		if b.diagnostic != nil {
			b.diagnostic.publish(b.grants[t.grantID], b.effects.objects.byCookie[r.ObjectCookie], r)
		}
		copyRecord := r
		t.publication = &copyRecord
		return nil
	case 2, 4:
		// Reject an early last-reference drop before recording either the object
		// transition or cleanup step 2, including outside a paused checkpoint.
		item := b.checkpoint.lifecycle.objects[r.ObjectCookie]
		if item == nil || !item.removalAllowed || item.removed || item.kernelID != r.KernelID {
			if b.diagnostic != nil {
				o := b.diagnostic.objectForRecord(&r)
				if o != nil && o.id == r.KernelID {
					reason := uint64(7)
					if item != nil && !item.removalAllowed {
						reason = 2
					} else if item != nil && item.removed {
						reason = 8
					}
					b.diagnostic.objectFact(o, 30, r.Sequence, 0, reason, r.Sequence)
				}
			}
			return errGuardProtocol
		}
		objectBefore := b.effects.objects.byCookie[r.ObjectCookie]
		rejection := uint64(9)
		if b.diagnostic != nil && objectBefore != nil {
			switch {
			case !objectBefore.removalAuthorized:
				rejection = 2
			case objectBefore.removalSeq != 0:
				rejection = 8
			case objectBefore.id != r.KernelID:
				rejection = 4
			case r.Sequence <= objectBefore.publicationSeq:
				rejection = 6
			case objectBefore.class == "program" && r.Kind != 2 || objectBefore.class == "map" && r.Kind != 4:
				rejection = 5
			}
		}
		trackerErr := b.effects.objects.removed(r)
		if b.diagnostic != nil {
			b.diagnostic.trackerThrough = r.Sequence
		}
		if b.diagnostic != nil && objectBefore != nil && b.diagnostic.objectForRecord(&r) == objectBefore {
			decision, reason := uint64(1), uint64(0)
			if trackerErr != nil {
				decision, reason = 2, rejection
			}
			b.diagnostic.objectFact(objectBefore, 26, r.Sequence, 0, decision, reason)
			if trackerErr != nil {
				b.diagnostic.objectFact(objectBefore, 30, r.Sequence, 0, rejection, r.Sequence)
			}
			if trackerErr == nil {
				b.diagnostic.objectFact(objectBefore, 29, r.Sequence, 0, objectBefore.removalSeq, 0)
			}
		}
		if trackerErr != nil {
			return trackerErr
		}
		if err := b.recordGrantObjectRemoval(r); err != nil {
			return err
		}
		object := b.effects.objects.byCookie[r.ObjectCookie]
		if object == nil {
			return errGuardProtocol
		}
		owner := b.effects.transactions[object.ownerTransaction]
		if owner != nil && owner.effectExecuted && owner.effectFD < 0 && owner.effectErrno != 0 {
			owner.cleanup = map[string]any{"policy": "close_unattached", "state": "settled", "steps": []any{map[string]any{"sequence": jsonU64(1), "action": "await_id_removed", "object_cookie": jsonU64(r.ObjectCookie), "attachment_id": nil, "guard_sequence": jsonU64(r.Sequence), "result": nil}}}
		} else if owner != nil && owner.cleanupActive {
			owner.cleanup = creationCloseCleanup(r.ObjectCookie, r.Sequence)
		}
		if !b.checkpoint.lifecycle.paused {
			item := b.checkpoint.lifecycle.objects[r.ObjectCookie]
			if item == nil || !item.removalAllowed || item.removed || item.kernelID != r.KernelID {
				if b.diagnostic != nil {
					b.diagnostic.lifecycleDecision(&r, 2, 7)
				}
				return errGuardProtocol
			}
			item.removed = true
			if b.diagnostic != nil {
				b.diagnostic.lifecycleDecision(&r, 1, 0)
			}
		}
		return nil
	case 5, 6:
		return b.observeEffectCgroup(r)
	default:
		return errGuardProtocol
	}
}
