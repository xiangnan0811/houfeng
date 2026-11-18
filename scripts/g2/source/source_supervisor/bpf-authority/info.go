//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"reflect"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type infoInput struct {
	outer, normalized, data []byte
	slot                    uint32
	address                 uint64
	class                   string
}
type infoEffect struct {
	result               int64
	length               uint32
	objectType, kernelID uint32
	data                 []byte
	attributes           map[string]any
}

// captureScalarInfo rejects every nested program-info pointer, capacity and
// record-size field before any kernel effect. The returned scalar values,
// including nonzero lengths/counts reported by the kernel, are never masked.
func captureScalarInfo(h *heldNotification, raw []byte, class string) (*infoInput, error) {
	if h == nil || h.memory == nil || h.notice.Args[0] != unix.BPF_OBJ_GET_INFO_BY_FD || len(raw) < 16 || len(raw) > 4096 || !guardZero(raw[16:]) {
		return nil, errGuardProtocol
	}
	size, address := guardU32(raw, 4), guardU64(raw, 8)
	want := uint32(0)
	switch class {
	case "program":
		want = 232
	case "map":
		want = 88
	default:
		return nil, errGuardProtocol
	}
	if size < want || size > 65536 || address == 0 {
		return nil, errGuardProtocol
	}
	data, err := h.memory.copyIn(address, int(size))
	if err != nil {
		return nil, err
	}
	if !guardZero(data) {
		return nil, errGuardProtocol
	}
	normalized := append([]byte(nil), raw...)
	for _, span := range [][2]int{{0, 4}, {8, 16}} {
		for i := span[0]; i < span[1]; i++ {
			normalized[i] = 0
		}
	}
	return &infoInput{outer: append([]byte(nil), raw...), normalized: normalized, data: data, slot: guardU32(raw, 0), address: address, class: class}, nil
}

func decodeProgramInfo(info []byte) (uint32, uint32, map[string]any, error) {
	if len(info) < 232 || guardU32(info, 4) == 0 || guardU32(info, 20)%8 != 0 {
		return 0, 0, nil, errGuardProtocol
	}
	name, err := kernelObjectName(info[64:80])
	if err != nil {
		return 0, 0, nil, err
	}
	attrs := map[string]any{"name": name, "tag": hex.EncodeToString(info[8:16]), "load_time": jsonU64(guardU64(info, 40)), "created_by_uid": jsonU64(uint64(guardU32(info, 48))), "translated_insn_count": jsonU64(uint64(guardU32(info, 20) / 8)), "verified_insns": jsonU64(uint64(guardU32(info, 216))), "attach_btf_id": jsonU64(uint64(guardU32(info, 224)))}
	return guardU32(info, 0), guardU32(info, 4), attrs, nil
}

func decodeMapInfo(info []byte) (uint32, uint32, map[string]any, error) {
	if len(info) < 88 || guardU32(info, 4) == 0 {
		return 0, 0, nil, errGuardProtocol
	}
	name, err := kernelObjectName(info[24:40])
	if err != nil {
		return 0, 0, nil, err
	}
	attrs := map[string]any{"name": name, "key_size": jsonU64(uint64(guardU32(info, 8))), "value_size": jsonU64(uint64(guardU32(info, 12))), "max_entries": jsonU64(uint64(guardU32(info, 16))), "map_flags": jsonU64(uint64(guardU32(info, 20))), "btf_id": jsonU64(uint64(guardU32(info, 64))), "btf_key_type_id": jsonU64(uint64(guardU32(info, 68))), "btf_value_type_id": jsonU64(uint64(guardU32(info, 72))), "map_extra": jsonU64(guardU64(info, 80))}
	return guardU32(info, 0), guardU32(info, 4), attrs, nil
}

// executeScalarInfo uses only the broker-held imported descriptor and private
// struct buffer. The caller establishes object ownership and commits its
// transaction before invoking this operation.
func executeScalarInfo(h *heldNotification, in *infoInput, fd int, object *objectIncarnation) (*infoEffect, error) {
	if h == nil || in == nil || fd < 0 || object == nil || object.class != in.class || object.state != "live" || object.removalSeq != 0 {
		return nil, errGuardProtocol
	}
	if in.class == "program" {
		if err := requireProgramInfoVisibility(); err != nil {
			return nil, err
		}
	}
	outer := append([]byte(nil), in.outer...)
	info := append([]byte(nil), in.data...)
	guardPut32(outer, 0, uint32(fd))
	guardPut64(outer, 8, uint64(uintptr(unsafe.Pointer(&info[0]))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_OBJ_GET_INFO_BY_FD, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	runtime.KeepAlive(info)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	out := &infoEffect{result: signed, length: guardU32(outer, 4), data: info}
	if errno != 0 {
		return out, nil
	}
	if out.length > uint32(len(info)) {
		return out, errGuardProtocol
	}
	var typ, id uint32
	var attrs map[string]any
	var err error
	if in.class == "program" {
		typ, id, attrs, err = decodeProgramInfo(info)
	} else {
		typ, id, attrs, err = decodeMapInfo(info)
	}
	if err != nil {
		return out, errGuardProtocol
	}
	out.objectType, out.kernelID = typ, id
	out.attributes = attrs
	if uint64(id) != object.id || typ != object.objectType || !reflect.DeepEqual(attrs, object.attributes) {
		return out, errGuardProtocol
	}
	return out, nil
}

func copyScalarInfo(h *heldNotification, in *infoInput, effect *infoEffect) error {
	if h == nil || in == nil || effect == nil {
		return errGuardProtocol
	}
	if effect.result < 0 {
		return nil
	}
	if effect.length == 0 || int(effect.length) > len(effect.data) {
		return errGuardProtocol
	}
	if err := h.memory.copyOut(in.address, effect.data[:effect.length]); err != nil {
		return err
	}
	var length [4]byte
	guardPut32(length[:], 0, effect.length)
	return h.memory.copyOut(h.notice.Args[1]+4, length[:])
}
