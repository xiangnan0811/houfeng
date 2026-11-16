package store

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/records"
)

// readAttachmentWriteObligations returns unlocked observations. Callers must
// acquire the declared rows and rediscover the same complete set before using
// them; a missing obligation never establishes authority or absence.
func readAttachmentWriteObligations(ctx context.Context, tx pgx.Tx, ids []string) ([]records.RevisionMutationWriteObligation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `select project_id, obligation_id, origin_kind, origin_id, coalesce(origin_owner_id,''),
		writer_instance::text, writer_principal::text, coalesce(operation_id,''), coalesce(temporary_key,''),
		request_seq, coalesce(temporary_device,0), coalesce(temporary_inode,0),
		coalesce(temporary_created_at,'0001-01-01 00:00:00+00'::timestamptz), origin_generation,
		coalesce(origin_owner_generation,0), coalesce(origin_owner_expires_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(record_id,''), coalesce(record_epoch,0), coalesce(namespace_id::text,''), coalesce(incarnation_id::text,''),
		backend_kind, canonical_key, encode(storage_binding,'hex'), sha256_digest, plan_digest, archive_digest, size_bytes,
		coalesce(publication_id,''), coalesce(object_version,''), outcome, coalesce(dispatch_id::text,''),
		coalesce(dispatched_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(returned_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(handed_off_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(published_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(cleanup_owner_id,''), cleanup_generation,
		coalesce(cleanup_expires_at,'0001-01-01 00:00:00+00'::timestamptz),
		coalesce(cleanup_dispatch_id::text,''), coalesce(cleanup_outcome,''), cleanup_receipt,
		coalesce(cleanup_completed_at,'0001-01-01 00:00:00+00'::timestamptz), quota_settlement_id,
		coalesce(quota_settled_at,'0001-01-01 00:00:00+00'::timestamptz)
		from public.record_write_obligations where obligation_id=any($1::text[])
		order by obligation_id collate "C"`, ids)
	if err != nil {
		return nil, fmt.Errorf("discover attachment write obligations: %w", err)
	}
	defer rows.Close()
	var result []records.RevisionMutationWriteObligation
	for rows.Next() {
		var item records.RevisionMutationWriteObligation
		var digest, planDigest, archiveDigest, receipt []byte
		var binding *string
		if err := rows.Scan(&item.ProjectID, &item.ObligationID, &item.OriginKind, &item.OriginID, &item.OriginOwnerID,
			&item.WriterInstance, &item.WriterPrincipal, &item.OperationID, &item.TemporaryKey,
			&item.RequestSeq, &item.TemporaryDevice, &item.TemporaryInode, &item.TemporaryCreatedAt,
			&item.OriginGeneration, &item.OriginOwnerGeneration, &item.OriginOwnerExpiresAt,
			&item.RecordID, &item.RecordEpoch, &item.NamespaceID, &item.IncarnationID,
			&item.BackendKind, &item.CanonicalKey, &binding, &digest, &planDigest, &archiveDigest, &item.SizeBytes,
			&item.PublicationID, &item.ObjectVersion, &item.Outcome, &item.DispatchID,
			&item.DispatchedAt, &item.ReturnedAt, &item.HandedOffAt, &item.PublishedAt,
			&item.CleanupOwnerID, &item.CleanupGeneration, &item.CleanupExpiresAt,
			&item.CleanupDispatchID, &item.CleanupOutcome, &receipt, &item.CleanupCompletedAt,
			&item.QuotaSettlementID, &item.QuotaSettledAt); err != nil {
			return nil, fmt.Errorf("scan attachment write obligation: %w", err)
		}
		if binding != nil {
			item.StorageBinding = *binding
		}
		if len(digest) != sha256.Size || (planDigest != nil && len(planDigest) != sha256.Size) ||
			(archiveDigest != nil && len(archiveDigest) != sha256.Size) || (receipt != nil && len(receipt) != sha256.Size) {
			return nil, records.ErrInvalidRevisionMutationResources
		}
		copy(item.SHA256[:], digest)
		copy(item.PlanDigest[:], planDigest)
		copy(item.ArchiveDigest[:], archiveDigest)
		copy(item.CleanupReceipt[:], receipt)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read attachment write obligations: %w", err)
	}
	return result, nil
}
