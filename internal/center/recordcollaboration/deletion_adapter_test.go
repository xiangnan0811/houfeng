package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestDeletionAdapterDelegatesClosedCollaborationContract(t *testing.T) {
	t.Parallel()

	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, collaborationDeletionTestDigest("health"))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: collaborationDeletionTestDigest("dependency"),
		ImpactDigest:     collaborationDeletionTestDigest("impact"),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	operation := collaborationDeletionTestOperation()
	verifiedAt := time.Date(2026, time.August, 17, 17, 30, 0, 0, time.UTC)
	claim := recorddeletion.OperationOwnerClaim{OperationID: operation.OperationID, Owner: recordplatform.OwnerLease{
		OwnerID: "collaboration_worker", Generation: 4, ExpiresAt: verifiedAt.Add(time.Hour),
	}}
	store := &collaborationDeletionStoreStub{health: health, preview: preview}
	expectedOperation := operation
	store.purge = func(command DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
		if command.Operation != expectedOperation || command.OwnerClaim != claim || command.SurfaceDigest != recorddeletion.RecordCollaborationSurfaceDigest() {
			t.Fatalf("collaboration purge command = %#v", command)
		}
		return recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordCollaboration,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    collaborationDeletionTestDigest("receipt"),
			RemovedRowCount:  19,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}

	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	descriptor := adapter.Descriptor()
	if descriptor.Name() != recorddeletion.AdapterNameRecordCollaboration ||
		!reflect.DeepEqual(descriptor.Surfaces(), recorddeletion.RecordCollaborationSurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
	if got, err := adapter.HealthSnapshot(context.Background()); err != nil || got != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", got, err)
	}
	if got, err := adapter.PreviewDeletion(context.Background(), collaborationDeletionTestPreviewTarget()); err != nil || !reflect.DeepEqual(got, preview) {
		t.Fatalf("PreviewDeletion() = %#v, %v", got, err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	receipt, err := adapter.PurgeDeletion(context.Background(), target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	legacy := collaborationDeletionTestOperation()
	legacy.ContractVersion = recorddeletion.DeletionContractV1
	legacy.LedgerSequence = 11
	legacy.LedgerEntryHash = collaborationDeletionTestDigest("legacy ledger")
	legacyTarget := recorddeletion.PurgeTarget{Operation: legacy, OwnerClaim: claim}
	if err := legacyTarget.Validate(); err != nil {
		t.Fatalf("legacy target must otherwise be valid: %v", err)
	}
	expectedOperation = legacy
	legacyReceipt, err := adapter.PurgeDeletion(context.Background(), legacyTarget)
	if err != nil {
		t.Fatalf("PurgeDeletion(legacy continuation) error = %v", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), legacyTarget, legacyReceipt); err != nil {
		t.Fatalf("VerifyDeletion(legacy continuation) error = %v", err)
	}
	invalid := legacyTarget
	invalid.OwnerClaim.OperationID = "rpo_other"
	if _, err := adapter.PurgeDeletion(context.Background(), invalid); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("PurgeDeletion(invalid owner) error = %v", err)
	}
	invalid = legacyTarget
	invalid.Operation.ContractVersion = 0
	if err := adapter.VerifyDeletion(context.Background(), invalid, legacyReceipt); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("VerifyDeletion(unknown version) error = %v", err)
	}
	if store.healthCalls != 1 || store.previewCalls != 1 || store.purgeCalls != 2 || store.verifyCalls != 2 {
		t.Fatalf("store calls health=%d preview=%d purge=%d verify=%d", store.healthCalls, store.previewCalls, store.purgeCalls, store.verifyCalls)
	}
}

func TestDeletionAdapterRejectsTypedNilAndInvalidStoreProofs(t *testing.T) {
	t.Parallel()

	var typedNil *collaborationDeletionStoreStub
	if _, err := NewDeletionAdapter(typedNil); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("NewDeletionAdapter(typed nil) error = %v", err)
	}
	adapter, err := NewDeletionAdapter(&collaborationDeletionStoreStub{})
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.HealthSnapshot(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("HealthSnapshot(invalid) error = %v", err)
	}
	if _, err := adapter.PreviewDeletion(context.Background(), collaborationDeletionTestPreviewTarget()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PreviewDeletion(invalid) error = %v", err)
	}
}

type collaborationDeletionStoreStub struct {
	health       recorddeletion.AdapterHealthSnapshot
	preview      recorddeletion.AdapterPreviewSnapshot
	purge        func(DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	healthCalls  int
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (store *collaborationDeletionStoreStub) CollaborationDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *collaborationDeletionStoreStub) PreviewCollaborationDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *collaborationDeletionStoreStub) PurgeRecordCollaboration(_ context.Context, command DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return store.purge(command)
}

func (store *collaborationDeletionStoreStub) VerifyRecordCollaborationPurge(context.Context, DeletionCommand, recorddeletion.AdapterPurgeReceipt) error {
	store.verifyCalls++
	return nil
}

func collaborationDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_collaboration01", ReservationID: "drs_collaboration01",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_collaboration01"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7}
}

func collaborationDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object:                collaborationDeletionTestOperation().Object,
		CurrentRevisionID:     "rrv_collaboration01",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: collaborationDeletionTestDigest("graph"),
	}
}

func collaborationDeletionTestDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
