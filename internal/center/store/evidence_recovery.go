package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
)

var _ evidence.EvidenceRecoveryRepository = (*PostgresEvidenceRepository)(nil)

// RestoreEvidenceInventory replays an adapter-validated inventory in one
// admitted transaction. The insert order is dependency-safe and deliberately
// has no generic JSON path: envelopes are expanded into the closed evidence
// schema and canonical payloads retain their content-addressed representation.
func (repository *PostgresEvidenceRepository) RestoreEvidenceInventory(
	ctx context.Context,
	inventory evidence.EvidenceRecoveryInventory,
) error {
	if ctx == nil || repository == nil || inventory.Payloads == nil || inventory.Snapshots == nil ||
		inventory.CaptureIntents == nil || inventory.RevisionReferences == nil || inventory.CopyLineage == nil {
		return evidence.ErrInvalidRecoveryInventory
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, payload := range inventory.Payloads {
		if err := restoreEvidencePayload(ctx, tx, payload); err != nil {
			return fmt.Errorf("restore evidence payload %x: %w", payload.Digest, err)
		}
	}
	for _, snapshot := range inventory.Snapshots {
		if err := restoreEvidenceSnapshot(ctx, tx, snapshot); err != nil {
			return fmt.Errorf("restore evidence snapshot %s: %w", snapshot.SnapshotID, err)
		}
	}
	for _, binding := range inventory.CaptureIntents {
		if err := restoreEvidenceCaptureIntent(ctx, tx, binding); err != nil {
			return fmt.Errorf("restore evidence capture intent %s: %w", binding.Intent.ID, err)
		}
	}
	for _, reference := range inventory.RevisionReferences {
		if err := restoreEvidenceRevisionReference(ctx, tx, reference); err != nil {
			return fmt.Errorf("restore evidence revision reference %s/%d: %w", reference.RevisionID, reference.Ordinal, err)
		}
	}
	for _, lineage := range inventory.CopyLineage {
		if err := restoreEvidenceCopyLineage(ctx, tx, lineage); err != nil {
			return fmt.Errorf("restore evidence copy lineage %s: %w", lineage.SnapshotID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit evidence recovery replay: %w", err)
	}
	return nil
}

func restoreEvidencePayload(ctx context.Context, tx pgx.Tx, payload evidence.EvidenceRecoveryPayload) error {
	if payload.Digest == [sha256.Size]byte{} ||
		validateEvidencePayloadBinding(payload.CanonicalPayload, payload.Digest, uint64(len(payload.CanonicalPayload))) != nil {
		return evidence.ErrInvalidRecoveryInventory
	}
	compressed, err := deterministicEvidenceGzip(payload.CanonicalPayload)
	if err != nil || len(compressed) == 0 || uint64(len(compressed)) > maxEvidenceCompressedBytes {
		return evidence.ErrInvalidRecoveryInventory
	}
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload
		) values ($1, $2, $3, $4)
		on conflict do nothing`,
		payload.Digest[:], int64(len(payload.CanonicalPayload)), int64(len(compressed)), compressed,
	)
	if err != nil {
		return fmt.Errorf("restore evidence payload: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var exact bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.evidence_payloads
				where payload_digest = $1
				  and payload_encoding = $2
				  and canonical_size_bytes = $3
				  and compressed_size_bytes = $4
				  and compressed_payload = $5
			)`, payload.Digest[:], EvidencePayloadEncodingCanonicalJSONGzipV1,
			int64(len(payload.CanonicalPayload)), int64(len(compressed)), compressed,
		).Scan(&exact); err != nil || !exact {
			return evidence.ErrInvalidRecoveryInventory
		}
	} else if inserted.RowsAffected() != 1 {
		return evidence.ErrInvalidRecoveryInventory
	}
	return nil
}

