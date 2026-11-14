//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"math"
	"strings"

	"golang.org/x/sys/unix"
)

type ownerProbeResult struct {
	value uint32
	err   error
}
type pendingBrokerSettlement struct {
	principal                                   *controlPrincipal
	grant                                       *brokerGrant
	requestID                                   uint64
	raw                                         []byte
	firstRecovery                               bool
	diagnostic                                  ownerProbeSettlementReceipt
	diagnosticEvents                            uint8
	sampleBaseNS                                uint64
	sampleArmed                                 bool
	lastWait                                    byte
	followupObject                              *objectIncarnation
	followupGrant                               *brokerGrant
	followupCookie, followupID, followupCreator uint64
	followupClass                               string
}

func (q *pendingBrokerSettlement) observe(event, point byte, detail ownerProbeRemovalDiagnostic) {
	if q == nil || !q.diagnostic.valid || event < 1 || event > 3 || q.diagnosticEvents&(1<<event) != 0 ||
		(event == 2 && (point < 1 || point > 13)) || (event != 2 && point != 0) {
		return
	}
	q.diagnosticEvents |= 1 << event
	r := q.diagnostic
	r.send(event, point, detail)
}

func (q *pendingBrokerSettlement) sampleReady(now uint64, point byte) bool {
	if q == nil || !q.diagnostic.valid || point < 1 || point > 13 || q.diagnosticEvents&(1<<2) != 0 {
		return false
	}
	if !q.sampleArmed {
		q.sampleBaseNS, q.sampleArmed = now, true
		return false
	}
	return now >= q.sampleBaseNS && now-q.sampleBaseNS >= 1_000_000_000
}

func (q *pendingBrokerSettlement) sample(now uint64, point byte) {
	if q != nil {
		q.lastWait = point
	}
	if q.sampleReady(now, point) {
		q.observe(2, point, ownerProbeRemovalDiagnostic{})
	}
}

func (q *pendingBrokerSettlement) sampleRemoval(now uint64, b *authorityBroker, cookie uint64, object *objectIncarnation) {
	if q != nil {
		q.lastWait = 10
	}
	if q.sampleReady(now, 10) {
		if b != nil && q.grant != nil && object != nil && cookie != 0 && object.cookie == cookie && b.effects != nil && b.effects.objects != nil && b.grants[q.grant.id] == q.grant && b.effects.objects.byCookie[cookie] == object {
			if owner, present := b.effects.objectGrant[cookie]; present && owner == q.grant.id {
				q.followupGrant = q.grant
				q.followupObject, q.followupCookie, q.followupID, q.followupCreator, q.followupClass = object, cookie, object.id, object.ownerTransaction, object.class
			}
		}
		q.observe(2, 10, b.ownerProbeRemovalSnapshot(q.grant, cookie, object))
	}
}

func (b *authorityBroker) settlementDiagnosticEligible(q *pendingBrokerSettlement) bool {
	return b != nil && q != nil && q.principal != nil && q.grant != nil &&
		q.principal.Role == "BOOT_OWNER" && q.principal.ID == b.bootOwner && b.bootOwner != 0 &&
		q.grant.owner == b.bootOwner && b.grants[q.grant.id] == q.grant &&
		q.grant.outcome == "completed" && q.grant.settlementRequests == 1 && !q.firstRecovery &&
		runtimeContainerID(q.grant.containerID) && (q.grant.purpose == "postgres-uid" || q.grant.purpose == "postgres-gid")
}

func (b *authorityBroker) emitGrantPhase(g *brokerGrant) error {
	return appendRuntimeRecord(b, "grant", map[string]any{"id": jsonU64(g.id), "owner": jsonU64(g.owner), "purpose": g.purpose, "container_id": g.containerID, "config": g.config, "config_sha256": g.configSHA, "invocation": jsonU64(g.generation), "phase": g.phase, "retirement": g.retirement})
}

