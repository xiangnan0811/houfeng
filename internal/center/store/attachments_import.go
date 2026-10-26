package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

// ObserveImportBlobGC commits a short, read-only snapshot before any Blob Put.
// The fence depends on retaining completed history without rekeying or reopening
// it; reclaim updates existing active rows rather than creating a new lifecycle.
func (repository *PostgresAttachmentRepository) ObserveImportBlobGC(
	ctx context.Context,
	digests [][sha256.Size]byte,
) ([]attachments.ImportBlobGCObservation, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || len(digests) == 0 {
		return nil, attachments.ErrInvalidAttachmentCommand
	}
	keys := make([]string, len(digests))
	for index, digest := range digests {
		keys[index] = "sha256/" + hex.EncodeToString(digest[:])
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin import Blob GC observation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counts, active, err := readImportBlobGCHistory(ctx, tx, keys)
	if err != nil {
		return nil, err
	}
	if active {
		return nil, attachments.ErrBlobGCProtected
	}
	observations := make([]attachments.ImportBlobGCObservation, len(digests))
	for index, digest := range digests {
		observations[index], err = attachments.NewImportBlobGCObservation(digest, counts[index])
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit import Blob GC observation: %w", err)
	}
	return observations, nil
}

// readImportBlobGCHistory uses one MVCC snapshot for the entire batch, retaining
// input order and duplicate keys without locking deletion rows. Registration
// calls it in a separate statement after acquiring the Blob table lock.
func readImportBlobGCHistory(ctx context.Context, tx attachmentTx, keys []string) ([]int64, bool, error) {
	var counts []int64
	var active bool
	if err := tx.QueryRow(ctx, `
		with history as (
			select blob_key, count(*) as n,
			       bool_or(deletion_state <> 'completed') as active
			from public.blob_gc_deletions
			where blob_key = any($1::text[])
			group by blob_key
		)
		select array_agg(coalesce(history.n, 0)::bigint order by keys.ordinality),
		       coalesce(bool_or(coalesce(history.active, false)), false)
		from unnest($1::text[]) with ordinality as keys(blob_key, ordinality)
		left join history using (blob_key)`, keys).Scan(&counts, &active); err != nil {
		return nil, false, fmt.Errorf("read import Blob GC history: %w", err)
	}
	if len(counts) != len(keys) {
		return nil, false, fmt.Errorf("read import Blob GC history: %w", attachments.ErrInvalidAttachmentCommand)
	}
	for _, count := range counts {
		if count < 0 {
			return nil, false, attachments.ErrInvalidAttachmentCommand
		}
	}
	return counts, active, nil
}

func insertImportedAvailableAttachments(
	ctx context.Context,
	tx pgx.Tx,
	projectID, recordID string,
	items []attachments.ImportedAvailableAttachment,
) error {
	if ctx == nil || tx == nil || projectID != "default" || !records.ValidRecordRootID(recordID) {
		return attachments.ErrInvalidAttachmentCommand
	}
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	keys := make([]string, len(items))
	for index, item := range items {
		if item.Validate() != nil {
			return attachments.ErrInvalidAttachmentCommand
		}
		if _, exists := seen[item.AttachmentID]; exists {
			return attachments.ErrInvalidAttachmentReferences
		}
		seen[item.AttachmentID] = struct{}{}
		keys[index] = item.Object.Key
	}

	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `lock table public.blob_objects in row exclusive mode`); err != nil {
		return fmt.Errorf("lock Blob metadata for import: %w", err)
	}
	counts, active, err := readImportBlobGCHistory(ctx, tx, keys)
	if err != nil {
		return err
	}
	if active {
		return attachments.ErrBlobGCProtected
	}
	for index, item := range items {
		if counts[index] != item.GCObservation.DeletionCount() {
			return attachments.ErrBlobGCProtected
		}
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.ErrQuotaOverflow
	}
	for _, item := range items {
		inserted, err := ensureAttachmentBlobLocked(ctx, tx, attachments.BlobObject{
			Key: item.Object.Key, SHA256: item.Object.SHA256,
			ObjectVersion: item.Object.VersionID, SizeBytes: item.Object.SizeBytes,
			BackendKind: item.BackendKind,
		})
		if err != nil {
			return err
		}
		written, err := tx.Exec(ctx, `
			insert into public.record_attachments (
				attachment_id, project_id, record_id,
				attachment_state, display_name, media_type, logical_size_bytes,
				blob_key, blob_object_version, created_by
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			item.AttachmentID, projectID, recordID, attachments.UploadStateAvailable,
			item.DisplayName, item.MediaType, item.LogicalSizeBytes,
			item.Object.Key, item.Object.VersionID, item.CreatedBy,
		)
		if err != nil {
			return fmt.Errorf("insert imported attachment: %w", err)
		}
		if written.RowsAffected() != 1 {
			return attachments.ErrAttachmentConflict
		}
		physicalDelta := int64(0)
		if inserted {
			physicalDelta = item.Object.SizeBytes
		}
		usage, err = usage.SolidifyReservation(0, item.LogicalSizeBytes, physicalDelta)
		if err != nil {
			return err
		}
	}
	return updateAttachmentQuotaAccount(ctx, tx, projectID, quotaVersion, usage)
}
