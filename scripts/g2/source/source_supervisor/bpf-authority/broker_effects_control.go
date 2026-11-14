//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// executeRegistryMap performs one actual broker-only operation on one of the
// two sealed registry maps. The returned sequence refers to the complete
// registry record; for a lookup it refers to the earlier record being read
// back. A failed syscall is recorded and cannot be retried implicitly.
func (b *authorityBroker) executeRegistryMap(command uint32, mapName string, key, value []byte, flags uint64, record map[string]any, previousSequence uint64, cleanup bool, parent *runtimeEffectTransaction) (uint64, []byte, error) {
	if b == nil || b.effects == nil || b.resolver == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil {
		return 0, nil, errGuardProtocol
	}
	if cleanup && b.checkpoint.invalid && !b.canObserveOwnerDeath() {
		return 0, nil, errGuardProtocol
	}
	if parent == nil {
		if b.effects.pending != nil {
			return 0, nil, errGuardProtocol
		}
	} else if err := b.validateNestedRegistry(parent, command, mapName, key, value, flags, record, previousSequence, cleanup); err != nil {
		return 0, nil, err
	}
	if !cleanup && (b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.lifecycle.paused || b.reader == nil || b.reader.invalid || !pidfdSelects(b.reader.pidfd, b.reader.pid)) {
		return 0, nil, errGuardProtocol
	}
	keySize, valueSize := 0, 0
	switch mapName {
	case "realm_targets":
		keySize, valueSize = 16, 24
	case "retire_tasks":
		keySize, valueSize = 32, 40
	default:
		return 0, nil, errGuardProtocol
	}
	m, ok := b.authority.maps[mapName]
	if !ok || m.fd < 0 || m.cookie == 0 || len(key) != keySize {
		return 0, nil, errGuardProtocol
	}
	switch command {
	case unix.BPF_MAP_UPDATE_ELEM:
		if len(value) != valueSize || flags > unix.BPF_EXIST || record == nil || previousSequence != 0 {
			return 0, nil, errGuardProtocol
		}
	case unix.BPF_MAP_LOOKUP_ELEM:
		if len(value) != 0 || flags != 0 || record != nil || previousSequence == 0 || previousSequence > uint64(len(b.transcript.entries)) {
			return 0, nil, errGuardProtocol
		}
	case unix.BPF_MAP_DELETE_ELEM:
		if len(value) != 0 || flags != 0 || record == nil || previousSequence != 0 {
			return 0, nil, errGuardProtocol
		}
	default:
		return 0, nil, errGuardProtocol
	}
	absenceLookup := false
	if command == unix.BPF_MAP_LOOKUP_ELEM {
		entry := b.transcript.entries[previousSequence-1].record
		if body, ok := entry["body"].(map[string]any); ok && entry["kind"] == "registry" && body["action"] == "delete" {
			if err := b.validateRegistryDelete(mapName, key, previousSequence); err != nil {
				return 0, nil, err
			}
			absenceLookup = true
		}
	}
	if b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 || len(b.effects.transactions) >= 4096 {
		return 0, nil, errGuardProtocol
	}
	id := b.effects.nextTransaction
	b.effects.nextTransaction++
	name := bpfCommand(uint64(command))
	if name == "" {
		return 0, nil, errGuardProtocol
	}
	attr := make([]byte, 32)
	guardPut64(attr, 24, flags)
	inputs := []any{map[string]any{"field": "map_fd", "kind": "map", "cookie": jsonU64(m.cookie), "requester_fd": jsonU64(uint64(m.fd))}}
	body := func(state string, result, output any) map[string]any {
		return map[string]any{"id": jsonU64(id), "grant_id": nil, "invocation_id": nil, "requester_cookie": nil, "notification_id": nil, "command": name,
			"attributes": map[string]any{"attr_size": jsonU64(uint64(len(attr))), "attr_hex": fmt.Sprintf("%x", attr)}, "program_bytes": nil, "license": nil, "input_objects": inputs,
			"executor_cookie": jsonU64(b.resolver.brokerCookie), "state": state, "result": result, "returned_fd": nil, "output": output, "cleanup": bootstrapCleanup()}
	}
	possible := map[string]any{"registry_sequence": jsonU64(math.MaxUint64)}
	if command == unix.BPF_MAP_LOOKUP_ELEM {
		possible["value_hex"] = fmt.Sprintf("%0*x", valueSize*2, 0)
	}
	if err := preflightRuntimeRecord(b, "transaction", body("effect_observed", jsonI64(math.MinInt64), possible)); err != nil {
		return 0, nil, err
	}
	var registry map[string]any
	if record != nil {
		registry = make(map[string]any, len(record)+1)
		for k, v := range record {
			registry[k] = v
		}
		if _, exists := registry["transaction_id"]; exists {
			return 0, nil, errGuardProtocol
		}
		registry["transaction_id"] = jsonU64(id)
		registry["parent_transaction_id"] = nil
		if parent != nil {
			registry["parent_transaction_id"] = jsonU64(parent.id)
		}
		if err := preflightRuntimeRecord(b, "registry", registry); err != nil {
			return 0, nil, err
		}
	}
	if err := appendRuntimeRecord(b, "transaction", body("prepared", nil, nil)); err != nil {
		return 0, nil, err
	}
	if err := appendRuntimeRecord(b, "transaction", body("committed", nil, nil)); err != nil {
		return 0, nil, err
	}
	b.checkpoint.activeTransactions++
	actual := append([]byte(nil), attr...)
	keyCopy := append([]byte(nil), key...)
	var data []byte
	if command == unix.BPF_MAP_UPDATE_ELEM {
		data = append([]byte(nil), value...)
	} else if command == unix.BPF_MAP_LOOKUP_ELEM {
		data = make([]byte, valueSize)
	}
	guardPut32(actual, 0, uint32(m.fd))
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&keyCopy[0]))))
	if len(data) != 0 {
		guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&data[0]))))
	}
	result, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(command), uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(keyCopy)
	runtime.KeepAlive(data)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: name, Number: uint64(command), Result: signed}); err != nil {
		b.checkpoint.markFault()
		return 0, nil, err
	}
	sequence := previousSequence
	var output any
	if errno == 0 {
		if result != 0 {
			b.checkpoint.markFault()
			return 0, nil, errGuardProtocol
		}
		if registry != nil {
			if err := appendRuntimeRecord(b, "registry", registry); err != nil {
				b.checkpoint.markFault()
				return 0, nil, err
			}
			sequence = uint64(len(b.transcript.entries))
		}
		output = map[string]any{"registry_sequence": jsonU64(sequence)}
		if command == unix.BPF_MAP_LOOKUP_ELEM {
			output = map[string]any{"registry_sequence": jsonU64(sequence), "value_hex": fmt.Sprintf("%x", data)}
		}
	} else if errno == unix.ENOENT && absenceLookup {
		output = map[string]any{"registry_sequence": jsonU64(sequence), "value_hex": nil}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err := appendRuntimeRecord(b, "transaction", body(state, jsonI64(signed), output)); err != nil {
			b.checkpoint.markFault()
			return 0, nil, err
		}
	}
	b.checkpoint.activeTransactions--
	if errno != 0 {
		return 0, nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: registry %s: %w", name, errno)
	}
	return sequence, data, nil
}