// startBrokerSettlement authenticates the owner and records a settling phase;
// the eventual ACK is delayed until actual cleanup is observed, except the
// first recovery request, which starts settlement before stopping a live VM.
func (b *authorityBroker) startBrokerSettlement(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || b.effects == nil || b.checkpoint == nil || b.checkpoint.invalid || b.pendingSettlement != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || b.pendingExecOp != nil || !allowedOperation(p.Role, "SETTLE") || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	id, ok := exactNumber(request["request_id"])
	if !ok || id == 0 || id == math.MaxUint64 || id != p.NextRequest {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "grant_id", "outcome") {
		return errControlFrame
	}
	grantID, ok := exactNumber(body["grant_id"])
	if !ok || grantID == 0 {
		return errControlFrame
	}
	outcome, ok := body["outcome"].(string)
	if !ok || outcome != "completed" && outcome != "failed" && outcome != "cancelled" {
		return errControlFrame
	}
	g := b.grants[grantID]
	if g == nil || g.owner != p.ID || g.phase != "admitted" && g.phase != "active" && g.phase != "settling" || g.outcome != "" && g.outcome != outcome {
		return errControlFrame
	}
	if g.phase == "admitted" {
		if outcome == "completed" {
			return errGuardProtocol
		}
		failed := 0
		for _, inv := range b.runtimeInvocations {
			if inv.grantID == grantID && inv.command == "create" && inv.state == "failed" {
				failed++
			}
		}
		if failed != 1 {
			return errGuardProtocol
		}
	}
	if g.phase == "active" && outcome == "completed" && (!g.createSettled || !g.actualQueryConfirmed || !g.startObserved) {
		return errGuardProtocol
	}
	firstRecovery := p.Role == "RECOVERY_OBSERVER" && g.settlementRequests == 0 && g.phase == "active"
	if g.exec != nil && !g.exec.settled && g.exec.state != "cancelled" && g.exec.state != "settled" {
		if g.exec.state == "admitted" && !g.exec.consumed {
			g.exec.state = "cancelled"
			g.exec.settled = true
			g.exec.closeHeld()
			if err := b.emitExecAdmission(g.exec); err != nil {
				b.checkpoint.markFault()
				return err
			}
		}
	}
	if g.phase != "settling" {
		g.phase, g.outcome = "settling", outcome
		if err := b.emitGrantPhase(g); err != nil {
			b.checkpoint.markFault()
			return err
		}
	}
	g.settlementRequests++
	q := &pendingBrokerSettlement{principal: p, grant: g, requestID: id, raw: append([]byte(nil), raw...), firstRecovery: firstRecovery}
	b.pendingSettlement = q
	if b.settlementDiagnosticEligible(q) {
		purpose := byte(1)
		if g.purpose == "postgres-gid" {
			purpose = 2
		}
		q.diagnostic = ownerProbeSettlementReceipt{cid: g.containerID, purpose: purpose, valid: true}
		q.observe(1, 0, ownerProbeRemovalDiagnostic{})
	}
	return nil
}

