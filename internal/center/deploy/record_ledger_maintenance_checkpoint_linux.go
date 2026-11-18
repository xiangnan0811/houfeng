//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

// Maintenance acknowledgements witness durable records independently of realm
// validity. They do not confer a no-copy or predecessor capability.
type ledgerMaintenanceCheckpoint struct {
	Family            string                    `json:"family"`
	RecoveryID        string                    `json:"recovery_id"`
	ApplicationDigest string                    `json:"application_digest"`
	PhaseGeneration   uint64                    `json:"phase_generation"`
	Attempt           uint64                    `json:"attempt"`
	Kind              string                    `json:"kind"`
	RecordSHA256      string                    `json:"record_sha256"`
	CurrentBackend    *ledgerPredecessorBackend `json:"current_backend,omitempty"`
}

type ledgerMaintenanceACK struct {
	WitnessSequence uint64
	WitnessDigest   string
	ObserverBackend *ledgerPredecessorBackend
}

type ledgerMaintenanceReadyDisposition struct {
	Disposition          string
	DecisionSequence     uint64
	DecisionDigest       string
	ReadyWitnessSequence *uint64
	ReadyWitnessDigest   *string
}

type ledgerMaintenanceCheckpointer interface {
	Checkpoint(context.Context, ledgerMaintenanceCheckpoint) (ledgerMaintenanceACK, error)
	ReadyDisposition(context.Context, ledgerMaintenanceCheckpoint) (ledgerMaintenanceReadyDisposition, error)
	Healthy() bool
}

type ledgerMaintenanceChannel struct {
	realm  *ledgerRealmChannel
	failed bool
}

func (channel *ledgerMaintenanceChannel) Checkpoint(ctx context.Context, request ledgerMaintenanceCheckpoint) (ledgerMaintenanceACK, error) {
	var result ledgerMaintenanceACK
	if channel == nil || channel.realm == nil || channel.realm.channel == nil {
		return result, ErrRecordLedgerFence
	}
	transport := channel.realm.channel
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if channel.failed || !ledgerHeldProcessLive(channel.realm.pidfd) {
		return result, ErrRecordLedgerFence
	}
	// Any incomplete exchange closes this invocation's authority. A new
	// invocation may retry the same immutable record with a fresh nonce.
	channel.failed = true
	if request.Family != "migration" && request.Family != "mutation" && request.Family != "transfer" {
		return result, ErrRecordLedgerFence
	}
	settlement := request.Family == "migration" && (request.Kind == "begin-settled" || request.Kind == "ready-abandoned")
	if settlement != (request.CurrentBackend != nil) || settlement && !ledgerMaintenanceBackendValid(*request.CurrentBackend) {
		return result, ErrRecordLedgerFence
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return result, err
	}
	nonceText := hex.EncodeToString(nonce[:])
	wire := struct {
		Version uint64 `json:"version"`
		Op      string `json:"op"`
		ledgerMaintenanceCheckpoint
		Nonce string `json:"nonce"`
	}{1, "maintenance_checkpoint", request, nonceText}
	if err := transport.send(ctx, wire); err != nil {
		return result, err
	}
	body, err := transport.receive(ctx)
	if err != nil {
		return result, err
	}
	names := []string{"version", "op", "family", "recovery_id", "application_digest", "phase_generation", "attempt", "kind", "record_sha256", "nonce", "witness_sequence", "witness_digest"}
	if settlement {
		names = append(names, "current_backend", "observer_backend")
	}
	fields, err := ledgerCallbackFields(body, names...)
	if err != nil {
		return result, err
	}
	var version, generation, attempt uint64
	if ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 ||
		ledgerCallbackDecode(fields, "phase_generation", &generation) != nil || generation != request.PhaseGeneration ||
		ledgerCallbackDecode(fields, "attempt", &attempt) != nil || attempt != request.Attempt ||
		ledgerCallbackDecode(fields, "witness_sequence", &result.WitnessSequence) != nil || result.WitnessSequence == 0 {
		return ledgerMaintenanceACK{}, ErrRecordLedgerFence
	}
	for key, expected := range map[string]string{"op": "maintenance_checkpoint_ack", "family": request.Family, "recovery_id": request.RecoveryID, "application_digest": request.ApplicationDigest, "kind": request.Kind, "record_sha256": request.RecordSHA256, "nonce": nonceText} {
		var actual string
		if ledgerCallbackDecode(fields, key, &actual) != nil || actual != expected {
			return ledgerMaintenanceACK{}, ErrRecordLedgerFence
		}
	}
	for _, key := range []string{"application_digest", "record_sha256", "nonce", "witness_digest"} {
		if _, err := ledgerCallbackDigest(fields, key); err != nil {
			return ledgerMaintenanceACK{}, err
		}
	}
	if settlement {
		current, err := ledgerMaintenanceDecodeBackend(fields["current_backend"])
		if err != nil || current.PID != request.CurrentBackend.PID || current.Role != request.CurrentBackend.Role || !current.Started.Equal(request.CurrentBackend.Started) {
			return ledgerMaintenanceACK{}, ErrRecordLedgerFence
		}
		observer, err := ledgerMaintenanceDecodeBackend(fields["observer_backend"])
		if err != nil || observer.PID == current.PID {
			return ledgerMaintenanceACK{}, ErrRecordLedgerFence
		}
		result.ObserverBackend = &observer
	}
	if ledgerCallbackDecode(fields, "witness_digest", &result.WitnessDigest) != nil || !ledgerHeldProcessLive(channel.realm.pidfd) {
		return ledgerMaintenanceACK{}, ErrRecordLedgerFence
	}
	channel.failed = false
	return result, nil
}

