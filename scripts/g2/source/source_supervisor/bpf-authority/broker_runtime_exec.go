//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// brokerExecAdmission holds the complete lifecycle and non-secret policy of one
// admitted container exec from reservation through actual exec branch exit.
type brokerExecAdmission struct {
	id, grantID        uint64
	ownerID            uint64
	observation        string
	state              string // capturing -> admitted -> bound -> settled (or admitted -> cancelled)
	consumed, settled  bool
	bootID             string
	daemonCookie       uint64
	grantGen           uint64
	endpointGen        uint64
	deadlineNS         uint64
	secret             *postgresServiceSecretRecord
	endpoint           *appEndpointFact
	appPIDFD           int
	dockerExecID       string
	processSHA256      string
	wrapperTaskCookie  uint64
	taskCookie         uint64
	execTaskPID        uint32
	execTaskStartNS    uint64
	execTaskPIDFD      int
	livePIDFDHeld      bool
	exeFile            *os.File
	exeKey             executableKey
	exeDigest          string
	checkInFlight      bool
	checkResult        chan execSettlementCheckResult
	dockerExitObserved bool
}

type execSettlementCheckResult struct {
	running  bool
	exitCode int64
	pid      uint32
	err      error
}

type admitExecIOResult struct {
	endpoint  *appEndpointFact
	appPIDFD  int
	exeFile   *os.File
	exeKey    executableKey
	exeDigest string
	err       error
}

type pendingExecOperation struct {
	op           string // ADMIT_EXEC, TRACK_EXEC, CANCEL_EXEC, END_ENDPOINT_TRANSITION
	principal    *controlPrincipal
	grant        *brokerGrant
	exec         *brokerExecAdmission
	transition   *endpointTransitionState
	requestID    uint64
	raw          []byte
	dockerExecID string
	deadline     time.Time
	eventFD      int
	admitResult  chan admitExecIOResult
	trackErr     chan error
	cancelState  chan struct {
		state string
		err   error
	}
	endResult chan struct {
		endpoint *appEndpointFact
		appPIDFD int
		err      error
	}
}

type endpointTransitionState struct {
	id                uint64
	grantID           uint64
	ownerID           uint64
	recoveryID        string
	coordinatorCookie uint64
	coordinatorPID    uint32
	coordinatorStart  uint64
	coordinatorPIDFD  int
	scopeDirFD        int
	scopeSHA256       string
	endpointGen       uint64
	state             string // begun, completed, closed
	endpoint          *appEndpointFact
}

func (e *brokerExecAdmission) closeHeld() {
	if e == nil {
		return
	}
	if e.appPIDFD >= 0 {
		_ = unix.Close(e.appPIDFD)
		e.appPIDFD = -1
	}
	if e.execTaskPIDFD >= 0 {
		_ = unix.Close(e.execTaskPIDFD)
		e.execTaskPIDFD = -1
	}
	if e.exeFile != nil {
		_ = e.exeFile.Close()
		e.exeFile = nil
	}
}

func (e *brokerExecAdmission) body() map[string]any {
	var epGen, dockerID, taskCookie, procSHA, epVal any
	if e.observation != "pg_controldata" && e.endpointGen != 0 {
		epGen = jsonU64(e.endpointGen)
	}
	if e.dockerExecID != "" && (e.state == "admitted" || e.state == "bound" || e.state == "settled") {
		dockerID = e.dockerExecID
	}
	if e.taskCookie != 0 && (e.state == "bound" || e.state == "settled") {
		taskCookie = jsonU64(e.taskCookie)
	}
	if e.processSHA256 != "" && (e.state == "bound" || e.state == "settled") {
		procSHA = e.processSHA256
	}
	if e.endpoint != nil && (e.state == "admitted" || e.state == "bound" || e.state == "settled") {
		epVal = e.endpoint.projection()
	}
	return map[string]any{
		"exec_id":             jsonU64(e.id),
		"grant_id":            jsonU64(e.grantID),
		"observation":         e.observation,
		"endpoint_generation": epGen,
		"docker_exec_id":      dockerID,
		"task_cookie":         taskCookie,
		"process_sha256":      procSHA,
		"state":               e.state,
		"endpoint":            epVal,
	}
}

func (b *authorityBroker) emitExecAdmission(e *brokerExecAdmission) error {
	if b == nil || e == nil || e.id == 0 || e.grantID == 0 || e.state == "" {
		return errGuardProtocol
	}
	return appendRuntimeRecord(b, "exec_admission", e.body())
}

func (t *endpointTransitionState) closeHeld() {
	if t == nil {
		return
	}
	if t.coordinatorPIDFD >= 0 {
		_ = unix.Close(t.coordinatorPIDFD)
		t.coordinatorPIDFD = -1
	}
	if t.scopeDirFD >= 0 {
		_ = unix.Close(t.scopeDirFD)
		t.scopeDirFD = -1
	}
}

func (t *endpointTransitionState) body() map[string]any {
	var epGen, epVal any
	if t.state == "completed" && t.endpointGen != 0 {
		epGen = jsonU64(t.endpointGen)
	}
	if t.state == "completed" && t.endpoint != nil {
		epVal = t.endpoint.projection()
	}
	return map[string]any{
		"transition_id":       jsonU64(t.id),
		"grant_id":            jsonU64(t.grantID),
		"recovery_id":         t.recoveryID,
		"coordinator_cookie":  jsonU64(t.coordinatorCookie),
		"scope_sha256":        t.scopeSHA256,
		"endpoint_generation": epGen,
		"state":               t.state,
		"endpoint":            epVal,
	}
}

