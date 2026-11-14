package attachments

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestDeletionAdapterRejectsMissingOwnerClaim(t *testing.T) {
	t.Parallel()

	store := &attachmentDeletionStoreStub{}
	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatal(err)
	}
	// An otherwise valid operation cannot authorize a purge without its owner claim.
	target := recorddeletion.PurgeTarget{Operation: attachmentDeletionTestOperation()}
	if err := target.Operation.Validate(); err != nil {
		t.Fatalf("operation fixture invalid: %v", err)
	}
	if _, err := adapter.PurgeDeletion(context.Background(), target); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("PurgeDeletion() error = %v, want missing owner claim rejection", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), target, recorddeletion.AdapterPurgeReceipt{}); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("VerifyDeletion() error = %v, want missing owner claim rejection", err)
	}
	if store.purgeCalled || store.verifyCalled {
		t.Fatalf("target without owner claim reached storage: purge=%t verify=%t", store.purgeCalled, store.verifyCalled)
	}
}

type attachmentDeletionStoreStub struct {
	AttachmentDeletionStore
	purgeCalled  bool
	verifyCalled bool
}

func (store *attachmentDeletionStoreStub) PurgeRecordAttachments(context.Context, AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalled = true
	return recorddeletion.AdapterPurgeReceipt{}, nil
}

func (store *attachmentDeletionStoreStub) VerifyRecordAttachmentsPurge(context.Context, AttachmentDeletionCommand, recorddeletion.AdapterPurgeReceipt) error {
	store.verifyCalled = true
	return nil
}

func attachmentDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_attachment01", ReservationID: "drs_attachment01",
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: "rec_attachment01",
		},
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch:      7,
		LedgerSequence:  11,
		LedgerEntryHash: attachmentDeletionTestDigest(6)}
}

func attachmentDeletionTestDigest(seed byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = seed + byte(index)
	}
	return digest
}
