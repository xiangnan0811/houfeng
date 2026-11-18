//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEarlyDiagnosticBoundedFirstFailure(t *testing.T) {
	cases := []struct {
		name  string
		stage string
		err   error
		want  string
	}{
		{"wrapped errno", "MOUNT_PROC", fmt.Errorf("private /path: %w", syscall.EACCES), "G2D1 E MOUNT_PROC ERRNO=13\n"},
		{"protocol check", "LSM", errGuardProtocol, "G2D1 E LSM CHECK\n"},
		{"other error", "CMDLINE", errors.New("private /path and credential"), "G2D1 E CMDLINE OTHER\n"},
		{"invalid stage", "private/path", syscall.ENOENT, "G2D1 E INVALID_STAGE ERRNO=2\n"},
		{"long stage", strings.Repeat("A", 65), nil, "G2D1 E INVALID_STAGE CHECK\n"},
		{"maximum stage and errno", strings.Repeat("A", 64), syscall.Errno(^uintptr(0)), "G2D1 E " + strings.Repeat("A", 64) + " ERRNO=18446744073709551615\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			original := g2d1
			defer func() { g2d1 = original }()
			g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'E', false
			g2d1.count, g2d1.failed = 0, false
			flags, err := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
			if err != nil {
				t.Fatal(err)
			}
			g2d1Mark("PROGRESS")
			g2d1Fail(tc.stage, tc.err)
			g2d1Fail("LATER", syscall.EIO)
			g2d1Close()
			after, err := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
			if err != nil || after != flags {
				t.Fatalf("inherited fd changed or closed: flags=%d after=%d err=%v", flags, after, err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want || len(got) > 128 {
				t.Fatalf("diagnostic = %q (%d bytes), want %q", got, len(got), tc.want)
			}
		})
	}
}

func TestHelperDiagnosticBorrowedStderr(t *testing.T) {
	for _, tc := range []struct {
		name string
		role byte
	}{
		{"audit-reader", 'R'},
		{"terminal-observer", 'O'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"/g2-bpf-authority", tc.name}
			if got := g2d1HelperRole(args); got != tc.role {
				t.Fatalf("role = %q, want %q", got, tc.role)
			}
			for _, invalid := range [][]string{nil, {args[0]}, {args[0], args[1], "extra"}, {"/other", args[1]}} {
				if got := g2d1HelperRole(invalid); got != 0 {
					t.Fatalf("unexpected role %q for %q", got, invalid)
				}
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			original := g2d1
			defer func() { g2d1 = original }()
			g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), tc.role, false
			g2d1.count, g2d1.failed = 0, false
			flags, err := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
			if err != nil {
				t.Fatal(err)
			}
			fdFlags, err := unix.FcntlInt(w.Fd(), unix.F_GETFD, 0)
			if err != nil {
				t.Fatal(err)
			}
			g2d1Mark("PROGRESS")
			g2d1Fail("FIRST", syscall.EACCES)
			g2d1Fail("LATER", syscall.EIO)
			if tc.role == 'R' {
				g2d1TerminalFail(syscall.EIO)
			}
			g2d1Close()
			after, err := unix.FcntlInt(w.Fd(), unix.F_GETFL, 0)
			if err != nil || after != flags {
				t.Fatalf("status flags changed or fd closed: before=%d after=%d err=%v", flags, after, err)
			}
			afterFD, err := unix.FcntlInt(w.Fd(), unix.F_GETFD, 0)
			if err != nil || afterFD != fdFlags {
				t.Fatalf("descriptor flags changed or fd closed: before=%d after=%d err=%v", fdFlags, afterFD, err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			want := fmt.Sprintf("G2D1 %c FIRST ERRNO=13\n", tc.role)
			if err != nil || string(got) != want {
				t.Fatalf("diagnostic = %q, read error = %v, want %q", got, err, want)
			}
		})
	}
}

func TestEarlyDiagnosticCompositePreservesCause(t *testing.T) {
	for _, tc := range []struct {
		role  byte
		scope string
	}{
		{'E', "MODULE_SEALS"},
		{'O', "MODULE_RECHECK"},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			original := g2d1
			defer func() { g2d1 = original }()
			g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), tc.role, false
			g2d1.count, g2d1.failed, g2d1.scope = 0, false, tc.scope
			// A closed descriptor exposes the real F_GET_SEALS error without BPF
			// or mount changes; the API still returns its protocol error.
			closed, err := unix.Dup(int(w.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			if err = unix.Close(closed); err != nil {
				t.Fatal(err)
			}
			_, _, _, err = verifySealedModuleMemfd(closed, "expected", 0, 0, 0)
			if err != errGuardProtocol {
				t.Fatalf("returned error = %v, want protocol error", err)
			}
			g2d1Fail("ENTRY", err)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			want := fmt.Sprintf("G2D1 %c %s ERRNO=9\n", tc.role, tc.scope)
			if err != nil || string(got) != want {
				t.Fatalf("diagnostic = %q, read error = %v, want %q", got, err, want)
			}
		})
	}
}

