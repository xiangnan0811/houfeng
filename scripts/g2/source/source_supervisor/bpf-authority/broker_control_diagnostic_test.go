//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBrokerControlDiagnostic(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	var d brokerControlDiagnostic
	if d.reject(brokerControlPointAuthenticate, first) != first || d.reject(brokerControlPointAuthDenial, second) != second || d.first != 12 || d.returned != 13 {
		t.Fatalf("rejection: %+v", d)
	}
	if d.reject(brokerControlPointReceive, nil) != nil || (*brokerControlDiagnostic)(nil).reject(brokerControlPointReceive, first) != first {
		t.Fatal("nil rejection")
	}
	d.bind = brokerBindDiagnostic{point: 3, before: 1, after: 4, deletePoint: 2, targetPoint: 3}
	d.finish(second)
	if d.first != 12 || d.returned != 13 {
		t.Fatal("failure cleared")
	}
	d.finish(nil)
	if d != (brokerControlDiagnostic{}) {
		t.Fatal("success retained metadata")
	}
	_ = d.reject(brokerControlPointAuthenticate, first)
	d.finish(nil)
	if d != (brokerControlDiagnostic{}) {
		t.Fatal("denial success retained observation")
	}
	pending := true
	if d.afterTaskDrop(nil, second) != second || d.first != 24 || d.returned != 24 || !pending {
		t.Fatal("drop selection")
	}
	d = brokerControlDiagnostic{first: 12, returned: 13, drop: 2, bind: brokerBindDiagnostic{point: 3, before: 1, after: 4, deletePoint: 2, targetPoint: 3}}
	if d.afterTaskDrop(first, second) != first || d.returned != 13 || d.drop != 2 || d.bind.deletePoint != 2 || d.bind.targetPoint != 3 {
		t.Fatal("prior precedence")
	}
	if d.afterTaskDrop(first, nil) != first || (*brokerControlDiagnostic)(nil).afterTaskDrop(nil, second) != second {
		t.Fatal("nil sink")
	}
	cases := []struct {
		op        string
		body      any
		operation brokerControlOperation
		command   brokerControlCommand
	}{
		{"BIND_RUNTIME", map[string]any{"command": "create"}, 2, 1}, {"BIND_RUNTIME", map[string]any{"command": "start"}, 2, 2},
		{"BIND_RUNTIME", nil, 2, 0}, {"BIND_RUNTIME", map[string]any{"command": 1}, 2, 0},
		{"BIND_RUNTIME", map[string]any{"command": "secret"}, 2, 8}, {"READ_STATUS", nil, 12, 0}, {"secret", nil, 13, 0},
		{"BIND_RUNTIME", map[string]any{"command": "state"}, 2, 3}, {"BIND_RUNTIME", map[string]any{"command": "delete"}, 2, 4},
		{"BIND_RUNTIME", map[string]any{"command": "kill"}, 2, 5}, {"BIND_RUNTIME", map[string]any{"command": "exec"}, 2, 6}, {"BIND_RUNTIME", map[string]any{"command": "features"}, 2, 7},
		{"REGISTER_LAUNCH", nil, 1, 0}, {"REGISTER_REALM", nil, 3, 0}, {"REGISTER_RECOVERY", nil, 3, 0}, {"REGISTER_DAEMON", nil, 3, 0},
		{"ADMIT", nil, 4, 0}, {"SETTLE", nil, 5, 0}, {"ADMIT_EXEC", nil, 6, 0}, {"TRACK_EXEC", nil, 7, 0}, {"CANCEL_EXEC", nil, 8, 0},
		{"BEGIN_ENDPOINT_TRANSITION", nil, 9, 0}, {"END_ENDPOINT_TRANSITION", nil, 9, 0}, {"SELFTEST_FD_ARM", nil, 10, 0},
		{"REGISTER_SELFTEST_PROBE", nil, 10, 0}, {"SELFTEST_RUNTIME_ARM", nil, 10, 0}, {"SELFTEST_BASELINE_STEP", nil, 10, 0},
		{"SELFTEST_REVOKE_AND_CLOSE", nil, 10, 0}, {"SELFTEST_OBSERVATION_FAULT", nil, 10, 0}, {"QUIESCE", nil, 11, 0},
		{"RELEASE", nil, 11, 0}, {"AUDIT_CUT", nil, 11, 0}, {"READ_EVIDENCE", nil, 12, 0},
	}
	for _, c := range cases {
		o := brokerControlOperationFor(c.op)
		if o != c.operation || brokerControlCommandFor(map[string]any{"body": c.body}, o) != c.command {
			t.Fatalf("classification: %+v", c)
		}
	}
}

func TestBrokerBindCheckpointDiagnostic(t *testing.T) {
	b := controlDiagnosticFixture()
	if brokerBindCheckpointState(nil) != 0 || brokerBindCheckpointState(&authorityBroker{}) != 0 {
		t.Fatal("missing")
	}
	for _, c := range []struct {
		invalid, faulted bool
		want             brokerBindCheckpoint
	}{
		{false, false, 1}, {true, false, 2}, {false, true, 3}, {true, true, 4},
	} {
		b.checkpoint.invalid, b.checkpoint.faulted = c.invalid, c.faulted
		if got := brokerBindCheckpointState(b); got != c.want {
			t.Fatalf("state %d want %d", got, c.want)
		}
	}
	b.checkpoint = &checkpointController{}
	d := brokerBindDiagnostic{point: 42, before: 3, after: 4, deletePoint: 2, targetPoint: 3}
	d.begin(b)
	if d != (brokerBindDiagnostic{before: 1}) {
		t.Fatalf("begin %+v", d)
	}
	e := errors.New("sentinel")
	if d.reject(3, e) != e || d.reject(4, e) != e || d.reject(5, nil) != nil || (*brokerBindDiagnostic)(nil).reject(1, e) != e || d.point != 3 {
		t.Fatal("reject")
	}
	if d.rejectDelete(0, e) != e || d.rejectDelete(2, nil) != nil || (*brokerBindDiagnostic)(nil).rejectDelete(1, e) != e || d.rejectDelete(2, e) != e || d.rejectDelete(3, e) != e || d.deletePoint != 2 || d.point != 3 || d.before != 1 || d.after != 0 {
		t.Fatal("delete reject")
	}
	if d.rejectTarget(0, e) != e || d.rejectTarget(2, nil) != nil || (*brokerBindDiagnostic)(nil).rejectTarget(1, e) != e || d.rejectTarget(3, e) != e || d.rejectTarget(4, e) != e || d.targetPoint != 3 || d.deletePoint != 2 || d.point != 3 || d.before != 1 || d.after != 0 {
		t.Fatal("target reject")
	}
	b.checkpoint.markFault()
	d.finish(b, e)
	if d != (brokerBindDiagnostic{point: 3, before: 1, after: 4, deletePoint: 2, targetPoint: 3}) {
		t.Fatalf("fault %+v", d)
	}
	old := b.checkpoint
	b.checkpoint = &checkpointController{faulted: true}
	d.finish(b, e)
	if d.after != 3 || !old.invalid || !old.faulted {
		t.Fatal("replacement")
	}
	b.checkpoint = nil
	d.finish(b, e)
	if d.after != 0 {
		t.Fatal("missing replacement")
	}
	d.finish(b, nil)
	if d != (brokerBindDiagnostic{}) {
		t.Fatal("success retained metadata")
	}
	(*brokerBindDiagnostic)(nil).begin(b)
	(*brokerBindDiagnostic)(nil).finish(b, e)
}

