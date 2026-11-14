package recorddeletion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type legacyCoordinatorFixture struct {
	operation     LegacyLedgerOperation
	entry         CanonicalLedgerEntry
	primary       LedgerSnapshot
	witness       LedgerSnapshot
	guard         *legacyCoordinatorGuard
	committed     bool
	appendError   bool
	appends       int
	confirmations int
	installed     [][]byte
}

type legacyCoordinatorGuard struct {
	fixture                                       *legacyCoordinatorFixture
	active, pending, rejectAdvance, neverEnrolled bool
	generation                                    uint64
}
type legacyCoordinatorReader struct {
	fixture *legacyCoordinatorFixture
	witness bool
}
type legacyCoordinatorWriter struct{ fixture *legacyCoordinatorFixture }
type legacyCoordinatorWitness struct{ fixture *legacyCoordinatorFixture }
type legacyCoordinatorStore struct{ fixture *legacyCoordinatorFixture }
type legacyCoordinatorInstaller struct{ fixture *legacyCoordinatorFixture }

func (g *legacyCoordinatorGuard) WithHeldLedgerFence(ctx context.Context, fn func(context.Context) error) error {
	if g.active {
		return fn(ctx)
	}
	g.active = true
	defer func() { g.active = false }()
	return fn(ctx)
}
func (g *legacyCoordinatorGuard) RecoveryBinding() (string, uint64, error) {
	if !g.active {
		return "", 0, errors.New("fence expired")
	}
	return "recovery_01", g.generation, nil
}
func (g *legacyCoordinatorGuard) NeverEnrolled() (bool, error) {
	if !g.active {
		return false, errors.New("fence expired")
	}
	return g.neverEnrolled, nil
}
func (g *legacyCoordinatorGuard) Trust() (LedgerVerificationTrust, error) {
	if !g.active {
		return LedgerVerificationTrust{}, errors.New("fence expired")
	}
	return LedgerVerificationTrust{Primary: g.fixture.primary.Identity, Witness: g.fixture.witness.Identity, Continuity: g}, nil
}
func (g *legacyCoordinatorGuard) VerifyLedgerContinuity(_ context.Context, _ LedgerContinuityObservation) error {
	if !g.active {
		return errors.New("fence expired")
	}
	return nil
}
func (g *legacyCoordinatorGuard) Preflight(context.Context) error {
	if !g.active {
		return errors.New("fence expired")
	}
	return nil
}
func (g *legacyCoordinatorGuard) BeginAppend(_ context.Context, _ LedgerAppendRequest) (bool, error) {
	if !g.active {
		return false, errors.New("fence expired")
	}
	if g.pending {
		return false, nil
	}
	g.pending = true
	return true, nil
}
func (g *legacyCoordinatorGuard) WithAbsentOperation(_ context.Context, original LedgerAppendRequest) (DeletionLedgerEntry, error) {
	if !g.active || !g.pending || !g.fixture.committed {
		return DeletionLedgerEntry{}, ErrLedgerPending
	}
	entry := g.fixture.entry.DeletionLedgerEntry
	if entry.Request.EntryType != LedgerEntryAttemptNotCommitted || !sameLedgerAppendRequest(entry.Request, original.AttemptNotCommitted(entry.Request.ReleaseEpoch)) {
		return DeletionLedgerEntry{}, ErrInvalidLedgerEvidence
	}
	return entry, nil
}
func (g *legacyCoordinatorGuard) Advance(context.Context) error {
	if !g.active || g.rejectAdvance {
		return errors.New("anchor rejected")
	}
	return nil
}
func (r legacyCoordinatorReader) ReadLedgerSnapshot(context.Context) (LedgerSnapshot, error) {
	if r.witness {
		return r.fixture.witness, nil
	}
	return r.fixture.primary, nil
}
func (w legacyCoordinatorWriter) ResolveDeletionEntry(_ context.Context, request LedgerAppendRequest) (LedgerResolution, error) {
	if !w.fixture.committed {
		return LedgerResolution{Kind: LedgerResolutionUnresolved}, nil
	}
	if !sameLedgerAppendRequest(w.fixture.entry.Request, request) {
		return LedgerResolution{}, ErrInvalidLedgerEvidence
	}
	return NewCommittedLedgerResolution(w.fixture.entry.DeletionLedgerEntry), nil
}
func (w legacyCoordinatorWriter) AppendDeletionEntry(_ context.Context, _ LedgerAppendRequest) (DeletionLedgerEntry, error) {
	w.fixture.appends++
	w.fixture.committed = true
	e := w.fixture.entry
	w.fixture.primary.Head = LedgerHead{Sequence: e.Sequence, Hash: e.EntryHash}
	w.fixture.primary.Entries = []LedgerChainEntry{{Sequence: e.Sequence, PreviousHash: e.PreviousHash, EntryHash: e.EntryHash, Canonical: e.Canonical, Route: LedgerRecordRoute, Record: &e}}
	if w.fixture.appendError {
		return DeletionLedgerEntry{}, errors.New("commit response lost")
	}
	return e.DeletionLedgerEntry, nil
}
func (w legacyCoordinatorWitness) ConfirmDeletionEntry(_ context.Context, entry DeletionLedgerEntry) (DeletionWitnessReceipt, error) {
	w.fixture.confirmations++
	w.fixture.witness.Head = w.fixture.primary.Head
	w.fixture.witness.Entries = append([]LedgerChainEntry(nil), w.fixture.primary.Entries...)
	return DeletionWitnessReceipt{Sequence: entry.Sequence, EntryHash: entry.EntryHash, ProofDigest: deletionTestDigest(91)}, nil
}
func (s legacyCoordinatorStore) WithLegacyOperation(ctx context.Context, _ string, fn func(context.Context, LegacyLedgerOperation, LegacyVerifiedEntryInstaller) error) error {
	return fn(ctx, s.fixture.operation, legacyCoordinatorInstaller{s.fixture})
}
func (s legacyCoordinatorStore) WithLegacyRecovery(ctx context.Context, fn func(context.Context, LegacyVerifiedEntryInstaller) error) error {
	return fn(ctx, legacyCoordinatorInstaller{s.fixture})
}
func (i legacyCoordinatorInstaller) InstallVerifiedEntry(_ context.Context, body []byte) error {
	i.fixture.installed = append(i.fixture.installed, append([]byte(nil), body...))
	return nil
}

