//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

const auditGetType = 1000
const auditListRulesType = 1013

var errAuditMissingDone = fmt.Errorf("%w: audit LIST_RULES missing NLMSG_DONE", errGuardProtocol)

// audit_rule_data ends with a variable-length buffer; the fixed no-key rules
// happen to have a zero-length buffer, but framing is checked independently.
func auditRuleFramed(raw []byte) bool {
	const base = 1040
	return len(raw) >= base && uint64(binary.LittleEndian.Uint32(raw[base-4:base])) == uint64(len(raw)-base)
}

// fixedAuditRule produces the exact ADD_RULE input, including prepend.
func fixedAuditRule(arch uint32, calls []uint32) []byte {
	b := make([]byte, 1040)
	binary.LittleEndian.PutUint32(b, 0x14)
	binary.LittleEndian.PutUint32(b[4:], 2)
	binary.LittleEndian.PutUint32(b[8:], 1)
	for _, n := range calls {
		off := 12 + int(n/32)*4
		binary.LittleEndian.PutUint32(b[off:], binary.LittleEndian.Uint32(b[off:])|1<<(n%32))
	}
	binary.LittleEndian.PutUint32(b[268:], 11)
	binary.LittleEndian.PutUint32(b[524:], arch)
	binary.LittleEndian.PutUint32(b[780:], 0x40000000)
	return b
}

// LIST_RULES returns the filter without the insertion-only prepend flag.
func auditFixedRule(arch uint32, calls ...uint32) []byte {
	b := fixedAuditRule(arch, calls)
	binary.LittleEndian.PutUint32(b, 4)
	return b
}

func auditRuleSet(final bool) [][]byte {
	rules := [][]byte{auditFixedRule(auditArchX8664, 321), auditFixedRule(auditArchI386, 357)}
	if final {
		rules = append(rules, auditFixedRule(auditArchX8664, 49, 59, 322), auditFixedRule(auditArchI386, 11, 102, 358, 361))
	}
	return rules
}

func auditNumber32(v any) (uint32, bool) {
	n, ok := exactNumber(v)
	return uint32(n), ok && n <= math.MaxUint32
}

// validateAuditQueries re-parses all reader-provided raw kernel frames. The
// reader authenticates their netlink sender; the broker validates every field,
// framing relation, and the two GETs independently.
func validateAuditQueries(value any, check string) (observedAuditStatus, error) {
	return validateAuditQueriesPhase(value, check, false)
}

// PID1 uses this before installing any rule; the authenticated reader itself
// is started only after the two BPF rules have been installed.
func validateInitialAuditQueries(value any) (observedAuditStatus, error) {
	status, err := validateAuditQueriesPhase(value, "configuration", true)
	if err != nil {
		return observedAuditStatus{}, err
	}
	queries := value.([]any)
	rows := queries[1].(map[string]any)["replies"].([]any)
	if len(rows) != 1 || status.pid != 0 || status.lost != 0 {
		return observedAuditStatus{}, errGuardProtocol
	}
	return status, nil
}

