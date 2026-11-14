//go:build linux && amd64 && !cgo

package main

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func (b *authorityBroker) beginNestedRegistry(parent *runtimeEffectTransaction, group *liveCgroup, inv *runtimeInvocation) error {
	if b == nil || b.effects == nil || b.effects.registry == nil || parent == nil || group == nil || inv == nil || inv.grant == nil || b.effects.pending != parent || parent.state != "committed" || parent.grantID != inv.grantID || parent.invocationID != inv.id || group.GrantID != inv.grantID || b.effects.cgroups[inv.grantID] != group || group.Retired || group.HeldFD < 0 || parent.command != "PROG_ATTACH" && parent.command != "PROG_DETACH" || b.effects.registry.nestedParent != nil {
		return errGuardProtocol
	}
	found := false
	for _, value := range parent.inputs {
		item, ok := value.(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if item["kind"] == "cgroup" {
			cookie, valid := exactNumber(item["cookie"])
			if found || !valid || cookie != group.Enrollment {
				return errGuardProtocol
			}
			found = true
		}
	}
	if !found {
		return errGuardProtocol
	}
	r := b.effects.registry
	r.nestedParent, r.nestedGroup, r.nestedInvocation = parent, group, inv
	return nil
}

func (b *authorityBroker) endNestedRegistry(parent *runtimeEffectTransaction) {
	r := b.effects.registry
	if r.nestedParent != parent {
		b.checkpoint.markFault()
		return
	}
	r.nestedParent, r.nestedGroup, r.nestedInvocation = nil, nil, nil
}

// validateNestedRegistry recognizes only the exact original target and the
// pinned PID1/current-runc retirement actors, within the committed parent.
func (b *authorityBroker) validateNestedRegistry(parent *runtimeEffectTransaction, command uint32, name string, key, value []byte, flags uint64, record map[string]any, previous uint64, cleanup bool) error {
	if b == nil || b.effects == nil || b.effects.registry == nil || b.transcript == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.lineage == nil || cleanup || b.effects.pending != parent || parent == nil || parent.state != "committed" {
		return errGuardProtocol
	}
	r := b.effects.registry
	g, inv := r.nestedGroup, r.nestedInvocation
	if r.nestedParent != parent || g == nil || inv == nil || inv.grant == nil || parent.grantID != inv.grantID || parent.invocationID != inv.id || g.GrantID != inv.grantID || b.effects.cgroups[inv.grantID] != g || g.Retired || g.HeldFD < 0 || parent.command != "PROG_ATTACH" && parent.command != "PROG_DETACH" {
		return errGuardProtocol
	}
	if command != unix.BPF_MAP_UPDATE_ELEM && command != unix.BPF_MAP_LOOKUP_ELEM {
		return errGuardProtocol
	}
	if len(key) < 16 || guardU64(key, 0) != g.RootCookie || guardU64(key, 8) != g.KernfsID {
		return errGuardProtocol
	}
	var expectedRecord map[string]any
	var expectedValue []byte
	switch name {
	case "realm_targets":
		if parent.command != "PROG_ATTACH" || len(key) != 16 {
			return errGuardProtocol
		}
		if command == unix.BPF_MAP_UPDATE_ELEM {
			if len(value) != 24 || guardU64(value, 0) != g.Enrollment || guardU64(value, 8) != inv.grantID {
				return errGuardProtocol
			}
			phase, retirement := guardU32(value, 16), guardU32(value, 20)
			if phase != 1 && phase != 2 || phase == 1 && flags != unix.BPF_NOEXIST || phase == 2 && flags != unix.BPF_EXIST {
				return errGuardProtocol
			}
			wantRetirement := uint32(0)
			if inv.grant.retirement == "auto_empty" {
				wantRetirement = 1
			} else if inv.grant.retirement == "explicit_cleanup" {
				wantRetirement = 2
			}
			if wantRetirement == 0 || retirement != wantRetirement {
				return errGuardProtocol
			}
			t := &effectRegistryTarget{group: g, grant: inv.grant, phase: phase, retirement: retirement}
			expectedValue = effectRegistryTargetValue(t)
			action := "insert"
			if phase == 2 {
				action = "update"
			}
			expectedRecord = effectRegistryTargetRecord(t, action)
		}
	case "retire_tasks":
		if len(key) != 32 || guardU32(key, 20) != 0 {
			return errGuardProtocol
		}
		tgid, start := guardU32(key, 16), guardU64(key, 24)
		actor := &effectRegistryTask{key: effectRegistryTaskKey{tgid: tgid, start: start}}
		if tgid == 1 && start == r.systemd.StartBoottimeNS {
			dev, err := kernelDevice(uint64(r.systemdStat.Dev))
			if err != nil {
				return err
			}
			actor.dev, actor.ino = dev, r.systemdStat.Ino
		} else if tgid == inv.tgid && start == inv.startNS {
			node := b.checkpoint.lifecycle.lineage.nodes[inv.taskCookie]
			if node == nil || node.startNS != inv.startNS || node.tgid != inv.tgid || node.role != "runc" || node.tainted {
				return errGuardProtocol
			}
			actor.invocation, actor.dev, actor.ino = inv.id, node.executable.dev, node.executable.ino
		} else {
			return errGuardProtocol
		}
		t := &effectRegistryTarget{group: g, grant: inv.grant}
		wantKey, wantValue := effectRegistryTaskBytes(t, actor)
		if !bytes.Equal(wantKey, key) {
			return errGuardProtocol
		}
		if command == unix.BPF_MAP_UPDATE_ELEM {
			if flags != unix.BPF_NOEXIST {
				return errGuardProtocol
			}
			expectedValue, expectedRecord = wantValue, effectRegistryTaskRecord(t, actor, "insert")
		}
	default:
		return errGuardProtocol
	}
	if command == unix.BPF_MAP_UPDATE_ELEM {
		if !bytes.Equal(value, expectedValue) || !sameJSON(record, expectedRecord) || previous != 0 {
			return errGuardProtocol
		}
		return nil
	}
	if flags != 0 || record != nil || len(value) != 0 || previous == 0 || previous > uint64(len(b.transcript.entries)) {
		return errGuardProtocol
	}
	entry := b.transcript.entries[previous-1].record
	body, ok := entry["body"].(map[string]any)
	if !ok || entry["kind"] != "registry" || body["map"] != name || body["grant_id"] != jsonU64(inv.grantID) {
		return errGuardProtocol
	}
	fields, ok := body["key"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	root, a := exactNumber(fields["root_kn_id"])
	node, c := exactNumber(fields["kn_id"])
	if !a || !c || root != g.RootCookie || node != g.KernfsID {
		return errGuardProtocol
	}
	if name == "realm_targets" {
		if !exactKeys(fields, "root_kn_id", "kn_id") {
			return errGuardProtocol
		}
	} else {
		tgid, x := exactNumber(fields["tgid"])
		start, y := exactNumber(fields["leader_start_ns"])
		if !exactKeys(fields, "root_kn_id", "kn_id", "tgid", "leader_start_ns") || !x || !y || tgid != uint64(guardU32(key, 16)) || start != guardU64(key, 24) {
			return errGuardProtocol
		}
	}
	return nil
}
