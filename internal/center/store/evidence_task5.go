package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

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
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, digestEvidenceDeletionStrings(evidenceDeletionHealthDigestDomainV1, names...))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("commit evidence deletion health: %w", err)
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
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var dependencyMaterial, impactMaterial []byte
	var survivingCopies int64
	err = tx.QueryRow(ctx, `
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
		preview.SurvivingCopies = append(preview.SurvivingCopies, recorddeletion.AdapterSurvivingCopy{
			Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: uint64(survivingCopies),
		})
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("commit evidence deletion preview: %w", err)
	}
	return preview, nil
}

func (repository *PostgresEvidenceRepository) PurgeRecordEvidence(
	ctx context.Context,
	command evidence.EvidenceDeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || command.Validate() != nil || command.Operation.LedgerSequence > math.MaxInt64 ||
		uint64(command.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, evidence.ErrInvalidDeletionAdapter
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateLockedEvidencePurgeOperation(ctx, tx, command.Operation); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if existing, found, err := loadEvidencePurgeReceipt(ctx, tx, command); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("commit record evidence purge replay read: %w", err)
		}
		if err := repository.VerifyRecordEvidencePurge(ctx, command, existing); err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, err
		}
		return existing, nil
	}
	rows, err := tx.Query(ctx, `
		select snapshot_id, payload_digest
		from public.evidence_snapshots
		where record_id = $1
		order by snapshot_id`, command.Operation.Object.ObjectID)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("lock record evidence snapshots: %w", err)
	}
	snapshotIDs := make([]string, 0)
	payloadDigests := make([][]byte, 0)
	for rows.Next() {
		var snapshotID string
		var digest []byte
		if err := rows.Scan(&snapshotID, &digest); err != nil || !evidence.ValidSnapshotID(snapshotID) || len(digest) != sha256.Size {
			rows.Close()
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		snapshotIDs = append(snapshotIDs, snapshotID)
		payloadDigests = append(payloadDigests, append([]byte(nil), digest...))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("iterate record evidence snapshots: %w", err)
	}
	rows.Close()
	sort.Strings(snapshotIDs)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`delete from public.record_revision_evidence where record_id = $1`, []any{command.Operation.Object.ObjectID}},
		{`delete from public.evidence_capture_intents where record_id = $1`, []any{command.Operation.Object.ObjectID}},
		{`delete from public.evidence_copy_lineage where snapshot_id = any($1::text[])`, []any{snapshotIDs}},
		{`delete from public.evidence_snapshots where record_id = $1`, []any{command.Operation.Object.ObjectID}},
		{`delete from public.evidence_payloads as payload
		  where payload.payload_digest = any($1::bytea[])
		    and not exists (select 1 from public.evidence_snapshots as snapshot where snapshot.payload_digest = payload.payload_digest)`, []any{payloadDigests}},
	} {
		result, err := tx.Exec(ctx, statement.sql, statement.args...)
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("purge record evidence: %w", err)
		}
		if result.RowsAffected() < 0 {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	// The immutable 0054 receipt schema intentionally contains no row-count
	// column. Keep the adapter receipt deterministic and replayable by using a
	// content-free zero count rather than returning a first-attempt-only count
	// that could not be reconstructed after an ambiguous commit.
	const receiptRemovedRows = uint64(0)
	var completedAt time.Time
	receiptDigest := evidenceDeletionReceiptDigest(command, receiptRemovedRows)
	err = tx.QueryRow(ctx, `
		insert into public.evidence_purge_receipts (
			operation_id, surface_kind, receipt_digest, completed_at
		) values ($1, 'record_evidence', $2, transaction_timestamp())
		returning completed_at`,
		command.Operation.OperationID, receiptDigest[:],
	).Scan(&completedAt)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("insert evidence purge receipt: %w", err)
	}
	if completedAt.IsZero() {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	completedAt = completedAt.UTC()
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("commit record evidence purge: %w", err)
	}
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordEvidence, OperationID: command.Operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: receiptDigest,
		RemovedRowCount: receiptRemovedRows, VerifiedAbsentAt: completedAt,
	}
	if err := repository.VerifyRecordEvidencePurge(ctx, command, receipt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func loadEvidencePurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command evidence.EvidenceDeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, bool, error) {
	var rawDigest []byte
	var completedAt time.Time
	err := tx.QueryRow(ctx, `
		select receipt_digest, completed_at
		from public.evidence_purge_receipts
		where operation_id = $1 and surface_kind = 'record_evidence'`,
		command.Operation.OperationID,
	).Scan(&rawDigest, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPurgeReceipt{}, false, nil
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, false, fmt.Errorf("load evidence purge receipt: %w", err)
	}
	completedAt = completedAt.UTC()
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordEvidence, OperationID: command.Operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: evidenceDeletionReceiptDigest(command, 0),
		RemovedRowCount: 0, VerifiedAbsentAt: completedAt,
	}
	if len(rawDigest) != sha256.Size || completedAt.IsZero() || !equalEvidenceDigest(rawDigest, receipt.ReceiptDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return receipt, true, nil
}

func (repository *PostgresEvidenceRepository) VerifyRecordEvidencePurge(
	ctx context.Context,
	command evidence.EvidenceDeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || command.Validate() != nil || receipt.AdapterName != recorddeletion.AdapterNameRecordEvidence ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() || receipt.RemovedRowCount != 0 ||
		receipt.ReceiptDigest != evidenceDeletionReceiptDigest(command, receipt.RemovedRowCount) {
		return evidence.ErrInvalidDeletionAdapter
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateLockedEvidencePurgeOperation(ctx, tx, command.Operation); err != nil {
		return err
	}
	var persistedDigest []byte
	var persistedAt time.Time
	if err := tx.QueryRow(ctx, `
		select receipt_digest, completed_at
		from public.evidence_purge_receipts
		where operation_id = $1 and surface_kind = 'record_evidence'`,
		command.Operation.OperationID,
	).Scan(&persistedDigest, &persistedAt); err != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if len(persistedDigest) != sha256.Size || !persistedAt.Equal(receipt.VerifiedAbsentAt) ||
		!equalEvidenceDigest(persistedDigest, receipt.ReceiptDigest) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	var refs, intents, snapshots, ownedLineage int64
	if err := tx.QueryRow(ctx, `
		select
		  (select count(*) from public.record_revision_evidence where record_id = $1),
		  (select count(*) from public.evidence_capture_intents where record_id = $1),
		  (select count(*) from public.evidence_snapshots where record_id = $1),
		  (select count(*) from public.evidence_copy_lineage as lineage
		     join public.evidence_snapshots as snapshot on snapshot.snapshot_id = lineage.snapshot_id
		    where snapshot.record_id = $1)`, command.Operation.Object.ObjectID,
	).Scan(&refs, &intents, &snapshots, &ownedLineage); err != nil {
		return fmt.Errorf("verify record evidence purge: %w", err)
	}
	if refs != 0 || intents != 0 || snapshots != 0 || ownedLineage != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record evidence purge verification: %w", err)
	}
	return nil
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
