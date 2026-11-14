//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func observeBootstrapProgramInfo(rec *bootstrapRecorder, fd int, cookie uint64, expectedType uint32) (uint32, map[string]any, error) {
	if rec == nil || fd < 0 || int64(fd) > 0x7fffffff || cookie == 0 {
		return 0, nil, errGuardProtocol
	}
	if err := requireProgramInfoVisibility(); err != nil {
		return 0, nil, err
	}
	const size = 232
	outer := make([]byte, 16)
	guardPut32(outer, 4, size)
	inputs := []any{map[string]any{"field": "info.bpf_fd", "kind": "program", "cookie": jsonU64(cookie), "requester_fd": jsonU64(uint64(fd))}}
	maxAttr := map[string]any{"name": "1234567890123456", "tag": "ffffffffffffffff", "load_time": jsonU64(^uint64(0)), "created_by_uid": jsonU64(0xffffffff), "translated_insn_count": jsonU64(0xffffffff), "verified_insns": jsonU64(0xffffffff), "attach_btf_id": jsonU64(0xffffffff)}
	possible := map[string]any{"info_len": jsonU64(size), "object_cookie": jsonU64(cookie), "type": jsonU64(0xffffffff), "kernel_id": jsonU64(0xffffffff), "attributes": maxAttr}
	t, err := rec.prepare("OBJ_GET_INFO_BY_FD", outer, nil, nil, inputs, possible)
	if err != nil {
		return 0, nil, err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return 0, nil, err
	}
	actual := append([]byte(nil), outer...)
	guardPut32(actual, 0, uint32(fd))
	var info [size]byte
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&info[0]))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_OBJ_GET_INFO_BY_FD, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(info)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var output any
	var attrs map[string]any
	var id uint32
	if errno == 0 {
		if guardU32(actual, 4) != size {
			return 0, nil, errGuardProtocol
		}
		typ, actualID, observed, e := decodeProgramInfo(info[:])
		if e != nil || typ != expectedType {
			return 0, nil, errGuardProtocol
		}
		id, attrs = actualID, observed
		output = map[string]any{"info_len": jsonU64(size), "object_cookie": jsonU64(cookie), "type": jsonU64(uint64(typ)), "kernel_id": jsonU64(uint64(actualID)), "attributes": attrs}
	}
	if err = rec.transition(t, "effect_observed", jsonI64(signed), output); err != nil {
		return 0, nil, err
	}
	if err = rec.transition(t, "response_settled", jsonI64(signed), output); err != nil {
		return 0, nil, err
	}
	if err = rec.transition(t, "retired", jsonI64(signed), output); err != nil {
		return 0, nil, err
	}
	if errno != 0 {
		return 0, nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap program info: %w", errno)
	}
	return id, attrs, nil
}