func (b *authorityBroker) emitEndpointTransition(t *endpointTransitionState) error {
	if b == nil || t == nil || t.id == 0 || t.grantID == 0 || t.state == "" {
		return errGuardProtocol
	}
	return appendRuntimeRecord(b, "endpoint_transition", t.body())
}

func (b *authorityBroker) startAdmitExec(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || !allowedOperation(p.Role, "ADMIT_EXEC") || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "grant_id", "observation") {
		return errControlFrame
	}
	grantID, ok := exactNumber(body["grant_id"])
	if !ok || grantID == 0 {
		return errControlFrame
	}
	observation, ok := body["observation"].(string)
	if !ok || !allowedObservation(p.Role, observation) {
		return errControlFrame
	}
	grant := b.grants[grantID]
	if grant == nil || grant.owner != p.ID || grant.phase != "active" || !grant.startObserved || !grant.actualQueryConfirmed || grant.facts == nil || !grant.facts.runtimeReady {
		return errGuardProtocol
	}
	if grant.exec != nil && !grant.exec.settled && grant.exec.state != "cancelled" && grant.exec.state != "settled" {
		return errGuardProtocol
	}
	var secret *postgresServiceSecretRecord
	if p.Role == "RECOVERY_OBSERVER" {
		if observation != "pg_controldata" || b.recovery == nil || !strings.HasPrefix(grant.purpose, "recovery-") {
			return errGuardProtocol
		}
	} else {
		if grant.purpose != "postgres-live-app" || b.pendingEndpoint != nil || b.endpointClosed {
			return errGuardProtocol
		}
		if b.serviceSecrets == nil {
			return errGuardProtocol
		}
		secret = b.serviceSecrets["app"]
		if secret == nil || !secret.handedOver || secret.liveGrantID != grant.id || secret.liveGeneration != grant.generation || secret.passwordSHA256 == "" {
			return errGuardProtocol
		}
		if recheckHeldPGDataDir(secret) != nil {
			return errGuardProtocol
		}
	}
	owner := p.ID
	if p.Role == "REALM_ROOT" {
		owner = b.bootOwner
	}
	daemon := b.daemons[owner]
	if daemon == nil || b.observeDaemonExec(daemon) != nil {
		return errGuardProtocol
	}
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return err
	}
	now, err := monoNowNS()
	if err != nil {
		return err
	}
	deadlineNS := now + uint64((60 * time.Second).Nanoseconds())
	if deadlineNS > authorityGuestDeadlineNS {
		deadlineNS = authorityGuestDeadlineNS
	}
	if b.nextExec == 0 {
		b.nextExec = 1
	}
	if b.nextExec == math.MaxUint64 {
		return errGuardProtocol
	}
	if b.endpointGeneration == 0 {
		b.endpointGeneration = 1
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
	id := b.nextExec
	b.nextExec++
	adm := &brokerExecAdmission{
		id:            id,
		grantID:       grant.id,
		ownerID:       p.ID,
		observation:   observation,
		state:         "capturing",
		bootID:        b.bootID,
		daemonCookie:  daemon.cookie,
		grantGen:      grant.generation,
		endpointGen:   b.endpointGeneration,
		deadlineNS:    deadlineNS,
		secret:        secret,
		appPIDFD:      -1,
		execTaskPIDFD: -1,
	}
	grant.exec = adm
	if b.execAdmissions == nil {
		b.execAdmissions = make(map[uint64]*brokerExecAdmission)
	}
	b.execAdmissions[id] = adm
	wantBin := "/usr/lib/postgresql/16/bin/pg_controldata"
	switch observation {
	case "negative_pg_dump", "app_rollback_dump":
		wantBin = "/usr/lib/postgresql/16/bin/pg_dump"
	case "app_rollback_restore":
		wantBin = "/usr/lib/postgresql/16/bin/pg_restore"
	}
	daemonCopy := *daemon
	grantCopy := *grant
	rootfs := grant.facts.rootfs
	admittedRID := b.admittedRecoveryID
	op := &pendingExecOperation{
		op:          "ADMIT_EXEC",
		principal:   p,
		grant:       grant,
		exec:        adm,
		requestID:   requestID,
		raw:         append([]byte(nil), raw...),
		deadline:    deadline,
		eventFD:     eventFD,
		admitResult: make(chan admitExecIOResult, 1),
	}
	b.pendingExecOp = op
	go func() {
		var res admitExecIOResult
		res.appPIDFD = -1
		res.exeFile, res.exeKey, res.exeDigest, res.err = openContainerExecBinary(rootfs, wantBin)
		if res.err == nil && observation != "pg_controldata" {
			res.endpoint, res.appPIDFD, res.err = observeLiveAppEndpoint(&daemonCopy, &grantCopy, admittedRID, deadline)
		}
		op.admitResult <- res
		var wake [8]byte
		binary.LittleEndian.PutUint64(wake[:], 1)
		_, _ = unix.Write(workerFD, wake[:])
		_ = unix.Close(workerFD)
	}()
	return nil
}

