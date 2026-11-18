//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
)

type pendingBootstrapEntry struct {
	kind string
	body map[string]any
}
type bootstrapRecorder struct {
	bootID       string
	executor     uint64
	nextID       uint64
	baseSequence uint64
	entries      []pendingBootstrapEntry
	guardRecords []guardRecord
	bytes        int
}
type bootstrapTransaction struct {
	id        uint64
	command   string
	attribute []byte
	program   any
	license   any
	inputs    []any
	cleanup   map[string]any
	state     string
}

func (r *bootstrapRecorder) preflight(kind string, body map[string]any) error {
	if r == nil || r.bootID == "" || r.executor == 0 || body == nil {
		return errGuardProtocol
	}
	// Sequence and principal use their maximum encoded length, while epoch and
	// previous have their fixed sizes. This reserves room for the enclosing
	// READ_EVIDENCE ACK as well as the individual transcript record.
	record := map[string]any{"version": json.Number("1"), "boot_id": r.bootID, "epoch": strings.Repeat("f", 32), "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": kind, "body": body}
	encoded, err := canonicalControl(record)
	if err != nil || len(encoded)+1 > maxAuthorityTranscriptBytes-r.bytes {
		return errControlFrame
	}
	ack := map[string]any{"version": json.Number("1"), "boot_id": r.bootID, "epoch": strings.Repeat("f", 32), "run_token": authorityRunToken, "principal_id": jsonU64(math.MaxUint64), "request_id": jsonU64(math.MaxUint64), "request_sha256": strings.Repeat("f", 64), "status": "OK", "body": map[string]any{"records": []any{record}, "next": jsonU64(math.MaxUint64), "complete": false}}
	if _, err = canonicalControl(ack); err != nil {
		return errControlFrame
	}
	return nil
}

func (r *bootstrapRecorder) add(kind string, body map[string]any) error {
	if err := r.preflight(kind, body); err != nil {
		return err
	}
	raw, err := canonicalControl(body)
	if err != nil {
		return err
	}
	// Freeze the pending value through canonical decoding: callers cannot
	// mutate the original map after its state has been recorded.
	frozen, err := parseCanonicalControl(raw)
	if err != nil {
		return err
	}
	record := map[string]any{"version": json.Number("1"), "boot_id": r.bootID, "epoch": strings.Repeat("f", 32), "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": kind, "body": frozen}
	full, err := canonicalControl(record)
	if err != nil {
		return err
	}
	r.entries = append(r.entries, pendingBootstrapEntry{kind: kind, body: frozen})
	r.bytes += len(full) + 1
	return nil
}

func bootstrapCleanup() map[string]any {
	return map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}
}

func (r *bootstrapRecorder) body(t *bootstrapTransaction, state string, result, output any) map[string]any {
	return map[string]any{
		"id": jsonU64(t.id), "grant_id": nil, "invocation_id": nil, "requester_cookie": nil, "notification_id": nil,
		"command": t.command, "attributes": map[string]any{"attr_size": jsonU64(uint64(len(t.attribute))), "attr_hex": hex.EncodeToString(t.attribute)},
		"program_bytes": t.program, "license": t.license, "input_objects": t.inputs, "executor_cookie": jsonU64(r.executor),
		"state": state, "result": result, "returned_fd": nil, "output": output, "cleanup": t.cleanup,
	}
}

func (r *bootstrapRecorder) prepare(command string, normalized []byte, program, license any, inputs []any, possibleOutput any) (*bootstrapTransaction, error) {
	if r == nil || command == "" || len(normalized) == 0 || len(normalized) > 4096 || inputs == nil {
		return nil, errGuardProtocol
	}
	if r.nextID == 0 {
		r.nextID = 1
	}
	if r.nextID == math.MaxUint64 {
		return nil, errGuardProtocol
	}
	t := &bootstrapTransaction{id: r.nextID, command: command, attribute: append([]byte(nil), normalized...), program: program, license: license, inputs: inputs, state: "prepared", cleanup: bootstrapCleanup()}
	// Effects whose maximum output is known must be representable before the
	// syscall, including the largest possible descriptor/result encoding.
	if err := r.preflight("transaction", r.body(t, "effect_observed", jsonI64(math.MaxInt64), possibleOutput)); err != nil {
		return nil, err
	}
	if err := r.add("transaction", r.body(t, "prepared", nil, nil)); err != nil {
		return nil, err
	}
	r.nextID++
	return t, nil
}

func (r *bootstrapRecorder) transition(t *bootstrapTransaction, state string, result, output any) error {
	if t == nil {
		return errGuardProtocol
	}
	valid := t.state == "prepared" && state == "committed" || t.state == "committed" && state == "effect_observed" || t.state == "effect_observed" && state == "response_settled" || t.state == "response_settled" && state == "retired"
	if !valid || (state == "committed" && (result != nil || output != nil)) || (state != "committed" && result == nil) {
		return errGuardProtocol
	}
	if err := r.add("transaction", r.body(t, state, result, output)); err != nil {
		return err
	}
	t.state = state
	return nil
}

func (r *bootstrapRecorder) publish(t *authorityTranscript) error {
	if r == nil || t == nil || t.bootID != r.bootID || r.baseSequence != 1 || uint64(len(t.entries)) != r.baseSequence {
		return errGuardProtocol
	}
	for _, entry := range r.entries {
		if _, _, err := t.append(entry.kind, entry.body); err != nil {
			return err
		}
	}
	r.entries = nil
	return nil
}
