//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"math"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type admitDiagnostic struct {
	stage    string
	observed error
}

// reject latches the first observed rejection; stage may hold provisional context.
func (d *admitDiagnostic) reject(stage string, observed error) {
	if d != nil && d.observed == nil && observed != nil {
		d.stage, d.observed = stage, observed
	}
}

type admitIOResult struct {
	probe      *ownerProbeObservation
	initWait   *initContainerObservation
	secretRec  *postgresServiceSecretRecord
	facts      *runtimeContainerFacts
	diagnostic admitDiagnostic
	err        error
}
type pendingAdmit struct {
	principal             *controlPrincipal
	daemon                *daemonRegistration
	requestID, generation uint64
	raw                   []byte
	purpose, cid, hash    string
	config                map[string]any
	deadline              time.Time
	eventFD               int
	result                chan admitIOResult
	connection            *brokerControlConnection
}

func (b *authorityBroker) pendingControlFD() int {
	if b == nil || b.pendingAdmit == nil {
		return -1
	}
	return b.pendingAdmit.eventFD
}

func (b *authorityBroker) startAdmitContainer(p *controlPrincipal, request map[string]any, raw []byte) error {
	if b == nil || p == nil || b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil || b.pendingAdmit != nil || !allowedOperation(p.Role, "ADMIT") || len(b.grants) >= 128 || !livePIDFD(p.PIDFD) {
		return errControlFrame
	}
	id, ok := exactNumber(request["request_id"])
	if !ok || id == 0 || id == math.MaxUint64 || id != p.NextRequest {
		return errControlFrame
	}
	body, ok := request["body"].(map[string]any)
	if !ok || !exactKeys(body, "purpose", "container_id", "config", "config_sha256") {
		return errControlFrame
	}
	purpose, ok := body["purpose"].(string)
	if !ok {
		return errControlFrame
	}
	if purpose == "container-bpf-selftest" {
		if p.Role != "REALM_ROOT" || b.caseFacts == nil || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" {
			return errControlFrame
		}
		variant := b.caseFacts["bpf_variant"]
		if variant != nil && variant != "" && variant != "baseline" {
			return errControlFrame
		}
	}
	cid, ok := body["container_id"].(string)
	if !ok || !runtimeContainerID(cid) {
		return errControlFrame
	}
	config, ok := body["config"].(map[string]any)
	if !ok {
		return errControlFrame
	}
	hash, ok := digestString(body["config_sha256"])
	if !ok {
		return errControlFrame
	}
	encoded, err := canonicalControl(config)
	if err != nil || hashBytes(encoded) != hash {
		return errControlFrame
	}
	config, err = parseCanonicalControl(encoded)
	if err != nil {
		return err
	}
	expected, err := b.templateForPrincipal(p, purpose)
	if err != nil || !sameTemplate(config, expected) {
		return errControlFrame
	}
	generation := uint64(1)
	for _, existing := range b.grants {
		if existing.containerID == cid {
			if existing.phase != "retired" || existing.configSHA != hash || existing.generation == 0 || existing.generation == math.MaxUint64 {
				return errGuardProtocol
			}
			if existing.generation >= generation {
				generation = existing.generation + 1
			}
		}
		if purpose == "container-bpf-selftest" && existing.purpose == purpose {
			return errGuardProtocol
		}
		if (purpose == "postgres-uid" || purpose == "postgres-gid") && existing.owner == p.ID && existing.purpose == purpose {
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
	eventFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return err
	}
	workerFD, err := unix.FcntlInt(uintptr(eventFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(eventFD)
		return err
	}
	pending := &pendingAdmit{principal: p, daemon: daemon, requestID: id, generation: generation, raw: append([]byte(nil), raw...), purpose: purpose, cid: cid, hash: hash, config: config, deadline: deadline, eventFD: eventFD, result: make(chan admitIOResult, 1)}
	var recovery *recoveryBinding
	var recoveryEntry recoveryContainer
	if p.Role == "RECOVERY_OBSERVER" {
		if b.recovery == nil || !strings.HasPrefix(purpose, "recovery-") {
			_ = unix.Close(workerFD)
			_ = unix.Close(eventFD)
			return errGuardProtocol
		}
		entry, ok := b.recovery.containers[strings.TrimPrefix(purpose, "recovery-")]
		if !ok || entry.id != cid {
			_ = unix.Close(workerFD)
			_ = unix.Close(eventFD)
			return errGuardProtocol
		}
		copyBinding := *b.recovery
		copyBinding.containers = make(map[string]recoveryContainer, len(b.recovery.containers))
		for k, v := range b.recovery.containers {
			copyBinding.containers[k] = v
		}
		recovery, recoveryEntry = &copyBinding, entry
	}
	pgUID, pgGID := b.postgresUID, b.postgresGID
	manifest, boot, daemonCopy := b.manifest, b.bootID, *daemon
	b.pendingAdmit = pending
	go func() {
		var result admitIOResult
		if purpose == "postgres-uid" || purpose == "postgres-gid" {
			result.diagnostic.stage = "ADMIT_INSPECT_OWNER_PROBE"
			result.err = inspectOwnerProbe(&daemonCopy, cid, purpose, deadline, &result.diagnostic)
			if result.err == nil {
				result.probe, result.err = openOwnerProbeObservation(&daemonCopy, cid, deadline, &result.diagnostic)
			}
		} else if recovery != nil {
			result.diagnostic.stage = "ADMIT_RECOVERY_BINDING"
			current, _, e := verifyRecoveryBinding(recovery.controlFD, recovery.seedFD, recovery.caseFD, manifest, boot, recovery.intentSHA, recovery.usedSHA, recovery.originalBoot)
			if e == nil && (current.uid != recovery.uid || current.gid != recovery.gid || current.credentialSHA != recovery.credentialSHA) {
				e = errGuardProtocol
			}
			if e == nil {
				for name, original := range recovery.containers {
					observed, ok := current.containers[name]
					if !ok || observed.id != original.id || !sameTemplate(observed.config, original.config) {
						e = errGuardProtocol
						break
					}
				}
			}
			if e == nil {
				result.diagnostic.stage = "ADMIT_INSPECT_RECOVERY_CONTAINER"
				e = inspectRecoveryContainer(&daemonCopy, recoveryEntry, deadline)
			}
			result.err = e
		}
		if result.err == nil {
			result.diagnostic.stage = "ADMIT_INSPECT_RUNTIME_CONTAINER"
			result.facts, result.err = inspectRuntimeContainer(&daemonCopy, cid, config, deadline, &result.diagnostic)
		}
		if result.err == nil && strings.HasPrefix(purpose, "postgres-init-") {
			svc := strings.TrimPrefix(purpose, "postgres-init-")
			result.diagnostic.stage = "ADMIT_INIT_SECRET_READ"
			pwSHA, secretStat, e := readAndCommitInitSecret(svc, pgUID, pgGID)
			if e != nil {
				result.err = e
			} else {
				result.diagnostic.stage = "ADMIT_INIT_PGDATA_OPEN"
				pgDirFD, pgStat, pgMntID, e := openHeldPGDataDir(svc, pgUID, pgGID)
				if e != nil {
					result.err = e
				} else {
					result.diagnostic.stage = "ADMIT_INIT_OBSERVATION_OPEN"
					initWait, e := openInitContainerObservation(&daemonCopy, cid, deadline)
					if e != nil {
						_ = unix.Close(pgDirFD)
						result.err = e
					} else {
						result.initWait = initWait
						result.secretRec = &postgresServiceSecretRecord{
							bootID:         boot,
							principalID:    p.ID,
							daemonCookie:   daemonCopy.cookie,
							service:        svc,
							uid:            pgUID,
							gid:            pgGID,
							pgdataDirFD:    pgDirFD,
							pgdataStat:     pgStat,
							pgdataMountID:  pgMntID,
							secretStat:     secretStat,
							initGeneration: generation,
							passwordSHA256: pwSHA,
							initWait:       initWait,
						}
					}
				}
			}
		}
		pending.result <- result
		var wake [8]byte
		binary.LittleEndian.PutUint64(wake[:], 1)
		_, _ = unix.Write(workerFD, wake[:])
		_ = unix.Close(workerFD)
	}()
	return nil
}

func (b *authorityBroker) finishAdmitContainer() (*controlPrincipal, []byte, bool, error) {
	if b == nil || b.pendingAdmit == nil {
		return nil, nil, false, errGuardProtocol
	}
	q := b.pendingAdmit
	var result admitIOResult
	select {
	case result = <-q.result:
	default:
		return q.principal, nil, false, nil
	}
	_ = unix.Close(q.eventFD)
	b.pendingAdmit = nil
	keep := false
	defer func() {
		if !keep {
			if result.probe != nil {
				result.probe.close()
			}
			if result.secretRec != nil {
				result.secretRec.close()
			} else if result.initWait != nil {
				result.initWait.close()
			}
		}
	}()
	if result.err != nil {
		observed := result.diagnostic.observed
		if observed == nil {
			observed = result.err
		}
		g2d1CaptureFailure(result.diagnostic.stage, observed)
		return q.principal, nil, true, result.err
	}
	if time.Until(q.deadline) <= 0 {
		g2d1CaptureFailure("ADMIT_DEADLINE", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	if b.checkpoint == nil || b.checkpoint.invalid || b.checkpoint.active != nil || b.pendingCheckpoint != nil {
		g2d1CaptureFailure("ADMIT_CHECKPOINT", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	if q.principal.Terminal || q.principal.Transferred || q.principal.NextRequest != q.requestID || !livePIDFD(q.principal.PIDFD) {
		g2d1CaptureFailure("ADMIT_PRINCIPAL_STATE", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	if !livePIDFD(q.daemon.pidfd) {
		g2d1CaptureFailure("ADMIT_DAEMON_PIDFD", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	if err := b.observeDaemonExec(q.daemon); err != nil {
		g2d1CaptureFailure("ADMIT_DAEMON_EXEC", err)
		return q.principal, nil, true, errGuardProtocol
	}
	current, err := observeProcess(q.principal.PID, q.principal.PIDFD)
	if err != nil || current.ExecutableSHA256 != q.principal.ExecutableSHA256 || current.ArgvSHA256 != q.principal.ArgvSHA256 || current.CgroupSHA256 != q.principal.CgroupSHA256 {
		g2d1CaptureFailure("ADMIT_PRINCIPAL_OBSERVATION", err)
		return q.principal, nil, true, errGuardProtocol
	}
	expected, err := b.templateForPrincipal(q.principal, q.purpose)
	if err != nil || !sameTemplate(q.config, expected) {
		g2d1CaptureFailure("ADMIT_TEMPLATE", err)
		return q.principal, nil, true, errGuardProtocol
	}
	for _, grant := range b.grants {
		if grant.containerID == q.cid && (grant.phase != "retired" || grant.configSHA != q.hash || grant.generation >= q.generation) {
			g2d1CaptureFailure("ADMIT_GRANT_REPLAY", errGuardProtocol)
			return q.principal, nil, true, errGuardProtocol
		}
	}
	if len(b.grants) >= 128 {
		g2d1CaptureFailure("ADMIT_GRANT_CAPACITY", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	if b.nextGrant == 0 {
		b.nextGrant = 1
	}
	if b.nextGrant == math.MaxUint64 {
		g2d1CaptureFailure("ADMIT_GRANT_SEQUENCE", errGuardProtocol)
		return q.principal, nil, true, errGuardProtocol
	}
	id := b.nextGrant
	b.nextGrant++
	g := &brokerGrant{id: id, owner: q.principal.ID, generation: q.generation, purpose: q.purpose, containerID: q.cid, config: q.config, configSHA: q.hash, phase: "admitted", retirement: "auto_empty", probe: result.probe, facts: result.facts}
	if strings.HasPrefix(q.purpose, "postgres-init-") {
		svc := strings.TrimPrefix(q.purpose, "postgres-init-")
		if result.secretRec == nil || recheckInitSecretBeforeStart(svc, result.secretRec.secretStat) != nil || recheckHeldPGDataDir(result.secretRec) != nil {
			g2d1CaptureFailure("ADMIT_INIT_RECHECK", errGuardProtocol)
			return q.principal, nil, true, errGuardProtocol
		}
		if b.serviceSecrets == nil {
			b.serviceSecrets = make(map[string]*postgresServiceSecretRecord)
		}
		if prior := b.serviceSecrets[svc]; prior != nil {
			prior.close()
			delete(b.serviceSecrets, svc)
		}
		result.secretRec.initGrantID = id
	} else if strings.HasPrefix(q.purpose, "postgres-live-") {
		svc := strings.TrimPrefix(q.purpose, "postgres-live-")
		if b.serviceSecrets == nil {
			g2d1CaptureFailure("ADMIT_LIVE_SECRET_STATE", errGuardProtocol)
			return q.principal, nil, true, errGuardProtocol
		}
		rec := b.serviceSecrets[svc]
		if rec == nil || rec.bootID != b.bootID || rec.principalID != q.principal.ID || rec.daemonCookie != q.daemon.cookie || rec.service != svc || rec.uid != b.postgresUID || rec.gid != b.postgresGID || !rec.initWaitCompleted || rec.handedOver || rec.liveGrantID != 0 || recheckHeldPGDataDir(rec) != nil {
			g2d1CaptureFailure("ADMIT_LIVE_RECHECK", errGuardProtocol)
			return q.principal, nil, true, errGuardProtocol
		}
	}
	ack, err := b.commitAdmittedGrant(q, g)
	if err != nil {
		return q.principal, nil, true, err
	}
	if strings.HasPrefix(q.purpose, "postgres-init-") && result.secretRec != nil {
		svc := strings.TrimPrefix(q.purpose, "postgres-init-")
		b.serviceSecrets[svc] = result.secretRec
		if svc == "app" && b.endpointGeneration == 0 {
			b.endpointGeneration = 1
		}
	} else if strings.HasPrefix(q.purpose, "postgres-live-") {
		svc := strings.TrimPrefix(q.purpose, "postgres-live-")
		if rec := b.serviceSecrets[svc]; rec != nil {
			rec.liveGrantID = id
			rec.liveGeneration = q.generation
			rec.handedOver = true
		}
	}
	keep = true
	return q.principal, ack, true, nil
}

// commitAdmittedGrant publishes the already validated grant and binds its
// diagnostic evidence to the control connection that authenticated ADMIT.
func (b *authorityBroker) commitAdmittedGrant(q *pendingAdmit, g *brokerGrant) ([]byte, error) {
	_, _, err := b.transcript.append("grant", map[string]any{"id": jsonU64(g.id), "owner": jsonU64(q.principal.ID), "purpose": q.purpose, "container_id": q.cid, "config": q.config, "config_sha256": q.hash, "invocation": jsonU64(q.generation), "phase": "admitted", "retirement": "auto_empty"})
	if err != nil {
		g2d1CaptureFailure("ADMIT_TRANSCRIPT", err)
		return nil, err
	}
	ack, err := controlACK(b.reads, q.principal, q.requestID, q.raw, "OK", map[string]any{"grant_id": jsonU64(g.id)})
	if err != nil {
		g2d1CaptureFailure("ADMIT_ACK", err)
		b.checkpoint.markFault()
		return nil, err
	}
	if err = q.principal.commitReply(q.requestID, q.raw, ack, nil); err != nil {
		g2d1CaptureFailure("ADMIT_COMMIT_REPLY", err)
		b.checkpoint.markFault()
		return nil, err
	}
	if b.grants == nil {
		b.grants = make(map[uint64]*brokerGrant)
	}
	b.grants[g.id] = g
	if b.diagnostic != nil {
		b.diagnostic.issue(b, g)
		if e := b.diagnostic.grantFor(g); e != nil {
			if q.connection == nil {
				e.gap |= settlementGapUnbound
			} else {
				q.connection.associateEvidence(e)
			}
		}
	}
	return ack, nil
}