func (b *authorityBroker) finishAdmitExec(q *pendingExecOperation) (*controlPrincipal, []byte, bool, error) {
	var res admitExecIOResult
	select {
	case res = <-q.admitResult:
	default:
		return q.principal, nil, false, nil
	}
	_ = unix.Close(q.eventFD)
	b.pendingExecOp = nil
	adm := q.exec
	grant := q.grant
	cleanupFail := func(e error) (*controlPrincipal, []byte, bool, error) {
		if res.appPIDFD >= 0 {
			_ = unix.Close(res.appPIDFD)
		}
		if res.exeFile != nil {
			_ = res.exeFile.Close()
		}
		adm.closeHeld()
		if grant.exec == adm {
			grant.exec = nil
		}
		delete(b.execAdmissions, adm.id)
		return q.principal, nil, true, e
	}
	if res.err != nil {
		return cleanupFail(res.err)
	}
	now, err := monoNowNS()
	if err != nil || now >= adm.deadlineNS || time.Until(q.deadline) <= 0 || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || q.principal.Terminal || q.principal.Transferred || q.principal.NextRequest != q.requestID || !livePIDFD(q.principal.PIDFD) || grant.phase != "active" || grant.generation != adm.grantGen || grant.exec != adm || adm.state != "capturing" {
		return cleanupFail(errGuardProtocol)
	}
	owner := q.principal.ID
	if q.principal.Role == "REALM_ROOT" {
		owner = b.bootOwner
	}
	daemon := b.daemons[owner]
	if daemon == nil || daemon.cookie != adm.daemonCookie || !livePIDFD(daemon.pidfd) || b.observeDaemonExec(daemon) != nil {
		return cleanupFail(errGuardProtocol)
	}
	if adm.observation != "pg_controldata" {
		if b.pendingEndpoint != nil || b.endpointClosed || adm.endpointGen != b.endpointGeneration || res.endpoint == nil || res.appPIDFD < 0 || !pidfdSelects(res.appPIDFD, res.endpoint.pid) {
			return cleanupFail(errGuardProtocol)
		}
		if adm.secret == nil || recheckHeldPGDataDir(adm.secret) != nil {
			return cleanupFail(errGuardProtocol)
		}
	}
	if b.checkpoint.lifecycle != nil && b.checkpoint.lifecycle.lineage != nil {
		l := b.checkpoint.lifecycle.lineage
		if l.executables == nil {
			l.executables = make(map[executableKey]lineageExecutable)
		}
		if prior, exists := l.executables[res.exeKey]; !exists {
			dupFD, e := unix.FcntlInt(res.exeFile.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			if e != nil {
				return cleanupFail(e)
			}
			l.executables[res.exeKey] = lineageExecutable{role: "exec_target", digest: res.exeDigest, file: os.NewFile(uintptr(dupFD), "exec-target")}
		} else if prior.digest != res.exeDigest {
			return cleanupFail(errGuardProtocol)
		}
	}
	adm.endpoint = res.endpoint
	adm.appPIDFD = res.appPIDFD
	adm.exeFile = res.exeFile
	adm.exeKey = res.exeKey
	adm.exeDigest = res.exeDigest
	adm.state = "admitted"
	if err := b.emitExecAdmission(adm); err != nil {
		return cleanupFail(err)
	}
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"exec_id": jsonU64(adm.id)})
	if err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	if err = q.principal.commitReply(q.requestID, q.raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	return q.principal, ack, true, nil
}

func inspectDockerExecBeforeStart(daemon *daemonRegistration, dockerExecID string, grant *brokerGrant, adm *brokerExecAdmission, deadline time.Time) error {
	if daemon == nil || !runtimeContainerID(dockerExecID) || grant == nil || grant.facts == nil || adm == nil {
		return errGuardProtocol
	}
	info, err := recoveryDockerGET(daemon, "/v1.41/exec/"+dockerExecID+"/json", deadline)
	if err != nil || info["ID"] != dockerExecID || info["ContainerID"] != grant.containerID || info["Running"] != false || info["Pid"] != json.Number("0") {
		return errGuardProtocol
	}
	if info["ExitCode"] != nil && info["ExitCode"] != json.Number("0") {
		return errGuardProtocol
	}
	pcfg, ok := info["ProcessConfig"].(map[string]any)
	if !ok || pcfg["tty"] != false || pcfg["privileged"] != false {
		return errGuardProtocol
	}
	userStr, _ := pcfg["user"].(string)
	if userStr != grant.facts.user {
		return errGuardProtocol
	}
	epStr, _ := pcfg["entrypoint"].(string)
	rawArgs, ok := pcfg["arguments"].([]any)
	if !ok || epStr == "" {
		return errGuardProtocol
	}
	gotArgs := make([]string, 0, 1+len(rawArgs))
	gotArgs = append(gotArgs, epStr)
	for _, a := range rawArgs {
		s, ok := a.(string)
		if !ok {
			return errGuardProtocol
		}
		gotArgs = append(gotArgs, s)
	}
	var appIP string
	if adm.endpoint != nil {
		appIP = adm.endpoint.ipv4
	}
	wantArgs, _, err := expectedExecArgs(adm.observation, appIP)
	if err != nil || !reflect.DeepEqual(gotArgs, wantArgs) {
		return errGuardProtocol
	}
	return nil
}

