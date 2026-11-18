package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"

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
		var err error
		health, err = portabilityDeletionHealthInTransaction(ctx, transaction.tx)
		return err
	})
	return health, err
}

func portabilityDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	surfaces := recorddeletion.RecordPortabilitySurfaceNames()
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
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read record portability deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.local_portability_purge_prepare(bytea)", "public.local_portability_purge_complete(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.local_portability_purge_verify(bytea)", "public.local_portability_purge_verify(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	var v1Present bool
	if err := tx.QueryRow(ctx, `select bool_and(pg_catalog.to_regprocedure(required.signature) is not null)
   from unnest(array[
     'public.record_portability_purge_prepare_v1(bytea)',
     'public.record_portability_purge_prepare_resolve_v1(bytea)',
     'public.record_portability_purge_v1(bytea)',
     'public.record_portability_purge_verify_v1(bytea)',
     'public.record_portability_local_prepare_v1(bytea)',
     'public.record_portability_local_claim_v1(bytea)',
     'public.record_portability_local_resolve_v1(bytea)',
     'public.record_portability_local_bind_final_v1(bytea)',
     'public.record_portability_local_complete_v1(bytea)',
     'public.record_portability_gc_claim_v1(bytea)',
     'public.record_portability_gc_claim_resolve_v1(bytea)',
     'public.record_portability_gc_complete_v1(bytea)',
     'public.record_portability_gc_retry_v1(bytea)',
     'public.record_portability_gc_resolve_v1(bytea)'
   ]::text[]) as required(signature)`).Scan(&v1Present); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read V1 portability deletion functions: %w", err)
	}
	if !v1Present {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(
		true, 1, digestRecordPurgeStrings(portabilityDeletionHealthDigestDomainV1, names...),
	)
	if err != nil {
		return health, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
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
		var err error
		snapshot, err = previewPortabilityDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	return snapshot, err
}

func previewPortabilityDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var dependencyMaterial, impactMaterial []byte
	var deliveredExports int64
	// Preview material is job/origin identifiers and counts only. Markdown,
	// archive bytes, and blob keys stay out of the hash so a preview log
	// cannot reconstruct an exported document.
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
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview record portability deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	surviving := []recorddeletion.AdapterSurvivingCopy{}
	if deliveredExports > 0 {
		surviving = append(surviving, recorddeletion.KnownAdapterSurvivingCopy(
			recorddeletion.SurvivingCopyKindDeliveredExport, uint64(deliveredExports),
		))
	}
	snapshot := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(
			portabilityDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
		),
		ImpactDigest:    digestRecordPurgeBytes(portabilityDeletionImpactDigestDomainV1, impactMaterial),
		SurvivingCopies: surviving,
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return snapshot, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordPortability(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
	surfaceDigest [sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil ||
		surfaceDigest != recorddeletion.RecordPortabilitySurfaceDigest() ||
		target.Operation.LedgerSequence > math.MaxInt64 || uint64(target.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if target.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
		return repository.purgeLocalRecordPortability(ctx, target, surfaceDigest)
	}
	return repository.purgePortabilityV1(ctx, target, surfaceDigest)
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordPortabilityPurge(
	ctx context.Context,
	target recorddeletion.PurgeTarget,
	surfaceDigest [sha256.Size]byte,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil ||
		surfaceDigest != recorddeletion.RecordPortabilitySurfaceDigest() ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordPortability ||
		receipt.OperationID != target.Operation.OperationID || receipt.SurfaceDigest != surfaceDigest || receipt.VerifiedAbsentAt.IsZero() {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if target.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
		return repository.verifyLocalRecordPortabilityPurge(ctx, target, surfaceDigest, receipt)
	}
	actual, err := repository.portabilityV1Finish(ctx, target, surfaceDigest, true)
	if err != nil {
		return err
	}
	return matchClientComparisonPurgeReceipt(receipt, actual)
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
