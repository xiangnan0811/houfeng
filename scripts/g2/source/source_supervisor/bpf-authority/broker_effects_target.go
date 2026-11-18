//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"

	"golang.org/x/sys/unix"
)

// importGrantCgroup records an actual first ENROLL under the existing grant.
// The caller must finish actor, copied policy, imported object and command
// validation before calling. The final ID_VALID check belongs to the prepared
// transaction path, after the enrollment record has been retained.
func (b *authorityBroker) importGrantCgroup(inv *runtimeInvocation, h *heldNotification, captured *capturedRuntimeEffect) (*liveCgroup, error) {
	if b == nil || b.effects == nil || b.effects.registry == nil || b.checkpoint == nil || b.checkpoint.invalid || inv == nil || inv.grant == nil || h == nil || h.memory == nil || captured == nil || b.runtimeInvocations[inv.listener] != inv || inv.listener != h.memory.listener || inv.grantID != inv.grant.id || inv.configSHA != inv.grant.configSHA || !runtimeContainerID(inv.grant.containerID) || captured.command != uint32(h.notice.Args[0]) {
		return nil, errGuardProtocol
	}
	if _, err := b.runtimeNotificationIdentity(inv); err != nil {
		return nil, err
	}
	var slot uint32
	status, err := h.guard.statusObserved(4)
	if err != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || hex.EncodeToString(status.Epoch[:]) != b.epoch || b.resolver == nil || status.BrokerCookie != b.resolver.brokerCookie {
		b.checkpoint.markFault()
		return nil, errGuardProtocol
	}
	switch captured.command {
	case unix.BPF_PROG_QUERY:
		if captured.query == nil || len(captured.attribute) < 4 {
			return nil, errGuardProtocol
		}
		slot = guardU32(captured.attribute, 0)
	case unix.BPF_PROG_ATTACH, unix.BPF_PROG_DETACH:
		if captured.attach == nil {
			return nil, errGuardProtocol
		}
		slot = captured.attach.targetSlot
	default:
		return nil, errGuardProtocol
	}
	grant := inv.grant
	current := b.effects.cgroups[grant.id]
	if current != nil && (current.Retired || current.GrantID != grant.id || current.HeldFD < 0) {
		return nil, errGuardProtocol
	}
	if _, exists := b.effects.enrollmentPending[grant.id]; exists {
		return nil, errGuardProtocol
	}
	root, err := unix.Open("/sys/fs/cgroup", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	var rootStat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(root, &rootStat) != nil || unix.Fstatfs(root, &fs) != nil || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Uid != 0 || uint64(fs.Type) != unix.CGROUP2_SUPER_MAGIC {
		return nil, errGuardProtocol
	}
	name := "docker/" + grant.containerID
	expected, err := unix.Openat2(root, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, err
	}
	defer unix.Close(expected)
	actual, err := h.importFD(slot)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = unix.Close(actual)
		}
	}()
	var want, got, rootAfter, named unix.Stat_t
	if unix.Fstat(expected, &want) != nil || unix.Fstat(actual, &got) != nil || want.Mode&unix.S_IFMT != unix.S_IFDIR || got.Mode&unix.S_IFMT != unix.S_IFDIR || want.Dev != rootStat.Dev || want.Dev != got.Dev || want.Ino != got.Ino {
		return nil, errGuardProtocol
	}
	if unix.Fstat(root, &rootAfter) != nil || rootAfter.Dev != rootStat.Dev || rootAfter.Ino != rootStat.Ino || unix.Fstatat(root, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != want.Dev || named.Ino != want.Ino || named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, errGuardProtocol
	}
	if current != nil {
		var retained unix.Stat_t
		if unix.Fstat(current.HeldFD, &retained) != nil || retained.Mode&unix.S_IFMT != unix.S_IFDIR || retained.Mode != got.Mode || retained.Nlink == 0 || retained.Dev != got.Dev || retained.Ino != got.Ino || b.effects.registry.root != 0 && current.RootCookie != b.effects.registry.root {
			return nil, errGuardProtocol
		}
		return current, nil
	}
	maximum := jsonU64(^uint64(0))
	preflight := map[string]any{"grant_id": maximum, "enrollment": maximum, "root_cookie": maximum, "cgroup_id": maximum, "invocation_id": maximum, "requester_cookie": maximum, "notification_id": maximum}
	if err = preflightRuntimeRecord(b, "cgroup_enrollment", preflight); err != nil {
		return nil, err
	}
	if b.effects.enrollmentPending == nil {
		b.effects.enrollmentPending = make(map[uint64]int)
	}
	b.effects.enrollmentPending[grant.id] = actual
	keep = true
	enrolled, err := h.guard.enrollCgroup(actual, grant.id)
	if err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	group := &liveCgroup{GrantID: grant.id, RootCookie: enrolled.RootCookie, KernfsID: enrolled.KernfsID, Enrollment: enrolled.Enrollment, HeldFD: actual}
	b.effects.cgroups[grant.id] = group
	delete(b.effects.enrollmentPending, grant.id)
	body := map[string]any{"grant_id": jsonU64(grant.id), "enrollment": jsonU64(group.Enrollment), "root_cookie": jsonU64(group.RootCookie), "cgroup_id": jsonU64(group.KernfsID), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(h.task.Cookie), "notification_id": jsonU64(h.notice.ID)}
	if err = appendRuntimeRecord(b, "cgroup_enrollment", body); err != nil {
		b.checkpoint.markFault()
		return group, err
	}
	if b.diagnostic != nil {
		b.diagnostic.enroll(grant, group)
	}
	if b.effects.registry.root != 0 && group.RootCookie != b.effects.registry.root {
		b.checkpoint.markFault()
		return group, errGuardProtocol
	}
	return group, nil
}

// importGrantDevNull admits only the fixed replace-probe target. The exact
// imported file remains held by the caller until the effect has settled.
func importGrantDevNull(h *heldNotification, slot uint32) (int, error) {
	if h == nil || h.memory == nil {
		return -1, errGuardProtocol
	}
	fd, err := h.importFD(slot)
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(st.Rdev)) != 1 || unix.Minor(uint64(st.Rdev)) != 3 {
		unix.Close(fd)
		return -1, errGuardProtocol
	}
	if err = notificationValid(h.memory.listener, h.notice.ID); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}
