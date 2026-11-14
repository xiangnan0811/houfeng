//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"

	"golang.org/x/sys/unix"
)

// runtimeEffects owns effect state for the lifetime of the original broker.
// Descriptors in objects and cgroups are retained until their actual lifecycle
// events have been accounted for; the task's descriptor number is never used as
// the authority to execute a later syscall.
type runtimeEffects struct {
	nextTransaction   uint64
	transactions      map[uint64]*runtimeEffectTransaction
	objects           *objectTracker
	cgroups           map[uint64]*liveCgroup
	objectFD          map[uint64]int
	objectGrant       map[uint64]uint64
	registry          *effectRegistry
	attachments       map[uint64]*runtimeAttachment
	nextAttachment    uint64
	enrollmentPending map[uint64]int
	grantCleanup      map[uint64]*grantObjectCleanup
	aliases           map[uint64]*brokerBorrowedAlias
	cleanupChild      *runtimeEffectTransaction
	pending           *runtimeEffectTransaction
}

type runtimeEffectTransaction struct {
	id, grantID, invocationID, requesterCookie, notificationID uint64
	command                                                    string
	attributes                                                 []byte
	program, license                                           any
	inputs                                                     []any
	effectFD                                                   int
	effectExecuted                                             bool
	effectErrno                                                unix.Errno
	cancelRecorded                                             bool
	policyErr                                                  error
	cleanupActive                                              bool
	executionReleased                                          bool
	responseUndelivered                                        bool
	internalDetach                                             bool
	internalSelftest                                           bool
	cleanupParent                                              *runtimeEffectTransaction
	cleanupAttachment                                          uint64
	publication                                                *guardRecord
	state                                                      string
	result, returnedFD, output                                 any
	cleanup                                                    map[string]any
}

func newRuntimeEffects(b *authorityBroker) (*runtimeEffects, error) {
	if b == nil || b.authority == nil || b.authority.recorder == nil || b.transcript == nil || b.authority.recorder.nextID == 0 || b.authority.recorder.nextID == math.MaxUint64 {
		return nil, errGuardProtocol
	}
	return &runtimeEffects{nextTransaction: b.authority.recorder.nextID, transactions: make(map[uint64]*runtimeEffectTransaction), objects: newObjectTracker(b.transcript), cgroups: make(map[uint64]*liveCgroup), objectFD: make(map[uint64]int), objectGrant: make(map[uint64]uint64), attachments: make(map[uint64]*runtimeAttachment), nextAttachment: 1}, nil
}

