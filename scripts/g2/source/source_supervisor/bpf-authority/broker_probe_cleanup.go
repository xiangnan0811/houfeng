//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"

	"golang.org/x/sys/unix"
)

func effectReferencesCookie(t *runtimeEffectTransaction, cookie uint64) bool {
	if t == nil {
		return false
	}
	for _, raw := range t.inputs {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		value, ok := exactNumber(item["cookie"])
		if ok && value == cookie {
			return true
		}
	}
	return false
}

// fixedProbeCleanupAllowed recognizes only the two already observed source
// feature probes, never a workload object or a caller-selected cleanup.
func (b *authorityBroker) fixedProbeCleanupAllowed(g *brokerGrant, cookie uint64) bool {
	if b == nil || b.effects == nil || b.effects.objects == nil || b.join == nil || !b.join.settled() || g == nil || b.grants[g.id] != g || g.phase != "admitted" || b.effects.objectGrant[cookie] != g.id {
		return false
	}
	object := b.effects.objects.byCookie[cookie]
	if object == nil || object.removalSeq != 0 || object.state != "live" {
		return false
	}
	creator := b.effects.transactions[object.ownerTransaction]
	if creator == nil || creator.grantID != g.id || creator.state != "retired" || !creator.effectExecuted || creator.effectErrno != 0 || creator.returnedFD == nil {
		return false
	}
	switch object.class {
	case "map":
		if creator.command != "MAP_CREATE" || !featureMapAttribute(creator.attributes) {
			return false
		}
		for _, t := range b.effects.transactions {
			if t.grantID == g.id && t.invocationID == creator.invocationID && t.command == "OBJ_GET_INFO_BY_FD" && t.state == "retired" && t.result == jsonI64(0) && effectReferencesCookie(t, cookie) {
				return true
			}
		}
	case "program":
		probe, err := probeProgram()
		if err != nil || creator.command != "PROG_LOAD" || object.programBytes != hex.EncodeToString(probe) {
			return false
		}
		for _, t := range b.effects.transactions {
			if t.grantID == g.id && t.invocationID == creator.invocationID && t.command == "PROG_ATTACH" && t.state == "retired" && t.result == jsonI64(-int64(unix.EBADF)) && effectReferencesCookie(t, cookie) {
				return true
			}
		}
	}
	return false
}

func (b *authorityBroker) serviceFixedProbeCleanup() error {
	if b == nil || b.effects == nil || b.checkpoint == nil {
		return errGuardProtocol
	}
	if b.checkpoint.invalid || b.effects.pending != nil || b.pendingCheckpoint != nil || b.checkpoint.active != nil {
		return nil
	}
	var selected uint64
	var grant *brokerGrant
	for cookie, grantID := range b.effects.objectGrant {
		if b.effects.grantCleanup[cookie] != nil {
			continue
		}
		candidate := b.grants[grantID]
		if b.fixedProbeCleanupAllowed(candidate, cookie) && (selected == 0 || cookie < selected) {
			selected, grant = cookie, candidate
		}
	}
	if selected == 0 {
		return nil
	}
	return b.beginGrantObjectCleanup(grant, selected)
}

// fixedProbeDrainPending permits prerequisite INFO/replace ATTACH and blocks
// the target once the fixed prerequisite response has settled.
func (b *authorityBroker) fixedProbeDrainPending(grantID uint64) bool {
	if b == nil || b.effects == nil || b.effects.objects == nil || b.join == nil || grantID == 0 {
		return true
	}
	for cookie, owner := range b.effects.objectGrant {
		if owner != grantID {
			continue
		}
		object := b.effects.objects.byCookie[cookie]
		if object == nil {
			return true
		}
		creator := b.effects.transactions[object.ownerTransaction]
		if creator == nil {
			return true
		}
		probe := object.class == "map" && creator.command == "MAP_CREATE" && featureMapAttribute(creator.attributes)
		if object.class == "program" && creator.command == "PROG_LOAD" {
			bytes, err := probeProgram()
			probe = err == nil && object.programBytes == hex.EncodeToString(bytes)
		}
		if !probe {
			continue
		}
		prerequisite := false
		for _, t := range b.effects.transactions {
			if t == nil || t.grantID != grantID || t.invocationID != creator.invocationID || t.state != "response_settled" && t.state != "retired" || !effectReferencesCookie(t, cookie) {
				continue
			}
			if object.class == "map" && t.command == "OBJ_GET_INFO_BY_FD" && t.result == jsonI64(0) || object.class == "program" && t.command == "PROG_ATTACH" && t.result == jsonI64(-int64(unix.EBADF)) {
				prerequisite = true
				break
			}
		}
		if !prerequisite {
			continue
		}
		cleanup := b.effects.grantCleanup[cookie]
		if cleanup == nil || cleanup.removalSequence == 0 || !b.join.settled() {
			return true
		}
	}
	return false
}