func restoreEvidenceSnapshot(ctx context.Context, tx pgx.Tx, recovered evidence.EvidenceRecoverySnapshot) error {
	envelope := recovered.Envelope
	if !validEvidenceStoreID(recovered.RecordID, "rec_") || !evidence.ValidSnapshotID(recovered.SnapshotID) ||
		recovered.PayloadDigest == [sha256.Size]byte{} || recovered.PayloadDigest != envelope.CanonicalHash ||
		envelope.CanonicalSize == 0 || envelope.CanonicalSize > evidence.MaxCanonicalPayloadBytes ||
		!evidenceRecoverySnapshotTimestampsExact(envelope) {
		return evidence.ErrInvalidRecoveryInventory
	}
	values := []any{envelope.Subject, envelope.Source, envelope.Authorization, envelope.ActualPrecision,
		envelope.BucketWidth, envelope.Units, envelope.Quality, envelope.QuotaOutcome, envelope.Retention, envelope.Redaction}
	encoded := make([][]byte, len(values))
	for index, value := range values {
		var err error
		encoded[index], err = marshalEvidenceParticipantJSON(value)
		if err != nil {
			return evidence.ErrInvalidRecoveryInventory
		}
	}
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_snapshots (
			snapshot_id, record_id, kind, schema_version, source_kind, source_id,
			subject_identity_snapshot, source_identity_snapshot,
			capture_authorization, capture_authorization_digest,
			requested_started_at, requested_ended_at,
			actual_started_at, actual_ended_at,
			observed_at, captured_at, referenced_at,
			source_revision, source_watermark, source_digest,
			producer_version, calculation_version,
			actual_precision, bucket_width, unit_semantics, quality,
			quota_outcome, retention, sensitivity_level, redaction,
			canonical_hash, logical_size_bytes, payload_digest
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $25, $26, $27, $28, $29, $30,
			$31, $32, $33
		)
		on conflict do nothing`,
		recovered.SnapshotID, recovered.RecordID, string(envelope.Key.Kind), int64(envelope.Key.SchemaVersion),
		envelope.Source.Type, envelope.Source.ID, encoded[0], encoded[1], encoded[2], envelope.Authorization.Digest[:],
		envelope.RequestedWindow.Start, envelope.RequestedWindow.End, envelope.ActualWindow.Start, envelope.ActualWindow.End,
		envelope.ObservedAt, envelope.CapturedAt, envelope.ReferencedAt, envelope.SourceRevision, envelope.SourceWatermark,
		envelope.SourceDigest[:], envelope.ProducerVersion, envelope.CalculationVersion, encoded[3], encoded[4], encoded[5],
		encoded[6], encoded[7], encoded[8], string(envelope.Sensitivity), encoded[9], recovered.PayloadDigest[:],
		int64(envelope.CanonicalSize), recovered.PayloadDigest[:],
	)
	if err != nil {
		return fmt.Errorf("restore evidence snapshot: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var exact bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.evidence_snapshots
				where snapshot_id = $1 and record_id = $2 and kind = $3 and schema_version = $4
				  and source_kind = $5 and source_id = $6
				  and subject_identity_snapshot = $7 and source_identity_snapshot = $8
				  and capture_authorization = $9 and capture_authorization_digest = $10
				  and requested_started_at = $11 and requested_ended_at = $12
				  and actual_started_at = $13 and actual_ended_at = $14
				  and observed_at = $15 and captured_at = $16 and referenced_at = $17
				  and source_revision = $18 and source_watermark = $19 and source_digest = $20
				  and producer_version = $21 and calculation_version = $22
				  and actual_precision = $23 and bucket_width = $24 and unit_semantics = $25
				  and quality = $26 and quota_outcome = $27 and retention = $28
				  and sensitivity_level = $29 and redaction = $30
				  and canonical_hash = $31 and logical_size_bytes = $32 and payload_digest = $33
			)`,
			recovered.SnapshotID, recovered.RecordID, string(envelope.Key.Kind), int64(envelope.Key.SchemaVersion),
			envelope.Source.Type, envelope.Source.ID, encoded[0], encoded[1], encoded[2], envelope.Authorization.Digest[:],
			envelope.RequestedWindow.Start, envelope.RequestedWindow.End, envelope.ActualWindow.Start, envelope.ActualWindow.End,
			envelope.ObservedAt, envelope.CapturedAt, envelope.ReferencedAt, envelope.SourceRevision, envelope.SourceWatermark,
			envelope.SourceDigest[:], envelope.ProducerVersion, envelope.CalculationVersion, encoded[3], encoded[4], encoded[5],
			encoded[6], encoded[7], encoded[8], string(envelope.Sensitivity), encoded[9], recovered.PayloadDigest[:],
			int64(envelope.CanonicalSize), recovered.PayloadDigest[:],
		).Scan(&exact); err != nil || !exact {
			return evidence.ErrInvalidRecoveryInventory
		}
	} else if inserted.RowsAffected() != 1 {
		return evidence.ErrInvalidRecoveryInventory
	}
	return nil
}

