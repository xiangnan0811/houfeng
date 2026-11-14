//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type pendingObservationFault struct {
	principal                                  *controlPrincipal
	requestID                                  uint64
	raw                                        []byte
	variant, nonce, caseSHA                    string
	caseFD, controlFD                          int
	deadline                                   uint64
	armSent, armed, ackDelivered, queryStarted bool
	credentialFD                               int
	credentialPages                            observerWitnessPages
	credentialSent                             bool
}

func (b *authorityBroker) startReaderObservationFault(p *controlPrincipal, request map[string]any, raw []byte, controlFD int) error {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingObservation != nil || b.selftestFaultVariant != "" || b.reader == nil || b.reader.request != nil || b.reader.ready || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken {
		return errControlFrame
	}
	if b.pendingStart != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || b.selftestRuntime != nil {
		return errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	body, bodyOK := request["body"].(map[string]any)
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest || !bodyOK || !exactKeys(body, "variant") {
		return errControlFrame
	}
	variant, ok := body["variant"].(string)
	if !ok || b.caseFacts["bpf_variant"] != variant {
		return errControlFrame
	}
	switch variant {
	case "reader-death", "reader-endpoint", "reader-timeout", "reader-replay", "reader-credential", "reader-extra-fd", "reader-frame", "reader-done":
	default:
		return errControlFrame
	}
	owner := b.principals[b.bootOwner]
	if owner == nil {
		return errGuardProtocol
	}
	var launch *launchRegistration
	for _, candidate := range b.launches {
		if candidate.childPIDFD == owner.PIDFD {
			if launch != nil {
				return errGuardProtocol
			}
			launch = candidate
		}
	}
	if launch == nil || len(launch.retainedFDs) != 4 {
		return errGuardProtocol
	}
	caseRaw, err := readHeldRegular(launch.retainedFDs[2], 4096)
	if err != nil {
		return err
	}
	nonce, err := earlyNonce()
	if err != nil {
		return err
	}
	now, err := monoNowNS()
	if err != nil {
		return err
	}
	deadline, err := clampGuestDeadlineNS(now + uint64((5 * time.Second).Nanoseconds()))
	if err != nil {
		return err
	}
	b.pendingObservation = &pendingObservationFault{principal: p, requestID: requestID, raw: append([]byte(nil), raw...), variant: variant, nonce: nonce, caseSHA: hashBytes(caseRaw), caseFD: launch.retainedFDs[2], controlFD: controlFD, deadline: deadline, credentialFD: -1}
	return nil
}

func (b *authorityBroker) advanceReaderObservationArm() ([]byte, bool, error) {
	pending := b.pendingObservation
	if pending == nil || pending.armed || pending.principal.Terminal || !livePIDFD(pending.principal.PIDFD) || !pidfdSelects(b.reader.pidfd, b.reader.pid) {
		return nil, false, errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil || now >= pending.deadline {
		return nil, false, unix.ETIMEDOUT
	}
	if !pending.armSent {
		frame, err := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": b.bootID, "kind": "reader_selftest_arm", "request_id": jsonU64(pending.requestID), "nonce": pending.nonce, "variant": pending.variant, "case_sha256": pending.caseSHA})
		if err != nil {
			return nil, false, err
		}
		if err = sendControl(b.reader.fd, frame, []int{pending.caseFD}); errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return nil, false, nil
		} else if err != nil {
			return nil, false, err
		}
		pending.armSent = true
	}
	expectedRights := 0
	if pending.variant == "reader-credential" {
		expectedRights = 1
	}
	raw, cred, rights, err := receiveControl(b.reader.fd, expectedRights)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	keepRights := false
	defer func() {
		if !keepRights {
			for _, right := range rights {
				_ = unix.Close(right)
			}
		}
	}()
	frame, err := parseCanonicalControl(raw)
	if err != nil || cred == nil || cred.Pid != int32(b.reader.pid) || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(b.reader.pidfd, b.reader.pid) || !exactKeys(frame, "version", "boot_id", "kind", "request_id", "nonce") || frame["version"] != jsonU64(1) || frame["boot_id"] != b.bootID || frame["kind"] != "reader_test_armed" || frame["request_id"] != jsonU64(pending.requestID) || frame["nonce"] != pending.nonce {
		return nil, false, errGuardProtocol
	}
	if len(rights) == 1 {
		typ, e := unix.GetsockoptInt(rights[0], unix.SOL_SOCKET, unix.SO_TYPE)
		peer, pe := unix.GetsockoptUcred(rights[0], unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil || pe != nil || typ != unix.SOCK_SEQPACKET || peer == nil || peer.Pid != 1 || peer.Uid != 0 || peer.Gid != 0 {
			return nil, false, errGuardProtocol
		}
		pending.credentialFD = rights[0]
		keepRights = true
	}
	ack, err := controlACK(b.reads, pending.principal, pending.requestID, pending.raw, "OK", map[string]any{"injected": true})
	if err != nil {
		return nil, false, err
	}
	if err = pending.principal.commitReply(pending.requestID, pending.raw, ack, nil); err != nil {
		return nil, false, err
	}
	pending.armed = true
	b.selftestFaultVariant = pending.variant
	return ack, true, nil
}

func (b *authorityBroker) advanceCredentialFixture(p *pendingObservationFault) error {
	if p.credentialSent {
		return nil
	}
	raw, cred, _, err := receiveControl(b.observerSocketFD, 0)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
		return nil
	}
	if err != nil {
		return err
	}
	observerPID, err := auditReaderPID(b.observerPIDFD)
	if err != nil || cred == nil || cred.Pid != int32(observerPID) || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(b.observerPIDFD, observerPID) {
		return errGuardProtocol
	}
	frame, err := parseCanonicalControl(raw)
	if err != nil || !exactKeys(frame, "version", "boot_id", "kind", "request_id", "nonce", "page_index", "page_count", "total_bytes", "total_sha256", "data_b64") || frame["version"] != jsonU64(1) || frame["boot_id"] != b.bootID || frame["kind"] != "observer_credential_chunk" || frame["request_id"] != jsonU64(b.reader.next) || frame["nonce"] != b.reader.nonce {
		return errGuardProtocol
	}
	index, a := exactNumber(frame["page_index"])
	count, c := exactNumber(frame["page_count"])
	total, t := exactNumber(frame["total_bytes"])
	sha, s := digestString(frame["total_sha256"])
	text, x := frame["data_b64"].(string)
	data, decErr := base64.StdEncoding.Strict().DecodeString(text)
	page := &p.credentialPages
	if !a || !c || !t || !s || !x || decErr != nil || base64.StdEncoding.EncodeToString(data) != text || total == 0 || total > maxControlFrame || count != (total+8191)/8192 || index != page.next || index >= count || len(data) == 0 || len(data) > 8192 || index+1 < count && len(data) != 8192 {
		return errGuardProtocol
	}
	if index == 0 {
		page.count, page.total, page.sha = count, total, sha
	}
	if page.count != count || page.total != total || page.sha != sha || uint64(len(page.data)+len(data)) > total {
		return errGuardProtocol
	}
	page.data = append(page.data, data...)
	page.next++
	if page.next != page.count {
		return nil
	}
	if uint64(len(page.data)) != total || hashBytes(page.data) != sha || p.credentialFD < 0 {
		return errGuardProtocol
	}
	reply, err := parseCanonicalControl(page.data)
	if err != nil || !exactKeys(reply, "version", "boot_id", "request_id", "nonce", "check", "queries") || reply["version"] != jsonU64(1) || reply["boot_id"] != b.bootID || reply["request_id"] != jsonU64(b.reader.next) || reply["nonce"] != b.reader.nonce || reply["check"] != b.reader.check {
		return errGuardProtocol
	}
	if _, err = validateAuditQueries(reply["queries"], b.reader.check); err != nil {
		return err
	}
	if err = sendControl(p.credentialFD, page.data, nil); err != nil {
		return err
	}
	_ = unix.Close(p.credentialFD)
	p.credentialFD = -1
	p.credentialSent = true
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	return earlyProbeSend(b.observerSocketFD, map[string]any{"version": jsonU64(1), "boot_id": b.bootID, "kind": "broker_credential_send", "variant": p.variant, "request_id": jsonU64(b.reader.next), "nonce": b.reader.nonce, "input_sha256": sha, "input_bytes": jsonU64(total), "send_result": jsonU64(0)}, nil, deadline)
}

func (b *authorityBroker) advanceReaderObservationAction() error {
	pending := b.pendingObservation
	if pending == nil || !pending.armed || !pending.ackDelivered {
		return nil
	}
	if pending.variant == "reader-death" {
		if !pending.queryStarted {
			if err := unix.PidfdSendSignal(b.reader.pidfd, unix.SIGKILL, nil, 0); err != nil {
				return err
			}
			pending.queryStarted = true
		}
		return nil
	}
	if !pending.queryStarted {
		check := "status"
		if pending.variant == "reader-done" {
			check = "configuration"
		}
		now, err := monoNowNS()
		if err != nil {
			return err
		}
		if err = b.reader.begin(check, pending.nonce, now+uint64((5*time.Second).Nanoseconds())); err != nil {
			return err
		}
		pending.queryStarted = true
	}
	if pending.variant == "reader-credential" {
		if err := b.advanceCredentialFixture(pending); err != nil {
			return err
		}
	}
	done, err := b.reader.service()
	if err != nil {
		return b.failReaderObservation(err)
	}
	if done {
		// The expected validator did not reject the delivered input.
		b.checkpoint.markFault()
		if err = b.authority.guard.revoke(); err != nil {
			return err
		}
		return errGuardProtocol
	}
	return nil
}

type observationFailure struct {
	variant string
	reader  readerDetection
}

func receiveObservationValue(o controlReceiveObservation) map[string]any {
	var digest, pid, uid, gid any
	if o.PacketSHA256 != nil {
		digest = *o.PacketSHA256
	}
	if o.PID != nil {
		pid = jsonI64(int64(*o.PID))
	}
	if o.UID != nil {
		uid = jsonU64(uint64(*o.UID))
	}
	if o.GID != nil {
		gid = jsonU64(uint64(*o.GID))
	}
	return map[string]any{"bytes": jsonU64(uint64(o.Bytes)), "packet_sha256": digest, "msg_trunc": o.Truncated, "msg_ctrunc": o.ControlTruncated, "pid": pid, "uid": uid, "gid": gid, "rights_count": jsonU64(uint64(o.RightsCount)), "closed_count": jsonU64(uint64(o.ClosedCount)), "validation_stage": o.Stage, "errno": jsonU64(uint64(o.Errno))}
}

// failObservation stops admission before lowering the native gate. Only a
// validator's recorded cause may be supplied; a requested test label is not a
// cause. The first cause remains authoritative for the lifetime of the broker.
func (b *authorityBroker) failObservation(cause observationFailure) error {
	if b == nil || b.checkpoint == nil || b.authority == nil || b.reader == nil || cause.reader.detector == "" {
		return errGuardProtocol
	}
	if b.observationFailure != nil {
		return errGuardProtocol
	}
	b.observationFailure = &cause
	b.checkpoint.markFault()
	if err := b.authority.guard.revoke(); err != nil {
		return err
	}
	var inputSHA, observedActor, deadlineNS any
	if cause.reader.receive.PacketSHA256 != nil {
		inputSHA = *cause.reader.receive.PacketSHA256
	}
	if cause.reader.receive.PID != nil {
		observedActor = jsonI64(int64(*cause.reader.receive.PID))
	}
	if cause.reader.deadline != 0 {
		deadlineNS = jsonU64(cause.reader.deadline)
	}
	frame := map[string]any{
		"version": jsonU64(1), "boot_id": b.bootID, "kind": "broker_receive_observation",
		"variant": cause.variant, "source": "broker_reader", "detector": cause.reader.detector,
		"request_id": jsonU64(cause.reader.requestID), "nonce": cause.reader.nonce,
		"input_sha256": inputSHA, "input_bytes": jsonU64(uint64(cause.reader.receive.Bytes)), "original_sha256": nil,
		"expected_actor": jsonU64(uint64(b.reader.pid)), "observed_actor": observedActor,
		"deadline_ns": deadlineNS, "observed_ns": jsonU64(cause.reader.observed),
		"capacity": nil, "count_before": nil, "rejected_wire": nil, "guard_sequence": nil,
		"receive_observation": receiveObservationValue(cause.reader.receive),
	}
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	if b.observerSocketFD < 0 {
		return errGuardProtocol
	}
	if err = earlyProbeSend(b.observerSocketFD, frame, nil, deadline); err != nil {
		return err
	}
	// After revocation the independent observer must see phase 2 while the
	// original broker is still alive; the guest then kills that same task.
	end, err := guestDeadline(30 * time.Second)
	if err != nil || b.observerPIDFD < 0 {
		return errGuardProtocol
	}
	if err = waitFD(b.observerPIDFD, unix.POLLIN, end); err != nil {
		return err
	}
	return errGuardProtocol
}

func (b *authorityBroker) failReaderObservation(err error) error {
	if b.reader.detection != nil && b.selftestFaultVariant != "" {
		return b.failObservation(observationFailure{variant: b.selftestFaultVariant, reader: *b.reader.detection})
	}
	b.checkpoint.markFault()
	if revokeErr := b.authority.guard.revoke(); revokeErr != nil {
		return revokeErr
	}
	return err
}

type selftestProbeActor struct {
	name               string
	probeCookie        uint64
	taskCookie         uint64
	tgid, tid          uint32
	startNS            uint64
	parentCookie       uint64
	exeSHA256          string
	pidfd              int
	listenerFD         int
	listenerID         uint64
	fixtureAFD         int
	fixtureADev        uint64
	fixtureAIno        uint64
	fixtureBFD         int
	fixtureBDev        uint64
	fixtureBIno        uint64
	importWatched      bool
	caseName, caseHash string
	caseBegin          int
	caseSettled        bool
	pipeFD             int
	resultFD           int
	resultBytes        []byte
	resultEOF          bool
}

func (p *selftestProbeActor) closeHeld() {
	if p == nil {
		return
	}
	for _, fd := range []*int{&p.pidfd, &p.listenerFD, &p.fixtureAFD, &p.fixtureBFD, &p.resultFD, &p.pipeFD} {
		if *fd >= 0 {
			_ = unix.Close(*fd)
			*fd = -1
		}
	}
}

type selftestRuntimeArm struct {
	nonce           string
	owner           uint64
	grantID         uint64
	mode            string
	phase           string
	deadline        uint64
	invocationID    uint64
	transactionID   uint64
	notifyingCookie uint64
	notificationID  uint64
	mutation        map[string]any
	signal          map[string]any
	caseObserved    bool
}

type pendingContainerSelftestProbe struct {
	principal        *controlPrincipal
	grant            *brokerGrant
	daemon           *daemonRegistration
	requestID, start uint64
	pid              uint32
	pidfd, eventFD   int
	raw              []byte
	result           chan error
}

// Docker inspection is performed by a bounded worker; no control-supplied PID
// is accepted as the container init identity before that independent readback.
func (b *authorityBroker) startContainerSelftestProbe(p *controlPrincipal, request map[string]any, raw []byte, rights []int) error {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingStart != nil || b.pendingObservation != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.pendingContainerProbe != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.selftestBaseline != nil && (b.selftestBaseline.name != "container-bpf" || b.selftestBaseline.owner != p.ID) || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken || len(rights) != 1 {
		return errControlFrame
	}
	variant, validVariant := b.caseFacts["bpf_variant"].(string)
	if (!validVariant && b.caseFacts["bpf_variant"] != nil) || (variant != "" && variant != "baseline") {
		return errControlFrame
	}
	id, ok := exactNumber(request["request_id"])
	body, valid := request["body"].(map[string]any)
	if !ok || id == 0 || id == math.MaxUint64 || id != p.NextRequest || !valid || !exactKeys(body, "name", "grant_id", "child_pid", "child_starttime") || body["name"] != "container-raw-denial" {
		return errControlFrame
	}
	grantID, grantOK := exactNumber(body["grant_id"])
	pid, pidOK := exactNumber(body["child_pid"])
	start, startOK := exactNumber(body["child_starttime"])
	grant := b.grants[grantID]
	if !grantOK || !pidOK || !startOK || pid <= 1 || pid > math.MaxInt32 || start == 0 || grant == nil || grant.owner != p.ID || grant.purpose != "container-bpf-selftest" || grant.phase != "active" || !grant.startObserved || !pidfdSelects(rights[0], uint32(pid)) {
		return errControlFrame
	}
	if b.selftestBaseline != nil && b.selftestBaseline.grantID != grantID {
		return errControlFrame
	}
	for _, actor := range b.probeActors {
		if actor != nil && actor.name == "container-raw-denial" {
			return errControlFrame
		}
	}
	daemon := b.daemons[b.bootOwner]
	if daemon == nil || b.observeDaemonExec(daemon) != nil {
		return errGuardProtocol
	}
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return err
	}
	eventFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return err
	}
	workerFD, err := unix.FcntlInt(uintptr(eventFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(eventFD)
		return err
	}
	pending := &pendingContainerSelftestProbe{principal: p, grant: grant, daemon: daemon, requestID: id, pid: uint32(pid), start: start, pidfd: rights[0], eventFD: eventFD, raw: append([]byte(nil), raw...), result: make(chan error, 1)}
	daemonCopy, cid := *daemon, grant.containerID
	b.pendingContainerProbe = pending
	go func() {
		item, inspectErr := recoveryDockerGET(&daemonCopy, "/v1.41/containers/"+cid+"/json", deadline)
		if inspectErr == nil {
			state, ok := item["State"].(map[string]any)
			observedPID, pidOK := exactNumber(state["Pid"])
			if item["Id"] != cid || item["Image"] != postgresImageID || !ok || state["Running"] != true || !pidOK || observedPID != pid {
				inspectErr = errGuardProtocol
			}
		}
		pending.result <- inspectErr
		var wake [8]byte
		binary.LittleEndian.PutUint64(wake[:], 1)
		_, _ = unix.Write(workerFD, wake[:])
		_ = unix.Close(workerFD)
	}()
	return nil
}

