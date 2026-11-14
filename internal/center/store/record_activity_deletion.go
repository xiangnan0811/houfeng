package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recorddeletion"
)

const (
	activityDeletionHealthDigestDomainV1  = "houfeng.record-activity.deletion-health.v1"
	activityDeletionPreviewDigestDomainV1 = "houfeng.record-activity.deletion-preview.v1"
	activityDeletionImpactDigestDomainV1  = "houfeng.record-activity.deletion-impact.v1"
	activityDeletionReceiptDigestDomainV1 = "houfeng.record-activity.deletion-receipt.v1"
)

func (repository *PostgresRecordDeletionRepository) ActivityDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, activity.ErrInvalidDeletionAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		health, err = activityDeletionHealthInTransaction(ctx, transaction.tx)
		return err
	})
	return health, err
}

func activityDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, activity.ErrInvalidDeletionAdapter
	}
	surfaces := recorddeletion.RecordActivitySurfaceNames()
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
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read record activity deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.record_activity_purge_v2(bytea)", "public.record_activity_purge_verify_v2(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(
		true, 1, digestRecordPurgeStrings(activityDeletionHealthDigestDomainV1, names...),
	)
	if err != nil {
		return health, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

func (repository *PostgresRecordDeletionRepository) PreviewActivityDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, activity.ErrInvalidDeletionAdapter
	}
	var snapshot recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		snapshot, err = previewActivityDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	return snapshot, err
}

func previewActivityDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, activity.ErrInvalidDeletionAdapter
	}
	var dependencyMaterial, impactMaterial []byte
	// Preview material is digests and counts only. Presentation JSON, subject
	// identity snapshots, and revision numbers stay out of the hash so a
	// preview log cannot reconstruct deleted timeline content.
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
			), material as (
				select jsonb_build_object(
				  'projection', coalesce((select jsonb_agg(jsonb_build_array(
				    activity_id, projection_generation, ingest_sequence, source_kind,
				    source_event_id, source_version, event_kind, encode(canonical_hash, 'hex')
				  ) order by ingest_sequence) from public.record_activity_projection
				  where record_id = root.record_id), '[]'::jsonb),
				  'subjects', coalesce((select jsonb_agg(jsonb_build_array(
				    activity_id, subject_kind, subject_source_id, relation_role, is_primary
				  ) order by activity_id, relation_order)
				  from public.record_activity_subjects where record_id = root.record_id), '[]'::jsonb),
				  'intervals', coalesce((select jsonb_agg(jsonb_build_array(
				    projection_generation, revision_id, revision_no,
				    valid_from_ingest_sequence, valid_to_ingest_sequence
				  ) order by projection_generation, valid_from_ingest_sequence)
				  from public.record_activity_revision_intervals where record_id = root.record_id), '[]'::jsonb)
				) as dependency_material,
				jsonb_build_object(
				  'projection', (select count(*) from public.record_activity_projection where record_id = root.record_id),
				  'subjects', (select count(*) from public.record_activity_subjects where record_id = root.record_id),
				  'intervals', (select count(*) from public.record_activity_revision_intervals where record_id = root.record_id)
				) as impact_material
				from root
			)
			select pg_catalog.convert_to(dependency_material::text, 'UTF8'),
			       pg_catalog.convert_to(impact_material::text, 'UTF8')
			from material`,
		target.Object.ProjectID, target.Object.ObjectID, target.CurrentRevisionID,
		int64(target.LockVersion), int64(target.AuthorizationEpoch), int64(target.ContentDeliveryEpoch),
	).Scan(&dependencyMaterial, &impactMaterial)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview record activity deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	snapshot := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(
			activityDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
		),
		ImpactDigest:    digestRecordPurgeBytes(activityDeletionImpactDigestDomainV1, impactMaterial),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return snapshot, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordActivity(
	ctx context.Context,
	command activity.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, activity.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_activity_purge_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_activity_purge_v2($1)`
		}
		receipt, err = runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordActivityProjection, command.PurgeTarget, command.SurfaceDigest)
		if err == nil && receipt.ReceiptDigest != activityDeletionReceiptDigest(command, receipt.RemovedRowCount) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordActivityPurge(
	ctx context.Context,
	command activity.DeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordActivityProjection ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != activityDeletionReceiptDigest(command, receipt.RemovedRowCount) {
		return activity.ErrInvalidDeletionAdapter
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_activity_purge_verify_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_activity_purge_verify_v2($1)`
		}
		actual, err := runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordActivityProjection, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}

// deletionAdapterEnvelope preserves the V1 binding while V2 carries only the exact compound owner.
func deletionAdapterEnvelope(ctx context.Context, tx pgx.Tx, target recorddeletion.PurgeTarget, surface [sha256.Size]byte) ([]byte, error) {
	if target.Operation.ContractVersion == recorddeletion.DeletionContractV1 {
		return legacyAdapterPurgeEnvelope(ctx, tx, target, surface)
	}
	owner := target.OwnerClaim.Owner
	if target.Operation.ContractVersion != recorddeletion.DeletionContractV2 || target.Validate() != nil ||
		uint64(target.Operation.FenceEpoch) > math.MaxInt64 || owner.Generation > math.MaxInt64 || owner.ExpiresAt.Nanosecond()%1000 != 0 {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	body, err := encodeRecordPurgeCommand(clientComparisonPurgeEnvelope{
		OperationID: target.Operation.OperationID, RecordID: target.Operation.Object.ObjectID,
		ReservationID: target.Operation.ReservationID, FenceEpoch: uint64(target.Operation.FenceEpoch),
		OwnerID: owner.OwnerID, OwnerGeneration: owner.Generation,
		OwnerExpiresAt: owner.ExpiresAt.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(surface[:]),
	})
	if err != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return body, nil
}

// legacyAdapterPurgeEnvelope combines the caller's observed owner with an
// independently installed ledger binding. Looking up the binding never adopts
// an owner from the database; each restricted command rechecks both identities.
func legacyAdapterPurgeEnvelope(ctx context.Context, tx pgx.Tx, target recorddeletion.PurgeTarget, surface [sha256.Size]byte) ([]byte, error) {
	operation, owner := target.Operation, target.OwnerClaim.Owner
	if target.Validate() != nil || operation.ContractVersion != recorddeletion.DeletionContractV1 ||
		operation.LedgerSequence > math.MaxInt64 || uint64(operation.FenceEpoch) > math.MaxInt64 ||
		owner.ExpiresAt.Nanosecond()%1000 != 0 {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	bindingRequest, err := encodeRecordPurgeCommand(struct {
		OperationID     string `json:"operation_id"`
		ProjectID       string `json:"project_id"`
		LedgerSequence  uint64 `json:"ledger_sequence"`
		LedgerEntryHash string `json:"ledger_entry_hash"`
	}{operation.OperationID, operation.Object.ProjectID, operation.LedgerSequence, hex.EncodeToString(operation.LedgerEntryHash[:])})
	if err != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	rows, err := tx.Query(ctx, `select ledger_domain, witness_proof_digest from public.record_deletion_verified_binding($1)`, bindingRequest)
	if err != nil {
		return nil, fmt.Errorf("read verified record deletion authority: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read verified record deletion authority: %w", err)
		}
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var domain, proof []byte
	if err := rows.Scan(&domain, &proof); err != nil {
		return nil, fmt.Errorf("read verified record deletion binding: %w", err)
	}
	if len(domain) != sha256.Size || len(proof) != sha256.Size {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var domainDigest, proofDigest [sha256.Size]byte
	copy(domainDigest[:], domain)
	copy(proofDigest[:], proof)
	if rows.Next() || rows.Err() != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if domainDigest == ([sha256.Size]byte{}) || proofDigest == ([sha256.Size]byte{}) {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	rows.Close()
	body, err := encodeRecordPurgeCommand(corePurgeV1Envelope{
		clientComparisonPurgeEnvelope: clientComparisonPurgeEnvelope{
			OperationID: operation.OperationID, RecordID: operation.Object.ObjectID,
			ReservationID: operation.ReservationID, FenceEpoch: uint64(operation.FenceEpoch),
			OwnerID: owner.OwnerID, OwnerGeneration: owner.Generation,
			OwnerExpiresAt: owner.ExpiresAt.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(surface[:]),
		},
		LedgerDomain: hex.EncodeToString(domainDigest[:]), LedgerSequence: operation.LedgerSequence,
		LedgerEntryHash: hex.EncodeToString(operation.LedgerEntryHash[:]), WitnessProofDigest: hex.EncodeToString(proofDigest[:]),
	})
	if err != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return body, nil
}

func runLegacyAdapterPurge(ctx context.Context, tx pgx.Tx, query string, body []byte, name recorddeletion.AdapterName,
	target recorddeletion.PurgeTarget, surface [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error) {
	var storedSurface, storedDigest []byte
	var removed int64
	var verifiedAt time.Time
	if err := tx.QueryRow(ctx, query, body).Scan(&storedSurface, &storedDigest, &removed, &verifiedAt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("run restricted %s V1 purge: %w", name, err)
	}
	if removed < 0 || verifiedAt.IsZero() || !equalRecordPurgeDigest(storedSurface, surface) || len(storedDigest) != sha256.Size {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var digest [sha256.Size]byte
	copy(digest[:], storedDigest)
	if digest == ([sha256.Size]byte{}) {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return recorddeletion.AdapterPurgeReceipt{AdapterName: name, OperationID: target.Operation.OperationID,
		SurfaceDigest: surface, ReceiptDigest: digest, RemovedRowCount: uint64(removed), VerifiedAbsentAt: verifiedAt.UTC()}, nil
}

func activityDeletionReceiptDigest(
	command activity.DeletionCommand,
	removed uint64,
) [sha256.Size]byte {
	return digestRecordPurgeBytes(
		activityDeletionReceiptDigestDomainV1,
		[]byte(command.Operation.OperationID),
		[]byte(command.Operation.Object.ProjectID),
		[]byte(command.Operation.Object.ObjectID),
		command.SurfaceDigest[:],
		recordPurgeUint64(removed),
	)
}

var _ activity.DeletionStore = (*PostgresRecordDeletionRepository)(nil)
