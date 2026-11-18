//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestObservationFaultProofs(t *testing.T) {
	if os.Getenv("G2_OBSERVATION_WITNESS_CHILD") == "1" {
		observationWitnessChild(t)
		return
	}
	if os.Getenv("G2_OBSERVATION_READER_CHILD") == "1" {
		if _, err := unix.Write(3, []byte{1}); err != nil {
			t.Fatal(err)
		}
		for {
			_ = unix.Pause()
		}
	}
	t.Run("unexpected-rights-are-closed", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err := unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		var pipe [2]int
		if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pipe[0])
		packet := []byte(`{"request_id":1}`)
		if err := sendControl(pair[0], packet, []int{pipe[1]}); err != nil {
			unix.Close(pipe[1])
			t.Fatal(err)
		}
		unix.Close(pipe[1])
		data, cred, rights, observed, err := receiveControlObserved(pair[1], 0)
		if !errors.Is(err, errControlFrame) || data != nil || cred != nil || rights != nil {
			t.Fatalf("unexpected receive: data=%q cred=%v rights=%v err=%v", data, cred, rights, err)
		}
		sum := sha256.Sum256(packet)
		if observed.Bytes != len(packet) || observed.PacketSHA256 == nil || *observed.PacketSHA256 != hex.EncodeToString(sum[:]) || observed.RightsCount != 1 || observed.ClosedCount != 1 || observed.Stage != "validation" || observed.Errno != 0 || observed.PID == nil || *observed.PID != int32(os.Getpid()) || observed.UID == nil || *observed.UID != uint32(os.Getuid()) || observed.GID == nil || *observed.GID != uint32(os.Getgid()) {
			t.Fatalf("incorrect receive observation: %+v", observed)
		}
		var b [1]byte
		if n, err := unix.Read(pipe[0], b[:]); err != nil || n != 0 {
			t.Fatalf("received writer remained open: n=%d err=%v", n, err)
		}
	})

	t.Run("missing-credentials", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = sendControl(pair[0], []byte("{}"), nil); err != nil {
			t.Fatal(err)
		}
		_, _, _, obs, err := receiveControlObserved(pair[1], 0)
		if !errors.Is(err, errControlFrame) || obs.PID != nil || obs.UID != nil || obs.GID != nil || obs.Bytes != 2 {
			t.Fatalf("missing credentials were inferred: observation=%+v err=%v", obs, err)
		}
	})

	t.Run("truncated-packet", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		packet := make([]byte, maxControlFrame+2)
		if _, err = unix.SendmsgN(pair[0], packet, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
		_, _, _, obs, err := receiveControlObserved(pair[1], 0)
		if !errors.Is(err, errControlFrame) || !obs.Truncated || obs.Bytes != maxControlFrame+1 || obs.PacketSHA256 != nil {
			t.Fatalf("truncated packet was treated as complete: observation=%+v err=%v", obs, err)
		}
	})

	t.Run("reader-deadline", func(t *testing.T) {
		rootTestPinProc(t)
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		boot, err := fixedBootID()
		if err != nil {
			t.Fatal(err)
		}
		now, err := monoNowNS()
		if err != nil || now == 0 {
			t.Fatalf("monotonic clock: now=%d err=%v", now, err)
		}
		client := &auditReaderClient{fd: pair[0], pidfd: pidfd, pid: uint32(os.Getpid()), boot: boot, next: 1, request: []byte("{}"), deadline: now - 1}
		if _, err = client.service(); !errors.Is(err, unix.ETIMEDOUT) || client.detection == nil || client.detection.detector != "reader_deadline" || client.detection.observed < client.detection.deadline {
			t.Fatalf("deadline detection: cause=%+v err=%v", client.detection, err)
		}
	})

	t.Run("reader-endpoint", func(t *testing.T) {
		rootTestPinProc(t)
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			unix.Close(pair[1])
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		boot, err := fixedBootID()
		if err != nil {
			unix.Close(pair[1])
			t.Fatal(err)
		}
		client := &auditReaderClient{fd: pair[0], pidfd: pidfd, pid: uint32(os.Getpid()), boot: boot}
		unix.Close(pair[1])
		if err = client.healthy(); err == nil || client.detection == nil || client.detection.detector != "reader_endpoint_closed" || !pidfdSelects(pidfd, uint32(os.Getpid())) {
			t.Fatalf("endpoint detection: cause=%+v err=%v", client.detection, err)
		}
	})

	t.Run("reader-credentials", func(t *testing.T) {
		rootTestPinProc(t)
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		parent := os.Getppid()
		pidfd, err := unix.PidfdOpen(parent, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		boot, err := fixedBootID()
		if err != nil {
			t.Fatal(err)
		}
		now, err := monoNowNS()
		if err != nil {
			t.Fatal(err)
		}
		if err = sendControl(pair[1], []byte(`{"request_id":1}`), nil); err != nil {
			t.Fatal(err)
		}
		client := &auditReaderClient{fd: pair[0], pidfd: pidfd, pid: uint32(parent), boot: boot, next: 1, request: []byte("{}"), deadline: now + 1_000_000_000}
		if _, err = client.service(); err == nil || client.detection == nil || client.detection.detector != "reader_message_credentials" || client.detection.receive.PID == nil || *client.detection.receive.PID != int32(os.Getpid()) {
			t.Fatalf("credential detection: cause=%+v err=%v", client.detection, err)
		}
	})

	t.Run("audit-duplicate", func(t *testing.T) {
		syscall, err := parseAuditFact(auditSyscallRecord, []byte("audit(42.123:17): arch=c000003e syscall=321 success=no exit=-1 a0=0 pid=123 ppid=12"))
		if err != nil {
			t.Fatal(err)
		}
		join := rawJoinBroker(t)
		join.fault = &observationFaultState{variant: "audit-duplicate", auditActor: func(pid uint64) bool { return pid == syscall.PID }}
		if err := deliverAuditFact(join, syscall); err == nil || join.fault.detector != "audit_duplicate" || join.fault.audit == nil || join.fault.audit.PayloadSHA256 != syscall.PayloadSHA256 {
			t.Fatalf("duplicate delivery did not reach the normal join detector: cause=%+v err=%v", join.fault, err)
		}
	})

	t.Run("audit-incomplete", func(t *testing.T) {
		guard, _, eoe := rawJoinFixture(t)
		guard.Operation = unix.BPF_MAP_CREATE
		syscall, err := parseAuditFact(auditSyscallRecord, []byte("audit(42.123:17): arch=c000003e syscall=321 success=no exit=-1 a0=0 pid=123 ppid=12"))
		if err != nil {
			t.Fatal(err)
		}
		join := rawJoinBroker(t)
		join.fault = &observationFaultState{variant: "audit-loss", auditActor: func(pid uint64) bool { return pid == syscall.PID }}
		if err := join.observeGuard(guard); err != nil {
			t.Fatal(err)
		}
		if err := deliverAuditFact(join, syscall); err != nil {
			t.Fatal(err)
		}
		if err := deliverAuditFact(join, eoe); err != nil {
			t.Fatal(err)
		}
		if err := join.detectOmittedAudit(); err == nil || join.fault.detector != "audit_incomplete" || join.fault.audit == nil || join.fault.audit.PayloadSHA256 != eoe.PayloadSHA256 {
			t.Fatalf("missing EOE was not detected: cause=%+v err=%v", join.fault, err)
		}
	})

	t.Run("audit-capacity", func(t *testing.T) {
		join := rawJoinBroker(t)
		observer := rawJoinObserver()
		join.fault = &observationFaultState{variant: "audit-overflow"}
		for i := range maxAuditWireIdentities + 1 {
			fact, err := parseAuditFact(auditSockaddrRecord, []byte(fmt.Sprintf("audit(42.123:%d): saddr=0200", i+1)))
			if err != nil {
				t.Fatal(err)
			}
			if err = observer.observeAuditFact(fact); err != nil || observer.auditLost || observer.transcript.outputLoss {
				t.Fatalf("observer lost wire %d: %v", i+1, err)
			}
			err = deliverAuditFact(join, fact)
			if i < maxAuditWireIdentities && err != nil {
				t.Fatalf("early capacity failure at %d: %v", i+1, err)
			}
			if i == maxAuditWireIdentities && (err == nil || join.fault.detector != "audit_wire_capacity" || join.fault.countBefore != maxAuditWireIdentities || join.fault.audit == nil || join.fault.audit.PayloadSHA256 != fact.PayloadSHA256) {
				t.Fatalf("wrong capacity boundary: cause=%+v err=%v", join.fault, err)
			}
		}
	})

	for _, variant := range []string{"snapshot-missing", "snapshot-bad-field"} {
		t.Run(variant, func(t *testing.T) {
			var raw [guardRecordSize]byte
			guardPut64(raw[:], 0, 1)
			guardPut32(raw[:], 8, 3)
			guardPut64(raw[:], 16, 2)
			guardPut64(raw[:], 24, 3)
			guardPut64(raw[:], 48, 4)
			guardPut32(raw[:], 128, unix.BPF_MAP_TYPE_ARRAY)
			copy(raw[136:152], "feature_test")
			guardPut32(raw[:], 152, 4)
			guardPut32(raw[:], 156, 4)
			guardPut32(raw[:], 160, 1)
			if _, err := decodeGuardRecord(raw[:], 1); err != nil {
				t.Fatal(err)
			}
			modified := append([]byte(nil), raw[:]...)
			if variant == "snapshot-missing" {
				clear(modified[128:192])
			} else {
				guardPut32(modified, 156, 8)
			}
			o := &terminalObserver{armedVariant: variant, brokerSnapshotPages: map[string][]byte{"original": raw[:], "modified": modified}, snapshotOriginals: map[uint64][guardRecordSize]byte{1: raw}, snapshotSequences: map[uint64]uint64{1: 2}, brokerWitnessSequences: []any{jsonU64(3)}}
			detector := "guard_snapshot_missing"
			if variant == "snapshot-bad-field" {
				detector = "guard_snapshot_mismatch"
			}
			o.brokerObservation = map[string]any{"variant": variant, "source": "broker_guard_copy", "detector": detector, "guard_sequence": jsonU64(1), "original_sha256": hashBytes(raw[:]), "input_sha256": hashBytes(modified), "input_bytes": jsonU64(guardRecordSize)}
			if _, err := o.sourceFaultEvidence(); err != nil {
				t.Fatalf("valid snapshot copies rejected: %v", err)
			}
			o.brokerObservation["detector"] = "audit_duplicate"
			if _, err := o.sourceFaultEvidence(); err == nil {
				t.Fatal("accepted mismatched detector")
			}
			o.brokerObservation["detector"] = detector
			modified[20] ^= 1
			if _, err := o.sourceFaultEvidence(); err == nil {
				t.Fatal("accepted a mismatched snapshot witness")
			}
		})
	}

	t.Run("reader-replay-and-frame", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		oldNonce, newNonce := "11111111111111111111111111111111", "22222222222222222222222222222222"
		original, err := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "request_id": jsonU64(1), "nonce": oldNonce, "check": "status", "queries": []any{}})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			packet   []byte
			detector string
		}{{original, "reader_response_identity"}, {append(append([]byte(nil), original...), ' '), "reader_noncanonical_frame"}} {
			if err = sendControl(pair[0], tc.packet, nil); err != nil {
				t.Fatal(err)
			}
			raw, cred, _, observation, err := receiveControlObserved(pair[1], 0)
			if err != nil || cred == nil || cred.Pid != int32(os.Getpid()) || observation.PacketSHA256 == nil || *observation.PacketSHA256 != hashBytes(tc.packet) {
				t.Fatalf("actual seqpacket receipt: cred=%v observation=%+v err=%v", cred, observation, err)
			}
			if _, detector, err := auditReaderReplyFrame(raw, "test", 2, newNonce, "status"); err == nil || detector != tc.detector {
				t.Fatalf("wrong reply detector: got=%q err=%v", detector, err)
			}
		}
	})
	t.Run("reader-witness-eof", testObservationWitnessEOF)
	t.Run("reader-pidfd-exit", func(t *testing.T) {
		rootTestPinProc(t)
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		executable, err := os.Executable()
		if err != nil {
			writer.Close()
			t.Fatal(err)
		}
		child := exec.Command(executable, "-test.run=^TestObservationFaultProofs$")
		child.Env = append(os.Environ(), "G2_OBSERVATION_READER_CHILD=1")
		child.ExtraFiles = []*os.File{writer}
		if err = child.Start(); err != nil {
			writer.Close()
			t.Fatal(err)
		}
		writer.Close()
		defer func() {
			if child.ProcessState == nil {
				_ = child.Process.Kill()
				_ = child.Wait()
			}
		}()
		barrier := []unix.PollFd{{Fd: int32(reader.Fd()), Events: unix.POLLIN}}
		if n, err := unix.Poll(barrier, 5000); err != nil || n != 1 || barrier[0].Revents&unix.POLLIN == 0 {
			t.Fatalf("child barrier not reached: n=%d err=%v", n, err)
		}
		var ready [1]byte
		if _, err = io.ReadFull(reader, ready[:]); err != nil || ready[0] != 1 {
			t.Fatalf("child barrier: %v", err)
		}
		pidfd, err := unix.PidfdOpen(child.Process.Pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		if err = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
			t.Fatal(err)
		}
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		if n, err := unix.Poll(poll, 5000); err != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
			t.Fatalf("reader pidfd did not report exit: n=%d err=%v", n, err)
		}
		_ = child.Wait()
		status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("child was not killed: %v", child.ProcessState)
		}
		client := &auditReaderClient{pidfd: pidfd, pid: uint32(child.Process.Pid)}
		if err = client.healthy(); err == nil || client.detection == nil || client.detection.detector != "reader_pidfd_exited" || client.healthReason != readerHealthPIDFDBefore {
			t.Fatalf("pidfd exit detector: cause=%+v err=%v", client.detection, err)
		}
	})

	t.Run("reader-missing-done", func(t *testing.T) {
		row := func(kind, flags, sequence uint64, payload []byte) map[string]any {
			return map[string]any{"source_pid": jsonU64(0), "source_groups": jsonU64(0), "length": jsonU64(uint64(16 + len(payload))), "type": jsonU64(kind), "flags": jsonU64(flags), "sequence": jsonU64(sequence), "header_pid": jsonU64(0), "payload_hex": hex.EncodeToString(payload)}
		}
		query := func(kind, sequence uint64, replies []any) map[string]any {
			return map[string]any{"request_type": jsonU64(kind), "request_sequence": jsonU64(sequence), "request_portid": jsonU64(7), "replies": replies}
		}
		rules := auditRuleSet(false)
		queries := []any{query(auditGetType, 1, []any{row(auditGetType, 0, 1, make([]byte, 44))}), query(auditListRulesType, 2, []any{row(auditListRulesType, unix.NLM_F_MULTI, 2, rules[0]), row(auditListRulesType, unix.NLM_F_MULTI, 2, rules[1]), row(unix.NLMSG_DONE, unix.NLM_F_MULTI, 2, nil)}), query(auditGetType, 3, []any{row(auditGetType, 0, 3, make([]byte, 44))})}
		if _, err := validateAuditQueries(queries, "configuration"); err != nil {
			t.Fatalf("valid configuration: %v", err)
		}
		modified, err := readerWithoutDone(queries)
		if err != nil {
			t.Fatal(err)
		}
		nonce := "33333333333333333333333333333333"
		packet, err := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "request_id": jsonU64(1), "nonce": nonce, "check": "configuration", "queries": modified})
		if err != nil {
			t.Fatal(err)
		}
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		if err = sendControl(pair[0], packet, nil); err != nil {
			t.Fatal(err)
		}
		raw, _, _, _, err := receiveControlObserved(pair[1], 0)
		if err != nil {
			t.Fatal(err)
		}
		frame, _, err := auditReaderReplyFrame(raw, "test", 1, nonce, "configuration")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = validateAuditQueries(frame["queries"], "configuration"); !errors.Is(err, errAuditMissingDone) {
			t.Fatalf("missing DONE not detected: %v", err)
		}
	})
	t.Run("label-only-is-not-proof", func(t *testing.T) {
		observer := &terminalObserver{armedVariant: "audit-loss", brokerObservation: map[string]any{"variant": "audit-loss", "source": "broker_audit_delivery", "detector": "audit_incomplete"}}
		if _, err := observer.sourceFaultEvidence(); err == nil || observer.auditLost || observer.nativeLost {
			t.Fatalf("label-only evidence accepted or changed stream loss: %v", err)
		}
	})
}