func evidenceRecoverySnapshotTimestampsExact(envelope evidence.SnapshotEnvelope) bool {
	values := []time.Time{
		envelope.RequestedWindow.Start,
		envelope.RequestedWindow.End,
		envelope.ActualWindow.Start,
		envelope.ActualWindow.End,
		envelope.ObservedAt,
		envelope.CapturedAt,
		envelope.ReferencedAt,
	}
	for _, value := range values {
		if value.IsZero() || value.Location() != time.UTC || value != value.Round(0) ||
			value.Nanosecond()%int(time.Microsecond) != 0 {
			return false
		}
	}
	return true
}

func restoreEvidenceCaptureIntent(ctx context.Context, tx pgx.Tx, binding evidence.EvidenceRecoveryCaptureIntent) error {
	if binding.Validate() != nil {
		return evidence.ErrInvalidRecoveryInventory
	}
	selectionJSON, err := json.Marshal(binding.Intent.Selection)
	if err != nil {
		return evidence.ErrInvalidRecoveryInventory
	}
	previewJSON, err := json.Marshal(binding.Preview)
	if err != nil {
		return evidence.ErrInvalidRecoveryInventory
	}
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_capture_intents (
			intent_id, record_id, kind, schema_version, preview_digest,
			source_digest, selection, preview, snapshot_id,
			estimated_size_bytes, created_at, valid_until
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict do nothing`,
		binding.Intent.ID, binding.RecordID, string(binding.Intent.Key.Kind), int64(binding.Intent.Key.SchemaVersion),
		binding.Intent.PreviewDigest[:], binding.Preview.SourceDigest[:], selectionJSON, previewJSON,
		binding.SnapshotID, int64(binding.Preview.EstimatedCanonicalBytes), binding.Preview.PreviewedAt, binding.Preview.ValidUntil,
	)
	if err != nil {
		return fmt.Errorf("restore evidence capture intent: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var exact bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.evidence_capture_intents
				where intent_id = $1 and record_id = $2 and kind = $3 and schema_version = $4
				  and preview_digest = $5 and source_digest = $6 and selection = $7 and preview = $8
				  and snapshot_id = $9 and estimated_size_bytes = $10
				  and created_at = $11 and valid_until = $12
			)`,
			binding.Intent.ID, binding.RecordID, string(binding.Intent.Key.Kind), int64(binding.Intent.Key.SchemaVersion),
			binding.Intent.PreviewDigest[:], binding.Preview.SourceDigest[:], selectionJSON, previewJSON,
			binding.SnapshotID, int64(binding.Preview.EstimatedCanonicalBytes), binding.Preview.PreviewedAt, binding.Preview.ValidUntil,
		).Scan(&exact); err != nil || !exact {
			return evidence.ErrInvalidRecoveryInventory
		}
	} else if inserted.RowsAffected() != 1 {
		return evidence.ErrInvalidRecoveryInventory
	}
	return nil
}

