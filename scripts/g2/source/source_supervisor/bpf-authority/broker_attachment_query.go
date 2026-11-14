//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func attachedProgramQueryBody(id, enrollment, heldFD, executor uint64, attr []byte, state string, result, output any) map[string]any {
	inputs := []any{map[string]any{"field": "query.target_fd", "kind": "cgroup", "cookie": jsonU64(enrollment), "requester_fd": jsonU64(heldFD)}}
	return map[string]any{"id": jsonU64(id), "grant_id": nil, "invocation_id": nil, "requester_cookie": nil, "notification_id": nil, "command": "PROG_QUERY", "attributes": map[string]any{"attr_size": jsonU64(uint64(len(attr))), "attr_hex": hex.EncodeToString(attr)}, "program_bytes": nil, "license": nil, "input_objects": inputs, "executor_cookie": jsonU64(executor), "state": state, "result": result, "returned_fd": nil, "output": output, "cleanup": bootstrapCleanup()}
}

// observeAttachedProgram queries the retained enrolled cgroup after the actual
// attachment. Its independently audited syscall has no requester notification.
func (b *authorityBroker) observeAttachedProgram(inv *runtimeInvocation, group *liveCgroup, program *objectIncarnation) error {
	if b == nil || b.effects == nil || b.resolver == nil || b.checkpoint == nil || inv == nil || group == nil || program == nil || group.Retired || group.HeldFD < 0 || group.GrantID != inv.grantID || program.id == 0 || program.id > math.MaxUint32 || b.effects.objectGrant[program.cookie] != inv.grantID || b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 {
		return errGuardProtocol
	}
	id := b.effects.nextTransaction
	b.effects.nextTransaction++
	attr := make([]byte, 40)
	guardPut32(attr, 4, unix.BPF_CGROUP_DEVICE)
	guardPut32(attr, 24, 64)
	body := func(state string, result, output any) map[string]any {
		return attachedProgramQueryBody(id, group.Enrollment, uint64(group.HeldFD), b.resolver.brokerCookie, attr, state, result, output)
	}
	idsMax, flagsMax := make([]any, 64), make([]any, 64)
	for i := range idsMax {
		idsMax[i], flagsMax[i] = jsonU64(math.MaxUint32), jsonU64(math.MaxUint32)
	}
	maximum := map[string]any{"attach_flags": jsonU64(math.MaxUint32), "program_ids": idsMax, "program_attach_flags": flagsMax}
	if err := preflightRuntimeRecord(b, "transaction", body("effect_observed", jsonI64(math.MinInt64), maximum)); err != nil {
		return err
	}
	for _, state := range []string{"prepared", "committed"} {
		if err := appendRuntimeRecord(b, "transaction", body(state, nil, nil)); err != nil {
			return err
		}
	}
	b.checkpoint.activeTransactions++
	actual := append([]byte(nil), attr...)
	ids, flags := make([]uint32, 64), make([]uint32, 64)
	// The pinned cgroup query copies each attachment's flags. Sentinel
	// initialization prevents an unwritten slot from looking like copyout.
	for i := range ids {
		ids[i], flags[i] = math.MaxUint32, math.MaxUint32
	}
	guardPut32(actual, 0, uint32(group.HeldFD))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&ids[0]))))
	guardPut64(actual, 32, uint64(uintptr(unsafe.Pointer(&flags[0]))))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_QUERY, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(ids)
	runtime.KeepAlive(flags)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "PROG_QUERY", Number: unix.BPF_PROG_QUERY, Result: signed}); err != nil {
		return err
	}
	var output any
	count := guardU32(actual, 24)
	if errno == 0 || errno == unix.ENOSPC {
		written := count
		if written > 64 {
			written = 64
		}
		outIDs, outFlags := make([]any, written), make([]any, written)
		for i := range outIDs {
			outIDs[i], outFlags[i] = jsonU64(uint64(ids[i])), jsonU64(uint64(flags[i]))
		}
		output = map[string]any{"attach_flags": jsonU64(uint64(guardU32(actual, 12))), "program_ids": outIDs, "program_attach_flags": outFlags}
	}
	for _, state := range []string{"effect_observed", "response_settled", "retired"} {
		if err := appendRuntimeRecord(b, "transaction", body(state, jsonI64(signed), output)); err != nil {
			return err
		}
	}
	b.checkpoint.activeTransactions--
	if errno != 0 || signed != 0 || count != 1 || ids[0] != uint32(program.id) || flags[0] != unix.BPF_F_ALLOW_MULTI {
		return errGuardProtocol
	}
	return b.confirmGrantQuery(inv, group, queryOutput{Executed: true, Result: signed, AttachFlags: guardU32(actual, 12), ProgramIDs: ids[:1], ProgramAttachFlags: flags[:1], Count: count})
}