func TestBrokerRuntimeBindDiagnostic(t *testing.T) {
	type testCase struct {
		name  string
		setup func(**authorityBroker, map[string]any)
		point brokerBindPoint
		state brokerBindCheckpoint
	}
	cases := []testCase{
		{"broker-nil", func(b **authorityBroker, _ map[string]any) { *b = nil }, 1, 0},
		{"checkpoint-nil", func(b **authorityBroker, _ map[string]any) { (*b).checkpoint = nil }, 2, 0},
		{"invalid", func(b **authorityBroker, _ map[string]any) { (*b).checkpoint.invalid = true }, 3, 2},
		{"active", func(b **authorityBroker, _ map[string]any) { (*b).checkpoint.active = &activeCheckpoint{} }, 4, 1},
		{"pending-checkpoint", func(b **authorityBroker, _ map[string]any) { (*b).pendingCheckpoint = &pendingCheckpointRequest{} }, 5, 1},
		{"pending-admit", func(b **authorityBroker, _ map[string]any) { (*b).pendingAdmit = &pendingAdmit{} }, 6, 1},
		{"pending-exec", func(b **authorityBroker, _ map[string]any) { (*b).pendingExecOp = &pendingExecOperation{} }, 7, 1},
		{"authority-nil", func(b **authorityBroker, _ map[string]any) { (*b).authority = nil }, 8, 1},
		{"lifecycle-nil", func(b **authorityBroker, _ map[string]any) { (*b).checkpoint.lifecycle = nil }, 9, 1},
		{"invalid-faulted", func(b **authorityBroker, _ map[string]any) {
			(*b).checkpoint.invalid = true
			(*b).checkpoint.faulted = true
		}, 3, 4},
		{"invalid-before-active", func(b **authorityBroker, _ map[string]any) {
			(*b).checkpoint.invalid = true
			(*b).checkpoint.active = &activeCheckpoint{}
		}, 3, 2},
		{"pending-order", func(b **authorityBroker, _ map[string]any) {
			(*b).pendingCheckpoint = &pendingCheckpointRequest{}
			(*b).pendingAdmit = &pendingAdmit{}
		}, 5, 1},
		{"request", func(_ **authorityBroker, r map[string]any) { r["version"] = json.Number("2") }, 10, 1},
		{"body-extra", func(_ **authorityBroker, r map[string]any) { r["body"].(map[string]any)["extra"] = true }, 11, 1},
		{"body-type", func(_ **authorityBroker, r map[string]any) { r["body"] = true }, 11, 1},
		{"command", func(_ **authorityBroker, r map[string]any) { r["body"].(map[string]any)["command"] = true }, 12, 1},
		{"cid", func(_ **authorityBroker, r map[string]any) { r["body"].(map[string]any)["container_id"] = "invalid" }, 14, 1},
		{"rights", func(_ **authorityBroker, _ map[string]any) {}, 15, 1},
		{"faulted-only", func(b **authorityBroker, _ map[string]any) { (*b).checkpoint.faulted = true }, 15, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, withSink := range []bool{true, false} {
				b := controlDiagnosticFixture()
				b.checkpoint.lifecycle = &checkpointLifecycle{}
				r := map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "request_id": json.Number("1"), "operation": "BIND_RUNTIME", "body": map[string]any{"container_id": strings.Repeat("a", 64), "command": "delete", "bundle_sha256": nil, "process_sha256": nil}}
				c.setup(&b, r)
				var d brokerBindDiagnostic
				var sink *brokerBindDiagnostic
				if withSink {
					sink = &d
				}
				ack, retained, err := b.bindRuntimeRequest(-1, nil, r, nil, nil, sink)
				if ack != nil || retained || err != errControlFrame {
					t.Fatalf("return %v %v %v", ack, retained, err)
				}
				if withSink && d != (brokerBindDiagnostic{point: c.point, before: c.state, after: c.state}) {
					t.Fatalf("diagnostic %+v", d)
				}
			}
		})
	}
	for _, c := range []struct {
		name string
		cred *unix.Ucred
	}{
		{"nil-credential", nil}, {"invalid-pid", &unix.Ucred{Pid: 1}}, {"nonroot-uid", &unix.Ucred{Pid: 2, Uid: 1}}, {"nonroot-gid", &unix.Ucred{Pid: 2, Gid: 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			pair := controlDiagnosticPair(t)
			flags, err := unix.FcntlInt(uintptr(pair[0]), unix.F_GETFD, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, withSink := range []bool{true, false} {
				b := controlDiagnosticFixture()
				b.checkpoint.lifecycle = &checkpointLifecycle{}
				r := map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "request_id": json.Number("1"), "operation": "BIND_RUNTIME", "body": map[string]any{"container_id": strings.Repeat("a", 64), "command": "delete", "bundle_sha256": nil, "process_sha256": nil}}
				var d brokerBindDiagnostic
				var sink *brokerBindDiagnostic
				if withSink {
					sink = &d
				}
				ack, retained, err := b.bindRuntimeRequest(pair[1], nil, r, c.cred, []int{pair[0]}, sink)
				if ack != nil || retained || err != errControlFrame || (withSink && d != (brokerBindDiagnostic{point: 15, before: 1, after: 1})) {
					t.Fatalf("credential %v %v %v %+v", ack, retained, err, d)
				}
			}
			var st unix.Stat_t
			if unix.Fstat(pair[0], &st) != nil {
				t.Fatal("right closed")
			}
			after, err := unix.FcntlInt(uintptr(pair[0]), unix.F_GETFD, 0)
			if err != nil || after != flags {
				t.Fatal("flags changed")
			}
		})
	}
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "peer-error", true: "peer-mismatch"}[mismatch], func(t *testing.T) {
			pair := controlDiagnosticPair(t)
			for _, withSink := range []bool{true, false} {
				b := controlDiagnosticFixture()
				b.checkpoint.lifecycle = &checkpointLifecycle{}
				r := map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "request_id": json.Number("1"), "operation": "BIND_RUNTIME", "body": map[string]any{"container_id": strings.Repeat("a", 64), "command": "delete", "bundle_sha256": nil, "process_sha256": nil}}
				fd := -1
				cred := &unix.Ucred{Pid: 2}
				if mismatch {
					fd = pair[1]
					peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
					if err != nil {
						t.Fatal(err)
					}
					if peer.Pid == 2 {
						cred.Pid = 3
					}
				}
				flags, err := unix.FcntlInt(uintptr(pair[0]), unix.F_GETFD, 0)
				if err != nil {
					t.Fatal(err)
				}
				var d brokerBindDiagnostic
				var sink *brokerBindDiagnostic
				if withSink {
					sink = &d
				}
				ack, retained, err := b.bindRuntimeRequest(fd, nil, r, cred, []int{pair[0]}, sink)
				if ack != nil || retained || err != errControlFrame || (withSink && d != (brokerBindDiagnostic{point: 16, before: 1, after: 1})) {
					t.Fatalf("peer %v %v %v %+v", ack, retained, err, d)
				}
				var st unix.Stat_t
				if unix.Fstat(pair[0], &st) != nil {
					t.Fatal("right closed")
				}
				after, err := unix.FcntlInt(uintptr(pair[0]), unix.F_GETFD, 0)
				if err != nil || after != flags {
					t.Fatal("flags changed")
				}
			}
		})
	}
}

func controlDiagnosticFixture() *authorityBroker {
	boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
	return &authorityBroker{bootID: boot, epoch: epoch, authority: &sealedAuthority{guard: guardDevice{fd: -1}}, runtimeRootPIDFD: -1, runtimeRootFD: -1, runtimeRootExeFD: -1,
		checkpoint: &checkpointController{}, transcript: newAuthorityTranscript(boot, epoch), reads: &brokerReadState{bootID: boot, epoch: epoch, runToken: authorityRunToken}, principals: map[uint64]*controlPrincipal{}}
}

func controlDiagnosticPair(t *testing.T) *[2]int {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, fd := range pair {
			if fd >= 0 {
				_ = unix.Close(fd)
			}
		}
	})
	if err := unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		t.Fatal(err)
	}
	return &pair
}

