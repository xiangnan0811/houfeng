package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

var ErrInvalidDeletionAdapter = errors.New("invalid evidence deletion adapter")

type EvidenceDeletionCommand struct {
	Operation     recorddeletion.DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command EvidenceDeletionCommand) Validate() error {
	if (recorddeletion.PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != RecordEvidenceSurfaceDigest() {
		return ErrInvalidDeletionAdapter
	}
	return nil
}

type EvidenceDeletionStore interface {
	EvidenceDeletionHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error)
	PreviewEvidenceDeletion(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	PurgeRecordEvidence(context.Context, EvidenceDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error)
	VerifyRecordEvidencePurge(context.Context, EvidenceDeletionCommand, recorddeletion.AdapterPurgeReceipt) error
}

type DeletionAdapter struct {
	store      EvidenceDeletionStore
	descriptor recorddeletion.AdapterDescriptor
}

func NewDeletionAdapter(store EvidenceDeletionStore) (*DeletionAdapter, error) {
	if nilRevisionPreparationDependency(store) {
		return nil, ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(
		recorddeletion.AdapterNameRecordEvidence,
		recorddeletion.RecordEvidenceSurfaceNames(),
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
	if ctx == nil || adapter == nil || nilRevisionPreparationDependency(adapter.store) {
		return recorddeletion.AdapterHealthSnapshot{}, ErrInvalidDeletionAdapter
	}
	health, err := adapter.store.EvidenceDeletionHealth(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := health.Validate(); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid evidence health", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *DeletionAdapter) PreviewDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilRevisionPreparationDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrInvalidDeletionAdapter
	}
	preview, err := adapter.store.PreviewEvidenceDeletion(ctx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid evidence preview", recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *DeletionAdapter) PurgeDeletion(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilRevisionPreparationDependency(adapter.store) || target.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	command := EvidenceDeletionCommand{Operation: target.Operation, SurfaceDigest: RecordEvidenceSurfaceDigest()}
	if command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrInvalidDeletionAdapter
	}
	receipt, err := adapter.store.PurgeRecordEvidence(ctx, command)
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
	if ctx == nil || adapter == nil || nilRevisionPreparationDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidDeletionAdapter
	}
	if err := recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, adapter.descriptor); err != nil {
		return err
	}
	command := EvidenceDeletionCommand{Operation: target.Operation, SurfaceDigest: RecordEvidenceSurfaceDigest()}
	return adapter.store.VerifyRecordEvidencePurge(ctx, command, receipt)
}

func RecordEvidenceSurfaceDigest() [sha256.Size]byte {
	return recorddeletion.RecordEvidenceSurfaceDigest()
}

var _ recorddeletion.DeletionPurgeAdapter = (*DeletionAdapter)(nil)