func (b *authorityBroker) finishContainerSelftestProbe() ([]byte, bool, error) {
	q := b.pendingContainerProbe
	if q == nil {
		return nil, false, errGuardProtocol
	}
	select {
	case workerErr := <-q.result:
		defer func() { _ = unix.Close(q.eventFD); b.pendingContainerProbe = nil }()
		keep := false
		defer func() {
			if !keep {
				_ = unix.Close(q.pidfd)
			}
		}()
		if workerErr != nil {
			return nil, false, workerErr
		}
		if q.principal.Terminal || q.principal.Transferred || !livePIDFD(q.principal.PIDFD) || !pidfdSelects(q.pidfd, q.pid) || b.grants[q.grant.id] != q.grant || q.grant.owner != q.principal.ID || q.grant.phase != "active" || !q.grant.startObserved || b.daemons[b.bootOwner] != q.daemon || b.observeDaemonExec(q.daemon) != nil {
			return nil, false, errGuardProtocol
		}
		process, err := observeProcess(q.pid, q.pidfd)
		if err != nil || process.StartTicks != q.start || process.UID != 10002 || process.GID != 10002 || process.Cgroup != "/docker/"+q.grant.containerID || process.ExecutableSHA256 != b.manifest.BrokerSHA256 || !exactArgumentVector(process.Argv, []string{"/opt/g2/g2-container-bpf-probe", "selftest", "container-raw-denial"}) {
			return nil, false, errGuardProtocol
		}
		if err = verifyContainerProbeExecutable(q.pid, b.manifest.BrokerSHA256); err != nil {
			return nil, false, err
		}
		task, err := b.authority.guard.pinTask(q.pid)
		if err != nil {
			return nil, false, err
		}
		keepTask := false
		defer func() {
			if !keepTask {
				_ = b.authority.guard.dropTask(task.Cookie)
			}
		}()
		if task.TID != q.pid || task.TGID != q.pid || task.StartBoottimeNS/10_000_000 != q.start || !livePIDFD(q.pidfd) {
			return nil, false, errGuardProtocol
		}
		if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		lineage := b.checkpoint.lifecycle.lineage
		if lineage == nil || lineage.nodes[task.Cookie] == nil {
			return nil, false, errGuardProtocol
		}
		node := lineage.nodes[task.Cookie]
		if node.tgid != task.TGID || node.tid != task.TID || node.startNS != task.StartBoottimeNS || node.exited {
			return nil, false, errGuardProtocol
		}
		if _, err = b.authority.guard.watchProbe(task.Cookie, 1); err != nil {
			return nil, false, err
		}
		if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		if b.nextProbeCookie == 0 {
			b.nextProbeCookie = 1
		}
		if b.nextProbeCookie == math.MaxUint64 {
			return nil, false, errGuardProtocol
		}
		cookie := b.nextProbeCookie
		b.nextProbeCookie++
		body := map[string]any{"name": "container-raw-denial", "probe_cookie": jsonU64(cookie), "task_cookie": jsonU64(task.Cookie), "tgid": jsonU64(uint64(task.TGID)), "tid": jsonU64(uint64(task.TID)), "start_ns": jsonU64(task.StartBoottimeNS), "parent_cookie": jsonU64(node.parent), "exe_sha256": process.ExecutableSHA256, "listener_id": nil}
		if err = appendRuntimeRecord(b, "probe_actor", body); err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"probe_cookie": jsonU64(cookie), "listener_id": nil})
		if err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		if err = q.principal.commitReply(q.requestID, q.raw, ack, []int{q.pidfd}); err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		if b.probeActors == nil {
			b.probeActors = make(map[uint64]*selftestProbeActor)
		}
		b.probeActors[cookie] = &selftestProbeActor{name: "container-raw-denial", probeCookie: cookie, taskCookie: task.Cookie, tgid: task.TGID, tid: task.TID, startNS: task.StartBoottimeNS, parentCookie: node.parent, exeSHA256: process.ExecutableSHA256, pidfd: q.pidfd, listenerFD: -1, fixtureAFD: -1, fixtureBFD: -1, resultFD: -1, pipeFD: -1}
		if s := b.selftestBaseline; s != nil && s.name == "container-bpf" && s.grantID == q.grant.id && s.owner == q.principal.ID {
			s.actorCookie = task.Cookie
		}
		keep, keepTask = true, true
		return ack, true, nil
	default:
		return nil, false, nil
	}
}

