package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
)

var _ attachments.AttachmentRecoveryRepository = (*PostgresAttachmentRepository)(nil)

func (repository *PostgresAttachmentRepository) EnumerateAttachmentInventory(
	ctx context.Context,
) (attachments.AttachmentRecoveryInventory, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil {
		return attachments.AttachmentRecoveryInventory{}, attachments.ErrInvalidRecoveryRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf("begin attachment recovery inventory: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inventory, err := readAttachmentRecoveryInventory(ctx, tx)
	if err != nil {
		return attachments.AttachmentRecoveryInventory{}, err
	}
	if err := inventory.Validate(); err != nil {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf(
			"%w: persisted inventory", attachments.ErrRecoveryContractFailure,
		)
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf("commit attachment recovery inventory: %w", err)
	}
	return inventory, nil
}

func (repository *PostgresAttachmentRepository) CreateAttachmentRecoveryPin(
	ctx context.Context,
	command attachments.CreateBlobGCPinCommand,
) (attachments.BlobProtection, error) {
	return repository.CreateBlobGCPin(ctx, command)
}

func (repository *PostgresAttachmentRepository) ReleaseAttachmentRecoveryPin(
	ctx context.Context,
	command attachments.ReleaseBlobGCPinCommand,
) (attachments.BlobProtection, error) {
	return repository.ReleaseBlobGCPin(ctx, command)
}

func (repository *PostgresAttachmentRepository) VerifyRestoredAttachmentBlob(
	ctx context.Context,
	object attachments.BlobObject,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || object.Validate() != nil {
		return attachments.ErrInvalidRecoveryRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin restored attachment Blob verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var digest []byte
	var version string
	var sizeBytes int64
	var backendKind attachments.BackendKind
	err = tx.QueryRow(ctx, `
		select sha256_digest, object_version, size_bytes, backend_kind
		from public.blob_objects
		where blob_key = $1`, object.Key,
	).Scan(&digest, &version, &sizeBytes, &backendKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ErrRestoredBlobMismatch
	}
	if err != nil {
		return fmt.Errorf("read restored attachment Blob metadata: %w", err)
	}
	if len(digest) != sha256.Size || version != object.ObjectVersion || sizeBytes != object.SizeBytes ||
		backendKind != object.BackendKind || !bytes.Equal(digest, object.SHA256[:]) {
		return attachments.ErrRestoredBlobMismatch
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit restored attachment Blob verification: %w", err)
	}
	return nil
}

func readAttachmentRecoveryInventory(
	ctx context.Context,
	tx attachmentTx,
) (attachments.AttachmentRecoveryInventory, error) {
	var blobKeys, blobVersions, blobBackends []string
	var blobDigests [][]byte
	var blobSizes []int64
	if err := tx.QueryRow(ctx, `
		select coalesce(array_agg(blob_key order by blob_key, object_version), array[]::text[]),
		       coalesce(array_agg(object_version order by blob_key, object_version), array[]::text[]),
		       coalesce(array_agg(sha256_digest order by blob_key, object_version), array[]::bytea[]),
		       coalesce(array_agg(size_bytes order by blob_key, object_version), array[]::bigint[]),
		       coalesce(array_agg(backend_kind order by blob_key, object_version), array[]::text[])
		from public.blob_objects`,
	).Scan(&blobKeys, &blobVersions, &blobDigests, &blobSizes, &blobBackends); err != nil {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf("enumerate attachment recovery Blobs: %w", err)
	}
	if len(blobKeys) != len(blobVersions) || len(blobKeys) != len(blobDigests) ||
		len(blobKeys) != len(blobSizes) || len(blobKeys) != len(blobBackends) {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf(
			"%w: inconsistent Blob inventory", attachments.ErrRecoveryContractFailure,
		)
	}

	inventory := attachments.AttachmentRecoveryInventory{
		Blobs:           make([]attachments.BlobObject, len(blobKeys)),
		UploadIDs:       make([]string, 0),
		ProcessorJobIDs: make([]string, 0),
		WorkspaceIDs:    make([]string, 0),
	}
	for index := range blobKeys {
		if len(blobDigests[index]) != sha256.Size {
			return attachments.AttachmentRecoveryInventory{}, fmt.Errorf(
				"%w: invalid Blob digest", attachments.ErrRecoveryContractFailure,
			)
		}
		inventory.Blobs[index] = attachments.BlobObject{
			Key:           blobKeys[index],
			ObjectVersion: blobVersions[index],
			SizeBytes:     blobSizes[index],
			BackendKind:   attachments.BackendKind(blobBackends[index]),
		}
		copy(inventory.Blobs[index].SHA256[:], blobDigests[index])
	}

	if err := tx.QueryRow(ctx, `
		select coalesce((select array_agg(upload_id order by upload_id)
		                 from public.attachment_uploads), array[]::text[]),
		       coalesce((select array_agg(processor_job_id order by processor_job_id)
		                 from public.attachment_processor_jobs), array[]::text[]),
		       coalesce((select array_agg(workspace_id order by workspace_id)
		                 from public.content_processor_workspaces), array[]::text[])`,
	).Scan(&inventory.UploadIDs, &inventory.ProcessorJobIDs, &inventory.WorkspaceIDs); err != nil {
		return attachments.AttachmentRecoveryInventory{}, fmt.Errorf("enumerate attachment recovery partials: %w", err)
	}
	return inventory, nil
}
