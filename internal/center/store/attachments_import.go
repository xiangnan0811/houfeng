package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordidentity"
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
	if ctx == nil || tx == nil || projectID != "default" || !recordidentity.IsValid(recordID) {
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
	if _, err := tx.Exec(ctx, `select public.record_revision_blob_fence()`); err != nil {
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
		body, err := json.Marshal(importAttachmentPublication{
			RecordID: recordID, AttachmentID: item.AttachmentID, ObligationID: item.ObligationID,
			NamespaceID: item.NamespaceID, BackendKind: item.BackendKind,
			Key: item.Object.Key, Version: item.Object.VersionID, SHA256: hex.EncodeToString(item.Object.SHA256[:]),
			Size: item.Object.SizeBytes, DisplayName: item.DisplayName, MediaType: item.MediaType,
			CreatedBy: item.CreatedBy, GCCount: item.GCObservation.DeletionCount(),
		})
		if err != nil {
			return err
		}
		var inserted bool
		if err := tx.QueryRow(ctx, `select public.record_import_attachment_publish($1)`, body).Scan(&inserted); err != nil {
			return fmt.Errorf("publish imported attachment: %w", err)
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

type importAttachmentPublication struct {
	RecordID     string                  `json:"record_id"`
	AttachmentID string                  `json:"attachment_id"`
	ObligationID string                  `json:"obligation_id"`
	NamespaceID  string                  `json:"namespace_id"`
	BackendKind  attachments.BackendKind `json:"backend_kind"`
	Key          string                  `json:"key"`
	Version      string                  `json:"version"`
	SHA256       string                  `json:"sha256"`
	Size         int64                   `json:"size"`
	DisplayName  string                  `json:"display_name"`
	MediaType    string                  `json:"media_type"`
	CreatedBy    string                  `json:"created_by"`
	GCCount      int64                   `json:"gc_count"`
}