func observationWitnessPacket() ([]byte, error) {
	return canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "kind": "reader_witness_chunk", "variant": "reader-death", "request_id": jsonU64(1), "nonce": "11111111111111111111111111111111", "role": "original_reply", "page_index": jsonU64(0), "page_count": jsonU64(1), "total_bytes": jsonU64(1), "total_sha256": hashBytes([]byte("a")), "data_b64": "YQ=="})
}

// The child sends any queued packet before the ready barrier, then waits for
// the parent to request an exit or a live endpoint shutdown.
func observationWitnessChild(t *testing.T) {
	var packet []byte
	switch os.Getenv("G2_WITNESS_PACKET") {
	case "malformed":
		packet = []byte("{")
	case "rights", "valid":
		var err error
		packet, err = observationWitnessPacket()
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("G2_WITNESS_PACKET") == "empty" {
		if _, err := unix.SendmsgN(4, []byte{}, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
	} else if packet != nil {
		var rights []int
		if os.Getenv("G2_WITNESS_PACKET") == "rights" {
			rights = []int{3}
		}
		if err := sendControl(4, packet, rights); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := unix.Write(3, []byte{1}); err != nil {
		t.Fatal(err)
	}
	var command [1]byte
	if n, err := unix.Read(5, command[:]); err != nil || n != 1 {
		t.Fatalf("child command: n=%d err=%v", n, err)
	}
	if command[0] == 's' {
		if err := unix.Shutdown(4, unix.SHUT_WR); err != nil {
			t.Fatal(err)
		}
		if _, err := unix.Write(3, []byte{2}); err != nil {
			t.Fatal(err)
		}
		for {
			_ = unix.Pause()
		}
	}
	os.Exit(0)
}

func testObservationWitnessEOF(t *testing.T) {
	rootTestPinProc(t)
	cases := []struct {
		name, packet, variant                     string
		live, hold, brokerDead, wrongSender, loss bool
	}{
		{"expected-reader-death", "", "reader-death", false, true, false, false, false},
		{"expected-reader-killed", "", "reader-death", false, true, false, false, false},
		{"expected-reader-death-reaped", "", "reader-death", false, true, false, false, false},
		{"normal-broker-teardown", "", "other", false, false, true, false, false},
		{"live-reader-closure", "", "reader-death", true, true, false, false, true},
		{"unexpected-reader-death", "", "other", false, false, false, false, true},
		{"missing-death-hold", "", "reader-death", false, false, false, false, true},
		{"missing-reader-pidfd", "", "reader-death", false, true, false, false, true},
		{"missing-hold-after-broker-death", "", "reader-death", false, false, true, false, true},
		{"queued-malformed", "malformed", "reader-death", false, true, false, false, true},
		{"queued-empty", "empty", "reader-death", false, true, false, false, true},
		{"queued-rights", "rights", "reader-death", false, true, false, false, true},
		{"wrong-credentials", "", "reader-death", false, true, false, true, true},
		{"queued-valid-before-eof", "valid", "reader-death", false, true, false, false, false},
		{"queued-valid-reaped", "valid", "reader-death", false, true, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.packet == "valid" && (os.Getuid() != 0 || os.Getgid() != 0) {
				t.Skip("authenticated reader witness requires root credentials")
			}
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pair[0])
			if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
				t.Fatal(err)
			}
			ready, readyWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer ready.Close()
			commandReader, command, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer command.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(executable, "-test.run=^TestObservationFaultProofs$")
			child.Env = append(os.Environ(), "G2_OBSERVATION_WITNESS_CHILD=1", "G2_WITNESS_PACKET="+tc.packet)
			peer := os.NewFile(uintptr(pair[1]), "witness-peer")
			child.ExtraFiles = []*os.File{readyWriter, peer, commandReader}
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			readyWriter.Close()
			commandReader.Close()
			defer func() {
				if child.ProcessState == nil {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			}()
			if tc.wrongSender {
				packet, e := observationWitnessPacket()
				if e != nil {
					t.Fatal(e)
				}
				if err = sendControl(pair[1], packet, nil); err != nil {
					t.Fatal(err)
				}
			}
			peer.Close()
			waitByte := func(want byte) {
				t.Helper()
				p := []unix.PollFd{{Fd: int32(ready.Fd()), Events: unix.POLLIN}}
				if n, e := unix.Poll(p, 5000); e != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
					t.Fatalf("child barrier: n=%d events=%v err=%v", n, p[0].Revents, e)
				}
				var b [1]byte
				if _, e := io.ReadFull(ready, b[:]); e != nil || b[0] != want {
					t.Fatalf("child barrier: got=%v err=%v", b, e)
				}
			}
			waitByte(1)
			pidfd, err := unix.PidfdOpen(child.Process.Pid, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			brokerFD, err := unix.PidfdOpen(os.Getpid(), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(brokerFD)
			brokerPID := uint32(os.Getpid())
			if tc.brokerDead {
				// A separate held child pidfd represents the original broker.
				broker := exec.Command(executable, "-test.run=^TestObservationFaultProofs$")
				broker.Env = append(os.Environ(), "G2_OBSERVATION_READER_CHILD=1")
				br, bw, e := os.Pipe()
				if e != nil {
					t.Fatal(e)
				}
				defer br.Close()
				broker.ExtraFiles = []*os.File{bw}
				if e = broker.Start(); e != nil {
					t.Fatal(e)
				}
				bw.Close()
				defer func() {
					if broker.ProcessState == nil {
						_ = broker.Process.Kill()
						_ = broker.Wait()
					}
				}()
				barrier := []unix.PollFd{{Fd: int32(br.Fd()), Events: unix.POLLIN}}
				if n, e := unix.Poll(barrier, 5000); e != nil || n != 1 || barrier[0].Revents&unix.POLLIN == 0 {
					t.Fatalf("broker barrier: n=%d err=%v", n, e)
				}
				var b [1]byte
				if _, e = io.ReadFull(br, b[:]); e != nil || b[0] != 1 {
					t.Fatalf("broker barrier: %v", e)
				}
				fd, e := unix.PidfdOpen(broker.Process.Pid, 0)
				if e != nil {
					t.Fatal(e)
				}
				defer unix.Close(fd)
				brokerFD, brokerPID = fd, uint32(broker.Process.Pid)
				if e = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); e != nil {
					t.Fatal(e)
				}
				p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
				if n, e := unix.Poll(p, 5000); e != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
					t.Fatalf("broker exit: n=%d err=%v", n, e)
				}
			}
			obs := &terminalObserver{bootID: "test", witnessFD: pair[0], readerPID: uint32(child.Process.Pid), readerPIDFD: pidfd, brokerPID: brokerPID, brokerPIDFD: brokerFD, armed: true, armedVariant: tc.variant, readerWitnessArmID: 1, readerWitnessHoldFD: -1, transcript: newObserverTranscript("test", "")}
			if tc.name == "missing-reader-pidfd" {
				obs.readerPIDFD = -1
			}
			if tc.hold {
				obs.readerWitnessHoldFD, err = unix.Dup(pidfd)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if obs.readerWitnessHoldFD >= 0 {
						_ = unix.Close(obs.readerWitnessHoldFD)
					}
				}()
			}
			if tc.name == "expected-reader-killed" {
				if err = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				cmd := byte('e')
				if tc.live {
					cmd = 's'
				}
				if _, err = command.Write([]byte{cmd}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.live {
				waitByte(2)
			} else {
				p := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
				if n, e := unix.Poll(p, 5000); e != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
					t.Fatalf("reader exit: n=%d err=%v", n, e)
				}
			}
			if tc.name == "expected-reader-death-reaped" || tc.name == "queued-valid-reaped" {
				if err = child.Wait(); err != nil {
					t.Fatalf("reader reap: %v", err)
				}
			}
			for {
				p := []unix.PollFd{{Fd: int32(pair[0]), Events: unix.POLLIN}}
				if n, e := unix.Poll(p, 5000); e != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
					t.Fatalf("witness poll: n=%d events=%v err=%v", n, p[0].Revents, e)
				}
				if !tc.live && p[0].Revents&unix.POLLHUP == 0 {
					t.Fatalf("expected POLLIN|POLLHUP: %v", p[0].Revents)
				}
				if obs.serviceReaderWitness(p[0].Revents) {
					break
				}
			}
			if obs.transcript.outputLoss != tc.loss {
				t.Fatalf("output loss=%v, want %v", obs.transcript.outputLoss, tc.loss)
			}
			if !tc.loss && tc.variant == "reader-death" && (!obs.readerDeathHoldObserved || obs.readerWitnessHoldFD != -1) {
				t.Fatal("reader death hold was not recorded and released")
			}
			if tc.packet == "valid" && !tc.loss && (obs.readerPages["original_reply"] == nil || string(obs.readerPages["original_reply"].data) != "a") {
				t.Fatal("queued authenticated frame was not drained")
			}
			if !tc.live && child.ProcessState == nil {
				err = child.Wait()
				if tc.name == "expected-reader-killed" && child.ProcessState == nil {
					t.Fatalf("reader reap failed: %v", err)
				}
				if tc.name == "expected-reader-killed" {
					status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
						t.Fatalf("reader was not killed: status=%v err=%v", child.ProcessState, err)
					}
				} else if err != nil {
					t.Fatalf("reader reap: %v", err)
				}
			}
		})
	}
}

