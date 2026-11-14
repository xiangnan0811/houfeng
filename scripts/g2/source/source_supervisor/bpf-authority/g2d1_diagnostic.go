//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// G2D1 is temporary, best-effort source-launch diagnosis. Launcher and broker
// own a nonblocking console description; early PID1 and the two helpers borrow
// inherited stderr. Inherited descriptor flags and ownership are unchanged.
// Borrowed records are one write attempt; size does not bound latency or delivery.
var g2d1 = struct {
	fd, count            int
	role                 byte
	failed               bool
	owned                bool
	scope                string
	registerFD           int
	registered           bool
	captured             bool
	capturedStage        string
	capturedErr          error
	capturedSelector     selectorObservation
	capturedControl      brokerControlDiagnostic
	capturedControlRoute brokerControlTerminalRoute
	closedAfterACK       bool
}{fd: -1, registerFD: -1}

func g2d1HelperRole(args []string) byte {
	if len(args) != 2 || args[0] != "/g2-bpf-authority" {
		return 0
	}
	switch args[1] {
	case "audit-reader":
		return 'R'
	case "terminal-observer":
		return 'O'
	default:
		return 0
	}
}

func g2d1Open(role byte) {
	fd, err := unix.Open("/dev/console", unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return
	}
	var held, named unix.Stat_t
	if unix.Fstat(fd, &held) != nil || unix.Lstat("/dev/console", &named) != nil || held.Mode&unix.S_IFMT != unix.S_IFCHR || named.Mode&unix.S_IFMT != unix.S_IFCHR || held.Dev != named.Dev || held.Ino != named.Ino || held.Rdev != unix.Mkdev(5, 1) {
		_ = unix.Close(fd)
		return
	}
	g2d1.fd, g2d1.role, g2d1.owned = fd, role, true
}

func g2d1Close() {
	if g2d1.fd >= 0 {
		if g2d1.owned {
			_ = unix.Close(g2d1.fd)
		}
		g2d1.owned = false
		g2d1.fd = -1
	}
}

// Stage values are fixed literals selected by the instrumented branches. No
// error text or caller data is emitted. Errno comes only from the supplied error.
func g2d1Record(stage string, failure bool, err error) {
	if g2d1.role == 'R' && failure {
		g2d1CaptureFailure(stage, err)
		return
	}
	if g2d1.fd < 0 || g2d1.count >= 24 || g2d1.captured || (failure && g2d1.failed) || ((g2d1.role == 'E' || g2d1.role == 'R' || g2d1.role == 'O' || g2d1.role == 'B' && !g2d1.owned) && !failure) {
		return
	}
	if failure {
		g2d1.failed = true
	}
	g2d1Emit(stage, failure, err, nil)
}

func g2d1AppendStage(dst []byte, stage string, selector *selectorObservation) []byte {
	invalid := len(stage) == 0 || len(stage) > 64
	for _, c := range stage {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			invalid = true
			break
		}
	}
	var suffix string
	poll := false
	if selector != nil && selector.reason != selectorUnknown {
		if selector.pollFacts > 63 {
			invalid = true
		}
		switch selector.reason {
		case selectorInvalidFD:
			suffix = "_BAD_FD"
		case selectorZeroPID:
			suffix = "_ZERO_PID"
		case selectorPrePollError:
			suffix = "_PRE_POLL_ERR"
		case selectorPrePollRejected:
			suffix, poll = "_PRE_POLL_", true
		case selectorFDInfoRead:
			suffix = "_FDINFO_READ"
		case selectorPIDMissing:
			suffix = "_PID_MISSING"
		case selectorPIDDuplicate:
			suffix = "_PID_DUP"
		case selectorPIDParse:
			suffix = "_PID_PARSE"
		case selectorPIDMismatch:
			suffix = "_PID_MISMATCH"
		case selectorPostPollError:
			suffix = "_POST_POLL_ERR"
		case selectorPostPollRejected:
			suffix, poll = "_POST_POLL_", true
		default:
			invalid = true
		}
	}
	extra := len(suffix)
	if poll {
		extra += 2
	}
	if len(stage)+extra > 64 {
		invalid = true
	}
	if invalid {
		return append(dst, "INVALID_STAGE"...)
	}
	dst = append(dst, stage...)
	dst = append(dst, suffix...)
	if poll {
		const hex = "0123456789ABCDEF"
		dst = append(dst, hex[selector.pollFacts>>4], hex[selector.pollFacts&15])
	}
	return dst
}

