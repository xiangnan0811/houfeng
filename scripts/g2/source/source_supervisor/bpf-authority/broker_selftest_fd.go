//go:build linux && amd64 && !cgo

package main

import (
	"golang.org/x/sys/unix"
	"math"
)

// selftestFDArm belongs to one authenticated realm and one already admitted
// grant. It does not confer BPF authority; the ordinary runtime checks still
// govern the selected notification.
type selftestFDArm struct {
	nonce, class             string
	owner, grantID, deadline uint64
	transactionID            uint64
	phase                    string
	replayOnly               bool
}

func eligibleSelftestFDGrant(grant *brokerGrant, principalID uint64) bool {
	return grant != nil && grant.owner == principalID && grant.phase == "admitted" && !grant.createBound
}

func (b *authorityBroker) armSelftestFD(p *controlPrincipal, request map[string]any, raw []byte) ([]byte, error) {
	if b == nil || p == nil || p.Role != "REALM_ROOT" || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.selftestFD != nil || b.selftestRuntime != nil || b.selftestBaseline != nil && (b.selftestBaseline.owner != p.ID || b.selftestBaseline.name != "fd-arm-replay" && b.selftestBaseline.name != "fd-arm-wrong-grant" && b.selftestBaseline.name != "fd-arm-second") || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["run_token"] != authorityRunToken {
		return nil, errControlFrame
	}
	if v, ok := b.caseFacts["bpf_variant"].(string); ok && v != "" && v != "baseline" && v != "fd-fill-failure" && v != "fd-restore-failure" {
		return nil, errControlFrame
	}
	boot, err := fixedBootID()
	if err != nil || boot != b.bootID {
		return nil, errGuardProtocol
	}
	requestID, ok := exactNumber(request["request_id"])
	if !ok || requestID == 0 || requestID == math.MaxUint64 || requestID != p.NextRequest {
		return nil, errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "nonce", "grant_id", "class") {
		return nil, errControlFrame
	}
	nonce, ok := body["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) || nonce == b.selftestMapNonce {
		return nil, errControlFrame
	}
	grantID, ok := exactNumber(body["grant_id"])
	if !ok || grantID == 0 {
		return nil, errControlFrame
	}
	if b.selftestBaseline != nil && b.selftestBaseline.name == "fd-arm-replay" && grantID != b.selftestBaseline.grantID {
		return nil, errControlFrame
	}
	class, ok := body["class"].(string)
	replayOnly := b.selftestBaseline != nil && b.selftestBaseline.name == "fd-arm-replay"
	if !ok || (class != "map" && class != "program") || !replayOnly && ((class == "map" && (b.selftestMapArmed || b.selftestProgramArmed)) || (class == "program" && (!b.selftestMapDone || b.selftestProgramArmed || grantID == b.selftestMapGrant))) {
		return nil, errControlFrame
	}
	grant := b.grants[grantID]
	if !eligibleSelftestFDGrant(grant, p.ID) {
		return nil, errControlFrame
	}
	switch grant.purpose {
	case "postgres-init-app", "postgres-init-w1", "postgres-init-w2", "postgres-init-control":
	default:
		return nil, errControlFrame
	}
	if b.effects == nil || b.effects.cgroups[grantID] != nil {
		return nil, errControlFrame
	}
	for _, owner := range b.effects.objectGrant {
		if owner == grantID {
			return nil, errControlFrame
		}
	}
	for _, tx := range b.effects.transactions {
		if tx != nil && tx.grantID == grantID {
			return nil, errControlFrame
		}
	}
	for _, inv := range b.runtimeInvocations {
		if inv != nil && inv.grantID == grantID {
			return nil, errControlFrame
		}
	}
	template, err := b.templateForPrincipal(p, grant.purpose)
	if err != nil || !sameTemplate(grant.config, template) {
		return nil, errControlFrame
	}
	now, err := monoNowNS()
	if err != nil || now >= authorityGuestDeadlineNS {
		return nil, errGuardProtocol
	}
	stage := map[string]any{"nonce": nonce, "grant_id": jsonU64(grantID), "class": class, "transaction_id": nil, "phase": "armed", "saved_soft": nil, "hard_limit": nil, "test_soft": nil, "original_fd_count": nil, "identity_fd_count": nil, "owned_fd_count": nil, "errno": nil}
	if err = preflightRuntimeRecord(b, "selftest_fd_stage", stage); err != nil {
		return nil, err
	}
	ack, err := controlACK(b.reads, p, requestID, raw, "OK", map[string]any{"armed": true})
	if err != nil {
		return nil, err
	}
	if err = appendRuntimeRecord(b, "selftest_fd_stage", stage); err != nil {
		return nil, err
	}
	if err = p.commitReply(requestID, raw, ack, nil); err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	if !replayOnly {
		if class == "map" {
			b.selftestMapArmed, b.selftestMapNonce, b.selftestMapGrant = true, nonce, grantID
		} else {
			b.selftestProgramArmed = true
		}
	}
	b.selftestFD = &selftestFDArm{nonce: nonce, class: class, owner: p.ID, grantID: grantID, deadline: authorityGuestDeadlineNS, phase: "armed", replayOnly: replayOnly}
	if err = b.emitFDCase(b.selftestFD, nil, "started"); err != nil {
		b.checkpoint.markFault()
		return nil, err
	}
	return ack, nil
}

func (b *authorityBroker) emitFDCase(stage *selftestFDArm, tx *runtimeEffectTransaction, phase string) error {
	if stage == nil || stage.replayOnly || b.caseFacts == nil || b.caseFacts["bpf_variant"] != nil && b.caseFacts["bpf_variant"] != "" && b.caseFacts["bpf_variant"] != "baseline" {
		return nil
	}
	s := &selftestBaselineStep{name: "fd-arm-" + stage.class, nonce: stage.nonce, grantID: stage.grantID}
	var actor any
	if tx != nil {
		if tx.id == 0 || tx.invocationID == 0 || tx.requesterCookie == 0 {
			return errGuardProtocol
		}
		s.invocationID, s.transactionID = tx.invocationID, tx.id
		actor = jsonU64(tx.requesterCookie)
		if phase == "settled" {
			s.result = jsonI64(-int64(unix.EMFILE))
		}
	}
	return appendRuntimeRecord(b, "selftest_case", s.record(phase, actor, nil, nil))
}
