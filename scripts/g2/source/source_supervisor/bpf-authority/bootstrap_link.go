//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// observeBootstrapLinkInfo joins the still-held LINK_CREATE result descriptor
// to real BPF link info, without inventing a link object class or cookie.
func observeBootstrapLinkInfo(rec *bootstrapRecorder, fd int, program bootstrapProgramHandle) (uint32, error) {
	if rec == nil || fd < 0 || int64(fd) > math.MaxInt32 || program.cookie == 0 || program.kernelID == 0 || program.attachBTF == 0 {
		return 0, errGuardProtocol
	}
	const size = 64
	outer := make([]byte, 16)
	guardPut32(outer, 0, uint32(fd))
	guardPut32(outer, 4, size)
	possible := map[string]any{"info_len": jsonU64(size), "link_id": jsonU64(math.MaxUint32), "program_cookie": jsonU64(program.cookie), "info_hex": hex.EncodeToString(make([]byte, size))}
	t, err := rec.prepare("OBJ_GET_INFO_BY_FD", outer, nil, nil, []any{}, possible)
	if err != nil {
		return 0, err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return 0, err
	}
	actual := append([]byte(nil), outer...)
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
	var id uint32
	if errno == 0 {
		id = guardU32(info[:], 4)
		if result != 0 || guardU32(actual, 4) != size || guardU32(info[:], 0) != 2 || id == 0 || uint64(guardU32(info[:], 8)) != program.kernelID || !guardZero(info[12:16]) || guardU32(info[:], 16) != uint32(ebpf.AttachLSMMac) || guardU32(info[:], 24) != program.attachBTF || !guardZero(info[28:]) {
			return 0, errGuardProtocol
		}
		output = map[string]any{"info_len": jsonU64(size), "link_id": jsonU64(uint64(id)), "program_cookie": jsonU64(program.cookie), "info_hex": hex.EncodeToString(info[:])}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), output); err != nil {
			return 0, err
		}
	}
	if errno != 0 {
		return 0, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap link info: %w", errno)
	}
	return id, nil
}

func attachBootstrapProgram(rec *bootstrapRecorder, hook string, program bootstrapProgramHandle) (_ int, _ uint32, err error) {
	if rec == nil || program.fd < 0 || int64(program.fd) > math.MaxInt32 || program.cookie == 0 || (hook != "bpf" && hook != "inode_rmdir" && hook != "inode_mkdir") {
		return -1, 0, errGuardProtocol
	}
	outer := make([]byte, 64)
	guardPut32(outer, 8, uint32(ebpf.AttachLSMMac))
	inputs := []any{map[string]any{"field": "link_create.prog_fd", "kind": "program", "cookie": jsonU64(program.cookie), "requester_fd": jsonU64(uint64(program.fd))}}
	possible := map[string]any{"hook": hook, "link_id": jsonU64(math.MaxUint32), "program_cookie": jsonU64(program.cookie)}
	t, err := rec.prepare("LINK_CREATE", outer, nil, nil, inputs, possible)
	if err != nil {
		return -1, 0, err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return -1, 0, err
	}
	actual := append([]byte(nil), outer...)
	guardPut32(actual, 0, uint32(program.fd))
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_LINK_CREATE, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed := int64(fd)
	if errno != 0 {
		signed = -int64(errno)
	}
	keep := false
	defer func() {
		if !keep && errno == 0 && fd <= math.MaxInt32 {
			_ = unix.Close(int(fd))
		}
	}()
	if errno != 0 {
		if err = rec.transition(t, "effect_observed", jsonI64(signed), nil); err != nil {
			return -1, 0, err
		}
		if err = rec.transition(t, "response_settled", jsonI64(signed), nil); err != nil {
			return -1, 0, err
		}
		if err = rec.transition(t, "retired", jsonI64(signed), nil); err != nil {
			return -1, 0, err
		}
		return -1, 0, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap LINK_CREATE: %w", errno)
	}
	if fd > math.MaxInt32 {
		return -1, 0, errGuardProtocol
	}
	output := map[string]any{"hook": hook, "link_id": jsonU64(0), "program_cookie": jsonU64(program.cookie)}
	effectIdx := len(rec.entries)
	if err = rec.transition(t, "effect_observed", jsonI64(signed), output); err != nil {
		return -1, 0, err
	}
	id, err := observeBootstrapLinkInfo(rec, int(fd), program)
	if err != nil {
		return -1, 0, err
	}
	output["link_id"] = jsonU64(uint64(id))
	rec.entries[effectIdx].body["output"].(map[string]any)["link_id"] = jsonU64(uint64(id))
	if err = rec.transition(t, "response_settled", jsonI64(signed), output); err != nil {
		return -1, 0, err
	}
	if err = rec.transition(t, "retired", jsonI64(signed), output); err != nil {
		return -1, 0, err
	}
	keep = true
	return int(fd), id, nil
}
