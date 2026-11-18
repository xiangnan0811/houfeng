//go:build linux

package deploy

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type ledgerMaintenanceFaultKey struct{}

type ledgerMaintenanceFault struct {
	channel   *ledgerCallbackChannel
	rootPIDFD int
	boundary  string
	nonce     string
	used      bool
}

func ledgerMaintenanceFaultFromContext(ctx context.Context) *ledgerMaintenanceFault {
	fault, _ := ctx.Value(ledgerMaintenanceFaultKey{}).(*ledgerMaintenanceFault)
	return fault
}

// Only the isolated source CLI opts into inherited FD7. Ordinary maintenance
// and predecessor callbacks retain their existing admission and transport.
func WithLegacySourceLedgerMaintenance(ctx context.Context, request RecordLedgerMaintenanceRequest, fn func(*RecordLedgerFence) error) error {
	if ctx == nil {
		return ErrRecordLedgerFence
	}
	faultCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	fault, err := ledgerCaptureMaintenanceFault(faultCtx, "app-commit", "w1-commit", "w2-commit", "anchor-fsync", "handoff-install", "handoff-return")
	if err != nil {
		return err
	}
	defer fault.close()
	if fault != nil {
		ctx = context.WithValue(faultCtx, ledgerMaintenanceFaultKey{}, fault)
	}
	return WithRecordLedgerMaintenance(ctx, request, fn)
}

