package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid collaboration deletion adapter")

type DeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command DeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != recorddeletion.RecordCollaborationSurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

type DeletionStore interface {
	CollaborationDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewCollaborationDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordCollaboration(context.Context, DeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordCollaborationPurge(context.Context, DeletionCommand, recorddeletion.AdapterPurgeReceipt) error
}

type DeletionAdapter struct {
	store      DeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store DeletionStore) (*DeletionAdapter, error) {
	if nilActionDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordCollaboration,
		recorddeletion.RecordCollaborationSurfaceNames(),
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
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.CollaborationDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid collaboration health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewCollaborationDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid collaboration preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordCollaborationSurfaceDigest(),
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordCollaboration(ctx, command)
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
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := DeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordCollaborationSurfaceDigest(),
	}
	return adapter.store.VerifyRecordCollaborationPurge(ctx, command, receipt)
}

var _ recorddeletion.DeletionPurgeAdapter = (*DeletionAdapter)(nil)
