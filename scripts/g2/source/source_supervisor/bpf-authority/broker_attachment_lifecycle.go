//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"math"

	"golang.org/x/sys/unix"
)

type runtimeAttachment struct {
	id, grantID, enrollment, rootCookie, cgroupID, programCookie, replaces, transactionID uint64
	flags                                                                                 uint32
	offlineSeq, releaseSeq                                                                uint64
	state                                                                                 string
}

func (b *authorityBroker) emitRuntimeAttachment(a *runtimeAttachment) error {
	if b == nil || a == nil {
		return errGuardProtocol
	}
	var replaces, offline, release any
	if a.replaces != 0 {
		replaces = jsonU64(a.replaces)
	}
	if a.offlineSeq != 0 {
		offline = jsonU64(a.offlineSeq)
	}
	if a.releaseSeq != 0 {
		release = jsonU64(a.releaseSeq)
	}
	return appendRuntimeRecord(b, "attachment", map[string]any{"id": jsonU64(a.id), "grant_id": jsonU64(a.grantID), "enrollment": jsonU64(a.enrollment), "root_cookie": jsonU64(a.rootCookie), "cgroup_id": jsonU64(a.cgroupID), "program_cookie": jsonU64(a.programCookie), "attach_type": jsonU64(unix.BPF_CGROUP_DEVICE), "flags": jsonU64(uint64(a.flags)), "replaces": replaces, "transaction_id": jsonU64(a.transactionID), "offline_seq": offline, "release_seq": release, "state": a.state})
}

// authorizeGrantAttachment validates the exact held program and enrolled
// cgroup before a PROG_ATTACH effect is committed.
func (b *authorityBroker) authorizeGrantAttachment(inv *runtimeInvocation, group *liveCgroup, programCookie, replacementCookie uint64, flags uint32) (uint64, error) {
	if b == nil || b.effects == nil || b.effects.registry == nil || inv == nil || inv.grant == nil || inv.command != "create" || group == nil || group.Retired || group.HeldFD < 0 || group.GrantID != inv.grantID || group.Enrollment == 0 || b.effects.objectGrant[programCookie] != inv.grantID {
		return 0, errGuardProtocol
	}
	target := b.effects.registry.targets[cgroupKey{group.RootCookie, group.KernfsID}]
	program := b.effects.objects.byCookie[programCookie]
	device, err := deviceProgram()
	if err != nil || target == nil || target.group != group || target.grant != inv.grant || target.phase != 2 || program == nil || program.class != "program" || program.state != "live" || program.programBytes != hex.EncodeToString(device) {
		return 0, errGuardProtocol
	}
	if b.effects.nextAttachment == 0 || b.effects.nextAttachment == math.MaxUint64 || len(b.effects.attachments) >= 4096 {
		return 0, errGuardProtocol
	}
	maximum := jsonU64(math.MaxUint64)
	if err := preflightRuntimeRecord(b, "attachment", map[string]any{"id": maximum, "grant_id": maximum, "enrollment": maximum, "root_cookie": maximum, "cgroup_id": maximum, "program_cookie": maximum, "attach_type": jsonU64(unix.BPF_CGROUP_DEVICE), "flags": jsonU64(math.MaxUint32), "replaces": maximum, "transaction_id": maximum, "offline_seq": maximum, "release_seq": maximum, "state": "offlining"}); err != nil {
		return 0, err
	}
	var current *runtimeAttachment
	for _, a := range b.effects.attachments {
		if a.grantID == inv.grantID && a.enrollment == group.Enrollment && a.state == "attached" {
			if current != nil {
				return 0, errGuardProtocol
			}
			current = a
		}
	}
	if flags == unix.BPF_F_ALLOW_MULTI {
		if replacementCookie != 0 || current != nil {
			return 0, errGuardProtocol
		}
		return 0, nil
	}
	if flags != unix.BPF_F_ALLOW_MULTI|unix.BPF_F_REPLACE || current == nil || current.programCookie != replacementCookie || current.programCookie == programCookie {
		return 0, errGuardProtocol
	}
	return current.id, nil
}

