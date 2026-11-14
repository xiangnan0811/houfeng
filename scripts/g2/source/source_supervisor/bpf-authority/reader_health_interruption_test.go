//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const readerHealthTraceEnv = "G2_READER_HEALTH_TRACE"

type readerHealthPIDFDTraceSpec struct {
	family, position, sink, outcome string
}

func readerHealthPIDFDTraceSpecFor(mode string) (readerHealthPIDFDTraceSpec, bool) {
	p := strings.Split(mode, "/")
	if len(p) != 5 || p[0] != "pidfd" {
		return readerHealthPIDFDTraceSpec{}, false
	}
	s := readerHealthPIDFDTraceSpec{p[1], p[2], p[3], p[4]}
	h := s.position == "before-pre" || s.position == "before-post" || s.position == "after-pre" || s.position == "after-post"
	switch s.family {
	case "direct":
		validSink := s.sink == "on" || s.sink == "off"
		if h && ((validSink && (s.outcome == "healthy" || s.outcome == "hup" || s.outcome == "unsolicited" || s.outcome == "eio")) || (s.sink == "latched" && s.outcome == "healthy") || (validSink && (s.position == "after-pre" || s.position == "after-post") && (s.outcome == "pending-hup" || s.outcome == "pending-unsolicited"))) {
			return s, true
		}
	case "owner-death", "committed-effect", "event":
		if h && (s.sink == "on" || s.sink == "off") && s.outcome == "reject" || s.family == "event" && s.position == "none" && s.sink == "off" && s.outcome == "control" {
			return s, true
		}
	case "loop":
		if h && s.sink == "off" && (s.outcome == "barrier" || s.outcome == "deadline") {
			return s, true
		}
	case "bool-live":
		if s.position == "pre" && s.sink == "off" && s.outcome == "reject" {
			return s, true
		}
	case "observed-live", "observed-select":
		if (s.position == "pre" || s.position == "post") && (s.sink == "on" || s.sink == "off") && (s.outcome == "reject" || s.outcome == "eio") {
			return s, true
		}
	case "bool-select":
		if (s.position == "pre" || s.position == "post") && s.sink == "off" && s.outcome == "reject" {
			return s, true
		}
	case "fdinfo":
		if s.position == "before-open" && ((s.sink == "on" || s.sink == "off") && (s.outcome == "reader" || s.outcome == "observed") || s.sink == "off" && s.outcome == "bool") {
			return s, true
		}
	}
	return readerHealthPIDFDTraceSpec{}, false
}

type readerHealthTraceClock struct {
	initialized                                               bool
	offset, hostDeadline, lastReal, lastLogical, observations uint64
}

func (c *readerHealthTraceClock) translate(realNS uint64) (uint64, error) {
	const window = uint64(2 * time.Second)
	const deadline = authorityGuestDeadlineNS
	if c == nil {
		return 0, errGuardProtocol
	}
	if !c.initialized {
		if deadline <= window || realNS < deadline-window || realNS > ^uint64(0)-window {
			return 0, errGuardProtocol
		}
		logical := deadline - window
		*c = readerHealthTraceClock{initialized: true, offset: realNS - logical, hostDeadline: realNS + window, lastReal: realNS, lastLogical: logical, observations: 1}
		return logical, nil
	}
	if realNS < c.lastReal || realNS < c.offset || c.observations == ^uint64(0) {
		return 0, errGuardProtocol
	}
	logical := realNS - c.offset
	if logical < c.lastLogical {
		return 0, errGuardProtocol
	}
	c.lastReal, c.lastLogical, c.observations = realNS, logical, c.observations+1
	return logical, nil
}

func readerHealthTraceClockNS(raw [16]byte) (uint64, error) {
	sec := int64(binary.LittleEndian.Uint64(raw[:8]))
	nsec := int64(binary.LittleEndian.Uint64(raw[8:]))
	if sec < 0 || nsec < 0 || nsec >= 1_000_000_000 || uint64(sec) > (^uint64(0)-uint64(nsec))/1_000_000_000 {
		return 0, errGuardProtocol
	}
	return uint64(sec)*1_000_000_000 + uint64(nsec), nil
}

func TestReaderHealthTraceClock(t *testing.T) {
	t.Run("fixed-sequence", func(t *testing.T) {
		var c readerHealthTraceClock
		for _, tc := range []struct{ real, logical uint64 }{{4000000000000, 3298000000000}, {4001000000000, 3299000000000}, {4002000000000, 3300000000000}, {4003000000000, 3301000000000}, {4003000000000, 3301000000000}} {
			got, err := c.translate(tc.real)
			if err != nil || got != tc.logical || c.offset != 702000000000 || c.hostDeadline != 4002000000000 {
				t.Fatalf("translate(%d)=%d, %v state=%+v", tc.real, got, err, c)
			}
		}
		if c.observations != 5 || c.lastReal != 4003000000000 || c.lastLogical != 3301000000000 {
			t.Fatalf("incorrect final state: %+v", c)
		}
	})
	t.Run("regression", func(t *testing.T) {
		var c readerHealthTraceClock
		if _, err := c.translate(4000000000000); err != nil {
			t.Fatal(err)
		}
		before := c
		if _, err := c.translate(3999999999999); !errors.Is(err, errGuardProtocol) || c != before {
			t.Fatalf("regression: %v %+v", err, c)
		}
	})
	t.Run("initial-too-small", func(t *testing.T) {
		var c readerHealthTraceClock
		if _, err := c.translate(3297999999999); !errors.Is(err, errGuardProtocol) || c != (readerHealthTraceClock{}) {
			t.Fatalf("initial: %v %+v", err, c)
		}
	})
	t.Run("host-deadline-overflow", func(t *testing.T) {
		var c readerHealthTraceClock
		if _, err := c.translate(^uint64(0)); !errors.Is(err, errGuardProtocol) || c != (readerHealthTraceClock{}) {
			t.Fatalf("overflow: %v %+v", err, c)
		}
	})
	t.Run("failed-first-can-arm", func(t *testing.T) {
		var c readerHealthTraceClock
		if _, err := c.translate(3297999999999); !errors.Is(err, errGuardProtocol) || c != (readerHealthTraceClock{}) {
			t.Fatalf("initial: %v %+v", err, c)
		}
		got, err := c.translate(4000000000000)
		if err != nil || got != 3298000000000 || c.offset != 702000000000 || c.hostDeadline != 4002000000000 || c.observations != 1 {
			t.Fatalf("arm: %d %v %+v", got, err, c)
		}
	})
	t.Run("nil-receiver", func(t *testing.T) {
		var c *readerHealthTraceClock
		if _, err := c.translate(4000000000000); !errors.Is(err, errGuardProtocol) {
			t.Fatalf("nil: %v", err)
		}
	})
	t.Run("timespec-boundaries", func(t *testing.T) {
		for _, tc := range []struct {
			sec, nsec int64
			want      uint64
			valid     bool
		}{
			{-1, 0, 0, false}, {0, -1, 0, false}, {0, 1000000000, 0, false},
			{18446744073, 709551615, ^uint64(0), true}, {18446744073, 709551616, 0, false},
			{9223372036, 854775808, 9223372036854775808, true},
		} {
			var raw [16]byte
			binary.LittleEndian.PutUint64(raw[:8], uint64(tc.sec))
			binary.LittleEndian.PutUint64(raw[8:], uint64(tc.nsec))
			before := raw
			got, err := readerHealthTraceClockNS(raw)
			if raw != before || tc.valid && (err != nil || got != tc.want) || !tc.valid && !errors.Is(err, errGuardProtocol) {
				t.Fatalf("timespec %d,%d: %d %v", tc.sec, tc.nsec, got, err)
			}
		}
	})
}