func (b *authorityBroker) advanceBrokerSettlement() (*controlPrincipal, []byte, bool, error) {
	if b == nil || b.pendingSettlement == nil || b.effects == nil || b.checkpoint == nil {
		return nil, nil, false, errGuardProtocol
	}
	q := b.pendingSettlement
	q.lastWait = 0
	p, g := q.principal, q.grant
	if b.checkpoint.invalid || p.Terminal || p.Transferred || !livePIDFD(p.PIDFD) {
		return p, nil, false, errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil || now >= authorityGuestDeadlineNS {
		b.checkpoint.markFault()
		return p, nil, false, errGuardProtocol
	}
	if q.firstRecovery {
		return b.finishBrokerSettlement(q, "settling")
	}
	if b.selftestFD != nil || b.effects.pending != nil {
		q.sample(now, 1)
		return p, nil, false, nil
	}
	for _, alias := range b.effects.aliases {
		if alias != nil && alias.grantID == g.id && !alias.closed && (alias.transaction == nil || !alias.transaction.executionReleased) {
			q.sample(now, 2)
			return p, nil, false, nil
		}
	}
	if err = b.closeGrantAliases(g); err != nil {
		b.checkpoint.markFault()
		return p, nil, false, err
	}
	var nextCookie uint64
	for cookie, owner := range b.effects.objectGrant {
		if owner != g.id || b.effects.grantCleanup[cookie] != nil {
			continue
		}
		object := b.effects.objects.byCookie[cookie]
		if object == nil {
			b.checkpoint.markFault()
			return p, nil, false, errGuardProtocol
		}
		if object.removalSeq != 0 {
			continue
		}
		if _, held := b.effects.objectFD[cookie]; !held {
			continue
		}
		if nextCookie == 0 || cookie < nextCookie {
			nextCookie = cookie
		}
	}
	if nextCookie != 0 {
		if err = b.beginGrantObjectCleanup(g, nextCookie); err != nil {
			b.checkpoint.markFault()
			return p, nil, false, err
		}
		q.sample(now, 3)
		return p, nil, false, nil
	}
	if group := b.effects.cgroups[g.id]; group != nil && !group.Retired {
		if b.effects.registry == nil {
			b.checkpoint.markFault()
			return p, nil, false, errGuardProtocol
		}
		target := b.effects.registry.targets[cgroupKey{group.RootCookie, group.KernfsID}]
		if target == nil {
			if group.OfflineSeq == 0 || group.ReleaseSeq == 0 {
				q.sample(now, 4)
				return p, nil, false, nil
			}
			observed, retireErr := b.authority.guard.retireCgroup(group.Enrollment)
			if errors.Is(retireErr, unix.EAGAIN) {
				q.sample(now, 5)
				return p, nil, false, nil
			}
			if retireErr != nil || observed.State != 2 || observed.Sequence != group.ReleaseSeq {
				b.checkpoint.markFault()
				return p, nil, false, errGuardProtocol
			}
			if err = unix.Close(group.HeldFD); err != nil {
				b.checkpoint.markFault()
				return p, nil, false, err
			}
			group.HeldFD, group.Retired = -1, true
		} else {
			if target.phase == 2 {
				if err = b.beginEffectCgroupSettlement(group, g); err != nil {
					b.checkpoint.markFault()
					return p, nil, false, err
				}
			}
			if target.offlineSequence == 0 || target.releaseSequence == 0 {
				q.sample(now, 6)
				return p, nil, false, nil
			}
			if err = b.settleEffectCgroup(group, g); err != nil {
				b.checkpoint.markFault()
				return p, nil, false, err
			}
		}
	}
	for _, inv := range b.runtimeInvocations {
		if inv.grantID == g.id && (inv.state != "settled" && inv.state != "failed" || livePIDFD(inv.pidfd) || b.runtimeBinds[inv.taskCookie] != nil) {
			q.sample(now, 7)
			return p, nil, false, nil
		}
	}
	if g.exec != nil && !g.exec.settled {
		q.sample(now, 8)
		return p, nil, false, nil
	}
	for _, tx := range b.effects.transactions {
		if tx.grantID == g.id && tx.state != "retired" {
			q.sample(now, 9)
			return p, nil, false, nil
		}
	}
	for cookie, owner := range b.effects.objectGrant {
		if owner == g.id {
			object := b.effects.objects.byCookie[cookie]
			if object == nil || object.removalSeq == 0 {
				q.sampleRemoval(now, b, cookie, object)
				return p, nil, false, nil
			}
		}
	}
	for _, a := range b.effects.attachments {
		if a.grantID == g.id && a.state != "released" {
			q.sample(now, 11)
			return p, nil, false, nil
		}
	}
	if g.probe != nil && !g.probeCompleted {
		if g.outcome != "completed" {
			g.probe.close()
			g.probe = nil
			g.probeCompleted = true
		} else {
			if !g.startObserved {
				return p, nil, false, errGuardProtocol
			}
			if g.probeResult == nil {
				g.probeResult = make(chan ownerProbeResult, 1)
				probe, result := g.probe, g.probeResult
				go func() { value, e := probe.finish(true); probe.close(); result <- ownerProbeResult{value, e} }()
			}
			select {
			case result := <-g.probeResult:
				if result.err != nil || result.value == 0 {
					b.checkpoint.markFault()
					return p, nil, false, errGuardProtocol
				}
				if g.purpose == "postgres-uid" {
					b.postgresUID = result.value
				} else if g.purpose == "postgres-gid" {
					b.postgresGID = result.value
				} else {
					b.checkpoint.markFault()
					return p, nil, false, errGuardProtocol
				}
				g.probe, g.probeResult, g.probeCompleted = nil, nil, true
			default:
				q.sample(now, 12)
				return p, nil, false, nil
			}
		}
	}
	if strings.HasPrefix(g.purpose, "postgres-init-") && !g.initCompleted {
		svc := strings.TrimPrefix(g.purpose, "postgres-init-")
		rec := b.serviceSecrets[svc]
		if g.outcome != "completed" {
			if rec != nil {
				rec.close()
				delete(b.serviceSecrets, svc)
			}
			g.initCompleted = true
		} else {
			if !g.startObserved || rec == nil || rec.initGrantID != g.id || rec.initWait == nil || recheckHeldPGDataDir(rec) != nil {
				b.checkpoint.markFault()
				return p, nil, false, errGuardProtocol
			}
			if g.initResult == nil {
				ownerID := g.owner
				if p.Role == "REALM_ROOT" {
					ownerID = b.bootOwner
				}
				daemon := b.daemons[ownerID]
				if daemon == nil || !livePIDFD(daemon.pidfd) {
					b.checkpoint.markFault()
					return p, nil, false, errGuardProtocol
				}
				g.initResult = make(chan initSettlementResult, 1)
				waitObs, ch := rec.initWait, g.initResult
				daemonCopy, cid, cfg := *daemon, g.containerID, g.config
				go func() {
					e := waitObs.finish(&daemonCopy, cid, cfg, true)
					waitObs.close()
					ch <- initSettlementResult{err: e}
				}()
			}
			select {
			case res := <-g.initResult:
				rec.initWait = nil
				if res.err != nil || recheckHeldPGDataDir(rec) != nil {
					rec.close()
					delete(b.serviceSecrets, svc)
					b.checkpoint.markFault()
					return p, nil, false, errGuardProtocol
				}
				rec.initWaitCompleted = true
				g.initResult, g.initCompleted = nil, true
			default:
				q.lastWait = 14
				return p, nil, false, nil
			}
		}
	}
	if b.join == nil || !b.join.settled() {
		q.sample(now, 13)
		return p, nil, false, nil
	}
	if g.createBound {
		if g.bundleDirFD < 0 || g.bundleConfigFD < 0 || g.bundleDirFD == g.bundleConfigFD {
			return p, nil, false, errGuardProtocol
		}
		for _, fd := range []int{g.bundleDirFD, g.bundleConfigFD} {
			if err = unix.Close(fd); err != nil {
				b.checkpoint.markFault()
				return p, nil, false, err
			}
		}
		g.bundleDirFD, g.bundleConfigFD = -1, -1
	}
	if !strings.HasPrefix(g.purpose, "postgres-init-") || g.outcome != "completed" {
		b.clearServiceSecretForGrant(g)
	}
	g.phase = "retired"
	if err = b.emitGrantPhase(g); err != nil {
		b.checkpoint.markFault()
		return p, nil, false, err
	}
	return b.finishBrokerSettlement(q, "retired")
}

func (b *authorityBroker) finishBrokerSettlement(q *pendingBrokerSettlement, state string) (*controlPrincipal, []byte, bool, error) {
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"state": state})
	if err == nil {
		err = q.principal.commitReply(q.requestID, q.raw, ack, nil)
	}
	if err != nil {
		b.checkpoint.markFault()
		return q.principal, nil, false, err
	}
	if state == "retired" && b.pendingSettlement == q && q.grant.phase == "retired" && b.settlementDiagnosticEligible(q) &&
		q.diagnostic.cid == q.grant.containerID && (q.grant.purpose == "postgres-uid" && q.diagnostic.purpose == 1 || q.grant.purpose == "postgres-gid" && q.diagnostic.purpose == 2) {
		q.observe(3, 0, ownerProbeRemovalDiagnostic{})
		q.followup(b, 2)
	}
	b.pendingSettlement = nil
	return q.principal, ack, true, nil
}
