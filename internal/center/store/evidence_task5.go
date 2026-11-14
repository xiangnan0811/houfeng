package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recorddeletion"
)

const (
	evidenceDeletionHealthDigestDomainV1  = "houfeng.evidence.deletion-health.v1"
	evidenceDeletionPreviewDigestDomainV1 = "houfeng.evidence.deletion-preview.v1"
	evidenceDeletionImpactDigestDomainV1  = "houfeng.evidence.deletion-impact.v1"
	evidenceDeletionReceiptDigestDomainV1 = "houfeng.evidence.deletion-receipt.v1"
)

var _ evidence.EvidenceDeletionStore = (*PostgresEvidenceRepository)(nil)

func (repository *PostgresEvidenceRepository) EvidenceDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, evidence.ErrInvalidDeletionAdapter
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	health, err := evidenceDeletionHealthInTransaction(ctx, tx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("commit evidence deletion health: %w", err)
	}
	return health, nil
}

func evidenceDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, evidence.ErrInvalidDeletionAdapter
	}
	surfaces := recorddeletion.RecordEvidenceSurfaceNames()
	names := make([]string, len(surfaces))
	for index, surface := range surfaces {
		names[index] = string(surface)
	}
	var count int64
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint
		from pg_catalog.pg_class as relation
		join pg_catalog.pg_namespace as namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = 'public'
		  and relation.relkind in ('r', 'p')
		  and relation.relname = any($1::text[])`, names).Scan(&count); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read evidence deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.record_evidence_purge_v2(bytea)", "public.record_evidence_purge_verify_v2(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.record_evidence_purge_prepare_v2(bytea)", "public.record_evidence_purge_payload_v2(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, digestEvidenceDeletionStrings(evidenceDeletionHealthDigestDomainV1, names...))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

func (repository *PostgresEvidenceRepository) PreviewEvidenceDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, evidence.ErrInvalidDeletionAdapter
	}
	if repository == nil || repository.platform == nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var preview recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		preview, err = previewEvidenceDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	return preview, err
}

func previewEvidenceDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, evidence.ErrInvalidDeletionAdapter
	}
	if err := verifyRecordEvidencePayloadsInTransaction(ctx, tx, target.Object.ObjectID); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	var dependencyMaterial, impactMaterial []byte
	var survivingCopies int64
	err := tx.QueryRow(ctx, `
		with root as materialized (
			select record.record_id
			from public.records as record
			join public.content_delivery_epochs as epoch
			  on epoch.project_id = record.project_id
			 and epoch.object_kind = 'record'
			 and epoch.object_id = record.record_id
			where record.project_id = $1
			  and record.record_id = $2
			  and record.current_revision_id = $3
			  and record.lock_version = $4
			  and record.authorization_epoch = $5
			  and epoch.delivery_epoch = $6
		), target_snapshots as materialized (
			select snapshot.snapshot_id, snapshot.payload_digest, snapshot.kind, snapshot.schema_version
			from public.evidence_snapshots as snapshot
			join root on root.record_id = snapshot.record_id
		), dependency as (
			select coalesce(jsonb_agg(jsonb_build_array(
				snapshot.snapshot_id, encode(snapshot.payload_digest, 'hex'), snapshot.kind, snapshot.schema_version
			) order by snapshot.snapshot_id), '[]'::jsonb) as material,
			count(*)::bigint as snapshot_count
			from target_snapshots as snapshot
		), refs as (
			select coalesce(jsonb_agg(jsonb_build_array(
				reference.revision_id, reference.ordinal, reference.snapshot_id
			) order by reference.revision_id, reference.ordinal), '[]'::jsonb) as material,
			count(*)::bigint as ref_count
			from public.record_revision_evidence as reference
			join root on root.record_id = reference.record_id
		), intents as (
			select coalesce(jsonb_agg(intent.intent_id order by intent.intent_id), '[]'::jsonb) as material,
			count(*)::bigint as intent_count
			from public.evidence_capture_intents as intent
			join root on root.record_id = intent.record_id
		), surviving as (
			select count(distinct other.snapshot_id)::bigint as copy_count
			from public.evidence_snapshots as other
			join target_snapshots as target on target.payload_digest = other.payload_digest
			join root on true
			where other.record_id <> root.record_id
		)
		select convert_to(jsonb_build_object(
			'snapshots', dependency.material, 'revision_refs', refs.material, 'intents', intents.material
		)::text, 'UTF8'),
		convert_to(jsonb_build_object(
			'snapshot_count', dependency.snapshot_count, 'revision_ref_count', refs.ref_count,
			'intent_count', intents.intent_count, 'surviving_other_record_copy_count', surviving.copy_count
		)::text, 'UTF8'), surviving.copy_count
		from root cross join dependency cross join refs cross join intents cross join surviving`,
		target.Object.ProjectID,
		target.Object.ObjectID,
		target.CurrentRevisionID,
		int64(target.LockVersion),
		int64(target.AuthorizationEpoch),
		int64(target.ContentDeliveryEpoch),
	).Scan(&dependencyMaterial, &impactMaterial, &survivingCopies)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview evidence deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 || survivingCopies < 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestEvidenceDeletionBytes(evidenceDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial),
		ImpactDigest:     digestEvidenceDeletionBytes(evidenceDeletionImpactDigestDomainV1, impactMaterial),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	if survivingCopies > 0 {
		preview.SurvivingCopies = append(preview.SurvivingCopies, recorddeletion.KnownAdapterSurvivingCopy(
			recorddeletion.SurvivingCopyKindOtherRecord, uint64(survivingCopies),
		))
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return preview, nil
}

func (repository *PostgresEvidenceRepository) PurgeRecordEvidence(
	ctx context.Context,
	command evidence.EvidenceDeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, evidence.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		// The prepare command authenticates the caller and holds the common and
		// evidence locks until this transaction ends. PostgreSQL cannot decode
		// this gzip format: the trusted worker validates the actual persisted
		// bytes here, then the restricted purge rechecks authority and performs
		// the effects, absence checks and receipt under the same locks.
		if err := verifyPreparedEvidenceDeletionPayloads(ctx, transaction.tx, body, command.Operation.ContractVersion); err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_evidence_purge_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_evidence_purge_v2($1)`
		}
		receipt, err = runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordEvidence, command.PurgeTarget, command.SurfaceDigest)
		if err == nil && (receipt.RemovedRowCount != 0 || receipt.ReceiptDigest != evidenceDeletionReceiptDigest(command, 0)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

// The prepare result contains metadata only. Fetch and validate each authorized
// payload as a scalar so PostgreSQL does not materialize the complete evidence
// inventory (including content) in a set-returning function's temporary store.
func verifyPreparedEvidenceDeletionPayloads(ctx context.Context, tx pgx.Tx, authority []byte, version recorddeletion.DeletionContractVersion) error {
	prepareQuery := `select * from public.record_evidence_purge_prepare_v1($1)`
	payloadQuery := `select public.record_evidence_purge_payload_v1($1)`
	if version == recorddeletion.DeletionContractV2 {
		prepareQuery = `select * from public.record_evidence_purge_prepare_v2($1)`
		payloadQuery = `select public.record_evidence_purge_payload_v2($1)`
	}
	rows, err := tx.Query(ctx, prepareQuery, authority)
	if err != nil {
		return fmt.Errorf("prepare restricted evidence purge: %w", err)
	}
	defer rows.Close()
	type payloadMetadata struct {
		snapshotID     string
		digest         [sha256.Size]byte
		canonicalSize  int64
		compressedSize int64
	}
	var payloads []payloadMetadata
	for rows.Next() {
		var snapshotID string
		var rawDigest, rawHash []byte
		var logicalSize int64
		var encoding *string
		var canonicalSize, compressedSize *int64
		if err := rows.Scan(&snapshotID, &rawDigest, &rawHash, &logicalSize, &encoding, &canonicalSize, &compressedSize); err != nil ||
			!evidence.ValidSnapshotID(snapshotID) || len(rawDigest) != sha256.Size || !bytes.Equal(rawHash, rawDigest) ||
			encoding == nil || *encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 || canonicalSize == nil ||
			compressedSize == nil || *canonicalSize != logicalSize {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		item := payloadMetadata{snapshotID: snapshotID, canonicalSize: *canonicalSize, compressedSize: *compressedSize}
		copy(item.digest[:], rawDigest)
		payloads = append(payloads, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate restricted evidence metadata: %w", err)
	}
	rows.Close()
	for _, item := range payloads {
		body, err := encodeRecordPurgeCommand(struct {
			Authority  []byte `json:"authority"`
			SnapshotID string `json:"snapshot_id"`
		}{authority, item.snapshotID})
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var compressed []byte
		if err := tx.QueryRow(ctx, payloadQuery, body).Scan(&compressed); err != nil {
			return fmt.Errorf("read restricted evidence payload: %w", err)
		}
		if _, err := decodeEvidencePayloadBytes(compressed, item.canonicalSize, item.compressedSize, item.digest); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	return nil
}

// A digest in a snapshot row is not proof that its compressed payload contains
// that canonical evidence. Refuse preview and purge receipts if any owned
// payload is missing, malformed, or decodes to a different digest.
func verifyRecordEvidencePayloadsInTransaction(ctx context.Context, tx pgx.Tx, recordID string) error {
	rows, err := tx.Query(ctx, `select snapshot.payload_digest,snapshot.canonical_hash,snapshot.logical_size_bytes,
		payload.payload_encoding,payload.canonical_size_bytes,payload.compressed_size_bytes,payload.compressed_payload
		from public.evidence_snapshots snapshot
		left join public.evidence_payloads payload on payload.payload_digest=snapshot.payload_digest
		where snapshot.record_id=$1 order by snapshot.snapshot_id`, recordID)
	if err != nil {
		return fmt.Errorf("read evidence deletion payloads: %w", err)
	}
	return verifyEvidenceDeletionPayloadRows(rows)
}

func verifyEvidenceDeletionPayloadRows(rows pgx.Rows) error {
	defer rows.Close()
	for rows.Next() {
		var rawDigest, rawHash, compressed []byte
		var logicalSize int64
		var encoding *string
		var canonicalSize, compressedSize *int64
		if err := rows.Scan(&rawDigest, &rawHash, &logicalSize, &encoding, &canonicalSize, &compressedSize, &compressed); err != nil ||
			len(rawDigest) != sha256.Size || !bytes.Equal(rawHash, rawDigest) || encoding == nil ||
			*encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 || canonicalSize == nil || compressedSize == nil ||
			*canonicalSize != logicalSize {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var digest [sha256.Size]byte
		copy(digest[:], rawDigest)
		if _, err := decodeEvidencePayloadBytes(compressed, *canonicalSize, *compressedSize, digest); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate evidence deletion payloads: %w", err)
	}
	return nil
}

func (repository *PostgresEvidenceRepository) VerifyRecordEvidencePurge(
	ctx context.Context,
	command evidence.EvidenceDeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordEvidence ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() || receipt.RemovedRowCount != 0 ||
		receipt.ReceiptDigest != evidenceDeletionReceiptDigest(command, receipt.RemovedRowCount) {
		return evidence.ErrInvalidDeletionAdapter
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_evidence_purge_verify_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_evidence_purge_verify_v2($1)`
		}
		actual, err := runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordEvidence, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}

func validateLockedEvidencePurgeOperation(
	ctx context.Context,
	tx pgx.Tx,
	operation recorddeletion.DeletionOperation,
) error {
	var state, reason, objectID, projectID string
	var sequence, fenceEpoch int64
	var entryHash []byte
	err := tx.QueryRow(ctx, `
		select operation.operation_state, operation.reason_code,
		       operation.ledger_sequence, operation.ledger_entry_hash,
		       reservation.object_id, reservation.project_id, reservation.fence_epoch
		from public.record_purge_operations as operation
		join public.deletion_reservations as reservation on reservation.reservation_id = operation.reservation_id
		where operation.operation_id = $1
		  and operation.reservation_id = $2
		  and operation.project_id = $3
		  and reservation.project_id = $3
		  and reservation.object_kind = 'record'
		  and reservation.object_id = $4
		for update of operation, reservation`,
		operation.OperationID, operation.ReservationID, operation.Object.ProjectID, operation.Object.ObjectID,
	).Scan(&state, &reason, &sequence, &entryHash, &objectID, &projectID, &fenceEpoch)
	if err != nil || state != string(recorddeletion.DeletionStateOnlinePurging) ||
		reason != string(operation.ReasonCode) || sequence != int64(operation.LedgerSequence) ||
		len(entryHash) != sha256.Size || objectID != operation.Object.ObjectID || projectID != operation.Object.ProjectID ||
		fenceEpoch != int64(operation.FenceEpoch) || !equalEvidenceDigest(entryHash, operation.LedgerEntryHash) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func evidenceDeletionReceiptDigest(
	command evidence.EvidenceDeletionCommand,
	removed uint64,
) [sha256.Size]byte {
	fields := [][]byte{
		[]byte(command.Operation.OperationID), []byte(command.Operation.Object.ProjectID),
		[]byte(command.Operation.Object.ObjectID), command.SurfaceDigest[:],
	}
	hasher := sha256.New()
	writeEvidenceTask5DigestField(hasher, []byte(evidenceDeletionReceiptDigestDomainV1))
	for _, field := range fields {
		writeEvidenceTask5DigestField(hasher, field)
	}
	writeEvidenceTask5DigestUint64(hasher, removed)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func digestEvidenceDeletionStrings(domain string, values ...string) [sha256.Size]byte {
	encoded := make([][]byte, len(values))
	for index, value := range values {
		encoded[index] = []byte(value)
	}
	return digestEvidenceDeletionBytes(domain, encoded...)
}

func digestEvidenceDeletionBytes(domain string, values ...[]byte) [sha256.Size]byte {
	hasher := sha256.New()
	writeEvidenceTask5DigestField(hasher, []byte(domain))
	for _, value := range values {
		writeEvidenceTask5DigestField(hasher, value)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeEvidenceTask5DigestField(hasher interface{ Write([]byte) (int, error) }, value []byte) {
	writeEvidenceTask5DigestUint64(hasher, uint64(len(value)))
	_, _ = hasher.Write(value)
}

func writeEvidenceTask5DigestUint64(hasher interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = hasher.Write(encoded[:])
}

func equalEvidenceDigest(raw []byte, digest [sha256.Size]byte) bool {
	if len(raw) != sha256.Size {
		return false
	}
	for index := range raw {
		if raw[index] != digest[index] {
			return false
		}
	}
	return true
}
