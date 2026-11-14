package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

var _ DeletionStore = (*store.PostgresRecordDeletionRepository)(nil)

func TestDeletionAdapterRejectsUnboundOwner(t *testing.T) {
	t.Parallel()

	for _, version := range []recorddeletion.DeletionContractVersion{recorddeletion.DeletionContractV1, recorddeletion.DeletionContractV2} {
		target := portabilityDeletionTestPurgeTarget()
		target.Operation.ContractVersion = version
		if version == recorddeletion.DeletionContractV2 {
			target.Operation.LedgerSequence = 0
			target.Operation.LedgerEntryHash = [sha256.Size]byte{}
		}
		store := &portabilityDeletionStoreStub{}
		adapter, err := NewDeletionAdapter(store)
		if err != nil {
			t.Fatalf("NewDeletionAdapter() error = %v", err)
		}
		receipt := recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordPortability,
			OperationID:      target.Operation.OperationID,
			SurfaceDigest:    recorddeletion.RecordPortabilitySurfaceDigest(),
			ReceiptDigest:    portabilityDeletionTestDigest("receipt"),
			VerifiedAbsentAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		}
		if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.Descriptor()); err != nil {
			t.Fatalf("contract %d valid target and receipt rejected: %v", version, err)
		}
		for _, tc := range []struct {
			name  string
			claim recorddeletion.OperationOwnerClaim
		}{
			{"missing", recorddeletion.OperationOwnerClaim{}},
			{"other operation", recorddeletion.OperationOwnerClaim{OperationID: "rpo_other", Owner: target.OwnerClaim.Owner}},
			{"zero generation", recorddeletion.OperationOwnerClaim{OperationID: target.Operation.OperationID, Owner: recordplatform.OwnerLease{
				OwnerID: target.OwnerClaim.Owner.OwnerID, ExpiresAt: target.OwnerClaim.Owner.ExpiresAt,
			}}},
		} {
			t.Run(fmt.Sprintf("contract_%d/%s", version, tc.name), func(t *testing.T) {
				invalid := target
				invalid.OwnerClaim = tc.claim
				command := DeletionCommand{PurgeTarget: invalid, SurfaceDigest: recorddeletion.RecordPortabilitySurfaceDigest()}
				if err := command.Validate(); !errors.Is(err, ErrInvalidDeletionAdapter) {
					t.Fatalf("Validate() error = %v, want ErrInvalidDeletionAdapter", err)
				}
				if _, err := adapter.PurgeDeletion(context.Background(), invalid); !errors.Is(err, ErrInvalidDeletionAdapter) {
					t.Fatalf("PurgeDeletion() error = %v, want ErrInvalidDeletionAdapter", err)
				}
				if err := adapter.VerifyDeletion(context.Background(), invalid, receipt); !errors.Is(err, ErrInvalidDeletionAdapter) {
					t.Fatalf("VerifyDeletion() error = %v, want ErrInvalidDeletionAdapter", err)
				}
				if store.purgeCalls != 0 || store.verifyCalls != 0 {
					t.Fatalf("invalid owner reached store: purge=%d verify=%d", store.purgeCalls, store.verifyCalls)
				}
			})
		}
	}
}

func TestDeletionAdapterRejectsTypedNilAndInvalidStoreProofs(t *testing.T) {
	t.Parallel()

	var typedNil *portabilityDeletionStoreStub
	if _, err := NewDeletionAdapter(typedNil); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("NewDeletionAdapter(typed nil) error = %v", err)
	}
	adapter, err := NewDeletionAdapter(&portabilityDeletionStoreStub{})
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.HealthSnapshot(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("HealthSnapshot(invalid) error = %v", err)
	}
	if _, err := adapter.PreviewDeletion(context.Background(), portabilityDeletionTestPreviewTarget()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PreviewDeletion(invalid) error = %v", err)
	}
}

func TestDeletionCommandRejectsForeignSurfaceDigest(t *testing.T) {
	t.Parallel()

	target := portabilityDeletionTestPurgeTarget()
	valid := DeletionCommand{PurgeTarget: target, SurfaceDigest: recorddeletion.RecordPortabilitySurfaceDigest()}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	for name, digest := range map[string][sha256.Size]byte{
		"empty":  {},
		"search": recorddeletion.RecordSearchSurfaceDigest(),
		"core":   recorddeletion.RecordCoreSurfaceDigest(),
	} {
		t.Run(name, func(t *testing.T) {
			command := DeletionCommand{PurgeTarget: target, SurfaceDigest: digest}
			if err := command.Validate(); !errors.Is(err, ErrInvalidDeletionAdapter) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionAdapter", err)
			}
		})
	}
}

type portabilityDeletionStoreStub struct {
	purgeCalls  int
	verifyCalls int
}

func (*portabilityDeletionStoreStub) PortabilityDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return recorddeletion.AdapterHealthSnapshot{}, nil
}

func (*portabilityDeletionStoreStub) PreviewPortabilityDeletion(
	context.Context,
	recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	return recorddeletion.AdapterPreviewSnapshot{}, nil
}

func (store *portabilityDeletionStoreStub) PurgeRecordPortability(
	context.Context,
	recorddeletion.PurgeTarget,
	[sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return recorddeletion.AdapterPurgeReceipt{}, nil
}

func (store *portabilityDeletionStoreStub) VerifyRecordPortabilityPurge(
	context.Context,
	recorddeletion.PurgeTarget,
	[sha256.Size]byte,
	recorddeletion.AdapterPurgeReceipt,
) error {
	store.verifyCalls++
	return nil
}

func portabilityDeletionTestPurgeTarget() recorddeletion.PurgeTarget {
	operation := portabilityDeletionTestOperation()
	return recorddeletion.PurgeTarget{
		Operation: operation,
		OwnerClaim: recorddeletion.OperationOwnerClaim{
			OperationID: operation.OperationID,
			Owner: recordplatform.OwnerLease{
				OwnerID: "portability_worker", Generation: 1,
				ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}
}

func portabilityDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_port01", ReservationID: "drs_port01",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_port01"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: portabilityDeletionTestDigest("ledger")}
}

func portabilityDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object:                portabilityDeletionTestOperation().Object,
		CurrentRevisionID:     "rrv_port01",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: portabilityDeletionTestDigest("graph"),
	}
}

func portabilityDeletionTestDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
