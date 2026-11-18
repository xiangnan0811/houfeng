//go:build linux && amd64 && !cgo

package main

import (
	"encoding/base64"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// triggerObservationFaultStimulus arms only the named fixed case, after the
// authenticated owner and case file have already been checked by the caller.
func (b *authorityBroker) triggerObservationFaultStimulus(variant string) error {
	if b == nil || b.join == nil || b.join.fault != nil || b.checkpoint == nil || b.checkpoint.invalid || b.caseFacts == nil || b.caseFacts["run_token"] != authorityRunToken || b.caseFacts["case"] != "realm-negative" || b.caseFacts["scenario"] != "bpf-authorization" || b.caseFacts["bpf_variant"] != variant {
		return errGuardProtocol
	}
	fault := &observationFaultState{variant: variant}
	switch variant {
	case "snapshot-missing", "snapshot-bad-field":
		fault.mapEligible = func(r guardRecord) bool {
			if b.effects == nil || b.effects.pending == nil || b.resolver == nil {
				return false
			}
			t := b.effects.pending
			return t.state == "committed" && t.command == "MAP_CREATE" && t.effectExecuted && t.effectFD >= 0 && t.effectErrno == 0 && t.publication == nil && featureMapAttribute(t.attributes) && r.Kind == 3 && r.TaskCookie == b.resolver.brokerCookie && guardU32(r.Snapshot[:], 0) == unix.BPF_MAP_TYPE_ARRAY && guardU32(r.Snapshot[:], 24) == 4 && guardU32(r.Snapshot[:], 28) == 4 && guardU32(r.Snapshot[:], 32) == 1
		}
	case "audit-loss", "audit-duplicate":
		fault.auditActor = func(pid uint64) bool {
			matches := 0
			for _, actor := range b.probeActors {
				if actor != nil && actor.name == "raw-denial" && uint64(actor.tgid) == pid && pidfdSelects(actor.pidfd, actor.tgid) {
					matches++
				}
			}
			return matches == 1
		}
	case "audit-overflow":
	default:
		return errGuardProtocol
	}
	b.join.fault = fault
	return nil
}

// failObservationFrame lowers the native authority only after an actual
// detector recorded its cause. The immutable first cause is never replaced.
func (b *authorityBroker) failObservationFrame(frame map[string]any, original, modified []byte) error {
	if b == nil || b.join == nil || b.join.fault == nil || b.join.fault.detector == "" || b.checkpoint == nil || b.authority == nil || b.observerSocketFD < 0 || frame["variant"] != b.join.fault.variant || frame["detector"] != b.join.fault.detector {
		return errGuardProtocol
	}
	if len(original)+len(modified) > 32768 {
		return errGuardProtocol
	}
	b.checkpoint.markFault()
	if err := b.authority.guard.revoke(); err != nil {
		return err
	}
	deadline, err := guestDeadline(5 * time.Second)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		role string
		data []byte
	}{{"original", original}, {"modified", modified}} {
		if len(item.data) == 0 {
			continue
		}
		if len(item.data) > 32768 {
			return errGuardProtocol
		}
		count := (len(item.data) + 8191) / 8192
		for i := range count {
			end := (i + 1) * 8192
			if end > len(item.data) {
				end = len(item.data)
			}
			page := map[string]any{"version": jsonU64(1), "boot_id": b.bootID, "kind": "broker_witness_chunk", "variant": b.join.fault.variant, "request_id": frame["request_id"], "nonce": frame["nonce"], "role": item.role, "page_index": jsonU64(uint64(i)), "page_count": jsonU64(uint64(count)), "total_bytes": jsonU64(uint64(len(item.data))), "total_sha256": hashBytes(item.data), "data_b64": base64.StdEncoding.EncodeToString(item.data[i*8192 : end])}
			if err = earlyProbeSend(b.observerSocketFD, page, nil, deadline); err != nil {
				return err
			}
		}
	}
	if err = earlyProbeSend(b.observerSocketFD, frame, nil, deadline); err != nil {
		return err
	}
	end, err := guestDeadline(30 * time.Second)
	if err != nil || b.observerPIDFD < 0 {
		return errGuardProtocol
	}
	if err = waitFD(b.observerPIDFD, unix.POLLIN, end); err != nil {
		return err
	}
	return errGuardProtocol
}

