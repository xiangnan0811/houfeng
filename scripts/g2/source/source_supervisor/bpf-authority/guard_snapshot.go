//go:build linux && amd64 && !cgo

package main

import "encoding/hex"

// objectSnapshot decodes the values copied by the guard at publication. The
// native object is never dereferenced from userspace after that callback.
func (r guardRecord) objectSnapshot() (map[string]any, error) {
	b := r.Snapshot[:]
	if r.Kind != 1 && r.Kind != 3 {
		if !guardZero(b) {
			return nil, errGuardProtocol
		}
		return nil, nil
	}
	if guardU32(b, 0) == 0 || guardU32(b, 4) != 0 {
		return nil, errGuardProtocol
	}
	name, err := kernelObjectName(b[8:24])
	if err != nil {
		return nil, err
	}
	var attributes map[string]any
	if r.Kind == 1 {
		if !guardZero(b[56:64]) {
			return nil, errGuardProtocol
		}
		attributes = map[string]any{
			"name": name, "tag": hex.EncodeToString(b[24:32]),
			"load_time":             jsonU64(guardU64(b, 32)),
			"created_by_uid":        jsonU64(uint64(guardU32(b, 40))),
			"translated_insn_count": jsonU64(uint64(guardU32(b, 44))),
			"verified_insns":        jsonU64(uint64(guardU32(b, 48))),
			"attach_btf_id":         jsonU64(uint64(guardU32(b, 52))),
		}
	} else {
		if guardU32(b, 52) != 0 || (guardU32(b, 40) == 0 && (guardU32(b, 44) != 0 || guardU32(b, 48) != 0)) {
			return nil, errGuardProtocol
		}
		attributes = map[string]any{
			"name": name, "key_size": jsonU64(uint64(guardU32(b, 24))),
			"value_size":        jsonU64(uint64(guardU32(b, 28))),
			"max_entries":       jsonU64(uint64(guardU32(b, 32))),
			"map_flags":         jsonU64(uint64(guardU32(b, 36))),
			"btf_id":            jsonU64(uint64(guardU32(b, 40))),
			"btf_key_type_id":   jsonU64(uint64(guardU32(b, 44))),
			"btf_value_type_id": jsonU64(uint64(guardU32(b, 48))),
			"map_extra":         jsonU64(guardU64(b, 56)),
		}
	}
	return map[string]any{"version": jsonU64(1), "type": jsonU64(uint64(guardU32(b, 0))), "attributes": attributes}, nil
}
