package activity

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

func TestDeletionAdapterDelegatesClosedActivityContract(t *testing.T) {
	t.Parallel()

	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, activityDeletionTestDigest("health"))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: activityDeletionTestDigest("dependency"),
		ImpactDigest:     activityDeletionTestDigest("impact"),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	operation := activityDeletionTestOperation()
	verifiedAt := time.Date(2026, time.August, 19, 9, 30, 0, 0, time.UTC)
	store := &activityDeletionStoreStub{health: health, preview: preview}
	store.purge = func(command DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
		if command.Operation != operation || command.SurfaceDigest != recorddeletion.RecordActivitySurfaceDigest() {
			t.Fatalf("activity purge command = %#v", command)
		}
		return recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordActivityProjection,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    activityDeletionTestDigest("receipt"),
			RemovedRowCount:  6,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}

	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	descriptor := adapter.Descriptor()
	if descriptor.Name() != recorddeletion.AdapterNameRecordActivityProjection ||
		!reflect.DeepEqual(descriptor.Surfaces(), recorddeletion.RecordActivitySurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
	if got, err := adapter.HealthSnapshot(context.Background()); err != nil || got != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", got, err)
	}
	if got, err := adapter.PreviewDeletion(context.Background(), activityDeletionTestPreviewTarget()); err != nil ||
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

	var typedNil *activityDeletionStoreStub
	if _, err := NewDeletionAdapter(typedNil); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("NewDeletionAdapter(typed nil) error = %v", err)
	}
	adapter, err := NewDeletionAdapter(&activityDeletionStoreStub{})
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.HealthSnapshot(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("HealthSnapshot(invalid) error = %v", err)
	}
	if _, err := adapter.PreviewDeletion(context.Background(), activityDeletionTestPreviewTarget()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
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
			command := DeletionCommand{Operation: activityDeletionTestOperation(), SurfaceDigest: digest}
			if err := command.Validate(); !errors.Is(err, ErrInvalidDeletionAdapter) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionAdapter", err)
			}
		})
	}
}

type activityDeletionStoreStub struct {
	health       recorddeletion.AdapterHealthSnapshot
	preview      recorddeletion.AdapterPreviewSnapshot
	purge        func(DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	healthCalls  int
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (store *activityDeletionStoreStub) ActivityDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *activityDeletionStoreStub) PreviewActivityDeletion(
	context.Context,
	recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *activityDeletionStoreStub) PurgeRecordActivity(
	_ context.Context,
	command DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return store.purge(command)
}

func (store *activityDeletionStoreStub) VerifyRecordActivityPurge(
	context.Context,
	DeletionCommand,
	recorddeletion.AdapterPurgeReceipt,
) error {
	store.verifyCalls++
	return nil
}

func activityDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID: "rpo_activity01", ReservationID: "drs_activity01",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_activity01"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: activityDeletionTestDigest("ledger"),
	}
}

func activityDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object:                activityDeletionTestOperation().Object,
		CurrentRevisionID:     "rrv_activity01",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: activityDeletionTestDigest("graph"),
	}
}

func activityDeletionTestDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