func TestBrokerControlDispatchDiagnostic(t *testing.T) {
	t.Run("receive", func(t *testing.T) {
		for _, withSink := range []bool{true, false} {
			b := controlDiagnosticFixture()
			var d brokerControlDiagnostic
			var sink *brokerControlDiagnostic
			if withSink {
				sink = &d
			}
			pending, err := b.dispatchBrokerControl(-1, sink)
			if pending || !errors.Is(err, unix.EBADF) || withSink && (d.first != 1 || d.returned != 1) {
				t.Fatalf("sink=%v pending=%v err=%v diagnostic=%+v", withSink, pending, err, d)
			}
			if b.checkpoint.invalid || b.checkpoint.faulted || len(b.transcript.entries) != 0 {
				t.Fatal("receive failure changed broker state")
			}
		}
	})
	original := g2d1
	defer func() { g2d1 = original }()
	t.Run("parse closes rights", func(t *testing.T) {
		for _, withSink := range []bool{true, false} {
			pair := controlDiagnosticPair(t)
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			if err = sendControl(pair[0], []byte(`{ "x":1}`), []int{int(w.Fd())}); err != nil {
				t.Fatal(err)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			b := controlDiagnosticFixture()
			var d brokerControlDiagnostic
			var sink *brokerControlDiagnostic
			if withSink {
				sink = &d
			}
			pending, err := b.dispatchBrokerControl(pair[1], sink)
			if pending || err != errControlFrame || withSink && (d.first != 2 || d.returned != 2) {
				t.Fatalf("sink=%v pending=%v err=%v diagnostic=%+v", withSink, pending, err, d)
			}
			if b.checkpoint.invalid || b.checkpoint.faulted || len(b.transcript.entries) != 0 {
				t.Fatal("parse failure changed broker state")
			}
			for _, fd := range []int{int(r.Fd()), pair[0], pair[1]} {
				var st unix.Stat_t
				if err := unix.Fstat(fd, &st); err != nil {
					t.Fatalf("caller descriptor %d closed: %v", fd, err)
				}
			}
			if _, err = r.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("received right leaked: %v", err)
			}
		}
	})
	cases := []struct {
		name, raw string
		setup     func(*authorityBroker)
		op        brokerControlOperation
		cmd       brokerControlCommand
		point     brokerControlPoint
	}{
		{"missing operation", `{}`, nil, 0, 0, 3},
		{"nonstring operation", `{"operation":1}`, nil, 0, 0, 3},
		{"observation gate", `{"operation":"REGISTER_LAUNCH"}`, func(b *authorityBroker) { b.pendingObservation = &pendingObservationFault{} }, 1, 0, 4},
		{"root gate", `{"operation":"SETTLE"}`, nil, 5, 0, 5},
		{"root recheck", `{"body":{"command":"create"},"operation":"BIND_RUNTIME"}`, func(b *authorityBroker) { b.runtimeRootState = runtimeRootReady }, 2, 1, 6},
		{"container gate", `{"operation":"READ_STATUS"}`, func(b *authorityBroker) { b.pendingContainerProbe = &pendingContainerSelftestProbe{} }, 12, 0, 7},
		{"authentication", `{"operation":"READ_STATUS"}`, nil, 12, 0, 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, sink := range []bool{true, false} {
				pair := controlDiagnosticPair(t)
				b := controlDiagnosticFixture()
				if c.setup != nil {
					c.setup(b)
				}
				if err := sendControl(pair[0], []byte(c.raw), nil); err != nil {
					t.Fatal(err)
				}
				d := brokerControlDiagnostic{operation: 13, command: 8, first: 28, returned: 28, drop: 2}
				var target *brokerControlDiagnostic
				if sink {
					target = &d
				}
				pending, err := b.dispatchBrokerControl(pair[1], target)
				expected := errControlFrame
				if c.point == 6 {
					expected = errGuardProtocol
				}
				if pending || err != expected {
					t.Fatalf("pending=%v err=%v, want %v", pending, err, expected)
				}
				if sink && (d.operation != c.op || d.command != c.cmd || d.first != c.point || d.returned != c.point || d.drop != 0) {
					t.Fatalf("diagnostic %+v", d)
				}
				if c.point == 6 && (!b.checkpoint.faulted || b.runtimeRootState != runtimeRootFailed) {
					t.Fatal("root fault missing")
				}
			}
		})
	}
	t.Run("denial ack and send failure", func(t *testing.T) {
		raw := []byte(`{"operation":"READ_STATUS","principal_id":9,"request_id":1}`)
		for _, closePeer := range []bool{false, true} {
			pair := controlDiagnosticPair(t)
			b := controlDiagnosticFixture()
			b.runtimeRootState = runtimeRootReady
			if err := sendControl(pair[0], raw, nil); err != nil {
				t.Fatal(err)
			}
			if closePeer {
				senderFD := pair[0]
				pair[0] = -1
				if err := unix.Close(senderFD); err != nil {
					t.Fatal(err)
				}
			}
			var d brokerControlDiagnostic
			pending, err := b.dispatchBrokerControl(pair[1], &d)
			if pending {
				t.Fatal("unexpected pending")
			}
			if closePeer {
				if !errors.Is(err, unix.EPIPE) || d.operation != 12 || d.first != 12 || d.returned != 13 {
					t.Fatalf("%v %+v", err, d)
				}
				continue
			}
			if err != nil || d != (brokerControlDiagnostic{}) {
				t.Fatalf("%v %+v", err, d)
			}
			buf := make([]byte, 4096)
			n, _, err := unix.Recvfrom(pair[0], buf, 0)
			if err != nil {
				t.Fatal(err)
			}
			var ack map[string]any
			if err := json.Unmarshal(buf[:n], &ack); err != nil {
				t.Fatal(err)
			}
			if ack["version"] != float64(1) || ack["status"] != "BPF_AUTH_DENIED" || ack["principal_id"] != float64(9) || ack["request_id"] != float64(1) || ack["request_sha256"] != hashBytes(raw) {
				t.Fatalf("ack %v", ack)
			}
		}
	})
	t.Run("sink reused", func(t *testing.T) {
		b := controlDiagnosticFixture()
		d := brokerControlDiagnostic{operation: 13, command: 8, first: 28, returned: 28, drop: 2}
		for _, raw := range []string{`{"operation":"SETTLE"}`, `{}`} {
			pair := controlDiagnosticPair(t)
			if err := sendControl(pair[0], []byte(raw), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := b.dispatchBrokerControl(pair[1], &d); err == nil {
				t.Fatal("accepted")
			}
		}
		if d != (brokerControlDiagnostic{first: 3, returned: 3}) {
			t.Fatalf("stale %+v", d)
		}
	})
}

func TestBrokerControlTaskCleanupDiagnostic(t *testing.T) {
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := &authorityBroker{authority: &sealedAuthority{guard: guardDevice{fd: int(f.Fd())}}, checkpoint: &checkpointController{}}
	var d brokerControlDiagnostic
	held := true
	err = b.finishBrokerControlTask(guardTask{Cookie: 1}, &held, &d)
	if !errors.Is(err, unix.ENOTTY) || held || d.drop != 2 || !b.checkpoint.invalid || !b.checkpoint.faulted {
		t.Fatalf("%v %+v", err, d)
	}
	if _, e := f.Stat(); e != nil {
		t.Fatal(e)
	}
	if e := b.finishBrokerControlTask(guardTask{Cookie: 1}, &held, &d); e != nil || d.drop != 2 {
		t.Fatalf("repeat %v", e)
	}
	prior := errors.New("prior")
	if d.afterTaskDrop(prior, err) != prior || d.afterTaskDrop(nil, err) != err || d.first != 24 || d.returned != 24 {
		t.Fatal("error precedence")
	}
	held = true
	if e := b.finishBrokerControlTask(guardTask{Cookie: 1}, &held, nil); !errors.Is(e, unix.ENOTTY) || held {
		t.Fatalf("nil sink %v", e)
	}
}

