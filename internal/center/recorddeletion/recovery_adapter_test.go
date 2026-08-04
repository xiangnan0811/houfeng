package recorddeletion

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/recordplatform"
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

func TestRecoveryAdapterReplaysDeleteCommitWithExactCoreAndMinimalAuditSurfaces(t *testing.T) {
	t.Parallel()

	entry := deletionTestLedgerEntry(t, deletionTestLedgerRequest(t, LedgerEntryDeleteCommit, 0))
	entry.Sequence = 12
	entry.EntryHash = deletionTestDigest(101)
	request := deletionTestRecoveryRequest(t, entry, 11, deletionTestDigest(100))
	store := &deletionRecoveryStoreStub{}
	store.apply = func(command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
		if !command.PurgeContent || !reflect.DeepEqual(command.AllowedSurfaces, RecordDeleteRecoverySurfaces()) {
			t.Fatalf("delete replay command = %#v", command)
		}
		if command.RequestFingerprintBytes != request.RequestFingerprintBytes {
			t.Fatalf("recovery fingerprint bytes = %x, want %x", command.RequestFingerprintBytes, request.RequestFingerprintBytes)
		}
		return RecoveryReplayReceipt{
			Sequence:      entry.Sequence,
			EntryHash:     entry.EntryHash,
			SurfaceDigest: command.SurfaceDigest,
			ContentPurged: true,
		}, nil
	}
	adapter, err := NewRecoveryAdapter(store)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}

	receipt, err := adapter.Replay(context.Background(), request)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if receipt.Sequence != entry.Sequence || !receipt.ContentPurged || store.calls != 1 {
		t.Fatalf("Replay() = %#v calls=%d", receipt, store.calls)
	}
	store.last.AllowedSurfaces[0] = "tampered"
	if !reflect.DeepEqual(RecordDeleteRecoverySurfaces(), deletionRecoveryDeleteSurfacesForTest()) {
		t.Fatal("store mutation changed recovery allowlist")
	}
}

func TestRecoveryAdapterReplaysNotCommittedWithoutCorePurgeOrTombstoneSurfaces(t *testing.T) {
	t.Parallel()

	entry := deletionTestLedgerEntry(t, deletionTestLedgerRequest(t, LedgerEntryAttemptNotCommitted, 5))
	entry.Sequence = 13
	entry.EntryHash = deletionTestDigest(103)
	request := deletionTestRecoveryRequest(t, entry, 12, deletionTestDigest(102))
	store := &deletionRecoveryStoreStub{}
	store.apply = func(command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
		if command.PurgeContent || !reflect.DeepEqual(command.AllowedSurfaces, RecordNotCommittedRecoverySurfaces()) {
			t.Fatalf("not-committed replay command = %#v", command)
		}
		for _, forbidden := range []RecoverySurface{
			RecoverySurfaceRecords,
			RecoverySurfaceRecordRevisions,
			RecoverySurfaceRecordCorePurgeReceipts,
		} {
			for _, surface := range command.AllowedSurfaces {
				if surface == forbidden {
					t.Fatalf("not-committed replay contains purge surface %q", forbidden)
				}
			}
		}
		return RecoveryReplayReceipt{
			Sequence:      entry.Sequence,
			EntryHash:     entry.EntryHash,
			SurfaceDigest: command.SurfaceDigest,
			ContentPurged: false,
		}, nil
	}
	adapter, err := NewRecoveryAdapter(store)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}

	receipt, err := adapter.Replay(context.Background(), request)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if receipt.ContentPurged || store.calls != 1 {
		t.Fatalf("Replay() = %#v calls=%d, want outcome-only replay", receipt, store.calls)
	}
}

