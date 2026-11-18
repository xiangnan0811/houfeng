//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"errors"
	"os"
	"sort"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func installNoSetrlimitFilter() error {
	const (
		load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		ret  = unix.BPF_RET | unix.BPF_K
		deny = unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
	)
	ins := []unix.SockFilter{
		{Code: load, K: 4},
		{Code: jeq, Jt: 0, Jf: 8, K: auditArchX8664},
		{Code: load, K: 0},
		{Code: jeq, Jt: 6, Jf: 0, K: unix.SYS_SETRLIMIT},
		{Code: jeq, Jt: 0, Jf: 5, K: unix.SYS_PRLIMIT64},
		{Code: load, K: 32},
		{Code: jeq, Jt: 0, Jf: 3, K: 0},
		{Code: load, K: 36},
		{Code: jeq, Jt: 1, Jf: 1, K: 0},
		{Code: ret, K: unix.SECCOMP_RET_ALLOW},
		{Code: ret, K: deny},
	}
	prog := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return errno
	}
	return nil
}

// brokerFDSet reads the actual shared files table. The directory descriptor
// used for enumeration has already been closed when its entries are checked.
func brokerFDSet() ([]int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return nil, err
	}
	fds := make([]int, 0, len(entries))
	for _, entry := range entries {
		fd, e := strconv.Atoi(entry.Name())
		if e != nil || fd < 0 {
			return nil, errGuardProtocol
		}
		_, e = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if errors.Is(e, unix.EBADF) {
			continue
		}
		if e != nil {
			return nil, e
		}
		fds = append(fds, fd)
	}
	sort.Ints(fds)
	return fds, nil
}

func (b *authorityBroker) executeSelftestMap(t *runtimeEffectTransaction, h *heldNotification, raw []byte, nullFD int) (bpfEffect, error) {
	if h == nil || h.memory == nil || t == nil || t.command != "MAP_CREATE" || !featureMapAttribute(raw) {
		return bpfEffect{FD: -1}, errGuardProtocol
	}
	return b.executeSelftestFD(t, nullFD, func() (bpfEffect, error) {
		return executeFeatureMap(h.memory, h.notice, raw)
	})
}

func (b *authorityBroker) executeSelftestProgram(t *runtimeEffectTransaction, h *heldNotification, in *programInput, nullFD int) (bpfEffect, error) {
	if h == nil || h.memory == nil || t == nil || t.command != "PROG_LOAD" || in == nil {
		return bpfEffect{FD: -1}, errGuardProtocol
	}
	device, err := deviceProgram()
	if err != nil || !bytes.Equal(device, in.instructions) || guardU32(in.outer, 0) != unix.BPF_PROG_TYPE_CGROUP_DEVICE {
		return bpfEffect{FD: -1}, errGuardProtocol
	}
	return b.executeSelftestFD(t, nullFD, func() (bpfEffect, error) {
		return executeProgramLoad(h.memory, h.notice, in)
	})
}

