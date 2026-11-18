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
	markdownClientDeletionHealthDigestDomainV1  = "houfeng.record-markdown-client.deletion-health.v1"
	markdownClientDeletionPreviewDigestDomainV1 = "houfeng.record-markdown-client.deletion-preview.v1"
	markdownClientDeletionImpactDigestDomainV1  = "houfeng.record-markdown-client.deletion-impact.v1"
)

var _ recorddeletion.RecordMarkdownClientStore = (*PostgresRecordDeletionRepository)(nil)

func (repository *PostgresRecordDeletionRepository) RecordMarkdownClientHealth(ctx context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		health, err = markdownClientDeletionHealthInTransaction(ctx, transaction.tx)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	return health, nil
}

func markdownClientDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	names, err := actualDeletionSurfaceNames(ctx, tx, recorddeletion.RecordMarkdownClientSurfaceNames())
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := requireClientPurgeFunctionContracts(ctx, tx, []string{
		"public.record_markdown_client_purge(bytea)", "public.record_markdown_client_purge_verify(bytea)",
		"public.record_markdown_client_purge_v1(bytea)", "public.record_markdown_client_purge_verify_v1(bytea)",
	}); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1,
		digestRecordPurgeStrings(markdownClientDeletionHealthDigestDomainV1, names...))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

// actualDeletionSurfaceNames verifies each owned relation in the caller's
// admitted snapshot; index-only health cannot substitute for these surfaces.
func actualDeletionSurfaceNames(ctx context.Context, tx pgx.Tx, surfaces []recorddeletion.SurfaceName) ([]string, error) {
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
		return nil, fmt.Errorf("read deletion adapter surfaces: %w", err)
	}
	if count != int64(len(names)) {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return names, nil
}

