//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAuditCapacityPolicy(t *testing.T) {
	t.Run("broker-distinct-and-occurrence", func(t *testing.T) {
		join := rawJoinBroker(t)
		join.fault = &observationFaultState{variant: "audit-overflow"}
		var first auditFact
		for i := range maxAuditWireIdentities {
			fact, err := parseAuditFact(auditSockaddrRecord, []byte(fmt.Sprintf("audit(42.123:%d): saddr=0200", i+1)))
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = fact
			}
			if err = join.observeFactOrdered(fact); err != nil {
				t.Fatalf("distinct wire %d: %v", i+1, err)
			}
		}
		wire := auditWireKey{first.Key, first.Kind, first.PayloadSHA256}
		if err := join.observeFactOrdered(first); err != nil || join.wireCounts[wire] != 2 || join.fault.detector != "" {
			t.Fatalf("existing wire at capacity: count=%d detector=%q err=%v", join.wireCounts[wire], join.fault.detector, err)
		}
		fact, err := parseAuditFact(auditSockaddrRecord, []byte(fmt.Sprintf("audit(42.123:%d): saddr=0200", maxAuditWireIdentities+1)))
		if err != nil {
			t.Fatal(err)
		}
		before := len(join.transcript.entries)
		if err = join.observeFactOrdered(fact); err == nil || join.fault.detector != "audit_wire_capacity" || join.fault.countBefore != maxAuditWireIdentities || join.fault.audit == nil || !reflect.DeepEqual(*join.fault.audit, fact) || len(join.transcript.entries) != before || len(join.wireCounts) != maxAuditWireIdentities {
			t.Fatalf("incorrect rejected fact or count: fault=%+v err=%v", join.fault, err)
		}
	})
	t.Run("broker-duplicate-at-capacity", func(t *testing.T) {
		join := rawJoinBroker(t)
		_, syscall, _ := rawJoinFixture(t)
		join.fault = &observationFaultState{variant: "audit-duplicate", actorPID: syscall.PID}
		if err := join.observeFactOrdered(syscall); err != nil {
			t.Fatal(err)
		}
		for i := range maxAuditWireIdentities - 1 {
			fact, err := parseAuditFact(auditSockaddrRecord, []byte(fmt.Sprintf("audit(42.123:%d): saddr=0200", i+1)))
			if err != nil {
				t.Fatal(err)
			}
			if err = join.observeFactOrdered(fact); err != nil {
				t.Fatal(err)
			}
		}
		wire := auditWireKey{syscall.Key, syscall.Kind, syscall.PayloadSHA256}
		err := join.observeFactOrdered(syscall)
		if err == nil || join.fault.detector != "audit_duplicate" || join.fault.audit == nil || !reflect.DeepEqual(*join.fault.audit, syscall) || join.wireCounts[wire] != 2 {
			t.Fatalf("duplicate at capacity: count=%d fault=%+v err=%v", join.wireCounts[wire], join.fault, err)
		}
	})

	t.Run("observer-completed-serials", func(t *testing.T) {
		o := rawJoinObserver()
		guard, _, _ := rawJoinFixture(t)
		// Inject raw matches directly to isolate cumulative audit serials from the
		// separately bounded native rawSeen set, which is intentionally unchanged.
		for i := 1; i <= maxObserverAuditWireIdentities/3; i++ {
			syscall, err := parseAuditFact(auditSyscallRecord, []byte(fmt.Sprintf("audit(42.123:%d): arch=c000003e syscall=321 success=no exit=-1 a0=1 pid=123 ppid=12", i)))
			if err != nil {
				t.Fatal(err)
			}
			sockaddr, err := parseAuditFact(auditSockaddrRecord, []byte(fmt.Sprintf("audit(42.123:%d): saddr=0200", i)))
			if err != nil {
				t.Fatal(err)
			}
			eoe, err := parseAuditFact(auditEOERecord, []byte(fmt.Sprintf("audit(42.123:%d):", i)))
			if err != nil {
				t.Fatal(err)
			}
			guard.AuditSerial = uint64(i)
			o.rawSyscalls[syscall.Key] = observerRawSyscallPending{record: guard}
			for _, fact := range []auditFact{syscall, sockaddr, eoe} {
				if err := o.observeAuditFact(fact); err != nil || o.auditLost || o.transcript.outputLoss {
					t.Fatalf("observer serial %d kind %d: %v", i, fact.Kind, err)
				}
			}
			o.joinRawSyscalls()
			if !o.auditSerials[syscall.Key].joined || o.auditLost || o.transcript.outputLoss {
				t.Fatalf("serial %d did not join without loss", i)
			}
		}
		if len(o.auditSerials) <= 4608 || len(o.wireCounts) != maxObserverAuditWireIdentities {
			t.Fatalf("wrong cumulative counts: serials=%d wires=%d", len(o.auditSerials), len(o.wireCounts))
		}
		extra, err := parseAuditFact(auditSockaddrRecord, []byte("audit(42.123:99999): saddr=0200"))
		if err != nil {
			t.Fatal(err)
		}
		if err = o.observeAuditFact(extra); err != nil || !o.auditLost || o.transcript.outputLoss || len(o.wireCounts) != maxObserverAuditWireIdentities {
			t.Fatalf("observer boundary: lost=%v outputLoss=%v err=%v", o.auditLost, o.transcript.outputLoss, err)
		}
	})
}