func ledgerCaptureMaintenanceFault(ctx context.Context, boundaries ...string) (*ledgerMaintenanceFault, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrRecordLedgerFence
	}
	flags, err := unix.FcntlInt(7, unix.F_GETFD, 0)
	if err == unix.EBADF {
		return nil, nil
	}
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	if flags&unix.FD_CLOEXEC != 0 {
		// These Go runtime descriptors cannot have survived the coordinator's
		// exec as FD7. An unexpected inherited descriptor is never ignored.
		name, err := os.Readlink("/proc/self/fd/7")
		if err == nil && (name == "anon_inode:[eventpoll]" || name == "anon_inode:[eventfd]") {
			return nil, nil
		}
		return nil, ErrRecordLedgerFence
	}
	kind, err := unix.GetsockoptInt(7, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_SEQPACKET {
		return nil, ErrRecordLedgerFence
	}
	peer, err := unix.GetsockoptUcred(7, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || peer.Pid != int32(os.Getppid()) || peer.Uid != 0 || peer.Gid != 0 {
		return nil, ErrRecordLedgerFence
	}
	pidfd, err := unix.PidfdOpen(int(peer.Pid), 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	channel, err := ledgerCallbackSocket(7, *peer)
	if err != nil {
		unix.Close(pidfd)
		return nil, err
	}
	// Only the CLOEXEC duplicate belongs to this coordinator from here on.
	unix.Close(7)
	fault := &ledgerMaintenanceFault{channel: channel, rootPIDFD: pidfd}
	fail := func() (*ledgerMaintenanceFault, error) { fault.close(); return nil, ErrRecordLedgerFence }
	if !ledgerHeldProcessLive(pidfd) || int(peer.Pid) != os.Getppid() {
		return fail()
	}
	body, err := channel.receive(ctx)
	if err != nil {
		return fail()
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "scenario", "boundary", "victim", "nonce")
	var version uint64
	var op, scenario, victim string
	if err != nil || ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 ||
		ledgerCallbackDecode(fields, "op", &op) != nil || op != "cutpoint_configure" ||
		ledgerCallbackDecode(fields, "scenario", &scenario) != nil ||
		ledgerCallbackDecode(fields, "boundary", &fault.boundary) != nil ||
		ledgerCallbackDecode(fields, "victim", &victim) != nil ||
		ledgerCallbackDecode(fields, "nonce", &fault.nonce) != nil || scenario != fault.boundary {
		return fail()
	}
	allowed := false
	for _, boundary := range boundaries {
		allowed = allowed || boundary == fault.boundary
	}
	expectedVictim := "coordinator"
	if fault.boundary == "c81-convergence-commit" {
		expectedVictim = "child"
	}
	if _, ok := ledgerC80DigestBytes(fault.nonce); !ok || !allowed || victim != expectedVictim {
		return fail()
	}
	return fault, nil
}

func (fault *ledgerMaintenanceFault) close() {
	if fault != nil {
		fault.channel.close()
		if fault.rootPIDFD >= 0 {
			unix.Close(fault.rootPIDFD)
			fault.rootPIDFD = -1
		}
	}
}

func (fault *ledgerMaintenanceFault) pause(ctx context.Context, f *RecordLedgerFence, boundary, operation, receipt string, child *ledgerPredecessorProcess, childPIDFD int, backend *ledgerPredecessorBackend) error {
	if fault == nil || fault.boundary != boundary {
		return nil
	}
	deadline, ok := f.ctx.Deadline()
	if !ok {
		return ErrRecordLedgerFence
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if fault.used || operation == "" || !ledgerHeldProcessLive(fault.rootPIDFD) || (child != nil && !ledgerHeldProcessLive(childPIDFD)) {
		return ErrRecordLedgerFence
	}
	fault.used = true
	coordinator, err := ledgerPredecessorSelf()
	if err != nil {
		return err
	}
	f.mu.Lock()
	phase := f.phase
	f.mu.Unlock()
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil || len(phases) == 0 || phases[len(phases)-1] != phase {
		return ErrRecordLedgerFence
	}
	raw, found, err := fenceReadFile(f.control, receipt)
	if err != nil || !found {
		return ErrRecordLedgerFence
	}
	digest := ledgerPredecessorHash(raw)
	if err := fault.channel.send(ctx, map[string]any{"version": 1, "op": "cutpoint_ready", "scenario": boundary, "boundary": boundary, "phase_generation": phase.Generation, "operation_id": operation, "coordinator": coordinator, "child": child, "backend": backend, "evidence_digest": digest, "nonce": fault.nonce}); err != nil {
		return err
	}
	body, err := fault.channel.receive(ctx)
	if err != nil {
		return err
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "nonce", "evidence_digest")
	var version uint64
	var op, nonce, acknowledged string
	if err != nil || ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 ||
		ledgerCallbackDecode(fields, "op", &op) != nil || op != "cutpoint_ack" ||
		ledgerCallbackDecode(fields, "nonce", &nonce) != nil || nonce != fault.nonce ||
		ledgerCallbackDecode(fields, "evidence_digest", &acknowledged) != nil || acknowledged != digest {
		return ErrRecordLedgerFence
	}
	if err := fault.channel.send(ctx, map[string]any{"version": 1, "op": "cutpoint_armed", "nonce": nonce, "evidence_digest": digest}); err != nil {
		return err
	}
	fds := []unix.PollFd{{Fd: int32(fault.rootPIDFD), Events: unix.POLLIN}}
	if child != nil {
		fds = append(fds, unix.PollFd{Fd: int32(childPIDFD), Events: unix.POLLIN})
	}
	for ctx.Err() == nil && ledgerHeldProcessLive(fault.rootPIDFD) && (child == nil || ledgerHeldProcessLive(childPIDFD)) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 {
			return ErrRecordLedgerFence
		}
		if _, err := unix.Poll(fds, int(min(time.Until(deadline).Milliseconds(), 100))); err != nil && err != unix.EINTR {
			return ErrRecordLedgerFence
		}
	}
	return ErrRecordLedgerFence
}

func (fault *ledgerMaintenanceFault) migrationCommitted(ctx context.Context, f *RecordLedgerFence, child ledgerPredecessorProcess, childPIDFD int, backend ledgerPredecessorBackend) error {
	if fault == nil {
		return nil
	}
	f.work.Lock()
	if err := f.checkAnchorsLocked(); err != nil {
		f.work.Unlock()
		return err
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		f.work.Unlock()
		return err
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, p, w, anchor)
	if err != nil {
		f.work.Unlock()
		return err
	}
	items, err := f.sourceJournalPair(primary, witness, anchor)
	operation := ""
	if err == nil {
		for _, item := range items {
			anchored, e := f.sourceAnchored(item)
			_, installed, readErr := fenceReadFile(f.control, "source-handoff-installed-"+item.preview.Key+".json")
			if e != nil || readErr != nil || !anchored || !item.found || item.entry.Sequence > witness.Head.Sequence {
				err = ErrRecordLedgerFence
				break
			}
			if installed {
				continue
			}
			if operation != "" {
				err = ErrRecordLedgerFence
				break
			}
			operation = item.preview.Request.OperationID
		}
	}
	f.work.Unlock()
	if err != nil || operation == "" {
		return ErrRecordLedgerFence
	}
	f.mu.Lock()
	phase := f.phase
	f.mu.Unlock()
	if phase.Phase != ledgerPhaseMigration {
		return ErrRecordLedgerFence
	}
	return fault.pause(ctx, f, "c81-convergence-commit", operation, fmt.Sprintf("phase-%020d.json", phase.Generation), &child, childPIDFD, &backend)
}
