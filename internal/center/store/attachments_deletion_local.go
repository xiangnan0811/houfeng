package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

// ConfigureAttachmentDeletionLocalCleanup binds the admitted namespace and the
// guarded coordinator. Its repository must use the caller's direct worker role;
// this configuration neither acquires nor supplies database authority.
func (repository *PostgresAttachmentRepository) ConfigureAttachmentDeletionLocalCleanup(coordinator *attachments.LocalCleanupCoordinator) error {
	if repository == nil || coordinator == nil {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionBlobMu.Lock()
	defer repository.attachmentDeletionBlobMu.Unlock()
	if repository.attachmentDeletionLocalCleanup != nil && repository.attachmentDeletionLocalCleanup != coordinator {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionLocalCleanup = coordinator
	return nil
}

type localAdapterPurgeCommand struct {
	attachments.LocalCleanupOperation
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	RecordID      string `json:"record_id"`
	ReservationID string `json:"reservation_id"`
	FenceEpoch    uint64 `json:"fence_epoch"`
	SurfaceDigest string `json:"surface_digest"`
}

type localAdapterPurgeReceipt struct {
	Digest      string    `json:"receipt_digest"`
	RemovedRows uint64    `json:"removed_row_count"`
	VerifiedAt  time.Time `json:"verified_absent_at"`
}

func (repository *PostgresAttachmentRepository) localAttachmentPurgeBinding(command attachments.AttachmentDeletionCommand) (*attachments.LocalCleanupCoordinator, localAdapterPurgeCommand, []byte, error) {
	repository.attachmentDeletionBlobMu.RLock()
	coordinator := repository.attachmentDeletionLocalCleanup
	repository.attachmentDeletionBlobMu.RUnlock()
	return localAdapterPurgeBinding(coordinator, command.PurgeTarget, command.SurfaceDigest)
}

func localAdapterPurgeBinding(coordinator *attachments.LocalCleanupCoordinator, target recorddeletion.PurgeTarget, surfaceDigest [sha256.Size]byte) (*attachments.LocalCleanupCoordinator, localAdapterPurgeCommand, []byte, error) {
	if coordinator == nil || target.Validate() != nil || target.Operation.ContractVersion != recorddeletion.DeletionContractV2 {
		return nil, localAdapterPurgeCommand{}, nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	owner := target.OwnerClaim.Owner
	generation := int64(owner.Generation)
	request := localAdapterPurgeCommand{
		LocalCleanupOperation: attachments.LocalCleanupOperation{
			OperationID: &target.Operation.OperationID, OwnerID: &owner.OwnerID,
			Generation: &generation, ExpiresAt: &owner.ExpiresAt,
		},
		NamespaceID: coordinator.NamespaceID(), IncarnationID: coordinator.IncarnationID(),
		RecordID: target.Operation.Object.ObjectID, ReservationID: target.Operation.ReservationID,
		FenceEpoch: uint64(target.Operation.FenceEpoch), SurfaceDigest: hex.EncodeToString(surfaceDigest[:]),
	}
	body, err := json.Marshal(request)
	return coordinator, request, body, err
}

func (repository *PostgresAttachmentRepository) localAttachmentPurgeCommand(ctx context.Context, statement string, body []byte, result any) error {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var encoded []byte
	if err := tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, result); err != nil {
		return err
	}
	// A lost commit response returns an error. Retrying the immutable operation
	// resolves the original manifest; it cannot create a second cleanup target.
	return tx.Commit(ctx)
}

func (repository *PostgresAttachmentRepository) purgeLocalRecordAttachments(ctx context.Context, command attachments.AttachmentDeletionCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	coordinator, request, body, err := repository.localAttachmentPurgeBinding(command)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	var preparation struct {
		Targets []struct {
			DeletionID string `json:"deletion_id"`
			Key        string `json:"key"`
		} `json:"targets"`
	}
	if err := repository.localAttachmentPurgeCommand(ctx, `select public.local_attachment_purge_prepare($1)`, body, &preparation); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("prepare local attachment purge: %w", err)
	}
	for _, target := range preparation.Targets {
		_, err := coordinator.Cleanup(ctx, attachments.LocalCleanupPreparation{
			LocalCleanupOperation: request.LocalCleanupOperation,
			DeletionID:            target.DeletionID, NamespaceID: request.NamespaceID,
			IncarnationID: request.IncarnationID, Key: target.Key,
		}, command.OwnerClaim.Owner.OwnerID)
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("clean local attachment purge target: %w", err)
		}
	}
	var completed localAdapterPurgeReceipt
	if err := repository.localAttachmentPurgeCommand(ctx, `select public.local_attachment_purge_complete($1)`, body, &completed); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("complete local attachment purge: %w", err)
	}
	return completed.adapterReceipt(recorddeletion.AdapterNameRecordAttachments, command.PurgeTarget, command.SurfaceDigest)
}

func (repository *PostgresAttachmentRepository) verifyLocalRecordAttachmentsPurge(ctx context.Context, command attachments.AttachmentDeletionCommand, receipt recorddeletion.AdapterPurgeReceipt) error {
	_, _, body, err := repository.localAttachmentPurgeBinding(command)
	if err != nil {
		return err
	}
	var completed localAdapterPurgeReceipt
	if err := repository.localAttachmentPurgeCommand(ctx, `select public.local_attachment_purge_verify($1)`, body, &completed); err != nil {
		return fmt.Errorf("verify local attachment purge: %w", err)
	}
	stored, err := completed.adapterReceipt(recorddeletion.AdapterNameRecordAttachments, command.PurgeTarget, command.SurfaceDigest)
	if err != nil {
		return err
	}
	if stored != receipt {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func (completed localAdapterPurgeReceipt) adapterReceipt(name recorddeletion.AdapterName, target recorddeletion.PurgeTarget, surfaceDigest [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error) {
	digest, err := hex.DecodeString(completed.Digest)
	if err != nil || len(digest) != sha256.Size || completed.VerifiedAt.IsZero() {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return recorddeletion.AdapterPurgeReceipt{
		AdapterName: name, OperationID: target.Operation.OperationID,
		SurfaceDigest: surfaceDigest, ReceiptDigest: [sha256.Size]byte(digest),
		RemovedRowCount: completed.RemovedRows, VerifiedAbsentAt: completed.VerifiedAt.UTC(),
	}, nil
}