func TestBrokerControlTerminalDiagnostic(t *testing.T) {
	original := g2d1
	defer func() { g2d1 = original }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	beforeStatus, e := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
	if e != nil {
		t.Fatal(e)
	}
	beforeFD, e := unix.FcntlInt(w.Fd(), unix.F_GETFD, 0)
	if e != nil {
		t.Fatal(e)
	}
	reset := func() {
		g2d1 = original
		g2d1.fd = int(w.Fd())
		g2d1.role = 'B'
		g2d1.owned = false
		g2d1.failed = false
		g2d1.captured = false
		g2d1.count = 0
		g2d1.closedAfterACK = false
	}
	read := func() string {
		buf := make([]byte, 128)
		n, e := unix.Read(int(r.Fd()), buf)
		if e != nil {
			t.Fatal(e)
		}
		return string(buf[:n])
	}
	reset()
	pair := controlDiagnosticPair(t)
	b := controlDiagnosticFixture()
	if err := sendControl(pair[0], []byte(`{"operation":"SETTLE"}`), nil); err != nil {
		t.Fatal(err)
	}
	var d brokerControlDiagnostic
	_, got := b.dispatchBrokerControl(pair[1], &d)
	if got == nil || d.first != 5 {
		t.Fatalf("%v %+v", got, d)
	}
	if g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, got, got) != got {
		t.Fatal("return changed")
	}
	p := []unix.PollFd{{Fd: int32(r.Fd()), Events: unix.POLLIN}}
	if n, e := unix.Poll(p, 0); e != nil || n != 0 {
		t.Fatalf("premature output %d %v", n, e)
	}
	d.first = 28
	g2d1CaptureBrokerControl(&d, brokerControlRouteJoinReport, unix.EIO, unix.EIO)
	g2d1TerminalFail(got)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O05_C00_P05_R05_D0_T2_B00_S0_E0_A00_G00 CHECK\n" {
		t.Fatalf("line %q", line)
	}
	if g2d1.capturedControl != (brokerControlDiagnostic{}) || g2d1.capturedControlRoute != 0 {
		t.Fatal("metadata retained")
	}
	reset()
	bindBroker := controlDiagnosticFixture()
	bindBroker.checkpoint.markFault()
	var bind brokerBindDiagnostic
	_, _, bindErr := bindBroker.bindRuntimeRequest(-1, nil, nil, nil, nil, &bind)
	if bindErr != errControlFrame || bind != (brokerBindDiagnostic{point: 3, before: 4, after: 4}) {
		t.Fatalf("bind: %v %+v", bindErr, bind)
	}
	d = brokerControlDiagnostic{operation: 2, command: 4, first: 11, returned: 11, bind: bind}
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, bindErr, bindErr)
	if n, e := unix.Poll(p, 0); e != nil || n != 0 {
		t.Fatalf("premature bind output %d %v", n, e)
	}
	d.bind.point = 1
	g2d1CaptureBrokerControl(&d, brokerControlRouteJoinReport, unix.EIO, unix.EIO)
	g2d1TerminalFail(bindErr)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O02_C04_P0B_R0B_D0_T2_B03_S4_E4_A00_G00 CHECK\n" {
		t.Fatalf("bind line %q", line)
	}
	if g2d1.capturedControl != (brokerControlDiagnostic{}) {
		t.Fatal("bind metadata retained")
	}
	reset()
	deleteBroker := controlDiagnosticFixture()
	deleteBroker.checkpoint.lifecycle = &checkpointLifecycle{}
	var deleteBind brokerBindDiagnostic
	deleteBind.begin(deleteBroker)
	deleteErr := deleteBroker.authorizeBoundDelete(nil, guardTask{}, -1, 0, &deleteBind)
	if deleteErr != errGuardProtocol || deleteBind.deletePoint != 2 {
		t.Fatalf("delete: %v %+v", deleteErr, deleteBind)
	}
	deleteBroker.checkpoint.markFault()
	_ = deleteBind.reject(brokerBindPointDeleteAuthorize, deleteErr)
	deleteBind.finish(deleteBroker, deleteErr)
	d = brokerControlDiagnostic{operation: 2, command: 4, first: 11, returned: 11, bind: deleteBind}
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, deleteErr, deleteErr)
	if n, e := unix.Poll(p, 0); e != nil || n != 0 {
		t.Fatalf("premature delete output %d %v", n, e)
	}
	d.bind.deletePoint = 1
	g2d1CaptureBrokerControl(&d, brokerControlRouteJoinReport, unix.EIO, unix.EIO)
	g2d1TerminalFail(deleteErr)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O02_C04_P0B_R0B_D0_T2_B3F_S1_E4_A02_G00 CHECK\n" {
		t.Fatalf("delete line %q", line)
	}
	if g2d1.capturedControl != (brokerControlDiagnostic{}) {
		t.Fatal("delete metadata retained")
	}
	reset()
	rootTestPinProc(t)
	pid := uint32(os.Getpid())
	if pid <= 1 {
		t.Fatal("self PID is not a process leader")
	}
	pidfd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	targetBroker := controlDiagnosticFixture()
	targetBroker.checkpoint.lifecycle = &checkpointLifecycle{}
	grant := &brokerGrant{id: 1}
	group := &liveCgroup{GrantID: 1, RootCookie: 2, KernfsID: 3, Enrollment: 4, HeldFD: int(r.Fd())}
	targetBroker.effects = &runtimeEffects{cgroups: map[uint64]*liveCgroup{1: group}}
	var targetBind brokerBindDiagnostic
	targetBind.begin(targetBroker)
	targetErr := targetBroker.authorizeBoundDelete(grant, guardTask{TID: pid, TGID: pid, StartBoottimeNS: 1}, pidfd, 1, &targetBind)
	if targetErr != errGuardProtocol || targetBind.deletePoint != 10 || targetBind.targetPoint != 3 {
		t.Fatalf("target rejection: %v %+v", targetErr, targetBind)
	}
	targetBroker.checkpoint.markFault()
	_ = targetBind.reject(brokerBindPointDeleteAuthorize, targetErr)
	targetBind.finish(targetBroker, targetErr)
	d = brokerControlDiagnostic{operation: 2, command: 4, first: 11, returned: 11, bind: targetBind}
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, targetErr, targetErr)
	if n, e := unix.Poll(p, 0); e != nil || n != 0 {
		t.Fatalf("premature target output %d %v", n, e)
	}
	d.bind.targetPoint = 1
	g2d1CaptureBrokerControl(&d, brokerControlRouteJoinReport, unix.EIO, unix.EIO)
	g2d1TerminalFail(targetErr)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O02_C04_P0B_R0B_D0_T2_B3F_S1_E4_A0A_G03 CHECK\n" {
		t.Fatalf("target line %q", line)
	}
	if g2d1.capturedControl != (brokerControlDiagnostic{}) {
		t.Fatal("target metadata retained")
	}
	reset()
	d = brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 25}}
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, unix.EACCES, unix.EACCES)
	g2d1TerminalFail(unix.EACCES)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O02_C04_P00_R00_D0_T2_B3F_S0_E0_A19_G00 ERRNO=13\n" {
		t.Fatalf("delete errno line %q", line)
	}
	reset()
	prior := errors.New("report")
	d = brokerControlDiagnostic{operation: 12, first: 12, returned: 24, drop: 2}
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	dropBroker := &authorityBroker{authority: &sealedAuthority{guard: guardDevice{fd: int(null.Fd())}}, checkpoint: &checkpointController{}}
	held := true
	dropErr := dropBroker.finishBrokerControlTask(guardTask{Cookie: 1}, &held, nil)
	if !errors.Is(dropErr, unix.ENOTTY) {
		t.Fatalf("drop: %v", dropErr)
	}
	if g2d1CaptureBrokerControl(&d, brokerControlRouteJoinReport, prior, dropErr) != prior {
		t.Fatal("returned error changed")
	}
	g2d1TerminalFail(prior)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O0C_C00_P0C_R18_D2_T1_B00_S0_E0_A00_G00 ERRNO=25\n" {
		t.Fatalf("line %q", line)
	}
	reset()
	if g2d1CaptureBrokerControl(nil, brokerControlRouteJoinReport, nil, unix.EIO) != nil || g2d1.captured {
		t.Fatal("nil captured")
	}
	if string(g2d1AppendControlStage(nil, brokerControlDiagnostic{}, 0)) != "INVALID_STAGE" {
		t.Fatal("invalid route accepted")
	}
	stage := g2d1AppendControlStage(nil, brokerControlDiagnostic{operation: 2, command: 8, first: 28, returned: 28, drop: 2}, brokerControlRouteNewInvalid)
	if len(stage) != 54 || len(stage)+len("G2D1 B ")+len(" ERRNO=18446744073709551615\n") > 128 {
		t.Fatalf("stage too long %q", stage)
	}
	g2d1CaptureBrokerControl(nil, brokerControlRouteJoinReport, errGuardProtocol, nil)
	g2d1TerminalFail(errGuardProtocol)
	if line := read(); line != "G2D1 B BROKER_CONTROL_O00_C00_P00_R00_D0_T1_B00_S0_E0_A00_G00 CHECK\n" {
		t.Fatalf("line %q", line)
	}
	for _, invalid := range []brokerControlDiagnostic{{operation: 14}, {operation: 1, command: 1}, {first: 29}, {drop: 3}, {operation: 2, bind: brokerBindDiagnostic{point: 66}}, {operation: 2, bind: brokerBindDiagnostic{point: 1, before: 5}}, {operation: 2, bind: brokerBindDiagnostic{point: 1, after: 5}}, {operation: 2, bind: brokerBindDiagnostic{before: 1}}, {operation: 1, bind: brokerBindDiagnostic{point: 3}}} {
		if string(g2d1AppendControlStage(nil, invalid, brokerControlRouteFaulted)) != "INVALID_STAGE" {
			t.Fatal("invalid stage accepted")
		}
	}
	for _, invalid := range []brokerControlDiagnostic{
		{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 26}},
		{operation: 1, bind: brokerBindDiagnostic{point: 63, deletePoint: 2}},
		{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 1, deletePoint: 2}},
		{operation: 2, command: 1, bind: brokerBindDiagnostic{point: 63, deletePoint: 2}},
		{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 10, targetPoint: 17}},
		{operation: 1, bind: brokerBindDiagnostic{point: 63, deletePoint: 10, targetPoint: 1}},
		{operation: 2, command: 1, bind: brokerBindDiagnostic{point: 63, deletePoint: 10, targetPoint: 1}},
		{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 1, deletePoint: 10, targetPoint: 1}},
		{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 11, targetPoint: 1}},
		{operation: 2, command: 4, bind: brokerBindDiagnostic{targetPoint: 1}},
	} {
		if string(g2d1AppendControlStage(nil, invalid, brokerControlRouteFaulted)) != "INVALID_STAGE" {
			t.Fatal("invalid delete stage accepted")
		}
	}
	for _, c := range []struct {
		d    brokerControlDiagnostic
		want string
	}{
		{brokerControlDiagnostic{operation: 2, bind: brokerBindDiagnostic{point: 1}}, "BROKER_CONTROL_O02_C00_P00_R00_D0_T2_B01_S0_E0_A00_G00"},
		{brokerControlDiagnostic{operation: 2, bind: brokerBindDiagnostic{point: 65, before: 4, after: 4}}, "BROKER_CONTROL_O02_C00_P00_R00_D0_T2_B41_S4_E4_A00_G00"},
		{brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 1}}, "BROKER_CONTROL_O02_C04_P00_R00_D0_T2_B3F_S0_E0_A01_G00"},
		{brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 25}}, "BROKER_CONTROL_O02_C04_P00_R00_D0_T2_B3F_S0_E0_A19_G00"},
		{brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 10, targetPoint: 1}}, "BROKER_CONTROL_O02_C04_P00_R00_D0_T2_B3F_S0_E0_A0A_G01"},
		{brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, deletePoint: 10, targetPoint: 16}}, "BROKER_CONTROL_O02_C04_P00_R00_D0_T2_B3F_S0_E0_A0A_G10"},
	} {
		if got := string(g2d1AppendControlStage(nil, c.d, brokerControlRouteFaulted)); got != c.want {
			t.Fatalf("stage %q want %q", got, c.want)
		}
	}
	reset()
	invalid := brokerControlDiagnostic{operation: brokerControlOpRead, command: brokerControlCmdCreate}
	g2d1CaptureBrokerControl(&invalid, brokerControlRouteFaulted, unix.EACCES, unix.EACCES)
	g2d1TerminalFail(unix.EACCES)
	if line := read(); line != "G2D1 B INVALID_STAGE ERRNO=13\n" {
		t.Fatalf("line %q", line)
	}
	reset()
	g2d1.capturedControl = d
	g2d1.capturedControlRoute = brokerControlRouteFaulted
	g2d1CaptureFailure("GENERIC", errGuardProtocol)
	if g2d1.capturedControl != (brokerControlDiagnostic{}) || g2d1.capturedControlRoute != 0 {
		t.Fatal("generic capture retained control")
	}
	g2d1TerminalFail(errGuardProtocol)
	if line := read(); line != "G2D1 B GENERIC CHECK\n" {
		t.Fatalf("line %q", line)
	}
	reset()
	g2d1.count = 24
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, prior, prior)
	if g2d1.captured {
		t.Fatal("limit ignored")
	}
	reset()
	g2d1CaptureFailure("EARLIER", errGuardProtocol)
	g2d1CaptureBrokerControl(&d, brokerControlRouteFaulted, prior, prior)
	g2d1TerminalFail(prior)
	if line := read(); line != "G2D1 B EARLIER CHECK\n" {
		t.Fatalf("line %q", line)
	}
	afterStatus, e := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
	if e != nil {
		t.Fatal(e)
	}
	afterFD, e := unix.FcntlInt(w.Fd(), unix.F_GETFD, 0)
	if e != nil || afterStatus != beforeStatus || afterFD != beforeFD {
		t.Fatal("borrowed descriptor changed")
	}
}