func newLegacyCoordinatorFixture(t *testing.T) (*legacyCoordinatorFixture, *LegacyLedgerCoordinator) {
	t.Helper()
	primary := LedgerPhysicalIdentity{ID: "rd-" + strings.Repeat("1", 64), Kind: "deletion_ledger", SystemIdentifier: "101", DatabaseOID: 101, DatabaseName: "ledger", Epoch: 1}
	witness := LedgerPhysicalIdentity{ID: "rd-" + strings.Repeat("2", 64), Kind: "deletion_witness", SystemIdentifier: "102", DatabaseOID: 102, DatabaseName: "witness", Epoch: 1}
	domain, err := LedgerDomainDigest(primary.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := deletionTestLedgerRequest(t, LedgerEntryDeleteCommit, 0)
	entry := CanonicalLedgerEntry{DeletionLedgerEntry: DeletionLedgerEntry{Request: request, Sequence: 1}, LedgerDomain: domain, ConfirmedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	entry.Canonical, err = EncodeCanonicalLedgerEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = sha256.Sum256(entry.Canonical)
	fixture := &legacyCoordinatorFixture{operation: LegacyLedgerOperation{Request: request, State: DeletionStateProvisionalFenced}, entry: entry,
		primary: LedgerSnapshot{Identity: primary}, witness: LedgerSnapshot{Identity: witness}}
	fixture.guard = &legacyCoordinatorGuard{fixture: fixture, generation: 2}
	coordinator, err := NewLegacyLedgerCoordinator(legacyCoordinatorStore{fixture}, legacyCoordinatorWriter{fixture}, legacyCoordinatorWitness{fixture},
		legacyCoordinatorReader{fixture, false}, legacyCoordinatorReader{fixture, true}, fixture.guard, domain)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, coordinator
}

func TestLegacyCoordinatorInstallsOnlyWitnessedIdentity(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	locator, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if locator.Sequence != 1 || locator.EntryHash != fixture.entry.EntryHash || fixture.appends != 1 || fixture.confirmations != 1 || len(fixture.installed) != 1 {
		t.Fatalf("unexpected reconciliation: locator=%+v append=%d confirmation=%d installs=%d", locator, fixture.appends, fixture.confirmations, len(fixture.installed))
	}
	var command map[string]any
	if err := json.Unmarshal(fixture.installed[0], &command); err != nil {
		t.Fatal(err)
	}
	if command["operation_id"] != fixture.operation.Request.OperationID || command["route"] != LedgerRecordRoute ||
		command["maintenance_rid"] != "recovery_01" || command["maintenance_generation"] != float64(2) ||
		command["ledger_entry_hash"] == "" || command["canonical_entry"] == "" || command["witness_proof_digest"] == "" {
		t.Fatalf("incorrect installed binding: %+v", command)
	}
}

func TestLegacyCoordinatorUnknownAppendResolvesOriginalIdentity(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	fixture.appendError = true
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); !errors.Is(err, ErrLegacyLedgerUnresolved) {
		t.Fatalf("unknown append status = %v", err)
	}
	fixture.appendError = false
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); err != nil {
		t.Fatal(err)
	}
	if fixture.appends != 1 || len(fixture.installed) != 1 {
		t.Fatalf("unknown append was resent: %d", fixture.appends)
	}
}

