package recordsearch

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid record search deletion adapter")

// DeletionCommand carries one purge authority into the store. The surface digest
// travels with it so the store cannot be asked to purge under another adapter's
// ownership claim.
type DeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command DeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != recorddeletion.RecordSearchSurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

type DeletionStore interface {
	SearchDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewSearchDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordSearch(context.Context, DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordSearchPurge(context.Context, DeletionCommand, recorddeletion.AdapterPurgeReceipt) error
}

// DeletionAdapter removes a purged record's projection rows from every search
// generation. Search holds a copy of record content, so a purge that skipped it
// would leave the deleted text readable to the next index-backed query.
type DeletionAdapter struct {
	store      DeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store DeletionStore) (*DeletionAdapter, error) {
	if nilSearchDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordSearch,
		recorddeletion.RecordSearchSurfaceNames(),
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
	if ctx == nil || adapter == nil || nilSearchDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.SearchDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf(
			"%w: invalid record search health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilSearchDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewSearchDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf(
			"%w: invalid record search preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilSearchDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordSearchSurfaceDigest(),
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordSearch(ctx, command)
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
	if ctx == nil || adapter == nil || nilSearchDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordSearchSurfaceDigest(),
	}
	return adapter.store.VerifyRecordSearchPurge(ctx, command, receipt)
}

var _ recorddeletion.DeletionPurgeAdapter = (*DeletionAdapter)(nil)