func TestBrokerDeleteAuthorizationDiagnostic(t *testing.T) {
	rootTestPinProc(t)
	pid := uint32(os.Getpid())
	if pid <= 1 {
		t.Fatal("self PID is not a process leader")
	}
	pidfd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(pidfd) })
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = null.Close() })
	type fixture struct {
		b          *authorityBroker
		grant      *brokerGrant
		task       guardTask
		pidfd      int
		invocation uint64
		group      *liveCgroup
		target     *effectRegistryTarget
	}
	newFixture := func() *fixture {
		b := controlDiagnosticFixture()
		b.checkpoint.lifecycle = &checkpointLifecycle{}
		b.effects = &runtimeEffects{cgroups: map[uint64]*liveCgroup{}}
		grant := &brokerGrant{id: 1}
		group := &liveCgroup{GrantID: 1, RootCookie: 2, KernfsID: 3, Enrollment: 4, HeldFD: int(null.Fd())}
		target := &effectRegistryTarget{group: group, grant: grant, phase: 2, tasks: map[effectRegistryTaskKey]*effectRegistryTask{}}
		return &fixture{b: b, grant: grant, task: guardTask{TGID: pid, TID: pid, StartBoottimeNS: 1}, pidfd: pidfd, invocation: 1, group: group, target: target}
	}
	withTarget := func(f *fixture) {
		f.b.effects.cgroups[1] = f.group
		f.b.effects.registry = &effectRegistry{targets: map[cgroupKey]*effectRegistryTarget{{root: 2, id: 3}: f.target}}
	}
	type testCase struct {
		name       string
		want       brokerDeletePoint
		wantTarget brokerTargetPoint
		success    bool
		setup      func(*fixture)
	}
	cases := []testCase{
		{"broker-nil", 1, 0, false, func(f *fixture) { f.b = nil }},
		{"effects-nil", 2, 0, false, func(f *fixture) { f.b.effects = nil }},
		{"checkpoint-nil", 3, 0, false, func(f *fixture) { f.b.checkpoint = nil }},
		{"lifecycle-nil", 4, 0, false, func(f *fixture) { f.b.checkpoint.lifecycle = nil }},
		{"grant-nil", 5, 0, false, func(f *fixture) { f.grant = nil }},
		{"invocation-zero", 6, 0, false, func(f *fixture) { f.invocation = 0 }},
		{"not-leader", 7, 0, false, func(f *fixture) { f.task.TID++ }},
		{"tgid", 8, 0, false, func(f *fixture) { f.task.TID, f.task.TGID = 1, 1 }},
		{"pidfd", 9, 0, false, func(f *fixture) { f.pidfd = -1 }},
		{"effects-before-checkpoint", 2, 0, false, func(f *fixture) { f.b.effects, f.b.checkpoint = nil, nil }},
		{"invocation-before-leader", 6, 0, false, func(f *fixture) { f.invocation = 0; f.task.TID++ }},
		{"missing-group", 0, 0, true, func(f *fixture) {}},
		{"retired-group", 0, 0, true, func(f *fixture) { f.group.Retired = true; f.b.effects.cgroups[1] = f.group }},
		{"registry-nil", 10, 3, false, func(f *fixture) { f.b.effects.cgroups[1] = f.group }},
		{"missing-target", 10, 13, false, func(f *fixture) { withTarget(f); delete(f.b.effects.registry.targets, cgroupKey{root: 2, id: 3}) }},
		{"wrong-grant", 10, 8, false, func(f *fixture) { withTarget(f); f.group.GrantID = 9 }},
		{"phase-four", 10, 16, false, func(f *fixture) { withTarget(f); f.target.phase = 4 }},
		{"group-alias", 10, 14, false, func(f *fixture) { withTarget(f); clone := *f.group; f.target.group = &clone }},
		{"grant-alias", 10, 15, false, func(f *fixture) { withTarget(f); clone := *f.grant; f.target.grant = &clone }},
		{"held-negative", 10, 6, false, func(f *fixture) { withTarget(f); f.group.HeldFD = -1 }},
		{"enrollment-zero", 10, 9, false, func(f *fixture) { withTarget(f); f.group.Enrollment = 0 }},
		{"root-zero", 10, 10, false, func(f *fixture) { withTarget(f); f.group.RootCookie = 0 }},
		{"kernfs-zero", 10, 11, false, func(f *fixture) { withTarget(f); f.group.KernfsID = 0 }},
		{"phase-zero", 11, 0, false, func(f *fixture) { withTarget(f); f.target.phase = 0 }},
		{"phase-one", 11, 0, false, func(f *fixture) { withTarget(f); f.target.phase = 1 }},
		{"phase-before-offline", 11, 0, false, func(f *fixture) { withTarget(f); f.target.phase = 1; f.target.offlineSequence = 1 }},
		{"offline", 12, 0, false, func(f *fixture) { withTarget(f); f.target.offlineSequence = 1 }},
		{"phase-two-no-lineage", 13, 0, false, func(f *fixture) { withTarget(f) }},
		{"phase-three-no-lineage", 13, 0, false, func(f *fixture) { withTarget(f); f.target.phase = 3 }},
		{"empty-lineage", 23, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{}}
		}},
		{"other-executable", 23, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{{}: {role: "other"}}}
		}},
		{"runc-file-nil", 15, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{{}: {role: "runc", digest: runtimeRuncSHA256}}}
		}},
		{"runc-file-before-digest", 15, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{{}: {role: "runc", digest: "wrong"}}}
		}},
		{"runc-registered-digest", 16, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{{}: {role: "runc", file: null, digest: "wrong"}}}
		}},
		{"runc-nonregular", 17, 0, false, func(f *fixture) {
			withTarget(f)
			f.b.checkpoint.lifecycle.lineage = &daemonLineage{executables: map[executableKey]lineageExecutable{{}: {role: "runc", file: null, digest: runtimeRuncSHA256}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, sink := range []bool{true, false} {
				f := newFixture()
				tc.setup(f)
				groupBefore, targetBefore := *f.group, *f.target
				taskBefore := f.task
				var grantBefore brokerGrant
				if f.grant != nil {
					grantBefore = *f.grant
				}
				var invalid, faulted bool
				if f.b != nil && f.b.checkpoint != nil {
					invalid, faulted = f.b.checkpoint.invalid, f.b.checkpoint.faulted
				}
				var pidStat, nullStat unix.Stat_t
				if err := unix.Fstat(pidfd, &pidStat); err != nil {
					t.Fatal(err)
				}
				if err := unix.Fstat(int(null.Fd()), &nullStat); err != nil {
					t.Fatal(err)
				}
				pidFlags, err := unix.FcntlInt(uintptr(pidfd), unix.F_GETFD, 0)
				if err != nil {
					t.Fatal(err)
				}
				nullFlags, err := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
				if err != nil {
					t.Fatal(err)
				}
				pidStatus, err := unix.FcntlInt(uintptr(pidfd), unix.F_GETFL, 0)
				if err != nil {
					t.Fatal(err)
				}
				nullStatus, err := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
				if err != nil {
					t.Fatal(err)
				}
				d := brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: 25, targetPoint: 16}
				var ptr *brokerBindDiagnostic
				if sink {
					ptr = &d
				}
				err = f.b.authorizeBoundDelete(f.grant, f.task, f.pidfd, f.invocation, ptr)
				if (tc.success && err != nil) || (!tc.success && err != errGuardProtocol) {
					t.Fatalf("sink=%v error=%v", sink, err)
				}
				if sink && d != (brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: tc.want, targetPoint: tc.wantTarget}) {
					t.Fatalf("diagnostic %+v", d)
				}
				if *f.group != groupBefore || !reflect.DeepEqual(*f.target, targetBefore) || len(f.target.tasks) != 0 || f.task != taskBefore || (f.grant != nil && !reflect.DeepEqual(*f.grant, grantBefore)) {
					t.Fatal("fixture mutated")
				}
				if f.b != nil && f.b.checkpoint != nil && (f.b.checkpoint.invalid != invalid || f.b.checkpoint.faulted != faulted) {
					t.Fatal("checkpoint mutated")
				}
				var pidAfter, nullAfter unix.Stat_t
				if unix.Fstat(pidfd, &pidAfter) != nil || unix.Fstat(int(null.Fd()), &nullAfter) != nil || pidAfter.Dev != pidStat.Dev || pidAfter.Ino != pidStat.Ino || pidAfter.Mode != pidStat.Mode || nullAfter.Dev != nullStat.Dev || nullAfter.Ino != nullStat.Ino || nullAfter.Mode != nullStat.Mode {
					t.Fatal("borrowed FD changed")
				}
				pf, pe := unix.FcntlInt(uintptr(pidfd), unix.F_GETFD, 0)
				nf, ne := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
				if pe != nil || ne != nil || pf != pidFlags || nf != nullFlags {
					t.Fatal("descriptor flags changed")
				}
				ps, pse := unix.FcntlInt(uintptr(pidfd), unix.F_GETFL, 0)
				ns, nse := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
				if pse != nil || nse != nil || ps != pidStatus || ns != nullStatus {
					t.Fatal("descriptor status changed")
				}
			}
		})
	}
	t.Run("captured-value-survives-success", func(t *testing.T) {
		original := g2d1
		defer func() { g2d1 = original }()
		g2d1.fd, g2d1.failed, g2d1.captured, g2d1.count = -1, false, false, 0
		f := newFixture()
		f.b.effects.cgroups[1] = f.group
		working := brokerControlDiagnostic{operation: 2, command: 4, bind: brokerBindDiagnostic{point: 63, before: 1, after: 4}}
		err := f.b.authorizeBoundDelete(f.grant, f.task, f.pidfd, f.invocation, &working.bind)
		if err != errGuardProtocol || working.bind.deletePoint != 10 || working.bind.targetPoint != 3 {
			t.Fatal("callee rejection")
		}
		g2d1CaptureBrokerControl(&working, brokerControlRouteFaulted, err, err)
		entry := newFixture()
		entry.b.effects = nil
		if entry.b.authorizeBoundDelete(entry.grant, entry.task, entry.pidfd, entry.invocation, &working.bind) != errGuardProtocol || working.bind.deletePoint != 2 || working.bind.targetPoint != 0 {
			t.Fatal("entry reset")
		}
		success := newFixture()
		if success.b.authorizeBoundDelete(success.grant, success.task, success.pidfd, success.invocation, &working.bind) != nil || working.bind.deletePoint != 0 || working.bind.targetPoint != 0 {
			t.Fatal("missing group reset")
		}
		retired := newFixture()
		retired.group.Retired = true
		retired.b.effects.cgroups[1] = retired.group
		if retired.b.authorizeBoundDelete(retired.grant, retired.task, retired.pidfd, retired.invocation, &working.bind) != nil || working.bind.deletePoint != 0 || working.bind.targetPoint != 0 {
			t.Fatal("retired group reset")
		}
		if g2d1.capturedControl.bind.deletePoint != 10 || g2d1.capturedControl.bind.targetPoint != 3 {
			t.Fatal("capture alias")
		}
		working.bind.finish(success.b, nil)
		if working.bind != (brokerBindDiagnostic{}) || g2d1.capturedControl.bind.deletePoint != 10 || g2d1.capturedControl.bind.targetPoint != 3 {
			t.Fatal("success reset")
		}
	})
}

