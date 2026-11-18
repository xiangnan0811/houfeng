//go:build linux && amd64 && !cgo

package main

import (
	"math"

	"golang.org/x/sys/unix"
)

// bootstrapPublication records the native snapshot as soon as the publication
// is drained, including creations for which new_fd subsequently failed.
type bootstrapPublication struct {
	rec         *bootstrapRecorder
	tx          *bootstrapTransaction
	class       string
	typ         uint32
	program     any
	failed      bool
	publication guardRecord
	removal     guardRecord
	snapshot    map[string]any
}

func (p *bootstrapPublication) object(state string, removal any) error {
	return p.rec.add("object", map[string]any{
		"cookie": jsonU64(p.publication.ObjectCookie), "class": p.class,
		"kernel_id": jsonU64(p.publication.KernelID), "owner_transaction": jsonU64(p.tx.id),
		"type": p.snapshot["type"], "attributes": p.snapshot["attributes"],
		"attribute_source": map[string]any{"kind": "native_publication", "guard_sequence": jsonU64(p.publication.Sequence)},
		"bytes":            p.program, "publication_seq": jsonU64(p.publication.Sequence), "removal_seq": removal, "state": state,
	})
}

func (p *bootstrapPublication) observe(r guardRecord) error {
	if r.Kind == 1 || r.Kind == 3 {
		if p.publication.Sequence != 0 || (p.class == "program") != (r.Kind == 1) || r.ObjectCookie == 0 || r.KernelID == 0 || r.KernelID > math.MaxUint32 || r.TaskCookie != p.rec.executor {
			return errGuardProtocol
		}
		snapshot, err := r.objectSnapshot()
		if err != nil || snapshot == nil || snapshot["type"] != jsonU64(uint64(p.typ)) {
			return errGuardProtocol
		}
		p.publication, p.snapshot = r, snapshot
		if p.failed {
			p.tx.cleanup = map[string]any{"policy": "close_unattached", "state": "pending", "steps": []any{map[string]any{"sequence": jsonU64(1), "action": "await_id_removed", "object_cookie": jsonU64(r.ObjectCookie), "attachment_id": nil, "guard_sequence": nil, "result": nil}}}
		}
		return p.object("live", nil)
	}
	if r.Kind == 2 || r.Kind == 4 {
		if !p.failed || p.publication.Sequence == 0 || p.removal.Sequence != 0 || r.ObjectCookie != p.publication.ObjectCookie || r.KernelID != p.publication.KernelID || (p.class == "program") != (r.Kind == 2) || r.Sequence <= p.publication.Sequence {
			return errGuardProtocol
		}
		p.removal = r
		p.tx.cleanup = map[string]any{"policy": "close_unattached", "state": "settled", "steps": []any{map[string]any{"sequence": jsonU64(1), "action": "await_id_removed", "object_cookie": jsonU64(r.ObjectCookie), "attachment_id": nil, "guard_sequence": jsonU64(r.Sequence), "result": nil}}}
		return p.object("id_removed", jsonU64(r.Sequence))
	}
	return nil
}

// waitRemoval waits only for the exact observed failed bootstrap incarnation.
// No ordinary service or independent BPF producer exists in this boot phase.
func (p *bootstrapPublication) waitRemoval(g guardDevice, cursor *uint64) error {
	if p == nil || !p.failed || p.publication.Sequence == 0 {
		return errGuardProtocol
	}
	start, err := monoNowNS()
	if err != nil || start >= authorityGuestDeadlineNS {
		return errGuardProtocol
	}
	deadline := start + 5_000_000_000
	if deadline < start || deadline > authorityGuestDeadlineNS {
		deadline = authorityGuestDeadlineNS
	}
	for p.removal.Sequence == 0 {
		if _, err = bootstrapDrainObserved(g, p.rec, cursor, p.observe); err != nil {
			return err
		}
		if p.removal.Sequence != 0 {
			break
		}
		now, e := monoNowNS()
		if e != nil || now >= deadline {
			return unix.ETIMEDOUT
		}
		if _, e = unix.Poll(nil, 1); e != nil && e != unix.EINTR {
			return e
		}
	}
	return nil
}
