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

// observeRuntimeObjectInfo performs a separately audited broker syscall on an
// actual retained object descriptor. It never derives kernel attributes from
// the requested program or map. The enclosing creation remains pending until
// the returned ID is matched to its guard publication.
func (b *authorityBroker) observeRuntimeObjectInfo(fd int, cookie uint64, class string, expectedType uint32) (uint32, map[string]any, error) {
	if b == nil || b.effects == nil || b.resolver == nil || b.checkpoint == nil || fd < 0 || cookie == 0 {
		return 0, nil, errGuardProtocol
	}
	if b.checkpoint.invalid {
		t := b.effects.pending
		committed := t != nil && t.effectFD == fd && b.canDrainCommitted(t)
		grant := b.grants[b.effects.objectGrant[cookie]]
		held, exists := b.effects.objectFD[cookie]
		cleanup := exists && held == fd && b.revokedCleanupAllowed(grant) && b.canObserveOwnerDeath()
		if !committed && !cleanup {
			return 0, nil, errGuardProtocol
		}
	}
	if b.reader == nil || b.reader.invalid || !pidfdSelects(b.reader.pidfd, b.reader.pid) {
		return 0, nil, errGuardProtocol
	}
	if class == "program" {
		if err := requireProgramInfoVisibility(); err != nil {
			return 0, nil, err
		}
	}
	size := 0
	switch class {
	case "program":
		size = 232
	case "map":
		size = 88
	default:
		return 0, nil, errGuardProtocol
	}
	if b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 {
		return 0, nil, errGuardProtocol
	}
	id := b.effects.nextTransaction
	b.effects.nextTransaction++
	attr := make([]byte, 16)
	guardPut32(attr, 4, uint32(size))
	inputs := []any{map[string]any{"field": "info.bpf_fd", "kind": class, "cookie": jsonU64(cookie), "requester_fd": jsonU64(uint64(fd))}}
	body := func(state string, result, output any) map[string]any {
		return map[string]any{"id": jsonU64(id), "grant_id": nil, "invocation_id": nil, "requester_cookie": nil, "notification_id": nil, "command": "OBJ_GET_INFO_BY_FD",
			"attributes": map[string]any{"attr_size": jsonU64(uint64(len(attr))), "attr_hex": hex.EncodeToString(attr)}, "program_bytes": nil, "license": nil, "input_objects": inputs,
			"executor_cookie": jsonU64(b.resolver.brokerCookie), "state": state, "result": result, "returned_fd": nil, "output": output, "cleanup": bootstrapCleanup()}
	}
	var maxAttrs map[string]any
	if class == "program" {
		maxAttrs = map[string]any{"name": "1234567890123456", "tag": "ffffffffffffffff", "load_time": jsonU64(math.MaxUint64), "created_by_uid": jsonU64(math.MaxUint32), "translated_insn_count": jsonU64(math.MaxUint32), "verified_insns": jsonU64(math.MaxUint32), "attach_btf_id": jsonU64(math.MaxUint32)}
	} else {
		maxAttrs = map[string]any{"name": "1234567890123456", "key_size": jsonU64(math.MaxUint32), "value_size": jsonU64(math.MaxUint32), "max_entries": jsonU64(math.MaxUint32), "map_flags": jsonU64(math.MaxUint32), "btf_id": jsonU64(math.MaxUint32), "btf_key_type_id": jsonU64(math.MaxUint32), "btf_value_type_id": jsonU64(math.MaxUint32), "map_extra": jsonU64(math.MaxUint64)}
	}
	possible := map[string]any{"info_len": jsonU64(uint64(size)), "object_cookie": jsonU64(cookie), "type": jsonU64(math.MaxUint32), "kernel_id": jsonU64(math.MaxUint32), "attributes": maxAttrs}
	if err := preflightRuntimeRecord(b, "transaction", body("effect_observed", jsonI64(math.MinInt64), possible)); err != nil {
		return 0, nil, err
	}
	if err := appendRuntimeRecord(b, "transaction", body("prepared", nil, nil)); err != nil {
		return 0, nil, err
	}
	if err := appendRuntimeRecord(b, "transaction", body("committed", nil, nil)); err != nil {
		return 0, nil, err
	}
	b.checkpoint.activeTransactions++
	actual := append([]byte(nil), attr...)
	info := make([]byte, size)
	guardPut32(actual, 0, uint32(fd))
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&info[0]))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_OBJ_GET_INFO_BY_FD, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(info)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "OBJ_GET_INFO_BY_FD", Number: unix.BPF_OBJ_GET_INFO_BY_FD, Result: signed}); err != nil {
		b.checkpoint.markFault()
		return 0, nil, err
	}
	var objectID uint32
	var attributes map[string]any
	var output any
	if errno == 0 {
		if result != 0 || guardU32(actual, 4) != uint32(size) {
			b.checkpoint.markFault()
			return 0, nil, errGuardProtocol
		}
		var typ uint32
		var err error
		if class == "program" {
			typ, objectID, attributes, err = decodeProgramInfo(info)
		} else {
			typ, objectID, attributes, err = decodeMapInfo(info)
		}
		if err != nil || typ != expectedType {
			b.checkpoint.markFault()
			return 0, nil, errGuardProtocol
		}
		output = map[string]any{"info_len": jsonU64(uint64(size)), "object_cookie": jsonU64(cookie), "type": jsonU64(uint64(typ)), "kernel_id": jsonU64(uint64(objectID)), "attributes": attributes}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err := appendRuntimeRecord(b, "transaction", body(state, jsonI64(signed), output)); err != nil {
			b.checkpoint.markFault()
			return 0, nil, err
		}
	}
	b.checkpoint.activeTransactions--
	if errno != 0 {
		return 0, nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: object info: %w", errno)
	}
	return objectID, attributes, nil
}
