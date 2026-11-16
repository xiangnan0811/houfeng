//go:build linux && amd64 && !cgo

package main

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This test owns its short-lived children. It exercises pidfd death and the
// production projection/seqpacket APIs in isolation; it does not start a real
// broker, load a module, or claim kernel authority evidence.
func TestOwnerProbeSettlementChildBoundary(t *testing.T) {
	switch os.Getenv("G2_SETTLEMENT_BOUNDARY_CHILD") {
	case "principal":
		_, _ = os.Stdout.Write([]byte{'R'})
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		return
	case "broker-exit":
		b := &authorityBroker{bootID: "00000000-0000-0000-0000-000000000001", epoch: strings.Repeat("b", 32), bootOwner: 1}
		g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64)}
		b.grants = map[uint64]*brokerGrant{g.id: g}
		tr := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
		tr.connections[0] = settlementEvidenceConnection{fd: 3, hello: true, purpose: 1, principal: &controlPrincipal{ID: 1}, nonce: [16]byte{1}, registeredAt: evidenceNow()}
		tr.connections[1].fd = -1
		s := &ownerProbeEvidenceState{broker: b, transport: tr, moduleInstance: 9}
		b.diagnostic = s
		s.issue(b, g)
		tr.flush(s)
		defer func() { s.terminal(s.grantFor(g), 5, evidenceNow(), 0); tr.flush(s); _ = unix.Close(3) }()
		return
	}
	start := func(t *testing.T, mode string, extra *os.File) (*exec.Cmd, context.CancelFunc) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnerProbeSettlementChildBoundary$", "-test.timeout=8s")
		cmd.Env = append(os.Environ(), "G2_SETTLEMENT_BOUNDARY_CHILD="+mode)
		if extra != nil {
			cmd.ExtraFiles = []*os.File{extra}
		}
		return cmd, cancel
	}
	receive := func(t *testing.T, fd int, sender int32) [settlementEvidenceBytes]byte {
		t.Helper()
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, err := unix.Poll(poll, 3000); err != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
			t.Fatalf("snapshot wait: %d %v", n, err)
		}
		var packet [settlementEvidenceBytes]byte
		var ancillary [128]byte
		n, on, flags, _, err := unix.Recvmsg(fd, packet[:], ancillary[:], 0)
		if err != nil || n != len(packet) || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			t.Fatalf("snapshot receive: %d %v", n, err)
		}
		messages, err := unix.ParseSocketControlMessage(ancillary[:on])
		if err != nil || len(messages) != 1 {
			t.Fatal("missing credentials")
		}
		cred, err := unix.ParseUnixCredentials(&messages[0])
		if err != nil || cred.Pid != sender || cred.Uid != uint32(os.Getuid()) || cred.Gid != uint32(os.Getgid()) {
			t.Fatal("wrong sender identity")
		}
		return packet
	}
	t.Run("pidfd-owner-death", func(t *testing.T) {
		cmd, cancel := start(t, "principal", nil)
		defer cancel()
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waited := false
		defer func() {
			_ = stdin.Close()
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		var ready [1]byte
		if _, err = io.ReadFull(stdout, ready[:]); err != nil || ready[0] != 'R' {
			t.Fatalf("child barrier: %v", err)
		}
		pidfd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		b := controlDiagnosticFixture()
		b.bootOwner, b.effects = 1, &runtimeEffects{}
		p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PIDFD: pidfd, PID: uint32(cmd.Process.Pid)}
		g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64), phase: "admitted", retirement: "auto_empty"}
		b.principals, b.grants = map[uint64]*controlPrincipal{1: p}, map[uint64]*brokerGrant{2: g}
		if _, _, err = b.transcript.append("principal", map[string]any{"id": jsonU64(1), "terminal": nil}); err != nil {
			t.Fatal(err)
		}
		tr := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
		tr.connections[0] = settlementEvidenceConnection{fd: pair[0], hello: true, purpose: 1, principal: p, nonce: [16]byte{1}, registeredAt: evidenceNow()}
		tr.connections[1].fd = -1
		s := &ownerProbeEvidenceState{broker: b, transport: tr, moduleInstance: 9}
		b.diagnostic = s
		s.issue(b, g)
		tr.flush(s)
		if receive(t, pair[1], int32(os.Getpid()))[10] != 1 {
			t.Fatal("missing enrollment")
		}
		if err = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
			t.Fatal(err)
		}
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		if n, pollErr := unix.Poll(poll, 3000); pollErr != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
			t.Fatal("child death not observed by pidfd")
		}
		if err = b.serviceOwnerDeaths(); err != nil || !p.Terminal || g.phase != "revoked" {
			t.Fatalf("owner death: %v", err)
		}
		s.terminal(s.grantFor(g), 4, evidenceNow(), 0)
		tr.flush(s)
		final := receive(t, pair[1], int32(os.Getpid()))
		if final[10] != 3 || binary.LittleEndian.Uint32(final[248:]) != 4 {
			t.Fatal("missing principal death final")
		}
		if err = cmd.Wait(); err == nil {
			t.Fatal("killed child reported success")
		}
		waited = true
		status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatal("wrong child exit status")
		}
	})
	t.Run("broker-process-exit", func(t *testing.T) {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[0])
		if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		childSocket := os.NewFile(uintptr(pair[1]), "settlement-child-socket")
		if childSocket == nil {
			_ = unix.Close(pair[1])
			t.Fatal("child socket")
		}
		cmd, cancel := start(t, "broker-exit", childSocket)
		defer cancel()
		if err = cmd.Start(); err != nil {
			_ = childSocket.Close()
			t.Fatal(err)
		}
		_ = childSocket.Close()
		waited := false
		defer func() {
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		pidfd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		enrolled, final := receive(t, pair[0], int32(cmd.Process.Pid)), receive(t, pair[0], int32(cmd.Process.Pid))
		if enrolled[10] != 1 || final[10] != 3 || binary.LittleEndian.Uint32(final[248:]) != 5 {
			t.Fatal("wrong broker exit snapshots")
		}
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		if n, pollErr := unix.Poll(poll, 3000); pollErr != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
			t.Fatal("broker child exit not observed")
		}
		if err = cmd.Wait(); err != nil {
			t.Fatalf("broker child: %v", err)
		}
		waited = true
	})
}
