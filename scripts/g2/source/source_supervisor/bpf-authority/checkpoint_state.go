//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
)

type deferredObject struct {
	kernelID                     uint64
	class                        string
	removalAllowed, removed      bool
	deferredRemovalOnNextRelease bool
	deferredRemovalOwner         uint64
}
type deferredCgroup struct {
	enrollment                           uint64
	retirementAllowed, offline, released bool
}
type cgroupKey struct{ root, id uint64 }

// checkpointLifecycle is populated from committed grant/cleanup facts before
// entering a protected observation. It accepts only their actual deferred
// kernel lifecycle events while new commits are paused.
type checkpointLifecycle struct {
	paused        bool
	objects       map[uint64]*deferredObject
	cgroups       map[cgroupKey]*deferredCgroup
	lineage       *daemonLineage
	retiringTasks map[uint64]bool
	runtimeGuard  func(guardRecord) error
	diagnostic    *ownerProbeEvidenceState
}

func (c *checkpointLifecycle) observe(r guardRecord) error {
	if c == nil {
		return errGuardProtocol
	}
	if r.Kind >= 9 && r.Kind <= 12 {
		if c.lineage == nil {
			return errGuardProtocol
		}
		if err := c.lineage.observe(r); err != nil {
			return err
		}
	}
	if !c.paused {
		return nil
	}
	switch r.Kind {
	case 2, 4:
		item := c.objects[r.ObjectCookie]
		if item == nil || !item.removalAllowed || item.removed || item.kernelID == 0 || item.kernelID != r.KernelID {
			if c.diagnostic != nil {
				reason := uint64(7)
				if item != nil && !item.removalAllowed {
					reason = 2
				} else if item != nil && item.removed {
					reason = 8
				} else if item != nil && item.kernelID != r.KernelID {
					reason = 4
				}
				c.diagnostic.lifecycleDecision(&r, 2, reason)
			}
			return errGuardProtocol
		}
		if r.Kind == 2 && item.class != "program" || r.Kind == 4 && item.class != "map" {
			if c.diagnostic != nil {
				c.diagnostic.lifecycleDecision(&r, 2, 5)
			}
			return errGuardProtocol
		}
		item.removed = true
		if c.diagnostic != nil {
			c.diagnostic.lifecycleDecision(&r, 1, 0)
		}
	case 5, 6:
		item := c.cgroups[cgroupKey{r.RootCookie, r.CgroupID}]
		if item == nil || item.enrollment == 0 || !item.retirementAllowed || item.released {
			return errGuardProtocol
		}
		if r.Kind == 5 {
			if item.offline {
				return errGuardProtocol
			}
			item.offline = true
		} else {
			if !item.offline {
				return errGuardProtocol
			}
			item.released = true
		}
	case 7, 8:
		// The audit join independently requires the same owned incarnation,
		// allowed operation and actual AUDIT_BPF record/context.
		if c.objects[r.ObjectCookie] == nil {
			return errGuardProtocol
		}
	case 11:
		if r.TaskCookie == 0 || !c.retiringTasks[r.TaskCookie] {
			return errGuardProtocol
		}
		delete(c.retiringTasks, r.TaskCookie)
	default:
		// No new object, attachment, fork, exec or watch is authorized while
		// the protected observation is in progress.
		return errGuardProtocol
	}
	return nil
}

type activeCheckpoint struct {
	principalID    uint64
	nonce          string
	watermark      uint64
	guardWatermark uint64
	deadlineNS     uint64
	tailBefore     string
	replyTail      string
	depth          uint32
}

type checkpointController struct {
	guard                 guardDevice
	audit                 *auditMulticast
	transcript            *authorityTranscript
	join                  *auditJoin
	lifecycle             *checkpointLifecycle
	cursor                uint64
	active                *activeCheckpoint
	activeTransactions    uint32
	auditBaselineSet      bool
	auditLost, auditOwner uint32
	invalid               bool
	faulted, ownerDead    bool
}

func (c *checkpointController) markFault() {
	if c != nil {
		c.invalid, c.faulted = true, true
	}
}

func (c *checkpointController) markOwnerDeath() {
	if c != nil {
		c.invalid, c.ownerDead = true, true
	}
}

func validCheckpointNonce(nonce string) bool {
	if len(nonce) != 32 {
		return false
	}
	for _, c := range nonce {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	b, err := hex.DecodeString(nonce)
	return err == nil && !guardZero(b)
}

func (c *checkpointController) begin(principalID uint64, nonce string, deadlineNS uint64, status guardStatus) (uint64, string, error) {
	if c == nil || c.invalid || principalID == 0 || !validCheckpointNonce(nonce) || c.lifecycle == nil || c.join == nil || c.transcript == nil || c.active != nil || c.activeTransactions != 0 || !c.lifecycle.paused || status.Sequence != c.cursor || status.AckCursor != c.cursor || !c.join.settled() {
		return 0, "", errGuardProtocol
	}
	before := c.transcript.tail
	sequence, tail, err := c.transcript.append("checkpoint", map[string]any{"nonce": nonce, "begin_watermark": jsonU64(status.Sequence), "end_watermark": jsonU64(status.Sequence), "tail_before": before, "principal_id": jsonU64(principalID), "state": "quiesced"})
	if err != nil {
		c.markFault()
		return 0, "", err
	}
	c.active = &activeCheckpoint{principalID: principalID, nonce: nonce, watermark: sequence, guardWatermark: status.Sequence, deadlineNS: deadlineNS, tailBefore: before, replyTail: tail, depth: 1}
	return sequence, tail, nil
}

func (c *checkpointController) release(principalID uint64, nonce string, watermark uint64, status guardStatus) (uint64, string, error) {
	if c == nil || c.invalid || c.active == nil || c.active.principalID != principalID || c.active.nonce != nonce || c.active.watermark != watermark || status.Sequence < c.active.guardWatermark || status.Sequence != c.cursor || status.AckCursor != c.cursor || !c.join.settled() {
		return 0, "", errGuardProtocol
	}
	if c.active.depth > 1 {
		c.active.depth--
		return c.active.watermark, c.active.replyTail, nil
	}
	for _, item := range c.lifecycle.objects {
		if item.deferredRemovalOnNextRelease && (item.removed || item.removalAllowed || item.class != "program" || item.kernelID == 0 || item.deferredRemovalOwner != principalID) {
			c.markFault()
			return 0, "", errGuardProtocol
		}
	}
	sequence, tail, err := c.transcript.append("checkpoint", map[string]any{"nonce": nonce, "begin_watermark": jsonU64(c.active.guardWatermark), "end_watermark": jsonU64(status.Sequence), "tail_before": c.active.tailBefore, "principal_id": jsonU64(principalID), "state": "released"})
	if err != nil {
		c.markFault()
		return 0, "", err
	}
	for _, item := range c.lifecycle.objects {
		if item.deferredRemovalOnNextRelease {
			item.removalAllowed = true
			item.deferredRemovalOnNextRelease = false
			item.deferredRemovalOwner = 0
		}
	}
	c.active = nil
	c.lifecycle.paused = false
	return sequence, tail, nil
}
