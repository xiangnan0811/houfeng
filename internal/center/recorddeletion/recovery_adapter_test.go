package recorddeletion

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRecoverySurfaceAllowlistsAreClosedOrderedAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	deleteWant := []RecoverySurface{
		RecoverySurface("content_delivery_epochs"),
		RecoverySurface("deletion_fence_leases"),
		RecoverySurfaceDeletionReplayState,
		RecoverySurfaceDeletionReservations,
		RecoverySurfaceRecordCorePurgeReceipts,
		RecoverySurfaceRecordDeletionAudits,
		RecoverySurfaceRecordDomainActivities,
		RecoverySurfaceRecordDraftCheckpoints,
		RecoverySurfaceRecordDrafts,
		RecoverySurfaceRecordPurgeOperations,
		RecoverySurfaceRecordRevisionParticipants,
		RecoverySurfaceRecordRevisionSubjects,
		RecoverySurfaceRecordRevisionTags,
		RecoverySurfaceRecordRevisions,
		RecoverySurfaceRecords,
	}
	outcomeWant := []RecoverySurface{
		RecoverySurface("deletion_fence_leases"),
		RecoverySurfaceDeletionReplayState,
		RecoverySurfaceDeletionReservations,
		RecoverySurfaceRecordDeletionAudits,
		RecoverySurfaceRecordPurgeOperations,
	}
	deleteGot := RecordDeleteRecoverySurfaces()
	outcomeGot := RecordNotCommittedRecoverySurfaces()
	if !reflect.DeepEqual(deleteGot, deleteWant) || !reflect.DeepEqual(outcomeGot, outcomeWant) {
		t.Fatalf("recovery surfaces delete=%#v outcome=%#v", deleteGot, outcomeGot)
	}
	deleteGot[0] = "tampered"
	outcomeGot[0] = "tampered"
	if !reflect.DeepEqual(RecordDeleteRecoverySurfaces(), deleteWant) || !reflect.DeepEqual(RecordNotCommittedRecoverySurfaces(), outcomeWant) {
		t.Fatal("recovery surface allowlist changed after caller mutation")
	}
}

func TestRecoveryAdapterUsesVerifiedEntryAndHeldFence(t *testing.T) {
	t.Parallel()
	for _, kind := range []LedgerEntryType{LedgerEntryDeleteCommit, LedgerEntryAttemptNotCommitted} {
		t.Run(string(kind), func(t *testing.T) {
			request := ledgerTestRequest(t)
			if kind == LedgerEntryAttemptNotCommitted {
				request = request.AttemptNotCommitted(5)
			}
			verifier, fence, locator := recoveryTestVerifier(t, request)
			store := &deletionRecoveryStoreStub{apply: func(command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
				if err := command.Validate(); err != nil {
					t.Fatalf("verified replay command invalid: %v", err)
				}
				entry := command.VerifiedEntry.Entry()
				if entry.Request.EntryType != kind || entry.Sequence != locator.Sequence || command.MaintenanceRID != "recovery_1" || command.MaintenanceGeneration != 2 {
					t.Fatalf("verified replay command = %#v", command)
				}
				wantSurfaces := RecordDeleteRecoverySurfaces()
				if kind == LedgerEntryAttemptNotCommitted {
					wantSurfaces = RecordNotCommittedRecoverySurfaces()
				}
				if !reflect.DeepEqual(command.AllowedSurfaces, wantSurfaces) || command.PurgeContent != (kind == LedgerEntryDeleteCommit) {
					t.Fatalf("wrong recovery surfaces: %#v", command)
				}
				return RecoveryReplayReceipt{Sequence: locator.Sequence, EntryHash: locator.EntryHash,
					SurfaceDigest: command.SurfaceDigest, ContentPurged: command.PurgeContent}, nil
			}}
			adapter, err := NewRecoveryAdapter(store, verifier, fence)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := adapter.Replay(context.Background(), RecoveryReplayRequest{Locator: locator})
			if err != nil || receipt.Sequence != locator.Sequence || receipt.ContentPurged != (kind == LedgerEntryDeleteCommit) || store.calls != 1 {
				t.Fatalf("Replay() = %#v, %v; calls=%d", receipt, err, store.calls)
			}
			fence.held = false
			if err := store.last.WithHeldMaintenance(context.Background(), func(context.Context) error { return nil }); err == nil {
				t.Fatal("retained command remained usable after its fence was released")
			}
		})
	}
}

