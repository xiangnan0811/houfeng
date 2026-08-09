package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid attachment deletion adapter")

type AttachmentDeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command AttachmentDeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != recorddeletion.RecordAttachmentsSurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

type AttachmentDeletionStore interface {
	AttachmentDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewAttachmentDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordAttachments(context.Context, AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordAttachmentsPurge(context.Context, AttachmentDeletionCommand, recorddeletion.AdapterPurgeReceipt) error
}

type DeletionAdapter struct {
	store      AttachmentDeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store AttachmentDeletionStore) (*DeletionAdapter, error) {
	if nilUploadServiceDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordAttachments,
		recorddeletion.RecordAttachmentsSurfaceNames(),
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
	if ctx == nil || adapter == nil || nilUploadServiceDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.AttachmentDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid attachment health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilUploadServiceDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewAttachmentDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid attachment preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilUploadServiceDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := AttachmentDeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordAttachmentsSurfaceDigest(),
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordAttachments(ctx, command)
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
	if ctx == nil || adapter == nil || nilUploadServiceDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := AttachmentDeletionCommand{
		Operation: target.Operation, SurfaceDigest: recorddeletion.RecordAttachmentsSurfaceDigest(),
	}
	return adapter.store.VerifyRecordAttachmentsPurge(ctx, command, receipt)
}

var _ recorddeletion.DeletionPurgeAdapter = (*DeletionAdapter)(nil)