func (b *authorityBroker) startTrackExec(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || !allowedOperation(p.Role, "TRACK_EXEC") || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "exec_id", "docker_exec_id") {
		return errControlFrame
	}
	execID, ok := exactNumber(body["exec_id"])
	if !ok || execID == 0 {
		return errControlFrame
	}
	dockerExecID, ok := body["docker_exec_id"].(string)
	if !ok || !runtimeContainerID(dockerExecID) {
		return errControlFrame
	}
	adm := b.execAdmissions[execID]
	if adm == nil || adm.ownerID != p.ID || adm.state != "admitted" || adm.consumed || adm.settled || adm.dockerExecID != "" {
		return errGuardProtocol
	}
	for _, other := range b.execAdmissions {
		if other != nil && other.dockerExecID == dockerExecID {
			return errGuardProtocol
		}
	}
	grant := b.grants[adm.grantID]
	if grant == nil || grant.owner != p.ID || grant.phase != "active" || grant.generation != adm.grantGen || grant.exec != adm {
		return errGuardProtocol
	}
	if adm.observation != "pg_controldata" && (b.pendingEndpoint != nil || b.endpointClosed || adm.endpointGen != b.endpointGeneration) {
		return errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil || now >= adm.deadlineNS {
		return errGuardProtocol
	}
	owner := p.ID
	if p.Role == "REALM_ROOT" {
		owner = b.bootOwner
	}
	daemon := b.daemons[owner]
	if daemon == nil || daemon.cookie != adm.daemonCookie || b.observeDaemonExec(daemon) != nil {
		return errGuardProtocol
	}
	deadline, err := guestDeadline(30 * time.Second)
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
	daemonCopy := *daemon
	grantCopy := *grant
	admCopy := *adm
	op := &pendingExecOperation{
		op:           "TRACK_EXEC",
		principal:    p,
		grant:        grant,
		exec:         adm,
		requestID:    requestID,
		raw:          append([]byte(nil), raw...),
		dockerExecID: dockerExecID,
		deadline:     deadline,
		eventFD:      eventFD,
		trackErr:     make(chan error, 1),
	}
	b.pendingExecOp = op
	go func() {
		op.trackErr <- inspectDockerExecBeforeStart(&daemonCopy, dockerExecID, &grantCopy, &admCopy, deadline)
		var wake [8]byte
		binary.LittleEndian.PutUint64(wake[:], 1)
		_, _ = unix.Write(workerFD, wake[:])
		_ = unix.Close(workerFD)
	}()
	return nil
}

func (b *authorityBroker) finishTrackExec(q *pendingExecOperation) (*controlPrincipal, []byte, bool, error) {
	var trackErr error
	select {
	case trackErr = <-q.trackErr:
	default:
		return q.principal, nil, false, nil
	}
	_ = unix.Close(q.eventFD)
	b.pendingExecOp = nil
	adm := q.exec
	grant := q.grant
	if trackErr != nil {
		return q.principal, nil, true, trackErr
	}
	now, err := monoNowNS()
	if err != nil || now >= adm.deadlineNS || time.Until(q.deadline) <= 0 || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || q.principal.Terminal || q.principal.Transferred || q.principal.NextRequest != q.requestID || !livePIDFD(q.principal.PIDFD) || grant.phase != "active" || grant.generation != adm.grantGen || grant.exec != adm || adm.state != "admitted" || adm.dockerExecID != "" {
		return q.principal, nil, true, errGuardProtocol
	}
	if adm.observation != "pg_controldata" && (b.pendingEndpoint != nil || b.endpointClosed || adm.endpointGen != b.endpointGeneration) {
		return q.principal, nil, true, errGuardProtocol
	}
	adm.dockerExecID = q.dockerExecID
	if err := b.emitExecAdmission(adm); err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"tracked": true})
	if err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	if err = q.principal.commitReply(q.requestID, q.raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	return q.principal, ack, true, nil
}

func (b *authorityBroker) startCancelExec(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, bool, error) {
	if b == nil || p == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || !allowedOperation(p.Role, "CANCEL_EXEC") || !livePIDFD(p.PIDFD) {
		return nil, false, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, false, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "exec_id") {
		return nil, false, errControlFrame
	}
	execID, ok := exactNumber(body["exec_id"])
	if !ok || execID == 0 {
		return nil, false, errControlFrame
	}
	adm := b.execAdmissions[execID]
	if adm == nil || adm.ownerID != p.ID {
		return nil, false, errGuardProtocol
	}
	grant := b.grants[adm.grantID]
	if grant == nil || grant.owner != p.ID {
		return nil, false, errGuardProtocol
	}
	switch adm.state {
	case "cancelled", "settled":
		ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"state": adm.state})
		if err != nil {
			return nil, false, err
		}
		if err = p.commitReply(requestID, raw, ack, nil); err != nil {
			return nil, false, err
		}
		return ack, false, nil
	case "bound":
		// Already bound: cannot cancel by request; caller receives "settling" until actual branch settles.
		ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"state": "settling"})
		if err != nil {
			return nil, false, err
		}
		if err = p.commitReply(requestID, raw, ack, nil); err != nil {
			return nil, false, err
		}
		return ack, false, nil
	case "admitted":
		if adm.dockerExecID == "" {
			adm.state = "cancelled"
			adm.settled = true
			adm.closeHeld()
			if err := b.emitExecAdmission(adm); err != nil {
				b.checkpoint.markFault()
				return nil, false, err
			}
			ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"state": "cancelled"})
			if err != nil {
				return nil, false, err
			}
			if err = p.commitReply(requestID, raw, ack, nil); err != nil {
				return nil, false, err
			}
			return ack, false, nil
		}
		// Tracked Docker exec object exists: verify in worker that it is not running before cancelling.
		owner := p.ID
		if p.Role == "REALM_ROOT" {
			owner = b.bootOwner
		}
		daemon := b.daemons[owner]
		if daemon == nil || daemon.cookie != adm.daemonCookie || b.observeDaemonExec(daemon) != nil {
			return nil, false, errGuardProtocol
		}
		deadline, err := guestDeadline(15 * time.Second)
		if err != nil {
			return nil, false, err
		}
		eventFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
		if err != nil {
			return nil, false, err
		}
		workerFD, err := unix.FcntlInt(uintptr(eventFD), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			_ = unix.Close(eventFD)
			return nil, false, err
		}
		daemonCopy := *daemon
		grantCopy := *grant
		admCopy := *adm
		dockerID := adm.dockerExecID
		op := &pendingExecOperation{
			op:        "CANCEL_EXEC",
			principal: p,
			grant:     grant,
			exec:      adm,
			requestID: requestID,
			raw:       append([]byte(nil), raw...),
			deadline:  deadline,
			eventFD:   eventFD,
			cancelState: make(chan struct {
				state string
				err   error
			}, 1),
		}
		b.pendingExecOp = op
		go func() {
			e := inspectDockerExecBeforeStart(&daemonCopy, dockerID, &grantCopy, &admCopy, deadline)
			st := "cancelled"
			if e != nil {
				st = "settling"
				e = nil
			}
			op.cancelState <- struct {
				state string
				err   error
			}{state: st, err: e}
			var wake [8]byte
			binary.LittleEndian.PutUint64(wake[:], 1)
			_, _ = unix.Write(workerFD, wake[:])
			_ = unix.Close(workerFD)
		}()
		return nil, true, nil
	default:
		return nil, false, errGuardProtocol
	}
}

