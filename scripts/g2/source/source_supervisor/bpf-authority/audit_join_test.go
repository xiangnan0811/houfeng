//go:build linux && amd64 && !cgo

package main

import (
	"reflect"
	"strings"
	"testing"
)

func rawJoinFixture(t *testing.T) (guardRecord, auditFact, auditFact) {
	t.Helper()
	syscall, err := parseAuditFact(auditSyscallRecord, []byte("audit(42.123:17): arch=c000003e syscall=321 success=no exit=-1 a0=1 pid=123 ppid=12"))
	if err != nil {
		t.Fatal(err)
	}
	eoe, err := parseAuditFact(auditEOERecord, []byte("audit(42.123:17):"))
	if err != nil {
		t.Fatal(err)
	}
	r := guardRecord{Sequence: 1, Kind: 13, TaskCookie: 101, TaskStartNS: 999, AuditSerial: 17, Operation: 1, Result: -1}
	guardPut32(r.Detail[:], 0, 123)
	guardPut32(r.Detail[:], 4, 124) // Deliberately not the audit TGID.
	guardPut64(r.Detail[:], 8, 42)
	guardPut32(r.Detail[:], 16, 123456789)
	guardPut64(r.Detail[:], 24, 2)
	guardPut64(r.Detail[:], 32, 3)
	return r, syscall, eoe
}

