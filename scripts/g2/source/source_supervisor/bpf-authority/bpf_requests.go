//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// bpfEffect records the actual syscall result separately from a transport or
// settlement error. In particular, a successful returned fd remains owned by
// the broker even if the original notification subsequently disappears.
type bpfEffect struct {
	FD    int
	Errno unix.Errno
}

func capturedAttribute(m *notificationMemory, n seccompNotification) ([]byte, error) {
	if n.Args[1] == 0 || n.Args[2] == 0 || n.Args[2] > 4096 {
		return nil, errGuardProtocol
	}
	return m.copyIn(n.Args[1], int(n.Args[2]))
}

// featureMapAttribute recognizes only the source-backed cilium feature_test
// array map. The input is the private copy; the task's address is never passed
// to bpf(2). Trailing bytes and all other fields must be zero.
func featureMapAttribute(raw []byte) bool {
	if len(raw) < 44 || len(raw) > 4096 {
		return false
	}
	if guardU32(raw, 0) != unix.BPF_MAP_TYPE_ARRAY || guardU32(raw, 4) != 4 || guardU32(raw, 8) != 4 || guardU32(raw, 12) != 1 {
		return false
	}
	if !guardZero(raw[16:28]) {
		return false
	}
	var name [16]byte
	copy(name[:], []byte("feature_test"))
	return bytes.Equal(raw[28:44], name[:]) && guardZero(raw[44:])
}

// executeFeatureMap performs the one validated creation. The caller owns its
// precommitted transaction and the returned fd even when later settlement
// fails; it must never repeat this syscall on an unknown result.
func executeFeatureMap(m *notificationMemory, n seccompNotification, raw []byte) (bpfEffect, error) {
	result := bpfEffect{FD: -1}
	if n.Args[0] != unix.BPF_MAP_CREATE || !featureMapAttribute(raw) {
		return result, errGuardProtocol
	}
	r, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_CREATE, uintptr(unsafe.Pointer(&raw[0])), uintptr(len(raw)))
	runtime.KeepAlive(raw)
	if errno != 0 {
		result.Errno = errno
		return result, nil
	}
	if r > math.MaxInt32 {
		return result, fmt.Errorf("BPF_AUTH_PENDING: invalid map descriptor")
	}
	result.FD = int(r)
	return result, nil
}

type programInput struct {
	outer         []byte
	normalized    []byte
	instructions  []byte
	license       []byte
	logAddress    uint64
	log           []byte
	returnedOuter []byte
}

// programLoadAttributeFieldsValid checks the fixed scalar and metadata shape
// before the caller removes pointers from its normalized evidence.
func programLoadAttributeFieldsValid(full []byte, attrSize int) bool {
	if len(full) != 4096 || attrSize < 24 || attrSize > 4096 {
		return false
	}
	var expected [4096]byte
	copy(expected[:40], full[:40])
	switch guardU32(full, 0) {
	case unix.BPF_PROG_TYPE_CGROUP_DEVICE:
		funcSize, lineSize := guardU32(full, 76), guardU32(full, 92)
		if funcSize == 8 && lineSize == 16 && attrSize >= 108 {
			guardPut32(expected[:], 76, 8)
			guardPut32(expected[:], 92, 16)
		} else if funcSize != 0 || lineSize != 0 {
			return false
		}
	case unix.BPF_PROG_TYPE_SOCKET_FILTER:
		guardPut32(expected[:], 72, math.MaxUint32)
		guardPut32(expected[:], 88, 1)
	default:
		return false
	}
	return bytes.Equal(full, expected[:])
}