func TestBrokerEffectTargetDiagnostic(t *testing.T) {
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = null.Close() })
	type fixture struct {
		b        *authorityBroker
		group    *liveCgroup
		grant    *brokerGrant
		target   *effectRegistryTarget
		registry *effectRegistry
		groupArg *liveCgroup
		grantArg *brokerGrant
	}
	newFixture := func() *fixture {
		b := controlDiagnosticFixture()
		g := &liveCgroup{GrantID: 1, RootCookie: 2, KernfsID: 3, Enrollment: 4, HeldFD: int(null.Fd())}
		grant := &brokerGrant{id: 1}
		key := effectRegistryTaskKey{tgid: 2, start: 3}
		target := &effectRegistryTarget{group: g, grant: grant, phase: 2, tasks: map[effectRegistryTaskKey]*effectRegistryTask{key: {key: key, invocation: 4, dev: 5, ino: 6, sequence: 7}}}
		registry := &effectRegistry{targets: map[cgroupKey]*effectRegistryTarget{{root: 2, id: 3}: target}, taskCount: 1}
		b.effects = &runtimeEffects{registry: registry}
		return &fixture{b: b, group: g, grant: grant, target: target, registry: registry, groupArg: g, grantArg: grant}
	}
	type testCase struct {
		name   string
		want   brokerTargetPoint
		mutate func(*fixture)
	}
	cases := []testCase{
		{"broker-nil", 1, func(f *fixture) { f.b = nil }},
		{"effects-nil", 2, func(f *fixture) { f.b.effects = nil }},
		{"registry-nil", 3, func(f *fixture) { f.b.effects.registry = nil }},
		{"group-nil", 4, func(f *fixture) { f.groupArg = nil }},
		{"grant-nil", 5, func(f *fixture) { f.grantArg = nil }},
		{"held-fd", 6, func(f *fixture) { f.group.HeldFD = -1 }},
		{"retired", 7, func(f *fixture) { f.group.Retired = true }},
		{"group-grant-id", 8, func(f *fixture) { f.group.GrantID = 9 }},
		{"enrollment", 9, func(f *fixture) { f.group.Enrollment = 0 }},
		{"root-cookie", 10, func(f *fixture) { f.group.RootCookie = 0 }},
		{"kernfs-id", 11, func(f *fixture) { f.group.KernfsID = 0 }},
		{"grant-zero", 12, func(f *fixture) { f.grant.id = 0; f.group.GrantID = 0 }},
		{"target-missing", 13, func(f *fixture) { delete(f.registry.targets, cgroupKey{root: 2, id: 3}) }},
		{"group-identity", 14, func(f *fixture) { clone := *f.group; f.target.group = &clone }},
		{"grant-identity", 15, func(f *fixture) { clone := *f.grant; f.target.grant = &clone }},
		{"phase-four", 16, func(f *fixture) { f.target.phase = 4 }},
		{"nil-target-map", 13, func(f *fixture) { f.registry.targets = nil }},
		{"nil-target-value", 13, func(f *fixture) { f.registry.targets[cgroupKey{root: 2, id: 3}] = nil }},
		{"registry-before-group", 3, func(f *fixture) { f.b.effects.registry = nil; f.groupArg = nil }},
		{"fd-before-retired", 6, func(f *fixture) { f.group.HeldFD = -1; f.group.Retired = true }},
		{"mismatch-before-zero", 8, func(f *fixture) { f.group.GrantID = 9; f.grant.id = 0 }},
		{"group-before-grant-and-phase", 14, func(f *fixture) {
			g := *f.group
			a := *f.grant
			f.target.group = &g
			f.target.grant = &a
			f.target.phase = 4
		}},
		{"grant-before-phase", 15, func(f *fixture) { a := *f.grant; f.target.grant = &a; f.target.phase = 4 }},
		{"phase-zero", 0, func(f *fixture) { f.target.phase = 0 }},
		{"phase-one", 0, func(f *fixture) { f.target.phase = 1 }},
		{"phase-two", 0, func(f *fixture) {}},
		{"phase-three", 0, func(f *fixture) { f.target.phase = 3 }},
		{"phase-five", 0, func(f *fixture) { f.target.phase = 5 }},
		{"nil-tasks", 0, func(f *fixture) { f.target.tasks = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, sink := range []bool{true, false} {
				f := newFixture()
				tc.mutate(f)
				groupBefore, grantBefore, targetBefore := *f.group, *f.grant, *f.target
				groupArgBefore, grantArgBefore := f.groupArg, f.grantArg
				var effectsBefore *runtimeEffects
				var registryBefore *effectRegistry
				var checkpointBefore *checkpointController
				if f.b != nil {
					effectsBefore, checkpointBefore = f.b.effects, f.b.checkpoint
					if effectsBefore != nil {
						registryBefore = effectsBefore.registry
					}
				}
				var aliasGroup liveCgroup
				var aliasGrant brokerGrant
				if f.target.group != nil {
					aliasGroup = *f.target.group
				}
				if f.target.grant != nil {
					aliasGrant = *f.target.grant
				}
				entries := make(map[cgroupKey]*effectRegistryTarget)
				for k, v := range f.registry.targets {
					entries[k] = v
				}
				tasks := make(map[effectRegistryTaskKey]effectRegistryTask)
				taskPointers := make(map[effectRegistryTaskKey]*effectRegistryTask)
				for k, v := range f.target.tasks {
					tasks[k] = *v
					taskPointers[k] = v
				}
				targetsNil, tasksNil, count := f.registry.targets == nil, f.target.tasks == nil, f.registry.taskCount
				var invalid, faulted bool
				if f.b != nil {
					invalid, faulted = f.b.checkpoint.invalid, f.b.checkpoint.faulted
				}
				var before unix.Stat_t
				if err := unix.Fstat(int(null.Fd()), &before); err != nil {
					t.Fatal(err)
				}
				fdFlags, err := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
				if err != nil {
					t.Fatal(err)
				}
				statusFlags, err := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
				if err != nil {
					t.Fatal(err)
				}
				d := brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: 10, targetPoint: 16}
				var ptr *brokerBindDiagnostic
				if sink {
					ptr = &d
				}
				got, err := f.b.effectRegistryTarget(f.groupArg, f.grantArg, ptr)
				if tc.want == 0 {
					if got != f.target || err != nil {
						t.Fatalf("success %p %v", got, err)
					}
				} else if got != nil || err != errGuardProtocol {
					t.Fatalf("rejection %p %v", got, err)
				}
				if sink && d != (brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: 10, targetPoint: tc.want}) {
					t.Fatalf("diagnostic %+v", d)
				}
				if !sink && d != (brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: 10, targetPoint: 16}) {
					t.Fatal("nil sink modified record")
				}
				if !reflect.DeepEqual(*f.group, groupBefore) || !reflect.DeepEqual(*f.grant, grantBefore) || !reflect.DeepEqual(*f.target, targetBefore) || (f.target.group != nil && !reflect.DeepEqual(*f.target.group, aliasGroup)) || (f.target.grant != nil && !reflect.DeepEqual(*f.target.grant, aliasGrant)) || f.registry.taskCount != count || (f.registry.targets == nil) != targetsNil || len(f.registry.targets) != len(entries) || (f.target.tasks == nil) != tasksNil || len(f.target.tasks) != len(tasks) {
					t.Fatal("graph changed")
				}
				if f.groupArg != groupArgBefore || f.grantArg != grantArgBefore || f.target.group != targetBefore.group || f.target.grant != targetBefore.grant || (f.b != nil && (f.b.effects != effectsBefore || f.b.checkpoint != checkpointBefore || (f.b.effects != nil && f.b.effects.registry != registryBefore))) {
					t.Fatal("graph identity changed")
				}
				for k, v := range entries {
					got, ok := f.registry.targets[k]
					if !ok || got != v {
						t.Fatal("registry changed")
					}
				}
				for k, v := range tasks {
					got, ok := f.target.tasks[k]
					if !ok || got != taskPointers[k] || got == nil || *got != v {
						t.Fatal("task changed")
					}
				}
				if f.b != nil && (f.b.checkpoint.invalid != invalid || f.b.checkpoint.faulted != faulted) {
					t.Fatal("checkpoint changed")
				}
				var after unix.Stat_t
				fdAfter, fe := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
				statusAfter, se := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
				if unix.Fstat(int(null.Fd()), &after) != nil || after.Dev != before.Dev || after.Ino != before.Ino || after.Mode != before.Mode || fe != nil || se != nil || fdAfter != fdFlags || statusAfter != statusFlags {
					t.Fatal("borrowed FD changed")
				}
			}
		})
	}
	t.Run("working-sink-reuse", func(t *testing.T) {
		d := brokerBindDiagnostic{point: 63, before: 1, after: 4, deletePoint: 10}
		f := newFixture()
		f.b.effects.registry = nil
		_, err := f.b.effectRegistryTarget(f.group, f.grant, &d)
		if err != errGuardProtocol || d.targetPoint != 3 {
			t.Fatal("first rejection")
		}
		f = newFixture()
		got, err := f.b.effectRegistryTarget(f.group, f.grant, &d)
		if err != nil || got != f.target || d.targetPoint != 0 {
			t.Fatal("success reset")
		}
		f = newFixture()
		alias := *f.grant
		f.target.grant = &alias
		_, err = f.b.effectRegistryTarget(f.group, f.grant, &d)
		if err != errGuardProtocol || d.targetPoint != 15 {
			t.Fatal("second rejection")
		}
	})
}