func (b *authorityBroker) finishCancelExec(q *pendingExecOperation) (*controlPrincipal, []byte, bool, error) {
	var res struct {
		state string
		err   error
	}
	select {
	case res = <-q.cancelState:
	default:
		return q.principal, nil, false, nil
	}
	_ = unix.Close(q.eventFD)
	b.pendingExecOp = nil
	adm := q.exec
	if res.err != nil {
		return q.principal, nil, true, res.err
	}
	if b.checkpoint.invalid || q.principal.Terminal || q.principal.Transferred || q.principal.NextRequest != q.requestID || !livePIDFD(q.principal.PIDFD) {
		return q.principal, nil, true, errGuardProtocol
	}
	outState := res.state
	if adm.state == "bound" {
		outState = "settling"
	} else if adm.state == "settled" || adm.state == "cancelled" {
		outState = adm.state
	} else if outState == "cancelled" && adm.state == "admitted" && !adm.consumed {
		adm.state = "cancelled"
		adm.settled = true
		adm.closeHeld()
		if err := b.emitExecAdmission(adm); err != nil {
			b.checkpoint.markFault()
			return q.principal, nil, true, err
		}
	} else {
		outState = "settling"
	}
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"state": outState})
	if err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	if err = q.principal.commitReply(q.requestID, q.raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	return q.principal, ack, true, nil
}

func verifyMigrationScopeStoppedCoordinator(scopeDirFD int, coordPID uint32) (string, error) {
	var st, named unix.Stat_t
	var fs unix.Statfs_t
	if scopeDirFD < 0 || unix.Fstat(scopeDirFD, &st) != nil || unix.Fstatfs(scopeDirFD, &fs) != nil || int64(fs.Type) != unix.CGROUP2_SUPER_MAGIC || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
		return "", errGuardProtocol
	}
	if unix.Lstat("/sys/fs/cgroup/g2-c81-migration", &named) != nil || !sameTrustedStat(st, named) {
		return "", errGuardProtocol
	}
	procsRaw, _, err := readAtRegular(scopeDirFD, "cgroup.procs", 65536)
	if err != nil {
		return "", errGuardProtocol
	}
	threadsRaw, _, err := readAtRegular(scopeDirFD, "cgroup.threads", 65536)
	if err != nil {
		return "", errGuardProtocol
	}
	procs := strings.Fields(string(procsRaw))
	threads := strings.Fields(string(threadsRaw))
	if coordPID == 0 {
		if len(procs) != 0 || len(threads) != 0 {
			return "", errGuardProtocol
		}
		eventsRaw, _, err := readAtRegular(scopeDirFD, "cgroup.events", 65536)
		if err != nil || !strings.Contains(string(eventsRaw), "populated 0") {
			return "", errGuardProtocol
		}
	} else {
		want := fmt.Sprint(coordPID)
		if len(procs) != 1 || procs[0] != want {
			return "", errGuardProtocol
		}
	}
	return hashBytes([]byte("/sys/fs/cgroup/g2-c81-migration\n")), nil
}

