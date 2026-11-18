//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type observedAuditStatus struct{ mask, enabled, failure, pid, rate, backlogLimit, lost, featureBitmap, backlogWaitTime uint32 }

// readerDetection captures the first production validator which rejected an
// observation. The receive record contains facts, never descriptor ownership.
type readerDetection struct {
	detector  string
	requestID uint64
	nonce     string
	deadline  uint64
	observed  uint64
	receive   controlReceiveObservation
}

type readerHealthReason uint8

const (
	readerHealthUnknown readerHealthReason = iota
	readerHealthInvalid
	readerHealthPIDFDBefore
	readerHealthBootRead
	readerHealthBootMismatch
	readerHealthPollError
	readerHealthPIDFDAfter
	readerHealthEndpoint
	readerHealthUnsolicited
)

// auditReaderClient is driven by the broker's existing event loop. It owns the
// query socket and reader pidfd; neither a pending request nor a dead reader may
// be hidden by a cached status. Only one request is outstanding at a time.
type auditReaderClient struct {
	fd, pidfd                int
	pid                      uint32
	querySequence, queryPort uint32
	boot                     string
	cookie, next, deadline   uint64
	nonce, check             string
	request                  []byte
	sent, ready, invalid     bool
	body                     map[string]any
	status                   observedAuditStatus
	stable                   observedAuditStatus
	stableSet                bool
	locked                   bool
	owner                    uint32
	detection                *readerDetection
	healthReason             readerHealthReason
	healthSelector           selectorObservation
}

func newAuditReaderClient(fd, pidfd int, pid uint32, boot string, cookie uint64) (*auditReaderClient, error) {
	actual, err := fixedBootID()
	if err != nil || boot != actual || pid <= 1 || cookie == 0 || fd < 0 || pidfd < 0 || !pidfdSelects(pidfd, pid) {
		return nil, errGuardProtocol
	}
	typ, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || typ != unix.SOCK_SEQPACKET {
		return nil, errGuardProtocol
	}
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return nil, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	return &auditReaderClient{fd: fd, pidfd: pidfd, pid: pid, boot: boot, cookie: cookie, next: 1}, nil
}

func (r *auditReaderClient) close() error {
	if r == nil {
		return nil
	}
	r.invalid = true
	e := unix.Close(r.fd)
	r.fd = -1
	p := unix.Close(r.pidfd)
	r.pidfd = -1
	if e != nil {
		return e
	}
	return p
}

func (r *auditReaderClient) fail(err error) error {
	r.invalid = true
	if err == nil {
		return errGuardProtocol
	}
	return err
}

func (r *auditReaderClient) failDetected(detector string, observation controlReceiveObservation, err error) error {
	if r.detection == nil {
		now, _ := monoNowNS()
		r.detection = &readerDetection{detector: detector, requestID: r.next, nonce: r.nonce, deadline: r.deadline, observed: now, receive: observation}
	}
	return r.fail(err)
}

func (r *auditReaderClient) noteHealthReason(reason readerHealthReason) {
	if r.healthReason == readerHealthUnknown {
		r.healthReason = reason
	}
}

func (r *auditReaderClient) selectHealthPIDFD(reason readerHealthReason) error {
	var observation selectorObservation
	var sink *selectorObservation
	if r.healthReason == readerHealthUnknown && g2d1.role == 'B' && (g2d1.fd >= 0 || g2d1.closedAfterACK) && !g2d1.failed && !g2d1.captured && g2d1.count < 24 {
		sink = &observation
	}
	selected, pollErr := pidfdSelectsObserved(r.pidfd, r.pid, sink)
	if selected {
		return nil
	}
	if errors.Is(pollErr, unix.EINTR) {
		return pollErr
	}
	if r.healthReason == readerHealthUnknown && sink != nil {
		r.healthSelector = observation
	}
	r.noteHealthReason(reason)
	return errGuardProtocol
}