// The container probe is deliberately root:10002 0550, unlike ordinary
// root:root executables. Resolve its fixed path without weakening that rule.
func verifyContainerProbeExecutable(pid uint32, expected string) error {
	root, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	var rootStat unix.Stat_t
	if unix.Fstat(root, &rootStat) != nil || rootStat.Uid != 0 || rootStat.Gid != 0 || rootStat.Mode&0022 != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errGuardProtocol
	}
	opt, err := unix.Openat2(root, "opt", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(opt)
	var optStat unix.Stat_t
	if unix.Fstat(opt, &optStat) != nil || optStat.Uid != 0 || optStat.Gid != 0 || optStat.Mode&0022 != 0 || optStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errGuardProtocol
	}
	dir, err := unix.Openat2(opt, "g2", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	var dirStat unix.Stat_t
	if unix.Fstat(dir, &dirStat) != nil || dirStat.Uid != 0 || dirStat.Gid != 0 || dirStat.Mode&0022 != 0 || dirStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errGuardProtocol
	}
	fd, err := unix.Openat2(dir, "g2-container-bpf-probe", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "g2-container-bpf-probe")
	if file == nil {
		_ = unix.Close(fd)
		return errGuardProtocol
	}
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Gid != 10002 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0550 || st.Size <= 0 || st.Size > 128<<20 {
		return errGuardProtocol
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, (128<<20)+1))
	if err != nil || n != st.Size || hex.EncodeToString(h.Sum(nil)) != expected {
		return errGuardProtocol
	}
	actualFD, err := unix.Openat(heldProcDirectory, fmt.Sprintf("%d/exe", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(actualFD)
	var actual, after, named, rootAfter, optAfter, optNamed, dirAfter, dirNamed unix.Stat_t
	if unix.Fstat(actualFD, &actual) != nil || unix.Fstat(fd, &after) != nil || unix.Fstatat(dir, "g2-container-bpf-probe", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(st, actual) || !sameTrustedStat(st, after) || !sameTrustedStat(st, named) || unix.Fstat(root, &rootAfter) != nil || !sameTrustedStat(rootStat, rootAfter) || unix.Fstat(opt, &optAfter) != nil || unix.Fstatat(root, "opt", &optNamed, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(optStat, optAfter) || !sameTrustedStat(optStat, optNamed) || unix.Fstat(dir, &dirAfter) != nil || unix.Fstatat(opt, "g2", &dirNamed, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(dirStat, dirAfter) || !sameTrustedStat(dirStat, dirNamed) {
		return errGuardProtocol
	}
	return nil
}

// selftestBaselineStep binds one baseline action to its owner and to evidence
// observed after that action was accepted. A runtime bind witness is populated
// only by the authenticated retired-grant lookup.
type selftestBaselineStep struct {
	name, nonce, requestHash                                               string
	owner, actorCookie                                                     uint64
	grantID                                                                uint64
	sealStatus                                                             guardStatus
	begin                                                                  int
	invocationID, transactionID, referenceCookie                           uint64
	secondTransactionID                                                    uint64
	extraAttachmentID                                                      uint64
	replacementAttachmentID, replacementTransactionID, detachTransactionID uint64
	stopCheck                                                              chan bool
	stopDaemonCookie                                                       uint64
	notificationID                                                         uint64
	publicationSeq, removalSeq                                             uint64
	result                                                                 any
	auditActors                                                            [2]*selftestProbeActor
	referenceCookies                                                       []any
	transactionIDs, guardSequences                                         []any
	observed                                                               bool
	witnessed, ackDelivered                                                bool
}

func (s *selftestBaselineStep) record(phase string, actor, requestHash, errorCode any) map[string]any {
	var invocation any
	transactions, guards, references := []any{}, []any{}, []any{}
	if s.invocationID != 0 {
		invocation = jsonU64(s.invocationID)
	}
	if s.transactionID != 0 {
		transactions = append(transactions, jsonU64(s.transactionID))
	}
	transactions = append(transactions, s.transactionIDs...)
	if s.publicationSeq != 0 {
		guards = append(guards, jsonU64(s.publicationSeq))
	}
	if s.removalSeq != 0 {
		guards = append(guards, jsonU64(s.removalSeq))
	}
	guards = append(guards, s.guardSequences...)
	if s.referenceCookie != 0 {
		references = append(references, jsonU64(s.referenceCookie))
	}
	references = append(references, s.referenceCookies...)
	return map[string]any{
		"name": s.name, "nonce": s.nonce, "phase": phase, "actor_cookie": actor,
		"invocation_id": invocation, "transaction_ids": transactions, "guard_sequences": guards,
		"reference_cookies": references, "request_sha256": requestHash, "result": s.result, "error_code": errorCode,
	}
}

func selftestSealStatusRecord(s *selftestBaselineStep, phase string, status guardStatus) map[string]any {
	return map[string]any{"name": s.name, "nonce": s.nonce, "phase": phase, "status": projectGuardStatus(status)}
}

func (b *authorityBroker) startSelftestBaseline(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.selftestBaseline != nil || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, errControlFrame
	}
	variant, ok := b.caseFacts["bpf_variant"].(string)
	if (!ok && b.caseFacts["bpf_variant"] != nil) || (variant != "" && variant != "baseline") {
		return nil, errControlFrame
	}
	id, ok := exactNumber(request["request_id"])
	if !ok || id == 0 || id == math.MaxUint64 || id != p.NextRequest {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "name", "nonce", "grant_id") {
		return nil, errControlFrame
	}
	name, ok := body["name"].(string)
	if !ok || b.selftestBaselineUsed[name] {
		return nil, errControlFrame
	}
	switch name {
	case "wrong-generation", "fd-arm-replay", "fd-arm-wrong-role", "fd-arm-wrong-grant", "fd-arm-second", "feature-map", "extended-info-ebadf", "replace-probe-ebadf", "allowed-device-start", "cgroup-auto-retire", "wrong-program-byte", "wrong-flags", "wrong-target", "failure-cleanup", "audit-interleaved", "id-reuse", "extra-fd-delays-removal", "attachment-replace", "container-bpf", "seal-detach-denied", "seal-unload-denied":
	default:
		return nil, errControlFrame
	}
	nonce, ok := body["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return nil, errControlFrame
	}
	var selectedGrant uint64
	if body["grant_id"] != nil {
		grantID, valid := exactNumber(body["grant_id"])
		grant := b.grants[grantID]
		if !valid || grantID == 0 || grant == nil || grant.owner != p.ID || (name == "wrong-generation" && grant.phase != "retired") || (name != "wrong-generation" && grant.phase != "admitted") {
			return nil, errControlFrame
		}
		selectedGrant = grantID
		template, err := b.templateForPrincipal(p, grant.purpose)
		if err != nil || !sameTemplate(grant.config, template) {
			return nil, errControlFrame
		}
	}
	if name == "feature-map" || name == "extended-info-ebadf" || name == "replace-probe-ebadf" || name == "allowed-device-start" || name == "cgroup-auto-retire" || name == "wrong-program-byte" || name == "wrong-flags" || name == "wrong-target" || name == "failure-cleanup" || name == "extra-fd-delays-removal" || name == "attachment-replace" {
		grant := b.grants[selectedGrant]
		if grant == nil || grant.createBound {
			return nil, errControlFrame
		}
		switch grant.purpose {
		case "postgres-init-app", "postgres-init-w1", "postgres-init-w2", "postgres-init-control":
		default:
			return nil, errControlFrame
		}
	}
	if name == "container-bpf" {
		grant := b.grants[selectedGrant]
		if grant == nil || grant.createBound || grant.purpose != "container-bpf-selftest" {
			return nil, errControlFrame
		}
	}
	if name == "audit-interleaved" && (selectedGrant != 0 || b.join == nil || b.join.reorder != nil) {
		return nil, errControlFrame
	}
	if name == "id-reuse" && (selectedGrant != 0 || b.effects == nil || b.resolver == nil || b.authority == nil) {
		return nil, errControlFrame
	}
	if name == "fd-arm-replay" && selectedGrant == 0 {
		return nil, errControlFrame
	}
	step := &selftestBaselineStep{name: name, nonce: nonce, owner: p.ID, grantID: selectedGrant}
	if name == "extra-fd-delays-removal" {
		for _, candidate := range b.probeActors {
			if candidate != nil && candidate.name == name && candidate.parentCookie == p.TaskCookie && candidate.listenerFD >= 0 && livePIDFD(candidate.pidfd) {
				if step.actorCookie != 0 {
					return nil, errControlFrame
				}
				step.actorCookie = candidate.taskCookie
			}
		}
		if step.actorCookie == 0 {
			return nil, errControlFrame
		}
	}
	var detachActor *selftestProbeActor
	if name == "seal-detach-denied" {
		for _, candidate := range b.probeActors {
			if candidate != nil && candidate.name == name && candidate.parentCookie == p.TaskCookie && candidate.pipeFD >= 0 && livePIDFD(candidate.pidfd) {
				if detachActor != nil {
					return nil, errControlFrame
				}
				detachActor = candidate
			}
		}
		if detachActor == nil {
			return nil, errControlFrame
		}
		step.actorCookie = detachActor.taskCookie
	}
	if name == "seal-unload-denied" || name == "seal-detach-denied" {
		if selectedGrant != 0 || b.authority == nil {
			return nil, errControlFrame
		}
		status, e := b.authority.guard.status()
		if e != nil || status.State != 1 || status.AuthorityPhase != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.ModuleInstance == 0 || status.LinkIDs[0] == 0 || status.LinkIDs[1] == 0 || status.LinkIDs[2] == 0 || status.LinkAttached != [3]uint32{1, 1, 1} {
			return nil, errGuardProtocol
		}
		step.sealStatus = status
	}
	if name == "seal-unload-denied" || name == "seal-detach-denied" {
		if err := preflightRuntimeRecord(b, "selftest_guard_status", selftestSealStatusRecord(step, "before", step.sealStatus)); err != nil {
			return nil, err
		}
		if err := preflightRuntimeRecord(b, "selftest_guard_status", selftestSealStatusRecord(step, "after", step.sealStatus)); err != nil {
			return nil, err
		}
	}
	if err := preflightRuntimeRecord(b, "selftest_case", step.record("started", nil, nil, nil)); err != nil {
		return nil, err
	}
	ack, err := controlACK(b.reads, p, id, raw, "OK", map[string]any{"started": true})
	if err != nil {
		return nil, err
	}
	if err = appendRuntimeRecord(b, "selftest_case", step.record("started", nil, nil, nil)); err != nil {
		return nil, err
	}
	if name == "seal-unload-denied" || name == "seal-detach-denied" {
		if err = appendRuntimeRecord(b, "selftest_guard_status", selftestSealStatusRecord(step, "before", step.sealStatus)); err != nil {
			return nil, err
		}
	}
	step.begin = len(b.transcript.entries)
	if err = p.commitReply(id, raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	if b.selftestBaselineUsed == nil {
		b.selftestBaselineUsed = make(map[string]bool)
	}
	b.selftestBaselineUsed[name] = true
	b.selftestBaseline = step
	if detachActor != nil {
		linkFD, e := unix.FcntlInt(uintptr(b.authority.links[0]), unix.F_DUPFD_CLOEXEC, 0)
		if e != nil {
			b.checkpoint.markFault()
			return nil, e
		}
		e = sendControl(detachActor.pipeFD, []byte(`{"fd":"link"}`), []int{linkFD})
		closeErr := unix.Close(linkFD)
		if e != nil || closeErr != nil {
			b.checkpoint.markFault()
			return nil, errGuardProtocol
		}
	}
	return ack, nil
}

// Control cases use the ordinary control socket; their evidence is advanced
// from the records appended for this case after the actual ACK is sent.
func (b *authorityBroker) selftestBaselinePollFD() int {
	if b == nil || b.selftestBaseline == nil || b.selftestBaseline.name != "seal-unload-denied" {
		return -1
	}
	for _, actor := range b.probeActors {
		if actor != nil && actor.name == "seal-unload-denied" && actor.taskCookie == b.selftestBaseline.actorCookie && !actor.resultEOF {
			return actor.resultFD
		}
	}
	return -1
}

func (b *authorityBroker) advanceSelftestBaseline() error {
	if b == nil {
		return nil
	}
	for _, actor := range b.probeActors {
		if actor == nil || actor.caseName != "wrong-actor" || actor.caseSettled {
			continue
		}
		step := &selftestBaselineStep{name: actor.caseName, nonce: actor.caseHash[:32], requestHash: actor.caseHash, actorCookie: actor.taskCookie, begin: actor.caseBegin}
		if err := b.advanceSelftestContainerBPF(step); err != nil {
			return err
		}
		if step.witnessed {
			actor.caseSettled = true
		}
	}
	if b.selftestBaseline == nil {
		return nil
	}
	s := b.selftestBaseline
	if s.name == "feature-map" {
		return b.advanceSelftestFeatureMap(s)
	}
	if s.name == "extended-info-ebadf" || s.name == "replace-probe-ebadf" {
		return b.advanceSelftestCompatProbe(s)
	}
	if s.name == "allowed-device-start" || s.name == "cgroup-auto-retire" {
		return b.advanceSelftestDeviceLifecycle(s)
	}
	if s.name == "wrong-program-byte" || s.name == "wrong-flags" || s.name == "wrong-target" {
		return b.advanceSelftestMutation(s)
	}
	if s.name == "failure-cleanup" {
		return b.advanceSelftestFailureCleanup(s)
	}
	if s.name == "container-bpf" {
		return b.advanceSelftestContainerBPF(s)
	}
	if s.name == "seal-unload-denied" {
		return b.advanceSelftestSealUnload(s)
	}
	if s.name == "seal-detach-denied" {
		return b.advanceSelftestSealDetach(s)
	}
	if s.name == "audit-interleaved" {
		return b.advanceSelftestAuditInterleaved(s)
	}
	if s.name == "id-reuse" {
		return b.advanceSelftestIDReuse(s)
	}
	if s.name == "extra-fd-delays-removal" {
		return b.advanceSelftestExtraFD(s)
	}
	if s.name == "attachment-replace" {
		return b.advanceSelftestReplacement(s)
	}
	if s.name != "wrong-generation" && !s.ackDelivered {
		if b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) {
			return errGuardProtocol
		}
		var records []map[string]any
		for _, entry := range b.transcript.entries[s.begin:] {
			if entry.record["kind"] == "control_rejection" {
				body, ok := entry.record["body"].(map[string]any)
				if !ok {
					return errGuardProtocol
				}
				if body["operation"] == "SELFTEST_FD_ARM" {
					records = append(records, body)
				}
			}
		}
		if s.name == "fd-arm-replay" {
			if len(records) < 3 {
				return nil
			}
			if len(records) != 3 || records[0]["status"] != "OK" || records[1]["status"] != "OK" || records[2]["status"] != "BPF_AUTH_DENIED" || records[0]["principal_id"] != jsonU64(s.owner) || records[1]["principal_id"] != records[0]["principal_id"] || records[2]["principal_id"] != records[0]["principal_id"] || records[1]["request_id"] != records[0]["request_id"] || records[2]["request_id"] != records[0]["request_id"] || records[1]["request_sha256"] != records[0]["request_sha256"] || records[2]["request_sha256"] == records[0]["request_sha256"] {
				return errGuardProtocol
			}
			original, ok := records[0]["body"].(map[string]any)
			grant, valid := exactNumber(original["grant_id"])
			if !ok || !valid || grant != s.grantID {
				return errGuardProtocol
			}
			stage := b.selftestFD
			if stage == nil || !stage.replayOnly || stage.owner != s.owner || stage.grantID != s.grantID || stage.phase != "armed" || stage.transactionID != 0 || original["nonce"] != stage.nonce || original["class"] != stage.class {
				return errGuardProtocol
			}
			principal := b.principals[s.owner]
			if principal == nil || principal.TaskCookie == 0 {
				return errGuardProtocol
			}
			s.actorCookie = principal.TaskCookie
			s.requestHash, _ = records[2]["request_sha256"].(string)
			s.witnessed, s.ackDelivered = true, true
		} else {
			for _, denial := range records {
				if denial["status"] != "BPF_AUTH_DENIED" || denial["code"] != "BPF_AUTH_DENIED" {
					continue
				}
				id, ok := exactNumber(denial["principal_id"])
				principal := b.principals[id]
				body, validBody := denial["body"].(map[string]any)
				if !ok || principal == nil || principal.TaskCookie == 0 || !validBody {
					return errGuardProtocol
				}
				grantID, validGrant := exactNumber(body["grant_id"])
				if !validGrant {
					return errGuardProtocol
				}
				matched := false
				switch s.name {
				case "fd-arm-wrong-role":
					matched = principal.Role != "REALM_ROOT"
				case "fd-arm-wrong-grant":
					grant := b.grants[grantID]
					nonce, nonceOK := body["nonce"].(string)
					matched = id == s.owner && principal.Role == "REALM_ROOT" && body["class"] == "map" && nonceOK && validCheckpointNonce(nonce) && nonce != b.selftestMapNonce && !b.selftestMapArmed && !b.selftestProgramArmed && b.selftestFD == nil && grant != nil && (grant.owner != id || grant.phase == "retired" || grant.phase == "revoked")
				case "fd-arm-second":
					class, _ := body["class"].(string)
					matched = id == s.owner && (class == "map" && b.selftestMapArmed || class == "program" && b.selftestProgramArmed)
				default:
					return errGuardProtocol
				}
				if matched {
					s.actorCookie = principal.TaskCookie
					s.requestHash, _ = denial["request_sha256"].(string)
					s.witnessed, s.ackDelivered = true, true
					break
				}
			}
		}
	}
	if !s.ackDelivered {
		return nil
	}
	if !s.witnessed || s.actorCookie == 0 {
		return errGuardProtocol
	}
	if _, ok := digestString(s.requestHash); !ok {
		return errGuardProtocol
	}
	actor := jsonU64(s.actorCookie)
	if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", actor, s.requestHash, "BPF_AUTH_DENIED")); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", actor, s.requestHash, "BPF_AUTH_DENIED")); err != nil {
		return err
	}
	if s.name == "fd-arm-replay" {
		b.selftestFD = nil
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) bindSelftestCreateInvocation(s *selftestBaselineStep) error {
	if b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) || s.grantID == 0 {
		return errGuardProtocol
	}
	if s.invocationID == 0 {
		for _, entry := range b.transcript.entries[s.begin:] {
			if entry.record["kind"] != "invocation" {
				continue
			}
			body, ok := entry.record["body"].(map[string]any)
			if !ok {
				return errGuardProtocol
			}
			if body["grant_id"] != jsonU64(s.grantID) || body["command"] != "create" || body["state"] != "bound" {
				continue
			}
			id, ok := exactNumber(body["id"])
			if !ok || id == 0 {
				return errGuardProtocol
			}
			s.invocationID = id
			break
		}
	}
	return nil
}

// advanceSelftestFeatureMap waits for the selected native map incarnation's removal.
func (b *authorityBroker) advanceSelftestFeatureMap(s *selftestBaselineStep) error {
	if err := b.bindSelftestCreateInvocation(s); err != nil {
		return err
	}
	if s.invocationID == 0 || b.effects == nil || b.effects.objects == nil {
		return nil
	}
	var selected *runtimeEffectTransaction
	for _, candidate := range b.effects.transactions {
		if candidate == nil || candidate.grantID != s.grantID || candidate.invocationID != s.invocationID || candidate.command != "MAP_CREATE" {
			continue
		}
		if selected != nil {
			return errGuardProtocol
		}
		selected = candidate
	}
	if selected == nil || selected.state != "retired" {
		return nil
	}
	if !selected.effectExecuted || selected.effectErrno != 0 || selected.effectFD < 0 || selected.requesterCookie == 0 || selected.publication == nil || !featureMapAttribute(selected.attributes) {
		return errGuardProtocol
	}
	object := b.effects.objects.byCookie[selected.publication.ObjectCookie]
	if object == nil || object.class != "map" || object.ownerTransaction != selected.id || object.publicationSeq != selected.publication.Sequence || object.publicationSeq == 0 {
		return errGuardProtocol
	}
	if !s.observed {
		s.actorCookie, s.transactionID, s.referenceCookie = selected.requesterCookie, selected.id, object.cookie
		s.publicationSeq, s.result = object.publicationSeq, selected.result
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if object.removalSeq == 0 {
		return nil
	}
	if object.state != "id_removed" || object.removalSeq <= object.publicationSeq {
		return errGuardProtocol
	}
	s.removalSeq = object.removalSeq
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

// advanceSelftestCompatProbe indexes only the completed transaction from the
// selected invocation. Retirement already requires its real audit join.
func (b *authorityBroker) advanceSelftestCompatProbe(s *selftestBaselineStep) error {
	if err := b.bindSelftestCreateInvocation(s); err != nil {
		return err
	}
	if s.invocationID == 0 || b.effects == nil || b.effects.objects == nil {
		return nil
	}
	var selected *runtimeEffectTransaction
	var program *objectIncarnation
	for _, tx := range b.effects.transactions {
		if tx == nil || tx.grantID != s.grantID || tx.invocationID != s.invocationID {
			continue
		}
		var candidate *objectIncarnation
		if s.name == "extended-info-ebadf" {
			if tx.command != "PROG_LOAD" || len(tx.attributes) < 4 || guardU32(tx.attributes, 0) != unix.BPF_PROG_TYPE_SOCKET_FILTER {
				continue
			}
		} else {
			if tx.command != "PROG_ATTACH" {
				continue
			}
			var target bool
			for _, value := range tx.inputs {
				item, ok := value.(map[string]any)
				if !ok {
					return errGuardProtocol
				}
				if item["field"] == "target_fd" && item["kind"] == "dev_null" {
					target = true
				}
				if item["field"] == "attach_bpf_fd" && item["kind"] == "program" {
					cookie, ok := exactNumber(item["cookie"])
					if !ok {
						return errGuardProtocol
					}
					candidate = b.effects.objects.byCookie[cookie]
				}
			}
			if !target {
				continue
			}
			if candidate == nil {
				return errGuardProtocol
			}
			probe, err := probeProgram()
			if err != nil || candidate.programBytes != hex.EncodeToString(probe) {
				return errGuardProtocol
			}
		}
		if selected != nil {
			return errGuardProtocol
		}
		selected, program = tx, candidate
	}
	if selected == nil || selected.state != "retired" {
		return nil
	}
	if selected.requesterCookie == 0 || selected.result != jsonI64(-int64(unix.EBADF)) || selected.returnedFD != nil {
		return errGuardProtocol
	}
	if s.name == "extended-info-ebadf" && (!selected.effectExecuted || selected.effectErrno != unix.EBADF || selected.effectFD >= 0 || selected.publication != nil) {
		return errGuardProtocol
	}
	s.actorCookie, s.transactionID, s.result = selected.requesterCookie, selected.id, selected.result
	if program != nil {
		s.referenceCookie, s.publicationSeq = program.cookie, program.publicationSeq
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

// advanceSelftestDeviceLifecycle retains the actual load, attach and query
// identities until the grant and its cgroup/program have really retired.
func (b *authorityBroker) advanceSelftestDeviceLifecycle(s *selftestBaselineStep) error {
	if err := b.bindSelftestCreateInvocation(s); err != nil {
		return err
	}
	if s.invocationID == 0 || b.effects == nil || b.effects.objects == nil {
		return nil
	}
	grant := b.grants[s.grantID]
	if grant == nil || grant.owner != s.owner {
		return errGuardProtocol
	}
	if !grant.startObserved || !grant.actualQueryConfirmed {
		return nil
	}
	var attachment *runtimeAttachment
	for _, a := range b.effects.attachments {
		if a == nil || a.grantID != s.grantID {
			continue
		}
		tx := b.effects.transactions[a.transactionID]
		if tx == nil || tx.invocationID != s.invocationID {
			continue
		}
		if attachment != nil {
			return errGuardProtocol
		}
		attachment = a
	}
	if attachment == nil {
		return nil
	}
	program := b.effects.objects.byCookie[attachment.programCookie]
	attach := b.effects.transactions[attachment.transactionID]
	if program == nil || attach == nil {
		return errGuardProtocol
	}
	load := b.effects.transactions[program.ownerTransaction]
	device, err := deviceProgram()
	if err != nil || load == nil || load.invocationID != s.invocationID || load.command != "PROG_LOAD" || program.programBytes != hex.EncodeToString(device) || program.publicationSeq == 0 || attachment.flags != unix.BPF_F_ALLOW_MULTI {
		return errGuardProtocol
	}
	if load.state != "retired" || attach.state != "retired" {
		return nil
	}
	if !load.effectExecuted || load.effectErrno != 0 || load.effectFD < 0 || attach.result != jsonI64(0) || load.requesterCookie == 0 {
		return errGuardProtocol
	}
	if b.transcript == nil || b.resolver == nil || attachment.enrollment == 0 || s.begin < 0 || s.begin > len(b.transcript.entries) {
		return errGuardProtocol
	}
	var queryID uint64
	attached := false
	for _, entry := range b.transcript.entries[s.begin:] {
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if entry.record["kind"] == "attachment" && body["id"] == jsonU64(attachment.id) && body["grant_id"] == jsonU64(s.grantID) && body["state"] == "attached" && body["enrollment"] == jsonU64(attachment.enrollment) && body["program_cookie"] == jsonU64(program.cookie) && body["transaction_id"] == jsonU64(attach.id) && body["flags"] == jsonU64(unix.BPF_F_ALLOW_MULTI) {
			attached = true
		}
		if !attached || entry.record["kind"] != "transaction" || body["command"] != "PROG_QUERY" || body["state"] != "retired" || body["result"] != jsonI64(0) || body["grant_id"] != nil || body["invocation_id"] != nil || body["requester_cookie"] != nil || body["notification_id"] != nil || body["executor_cookie"] != jsonU64(b.resolver.brokerCookie) {
			continue
		}
		inputs, ok := body["input_objects"].([]any)
		if !ok || len(inputs) != 1 {
			continue
		}
		input, ok := inputs[0].(map[string]any)
		if !ok || !exactKeys(input, "field", "kind", "cookie", "requester_fd") || input["field"] != "query.target_fd" || input["kind"] != "cgroup" || input["cookie"] != jsonU64(attachment.enrollment) {
			continue
		}
		out, ok := body["output"].(map[string]any)
		if !ok {
			continue
		}
		ids, idsOK := out["program_ids"].([]any)
		flags, flagsOK := out["program_attach_flags"].([]any)
		if !idsOK || !flagsOK || len(ids) != 1 || len(flags) != 1 || ids[0] != jsonU64(program.id) || flags[0] != jsonU64(unix.BPF_F_ALLOW_MULTI) || out["attach_flags"] != jsonU64(unix.BPF_F_ALLOW_MULTI) {
			continue
		}
		id, valid := exactNumber(body["id"])
		if !valid || id <= attach.id || attach.id <= load.id || queryID != 0 {
			return errGuardProtocol
		}
		queryID = id
	}
	if queryID == 0 {
		return nil
	}
	if !s.observed {
		s.actorCookie, s.referenceCookie, s.publicationSeq, s.result = load.requesterCookie, program.cookie, program.publicationSeq, attach.result
		s.transactionIDs = []any{jsonU64(load.id), jsonU64(attach.id), jsonU64(queryID)}
		if err = appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if attachment.offlineSeq == 0 || attachment.releaseSeq == 0 || program.removalSeq == 0 || grant.phase != "retired" {
		return nil
	}
	if attachment.state != "released" || attachment.releaseSeq <= attachment.offlineSeq || program.state != "id_removed" || program.removalSeq <= program.publicationSeq {
		return errGuardProtocol
	}
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "attachment" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if s.name == "cgroup-auto-retire" && body["id"] == jsonU64(attachment.id) && body["state"] == "detached" {
			return errGuardProtocol
		}
	}
	s.removalSeq = program.removalSeq
	s.guardSequences = []any{jsonU64(attachment.offlineSeq), jsonU64(attachment.releaseSeq)}
	if err = appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

// mutateSelftestRequest changes only the pinned requester's fixed test input
// before the ordinary capture/validation path sees it.
func (b *authorityBroker) mutateSelftestRequest(inv *runtimeInvocation, h *heldNotification) (bool, error) {
	s := b.selftestBaseline
	if s == nil || (s.name != "wrong-program-byte" && s.name != "wrong-flags" && s.name != "wrong-target") || s.notificationID != 0 || inv.grantID != s.grantID || inv.command != "create" {
		return false, nil
	}
	if err := b.bindSelftestCreateInvocation(s); err != nil {
		return false, err
	}
	if s.invocationID != inv.id {
		return false, errGuardProtocol
	}
	var before, after, attributes, rawAttributes []byte
	var programBytes, license any
	var targetIdentity map[string]any
	var address, offset uint64
	if s.name == "wrong-program-byte" {
		if h.notice.Args[0] != unix.BPF_PROG_LOAD {
			return false, nil
		}
		raw, err := capturedAttribute(h.memory, h.notice)
		if err != nil {
			return false, nil
		}
		input, err := programLoadInput(h.memory, h.notice, raw, b.programLogScratch)
		if err != nil {
			return false, nil
		}
		device, err := deviceProgram()
		if err != nil {
			return false, err
		}
		if !bytes.Equal(input.instructions, device) {
			return false, nil
		}
		before = input.instructions
		address = guardU64(input.outer, 8)
		attributes, rawAttributes = input.normalized, raw
		license = string(input.license[:len(input.license)-1])
		after = append([]byte(nil), before...)
		after[0] = 255
		programBytes = hex.EncodeToString(after)
	} else {
		if h.notice.Args[0] != unix.BPF_PROG_ATTACH {
			return false, nil
		}
		raw, err := capturedAttribute(h.memory, h.notice)
		if err != nil {
			return false, nil
		}
		input, err := captureAttach(h.notice, raw)
		if err != nil || input.flags != unix.BPF_F_ALLOW_MULTI {
			return false, nil
		}
		fd, program, err := b.importBrokerObject(inv, h, input.programSlot, "program")
		if err != nil {
			return false, nil
		}
		if err = unix.Close(fd); err != nil {
			return false, err
		}
		device, err := deviceProgram()
		if err != nil {
			return false, err
		}
		if program.programBytes != hex.EncodeToString(device) {
			return false, nil
		}
		before, address = raw, h.notice.Args[1]
		after = append([]byte(nil), before...)
		if s.name == "wrong-target" {
			original, e := h.importFD(input.targetSlot)
			if e != nil {
				return false, e
			}
			defer unix.Close(original)
			other, e := unix.Open("/sys/fs/cgroup", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if e != nil {
				return false, e
			}
			defer unix.Close(other)
			var targetStat, otherStat unix.Stat_t
			var fs unix.Statfs_t
			if unix.Fstat(original, &targetStat) != nil || unix.Fstat(other, &otherStat) != nil || unix.Fstatfs(other, &fs) != nil || uint64(fs.Type) != unix.CGROUP2_SUPER_MAGIC || targetStat.Dev != otherStat.Dev || targetStat.Ino == otherStat.Ino {
				return false, errGuardProtocol
			}
			add := make([]byte, 24)
			guardPut64(add, 0, h.notice.ID)
			guardPut32(add, 12, uint32(other))
			guardPut32(add, 20, unix.O_CLOEXEC)
			slot, e := notificationIOCTL(h.memory.listener, unix.SECCOMP_IOCTL_NOTIF_ADDFD, add)
			if e != nil || uint64(slot) > math.MaxUint32 {
				return false, errGuardProtocol
			}
			installed, e := h.importFD(uint32(slot))
			if e != nil {
				return false, e
			}
			var installedStat unix.Stat_t
			statErr := unix.Fstat(installed, &installedStat)
			closeErr := unix.Close(installed)
			if statErr != nil || closeErr != nil || installedStat.Dev != otherStat.Dev || installedStat.Ino != otherStat.Ino {
				return false, errGuardProtocol
			}
			targetIdentity = map[string]any{"original_device": jsonU64(uint64(targetStat.Dev)), "original_inode": jsonU64(targetStat.Ino), "substitute_device": jsonU64(uint64(installedStat.Dev)), "substitute_inode": jsonU64(installedStat.Ino)}
			offset = 0
			guardPut32(after, 0, uint32(slot))
		} else {
			offset = 12
			guardPut32(after, 12, 1<<31)
		}
		attributes = after
	}
	if address == 0 || len(before) == 0 {
		return false, errGuardProtocol
	}
	mutation := map[string]any{"kind": s.name, "before_sha256": hashBytes(before), "after_sha256": hashBytes(after), "attr_size": jsonU64(uint64(len(attributes))), "attr_hex": hex.EncodeToString(attributes), "program_bytes": programBytes, "license": license, "target_cookie": nil}
	if targetIdentity != nil {
		mutation["target_identity"] = targetIdentity
	}
	stage := &selftestRuntimeArm{nonce: s.nonce, grantID: s.grantID, invocationID: inv.id, mode: s.name, phase: "mutated", notifyingCookie: h.task.Cookie, notificationID: h.notice.ID, mutation: mutation}
	if err := preflightRuntimeRecord(b, "selftest_runtime_stage", stage.body()); err != nil {
		return false, err
	}
	width := uint64(1)
	if s.name == "wrong-flags" || s.name == "wrong-target" {
		width = 4
	}
	if err := h.memory.copyOut(address+offset, after[offset:offset+width]); err != nil {
		return false, err
	}
	actual, err := h.memory.copyIn(address, len(after))
	if err != nil || !bytes.Equal(actual, after) {
		return false, errGuardProtocol
	}
	mutation["after_sha256"] = hashBytes(actual)
	if s.name == "wrong-program-byte" {
		current, readErr := capturedAttribute(h.memory, h.notice)
		if readErr != nil || !bytes.Equal(current, rawAttributes) {
			return false, errGuardProtocol
		}
		mutation["program_bytes"] = hex.EncodeToString(actual)
	} else {
		mutation["attr_hex"] = hex.EncodeToString(actual)
	}
	if err = b.emitSelftestRuntimeStage(stage, "mutated"); err != nil {
		return false, err
	}
	s.actorCookie, s.notificationID = h.task.Cookie, h.notice.ID
	return true, nil
}

func (b *authorityBroker) advanceSelftestMutation(s *selftestBaselineStep) error {
	if s.notificationID == 0 {
		return nil
	}
	if b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) || b.effects == nil {
		return errGuardProtocol
	}
	for _, tx := range b.effects.transactions {
		if tx != nil && tx.invocationID == s.invocationID && tx.notificationID == s.notificationID {
			return errGuardProtocol
		}
	}
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "notification_denial" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if body["invocation_id"] != jsonU64(s.invocationID) || body["notification_id"] != jsonU64(s.notificationID) {
			continue
		}
		if body["requester_cookie"] != jsonU64(s.actorCookie) || body["result"] != jsonI64(-int64(unix.EPERM)) {
			return errGuardProtocol
		}
		before, ok1 := exactNumber(body["guard_before"])
		after, ok2 := exactNumber(body["guard_after"])
		if !ok1 || !ok2 || after < before {
			return errGuardProtocol
		}
		if before != 0 {
			s.guardSequences = append(s.guardSequences, jsonU64(before))
		}
		if after != 0 && after != before {
			s.guardSequences = append(s.guardSequences, jsonU64(after))
		}
		s.result = body["result"]
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		b.selftestBaseline = nil
		return nil
	}
	return nil
}

func (b *authorityBroker) advanceSelftestFailureCleanup(s *selftestBaselineStep) error {
	if s.transactionID == 0 {
		return nil
	}
	if b.effects == nil || b.effects.objects == nil {
		return errGuardProtocol
	}
	t := b.effects.transactions[s.transactionID]
	if t == nil || t.grantID != s.grantID || t.invocationID != s.invocationID || t.command != "PROG_LOAD" || t.publication == nil || !t.effectExecuted || t.effectErrno != 0 || !t.cleanupActive {
		return errGuardProtocol
	}
	object := b.effects.objects.byCookie[t.publication.ObjectCookie]
	if object == nil || object.class != "program" || object.ownerTransaction != t.id {
		return errGuardProtocol
	}
	if !s.observed {
		s.actorCookie, s.referenceCookie, s.publicationSeq = t.requesterCookie, object.cookie, t.publication.Sequence
		s.result = jsonI64(-int64(unix.ENOENT))
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if object.removalSeq == 0 || t.state != "retired" {
		return nil
	}
	if object.state != "id_removed" || object.removalSeq <= t.publication.Sequence || t.cleanup["state"] != "settled" {
		return errGuardProtocol
	}
	s.removalSeq = object.removalSeq
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) loadSelftestReplacement(s *selftestBaselineStep) (*runtimeEffectTransaction, error) {
	if b.effects.pending != nil || b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 || len(b.effects.transactions) >= 4096 {
		return nil, errGuardProtocol
	}
	device, err := deviceProgram()
	if err != nil || len(device) == 0 || len(device)%8 != 0 {
		return nil, errGuardProtocol
	}
	attr := make([]byte, 40)
	guardPut32(attr, 0, unix.BPF_PROG_TYPE_CGROUP_DEVICE)
	guardPut32(attr, 4, uint32(len(device)/8))
	in := &programInput{outer: append([]byte(nil), attr...), normalized: attr, instructions: device, license: []byte("Apache\x00")}
	id := b.effects.nextTransaction
	t := &runtimeEffectTransaction{id: id, grantID: s.grantID, invocationID: s.invocationID, command: "PROG_LOAD", attributes: attr, program: hex.EncodeToString(device), license: "Apache", inputs: []any{}, state: "prepared", effectFD: -1, internalSelftest: true, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	maximum := map[string]any{"object_cookie": jsonU64(math.MaxUint64), "log_true_size": jsonU64(math.MaxUint32)}
	if err = preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), nil, maximum)); err != nil {
		return nil, err
	}
	if err = appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return nil, err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id], b.effects.pending = t, t
	b.checkpoint.activeTransactions++
	if err = b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		return t, err
	}
	effect, err := executeProgramLoad(nil, seccompNotification{}, in)
	if err != nil {
		return t, err
	}
	t.effectExecuted, t.effectFD, t.effectErrno = true, effect.FD, effect.Errno
	result := int64(effect.FD)
	if effect.Errno != 0 {
		result = -int64(effect.Errno)
	}
	if err = b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "PROG_LOAD", Number: unix.BPF_PROG_LOAD, Result: result}); err != nil {
		return t, err
	}
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		return t, err
	}
	if effect.Errno != 0 || t.publication == nil {
		return t, errGuardProtocol
	}
	output := map[string]any{"object_cookie": jsonU64(t.publication.ObjectCookie), "log_true_size": jsonU64(0)}
	if err = b.transitionRuntimeEffect(t, "effect_observed", jsonI64(result), nil, output); err != nil {
		return t, err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), nil, output); err != nil {
		return t, err
	}
	return t, nil
}