// initializeRuntimeEffects accepts the PID1 task and executable only after
// the caller has independently compared the held file with pinned systemd.
func (b *authorityBroker) initializeRuntimeEffects(systemdTask guardTask, systemdFD int, systemdStat unix.Stat_t) error {
	if b == nil || b.effects != nil || systemdTask.Cookie == 0 || systemdTask.TGID != 1 || systemdTask.TID != 1 || systemdTask.StartBoottimeNS == 0 || systemdFD < 0 || systemdStat.Mode&unix.S_IFMT != unix.S_IFREG || systemdStat.Uid != 0 || systemdStat.Gid != 0 || systemdStat.Mode&0022 != 0 {
		return errGuardProtocol
	}
	var actual unix.Stat_t
	if unix.Fstat(systemdFD, &actual) != nil || !sameTrustedStat(actual, systemdStat) {
		return errGuardProtocol
	}
	fd, err := unix.FcntlInt(uintptr(systemdFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	effects, err := newRuntimeEffects(b)
	if err != nil {
		_ = unix.Close(fd)
		return err
	}
	registry, err := newEffectRegistry(b, systemdTask, fd, systemdStat)
	if err != nil {
		_ = unix.Close(fd)
		return err
	}
	effects.registry = registry
	b.effects = effects
	return nil
}

func (t *runtimeEffectTransaction) body(executor uint64, state string, result, returnedFD, output any) map[string]any {
	var invocation, requester, notification, grant any
	if !t.internalDetach && !t.internalSelftest {
		invocation, requester, notification = jsonU64(t.invocationID), jsonU64(t.requesterCookie), jsonU64(t.notificationID)
	}
	if !t.internalSelftest || t.grantID != 0 {
		grant = jsonU64(t.grantID)
	}
	return map[string]any{
		"id": jsonU64(t.id), "grant_id": grant, "invocation_id": invocation, "requester_cookie": requester, "notification_id": notification,
		"command": t.command, "attributes": map[string]any{"attr_size": jsonU64(uint64(len(t.attributes))), "attr_hex": hex.EncodeToString(t.attributes)},
		"program_bytes": t.program, "license": t.license, "input_objects": t.inputs, "executor_cookie": jsonU64(executor),
		"state": state, "result": result, "returned_fd": returnedFD, "output": output, "cleanup": t.cleanup,
	}
}

// preflightRuntimeRecord reserves representability both as a transcript record
// and as a whole READ_EVIDENCE response before any BPF effect is committed.
func preflightRuntimeRecord(b *authorityBroker, kind string, body map[string]any) error {
	if b == nil || b.transcript == nil || body == nil {
		return errGuardProtocol
	}
	record := map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "epoch": b.epoch, "sequence": jsonU64(math.MaxUint64), "previous": strings.Repeat("f", 64), "kind": kind, "body": body}
	raw, err := canonicalControl(record)
	if err != nil || len(raw)+1 > maxAuthorityTranscriptBytes-b.transcript.total {
		return errControlFrame
	}
	ack := map[string]any{"version": json.Number("1"), "boot_id": b.bootID, "epoch": b.epoch, "run_token": authorityRunToken, "principal_id": jsonU64(math.MaxUint64), "request_id": jsonU64(math.MaxUint64), "request_sha256": strings.Repeat("f", 64), "status": "OK", "body": map[string]any{"records": []any{record}, "next": jsonU64(math.MaxUint64), "complete": false}}
	_, err = canonicalControl(ack)
	return err
}

func appendRuntimeRecord(b *authorityBroker, kind string, body map[string]any) error {
	if err := preflightRuntimeRecord(b, kind, body); err != nil {
		return err
	}
	raw, err := canonicalControl(body)
	if err != nil {
		return err
	}
	frozen, err := parseCanonicalControl(raw)
	if err != nil {
		return err
	}
	_, _, err = b.transcript.append(kind, frozen)
	return err
}

func (b *authorityBroker) prepareRuntimeEffect(inv *runtimeInvocation, h *heldNotification, command string, attr []byte, program, license any, inputs []any, maximumOutput any) (*runtimeEffectTransaction, error) {
	if b == nil || b.effects == nil || b.resolver == nil || b.checkpoint == nil || b.checkpoint.lifecycle == nil || b.checkpoint.invalid || b.pendingCheckpoint != nil || b.checkpoint.active != nil || b.checkpoint.lifecycle.paused || b.effects.pending != nil || inv == nil || inv.grant == nil || h == nil || h.task.Cookie == 0 || h.notice.ID == 0 || command == "" || len(attr) == 0 || len(attr) > 4096 || inputs == nil || len(b.effects.transactions) >= 4096 {
		return nil, errGuardProtocol
	}
	owner := b.principals[inv.grant.owner]
	if owner == nil || owner.Terminal || owner.Transferred || !livePIDFD(owner.PIDFD) || inv.grant.phase == "retired" || inv.grant.phase == "revoked" || inv.grant.phase == "settling" && inv.command != "delete" {
		return nil, errGuardProtocol
	}
	if b.selftestFD != nil {
		now, err := monoNowNS()
		if err != nil || now >= b.selftestFD.deadline {
			b.checkpoint.markFault()
			return nil, errGuardProtocol
		}
		if b.selftestFD.phase != "armed" || inv.grantID != b.selftestFD.grantID {
			return nil, errGuardProtocol
		}
	}
	id := b.effects.nextTransaction
	if id == 0 || id == math.MaxUint64 {
		return nil, errGuardProtocol
	}
	t := &runtimeEffectTransaction{id: id, grantID: inv.grantID, invocationID: inv.id, requesterCookie: h.task.Cookie, notificationID: h.notice.ID, command: command, attributes: append([]byte(nil), attr...), program: program, license: license, inputs: inputs, state: "prepared", effectFD: -1, cleanup: map[string]any{"policy": "none", "state": "not_needed", "steps": []any{}}}
	if err := preflightRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "effect_observed", jsonI64(math.MinInt64), jsonU64(math.MaxInt32), maximumOutput)); err != nil {
		return nil, err
	}
	if err := appendRuntimeRecord(b, "transaction", t.body(b.resolver.brokerCookie, "prepared", nil, nil, nil)); err != nil {
		return nil, err
	}
	b.effects.nextTransaction++
	b.effects.transactions[id] = t
	b.effects.pending = t
	b.checkpoint.activeTransactions++
	return t, nil
}

