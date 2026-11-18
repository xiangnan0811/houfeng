//go:build linux && amd64 && !cgo

package main

import "math"

// registerDaemon accepts only the stopped helper which is the authenticated
// owner's direct child. WATCH_DAEMON is established before its ACK, so the
// subsequent same-task dockerd exec cannot race ahead of lineage observation.
func (b *authorityBroker) registerDaemon(p *controlPrincipal, request map[string]any, raw []byte, rights []int) ([]byte, error) {
	if b == nil || p == nil || len(rights) != 1 || b.daemons[p.ID] != nil {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "child_pid", "child_starttime", "mode") {
		return nil, errControlFrame
	}
	mode, ok := body["mode"].(string)
	if !ok || !(p.Role == "BOOT_OWNER" && mode == "initial" || p.Role == "RECOVERY_OBSERVER" && mode == "reboot") {
		return nil, errControlFrame
	}
	pid, ok := exactNumber(body["child_pid"])
	if !ok || pid <= 1 || pid > math.MaxInt32 || !pidfdSelects(rights[0], uint32(pid)) {
		return nil, errControlFrame
	}
	start, ok := exactNumber(body["child_starttime"])
	if !ok || start == 0 {
		return nil, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID != p.NextRequest {
		return nil, errControlFrame
	}
	owner, err := observeProcess(p.PID, p.PIDFD)
	if err != nil {
		return nil, err
	}
	child, err := observeProcess(uint32(pid), rights[0])
	if err != nil {
		return nil, err
	}
	if child.PPID != p.PID || child.StartTicks != start || child.State != 'T' || child.Tracer != 0 || child.Cgroup != owner.Cgroup || child.ExecutableSHA256 != b.manifest.BrokerSHA256 || !exactArgumentVector(child.Argv, []string{"/opt/g2/g2-bpf-authority", "daemon-child"}) {
		return nil, errGuardProtocol
	}
	if err = verifyProcessExecutable(child.PID, "/opt/g2/g2-bpf-authority", b.manifest.BrokerSHA256); err != nil {
		return nil, err
	}
	if err = checkDaemonConfigAbsent(nil); err != nil {
		return nil, err
	}
	dockerd := b.manifest.Executables["dockerd"]
	file, digest, err := openTrustedRegular(dockerd.Path, 128<<20)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if digest != dockerd.SHA256 || dockerd.Path != "/usr/bin/dockerd" {
		return nil, errGuardProtocol
	}
	task, err := b.authority.guard.pinTask(uint32(pid))
	if err != nil {
		return nil, err
	}
	keep, watched := false, false
	defer func() {
		if !keep {
			if watched {
				b.checkpoint.markFault()
			}
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != uint32(pid) || task.TGID != uint32(pid) || !livePIDFD(rights[0]) {
		return nil, errGuardProtocol
	}
	sequence, err := b.authority.guard.watchDaemon(task.Cookie)
	if err != nil {
		return nil, err
	}
	watched = true
	if b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.lineage == nil {
		return nil, errGuardProtocol
	}
	lineage := b.checkpoint.lifecycle.lineage
	lineage.root = task.Cookie
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		return nil, err
	}
	node := lineage.nodes[task.Cookie]
	if lineage.root != task.Cookie || node == nil || node.cookie != task.Cookie || node.startNS != task.StartBoottimeNS ||
		node.tgid != task.TGID || node.tid != task.TID || node.forkSeq != sequence || node.generation != 1 ||
		node.role != "helper" || node.exited || node.tainted || b.checkpoint.cursor < sequence {
		return nil, errGuardProtocol
	}
	if !livePIDFD(rights[0]) || !livePIDFD(p.PIDFD) {
		return nil, errGuardProtocol
	}
	reg := &daemonRegistration{owner: p.ID, cookie: task.Cookie, startNS: task.StartBoottimeNS, watchSequence: sequence, pid: uint32(pid), pidfd: rights[0], mode: mode, state: "stopped", helperSHA: child.ExecutableSHA256, dockerdSHA: digest, argvSHA: child.ArgvSHA256}
	_, _, err = b.transcript.append("daemon", map[string]any{
		"cookie": jsonU64(reg.cookie), "owner": jsonU64(p.ID), "mode": mode, "task_cookie": jsonU64(task.Cookie),
		"start_ns": jsonU64(task.StartBoottimeNS), "helper_sha256": child.ExecutableSHA256,
		"dockerd_sha256": digest, "argv_sha256": child.ArgvSHA256, "watch_sequence": jsonU64(sequence), "state": "stopped",
	})
	if err != nil {
		return nil, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"daemon_cookie": jsonU64(task.Cookie), "watch_sequence": jsonU64(sequence)})
	if err != nil {
		return nil, err
	}
	if err = p.commitReply(requestID, raw, ack, rights); err != nil {
		return nil, err
	}
	if b.daemons == nil {
		b.daemons = make(map[uint64]*daemonRegistration)
	}
	b.daemons[p.ID] = reg
	keep = true
	return ack, nil
}

// observeDaemonExec is called after draining the guard event stream. It emits
// a new record only for the real same-task helper-to-dockerd transition.
func (b *authorityBroker) observeDaemonExec(reg *daemonRegistration) error {
	if b == nil || reg == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.lifecycle.lineage == nil || !livePIDFD(reg.pidfd) {
		return errGuardProtocol
	}
	lineage := b.checkpoint.lifecycle.lineage
	n := lineage.nodes[reg.cookie]
	if n == nil || n.cookie != lineage.root || n.exited || n.tainted || n.startNS != reg.startNS || n.tgid != reg.pid || n.tid != reg.pid || n.forkSeq != reg.watchSequence || n.generation != 2 || n.execSeq <= reg.watchSequence || n.role != "dockerd" || n.digest != reg.dockerdSHA {
		return errGuardProtocol
	}
	process, err := observeProcess(reg.pid, reg.pidfd)
	if err != nil {
		return err
	}
	if process.ExecutableSHA256 != reg.dockerdSHA || !exactArgumentVector(process.Argv, fixedDockerArgs) {
		return errGuardProtocol
	}
	if err = verifyProcessExecutable(reg.pid, b.manifest.Executables["dockerd"].Path, reg.dockerdSHA); err != nil {
		return err
	}
	if err = checkDaemonConfigAbsent(nil); err != nil {
		return err
	}
	if err = checkDaemonNamespacesAndRlimits(reg.pid, nil); err != nil {
		return err
	}
	task, err := b.authority.guard.pinTask(reg.pid)
	if err != nil {
		return err
	}
	defer b.authority.guard.dropTask(task.Cookie)
	if task.Cookie != reg.cookie || task.StartBoottimeNS != reg.startNS || !livePIDFD(reg.pidfd) {
		return errGuardProtocol
	}
	if reg.state == "executing" {
		if reg.argvSHA != process.ArgvSHA256 {
			return errGuardProtocol
		}
		return nil
	}
	if reg.state != "stopped" {
		return errGuardProtocol
	}
	_, _, err = b.transcript.append("daemon", map[string]any{"cookie": jsonU64(reg.cookie), "owner": jsonU64(reg.owner), "mode": reg.mode, "task_cookie": jsonU64(reg.cookie), "start_ns": jsonU64(reg.startNS), "helper_sha256": reg.helperSHA, "dockerd_sha256": reg.dockerdSHA, "argv_sha256": process.ArgvSHA256, "watch_sequence": jsonU64(reg.watchSequence), "state": "executing"})
	if err != nil {
		return err
	}
	reg.state, reg.argvSHA = "executing", process.ArgvSHA256
	return nil
}
