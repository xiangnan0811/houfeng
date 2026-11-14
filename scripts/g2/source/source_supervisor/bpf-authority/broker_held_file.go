//go:build linux && amd64 && !cgo

package main

import (
	"math"

	"golang.org/x/sys/unix"
)

func validDevNullStat(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFCHR && st.Mode&07777 == 0666 && st.Uid == 0 && st.Gid == 0 && st.Dev != 0 && st.Ino != 0 && uint64(st.Rdev) == unix.Mkdev(1, 3)
}

func sameDevNullNode(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Rdev == b.Rdev && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid
}

func heldDevNullBody(st unix.Stat_t, cookie uint64, source string, invocation, requester, notification, slot, baseline any) map[string]any {
	return map[string]any{"cookie": jsonU64(cookie), "kind": "dev_null", "source": source, "device": jsonU64(uint64(st.Dev)), "inode": jsonU64(st.Ino), "rdev": jsonU64(uint64(st.Rdev)), "mode": jsonU64(uint64(st.Mode)), "uid": jsonU64(uint64(st.Uid)), "gid": jsonU64(uint64(st.Gid)), "invocation_id": invocation, "requester_cookie": requester, "notification_id": notification, "requester_fd": slot, "baseline_cookie": baseline}
}

// recordBootstrapDevNull records the actual held device node. The descriptor
// remains owned by the original broker through the boot.
func (b *authorityBroker) recordBootstrapDevNull(fd int) (uint64, error) {
	if b == nil || b.transcript == nil || b.checkpoint == nil || b.checkpoint.invalid || fd < 0 || b.devNullCookie != 0 || uint64(len(b.transcript.entries)) == math.MaxUint64 {
		return 0, errGuardProtocol
	}
	var held, named unix.Stat_t
	if unix.Fstat(fd, &held) != nil || unix.Lstat("/dev/null", &named) != nil || !validDevNullStat(held) || !validDevNullStat(named) || !sameDevNullNode(held, named) {
		return 0, errGuardProtocol
	}
	cookie := uint64(len(b.transcript.entries)) + 1
	if err := appendRuntimeRecord(b, "held_file", heldDevNullBody(held, cookie, "bootstrap", nil, nil, nil, nil, nil)); err != nil {
		b.checkpoint.markFault()
		return 0, err
	}
	b.devNullFD, b.devNullCookie, b.devNullStat = fd, cookie, held
	return cookie, nil
}

// importRuntimeDevNull retains the requester's actual imported description;
// matching the baseline proves the device node, not description equality.
func (b *authorityBroker) importRuntimeDevNull(inv *runtimeInvocation, h *heldNotification, slot uint32) (int, uint64, error) {
	if b == nil || b.transcript == nil || b.checkpoint == nil || b.checkpoint.invalid || b.devNullCookie == 0 || b.devNullFD < 0 || inv == nil || h == nil || h.memory == nil || b.runtimeInvocations[inv.listener] != inv || inv.listener != h.memory.listener || h.task.Cookie == 0 || h.notice.ID == 0 || uint64(len(b.transcript.entries)) == math.MaxUint64 {
		return -1, 0, errGuardProtocol
	}
	var baseline unix.Stat_t
	if unix.Fstat(b.devNullFD, &baseline) != nil || !validDevNullStat(baseline) || !sameDevNullNode(baseline, b.devNullStat) {
		b.checkpoint.markFault()
		return -1, 0, errGuardProtocol
	}
	maximum := jsonU64(math.MaxUint64)
	if err := preflightRuntimeRecord(b, "held_file", heldDevNullBody(baseline, math.MaxUint64, "runtime_import", maximum, maximum, maximum, jsonU64(math.MaxUint32), maximum)); err != nil {
		return -1, 0, err
	}
	fd, err := h.importFD(slot)
	if err != nil {
		return -1, 0, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = unix.Close(fd)
		}
	}()
	var imported unix.Stat_t
	if unix.Fstat(fd, &imported) != nil || !validDevNullStat(imported) || !sameDevNullNode(imported, baseline) {
		return -1, 0, errGuardProtocol
	}
	cookie := uint64(len(b.transcript.entries)) + 1
	body := heldDevNullBody(imported, cookie, "runtime_import", jsonU64(inv.id), jsonU64(h.task.Cookie), jsonU64(h.notice.ID), jsonU64(uint64(slot)), jsonU64(b.devNullCookie))
	if err = appendRuntimeRecord(b, "held_file", body); err != nil {
		b.checkpoint.markFault()
		return -1, 0, err
	}
	keep = true
	return fd, cookie, nil
}
