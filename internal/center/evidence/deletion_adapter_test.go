package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestEvidenceDeletionAdapterDelegatesClosedOwnedSurfaceContract(t *testing.T) {
	health, _ := recorddeletion.NewAdapterHealthSnapshot(true, 1, sha256.Sum256([]byte("health")))
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: sha256.Sum256([]byte("dependency")),
		ImpactDigest:     sha256.Sum256([]byte("impact")),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	operation := evidenceDeletionTestOperation()
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordEvidence, OperationID: operation.OperationID,
		SurfaceDigest: RecordEvidenceSurfaceDigest(), ReceiptDigest: sha256.Sum256([]byte("receipt")),
		RemovedRowCount: 4, VerifiedAbsentAt: time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
	}
	store := &evidenceDeletionStoreStub{health: health, preview: preview, receipt: receipt}
	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if adapter.Descriptor().Name() != recorddeletion.AdapterNameRecordEvidence {
		t.Fatalf("descriptor = %#v", adapter.Descriptor())
	}
	if _, err := adapter.HealthSnapshot(context.Background()); err != nil {
		t.Fatalf("HealthSnapshot() error = %v", err)
	}
	if _, err := adapter.PreviewDeletion(context.Background(), evidenceDeletionTestPreviewTarget()); err != nil {
		t.Fatalf("PreviewDeletion() error = %v", err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
	got, err := adapter.PurgeDeletion(context.Background(), target)
	if err != nil || got != receipt {
		t.Fatalf("PurgeDeletion() = %#v, %v", got, err)
	}
	if store.command.SurfaceDigest != RecordEvidenceSurfaceDigest() || store.command.Operation.OperationID != operation.OperationID {
		t.Fatalf("purge command = %#v", store.command)
	}
	if err := adapter.VerifyDeletion(context.Background(), target, got); err != nil || store.verifyCalls != 1 {
		t.Fatalf("VerifyDeletion() error/calls = %v/%d", err, store.verifyCalls)
	}
}

func TestEvidenceDeletionAdapterRejectsTypedNilAndInvalidStoreProofs(t *testing.T) {
	var typedNil *evidenceDeletionStoreStub
	if _, err := NewDeletionAdapter(typedNil); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("NewDeletionAdapter(typed nil) error = %v", err)
	}
	adapter, err := NewDeletionAdapter(&evidenceDeletionStoreStub{})
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.HealthSnapshot(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("invalid health error = %v", err)
	}
}

type evidenceDeletionStoreStub struct {
	health      recorddeletion.AdapterHealthSnapshot
	preview     recorddeletion.AdapterPreviewSnapshot
	receipt     recorddeletion.AdapterPurgeReceipt
	command     EvidenceDeletionCommand
	verifyCalls int
}

func (store *evidenceDeletionStoreStub) EvidenceDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return store.health, nil
}

func (store *evidenceDeletionStoreStub) PreviewEvidenceDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	return store.preview, nil
}

func (store *evidenceDeletionStoreStub) PurgeRecordEvidence(_ context.Context, command EvidenceDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	store.command = command
	return store.receipt, nil
}

func (store *evidenceDeletionStoreStub) VerifyRecordEvidencePurge(context.Context, EvidenceDeletionCommand, recorddeletion.AdapterPurgeReceipt) error {
	store.verifyCalls++
	return nil
}

func evidenceDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID: "rpo_evidence", ReservationID: "drs_evidence",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_evidence"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 1, LedgerSequence: 1, LedgerEntryHash: sha256.Sum256([]byte("ledger")),
	}
}

func evidenceDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object: evidenceDeletionTestOperation().Object, CurrentRevisionID: "rrv_evidence",
		LockVersion: 1, AuthorizationEpoch: 1, ContentDeliveryEpoch: 1,
		DependencyGraphDigest: sha256.Sum256([]byte("graph")),
	}
}
