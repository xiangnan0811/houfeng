package activity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid activity deletion adapter")

// DeletionCommand carries one purge authority into the store. The surface digest
// travels with it so the store cannot be asked to purge under another adapter's
// ownership claim.
type DeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command DeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != recorddeletion.RecordActivitySurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

// DeletionStore is the persistence seam the adapter owns. Health and preview
// stay content-free: they may count rows and hash digests, but they must not
// return presentation text, subject identity, or revision bodies.
type DeletionStore interface {
	ActivityDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewActivityDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordActivity(context.Context, DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordActivityPurge(context.Context, DeletionCommand, recorddeletion.AdapterPurgeReceipt) error
}

// DeletionAdapter removes a purged record's projected activity. Activity is a
// derived copy of authorized presentation, so a purge that skipped it would
// leave deleted record facts readable on every subject timeline that still
// pointed at them.
type DeletionAdapter struct {
	store      DeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store DeletionStore) (*DeletionAdapter, error) {
	if nilActivityDeletionDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordActivityProjection,
		recorddeletion.RecordActivitySurfaceNames(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor", ErrInvalidDeletionAdapter)
	}
	return &DeletionAdapter{store: store, descriptor: descriptor}, nil
}

func (adapter *DeletionAdapter) Descriptor() recorddeletion.AdapterDescriptor {
	if adapter == nil {
		return recorddeletion.AdapterDescriptor{}
	}
	return adapter.descriptor
}

func (adapter *DeletionAdapter) HealthSnapshot(ctx context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || adapter == nil || nilActivityDeletionDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.ActivityDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf(
			"%w: invalid activity health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilActivityDeletionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewActivityDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf(
			"%w: invalid activity preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilActivityDeletionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordActivitySurfaceDigest(),
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordActivity(ctx, command)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (adapter *DeletionAdapter) VerifyDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || adapter == nil || nilActivityDeletionDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordActivitySurfaceDigest(),
	}
	return adapter.store.VerifyRecordActivityPurge(ctx, command, receipt)
}

func nilActivityDeletionDependency(value any) bool {
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

var _ recorddeletion.DeletionPurgeAdapter = (*DeletionAdapter)(nil)