func g2d1Emit(stage string, failure bool, err error, selector *selectorObservation) {
	var buf [128]byte
	line := append(buf[:0], "G2D1 "...)
	line = append(line, g2d1.role, ' ')
	if stage == "BROKER_CONTROL_DISPATCH" && g2d1.captured && g2d1.capturedControlRoute != 0 {
		line = g2d1AppendControlStage(line, g2d1.capturedControl, g2d1.capturedControlRoute)
	} else {
		line = g2d1AppendStage(line, stage, selector)
	}
	if failure {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno != 0 {
			line = append(line, " ERRNO="...)
			line = strconv.AppendUint(line, uint64(errno), 10)
		} else if err == nil || errors.Is(err, errGuardProtocol) || errors.Is(err, errControlFrame) {
			line = append(line, " CHECK"...)
		} else {
			line = append(line, " OTHER"...)
		}
	}
	line = append(line, '\n')
	g2d1.count++
	_, _ = unix.Write(g2d1.fd, line)
}

func g2d1Fail(stage string, err error) { g2d1Record(stage, true, err) }
func g2d1Mark(stage string)            { g2d1Record(stage, false, nil) }

// These selectors contain only fixed stage literals; no request data or raw poll
// bits are copied into a diagnostic record.
func g2d1ReaderHealthStage(r *auditReaderClient, event bool) string {
	if r == nil {
		if event {
			return "BROKER_READER_EVENT_NIL"
		}
		return "BROKER_READER_NIL"
	}
	switch r.healthReason {
	case readerHealthInvalid:
		if event {
			return "BROKER_READER_EVENT_INVALID"
		}
		return "BROKER_READER_INVALID"
	case readerHealthPIDFDBefore:
		if event {
			return "BROKER_READER_EVENT_PIDFD_BEFORE"
		}
		return "BROKER_READER_PIDFD_BEFORE"
	case readerHealthBootRead:
		if event {
			return "BROKER_READER_EVENT_BOOT_READ"
		}
		return "BROKER_READER_BOOT_READ"
	case readerHealthBootMismatch:
		if event {
			return "BROKER_READER_EVENT_BOOT_MISMATCH"
		}
		return "BROKER_READER_BOOT_MISMATCH"
	case readerHealthPollError:
		if event {
			return "BROKER_READER_EVENT_POLL_ERROR"
		}
		return "BROKER_READER_POLL_ERROR"
	case readerHealthPIDFDAfter:
		if event {
			return "BROKER_READER_EVENT_PIDFD_AFTER"
		}
		return "BROKER_READER_PIDFD_AFTER"
	case readerHealthEndpoint:
		if event {
			return "BROKER_READER_EVENT_ENDPOINT"
		}
		return "BROKER_READER_ENDPOINT"
	case readerHealthUnsolicited:
		if event {
			return "BROKER_READER_EVENT_UNSOLICITED"
		}
		return "BROKER_READER_UNSOLICITED"
	default:
		if event {
			return "BROKER_READER_EVENT_HEALTH"
		}
		return "BROKER_READER_HEALTH"
	}
}

func g2d1ReaderQueryEventStage(revents int16) string {
	switch revents & (unix.POLLERR | unix.POLLHUP | unix.POLLNVAL) {
	case unix.POLLERR:
		return "READER_QUERY_ERR"
	case unix.POLLHUP:
		return "READER_QUERY_HUP"
	case unix.POLLNVAL:
		return "READER_QUERY_NVAL"
	case unix.POLLERR | unix.POLLHUP:
		return "READER_QUERY_ERR_HUP"
	case unix.POLLERR | unix.POLLNVAL:
		return "READER_QUERY_ERR_NVAL"
	case unix.POLLHUP | unix.POLLNVAL:
		return "READER_QUERY_HUP_NVAL"
	case unix.POLLERR | unix.POLLHUP | unix.POLLNVAL:
		return "READER_QUERY_ERR_HUP_NVAL"
	default:
		return "READER_QUERY_EVENT"
	}
}

// Capture reserves the existing first-failure slot without writing to a possibly
// blocking inherited descriptor while the runtime still has cleanup to do.
func g2d1CaptureFailure(stage string, err error) {
	g2d1CaptureFailureWithControl(stage, err, nil, 0)
}

func g2d1CaptureFailureWithControl(stage string, err error, diagnostic *brokerControlDiagnostic, route brokerControlTerminalRoute) {
	if g2d1.failed || g2d1.count >= 24 {
		return
	}
	g2d1.capturedSelector = selectorObservation{}
	g2d1.capturedControl = brokerControlDiagnostic{}
	g2d1.capturedControlRoute = route
	if diagnostic != nil {
		g2d1.capturedControl = *diagnostic
	}
	g2d1.failed = true
	g2d1.captured = true
	g2d1.capturedStage, g2d1.capturedErr = stage, err
}

func g2d1CaptureBrokerControl(diagnostic *brokerControlDiagnostic, route brokerControlTerminalRoute, returned, observed error) error {
	if returned != nil {
		if observed == nil {
			observed = returned
		}
		g2d1CaptureFailureWithControl("BROKER_CONTROL_DISPATCH", observed, diagnostic, route)
	}
	return returned
}