// lookupRegistryAbsent proves deletion through a second real kernel lookup.
// ENOENT is an observed syscall result, not a synthesized successful lookup.
func (b *authorityBroker) validateRegistryDelete(mapName string, key []byte, deleteSequence uint64) error {
	if b == nil || b.transcript == nil || deleteSequence == 0 || deleteSequence > uint64(len(b.transcript.entries)) {
		return errGuardProtocol
	}
	entry := b.transcript.entries[deleteSequence-1].record
	body, ok := entry["body"].(map[string]any)
	if !ok || entry["kind"] != "registry" || body["map"] != mapName || body["action"] != "delete" || body["value"] != nil {
		return errGuardProtocol
	}
	fields, ok := body["key"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	root, ok := exactNumber(fields["root_kn_id"])
	if !ok || root == 0 {
		return errGuardProtocol
	}
	node, ok := exactNumber(fields["kn_id"])
	if !ok || node == 0 {
		return errGuardProtocol
	}
	grantID, valid := exactNumber(body["grant_id"])
	if !valid || grantID == 0 || b.effects == nil || b.effects.registry == nil {
		return errGuardProtocol
	}
	target := b.effects.registry.targets[cgroupKey{root: root, id: node}]
	if target == nil || target.grant == nil || target.grant.id != grantID || target.group == nil || target.group.RootCookie != root || target.group.KernfsID != node {
		return errGuardProtocol
	}
	var expected []byte
	switch mapName {
	case "realm_targets":
		if !exactKeys(fields, "root_kn_id", "kn_id") {
			return errGuardProtocol
		}
		expected = make([]byte, 16)
	case "retire_tasks":
		if !exactKeys(fields, "root_kn_id", "kn_id", "tgid", "leader_start_ns") {
			return errGuardProtocol
		}
		tgid, valid := exactNumber(fields["tgid"])
		if !valid || tgid == 0 || tgid > math.MaxUint32 {
			return errGuardProtocol
		}
		start, valid := exactNumber(fields["leader_start_ns"])
		if !valid || start == 0 {
			return errGuardProtocol
		}
		expected = make([]byte, 32)
		guardPut32(expected, 16, uint32(tgid))
		guardPut64(expected, 24, start)
	default:
		return errGuardProtocol
	}
	guardPut64(expected, 0, root)
	guardPut64(expected, 8, node)
	if !equalBytes(expected, key) {
		return errGuardProtocol
	}
	return nil
}

func (b *authorityBroker) lookupRegistryAbsent(mapName string, key []byte, deleteSequence uint64, cleanup bool) error {
	if err := b.validateRegistryDelete(mapName, key, deleteSequence); err != nil {
		return err
	}
	_, _, err := b.executeRegistryMap(unix.BPF_MAP_LOOKUP_ELEM, mapName, key, nil, 0, nil, deleteSequence, cleanup, nil)
	if !errors.Is(err, unix.ENOENT) {
		if err == nil {
			return errGuardProtocol
		}
		return err
	}
	return nil
}