// beginEndpointTransition authenticates the stopped UID 10002 admin coordinator
// before PTRACE_DETACH/resume, atomically cancels any untracked exec admission,
// rejects with BPF_AUTH_PENDING if an exec is tracked or bound, and invalidates
// the previous endpoint generation before returning OK.
func (b *authorityBroker) beginEndpointTransition(p *controlPrincipal, request map[string]any, raw []byte, rights []int) ([]byte, bool, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || len(rights) != 2 || !livePIDFD(p.PIDFD) {
		return nil, false, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, false, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "grant_id", "recovery_id", "coordinator_pid", "coordinator_starttime") {
		return nil, false, errControlFrame
	}
	grantID, ok := exactNumber(body["grant_id"])
	if !ok || grantID == 0 {
		return nil, false, errControlFrame
	}
	rid, ok := body["recovery_id"].(string)
	if !ok || !validRecoveryID(rid) {
		return nil, false, errControlFrame
	}
	coordPIDNum, ok := exactNumber(body["coordinator_pid"])
	if !ok || coordPIDNum <= 1 || coordPIDNum > math.MaxInt32 || !pidfdSelects(rights[0], uint32(coordPIDNum)) {
		return nil, false, errControlFrame
	}
	coordStart, ok := exactNumber(body["coordinator_starttime"])
	if !ok || coordStart == 0 {
		return nil, false, errControlFrame
	}
	if b.endpointClosed {
		return nil, false, errGuardProtocol
	}
	if b.pendingEndpoint != nil {
		ack, err := controlACK(b.reads, p, requestID, raw, "BPF_AUTH_PENDING", map[string]any{"code": "BPF_AUTH_PENDING", "sequence": jsonU64(uint64(len(b.transcript.entries)))})
		if err != nil {
			return nil, false, err
		}
		if err = p.commitReply(requestID, raw, ack, rights); err != nil {
			return nil, false, err
		}
		return ack, true, nil
	}
	grant := b.grants[grantID]
	if grant == nil || grant.owner != p.ID || grant.purpose != "postgres-live-app" || grant.phase != "active" || !grant.startObserved || !grant.actualQueryConfirmed {
		return nil, false, errGuardProtocol
	}
	if b.admittedRecoveryID != "" && b.admittedRecoveryID != rid {
		return nil, false, errGuardProtocol
	}
	coordPID := uint32(coordPIDNum)
	coordProc, err := observeProcess(coordPID, rights[0])
	if err != nil || coordProc.PPID != p.PID || coordProc.StartTicks != coordStart || coordProc.UID != 10002 || coordProc.GID != 10002 || coordProc.State != 'T' && coordProc.State != 't' || coordProc.Tracer != p.PID {
		return nil, false, errGuardProtocol
	}
	adminFile, adminSHA, err := openTrustedRegular("/opt/g2/houfeng-record-platform-admin", 128<<20)
	if err != nil {
		return nil, false, errGuardProtocol
	}
	_ = adminFile.Close()
	if coordProc.ExecutableSHA256 != adminSHA || verifyProcessExecutable(coordPID, "/opt/g2/houfeng-record-platform-admin", adminSHA) != nil {
		return nil, false, errGuardProtocol
	}
	scopeSHA, err := verifyMigrationScopeStoppedCoordinator(rights[1], coordPID)
	if err != nil {
		// Non-scoped commands (e.g. preview / retire / c80-service) run in child_cgroup while migration_cgroup is empty.
		scopeSHA, err = verifyMigrationScopeStoppedCoordinator(rights[1], 0)
		if err != nil {
			return nil, false, errGuardProtocol
		}
	}
	if adm := grant.exec; adm != nil && !adm.settled && adm.state != "cancelled" && adm.state != "settled" {
		if adm.state == "admitted" && adm.dockerExecID == "" && !adm.consumed {
			adm.state = "cancelled"
			adm.settled = true
			adm.closeHeld()
			if err := b.emitExecAdmission(adm); err != nil {
				b.checkpoint.markFault()
				return nil, false, err
			}
		} else {
			ack, err := controlACK(b.reads, p, requestID, raw, "BPF_AUTH_PENDING", map[string]any{"code": "BPF_AUTH_PENDING", "sequence": jsonU64(uint64(len(b.transcript.entries)))})
			if err != nil {
				return nil, false, err
			}
			if err = p.commitReply(requestID, raw, ack, rights); err != nil {
				return nil, false, err
			}
			return ack, true, nil
		}
	}
	coordTask, err := b.authority.guard.pinTask(coordPID)
	if err != nil {
		return nil, false, err
	}
	keepTask := false
	defer func() {
		if !keepTask {
			_ = b.authority.guard.dropTask(coordTask.Cookie)
		}
	}()
	if coordTask.TGID != coordPID || coordTask.TID != coordPID || coordTask.StartBoottimeNS/10_000_000 != coordStart || !livePIDFD(rights[0]) {
		return nil, false, errGuardProtocol
	}
	if b.nextTransition == 0 {
		b.nextTransition = 1
	}
	if b.nextTransition == math.MaxUint64 {
		return nil, false, errGuardProtocol
	}
	tid := b.nextTransition
	b.nextTransition++
	pidfdDup, err := unix.FcntlInt(uintptr(rights[0]), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	scopeDup, err := unix.FcntlInt(uintptr(rights[1]), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(pidfdDup)
		return nil, false, err
	}
	tr := &endpointTransitionState{
		id:                tid,
		grantID:           grant.id,
		ownerID:           p.ID,
		recoveryID:        rid,
		coordinatorCookie: coordTask.Cookie,
		coordinatorPID:    coordPID,
		coordinatorStart:  coordStart,
		coordinatorPIDFD:  pidfdDup,
		scopeDirFD:        scopeDup,
		scopeSHA256:       scopeSHA,
		state:             "begun",
	}
	if err := b.emitEndpointTransition(tr); err != nil {
		tr.closeHeld()
		return nil, false, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"transition_id": jsonU64(tid)})
	if err != nil {
		tr.closeHeld()
		b.checkpoint.markFault()
		return nil, false, err
	}
	if err = p.commitReply(requestID, raw, ack, rights); err != nil {
		tr.closeHeld()
		b.checkpoint.markFault()
		return nil, false, err
	}
	b.pendingEndpoint = tr
	b.admittedRecoveryID = rid
	keepTask = true
	return ack, true, nil
}