func (b *authorityBroker) detectedObservationFrame() (map[string]any, error) {
	if b == nil || b.join == nil || b.join.fault == nil || b.join.fault.detector == "" {
		return nil, errGuardProtocol
	}
	f := b.join.fault
	now, err := monoNowNS()
	if err != nil {
		return nil, err
	}
	frame := map[string]any{"version": jsonU64(1), "boot_id": b.bootID, "kind": "broker_receive_observation", "variant": f.variant, "source": nil, "detector": f.detector, "request_id": nil, "nonce": nil, "input_sha256": nil, "input_bytes": nil, "original_sha256": nil, "expected_actor": nil, "observed_actor": nil, "deadline_ns": nil, "observed_ns": jsonU64(now), "capacity": nil, "count_before": nil, "rejected_wire": nil, "guard_sequence": nil, "receive_observation": nil}
	if f.variant == "snapshot-missing" || f.variant == "snapshot-bad-field" {
		if len(f.original) != guardRecordSize || len(f.modified) != guardRecordSize || f.guardSequence == 0 {
			return nil, errGuardProtocol
		}
		frame["source"], frame["input_sha256"], frame["input_bytes"] = "broker_guard_copy", hashBytes(f.modified), jsonU64(uint64(len(f.modified)))
		frame["original_sha256"], frame["guard_sequence"] = hashBytes(f.original), jsonU64(f.guardSequence)
		return frame, nil
	}
	if f.audit == nil {
		return nil, errGuardProtocol
	}
	a := f.audit
	occurrence := uint64(1)
	if f.detector == "audit_duplicate" {
		occurrence = 2
	}
	frame["source"], frame["input_sha256"] = "broker_audit_delivery", a.PayloadSHA256
	frame["rejected_wire"] = map[string]any{"stamp": a.Key.stamp(), "serial": jsonU64(a.Serial), "record_type": jsonU64(uint64(a.Kind)), "payload_sha256": a.PayloadSHA256, "occurrence": jsonU64(occurrence)}
	if f.actorPID != 0 {
		frame["expected_actor"] = jsonU64(f.actorPID)
	}
	if a.PID != 0 {
		frame["observed_actor"] = jsonU64(a.PID)
	}
	if f.detector == "audit_wire_capacity" {
		frame["capacity"], frame["count_before"] = jsonU64(maxAuditWireIdentities), jsonU64(f.countBefore)
	}
	return frame, nil
}

func (b *authorityBroker) reportDetectedObservation() error {
	frame, err := b.detectedObservationFrame()
	if err != nil {
		return err
	}
	return b.failObservationFrame(frame, b.join.fault.original, b.join.fault.modified)
}

// drainGuardEvents first commits each actual record to the transcript and only
// acknowledges it to the module in a subsequent READ_EVENTS call. Lost,
// malformed, or ambiguous events invalidate the observation.
func drainGuardEvents(g guardDevice, transcript *authorityTranscript, join *auditJoin, cursor *uint64, lifecycle *checkpointLifecycle) error {
	if transcript == nil || join == nil || cursor == nil || lifecycle == nil {
		return errGuardProtocol
	}
	for {
		batch, err := g.readEvents(64, *cursor, *cursor)
		if err != nil {
			return err
		}
		if g.diagnostic != nil && batch.Last != 0 {
			g.diagnostic.ringThrough = batch.Last
		}
		if batch.Sticky != 0 {
			return errGuardProtocol
		}
		if len(batch.Records) == 0 {
			return nil
		}
		for _, record := range batch.Records {
			projected, err := record.projection()
			if err != nil {
				return err
			}
			if _, _, err = transcript.append("guard", map[string]any{"record": projected}); err != nil {
				return err
			}
			if g.diagnostic != nil {
				g.diagnostic.pipeline(&record, 25)
			}
			*cursor = record.Sequence
			fault := join.fault
			if fault != nil && fault.detector == "" && fault.mapEligible != nil && fault.mapEligible(record) {
				original := append([]byte(nil), record.Raw[:]...)
				modified := append([]byte(nil), original...)
				if fault.variant == "snapshot-missing" {
					clear(modified[128:192])
					if _, e := decodeGuardRecord(modified, record.Sequence); e == nil {
						return errGuardProtocol
					} else {
						fault.detector, fault.guardSequence, fault.original, fault.modified = "guard_snapshot_missing", record.Sequence, original, modified
						return e
					}
				}
				if fault.variant == "snapshot-bad-field" && lifecycle.runtimeGuard != nil {
					guardPut32(modified, 156, 8)
					changed, e := decodeGuardRecord(modified, record.Sequence)
					if e != nil {
						return e
					}
					e = lifecycle.runtimeGuard(changed)
					if errors.Is(e, errGuardSnapshotMismatch) {
						fault.detector, fault.guardSequence, fault.original, fault.modified = "guard_snapshot_mismatch", record.Sequence, original, modified
						return e
					}
					if e != nil {
						return e
					}
					return errGuardProtocol
				}
			}
			if record.Kind >= 1 && record.Kind <= 6 && lifecycle.runtimeGuard != nil {
				err = lifecycle.runtimeGuard(record)
				if err != nil {
					return err
				}
			}
			err = lifecycle.observe(record)
			if g.diagnostic != nil {
				g.diagnostic.pipeline(&record, 27)
			}
			if err != nil {
				return err
			}
			if record.Kind == 7 || record.Kind == 8 || record.Kind == 13 {
				if err = join.observeGuard(record); err != nil {
					return err
				}
			}
			if g.diagnostic != nil && lifecycle.runtimeGuard != nil {
				g.diagnostic.trackerThrough = record.Sequence
			}
		}
	}
}