func (b *authorityBroker) transitionRuntimeEffect(t *runtimeEffectTransaction, state string, result, returnedFD, output any) error {
	if b == nil || b.effects == nil || b.checkpoint == nil || t == nil || b.effects.transactions[t.id] != t {
		return errGuardProtocol
	}
	cancelled := t.state == "prepared" && state == "retired" && t.cancelRecorded && !t.effectExecuted && t.publication == nil
	valid := t.state == "prepared" && state == "committed" || t.state == "committed" && state == "effect_observed" || t.state == "effect_observed" && state == "response_settled" || t.state == "response_settled" && state == "retired" || cancelled
	if !valid || (state == "committed" || cancelled) && (result != nil || returnedFD != nil || output != nil) || state != "committed" && !cancelled && result == nil {
		return errGuardProtocol
	}
	if t.command == "MAP_CREATE" || t.command == "PROG_LOAD" {
		if state != "committed" && !cancelled {
			if !t.effectExecuted || (t.effectFD >= 0) == (t.effectErrno != 0) {
				return errGuardProtocol
			}
			actual := int64(t.effectFD)
			if t.effectErrno != 0 {
				actual = -int64(t.effectErrno)
			}
			if result != jsonI64(actual) || t.effectFD >= 0 && t.publication == nil {
				return errGuardProtocol
			}
			if state == "retired" && (t.effectFD < 0 && t.publication != nil || t.cleanupActive) && t.cleanup["state"] != "settled" {
				return errGuardProtocol
			}
		}
	}
	stage := b.selftestFD
	if state == "retired" && stage != nil && stage.transactionID == t.id {
		if stage.phase != "restored" || t.publication == nil || !t.effectExecuted || t.effectErrno != unix.EMFILE || t.effectFD >= 0 || result != jsonI64(-int64(unix.EMFILE)) || returnedFD != nil || t.cleanup["state"] != "settled" || b.join == nil || !b.join.settled() {
			return errGuardProtocol
		}
		object := b.effects.objects.byCookie[t.publication.ObjectCookie]
		if object == nil || object.removalSeq == 0 || object.id != t.publication.KernelID {
			return errGuardProtocol
		}
	}
	if state == "retired" {
		if t.executionReleased {
			if t.command != "PROG_GET_FD_BY_ID" || t.cleanup["policy"] != "close_borrowed_ref" || t.cleanup["state"] != "settled" {
				return errGuardProtocol
			}
		} else if b.effects.pending != t || b.checkpoint.activeTransactions == 0 {
			return errGuardProtocol
		}
	}
	body := t.body(b.resolver.brokerCookie, state, result, returnedFD, output)
	if err := appendRuntimeRecord(b, "transaction", body); err != nil {
		return err
	}
	t.state, t.result, t.returnedFD, t.output = state, result, returnedFD, output
	if state == "retired" {
		if !t.executionReleased {
			b.effects.pending = nil
			b.checkpoint.activeTransactions--
		}
		if stage != nil && stage.transactionID == t.id {
			if stage.class == "map" {
				b.selftestMapDone, b.selftestMapGrant, b.selftestMapNonce = true, stage.grantID, stage.nonce
			} else {
				b.selftestProgramDone = true
			}
			b.selftestFD = nil
		}
		if rtStage := b.selftestRuntime; rtStage != nil && rtStage.transactionID == t.id {
			if err := b.emitSelftestRuntimeStage(rtStage, "settled"); err != nil {
				return err
			}
			b.selftestRuntime = nil
		}
	}
	return nil
}