func (b *authorityBroker) startEndEndpointTransition(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingSettlement != nil || b.pendingExecOp != nil || b.selftestFD != nil || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "transition_id") {
		return errControlFrame
	}
	tid, ok := exactNumber(body["transition_id"])
	if !ok || tid == 0 {
		return errControlFrame
	}
	tr := b.pendingEndpoint
	if tr == nil || tr.id != tid || tr.ownerID != p.ID || tr.state != "begun" || b.endpointClosed {
		return errGuardProtocol
	}
	dead, err := pidfdGroupExited(tr.coordinatorPIDFD)
	if err != nil || !dead {
		b.closeEndpointTransitionGate()
		return errGuardProtocol
	}
	if _, err := verifyMigrationScopeStoppedCoordinator(tr.scopeDirFD, 0); err != nil {
		b.closeEndpointTransitionGate()
		return errGuardProtocol
	}
	grant := b.grants[tr.grantID]
	if grant == nil || grant.owner != p.ID || grant.purpose != "postgres-live-app" || grant.phase != "active" {
		b.closeEndpointTransitionGate()
		return errGuardProtocol
	}
	owner := b.bootOwner
	daemon := b.daemons[owner]
	if daemon == nil || b.observeDaemonExec(daemon) != nil {
		b.closeEndpointTransitionGate()
		return errGuardProtocol
	}
	deadline, err := guestDeadline(30 * time.Second)
	if err != nil {
		b.closeEndpointTransitionGate()
		return err
	}
	eventFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		b.closeEndpointTransitionGate()
		return err
	}
	workerFD, err := unix.FcntlInt(uintptr(eventFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(eventFD)
		b.closeEndpointTransitionGate()
		return err
	}
	daemonCopy := *daemon
	grantCopy := *grant
	rid := tr.recoveryID
	op := &pendingExecOperation{
		op:         "END_ENDPOINT_TRANSITION",
		principal:  p,
		grant:      grant,
		transition: tr,
		requestID:  requestID,
		raw:        append([]byte(nil), raw...),
		deadline:   deadline,
		eventFD:    eventFD,
		endResult: make(chan struct {
			endpoint *appEndpointFact
			appPIDFD int
			err      error
		}, 1),
	}
	b.pendingExecOp = op
	go func() {
		ep, fd, e := observeLiveAppEndpoint(&daemonCopy, &grantCopy, rid, deadline)
		op.endResult <- struct {
			endpoint *appEndpointFact
			appPIDFD int
			err      error
		}{endpoint: ep, appPIDFD: fd, err: e}
		var wake [8]byte
		binary.LittleEndian.PutUint64(wake[:], 1)
		_, _ = unix.Write(workerFD, wake[:])
		_ = unix.Close(workerFD)
	}()
	return nil
}

func (b *authorityBroker) closeEndpointTransitionGate() {
	if b == nil {
		return
	}
	b.endpointClosed = true
	if tr := b.pendingEndpoint; tr != nil {
		if tr.state == "begun" {
			tr.state = "closed"
			_ = b.emitEndpointTransition(tr)
		}
		_ = b.authority.guard.dropTask(tr.coordinatorCookie)
		tr.closeHeld()
		b.pendingEndpoint = nil
	}
}

func (b *authorityBroker) finishEndEndpointTransition(q *pendingExecOperation) (*controlPrincipal, []byte, bool, error) {
	var res struct {
		endpoint *appEndpointFact
		appPIDFD int
		err      error
	}
	select {
	case res = <-q.endResult:
	default:
		return q.principal, nil, false, nil
	}
	_ = unix.Close(q.eventFD)
	b.pendingExecOp = nil
	if res.appPIDFD >= 0 {
		_ = unix.Close(res.appPIDFD)
	}
	tr := q.transition
	if res.err != nil || time.Until(q.deadline) <= 0 || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || q.principal.Terminal || q.principal.Transferred || q.principal.NextRequest != q.requestID || !livePIDFD(q.principal.PIDFD) || b.pendingEndpoint != tr || tr == nil || tr.state != "begun" {
		b.closeEndpointTransitionGate()
		if res.err != nil {
			return q.principal, nil, true, res.err
		}
		return q.principal, nil, true, errGuardProtocol
	}
	dead, err := pidfdGroupExited(tr.coordinatorPIDFD)
	if err != nil || !dead {
		b.closeEndpointTransitionGate()
		return q.principal, nil, true, errGuardProtocol
	}
	if _, err := verifyMigrationScopeStoppedCoordinator(tr.scopeDirFD, 0); err != nil {
		b.closeEndpointTransitionGate()
		return q.principal, nil, true, errGuardProtocol
	}
	if b.endpointGeneration == 0 {
		b.endpointGeneration = 1
	}
	if b.endpointGeneration == math.MaxUint64 {
		b.closeEndpointTransitionGate()
		return q.principal, nil, true, errGuardProtocol
	}
	b.endpointGeneration++
	tr.endpointGen = b.endpointGeneration
	tr.endpoint = res.endpoint
	tr.state = "completed"
	if err := b.emitEndpointTransition(tr); err != nil {
		b.closeEndpointTransitionGate()
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	_ = b.authority.guard.dropTask(tr.coordinatorCookie)
	tr.closeHeld()
	b.pendingEndpoint = nil
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"endpoint_generation": jsonU64(tr.endpointGen)})
	if err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	if err = q.principal.commitReply(q.requestID, q.raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, true, err
	}
	return q.principal, ack, true, nil
}

func (b *authorityBroker) finishPendingExecOp() (*controlPrincipal, []byte, bool, error) {
	if b == nil || b.pendingExecOp == nil {
		return nil, nil, false, errGuardProtocol
	}
	q := b.pendingExecOp
	switch q.op {
	case "ADMIT_EXEC":
		return b.finishAdmitExec(q)
	case "TRACK_EXEC":
		return b.finishTrackExec(q)
	case "CANCEL_EXEC":
		return b.finishCancelExec(q)
	case "END_ENDPOINT_TRANSITION":
		return b.finishEndEndpointTransition(q)
	default:
		return nil, nil, false, errGuardProtocol
	}
}