// healthy checks for death, EOF or unsolicited packets without consuming a
// frame. Call before each admission and include pidfd in the event-loop poll.
func (r *auditReaderClient) healthy() error {
	if r == nil {
		return errGuardProtocol
	}
	if r.invalid {
		r.noteHealthReason(readerHealthInvalid)
		return errGuardProtocol
	}
	if err := r.selectHealthPIDFD(readerHealthPIDFDBefore); err != nil {
		if errors.Is(err, unix.EINTR) {
			return err
		}
		return r.failDetected("reader_pidfd_exited", controlReceiveObservation{}, errGuardProtocol)
	}
	boot, err := fixedBootID()
	if err != nil || boot != r.boot {
		if err != nil {
			r.noteHealthReason(readerHealthBootRead)
		} else {
			r.noteHealthReason(readerHealthBootMismatch)
		}
		return r.fail(errGuardProtocol)
	}
	p := []unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}}
	_, err = unix.Poll(p, 0)
	if errors.Is(err, unix.EINTR) {
		return err
	}
	if err != nil {
		r.noteHealthReason(readerHealthPollError)
		return r.fail(err)
	}
	if err := r.selectHealthPIDFD(readerHealthPIDFDAfter); err != nil {
		if errors.Is(err, unix.EINTR) {
			return err
		}
		return r.failDetected("reader_pidfd_exited", controlReceiveObservation{}, errGuardProtocol)
	}
	if p[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
		r.noteHealthReason(readerHealthEndpoint)
		return r.failDetected("reader_endpoint_closed", controlReceiveObservation{}, errGuardProtocol)
	}
	if r.request == nil && p[0].Revents&unix.POLLIN != 0 {
		r.noteHealthReason(readerHealthUnsolicited)
		return r.fail(errGuardProtocol)
	}
	return nil
}

func (r *auditReaderClient) begin(check, nonce string, deadlineNS uint64) error {
	if r == nil || r.invalid || r.request != nil || r.ready || r.next == 0 || r.next == math.MaxUint64 || (check != "status" && check != "configuration") || !validCheckpointNonce(nonce) || !pidfdSelects(r.pidfd, r.pid) {
		return errGuardProtocol
	}
	deadline, err := clampGuestDeadlineNS(deadlineNS)
	if err != nil {
		return r.fail(err)
	}
	now, err := monoNowNS()
	if err != nil {
		return r.fail(err)
	}
	maximum := now + uint64((5 * time.Second).Nanoseconds())
	if maximum < now {
		return r.fail(errGuardProtocol)
	}
	if deadline > maximum {
		deadline = maximum
	}
	raw, err := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": r.boot, "request_id": jsonU64(r.next), "nonce": nonce, "check": check})
	if err != nil {
		return r.fail(err)
	}
	r.request, r.check, r.nonce, r.deadline, r.sent = raw, check, nonce, deadline, false
	return nil
}

func auditReaderReplyFrame(raw []byte, boot string, requestID uint64, nonce, check string) (map[string]any, string, error) {
	reply, err := parseCanonicalControl(raw)
	if err != nil {
		return nil, "reader_noncanonical_frame", err
	}
	if !exactKeys(reply, "version", "boot_id", "request_id", "nonce", "check", "queries") || reply["version"] != json.Number("1") || reply["boot_id"] != boot || reply["request_id"] != jsonU64(requestID) || reply["nonce"] != nonce || reply["check"] != check {
		return nil, "reader_response_identity", errGuardProtocol
	}
	return reply, "", nil
}