func TestBrokerOfflineDeleteLifecycle(t *testing.T) {
	rootTestPinProc(t)
	pid := uint32(os.Getpid())
	if pid <= 1 {
		t.Fatal("self PID is not a process leader")
	}
	pidfd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(pidfd) })
	null, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = null.Close() })

	type fixture struct {
		b      *authorityBroker
		grant  *brokerGrant
		group  *liveCgroup
		target *effectRegistryTarget
		task   guardTask
	}
	newFixture := func(phase, retirement uint32) *fixture {
		b := controlDiagnosticFixture()
		b.checkpoint.lifecycle = &checkpointLifecycle{}
		grant := &brokerGrant{id: 1}
		group := &liveCgroup{GrantID: 1, RootCookie: 2, KernfsID: 3, Enrollment: 4, HeldFD: int(null.Fd())}
		key := effectRegistryTaskKey{tgid: 7, start: 8}
		target := &effectRegistryTarget{group: group, grant: grant, phase: phase, retirement: retirement,
			tasks: map[effectRegistryTaskKey]*effectRegistryTask{key: {key: key, invocation: 9, dev: 10, ino: 11, sequence: 12}}}
		b.effects = &runtimeEffects{cgroups: map[uint64]*liveCgroup{1: group}, registry: &effectRegistry{
			targets: map[cgroupKey]*effectRegistryTarget{{root: 2, id: 3}: target}, taskCount: 1,
		}}
		return &fixture{b: b, grant: grant, group: group, target: target,
			task: guardTask{TGID: pid, TID: pid, StartBoottimeNS: 1}}
	}
	observe := func(f *fixture, kind uint32, seq, root, node uint64) error {
		record := guardRecord{Kind: kind, Sequence: seq, RootCookie: root, CgroupID: node}
		if kind == 5 || kind == 6 {
			guardPut64(record.Detail[:], 0, seq)
		}
		return f.b.observeEffectCgroup(record)
	}
	// Only the authorization call is required to leave this graph unchanged;
	// observeEffectCgroup has its own existing partial-update semantics on error.
	checkAuthorization := func(t *testing.T, f *fixture, selectedPIDFD int, want brokerDeletePoint, wantTarget brokerTargetPoint, success bool) {
		t.Helper()
		groupBefore, grantBefore, targetBefore := *f.group, *f.grant, *f.target
		checkpointBefore, transcriptBefore := *f.b.checkpoint, *f.b.transcript
		entriesBefore := append([]transcriptEntry(nil), f.b.transcript.entries...)
		tasksBefore := make(map[effectRegistryTaskKey]effectRegistryTask)
		taskPointers := make(map[effectRegistryTaskKey]*effectRegistryTask)
		for k, v := range f.target.tasks {
			tasksBefore[k], taskPointers[k] = *v, v
		}
		registry := f.b.effects.registry
		targetCount, taskCount := len(registry.targets), registry.taskCount
		var pidStat, nullStat unix.Stat_t
		if err := unix.Fstat(pidfd, &pidStat); err != nil {
			t.Fatal(err)
		}
		if err := unix.Fstat(int(null.Fd()), &nullStat); err != nil {
			t.Fatal(err)
		}
		pidFlags, err := unix.FcntlInt(uintptr(pidfd), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		pidStatus, err := unix.FcntlInt(uintptr(pidfd), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		nullFlags, err := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		nullStatus, err := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		d := brokerBindDiagnostic{}
		err = f.b.authorizeBoundDelete(f.grant, f.task, selectedPIDFD, 1, &d)
		if (success && err != nil) || (!success && err != errGuardProtocol) || d.deletePoint != want || d.targetPoint != wantTarget {
			t.Fatalf("authorization: error=%v diagnostic=%+v", err, d)
		}
		if *f.group != groupBefore || !reflect.DeepEqual(*f.grant, grantBefore) || !reflect.DeepEqual(*f.target, targetBefore) ||
			!reflect.DeepEqual(*f.b.checkpoint, checkpointBefore) || f.b.effects.registry != registry ||
			len(registry.targets) != targetCount || registry.targets[cgroupKey{root: 2, id: 3}] != f.target || registry.taskCount != taskCount ||
			len(f.target.tasks) != len(tasksBefore) || f.b.transcript.total != transcriptBefore.total || f.b.transcript.tail != transcriptBefore.tail ||
			len(f.b.transcript.entries) != len(entriesBefore) || !reflect.DeepEqual(f.b.transcript.entries, transcriptBefore.entries) {
			t.Fatal("authorization changed the fixture")
		}
		for k, before := range tasksBefore {
			if f.target.tasks[k] != taskPointers[k] || f.target.tasks[k] == nil || *f.target.tasks[k] != before {
				t.Fatal("authorization changed an existing actor")
			}
		}
		var pidAfter, nullAfter unix.Stat_t
		pf, pe := unix.FcntlInt(uintptr(pidfd), unix.F_GETFD, 0)
		ps, pse := unix.FcntlInt(uintptr(pidfd), unix.F_GETFL, 0)
		nf, ne := unix.FcntlInt(null.Fd(), unix.F_GETFD, 0)
		ns, nse := unix.FcntlInt(null.Fd(), unix.F_GETFL, 0)
		if unix.Fstat(pidfd, &pidAfter) != nil || unix.Fstat(int(null.Fd()), &nullAfter) != nil ||
			pidAfter.Dev != pidStat.Dev || pidAfter.Ino != pidStat.Ino || pidAfter.Mode != pidStat.Mode ||
			nullAfter.Dev != nullStat.Dev || nullAfter.Ino != nullStat.Ino || nullAfter.Mode != nullStat.Mode ||
			pe != nil || pse != nil || ne != nil || nse != nil || pf != pidFlags || ps != pidStatus || nf != nullFlags || ns != nullStatus {
			t.Fatal("authorization changed a borrowed FD")
		}
	}
	for _, c := range []struct {
		name              string
		phase, retirement uint32
	}{
		{"phase2-retirement1", 2, 1}, {"phase2-retirement2", 2, 2},
		{"phase3-retirement1", 3, 1}, {"phase3-retirement2", 3, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(c.phase, c.retirement)
			if err := observe(f, 5, 10, 2, 3); err != nil {
				t.Fatal(err)
			}
			if f.group.OfflineSeq != 10 || f.target.offlineSequence != 10 || f.group.ReleaseSeq != 0 || f.target.releaseSequence != 0 {
				t.Fatal("offline observation not recorded")
			}
			checkAuthorization(t, f, pidfd, 0, 0, true)
			if err := observe(f, 6, 11, 2, 3); err != nil {
				t.Fatal(err)
			}
			if f.group.ReleaseSeq != 11 || f.target.releaseSequence != 11 {
				t.Fatal("release observation not recorded")
			}
			checkAuthorization(t, f, pidfd, 0, 0, true)
			if f.group.Retired || f.group.HeldFD != int(null.Fd()) {
				t.Fatal("authorization retired the group")
			}
		})
	}
	for _, c := range []struct {
		name string
		set  func(*fixture)
	}{
		{"target-only-offline", func(f *fixture) { f.target.offlineSequence = 10 }},
		{"group-only-offline", func(f *fixture) { f.group.OfflineSeq = 10 }},
		{"offline-mismatch", func(f *fixture) { f.group.OfflineSeq, f.target.offlineSequence = 9, 10 }},
		{"target-only-release", func(f *fixture) { f.group.OfflineSeq, f.target.offlineSequence, f.target.releaseSequence = 10, 10, 11 }},
		{"group-only-release", func(f *fixture) { f.group.OfflineSeq, f.target.offlineSequence, f.group.ReleaseSeq = 10, 10, 11 }},
		{"release-mismatch", func(f *fixture) {
			f.group.OfflineSeq, f.target.offlineSequence, f.group.ReleaseSeq, f.target.releaseSequence = 10, 10, 11, 12
		}},
		{"release-without-offline", func(f *fixture) { f.group.ReleaseSeq, f.target.releaseSequence = 11, 11 }},
		{"release-equal", func(f *fixture) {
			f.group.OfflineSeq, f.target.offlineSequence, f.group.ReleaseSeq, f.target.releaseSequence = 10, 10, 10, 10
		}},
		{"release-earlier", func(f *fixture) {
			f.group.OfflineSeq, f.target.offlineSequence, f.group.ReleaseSeq, f.target.releaseSequence = 10, 10, 9, 9
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(2, 1)
			c.set(f)
			checkAuthorization(t, f, pidfd, 12, 0, false)
		})
	}
	for _, c := range []struct {
		name  string
		phase uint32
	}{{"live-phase2", 2}, {"live-phase3", 3}} {
		t.Run(c.name, func(t *testing.T) { checkAuthorization(t, newFixture(c.phase, 1), pidfd, 13, 0, false) })
	}
	for _, c := range []struct {
		name        string
		mutate      func(*fixture)
		pidfd       int
		point       brokerDeletePoint
		targetPoint brokerTargetPoint
	}{
		{"wrong-grant", func(f *fixture) { f.group.GrantID = 9 }, pidfd, 10, 8},
		{"group-alias", func(f *fixture) { clone := *f.group; f.target.group = &clone }, pidfd, 10, 14},
		{"grant-alias", func(f *fixture) { clone := *f.grant; f.target.grant = &clone }, pidfd, 10, 15},
		{"held-fd", func(f *fixture) { f.group.HeldFD = -1 }, pidfd, 10, 6},
		{"enrollment", func(f *fixture) { f.group.Enrollment = 0 }, pidfd, 10, 9},
		{"root", func(f *fixture) { f.group.RootCookie = 0 }, pidfd, 10, 10},
		{"node", func(f *fixture) { f.group.KernfsID = 0 }, pidfd, 10, 11},
		{"phase4", func(f *fixture) { f.target.phase = 4 }, pidfd, 10, 16},
		{"phase1", func(f *fixture) { f.target.phase = 1 }, pidfd, 11, 0},
		{"invalid-pidfd", func(f *fixture) {}, -1, 9, 0},
	} {
		t.Run("offline-early-"+c.name, func(t *testing.T) {
			f := newFixture(2, 1)
			if err := observe(f, 5, 10, 2, 3); err != nil {
				t.Fatal(err)
			}
			c.mutate(f)
			checkAuthorization(t, f, c.pidfd, c.point, c.targetPoint, false)
		})
	}
	t.Run("event-rejections", func(t *testing.T) {
		f := newFixture(2, 1)
		if observe(f, 6, 11, 2, 3) != errGuardProtocol {
			t.Fatal("release without offline accepted")
		}
		if err := observe(f, 5, 10, 2, 3); err != nil {
			t.Fatal(err)
		}
		if observe(f, 5, 11, 2, 3) != errGuardProtocol {
			t.Fatal("duplicate offline accepted")
		}
		if observe(f, 6, 10, 2, 3) != errGuardProtocol || observe(f, 6, 9, 2, 3) != errGuardProtocol {
			t.Fatal("non-increasing release accepted")
		}
		if err := observe(f, 6, 11, 2, 3); err != nil {
			t.Fatal(err)
		}
		if observe(f, 6, 12, 2, 3) != errGuardProtocol {
			t.Fatal("duplicate release accepted")
		}
	})
	for _, c := range []struct {
		name       string
		root, node uint64
	}{{"wrong-root", 4, 3}, {"wrong-node", 2, 4}} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(2, 1)
			if err := observe(f, 5, 10, c.root, c.node); err != nil {
				t.Fatal(err)
			}
			if f.group.OfflineSeq != 0 || f.target.offlineSequence != 0 {
				t.Fatal("unrelated event altered target")
			}
			checkAuthorization(t, f, pidfd, 13, 0, false)
		})
	}
}