// serviceActiveExecAdmissions advances bound exec admissions by locating the
// actual exec task in guard lineage, holding its PIDFD when live, verifying all
// branch descendants have exited, and confirming Docker ExecInspect Running=false.
func (b *authorityBroker) serviceActiveExecAdmissions(lineage *daemonLineage) error {
	if b == nil || lineage == nil {
		return errGuardProtocol
	}
	for _, adm := range b.execAdmissions {
		if adm == nil || adm.settled || adm.state != "bound" {
			continue
		}
		grant := b.grants[adm.grantID]
		if grant == nil || adm.wrapperTaskCookie == 0 {
			b.checkpoint.markFault()
			return errGuardProtocol
		}
		// Locate the descendant node under wrapperTaskCookie that executed adm.exeKey ("exec_target")
		if adm.taskCookie == 0 {
			var matchNode *lineageNode
			for _, n := range lineage.nodes {
				if n == nil || n.cookie == adm.wrapperTaskCookie {
					continue
				}
				cur := n
				underWrapper := false
				for depth := 0; depth < 256 && cur != nil; depth++ {
					if cur.cookie == adm.wrapperTaskCookie {
						underWrapper = true
						break
					}
					if cur.cookie == lineage.root {
						break
					}
					cur = lineage.nodes[cur.parent]
				}
				if !underWrapper {
					continue
				}
				if n.executable == adm.exeKey && n.role == "exec_target" && !n.tainted {
					if matchNode != nil && matchNode.cookie != n.cookie {
						b.checkpoint.markFault()
						return errGuardProtocol
					}
					matchNode = n
				}
			}
			if matchNode != nil {
				adm.taskCookie = matchNode.cookie
				adm.execTaskPID = matchNode.tgid
				adm.execTaskStartNS = matchNode.startNS
				if !matchNode.exited {
					fd, e := unix.PidfdOpen(int(matchNode.tgid), 0)
					if e == nil {
						if pidfdSelects(fd, matchNode.tgid) {
							adm.execTaskPIDFD = fd
							adm.livePIDFDHeld = true
						} else {
							_ = unix.Close(fd)
						}
					}
				} else if adm.observation != "pg_controldata" {
					// Only pg_controldata may use the narrow fast-exit branch without a held live PIDFD.
					b.checkpoint.markFault()
					return errGuardProtocol
				}
				if err := b.emitExecAdmission(adm); err != nil {
					b.checkpoint.markFault()
					return err
				}
			}
		}
		if adm.taskCookie == 0 {
			continue
		}
		// Check if wrapper invocation and all lineage descendants of wrapperTaskCookie have exited
		allExited := true
		for _, n := range lineage.nodes {
			if n == nil || n.exited {
				continue
			}
			cur := n
			for depth := 0; depth < 256 && cur != nil; depth++ {
				if cur.cookie == adm.wrapperTaskCookie {
					allExited = false
					break
				}
				if cur.cookie == lineage.root {
					break
				}
				cur = lineage.nodes[cur.parent]
			}
			if !allExited {
				break
			}
		}
		if !allExited {
			continue
		}
		if adm.livePIDFDHeld && adm.execTaskPIDFD >= 0 {
			dead, err := pidfdGroupExited(adm.execTaskPIDFD)
			if err != nil {
				b.checkpoint.markFault()
				return err
			}
			if !dead {
				continue
			}
		}
		// Check that wrapper runtimeInvocation has settled and listener is retired
		wrapperSettled := false
		for _, inv := range b.runtimeInvocations {
			if inv != nil && inv.execID == adm.id && inv.command == "exec" {
				if (inv.state == "settled" || inv.state == "failed") && inv.listener < 0 && b.runtimeBinds[inv.taskCookie] == nil {
					wrapperSettled = true
				} else {
					wrapperSettled = false
					break
				}
			}
		}
		if !wrapperSettled {
			continue
		}
		// Check Docker ExecInspect Running=false
		if !adm.dockerExitObserved {
			if !adm.checkInFlight {
				ownerID := grant.owner
				if p := b.principals[grant.owner]; p != nil && p.Role == "REALM_ROOT" {
					ownerID = b.bootOwner
				}
				daemon := b.daemons[ownerID]
				if daemon == nil || !livePIDFD(daemon.pidfd) {
					continue
				}
				deadline, err := guestDeadline(15 * time.Second)
				if err != nil {
					continue
				}
				adm.checkInFlight = true
				adm.checkResult = make(chan execSettlementCheckResult, 1)
				daemonCopy := *daemon
				dockerID := adm.dockerExecID
				ch := adm.checkResult
				go func() {
					var r execSettlementCheckResult
					info, e := recoveryDockerGET(&daemonCopy, "/v1.41/exec/"+dockerID+"/json", deadline)
					if e != nil {
						r.err = e
					} else {
						r.running = info["Running"] == true
						if ec, ok := exactNumber(info["ExitCode"]); ok {
							r.exitCode = int64(ec)
						} else {
							r.err = errGuardProtocol
						}
						if pidN, ok := exactNumber(info["Pid"]); ok {
							r.pid = uint32(pidN)
						}
					}
					ch <- r
				}()
			}
			select {
			case r := <-adm.checkResult:
				adm.checkInFlight = false
				if r.err != nil || r.running {
					continue
				}
				if r.pid != 0 && r.pid != adm.execTaskPID {
					b.checkpoint.markFault()
					return errGuardProtocol
				}
				adm.dockerExitObserved = true
			default:
				continue
			}
		}
		adm.state = "settled"
		adm.settled = true
		adm.closeHeld()
		if err := b.emitExecAdmission(adm); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	return nil
}
