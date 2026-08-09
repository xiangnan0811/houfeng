package attachments

import (
	"context"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestDeletionAdapterDelegatesClosedAttachmentContract(t *testing.T) {
	t.Parallel()

	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, attachmentDeletionTestDigest(1))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: attachmentDeletionTestDigest(2),
		ImpactDigest:     attachmentDeletionTestDigest(3),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{
			{Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: 2},
		},
	}
	operation := attachmentDeletionTestOperation()
	verifiedAt := time.Date(2026, time.August, 9, 18, 0, 0, 0, time.UTC)
	store := &attachmentDeletionStoreStub{health: health, preview: preview}
	store.purge = func(command AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
		if command.Operation != operation || command.SurfaceDigest != recorddeletion.RecordAttachmentsSurfaceDigest() {
			t.Fatalf("attachment purge command = %#v", command)
		}
		return recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordAttachments,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    attachmentDeletionTestDigest(4),
			RemovedRowCount:  12,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}

	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	descriptor := adapter.Descriptor()
	if descriptor.Name() != recorddeletion.AdapterNameRecordAttachments ||
		!reflect.DeepEqual(descriptor.Surfaces(), recorddeletion.RecordAttachmentsSurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
	if got, err := adapter.HealthSnapshot(context.Background()); err != nil || got != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", got, err)
	}
	target := recorddeletion.PreviewTarget{
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: operation.Object.ObjectID,
		},
		CurrentRevisionID:     "rrv_current01",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: attachmentDeletionTestDigest(5),
	}
	if got, err := adapter.PreviewDeletion(context.Background(), target); err != nil || !reflect.DeepEqual(got, preview) {
		t.Fatalf("PreviewDeletion() = %#v, %v", got, err)
	}
	purgeTarget := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := adapter.PurgeDeletion(context.Background(), purgeTarget)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), purgeTarget, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	if store.healthCalls != 1 || store.previewCalls != 1 || store.purgeCalls != 1 || store.verifyCalls != 1 {
		t.Fatalf("store calls health=%d preview=%d purge=%d verify=%d", store.healthCalls, store.previewCalls, store.purgeCalls, store.verifyCalls)
	}
}

type attachmentDeletionStoreStub struct {
	health       recorddeletion.AdapterHealthSnapshot
	preview      recorddeletion.AdapterPreviewSnapshot
	purge        func(AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	healthCalls  int
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (store *attachmentDeletionStoreStub) AttachmentDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *attachmentDeletionStoreStub) PreviewAttachmentDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *attachmentDeletionStoreStub) PurgeRecordAttachments(_ context.Context, command AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return store.purge(command)
}

func (store *attachmentDeletionStoreStub) VerifyRecordAttachmentsPurge(context.Context, AttachmentDeletionCommand, recorddeletion.AdapterPurgeReceipt) error {
	store.verifyCalls++
	return nil
}

func attachmentDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID:   "rpo_attachment01",
		ReservationID: "drs_attachment01",
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: "rec_attachment01",
		},
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch:      7,
		LedgerSequence:  11,
		LedgerEntryHash: attachmentDeletionTestDigest(6),
	}
}

func attachmentDeletionTestDigest(seed byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = seed + byte(index)
	}
	return digest
}
