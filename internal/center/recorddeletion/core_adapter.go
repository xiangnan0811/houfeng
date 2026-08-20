package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

const (
	adapterSurfaceDigestDomainV1 = "houfeng.record-deletion.adapter-surfaces.v1"
	onlinePurgeDigestDomainV1    = "houfeng.record-deletion.online-purge.v1"
)

var ErrInvalidCoreAdapter = errors.New("invalid record core deletion adapter")

type PurgeTarget struct {
	Operation DeletionOperation
}

func (target PurgeTarget) Validate() error {
	if target.Operation.Validate() != nil || target.Operation.State != DeletionStateOnlinePurging {
		return ErrInvalidDeletionOperation
	}
	return nil
}

type AdapterPurgeReceipt struct {
	AdapterName      AdapterName
	OperationID      string
	SurfaceDigest    [sha256.Size]byte
	ReceiptDigest    [sha256.Size]byte
	RemovedRowCount  uint64
	VerifiedAbsentAt time.Time
}

func (receipt AdapterPurgeReceipt) validate(target PurgeTarget, descriptor AdapterDescriptor) error {
	if target.Validate() != nil || descriptor.validate() != nil || receipt.AdapterName != descriptor.Name() ||
		receipt.OperationID != target.Operation.OperationID || receipt.SurfaceDigest != digestAdapterSurfaces(descriptor) ||
		zeroDeletionDigest(receipt.ReceiptDigest) || receipt.VerifiedAbsentAt.IsZero() {
		return ErrDeletionSafetyUnavailable
	}
	return nil
}

func ValidateAdapterPurgeReceipt(
	receipt AdapterPurgeReceipt,
	target PurgeTarget,
	descriptor AdapterDescriptor,
) error {
	return receipt.validate(target, descriptor)
}

type CorePurgeCommand struct {
	Operation     DeletionOperation
	SurfaceDigest [sha256.Size]byte
}

func (command CorePurgeCommand) Validate() error {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil || (PurgeTarget{Operation: command.Operation}).Validate() != nil ||
		command.SurfaceDigest != digestAdapterSurfaces(descriptor) {
		return ErrInvalidCoreAdapter
	}
	return nil
}

type RecordCoreStore interface {
	RecordCoreHealth(context.Context) (AdapterHealthSnapshot, error)
	PreviewRecordCore(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error)
	PurgeRecordCore(context.Context, CorePurgeCommand) (AdapterPurgeReceipt, error)
	VerifyRecordCorePurge(context.Context, CorePurgeCommand, AdapterPurgeReceipt) error
}

type CoreAdapter struct {
	store      RecordCoreStore
	descriptor AdapterDescriptor
}

func NewCoreAdapter(store RecordCoreStore) (*CoreAdapter, error) {
	if nilDeletionServiceDependency(store) {
		return nil, ErrInvalidCoreAdapter
	}
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor", ErrInvalidCoreAdapter)
	}
	return &CoreAdapter{store: store, descriptor: descriptor}, nil
}

func (adapter *CoreAdapter) Descriptor() AdapterDescriptor {
	if adapter == nil {
		return AdapterDescriptor{}
	}
	return AdapterDescriptor{name: adapter.descriptor.name, surfaces: adapter.descriptor.Surfaces()}
}

func (adapter *CoreAdapter) HealthSnapshot(ctx context.Context) (AdapterHealthSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) {
		return AdapterHealthSnapshot{}, ErrInvalidCoreAdapter
	}
	health, err := adapter.store.RecordCoreHealth(ctx)
	if err != nil {
		return AdapterHealthSnapshot{}, err
	}
	if err := health.validate(); err != nil {
		return AdapterHealthSnapshot{}, fmt.Errorf("%w: invalid core health", ErrDeletionSafetyUnavailable)
	}
	return health, nil
}

func (adapter *CoreAdapter) PreviewDeletion(ctx context.Context, target PreviewTarget) (AdapterPreviewSnapshot, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || target.Validate() != nil {
		return AdapterPreviewSnapshot{}, ErrInvalidCoreAdapter
	}
	preview, err := adapter.store.PreviewRecordCore(ctx, target)
	if err != nil {
		return AdapterPreviewSnapshot{}, err
	}
	if err := preview.Validate(); err != nil {
		return AdapterPreviewSnapshot{}, fmt.Errorf("%w: invalid core preview", ErrDeletionSafetyUnavailable)
	}
	return preview, nil
}

