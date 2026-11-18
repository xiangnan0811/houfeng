//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func runtimeListenerFD(fd int) error {
	if fd < 0 {
		return errGuardProtocol
	}
	link, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil || link != "anon_inode:seccomp notify" {
		return errGuardProtocol
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDWR {
		return errGuardProtocol
	}
	// This only establishes the received FD's ioctl type. ADDFD|SEND was
	// independently exercised during early bootstrap, not inferred here.
	if err = notificationValid(fd, math.MaxUint64); err != unix.ENOENT {
		return errGuardProtocol
	}
	return nil
}

func (b *authorityBroker) nextListenerID() (uint64, error) {
	if b.nextListener == 0 {
		b.nextListener = 1
	}
	if b.nextListener == math.MaxUint64 {
		return 0, errGuardProtocol
	}
	id := b.nextListener
	b.nextListener++
	return id, nil
}

func (b *authorityBroker) reportsOwnerProbeCreate(grant *brokerGrant, cid, command string) bool {
	return b != nil && grant != nil && command == "create" && grant.containerID == cid &&
		grant.id != 0 && grant.generation != 0 && b.bootOwner != 0 && grant.owner == b.bootOwner &&
		(grant.purpose == "postgres-uid" || grant.purpose == "postgres-gid") &&
		(grant.phase == "admitted" || grant.phase == "active" || grant.phase == "settling")
}