func TestRecoveryAdapterFailsClosedBeforeStoreForUnknownOrDiscontinuousContracts(t *testing.T) {
	t.Parallel()

	validEntry := deletionTestLedgerEntry(t, deletionTestLedgerRequest(t, LedgerEntryDeleteCommit, 0))
	validEntry.Sequence = 12
	validEntry.EntryHash = deletionTestDigest(111)
	valid := deletionTestRecoveryRequest(t, validEntry, 11, deletionTestDigest(110))
	for _, tt := range []struct {
		name   string
		mutate func(*RecoveryReplayRequest)
	}{
		{name: "unknown contract", mutate: func(request *RecoveryReplayRequest) { request.Entry.Request.DeletionContractVersion = 2 }},
		{name: "unknown entry type", mutate: func(request *RecoveryReplayRequest) { request.Entry.Request.EntryType = "future_entry" }},
		{name: "unknown object kind", mutate: func(request *RecoveryReplayRequest) { request.Entry.Request.Object.ObjectKind = "future_object" }},
		{name: "sequence gap", mutate: func(request *RecoveryReplayRequest) { request.Cursor.Sequence = 9 }},
		{name: "previous hash mismatch", mutate: func(request *RecoveryReplayRequest) { request.PreviousHash[0] ^= 0xff }},
		{name: "missing witness proof", mutate: func(request *RecoveryReplayRequest) { request.WitnessProofDigest = [32]byte{} }},
		{name: "missing canonical fingerprint bytes", mutate: func(request *RecoveryReplayRequest) { request.RequestFingerprintBytes = [32]byte{} }},
		{name: "mismatched canonical fingerprint bytes", mutate: func(request *RecoveryReplayRequest) { request.RequestFingerprintBytes[0] ^= 0xff }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.mutate(&request)
			store := &deletionRecoveryStoreStub{}
			adapter, err := NewRecoveryAdapter(store)
			if err != nil {
				t.Fatalf("NewRecoveryAdapter() error = %v", err)
			}
			if _, err := adapter.Replay(context.Background(), request); !errors.Is(err, ErrRecoveryContractUnavailable) {
				t.Fatalf("Replay() error = %v, want ErrRecoveryContractUnavailable", err)
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want zero", store.calls)
			}
		})
	}
}

func TestRecoveryAdapterRejectsDivergentStoreReceiptAndTypedNilStore(t *testing.T) {
	t.Parallel()

	entry := deletionTestLedgerEntry(t, deletionTestLedgerRequest(t, LedgerEntryDeleteCommit, 0))
	entry.Sequence = 2
	entry.EntryHash = deletionTestDigest(121)
	request := deletionTestRecoveryRequest(t, entry, 1, deletionTestDigest(120))
	store := &deletionRecoveryStoreStub{apply: func(command RecoveryReplayCommand) (RecoveryReplayReceipt, error) {
		return RecoveryReplayReceipt{
			Sequence:      command.Entry.Sequence,
			EntryHash:     command.Entry.EntryHash,
			SurfaceDigest: deletionTestDigest(122),
			ContentPurged: true,
		}, nil
	}}
	adapter, err := NewRecoveryAdapter(store)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	if _, err := adapter.Replay(context.Background(), request); !errors.Is(err, ErrRecoveryContractUnavailable) {
		t.Fatalf("Replay() divergent receipt error = %v, want ErrRecoveryContractUnavailable", err)
	}

	var typedNil *deletionRecoveryStoreStub
	if _, err := NewRecoveryAdapter(typedNil); !errors.Is(err, ErrInvalidRecoveryAdapter) {
		t.Fatalf("NewRecoveryAdapter(typed nil) error = %v, want ErrInvalidRecoveryAdapter", err)
	}
}

func deletionTestRecoveryRequest(
	t *testing.T,
	entry DeletionLedgerEntry,
	cursorSequence uint64,
	cursorHash [32]byte,
) RecoveryReplayRequest {
	t.Helper()
	return RecoveryReplayRequest{
		Cursor: RecoveryReplayCursor{
			Sequence:  cursorSequence,
			EntryHash: cursorHash,
		},
		Entry:                   entry,
		PreviousHash:            cursorHash,
		WitnessProofDigest:      deletionTestDigest(115),
		RequestFingerprintBytes: deletionTestRecoveryFingerprintBytes(t),
	}
}

func deletionTestRecoveryFingerprintBytes(t *testing.T) [32]byte {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   deletionTestDigest(71),
		RequestScopeDigest: deletionTestDigest(72),
		PayloadDigest:      deletionTestDigest(73),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	bytes, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	return bytes
}

func deletionRecoveryDeleteSurfacesForTest() []RecoverySurface {
	return []RecoverySurface{
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