// recordGrantAttachment is called only for the actual successful attach
// syscall. Replacement preserves both incarnations and their real order.
func (b *authorityBroker) recordGrantAttachment(inv *runtimeInvocation, tx *runtimeEffectTransaction, group *liveCgroup, programCookie, replacementCookie uint64, flags uint32, result int64) (uint64, error) {
	if inv == nil || tx == nil || result != 0 || tx.command != "PROG_ATTACH" || tx.invocationID != inv.id || tx.grantID != inv.grantID || tx.state != "committed" || len(tx.attributes) < 16 || guardU32(tx.attributes, 12) != flags {
		return 0, errGuardProtocol
	}
	replaced, err := b.authorizeGrantAttachment(inv, group, programCookie, replacementCookie, flags)
	if err != nil {
		return 0, err
	}
	id := b.effects.nextAttachment
	if id == 0 || id == math.MaxUint64 || len(b.effects.attachments) >= 4096 {
		return 0, errGuardProtocol
	}
	a := &runtimeAttachment{id: id, grantID: inv.grantID, enrollment: group.Enrollment, rootCookie: group.RootCookie, cgroupID: group.KernfsID, programCookie: programCookie, replaces: replaced, transactionID: tx.id, flags: flags, state: "attached"}
	if err = b.emitRuntimeAttachment(a); err != nil {
		return 0, err
	}
	b.effects.attachments[id] = a
	b.effects.nextAttachment++
	if replaced != 0 {
		old := b.effects.attachments[replaced]
		old.state = "detached"
		if err = b.emitRuntimeAttachment(old); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (b *authorityBroker) authorizeGrantDetach(inv *runtimeInvocation, group *liveCgroup, programCookie uint64) (uint64, error) {
	if b == nil || b.effects == nil || inv == nil || inv.grant == nil || group == nil || group.Retired || group.GrantID != inv.grantID {
		return 0, errGuardProtocol
	}
	var found uint64
	for _, a := range b.effects.attachments {
		if a.grantID == inv.grantID && a.enrollment == group.Enrollment && a.programCookie == programCookie && a.state == "attached" {
			if found != 0 {
				return 0, errGuardProtocol
			}
			found = a.id
		}
	}
	if found == 0 {
		return 0, errGuardProtocol
	}
	return found, nil
}

func (b *authorityBroker) recordGrantDetach(inv *runtimeInvocation, tx *runtimeEffectTransaction, group *liveCgroup, programCookie uint64, result int64) (uint64, error) {
	if inv == nil || tx == nil || tx.command != "PROG_DETACH" || tx.invocationID != inv.id || tx.grantID != inv.grantID || tx.state != "committed" || result != 0 {
		return 0, errGuardProtocol
	}
	id, err := b.authorizeGrantDetach(inv, group, programCookie)
	if err != nil {
		return 0, err
	}
	a := b.effects.attachments[id]
	a.state = "detached"
	if err = b.emitRuntimeAttachment(a); err != nil {
		return 0, err
	}
	return id, nil
}

func (b *authorityBroker) observeAttachmentCgroup(r guardRecord) error {
	for _, a := range b.effects.attachments {
		if a.rootCookie != r.RootCookie || a.cgroupID != r.CgroupID || a.state == "released" {
			continue
		}
		if r.Kind == 5 {
			if a.offlineSeq != 0 || a.releaseSeq != 0 {
				return errGuardProtocol
			}
			a.offlineSeq, a.state = r.Sequence, "offlining"
		} else if r.Kind == 6 {
			if a.offlineSeq == 0 || a.releaseSeq != 0 || r.Sequence <= a.offlineSeq {
				return errGuardProtocol
			}
			a.releaseSeq, a.state = r.Sequence, "released"
		} else {
			return errGuardProtocol
		}
		if err := b.emitRuntimeAttachment(a); err != nil {
			return err
		}
	}
	return nil
}

func (b *authorityBroker) confirmGrantQuery(inv *runtimeInvocation, group *liveCgroup, out queryOutput) error {
	if b == nil || inv == nil || inv.grant == nil || group == nil || group.GrantID != inv.grantID || out.Errno != 0 || out.AttachFlags != unix.BPF_F_ALLOW_MULTI || len(out.ProgramIDs) != 1 {
		return errGuardProtocol
	}
	var match *runtimeAttachment
	for _, a := range b.effects.attachments {
		if a.grantID == inv.grantID && a.enrollment == group.Enrollment && a.state == "attached" {
			if match != nil {
				return errGuardProtocol
			}
			match = a
		}
	}
	if match == nil {
		return errGuardProtocol
	}
	object := b.effects.objects.byCookie[match.programCookie]
	if object == nil || object.id != uint64(out.ProgramIDs[0]) || len(out.ProgramAttachFlags) > 1 || len(out.ProgramAttachFlags) == 1 && out.ProgramAttachFlags[0] != out.AttachFlags {
		return errGuardProtocol
	}
	inv.grant.actualQueryConfirmed = true
	return nil
}