func (channel *ledgerMaintenanceChannel) ReadyDisposition(ctx context.Context, request ledgerMaintenanceCheckpoint) (ledgerMaintenanceReadyDisposition, error) {
	var result ledgerMaintenanceReadyDisposition
	if channel == nil || channel.realm == nil || channel.realm.channel == nil {
		return result, ErrRecordLedgerFence
	}
	transport := channel.realm.channel
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if channel.failed || !ledgerHeldProcessLive(channel.realm.pidfd) {
		return result, ErrRecordLedgerFence
	}
	channel.failed = true
	if request.Family != "migration" || request.Kind != "ready" || request.Attempt != 0 || request.PhaseGeneration == 0 || request.CurrentBackend != nil || !fenceRID.MatchString(request.RecoveryID) {
		return result, ErrRecordLedgerFence
	}
	if _, ok := ledgerC80DigestBytes(request.ApplicationDigest); !ok {
		return result, ErrRecordLedgerFence
	}
	if _, ok := ledgerC80DigestBytes(request.RecordSHA256); !ok {
		return result, ErrRecordLedgerFence
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return result, err
	}
	nonceText := hex.EncodeToString(nonce[:])
	wire := struct {
		Version uint64 `json:"version"`
		Op      string `json:"op"`
		ledgerMaintenanceCheckpoint
		Nonce string `json:"nonce"`
	}{1, "maintenance_ready_disposition", request, nonceText}
	if err := transport.send(ctx, wire); err != nil {
		return result, err
	}
	body, err := transport.receive(ctx)
	if err != nil {
		return result, err
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "family", "recovery_id", "application_digest", "phase_generation", "attempt", "kind", "record_sha256", "nonce", "disposition", "decision_sequence", "decision_digest", "ready_witness_sequence", "ready_witness_digest")
	if err != nil {
		return result, err
	}
	var version, generation, attempt uint64
	if ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 ||
		ledgerCallbackDecode(fields, "phase_generation", &generation) != nil || generation != request.PhaseGeneration ||
		ledgerCallbackDecode(fields, "attempt", &attempt) != nil || attempt != 0 ||
		ledgerCallbackDecode(fields, "decision_sequence", &result.DecisionSequence) != nil || result.DecisionSequence == 0 {
		return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
	}
	for key, expected := range map[string]string{"op": "maintenance_ready_disposition_ack", "family": request.Family, "recovery_id": request.RecoveryID, "application_digest": request.ApplicationDigest, "kind": request.Kind, "record_sha256": request.RecordSHA256, "nonce": nonceText} {
		var actual string
		if ledgerCallbackDecode(fields, key, &actual) != nil || actual != expected {
			return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
		}
	}
	for _, key := range []string{"application_digest", "record_sha256", "nonce", "decision_digest"} {
		if _, err := ledgerCallbackDigest(fields, key); err != nil {
			return ledgerMaintenanceReadyDisposition{}, err
		}
	}
	if ledgerCallbackDecode(fields, "decision_digest", &result.DecisionDigest) != nil || ledgerCallbackDecode(fields, "disposition", &result.Disposition) != nil {
		return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
	}
	switch result.Disposition {
	case "witnessed":
		var sequence uint64
		var digest string
		if ledgerCallbackDecode(fields, "ready_witness_sequence", &sequence) != nil || sequence == 0 || sequence >= result.DecisionSequence ||
			ledgerCallbackDecode(fields, "ready_witness_digest", &digest) != nil {
			return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
		}
		if _, err := ledgerCallbackDigest(fields, "ready_witness_digest"); err != nil {
			return ledgerMaintenanceReadyDisposition{}, err
		}
		result.ReadyWitnessSequence, result.ReadyWitnessDigest = &sequence, &digest
	case "unwitnessed_retired":
		if strings.TrimSpace(string(fields["ready_witness_sequence"])) != "null" || strings.TrimSpace(string(fields["ready_witness_digest"])) != "null" {
			return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
		}
	default:
		return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
	}
	if !ledgerHeldProcessLive(channel.realm.pidfd) {
		return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
	}
	channel.failed = false
	return result, nil
}