// programLoadInput snapshots the complete nested input and recognizes only
// the two source-backed probes or the fixed Docker device program. Validation
// is performed against the bytes subsequently given to the kernel.
func programLoadInput(m *notificationMemory, n seccompNotification, raw []byte, logScratch []byte) (*programInput, error) {
	if n.Args[0] != unix.BPF_PROG_LOAD || len(raw) < 24 || len(raw) > 4096 {
		return nil, errGuardProtocol
	}
	// Zero extension models the kernel's copy_struct_from_user behavior while
	// retaining the actual attr_size separately for the syscall and evidence.
	full := make([]byte, 4096)
	copy(full, raw)
	typ, count := guardU32(full, 0), guardU32(full, 4)
	if count == 0 || count > 4096 {
		return nil, errGuardProtocol
	}
	insnAddr, licenseAddr := guardU64(full, 8), guardU64(full, 16)
	insns, err := m.copyIn(insnAddr, int(count)*8)
	if err != nil {
		return nil, err
	}
	probe, err := probeProgram()
	if err != nil {
		return nil, err
	}
	device, err := deviceProgram()
	if err != nil {
		return nil, err
	}
	var license []byte
	switch {
	case typ == unix.BPF_PROG_TYPE_CGROUP_DEVICE && bytes.Equal(insns, device):
		license = []byte("Apache\x00")
	case typ == unix.BPF_PROG_TYPE_CGROUP_DEVICE && bytes.Equal(insns, probe):
		license = []byte("MIT\x00")
	case typ == unix.BPF_PROG_TYPE_SOCKET_FILTER && bytes.Equal(insns, probe):
		license = []byte("MIT\x00")
	default:
		return nil, errGuardProtocol
	}
	actualLicense, err := m.copyIn(licenseAddr, len(license))
	if err != nil || !bytes.Equal(actualLicense, license) {
		return nil, errGuardProtocol
	}
	logLevel, logSize, logAddr := guardU32(full, 24), guardU32(full, 28), guardU64(full, 32)
	if (logSize != 0 && logSize != 131072) || (logSize == 0 && (logLevel != 0 || logAddr != 0)) || (logSize > 0 && (logLevel != 1 || logAddr == 0)) {
		return nil, errGuardProtocol
	}
	if !programLoadAttributeFieldsValid(full, len(raw)) {
		return nil, errGuardProtocol
	}
	normalized := append([]byte(nil), raw...)
	for _, span := range [][2]int{{8, 16}, {16, 24}, {32, 40}, {80, 88}, {96, 104}} {
		for i := span[0]; i < span[1] && i < len(normalized); i++ {
			normalized[i] = 0
		}
	}
	in := &programInput{outer: append([]byte(nil), raw...), normalized: normalized, instructions: insns, license: license, logAddress: logAddr}
	if logSize > 0 {
		if len(logScratch) >= int(logSize) {
			in.log = logScratch[:int(logSize)]
			clear(in.log)
		} else {
			in.log = make([]byte, int(logSize))
		}
	}
	return in, nil
}

// executeProgramLoad issues the real kernel operation with broker-owned
// instruction, license and log buffers. It never uses CONTINUE and it retains
// an actual returned descriptor for the transaction owner to settle.
func executeProgramLoad(m *notificationMemory, n seccompNotification, in *programInput) (bpfEffect, error) {
	defer func() {
		if in != nil && len(in.log) > 0 {
			// Clear only after copyProgramLoadResult or when copyProgramLoadResult is not called.
		}
	}()
	result := bpfEffect{FD: -1}
	if in == nil || len(in.outer) < 24 || len(in.instructions) == 0 || len(in.license) == 0 {
		return result, errGuardProtocol
	}
	outer := append([]byte(nil), in.outer...)
	guardPut64(outer, 8, uint64(uintptr(unsafe.Pointer(&in.instructions[0]))))
	guardPut64(outer, 16, uint64(uintptr(unsafe.Pointer(&in.license[0]))))
	if len(in.log) > 0 {
		if len(outer) < 40 {
			return result, errGuardProtocol
		}
		guardPut64(outer, 32, uint64(uintptr(unsafe.Pointer(&in.log[0]))))
	}
	r, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_LOAD, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	runtime.KeepAlive(in.instructions)
	runtime.KeepAlive(in.license)
	runtime.KeepAlive(in.log)
	if errno != 0 {
		result.Errno = errno
	} else if r <= math.MaxInt32 {
		result.FD = int(r)
	} else {
		return result, fmt.Errorf("BPF_AUTH_PENDING: invalid program descriptor")
	}
	in.returnedOuter = outer
	return result, nil
}

// copyProgramLoadResult is deliberately separate from the syscall. A caller
// exercising the broker FD limit restores its descriptors and limit first.
// Copyout failure remains an unresolved transaction, never a reason to retry.
func copyProgramLoadResult(m *notificationMemory, n seccompNotification, in *programInput) error {
	if m == nil || in == nil || len(in.returnedOuter) != len(in.outer) {
		return errGuardProtocol
	}
	if len(in.returnedOuter) >= 144 {
		copy(in.outer[140:144], in.returnedOuter[140:144])
		if err := m.copyOut(n.Args[1]+140, in.returnedOuter[140:144]); err != nil {
			return err
		}
	}
	if len(in.log) > 0 {
		err := m.copyOut(in.logAddress, in.log)
		clear(in.log)
		if err != nil {
			return err
		}
	}
	return nil
}

func closeEffect(effect bpfEffect) error {
	if effect.FD < 0 {
		return nil
	}
	if err := unix.Close(effect.FD); err != nil && !errors.Is(err, unix.EINTR) {
		return err
	}
	return nil
}
