//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"math"
	"strconv"
)

const maxAuthorityTranscriptBytes = 16777216

func jsonU64(n uint64) json.Number { return json.Number(strconv.FormatUint(n, 10)) }
func jsonI64(n int64) json.Number  { return json.Number(strconv.FormatInt(n, 10)) }

func (r guardRecord) projection() (map[string]any, error) {
	if r.Sequence == 0 || r.Kind < 1 || r.Kind > 16 || r.Flags & ^uint32(7) != 0 {
		return nil, errGuardProtocol
	}
	snapshot, err := r.objectSnapshot()
	if err != nil {
		return nil, err
	}
	var snapshotValue any
	if snapshot != nil {
		snapshotValue = snapshot
	}
	var detail any
	if r.Kind < 8 {
		if !validLifecycleDetail(r.Kind, r.Detail[:]) || r.Flags&1 != 0 || (r.Kind == 7 && r.AuditSerial != 0) {
			return nil, errGuardProtocol
		}
		if r.Kind == 2 || r.Kind == 4 || r.Kind == 5 || r.Kind == 6 {
			detail = map[string]any{"observed_mono_ns": jsonU64(guardU64(r.Detail[:], 0))}
		}
	} else if r.Kind == 8 {
		if _, err := r.auditKey(); err != nil {
			return nil, err
		}
		detail = map[string]any{"seconds": jsonU64(guardU64(r.Detail[:], 0)), "nanoseconds": jsonU64(uint64(guardU32(r.Detail[:], 8)))}
	} else if r.Kind >= 9 && r.Kind <= 12 {
		if r.Flags != 0 || r.ObjectCookie != 0 || r.KernelID != 0 || r.RootCookie != 0 || r.CgroupID != 0 || r.AuditSerial != 0 || r.Operation != 0 || r.Result != 0 || guardU32(r.Detail[:], 36) != 0 {
			return nil, errGuardProtocol
		}
		exitCode := int32(guardU32(r.Detail[:], 32))
		if r.Kind != 11 && exitCode != 0 {
			return nil, errGuardProtocol
		}
		detail = map[string]any{
			"tgid":            jsonU64(uint64(guardU32(r.Detail[:], 0))),
			"tid":             jsonU64(uint64(guardU32(r.Detail[:], 4))),
			"exe_dev":         jsonU64(guardU64(r.Detail[:], 8)),
			"exe_ino":         jsonU64(guardU64(r.Detail[:], 16)),
			"exec_generation": jsonU64(guardU64(r.Detail[:], 24)),
			"exit_code":       jsonI64(int64(exitCode)),
		}
	} else if r.Kind == 13 {
		if r.Flags != 0 || r.ObjectCookie != 0 || r.KernelID != 0 || r.RootCookie != 0 || r.CgroupID != 0 || r.ParentCookie != 0 || r.TaskCookie == 0 || r.TaskStartNS == 0 || r.AuditSerial == 0 || r.AuditSerial > math.MaxUint32 || guardU64(r.Detail[:], 8) > math.MaxInt64 || guardU32(r.Detail[:], 16) >= 1000000000 || guardU32(r.Detail[:], 20) != 0 {
			return nil, errGuardProtocol
		}
		detail = map[string]any{
			"tgid":        jsonU64(uint64(guardU32(r.Detail[:], 0))),
			"tid":         jsonU64(uint64(guardU32(r.Detail[:], 4))),
			"seconds":     jsonU64(guardU64(r.Detail[:], 8)),
			"nanoseconds": jsonU64(uint64(guardU32(r.Detail[:], 16))),
			"exe_dev":     jsonU64(guardU64(r.Detail[:], 24)),
			"exe_ino":     jsonU64(guardU64(r.Detail[:], 32)),
		}
	} else if r.Kind == 14 {
		if r.Flags != 0 || r.ObjectCookie != 0 || r.KernelID != 0 || r.RootCookie != 0 || r.CgroupID != 0 || r.AuditSerial != 0 || r.Result != 0 || r.ParentCookie != 0 || r.TaskCookie == 0 || r.TaskStartNS == 0 || (r.Operation != 2 && r.Operation != 0) || !guardZero(r.Detail[:]) {
			return nil, errGuardProtocol
		}
	} else if r.Kind == 15 {
		if r.Flags != 0 || r.ObjectCookie != 0 || r.KernelID != 0 || r.RootCookie != 0 || r.CgroupID != 0 || r.AuditSerial != 0 || r.Operation != 0 || r.ParentCookie != 0 || r.TaskCookie == 0 || r.TaskStartNS == 0 || guardU32(r.Detail[:], 36) != 0 {
			return nil, errGuardProtocol
		}
		exitCode := int32(guardU32(r.Detail[:], 32))
		if exitCode != r.Result {
			return nil, errGuardProtocol
		}
		detail = map[string]any{
			"tgid":            jsonU64(uint64(guardU32(r.Detail[:], 0))),
			"tid":             jsonU64(uint64(guardU32(r.Detail[:], 4))),
			"exe_dev":         jsonU64(guardU64(r.Detail[:], 8)),
			"exe_ino":         jsonU64(guardU64(r.Detail[:], 16)),
			"exec_generation": jsonU64(guardU64(r.Detail[:], 24)),
			"exit_code":       jsonI64(int64(exitCode)),
		}
	} else if r.Kind == 16 {
		if err := r.validateIDCursor(); err != nil {
			return nil, err
		}
		detail = map[string]any{
			"first_removal_sequence": jsonU64(guardU64(r.Detail[:], 0)),
			"transaction_id":         jsonU64(guardU64(r.Detail[:], 8)),
			"saved_cursor":           jsonU64(uint64(guardU32(r.Detail[:], 16))),
			"requested_id":           jsonU64(uint64(guardU32(r.Detail[:], 20))),
			"allocator_result":       jsonI64(int64(int32(guardU32(r.Detail[:], 24)))),
			"cursor_after":           jsonU64(uint64(guardU32(r.Detail[:], 28))),
		}
	}
	return map[string]any{
		"sequence": jsonU64(r.Sequence), "kind": jsonU64(uint64(r.Kind)), "flags": jsonU64(uint64(r.Flags)),
		"object_cookie": jsonU64(r.ObjectCookie), "kernel_id": jsonU64(r.KernelID),
		"root_cookie": jsonU64(r.RootCookie), "cgroup_id": jsonU64(r.CgroupID),
		"task_cookie": jsonU64(r.TaskCookie), "task_start_ns": jsonU64(r.TaskStartNS),
		"audit_serial": jsonU64(r.AuditSerial), "operation": jsonU64(uint64(r.Operation)),
		"result": jsonI64(int64(r.Result)), "parent_cookie": jsonU64(r.ParentCookie), "detail": detail, "object_snapshot": snapshotValue,
	}, nil
}

