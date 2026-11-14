//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"

	"golang.org/x/sys/unix"
)

type runtimeBindReplay struct {
	cookie       uint64
	pidfd        int
	request, ack []byte
	rights       []int
}

func (b *authorityBroker) nextRuntimeID() (uint64, error) {
	if b.nextInvocation == 0 {
		b.nextInvocation = 1
	}
	if b.nextInvocation == math.MaxUint64 {
		return 0, errGuardProtocol
	}
	id := b.nextInvocation
	b.nextInvocation++
	return id, nil
}
func (b *authorityBroker) nextFeatureOrdinal() (uint64, error) {
	if b.nextFeatureGeneration == 0 {
		b.nextFeatureGeneration = 1
	}
	if b.nextFeatureGeneration == math.MaxUint64 {
		return 0, errGuardProtocol
	}
	id := b.nextFeatureGeneration
	b.nextFeatureGeneration++
	return id, nil
}

func runtimeArgumentVector(raw []byte) ([]string, error) {
	if len(raw) == 0 || raw[len(raw)-1] != 0 {
		return nil, errGuardProtocol
	}
	parts := bytes.Split(raw[:len(raw)-1], []byte{0})
	if len(parts) == 0 || string(parts[0]) != "/opt/g2/g2-bpf-runtime" {
		return nil, errGuardProtocol
	}
	out := make([]string, len(parts)-1)
	for i, value := range parts[1:] {
		if len(value) == 0 || bytes.IndexByte(value, 0) >= 0 {
			return nil, errGuardProtocol
		}
		out[i] = string(value)
	}
	return out, nil
}