func TestRecoveryAdapterRejectsUnwitnessedOrUnboundReplay(t *testing.T) {
	t.Parallel()
	verifier, fence, locator := recoveryTestVerifier(t, ledgerTestRequest(t))
	store := &deletionRecoveryStoreStub{}
	adapter, err := NewRecoveryAdapter(store, verifier, fence)
	if err != nil {
		t.Fatal(err)
	}
	wrong := locator
	wrong.EntryHash[0] ^= 1
	if _, err := adapter.Replay(context.Background(), RecoveryReplayRequest{Locator: wrong}); err == nil {
		t.Fatal("mismatched witnessed entry accepted")
	}
	if _, err := adapter.Replay(context.Background(), RecoveryReplayRequest{Locator: locator, Cursor: RecoveryReplayCursor{Sequence: 1, EntryHash: locator.EntryHash}}); !errors.Is(err, ErrRecoveryContractUnavailable) {
		t.Fatalf("discontinuous replay error = %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("unverified replay reached store %d times", store.calls)
	}
	if _, err := NewRecoveryAdapter(store, verifier, &recoveryTestFence{held: true, rid: "recovery_1", generation: 2}); !errors.Is(err, ErrInvalidRecoveryAdapter) {
		t.Fatalf("different fence accepted: %v", err)
	}
	var typedNil *deletionRecoveryStoreStub
	if _, err := NewRecoveryAdapter(typedNil, verifier, fence); !errors.Is(err, ErrInvalidRecoveryAdapter) {
		t.Fatalf("typed nil store accepted: %v", err)
	}
}

func TestRecoveryAdapterRejectsDivergentStoreReceipt(t *testing.T) {
	t.Parallel()
	verifier, fence, locator := recoveryTestVerifier(t, ledgerTestRequest(t))
	store := &deletionRecoveryStoreStub{apply: func(command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
		return RecoveryReplayReceipt{Sequence: locator.Sequence, EntryHash: locator.EntryHash,
			SurfaceDigest: deletionTestDigest(122), ContentPurged: true}, nil
	}}
	adapter, err := NewRecoveryAdapter(store, verifier, fence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Replay(context.Background(), RecoveryReplayRequest{Locator: locator}); !errors.Is(err, ErrRecoveryContractUnavailable) {
		t.Fatalf("divergent receipt accepted: %v", err)
	}
}

func recoveryTestVerifier(t *testing.T, request LedgerAppendRequest) (*LedgerVerifier, *recoveryTestFence, LedgerLocator) {
	t.Helper()
	primaryID := ledgerTestIdentity("a", "deletion_ledger", "1", "ledger")
	witnessID := ledgerTestIdentity("b", "deletion_witness", "2", "witness")
	domain, err := LedgerDomainDigest(primaryID.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry := ledgerTestEntry(t, domain, 1, [32]byte{}, request)
	primary := LedgerSnapshot{Identity: primaryID, Head: LedgerHead{Sequence: 1, Hash: entry.EntryHash}, Entries: []LedgerChainEntry{ledgerTestRow(entry)}}
	witness := LedgerSnapshot{Identity: witnessID, Head: LedgerHead{Sequence: 1, Hash: entry.EntryHash}, Entries: []LedgerChainEntry{ledgerTestRow(entry)}}
	fence := &recoveryTestFence{held: true, rid: "recovery_1", generation: 2}
	verifier, err := NewLedgerVerifier(ledgerTestReader{primary}, ledgerTestReader{witness}, LedgerVerificationTrust{Primary: primaryID, Witness: witnessID, Continuity: fence})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, fence, LedgerLocator{LedgerDomain: domain, Sequence: entry.Sequence, EntryHash: entry.EntryHash}
}

type recoveryTestFence struct {
	held       bool
	rid        string
	generation uint64
}

func (fence *recoveryTestFence) WithHeldLedgerFence(ctx context.Context, apply func(context.Context) error) error {
	if !fence.held {
		return errors.New("maintenance fence closed")
	}
	return apply(ctx)
}

func (fence *recoveryTestFence) RecoveryBinding() (string, uint64, error) {
	if !fence.held {
		return "", 0, errors.New("maintenance fence closed")
	}
	return fence.rid, fence.generation, nil
}

func (fence *recoveryTestFence) NeverEnrolled() (bool, error) {
	if !fence.held {
		return false, errors.New("maintenance fence closed")
	}
	return fence.generation == 0, nil
}

func (fence *recoveryTestFence) VerifyLedgerContinuity(context.Context, LedgerContinuityObservation) error {
	if !fence.held {
		return errors.New("maintenance fence closed")
	}
	return nil
}

type deletionRecoveryStoreStub struct {
	apply func(RecoveryReplayCommand) (RecoveryReplayReceipt, error)
	last  RecoveryReplayCommand
	calls int
}

func (store *deletionRecoveryStoreStub) ApplyRecoveryEntry(_ context.Context, command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
	store.calls++
	store.last = command
	if store.apply != nil {
		return store.apply(command)
	}
	return RecoveryReplayReceipt{}, nil
}
