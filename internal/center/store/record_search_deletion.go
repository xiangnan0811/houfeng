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
		surfaces := recorddeletion.RecordSearchSurfaceNames()
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
			return fmt.Errorf("read record search deletion health: %w", err)
		}
		if count != int64(len(names)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var err error
		health, err = recorddeletion.NewAdapterHealthSnapshot(
			true, 1, digestRecordPurgeStrings(searchDeletionHealthDigestDomainV1, names...),
		)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return health, err
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
		var dependencyMaterial, impactMaterial []byte
		// The projection is derived, so the index never holds a copy that outlives
		// the record: there is nothing here for an operator to be warned about, and
		// the preview reports no surviving copies. Indexed content is bound through
		// document_digest rather than the text itself, so preview material stays
		// safe to hash, log, and compare.
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
			return recorddeletion.ErrDeletionPreviewStale
		}
		if err != nil {
			return fmt.Errorf("preview record search deletion: %w", err)
		}
		if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		snapshot = recorddeletion.AdapterPreviewSnapshot{
			DependencyDigest: digestRecordPurgeBytes(
				searchDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
			),
			ImpactDigest:    digestRecordPurgeBytes(searchDeletionImpactDigestDomainV1, impactMaterial),
			SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
		}
		if err := snapshot.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return snapshot, err
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordSearch(
	ctx context.Context,
	command recordsearch.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		command.Operation.LedgerSequence > math.MaxInt64 || uint64(command.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, recordsearch.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, command.Operation); err != nil {
			return err
		}
		existing, found, err := loadSearchPurgeReceipt(ctx, transaction.tx, command)
		if err != nil {
			return err
		}
		if found {
			if err := assertRecordSearchSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
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
			select public.record_search_purge($1)`, encoded,
		).Scan(&removed); err != nil {
			return fmt.Errorf("purge record search through constrained function: %w", err)
		}
		if removed < 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := assertRecordSearchSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
			return err
		}
		removedRows := uint64(removed)
		receiptDigest := searchDeletionReceiptDigest(command, removedRows)
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			insert into public.record_search_purge_receipts (
				operation_id, removed_surface_digest, receipt_digest,
				removed_row_count, verified_absent_at
			) values ($1, $2, $3, $4, transaction_timestamp())
			returning verified_absent_at`, command.Operation.OperationID, command.SurfaceDigest[:],
			receiptDigest[:], removed,
		).Scan(&verifiedAt); err != nil {
			return fmt.Errorf("insert record search purge receipt: %w", err)
		}
		if verifiedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		receipt = recorddeletion.AdapterPurgeReceipt{
			AdapterName: recorddeletion.AdapterNameRecordSearch, OperationID: command.Operation.OperationID,
			SurfaceDigest: command.SurfaceDigest, ReceiptDigest: receiptDigest,
			RemovedRowCount: removedRows, VerifiedAbsentAt: verifiedAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.VerifyRecordSearchPurge(ctx, command, receipt); err != nil {
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
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, command.Operation); err != nil {
			return err
		}
		var rawSurface, rawReceipt []byte
		var removed int64
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_search_purge_receipts
			where operation_id = $1 and adapter_name = 'record_search'`,
			command.Operation.OperationID,
		).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if removed < 0 || uint64(removed) != receipt.RemovedRowCount ||
			!verifiedAt.Equal(receipt.VerifiedAbsentAt) ||
			!equalRecordPurgeDigest(rawSurface, receipt.SurfaceDigest) ||
			!equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return assertRecordSearchSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID)
	})
}

// assertRecordSearchSurfacesAbsent checks every generation, not just the
// published one. A shadow rebuild in flight would otherwise be free to carry a
// purged record's text into the next generation it publishes.
func assertRecordSearchSurfacesAbsent(ctx context.Context, tx pgx.Tx, recordID string) error {
	var remaining int64
	if err := tx.QueryRow(ctx, `
		select
		  (select count(*) from public.record_search_subjects where record_id = $1) +
		  (select count(*) from public.record_search_documents where record_id = $1)`, recordID,
	).Scan(&remaining); err != nil {
		return fmt.Errorf("verify record search purge absence: %w", err)
	}
	if remaining != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func loadSearchPurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command recordsearch.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, bool, error) {
	var rawSurface, rawReceipt []byte
	var removed int64
	var verifiedAt time.Time
	err := tx.QueryRow(ctx, `
		select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
		from public.record_search_purge_receipts
		where operation_id = $1 and adapter_name = 'record_search'`,
		command.Operation.OperationID,
	).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPurgeReceipt{}, false, nil
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, false, fmt.Errorf("load record search purge receipt: %w", err)
	}
	if removed < 0 || verifiedAt.IsZero() || !equalRecordPurgeDigest(rawSurface, command.SurfaceDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordSearch, OperationID: command.Operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: searchDeletionReceiptDigest(command, uint64(removed)),
		RemovedRowCount: uint64(removed), VerifiedAbsentAt: verifiedAt.UTC(),
	}
	if !equalRecordPurgeDigest(rawReceipt, receipt.ReceiptDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return receipt, true, nil
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
