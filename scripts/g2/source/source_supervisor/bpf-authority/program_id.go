//go:build linux && amd64 && !cgo

package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type getProgramIDInput struct {
	outer, normalized []byte
	programID         uint32
}

func captureGetProgramID(n seccompNotification, raw []byte) (*getProgramIDInput, error) {
	if n.Args[0] != unix.BPF_PROG_GET_FD_BY_ID || len(raw) < 12 || len(raw) > 4096 {
		return nil, errGuardProtocol
	}
	full := make([]byte, 4096)
	copy(full, raw)
	id := guardU32(full, 0)
	if id == 0 || !guardZero(full[4:]) {
		return nil, errGuardProtocol
	}
	return &getProgramIDInput{outer: append([]byte(nil), raw...), normalized: append([]byte(nil), raw...), programID: id}, nil
}

// openKnownProgram is callable only after the caller matches programID to an
// already-held live incarnation of this grant. The real returned descriptor
// is retained even when subsequent notification delivery is uncertain.
func openKnownProgram(h *heldNotification, in *getProgramIDInput, owned *objectIncarnation) (bpfEffect, error) {
	out := bpfEffect{FD: -1}
	if h == nil || in == nil || owned == nil || owned.class != "program" || owned.id != uint64(in.programID) || owned.state != "live" || owned.removalSeq != 0 {
		return out, errGuardProtocol
	}
	outer := append([]byte(nil), in.outer...)
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_GET_FD_BY_ID, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	if errno != 0 {
		out.Errno = errno
		return out, nil
	}
	if fd > 0x7fffffff {
		return out, errGuardProtocol
	}
	out.FD = int(fd)
	return out, nil
}
