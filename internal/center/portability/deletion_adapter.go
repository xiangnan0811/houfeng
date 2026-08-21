package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid record portability deletion adapter")

// DeletionCommand carries one purge authority into the store. The surface digest
// travels with it so the store cannot be asked to purge under another adapter's
// ownership claim.
type DeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command DeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != recorddeletion.RecordPortabilitySurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

// DeletionStore is the persistence seam the adapter owns. Health and preview
// stay content-free: they may count rows and hash locators, but they must not
// return markdown, archive bytes, or blob keys.
type DeletionStore interface {
	PortabilityDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewPortabilityDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordPortability(context.Context, recorddeletion.DeletionOperation, [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordPortabilityPurge(context.Context, recorddeletion.DeletionOperation, [sha256.Size]byte, recorddeletion.AdapterPurgeReceipt) error
}

// DeletionAdapter removes a purged record's export/import locators and origin
// rows. Origin tombstones stay so Child 11 can refuse official restore of that
// identity. Permanent-delete transport stays closed until the aggregate
// registry is healthy.
type DeletionAdapter struct {
	store      DeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store DeletionStore) (*DeletionAdapter, error) {
	if nilPortabilityDeletionDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordPortability,
		recorddeletion.RecordPortabilitySurfaceNames(),
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
	if ctx == nil || adapter == nil || nilPortabilityDeletionDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.PortabilityDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf(
			"%w: invalid record portability health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilPortabilityDeletionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewPortabilityDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf(
			"%w: invalid record portability preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilPortabilityDeletionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordPortabilitySurfaceDigest(),
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordPortability(ctx, command.Operation, command.SurfaceDigest)
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
	if ctx == nil || adapter == nil || nilPortabilityDeletionDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordPortabilitySurfaceDigest(),
	}
	return adapter.store.VerifyRecordPortabilityPurge(ctx, command.Operation, command.SurfaceDigest, receipt)
}

func nilPortabilityDeletionDependency(value any) bool {
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
