package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

const (
	portabilityDeletionHealthDigestDomainV1  = "houfeng.record-portability.deletion-health.v1"
	portabilityDeletionPreviewDigestDomainV1 = "houfeng.record-portability.deletion-preview.v1"
	portabilityDeletionImpactDigestDomainV1  = "houfeng.record-portability.deletion-impact.v1"
	portabilityDeletionReceiptDigestDomainV1 = "houfeng.record-portability.deletion-receipt.v1"
)

func (repository *PostgresRecordDeletionRepository) PortabilityDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		surfaces := recorddeletion.RecordPortabilitySurfaceNames()
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
			return fmt.Errorf("read record portability deletion health: %w", err)
		}
		if count != int64(len(names)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var err error
		health, err = recorddeletion.NewAdapterHealthSnapshot(
			true, 1, digestRecordPurgeStrings(portabilityDeletionHealthDigestDomainV1, names...),
		)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return health, err
}

func (repository *PostgresRecordDeletionRepository) PreviewPortabilityDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var snapshot recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var dependencyMaterial, impactMaterial []byte
		var deliveredExports int64
		// Preview material is job/origin identifiers and counts only. Markdown,
		// archive bytes, and blob keys stay out of the hash so a preview log
		// cannot reconstruct an exported document.
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
				  'jobs', coalesce((select jsonb_agg(jsonb_build_array(
				    export_job_id, export_kind, job_state
				  ) order by export_job_id) from public.record_export_jobs
				  where project_id = $1 and record_id = root.record_id), '[]'::jsonb),
				  'artifacts', coalesce((select jsonb_agg(jsonb_build_array(
				    artifact.export_artifact_id, artifact.artifact_kind, encode(artifact.sha256, 'hex')
				  ) order by artifact.export_artifact_id)
				  from public.record_export_artifacts as artifact
				  join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
				  where job.project_id = $1 and job.record_id = root.record_id), '[]'::jsonb),
				  'origins', coalesce((select jsonb_agg(jsonb_build_array(
				    origin_id, origin_kind, encode(origin_digest, 'hex')
				  ) order by origin_id) from public.record_origins
				  where project_id = $1 and source_record_id = root.record_id), '[]'::jsonb),
				  'mappings', coalesce((select jsonb_agg(jsonb_build_array(
				    import_plan_id, entity_kind
				  ) order by import_plan_id)
				  from public.record_import_entity_mappings
				  where entity_kind = 'record' and target_id = root.record_id), '[]'::jsonb)
				) as dependency_material,
				jsonb_build_object(
				  'jobs', (select count(*) from public.record_export_jobs
				    where project_id = $1 and record_id = root.record_id),
				  'artifacts', (select count(*) from public.record_export_artifacts as artifact
				    join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
				    where job.project_id = $1 and job.record_id = root.record_id),
				  'origins', (select count(*) from public.record_origins
				    where project_id = $1 and source_record_id = root.record_id),
				  'mappings', (select count(*) from public.record_import_entity_mappings
				    where entity_kind = 'record' and target_id = root.record_id)
				) as impact_material,
				(
				  select count(*) from public.record_export_artifacts as artifact
				  join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
				  where job.project_id = $1
				    and job.record_id = root.record_id
				    and job.job_state = 'published'
				    and artifact.revoked_at is null
				) as delivered_exports
				from root
			)
			select pg_catalog.convert_to(dependency_material::text, 'UTF8'),
			       pg_catalog.convert_to(impact_material::text, 'UTF8'),
			       delivered_exports
			from material`,
			target.Object.ProjectID, target.Object.ObjectID, target.CurrentRevisionID,
			int64(target.LockVersion), int64(target.AuthorizationEpoch), int64(target.ContentDeliveryEpoch),
		).Scan(&dependencyMaterial, &impactMaterial, &deliveredExports)
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.ErrDeletionPreviewStale
		}
		if err != nil {
			return fmt.Errorf("preview record portability deletion: %w", err)
		}
		if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		surviving := []recorddeletion.AdapterSurvivingCopy{}
		if deliveredExports > 0 {
			surviving = append(surviving, recorddeletion.AdapterSurvivingCopy{
				Kind:      recorddeletion.SurvivingCopyKindDeliveredExport,
				CopyCount: uint64(deliveredExports),
			})
		}
		snapshot = recorddeletion.AdapterPreviewSnapshot{
			DependencyDigest: digestRecordPurgeBytes(
				portabilityDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
			),
			ImpactDigest:    digestRecordPurgeBytes(portabilityDeletionImpactDigestDomainV1, impactMaterial),
			SurvivingCopies: surviving,
		}
		if err := snapshot.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return snapshot, err
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordPortability(
	ctx context.Context,
	operation recorddeletion.DeletionOperation,
	surfaceDigest [sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		(recorddeletion.PurgeTarget{Operation: operation}).Validate() != nil ||
		surfaceDigest != recorddeletion.RecordPortabilitySurfaceDigest() ||
		operation.LedgerSequence > math.MaxInt64 || uint64(operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, operation); err != nil {
			return err
		}
		existing, found, err := loadPortabilityPurgeReceipt(ctx, transaction.tx, operation, surfaceDigest)
		if err != nil {
			return err
		}
		if found {
			if err := assertRecordPortabilitySurfacesAbsent(ctx, transaction.tx, operation.Object.ObjectID); err != nil {
				return err
			}
			receipt = existing
			return nil
		}
		var removed int64
		encoded, err := encodeRecordPurgeCommand(recordPurgeFunctionCommand{
			OperationID: operation.OperationID, ReservationID: operation.ReservationID,
			ProjectID: operation.Object.ProjectID, RecordID: operation.Object.ObjectID,
			FenceEpoch: int64(operation.FenceEpoch), LedgerSequence: int64(operation.LedgerSequence),
			LedgerEntryHash: encodeRecordPurgeLedgerHash(operation.LedgerEntryHash),
		})
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := transaction.tx.QueryRow(ctx, `
			select public.record_portability_purge($1)`, encoded,
		).Scan(&removed); err != nil {
			return fmt.Errorf("purge record portability through constrained function: %w", err)
		}
		if removed < 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := assertRecordPortabilitySurfacesAbsent(ctx, transaction.tx, operation.Object.ObjectID); err != nil {
			return err
		}
		removedRows := uint64(removed)
		receiptDigest := portabilityDeletionReceiptDigest(operation, surfaceDigest, removedRows)
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			insert into public.record_portability_purge_receipts (
				operation_id, removed_surface_digest, receipt_digest,
				removed_row_count, verified_absent_at
			) values ($1, $2, $3, $4, transaction_timestamp())
			returning verified_absent_at`, operation.OperationID, surfaceDigest[:],
			receiptDigest[:], removed,
		).Scan(&verifiedAt); err != nil {
			return fmt.Errorf("insert record portability purge receipt: %w", err)
		}
		if verifiedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		receipt = recorddeletion.AdapterPurgeReceipt{
			AdapterName: recorddeletion.AdapterNameRecordPortability, OperationID: operation.OperationID,
			SurfaceDigest: surfaceDigest, ReceiptDigest: receiptDigest,
			RemovedRowCount: removedRows, VerifiedAbsentAt: verifiedAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.VerifyRecordPortabilityPurge(ctx, operation, surfaceDigest, receipt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordPortabilityPurge(
	ctx context.Context,
	operation recorddeletion.DeletionOperation,
	surfaceDigest [sha256.Size]byte,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil ||
		(recorddeletion.PurgeTarget{Operation: operation}).Validate() != nil ||
		surfaceDigest != recorddeletion.RecordPortabilitySurfaceDigest() ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordPortability ||
		receipt.OperationID != operation.OperationID || receipt.SurfaceDigest != surfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != portabilityDeletionReceiptDigest(operation, surfaceDigest, receipt.RemovedRowCount) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, operation); err != nil {
			return err
		}
		var rawSurface, rawReceipt []byte
		var removed int64
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_portability_purge_receipts
			where operation_id = $1 and adapter_name = 'record_portability'`,
			operation.OperationID,
		).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if removed < 0 || uint64(removed) != receipt.RemovedRowCount ||
			!verifiedAt.UTC().Equal(receipt.VerifiedAbsentAt) ||
			!equalRecordPurgeDigest(rawSurface, receipt.SurfaceDigest) ||
			!equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return assertRecordPortabilitySurfacesAbsent(ctx, transaction.tx, operation.Object.ObjectID)
	})
}

func assertRecordPortabilitySurfacesAbsent(ctx context.Context, tx pgx.Tx, recordID string) error {
	var remaining int64
	if err := tx.QueryRow(ctx, `
		select
		  (select count(*) from public.record_export_jobs where record_id = $1) +
		  (select count(*) from public.record_export_artifacts as artifact
		    join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
		    where job.record_id = $1) +
		  (select count(*) from public.record_origins where source_record_id = $1) +
		  (select count(*) from public.record_import_entity_mappings
		    where entity_kind = 'record' and target_id = $1)`, recordID,
	).Scan(&remaining); err != nil {
		return fmt.Errorf("verify record portability purge absence: %w", err)
	}
	if remaining != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func loadPortabilityPurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	operation recorddeletion.DeletionOperation,
	surfaceDigest [sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, bool, error) {
	var rawSurface, rawReceipt []byte
	var removed int64
	var verifiedAt time.Time
	err := tx.QueryRow(ctx, `
		select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
		from public.record_portability_purge_receipts
		where operation_id = $1 and adapter_name = 'record_portability'`,
		operation.OperationID,
	).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPurgeReceipt{}, false, nil
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, false, fmt.Errorf("load record portability purge receipt: %w", err)
	}
	if removed < 0 || verifiedAt.IsZero() || !equalRecordPurgeDigest(rawSurface, surfaceDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordPortability, OperationID: operation.OperationID,
		SurfaceDigest: surfaceDigest, ReceiptDigest: portabilityDeletionReceiptDigest(operation, surfaceDigest, uint64(removed)),
		RemovedRowCount: uint64(removed), VerifiedAbsentAt: verifiedAt.UTC(),
	}
	if !equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return receipt, true, nil
}

func portabilityDeletionReceiptDigest(
	operation recorddeletion.DeletionOperation,
	surfaceDigest [sha256.Size]byte,
	removed uint64,
) [sha256.Size]byte {
	return digestRecordPurgeBytes(
		portabilityDeletionReceiptDigestDomainV1,
		[]byte(operation.OperationID),
		[]byte(operation.Object.ProjectID),
		[]byte(operation.Object.ObjectID),
		surfaceDigest[:],
		recordPurgeUint64(removed),
	)
}