func TestReaderHealthDiagnostic(t *testing.T) {
	rootTestPinProc(t)
	var nilClient *auditReaderClient
	if err := nilClient.healthy(); !errors.Is(err, errGuardProtocol) || g2d1ReaderHealthStage(nilClient, false) != "BROKER_READER_NIL" {
		t.Fatalf("nil reader: %v", err)
	}
	invalid := &auditReaderClient{invalid: true}
	if err := invalid.healthy(); !errors.Is(err, errGuardProtocol) || invalid.healthReason != readerHealthInvalid {
		t.Fatalf("invalid reader: reason=%v err=%v", invalid.healthReason, err)
	}
	for _, variant := range []string{"endpoint", "unsolicited"} {
		t.Run(variant, func(t *testing.T) {
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pair[0])
			peer := pair[1]
			defer func() {
				if peer >= 0 {
					_ = unix.Close(peer)
				}
			}()
			pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			boot, err := fixedBootID()
			if err != nil {
				t.Fatal(err)
			}
			mismatch := &auditReaderClient{fd: pair[0], pidfd: pidfd, pid: uint32(os.Getpid()), boot: "not-the-current-boot"}
			if err := mismatch.healthy(); !errors.Is(err, errGuardProtocol) || mismatch.healthReason != readerHealthBootMismatch || g2d1ReaderHealthStage(mismatch, true) != "BROKER_READER_EVENT_BOOT_MISMATCH" {
				t.Fatalf("boot mismatch: reason=%v err=%v", mismatch.healthReason, err)
			}
			client := &auditReaderClient{fd: pair[0], pidfd: pidfd, pid: uint32(os.Getpid()), boot: boot}
			if err := client.healthy(); err != nil || client.healthReason != readerHealthUnknown {
				t.Fatalf("live reader: reason=%v err=%v", client.healthReason, err)
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			original := g2d1
			defer func() { g2d1 = original }()
			g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'B', false
			g2d1.count, g2d1.failed, g2d1.captured = 0, false, false
			const secret = "private-query-payload-47b9"
			wantReason, wantStage := readerHealthEndpoint, "BROKER_READER_ENDPOINT"
			wantEventStage := "BROKER_READER_EVENT_ENDPOINT"
			if variant == "endpoint" {
				if err := unix.Close(peer); err != nil {
					t.Fatal(err)
				}
				peer = -1
			} else {
				wantReason, wantStage = readerHealthUnsolicited, "BROKER_READER_UNSOLICITED"
				wantEventStage = "BROKER_READER_EVENT_UNSOLICITED"
				if err := unix.Sendmsg(peer, []byte(secret), nil, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			observed := client.healthy()
			if !errors.Is(observed, errGuardProtocol) || !client.invalid || client.healthReason != wantReason || g2d1ReaderHealthStage(client, false) != wantStage || g2d1ReaderHealthStage(client, true) != wantEventStage {
				t.Fatalf("health result: reason=%v invalid=%t err=%v", client.healthReason, client.invalid, observed)
			}
			if g2d1.failed || g2d1.captured || g2d1.count != 0 {
				t.Fatal("healthy captured a global failure")
			}
			if variant == "unsolicited" {
				var buf [128]byte
				n, _, err := unix.Recvfrom(pair[0], buf[:], unix.MSG_DONTWAIT)
				if err != nil || string(buf[:n]) != secret {
					t.Fatalf("healthy consumed or changed packet: n=%d err=%v", n, err)
				}
			}
			if err := client.healthy(); !errors.Is(err, errGuardProtocol) || client.healthReason != wantReason {
				t.Fatalf("first reason not retained: reason=%v err=%v", client.healthReason, err)
			}
			if got := g2d1CaptureReturned(wantStage, nil, observed); got != nil || g2d1.failed {
				t.Fatal("handled observation captured a failure")
			}
			if got := g2d1CaptureReturned(g2d1ReaderHealthStage(client, false), observed, observed); got != observed {
				t.Fatal("returned error changed")
			}
			g2d1CaptureFailure("LATER_FAILURE", unix.EIO)
			p := []unix.PollFd{{Fd: int32(r.Fd()), Events: unix.POLLIN}}
			if n, err := unix.Poll(p, 0); err != nil || n != 0 {
				t.Fatalf("premature diagnostic: ready=%d err=%v", n, err)
			}
			g2d1TerminalFail(unix.EIO)
			g2d1TerminalFail(unix.EIO)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			out, err := io.ReadAll(r)
			if err != nil || string(out) != "G2D1 B "+wantStage+" CHECK\n" || len(out) > 128 || strings.Contains(string(out), secret) || g2d1.count != 1 {
				t.Fatalf("terminal diagnostic: %q count=%d err=%v", out, g2d1.count, err)
			}
		})
	}
}