func drainAuditMessages(a *auditMulticast, join *auditJoin) error {
	if a == nil || join == nil {
		return errGuardProtocol
	}
	for {
		facts, err := a.receive()
		if auditReceiveAgain(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, fact := range facts {
			if err := deliverAuditFact(join, fact); err != nil {
				return err
			}
		}
	}
}

func deliverAuditFact(join *auditJoin, fact auditFact) error {
	fault := join.fault
	if fault != nil && fault.detector == "" && (fault.variant == "audit-loss" || fault.variant == "audit-duplicate") {
		if fact.Kind == auditSyscallRecord && fault.auditActor != nil && fault.auditActor(fact.PID) && fact.Syscall == unix.SYS_BPF && fact.Argument0 == unix.BPF_MAP_CREATE && !fact.Success {
			if fault.omittedKey != nil {
				return errGuardProtocol
			}
			fault.actorPID = fact.PID
			key := fact.Key
			fault.omittedKey = &key
			if err := join.observeFact(fact); err != nil {
				return err
			}
			if fault.variant == "audit-duplicate" {
				return join.observeFact(fact)
			}
			return nil
		}
		if fault.variant == "audit-loss" && fault.omittedKey != nil && fact.Kind == auditEOERecord && fact.Key == *fault.omittedKey {
			copyFact := fact
			fault.audit = &copyFact
			return nil
		}
	}
	return join.observeFact(fact)
}

func monoNowNS() (uint64, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	if ts.Sec < 0 || ts.Nsec < 0 || ts.Nsec >= 1_000_000_000 {
		return 0, errGuardProtocol
	}
	return uint64(ts.Sec)*1_000_000_000 + uint64(ts.Nsec), nil
}

// pollAuditWatermark advances the shared event loop without waiting. A pending
// join or guard callback is retried only when the loop services more events.
func pollAuditWatermark(g guardDevice, a *auditMulticast, transcript *authorityTranscript, join *auditJoin, cursor *uint64, lifecycle *checkpointLifecycle) (guardStatus, bool, error) {
	if err := drainGuardEvents(g, transcript, join, cursor, lifecycle); err != nil {
		return guardStatus{}, false, err
	}
	if err := drainAuditMessages(a, join); err != nil {
		return guardStatus{}, false, err
	}
	status, err := g.statusObserved(1)
	if errors.Is(err, unix.EAGAIN) {
		return guardStatus{}, false, nil
	}
	if err != nil {
		return guardStatus{}, false, err
	}
	if status.State != 1 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 || status.Sequence < *cursor {
		return guardStatus{}, false, errGuardProtocol
	}
	if status.Sequence != *cursor || status.AckCursor != *cursor {
		return status, false, nil
	}
	if err = join.drain(); err != nil {
		return guardStatus{}, false, err
	}
	return status, join.settled(), nil
}