func requireClientComparisonPurgeFunctions(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, purge, verify string) error {
	var present bool
	if err := tx.QueryRow(ctx, `select pg_catalog.to_regprocedure($1) is not null
		and pg_catalog.to_regprocedure($2) is not null`, purge, verify).Scan(&present); err != nil {
		return fmt.Errorf("read restricted deletion functions: %w", err)
	}
	if !present {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func requireClientPurgeFunctionContracts(ctx context.Context, tx pgx.Tx, signatures []string) error {
	var invalid int64
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint from unnest($1::text[]) as required(signature)
		left join pg_catalog.pg_proc as proc on proc.oid=pg_catalog.to_regprocedure(required.signature)
		where proc.oid is null or proc.prosecdef is not true or proc.proretset is not true
		  or proc.prorettype <> 'record'::regtype
		  or not coalesce(proc.proconfig @> array['search_path=pg_catalog'],false)
		  or proc.proargnames[(proc.pronargs::int+1):] is distinct from
		    array['removed_surface_digest','receipt_digest','removed_row_count','verified_absent_at']
		  or proc.proallargtypes[(proc.pronargs::int+1):] is distinct from
		    array['bytea'::regtype::oid,'bytea'::regtype::oid,'bigint'::regtype::oid,'timestamptz'::regtype::oid]
		  or proc.proargmodes[(proc.pronargs::int+1):] is distinct from array_fill('t'::"char",array[4])`, signatures).Scan(&invalid); err != nil {
		return fmt.Errorf("read client deletion function contracts: %w", err)
	}
	if invalid != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func (repository *PostgresRecordDeletionRepository) PreviewRecordMarkdownClient(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || !validClientDeletionPreviewTarget(target) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	var preview recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		preview, err = previewMarkdownClientDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	return preview, nil
}

func validClientDeletionPreviewTarget(target recorddeletion.PreviewTarget) bool {
	return target.Validate() == nil && target.LockVersion <= math.MaxInt64 &&
		target.AuthorizationEpoch <= math.MaxInt64 && uint64(target.ContentDeliveryEpoch) <= math.MaxInt64
}

func previewMarkdownClientDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || !validClientDeletionPreviewTarget(target) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	var material []byte
	err := tx.QueryRow(ctx, `
		with root as materialized (
		  select record.record_id
		  from public.records as record
		  join public.content_delivery_epochs as epoch
		    on epoch.project_id = record.project_id and epoch.object_kind = 'record'
		   and epoch.object_id = record.record_id
		  where record.project_id = $1 and record.record_id = $2
		    and record.current_revision_id = $3 and record.lock_version = $4
		    and record.authorization_epoch = $5 and epoch.delivery_epoch = $6
		)
		select pg_catalog.convert_to(jsonb_build_object(
		  'targets', coalesce((select jsonb_agg(jsonb_build_array(
		    user_id, browser_id, project_id, extract(epoch from registered_at))
		    order by user_id collate "C", browser_id collate "C")
		    from public.browser_record_cleanup_targets where project_id = $1 and record_id = root.record_id), '[]'::jsonb),
		  'events', coalesce((select jsonb_agg(jsonb_build_array(
		    cursor, operation_id, project_id, cleanup_epoch, scopes, extract(epoch from created_at)) order by cursor)
		    from public.browser_record_cleanup_events where project_id = $1 and record_id = root.record_id), '[]'::jsonb),
		  'acknowledgements', coalesce((select jsonb_agg(jsonb_build_array(
		    receipt.user_id, receipt.browser_id, receipt.event_cursor, receipt.operation_id,
		    receipt.cleanup_epoch, extract(epoch from receipt.acked_at))
		    order by receipt.user_id collate "C", receipt.browser_id collate "C", receipt.event_cursor)
		    from public.browser_record_cleanup_receipts as receipt
		    join public.browser_record_cleanup_events as event on event.cursor = receipt.event_cursor
		    where event.project_id = $1 and receipt.record_id = root.record_id), '[]'::jsonb),
		  'draft_buffers', coalesce((select jsonb_agg(jsonb_build_array(
		    user_id, browser_id, project_id, session_generation, cleanup_generation,
		    extract(epoch from registered_at), extract(epoch from updated_at))
		    order by user_id collate "C", browser_id collate "C")
		    from public.markdown_client_draft_buffers where project_id = $1 and record_id = root.record_id), '[]'::jsonb),
		  'workspaces', coalesce((select jsonb_agg(jsonb_build_array(
		    user_id, browser_id, project_id, session_generation, cleanup_generation,
		    extract(epoch from registered_at), extract(epoch from updated_at))
		    order by user_id collate "C", browser_id collate "C")
		    from public.markdown_client_workspaces where project_id = $1 and record_id = root.record_id), '[]'::jsonb),
		  'purge_receipts', coalesce((select jsonb_agg(jsonb_build_array(
		    receipt.operation_id, encode(receipt.removed_surface_digest, 'hex'),
		    encode(receipt.receipt_digest, 'hex'), receipt.removed_row_count,
		    extract(epoch from receipt.verified_absent_at), extract(epoch from receipt.created_at))
		    order by receipt.operation_id collate "C")
		    from public.record_markdown_client_purge_receipts as receipt
		    join public.record_purge_operations as operation on operation.operation_id = receipt.operation_id
		    join public.deletion_reservations as reservation on reservation.reservation_id = operation.reservation_id
		    where reservation.project_id = $1 and reservation.object_kind = 'record'
		      and reservation.object_id = root.record_id), '[]'::jsonb)
		)::text, 'UTF8') from root`,
		target.Object.ProjectID, target.Object.ObjectID, target.CurrentRevisionID,
		int64(target.LockVersion), int64(target.AuthorizationEpoch), int64(target.ContentDeliveryEpoch),
	).Scan(&material)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("inventory markdown client deletion preview: %w", err)
	}
	if len(material) == 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(markdownClientDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], material),
		ImpactDigest:     digestRecordPurgeBytes(markdownClientDeletionImpactDigestDomainV1, material),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{
			recorddeletion.UnknownAdapterSurvivingCopy(recorddeletion.SurvivingCopyKindOfflineBrowserBuffer),
		},
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return preview, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordMarkdownClient(
	ctx context.Context,
	command recorddeletion.MarkdownClientPurgeCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		receipt, err = runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_markdown_client_purge($1)`, `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_markdown_client_purge_v1($1)`, recorddeletion.AdapterNameRecordMarkdownClient,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordMarkdownClientPurge(
	ctx context.Context,
	command recorddeletion.MarkdownClientPurgeCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.ErrInvalidMarkdownClientAdapter
	}
	target := recorddeletion.PurgeTarget{Operation: command.Operation, OwnerClaim: command.OwnerClaim}
	descriptor, err := recorddeletion.NewAdapterDescriptor(recorddeletion.AdapterNameRecordMarkdownClient, recorddeletion.RecordMarkdownClientSurfaceNames())
	if err != nil || recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, descriptor) != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		actual, err := runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_markdown_client_purge_verify($1)`, `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_markdown_client_purge_verify_v1($1)`, recorddeletion.AdapterNameRecordMarkdownClient,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}

// clientComparisonPurgeEnvelope is the exact bytea JSON accepted by both
// restricted V2 functions. Receipt data is always returned by PostgreSQL.
type clientComparisonPurgeEnvelope struct {
	OperationID     string `json:"operation_id"`
	RecordID        string `json:"record_id"`
	ReservationID   string `json:"reservation_id"`
	FenceEpoch      uint64 `json:"fence_epoch"`
	OwnerID         string `json:"owner_id"`
	OwnerGeneration uint64 `json:"owner_generation"`
	OwnerExpiresAt  string `json:"owner_expires_at"`
	SurfaceDigest   string `json:"surface_digest"`
}

func runClientComparisonPurgeFunction(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	legacyQuery string,
	name recorddeletion.AdapterName,
	operation recorddeletion.DeletionOperation,
	claim recorddeletion.OperationOwnerClaim,
	surfaceDigest [sha256.Size]byte,
) (recorddeletion.AdapterPurgeReceipt, error) {
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	encoded, err := deletionAdapterEnvelope(ctx, tx, target, surfaceDigest)
	if operation.ContractVersion == recorddeletion.DeletionContractV1 {
		query = legacyQuery
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var removedDigest, receiptDigest []byte
	var removedRows int64
	var verifiedAt time.Time
	if err := tx.QueryRow(ctx, query, encoded).Scan(&removedDigest, &receiptDigest, &removedRows, &verifiedAt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("run restricted %s deletion command: %w", name, err)
	}
	if !equalRecordPurgeDigest(removedDigest, surfaceDigest) || len(receiptDigest) != sha256.Size ||
		removedRows < 0 || verifiedAt.IsZero() {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var digest [sha256.Size]byte
	copy(digest[:], receiptDigest)
	if digest == [sha256.Size]byte{} {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return recorddeletion.AdapterPurgeReceipt{
		AdapterName: name, OperationID: operation.OperationID, SurfaceDigest: surfaceDigest,
		ReceiptDigest: digest, RemovedRowCount: uint64(removedRows), VerifiedAbsentAt: verifiedAt.UTC(),
	}, nil
}

func matchClientComparisonPurgeReceipt(expected, actual recorddeletion.AdapterPurgeReceipt) error {
	if expected.AdapterName != actual.AdapterName || expected.OperationID != actual.OperationID ||
		expected.SurfaceDigest != actual.SurfaceDigest || expected.ReceiptDigest != actual.ReceiptDigest ||
		expected.RemovedRowCount != actual.RemovedRowCount || !expected.VerifiedAbsentAt.Equal(actual.VerifiedAbsentAt) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}
