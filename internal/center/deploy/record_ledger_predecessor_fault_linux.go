//go:build linux

package deploy

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type ledgerPredecessorFault struct {
	channel  *ledgerCallbackChannel
	boundary string
	scenario string
	victim   string
	nonce    string
	used     bool
}

func hexDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }

func ledgerOpenPredecessorFaultFD(ctx context.Context, realm *ledgerRealmChannel, fd int) (*ledgerPredecessorFault, error) {
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_SEQPACKET {
		return nil, ErrRecordLedgerFence
	}
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || *peer != realm.channel.peer {
		return nil, ErrRecordLedgerFence
	}
	channel, err := ledgerCallbackSocket(fd, *peer)
	if err != nil {
		return nil, err
	}
	fail := func() (*ledgerPredecessorFault, error) { channel.close(); return nil, ErrRecordLedgerFence }
	body, err := channel.receive(ctx)
	if err != nil {
		return fail()
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "scenario", "boundary", "victim", "nonce")
	if err != nil {
		return fail()
	}
	var version uint64
	var op string
	fault := &ledgerPredecessorFault{channel: channel}
	if ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 || ledgerCallbackDecode(fields, "op", &op) != nil || op != "cutpoint_configure" || ledgerCallbackDecode(fields, "scenario", &fault.scenario) != nil || ledgerCallbackDecode(fields, "boundary", &fault.boundary) != nil || ledgerCallbackDecode(fields, "victim", &fault.victim) != nil || ledgerCallbackDecode(fields, "nonce", &fault.nonce) != nil {
		return fail()
	}
	if _, ok := ledgerC80DigestBytes(fault.nonce); !ok {
		return fail()
	}
	switch fault.boundary {
	case "create-intent", "create-commit-before-result", "preview-ack-loss", "reserve-intent", "reserve-commit-before-stdout", "stdout-loss":
	default:
		return fail()
	}
	if (fault.victim != "child" && fault.victim != "coordinator") || fault.scenario != fault.boundary+"-"+fault.victim {
		return fail()
	}
	return fault, nil
}

func (call *ledgerPredecessorCall) cutpoint(ctx context.Context, boundary, kind string) error {
	fault := call.fault
	if fault == nil || fault.boundary != boundary {
		return nil
	}
	if fault.used {
		return ErrRecordLedgerFence
	}
	fault.used = true
	raw, found, err := fenceReadFile(call.fence.control, ledgerPredecessorName(kind, call.intent.PhaseGeneration))
	if err != nil || !found {
		return ErrRecordLedgerFence
	}
	digest := ledgerPredecessorHash(raw)
	operation := ""
	if call.committed != nil {
		operation = call.committed.OperationID
	}
	if err := fault.channel.send(ctx, map[string]any{"version": 1, "op": "cutpoint_ready", "scenario": fault.scenario, "boundary": boundary, "phase_generation": call.intent.PhaseGeneration, "operation_id": operation, "coordinator": call.intent.Coordinator, "child": call.child.process, "backend": call.backend, "evidence_digest": digest, "nonce": fault.nonce}); err != nil {
		return err
	}
	body, err := fault.channel.receive(ctx)
	if err != nil {
		return err
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "nonce", "evidence_digest")
	if err != nil {
		return err
	}
	var version uint64
	var op, nonce, ack string
	if ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 || ledgerCallbackDecode(fields, "op", &op) != nil || op != "cutpoint_ack" || ledgerCallbackDecode(fields, "nonce", &nonce) != nil || nonce != fault.nonce || ledgerCallbackDecode(fields, "evidence_digest", &ack) != nil || ack != digest {
		return ErrRecordLedgerFence
	}
	if err := fault.channel.send(ctx, map[string]any{"version": 1, "op": "cutpoint_armed", "nonce": nonce, "evidence_digest": digest}); err != nil {
		return err
	}
	// The child remains paused at its ACK. Root alone injects the selected kill.
	// No resume message exists: a surviving coordinator must enter refencing.
	for {
		if ctx.Err() != nil || !ledgerHeldProcessLive(call.child.pidfd) || !ledgerHeldProcessLive(call.realm.pidfd) {
			return ErrRecordLedgerFence
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			return ErrRecordLedgerFence
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ErrRecordLedgerFence
		}
		fds := []unix.PollFd{{Fd: int32(call.child.pidfd), Events: unix.POLLIN}, {Fd: int32(call.realm.pidfd), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, int(min(remaining.Milliseconds(), 100))); err != nil && err != unix.EINTR {
			return ErrRecordLedgerFence
		}
	}
}

type ledgerPredecessorEffectCounts struct {
	Previews, Operations int64
	Record               bool
}

func (call *ledgerPredecessorCall) effectCounts(ctx context.Context) (ledgerPredecessorEffectCounts, error) {
	var counts ledgerPredecessorEffectCounts
	err := call.fence.app.QueryRow(ctx, `select
 (select count(*) from public.deletion_reservations where project_id=$1 and object_kind='record' and object_id=$2),
 (select count(*) from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id where r.project_id=$1 and r.object_kind='record' and r.object_id=$2),
 exists(select 1 from public.records where project_id=$1 and record_id=$2)`, call.config.ProjectID, call.config.RecordID).Scan(&counts.Previews, &counts.Operations, &counts.Record)
	if err != nil {
		return counts, ErrRecordLedgerFence
	}
	return counts, nil
}
func (call *ledgerPredecessorCall) unchangedDenied(ctx context.Context) error {
	counts, err := call.effectCounts(ctx)
	if err != nil || counts != call.initialEffects || !counts.Record {
		return ErrRecordLedgerFence
	}
	_, items, err := call.ledger(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		source := item.preview.Request.Origin.Source
		table, key := "", ""
		switch string(source.Kind) {
		case "vps":
			table, key = "vps_assets", "vps_id"
		case "monitoring_instance":
			table, key = "monitoring_instances", "monitoring_instance_id"
		case "target":
			table, key = "targets", "target_id"
		default:
			return ErrRecordLedgerFence
		}
		if strings.ContainsAny(source.ID, "\x00") {
			return ErrRecordLedgerFence
		}
		var exists bool
		if call.fence.app.QueryRow(ctx, "select exists(select 1 from public."+table+" where "+key+"=$1)", source.ID).Scan(&exists) != nil || !exists {
			return ErrRecordLedgerFence
		}
	}
	return nil
}