// bindFeatures registers only the no-container runc features invocation. The
// corresponding wrapper has no seccomp listener or BPF operation authority.
func (b *authorityBroker) bindFeatures(fd int, raw []byte, request map[string]any, cred *unix.Ucred, rights []int) ([]byte, error) {
	if b == nil || b.authority == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || len(rights) != 0 {
		return nil, errControlFrame
	}
	if !exactKeys(request, "version", "boot_id", "request_id", "operation", "body") || request["version"] != json.Number("1") || request["boot_id"] != b.bootID || request["request_id"] != json.Number("1") || request["operation"] != "BIND_RUNTIME" {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "container_id", "command", "bundle_sha256", "process_sha256") || body["container_id"] != nil || body["command"] != "features" || body["bundle_sha256"] != nil || body["process_sha256"] != nil {
		return nil, errControlFrame
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || cred == nil || cred.Pid <= 1 || cred.Uid != 0 || cred.Gid != 0 || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		return nil, errControlFrame
	}
	task, err := b.authority.guard.pinTask(uint32(cred.Pid))
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != uint32(cred.Pid) || task.TGID != uint32(cred.Pid) {
		return nil, errGuardProtocol
	}
	if old := b.runtimeBinds[task.Cookie]; old != nil {
		if !livePIDFD(old.pidfd) || !bytes.Equal(old.request, raw) || len(old.rights) != 0 {
			return nil, errControlFrame
		}
		return append([]byte(nil), old.ack...), nil
	}
	pidfd, err := unix.PidfdOpen(int(cred.Pid), 0)
	if err != nil {
		return nil, err
	}
	keepFD := false
	defer func() {
		if !keepFD {
			_ = unix.Close(pidfd)
		}
	}()
	process, err := observeProcess(uint32(cred.Pid), pidfd)
	if err != nil {
		return nil, err
	}
	if process.UID != 0 || process.GID != 0 || process.ExecutableSHA256 != b.manifest.BrokerSHA256 {
		return nil, errGuardProtocol
	}
	if err = verifyProcessExecutable(process.PID, "/opt/g2/g2-bpf-runtime", b.manifest.BrokerSHA256); err != nil {
		return nil, err
	}
	args, err := runtimeArgumentVector(process.Argv)
	if err != nil {
		return nil, err
	}
	parsed, err := parseRuntimeArgs(args)
	if err != nil || parsed.Command != "features" || len(args) != 1 || strings.Join(args, "") != "features" {
		return nil, errControlFrame
	}
	if err = drainGuardEvents(b.authority.guard, b.transcript, b.join, &b.checkpoint.cursor, b.checkpoint.lifecycle); err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	lineage := b.checkpoint.lifecycle.lineage
	ancestry, err := lineage.ancestry(task.Cookie, "wrapper")
	if err != nil {
		return nil, err
	}
	node := lineage.nodes[task.Cookie]
	if node == nil || node.startNS != task.StartBoottimeNS || node.tgid != task.TGID || node.tid != task.TID {
		return nil, errGuardProtocol
	}
	// features is a direct dockerd descendant with only same-role fork or
	// thread intermediates; shim or application ancestry cannot qualify.
	for current := node; current.cookie != lineage.root; {
		if current.role != "wrapper" && current.role != "dockerd" {
			return nil, errGuardProtocol
		}
		current = lineage.nodes[current.parent]
		if current == nil {
			return nil, errGuardProtocol
		}
	}
	var daemon *daemonRegistration
	for _, candidate := range b.daemons {
		if candidate.cookie == lineage.root {
			if daemon != nil {
				return nil, errGuardProtocol
			}
			daemon = candidate
		}
	}
	if daemon == nil || b.observeDaemonExec(daemon) != nil {
		return nil, errGuardProtocol
	}
	principalID, err := b.nextPrincipalID()
	if err != nil {
		return nil, err
	}
	invocationID, err := b.nextRuntimeID()
	if err != nil {
		return nil, err
	}
	generation, err := b.nextFeatureOrdinal()
	if err != nil {
		return nil, err
	}
	principal := &controlPrincipal{ID: principalID, TaskCookie: task.Cookie, StartNS: task.StartBoottimeNS, Role: "RUNTIME", PIDFD: pidfd, PID: process.PID, UID: 0, GID: 0, NextRequest: 1, ExecutableSHA256: process.ExecutableSHA256, ArgvSHA256: process.ArgvSHA256, CgroupSHA256: process.CgroupSHA256}
	if err = b.appendPrincipal(principal, process, node.parent, "", "runtime"); err != nil {
		return nil, err
	}
	_, _, err = b.transcript.append("invocation", map[string]any{"id": jsonU64(invocationID), "principal_id": jsonU64(principalID), "task_cookie": jsonU64(task.Cookie), "daemon_cookie": jsonU64(daemon.cookie), "grant_id": nil, "container_id": nil, "generation": jsonU64(generation), "command": "features", "config_sha256": nil, "bundle_sha256": nil, "process_sha256": nil, "exec_id": nil, "listener_id": nil, "ancestry": ancestry, "state": "bound"})
	if err != nil {
		return nil, err
	}
	ack, err := controlACK(b.reads, principal, 1, raw, "OK", map[string]any{"invocation_id": jsonU64(invocationID), "task_cookie": jsonU64(task.Cookie), "grant_id": nil, "generation": jsonU64(generation), "config_sha256": nil, "exec_id": nil})
	if err != nil {
		return nil, err
	}
	if b.runtimeBinds == nil {
		b.runtimeBinds = make(map[uint64]*runtimeBindReplay)
	}
	b.runtimeBinds[task.Cookie] = &runtimeBindReplay{cookie: task.Cookie, pidfd: pidfd, request: append([]byte(nil), raw...), ack: append([]byte(nil), ack...)}
	if b.featureInvocations == nil {
		b.featureInvocations = make(map[uint64]*runtimeInvocation)
	}
	if b.featureInvocations[task.Cookie] != nil || len(b.featureInvocations) >= 4096 {
		b.checkpoint.markFault()
		return nil, errGuardProtocol
	}
	b.featureInvocations[task.Cookie] = &runtimeInvocation{id: invocationID, principalID: principalID, taskCookie: task.Cookie, daemonCookie: daemon.cookie, generation: generation, command: "features", state: "bound", pid: task.TID, tgid: task.TGID, startNS: task.StartBoottimeNS, wrapperExecSequence: node.execSeq, pidfd: pidfd, listener: -1, ancestry: ancestry}
	b.principals[principalID] = principal
	keep, keepFD = true, true
	return ack, nil
}
