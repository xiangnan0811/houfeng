//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnerProbeSettlementTerminalTransport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   uint32
		deadline bool
	}{
		{"deadline-without-settle", 1, true},
		{"ack-sent", 2, false},
		{"control-hup", 3, false},
		{"principal-death", 4, false},
		{"broker-exit", 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pair[0])
			defer unix.Close(pair[1])
			if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
				t.Fatal(err)
			}
			b := &authorityBroker{bootID: "00000000-0000-0000-0000-000000000001", epoch: strings.Repeat("b", 32), bootOwner: 1}
			g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64)}
			b.grants = map[uint64]*brokerGrant{g.id: g}
			tr := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
			tr.connections[0] = settlementEvidenceConnection{fd: pair[0], hello: true, purpose: 1, principal: &controlPrincipal{ID: 1}, nonce: [16]byte{1}, registeredAt: evidenceNow()}
			tr.connections[1].fd = -1
			s := &ownerProbeEvidenceState{broker: b, transport: tr, moduleInstance: 9}
			b.diagnostic = s
			s.issue(b, g)
			e := s.grantFor(g)
			receive := func() [settlementEvidenceBytes]byte {
				t.Helper()
				var packet [settlementEvidenceBytes]byte
				var ancillary [128]byte
				n, on, flags, _, err := unix.Recvmsg(pair[1], packet[:], ancillary[:], 0)
				if err != nil || n != len(packet) || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
					t.Fatalf("snapshot frame: n=%d flags=%x err=%v", n, flags, err)
				}
				messages, err := unix.ParseSocketControlMessage(ancillary[:on])
				if err != nil || len(messages) != 1 {
					t.Fatal("missing snapshot credentials")
				}
				cred, err := unix.ParseUnixCredentials(&messages[0])
				if err != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != uint32(os.Getuid()) || cred.Gid != uint32(os.Getgid()) {
					t.Fatal("wrong snapshot credentials")
				}
				return packet
			}
			tr.service(b, s)
			enrolled := receive()
			if enrolled[10] != 1 {
				t.Fatal("missing enrolled snapshot")
			}
			if tc.reason == 2 || tc.reason == 3 {
				control, controlErr := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
				if controlErr != nil {
					t.Fatal(controlErr)
				}
				defer unix.Close(control[0])
				if tc.reason == 2 {
					if sendControl(control[0], []byte("ack"), nil) != nil {
						t.Fatal("control ACK send failed")
					}
					var ack [8]byte
					if n, readErr := unix.Read(control[1], ack[:]); readErr != nil || string(ack[:n]) != "ack" {
						t.Fatal("control ACK not received")
					}
					_ = unix.Close(control[1])
				} else {
					_ = unix.Close(control[1])
					poll := []unix.PollFd{{Fd: int32(control[0]), Events: unix.POLLIN}}
					if _, pollErr := unix.Poll(poll, 0); pollErr != nil || poll[0].Revents&unix.POLLHUP == 0 {
						t.Fatal("control HUP not observed")
					}
				}
			}
			now := evidenceNow()
			if tc.deadline {
				e.deadline = now
				s.deadlines(now)
			} else {
				s.terminal(e, tc.reason, now, 19)
			}
			tr.flush(s)
			final := receive()
			if final[10] != 3 || binary.LittleEndian.Uint32(final[248:]) != tc.reason || binary.LittleEndian.Uint64(final[120:]) != now {
				t.Fatal("wrong final boundary")
			}
			frozen := e.snapshots[1]
			s.terminal(e, 5, evidenceNow(), 0)
			tr.flush(s)
			if e.snapshots[1] != frozen || e.snapshotCount != 2 {
				t.Fatal("late event changed final snapshot")
			}
			var extra [1]byte
			if _, err = unix.Read(pair[1], extra[:]); err != unix.EAGAIN {
				t.Fatalf("unexpected extra packet: %v", err)
			}
		})
	}
}

