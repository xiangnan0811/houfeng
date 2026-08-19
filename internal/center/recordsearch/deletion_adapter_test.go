package recordsearch

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

func TestDeletionAdapterDelegatesClosedSearchContract(t *testing.T) {
	t.Parallel()

	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, searchDeletionTestDigest("health"))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: searchDeletionTestDigest("dependency"),
		ImpactDigest:     searchDeletionTestDigest("impact"),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	operation := searchDeletionTestOperation()
	verifiedAt := time.Date(2026, time.August, 19, 9, 30, 0, 0, time.UTC)
	store := &searchDeletionStoreStub{health: health, preview: preview}
	store.purge = func(command DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
		if command.Operation != operation || command.SurfaceDigest != recorddeletion.RecordSearchSurfaceDigest() {
			t.Fatalf("search purge command = %#v", command)
		}
		return recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordSearch,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    searchDeletionTestDigest("receipt"),
			RemovedRowCount:  4,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}

	adapter, err := NewDeletionAdapter(store)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	descriptor := adapter.Descriptor()
	if descriptor.Name() != recorddeletion.AdapterNameRecordSearch ||
		!reflect.DeepEqual(descriptor.Surfaces(), recorddeletion.RecordSearchSurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", descriptor)
	}
	if got, err := adapter.HealthSnapshot(context.Background()); err != nil || got != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", got, err)
	}
	if got, err := adapter.PreviewDeletion(context.Background(), searchDeletionTestPreviewTarget()); err != nil ||
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

	var typedNil *searchDeletionStoreStub
	if _, err := NewDeletionAdapter(typedNil); !errors.Is(err, ErrInvalidDeletionAdapter) {
		t.Fatalf("NewDeletionAdapter(typed nil) error = %v", err)
	}
	adapter, err := NewDeletionAdapter(&searchDeletionStoreStub{})
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	if _, err := adapter.HealthSnapshot(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("HealthSnapshot(invalid) error = %v", err)
	}
	if _, err := adapter.PreviewDeletion(context.Background(), searchDeletionTestPreviewTarget()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("PreviewDeletion(invalid) error = %v", err)
	}
}

// A receipt whose surface digest is not the closed record_search list would let a
// purge claim coverage of tables this adapter does not own.
func TestDeletionCommandRejectsForeignSurfaceDigest(t *testing.T) {
	t.Parallel()

	for name, digest := range map[string][sha256.Size]byte{
		"empty":         {},
		"collaboration": recorddeletion.RecordCollaborationSurfaceDigest(),
		"core":          recorddeletion.RecordCoreSurfaceDigest(),
	} {
		t.Run(name, func(t *testing.T) {
			command := DeletionCommand{Operation: searchDeletionTestOperation(), SurfaceDigest: digest}
			if err := command.Validate(); !errors.Is(err, ErrInvalidDeletionAdapter) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDeletionAdapter", err)
			}
		})
	}
}

type searchDeletionStoreStub struct {
	health       recorddeletion.AdapterHealthSnapshot
	preview      recorddeletion.AdapterPreviewSnapshot
	purge        func(DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	healthCalls  int
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (store *searchDeletionStoreStub) SearchDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *searchDeletionStoreStub) PreviewSearchDeletion(
	context.Context,
	recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *searchDeletionStoreStub) PurgeRecordSearch(
	_ context.Context,
	command DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	store.purgeCalls++
	return store.purge(command)
}

func (store *searchDeletionStoreStub) VerifyRecordSearchPurge(
	context.Context,
	DeletionCommand,
	recorddeletion.AdapterPurgeReceipt,
) error {
	store.verifyCalls++
	return nil
}

func searchDeletionTestOperation() recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID: "rpo_search01", ReservationID: "drs_search01",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_search01"},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: searchDeletionTestDigest("ledger"),
	}
}

func searchDeletionTestPreviewTarget() recorddeletion.PreviewTarget {
	return recorddeletion.PreviewTarget{
		Object:                searchDeletionTestOperation().Object,
		CurrentRevisionID:     "rrv_search01",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: searchDeletionTestDigest("graph"),
	}
}

func searchDeletionTestDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
