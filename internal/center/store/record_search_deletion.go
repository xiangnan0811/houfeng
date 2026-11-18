package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordsearch"
)

const (
	searchDeletionHealthDigestDomainV1  = "houfeng.record-search.deletion-health.v1"
	searchDeletionPreviewDigestDomainV1 = "houfeng.record-search.deletion-preview.v1"
	searchDeletionImpactDigestDomainV1  = "houfeng.record-search.deletion-impact.v1"
	searchDeletionReceiptDigestDomainV1 = "houfeng.record-search.deletion-receipt.v1"
)

func (repository *PostgresRecordDeletionRepository) SearchDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordsearch.ErrInvalidDeletionAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		health, err = searchDeletionHealthInTransaction(ctx, transaction.tx)
		return err
	})
	return health, err
}

func searchDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordsearch.ErrInvalidDeletionAdapter
	}
	surfaces := recorddeletion.RecordSearchSurfaceNames()
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
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read record search deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.record_search_purge_v2(bytea)", "public.record_search_purge_verify_v2(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(
		true, 1, digestRecordPurgeStrings(searchDeletionHealthDigestDomainV1, names...),
	)
	if err != nil {
		return health, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

func (repository *PostgresRecordDeletionRepository) PreviewSearchDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recordsearch.ErrInvalidDeletionAdapter
	}
	var snapshot recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		snapshot, err = previewSearchDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	return snapshot, err
}

func previewSearchDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recordsearch.ErrInvalidDeletionAdapter
	}
	var dependencyMaterial, impactMaterial []byte
	// The projection is derived, so the index never holds a copy that outlives
	// the record: there is nothing here for an operator to be warned about, and
	// the preview reports no surviving copies. Indexed content is bound through
	// document_digest rather than the text itself, so preview material stays
	// safe to hash, log, and compare.
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
				  'documents', coalesce((select jsonb_agg(jsonb_build_array(
				    generation, current_revision_id, record_lock_version, authorization_epoch,
				    record_fence_epoch, lifecycle, encode(document_digest, 'hex')
				  ) order by generation) from public.record_search_documents
				  where record_id = root.record_id), '[]'::jsonb),
				  'subjects', coalesce((select jsonb_agg(jsonb_build_array(
				    generation, subject_kind, source_id, relation_role, is_primary
				  ) order by generation, subject_kind, source_id, relation_role)
				  from public.record_search_subjects where record_id = root.record_id), '[]'::jsonb)
				) as dependency_material,
				jsonb_build_object(
				  'documents', (select count(*) from public.record_search_documents where record_id = root.record_id),
				  'subjects', (select count(*) from public.record_search_subjects where record_id = root.record_id)
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
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview record search deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	snapshot := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(
			searchDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
		),
		ImpactDigest:    digestRecordPurgeBytes(searchDeletionImpactDigestDomainV1, impactMaterial),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return snapshot, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordSearch(
	ctx context.Context,
	command recordsearch.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recordsearch.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_search_purge_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_search_purge_v2($1)`
		}
		receipt, err = runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordSearch, command.PurgeTarget, command.SurfaceDigest)
		if err == nil && receipt.ReceiptDigest != searchDeletionReceiptDigest(command, receipt.RemovedRowCount) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordSearchPurge(
	ctx context.Context,
	command recordsearch.DeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordSearch ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != searchDeletionReceiptDigest(command, receipt.RemovedRowCount) {
		return recordsearch.ErrInvalidDeletionAdapter
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		body, err := deletionAdapterEnvelope(ctx, transaction.tx, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_search_purge_verify_v1($1)`
		if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_search_purge_verify_v2($1)`
		}
		actual, err := runLegacyAdapterPurge(ctx, transaction.tx, query, body,
			recorddeletion.AdapterNameRecordSearch, command.PurgeTarget, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}

func searchDeletionReceiptDigest(
	command recordsearch.DeletionCommand,
	removed uint64,
) [sha256.Size]byte {
	return digestRecordPurgeBytes(
		searchDeletionReceiptDigestDomainV1,
		[]byte(command.Operation.OperationID),
		[]byte(command.Operation.Object.ProjectID),
		[]byte(command.Operation.Object.ObjectID),
		command.SurfaceDigest[:],
		recordPurgeUint64(removed),
	)
}

var _ recordsearch.DeletionStore = (*PostgresRecordDeletionRepository)(nil)
