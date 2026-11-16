package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
)

var ErrInvalidMarkdownClientAdapter = errors.New("invalid record markdown client deletion adapter")

// MarkdownClientPurgeCommand carries the exact operation and observed owner
// claim to the restricted persistence command. The store must recheck both
// against the current database authority before returning a receipt.
type MarkdownClientPurgeCommand struct {
	Operation     DeletionOperation
	OwnerClaim    OperationOwnerClaim
	SurfaceDigest [sha256.Size]byte
}

func (command MarkdownClientPurgeCommand) Validate() error {
	target := PurgeTarget{Operation: command.Operation, OwnerClaim: command.OwnerClaim}
	if (command.Operation.ContractVersion != DeletionContractV1 && command.Operation.ContractVersion != DeletionContractV2) || target.Validate() != nil ||
		command.SurfaceDigest != RecordMarkdownClientSurfaceDigest() {
		return ErrInvalidMarkdownClientAdapter
	}
	return nil
}

type RecordMarkdownClientStore interface {
	RecordMarkdownClientHealth(context.Context) (AdapterHealthSnapshot, error)
	PreviewRecordMarkdownClient(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error)
	PurgeRecordMarkdownClient(context.Context, MarkdownClientPurgeCommand) (AdapterPurgeReceipt, error)
	VerifyRecordMarkdownClientPurge(context.Context, MarkdownClientPurgeCommand, AdapterPurgeReceipt) error
}

type MarkdownClientAdapter struct {
	store      RecordMarkdownClientStore
	descriptor AdapterDescriptor
}

var _ DeletionPurgeAdapter = (*MarkdownClientAdapter)(nil)

func NewMarkdownClientAdapter(store RecordMarkdownClientStore) (*MarkdownClientAdapter, error) {
	if nilDeletionServiceDependency(store) {
		return nil, ErrInvalidMarkdownClientAdapter
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordMarkdownClient, RecordMarkdownClientSurfaceNames())
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor", ErrInvalidMarkdownClientAdapter)
	}
	return &MarkdownClientAdapter{store: store, descriptor: descriptor}, nil
}

func (adapter *MarkdownClientAdapter) Descriptor() AdapterDescriptor {
	if adapter == nil {
		return AdapterDescriptor{}
	}
	return AdapterDescriptor{name: adapter.descriptor.name, surfaces: adapter.descriptor.Surfaces()}
}

func (adapter *MarkdownClientAdapter) HealthSnapshot(ctx context.Context) (AdapterHealthSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) {
		return AdapterHealthSnapshot{}, ErrInvalidMarkdownClientAdapter
	}
	health, err := adapter.store.RecordMarkdownClientHealth(ctx)
	if err != nil {
		return AdapterHealthSnapshot{}, err
	}
	if err := health.validate(); err != nil {
		return AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid markdown client health", ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *MarkdownClientAdapter) PreviewDeletion(ctx context.Context, target PreviewTarget) (AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || target.Validate() != nil {
		return AdapterPreviewSnapshot{}, ErrInvalidMarkdownClientAdapter
	}
	preview, err := adapter.store.PreviewRecordMarkdownClient(ctx, target)
	if err != nil {
		return AdapterPreviewSnapshot{}, err
	}
	if preview.Validate() != nil || len(preview.SurvivingCopies) != 1 ||
		preview.SurvivingCopies[0].Kind != SurvivingCopyKindOfflineBrowserBuffer ||
		preview.SurvivingCopies[0].CountStatus != SurvivingCopyCountUnknown ||
		preview.SurvivingCopies[0].CopyCount != nil {
		return AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid markdown client preview", ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *MarkdownClientAdapter) PurgeDeletion(ctx context.Context, target PurgeTarget) (AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) ||
		target.Validate() != nil {
		return AdapterPurgeReceipt{}, ErrInvalidMarkdownClientAdapter
	}
	command := MarkdownClientPurgeCommand{
		Operation: target.Operation, OwnerClaim: target.OwnerClaim, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor),
	}
	if err := command.Validate(); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordMarkdownClient(ctx, command)
	if err != nil {
		return AdapterPurgeReceipt{}, err
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (adapter *MarkdownClientAdapter) VerifyDeletion(ctx context.Context, target PurgeTarget, receipt AdapterPurgeReceipt) error {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) ||
		target.Validate() != nil {
		return ErrInvalidMarkdownClientAdapter
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return err
	}
	command := MarkdownClientPurgeCommand{
		Operation: target.Operation, OwnerClaim: target.OwnerClaim, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor),
	}
	if err := command.Validate(); err != nil {
		return err
	}
	return adapter.store.VerifyRecordMarkdownClientPurge(ctx, command, receipt)
}

// RecordMarkdownClientSurfaceDigest binds the exact owned surfaces in receipts.
func RecordMarkdownClientSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordMarkdownClient, RecordMarkdownClientSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}