func init() {
	if os.Getenv(readerHealthTraceEnv) != "" {
		runtime.LockOSThread()
	}
}

func TestMain(m *testing.M) {
	mode := os.Getenv(readerHealthTraceEnv)
	if mode == "" {
		os.Exit(m.Run())
	}
	if _, ok := readerHealthPIDFDTraceSpecFor(mode); ok {
		os.Exit(readerHealthTraceChild(mode))
	}
	switch mode {
	case "control", "socket-eintr-then-healthy", "socket-eintr-then-hup", "socket-eintr-then-unsolicited", "socket-eio", "already-invalid", "owner-death-eintr", "committed-effect-eintr", "loop-control", "loop-eintr-then-barrier", "loop-eintr-until-deadline":
		os.Exit(readerHealthTraceChild(mode))
	default:
		os.Exit(2)
	}
}

// The test child runs on the locked startup thread, rather than on a testing
// goroutine. This keeps both the traced syscall and broker's main-TID check on
// the exact same task.
func readerHealthTraceChild(mode string) int {
	fail := func(format string, args ...any) int {
		_, _ = fmt.Fprintf(os.Stderr, "reader trace child %s: %s\n", mode, fmt.Sprintf(format, args...))
		return 1
	}
	defer unix.Close(3)
	defer unix.Close(4)
	if unix.Gettid() != os.Getpid() {
		return fail("startup thread is not the process leader")
	}
	if err := pinProcDirectory(); err != nil {
		return fail("pin proc: %v", err)
	}
	defer unix.Close(heldProcDirectory)
	boot, err := fixedBootID()
	if err != nil {
		return fail("boot ID: %v", err)
	}
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return fail("pidfd: %v", err)
	}
	defer unix.Close(pidfd)
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fail("socketpair: %v", err)
	}
	defer unix.Close(pair[0])
	peer := pair[1]
	defer func() {
		if peer >= 0 {
			_ = unix.Close(peer)
		}
	}()
	guardFD, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fail("guard barrier: %v", err)
	}
	defer unix.Close(guardFD)
	if pair[0] == pidfd || pair[0] == guardFD || pidfd == guardFD {
		return fail("fixture descriptors alias")
	}
	reader, err := newAuditReaderClient(pair[0], pidfd, uint32(os.Getpid()), boot, 1)
	if err != nil {
		return fail("reader setup: %v", err)
	}
	deadline := authorityGuestDeadlineNS
	var meta [32]byte
	binary.LittleEndian.PutUint32(meta[0:4], 2)
	binary.LittleEndian.PutUint32(meta[4:8], uint32(unix.Gettid()))
	binary.LittleEndian.PutUint32(meta[8:12], uint32(pair[0]))
	binary.LittleEndian.PutUint32(meta[12:16], uint32(pidfd))
	binary.LittleEndian.PutUint32(meta[16:20], uint32(guardFD))
	binary.LittleEndian.PutUint32(meta[20:24], uint32(heldProcDirectory))
	binary.LittleEndian.PutUint64(meta[24:32], deadline)
	if n, e := unix.Write(3, meta[:]); e != nil || n != len(meta) {
		return fail("metadata write: %d %v", n, e)
	}
	var release [1]byte
	if n, e := unix.Read(4, release[:]); e != nil || n != 1 || release[0] != 1 {
		return fail("release: %d %v", n, e)
	}
	spec, pidfdMode := readerHealthPIDFDTraceSpecFor(mode)
	if pidfdMode {
		g2d1.role, g2d1.fd, g2d1.closedAfterACK = 'R', -1, true
		g2d1.failed, g2d1.captured, g2d1.count = false, false, 0
		if spec.sink == "on" {
			g2d1.role = 'B'
		}
		if spec.sink == "latched" {
			reader.healthReason = readerHealthEndpoint
			reader.healthSelector = selectorObservation{reason: selectorPIDParse, err: unix.EIO}
		}
		switch spec.family {
		case "owner-death":
			mode = "owner-death-eintr"
		case "committed-effect":
			mode = "committed-effect-eintr"
		case "loop":
			if spec.outcome == "deadline" {
				mode = "loop-eintr-until-deadline"
			} else {
				mode = "loop-eintr-then-barrier"
			}
		}
	}
	if pidfdMode {
		switch spec.family {
		case "bool-live":
			if livePIDFD(pidfd) {
				return fail("bool live accepted interrupted poll")
			}
			var st unix.Stat_t
			if unix.Fstat(pidfd, &st) != nil {
				return fail("bool live closed pidfd")
			}
			return 0
		case "bool-select":
			if pidfdSelects(pidfd, uint32(os.Getpid())) {
				return fail("bool selector accepted interrupted poll")
			}
			var st unix.Stat_t
			if unix.Fstat(pidfd, &st) != nil {
				return fail("bool selector closed pidfd")
			}
			return 0
		case "observed-live", "observed-select":
			var obs selectorObservation
			var sink *selectorObservation
			if spec.sink == "on" {
				sink = &obs
			}
			var selected bool
			var e error
			if spec.family == "observed-live" {
				phase := selectorPollPre
				if spec.position == "post" {
					phase = selectorPollPost
				}
				selected, e = livePIDFDObserved(pidfd, sink, phase)
			} else {
				selected, e = pidfdSelectsObserved(pidfd, uint32(os.Getpid()), sink)
			}
			want := unix.EINTR
			if spec.outcome == "eio" {
				want = unix.EIO
			}
			reason := selectorPrePollError
			if spec.position == "post" {
				reason = selectorPostPollError
			}
			if selected || !errors.Is(e, want) || sink != nil && (obs.reason != reason || !errors.Is(obs.err, want)) {
				return fail("observed selector: %t %v %+v", selected, e, obs)
			}
			var st unix.Stat_t
			if unix.Fstat(pidfd, &st) != nil {
				return fail("observed selector closed pidfd")
			}
			return 0
		case "fdinfo":
			if spec.outcome == "reader" {
				e := reader.healthy()
				if !errors.Is(e, errGuardProtocol) || errors.Is(e, unix.EINTR) || !reader.invalid || reader.healthReason != readerHealthPIDFDBefore {
					return fail("fdinfo reader: %v %+v", e, reader)
				}
			} else if spec.outcome == "bool" {
				if pidfdSelects(pidfd, uint32(os.Getpid())) {
					return fail("fdinfo bool accepted")
				}
			} else {
				var obs selectorObservation
				var sink *selectorObservation
				if spec.sink == "on" {
					sink = &obs
				}
				selected, e := pidfdSelectsObserved(pidfd, uint32(os.Getpid()), sink)
				if selected || e != nil || sink != nil && (obs.reason != selectorFDInfoRead || !errors.Is(obs.err, unix.EINTR)) {
					return fail("fdinfo observed: %t %v %+v", selected, e, obs)
				}
			}
			return 0
		case "event":
			checkpoint := &checkpointController{lifecycle: &checkpointLifecycle{}}
			tx := &runtimeEffectTransaction{id: 1, grantID: 1, command: "MAP_CREATE", state: "committed"}
			principal := &controlPrincipal{ID: 1, Role: "REALM_ROOT", PIDFD: pidfd}
			grant := &brokerGrant{id: 1, owner: 1, generation: 1, phase: "admitted"}
			transcript := &authorityTranscript{}
			txBefore, principalBefore, grantBefore, transcriptBefore := *tx, *principal, *grant, *transcript
			broker := &authorityBroker{authority: &sealedAuthority{guard: guardDevice{fd: guardFD}}, checkpoint: checkpoint, reader: reader, effects: &runtimeEffects{pending: tx}, principals: map[uint64]*controlPrincipal{1: principal}, grants: map[uint64]*brokerGrant{1: grant}, transcript: transcript}
			e := broker.rejectReaderEvent()
			if spec.outcome == "control" {
				if !errors.Is(e, errGuardProtocol) || g2d1.capturedStage != "BROKER_READER_EVENT" {
					return fail("event control: %v %s", e, g2d1.capturedStage)
				}
			} else {
				if !errors.Is(e, unix.ENOTTY) || !checkpoint.invalid || !checkpoint.faulted || reader.invalid || g2d1.capturedStage != "BROKER_READER_EVENT_HEALTH" || !errors.Is(g2d1.capturedErr, unix.EINTR) {
					return fail("event rejection: %v %s %+v", e, g2d1.capturedStage, checkpoint)
				}
				if reader.healthy() != nil || !checkpoint.faulted {
					return fail("event fault restored")
				}
			}
			if broker.effects.pending != tx || !reflect.DeepEqual(txBefore, *tx) || broker.principals[1] != principal || !reflect.DeepEqual(principalBefore, *principal) || broker.grants[1] != grant || !reflect.DeepEqual(grantBefore, *grant) || !reflect.DeepEqual(transcriptBefore, *transcript) {
				return fail("event changed protected state")
			}
			for _, fd := range []int{guardFD, reader.fd, reader.pidfd} {
				var st unix.Stat_t
				if unix.Fstat(fd, &st) != nil {
					return fail("event closed fd %d", fd)
				}
			}
			return 0
		case "direct":
			reader.deadline, reader.nonce, reader.check = 123456, "fixture-nonce", "status"
			reader.sent, reader.ready, reader.stableSet = true, true, true
			reader.status, reader.stable = observedAuditStatus{pid: 42}, observedAuditStatus{pid: 24}
			if spec.outcome != "unsolicited" && spec.outcome != "pending-unsolicited" {
				reader.request = []byte("fixture-request")
			}
			requestCopy := append([]byte(nil), reader.request...)
			if spec.outcome == "pending-hup" {
				if e := unix.Close(peer); e != nil {
					return fail("close peer: %v", e)
				}
				peer = -1
			}
			if spec.outcome == "pending-unsolicited" {
				if n, e := unix.Write(peer, []byte{1}); e != nil || n != 1 {
					return fail("send: %d %v", n, e)
				}
			}
			var socketBefore, pidBefore unix.Stat_t
			if unix.Fstat(reader.fd, &socketBefore) != nil || unix.Fstat(reader.pidfd, &pidBefore) != nil {
				return fail("stat before")
			}
			before := *reader
			e := reader.healthy()
			if !errors.Is(e, unix.EINTR) || !reflect.DeepEqual(before, *reader) || !reflect.DeepEqual(requestCopy, reader.request) || g2d1.captured || g2d1.failed {
				return fail("interrupted health: %v %+v", e, reader)
			}
			var socketAfter, pidAfter unix.Stat_t
			if unix.Fstat(reader.fd, &socketAfter) != nil || unix.Fstat(reader.pidfd, &pidAfter) != nil || socketBefore.Dev != socketAfter.Dev || socketBefore.Ino != socketAfter.Ino || socketBefore.Mode != socketAfter.Mode || pidBefore.Dev != pidAfter.Dev || pidBefore.Ino != pidAfter.Ino || pidBefore.Mode != pidAfter.Mode {
				return fail("descriptor identity changed")
			}
			if spec.outcome == "hup" {
				if e = unix.Close(peer); e != nil {
					return fail("close peer: %v", e)
				}
				peer = -1
			}
			if spec.outcome == "unsolicited" {
				if n, e := unix.Write(peer, []byte{1}); e != nil || n != 1 {
					return fail("send: %d %v", n, e)
				}
			}
			e = reader.healthy()
			if spec.outcome == "healthy" {
				if e != nil || reader.invalid {
					return fail("health recovery: %v", e)
				}
			} else {
				want := readerHealthEndpoint
				if spec.outcome == "unsolicited" || spec.outcome == "pending-unsolicited" {
					want = readerHealthUnsolicited
				}
				if spec.outcome == "eio" {
					want = readerHealthPIDFDBefore
				}
				if !errors.Is(e, errGuardProtocol) || !reader.invalid || reader.healthReason != want {
					return fail("permanent rejection: %v %v", e, reader.healthReason)
				}
				if spec.outcome == "eio" {
					if spec.sink == "on" && (reader.healthSelector.reason != selectorPrePollError || !errors.Is(reader.healthSelector.err, unix.EIO)) {
						return fail("EIO selector observation: %+v", reader.healthSelector)
					}
					if spec.sink == "off" && reader.healthSelector != (selectorObservation{}) {
						return fail("unexpected selector observation: %+v", reader.healthSelector)
					}
				}
				if !errors.Is(reader.healthy(), errGuardProtocol) || reader.healthReason != want {
					return fail("first reason changed")
				}
			}
			return 0
		}
	}

	switch mode {
	case "owner-death-eintr":
		checkpoint := &checkpointController{invalid: true, ownerDead: true}
		broker := &authorityBroker{checkpoint: checkpoint, authority: &sealedAuthority{guard: guardDevice{fd: guardFD}}, reader: reader, join: &auditJoin{}}
		if broker.canObserveOwnerDeath() || !checkpoint.invalid || !checkpoint.faulted || reader.invalid || reader.healthReason != readerHealthUnknown {
			return fail("owner-death guard: checkpoint=%+v reader=%+v", checkpoint, reader)
		}
		if reader.healthy() != nil || !checkpoint.faulted {
			return fail("owner-death fault was restored")
		}
		return 0
	case "committed-effect-eintr":
		ownerFD, e := unix.FcntlInt(uintptr(pidfd), unix.F_DUPFD_CLOEXEC, 0)
		if e != nil {
			return fail("owner pidfd: %v", e)
		}
		defer unix.Close(ownerFD)
		tx := &runtimeEffectTransaction{id: 1, grantID: 1, command: "MAP_CREATE", state: "committed"}
		before := *tx
		checkpoint := &checkpointController{guard: guardDevice{fd: guardFD}, lifecycle: &checkpointLifecycle{}, activeTransactions: 1}
		broker := &authorityBroker{selftestFD: &selftestFDArm{class: "map", owner: 1, grantID: 1, phase: "armed"}, effects: &runtimeEffects{pending: tx}, checkpoint: checkpoint, reader: reader, join: &auditJoin{}, principals: map[uint64]*controlPrincipal{1: {ID: 1, Role: "REALM_ROOT", PIDFD: ownerFD}}}
		calls := 0
		effect, e := broker.executeSelftestFD(tx, guardFD, func() (bpfEffect, error) { calls++; return bpfEffect{}, nil })
		if !errors.Is(e, errGuardProtocol) || effect.FD != -1 || calls != 0 || !checkpoint.invalid || !checkpoint.faulted || reader.invalid || !reflect.DeepEqual(before, *tx) || broker.effects.pending != tx {
			return fail("committed effect: effect=%+v error=%v calls=%d checkpoint=%+v reader=%+v", effect, e, calls, checkpoint, reader)
		}
		if reader.healthy() != nil || !checkpoint.faulted {
			return fail("effect fault was restored")
		}
		return 0
	case "loop-control", "loop-eintr-then-barrier", "loop-eintr-until-deadline":
		listener, e := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
		if e != nil {
			return fail("listener: %v", e)
		}
		defer unix.Close(listener)
		if e = unix.Bind(listener, &unix.SockaddrUnix{Name: fmt.Sprintf("\x00g2-reader-test-%d", os.Getpid())}); e != nil {
			return fail("bind: %v", e)
		}
		if e = unix.Listen(listener, 1); e != nil {
			return fail("listen: %v", e)
		}
		tx := &runtimeEffectTransaction{id: 1, grantID: 1, command: "MAP_CREATE", state: "committed"}
		before := *tx
		transcript := &authorityTranscript{}
		checkpoint := &checkpointController{lifecycle: &checkpointLifecycle{}}
		principal := &controlPrincipal{ID: 1, Role: "REALM_ROOT", PIDFD: pidfd}
		grant := &brokerGrant{id: 1, owner: 1, generation: 1, phase: "admitted"}
		principalBefore, grantBefore := *principal, *grant
		broker := &authorityBroker{authority: &sealedAuthority{guard: guardDevice{fd: guardFD}}, checkpoint: checkpoint, transcript: transcript, join: &auditJoin{}, audit: &auditMulticast{}, reader: reader, effects: &runtimeEffects{pending: tx}, principals: map[uint64]*controlPrincipal{1: principal}, grants: map[uint64]*brokerGrant{1: grant}}
		transcriptBefore := *transcript
		calls := 0
		handlers := brokerLoopHandlers{notification: func(*runtimeInvocation, seccompNotification) error { calls++; return errGuardProtocol }, deaths: func() error { calls++; return errGuardProtocol }}
		e = broker.serveBrokerPoll(listener, handlers)
		if mode == "loop-eintr-until-deadline" {
			if !errors.Is(e, errGuardProtocol) || g2d1.capturedStage != "BROKER_DEADLINE" || authorityGuestDeadlineNS != deadline {
				return fail("deadline result: %v stage=%s", e, g2d1.capturedStage)
			}
		} else if !errors.Is(e, unix.ENOTTY) || g2d1.capturedStage != "BROKER_GUARD_DRAIN" || !checkpoint.faulted {
			return fail("loop barrier: %v stage=%s checkpoint=%+v", e, g2d1.capturedStage, checkpoint)
		}
		if calls != 0 || !reflect.DeepEqual(before, *tx) || broker.effects.pending != tx || !reflect.DeepEqual(transcriptBefore, *transcript) || reader.invalid || broker.principals[1] != principal || !reflect.DeepEqual(principalBefore, *principal) || broker.grants[1] != grant || !reflect.DeepEqual(grantBefore, *grant) {
			return fail("loop published an effect or changed protected state")
		}
		for _, fd := range []int{listener, guardFD, reader.fd, reader.pidfd} {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil {
				return fail("loop closed fixture FD %d", fd)
			}
		}
		return 0
	case "already-invalid":
		reader.invalid = true
		reader.healthReason = readerHealthEndpoint
		if !errors.Is(reader.healthy(), errGuardProtocol) || reader.healthReason != readerHealthEndpoint {
			return fail("invalid reader revived")
		}
		return 0
	case "control":
		if e := reader.healthy(); e != nil || reader.invalid || reader.healthReason != readerHealthUnknown {
			return fail("control: %v", e)
		}
		return 0
	case "socket-eio":
		e := reader.healthy()
		if !errors.Is(e, unix.EIO) || !reader.invalid || reader.healthReason != readerHealthPollError {
			return fail("socket EIO: %v reason=%v", e, reader.healthReason)
		}
		return 0
	}
	reader.deadline, reader.nonce, reader.check = 123456, "fixture-nonce", "status"
	reader.sent, reader.ready, reader.stableSet = true, true, true
	reader.status = observedAuditStatus{pid: 42}
	reader.stable = observedAuditStatus{pid: 24}
	before := *reader
	e := reader.healthy()
	if !errors.Is(e, unix.EINTR) || reader.invalid || g2d1.captured || !reflect.DeepEqual(before, *reader) {
		return fail("socket EINTR must leave reader unchanged: err=%v before=%+v after=%+v", e, before, *reader)
	}
	var socketStat, pidfdStat unix.Stat_t
	if unix.Fstat(reader.fd, &socketStat) != nil || unix.Fstat(reader.pidfd, &pidfdStat) != nil {
		return fail("health changed descriptor ownership")
	}
	if mode == "socket-eintr-then-hup" {
		if e = unix.Close(peer); e != nil {
			return fail("close peer: %v", e)
		}
		peer = -1
	} else if mode == "socket-eintr-then-unsolicited" {
		if n, sendErr := unix.Write(peer, []byte{1}); sendErr != nil || n != 1 {
			return fail("send unsolicited: %d %v", n, sendErr)
		}
	}
	e = reader.healthy()
	if mode == "socket-eintr-then-healthy" {
		if e != nil || reader.invalid || reader.healthReason != readerHealthUnknown {
			return fail("reader did not recover: %v", e)
		}
	} else {
		want := readerHealthEndpoint
		if mode == "socket-eintr-then-unsolicited" {
			want = readerHealthUnsolicited
		}
		if !errors.Is(e, errGuardProtocol) || !reader.invalid || reader.healthReason != want {
			return fail("permanent failure: %v reason=%v", e, reader.healthReason)
		}
		if !errors.Is(reader.healthy(), errGuardProtocol) || reader.healthReason != want {
			return fail("first health reason changed")
		}
	}
	return 0
}

