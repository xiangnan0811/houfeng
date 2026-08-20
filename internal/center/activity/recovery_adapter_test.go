package activity

import (
	"context"
	"errors"
	"testing"
)

func TestRecoveryAdapterRebuildRequiresAnAdvancedGeneration(t *testing.T) {
	t.Parallel()

	store := &activityRecoveryStoreStub{result: RecoveryResult{
		RetiredGeneration: 1, ActiveGeneration: 2, RemovedRowCount: 9,
	}}
	adapter, err := NewRecoveryAdapter(store)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	result, err := adapter.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if result != store.result {
		t.Fatalf("Rebuild() = %#v, want %#v", result, store.result)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
}

func TestRecoveryAdapterRejectsTypedNilAndStalledRebuild(t *testing.T) {
	t.Parallel()

	var typedNil *activityRecoveryStoreStub
	if _, err := NewRecoveryAdapter(typedNil); !errors.Is(err, ErrInvalidRecoveryAdapter) {
		t.Fatalf("NewRecoveryAdapter(typed nil) error = %v", err)
	}

	adapter, err := NewRecoveryAdapter(&activityRecoveryStoreStub{result: RecoveryResult{
		RetiredGeneration: 3, ActiveGeneration: 3,
	}})
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	if _, err := adapter.Rebuild(context.Background()); !errors.Is(err, ErrInvalidRecoveryAdapter) {
		t.Fatalf("Rebuild(stalled) error = %v", err)
	}
}

type activityRecoveryStoreStub struct {
	result RecoveryResult
	calls  int
}

func (store *activityRecoveryStoreStub) RebuildActivityProjection(context.Context) (RecoveryResult, error) {
	store.calls++
	return store.result, nil
}