type transcriptEntry struct {
	record map[string]any
	raw    []byte
	hash   string
}
type authorityTranscript struct {
	bootID, epoch string
	entries       []transcriptEntry
	tail          string
	total         int
}

func newAuthorityTranscript(bootID, epoch string) *authorityTranscript {
	return &authorityTranscript{bootID: bootID, epoch: epoch, tail: "0000000000000000000000000000000000000000000000000000000000000000"}
}

func (t *authorityTranscript) append(kind string, body map[string]any) (uint64, string, error) {
	if t == nil || kind == "" || body == nil || uint64(len(t.entries)) == math.MaxUint64 {
		return 0, "", errControlFrame
	}
	switch kind {
	case "principal", "grant", "invocation", "daemon", "transaction", "object", "attachment", "audit", "audit_wire", "audit_control", "audit_readback", "audit_cut", "checkpoint", "guard", "registry", "selftest_fd_stage", "selftest_runtime_stage", "selftest_case", "selftest_guard_status", "selftest_audit_reorder", "control_rejection", "probe_actor", "probe_import", "module_instance", "exec_admission", "endpoint_transition", "notification_denial", "notification_cancel", "notification_response", "cgroup_enrollment", "held_file", "grant_object_cleanup", "runtime_exec", "runtime_root", "containerd_launcher":
	default:
		return 0, "", errControlFrame
	}
	sequence := uint64(len(t.entries)) + 1
	record := map[string]any{"version": json.Number("1"), "boot_id": t.bootID, "epoch": t.epoch, "sequence": jsonU64(sequence), "previous": t.tail, "kind": kind, "body": body}
	raw, err := canonicalControl(record)
	if err != nil {
		return 0, "", err
	}
	// Full transcript export is bounded by the existing result budget; it is
	// never truncated or reduced to a tail digest to make an export fit.
	if len(raw)+1 > maxAuthorityTranscriptBytes-t.total {
		return 0, "", errControlFrame
	}
	hash, err := transcriptHash(record)
	if err != nil {
		return 0, "", err
	}
	t.entries = append(t.entries, transcriptEntry{record: record, raw: raw, hash: hash})
	t.total += len(raw) + 1
	t.tail = hash
	return sequence, hash, nil
}

// recordsAfter returns only whole records; the caller must also encode the ACK
// envelope and check the same 16KiB frame bound before sending it.
func (t *authorityTranscript) recordsAfter(after uint64, max uint32) ([]any, uint64, bool, error) {
	if t == nil || max == 0 || max > 64 || after > uint64(len(t.entries)) {
		return nil, 0, false, errControlFrame
	}
	records := make([]any, 0, max)
	next := after
	for next < uint64(len(t.entries)) && len(records) < int(max) {
		candidate := append(records, t.entries[next].record)
		if _, err := canonicalControl(candidate); err != nil {
			if len(records) == 0 {
				return nil, 0, false, err
			}
			break
		}
		records = candidate
		next++
	}
	return records, next, next == uint64(len(t.entries)), nil
}