// service never waits. The owner must also invoke it on timeout and before
// committing new effects, even if the socket was not reported readable.
func (r *auditReaderClient) service() (bool, error) {
	if r == nil || r.invalid {
		return false, errGuardProtocol
	}
	if !pidfdSelects(r.pidfd, r.pid) {
		return false, r.failDetected("reader_pidfd_exited", controlReceiveObservation{}, errGuardProtocol)
	}
	boot, bootErr := fixedBootID()
	if bootErr != nil || boot != r.boot {
		return false, r.fail(errGuardProtocol)
	}
	if r.request == nil || r.ready {
		return false, r.fail(errGuardProtocol)
	}
	now, err := monoNowNS()
	if err != nil {
		return false, r.fail(err)
	}
	if now >= r.deadline {
		return false, r.failDetected("reader_deadline", controlReceiveObservation{}, unix.ETIMEDOUT)
	}
	if !r.sent {
		err = sendControl(r.fd, r.request, nil)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return false, nil
		}
		if err != nil {
			return false, r.fail(err)
		}
		r.sent = true
	}
	raw, cred, _, observation, err := receiveControlObserved(r.fd, 0)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		if observation.RightsCount > 0 {
			return false, r.failDetected("reader_unexpected_rights", observation, err)
		}
		if observation.Bytes == 0 && observation.Stage == "validation" && pidfdSelects(r.pidfd, r.pid) {
			return false, r.failDetected("reader_endpoint_closed", observation, err)
		}
		return false, r.fail(err)
	}
	if !pidfdSelects(r.pidfd, r.pid) {
		return false, r.failDetected("reader_pidfd_exited", observation, errGuardProtocol)
	}
	if cred == nil || cred.Pid != int32(r.pid) || cred.Uid != 0 || cred.Gid != 0 {
		return false, r.failDetected("reader_message_credentials", observation, errGuardProtocol)
	}
	reply, detector, err := auditReaderReplyFrame(raw, r.boot, r.next, r.nonce, r.check)
	if err != nil {
		return false, r.failDetected(detector, observation, err)
	}
	status, err := validateAuditQueries(reply["queries"], r.check)
	if err != nil {
		if errors.Is(err, errAuditMissingDone) {
			return false, r.failDetected("reader_missing_done", observation, err)
		}
		return false, r.fail(err)
	}
	last, port := r.querySequence, r.queryPort
	for _, item := range reply["queries"].([]any) {
		q := item.(map[string]any)
		seq, _ := auditNumber32(q["request_sequence"])
		actualPort, _ := auditNumber32(q["request_portid"])
		if last == math.MaxUint32 || seq != last+1 || port != 0 && actualPort != port {
			return false, r.fail(errGuardProtocol)
		}
		last, port = seq, actualPort
	}
	body := map[string]any{"reader_cookie": jsonU64(r.cookie), "request_id": jsonU64(r.next), "nonce": r.nonce, "check": r.check, "queries": reply["queries"]}
	if !r.stableSet {
		if _, err = validateAuditConfiguration(body, false, 0); err != nil {
			return false, r.fail(err)
		}
	} else if status.mask != r.stable.mask || status.failure != r.stable.failure || status.rate != r.stable.rate || status.backlogLimit != r.stable.backlogLimit || status.lost != r.stable.lost || status.featureBitmap != r.stable.featureBitmap || status.backlogWaitTime != r.stable.backlogWaitTime {
		return false, r.fail(errGuardProtocol)
	}
	if status.enabled != 1 && status.enabled != 2 || r.locked && status.enabled != 2 || status.enabled == 1 && status.pid != 0 || r.owner != 0 && status.pid != r.owner {
		return false, r.fail(errGuardProtocol)
	}
	record := map[string]any{"version": json.Number("1"), "boot_id": r.boot, "epoch": strings.Repeat("f", 32), "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": "audit_readback", "body": body}
	if _, err = canonicalControl(record); err != nil {
		return false, r.fail(err)
	}
	if !r.stableSet {
		r.stable, r.stableSet = status, true
	}
	if status.enabled == 2 {
		r.locked = true
	}
	if status.pid != 0 {
		r.owner = status.pid
	}
	r.querySequence, r.queryPort = last, port
	r.body, r.status, r.ready = body, status, true
	return true, nil
}

func (r *auditReaderClient) result() (observedAuditStatus, map[string]any, error) {
	if r == nil || r.invalid || !r.ready || !pidfdSelects(r.pidfd, r.pid) {
		return observedAuditStatus{}, nil, errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil || now >= r.deadline {
		r.invalid = true
		return observedAuditStatus{}, nil, errGuardProtocol
	}
	status, body := r.status, r.body
	r.next++
	r.request, r.body = nil, nil
	r.ready, r.sent = false, false
	return status, body, nil
}

func (c *checkpointController) observeAuditStatus(owner uint32, status observedAuditStatus) (bool, error) {
	if c == nil || owner <= 1 {
		return false, errGuardProtocol
	}
	if !c.auditBaselineSet {
		return false, errGuardProtocol
	}
	if status.lost != c.auditLost {
		return false, errGuardProtocol
	}
	locked := status.enabled == 2 && status.pid == owner && status.failure == 1 && status.rate == 0 && status.backlogLimit == 16384
	if c.join.coverageStarted && (!locked || c.auditOwner != owner) {
		return false, errGuardProtocol
	}
	if status.enabled == 2 && !locked {
		return false, errGuardProtocol
	}
	if !locked && status.pid != 0 && status.pid != owner {
		return false, errGuardProtocol
	}
	return locked, nil
}