func TestDiagnosticDeferredBrokerTerminalBorrow(t *testing.T) {
	ownedRead, ownedWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ownedRead.Close()
	defer ownedWrite.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrRead.Close()
	defer stderrWrite.Close()

	original := g2d1
	defer func() { g2d1 = original }()
	fdFlags, err := unix.FcntlInt(2, unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	savedStderr, err := unix.Dup(2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Dup2(savedStderr, 2); err != nil {
			t.Errorf("restore stderr: %v", err)
		} else if _, err := unix.FcntlInt(2, unix.F_SETFD, fdFlags); err != nil {
			t.Errorf("restore stderr flags: %v", err)
		}
		_ = unix.Close(savedStderr)
	}()
	if err := unix.Dup2(int(stderrWrite.Fd()), 2); err != nil {
		t.Fatal(err)
	}
	statusBefore, err := unix.FcntlInt(2, unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	descriptorBefore, err := unix.FcntlInt(2, unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	ownedFD, err := unix.Dup(int(ownedWrite.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ownedClosed := false
	defer func() {
		if !ownedClosed {
			_ = unix.Close(ownedFD)
		}
	}()
	g2d1.fd, g2d1.role, g2d1.owned = ownedFD, 'B', true
	g2d1.count, g2d1.failed, g2d1.captured = 7, false, false
	g2d1.closedAfterACK = true
	g2d1CaptureFailure("BROKER_READER_HEALTH", syscall.EACCES)
	g2d1Fail("LATER_IMMEDIATE", syscall.EIO)
	g2d1CaptureFailure("LATER_CAPTURE", syscall.ENOENT)
	g2d1Mark("LATER_PROGRESS")
	for _, fd := range []int{int(ownedRead.Fd()), int(stderrRead.Fd())} {
		p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, err := unix.Poll(p, 0); err != nil || n != 0 {
			t.Fatalf("output before cleanup: ready=%d err=%v", n, err)
		}
	}
	if g2d1.count != 7 || !g2d1.failed {
		t.Fatalf("capture changed count or failed latch: count=%d failed=%t", g2d1.count, g2d1.failed)
	}
	g2d1Close()
	ownedClosed = true
	if _, err := unix.FcntlInt(uintptr(ownedFD), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatalf("owned descriptor not closed: %v", err)
	}
	g2d1TerminalFail(syscall.EIO)
	g2d1Mark("BORROWED_PROGRESS")
	g2d1TerminalFail(syscall.ENOENT)
	if g2d1.count != 8 || !g2d1.failed || g2d1.owned {
		t.Fatalf("terminal state: count=%d failed=%t owned=%t", g2d1.count, g2d1.failed, g2d1.owned)
	}
	g2d1Close()
	statusAfter, statusErr := unix.FcntlInt(2, unix.F_GETFL, 0)
	descriptorAfter, descriptorErr := unix.FcntlInt(2, unix.F_GETFD, 0)
	if statusErr != nil || descriptorErr != nil || statusBefore != statusAfter || descriptorBefore != descriptorAfter {
		t.Fatalf("stderr changed or closed: status %d/%d (%v), descriptor %d/%d (%v)", statusBefore, statusAfter, statusErr, descriptorBefore, descriptorAfter, descriptorErr)
	}
	p := []unix.PollFd{{Fd: int32(stderrRead.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("terminal record unavailable: ready=%d events=%d err=%v", n, p[0].Revents, err)
	}
	buf := make([]byte, 128)
	n, err := unix.Read(int(stderrRead.Fd()), buf)
	if err != nil {
		t.Fatalf("read terminal record: %v", err)
	}
	if string(buf[:n]) != "G2D1 B BROKER_READER_HEALTH ERRNO=13\n" {
		t.Fatalf("terminal record = %q", buf[:n])
	}
	p[0].Revents = 0
	if n, err := unix.Poll(p, 0); err != nil || n != 0 {
		t.Fatalf("unexpected additional record: ready=%d err=%v", n, err)
	}
}

func TestDiagnosticDeferredRespectsRecordLimit(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	original := g2d1
	defer func() { g2d1 = original }()
	g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'R', false
	g2d1.count, g2d1.failed, g2d1.captured = 24, false, false
	g2d1CaptureFailure("READER_POLL", syscall.EIO)
	g2d1TerminalFail(syscall.EIO)
	if g2d1.count != 24 || g2d1.failed || g2d1.captured {
		t.Fatalf("record limit changed: count=%d failed=%t captured=%t", g2d1.count, g2d1.failed, g2d1.captured)
	}
	p := []unix.PollFd{{Fd: int32(r.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 0 {
		t.Fatalf("record emitted beyond limit: ready=%d err=%v", n, err)
	}
}

func TestDiagnosticDeferredReaderFailure(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	original := g2d1
	defer func() { g2d1 = original }()
	g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'R', false
	g2d1.count, g2d1.failed, g2d1.captured = 0, false, false
	g2d1Fail("PROCESS_STATUS", syscall.EBADF)
	g2d1CaptureFailure("READER_POLL", syscall.EIO)
	p := []unix.PollFd{{Fd: int32(r.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 0 {
		t.Fatalf("reader wrote before terminal publication: ready=%d err=%v", n, err)
	}
	g2d1TerminalFail(errGuardProtocol)
	g2d1TerminalFail(errGuardProtocol)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "G2D1 R PROCESS_STATUS ERRNO=9\n" {
		t.Fatalf("reader diagnostic = %q, err=%v", got, err)
	}
	if g2d1.count != 1 || !g2d1.failed {
		t.Fatalf("reader state: count=%d failed=%t", g2d1.count, g2d1.failed)
	}
}

func TestDiagnosticDeferredPreservesPriorFailure(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	original := g2d1
	defer func() { g2d1 = original }()
	g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'B', true
	g2d1.count, g2d1.failed, g2d1.captured = 4, false, false
	g2d1Fail("EARLIER_FAILURE", syscall.EACCES)
	g2d1CaptureFailure("BROKER_READER_HEALTH", syscall.EIO)
	g2d1TerminalFail(syscall.EIO)
	if g2d1.count != 5 || !g2d1.failed || g2d1.captured {
		t.Fatalf("prior latch changed: count=%d failed=%t captured=%t", g2d1.count, g2d1.failed, g2d1.captured)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "G2D1 B EARLIER_FAILURE ERRNO=13\n" {
		t.Fatalf("prior diagnostic = %q, err=%v", got, err)
	}
}

func TestReaderQueryEventDiagnostic(t *testing.T) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	if err := unix.Close(pair[1]); err != nil {
		t.Fatal(err)
	}
	p := []unix.PollFd{{Fd: int32(pair[0]), Events: unix.POLLIN}, {Fd: int32(pair[1]), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 2 || p[0].Revents&unix.POLLHUP == 0 || p[1].Revents&unix.POLLNVAL == 0 {
		t.Fatalf("socket poll: n=%d events=%v err=%v", n, p, err)
	}
	if stage := g2d1ReaderQueryEventStage(p[0].Revents); stage != "READER_QUERY_HUP" {
		t.Fatalf("closed peer stage: %s", stage)
	}
	if stage := g2d1ReaderQueryEventStage(p[1].Revents); stage != "READER_QUERY_NVAL" {
		t.Fatalf("closed descriptor stage: %s", stage)
	}
	seen := make(map[string]bool)
	bits := []int16{unix.POLLERR, unix.POLLHUP, unix.POLLNVAL}
	for mask := 1; mask < 8; mask++ {
		var events int16
		for i, bit := range bits {
			if mask&(1<<i) != 0 {
				events |= bit
			}
		}
		stage := g2d1ReaderQueryEventStage(events)
		if seen[stage] || len(stage) > 64 || stage != g2d1ReaderQueryEventStage(events|unix.POLLIN) {
			t.Fatalf("ambiguous or unbounded query event stage: %q", stage)
		}
		seen[stage] = true
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	original := g2d1
	defer func() { g2d1 = original }()
	g2d1.fd, g2d1.role, g2d1.owned = int(w.Fd()), 'R', false
	g2d1.count, g2d1.failed, g2d1.captured = 0, false, false
	const secret = "private-error-payload-8e31"
	g2d1CaptureFailure(g2d1ReaderQueryEventStage(p[0].Revents), errors.New(secret))
	g2d1CaptureFailure(g2d1ReaderQueryEventStage(p[1].Revents), errGuardProtocol)
	ready := []unix.PollFd{{Fd: int32(r.Fd()), Events: unix.POLLIN}}
	if n, err := unix.Poll(ready, 0); err != nil || n != 0 {
		t.Fatalf("premature reader diagnostic: n=%d err=%v", n, err)
	}
	g2d1TerminalFail(errGuardProtocol)
	g2d1TerminalFail(errGuardProtocol)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil || string(out) != "G2D1 R READER_QUERY_HUP OTHER\n" || len(out) > 128 || strings.Contains(string(out), secret) || g2d1.count != 1 {
		t.Fatalf("reader diagnostic: %q count=%d err=%v", out, g2d1.count, err)
	}
}