// This uses the production admission commit and poll HUP hook, without
// pretending the fixture has a live kernel guard or a settled grant.
func TestOwnerProbeAdmissionControlHUP(t *testing.T) {
	rootTestPinProc(t)
	pid := os.Getpid()
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	process, err := observeProcess(uint32(pid), pidfd)
	if err != nil {
		t.Fatal(err)
	}
	for _, shared := range []bool{false, true} {
		name := "separate-connections"
		if shared {
			name = "two-grants-one-connection"
		}
		t.Run(name, func(t *testing.T) {
			b := controlDiagnosticFixture()
			b.bootOwner = 1
			p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PID: uint32(pid), PIDFD: pidfd, UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), StartNS: process.StartTicks * 10_000_000, NextRequest: 1}
			b.principals[1] = p
			tr := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
			tr.connections[0].fd, tr.connections[1].fd = -1, -1
			s := &ownerProbeEvidenceState{broker: b, transport: tr, moduleInstance: 9}
			b.diagnostic = s
			var evidencePairs [2][2]int
			var controls [2][2]int
			var connections [2]brokerControlConnection
			var grants [2]*brokerGrant
			now := evidenceNow()
			if now == 0 {
				t.Fatal("monotonic clock unavailable")
			}
			for i, purpose := range []string{"postgres-uid", "postgres-gid"} {
				pair, e := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
				if e != nil {
					t.Fatal(e)
				}
				evidencePairs[i] = pair
				defer unix.Close(pair[0])
				defer unix.Close(pair[1])
				if e = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); e != nil {
					t.Fatal(e)
				}
				if e = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); e != nil {
					t.Fatal(e)
				}
				tr.connections[i].fd = pair[0]
				nonce := strings.Repeat(string(rune('1'+i)), 32)
				cid := strings.Repeat(string(rune('a'+i)), 64)
				hello, e := canonicalControl(map[string]any{"version": jsonU64(1), "purpose": purpose, "nonce": nonce, "principal_id": jsonU64(p.ID)})
				if e != nil || sendControl(pair[1], hello, nil) != nil {
					t.Fatal("sending HELLO", e)
				}
				tr.service(b, s)
				if !tr.connections[i].hello {
					t.Fatal("authenticated HELLO failed")
				}
				var reply [128]byte
				if _, e = unix.Read(pair[1], reply[:]); e != nil {
					t.Fatal(e)
				}
				bind, e := canonicalControl(map[string]any{"version": jsonU64(1), "purpose": purpose, "nonce": nonce, "principal_id": jsonU64(p.ID), "container_id": cid, "operation_start_ns": jsonU64(now), "operation_deadline_ns": jsonU64(now + 60_000_000_000), "authority_deadline_ns": jsonU64(now + 60_000_000_000)})
				if e != nil || sendControl(pair[1], bind, nil) != nil {
					t.Fatal("sending BIND", e)
				}
				tr.service(b, s)
				if !tr.connections[i].bound {
					t.Fatal("authenticated BIND failed")
				}
				control, e := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
				if e != nil {
					t.Fatal(e)
				}
				controls[i] = control
				defer unix.Close(control[0])
				defer func(index int) {
					if controls[index][1] >= 0 {
						_ = unix.Close(controls[index][1])
					}
				}(i)
				connection := &connections[i]
				if shared {
					connection = &connections[0]
				}
				g := &brokerGrant{id: uint64(i + 2), owner: p.ID, generation: 1, purpose: purpose, containerID: cid, phase: "admitted", retirement: "auto_empty"}
				grants[i] = g
				q := &pendingAdmit{principal: p, requestID: uint64(i + 1), raw: []byte("authenticated-admit"), purpose: purpose, cid: cid, generation: 1, config: map[string]any{}, connection: connection}
				if ack, e := b.commitAdmittedGrant(q, g); e != nil || len(ack) == 0 {
					t.Fatalf("admission commit: %v", e)
				}
			}
			tr.flush(s)
			receive := func(index int) [settlementEvidenceBytes]byte {
				t.Helper()
				var frame [settlementEvidenceBytes]byte
				var ancillary [128]byte
				n, on, flags, _, e := unix.Recvmsg(evidencePairs[index][1], frame[:], ancillary[:], 0)
				if e != nil || n != len(frame) || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
					t.Fatalf("evidence frame: %d %v", n, e)
				}
				messages, e := unix.ParseSocketControlMessage(ancillary[:on])
				if e != nil || len(messages) != 1 {
					t.Fatal("missing evidence credentials")
				}
				cred, e := unix.ParseUnixCredentials(&messages[0])
				if e != nil || cred.Pid != int32(pid) {
					t.Fatal("unexpected evidence sender")
				}
				return frame
			}
			for i := range grants {
				enrolled := receive(i)
				if enrolled[10] != 1 || binary.LittleEndian.Uint64(enrolled[16:]) != grants[i].generation || binary.LittleEndian.Uint64(enrolled[24:]) != grants[i].id || binary.LittleEndian.Uint64(enrolled[32:]) != p.ID || hex.EncodeToString(enrolled[184:216]) != grants[i].containerID {
					t.Fatal("missing ENROLLED or incorrect grant identity")
				}
			}
			if b.pendingSettlement != nil || !livePIDFD(pidfd) {
				t.Fatal("fixture lost live owner or unexpectedly settled")
			}
			observeHUP := func(i int) (uint64, uint64) {
				t.Helper()
				if e := unix.Close(controls[i][1]); e != nil {
					t.Fatal(e)
				}
				controls[i][1] = -1
				poll := []unix.PollFd{{Fd: int32(controls[i][0]), Events: unix.POLLIN}}
				if n, e := unix.Poll(poll, 0); e != nil || n != 1 || poll[0].Revents&unix.POLLHUP == 0 {
					t.Fatal("control HUP not observed", e)
				}
				before := evidenceNow()
				b.controlHUP(&connections[i], nil)
				return before, evidenceNow()
			}
			before, after := observeHUP(0)
			if !shared && s.grantFor(grants[1]).final {
				t.Fatal("unrelated connection finalized")
			}
			tr.flush(s)
			final := receive(0)
			cutoff := binary.LittleEndian.Uint64(final[120:])
			firstCutoff := cutoff
			if final[10] != 3 || binary.LittleEndian.Uint32(final[248:]) != 3 || cutoff < before || cutoff > after || binary.LittleEndian.Uint64(final[256+9*80+48:]) != 1 {
				t.Fatal("wrong HUP boundary or duplicate fact")
			}
			frozen := s.grantFor(grants[0]).snapshots[1]
			b.controlHUP(&connections[0], &pendingBrokerSettlement{evidence: s.grantFor(grants[0])})
			if s.grantFor(grants[0]).snapshotCount != 2 || s.grantFor(grants[0]).snapshots[1] != frozen {
				t.Fatal("late HUP rewrote FINAL")
			}
			if !shared {
				before, after = observeHUP(1)
				tr.flush(s)
			}
			final = receive(1)
			cutoff = binary.LittleEndian.Uint64(final[120:])
			if final[10] != 3 || binary.LittleEndian.Uint32(final[248:]) != 3 || cutoff < before || cutoff > after || binary.LittleEndian.Uint64(final[256+9*80+48:]) != 1 {
				t.Fatal("second grant lost its exact HUP boundary")
			}
			if shared && cutoff != firstCutoff {
				t.Fatal("one HUP produced different cutoffs")
			}
			if !livePIDFD(pidfd) || b.pendingSettlement != nil {
				t.Fatal("HUP changed owner or settlement authority")
			}
		})
	}
}
