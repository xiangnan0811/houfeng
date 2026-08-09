package attachments

import (
	"context"
	"errors"
	"fmt"
)

const AttachmentRecoveryContractVersionV1 uint64 = 1

var (
	ErrInvalidRecoveryRequest  = errors.New("invalid attachment recovery request")
	ErrRecoveryContractFailure = errors.New("attachment recovery contract unavailable")
	ErrRestoredBlobMismatch    = errors.New("restored Blob identity mismatch")
)

type AttachmentRecoveryInventory struct {
	Blobs           []BlobObject
	UploadIDs       []string
	ProcessorJobIDs []string
	WorkspaceIDs    []string
}

func (inventory AttachmentRecoveryInventory) Validate() error {
	if inventory.Blobs == nil || inventory.UploadIDs == nil || inventory.ProcessorJobIDs == nil || inventory.WorkspaceIDs == nil {
		return ErrInvalidRecoveryRequest
	}
	for index, blob := range inventory.Blobs {
		if blob.Validate() != nil || (index > 0 && compareRecoveryBlobs(inventory.Blobs[index-1], blob) >= 0) {
			return ErrInvalidRecoveryRequest
		}
	}
	if validateOrderedRecoveryIDs(inventory.UploadIDs, "aup_") != nil ||
		validateOrderedRecoveryIDs(inventory.ProcessorJobIDs, "apj_") != nil ||
		validateOrderedRecoveryIDs(inventory.WorkspaceIDs, "cpw_") != nil {
		return ErrInvalidRecoveryRequest
	}
	return nil
}

func (inventory AttachmentRecoveryInventory) clone() AttachmentRecoveryInventory {
	return AttachmentRecoveryInventory{
		Blobs:           append(make([]BlobObject, 0, len(inventory.Blobs)), inventory.Blobs...),
		UploadIDs:       append(make([]string, 0, len(inventory.UploadIDs)), inventory.UploadIDs...),
		ProcessorJobIDs: append(make([]string, 0, len(inventory.ProcessorJobIDs)), inventory.ProcessorJobIDs...),
		WorkspaceIDs:    append(make([]string, 0, len(inventory.WorkspaceIDs)), inventory.WorkspaceIDs...),
	}
}

type AttachmentRecoveryRepository interface {
	EnumerateAttachmentInventory(context.Context) (AttachmentRecoveryInventory, error)
	CreateAttachmentRecoveryPin(context.Context, CreateBlobGCPinCommand) (BlobProtection, error)
	ReleaseAttachmentRecoveryPin(context.Context, ReleaseBlobGCPinCommand) (BlobProtection, error)
	VerifyRestoredAttachmentBlob(context.Context, BlobObject) error
}

type RecoveryAdapter struct {
	repository AttachmentRecoveryRepository
}

func NewRecoveryAdapter(repository AttachmentRecoveryRepository) (*RecoveryAdapter, error) {
	if nilUploadServiceDependency(repository) {
		return nil, ErrInvalidRecoveryRequest
	}
	return &RecoveryAdapter{repository: repository}, nil
}

func (adapter *RecoveryAdapter) ContractVersion() uint64 {
	return AttachmentRecoveryContractVersionV1
}

func (adapter *RecoveryAdapter) Inventory(ctx context.Context) (AttachmentRecoveryInventory, error) {
	if err := adapter.validate(ctx); err != nil {
		return AttachmentRecoveryInventory{}, err
	}
	inventory, err := adapter.repository.EnumerateAttachmentInventory(ctx)
	if err != nil {
		return AttachmentRecoveryInventory{}, err
	}
	if err := inventory.Validate(); err != nil {
		return AttachmentRecoveryInventory{}, fmt.Errorf("%w: inventory", ErrRecoveryContractFailure)
	}
	return inventory.clone(), nil
}

func (adapter *RecoveryAdapter) CreatePin(
	ctx context.Context,
	command CreateBlobGCPinCommand,
) (BlobProtection, error) {
	if err := adapter.validate(ctx); err != nil || command.Validate() != nil {
		return BlobProtection{}, ErrInvalidRecoveryRequest
	}
	return adapter.repository.CreateAttachmentRecoveryPin(ctx, command)
}

func (adapter *RecoveryAdapter) ReleasePin(
	ctx context.Context,
	command ReleaseBlobGCPinCommand,
) (BlobProtection, error) {
	if err := adapter.validate(ctx); err != nil || command.Validate() != nil {
		return BlobProtection{}, ErrInvalidRecoveryRequest
	}
	return adapter.repository.ReleaseAttachmentRecoveryPin(ctx, command)
}

func (adapter *RecoveryAdapter) VerifyRestoredBlob(
	ctx context.Context,
	expected BlobObject,
	actual BlobObject,
) error {
	if err := adapter.validate(ctx); err != nil || expected.Validate() != nil || actual.Validate() != nil {
		return ErrInvalidRecoveryRequest
	}
	if expected != actual {
		return ErrRestoredBlobMismatch
	}
	if err := adapter.repository.VerifyRestoredAttachmentBlob(ctx, actual); err != nil {
		return err
	}
	return nil
}

func (adapter *RecoveryAdapter) validate(ctx context.Context) error {
	if ctx == nil || adapter == nil || nilUploadServiceDependency(adapter.repository) {
		return ErrInvalidRecoveryRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func compareRecoveryBlobs(left, right BlobObject) int {
	if left.Key < right.Key {
		return -1
	}
	if left.Key > right.Key {
		return 1
	}
	if left.ObjectVersion < right.ObjectVersion {
		return -1
	}
	if left.ObjectVersion > right.ObjectVersion {
		return 1
	}
	return 0
}

func validateOrderedRecoveryIDs(values []string, prefix string) error {
	for index, value := range values {
		if !validPrefixedID(value, prefix) || (index > 0 && values[index-1] >= value) {
			return ErrInvalidRecoveryRequest
		}
	}
	return nil
}