func (b *authorityBroker) attachSelftestReplacement(s *selftestBaselineStep, inv *runtimeInvocation, group *liveCgroup, old, replacement *objectIncarnation) error {
	if b.effects.pending != nil || b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 {
		return errGuardProtocol
	}
	oldFD, oldHeld := b.effects.objectFD[old.cookie]
	newFD, newHeld := b.effects.objectFD[replacement.cookie]
	if !oldHeld || !newHeld || oldFD < 0 || newFD < 0 || old.cookie == replacement.cookie {
		return errGuardProtocol
	}
	flags := uint32(unix.BPF_F_ALLOW_MULTI | unix.BPF_F_REPLACE)
	if _, err := b.authorizeGrantAttachment(inv, group, replacement.cookie, old.cookie, flags); err != nil {
		return err
	}
	attr := make([]byte, 20)
	guardPut32(attr, 8, unix.BPF_CGROUP_DEVICE)
	guardPut32(attr, 12, flags)
	inputs := []any{map[string]any{"field": "target_fd", "kind": "cgroup", "cookie": jsonU64(group.Enrollment), "requester_fd": nil}, map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(replacement.cookie), "requester_fd": nil}, map[string]any{"field": "replace_bpf_fd", "kind": "program", "cookie": jsonU64(old.cookie), "requester_fd": nil}}
	id := b.effects.nextTransaction
	t := &runtimeEffectTransaction{id: id, grantID: s.grantID, invocationID: inv.id, command: "PROG_ATTACH", attributes: attr, inputs: inputs, state: "prepared", effectFD: -1, internalSelftest: true, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	if err := preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), nil, map[string]any{"attachment_id": jsonU64(math.MaxUint64)})); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id], b.effects.pending = t, t
	b.checkpoint.activeTransactions++
	if err := b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		return err
	}
	actual := append([]byte(nil), attr...)
	guardPut32(actual, 0, uint32(group.HeldFD))
	guardPut32(actual, 4, uint32(newFD))
	guardPut32(actual, 16, uint32(oldFD))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_ATTACH, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "PROG_ATTACH", Number: unix.BPF_PROG_ATTACH, Result: signed}); err != nil {
		return err
	}
	if signed != 0 {
		return errGuardProtocol
	}
	attachmentID, err := b.recordGrantAttachment(inv, t, group, replacement.cookie, old.cookie, flags, 0)
	if err != nil {
		return err
	}
	output := map[string]any{"attachment_id": jsonU64(attachmentID)}
	if err = b.transitionRuntimeEffect(t, "effect_observed", jsonI64(0), nil, output); err != nil {
		return err
	}
	if err = b.observeAttachedProgram(inv, group, replacement); err != nil {
		return err
	}
	if err = b.transitionRuntimeEffect(t, "response_settled", jsonI64(0), nil, output); err != nil {
		return err
	}
	s.replacementAttachmentID, s.replacementTransactionID = attachmentID, id
	s.transactionIDs = append(s.transactionIDs, jsonU64(id))
	return nil
}

func (b *authorityBroker) closeSelftestProgram(grantID, cookie uint64) error {
	object := b.effects.objects.byCookie[cookie]
	item := b.checkpoint.lifecycle.objects[cookie]
	fd, held := b.effects.objectFD[cookie]
	if object == nil || object.class != "program" || object.state != "live" || object.removalAuthorized || b.effects.objectGrant[cookie] != grantID || item == nil || item.removalAllowed || item.removed || !held || fd < 0 || b.effects.pending != nil {
		return errGuardProtocol
	}
	for _, alias := range b.effects.aliases {
		if alias != nil && alias.cookie == cookie && !alias.closed {
			return errGuardProtocol
		}
	}
	id, attributes, err := b.observeRuntimeObjectInfo(fd, cookie, object.class, object.objectType)
	if err != nil || uint64(id) != object.id || !sameJSON(attributes, object.attributes) {
		return errGuardProtocol
	}
	cleanup := &grantObjectCleanup{grantID: grantID, creatorID: object.ownerTransaction, cookie: cookie}
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", math.MinInt32, nil)); err != nil {
		return err
	}
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 2, "await_id_removed", math.MinInt32, jsonU64(math.MaxUint64))); err != nil {
		return err
	}
	if err = b.effects.objects.authorizeRemoval(cookie); err != nil {
		return err
	}
	if err = b.resolver.authorizeUnload(cookie, object.ownerTransaction); err != nil {
		return err
	}
	item.removalAllowed = true
	if b.effects.grantCleanup == nil {
		b.effects.grantCleanup = make(map[uint64]*grantObjectCleanup)
	}
	if b.effects.grantCleanup[cookie] != nil {
		return errGuardProtocol
	}
	b.effects.grantCleanup[cookie] = cleanup
	if err = unix.Close(fd); err != nil {
		return err
	}
	delete(b.effects.objectFD, cookie)
	return appendRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil))
}