// bindRuntimeRequest authenticates one pre-principal runtime invocation. A
// successful new binding retains all passed descriptors for exact replay.
func (b *authorityBroker) bindRuntimeRequest(fd int, raw []byte, request map[string]any, cred *unix.Ucred, rights []int, bindDiagnostic *brokerBindDiagnostic) (_ []byte, _ bool, resultErr error) {
	bindDiagnostic.begin(b)
	defer func() { bindDiagnostic.finish(b, resultErr) }()
	if b == nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointBrokerNil, errControlFrame)
	}
	if b.checkpoint == nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointCheckpointNil, errControlFrame)
	}
	if b.checkpoint.invalid {
		return nil, false, bindDiagnostic.reject(brokerBindPointCheckpointInvalid, errControlFrame)
	}
	if b.checkpoint.active != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointCheckpointActive, errControlFrame)
	}
	if b.pendingCheckpoint != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPendingCheckpoint, errControlFrame)
	}
	if b.pendingAdmit != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPendingAdmit, errControlFrame)
	}
	if b.pendingExecOp != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPendingExec, errControlFrame)
	}
	if b.authority == nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointAuthorityNil, errControlFrame)
	}
	if b.checkpoint.lifecycle == nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointLifecycleNil, errControlFrame)
	}
	if !exactKeys(request, "version", "boot_id", "request_id", "operation", "body") || request["version"] != json.Number("1") || request["boot_id"] != b.bootID || request["request_id"] != json.Number("1") || request["operation"] != "BIND_RUNTIME" {
		return nil, false, bindDiagnostic.reject(brokerBindPointRequest, errControlFrame)
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "container_id", "command", "bundle_sha256", "process_sha256") {
		return nil, false, bindDiagnostic.reject(brokerBindPointBody, errControlFrame)
	}
	command, ok := body["command"].(string)
	if !ok {
		return nil, false, bindDiagnostic.reject(brokerBindPointCommand, errControlFrame)
	}
	if command == "features" {
		ack, err := b.bindFeatures(fd, raw, request, cred, rights)
		return ack, false, bindDiagnostic.reject(brokerBindPointFeatures, err)
	}
	cid, ok := body["container_id"].(string)
	if !ok || !runtimeContainerID(cid) {
		return nil, false, bindDiagnostic.reject(brokerBindPointContainerID, errControlFrame)
	}
	wantFDs := 1
	var diagnosticCID string
	var diagnostic ownerProbeRuntimeDiagnostic
	if command == "create" {
		wantFDs = 3
		defer func() {
			if resultErr != nil && diagnosticCID != "" {
				sendOwnerProbeRuntimeDiagnostic(diagnosticCID, diagnostic, resultErr)
			}
		}()
	} else if command == "exec" {
		wantFDs = 2
	}
	if len(rights) != wantFDs || cred == nil || cred.Pid <= 1 || cred.Uid != 0 || cred.Gid != 0 {
		return nil, false, bindDiagnostic.reject(brokerBindPointRightsCredential, errControlFrame)
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		return nil, false, bindDiagnostic.reject(brokerBindPointPeer, errControlFrame)
	}
	task, err := b.authority.guard.pinTask(uint32(cred.Pid))
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointTaskPin, err)
	}
	keepTask := false
	defer func() {
		if !keepTask {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != uint32(cred.Pid) || task.TGID != uint32(cred.Pid) {
		return nil, false, bindDiagnostic.reject(brokerBindPointTaskIdentity, errGuardProtocol)
	}
	if old := b.runtimeBinds[task.Cookie]; old != nil {
		if !livePIDFD(old.pidfd) || !bytes.Equal(old.request, raw) || len(old.rights) != len(rights) {
			return nil, false, bindDiagnostic.reject(brokerBindPointReplayGuard, errControlFrame)
		}
		for i := range rights {
			if !sameHeldDescription(old.rights[i], rights[i]) {
				return nil, false, bindDiagnostic.reject(brokerBindPointReplayRight, errControlFrame)
			}
		}
		return append([]byte(nil), old.ack...), false, nil
	}
	pidfd, err := unix.PidfdOpen(int(cred.Pid), 0)
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPIDFDOpen, err)
	}
	keepFD := false
	defer func() {
		if !keepFD {
			_ = unix.Close(pidfd)
		}
	}()
	process, err := observeProcess(uint32(cred.Pid), pidfd)
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointProcessObserve, err)
	}
	if process.UID != 0 || process.GID != 0 || process.ExecutableSHA256 != b.manifest.BrokerSHA256 || verifyProcessExecutable(process.PID, "/opt/g2/g2-bpf-runtime", b.manifest.BrokerSHA256) != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointProcessIdentity, errGuardProtocol)
	}
	args, err := runtimeArgumentVector(process.Argv)
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointArgv, err)
	}
	parsed, err := parseRuntimeArgs(args)
	if err != nil || parsed.Command != command || parsed.ContainerID != cid {
		return nil, false, bindDiagnostic.reject(brokerBindPointRuntimeArgs, errControlFrame)
	}
	if err = runtimeListenerFD(rights[0]); err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointListener, err)
	}
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return nil, false, bindDiagnostic.reject(brokerBindPointGuardDrain, err)
	}
	lineage := b.checkpoint.lifecycle.lineage
	if lineage == nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointLineageMissing, errGuardProtocol)
	}
	ancestry, err := lineage.ancestry(task.Cookie, "wrapper")
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointAncestry, err)
	}
	node := lineage.nodes[task.Cookie]
	if node == nil || node.startNS != task.StartBoottimeNS || node.tgid != task.TGID || node.tid != task.TID {
		return nil, false, bindDiagnostic.reject(brokerBindPointNodeIdentity, errGuardProtocol)
	}
	containerd, shim := false, false
	for current := node; current.cookie != lineage.root; {
		if current.role == "shim" {
			shim = true
		}
		if current.role == "containerd" {
			containerd = true
		}
		current = lineage.nodes[current.parent]
		if current == nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointAncestorMissing, errGuardProtocol)
		}
	}
	if !containerd || !shim {
		return nil, false, bindDiagnostic.reject(brokerBindPointRequiredAncestors, errGuardProtocol)
	}
	var daemon *daemonRegistration
	for _, candidate := range b.daemons {
		if candidate.cookie == lineage.root {
			if daemon != nil {
				return nil, false, bindDiagnostic.reject(brokerBindPointDaemonDuplicate, errGuardProtocol)
			}
			daemon = candidate
		}
	}
	if daemon == nil || b.observeDaemonExec(daemon) != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointDaemon, errGuardProtocol)
	}
	var grant *brokerGrant
	for _, candidate := range b.grants {
		if candidate.containerID == cid && candidate.phase != "retired" && candidate.phase != "revoked" {
			if grant != nil {
				return nil, false, bindDiagnostic.reject(brokerBindPointGrantDuplicate, errGuardProtocol)
			}
			grant = candidate
		}
	}
	if grant == nil && (command == "start" || command == "create") && b.selftestBaseline != nil && b.selftestBaseline.name == "wrong-generation" && !b.selftestBaseline.witnessed && daemon.owner == b.bootOwner {
		var retired *brokerGrant
		for _, candidate := range b.grants {
			if candidate.id == b.selftestBaseline.grantID && candidate.containerID == cid && candidate.owner == b.selftestBaseline.owner && candidate.phase == "retired" && candidate.generation != 0 {
				if retired != nil {
					return nil, false, bindDiagnostic.reject(brokerBindPointRetiredDuplicate, errGuardProtocol)
				}
				retired = candidate
			}
		}
		if retired != nil {
			b.selftestBaseline.actorCookie = task.Cookie
			b.selftestBaseline.requestHash = hashBytes(raw)
			b.selftestBaseline.witnessed = true
			return nil, false, bindDiagnostic.reject(brokerBindPointWrongGeneration, errControlFrame)
		}
	}
	if grant == nil || grant.id == 0 || grant.generation == 0 {
		return nil, false, bindDiagnostic.reject(brokerBindPointGrant, errGuardProtocol)
	}
	if s := b.selftestBaseline; s != nil && s.name == "attachment-replace" && s.grantID == grant.id && grant.startObserved && (command == "start" || command == "create") {
		return nil, false, bindDiagnostic.reject(brokerBindPointAttachmentReplace, errControlFrame)
	}
	owner := b.principals[grant.owner]
	if owner == nil || owner.Terminal || !livePIDFD(owner.PIDFD) {
		return nil, false, bindDiagnostic.reject(brokerBindPointOwner, errGuardProtocol)
	}
	daemonOwner := owner.ID
	if owner.Role == "REALM_ROOT" {
		daemonOwner = b.bootOwner
	}
	if daemon.owner != daemonOwner {
		return nil, false, bindDiagnostic.reject(brokerBindPointDaemonOwner, errGuardProtocol)
	}
	if b.reportsOwnerProbeCreate(grant, cid, command) {
		diagnosticCID, diagnostic = cid, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerCreatePolicy}
	}
	var bundleSHA, processSHA string
	var candidateFacts *runtimeContainerFacts
	var candidateStat unix.Stat_t
	switch command {
	case "create":
		if grant.phase != "admitted" || grant.createBound || body["process_sha256"] != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointCreatePolicy, errGuardProtocol)
		}
		bundleSHA, ok = digestString(body["bundle_sha256"])
		if !ok {
			return nil, false, bindDiagnostic.reject(brokerBindPointCreateDigest, errControlFrame)
		}
		var bundleDiagnostic ownerProbeRuntimeDiagnostic
		candidateFacts, candidateStat, bundleDiagnostic, err = verifyRuntimeBundle(b, grant, parsed.BundlePath, rights[1], rights[2], bundleSHA)
		if err != nil {
			diagnostic = bundleDiagnostic
			return nil, false, bindDiagnostic.reject(brokerBindPointCreateBundle, err)
		}
		if diagnosticCID != "" {
			diagnostic = ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerCreateFinal}
		}
	case "start":
		if body["bundle_sha256"] != nil || body["process_sha256"] != nil || !grant.createBound || !grant.createSettled || !grant.actualQueryConfirmed || grant.startObserved {
			return nil, false, bindDiagnostic.reject(brokerBindPointStartPolicy, errGuardProtocol)
		}
		if err = recheckRuntimeBundle(grant); err != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointStartBundle, err)
		}
	case "state":
		if body["bundle_sha256"] != nil || body["process_sha256"] != nil || !grant.createBound {
			return nil, false, bindDiagnostic.reject(brokerBindPointStatePolicy, errGuardProtocol)
		}
		if err = recheckRuntimeBundle(grant); err != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointStateBundle, err)
		}
	case "kill", "delete":
		if body["bundle_sha256"] != nil || body["process_sha256"] != nil || !grant.createBound {
			return nil, false, bindDiagnostic.reject(brokerBindPointKillDeletePolicy, errGuardProtocol)
		}
		if err = recheckRuntimeBundle(grant); err != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointKillDeleteBundle, err)
		}
	case "exec":
		if body["bundle_sha256"] != nil || grant.phase != "active" || grant.exec == nil || grant.exec.state != "admitted" || grant.exec.consumed || grant.exec.settled || !grant.actualQueryConfirmed || grant.exec.dockerExecID == "" || parsed.PidFile != filepath.Join(grant.bundlePath, grant.exec.dockerExecID+".pid") {
			return nil, false, bindDiagnostic.reject(brokerBindPointExecPolicy, errGuardProtocol)
		}
		processSHA, ok = digestString(body["process_sha256"])
		if !ok {
			return nil, false, bindDiagnostic.reject(brokerBindPointExecDigest, errControlFrame)
		}
		if err = recheckRuntimeBundle(grant); err != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointExecBundle, err)
		}
		if err = verifyRuntimeExec(b, grant, parsed.ProcessPath, rights[1], processSHA); err != nil {
			return nil, false, bindDiagnostic.reject(brokerBindPointExecProcess, err)
		}
	default:
		return nil, false, bindDiagnostic.reject(brokerBindPointCommandUnsupported, errControlFrame)
	}
	if !livePIDFD(pidfd) || !livePIDFD(owner.PIDFD) || !livePIDFD(daemon.pidfd) || len(b.runtimeInvocations) >= 4096 {
		return nil, false, bindDiagnostic.reject(brokerBindPointLiveCapacity, errGuardProtocol)
	}
	if b.runtimeInvocations != nil && b.runtimeInvocations[rights[0]] != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointListenerCollision, errGuardProtocol)
	}
	principalID, err := b.nextPrincipalID()
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPrincipalID, err)
	}
	invocationID, err := b.nextRuntimeID()
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointInvocationID, err)
	}
	listenerID, err := b.nextListenerID()
	if err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointListenerID, err)
	}
	var bundleValue, processValue, execValue any
	var execID uint64
	if command == "create" {
		bundleValue = bundleSHA
	}
	if command == "exec" {
		processValue, execID = processSHA, grant.exec.id
		execValue = jsonU64(execID)
	}
	principal := &controlPrincipal{ID: principalID, TaskCookie: task.Cookie, StartNS: task.StartBoottimeNS, Role: "RUNTIME", PIDFD: pidfd, PID: process.PID, UID: 0, GID: 0, NextRequest: 1, ExecutableSHA256: process.ExecutableSHA256, ArgvSHA256: process.ArgvSHA256, CgroupSHA256: process.CgroupSHA256}
	if err = b.appendPrincipal(principal, process, node.parent, "", "runtime"); err != nil {
		return nil, false, bindDiagnostic.reject(brokerBindPointPrincipalAppend, err)
	}
	_, _, err = b.transcript.append("invocation", map[string]any{"id": jsonU64(invocationID), "principal_id": jsonU64(principalID), "task_cookie": jsonU64(task.Cookie), "daemon_cookie": jsonU64(daemon.cookie), "grant_id": jsonU64(grant.id), "container_id": cid, "generation": jsonU64(grant.generation), "command": command, "config_sha256": grant.configSHA, "bundle_sha256": bundleValue, "process_sha256": processValue, "exec_id": execValue, "listener_id": jsonU64(listenerID), "ancestry": ancestry, "state": "bound"})
	if err != nil {
		b.checkpoint.markFault()
		return nil, false, bindDiagnostic.reject(brokerBindPointInvocationAppend, err)
	}
	if command == "delete" {
		if err = b.authorizeBoundDelete(grant, task, pidfd, invocationID, bindDiagnostic); err != nil {
			b.checkpoint.markFault()
			b.reportOwnerProbeCreateHistory(grant, err)
			return nil, false, bindDiagnostic.reject(brokerBindPointDeleteAuthorize, err)
		}
	}
	ack, err := controlACK(b.reads, principal, 1, raw, "OK", map[string]any{"invocation_id": jsonU64(invocationID), "task_cookie": jsonU64(task.Cookie), "grant_id": jsonU64(grant.id), "generation": jsonU64(grant.generation), "config_sha256": grant.configSHA, "exec_id": execValue})
	if err != nil {
		b.checkpoint.markFault()
		return nil, false, bindDiagnostic.reject(brokerBindPointACK, err)
	}
	if command == "exec" {
		grant.exec.consumed = true
		grant.exec.state = "bound"
		grant.exec.processSHA256 = processSHA
		grant.exec.wrapperTaskCookie = task.Cookie
		if err = b.emitExecAdmission(grant.exec); err != nil {
			b.checkpoint.markFault()
			return nil, false, bindDiagnostic.reject(brokerBindPointExecAdmission, err)
		}
	}
	inv := &runtimeInvocation{id: invocationID, principalID: principalID, taskCookie: task.Cookie, daemonCookie: daemon.cookie, grantID: grant.id, generation: grant.generation, listenerID: listenerID, execID: execID, containerID: cid, command: command, configSHA: grant.configSHA, bundleSHA: bundleSHA, processSHA: processSHA, state: "bound", pid: task.TID, tgid: task.TGID, startNS: task.StartBoottimeNS, wrapperExecSequence: node.execSeq, pidfd: pidfd, listener: rights[0], grant: grant, ancestry: ancestry}
	replay := &runtimeBindReplay{cookie: task.Cookie, pidfd: pidfd, request: append([]byte(nil), raw...), ack: append([]byte(nil), ack...), rights: append([]int(nil), rights...)}
	binds := b.runtimeBinds
	if binds == nil {
		binds = make(map[uint64]*runtimeBindReplay)
	}
	invocations := b.runtimeInvocations
	if invocations == nil {
		invocations = make(map[int]*runtimeInvocation)
	}
	if command == "create" {
		grant.facts = candidateFacts
		grant.bundleDirFD, grant.bundleConfigFD, grant.bundlePath, grant.bundleSHA = rights[1], rights[2], parsed.BundlePath, bundleSHA
		grant.bundleStat = candidateStat
		grant.createBound = true
	}
	b.runtimeBinds, b.runtimeInvocations = binds, invocations
	binds[task.Cookie] = replay
	invocations[rights[0]], b.principals[principalID] = inv, principal
	keepTask, keepFD = true, true
	return ack, true, nil
}
