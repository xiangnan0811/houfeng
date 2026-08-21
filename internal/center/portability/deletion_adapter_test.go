package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

var _ DeletionStore = (*store.PostgresRecordDeletionRepository)(nil)

func TestDeletionAdapterDelegatesClosedPortabilityContract(t *testing.T) {
	t.Parallel()

	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, portabilityDeletionTestDigest("health"))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: portabilityDeletionTestDigest("dependency"),
		ImpactDigest:     portabilityDeletionTestDigest("impact"),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{{
			Kind:      recorddeletion.SurvivingCopyKindDeliveredExport,
			CopyCount: 1,
		}},
	}
	operation := portabilityDeletionTestOperation()
	verifiedAt := time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC)
	store := &portabilityDeletionStoreStub{health: health, preview: preview}
	store.purge = func(operation recorddeletion.DeletionOperation, digest [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error) {
		if operation != portabilityDeletionTestOperation() || digest != recorddeletion.RecordPortabilitySurfaceDigest() {
			t.Fatalf("portability purge command = %#v digest=%x", operation, digest)
		}
		return recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordPortability,
			OperationID:      operation.OperationID,
			SurfaceDigest:    digest,
			ReceiptDigest:    portabilityDeletionTestDigest("receipt"),
			RemovedRowCount:  3,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}

	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	descriptor := adapter.Descriptor()
	if descriptor.Name() != recorddeletion.AdapterNameRecordPortability ||
		!reflect.DeepEqual(descriptor.Surfaces(), recorddeletion.RecordPortabilitySurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
	if got, err := adapter.HealthSnapshot(context.Background()); err != nil || got != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", got, err)
	}
	if got, err := adapter.PreviewDeletion(context.Background(), portabilityDeletionTestPreviewTarget()); err != nil ||
		!reflect.DeepEqual(got, preview) {
		t.Fatalf("PreviewDeletion() = %#v, %v", got, err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := adapter.PurgeDeletion(context.Background(), target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	if store.healthCalls != 1 || store.previewCalls != 1 || store.purgeCalls != 1 || store.verifyCalls != 1 {
		t.Fatalf("store calls health=%d preview=%d purge=%d verify=%d",
			store.healthCalls, store.previewCalls, store.purgeCalls, store.verifyCalls)
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

	for name, digest := range map[string][sha256.Size]byte{
		"empty":  {},
		"search": recorddeletion.RecordSearchSurfaceDigest(),
		"core":   recorddeletion.RecordCoreSurfaceDigest(),
	} {
		t.Run(name, func(t *testing.T) {
			command := DeletionCommand{Operation: portabilityDeletionTestOperation(), SurfaceDigest: digest}
			if err := command.Validate(); !errors.Is(err, ErrInvalidDeletionAdapter) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionAdapter", err)
			}
		})
	}
}

type portabilityDeletionStoreStub struct {
	health       recorddeletion.AdapterHealthSnapshot
	preview      recorddeletion.AdapterPreviewSnapshot
	purge        func(recorddeletion.DeletionOperation, [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error)
	healthCalls  int
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (store *portabilityDeletionStoreStub) PortabilityDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *portabilityDeletionStoreStub) PreviewPortabilityDeletion(
	context.Context,
	recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *portabilityDeletionStoreStub) PurgeRecordPortability(
	_ context.Context,
	operation recorddeletion.DeletionOperation,
	digest [sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return store.purge(operation, digest)
}

func (store *portabilityDeletionStoreStub) VerifyRecordPortabilityPurge(
	context.Context,
	recorddeletion.DeletionOperation,
	[sha256.Size]byte,
	recorddeletion.AdapterPurgeReceipt,
) error {
	store.verifyCalls++
	return nil
}

func portabilityDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID: "rpo_port01", ReservationID: "drs_port01",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_port01"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: portabilityDeletionTestDigest("ledger"),
	}
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