func rawJoinObserver() *terminalObserver {
	return &terminalObserver{
		transcript:   newObserverTranscript("00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)),
		rawSyscalls:  make(map[auditEventKey]observerRawSyscallPending),
		rawSeen:      make(map[auditEventKey]struct{}),
		auditSerials: make(map[auditEventKey]*observerAuditSerialPending),
		wireCounts:   make(map[auditWireKey]uint64),
		armedByRole:  "REALM_ROOT",
	}
}

func rawJoinBroker(t *testing.T) *auditJoin {
	t.Helper()
	resolver, err := newBrokerAuditResolver(400, 99)
	if err != nil {
		t.Fatal(err)
	}
	return newAuditJoin(newAuthorityTranscript("00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)), resolver)
}

func rawAuditBodies(entries []observerRecordEntry) []map[string]any {
	var result []map[string]any
	for _, entry := range entries {
		if entry.record["kind"] == "audit" {
			result = append(result, entry.record["body"].(map[string]any))
		}
	}
	return result
}

func requireRawAudit(t *testing.T, body map[string]any, r guardRecord, syscall, eoe auditFact) {
	t.Helper()
	want := map[string]any{
		"tgid": jsonU64(syscall.PID), "syscall_payload_sha256": syscall.PayloadSHA256,
		"syscall_occurrence": jsonU64(1), "eoe_payload_sha256": eoe.PayloadSHA256,
		"eoe_occurrence": jsonU64(1),
	}
	if !reflect.DeepEqual(body["raw"], want) || body["task_cookie"] != jsonU64(r.TaskCookie) ||
		body["transaction_id"] != nil || body["command"] != "MAP_LOOKUP_ELEM" || body["result"] != jsonI64(-1) {
		t.Fatalf("raw audit does not bind actual facts: %#v", body)
	}
}

func requireRawWires(t *testing.T, records []map[string]any, syscall, eoe auditFact) {
	t.Helper()
	seen := make(map[uint16]int)
	typed := false
	for _, record := range records {
		if record["kind"] == "audit" {
			typed = true
			continue
		}
		if record["kind"] != "audit_wire" {
			continue
		}
		body := record["body"].(map[string]any)
		for _, fact := range []auditFact{syscall, eoe} {
			if body["record_type"] != jsonU64(uint64(fact.Kind)) {
				continue
			}
			if typed || !reflect.DeepEqual(body["stamp"], fact.Key.stamp()) || body["serial"] != jsonU64(fact.Serial) ||
				body["payload_sha256"] != fact.PayloadSHA256 || body["occurrence"] != jsonU64(1) {
				t.Fatalf("wrong or late wire reference: %#v", body)
			}
			seen[fact.Kind]++
		}
	}
	if !typed || seen[auditSyscallRecord] != 1 || seen[auditEOERecord] != 1 {
		t.Fatalf("typed audit lacks its exact wire occurrences: %#v", seen)
	}
}

func TestRawAuditEvidenceJoin(t *testing.T) {
	r, syscall, eoe := rawJoinFixture(t)
	t.Run("broker-delivery-reordered", func(t *testing.T) {
		j := rawJoinBroker(t)
		if err := j.observeFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := j.observeFact(eoe); err != nil {
			t.Fatal(err)
		}
		if err := j.drain(); err != nil || j.settled() {
			t.Fatalf("unmatched syscall must remain pending: settled=%v err=%v", j.settled(), err)
		}
		if err := j.observeGuard(r); err != nil {
			t.Fatal(err)
		}
		if err := j.drain(); err != nil || !j.settled() {
			t.Fatalf("complete raw join: settled=%v err=%v", j.settled(), err)
		}
		var bodies []map[string]any
		var records []map[string]any
		for _, entry := range j.transcript.entries {
			records = append(records, entry.record)
			if entry.record["kind"] == "audit" {
				bodies = append(bodies, entry.record["body"].(map[string]any))
			}
		}
		if len(bodies) != 1 {
			t.Fatalf("got %d typed audits", len(bodies))
		}
		requireRawAudit(t, bodies[0], r, syscall, eoe)
		requireRawWires(t, records, syscall, eoe)
	})
	t.Run("observer-delivery-reordered", func(t *testing.T) {
		o := rawJoinObserver()
		if err := o.observeAuditFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := o.observeAuditFact(eoe); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 0 {
			t.Fatal("published before guard observation")
		}
		o.observeRawGuard(r)
		o.joinRawSyscalls()
		bodies := rawAuditBodies(o.transcript.entries)
		if len(bodies) != 1 || o.finishOutcome() != "denial_proved" {
			t.Fatalf("observer join failed: audits=%d auditLost=%v", len(bodies), o.auditLost)
		}
		requireRawAudit(t, bodies[0], r, syscall, eoe)
		var records []map[string]any
		for _, entry := range o.transcript.entries {
			records = append(records, entry.record)
		}
		requireRawWires(t, records, syscall, eoe)
	})
}

func TestBrokerRawAuditRejectsIncompleteAndDuplicate(t *testing.T) {
	r, syscall, eoe := rawJoinFixture(t)
	t.Run("wrong-tgid", func(t *testing.T) {
		j := rawJoinBroker(t)
		guard := r
		guardPut32(guard.Detail[:], 0, 124)
		if err := j.observeGuard(guard); err != nil {
			t.Fatal(err)
		}
		if err := j.observeFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := j.observeFact(eoe); err != nil {
			t.Fatal(err)
		}
		if err := j.drain(); err == nil {
			t.Fatal("broker accepted audit TGID that differs from guard TGID")
		}
	})
	for _, mode := range []string{"missing-eoe", "wrong-stamp"} {
		t.Run(mode, func(t *testing.T) {
			j := rawJoinBroker(t)
			guard := r
			if mode == "wrong-stamp" {
				guardPut32(guard.Detail[:], 16, 124000000)
			}
			if err := j.observeGuard(guard); err != nil {
				t.Fatal(err)
			}
			if err := j.observeFact(syscall); err != nil {
				t.Fatal(err)
			}
			if mode != "missing-eoe" {
				if err := j.observeFact(eoe); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := j.auditCutReady(); err == nil || j.settled() {
				t.Fatal("incomplete raw join admitted an audit cut")
			}
		})
	}
	for _, mode := range []string{"guard", "syscall", "eoe"} {
		t.Run("duplicate-"+mode, func(t *testing.T) {
			j := rawJoinBroker(t)
			if err := j.observeGuard(r); err != nil {
				t.Fatal(err)
			}
			if mode == "guard" {
				if err := j.observeGuard(r); err == nil {
					t.Fatal("duplicate raw guard accepted")
				}
				return
			}
			if err := j.observeFact(syscall); err != nil {
				t.Fatal(err)
			}
			if mode == "syscall" {
				if err := j.observeFact(syscall); err == nil {
					t.Fatal("duplicate syscall accepted")
				}
				return
			}
			if err := j.observeFact(eoe); err != nil {
				t.Fatal(err)
			}
			if err := j.observeFact(eoe); err == nil {
				t.Fatal("duplicate EOE accepted")
			}
		})
	}
}

func TestRawAuditEvidenceRejectsAmbiguity(t *testing.T) {
	r, syscall, eoe := rawJoinFixture(t)
	for _, tc := range []struct {
		name  string
		alter func(*guardRecord, *auditFact, *auditFact)
	}{
		{"tgid", func(r *guardRecord, s, e *auditFact) { guardPut32(r.Detail[:], 0, 124) }},
		{"result", func(r *guardRecord, s, e *auditFact) { r.Result = -2 }},
		{"command", func(r *guardRecord, s, e *auditFact) { r.Operation = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, s, end := r, syscall, eoe
			tc.alter(&guard, &s, &end)
			o := rawJoinObserver()
			o.observeRawGuard(guard)
			if err := o.observeAuditFact(s); err != nil {
				t.Fatal(err)
			}
			if err := o.observeAuditFact(end); err != nil {
				t.Fatal(err)
			}
			o.joinRawSyscalls()
			if len(rawAuditBodies(o.transcript.entries)) != 0 || o.finishOutcome() != "closure_on_fault" {
				t.Fatal("mismatched guard and audit facts proved a denial")
			}
		})
	}
	t.Run("different-stamp", func(t *testing.T) {
		guard := r
		guardPut32(guard.Detail[:], 16, 124000000)
		o := rawJoinObserver()
		o.observeRawGuard(guard)
		if err := o.observeAuditFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := o.observeAuditFact(eoe); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 0 || o.finishOutcome() != "closure_on_fault" {
			t.Fatal("different audit stamp proved a denial")
		}
	})
	t.Run("eoe-other-stamp", func(t *testing.T) {
		other, err := parseAuditFact(auditEOERecord, []byte("audit(42.124:17):"))
		if err != nil {
			t.Fatal(err)
		}
		o := rawJoinObserver()
		o.observeRawGuard(r)
		if err := o.observeAuditFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := o.observeAuditFact(other); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 0 || o.finishOutcome() != "closure_on_fault" {
			t.Fatal("EOE from another stamp proved a denial")
		}
		j := rawJoinBroker(t)
		if err := j.observeGuard(r); err != nil {
			t.Fatal(err)
		}
		if err := j.observeFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := j.observeFact(other); err == nil {
			t.Fatal("broker accepted EOE from another stamp")
		}
	})
	t.Run("distinct-eoe-duplicate", func(t *testing.T) {
		other, err := parseAuditFact(auditEOERecord, []byte("audit(42.123:17): "))
		if err != nil || other.PayloadSHA256 == eoe.PayloadSHA256 {
			t.Fatalf("distinct EOE fixture failed: %v", err)
		}
		o := rawJoinObserver()
		o.observeRawGuard(r)
		for _, fact := range []auditFact{syscall, eoe, other} {
			if err := o.observeAuditFact(fact); err != nil {
				t.Fatal(err)
			}
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 0 || o.finishOutcome() != "closure_on_fault" {
			t.Fatal("distinct duplicate EOE proved a denial")
		}
	})
	for _, duplicate := range []string{"syscall", "eoe", "guard"} {
		t.Run("duplicate-"+duplicate, func(t *testing.T) {
			o := rawJoinObserver()
			o.observeRawGuard(r)
			if duplicate == "guard" {
				o.observeRawGuard(r)
			}
			if err := o.observeAuditFact(syscall); err != nil {
				t.Fatal(err)
			}
			if duplicate == "syscall" {
				if err := o.observeAuditFact(syscall); err != nil {
					t.Fatal(err)
				}
			}
			if err := o.observeAuditFact(eoe); err != nil {
				t.Fatal(err)
			}
			if duplicate == "eoe" {
				if err := o.observeAuditFact(eoe); err != nil {
					t.Fatal(err)
				}
			}
			o.joinRawSyscalls()
			if o.finishOutcome() != "closure_on_fault" {
				t.Fatal("duplicate required fact proved a denial")
			}
			if duplicate != "guard" && len(rawAuditBodies(o.transcript.entries)) != 0 {
				t.Fatal("ambiguous audit facts published a typed join")
			}
		})
	}
	t.Run("late-duplicate", func(t *testing.T) {
		o := rawJoinObserver()
		o.observeRawGuard(r)
		if err := o.observeAuditFact(syscall); err != nil {
			t.Fatal(err)
		}
		if err := o.observeAuditFact(eoe); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if err := o.observeAuditFact(eoe); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 1 || o.finishOutcome() != "closure_on_fault" {
			t.Fatal("late duplicate accepted or published another typed join")
		}
	})
	t.Run("missing-eoe-and-early-outcome", func(t *testing.T) {
		o := rawJoinObserver()
		o.observeRawGuard(r)
		if err := o.observeAuditFact(syscall); err != nil {
			t.Fatal(err)
		}
		o.joinRawSyscalls()
		if len(rawAuditBodies(o.transcript.entries)) != 0 || o.finishOutcome() != "closure_on_fault" {
			t.Fatal("missing EOE proved a denial")
		}
		o.armedByRole = "PID1"
		if o.finishOutcome() != "early_no_handoff" {
			t.Fatal("pending raw join changed the declared early outcome")
		}
	})
}

func TestOrdinaryAuditHasNullRawEvidence(t *testing.T) {
	_, syscall, eoe := rawJoinFixture(t)
	resolver, err := newBrokerAuditResolver(123, 101)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.recordTransaction(auditedTransaction{ID: 42, ExecutorCookie: 101, Command: "MAP_LOOKUP_ELEM", Number: 1, Result: -1}); err != nil {
		t.Fatal(err)
	}
	j := newAuditJoin(newAuthorityTranscript("00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)), resolver)
	if err := j.observeFact(syscall); err != nil {
		t.Fatal(err)
	}
	if err := j.observeFact(eoe); err != nil {
		t.Fatal(err)
	}
	if err := j.drain(); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	for _, entry := range j.transcript.entries {
		if entry.record["kind"] == "audit" {
			body = entry.record["body"].(map[string]any)
		}
	}
	raw, present := body["raw"]
	if body == nil || !present || raw != nil || body["transaction_id"] != jsonU64(42) {
		t.Fatalf("ordinary audit raw evidence = %#v", body)
	}
	bpf, err := parseAuditFact(auditBPFRecord, []byte("audit(42.123:18): prog-id=55 op=LOAD"))
	if err != nil {
		t.Fatal(err)
	}
	guard := guardRecord{Kind: 8, Flags: 1, ObjectCookie: 200, KernelID: 55, TaskCookie: 101}
	if err := j.appendBPF(bpf, guard, auditBinding{TaskCookie: 101, TransactionID: 42, Command: "PROG_LOAD"}); err != nil {
		t.Fatal(err)
	}
	body = j.transcript.entries[len(j.transcript.entries)-1].record["body"].(map[string]any)
	raw, present = body["raw"]
	if !present || raw != nil || body["record_type"] != "BPF" {
		t.Fatalf("ordinary BPF audit raw evidence = %#v", body)
	}
}