// executeSelftestFD is called on the original locked broker thread only, after
// the selected transaction has committed. All identity copies and the trusted
// null descriptor exist before exhaustion. Restoration precedes all response,
// audit, guard and program-log copyout processing.
func (b *authorityBroker) executeSelftestFD(t *runtimeEffectTransaction, nullFD int, execute func() (bpfEffect, error)) (effect bpfEffect, err error) {
	effect.FD = -1
	if b == nil || b.selftestFD == nil || b.effects == nil || b.checkpoint == nil || b.checkpoint.invalid || t == nil || b.effects.pending != t || t.state != "committed" || t.grantID != b.selftestFD.grantID || b.selftestFD.phase != "armed" || execute == nil || nullFD < 0 || unix.Gettid() != os.Getpid() {
		return effect, errGuardProtocol
	}
	stage := b.selftestFD
	if stage.class == "map" && t.command != "MAP_CREATE" || stage.class == "program" && t.command != "PROG_LOAD" {
		return effect, errGuardProtocol
	}
	owner := b.principals[stage.owner]
	if owner == nil || owner.Role != "REALM_ROOT" || owner.Terminal || owner.Transferred || !livePIDFD(owner.PIDFD) || b.reader == nil || b.reader.healthy() != nil || b.join == nil || !b.join.settled() || b.checkpoint.activeTransactions != 1 || b.checkpoint.lifecycle == nil {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	for _, item := range b.checkpoint.lifecycle.objects {
		if item.removalAllowed && !item.removed {
			b.checkpoint.markFault()
			return effect, errGuardProtocol
		}
	}
	for _, item := range b.checkpoint.lifecycle.cgroups {
		if item.retirementAllowed && !item.released {
			b.checkpoint.markFault()
			return effect, errGuardProtocol
		}
	}
	status, e := b.checkpoint.guard.status()
	if e != nil || status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence != b.checkpoint.cursor || status.AckCursor != b.checkpoint.cursor {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	now, e := monoNowNS()
	if e != nil || now >= stage.deadline {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	var nullStat unix.Stat_t
	if unix.Fstat(nullFD, &nullStat) != nil || nullStat.Mode&unix.S_IFMT != unix.S_IFCHR || uint64(nullStat.Rdev) != unix.Mkdev(1, 3) {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	var saved unix.Rlimit
	if e = unix.Getrlimit(unix.RLIMIT_NOFILE, &saved); e != nil || saved.Cur < 256 || saved.Max < saved.Cur {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	original, e := brokerFDSet()
	if e != nil || len(original) == 0 || len(original) > 128 {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	foundNull := false
	for _, fd := range original {
		if fd == nullFD {
			foundNull = true
		}
	}
	if !foundNull {
		b.checkpoint.markFault()
		return effect, errGuardProtocol
	}
	for _, phase := range []string{"entered", "restored"} {
		var errno any
		if phase == "restored" {
			errno = jsonU64(uint64(unix.EMFILE))
		}
		body := map[string]any{"nonce": stage.nonce, "grant_id": jsonU64(stage.grantID), "class": stage.class, "transaction_id": jsonU64(t.id), "phase": phase, "saved_soft": jsonU64(saved.Cur), "hard_limit": jsonU64(saved.Max), "test_soft": jsonU64(256), "original_fd_count": jsonU64(128), "identity_fd_count": jsonU64(128), "owned_fd_count": jsonU64(256), "errno": errno}
		if e = preflightRuntimeRecord(b, "selftest_fd_stage", body); e != nil {
			b.checkpoint.markFault()
			return effect, e
		}
	}
	var copies [128]int
	for i := range copies {
		copies[i] = -1
	}
	copyCount := 0
	var owned [256]int
	ownedCount := 0
	limitChanged := false
	variant, _ := b.caseFacts["bpf_variant"].(string)
	entered := false
	// Every return restores the limit and closes only descriptors created here.
	defer func() {
		valid := true
		for i := ownedCount - 1; i >= 0; i-- {
			if unix.Close(owned[i]) != nil {
				valid = false
			}
		}
		if limitChanged && unix.Setrlimit(unix.RLIMIT_NOFILE, &saved) != nil {
			valid = false
		}
		var restored unix.Rlimit
		if unix.Getrlimit(unix.RLIMIT_NOFILE, &restored) != nil || restored != saved {
			valid = false
		}
		for i := range copyCount {
			if !sameHeldDescription(original[i], copies[i]) {
				valid = false
			}
		}
		for i := copyCount - 1; i >= 0; i-- {
			if unix.Close(copies[i]) != nil {
				valid = false
			}
		}
		after, scanErr := brokerFDSet()
		if scanErr != nil || len(after) != len(original) {
			valid = false
		} else {
			for i := range original {
				if original[i] != after[i] {
					valid = false
				}
			}
		}
		if !valid || !entered || err != nil || effect.Errno != unix.EMFILE || effect.FD >= 0 {
			count := uint64(len(original) + copyCount + ownedCount)
			detector := ""
			if entered && variant == "fd-fill-failure" && err != nil && count < 256 {
				detector = "fd_fill_failure"
			} else if entered && variant == "fd-restore-failure" && !valid && count == 256 && effect.Errno == unix.EMFILE && effect.FD < 0 {
				detector = "fd_restore_failure"
			}
			if detector != "" {
				now, timeErr := monoNowNS()
				deadline, deadlineErr := guestDeadline(5 * time.Second)
				if timeErr == nil && deadlineErr == nil {
					frame := map[string]any{"version": jsonU64(1), "boot_id": b.bootID, "kind": "broker_receive_observation", "variant": variant, "source": "broker_fd_stage", "detector": detector, "request_id": nil, "nonce": nil, "input_sha256": nil, "input_bytes": jsonU64(saved.Cur), "original_sha256": nil, "expected_actor": nil, "observed_actor": nil, "deadline_ns": nil, "observed_ns": jsonU64(now), "capacity": jsonU64(256), "count_before": jsonU64(count), "rejected_wire": nil, "guard_sequence": nil, "receive_observation": nil}
					sendErr := earlyProbeSend(b.observerSocketFD, frame, nil, deadline)
					if sendErr == nil {
						var ack map[string]any
						var cred *unix.Ucred
						ack, cred, _, sendErr = earlyProbeReceive(b.observerSocketFD, 0, deadline)
						if sendErr == nil && (cred == nil || cred.Pid <= 0 || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(b.observerPIDFD, uint32(cred.Pid)) || !exactKeys(ack, "version", "boot_id", "kind", "variant", "detector", "count_before") || ack["version"] != jsonU64(1) || ack["boot_id"] != b.bootID || ack["kind"] != "broker_fd_witness_ack" || ack["variant"] != variant || ack["detector"] != detector || ack["count_before"] != jsonU64(count)) {
							sendErr = errGuardProtocol
						}
					}
					if sendErr != nil && err == nil {
						err = sendErr
					}
				} else if err == nil {
					err = errGuardProtocol
				}
			}
			_ = b.authority.guard.revoke()
			b.checkpoint.markFault()
			if err == nil {
				err = errGuardProtocol
			}
			return
		}
		t.effectExecuted, t.effectFD, t.effectErrno = true, effect.FD, effect.Errno
		body := map[string]any{"nonce": stage.nonce, "grant_id": jsonU64(stage.grantID), "class": stage.class, "transaction_id": jsonU64(t.id), "phase": "restored", "saved_soft": jsonU64(saved.Cur), "hard_limit": jsonU64(saved.Max), "test_soft": jsonU64(256), "original_fd_count": jsonU64(uint64(len(original))), "identity_fd_count": jsonU64(uint64(copyCount)), "owned_fd_count": jsonU64(uint64(ownedCount)), "errno": jsonU64(uint64(unix.EMFILE))}
		if e := appendRuntimeRecord(b, "selftest_fd_stage", body); e != nil {
			b.checkpoint.markFault()
			err = e
			return
		}
		stage.phase = "restored"
		if e := b.emitFDCase(stage, t, "settled"); e != nil {
			b.checkpoint.markFault()
			err = e
		}
	}()
	for _, fd := range original {
		if fd >= 256 {
			return effect, errGuardProtocol
		}
		copyFD, e := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if e != nil || copyFD >= 256 {
			if e == nil {
				_ = unix.Close(copyFD)
			}
			return effect, errGuardProtocol
		}
		copies[copyCount] = copyFD
		copyCount++
	}
	fillSourceFD, e := unix.FcntlInt(uintptr(nullFD), unix.F_DUPFD_CLOEXEC, 0)
	if e != nil || fillSourceFD < 0 || fillSourceFD >= 256 {
		if e == nil && fillSourceFD >= 0 {
			_ = unix.Close(fillSourceFD)
		}
		return effect, errGuardProtocol
	}
	owned[ownedCount] = fillSourceFD
	ownedCount++
	testLimit := saved
	testLimit.Cur = 256
	if e = unix.Setrlimit(unix.RLIMIT_NOFILE, &testLimit); e != nil {
		return effect, e
	}
	limitChanged = true
	if variant == "fd-restore-failure" {
		if e = installNoSetrlimitFilter(); e != nil {
			return effect, e
		}
	}
	if variant == "fd-fill-failure" {
		body := map[string]any{"nonce": stage.nonce, "grant_id": jsonU64(stage.grantID), "class": stage.class, "transaction_id": jsonU64(t.id), "phase": "entered", "saved_soft": jsonU64(saved.Cur), "hard_limit": jsonU64(saved.Max), "test_soft": jsonU64(256), "original_fd_count": jsonU64(uint64(len(original))), "identity_fd_count": jsonU64(uint64(copyCount)), "owned_fd_count": jsonU64(uint64(ownedCount)), "errno": nil}
		if e = appendRuntimeRecord(b, "selftest_fd_stage", body); e != nil {
			return effect, e
		}
		stage.transactionID, stage.phase = t.id, "entered"
		entered = true
		_ = unix.Close(fillSourceFD)
		ownedCount--
	}
	for {
		fd, e := unix.FcntlInt(uintptr(fillSourceFD), unix.F_DUPFD_CLOEXEC, 0)
		if errors.Is(e, unix.EMFILE) {
			break
		}
		if e != nil || fd < 0 || fd >= 256 || ownedCount == len(owned) {
			if e == nil && fd >= 0 {
				_ = unix.Close(fd)
			}
			if e != nil {
				return effect, e
			}
			return effect, errGuardProtocol
		}
		owned[ownedCount] = fd
		ownedCount++
	}
	if copyCount != len(original) || len(original)+copyCount+ownedCount != 256 {
		return effect, errGuardProtocol
	}
	if !entered {
		body := map[string]any{"nonce": stage.nonce, "grant_id": jsonU64(stage.grantID), "class": stage.class, "transaction_id": jsonU64(t.id), "phase": "entered", "saved_soft": jsonU64(saved.Cur), "hard_limit": jsonU64(saved.Max), "test_soft": jsonU64(256), "original_fd_count": jsonU64(uint64(len(original))), "identity_fd_count": jsonU64(uint64(copyCount)), "owned_fd_count": jsonU64(uint64(ownedCount)), "errno": nil}
		if e = appendRuntimeRecord(b, "selftest_fd_stage", body); e != nil {
			return effect, e
		}
		if e = b.emitFDCase(stage, t, "observed"); e != nil {
			return effect, e
		}
	}
	stage.transactionID, stage.phase = t.id, "entered"
	entered = true
	return execute()
}
