package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
)

func TestPostgresIntegrationAttachmentRecoveryInventoryPinsAndRestoreVerification(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-recovery", 2),
	)

	empty, err := repository.EnumerateAttachmentInventory(ctx)
	if err != nil {
		t.Fatalf("EnumerateAttachmentInventory(empty) error = %v", err)
	}
	if empty.Blobs == nil || empty.UploadIDs == nil || empty.ProcessorJobIDs == nil || empty.WorkspaceIDs == nil {
		t.Fatalf("EnumerateAttachmentInventory(empty) = %#v, want non-nil empty slices", empty)
	}
	if len(empty.Blobs)+len(empty.UploadIDs)+len(empty.ProcessorJobIDs)+len(empty.WorkspaceIDs) != 0 {
		t.Fatalf("EnumerateAttachmentInventory(empty) = %#v, want empty inventory", empty)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	first := attachmentBlobGCIntegrationObject(
		"attachment recovery first", "local-recovery-first-v1", attachments.BackendKindLocal,
	)
	second := attachmentBlobGCIntegrationObject(
		"attachment recovery second", "local-recovery-second-v1", attachments.BackendKindLocal,
	)
	objects := []attachments.BlobObject{first, second}
	sort.Slice(objects, func(left, right int) bool {
		if objects[left].Key == objects[right].Key {
			return objects[left].ObjectVersion < objects[right].ObjectVersion
		}
		return objects[left].Key < objects[right].Key
	})
	seedAttachmentBlobGCObject(t, ctx, fixture, objects[1], now.Add(-2*time.Hour))
	seedAttachmentBlobGCObject(t, ctx, fixture, objects[0], now.Add(-time.Hour))

	jobZ := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "recoveryz", Source: second, State: attachments.ProcessorStateQueued,
		Profile: attachments.ProcessorProfileText, CreatedAt: now.Add(-2 * time.Minute),
		ExpiresAt: now.Add(time.Hour),
	})
	jobA := seedAttachmentProcessorJob(t, ctx, fixture, attachmentProcessorSeed{
		Suffix: "recoverya", Source: first, State: attachments.ProcessorStateQueued,
		Profile: attachments.ProcessorProfileText, CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
	})
	for _, workspace := range []struct {
		id    string
		jobID string
		seed  string
	}{
		{id: "cpw_recoveryz", jobID: jobZ.ProcessorJobID, seed: "workspace-z"},
		{id: "cpw_recoverya", jobID: jobA.ProcessorJobID, seed: "workspace-a"},
	} {
		digest := sha256.Sum256([]byte(workspace.seed))
		if _, err := fixture.db.Exec(ctx, `
			insert into public.content_processor_workspaces (
				workspace_id, processor_job_id, attempt, workspace_state,
				workspace_path_digest, created_at, updated_at, expires_at
			) values ($1, $2, 0, 'registered', $3, $4, $4, $5)`,
			workspace.id, workspace.jobID, digest[:], now, now.Add(time.Hour),
		); err != nil {
			t.Fatalf("seed attachment recovery workspace %q: %v", workspace.id, err)
		}
	}

	inventory, err := repository.EnumerateAttachmentInventory(ctx)
	if err != nil {
		t.Fatalf("EnumerateAttachmentInventory() error = %v", err)
	}
	want := attachments.AttachmentRecoveryInventory{
		Blobs:           objects,
		UploadIDs:       []string{jobA.UploadID, jobZ.UploadID},
		ProcessorJobIDs: []string{jobA.ProcessorJobID, jobZ.ProcessorJobID},
		WorkspaceIDs:    []string{"cpw_recoverya", "cpw_recoveryz"},
	}
	if !reflect.DeepEqual(inventory, want) {
		t.Fatalf("EnumerateAttachmentInventory() = %#v, want %#v", inventory, want)
	}
	if err := inventory.Validate(); err != nil {
		t.Fatalf("EnumerateAttachmentInventory().Validate() error = %v", err)
	}

	pin := attachments.CreateBlobGCPinCommand{
		PinID:             "bgp_recoveryinventory",
		OwnerKind:         attachments.BlobGCPinOwnerRestoreAttempt,
		OwnerID:           "restore_recovery_inventory",
		BlobKey:           first.Key,
		BlobObjectVersion: first.ObjectVersion,
		ExpiresAt:         now.Add(30 * time.Minute),
	}
	protection, err := repository.CreateAttachmentRecoveryPin(ctx, pin)
	if err != nil {
		t.Fatalf("CreateAttachmentRecoveryPin() error = %v", err)
	}
	if protection.ActivePinCount != 1 {
		t.Fatalf("CreateAttachmentRecoveryPin() protection = %#v, want one active pin", protection)
	}
	protection, err = repository.ReleaseAttachmentRecoveryPin(ctx, attachments.ReleaseBlobGCPinCommand{
		PinID: pin.PinID, OwnerKind: pin.OwnerKind, OwnerID: pin.OwnerID,
		BlobKey: pin.BlobKey, BlobObjectVersion: pin.BlobObjectVersion,
	})
	if err != nil {
		t.Fatalf("ReleaseAttachmentRecoveryPin() error = %v", err)
	}
	if protection.ActivePinCount != 0 {
		t.Fatalf("ReleaseAttachmentRecoveryPin() protection = %#v, want no active pin", protection)
	}

	if err := repository.VerifyRestoredAttachmentBlob(ctx, first); err != nil {
		t.Fatalf("VerifyRestoredAttachmentBlob(exact) error = %v", err)
	}
	wrongSize := first
	wrongSize.SizeBytes++
	if err := repository.VerifyRestoredAttachmentBlob(ctx, wrongSize); !errors.Is(err, attachments.ErrRestoredBlobMismatch) {
		t.Fatalf("VerifyRestoredAttachmentBlob(wrong size) error = %v, want ErrRestoredBlobMismatch", err)
	}
	missing := attachmentBlobGCIntegrationObject(
		"attachment recovery missing", "local-recovery-missing-v1", attachments.BackendKindLocal,
	)
	if err := repository.VerifyRestoredAttachmentBlob(ctx, missing); !errors.Is(err, attachments.ErrRestoredBlobMismatch) {
		t.Fatalf("VerifyRestoredAttachmentBlob(missing) error = %v, want ErrRestoredBlobMismatch", err)
	}
}
