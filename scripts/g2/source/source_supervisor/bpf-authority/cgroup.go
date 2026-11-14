//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type liveCgroup struct {
	GrantID                          uint64
	RootCookie, KernfsID, Enrollment uint64
	HeldFD                           int
	OfflineSeq, ReleaseSeq           uint64
	Retired                          bool
}

type queryInput struct {
	outer, normalized        []byte
	idsAddress, flagsAddress uint64
	capacity                 uint32
}

type queryOutput struct {
	AttachFlags                    uint32
	ProgramIDs, ProgramAttachFlags []uint32
	Executed                       bool
	Result                         int64
	Count                          uint32
	Errno                          unix.Errno
}

func parseQueryInput(n seccompNotification, raw []byte) (*queryInput, error) {
	if n.Args[0] != unix.BPF_PROG_QUERY || len(raw) < 28 || len(raw) > 4096 {
		return nil, errGuardProtocol
	}
	full := make([]byte, 4096)
	copy(full, raw)
	if guardU32(full, 4) != unix.BPF_CGROUP_DEVICE || guardU32(full, 8) != 0 || guardU32(full, 12) != 0 || !guardZero(full[28:32]) || !guardZero(full[40:]) {
		return nil, errGuardProtocol
	}
	capacity := guardU32(full, 24)
	idsAddress, flagsAddress := guardU64(full, 16), guardU64(full, 32)
	if capacity > 64 || (capacity != 0 && idsAddress == 0) || (capacity == 0 && (idsAddress != 0 || flagsAddress != 0)) {
		return nil, errGuardProtocol
	}
	norm := append([]byte(nil), raw...)
	for _, span := range [][2]int{{0, 4}, {16, 24}, {32, 40}} {
		for i := span[0]; i < span[1] && i < len(norm); i++ {
			norm[i] = 0
		}
	}
	return &queryInput{outer: append([]byte(nil), raw...), normalized: norm, idsAddress: idsAddress, flagsAddress: flagsAddress, capacity: capacity}, nil
}

// executeQuery uses only an already authenticated held target. Every returned
// ID must belong to this grant before any output is copied to the requester.
func executeQuery(h *heldNotification, in *queryInput, target *liveCgroup, owned map[uint32]uint64) (queryOutput, error) {
	var out queryOutput
	if h == nil || in == nil || target == nil || target.Retired || target.HeldFD < 0 || target.Enrollment == 0 {
		return out, errGuardProtocol
	}
	outer := append([]byte(nil), in.outer...)
	guardPut32(outer, 0, uint32(target.HeldFD))
	ids := make([]uint32, in.capacity)
	flags := make([]uint32, in.capacity)
	for i := range ids {
		ids[i], flags[i] = math.MaxUint32, math.MaxUint32
	}
	if len(ids) > 0 {
		guardPut64(outer, 16, uint64(uintptr(unsafe.Pointer(&ids[0]))))
	}
	if in.flagsAddress != 0 {
		if len(outer) < 40 || len(flags) == 0 {
			return out, errGuardProtocol
		}
		guardPut64(outer, 32, uint64(uintptr(unsafe.Pointer(&flags[0]))))
	}
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_QUERY, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	runtime.KeepAlive(ids)
	runtime.KeepAlive(flags)
	out.Errno = errno
	out.Executed = true
	out.Result = int64(result)
	if errno != 0 {
		out.Result = -int64(errno)
	}
	if errno != 0 && errno != unix.ENOSPC {
		return out, nil
	}
	out.AttachFlags = guardU32(outer, 12)
	count := guardU32(outer, 24)
	out.Count = count
	written := count
	if written > in.capacity {
		written = in.capacity
	}
	out.ProgramIDs = append([]uint32(nil), ids[:written]...)
	if in.flagsAddress != 0 {
		out.ProgramAttachFlags = append([]uint32(nil), flags[:written]...)
	}
	if count > 64 {
		return out, errGuardProtocol
	}
	for _, id := range out.ProgramIDs {
		if id == 0 || owned[id] == 0 {
			return out, errGuardProtocol
		}
	}
	if in.flagsAddress != 0 {
		for _, flag := range out.ProgramAttachFlags {
			if flag != out.AttachFlags {
				return out, errGuardProtocol
			}
		}
	}
	if errno == 0 && out.Result != 0 {
		return out, errGuardProtocol
	}
	return out, nil
}

// copyQueryResult copies the captured kernel output only after the caller has
// recorded the actual syscall result. A copyout error never permits a retry.
func copyQueryResult(h *heldNotification, in *queryInput, out queryOutput) error {
	if h == nil || h.memory == nil || in == nil || !out.Executed {
		return errGuardProtocol
	}
	if out.Errno != 0 && out.Errno != unix.ENOSPC {
		return nil
	}
	if out.Count > 64 || len(out.ProgramIDs) > int(in.capacity) || in.flagsAddress != 0 && len(out.ProgramAttachFlags) != len(out.ProgramIDs) {
		return errGuardProtocol
	}
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], out.AttachFlags)
	if err := h.memory.copyOut(h.notice.Args[1]+12, value[:]); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(value[:], out.Count)
	if err := h.memory.copyOut(h.notice.Args[1]+24, value[:]); err != nil {
		return err
	}
	if len(out.ProgramIDs) > 0 {
		encoded := make([]byte, len(out.ProgramIDs)*4)
		for i, id := range out.ProgramIDs {
			binary.LittleEndian.PutUint32(encoded[i*4:], id)
		}
		if err := h.memory.copyOut(in.idsAddress, encoded); err != nil {
			return err
		}
		if in.flagsAddress != 0 {
			for i, flag := range out.ProgramAttachFlags {
				binary.LittleEndian.PutUint32(encoded[i*4:], flag)
			}
			if err := h.memory.copyOut(in.flagsAddress, encoded); err != nil {
				return err
			}
		}
	}
	return nil
}