func g2d1AppendControlStage(dst []byte, diagnostic brokerControlDiagnostic, route brokerControlTerminalRoute) []byte {
	if diagnostic.operation > 13 || diagnostic.command > 8 || diagnostic.first > 28 || diagnostic.returned > 28 || diagnostic.drop > 2 || route < 1 || route > 3 || (diagnostic.operation != brokerControlOpBind && diagnostic.command != 0) || diagnostic.bind.point > 65 || diagnostic.bind.before > 4 || diagnostic.bind.after > 4 || (diagnostic.bind.point == 0 && (diagnostic.bind.before != 0 || diagnostic.bind.after != 0)) || (diagnostic.operation != brokerControlOpBind && diagnostic.bind != (brokerBindDiagnostic{})) || diagnostic.bind.deletePoint > 25 || (diagnostic.bind.deletePoint != 0 && (diagnostic.operation != brokerControlOpBind || diagnostic.command != brokerControlCmdDelete || diagnostic.bind.point != brokerBindPointDeleteAuthorize)) || diagnostic.bind.targetPoint > 16 || (diagnostic.bind.targetPoint != 0 && (diagnostic.operation != brokerControlOpBind || diagnostic.command != brokerControlCmdDelete || diagnostic.bind.point != brokerBindPointDeleteAuthorize || diagnostic.bind.deletePoint != brokerDeletePointTarget)) {
		return append(dst, "INVALID_STAGE"...)
	}
	const hex = "0123456789ABCDEF"
	dst = append(dst, "BROKER_CONTROL_O"...)
	dst = append(dst, hex[uint8(diagnostic.operation)>>4], hex[uint8(diagnostic.operation)&15])
	dst = append(dst, "_C"...)
	dst = append(dst, hex[uint8(diagnostic.command)>>4], hex[uint8(diagnostic.command)&15])
	dst = append(dst, "_P"...)
	dst = append(dst, hex[uint8(diagnostic.first)>>4], hex[uint8(diagnostic.first)&15])
	dst = append(dst, "_R"...)
	dst = append(dst, hex[uint8(diagnostic.returned)>>4], hex[uint8(diagnostic.returned)&15])
	dst = append(dst, '_', 'D', '0'+diagnostic.drop, '_', 'T', '0'+byte(route))
	dst = append(dst, '_', 'B', hex[uint8(diagnostic.bind.point)>>4], hex[uint8(diagnostic.bind.point)&15], '_', 'S', '0'+byte(diagnostic.bind.before), '_', 'E', '0'+byte(diagnostic.bind.after))
	dst = append(dst, '_', 'A', hex[uint8(diagnostic.bind.deletePoint)>>4], hex[uint8(diagnostic.bind.deletePoint)&15])
	dst = append(dst, '_', 'G', hex[uint8(diagnostic.bind.targetPoint)>>4], hex[uint8(diagnostic.bind.targetPoint)&15])
	return dst
}

func g2d1CaptureReturned(stage string, returned, observed error) error {
	if returned != nil {
		if observed == nil {
			observed = returned
		}
		g2d1CaptureFailure(stage, observed)
	}
	return returned
}

func g2d1CaptureReaderHealth(stage string, returned, observed error, selector *selectorObservation) error {
	if selector != nil && selector.reason != selectorUnknown && selector.err != nil {
		observed = selector.err
	}
	failed := g2d1.failed
	result := g2d1CaptureReturned(stage, returned, observed)
	if returned != nil && !failed && g2d1.captured && selector != nil && selector.reason != selectorUnknown {
		g2d1.capturedSelector = *selector
	}
	return result
}

func g2d1TerminalFail(err error) {
	if g2d1.role == 'B' && g2d1.fd < 0 && g2d1.closedAfterACK {
		g2d1.fd, g2d1.owned = 2, false
	}
	if g2d1.captured {
		if g2d1.fd >= 0 && g2d1.count < 24 {
			g2d1Emit(g2d1.capturedStage, true, g2d1.capturedErr, &g2d1.capturedSelector)
		}
		g2d1.capturedSelector = selectorObservation{}
		g2d1.capturedControl = brokerControlDiagnostic{}
		g2d1.capturedControlRoute = 0
		g2d1.captured = false
		return
	}
	if g2d1.role == 'R' {
		// This is already the terminal output point, so avoid recapturing.
		if g2d1.fd >= 0 && g2d1.count < 24 && !g2d1.failed {
			g2d1.failed = true
			g2d1Emit("ENTRY", true, err, nil)
		}
		return
	}
	g2d1Fail("ENTRY", err)
}

// Record an actual operation error before a composite check converts it to a
// protocol error. An errno here describes that observed error, not the returned
// protocol error; no stage or ambient errno is used as evidence.
func g2d1EarlyCause(stage string, err error) {
	if (g2d1.role == 'E' || g2d1.role == 'O' && g2d1.scope != "") && err != nil {
		g2d1Fail(stage, err)
	}
}
