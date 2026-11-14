//go:build linux && amd64 && !cgo

package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type attachInput struct {
	outer, normalized                        []byte
	targetSlot, programSlot, replacementSlot uint32
	flags                                    uint32
	attach, replace                          bool
}

func captureAttach(n seccompNotification, raw []byte) (*attachInput, error) {
	if n.Args[0] != unix.BPF_PROG_ATTACH && n.Args[0] != unix.BPF_PROG_DETACH || len(raw) < 16 || len(raw) > 4096 {
		return nil, errGuardProtocol
	}
	full := make([]byte, 4096)
	copy(full, raw)
	if guardU32(full, 8) != unix.BPF_CGROUP_DEVICE || !guardZero(full[20:]) {
		return nil, errGuardProtocol
	}
	flags, replacement := guardU32(full, 12), guardU32(full, 16)
	isAttach := n.Args[0] == unix.BPF_PROG_ATTACH
	if isAttach {
		if flags != unix.BPF_F_ALLOW_MULTI && flags != unix.BPF_F_ALLOW_MULTI|unix.BPF_F_REPLACE {
			return nil, errGuardProtocol
		}
		if flags&unix.BPF_F_REPLACE != 0 && (len(raw) < 20 || replacement == 0) || flags&unix.BPF_F_REPLACE == 0 && replacement != 0 {
			return nil, errGuardProtocol
		}
	} else if flags != 0 || replacement != 0 {
		return nil, errGuardProtocol
	}
	normalized := append([]byte(nil), raw...)
	for _, span := range [][2]int{{0, 4}, {4, 8}, {16, 20}} {
		for i := span[0]; i < span[1] && i < len(normalized); i++ {
			normalized[i] = 0
		}
	}
	return &attachInput{outer: append([]byte(nil), raw...), normalized: normalized, targetSlot: guardU32(full, 0), programSlot: guardU32(full, 4), replacementSlot: replacement, flags: flags, attach: isAttach, replace: flags&unix.BPF_F_REPLACE != 0}, nil
}

// performAttach runs exactly one effect after the caller has recorded and
// committed the transaction and authenticated all held file descriptions.
// A failed response or later copyout does not authorize repeating this call.
func performAttach(h *heldNotification, in *attachInput, targetFD, programFD, replacementFD int) (int64, error) {
	if h == nil || in == nil || targetFD < 0 || programFD < 0 || in.replace != (replacementFD >= 0) {
		return 0, errGuardProtocol
	}
	outer := append([]byte(nil), in.outer...)
	guardPut32(outer, 0, uint32(targetFD))
	guardPut32(outer, 4, uint32(programFD))
	if in.replace {
		guardPut32(outer, 16, uint32(replacementFD))
	}
	command := uintptr(unix.BPF_PROG_DETACH)
	if in.attach {
		command = unix.BPF_PROG_ATTACH
	}
	result, _, errno := unix.Syscall(unix.SYS_BPF, command, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	if errno != 0 {
		return -int64(errno), nil
	}
	return int64(result), nil
}