func validateAuditQueriesPhase(value any, check string, allowEmpty bool) (observedAuditStatus, error) {
	var empty observedAuditStatus
	queries, ok := value.([]any)
	if !ok {
		return empty, errGuardProtocol
	}
	expected := []uint32{auditGetType}
	if check == "configuration" {
		expected = []uint32{auditGetType, auditListRulesType, auditGetType}
	} else if check != "status" {
		return empty, errGuardProtocol
	}
	if len(queries) != len(expected) {
		return empty, errGuardProtocol
	}
	var first observedAuditStatus
	var previousSeq, port uint32
	for i, qv := range queries {
		q, ok := qv.(map[string]any)
		if !ok || !exactKeys(q, "request_type", "request_sequence", "request_portid", "replies") {
			return empty, errGuardProtocol
		}
		typ, a := auditNumber32(q["request_type"])
		seq, b := auditNumber32(q["request_sequence"])
		qp, c := auditNumber32(q["request_portid"])
		if !a || !b || !c || typ != expected[i] || seq == 0 || qp == 0 || i > 0 && (seq <= previousSeq || qp != port) {
			return empty, errGuardProtocol
		}
		previousSeq, port = seq, qp
		replies, ok := q["replies"].([]any)
		if !ok || len(replies) == 0 || len(replies) > 5 {
			return empty, errGuardProtocol
		}
		if typ == auditGetType && len(replies) != 1 {
			return empty, errGuardProtocol
		}
		done := false
		seenRules := make(map[string]bool)
		for j, rv := range replies {
			row, ok := rv.(map[string]any)
			if !ok || !exactKeys(row, "source_pid", "source_groups", "length", "type", "flags", "sequence", "header_pid", "payload_hex") {
				return empty, errGuardProtocol
			}
			sp, a := auditNumber32(row["source_pid"])
			sg, b := auditNumber32(row["source_groups"])
			length, c := auditNumber32(row["length"])
			rt, d := auditNumber32(row["type"])
			flags, e := auditNumber32(row["flags"])
			rs, f := auditNumber32(row["sequence"])
			hp, g := auditNumber32(row["header_pid"])
			text, h := row["payload_hex"].(string)
			payload, decErr := hex.DecodeString(text)
			if !a || !b || !c || !d || !e || !f || !g || !h || decErr != nil || hex.EncodeToString(payload) != text || sp != 0 || sg != 0 || hp != 0 || rs != seq || length != uint32(16+len(payload)) || flags > math.MaxUint16 || flags&unix.NLM_F_DUMP_INTR != 0 || done {
				return empty, errGuardProtocol
			}
			if typ == auditGetType {
				if rt != auditGetType || flags != 0 || len(payload) != 44 {
					return empty, errGuardProtocol
				}
				status := observedAuditStatus{mask: guardU32(payload, 0), enabled: guardU32(payload, 4), failure: guardU32(payload, 8), pid: guardU32(payload, 12), rate: guardU32(payload, 16), backlogLimit: guardU32(payload, 20), lost: guardU32(payload, 24), featureBitmap: guardU32(payload, 32), backlogWaitTime: guardU32(payload, 36)}
				if i == 0 {
					first = status
				} else if status != first {
					return empty, errGuardProtocol
				}
			} else if rt == unix.NLMSG_DONE {
				if j != len(replies)-1 || flags != unix.NLM_F_MULTI || len(payload) != 0 && !(len(payload) == 4 && guardU32(payload, 0) == 0) {
					return empty, errGuardProtocol
				}
				done = true
			} else {
				if rt != auditListRulesType || flags != unix.NLM_F_MULTI || !auditRuleFramed(payload) || seenRules[text] {
					return empty, errGuardProtocol
				}
				found := false
				for _, rule := range auditRuleSet(true) {
					if bytes.Equal(payload, rule) {
						found = true
						break
					}
				}
				if !found {
					return empty, errGuardProtocol
				}
				seenRules[text] = true
			}
		}
		if typ == auditListRulesType && len(seenRules) != 2 && len(seenRules) != 4 && !(allowEmpty && len(seenRules) == 0) {
			return empty, errGuardProtocol
		}
		if typ == auditListRulesType && !done {
			return empty, errAuditMissingDone
		}
	}
	return first, nil
}

// validateAuditConfiguration binds configuration to the caller's actual phase;
// neither a rule count nor a cached earlier check is sufficient.
func validateAuditConfiguration(body map[string]any, final bool, owner uint32) (observedAuditStatus, error) {
	var empty observedAuditStatus
	if body == nil || body["check"] != "configuration" {
		return empty, errGuardProtocol
	}
	status, err := validateAuditQueries(body["queries"], "configuration")
	if err != nil {
		return empty, err
	}
	enabled := uint32(1)
	if final {
		enabled = 2
	}
	if status.enabled != enabled || status.failure != 1 || status.pid != owner || status.rate != 0 || status.backlogLimit != 16384 || status.lost != 0 {
		return empty, errGuardProtocol
	}
	queries := body["queries"].([]any)
	rows := queries[1].(map[string]any)["replies"].([]any)
	wanted := auditRuleSet(final)
	if len(rows) != len(wanted)+1 {
		return empty, errGuardProtocol
	}
	for _, rule := range wanted {
		found := false
		for _, row := range rows[:len(rows)-1] {
			if row.(map[string]any)["payload_hex"] == hex.EncodeToString(rule) {
				found = true
			}
		}
		if !found {
			return empty, errGuardProtocol
		}
	}
	return status, nil
}
