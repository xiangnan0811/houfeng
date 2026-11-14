//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func bootstrapMapInput(m bootstrapMapHandle) ([]any, error) {
	if m.fd < 0 || int64(m.fd) > math.MaxInt32 || m.cookie == 0 {
		return nil, errGuardProtocol
	}
	return []any{map[string]any{"field": "map_fd", "kind": "map", "cookie": jsonU64(m.cookie), "requester_fd": jsonU64(uint64(m.fd))}}, nil
}

// installBrokerToken records the one task-storage insertion and proves exact
// readback before freezing the map. The key is the broker's actual pidfd slot;
// neither a PID nor a fabricated synthetic registry key is substituted.
func installBrokerToken(rec *bootstrapRecorder, m bootstrapMapHandle, pidfd int, brokerCookie uint64) error {
	if rec == nil || rec.baseSequence != 1 || pidfd < 0 || int64(pidfd) > math.MaxInt32 || brokerCookie == 0 {
		return errGuardProtocol
	}
	inputs, err := bootstrapMapInput(m)
	if err != nil {
		return err
	}
	key, value := uint32(pidfd), brokerCookie
	outer := make([]byte, 32)
	guardPut64(outer, 24, unix.BPF_NOEXIST)
	possible := map[string]any{"registry_sequence": jsonU64(math.MaxUint64)}
	t, err := rec.prepare("MAP_UPDATE_ELEM", outer, nil, nil, inputs, possible)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual := append([]byte(nil), outer...)
	guardPut32(actual, 0, uint32(m.fd))
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&key))))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&value))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_UPDATE_ELEM, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(key)
	runtime.KeepAlive(value)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var output any
	var registrySeq uint64
	if errno == 0 {
		if result != 0 || uint64(len(rec.entries)) >= math.MaxUint64-rec.baseSequence {
			return errGuardProtocol
		}
		registry := map[string]any{"map": "broker_token", "action": "insert", "key": map[string]any{"pidfd": jsonU64(uint64(pidfd))}, "value": map[string]any{"cookie": jsonU64(brokerCookie)}, "grant_id": nil, "transaction_id": jsonU64(t.id), "parent_transaction_id": nil}
		if err = rec.add("registry", registry); err != nil {
			return err
		}
		registrySeq = rec.baseSequence + uint64(len(rec.entries))
		output = map[string]any{"registry_sequence": jsonU64(registrySeq)}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), output); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: task token update: %w", errno)
	}
	lookup := make([]byte, 32)
	lookupMaximum := map[string]any{"registry_sequence": jsonU64(math.MaxUint64), "value_hex": "0000000000000000"}
	t, err = rec.prepare("MAP_LOOKUP_ELEM", lookup, nil, nil, inputs, lookupMaximum)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual = append([]byte(nil), lookup...)
	guardPut32(actual, 0, uint32(m.fd))
	var observed uint64
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&key))))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&observed))))
	result, _, errno = unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_LOOKUP_ELEM, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(key)
	runtime.KeepAlive(observed)
	signed = int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var lookupOutput any
	if errno == 0 {
		var returned [8]byte
		guardPut64(returned[:], 0, observed)
		lookupOutput = map[string]any{"registry_sequence": jsonU64(registrySeq), "value_hex": fmt.Sprintf("%x", returned[:])}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), lookupOutput); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: task token readback: %w", errno)
	}
	if result != 0 || observed != brokerCookie {
		return errGuardProtocol
	}
	freeze := make([]byte, 4)
	t, err = rec.prepare("MAP_FREEZE", freeze, nil, nil, inputs, output)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual = append([]byte(nil), freeze...)
	guardPut32(actual, 0, uint32(m.fd))
	result, _, errno = unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_FREEZE, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed = int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var freezeOutput any
	if errno == 0 {
		freezeOutput = output
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), freezeOutput); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: task token freeze: %w", errno)
	}
	if result != 0 || registrySeq == 0 {
		return errGuardProtocol
	}
	return nil
}

