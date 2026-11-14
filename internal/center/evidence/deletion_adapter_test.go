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
	target := evidenceDeletionTestTarget(operation)
	got, err := adapter.PurgeDeletion(context.Background(), target)
	if err != nil || got != receipt {
		t.Fatalf("PurgeDeletion() = %#v, %v", got, err)
	}
	if store.command.SurfaceDigest != RecordEvidenceSurfaceDigest() || store.command.PurgeTarget != target {
		t.Fatalf("purge command = %#v", store.command)
	}
	if err := adapter.VerifyDeletion(context.Background(), target, got); err != nil || store.verifyCalls != 1 {
		t.Fatalf("VerifyDeletion() error/calls = %v/%d", err, store.verifyCalls)
	}
	if store.verified.PurgeTarget != target {
		t.Fatalf("evidence verification target = %#v, want %#v", store.verified.PurgeTarget, target)
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

func TestEvidenceDeletionAdapterRejectsMissingAndForeignOwner(t *testing.T) {
	operation := evidenceDeletionTestOperation()
	target := evidenceDeletionTestTarget(operation)
	store := &evidenceDeletionStoreStub{}
	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatal(err)
	}
	receipt := recorddeletion.AdapterPurgeReceipt{AdapterName: recorddeletion.AdapterNameRecordEvidence,
		OperationID: operation.OperationID, SurfaceDigest: RecordEvidenceSurfaceDigest(),
		ReceiptDigest: sha256.Sum256([]byte("receipt")), VerifiedAbsentAt: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	for _, claim := range []recorddeletion.OperationOwnerClaim{{}, {OperationID: "rpo_other", Owner: target.OwnerClaim.Owner}} {
		invalid := target
		invalid.OwnerClaim = claim
		if _, err := adapter.PurgeDeletion(context.Background(), invalid); !errors.Is(err, ErrInvalidDeletionAdapter) {
			t.Fatalf("PurgeDeletion(%#v) error = %v", claim, err)
		}
		if err := adapter.VerifyDeletion(context.Background(), invalid, receipt); !errors.Is(err, ErrInvalidDeletionAdapter) {
			t.Fatalf("VerifyDeletion(%#v) error = %v", claim, err)
		}
	}
	if store.purgeCalls != 0 || store.verifyCalls != 0 {
		t.Fatalf("unclaimed store calls purge=%d verify=%d", store.purgeCalls, store.verifyCalls)
	}
}

type evidenceDeletionStoreStub struct {
	health      recorddeletion.AdapterHealthSnapshot
	preview     recorddeletion.AdapterPreviewSnapshot
	receipt     recorddeletion.AdapterPurgeReceipt
	command     EvidenceDeletionCommand
	verified    EvidenceDeletionCommand
	purgeCalls  int
	verifyCalls int
}

func (store *evidenceDeletionStoreStub) EvidenceDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return store.health, nil
}

func (store *evidenceDeletionStoreStub) PreviewEvidenceDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	return store.preview, nil
}

func (store *evidenceDeletionStoreStub) PurgeRecordEvidence(_ context.Context, command EvidenceDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	store.command = command
	return store.receipt, nil
}

func (store *evidenceDeletionStoreStub) VerifyRecordEvidencePurge(_ context.Context, command EvidenceDeletionCommand, _ recorddeletion.AdapterPurgeReceipt) error {
	store.verified = command
	store.verifyCalls++
	return nil
}

func evidenceDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_evidence", ReservationID: "drs_evidence",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_evidence"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 1, LedgerSequence: 1, LedgerEntryHash: sha256.Sum256([]byte("ledger"))}
}

func evidenceDeletionTestTarget(operation recorddeletion.DeletionOperation) recorddeletion.PurgeTarget {
	return recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: recorddeletion.OperationOwnerClaim{
		OperationID: operation.OperationID,
		Owner: recordplatform.OwnerLease{OwnerID: "deletion_worker", Generation: 2,
			ExpiresAt: time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)},
	}}
}

func evidenceDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object: evidenceDeletionTestOperation().Object, CurrentRevisionID: "rrv_evidence",
		LockVersion: 1, AuthorizationEpoch: 1, ContentDeliveryEpoch: 1,
		DependencyGraphDigest: sha256.Sum256([]byte("graph")),
	}
}