func (b *authorityBroker) advanceSelftestReplacement(s *selftestBaselineStep) error {
	if b.effects == nil || b.effects.objects == nil || b.resolver == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil {
		return errGuardProtocol
	}
	if b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.lifecycle.paused {
		return nil
	}
	grant := b.grants[s.grantID]
	if grant == nil || grant.owner != s.owner {
		return errGuardProtocol
	}
	if s.extraAttachmentID == 0 {
		for _, a := range b.effects.attachments {
			if a == nil || a.grantID != s.grantID || a.state != "attached" {
				continue
			}
			object := b.effects.objects.byCookie[a.programCookie]
			if object == nil {
				return errGuardProtocol
			}
			load, attach := b.effects.transactions[object.ownerTransaction], b.effects.transactions[a.transactionID]
			if load == nil || attach == nil || load.state != "retired" || attach.state != "response_settled" && attach.state != "retired" {
				continue
			}
			device, err := deviceProgram()
			if err != nil || object.class != "program" || object.programBytes != hex.EncodeToString(device) || a.flags != unix.BPF_F_ALLOW_MULTI || load.invocationID == 0 || load.invocationID != attach.invocationID || load.requesterCookie == 0 {
				return errGuardProtocol
			}
			if s.extraAttachmentID != 0 {
				return errGuardProtocol
			}
			s.extraAttachmentID, s.referenceCookie, s.publicationSeq = a.id, object.cookie, object.publicationSeq
			s.transactionID, s.invocationID, s.actorCookie = load.id, load.invocationID, load.requesterCookie
			s.transactionIDs = []any{jsonU64(attach.id)}
		}
		return nil
	}
	old := b.effects.objects.byCookie[s.referenceCookie]
	original := b.effects.attachments[s.extraAttachmentID]
	if old == nil || original == nil {
		return errGuardProtocol
	}
	if s.secondTransactionID == 0 {
		if b.effects.pending != nil || b.join == nil || !b.join.settled() {
			return nil
		}
		t, err := b.loadSelftestReplacement(s)
		if err != nil {
			return err
		}
		s.secondTransactionID = t.id
		s.transactionIDs = append(s.transactionIDs, jsonU64(t.id))
		return nil
	}
	load := b.effects.transactions[s.secondTransactionID]
	if load == nil || load.publication == nil {
		return errGuardProtocol
	}
	replacement := b.effects.objects.byCookie[load.publication.ObjectCookie]
	if replacement == nil || replacement.cookie == old.cookie {
		return errGuardProtocol
	}
	if s.replacementTransactionID == 0 {
		if load.state != "retired" || b.effects.pending != nil || b.join == nil || !b.join.settled() {
			return nil
		}
		var inv *runtimeInvocation
		for _, candidate := range b.runtimeInvocations {
			if candidate != nil && candidate.id == s.invocationID {
				inv = candidate
				break
			}
		}
		group := b.effects.cgroups[s.grantID]
		if inv == nil || group == nil || group.Retired {
			return errGuardProtocol
		}
		if err := b.attachSelftestReplacement(s, inv, group, old, replacement); err != nil {
			return err
		}
		s.referenceCookies = []any{jsonU64(replacement.cookie)}
		s.guardSequences = []any{jsonU64(replacement.publicationSeq)}
		return nil
	}
	replaceTx := b.effects.transactions[s.replacementTransactionID]
	newAttachment := b.effects.attachments[s.replacementAttachmentID]
	if replaceTx == nil || newAttachment == nil || original.state == "attached" || newAttachment.replaces != original.id || newAttachment.programCookie != replacement.cookie {
		return errGuardProtocol
	}
	if replaceTx.state != "retired" {
		return nil
	}
	if !s.observed {
		s.result = jsonI64(0)
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if b.effects.pending != nil || b.join == nil || !b.join.settled() {
		return nil
	}
	var inv *runtimeInvocation
	for _, candidate := range b.runtimeInvocations {
		if candidate != nil && candidate.id == s.invocationID {
			inv = candidate
			break
		}
	}
	if inv == nil {
		return errGuardProtocol
	}
	if old.removalSeq == 0 && b.effects.grantCleanup[old.cookie] == nil && (inv.state == "settled" || inv.state == "failed") && !livePIDFD(inv.pidfd) {
		if err := b.closeSelftestProgram(s.grantID, old.cookie); err != nil {
			return err
		}
		return nil
	}
	if s.detachTransactionID == 0 {
		if !grant.startObserved || !grant.createSettled {
			return nil
		}
		if s.stopCheck == nil {
			daemon := b.daemons[b.bootOwner]
			if daemon == nil || !livePIDFD(daemon.pidfd) || b.observeDaemonExec(daemon) != nil {
				return errGuardProtocol
			}
			cid := grant.containerID
			check := make(chan bool, 1)
			s.stopCheck, s.stopDaemonCookie = check, daemon.cookie
			go func() {
				item, err := recoveryDockerGET(daemon, "/v1.41/containers/"+cid+"/json", time.Now().Add(3*time.Second))
				state, ok := item["State"].(map[string]any)
				check <- err == nil && item["Id"] == cid && ok && state["Running"] == false && state["Pid"] == json.Number("0")
			}()
			return nil
		}
		select {
		case stopped := <-s.stopCheck:
			s.stopCheck = nil
			if !stopped {
				return nil
			}
		default:
			return nil
		}
		daemon := b.daemons[b.bootOwner]
		if daemon == nil || daemon.cookie != s.stopDaemonCookie || !livePIDFD(daemon.pidfd) || b.observeDaemonExec(daemon) != nil {
			return errGuardProtocol
		}
		group := b.effects.cgroups[s.grantID]
		fd, held := b.effects.objectFD[replacement.cookie]
		if group == nil || group.GrantID != s.grantID || group.Retired || group.HeldFD < 0 || newAttachment.grantID != s.grantID || newAttachment.enrollment != group.Enrollment || newAttachment.rootCookie != group.RootCookie || newAttachment.cgroupID != group.KernfsID || newAttachment.state != "attached" || b.effects.objectGrant[replacement.cookie] != s.grantID || !held || fd < 0 {
			return errGuardProtocol
		}
		if err := b.detachSelftestExtraFD(s, group, newAttachment, fd); err != nil {
			return err
		}
		return nil
	}
	detach := b.effects.transactions[s.detachTransactionID]
	if detach == nil || detach.state != "retired" {
		return nil
	}
	if replacement.removalSeq == 0 && b.effects.grantCleanup[replacement.cookie] == nil {
		if err := b.closeSelftestProgram(s.grantID, replacement.cookie); err != nil {
			return err
		}
		return nil
	}
	if old.removalSeq == 0 || replacement.removalSeq == 0 || grant.phase != "retired" {
		return nil
	}
	if old.state != "id_removed" || replacement.state != "id_removed" || newAttachment.state != "released" {
		return errGuardProtocol
	}
	s.removalSeq = old.removalSeq
	s.guardSequences = append(s.guardSequences, jsonU64(replacement.removalSeq))
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) advanceSelftestExtraFD(s *selftestBaselineStep) error {
	if b.effects == nil || b.effects.objects == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil {
		return errGuardProtocol
	}
	if s.referenceCookie == 0 {
		for _, a := range b.effects.attachments {
			if a == nil || a.grantID != s.grantID || a.state != "attached" {
				continue
			}
			object := b.effects.objects.byCookie[a.programCookie]
			if object == nil {
				return errGuardProtocol
			}
			load, attach := b.effects.transactions[object.ownerTransaction], b.effects.transactions[a.transactionID]
			if load == nil || attach == nil || load.state != "retired" || attach.state != "retired" {
				continue
			}
			device, err := deviceProgram()
			if err != nil || object.class != "program" || object.programBytes != hex.EncodeToString(device) || object.publicationSeq == 0 || a.flags != unix.BPF_F_ALLOW_MULTI || load.invocationID == 0 || load.invocationID != attach.invocationID {
				return errGuardProtocol
			}
			if s.referenceCookie != 0 {
				return errGuardProtocol
			}
			s.referenceCookie, s.publicationSeq, s.extraAttachmentID = object.cookie, object.publicationSeq, a.id
			s.transactionID, s.invocationID = load.id, load.invocationID
			s.transactionIDs = []any{jsonU64(attach.id)}
		}
		return nil
	}
	if s.secondTransactionID == 0 {
		return nil
	}
	object := b.effects.objects.byCookie[s.referenceCookie]
	item := b.checkpoint.lifecycle.objects[s.referenceCookie]
	detach := b.effects.transactions[s.secondTransactionID]
	if object == nil || item == nil || detach == nil {
		return errGuardProtocol
	}
	if detach.state != "retired" {
		return nil
	}
	if !s.observed {
		if object.removalSeq != 0 || item.removalAllowed || !item.deferredRemovalOnNextRelease {
			return errGuardProtocol
		}
		s.result = jsonI64(0)
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if object.removalSeq == 0 {
		return nil
	}
	if !item.removalAllowed || object.state != "id_removed" {
		return errGuardProtocol
	}
	var actor *selftestProbeActor
	for _, candidate := range b.probeActors {
		if candidate != nil && candidate.taskCookie == s.actorCookie {
			actor = candidate
			break
		}
	}
	if actor == nil {
		return errGuardProtocol
	}
	if livePIDFD(actor.pidfd) {
		return nil
	}
	s.removalSeq = object.removalSeq
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) dispatchSelftestExtraFD(actor *selftestProbeActor, n seccompNotification) error {
	s := b.selftestBaseline
	if s == nil || s.name != "extra-fd-delays-removal" || s.actorCookie != actor.taskCookie || s.notificationID != 0 || n.TID != actor.tid || n.Args[0] != unix.BPF_MAP_CREATE || n.Args[2] != 16 {
		return errGuardProtocol
	}
	if err := b.advanceSelftestExtraFD(s); err != nil {
		return err
	}
	if s.referenceCookie == 0 || b.effects.pending != nil || b.checkpoint.active != nil || b.pendingCheckpoint != nil {
		return errGuardProtocol
	}
	task, err := b.authority.guard.pinTask(n.TID)
	if err != nil {
		return err
	}
	defer b.authority.guard.dropTask(task.Cookie)
	if task.Cookie != actor.taskCookie || task.TGID != actor.tgid || task.TID != actor.tid || task.StartBoottimeNS != actor.startNS || !livePIDFD(actor.pidfd) {
		return errGuardProtocol
	}
	mem, err := openNotificationMemory(actor.listenerFD, n)
	if err != nil {
		return err
	}
	defer mem.close()
	input, err := mem.copyIn(n.Args[1], 16)
	if err != nil || !guardZero(input) || notificationValid(actor.listenerFD, n.ID) != nil {
		return errGuardProtocol
	}
	group, a := b.effects.cgroups[s.grantID], b.effects.attachments[s.extraAttachmentID]
	object := b.effects.objects.byCookie[s.referenceCookie]
	item := b.checkpoint.lifecycle.objects[s.referenceCookie]
	fd, held := b.effects.objectFD[s.referenceCookie]
	if group == nil || group.GrantID != s.grantID || group.Retired || group.HeldFD < 0 || a == nil || a.grantID != s.grantID || a.state != "attached" || a.programCookie != s.referenceCookie || a.enrollment != group.Enrollment || a.rootCookie != group.RootCookie || a.cgroupID != group.KernfsID || object == nil || b.effects.objectGrant[object.cookie] != s.grantID || object.state != "live" || object.removalAuthorized || item == nil || item.removalAllowed || item.removed || !held || fd < 0 {
		return errGuardProtocol
	}
	for _, alias := range b.effects.aliases {
		if alias != nil && alias.cookie == object.cookie && !alias.closed {
			return errGuardProtocol
		}
	}
	stage := &selftestRuntimeArm{nonce: s.nonce, grantID: s.grantID, invocationID: s.invocationID, mode: s.name, phase: "injected", notifyingCookie: actor.taskCookie, notificationID: n.ID, mutation: map[string]any{"program_cookie": jsonU64(object.cookie), "listener_id": jsonU64(actor.listenerID), "installed_fd": jsonU64(math.MaxInt32)}}
	if err = preflightRuntimeRecord(b, "selftest_runtime_stage", stage.body()); err != nil {
		return err
	}
	cleanupPreflight := &grantObjectCleanup{grantID: s.grantID, creatorID: object.ownerTransaction, cookie: object.cookie}
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanupPreflight, 1, "close_ref", math.MinInt32, nil)); err != nil {
		return err
	}
	if err = preflightRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanupPreflight, 2, "await_id_removed", math.MinInt32, jsonU64(math.MaxUint64))); err != nil {
		return err
	}
	installed, err := injectNotificationFD(actor.listenerFD, n.ID, fd)
	if err != nil {
		return err
	}
	stage.mutation["installed_fd"] = jsonU64(uint64(installed))
	if err = b.emitSelftestRuntimeStage(stage, "injected"); err != nil {
		return err
	}
	s.notificationID = n.ID
	if err = b.detachSelftestExtraFD(s, group, a, fd); err != nil {
		return err
	}
	if err = b.effects.objects.authorizeRemoval(object.cookie); err != nil {
		return err
	}
	if err = b.resolver.authorizeUnload(object.cookie, object.ownerTransaction); err != nil {
		return err
	}
	item.deferredRemovalOnNextRelease, item.deferredRemovalOwner = true, s.owner
	if b.effects.grantCleanup == nil {
		b.effects.grantCleanup = make(map[uint64]*grantObjectCleanup)
	}
	if b.effects.grantCleanup[object.cookie] != nil {
		return errGuardProtocol
	}
	cleanup := &grantObjectCleanup{grantID: s.grantID, creatorID: object.ownerTransaction, cookie: object.cookie}
	b.effects.grantCleanup[object.cookie] = cleanup
	if err = unix.Close(fd); err != nil {
		return err
	}
	delete(b.effects.objectFD, object.cookie)
	if err = appendRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil)); err != nil {
		return err
	}
	return nil
}

func (b *authorityBroker) detachSelftestExtraFD(s *selftestBaselineStep, group *liveCgroup, a *runtimeAttachment, fd int) error {
	if b.effects.pending != nil || b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 || len(b.effects.transactions) >= 4096 {
		return errGuardProtocol
	}
	attr := make([]byte, 16)
	guardPut32(attr, 8, unix.BPF_CGROUP_DEVICE)
	inputs := []any{map[string]any{"field": "target_fd", "kind": "cgroup", "cookie": jsonU64(group.Enrollment), "requester_fd": nil}, map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(a.programCookie), "requester_fd": nil}}
	id := b.effects.nextTransaction
	t := &runtimeEffectTransaction{id: id, grantID: s.grantID, command: "PROG_DETACH", attributes: attr, inputs: inputs, state: "prepared", effectFD: -1, internalSelftest: true, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	if err := preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), nil, map[string]any{"attachment_id": jsonU64(math.MaxUint64)})); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id], b.effects.pending = t, t
	b.checkpoint.activeTransactions++
	if err := b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		return err
	}
	actual := append([]byte(nil), attr...)
	guardPut32(actual, 0, uint32(group.HeldFD))
	guardPut32(actual, 4, uint32(fd))
	result, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_DETACH, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	signed := int64(result)
	if errno != 0 {
		signed = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "PROG_DETACH", Number: unix.BPF_PROG_DETACH, Result: signed}); err != nil {
		return err
	}
	if signed != 0 {
		return errGuardProtocol
	}
	a.state = "detached"
	if err := b.emitRuntimeAttachment(a); err != nil {
		return err
	}
	output := map[string]any{"attachment_id": jsonU64(a.id)}
	if err := b.transitionRuntimeEffect(t, "effect_observed", jsonI64(0), nil, output); err != nil {
		return err
	}
	if err := b.transitionRuntimeEffect(t, "response_settled", jsonI64(0), nil, output); err != nil {
		return err
	}
	if s.name == "attachment-replace" {
		s.detachTransactionID = id
	} else {
		s.secondTransactionID = id
	}
	s.transactionIDs = append(s.transactionIDs, jsonU64(id))
	return nil
}

