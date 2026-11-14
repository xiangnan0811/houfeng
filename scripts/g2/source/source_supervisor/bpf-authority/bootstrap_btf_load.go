//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// loadBootstrapBTF performs the observed, bootstrap-only BTF_LOAD. The full
// exact kernel input is budgeted before the effect and remains in the ledger;
// its returned broker FD is held until the isolated kernel lifetime ends.
func loadBootstrapBTF(recorder *bootstrapRecorder, btfBytes []byte) (int, error) {
	if recorder == nil || len(btfBytes) == 0 || len(btfBytes) > math.MaxUint32 {
		return -1, errGuardProtocol
	}
	outer := make([]byte, 32)
	guardPut32(outer, 16, uint32(len(btfBytes)))
	output := map[string]any{"btf_bytes": hex.EncodeToString(btfBytes), "btf_sha256": hashBytes(btfBytes)}
	t, err := recorder.prepare("BTF_LOAD", outer, nil, nil, []any{}, output)
	if err != nil {
		return -1, err
	}
	if err = recorder.transition(t, "committed", nil, nil); err != nil {
		return -1, err
	}
	actual := append([]byte(nil), outer...)
	guardPut64(actual, 0, uint64(uintptr(unsafe.Pointer(&btfBytes[0]))))
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_BTF_LOAD, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(btfBytes)
	result := int64(fd)
	if errno != 0 {
		result = -int64(errno)
	}
	if err = recorder.transition(t, "effect_observed", jsonI64(result), output); err != nil {
		if errno == 0 && fd <= math.MaxInt32 {
			_ = unix.Close(int(fd))
		}
		return -1, err
	}
	if err = recorder.transition(t, "response_settled", jsonI64(result), output); err != nil {
		if errno == 0 && fd <= math.MaxInt32 {
			_ = unix.Close(int(fd))
		}
		return -1, err
	}
	if err = recorder.transition(t, "retired", jsonI64(result), output); err != nil {
		if errno == 0 && fd <= math.MaxInt32 {
			_ = unix.Close(int(fd))
		}
		return -1, err
	}
	if errno != 0 {
		return -1, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap BTF_LOAD: %w", errno)
	}
	if fd > math.MaxInt32 {
		return -1, errGuardProtocol
	}
	return int(fd), nil
}