func TestLegacyCoordinatorDoesNotInferAbsenceOrInstallWithoutAnchor(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	fixture.guard.pending = true
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); !errors.Is(err, ErrLegacyLedgerUnresolved) {
		t.Fatalf("missing journaled entry: %v", err)
	}
	if fixture.appends != 0 || len(fixture.installed) != 0 {
		t.Fatal("unresolved entry caused mutation")
	}
	fixture.guard.pending = false
	fixture.guard.rejectAdvance = true
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); err == nil {
		t.Fatal("unretained anchor authorized APP installation")
	}
	if len(fixture.installed) != 0 {
		t.Fatal("APP authority installed before anchor retention")
	}
}

func TestLegacyCoordinatorRequiresProtectedNeverEnrolledEvidence(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	locator, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.guard.generation = 0
	if err := coordinator.InstallRecoveryEntry(context.Background(), locator); err == nil {
		t.Fatal("generation zero without protected never-enrolled evidence accepted")
	}
	fixture.guard.neverEnrolled = true
	if err := coordinator.InstallRecoveryEntry(context.Background(), locator); err != nil {
		t.Fatal(err)
	}
	if len(fixture.installed) != 2 {
		t.Fatalf("recovery installs = %d", len(fixture.installed))
	}
	var command struct {
		MaintenanceRID string `json:"maintenance_rid"`
		Generation     uint64 `json:"maintenance_generation"`
	}
	if err := json.Unmarshal(fixture.installed[1], &command); err != nil {
		t.Fatal(err)
	}
	if command.MaintenanceRID != "recovery_01" || command.Generation != 0 {
		t.Fatalf("invalid never-enrolled binding: %+v", command)
	}
}

func TestLegacyCoordinatorRejectsDifferentDomainBeforeAppend(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	coordinator.domain = deletionTestDigest(120)
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); err == nil {
		t.Fatal("different W1 domain authorized a deletion append")
	}
	if fixture.appends != 0 || fixture.confirmations != 0 || len(fixture.installed) != 0 {
		t.Fatal("mismatched domain caused an external effect")
	}
}

func TestLegacyCoordinatorResolvesJournaledOutcomeWithoutReissuing(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	fixture.operation.State = DeletionStateLedgerCommitUnknown
	fixture.guard.pending = true
	fixture.entry.Request = fixture.operation.Request.AttemptNotCommitted(7)
	canonical, err := EncodeCanonicalLedgerEntry(fixture.entry)
	if err != nil {
		t.Fatal(err)
	}
	fixture.entry.Canonical = canonical
	fixture.entry.EntryHash = sha256.Sum256(canonical)
	fixture.committed = true
	entry := fixture.entry
	fixture.primary.Head = LedgerHead{Sequence: entry.Sequence, Hash: entry.EntryHash}
	fixture.primary.Entries = []LedgerChainEntry{{Sequence: entry.Sequence, PreviousHash: entry.PreviousHash, EntryHash: entry.EntryHash, Canonical: entry.Canonical, Route: LedgerRecordRoute, Record: &entry}}
	locator, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID)
	if err != nil || locator.EntryHash != entry.EntryHash {
		t.Fatalf("resolve original outcome: %+v %v", locator, err)
	}
	if fixture.appends != 0 || fixture.confirmations != 1 || len(fixture.installed) != 1 {
		t.Fatal("outcome was reissued or not witnessed")
	}
	var binding struct {
		EntryType    string `json:"entry_type"`
		ReleaseEpoch uint64 `json:"release_epoch"`
	}
	if err := json.Unmarshal(fixture.installed[0], &binding); err != nil {
		t.Fatal(err)
	}
	if binding.EntryType != string(LedgerEntryAttemptNotCommitted) || binding.ReleaseEpoch != 7 {
		t.Fatalf("wrong outcome binding: %+v", binding)
	}
}

func TestLegacyCoordinatorKeepsMissingUnknownOperationUnresolved(t *testing.T) {
	fixture, coordinator := newLegacyCoordinatorFixture(t)
	fixture.operation.State = DeletionStateLedgerCommitUnknown
	if _, err := coordinator.ReconcileOperation(context.Background(), fixture.operation.Request.OperationID); !errors.Is(err, ErrLegacyLedgerUnresolved) {
		t.Fatalf("unknown predecessor: %v", err)
	}
	if fixture.appends != 0 || fixture.confirmations != 0 || len(fixture.installed) != 0 {
		t.Fatal("missing predecessor entry caused an external mutation")
	}
}
