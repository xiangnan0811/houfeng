package recorddeletion

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
)

func TestCoreAdapterDelegatesHealthPreviewPurgeAndVerifiedAbsenceWithExactOwnership(t *testing.T) {
	t.Parallel()

	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil {
		t.Fatalf("NewAdapterDescriptor() error = %v", err)
	}
	health, err := NewAdapterHealthSnapshot(true, 1, deletionTestDigest(131))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot() error = %v", err)
	}
	preview := AdapterPreviewSnapshot{
		DependencyDigest: deletionTestDigest(132),
		ImpactDigest:     deletionTestDigest(133),
		SurvivingCopies:  []AdapterSurvivingCopy{},
	}
	operation := deletionTestOperation(DeletionStateOnlinePurging)
	purgeTarget := coreTestPurgeTarget(operation)
	verifiedAt := time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC)
	store := &recordCoreStoreStub{health: health, preview: preview}
	store.purge = func(command CorePurgeCommand) (AdapterPurgeReceipt, error) {
		if command.PurgeTarget != purgeTarget || command.SurfaceDigest != digestAdapterSurfaces(descriptor) {
			t.Fatalf("core purge command = %#v", command)
		}
		return AdapterPurgeReceipt{
			AdapterName:      AdapterNameRecordCore,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    deletionTestDigest(134),
			RemovedRowCount:  17,
			VerifiedAbsentAt: verifiedAt,
		}, nil
	}
	adapter, err := NewCoreAdapter(store)
	if err != nil {
		t.Fatalf("NewCoreAdapter() error = %v", err)
	}

	if got := adapter.Descriptor(); got.Name() != AdapterNameRecordCore || !reflect.DeepEqual(got.Surfaces(), RecordCoreSurfaceNames()) {
		t.Fatalf("Descriptor() = %#v", got)
	}
	gotHealth, err := adapter.HealthSnapshot(context.Background())
	if err != nil || gotHealth != health {
		t.Fatalf("HealthSnapshot() = %#v, %v", gotHealth, err)
	}
	target := previewTarget(operation.Object, deletionTestRecordSnapshot(t))
	gotPreview, err := adapter.PreviewDeletion(context.Background(), target)
	if err != nil || !reflect.DeepEqual(gotPreview, preview) {
		t.Fatalf("PreviewDeletion() = %#v, %v", gotPreview, err)
	}
	receipt, err := adapter.PurgeDeletion(context.Background(), purgeTarget)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(context.Background(), purgeTarget, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	if store.verifiedTarget != purgeTarget {
		t.Fatalf("verified target = %#v, want %#v", store.verifiedTarget, purgeTarget)
	}
	if store.healthCalls != 1 || store.previewCalls != 1 || store.purgeCalls != 1 || store.verifyCalls != 1 {
		t.Fatalf("core store calls health=%d preview=%d purge=%d verify=%d", store.healthCalls, store.previewCalls, store.purgeCalls, store.verifyCalls)
	}
}

func TestRecordCoreSurfaceDigestMatchesClosedDescriptor(t *testing.T) {
	t.Parallel()

	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil {
		t.Fatalf("NewAdapterDescriptor() error = %v", err)
	}
	if got, want := RecordCoreSurfaceDigest(), digestAdapterSurfaces(descriptor); got != want || got == ([32]byte{}) {
		t.Fatalf("RecordCoreSurfaceDigest() = %x, want %x", got, want)
	}
}

