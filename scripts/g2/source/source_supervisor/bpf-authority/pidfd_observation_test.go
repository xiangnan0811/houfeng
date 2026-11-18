//go:build linux && amd64 && !cgo

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The subprocess isolates proc custody changes and the closed-descriptor probe.
func selectorTestChild(t *testing.T, name string) bool {
	t.Helper()
	if os.Getenv("G2_PIDFD_SELECTOR_TEST_CHILD") == "1" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+name+"$", "-test.v", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "G2_PIDFD_SELECTOR_TEST_CHILD=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("isolated child: %v, context=%v\n%s", err, ctx.Err(), out)
	}
	t.Logf("isolated child:\n%s", out)
	return false
}

func TestPIDFDSelectorObservation(t *testing.T) {
	if os.Getenv("G2_PIDFD_SELECTOR_TEST_WAIT") == "1" {
		_, _ = os.Stdout.Write([]byte{'R'})
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		return
	}
	if !selectorTestChild(t, "TestPIDFDSelectorObservation") {
		return
	}
	rootTestPinProc(t)
	self := uint32(os.Getpid())
	fd, err := unix.PidfdOpen(int(self), 0)
	if err != nil {
		t.Fatalf("self pidfd: %v", err)
	}
	defer unix.Close(fd)
	var obs selectorObservation
	selected, pollErr := pidfdSelectsObserved(fd, self, &obs)
	selectedNoSink, pollErrNoSink := pidfdSelectsObserved(fd, self, nil)
	if !selected || pollErr != nil || obs != (selectorObservation{}) || !selectedNoSink || pollErrNoSink != nil || !pidfdSelects(fd, self) {
		t.Fatal("live self selector failed")
	}
	check := func(fd int, pid uint32, reason selectorReason) selectorObservation {
		t.Helper()
		var got selectorObservation
		selected, pollErr := pidfdSelectsObserved(fd, pid, &got)
		selectedNoSink, pollErrNoSink := pidfdSelectsObserved(fd, pid, nil)
		if selected || pollErr != nil || got.reason != reason || selectedNoSink || pollErrNoSink != nil || pidfdSelects(fd, pid) {
			t.Fatalf("selector fd=%d pid=%d: %+v, want %v", fd, pid, got, reason)
		}
		return got
	}
	check(-1, 0, selectorInvalidFD)
	check(fd, 0, selectorZeroPID)
	check(fd, self+1, selectorPIDMismatch)
	pipe := []int{0, 0}
	if err := unix.Pipe2(pipe, unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	check(pipe[0], self, selectorPIDMissing)

	cmd := exec.Command(os.Args[0], "-test.run=^TestPIDFDSelectorObservation$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "G2_PIDFD_SELECTOR_TEST_WAIT=1")
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
	defer func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var ready [1]byte
	if _, err = io.ReadFull(stdout, ready[:]); err != nil || ready[0] != 'R' {
		t.Fatalf("child barrier: %q %v", ready, err)
	}
	childFD, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(childFD)
	if err = unix.PidfdSendSignal(childFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil || cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("child was not killed and reaped: %v", err)
	}
	obs = check(childFD, uint32(cmd.Process.Pid), selectorPrePollRejected)
	if obs.pollFacts&0x21 != 0x21 {
		t.Fatalf("reaped child poll facts=%02X", obs.pollFacts)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "self", "fdinfo"), 0700); err != nil {
		t.Fatal(err)
	}
	fake, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fake)
	original := heldProcDirectory
	defer func() { heldProcDirectory = original }()
	file := filepath.Join(dir, "self", "fdinfo", fmt.Sprint(fd))
	for _, tc := range []struct {
		name, text string
		reason     selectorReason
		ok         bool
	}{
		{"missing", "Name: x\n", selectorPIDMissing, false},
		{"duplicate", fmt.Sprintf("Pid: %d\nPid: %d\n", self, self), selectorPIDDuplicate, false},
		{"invalid", "Pid: secret_invalid\n", selectorPIDParse, false},
		{"overflow", "Pid: 4294967296\n", selectorPIDParse, false},
		{"mismatch", fmt.Sprintf("Pid: %d\n", self+1), selectorPIDMismatch, false},
		{"match", fmt.Sprintf("Pid: %d\n", self), selectorUnknown, true},
	} {
		if err := os.WriteFile(file, []byte(tc.text), 0600); err != nil {
			t.Fatal(err)
		}
		heldProcDirectory = fake
		got := selectorObservation{reason: selectorPostPollError, pollFacts: 0x3f, err: unix.EIO}
		ok, pollErr := pidfdSelectsObserved(fd, self, &got)
		if pollErr != nil {
			t.Fatalf("%s: unexpected poll error: %v", tc.name, pollErr)
		}
		heldProcDirectory = original
		if tc.ok {
			if !ok || got != (selectorObservation{reason: selectorPostPollError, pollFacts: 0x3f, err: unix.EIO}) {
				t.Fatalf("%s: success changed observation: %+v", tc.name, got)
			}
		} else if ok || got != (selectorObservation{reason: tc.reason}) {
			t.Fatalf("%s: ok=%v obs=%+v", tc.name, ok, got)
		}
	}
	heldProcDirectory = -1
	obs = check(fd, self, selectorFDInfoRead)
	heldProcDirectory = original
	if !errors.Is(obs.err, errGuardProtocol) {
		t.Fatalf("unheld proc: %v", obs.err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	heldProcDirectory = fake
	obs = check(fd, self, selectorFDInfoRead)
	heldProcDirectory = original
	if !errors.Is(obs.err, unix.ENOENT) {
		t.Fatalf("missing fdinfo: %v", obs.err)
	}

	// No FD-producing operation occurs between closing the high duplicate and poll.
	high, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 1024)
	if err != nil {
		t.Fatalf("high fd prerequisite: %v", err)
	}
	if err = unix.Close(high); err != nil {
		t.Fatal(err)
	}
	closed := selectorObservation{reason: selectorPostPollError, pollFacts: 0x3f, err: unix.EIO}
	selected, pollErr = pidfdSelectsObserved(high, self, &closed)
	if selected || pollErr != nil || closed.reason != selectorPrePollRejected || closed.pollFacts != 0x22 || closed.err != nil {
		t.Fatalf("closed high fd: selected=%v observation=%+v", selected, closed)
	}
}

func TestPIDFDPollObservation(t *testing.T) {
	for _, tc := range []struct {
		n      int
		events int16
		want   uint8
	}{
		{1, 0, 0x20}, {0, unix.POLLIN, 0x01}, {1, unix.POLLIN | unix.POLLHUP, 0x25},
		{1, unix.POLLNVAL, 0x22}, {1, unix.POLLERR | unix.POLLHUP | unix.POLLNVAL, 0x2e},
		{1, unix.POLLPRI, 0x30}, {1, unix.POLLIN | unix.POLLNVAL | unix.POLLHUP | unix.POLLERR | unix.POLLPRI, 0x3f},
	} {
		if got := normalizeSelectorPoll(tc.n, tc.events); got != tc.want {
			t.Errorf("n=%d events=%x got=%02X want=%02X", tc.n, tc.events, got, tc.want)
		}
		for _, phase := range []struct {
			reason selectorReason
			prefix string
		}{{selectorPrePollRejected, "X_PRE_POLL_"}, {selectorPostPollRejected, "X_POST_POLL_"}} {
			obs := selectorObservation{reason: phase.reason, pollFacts: tc.want}
			if got, want := string(g2d1AppendStage(nil, "X", &obs)), fmt.Sprintf("%s%02X", phase.prefix, tc.want); got != want {
				t.Errorf("stage=%q want=%q", got, want)
			}
		}
	}
	for _, tc := range []struct {
		stage string
		obs   *selectorObservation
		want  string
	}{
		{"BASE", nil, "BASE"}, {"BASE", &selectorObservation{}, "BASE"},
		{"bad-secret", nil, "INVALID_STAGE"}, {strings.Repeat("A", 64), &selectorObservation{reason: selectorPIDMismatch}, "INVALID_STAGE"},
		{"BASE", &selectorObservation{reason: selectorReason(255)}, "INVALID_STAGE"}, {"BASE", &selectorObservation{reason: selectorPrePollRejected, pollFacts: 64}, "INVALID_STAGE"},
	} {
		if got := string(g2d1AppendStage(nil, tc.stage, tc.obs)); got != tc.want {
			t.Errorf("stage=%q want=%q", got, tc.want)
		}
	}
	for _, tc := range []struct {
		reason selectorReason
		suffix string
	}{
		{selectorInvalidFD, "_BAD_FD"}, {selectorZeroPID, "_ZERO_PID"},
		{selectorPrePollError, "_PRE_POLL_ERR"}, {selectorFDInfoRead, "_FDINFO_READ"},
		{selectorPIDMissing, "_PID_MISSING"}, {selectorPIDDuplicate, "_PID_DUP"},
		{selectorPIDParse, "_PID_PARSE"}, {selectorPIDMismatch, "_PID_MISMATCH"},
		{selectorPostPollError, "_POST_POLL_ERR"},
	} {
		obs := selectorObservation{reason: tc.reason}
		if got := string(g2d1AppendStage(nil, "X", &obs)); got != "X"+tc.suffix {
			t.Errorf("reason %v stage %q", tc.reason, got)
		}
	}
}

func selectorPipeEmpty(t *testing.T, fd int) {
	t.Helper()
	p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 0 {
		t.Fatalf("unexpected output: n=%d events=%x err=%v", n, p[0].Revents, err)
	}
}

func selectorReadRecord(t *testing.T, fd int, want string) {
	t.Helper()
	p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if n, err := unix.Poll(p, 0); err != nil || n != 1 || p[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("record missing: n=%d err=%v", n, err)
	}
	var buf [128]byte
	n, err := unix.Read(fd, buf[:])
	if err != nil || string(buf[:n]) != want {
		t.Fatalf("record=%q err=%v want=%q", buf[:n], err, want)
	}
	selectorPipeEmpty(t, fd)
}

func TestReaderPIDFDObservationDeferred(t *testing.T) {
	if !selectorTestChild(t, "TestReaderPIDFDObservationDeferred") {
		return
	}
	rootTestPinProc(t)
	originalProc := heldProcDirectory
	defer func() { heldProcDirectory = originalProc }()
	original := g2d1
	defer func() { g2d1 = original }()
	pid := uint32(os.Getpid())
	fd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	defer unix.Close(pair[1])
	boot, err := fixedBootID()
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	readFD, writeFD := int(r.Fd()), int(w.Fd())
	reset := func() {
		g2d1 = original
		g2d1.fd, g2d1.role, g2d1.owned = writeFD, 'B', false
		g2d1.count, g2d1.failed, g2d1.captured, g2d1.closedAfterACK = 0, false, false, false
	}
	client := func() *auditReaderClient { return &auditReaderClient{fd: pair[0], pidfd: fd, pid: pid, boot: boot} }
	reset()
	if err := client().healthy(); err != nil {
		t.Fatalf("live reader: %v", err)
	}
	for _, tc := range []struct {
		name    string
		missing bool
		reason  selectorReason
		record  string
	}{
		{"mismatch", false, selectorPIDMismatch, "G2D1 B BROKER_READER_PIDFD_BEFORE_PID_MISMATCH CHECK\n"},
		{"missing-file", true, selectorFDInfoRead, "G2D1 B BROKER_READER_PIDFD_BEFORE_FDINFO_READ ERRNO=2\n"},
	} {
		reset()
		c := client()
		if tc.missing {
			dir := t.TempDir()
			fake, e := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if e != nil {
				t.Fatal(e)
			}
			heldProcDirectory = fake
			defer unix.Close(fake)
		} else {
			c.pid++
		}
		observed := c.healthy()
		heldProcDirectory = originalProc
		if !errors.Is(observed, errGuardProtocol) || !c.invalid || c.detection == nil || c.detection.detector != "reader_pidfd_exited" || c.healthReason != readerHealthPIDFDBefore || c.healthSelector.reason != tc.reason {
			t.Fatalf("%s health: %+v %v", tc.name, c, observed)
		}
		first := c.healthSelector
		if err := c.healthy(); !errors.Is(err, errGuardProtocol) || c.healthSelector != first || c.healthReason != readerHealthPIDFDBefore {
			t.Fatalf("%s lost first reason", tc.name)
		}
		if g2d1.failed || g2d1.captured || g2d1.count != 0 {
			t.Fatal("health captured early")
		}
		selectorPipeEmpty(t, readFD)
		if got := g2d1CaptureReaderHealth(g2d1ReaderHealthStage(c, false), nil, observed, &c.healthSelector); got != nil || g2d1.failed {
			t.Fatal("nil return captured")
		}
		revoke := errors.New("secret-revoke-error")
		if got := g2d1CaptureReaderHealth(g2d1ReaderHealthStage(c, false), revoke, observed, &c.healthSelector); got != revoke {
			t.Fatal("returned error replaced")
		}
		g2d1CaptureFailure("LATER", unix.EIO)
		selectorPipeEmpty(t, readFD)
		g2d1TerminalFail(revoke)
		g2d1TerminalFail(revoke)
		if g2d1.count != 1 {
			t.Fatalf("terminal count=%d", g2d1.count)
		}
		selectorReadRecord(t, readFD, tc.record)
	}
	for _, variant := range []string{"closed", "other-role", "failed", "captured", "limit", "latched"} {
		reset()
		c := client()
		c.pid++
		marker := selectorObservation{reason: selectorPIDParse}
		c.healthSelector = marker
		switch variant {
		case "closed":
			g2d1.fd = -1
		case "other-role":
			g2d1.role = 'R'
		case "failed":
			g2d1.failed = true
		case "captured":
			g2d1.captured = true
		case "limit":
			g2d1.count = 24
		case "latched":
			c.healthReason = readerHealthBootRead
		}
		if err := c.healthy(); !errors.Is(err, errGuardProtocol) || !c.invalid || c.detection == nil || c.detection.detector != "reader_pidfd_exited" || c.healthSelector != marker {
			t.Fatalf("%s: detection changed: %+v %v", variant, c, err)
		}
		if variant == "latched" && c.healthReason != readerHealthBootRead {
			t.Fatal("prior reason replaced")
		}
		selectorPipeEmpty(t, readFD)
	}
	reset()
	var nilClient *auditReaderClient
	if !errors.Is(nilClient.healthy(), errGuardProtocol) {
		t.Fatal("nil health")
	}
	invalid := client()
	invalid.invalid = true
	invalid.healthSelector = selectorObservation{reason: selectorPIDParse}
	if !errors.Is(invalid.healthy(), errGuardProtocol) || invalid.healthSelector.reason != selectorPIDParse || invalid.healthReason != readerHealthInvalid {
		t.Fatal("invalid health")
	}
	for _, tc := range []struct {
		stage string
		obs   selectorObservation
		want  string
	}{
		{"BROKER_READER_PIDFD_BEFORE", selectorObservation{reason: selectorPrePollRejected, pollFacts: 0x25}, "G2D1 B BROKER_READER_PIDFD_BEFORE_PRE_POLL_25 CHECK\n"},
		{"BROKER_READER_EVENT_PIDFD_AFTER", selectorObservation{reason: selectorPostPollError, err: unix.EIO}, "G2D1 B BROKER_READER_EVENT_PIDFD_AFTER_POST_POLL_ERR ERRNO=5\n"},
	} {
		reset()
		returned := errors.New("secret text")
		if got := g2d1CaptureReaderHealth(tc.stage, returned, errGuardProtocol, &tc.obs); got != returned {
			t.Fatal("return changed")
		}
		g2d1TerminalFail(returned)
		selectorReadRecord(t, readFD, tc.want)
		g2d1.failed = false
		g2d1Record("NEXT", true, errGuardProtocol)
		selectorReadRecord(t, readFD, "G2D1 B NEXT CHECK\n")
	}
	for _, tc := range []struct {
		stage string
		obs   *selectorObservation
		want  string
	}{
		{"BASE", nil, "G2D1 B BASE CHECK\n"},
		{"BASE", &selectorObservation{}, "G2D1 B BASE CHECK\n"},
		{"bad-secret", &selectorObservation{reason: selectorPIDParse}, "G2D1 B INVALID_STAGE CHECK\n"},
		{strings.Repeat("A", 64), &selectorObservation{reason: selectorPIDMismatch}, "G2D1 B INVALID_STAGE CHECK\n"},
		{"BASE", &selectorObservation{reason: selectorReason(255)}, "G2D1 B INVALID_STAGE CHECK\n"},
		{"BASE", &selectorObservation{reason: selectorPrePollRejected, pollFacts: 64}, "G2D1 B INVALID_STAGE CHECK\n"},
		{strings.Repeat("A", 64), nil, "G2D1 B " + strings.Repeat("A", 64) + " CHECK\n"},
	} {
		reset()
		_ = g2d1CaptureReaderHealth(tc.stage, errGuardProtocol, errGuardProtocol, tc.obs)
		g2d1TerminalFail(errGuardProtocol)
		selectorReadRecord(t, readFD, tc.want)
	}
	// A closed broker diagnostic can reserve its observation and publish once
	// through borrowed stderr without changing that descriptor's flags.
	reset()
	borrow := client()
	borrow.pid++
	g2d1.fd, g2d1.closedAfterACK = -1, true
	if err := borrow.healthy(); !errors.Is(err, errGuardProtocol) || borrow.healthSelector.reason != selectorPIDMismatch {
		t.Fatalf("closed diagnostic health: %v %+v", err, borrow.healthSelector)
	}
	selectorPipeEmpty(t, readFD)
	fdFlags, err := unix.FcntlInt(2, unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := unix.Dup(2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Dup2(saved, 2); _, _ = unix.FcntlInt(2, unix.F_SETFD, fdFlags); _ = unix.Close(saved) }()
	if err = unix.Dup2(writeFD, 2); err != nil {
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
	_ = g2d1CaptureReaderHealth(g2d1ReaderHealthStage(borrow, false), errGuardProtocol, errGuardProtocol, &borrow.healthSelector)
	selectorPipeEmpty(t, readFD)
	g2d1TerminalFail(errGuardProtocol)
	g2d1TerminalFail(errGuardProtocol)
	g2d1Close()
	statusAfter, statusErr := unix.FcntlInt(2, unix.F_GETFL, 0)
	descriptorAfter, descriptorErr := unix.FcntlInt(2, unix.F_GETFD, 0)
	if statusErr != nil || descriptorErr != nil || statusBefore != statusAfter || descriptorBefore != descriptorAfter || g2d1.count != 1 {
		t.Fatal("borrowed stderr flags, ownership, or count changed")
	}
	selectorReadRecord(t, readFD, "G2D1 B BROKER_READER_PIDFD_BEFORE_PID_MISMATCH CHECK\n")
	if err = unix.Dup2(saved, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = unix.FcntlInt(2, unix.F_SETFD, fdFlags); err != nil {
		t.Fatal(err)
	}

	reset()
	g2d1.count = 23
	g2d1CaptureFailure("FIRST", unix.EACCES)
	obs := selectorObservation{reason: selectorPIDMismatch}
	if got := g2d1CaptureReaderHealth("LATER", errGuardProtocol, errGuardProtocol, &obs); got != errGuardProtocol {
		t.Fatal("prior return")
	}
	g2d1TerminalFail(errGuardProtocol)
	g2d1TerminalFail(errGuardProtocol)
	if g2d1.count != 24 {
		t.Fatalf("count=%d", g2d1.count)
	}
	selectorReadRecord(t, readFD, "G2D1 B FIRST ERRNO=13\n")
}
