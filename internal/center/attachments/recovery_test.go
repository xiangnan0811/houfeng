package attachments

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRecoveryAdapterValidatesOrderedInventoryPinsAndRestoreIdentity(t *testing.T) {
	t.Parallel()

	first := recoveryTestBlob(1, "local-v1")
	second := recoveryTestBlob(2, "local-v2")
	repository := &recoveryRepositoryStub{
		inventory: AttachmentRecoveryInventory{
			Blobs:           []BlobObject{first, second},
			UploadIDs:       []string{"aup_one"},
			ProcessorJobIDs: []string{"apj_one"},
			WorkspaceIDs:    []string{"cpw_one"},
		},
	}
	adapter, err := NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	inventory, err := adapter.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if !reflect.DeepEqual(inventory, repository.inventory) {
		t.Fatalf("Inventory() = %#v, want %#v", inventory, repository.inventory)
	}
	inventory.Blobs[0].Key = "tampered"
	if repository.inventory.Blobs[0].Key == "tampered" {
		t.Fatal("Inventory() returned mutable repository storage")
	}

	pin := CreateBlobGCPinCommand{
		PinID:             "bgp_restoreone",
		OwnerKind:         BlobGCPinOwnerRestoreAttempt,
		OwnerID:           "restore_one",
		BlobKey:           first.Key,
		BlobObjectVersion: first.ObjectVersion,
		ExpiresAt:         time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	}
	if _, err := adapter.CreatePin(context.Background(), pin); err != nil {
		t.Fatalf("CreatePin() error = %v", err)
	}
	if repository.createPinCalls != 1 {
		t.Fatalf("CreatePin() calls = %d, want 1", repository.createPinCalls)
	}

	if err := adapter.VerifyRestoredBlob(context.Background(), first, first); err != nil {
		t.Fatalf("VerifyRestoredBlob(equal) error = %v", err)
	}
	mismatch := first
	mismatch.ObjectVersion = "local-other"
	if err := adapter.VerifyRestoredBlob(context.Background(), first, mismatch); !errors.Is(err, ErrRestoredBlobMismatch) {
		t.Fatalf("VerifyRestoredBlob(mismatch) error = %v, want ErrRestoredBlobMismatch", err)
	}
	if repository.verifyBlobCalls != 1 {
		t.Fatalf("VerifyRestoredAttachmentBlob() calls = %d, want only exact identity", repository.verifyBlobCalls)
	}
}

func TestRecoveryAdapterPreservesNonNilEmptyInventory(t *testing.T) {
	t.Parallel()

	adapter, err := NewRecoveryAdapter(&recoveryRepositoryStub{
		inventory: AttachmentRecoveryInventory{
			Blobs:           make([]BlobObject, 0),
			UploadIDs:       make([]string, 0),
			ProcessorJobIDs: make([]string, 0),
			WorkspaceIDs:    make([]string, 0),
		},
	})
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	inventory, err := adapter.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if inventory.Blobs == nil || inventory.UploadIDs == nil ||
		inventory.ProcessorJobIDs == nil || inventory.WorkspaceIDs == nil {
		t.Fatalf("Inventory() = %#v, want non-nil empty slices", inventory)
	}
}

type recoveryRepositoryStub struct {
	inventory       AttachmentRecoveryInventory
	createPinCalls  int
	verifyBlobCalls int
}

func (repository *recoveryRepositoryStub) EnumerateAttachmentInventory(context.Context) (AttachmentRecoveryInventory, error) {
	return repository.inventory, nil
}

func (repository *recoveryRepositoryStub) CreateAttachmentRecoveryPin(context.Context, CreateBlobGCPinCommand) (BlobProtection, error) {
	repository.createPinCalls++
	return BlobProtection{}, nil
}

func (repository *recoveryRepositoryStub) ReleaseAttachmentRecoveryPin(context.Context, ReleaseBlobGCPinCommand) (BlobProtection, error) {
	return BlobProtection{}, nil
}

func (repository *recoveryRepositoryStub) VerifyRestoredAttachmentBlob(context.Context, BlobObject) error {
	repository.verifyBlobCalls++
	return nil
}

func recoveryTestBlob(seed byte, version string) BlobObject {
	var digest [32]byte
	for index := range digest {
		digest[index] = seed + byte(index)
	}
	return BlobObject{
		Key:           "sha256/" + hexDigest(digest),
		SHA256:        digest,
		ObjectVersion: version,
		SizeBytes:     int64(seed + 1),
		BackendKind:   BackendKindLocal,
	}
}