func TestReaderHealthInterruptedPoll(t *testing.T) {
	for _, mode := range []string{"control", "socket-eintr-then-healthy", "socket-eintr-then-hup", "socket-eintr-then-unsolicited", "socket-eio", "already-invalid", "owner-death-eintr", "committed-effect-eintr"} {
		t.Run(mode, func(t *testing.T) { readerHealthTraceCase(t, mode) })
	}
}

func TestBrokerReaderHealthInterruptedPoll(t *testing.T) {
	for _, mode := range []string{"loop-control", "loop-eintr-then-barrier", "loop-eintr-until-deadline"} {
		t.Run(mode, func(t *testing.T) { readerHealthTraceCase(t, mode) })
	}
}

func TestReaderPIDFDInterruptedPoll(t *testing.T) {
	positions := []string{"before-pre", "before-post", "after-pre", "after-post"}
	count := 0
	run := func(family string, ps, sinks, outcomes []string) {
		for _, position := range ps {
			for _, sink := range sinks {
				for _, outcome := range outcomes {
					mode := "pidfd/" + family + "/" + position + "/" + sink + "/" + outcome
					if _, ok := readerHealthPIDFDTraceSpecFor(mode); !ok {
						t.Fatalf("invalid required mode %s", mode)
					}
					count++
					t.Run(mode, func(t *testing.T) { readerHealthTraceCase(t, mode) })
				}
			}
		}
	}
	run("direct", positions, []string{"on", "off"}, []string{"healthy", "hup", "unsolicited", "eio"})
	run("direct", positions, []string{"latched"}, []string{"healthy"})
	run("direct", []string{"after-pre", "after-post"}, []string{"on", "off"}, []string{"pending-hup", "pending-unsolicited"})
	for _, family := range []string{"owner-death", "committed-effect", "event"} {
		run(family, positions, []string{"on", "off"}, []string{"reject"})
	}
	run("event", []string{"none"}, []string{"off"}, []string{"control"})
	run("loop", positions, []string{"off"}, []string{"barrier", "deadline"})
	run("bool-live", []string{"pre"}, []string{"off"}, []string{"reject"})
	run("observed-live", []string{"pre", "post"}, []string{"on", "off"}, []string{"reject", "eio"})
	run("bool-select", []string{"pre", "post"}, []string{"off"}, []string{"reject"})
	run("observed-select", []string{"pre", "post"}, []string{"on", "off"}, []string{"reject", "eio"})
	run("fdinfo", []string{"before-open"}, []string{"on", "off"}, []string{"reader", "observed"})
	run("fdinfo", []string{"before-open"}, []string{"off"}, []string{"bool"})
	if count != 101 {
		t.Fatalf("mode count %d", count)
	}
}