// installGuardLiveness initializes key 0 of guard_liveness to phase=1 (active),
// proves exact readback via MAP_LOOKUP_ELEM, and freezes the map so ordinary
// userspace cannot modify it. Because map_flags == 0 (no BPF_F_RDONLY_PROG),
// the BPF verifier does not constant-fold the value, allowing the native guard
// module to update the underlying array slot on revocation or death.
func installGuardLiveness(rec *bootstrapRecorder, m bootstrapMapHandle) error {
	if rec == nil || rec.baseSequence != 1 || m.fd < 0 || int64(m.fd) > math.MaxInt32 || m.cookie == 0 {
		return errGuardProtocol
	}
	inputs, err := bootstrapMapInput(m)
	if err != nil {
		return err
	}
	key, value := uint32(0), uint64(1)
	outer := make([]byte, 32)
	guardPut64(outer, 24, unix.BPF_ANY)
	possible := map[string]any{"registry_sequence": jsonU64(math.MaxUint64)}
	t, err := rec.prepare("MAP_UPDATE_ELEM", outer, nil, nil, inputs, possible)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual := append([]byte(nil), outer...)
	guardPut32(actual, 0, uint32(m.fd))
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&key))))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&value))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_UPDATE_ELEM, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(key)
	runtime.KeepAlive(value)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var output any
	var registrySeq uint64
	if errno == 0 {
		if result != 0 || uint64(len(rec.entries)) >= math.MaxUint64-rec.baseSequence {
			return errGuardProtocol
		}
		registry := map[string]any{"map": "guard_liveness", "action": "update", "key": map[string]any{"slot": jsonU64(0)}, "value": map[string]any{"phase": jsonU64(1)}, "grant_id": nil, "transaction_id": jsonU64(t.id), "parent_transaction_id": nil}
		if err = rec.add("registry", registry); err != nil {
			return err
		}
		registrySeq = rec.baseSequence + uint64(len(rec.entries))
		output = map[string]any{"registry_sequence": jsonU64(registrySeq)}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), output); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: guard liveness update: %w", errno)
	}
	lookup := make([]byte, 32)
	lookupMaximum := map[string]any{"registry_sequence": jsonU64(math.MaxUint64), "value_hex": "0100000000000000"}
	t, err = rec.prepare("MAP_LOOKUP_ELEM", lookup, nil, nil, inputs, lookupMaximum)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual = append([]byte(nil), lookup...)
	guardPut32(actual, 0, uint32(m.fd))
	var observed uint64
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&key))))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&observed))))
	result, _, errno = unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_LOOKUP_ELEM, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(key)
	runtime.KeepAlive(observed)
	signed = int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var lookupOutput any
	if errno == 0 {
		var returned [8]byte
		guardPut64(returned[:], 0, observed)
		lookupOutput = map[string]any{"registry_sequence": jsonU64(registrySeq), "value_hex": fmt.Sprintf("%x", returned[:])}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), lookupOutput); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: guard liveness readback: %w", errno)
	}
	if result != 0 || observed != 1 {
		return errGuardProtocol
	}
	freeze := make([]byte, 4)
	t, err = rec.prepare("MAP_FREEZE", freeze, nil, nil, inputs, output)
	if err != nil {
		return err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return err
	}
	actual = append([]byte(nil), freeze...)
	guardPut32(actual, 0, uint32(m.fd))
	result, _, errno = unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_FREEZE, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed = int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	var freezeOutput any
	if errno == 0 {
		freezeOutput = output
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err = rec.transition(t, state, jsonI64(signed), freezeOutput); err != nil {
			return err
		}
	}
	if errno != 0 {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: guard liveness freeze: %w", errno)
	}
	if result != 0 || registrySeq == 0 {
		return errGuardProtocol
	}
	return nil
}