func TestTranscriptBudgetPolicy(t *testing.T) {
	const boot = "00000000-0000-0000-0000-000000000000"
	epoch := strings.Repeat("a", 32)
	body := map[string]any{"value": "test"}
	t.Run("authority-and-hash", func(t *testing.T) {
		probe := newAuthorityTranscript(boot, epoch)
		if _, _, err := probe.append("audit_wire", body); err != nil {
			t.Fatal(err)
		}
		entry := probe.entries[0]
		charge := len(entry.raw) + 1
		if probe.total != charge {
			t.Fatalf("charge %d want %d", probe.total, charge)
		}
		replayed, err := transcriptHash(entry.record)
		independent := sha256.Sum256(append([]byte("HOUFENG-G2-BPF-V1\x00"), entry.raw...))
		if err != nil || replayed != entry.hash || replayed != fmt.Sprintf("%x", independent) {
			t.Fatalf("hash replay: %s %v", replayed, err)
		}
		at := newAuthorityTranscript(boot, epoch)
		at.total = maxAuthorityTranscriptBytes - charge
		if _, _, err := at.append("audit_wire", body); err != nil || at.total != maxAuthorityTranscriptBytes {
			t.Fatalf("T boundary: total=%d err=%v", at.total, err)
		}
		over := newAuthorityTranscript(boot, epoch)
		over.total = maxAuthorityTranscriptBytes - charge + 1
		if _, _, err := over.append("audit_wire", body); !errors.Is(err, errControlFrame) || len(over.entries) != 0 {
			t.Fatalf("T+1 accepted: %v", err)
		}
	})
	t.Run("observer-and-hash", func(t *testing.T) {
		probe := newObserverTranscript(boot, epoch)
		if _, _, err := probe.append("audit_wire", body); err != nil {
			t.Fatal(err)
		}
		entry := probe.entries[0]
		charge := len(entry.raw) + 1
		replayed, err := observerTranscriptHash(entry.record)
		independent := sha256.Sum256(append([]byte("HOUFENG-G2-BPF-OBS-V1\x00"), entry.raw...))
		if err != nil || replayed != entry.hash || replayed != fmt.Sprintf("%x", independent) || probe.total != charge {
			t.Fatalf("observer hash or charge: %s %v", replayed, err)
		}
		at := newObserverTranscript(boot, epoch)
		at.total = maxAuthorityTranscriptBytes - charge
		if _, _, err := at.append("audit_wire", body); err != nil || at.total != maxAuthorityTranscriptBytes || at.outputLoss {
			t.Fatalf("observer T boundary: %v", err)
		}
		over := newObserverTranscript(boot, epoch)
		over.total = maxAuthorityTranscriptBytes - charge + 1
		if _, _, err := over.append("audit_wire", body); !errors.Is(err, errControlFrame) || !over.outputLoss || len(over.entries) != 0 {
			t.Fatalf("observer T+1: %v", err)
		}
	})
	t.Run("effect-preflights", func(t *testing.T) {
		record := map[string]any{"version": json.Number("1"), "boot_id": boot, "epoch": strings.Repeat("f", 32), "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": "transaction", "body": body}
		raw, err := canonicalControl(record)
		if err != nil {
			t.Fatal(err)
		}
		charge := len(raw) + 1
		r := &bootstrapRecorder{bootID: boot, executor: 1, bytes: maxAuthorityTranscriptBytes - charge}
		if err := r.add("transaction", body); err != nil || r.bytes != maxAuthorityTranscriptBytes {
			t.Fatalf("bootstrap T: bytes=%d err=%v", r.bytes, err)
		}
		r = &bootstrapRecorder{bootID: boot, executor: 1, bytes: maxAuthorityTranscriptBytes - charge + 1}
		if err := r.add("transaction", body); !errors.Is(err, errControlFrame) || len(r.entries) != 0 {
			t.Fatalf("bootstrap T+1: %v", err)
		}
		tr := newAuthorityTranscript(boot, strings.Repeat("f", 32))
		b := &authorityBroker{bootID: boot, epoch: tr.epoch, transcript: tr}
		tr.total = maxAuthorityTranscriptBytes - charge
		if err := preflightRuntimeRecord(b, "transaction", body); err != nil {
			t.Fatalf("runtime T: %v", err)
		}
		tr.total++
		if err := preflightRuntimeRecord(b, "transaction", body); !errors.Is(err, errControlFrame) || len(tr.entries) != 0 {
			t.Fatalf("runtime T+1: %v", err)
		}
		large := map[string]any{"value": strings.Repeat("x", 16000)}
		r = &bootstrapRecorder{bootID: boot, executor: 1}
		tr.total = 0
		if err := r.preflight("transaction", large); !errors.Is(err, errControlFrame) {
			t.Fatalf("oversized bootstrap ACK accepted: %v", err)
		}
		if err := preflightRuntimeRecord(b, "transaction", large); !errors.Is(err, errControlFrame) {
			t.Fatalf("oversized runtime ACK accepted: %v", err)
		}
	})
}

