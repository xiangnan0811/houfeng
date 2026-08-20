package activity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

var ErrInvalidRecoveryAdapter = errors.New("invalid activity recovery adapter")

// RecoveryResult is what a disaster rebuild did. Callers need the new generation
// so they can restart the projector against it rather than against the retired
// one they just emptied.
type RecoveryResult struct {
	RetiredGeneration uint64
	ActiveGeneration  uint64
	RemovedRowCount   uint64
}

// RecoveryStore clears and reopens the activity projection. It must not restore
// rows from purge receipts: a rebuilt generation starts empty and the projector
// re-reads authoritative sources, which still refuse fenced records.
type RecoveryStore interface {
	RebuildActivityProjection(context.Context) (RecoveryResult, error)
}

// RecoveryAdapter owns the activity-side disaster rebuild. It retires the active
// generation, opens the next one, and removes every derived row that belonged to
// the retired generation. Online deletion receipts stay put so a later projector
// pass cannot revive presentation the deletion adapter already proved absent.
type RecoveryAdapter struct {
	store RecoveryStore
}

func NewRecoveryAdapter(store RecoveryStore) (*RecoveryAdapter, error) {
	if nilActivityRecoveryDependency(store) {
		return nil, ErrInvalidRecoveryAdapter
	}
	return &RecoveryAdapter{store: store}, nil
}

func (adapter *RecoveryAdapter) Rebuild(ctx context.Context) (RecoveryResult, error) {
	if ctx == nil || adapter == nil || nilActivityRecoveryDependency(adapter.store) {
		return RecoveryResult{}, ErrInvalidRecoveryAdapter
	}
	result, err := adapter.store.RebuildActivityProjection(ctx)
	if err != nil {
		return RecoveryResult{}, err
	}
	if result.ActiveGeneration == 0 || result.ActiveGeneration <= result.RetiredGeneration {
		return RecoveryResult{}, fmt.Errorf(
			"%w: rebuild did not advance the generation", ErrInvalidRecoveryAdapter)
	}
	return result, nil
}

func nilActivityRecoveryDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
