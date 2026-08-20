package store

import (
	"context"
	"crypto/sha256"
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
		surfaces := recorddeletion.RecordActivitySurfaceNames()
		names := make([]string, len(surfaces))
		for index, surface := range surfaces {
			names[index] = string(surface)
		}
		var count int64
		if err := transaction.tx.QueryRow(ctx, `
			select count(*)::bigint
			from pg_catalog.pg_class as relation
			join pg_catalog.pg_namespace as namespace on namespace.oid = relation.relnamespace
			where namespace.nspname = 'public'
			  and relation.relkind in ('r', 'p')
			  and relation.relname = any($1::text[])`, names).Scan(&count); err != nil {
			return fmt.Errorf("read record activity deletion health: %w", err)
		}
		if count != int64(len(names)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var err error
		health, err = recorddeletion.NewAdapterHealthSnapshot(
			true, 1, digestRecordPurgeStrings(activityDeletionHealthDigestDomainV1, names...),
		)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return health, err
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
		var dependencyMaterial, impactMaterial []byte
		// Preview material is digests and counts only. Presentation JSON, subject
		// identity snapshots, and revision numbers stay out of the hash so a
		// preview log cannot reconstruct deleted timeline content.
		err := transaction.tx.QueryRow(ctx, `
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
			return recorddeletion.ErrDeletionPreviewStale
		}
		if err != nil {
			return fmt.Errorf("preview record activity deletion: %w", err)
		}
		if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		snapshot = recorddeletion.AdapterPreviewSnapshot{
			DependencyDigest: digestRecordPurgeBytes(
				activityDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
			),
			ImpactDigest:    digestRecordPurgeBytes(activityDeletionImpactDigestDomainV1, impactMaterial),
			SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
		}
		if err := snapshot.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return snapshot, err
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordActivity(
	ctx context.Context,
	command activity.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		command.Operation.LedgerSequence > math.MaxInt64 || uint64(command.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, activity.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, command.Operation); err != nil {
			return err
		}
		existing, found, err := loadActivityPurgeReceipt(ctx, transaction.tx, command)
		if err != nil {
			return err
		}
		if found {
			if err := assertRecordActivitySurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
				return err
			}
			receipt = existing
			return nil
		}
		var removed int64
		encoded, err := encodeRecordPurgeCommand(recordPurgeFunctionCommand{
			OperationID: command.Operation.OperationID, ReservationID: command.Operation.ReservationID,
			ProjectID: command.Operation.Object.ProjectID, RecordID: command.Operation.Object.ObjectID,
			FenceEpoch: int64(command.Operation.FenceEpoch), LedgerSequence: int64(command.Operation.LedgerSequence),
			LedgerEntryHash: encodeRecordPurgeLedgerHash(command.Operation.LedgerEntryHash),
		})
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := transaction.tx.QueryRow(ctx, `
			select public.record_activity_purge($1)`, encoded,
		).Scan(&removed); err != nil {
			return fmt.Errorf("purge record activity through constrained function: %w", err)
		}
		if removed < 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := assertRecordActivitySurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
			return err
		}
		removedRows := uint64(removed)
		receiptDigest := activityDeletionReceiptDigest(command, removedRows)
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			insert into public.record_activity_purge_receipts (
				operation_id, removed_surface_digest, receipt_digest,
				removed_row_count, verified_absent_at
			) values ($1, $2, $3, $4, transaction_timestamp())
			returning verified_absent_at`, command.Operation.OperationID, command.SurfaceDigest[:],
			receiptDigest[:], removed,
		).Scan(&verifiedAt); err != nil {
			return fmt.Errorf("insert record activity purge receipt: %w", err)
		}
		if verifiedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		receipt = recorddeletion.AdapterPurgeReceipt{
			AdapterName: recorddeletion.AdapterNameRecordActivityProjection, OperationID: command.Operation.OperationID,
			SurfaceDigest: command.SurfaceDigest, ReceiptDigest: receiptDigest,
			RemovedRowCount: removedRows, VerifiedAbsentAt: verifiedAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.VerifyRecordActivityPurge(ctx, command, receipt); err != nil {
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
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, command.Operation); err != nil {
			return err
		}
		var rawSurface, rawReceipt []byte
		var removed int64
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_activity_purge_receipts
			where operation_id = $1 and adapter_name = 'record_activity_projection'`,
			command.Operation.OperationID,
		).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt); err != nil {
			return fmt.Errorf("verify record activity purge receipt: %w", err)
		}
		if removed < 0 || uint64(removed) != receipt.RemovedRowCount ||
			!equalRecordPurgeDigest(rawSurface, command.SurfaceDigest) ||
			!equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) ||
			!verifiedAt.UTC().Equal(receipt.VerifiedAbsentAt) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return assertRecordActivitySurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID)
	})
}

func assertRecordActivitySurfacesAbsent(ctx context.Context, tx pgx.Tx, recordID string) error {
	var remaining int64
	if err := tx.QueryRow(ctx, `
		select
		  (select count(*) from public.record_activity_subjects where record_id = $1) +
		  (select count(*) from public.record_activity_projection where record_id = $1) +
		  (select count(*) from public.record_activity_revision_intervals where record_id = $1)`, recordID,
	).Scan(&remaining); err != nil {
		return fmt.Errorf("verify record activity purge absence: %w", err)
	}
	if remaining != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func loadActivityPurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command activity.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, bool, error) {
	var rawSurface, rawReceipt []byte
	var removed int64
	var verifiedAt time.Time
	err := tx.QueryRow(ctx, `
		select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
		from public.record_activity_purge_receipts
		where operation_id = $1 and adapter_name = 'record_activity_projection'`,
		command.Operation.OperationID,
	).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPurgeReceipt{}, false, nil
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, false, fmt.Errorf("load record activity purge receipt: %w", err)
	}
	if removed < 0 || verifiedAt.IsZero() || !equalRecordPurgeDigest(rawSurface, command.SurfaceDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordActivityProjection, OperationID: command.Operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: activityDeletionReceiptDigest(command, uint64(removed)),
		RemovedRowCount: uint64(removed), VerifiedAbsentAt: verifiedAt.UTC(),
	}
	if !equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return receipt, true, nil
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