// createSelftestReuseMap records and executes one fixed broker-owned map attempt.
func (b *authorityBroker) createSelftestReuseMap() (*runtimeEffectTransaction, error) {
	if b.effects == nil || b.effects.pending != nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.paused || b.effects.nextTransaction == 0 || b.effects.nextTransaction == math.MaxUint64 || len(b.effects.transactions) >= 4096 {
		return nil, errGuardProtocol
	}
	attr := make([]byte, 44)
	guardPut32(attr, 0, unix.BPF_MAP_TYPE_ARRAY)
	guardPut32(attr, 4, 4)
	guardPut32(attr, 8, 4)
	guardPut32(attr, 12, 1)
	copy(attr[28:44], "g2_id_reuse")
	id := b.effects.nextTransaction
	t := &runtimeEffectTransaction{id: id, command: "MAP_CREATE", attributes: attr, inputs: []any{}, state: "prepared", effectFD: -1, internalSelftest: true, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	if err := preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), nil, map[string]any{"object_cookie": jsonU64(math.MaxUint64)})); err != nil {
		return nil, err
	}
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return nil, err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id], b.effects.pending = t, t
	b.checkpoint.activeTransactions++
	if err := b.transitionRuntimeEffect(t, "committed", nil, nil, nil); err != nil {
		return t, err
	}
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_CREATE, uintptr(unsafe.Pointer(&attr[0])), uintptr(len(attr)))
	runtime.KeepAlive(attr)
	t.effectExecuted = true
	if errno != 0 {
		t.effectErrno = errno
	} else if fd <= math.MaxInt32 {
		t.effectFD = int(fd)
	} else {
		return t, errGuardProtocol
	}
	result := int64(t.effectFD)
	if errno != 0 {
		result = -int64(errno)
	}
	if err := b.resolver.recordTransaction(auditedTransaction{ID: id, ExecutorCookie: b.resolver.brokerCookie, Command: "MAP_CREATE", Number: unix.BPF_MAP_CREATE, Result: result}); err != nil {
		return t, err
	}
	if err := drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		return t, err
	}
	if errno != 0 || t.publication == nil {
		return t, errGuardProtocol
	}
	output := map[string]any{"object_cookie": jsonU64(t.publication.ObjectCookie)}
	if err := b.transitionRuntimeEffect(t, "effect_observed", jsonI64(result), nil, output); err != nil {
		return t, err
	}
	if err := b.beginCreationCleanup(t); err != nil {
		return t, err
	}
	if err := b.transitionRuntimeEffect(t, "response_settled", jsonI64(result), nil, output); err != nil {
		return t, err
	}
	return t, nil
}

func (b *authorityBroker) advanceSelftestIDReuse(s *selftestBaselineStep) error {
	if b.effects == nil || b.effects.objects == nil || b.resolver == nil {
		return errGuardProtocol
	}
	if s.transactionID == 0 {
		if b.effects.pending != nil || b.join == nil || !b.join.settled() || b.checkpoint == nil || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.paused {
			return nil
		}
		t, err := b.createSelftestReuseMap()
		if err != nil {
			return err
		}
		s.transactionID, s.actorCookie = t.id, b.resolver.brokerCookie
		return nil
	}
	first := b.effects.transactions[s.transactionID]
	if first == nil || first.publication == nil {
		return errGuardProtocol
	}
	old := b.effects.objects.byCookie[first.publication.ObjectCookie]
	if old == nil {
		return errGuardProtocol
	}
	if first.state != "retired" || old.removalSeq == 0 {
		return nil
	}
	if old.state != "id_removed" || old.removalSeq <= old.publicationSeq {
		return errGuardProtocol
	}
	if s.secondTransactionID == 0 {
		if b.effects.pending != nil || b.join == nil || !b.join.settled() || b.checkpoint == nil || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.paused {
			return nil
		}
		raw, err := hex.DecodeString(s.nonce)
		if err != nil || len(raw) != 16 {
			return errGuardProtocol
		}
		var nonce [16]byte
		copy(nonce[:], raw)
		if err = b.authority.guard.selftestIDReuse(old.cookie, old.removalSeq, b.effects.nextTransaction, nonce); err != nil {
			return err
		}
		t, err := b.createSelftestReuseMap()
		if err != nil {
			return err
		}
		s.secondTransactionID = t.id
		s.transactionIDs = []any{jsonU64(t.id)}
		return nil
	}
	second := b.effects.transactions[s.secondTransactionID]
	if second == nil || second.publication == nil {
		return errGuardProtocol
	}
	newObject := b.effects.objects.byCookie[second.publication.ObjectCookie]
	if newObject == nil || newObject.id != old.id || newObject.cookie == old.cookie {
		return errGuardProtocol
	}
	if !s.observed {
		s.referenceCookie, s.publicationSeq, s.removalSeq = old.cookie, old.publicationSeq, old.removalSeq
		s.referenceCookies = []any{jsonU64(newObject.cookie)}
		s.guardSequences = []any{jsonU64(newObject.publicationSeq)}
		s.result = jsonI64(0)
		if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
			return err
		}
		s.observed = true
	}
	if second.state != "retired" || newObject.removalSeq == 0 {
		return nil
	}
	if newObject.state != "id_removed" || newObject.removalSeq <= newObject.publicationSeq {
		return errGuardProtocol
	}
	s.guardSequences = append(s.guardSequences, jsonU64(newObject.removalSeq))
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) advanceSelftestContainerBPF(s *selftestBaselineStep) error {
	if s.actorCookie == 0 {
		return nil
	}
	if s.name == "container-bpf" {
		if err := b.bindSelftestCreateInvocation(s); err != nil {
			return err
		}
		if s.invocationID == 0 {
			return nil
		}
	} else if b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) {
		return errGuardProtocol
	}
	type rawCall struct {
		operation, sequence uint64
		joined              bool
	}
	calls := map[uint64]*rawCall{}
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "guard" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		rec, ok := body["record"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if rec["task_cookie"] != jsonU64(s.actorCookie) {
			continue
		}
		kind, ok := exactNumber(rec["kind"])
		if !ok {
			return errGuardProtocol
		}
		if kind == 1 || kind == 3 {
			return errGuardProtocol
		}
		if kind != 13 {
			continue
		}
		serial, a := exactNumber(rec["audit_serial"])
		operation, c := exactNumber(rec["operation"])
		sequence, d := exactNumber(rec["sequence"])
		if !a || !c || !d || serial == 0 || sequence == 0 || rec["result"] != jsonI64(-int64(unix.EPERM)) || (operation != unix.BPF_MAP_CREATE && operation != unix.BPF_PROG_LOAD) || calls[serial] != nil {
			return errGuardProtocol
		}
		calls[serial] = &rawCall{operation: operation, sequence: sequence}
	}
	if len(calls) < 2 {
		return nil
	}
	if len(calls) != 2 {
		return errGuardProtocol
	}
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "audit" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		serial, _ := exactNumber(body["serial"])
		call := calls[serial]
		if call == nil {
			continue
		}
		if call.joined || body["task_cookie"] != jsonU64(s.actorCookie) || body["result"] != jsonI64(-int64(unix.EPERM)) || body["raw"] == nil || body["transaction_id"] != nil {
			return errGuardProtocol
		}
		call.joined = true
	}
	var mapCall, progCall *rawCall
	for _, call := range calls {
		if !call.joined {
			return nil
		}
		if call.operation == unix.BPF_MAP_CREATE {
			mapCall = call
		} else {
			progCall = call
		}
	}
	if mapCall == nil || progCall == nil || mapCall.sequence >= progCall.sequence {
		return errGuardProtocol
	}
	s.guardSequences = []any{jsonU64(mapCall.sequence), jsonU64(progCall.sequence)}
	s.result = jsonI64(-int64(unix.EPERM))
	var requestHash any
	if s.requestHash != "" {
		requestHash = s.requestHash
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), requestHash, nil)); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), requestHash, nil)); err != nil {
		return err
	}
	s.witnessed = true
	if s.name == "container-bpf" {
		b.selftestBaseline = nil
	}
	return nil
}

