package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
)

var ErrInvalidComparisonAdapter = errors.New("invalid record comparison deletion adapter")

// ComparisonPurgeCommand binds the operation and its exact observed owner to
// the restricted comparison purge and verification functions.
type ComparisonPurgeCommand struct {
	Operation     DeletionOperation
	OwnerClaim    OperationOwnerClaim
	SurfaceDigest [sha256.Size]byte
}

func (command ComparisonPurgeCommand) Validate() error {
	target := PurgeTarget{Operation: command.Operation, OwnerClaim: command.OwnerClaim}
	if command.Operation.ContractVersion != DeletionContractV2 || target.Validate() != nil ||
		command.SurfaceDigest != RecordComparisonSurfaceDigest() {
		return ErrInvalidComparisonAdapter
	}
	return nil
}

type RecordComparisonStore interface {
	RecordComparisonHealth(context.Context) (AdapterHealthSnapshot, error)
	PreviewComparisonDeletion(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error)
	PurgeRecordComparison(context.Context, ComparisonPurgeCommand) (AdapterPurgeReceipt, error)
	VerifyRecordComparisonPurge(context.Context, ComparisonPurgeCommand, AdapterPurgeReceipt) error
}

type ComparisonAdapter struct {
	store      RecordComparisonStore
	descriptor AdapterDescriptor
}

var _ DeletionPurgeAdapter = (*ComparisonAdapter)(nil)

func NewComparisonAdapter(store RecordComparisonStore) (*ComparisonAdapter, error) {
	if nilDeletionServiceDependency(store) {
		return nil, ErrInvalidComparisonAdapter
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordComparison, RecordComparisonSurfaceNames())
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor", ErrInvalidComparisonAdapter)
	}
	return &ComparisonAdapter{store: store, descriptor: descriptor}, nil
}

func (adapter *ComparisonAdapter) Descriptor() AdapterDescriptor {
	if adapter == nil {
		return AdapterDescriptor{}
	}
	return AdapterDescriptor{name: adapter.descriptor.name, surfaces: adapter.descriptor.Surfaces()}
}

func (adapter *ComparisonAdapter) HealthSnapshot(ctx context.Context) (AdapterHealthSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) {
		return AdapterHealthSnapshot{}, ErrInvalidComparisonAdapter
	}
	health, err := adapter.store.RecordComparisonHealth(ctx)
	if err != nil {
		return AdapterHealthSnapshot{}, err
	}
	if err := health.validate(); err != nil {
		return AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid comparison health", ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *ComparisonAdapter) PreviewDeletion(ctx context.Context, target PreviewTarget) (AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || target.Validate() != nil {
		return AdapterPreviewSnapshot{}, ErrInvalidComparisonAdapter
	}
	preview, err := adapter.store.PreviewComparisonDeletion(ctx, target)
	if err != nil {
		return AdapterPreviewSnapshot{}, err
	}
	if preview.Validate() != nil || len(preview.SurvivingCopies) > 1 ||
		(len(preview.SurvivingCopies) == 1 &&
			(preview.SurvivingCopies[0].Kind != SurvivingCopyKindOtherRecord ||
				preview.SurvivingCopies[0].CountStatus != SurvivingCopyCountKnown)) {
		return AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid comparison preview", ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *ComparisonAdapter) PurgeDeletion(ctx context.Context, target PurgeTarget) (AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) ||
		target.Validate() != nil || target.Operation.ContractVersion != DeletionContractV2 {
		return AdapterPurgeReceipt{}, ErrInvalidComparisonAdapter
	}
	command := ComparisonPurgeCommand{
		Operation: target.Operation, OwnerClaim: target.OwnerClaim, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor),
	}
	if err := command.Validate(); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordComparison(ctx, command)
	if err != nil {
		return AdapterPurgeReceipt{}, err
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (adapter *ComparisonAdapter) VerifyDeletion(ctx context.Context, target PurgeTarget, receipt AdapterPurgeReceipt) error {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) ||
		target.Validate() != nil || target.Operation.ContractVersion != DeletionContractV2 {
		return ErrInvalidComparisonAdapter
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return err
	}
	command := ComparisonPurgeCommand{
		Operation: target.Operation, OwnerClaim: target.OwnerClaim, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor),
	}
	if err := command.Validate(); err != nil {
		return err
	}
	return adapter.store.VerifyRecordComparisonPurge(ctx, command, receipt)
}

// RecordComparisonSurfaceDigest binds the exact owned surfaces in receipts.
func RecordComparisonSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordComparison, RecordComparisonSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}