func TestEarlyResultBudgetPolicy(t *testing.T) {
	if authorityRunToken != "ddac5da9673f4c9ba7d2ad68570fd82d" {
		t.Fatal("unexpected authority run token")
	}
	caseSHA := strings.Repeat("a", 64)
	variant := "seal-guard-loaded"
	padding := strings.Repeat("x", 5*1024*1024)
	payload := map[string]any{"version": json.Number("1"), "run_token": authorityRunToken, "case_sha256": caseSHA, "seal_stage": variant, "init_handoff": false, "terminal_observation": map[string]any{"padding": padding}}
	var stream bytes.Buffer
	if err := writeEarlyResult(&stream, payload, caseSHA, variant); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(stream.Bytes(), []byte("\n"))
	if len(lines) < 4 || len(lines[len(lines)-1]) != 0 || !bytes.HasPrefix(lines[0], []byte("G2_BPF_EARLY_BEGIN ")) || !bytes.HasPrefix(lines[len(lines)-2], []byte("G2_BPF_EARLY_END ")) {
		t.Fatal("incomplete early stream")
	}
	var decoded bytes.Buffer
	for i, line := range lines[:len(lines)-1] {
		space := bytes.IndexByte(line, ' ')
		if space < 0 || len(line)-space > maxControlFrame {
			t.Fatalf("frame %d exceeds control bound", i)
		}
		frame, err := parseCanonicalControl(line[space+1:])
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && i < len(lines)-2 {
			if !bytes.HasPrefix(line, []byte("G2_BPF_EARLY_CHUNK ")) {
				t.Fatalf("unexpected chunk prefix %d", i)
			}
			data, ok := frame["data"].(string)
			part, err := base64.StdEncoding.DecodeString(data)
			if !ok || err != nil || len(part) > 8192 || frame["sequence"] != jsonU64(uint64(i-1)) {
				t.Fatalf("chunk %d: %v", i, err)
			}
			decoded.Write(part)
		}
	}
	expected := fmt.Sprintf("{\"case_sha256\":\"%s\",\"init_handoff\":false,\"run_token\":\"%s\",\"seal_stage\":\"%s\",\"terminal_observation\":{\"padding\":\"%s\"},\"version\":1}\n", caseSHA, authorityRunToken, variant, padding)
	if decoded.String() != expected {
		t.Fatal("decoded early payload differs")
	}
	sum := sha256.Sum256(decoded.Bytes())
	begin, err := parseCanonicalControl(lines[0][len("G2_BPF_EARLY_BEGIN "):])
	if err != nil || begin["sha256"] != fmt.Sprintf("%x", sum) || begin["bytes"] != jsonU64(uint64(decoded.Len())) {
		t.Fatalf("bad begin frame: %v", err)
	}
	end, err := parseCanonicalControl(lines[len(lines)-2][len("G2_BPF_EARLY_END "):])
	if err != nil || end["sha256"] != begin["sha256"] || end["bytes"] != begin["bytes"] || begin["chunks"] != jsonU64(uint64(len(lines)-3)) || end["chunks"] != begin["chunks"] {
		t.Fatalf("bad end frame: %v", err)
	}
	if path := os.Getenv("G2_EARLY_RESULT_FIXTURE_PATH"); path != "" {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		n, writeErr := f.Write(stream.Bytes())
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil || n != stream.Len() {
			t.Fatalf("fixture bytes=%d write=%v close=%v", n, writeErr, closeErr)
		}
	}
}
