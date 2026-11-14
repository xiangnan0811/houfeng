//go:build linux && amd64 && !cgo

package main

import "math"

// registerRecovery transfers the one launch principal only after independent
// verification of the original control directory and the newly mounted seed.
// It retains the supplied descriptors for exact replay and future rechecks.
func (b *authorityBroker) registerRecovery(p *controlPrincipal, request map[string]any, raw []byte, rights []int) ([]byte, bool, error) {
	if b == nil || p == nil || p.Role != "RECOVERY_PENDING" || b.recoveryPending != p.ID || p.Transferred || b.recovery != nil || len(rights) != 3 || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil {
		return nil, false, errGuardProtocol
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "intent_sha256", "original_boot_id", "used_sha256") {
		return nil, false, errControlFrame
	}
	intentSHA, ok := digestString(body["intent_sha256"])
	if !ok {
		return nil, false, errControlFrame
	}
	usedSHA, ok := digestString(body["used_sha256"])
	if !ok {
		return nil, false, errControlFrame
	}
	originalBoot, ok := body["original_boot_id"].(string)
	if !ok || len(originalBoot) != 36 || originalBoot == b.bootID {
		return nil, false, errControlFrame
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, false, errControlFrame
	}
	if !livePIDFD(p.PIDFD) {
		return nil, false, errGuardProtocol
	}
	binding, facts, err := verifyRecoveryBinding(rights[0], rights[1], rights[2], b.manifest, b.bootID, intentSHA, usedSHA, originalBoot)
	if err != nil {
		return nil, false, err
	}
	var launch *launchRegistration
	for _, candidate := range b.launches {
		if candidate.childPIDFD == p.PIDFD {
			if launch != nil {
				return nil, false, errGuardProtocol
			}
			launch = candidate
		}
	}
	if launch == nil || len(launch.retainedFDs) != 2 || verifyHeldScript(launch.retainedFDs[1], "/opt/g2/realm-supervisor/reboot_observer.py", b.manifest.Scripts["reboot_observer"]) != nil {
		return nil, false, errGuardProtocol
	}
	process, err := observeProcess(p.PID, p.PIDFD)
	if err != nil {
		return nil, false, err
	}
	if process.ExecutableSHA256 != p.ExecutableSHA256 || process.ArgvSHA256 != p.ArgvSHA256 || process.CgroupSHA256 != p.CgroupSHA256 || process.UID != 0 || process.GID != 0 {
		return nil, false, errGuardProtocol
	}
	task, err := b.authority.guard.pinTask(p.PID)
	if err != nil {
		return nil, false, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = b.authority.guard.dropTask(task.Cookie)
		}
	}()
	if task.Cookie != p.TaskCookie || task.TGID != p.PID || task.TID != p.PID || task.StartBoottimeNS != p.StartNS || !livePIDFD(p.PIDFD) {
		return nil, false, errGuardProtocol
	}
	id, err := b.nextPrincipalID()
	if err != nil {
		return nil, false, err
	}
	principal := &controlPrincipal{ID: id, TaskCookie: task.Cookie, StartNS: task.StartBoottimeNS, Role: "RECOVERY_OBSERVER", PIDFD: p.PIDFD, PID: p.PID, UID: 0, GID: 0, NextRequest: 1, ExecutableSHA256: process.ExecutableSHA256, ArgvSHA256: process.ArgvSHA256, CgroupSHA256: process.CgroupSHA256}
	if err = b.appendPrincipal(principal, process, launch.launcherCookie, b.manifest.Scripts["reboot_observer"], "recovery"); err != nil {
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
	if err = p.commitReply(requestID, raw, ack, rights); err != nil {
		return nil, false, err
	}
	b.principals[id] = principal
	b.recovery, b.caseFacts, b.postgresUID, b.postgresGID = binding, facts, binding.uid, binding.gid
	p.Transferred, p.TransferRequest, p.TransferChildPIDFD = true, requestID, p.PIDFD
	keep = true
	return ack, true, nil
}