// readerHealthTraceCase is the sole waiter and tracer of its own child. It
// identifies syscall phase with GET_SYSCALL_INFO, never by toggling at traps.
func readerHealthTraceCase(t *testing.T, mode string) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	metaR, metaW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer metaR.Close()
	defer metaW.Close()
	releaseR, releaseW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer releaseR.Close()
	defer releaseW.Close()
	proc, err := os.StartProcess(os.Args[0], []string{os.Args[0]}, &os.ProcAttr{Env: append(os.Environ(), readerHealthTraceEnv+"="+mode), Files: []*os.File{os.Stdin, os.Stdout, os.Stderr, metaW, releaseR}, Sys: &unix.SysProcAttr{Ptrace: true, Pdeathsig: unix.SIGKILL}})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Release()
	pid := proc.Pid
	childPIDFD, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		_ = unix.Kill(pid, unix.SIGKILL)
		for {
			var status unix.WaitStatus
			_, e := unix.Wait4(pid, &status, 0, nil)
			if e != unix.EINTR {
				break
			}
		}
		t.Fatalf("open child pidfd: %v", err)
	}
	defer unix.Close(childPIDFD)
	reaped := false
	watchStop := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		timer := time.NewTimer(35 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			_ = unix.PidfdSendSignal(childPIDFD, unix.SIGKILL, nil, 0)
		case <-watchStop:
		}
	}()
	defer func() { close(watchStop); <-watchDone }()
	defer func() {
		if !reaped {
			_ = unix.PidfdSendSignal(childPIDFD, unix.SIGKILL, nil, 0)
			for {
				var status unix.WaitStatus
				_, e := unix.Wait4(pid, &status, 0, nil)
				if e != unix.EINTR {
					break
				}
			}
		}
	}()
	wait := func() unix.WaitStatus {
		for {
			var status unix.WaitStatus
			got, e := unix.Wait4(pid, &status, 0, nil)
			if e == unix.EINTR {
				continue
			}
			if e != nil || got != pid {
				t.Fatalf("wait4 pid=%d got=%d: %v", pid, got, e)
			}
			return status
		}
	}
	status := wait()
	if !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
		t.Fatalf("initial exec stop: %v", status)
	}
	if err = unix.PtraceSetOptions(pid, unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_EXITKILL); err != nil {
		t.Fatalf("ptrace options: %v", err)
	}
	if err = unix.PtraceSyscall(pid, 0); err != nil {
		t.Fatal(err)
	}
	var meta [32]byte
	ready := false
	var socketFD, pidFD, guardFD, procFD uint32
	var deadline uint64
	var lastNR, lastArg0, lastArg1, lastArg2 uint64
	inEntry := false
	injected := false
	var original uint64
	socketCount, pidCount, guardCount, injections := 0, 0, 0, 0
	spec, pidfdMode := readerHealthPIDFDTraceSpecFor(mode)
	clockEnabled := mode == "loop-control" || mode == "loop-eintr-then-barrier" || mode == "loop-eintr-until-deadline" || pidfdMode && spec.family == "loop"
	phaseCount := 0
	firstTargetClock := uint64(0)
	injectedErr := unix.EINTR
	var targetPoll, prePollSucceeded bool
	var clock readerHealthTraceClock
	var clockDelivered uint64
	var clockPending bool
	var clockAddress uintptr
	var clockID uint64
	var firstOffset, firstHostDeadline, firstSocketClock uint64
	var pollOrder []string
	for {
		status = wait()
		if status.Exited() || status.Signaled() {
			reaped = true
			if !status.Exited() || status.ExitStatus() != 0 {
				t.Fatalf("child termination: %v (ready=%t socket=%d pidfd=%d injected=%d guard=%d order=%v)", status, ready, socketCount, pidCount, injections, guardCount, pollOrder)
			}
			t.Logf("wait4 reaped pid=%d status=%v exit=%d", pid, status, status.ExitStatus())
			if inEntry && (lastNR == unix.SYS_EXIT_GROUP || lastNR == unix.SYS_EXIT) {
				inEntry = false
			}
			break
		}
		if !status.Stopped() {
			t.Fatalf("unexpected wait status: %v", status)
		}
		sig := status.StopSignal()
		if sig != unix.Signal(int(unix.SIGTRAP)|0x80) {
			if sig == unix.SIGTRAP {
				t.Fatal("unexpected ptrace trap")
			}
			if err = unix.PtraceSyscall(pid, int(sig)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var info [88]byte
		n, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_GET_SYSCALL_INFO, uintptr(pid), uintptr(len(info)), uintptr(unsafe.Pointer(&info[0])), 0, 0)
		if errno != 0 || n < 24 || binary.LittleEndian.Uint32(info[4:8]) != auditArchX8664 {
			t.Fatalf("GET_SYSCALL_INFO n=%d errno=%v arch=%x", n, errno, binary.LittleEndian.Uint32(info[4:8]))
		}
		switch info[0] {
		case 1:
			if n < 80 || inEntry || clockPending || injected {
				t.Fatalf("invalid syscall entry n=%d pending=%t", n, inEntry)
			}
			inEntry = true
			targetPoll = false
			lastNR = binary.LittleEndian.Uint64(info[24:32])
			lastArg0 = binary.LittleEndian.Uint64(info[32:40])
			lastArg1 = binary.LittleEndian.Uint64(info[40:48])
			lastArg2 = binary.LittleEndian.Uint64(info[48:56])
			if ready && clockEnabled && lastNR == unix.SYS_CLOCK_GETTIME && lastArg0 == uint64(unix.CLOCK_MONOTONIC) {
				clockAddress = uintptr(lastArg1)
				if lastArg1 == 0 || uint64(clockAddress) != lastArg1 || clockAddress > ^uintptr(0)-15 {
					t.Fatalf("invalid clock output address %#x", lastArg1)
				}
				clockID = lastArg0
				clockPending = true
			}
			if ready && lastNR == unix.SYS_IOCTL && uint32(lastArg0) == guardFD {
				if clockEnabled && clockDelivered == 0 {
					t.Fatal("guard reached before delivered clock")
				}
				guardCount++
				if pidfdMode && spec.family == "loop" && spec.outcome == "barrier" && (injections != 1 || phaseCount != 5 || clockDelivered <= firstTargetClock) {
					t.Fatalf("guard reached before complete reobservation: order=%v", pollOrder)
				}
				if mode == "loop-eintr-then-barrier" && (socketCount != 2 || pidCount != 6) {
					t.Fatalf("guard reached before complete second health observation: order=%v", pollOrder)
				}
				if mode == "loop-eintr-until-deadline" || pidfdMode && spec.family == "loop" && spec.outcome == "deadline" {
					t.Fatal("guard reached during interrupted observations")
				}
			}
			if ready && lastNR == unix.SYS_PPOLL && lastArg1 == 1 {
				var raw [8]byte
				if n, e := unix.PtracePeekData(pid, uintptr(lastArg0), raw[:]); e != nil || n != len(raw) {
					t.Fatalf("read pollfd: n=%d error=%v", n, e)
				}
				fd := binary.LittleEndian.Uint32(raw[:4])
				events := binary.LittleEndian.Uint16(raw[4:6])
				timeout := binary.LittleEndian.Uint64(info[48:56])
				mask := binary.LittleEndian.Uint64(info[56:64])
				if fd == socketFD || fd == pidFD {
					targetPoll = fd == pidFD
					if events != unix.POLLIN || timeout == 0 || mask != 0 {
						t.Fatalf("unexpected target poll fd=%d events=%d timeout=%x mask=%x", fd, events, timeout, mask)
					}
					var ts [16]byte
					if n, e := unix.PtracePeekData(pid, uintptr(timeout), ts[:]); e != nil || n != len(ts) {
						t.Fatalf("read timeout: n=%d error=%v", n, e)
					}
					if ts != ([16]byte{}) {
						t.Fatal("nonzero poll timeout")
					}
					if fd == socketFD {
						socketCount++
						pollOrder = append(pollOrder, "socket")
						if clockEnabled && !pidfdMode {
							if clockDelivered == 0 || clock.lastLogical >= authorityGuestDeadlineNS {
								t.Fatalf("socket reached without an unexpired delivered clock: %+v", clock)
							}
							if socketCount <= 2 {
								now, e := monoNowNS()
								if e != nil || now >= firstHostDeadline {
									t.Fatalf("socket outside host window: now=%d deadline=%d error=%v", now, firstHostDeadline, e)
								}
							}
							if socketCount == 1 {
								firstSocketClock = clockDelivered
							} else if socketCount == 2 && clockDelivered <= firstSocketClock {
								t.Fatal("second observation lacked a fresh clock read")
							}
						}
					} else {
						pidCount++
						pollOrder = append(pollOrder, "pidfd")
					}
					if pidfdMode {
						phaseCount++
						if spec.family == "loop" && phaseCount == 1 {
							if clockDelivered == 0 || clock.lastLogical >= authorityGuestDeadlineNS {
								t.Fatal("loop target without unexpired clock")
							}
							now, e := monoNowNS()
							if e != nil || now >= firstHostDeadline {
								t.Fatal("loop outside host window")
							}
							if injections == 0 {
								firstTargetClock = clockDelivered
							} else if clockDelivered <= firstTargetClock {
								t.Fatal("loop lacked fresh clock")
							}
						}
					}
					inject := fd == socketFD && (strings.HasPrefix(mode, "socket-") || mode == "owner-death-eintr" || mode == "committed-effect-eintr" || mode == "loop-eintr-then-barrier") && socketCount == 1
					if pidfdMode && fd == pidFD && spec.family != "fdinfo" && spec.outcome != "control" {
						target := 1
						switch spec.position {
						case "before-post", "post":
							target = 2
						case "after-pre":
							target = 4
						case "after-post":
							target = 5
						}
						if spec.family == "observed-live" {
							target = 1
						}
						inject = phaseCount == target && (injections == 0 || spec.family == "loop" && spec.outcome == "deadline")
						if spec.family == "direct" && spec.outcome == "eio" && injections == 1 && phaseCount == 1 {
							inject = true
						}
						if inject && spec.family == "loop" {
							now, e := monoNowNS()
							if e != nil || now >= firstHostDeadline || clock.lastLogical >= authorityGuestDeadlineNS {
								t.Fatal("target outside fixed clock window")
							}
						}
						if inject && spec.family == "loop" && spec.outcome == "deadline" && injections == 1 {
							if clockDelivered <= firstTargetClock || clock.lastLogical >= authorityGuestDeadlineNS {
								t.Fatal("deadline second observation clock")
							}
							now, e := monoNowNS()
							if e != nil || now >= firstHostDeadline {
								t.Fatal("deadline window exhausted")
							}
							remaining := firstHostDeadline - now
							if remaining == 0 || remaining > uint64(2*time.Second) {
								t.Fatal("invalid deadline window")
							}
							timer := time.NewTimer(time.Duration(remaining))
							<-timer.C
							timer.Stop()
							now, e = monoNowNS()
							if e != nil || now < firstHostDeadline {
								t.Fatal("timer returned early")
							}
						}
					}
					if mode == "loop-eintr-until-deadline" && fd == socketFD {
						inject = true
						if socketCount == 2 {
							now, e := monoNowNS()
							if e != nil || now >= firstHostDeadline {
								t.Fatalf("deadline window exhausted: now=%d deadline=%d error=%v", now, firstHostDeadline, e)
							}
							remaining := firstHostDeadline - now
							if remaining == 0 || remaining > uint64(2*time.Second) {
								t.Fatalf("invalid deadline window: %d", remaining)
							}
							timer := time.NewTimer(time.Duration(remaining))
							<-timer.C
							timer.Stop()
							now, e = monoNowNS()
							if e != nil || now < firstHostDeadline {
								t.Fatalf("timer returned before host deadline: now=%d deadline=%d error=%v", now, firstHostDeadline, e)
							}
						}
					}
					if inject {
						injectedErr = unix.EINTR
						if pidfdMode && (spec.outcome == "eio" && (spec.family != "direct" || injections == 1)) || mode == "socket-eio" {
							injectedErr = unix.EIO
						}
					}
					if inject {
						var regs unix.PtraceRegs
						if err = unix.PtraceGetRegs(pid, &regs); err != nil {
							t.Fatal(err)
						}
						original = regs.Orig_rax
						regs.Orig_rax = ^uint64(0)
						if err = unix.PtraceSetRegs(pid, &regs); err != nil {
							t.Fatal(err)
						}
						injected = true
						injections++
						t.Logf("injection entry tid=%d fd=%d syscall=%d index=%d", pid, fd, lastNR, injections)
					}
				}
			}
			if ready && pidfdMode && spec.family == "fdinfo" && lastNR == unix.SYS_OPENAT && uint32(lastArg0) == procFD && prePollSucceeded && injections == 0 {
				if lastArg2 != uint64(unix.O_RDONLY|unix.O_CLOEXEC) {
					t.Fatal("unexpected fdinfo flags")
				}
				want := fmt.Sprintf("self/fdinfo/%d", pidFD)
				raw := make([]byte, len(want)+1)
				if n, e := unix.PtracePeekData(pid, uintptr(lastArg1), raw); e != nil || n != len(raw) || string(raw) != want+"\x00" {
					t.Fatalf("fdinfo path: %q %v", raw, e)
				}
				var regs unix.PtraceRegs
				if err = unix.PtraceGetRegs(pid, &regs); err != nil {
					t.Fatal(err)
				}
				original, regs.Orig_rax = regs.Orig_rax, ^uint64(0)
				if err = unix.PtraceSetRegs(pid, &regs); err != nil {
					t.Fatal(err)
				}
				injected, injectedErr = true, unix.EINTR
				injections++
				t.Logf("fdinfo injection entry tid=%d dirfd=%d path=%s", pid, procFD, want)
			}
		case 2:
			if n < 33 || !inEntry {
				t.Fatalf("invalid syscall exit n=%d pending=%t", n, inEntry)
			}
			result := int64(binary.LittleEndian.Uint64(info[24:32]))
			if pidfdMode && spec.family == "fdinfo" && targetPoll && !injected && result == 0 && phaseCount == 1 {
				prePollSucceeded = true
			}
			eligibleClock := ready && clockEnabled && lastNR == unix.SYS_CLOCK_GETTIME && lastArg0 == uint64(unix.CLOCK_MONOTONIC)
			if clockPending != eligibleClock || clockPending && (injected || clockID != lastArg0 || uint64(clockAddress) != lastArg1) {
				t.Fatal("clock entry/exit pairing differs")
			}
			if clockPending {
				if result != 0 {
					t.Fatalf("clock syscall result=%d", result)
				}
				var raw [16]byte
				if n, e := unix.PtracePeekData(pid, clockAddress, raw[:]); e != nil || n != len(raw) {
					t.Fatalf("read clock output: n=%d error=%v", n, e)
				}
				realNS, e := readerHealthTraceClockNS(raw)
				if e != nil {
					t.Fatalf("decode clock output: %v", e)
				}
				logicalNS, e := clock.translate(realNS)
				if e != nil {
					t.Fatalf("translate clock output: %v", e)
				}
				var replacement [16]byte
				binary.LittleEndian.PutUint64(replacement[:8], logicalNS/1_000_000_000)
				binary.LittleEndian.PutUint64(replacement[8:], logicalNS%1_000_000_000)
				if n, e := unix.PtracePokeData(pid, clockAddress, replacement[:]); e != nil || n != len(replacement) {
					t.Fatalf("write clock output: n=%d error=%v", n, e)
				}
				var check [16]byte
				if n, e := unix.PtracePeekData(pid, clockAddress, check[:]); e != nil || n != len(check) || check != replacement {
					t.Fatalf("clock readback: n=%d error=%v", n, e)
				}
				clockDelivered++
				if clockDelivered == 1 {
					firstOffset, firstHostDeadline = clock.offset, clock.hostDeadline
				} else if clock.offset != firstOffset || clock.hostDeadline != firstHostDeadline {
					t.Fatal("clock mapping changed")
				}
				t.Logf("clock delivered tid=%d sec=%d nsec=%d real=%d logical=%d offset=%d hostDeadline=%d ordinal=%d", pid, int64(binary.LittleEndian.Uint64(raw[:8])), int64(binary.LittleEndian.Uint64(raw[8:])), realNS, logicalNS, clock.offset, clock.hostDeadline, clockDelivered)
				clockPending = false
			}
			if injected {
				if result != -int64(unix.ENOSYS) {
					t.Fatalf("skipped syscall result=%d", result)
				}
				var regs unix.PtraceRegs
				if err = unix.PtraceGetRegs(pid, &regs); err != nil {
					t.Fatal(err)
				}
				wanted := injectedErr
				regs.Rax = ^uint64(wanted - 1)
				regs.Orig_rax = original
				if err = unix.PtraceSetRegs(pid, &regs); err != nil {
					t.Fatal(err)
				}
				injected = false
				if pidfdMode && lastNR == unix.SYS_PPOLL {
					phaseCount = 0
				}
				t.Logf("injection exit tid=%d return=-%d", pid, wanted)
			}
			if !ready && lastNR == unix.SYS_WRITE && lastArg0 == 3 && lastArg1 != 0 && lastArg2 == 32 && result == 32 {
				if _, err = io.ReadFull(metaR, meta[:]); err != nil {
					t.Fatalf("metadata: %v", err)
				}
				if binary.LittleEndian.Uint32(meta[:4]) != 2 || int(binary.LittleEndian.Uint32(meta[4:8])) != pid {
					t.Fatal("bad metadata")
				}
				socketFD = binary.LittleEndian.Uint32(meta[8:12])
				pidFD = binary.LittleEndian.Uint32(meta[12:16])
				guardFD = binary.LittleEndian.Uint32(meta[16:20])
				procFD = binary.LittleEndian.Uint32(meta[20:24])
				deadline = binary.LittleEndian.Uint64(meta[24:32])
				if socketFD < 5 || pidFD < 5 || guardFD < 5 || procFD < 5 || socketFD == pidFD || socketFD == guardFD || pidFD == guardFD || procFD == socketFD || procFD == pidFD || procFD == guardFD || deadline == 0 {
					t.Fatal("invalid metadata descriptors")
				}
				if clockEnabled && deadline != authorityGuestDeadlineNS {
					t.Fatalf("unexpected metadata deadline %d", deadline)
				}
				if n, e := releaseW.Write([]byte{1}); e != nil || n != 1 {
					t.Fatalf("release write: n=%d error=%v", n, e)
				}
				t.Logf("metadata released version=%d tid=%d socket=%d pidfd=%d guard=%d held-proc=%d deadline=%d", binary.LittleEndian.Uint32(meta[:4]), binary.LittleEndian.Uint32(meta[4:8]), socketFD, pidFD, guardFD, procFD, deadline)
				ready = true
			}
			inEntry = false
		default:
			t.Fatalf("unexpected syscall info op=%d", info[0])
		}
		if err = unix.PtraceSyscall(pid, 0); err != nil {
			t.Fatal(err)
		}
	}
	if !ready || inEntry || injected || clockPending {
		t.Fatal("incomplete trace or metadata handshake")
	}
	if clockEnabled {
		if !clock.initialized || clockDelivered == 0 || clockDelivered != clock.observations {
			t.Fatalf("incomplete clock delivery: delivered=%d clock=%+v", clockDelivered, clock)
		}
	} else if clock != (readerHealthTraceClock{}) || clockDelivered != 0 {
		t.Fatalf("direct mode changed clock: delivered=%d clock=%+v", clockDelivered, clock)
	}
	if pidfdMode {
		full := []string{"pidfd", "pidfd", "socket", "pidfd", "pidfd"}
		var prefix []string
		switch spec.position {
		case "before-pre", "pre", "before-open":
			prefix = []string{"pidfd"}
		case "before-post", "post":
			prefix = []string{"pidfd", "pidfd"}
		case "after-pre":
			prefix = []string{"pidfd", "pidfd", "socket", "pidfd"}
		case "after-post":
			prefix = []string{"pidfd", "pidfd", "socket", "pidfd", "pidfd"}
		}
		if spec.family == "observed-live" {
			prefix = []string{"pidfd"}
		}
		var expected []string
		switch spec.family {
		case "direct":
			expected = append(expected, prefix...)
			if spec.outcome == "eio" {
				expected = append(expected, "pidfd")
			} else {
				expected = append(expected, full...)
			}
		case "owner-death", "committed-effect":
			expected = append(append(expected, prefix...), full...)
		case "event":
			if spec.outcome == "control" {
				expected = append(expected, full...)
			} else {
				expected = append(append(expected, prefix...), full...)
			}
		case "loop":
			expected = append(expected, prefix...)
			if spec.outcome == "deadline" {
				expected = append(expected, prefix...)
			} else {
				expected = append(expected, full...)
			}
		default:
			expected = append(expected, prefix...)
		}
		wantInjections := 1
		if spec.outcome == "control" {
			wantInjections = 0
		}
		if spec.family == "loop" && spec.outcome == "deadline" || spec.family == "direct" && spec.outcome == "eio" {
			wantInjections = 2
		}
		if injections != wantInjections || !reflect.DeepEqual(pollOrder, expected) {
			t.Fatalf("matrix trace injections=%d want=%d order=%v want=%v", injections, wantInjections, pollOrder, expected)
		}
		wantGuard := 0
		if spec.family == "event" && spec.outcome == "reject" || spec.family == "loop" && spec.outcome == "barrier" {
			wantGuard = 1
		}
		if guardCount != wantGuard {
			t.Fatalf("guard calls=%d want=%d", guardCount, wantGuard)
		}
		if spec.family == "loop" && spec.outcome == "deadline" && (clock.lastLogical < authorityGuestDeadlineNS || clock.offset != firstOffset || clock.hostDeadline != firstHostDeadline || deadline != authorityGuestDeadlineNS) {
			t.Fatal("deadline mapping changed or not reached")
		}
		t.Logf("matrix trace tid=%d injections=%d guard=%d order=%v", pid, injections, guardCount, pollOrder)
		return
	}
	wantInject := mode != "control" && mode != "loop-control" && mode != "already-invalid"
	if wantInject && injections == 0 || !wantInject && injections != 0 {
		t.Fatalf("injections=%d", injections)
	}
	if mode != "loop-eintr-until-deadline" && wantInject && injections != 1 {
		t.Fatalf("expected one injection, got %d", injections)
	}
	if !strings.HasPrefix(mode, "loop-") && guardCount != 0 {
		t.Fatalf("unexpected guard ioctl: %d", guardCount)
	}
	var expectedOrder []string
	switch mode {
	case "control", "loop-control":
		expectedOrder = []string{"pidfd", "pidfd", "socket", "pidfd", "pidfd"}
	case "socket-eintr-then-healthy", "socket-eintr-then-hup", "socket-eintr-then-unsolicited", "owner-death-eintr", "committed-effect-eintr", "loop-eintr-then-barrier":
		expectedOrder = []string{"pidfd", "pidfd", "socket", "pidfd", "pidfd", "socket", "pidfd", "pidfd"}
	case "socket-eio":
		expectedOrder = []string{"pidfd", "pidfd", "socket"}
	case "loop-eintr-until-deadline":
		expectedOrder = []string{"pidfd", "pidfd", "socket", "pidfd", "pidfd", "socket"}
	case "already-invalid":
		expectedOrder = nil
	}
	if !reflect.DeepEqual(pollOrder, expectedOrder) {
		t.Fatalf("poll order=%v want=%v", pollOrder, expectedOrder)
	}
	if mode == "loop-eintr-then-barrier" && (socketCount != 2 || guardCount != 1) {
		t.Fatal("guard barrier did not follow a fresh complete health observation")
	}
	if mode == "already-invalid" && (socketCount != 0 || pidCount != 0) {
		t.Fatalf("invalid reader polled: %v", pollOrder)
	}
	if strings.HasPrefix(mode, "loop-") {
		if mode == "loop-eintr-until-deadline" && (guardCount != 0 || injections != 2 || socketCount != 2 || clock.lastLogical < authorityGuestDeadlineNS || clock.offset != firstOffset || clock.hostDeadline != firstHostDeadline || deadline != authorityGuestDeadlineNS) {
			t.Fatalf("deadline trace guard=%d injections=%d socket=%d clock=%+v", guardCount, injections, socketCount, clock)
		}
		if mode != "loop-eintr-until-deadline" && guardCount != 1 {
			t.Fatalf("guard barrier calls=%d", guardCount)
		}
	}
	t.Logf("trace completed tid=%d socket=%d pidfd=%d injections=%d guard=%d order=%v", pid, socketCount, pidCount, injections, guardCount, pollOrder)
}
