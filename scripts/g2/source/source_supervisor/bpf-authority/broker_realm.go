//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"math"
	"time"

	"golang.org/x/sys/unix"
)

// registerRealm creates a new principal for the existing owner or its one
// live direct restart child. The previous owner retains only exact replay of
// this transfer while both participants remain alive.
func (b *authorityBroker) registerRealm(p *controlPrincipal, request map[string]any, raw []byte, rights []int) ([]byte, bool, error) {
	if b == nil || p == nil || p.Role != "BOOT_OWNER" || b.bootOwner != p.ID || p.Transferred || len(rights) != 1 || !b.bootstrapGrantsRetired(p.ID) || b.checkpoint == nil || b.checkpoint.active != nil {
		return nil, false, errGuardProtocol
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "child_pid", "child_starttime", "scenario") {
		return nil, false, errControlFrame
	}
	pid, ok := exactNumber(body["child_pid"])
	if !ok || pid <= 1 || pid > math.MaxInt32 || !pidfdSelects(rights[0], uint32(pid)) {
		return nil, false, errControlFrame
	}
	start, ok := exactNumber(body["child_starttime"])
	if !ok || start == 0 {
		return nil, false, errControlFrame
	}
	scenario, ok := body["scenario"].(string)
	if !ok || b.caseFacts == nil || scenario != b.caseFacts["scenario"] {
		return nil, false, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, false, errControlFrame
	}
	owner, err := observeProcess(p.PID, p.PIDFD)
	if err != nil {
		return nil, false, err
	}
	child, err := observeProcess(uint32(pid), rights[0])
	if err != nil {
		return nil, false, err
	}
	if child.StartTicks != start || child.ExecutableSHA256 != owner.ExecutableSHA256 || child.ArgvSHA256 != owner.ArgvSHA256 || child.CgroupSHA256 != owner.CgroupSHA256 || child.UID != 0 || child.GID != 0 || child.Tracer != 0 {
		return nil, false, errGuardProtocol
	}
	if err = verifyProcessExecutable(child.PID, b.manifest.Executables["python"].Path, b.manifest.Executables["python"].SHA256); err != nil {
		return nil, false, err
	}
	var launch *launchRegistration
	for _, value := range b.launches {
		if value.childPIDFD == p.PIDFD {
			if launch != nil {
				return nil, false, errGuardProtocol
			}
			launch = value
		}
	}
	if launch == nil || len(launch.retainedFDs) != 4 || verifyHeldScript(launch.retainedFDs[3], "/run/g2/source_guest.py", b.manifest.Scripts["source_guest"]) != nil {
		return nil, false, errGuardProtocol
	}
	task, err := b.authority.guard.pinTask(uint32(pid))
	if err != nil {
		return nil, false, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.TID != uint32(pid) || task.TGID != uint32(pid) || !livePIDFD(rights[0]) || !livePIDFD(p.PIDFD) {
		return nil, false, errGuardProtocol
	}
	parentCookie, registration := launch.launcherCookie, "realm_self"
	if uint32(pid) == p.PID {
		if task.Cookie != p.TaskCookie || task.StartBoottimeNS != p.StartNS {
			return nil, false, errGuardProtocol
		}
	} else {
		if !((b.caseFacts["case"] == "realm-negative" && scenario == "root-restart") || (b.caseFacts["case"] == "source-recovery" && scenario == "root-kill")) || child.PPID != p.PID {
			return nil, false, errGuardProtocol
		}
		if err = verifyRealmChildBarrier(b.authority.guard, p.TaskCookie, task.Cookie, p.PID, uint32(pid)); err != nil {
			return nil, false, err
		}
		parentCookie, registration = p.TaskCookie, "realm_child"
	}
	id, err := b.nextPrincipalID()
	if err != nil {
		return nil, false, err
	}
	principal := &controlPrincipal{ID: id, TaskCookie: task.Cookie, StartNS: task.StartBoottimeNS, Role: "REALM_ROOT", PIDFD: rights[0], PID: uint32(pid), UID: 0, GID: 0, NextRequest: 1, ExecutableSHA256: child.ExecutableSHA256, ArgvSHA256: child.ArgvSHA256, CgroupSHA256: child.CgroupSHA256}
	if err = b.appendPrincipal(principal, child, parentCookie, b.manifest.Scripts["source_guest"], registration); err != nil {
		return nil, false, err
	}
	if err = b.expectPrincipalAuditClaim(principal); err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"principal_id": jsonU64(id), "child_cookie": jsonU64(task.Cookie)})
	if err != nil {
		return nil, false, err
	}
	if b.observerSocketFD >= 0 && launch != nil && len(launch.retainedFDs) >= 3 {
		caseRaw, e := readHeldRegular(launch.retainedFDs[2], 4096)
		if e != nil {
			b.checkpoint.markFault()
			return nil, false, e
		}
		dupRootFD, e := unix.FcntlInt(uintptr(rights[0]), unix.F_DUPFD_CLOEXEC, 0)
		if e != nil {
			b.checkpoint.markFault()
			return nil, false, e
		}
		dl, e := guestDeadline(5 * time.Second)
		if e == nil {
			e = earlyProbeSend(b.observerSocketFD, map[string]any{
				"version":       json.Number("1"),
				"boot_id":       b.bootID,
				"kind":          "bind_realm_root",
				"pid":           jsonU64(uint64(pid)),
				"task_cookie":   jsonU64(task.Cookie),
				"start_ns":      jsonU64(task.StartBoottimeNS),
				"exe_sha256":    child.ExecutableSHA256,
				"script_sha256": b.manifest.Scripts["source_guest"],
				"case_sha256":   hashBytes(caseRaw),
				"epoch":         b.epoch,
			}, []int{dupRootFD}, dl)
		}
		_ = unix.Close(dupRootFD)
		if e != nil {
			b.checkpoint.markFault()
			return nil, false, e
		}
	}
	if err = p.commitReply(requestID, raw, ack, rights); err != nil {
		b.checkpoint.markFault()
		return nil, false, err
	}
	b.principals[id] = principal
	p.Transferred = true
	p.TransferRequest = requestID
	p.TransferChildPIDFD = rights[0]
	keep = true
	return ack, true, nil
}