func (b *authorityBroker) advanceSelftestAuditInterleaved(s *selftestBaselineStep) error {
	if b.join == nil || b.join.reorder == nil {
		return nil
	}
	r := b.join.reorder
	if !r.complete {
		return nil
	}
	if b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) {
		return errGuardProtocol
	}
	var cookies, sequences [2]uint64
	var joined [2]bool
	for i := range 2 {
		for _, actor := range s.auditActors {
			if actor != nil && uint64(actor.tgid) == r.seenActors[i] {
				cookies[i] = actor.taskCookie
			}
		}
		if cookies[i] == 0 {
			return errGuardProtocol
		}
	}
	for _, entry := range b.transcript.entries[s.begin:] {
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		for i := range 2 {
			if entry.record["kind"] == "guard" {
				rec, valid := body["record"].(map[string]any)
				if !valid {
					return errGuardProtocol
				}
				if rec["kind"] != jsonU64(13) || rec["audit_serial"] != jsonU64(r.keys[i].Serial) || rec["task_cookie"] != jsonU64(cookies[i]) {
					continue
				}
				if sequences[i] != 0 || rec["result"] != jsonI64(-int64(unix.EPERM)) {
					return errGuardProtocol
				}
				sequences[i], valid = exactNumber(rec["sequence"])
				if !valid || sequences[i] == 0 {
					return errGuardProtocol
				}
			} else if entry.record["kind"] == "audit" && body["serial"] == jsonU64(r.keys[i].Serial) && body["task_cookie"] == jsonU64(cookies[i]) {
				if joined[i] || body["result"] != jsonI64(-int64(unix.EPERM)) || body["raw"] == nil || body["transaction_id"] != nil {
					return errGuardProtocol
				}
				joined[i] = true
			}
		}
	}
	if !joined[0] || !joined[1] || sequences[0] == 0 || sequences[1] == 0 {
		return nil
	}
	s.actorCookie, s.result = cookies[0], jsonI64(-int64(unix.EPERM))
	s.guardSequences = []any{jsonU64(sequences[0]), jsonU64(sequences[1])}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	if err := appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) advanceSelftestSealUnload(s *selftestBaselineStep) error {
	if s.actorCookie == 0 {
		return nil
	}
	var actor *selftestProbeActor
	for _, candidate := range b.probeActors {
		if candidate != nil && candidate.name == s.name && candidate.taskCookie == s.actorCookie {
			if actor != nil {
				return errGuardProtocol
			}
			actor = candidate
		}
	}
	if actor == nil {
		return errGuardProtocol
	}
	if !actor.resultEOF {
		var buf [4096]byte
		for {
			n, err := unix.Read(actor.resultFD, buf[:])
			if n > 0 {
				if len(actor.resultBytes)+n > 16384 {
					return errGuardProtocol
				}
				actor.resultBytes = append(actor.resultBytes, buf[:n]...)
			}
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				return nil
			}
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return err
			}
			if n == 0 {
				actor.resultEOF = true
				break
			}
		}
	}
	if !actor.resultEOF || livePIDFD(actor.pidfd) {
		return nil
	}
	if len(actor.resultBytes) < 2 || actor.resultBytes[len(actor.resultBytes)-1] != '\n' {
		return errGuardProtocol
	}
	frame, err := parseCanonicalControl(actor.resultBytes[:len(actor.resultBytes)-1])
	if err != nil || !exactKeys(frame, "mode", "tid", "result") || frame["mode"] != s.name || frame["tid"] != jsonI64(int64(actor.tid)) {
		return errGuardProtocol
	}
	number, ok := frame["result"].(json.Number)
	if !ok {
		return errGuardProtocol
	}
	result, err := number.Int64()
	if err != nil || result >= 0 || result == -int64(unix.ENOENT) {
		return errGuardProtocol
	}
	var exitSequence uint64
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "guard" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		rec, ok := body["record"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if rec["kind"] != jsonU64(11) || rec["task_cookie"] != jsonU64(actor.taskCookie) {
			continue
		}
		detail, ok := rec["detail"].(map[string]any)
		if !ok || detail["exit_code"] != jsonI64(0) || exitSequence != 0 {
			return errGuardProtocol
		}
		exitSequence, ok = exactNumber(rec["sequence"])
		if !ok || exitSequence == 0 {
			return errGuardProtocol
		}
	}
	if exitSequence == 0 {
		return nil
	}
	status, err := b.authority.guard.status()
	if err != nil || status.State != 1 || status.AuthorityPhase != 1 || status.ModuleInstance != s.sealStatus.ModuleInstance || status.LinkIDs != s.sealStatus.LinkIDs || status.LinkAttached != [3]uint32{1, 1, 1} || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 {
		return errGuardProtocol
	}
	if err = appendRuntimeRecord(b, "selftest_guard_status", selftestSealStatusRecord(s, "after", status)); err != nil {
		return err
	}
	s.result, s.guardSequences = jsonI64(result), []any{jsonU64(exitSequence)}
	if err = appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(actor.taskCookie), nil, nil)); err != nil {
		return err
	}
	if err = appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(actor.taskCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

func (b *authorityBroker) advanceSelftestSealDetach(s *selftestBaselineStep) error {
	if s.actorCookie == 0 || b.transcript == nil || s.begin < 0 || s.begin > len(b.transcript.entries) {
		return errGuardProtocol
	}
	var serial, denialSeq, exitSeq uint64
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "guard" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		rec, ok := body["record"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if rec["task_cookie"] != jsonU64(s.actorCookie) {
			continue
		}
		if rec["kind"] == jsonU64(13) && rec["operation"] == jsonU64(unix.BPF_LINK_DETACH) {
			if serial != 0 || rec["result"] != jsonI64(-int64(unix.EPERM)) {
				return errGuardProtocol
			}
			serial, ok = exactNumber(rec["audit_serial"])
			if !ok || serial == 0 {
				return errGuardProtocol
			}
			denialSeq, ok = exactNumber(rec["sequence"])
			if !ok || denialSeq == 0 {
				return errGuardProtocol
			}
		} else if rec["kind"] == jsonU64(11) {
			detail, valid := rec["detail"].(map[string]any)
			if !valid || detail["exit_code"] != jsonI64(0) || exitSeq != 0 {
				return errGuardProtocol
			}
			exitSeq, ok = exactNumber(rec["sequence"])
			if !ok || exitSeq == 0 {
				return errGuardProtocol
			}
		}
	}
	if serial == 0 || exitSeq == 0 {
		return nil
	}
	joined := false
	for _, entry := range b.transcript.entries[s.begin:] {
		if entry.record["kind"] != "audit" {
			continue
		}
		body, ok := entry.record["body"].(map[string]any)
		if !ok {
			return errGuardProtocol
		}
		if body["serial"] != jsonU64(serial) {
			continue
		}
		if joined || body["task_cookie"] != jsonU64(s.actorCookie) || body["command"] != "LINK_DETACH" || body["result"] != jsonI64(-int64(unix.EPERM)) || body["raw"] == nil || body["transaction_id"] != nil {
			return errGuardProtocol
		}
		joined = true
	}
	if !joined {
		return nil
	}
	status, err := b.authority.guard.status()
	if err != nil || status.State != 1 || status.AuthorityPhase != 1 || status.ModuleInstance != s.sealStatus.ModuleInstance || status.LinkIDs != s.sealStatus.LinkIDs || status.LinkAttached != [3]uint32{1, 1, 1} || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 {
		return errGuardProtocol
	}
	if err = appendRuntimeRecord(b, "selftest_guard_status", selftestSealStatusRecord(s, "after", status)); err != nil {
		return err
	}
	s.result, s.guardSequences = jsonI64(-int64(unix.EPERM)), []any{jsonU64(denialSeq), jsonU64(exitSeq)}
	if err = appendRuntimeRecord(b, "selftest_case", s.record("observed", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	if err = appendRuntimeRecord(b, "selftest_case", s.record("settled", jsonU64(s.actorCookie), nil, nil)); err != nil {
		return err
	}
	b.selftestBaseline = nil
	return nil
}

var (
	fixedProbePatternA = bytes.Repeat([]byte{0x41}, 32)
	fixedProbePatternB = bytes.Repeat([]byte{0x42}, 32)
)

func verifyProbeFixtureMemfd(fd int, wantPattern []byte) (uint64, uint64, error) {
	if fd < 0 || len(wantPattern) != 32 {
		return 0, 0, errGuardProtocol
	}
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || seals&moduleSealsMask != moduleSealsMask {
		return 0, 0, errGuardProtocol
	}
	raw, err := readHeldRegular(fd, 32)
	if err != nil || !bytes.Equal(raw, wantPattern) {
		return 0, 0, errGuardProtocol
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatfs(fd, &fs) != nil || int64(fs.Type) != unix.TMPFS_MAGIC || st.Size != 32 || st.Ino == 0 {
		return 0, 0, errGuardProtocol
	}
	dev, err := kernelDevice(uint64(st.Dev))
	if err != nil {
		return 0, 0, err
	}
	return dev, st.Ino, nil
}

// registerSelftestProbe implements REGISTER_SELFTEST_PROBE.
func (b *authorityBroker) registerSelftestProbe(p *controlPrincipal, request map[string]any, raw []byte, rights []int) ([]byte, bool, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, false, errControlFrame
	}
	scenario, _ := b.caseFacts["scenario"].(string)
	if scenario != "bpf-authorization" && scenario != "bpf-broker-kill" {
		return nil, false, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, false, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !(exactKeys(body, "name", "child_pid", "child_starttime") || exactKeys(body, "name", "child_pid", "child_starttime", "parent_probe_cookie")) {
		return nil, false, errControlFrame
	}
	name, ok := body["name"].(string)
	if !ok {
		return nil, false, errControlFrame
	}
	var parentProbe *selftestProbeActor
	if name == "tid-reuse-child" {
		parentID, valid := exactNumber(body["parent_probe_cookie"])
		parentProbe = b.probeActors[parentID]
		if !valid || parentProbe == nil || parentProbe.name != "tid-reuse" || parentProbe.parentCookie != p.TaskCookie || parentProbe.listenerFD < 0 || !livePIDFD(parentProbe.pidfd) {
			return nil, false, errControlFrame
		}
	} else if !exactKeys(body, "name", "child_pid", "child_starttime") {
		return nil, false, errControlFrame
	}
	childPIDNum, ok := exactNumber(body["child_pid"])
	if !ok || childPIDNum <= 1 || childPIDNum > math.MaxInt32 {
		return nil, false, errControlFrame
	}
	childStart, ok := exactNumber(body["child_starttime"])
	if !ok || childStart == 0 {
		return nil, false, errControlFrame
	}
	isRaw := false
	isImportProbe := false
	isExtraFD := false
	switch name {
	case "raw-denial", "post-death-raw", "ring-overflow-filler", "tid-reuse-child", "audit-overflow-bind-filler", "seal-detach-denied", "seal-unload-denied":
		isRaw = true
	case "fd-slot-reuse", "nonleader-files", "shared-files", "leader-exit", "tid-reuse":
		isImportProbe = true
	case "extra-fd-delays-removal":
		isExtraFD = true
	default:
		return nil, false, errControlFrame
	}
	if scenario == "bpf-broker-kill" && name != "raw-denial" && name != "post-death-raw" {
		return nil, false, errControlFrame
	}
	variant, _ := b.caseFacts["bpf_variant"].(string)
	if name == "audit-overflow-bind-filler" && variant != "audit-overflow" || (name == "seal-detach-denied" || name == "seal-unload-denied" || name == "tid-reuse-child" || isExtraFD) && variant != "" && variant != "baseline" {
		return nil, false, errControlFrame
	}
	if isRaw && name != "seal-unload-denied" && name != "seal-detach-denied" && len(rights) != 1 || (name == "seal-unload-denied" || name == "seal-detach-denied") && len(rights) != 2 || isImportProbe && len(rights) != 4 || isExtraFD && len(rights) != 2 {
		return nil, false, errControlFrame
	}
	if name == "seal-unload-denied" && (b.selftestBaseline == nil || b.selftestBaseline.name != name || b.selftestBaseline.owner != p.ID || b.selftestBaseline.actorCookie != 0) {
		return nil, false, errControlFrame
	}
	auditActor := name == "raw-denial" && b.selftestBaseline != nil && b.selftestBaseline.name == "audit-interleaved"
	if auditActor && (b.selftestBaseline.owner != p.ID || b.selftestBaseline.auditActors[1] != nil || b.join == nil) {
		return nil, false, errControlFrame
	}
	childPID := uint32(childPIDNum)
	if !pidfdSelects(rights[0], childPID) {
		return nil, false, errControlFrame
	}
	childProc, err := observeProcess(childPID, rights[0])
	expectedParent := p.PID
	if parentProbe != nil {
		expectedParent = parentProbe.tgid
	}
	if err != nil || childProc.PPID != expectedParent || childProc.StartTicks != childStart || childProc.UID != 0 || childProc.GID != 0 || childProc.ExecutableSHA256 != b.manifest.BrokerSHA256 {
		return nil, false, errGuardProtocol
	}
	if name == "audit-overflow-bind-filler" || name == "seal-detach-denied" || name == "seal-unload-denied" || name == "tid-reuse-child" || isExtraFD {
		probeMode := name
		if parentProbe != nil {
			probeMode = parentProbe.name
		}
		if !exactArgumentVector(childProc.Argv, []string{"/opt/g2/g2-bpf-authority", "selftest", probeMode}) {
			return nil, false, errGuardProtocol
		}
	}
	if err = verifyProcessExecutable(childPID, "/opt/g2/g2-bpf-authority", b.manifest.BrokerSHA256); err != nil {
		return nil, false, err
	}
	if err = b.installBrokerLineage(); err != nil {
		return nil, false, err
	}
	var devA, inoA, devB, inoB uint64
	var listenerID uint64
	var listenerIDVal any
	if isExtraFD && runtimeListenerFD(rights[1]) != nil {
		return nil, false, errControlFrame
	}
	if isExtraFD {
		listenerID, err = b.nextListenerID()
		if err != nil {
			return nil, false, err
		}
		listenerIDVal = jsonU64(listenerID)
	}
	if isImportProbe {
		if runtimeListenerFD(rights[1]) != nil {
			return nil, false, errControlFrame
		}
		devA, inoA, err = verifyProbeFixtureMemfd(rights[2], fixedProbePatternA)
		if err != nil {
			return nil, false, err
		}
		devB, inoB, err = verifyProbeFixtureMemfd(rights[3], fixedProbePatternB)
		if err != nil || (devA == devB && inoA == inoB) {
			return nil, false, errGuardProtocol
		}
		listenerID, err = b.nextListenerID()
		if err != nil {
			return nil, false, err
		}
		listenerIDVal = jsonU64(listenerID)
	}
	if parentProbe != nil {
		listenerID = parentProbe.listenerID
		listenerIDVal = jsonU64(listenerID)
	}
	task, err := b.authority.guard.pinTask(childPID)
	if err != nil {
		return nil, false, err
	}
	keepTask := false
	defer func() {
		if !keepTask {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TGID != childPID || task.TID != childPID || task.StartBoottimeNS/10_000_000 != childStart || !livePIDFD(rights[0]) {
		return nil, false, errGuardProtocol
	}
	if name == "seal-unload-denied" {
		var readStat, writeStat unix.Stat_t
		readFlags, e := unix.FcntlInt(uintptr(rights[1]), unix.F_GETFL, 0)
		if e != nil || readFlags&unix.O_ACCMODE != unix.O_RDONLY || unix.Fstat(rights[1], &readStat) != nil || readStat.Mode&unix.S_IFMT != unix.S_IFIFO {
			return nil, false, errControlFrame
		}
		outFD, e := b.authority.guard.importFD(task.Cookie, 1)
		if e != nil {
			return nil, false, e
		}
		writeFlags, flagErr := unix.FcntlInt(uintptr(outFD), unix.F_GETFL, 0)
		statErr := unix.Fstat(outFD, &writeStat)
		closeErr := unix.Close(outFD)
		if flagErr != nil || statErr != nil || closeErr != nil || writeFlags&unix.O_ACCMODE != unix.O_WRONLY || writeStat.Mode&unix.S_IFMT != unix.S_IFIFO || readStat.Dev != writeStat.Dev || readStat.Ino != writeStat.Ino || !livePIDFD(rights[0]) {
			return nil, false, errGuardProtocol
		}
		if err = unix.SetNonblock(rights[1], true); err != nil {
			return nil, false, err
		}
	}
	if name == "seal-detach-denied" {
		kind, e := unix.GetsockoptInt(rights[1], unix.SOL_SOCKET, unix.SO_TYPE)
		childFD, importErr := b.authority.guard.importFD(task.Cookie, 9)
		if importErr != nil {
			return nil, false, errControlFrame
		}
		if e != nil || kind != unix.SOCK_SEQPACKET {
			_ = unix.Close(childFD)
			return nil, false, errControlFrame
		}
		childKind, childErr := unix.GetsockoptInt(childFD, unix.SOL_SOCKET, unix.SO_TYPE)
		credentialErr := unix.SetsockoptInt(childFD, unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
		closeErr := unix.Close(childFD)
		if childErr != nil || childKind != unix.SOCK_SEQPACKET || credentialErr != nil || closeErr != nil || !livePIDFD(rights[0]) {
			return nil, false, errGuardProtocol
		}
	}
	if _, err = b.authority.guard.watchProbe(task.Cookie, 1); err != nil {
		return nil, false, err
	}
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	if b.nextProbeCookie == 0 {
		b.nextProbeCookie = 1
	}
	if b.nextProbeCookie == math.MaxUint64 {
		return nil, false, errGuardProtocol
	}
	probeCookie := b.nextProbeCookie
	b.nextProbeCookie++
	parentCookie := p.TaskCookie
	if parentProbe != nil {
		parentCookie = parentProbe.taskCookie
	}
	actorBody := map[string]any{
		"name":          name,
		"probe_cookie":  jsonU64(probeCookie),
		"task_cookie":   jsonU64(task.Cookie),
		"tgid":          jsonU64(uint64(task.TGID)),
		"tid":           jsonU64(uint64(task.TID)),
		"start_ns":      jsonU64(task.StartBoottimeNS),
		"parent_cookie": jsonU64(parentCookie),
		"exe_sha256":    childProc.ExecutableSHA256,
		"listener_id":   listenerIDVal,
	}
	if err = appendRuntimeRecord(b, "probe_actor", actorBody); err != nil {
		return nil, false, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{
		"probe_cookie": jsonU64(probeCookie),
		"listener_id":  listenerIDVal,
	})
	if err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	if err = p.commitReply(requestID, raw, ack, rights); err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	actor := &selftestProbeActor{
		name:          name,
		probeCookie:   probeCookie,
		taskCookie:    task.Cookie,
		tgid:          task.TGID,
		tid:           task.TID,
		startNS:       task.StartBoottimeNS,
		parentCookie:  parentCookie,
		exeSHA256:     childProc.ExecutableSHA256,
		pidfd:         rights[0],
		listenerFD:    -1,
		listenerID:    listenerID,
		fixtureAFD:    -1,
		fixtureBFD:    -1,
		resultFD:      -1,
		pipeFD:        -1,
		importWatched: isImportProbe,
	}
	if name == "seal-unload-denied" {
		actor.resultFD = rights[1]
		b.selftestBaseline.actorCookie = task.Cookie
	}
	if name == "seal-detach-denied" {
		actor.pipeFD = rights[1]
	}
	if isExtraFD {
		actor.listenerFD = rights[1]
		if b.probeListeners == nil {
			b.probeListeners = make(map[int]*selftestProbeActor)
		}
		b.probeListeners[actor.listenerFD] = actor
	}
	if isImportProbe {
		actor.listenerFD = rights[1]
		actor.fixtureAFD = rights[2]
		actor.fixtureADev = devA
		actor.fixtureAIno = inoA
		actor.fixtureBFD = rights[3]
		actor.fixtureBDev = devB
		actor.fixtureBIno = inoB
		if b.probeListeners == nil {
			b.probeListeners = make(map[int]*selftestProbeActor)
		}
		b.probeListeners[actor.listenerFD] = actor
	}
	if b.probeActors == nil {
		b.probeActors = make(map[uint64]*selftestProbeActor)
	}
	b.probeActors[probeCookie] = actor
	if auditActor {
		s := b.selftestBaseline
		if s.auditActors[0] == nil {
			s.auditActors[0] = actor
		} else {
			if s.auditActors[0].tgid == actor.tgid || s.auditActors[0].taskCookie == actor.taskCookie {
				b.checkpoint.markFault()
				return nil, false, errGuardProtocol
			}
			s.auditActors[1] = actor
			b.join.reorder = &auditReorder{actors: [2]uint64{uint64(s.auditActors[0].tgid), uint64(actor.tgid)}, nonce: s.nonce}
		}
	}
	if scenario == "bpf-authorization" && (variant == "" || variant == "baseline") && (isImportProbe || name == "raw-denial" && !auditActor) {
		caseName := name
		if name == "raw-denial" {
			caseName = "wrong-actor"
		}
		actor.caseName, actor.caseHash = caseName, hashBytes(raw)
		step := &selftestBaselineStep{name: caseName, nonce: actor.caseHash[:32]}
		if err = appendRuntimeRecord(b, "selftest_case", step.record("started", jsonU64(task.Cookie), actor.caseHash, nil)); err != nil {
			b.checkpoint.markFault()
			return nil, false, err
		}
		actor.caseBegin = len(b.transcript.entries)
	}
	keepTask = true
	return ack, true, nil
}

// probeNotificationActor resolves a reused TID to its independently registered
// incarnation. The listener and fixtures remain owned by the parent probe;
// possession of that listener alone does not authenticate its child.
func (b *authorityBroker) probeNotificationActor(listenerOwner *selftestProbeActor, task guardTask) *selftestProbeActor {
	if listenerOwner == nil || listenerOwner.name != "tid-reuse" {
		return listenerOwner
	}
	if task.Cookie == listenerOwner.taskCookie && task.TID == listenerOwner.tid && task.TGID == listenerOwner.tgid && task.StartBoottimeNS == listenerOwner.startNS && livePIDFD(listenerOwner.pidfd) {
		return listenerOwner
	}
	var found *selftestProbeActor
	for _, candidate := range b.probeActors {
		if candidate == nil || candidate.name != "tid-reuse-child" || candidate.parentCookie != listenerOwner.taskCookie || candidate.listenerID != listenerOwner.listenerID || candidate.taskCookie != task.Cookie || candidate.tid != task.TID || candidate.tgid != task.TGID || candidate.startNS != task.StartBoottimeNS || !livePIDFD(candidate.pidfd) {
			continue
		}
		if found != nil {
			return nil
		}
		found = candidate
	}
	return found
}

// dispatchProbeNotification handles a seccomp notification on a registered
// selftest import probe listener. It pins the actual notifying TID, imports
// the descriptor slot from that TID, compares it to held fixtures A/B, emits
// `probe_import`, and denies the BPF call with EPERM.
func (b *authorityBroker) dispatchProbeNotification(actor *selftestProbeActor, n seccompNotification) error {
	if b == nil || actor == nil || actor.listenerFD < 0 || b.checkpoint == nil || b.checkpoint.invalid {
		return errGuardProtocol
	}
	if actor.name == "extra-fd-delays-removal" {
		return b.dispatchSelftestExtraFD(actor, n)
	}
	if err := drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return err
	}
	before := b.checkpoint.cursor
	slot := uint32(10)
	var notifyingCookie uint64
	var importedDev, importedIno uint64
	fixtureVal := any(nil)
	resultStr := "denied"
	probeCookie := actor.probeCookie

	task, pinErr := b.authority.guard.pinTask(n.TID)
	if pinErr == nil {
		identity := b.probeNotificationActor(actor, task)
		if identity == nil {
			_ = b.authority.guard.dropTask(task.Cookie)
			if err := replyNotification(actor.listenerFD, n.ID, 0, unix.EPERM); err != nil && !errors.Is(err, unix.ENOENT) {
				b.checkpoint.markFault()
				return err
			}
			return nil
		}
		probeCookie = identity.probeCookie
		notifyingCookie = task.Cookie
		if mem, memErr := openNotificationMemory(actor.listenerFD, n); memErr == nil {
			if attr, attrErr := mem.copyIn(n.Args[1], 8); attrErr == nil && len(attr) == 8 {
				slot = guardU32(attr, 0)
			}
			_ = mem.close()
		}
		impFD, impErr := b.authority.guard.importFD(task.Cookie, slot)
		if impErr == nil && impFD >= 0 {
			var st unix.Stat_t
			if unix.Fstat(impFD, &st) == nil {
				if dev, de := kernelDevice(uint64(st.Dev)); de == nil && st.Ino != 0 {
					importedDev = dev
					importedIno = st.Ino
					if sameHeldDescription(impFD, actor.fixtureAFD) || (dev == actor.fixtureADev && st.Ino == actor.fixtureAIno) {
						fixtureVal = "a"
						resultStr = "imported"
					} else if sameHeldDescription(impFD, actor.fixtureBFD) || (dev == actor.fixtureBDev && st.Ino == actor.fixtureBIno) {
						fixtureVal = "b"
						resultStr = "imported"
					} else {
						resultStr = "mismatch"
					}
				}
			}
			_ = unix.Close(impFD)
		} else {
			resultStr = "ebadf"
		}
		_ = b.authority.guard.dropTask(task.Cookie)
	} else {
		resultStr = "esrch"
	}
	replyErr := replyNotification(actor.listenerFD, n.ID, 0, unix.EPERM)
	if replyErr != nil && !errors.Is(replyErr, unix.ENOENT) {
		b.checkpoint.markFault()
		return replyErr
	}
	if err := drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return err
	}
	after := b.checkpoint.cursor
	var notifCookieVal, devVal, inoVal any
	if notifyingCookie != 0 {
		notifCookieVal = jsonU64(notifyingCookie)
	}
	if importedIno != 0 {
		devVal = jsonU64(importedDev)
		inoVal = jsonU64(importedIno)
	}
	body := map[string]any{
		"probe_cookie":     jsonU64(probeCookie),
		"notifying_cookie": notifCookieVal,
		"notification_id":  jsonU64(n.ID),
		"slot":             jsonU64(uint64(slot)),
		"imported_device":  devVal,
		"imported_inode":   inoVal,
		"fixture":          fixtureVal,
		"guard_begin":      jsonU64(before),
		"guard_end":        jsonU64(after),
		"result":           resultStr,
	}
	if err := appendRuntimeRecord(b, "probe_import", body); err != nil {
		return err
	}
	if actor.caseName != "" && !actor.caseSettled && resultStr == "imported" && fixtureVal == "b" && notifyingCookie != 0 && replyErr == nil {
		step := &selftestBaselineStep{name: actor.caseName, nonce: actor.caseHash[:32], result: jsonI64(-int64(unix.EPERM))}
		if before != 0 {
			step.guardSequences = append(step.guardSequences, jsonU64(before))
		}
		if after != 0 && after != before {
			step.guardSequences = append(step.guardSequences, jsonU64(after))
		}
		if err := appendRuntimeRecord(b, "selftest_case", step.record("observed", jsonU64(notifyingCookie), actor.caseHash, nil)); err != nil {
			return err
		}
		if err := appendRuntimeRecord(b, "selftest_case", step.record("settled", jsonU64(notifyingCookie), actor.caseHash, nil)); err != nil {
			return err
		}
		actor.caseSettled = true
	}
	return nil
}

func (s *selftestRuntimeArm) body() map[string]any {
	var invVal, txVal, notifCookieVal, notifIDVal, mutVal, sigVal any
	if s.invocationID != 0 {
		invVal = jsonU64(s.invocationID)
	}
	if s.transactionID != 0 {
		txVal = jsonU64(s.transactionID)
	}
	if s.notifyingCookie != 0 {
		notifCookieVal = jsonU64(s.notifyingCookie)
	}
	if s.notificationID != 0 {
		notifIDVal = jsonU64(s.notificationID)
	}
	if s.mutation != nil {
		mutVal = s.mutation
	}
	if s.signal != nil {
		sigVal = s.signal
	}
	return map[string]any{
		"nonce":            s.nonce,
		"grant_id":         jsonU64(s.grantID),
		"invocation_id":    invVal,
		"transaction_id":   txVal,
		"mode":             s.mode,
		"phase":            s.phase,
		"notifying_cookie": notifCookieVal,
		"notification_id":  notifIDVal,
		"mutation":         mutVal,
		"signal":           sigVal,
	}
}

func (b *authorityBroker) emitSelftestRuntimeStage(s *selftestRuntimeArm, phase string) error {
	if b == nil || s == nil {
		return errGuardProtocol
	}
	s.phase = phase
	if err := appendRuntimeRecord(b, "selftest_runtime_stage", s.body()); err != nil {
		return err
	}
	switch s.mode {
	case "nested-buffer-race", "commit-before-cancel", "after-effect-cancel", "after-reply-cancel":
	default:
		return nil
	}
	casePhase := ""
	if phase == "armed" {
		casePhase = "started"
	}
	if phase == "mutated" && s.mode == "nested-buffer-race" || phase == "cancelled" && s.mode != "nested-buffer-race" {
		casePhase = "observed"
	}
	if phase == "settled" {
		casePhase = "settled"
	}
	if casePhase == "" {
		return nil
	}
	if casePhase == "observed" {
		if s.caseObserved || s.notifyingCookie == 0 || s.invocationID == 0 {
			return errGuardProtocol
		}
		s.caseObserved = true
	}
	if casePhase == "settled" && !s.caseObserved {
		return errGuardProtocol
	}
	step := &selftestBaselineStep{name: s.mode, nonce: s.nonce, grantID: s.grantID, invocationID: s.invocationID, transactionID: s.transactionID}
	var actor any
	if s.notifyingCookie != 0 {
		actor = jsonU64(s.notifyingCookie)
	}
	if s.signal != nil {
		step.result = s.signal["send_result"]
	}
	if casePhase == "settled" && b.effects != nil {
		if tx := b.effects.transactions[s.transactionID]; tx != nil {
			step.result = tx.result
			if tx.publication != nil {
				step.referenceCookie, step.publicationSeq = tx.publication.ObjectCookie, tx.publication.Sequence
			}
		}
	}
	return appendRuntimeRecord(b, "selftest_case", step.record(casePhase, actor, nil, nil))
}
func runBoundedSignalWorker(pidfd int, taskCookie uint64) (map[string]any, error) {
	if pidfd < 0 || taskCookie == 0 {
		return nil, errGuardProtocol
	}
	ch := make(chan int64, 1)
	go func() {
		err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
		if err == nil {
			ch <- 0
			return
		}
		var errno unix.Errno
		if errors.As(err, &errno) && errno != 0 {
			ch <- -int64(errno)
			return
		}
		ch <- -int64(unix.EIO)
	}()
	res := <-ch
	// Wait briefly until the killed task's seccomp notification or process state reflects SIGKILL
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		if n, _ := unix.Poll(p, 10); n > 0 && p[0].Revents&unix.POLLIN != 0 {
			break
		}
	}
	return map[string]any{
		"task_cookie": jsonU64(taskCookie),
		"signal":      jsonU64(uint64(unix.SIGKILL)),
		"send_result": jsonI64(res),
	}, nil
}

func runBoundedMutationWorker(m *notificationMemory, insnAddr uint64, origByte byte, mutByte byte, restore bool) error {
	if m == nil || m.fd < 0 || insnAddr == 0 {
		return errGuardProtocol
	}
	ch := make(chan error, 1)
	go func() {
		cur, err := m.copyIn(insnAddr, 1)
		if err != nil || len(cur) != 1 {
			ch <- errGuardProtocol
			return
		}
		if !restore {
			if cur[0] != origByte {
				ch <- errGuardProtocol
				return
			}
			if err := m.copyOut(insnAddr, []byte{mutByte}); err != nil {
				ch <- err
				return
			}
			after, err := m.copyIn(insnAddr, 1)
			if err != nil || len(after) != 1 || after[0] != mutByte {
				ch <- errGuardProtocol
				return
			}
			ch <- nil
			return
		}
		if cur[0] != mutByte {
			ch <- errGuardProtocol
			return
		}
		if err := m.copyOut(insnAddr, []byte{origByte}); err != nil {
			ch <- err
			return
		}
		after, err := m.copyIn(insnAddr, 1)
		if err != nil || len(after) != 1 || after[0] != origByte {
			ch <- errGuardProtocol
			return
		}
		ch <- nil
	}()
	return <-ch
}

// armSelftestRuntime implements SELFTEST_RUNTIME_ARM.
func (b *authorityBroker) armSelftestRuntime(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.selftestBaseline != nil || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, errControlFrame
	}
	if v, ok := b.caseFacts["bpf_variant"].(string); ok && v != "" && v != "baseline" {
		return nil, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "nonce", "grant_id", "mode") {
		return nil, errControlFrame
	}
	nonce, ok := body["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return nil, errControlFrame
	}
	grantID, ok := exactNumber(body["grant_id"])
	if !ok || grantID == 0 {
		return nil, errControlFrame
	}
	mode, ok := body["mode"].(string)
	if !ok {
		return nil, errControlFrame
	}
	switch mode {
	case "nested-buffer-race", "commit-before-cancel", "after-effect-cancel", "after-reply-cancel":
	default:
		return nil, errControlFrame
	}
	if b.selftestRuntimeModesUsed == nil {
		b.selftestRuntimeModesUsed = make(map[string]bool)
	}
	if b.selftestRuntimeModesUsed[mode] {
		return nil, errControlFrame
	}
	grant := b.grants[grantID]
	if grant == nil || grant.owner != p.ID || grant.phase != "admitted" || grant.createBound {
		return nil, errControlFrame
	}
	switch grant.purpose {
	case "postgres-init-app", "postgres-init-w1", "postgres-init-w2", "postgres-init-control":
	default:
		return nil, errControlFrame
	}
	template, err := b.templateForPrincipal(p, grant.purpose)
	if err != nil || !sameTemplate(grant.config, template) {
		return nil, errControlFrame
	}
	now, err := monoNowNS()
	if err != nil || now >= authorityGuestDeadlineNS {
		return nil, errGuardProtocol
	}
	arm := &selftestRuntimeArm{
		nonce:    nonce,
		owner:    p.ID,
		grantID:  grantID,
		mode:     mode,
		phase:    "armed",
		deadline: authorityGuestDeadlineNS,
	}
	if err = preflightRuntimeRecord(b, "selftest_runtime_stage", arm.body()); err != nil {
		return nil, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"armed": true})
	if err != nil {
		return nil, err
	}
	if err = b.emitSelftestRuntimeStage(arm, "armed"); err != nil {
		return nil, err
	}
	if err = p.commitReply(requestID, raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	b.selftestRuntimeModesUsed[mode] = true
	b.selftestRuntime = arm
	return ack, nil
}

// selftestRevokeAndClose implements SELFTEST_REVOKE_AND_CLOSE for
// realm-negative/bpf-broker-kill. After ACK, on the original locked main thread,
// it watches kind 2 on the original broker cookie, calls REVOKE -> phase 2,
// attempts MAP_CREATE / PROG_LOAD / PROG_ATTACH (all EPERM), performs a
// preauthorized registry lookup (allowed in phase 2), closes its own control FD
// (lowering to phase 0 while original broker task stays live), attempts the
// three commands again (all EPERM), and enters bounded terminal wait.
func (b *authorityBroker) selftestRevokeAndClose(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.revokeAndCloseDone || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-broker-kill" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "nonce") {
		return nil, errControlFrame
	}
	nonce, ok := body["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return nil, errControlFrame
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"started": true})
	if err != nil {
		return nil, err
	}
	if err = p.commitReply(requestID, raw, ack, nil); err != nil {
		return nil, err
	}
	b.revokeAndCloseDone = true
	b.pendingRevokeAndClose = true
	return ack, nil
}

func (b *authorityBroker) executeRevokeAndCloseSequence() error {
	runtime.LockOSThread()
	if b == nil || b.authority == nil || unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	if _, err := b.authority.guard.watchProbe(b.authority.brokerCookie, 2); err != nil {
		return err
	}
	if err := b.authority.guard.revoke(); err != nil {
		return err
	}
	mapAttr := make([]byte, 72)
	guardPut32(mapAttr, 0, unix.BPF_MAP_TYPE_ARRAY)
	guardPut32(mapAttr, 4, 4)
	guardPut32(mapAttr, 8, 4)
	guardPut32(mapAttr, 12, 1)
	copy(mapAttr[28:40], "feature_test")

	progBytes, err := probeProgram()
	if err != nil {
		return err
	}
	lic := []byte{'G', 'P', 'L', 0}
	progAttr := make([]byte, 72)
	guardPut32(progAttr, 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
	guardPut32(progAttr, 4, uint32(len(progBytes)/8))
	guardPut64(progAttr, 8, uint64(uintptr(unsafe.Pointer(&progBytes[0]))))
	guardPut64(progAttr, 16, uint64(uintptr(unsafe.Pointer(&lic[0]))))

	attachAttr := make([]byte, 16)
	guardPut32(attachAttr, 0, uint32(b.devNullFD))
	guardPut32(attachAttr, 4, uint32(b.authority.programs["g2_bpf"].fd))
	guardPut32(attachAttr, 8, unix.BPF_CGROUP_DEVICE)

	tryDeniedThree := func() error {
		for _, call := range []struct {
			cmd  uintptr
			attr []byte
		}{
			{unix.BPF_MAP_CREATE, mapAttr},
			{unix.BPF_PROG_LOAD, progAttr},
			{unix.BPF_PROG_ATTACH, attachAttr},
		} {
			r, _, errno := unix.Syscall(unix.SYS_BPF, call.cmd, uintptr(unsafe.Pointer(&call.attr[0])), uintptr(len(call.attr)))
			runtime.KeepAlive(call.attr)
			runtime.KeepAlive(progBytes)
			runtime.KeepAlive(lic)
			if errno != unix.EPERM {
				if errno == 0 && r <= math.MaxInt32 {
					_ = unix.Close(int(r))
				}
				return errGuardProtocol
			}
		}
		return nil
	}

	if err := tryDeniedThree(); err != nil {
		return err
	}

	// Prove narrow cleanup lookup still works in phase 2 using guard_liveness map
	livenessMap := b.authority.maps["guard_liveness"]
	var key uint32 = 0
	var val uint64 = 0
	lookupAttr := make([]byte, 32)
	guardPut32(lookupAttr, 0, uint32(livenessMap.fd))
	guardPut64(lookupAttr, 8, uint64(uintptr(unsafe.Pointer(&key))))
	guardPut64(lookupAttr, 16, uint64(uintptr(unsafe.Pointer(&val))))
	_, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_LOOKUP_ELEM, uintptr(unsafe.Pointer(&lookupAttr[0])), uintptr(len(lookupAttr)))
	runtime.KeepAlive(lookupAttr)
	runtime.KeepAlive(key)
	runtime.KeepAlive(val)
	if errno != 0 || val != 2 {
		return errGuardProtocol
	}

	// Now close control FD -> native lowers to phase 0 while original broker task stays live
	if err := unix.Close(b.authority.guard.fd); err != nil {
		return err
	}
	b.authority.guard.fd = -1

	// Try the three commands again at phase 0 (and also lookup must now be EPERM!)
	if err := tryDeniedThree(); err != nil {
		return err
	}
	_, _, errno = unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_LOOKUP_ELEM, uintptr(unsafe.Pointer(&lookupAttr[0])), uintptr(len(lookupAttr)))
	runtime.KeepAlive(lookupAttr)
	if errno != unix.EPERM {
		return errGuardProtocol
	}

	// Bounded terminal wait for REALM_ROOT to SIGKILL this broker after observer sees phase 0
	deadline, err := guestDeadline(30 * time.Second)
	if err != nil {
		return err
	}
	for time.Until(deadline) > 0 {
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// selftestObservationFault implements SELFTEST_OBSERVATION_FAULT.
func (b *authorityBroker) selftestObservationFault(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.selftestFaultVariant != "" || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "variant") {
		return nil, errControlFrame
	}
	variant, ok := body["variant"].(string)
	if !ok {
		return nil, errControlFrame
	}
	declaredVariant, _ := b.caseFacts["bpf_variant"].(string)
	if declaredVariant != variant {
		return nil, errControlFrame
	}
	switch variant {
	case "guard-miss":
		if err := b.authority.guard.selftestObservationFault(1); err != nil {
			return nil, err
		}
		b.checkpoint.markFault()
	case "guard-pairing":
		if err := b.authority.guard.selftestObservationFault(2); err != nil {
			return nil, err
		}
		b.checkpoint.markFault()
	case "guard-overflow":
		if err := b.authority.guard.selftestObservationFault(3); err != nil {
			return nil, err
		}
	case "snapshot-missing", "snapshot-bad-field", "audit-loss", "audit-duplicate", "audit-overflow":
		if err := b.triggerObservationFaultStimulus(variant); err != nil {
			return nil, err
		}
	default:
		return nil, errControlFrame
	}
	b.selftestFaultVariant = variant
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"injected": true})
	if err != nil {
		return nil, err
	}
	if err = p.commitReply(requestID, raw, ack, nil); err != nil {
		return nil, err
	}
	return ack, nil
}