func (adapter *CoreAdapter) PurgeDeletion(ctx context.Context, target PurgeTarget) (AdapterPurgeReceipt, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || target.Validate() != nil {
		return AdapterPurgeReceipt{}, ErrInvalidCoreAdapter
	}
	command := CorePurgeCommand{Operation: target.Operation, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor)}
	if err := command.Validate(); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	receipt, err := adapter.store.PurgeRecordCore(ctx, command)
	if err != nil {
		return AdapterPurgeReceipt{}, err
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (adapter *CoreAdapter) VerifyDeletion(ctx context.Context, target PurgeTarget, receipt AdapterPurgeReceipt) error {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || target.Validate() != nil {
		return ErrInvalidCoreAdapter
	}
	if err := receipt.validate(target, adapter.descriptor); err != nil {
		return err
	}
	command := CorePurgeCommand{Operation: target.Operation, SurfaceDigest: digestAdapterSurfaces(adapter.descriptor)}
	if err := adapter.store.VerifyRecordCorePurge(ctx, command, receipt); err != nil {
		return err
	}
	return nil
}

type DeletionPurgeAdapter interface {
	DeletionPreviewAdapter
	PurgeDeletion(context.Context, PurgeTarget) (AdapterPurgeReceipt, error)
	VerifyDeletion(context.Context, PurgeTarget, AdapterPurgeReceipt) error
}

type RegistryPurger struct {
	registry Registry
}

func NewRegistryPurger(registry Registry) *RegistryPurger {
	return &RegistryPurger{registry: registry}
}

func (purger *RegistryPurger) PurgeOnline(ctx context.Context, operation DeletionOperation) (OnlinePurgeReceipt, error) {
	if ctx == nil || purger == nil {
		return OnlinePurgeReceipt{}, ErrDeletionSafetyUnavailable
	}
	target := PurgeTarget{Operation: operation}
	if err := target.Validate(); err != nil {
		return OnlinePurgeReceipt{}, err
	}
	if _, err := purger.registry.RequireReady(ctx); err != nil {
		return OnlinePurgeReceipt{}, err
	}
	payload := make([]byte, 0, 1024)
	payload = appendLengthPrefixed(payload, onlinePurgeDigestDomainV1)
	payload = appendUint64(payload, 1)
	payload = appendLengthPrefixed(payload, operation.OperationID)
	receipts := make(map[AdapterName]AdapterPurgeReceipt, len(requiredAdapterNames))
	// Readiness and receipt encoding keep the closed root-first order. Purge runs
	// in reverse so dependent child surfaces release their restrictive references
	// before record_core removes revisions, drafts, and the record root.
	for index := len(requiredAdapterNames) - 1; index >= 0; index-- {
		name := requiredAdapterNames[index]
		registered, exists := purger.registry.adapters[name]
		if !exists {
			return OnlinePurgeReceipt{}, ErrDeletionSafetyUnavailable
		}
		adapter, ok := registered.adapter.(DeletionPurgeAdapter)
		if !ok || nilDeletionServiceDependency(adapter) {
			return OnlinePurgeReceipt{}, fmt.Errorf("%w: adapter %q purge contract", ErrDeletionSafetyUnavailable, name)
		}
		if err := ctx.Err(); err != nil {
			return OnlinePurgeReceipt{}, err
		}
		receipt, err := adapter.PurgeDeletion(ctx, target)
		if err != nil {
			return OnlinePurgeReceipt{}, fmt.Errorf("%w: adapter %q purge: %v", ErrDeletionSafetyUnavailable, name, err)
		}
		if err := receipt.validate(target, registered.descriptor); err != nil {
			return OnlinePurgeReceipt{}, err
		}
		if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
			return OnlinePurgeReceipt{}, fmt.Errorf("%w: adapter %q verify: %v", ErrDeletionSafetyUnavailable, name, err)
		}
		receipts[name] = receipt
	}
	for _, name := range requiredAdapterNames {
		receipt := receipts[name]
		payload = appendLengthPrefixed(payload, string(name))
		payload = append(payload, receipt.SurfaceDigest[:]...)
		payload = append(payload, receipt.ReceiptDigest[:]...)
	}
	return OnlinePurgeReceipt{OperationID: operation.OperationID, ReceiptDigest: sha256.Sum256(payload)}, nil
}

func digestAdapterSurfaces(descriptor AdapterDescriptor) [sha256.Size]byte {
	payload := make([]byte, 0, 512)
	payload = appendLengthPrefixed(payload, adapterSurfaceDigestDomainV1)
	payload = appendUint64(payload, 1)
	payload = appendLengthPrefixed(payload, string(descriptor.Name()))
	surfaces := descriptor.Surfaces()
	payload = appendUint64(payload, uint64(len(surfaces)))
	for _, surface := range surfaces {
		payload = appendLengthPrefixed(payload, string(surface))
	}
	return sha256.Sum256(payload)
}

// RecordCoreSurfaceDigest exposes the closed record_core ownership digest to
// persistence adapters without exposing the descriptor's internal storage.
func RecordCoreSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCore, RecordCoreSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}

func RecordAttachmentsSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordAttachments, RecordAttachmentsSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}

func RecordEvidenceSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordEvidence, RecordEvidenceSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}

func RecordCollaborationSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordCollaboration, RecordCollaborationSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}

func RecordSearchSurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordSearch, RecordSearchSurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}

func RecordActivitySurfaceDigest() [sha256.Size]byte {
	descriptor, err := NewAdapterDescriptor(AdapterNameRecordActivityProjection, RecordActivitySurfaceNames())
	if err != nil {
		return [sha256.Size]byte{}
	}
	return digestAdapterSurfaces(descriptor)
}