func (channel *ledgerMaintenanceChannel) Healthy() bool {
	if channel == nil || channel.realm == nil || channel.realm.channel == nil {
		return false
	}
	channel.realm.channel.mu.Lock()
	defer channel.realm.channel.mu.Unlock()
	return !channel.failed && ledgerHeldProcessLive(channel.realm.pidfd)
}

func ledgerMaintenanceBackendValid(backend ledgerPredecessorBackend) bool {
	return backend.PID > 0 && !backend.Started.IsZero() && backend.Started.Nanosecond()%1000 == 0 && ledgerSafeRole(backend.Role)
}

func ledgerMaintenanceDecodeBackend(body []byte) (ledgerPredecessorBackend, error) {
	var backend ledgerPredecessorBackend
	fields, err := ledgerCallbackFields(body, "pid", "started", "role")
	if err != nil || ledgerCallbackDecode(fields, "pid", &backend.PID) != nil || ledgerCallbackDecode(fields, "started", &backend.Started) != nil || ledgerCallbackDecode(fields, "role", &backend.Role) != nil || !ledgerMaintenanceBackendValid(backend) {
		return ledgerPredecessorBackend{}, ErrRecordLedgerFence
	}
	return backend, nil
}

// Settlement records retain their original observer provenance; the root
// request instead binds the management connection of this invocation.
func ledgerMigrationSettlementCheckpoint(ctx context.Context, f *RecordLedgerFence, recoveryID string, application RecordLedgerApplicationIdentity, generation uint64, kind string, value any) error {
	if kind != "begin-settled" && kind != "ready-abandoned" || f.maintenanceCheckpointer == nil {
		return ErrRecordLedgerFence
	}
	name := ledgerMigrationName(kind, generation)
	body, found, err := fenceReadFile(f.control, name)
	if err != nil || !found || ledgerReadExact(f.control, name, value) != nil {
		return ErrRecordLedgerFence
	}
	applicationDigest, err := ledgerMigrationDigest(application)
	if err != nil {
		return err
	}
	connection, err := f.app.Acquire(ctx)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer connection.Release()
	var current ledgerPredecessorBackend
	if connection.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&current.PID, &current.Started, &current.Role) != nil || !ledgerMaintenanceBackendValid(current) {
		return ErrRecordLedgerFence
	}
	ack, err := f.maintenanceCheckpointer.Checkpoint(ctx, ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: recoveryID, ApplicationDigest: applicationDigest, PhaseGeneration: generation, Kind: kind, RecordSHA256: ledgerPredecessorHash(body), CurrentBackend: &current})
	if err != nil || ack.WitnessSequence == 0 || ack.ObserverBackend == nil || !ledgerMaintenanceBackendValid(*ack.ObserverBackend) {
		return ErrRecordLedgerFence
	}
	if _, ok := ledgerC80DigestBytes(ack.WitnessDigest); !ok {
		return ErrRecordLedgerFence
	}
	var actual ledgerPredecessorBackend
	var observerPresent bool
	if connection.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&actual.PID, &actual.Started, &actual.Role) != nil || actual.PID != current.PID || actual.Role != current.Role || !actual.Started.Equal(current.Started) ||
		connection.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3)`, ack.ObserverBackend.PID, ack.ObserverBackend.Started, ack.ObserverBackend.Role).Scan(&observerPresent) != nil || observerPresent {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerMigrationCheckpointRecord(ctx context.Context, checkpointer ledgerMaintenanceCheckpointer, intent ledgerMigrationIntent, attempt uint64, kind string, value any) error {
	if checkpointer == nil {
		return ErrRecordLedgerFence
	}
	recordDigest, err := ledgerMigrationDigest(value)
	if err != nil {
		return err
	}
	applicationDigest, err := ledgerMigrationDigest(intent.Application)
	if err != nil {
		return err
	}
	_, err = checkpointer.Checkpoint(ctx, ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: intent.RecoveryID, ApplicationDigest: applicationDigest, PhaseGeneration: intent.PhaseGeneration, Attempt: attempt, Kind: kind, RecordSHA256: recordDigest})
	return err
}

func ledgerMigrationCheckpoint(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, attempt uint64, kind string, value any) error {
	name := ledgerMigrationName(kind, intent.PhaseGeneration)
	if attempt != 0 {
		name = ledgerMigrationObservationName(strings.TrimPrefix(kind, "observe-"), intent.PhaseGeneration, attempt)
	}
	if ledgerReadExact(f.control, name, value) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, attempt, kind, value)
}

// A durable terminal is not authority: root must witness its exact bytes and
// revalidate its retained prefix in this invocation. No local receipt can
// substitute for the live channel, including after a lost acknowledgement.
func ledgerMigrationAuthorizeJournal(ctx context.Context, control int, journal ledgerMigrationJournal, checkpointer ledgerMaintenanceCheckpointer) error {
	for _, terminal := range journal.Terminals {
		if ctx == nil || ctx.Err() != nil || checkpointer == nil {
			return ErrRecordLedgerFence
		}
		body, found, err := fenceReadFile(control, fmt.Sprintf("phase-%020d.json", terminal.Generation))
		var actual ledgerMaintenancePhase
		if err != nil || !found || fenceDecode(body, &actual) != nil || !reflect.DeepEqual(actual, terminal) {
			return ErrRecordLedgerFence
		}
		application, err := ledgerMigrationDigest(terminal.Application)
		if err != nil {
			return err
		}
		ack, err := checkpointer.Checkpoint(ctx, ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: terminal.RecoveryID, ApplicationDigest: application, PhaseGeneration: terminal.Generation, Kind: "terminal", RecordSHA256: ledgerPredecessorHash(body)})
		if err != nil || ack.WitnessSequence == 0 {
			return ErrRecordLedgerFence
		}
		if _, ok := ledgerC80DigestBytes(ack.WitnessDigest); !ok {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func (f *RecordLedgerFence) checkpointMaintenancePhase(ctx context.Context, phase ledgerMaintenancePhase) error {
	if f.maintenanceCheckpointer == nil {
		return nil
	}
	digest, err := ledgerMigrationDigest(phase)
	if err != nil {
		return err
	}
	application, err := ledgerMigrationDigest(phase.Application)
	if err != nil {
		return err
	}
	kind := "phase"
	if phase.Phase == ledgerPhaseQuiescent {
		kind = "terminal"
	}
	_, err = f.maintenanceCheckpointer.Checkpoint(ctx, ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: phase.RecoveryID, ApplicationDigest: application, PhaseGeneration: phase.Generation, Kind: kind, RecordSHA256: digest})
	return err
}

// Poll only hangup/error state; consuming packets here would race the sole
// request owner. Cancellation interrupts work without extending its cleanup
// deadline or claiming that any PostgreSQL transaction has settled.
func (channel *ledgerMaintenanceChannel) watch(ctx context.Context, cancel context.CancelFunc) (func(), error) {
	if channel == nil || channel.realm == nil || channel.realm.channel == nil {
		return nil, ErrRecordLedgerFence
	}
	raw, err := channel.realm.channel.conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	watchFD := -1
	var duplicateErr error
	if err := raw.Control(func(fd uintptr) { watchFD, duplicateErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		return nil, err
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		defer unix.Close(watchFD)
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			default:
			}
			fds := []unix.PollFd{{Fd: int32(watchFD), Events: unix.POLLRDHUP}, {Fd: int32(channel.realm.pidfd), Events: unix.POLLIN}}
			_, err := unix.Poll(fds, 50)
			if err == unix.EINTR {
				continue
			}
			if err != nil || fds[0].Revents != 0 || fds[1].Revents != 0 {
				cancel()
				return
			}
		}
	}()
	return func() { close(stop); <-done }, nil
}