func TestCoreAdapterRejectsInvalidStoreResultsAndTypedNilStore(t *testing.T) {
	t.Parallel()

	operation := deletionTestOperation(DeletionStateOnlinePurging)
	validHealth, _ := NewAdapterHealthSnapshot(true, 1, deletionTestDigest(140))
	validPreview := AdapterPreviewSnapshot{
		DependencyDigest: deletionTestDigest(141),
		ImpactDigest:     deletionTestDigest(142),
		SurvivingCopies:  []AdapterSurvivingCopy{},
	}
	for _, tt := range []struct {
		name   string
		mutate func(*recordCoreStoreStub)
	}{
		{name: "invalid health", mutate: func(store *recordCoreStoreStub) { store.health = AdapterHealthSnapshot{} }},
		{name: "invalid preview", mutate: func(store *recordCoreStoreStub) { store.preview = AdapterPreviewSnapshot{} }},
		{name: "wrong receipt adapter", mutate: func(store *recordCoreStoreStub) {
			store.purge = func(command CorePurgeCommand) (AdapterPurgeReceipt, error) {
				return AdapterPurgeReceipt{AdapterName: AdapterNameRecordSearch, OperationID: operation.OperationID, SurfaceDigest: command.SurfaceDigest, ReceiptDigest: deletionTestDigest(143), VerifiedAbsentAt: time.Now()}, nil
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordCoreStoreStub{health: validHealth, preview: validPreview}
			tt.mutate(store)
			adapter, err := NewCoreAdapter(store)
			if err != nil {
				t.Fatalf("NewCoreAdapter() error = %v", err)
			}
			switch tt.name {
			case "invalid health":
				_, err = adapter.HealthSnapshot(context.Background())
			case "invalid preview":
				_, err = adapter.PreviewDeletion(context.Background(), previewTarget(operation.Object, deletionTestRecordSnapshot(t)))
			default:
				_, err = adapter.PurgeDeletion(context.Background(), coreTestPurgeTarget(operation))
			}
			if !errors.Is(err, ErrDeletionSafetyUnavailable) {
				t.Fatalf("adapter error = %v, want ErrDeletionSafetyUnavailable", err)
			}
		})
	}

	var typedNil *recordCoreStoreStub
	if _, err := NewCoreAdapter(typedNil); !errors.Is(err, ErrInvalidCoreAdapter) {
		t.Fatalf("NewCoreAdapter(typed nil) error = %v, want ErrInvalidCoreAdapter", err)
	}
}

func TestRegistryPurgerUsesDependencySafeReverseOrderAndVerifiesEachReceiptBeforeNext(t *testing.T) {
	t.Parallel()

	registry, adapters := deletionTestRegistry(t)
	operation := deletionTestOperation(DeletionStateOnlinePurging)
	var calls []string
	for index, adapter := range adapters {
		adapter.purge = AdapterPurgeReceipt{
			AdapterName:      adapter.name,
			OperationID:      operation.OperationID,
			SurfaceDigest:    digestAdapterSurfaces(adapter.descriptor),
			ReceiptDigest:    deletionTestDigest(byte(150 + index)),
			RemovedRowCount:  uint64(index + 1),
			VerifiedAbsentAt: time.Date(2026, time.August, 3, 15, index, 0, 0, time.UTC),
		}
		adapter.purgeHook = func(name AdapterName) { calls = append(calls, "purge:"+string(name)) }
		adapter.verifyHook = func(name AdapterName) { calls = append(calls, "verify:"+string(name)) }
	}
	purger := NewRegistryPurger(registry)

	receipt, err := purger.PurgeOnline(context.Background(), coreTestPurgeTarget(operation))
	if err != nil {
		t.Fatalf("PurgeOnline() error = %v", err)
	}
	if receipt.OperationID != operation.OperationID || receipt.ReceiptDigest == ([32]byte{}) {
		t.Fatalf("PurgeOnline() = %#v", receipt)
	}
	wantCalls := make([]string, 0, 2*len(adapters))
	names := RequiredAdapterNames()
	for index := len(names) - 1; index >= 0; index-- {
		name := names[index]
		wantCalls = append(wantCalls, "purge:"+string(name), "verify:"+string(name))
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("adapter calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestRegistryPurgerStopsAtFirstPurgeOrVerificationFailure(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"purge", "verify"} {
		t.Run(failure, func(t *testing.T) {
			registry, adapters := deletionTestRegistry(t)
			operation := deletionTestOperation(DeletionStateOnlinePurging)
			for index, adapter := range adapters {
				adapter.purge = AdapterPurgeReceipt{
					AdapterName:      adapter.name,
					OperationID:      operation.OperationID,
					SurfaceDigest:    digestAdapterSurfaces(adapter.descriptor),
					ReceiptDigest:    deletionTestDigest(byte(170 + index)),
					VerifiedAbsentAt: time.Date(2026, time.August, 3, 16, index, 0, 0, time.UTC),
				}
			}
			lost := recordplatform.ErrLostOwnerLease
			if failure == "purge" {
				adapters[2].purgeErr = lost
			} else {
				adapters[2].verifyErr = lost
			}
			purger := NewRegistryPurger(registry)

			if _, err := purger.PurgeOnline(context.Background(), coreTestPurgeTarget(operation)); !errors.Is(err, ErrDeletionSafetyUnavailable) || !errors.Is(err, lost) {
				t.Fatalf("PurgeOnline() error = %v, want safety failure retaining lost owner lease", err)
			}
			if adapters[1].purgeCalls != 0 {
				t.Fatalf("adapter after failure purge calls = %d, want zero", adapters[1].purgeCalls)
			}
		})
	}
}

func coreTestPurgeTarget(operation DeletionOperation) PurgeTarget {
	return PurgeTarget{Operation: operation, OwnerClaim: OperationOwnerClaim{
		OperationID: operation.OperationID,
		Owner: recordplatform.OwnerLease{
			OwnerID: "deletion_worker", Generation: 7,
			ExpiresAt: time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC),
		},
	}}
}

func TestCoreAdapterRequiresExactOwnerForBothContracts(t *testing.T) {
	t.Parallel()
	for _, version := range []DeletionContractVersion{DeletionContractV1, DeletionContractV2} {
		operation := deletionTestOperation(DeletionStateOnlinePurging)
		operation.ContractVersion = version
		if version == DeletionContractV2 {
			operation.LedgerSequence = 0
			operation.LedgerEntryHash = [32]byte{}
		}
		target := coreTestPurgeTarget(operation)
		if err := target.Validate(); err != nil {
			t.Fatalf("contract %d target rejected: %v", version, err)
		}
		for _, claim := range []OperationOwnerClaim{{}, {OperationID: "rpo_another", Owner: target.OwnerClaim.Owner}} {
			invalid := target
			invalid.OwnerClaim = claim
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidDeletionOperation) {
				t.Fatalf("contract %d accepted owner %#v: %v", version, claim, err)
			}
		}
	}
}

type recordCoreStoreStub struct {
	health         AdapterHealthSnapshot
	preview        AdapterPreviewSnapshot
	purge          func(CorePurgeCommand) (AdapterPurgeReceipt, error)
	verifyErr      error
	verifiedTarget PurgeTarget
	healthCalls    int
	previewCalls   int
	purgeCalls     int
	verifyCalls    int
}

func (store *recordCoreStoreStub) RecordCoreHealth(context.Context) (AdapterHealthSnapshot, error) {
	store.healthCalls++
	return store.health, nil
}

func (store *recordCoreStoreStub) PreviewRecordCore(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error) {
	store.previewCalls++
	return store.preview, nil
}

func (store *recordCoreStoreStub) PurgeRecordCore(_ context.Context, command CorePurgeCommand) (AdapterPurgeReceipt, error) {
	store.purgeCalls++
	if store.purge != nil {
		return store.purge(command)
	}
	return AdapterPurgeReceipt{}, nil
}

func (store *recordCoreStoreStub) VerifyRecordCorePurge(_ context.Context, command CorePurgeCommand, _ AdapterPurgeReceipt) error {
	store.verifyCalls++
	store.verifiedTarget = command.PurgeTarget
	return store.verifyErr
}
