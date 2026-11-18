//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"

	"golang.org/x/sys/unix"
)

type pendingRuntimeStart struct {
	fd       int
	raw      []byte
	request  map[string]any
	cred     unix.Ucred
	rights   []int
	task     guardTask
	pidfd    int
	grant    *brokerGrant
	deadline uint64
	waiters  map[int]bool
}

// queueRuntimeStart retains the authenticated sender incarnation and received
// listener while the same grant's create is awaiting observed settlement.
func (b *authorityBroker) queueRuntimeStart(fd int, raw []byte, request map[string]any, cred *unix.Ucred, rights []int) (bool, error) {
	body, ok := request["body"].(map[string]any)
	if !ok || body["command"] != "start" {
		return false, nil
	}
	if !exactKeys(request, "version", "boot_id", "request_id", "operation", "body") || request["version"] != json.Number("1") || request["boot_id"] != b.bootID || request["request_id"] != json.Number("1") || request["operation"] != "BIND_RUNTIME" || !exactKeys(body, "container_id", "command", "bundle_sha256", "process_sha256") || body["bundle_sha256"] != nil || body["process_sha256"] != nil {
		return false, errControlFrame
	}
	cid, ok := body["container_id"].(string)
	if !ok || !runtimeContainerID(cid) {
		return false, nil
	}
	if q := b.pendingStart; q != nil {
		if cred == nil || *cred != q.cred || !bytes.Equal(raw, q.raw) || len(rights) != len(q.rights) || !pidfdSelects(q.pidfd, q.task.TGID) {
			return false, errControlFrame
		}
		peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil || *peer != q.cred {
			return false, errControlFrame
		}
		for i := range rights {
			if !sameHeldDescription(rights[i], q.rights[i]) {
				return false, errControlFrame
			}
		}
		for _, right := range rights {
			_ = unix.Close(right)
		}
		q.waiters[fd] = true
		return true, nil
	}
	var grant *brokerGrant
	for _, g := range b.grants {
		if g.containerID == cid && g.phase != "retired" && g.phase != "revoked" {
			if grant != nil {
				return false, errControlFrame
			}
			grant = g
		}
	}
	if grant == nil || !grant.createBound || grant.createSettled {
		return false, nil
	}
	pendingCreate := false
	for _, inv := range b.runtimeInvocations {
		if inv.grant == grant && inv.command == "create" && inv.state != "settled" && inv.state != "failed" {
			pendingCreate = true
		}
	}
	if !pendingCreate {
		return false, nil
	}
	if cred == nil || cred.Pid <= 1 || cred.Uid != 0 || cred.Gid != 0 || len(rights) != 1 || runtimeListenerFD(rights[0]) != nil {
		return false, errControlFrame
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || *peer != *cred {
		return false, errControlFrame
	}
	task, err := b.authority.guard.pinTask(uint32(cred.Pid))
	if err != nil {
		return false, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	pidfd, err := unix.PidfdOpen(int(cred.Pid), 0)
	if err != nil {
		return false, err
	}
	defer func() {
		if !keep {
			_ = unix.Close(pidfd)
		}
	}()
	process, err := observeProcess(uint32(cred.Pid), pidfd)
	if err != nil || task.TID != uint32(cred.Pid) || task.TGID != task.TID || task.StartBoottimeNS/10_000_000 != process.StartTicks || !pidfdSelects(pidfd, task.TID) || process.ExecutableSHA256 != b.manifest.BrokerSHA256 || verifyProcessExecutable(process.PID, "/opt/g2/g2-bpf-runtime", b.manifest.BrokerSHA256) != nil {
		return false, errGuardProtocol
	}
	args, err := runtimeArgumentVector(process.Argv)
	if err != nil {
		return false, err
	}
	parsed, err := parseRuntimeArgs(args)
	if err != nil || parsed.Command != "start" || parsed.ContainerID != cid {
		return false, errControlFrame
	}
	now, err := monoNowNS()
	if err != nil {
		return false, err
	}
	deadline := now + 30_000_000_000
	if deadline > authorityGuestDeadlineNS {
		deadline = authorityGuestDeadlineNS
	}
	b.pendingStart = &pendingRuntimeStart{fd: fd, raw: append([]byte(nil), raw...), request: request, cred: *cred, rights: rights, task: task, pidfd: pidfd, grant: grant, deadline: deadline, waiters: map[int]bool{fd: true}}
	keep = true
	return true, nil
}

func (b *authorityBroker) finishPendingStart(disconnected bool, now uint64) ([]byte, bool, error) {
	q := b.pendingStart
	if q == nil {
		return nil, false, errGuardProtocol
	}
	failed := disconnected || now >= q.deadline || b.checkpoint.invalid || !pidfdSelects(q.pidfd, q.task.TGID) || q.grant.phase == "revoked" || q.grant.phase == "retired"
	createFailed := false
	for _, inv := range b.runtimeInvocations {
		if inv.grant == q.grant && inv.command == "create" && inv.state == "failed" {
			createFailed = true
		}
	}
	if !failed && !createFailed && !q.grant.createSettled {
		return nil, false, nil
	}
	var ack []byte
	retained := false
	var err error
	if !failed && !createFailed {
		before := len(b.transcript.entries)
		ack, retained, err = b.bindRuntimeRequest(q.fd, q.raw, q.request, &q.cred, q.rights, nil)
		if err != nil && !retained && !b.checkpoint.invalid && !b.checkpoint.faulted && len(b.transcript.entries) == before {
			ack, err = controlACK(b.reads, &controlPrincipal{ID: 0}, 1, q.raw, "BPF_AUTH_DENIED", map[string]any{"code": "BPF_AUTH_DENIED", "sequence": jsonU64(uint64(before))})
		}
	} else if !disconnected {
		code := "BPF_AUTH_DENIED"
		if now >= q.deadline || b.checkpoint.invalid {
			code = "BPF_AUTH_UNAVAILABLE"
		}
		ack, err = controlACK(b.reads, &controlPrincipal{ID: 0}, 1, q.raw, code, map[string]any{"code": code, "sequence": jsonU64(uint64(len(b.transcript.entries)))})
	}
	if !retained {
		for _, fd := range q.rights {
			_ = unix.Close(fd)
		}
	}
	if e := b.authority.guard.dropTask(q.task.Cookie); e != nil && err == nil {
		err = e
	}
	if e := unix.Close(q.pidfd); e != nil && err == nil {
		err = e
	}
	b.pendingStart = nil
	return ack, true, err
}