func restoreEvidenceRevisionReference(
	ctx context.Context,
	tx pgx.Tx,
	reference evidence.EvidenceRecoveryRevisionReference,
) error {
	inserted, err := tx.Exec(ctx, `
		insert into public.record_revision_evidence (
			record_id, revision_id, ordinal, snapshot_id, caption, reference_role
		) values ($1, $2, $3, $4, $5, $6)
		on conflict do nothing`,
		reference.RecordID, reference.RevisionID, int64(reference.Ordinal), reference.SnapshotID,
		reference.Caption, string(reference.ReferenceRole),
	)
	if err != nil {
		return fmt.Errorf("restore evidence revision reference: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var exact bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.record_revision_evidence
				where record_id = $1 and revision_id = $2 and ordinal = $3 and snapshot_id = $4
				  and caption = $5 and reference_role = $6
			)`, reference.RecordID, reference.RevisionID, int64(reference.Ordinal), reference.SnapshotID,
			reference.Caption, string(reference.ReferenceRole),
		).Scan(&exact); err != nil || !exact {
			return evidence.ErrInvalidRecoveryInventory
		}
	} else if inserted.RowsAffected() != 1 {
		return evidence.ErrInvalidRecoveryInventory
	}
	return nil
}

func restoreEvidenceCopyLineage(ctx context.Context, tx pgx.Tx, lineage evidence.EvidenceRecoveryCopyLineage) error {
	inserted, err := tx.Exec(ctx, `
		insert into public.evidence_copy_lineage (snapshot_id, copied_from_snapshot_id, copy_reason)
		values ($1, $2, $3)
		on conflict do nothing`, lineage.SnapshotID, lineage.CopiedFromSnapshotID, lineage.CopyReason)
	if err != nil {
		return fmt.Errorf("restore evidence copy lineage: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		var exact bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.evidence_copy_lineage
				where snapshot_id = $1 and copied_from_snapshot_id = $2 and copy_reason = $3
			)`, lineage.SnapshotID, lineage.CopiedFromSnapshotID, lineage.CopyReason,
		).Scan(&exact); err != nil || !exact {
			return evidence.ErrInvalidRecoveryInventory
		}
	} else if inserted.RowsAffected() != 1 {
		return evidence.ErrInvalidRecoveryInventory
	}
	return nil
}

// CollectUnreferencedEvidencePayloads is the recovery-specific global sweep.
// Unlike the background orphan janitor it has no age grace: replay has already
// rebuilt every logical reference, so every remaining unreferenced payload is
// conclusively unreachable.
func (repository *PostgresEvidenceRepository) CollectUnreferencedEvidencePayloads(ctx context.Context) error {
	if ctx == nil || repository == nil {
		return evidence.ErrInvalidRecoveryAdapter
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		delete from public.evidence_payloads as payload
		where not exists (
			select 1 from public.evidence_snapshots as snapshot
			where snapshot.payload_digest = payload.payload_digest
		)
		returning payload.payload_digest, payload.payload_encoding,
		          payload.canonical_size_bytes, payload.compressed_size_bytes,
		          payload.created_at, transaction_timestamp()`)
	if err != nil {
		return fmt.Errorf("collect globally unreferenced evidence payloads: %w", err)
	}
	type deletedPayload struct {
		digest                        [sha256.Size]byte
		encoding                      string
		canonicalSize, compressedSize uint64
		createdAt, deletedAt          time.Time
	}
	deleted := make([]deletedPayload, 0)
	for rows.Next() {
		var rawDigest []byte
		var canonicalSize, compressedSize int64
		var item deletedPayload
		if err := rows.Scan(&rawDigest, &item.encoding, &canonicalSize, &compressedSize, &item.createdAt, &item.deletedAt); err != nil ||
			len(rawDigest) != sha256.Size || item.encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 ||
			canonicalSize < 1 || compressedSize < 1 {
			rows.Close()
			return ErrEvidencePersistenceConflict
		}
		copy(item.digest[:], rawDigest)
		item.canonicalSize, item.compressedSize = uint64(canonicalSize), uint64(compressedSize)
		item.createdAt, item.deletedAt = item.createdAt.UTC(), item.deletedAt.UTC()
		deleted = append(deleted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate globally unreferenced evidence payloads: %w", err)
	}
	rows.Close()
	for _, item := range deleted {
		versionDigest := evidencePayloadVersionDigest(item.digest, item.encoding, item.canonicalSize, item.compressedSize, item.createdAt)
		receiptDigest := evidencePayloadGCReceiptDigest(versionDigest, item.deletedAt)
		inserted, err := tx.Exec(ctx, `
			insert into public.evidence_payload_gc_receipts (
				payload_version_digest, receipt_digest, deleted_at
			) values ($1, $2, $3)`, versionDigest[:], receiptDigest[:], item.deletedAt)
		if err != nil {
			return fmt.Errorf("record recovery evidence payload GC: %w", err)
		}
		if inserted.RowsAffected() != 1 {
			return ErrEvidencePersistenceConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recovery evidence payload GC: %w", err)
	}
	return nil
}
