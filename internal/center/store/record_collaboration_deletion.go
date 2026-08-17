package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
)

const (
	collaborationDeletionHealthDigestDomainV1  = "houfeng.record-collaboration.deletion-health.v1"
	collaborationDeletionPreviewDigestDomainV1 = "houfeng.record-collaboration.deletion-preview.v1"
	collaborationDeletionImpactDigestDomainV1  = "houfeng.record-collaboration.deletion-impact.v1"
	collaborationDeletionReceiptDigestDomainV1 = "houfeng.record-collaboration.deletion-receipt.v1"
)

func (repository *PostgresRecordDeletionRepository) CollaborationDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		surfaces := recorddeletion.RecordCollaborationSurfaceNames()
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
			return fmt.Errorf("read collaboration deletion health: %w", err)
		}
		if count != int64(len(names)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var err error
		health, err = recorddeletion.NewAdapterHealthSnapshot(
			true, 1, digestCollaborationDeletionStrings(collaborationDeletionHealthDigestDomainV1, names...),
		)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return health, err
}

func (repository *PostgresRecordDeletionRepository) PreviewCollaborationDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var snapshot recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var dependencyMaterial, impactMaterial []byte
		var deliveredCount, possibleCount int64
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
				  'actions', coalesce((select jsonb_agg(jsonb_build_array(
				    action_id, action_version, status, assignee_id, due_at, subject_revision_id
				  ) order by action_id) from public.record_actions where record_id = root.record_id), '[]'::jsonb),
				  'action_events', coalesce((select jsonb_agg(jsonb_build_array(
				    action_event_id, action_id, action_version, event_kind, record_fence_epoch
				  ) order by action_event_id) from public.record_action_events where record_id = root.record_id), '[]'::jsonb),
				  'comments', coalesce((select jsonb_agg(jsonb_build_array(
				    comment_id, comment_state, comment_version, encode(body_digest, 'hex'), tombstone_id, record_fence_epoch
				  ) order by comment_id) from public.record_comments where record_id = root.record_id), '[]'::jsonb),
				  'comment_revisions', coalesce((select jsonb_agg(jsonb_build_array(
				    comment_revision_id, comment_id, comment_version, encode(body_digest, 'hex'), tombstone_id, record_fence_epoch
				  ) order by comment_revision_id) from public.record_comment_revisions where record_id = root.record_id), '[]'::jsonb),
				  'comment_tombstones', coalesce((select jsonb_agg(jsonb_build_array(
				    tombstone_id, comment_id, tombstone_version, reason_code, record_fence_epoch
				  ) order by tombstone_id) from public.record_comment_tombstones where record_id = root.record_id), '[]'::jsonb),
				  'comment_replies', coalesce((select jsonb_agg(jsonb_build_array(
				    child_comment_id, parent_comment_id, record_fence_epoch
				  ) order by child_comment_id) from public.record_comment_replies where record_id = root.record_id), '[]'::jsonb),
				  'comment_mentions', coalesce((select jsonb_agg(jsonb_build_array(
				    comment_id, comment_version, mentioned_user_id, record_fence_epoch
				  ) order by comment_id, comment_version, mentioned_user_id) from public.record_comment_mentions where record_id = root.record_id), '[]'::jsonb),
				  'followers', coalesce((select jsonb_agg(jsonb_build_array(
				    user_id, follower_version, manual_preference, record_fence_epoch
				  ) order by user_id) from public.record_followers where record_id = root.record_id), '[]'::jsonb),
				  'notifications', coalesce((select jsonb_agg(jsonb_build_array(
				    notification_id, event_kind, subject_kind, subject_id, source_version, record_fence_epoch
				  ) order by notification_id) from public.record_notifications where record_id = root.record_id), '[]'::jsonb),
				  'recipients', coalesce((select jsonb_agg(jsonb_build_array(
				    notification_id, recipient_user_id, reason_kind, record_fence_epoch
				  ) order by notification_id, recipient_user_id) from public.record_notification_recipients where record_id = root.record_id), '[]'::jsonb),
				  'deliveries', coalesce((select jsonb_agg(jsonb_build_array(
				    delivery_id, notification_id, recipient_user_id, channel, delivery_state, record_fence_epoch
				  ) order by delivery_id) from public.record_notification_deliveries where record_id = root.record_id), '[]'::jsonb),
				  'delivery_attempts', coalesce((select jsonb_agg(jsonb_build_array(
				    attempt_id, delivery_id, attempt_no, outcome, reason_code, record_fence_epoch
				  ) order by attempt_id) from public.record_notification_delivery_attempts where record_id = root.record_id), '[]'::jsonb)
				) as dependency_material,
				jsonb_build_object(
				  'actions', (select count(*) from public.record_actions where record_id = root.record_id),
				  'action_events', (select count(*) from public.record_action_events where record_id = root.record_id),
				  'comments', (select count(*) from public.record_comments where record_id = root.record_id),
				  'comment_revisions', (select count(*) from public.record_comment_revisions where record_id = root.record_id),
				  'comment_tombstones', (select count(*) from public.record_comment_tombstones where record_id = root.record_id),
				  'comment_replies', (select count(*) from public.record_comment_replies where record_id = root.record_id),
				  'comment_mentions', (select count(*) from public.record_comment_mentions where record_id = root.record_id),
				  'followers', (select count(*) from public.record_followers where record_id = root.record_id),
				  'notifications', (select count(*) from public.record_notifications where record_id = root.record_id),
				  'recipients', (select count(*) from public.record_notification_recipients where record_id = root.record_id),
				  'deliveries', (select count(*) from public.record_notification_deliveries where record_id = root.record_id),
				  'delivery_attempts', (select count(*) from public.record_notification_delivery_attempts where record_id = root.record_id)
				) as impact_material,
				(select count(*) from public.record_notification_deliveries
				  where record_id = root.record_id and delivery_state = 'sent') as delivered_count,
				(select count(*) from public.record_notification_deliveries
				  where record_id = root.record_id and delivery_state = 'unknown_outcome') as possible_count
				from root
			)
			select pg_catalog.convert_to(dependency_material::text, 'UTF8'),
			       pg_catalog.convert_to(impact_material::text, 'UTF8'),
			       delivered_count, possible_count
			from material`,
			target.Object.ProjectID, target.Object.ObjectID, target.CurrentRevisionID,
			int64(target.LockVersion), int64(target.AuthorizationEpoch), int64(target.ContentDeliveryEpoch),
		).Scan(&dependencyMaterial, &impactMaterial, &deliveredCount, &possibleCount)
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.ErrDeletionPreviewStale
		}
		if err != nil {
			return fmt.Errorf("preview collaboration deletion: %w", err)
		}
		if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 || deliveredCount < 0 || possibleCount < 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		snapshot = recorddeletion.AdapterPreviewSnapshot{
			DependencyDigest: digestCollaborationDeletionBytes(
				collaborationDeletionPreviewDigestDomainV1, target.DependencyGraphDigest[:], dependencyMaterial,
			),
			ImpactDigest:    digestCollaborationDeletionBytes(collaborationDeletionImpactDigestDomainV1, impactMaterial),
			SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
		}
		if deliveredCount > 0 {
			snapshot.SurvivingCopies = append(snapshot.SurvivingCopies, recorddeletion.AdapterSurvivingCopy{
				Kind: recorddeletion.SurvivingCopyKindDeliveredNotification, CopyCount: uint64(deliveredCount),
			})
		}
		if possibleCount > 0 {
			snapshot.SurvivingCopies = append(snapshot.SurvivingCopies, recorddeletion.AdapterSurvivingCopy{
				Kind: recorddeletion.SurvivingCopyKindPossibleExternalDelivery, CopyCount: uint64(possibleCount),
			})
		}
		if err := snapshot.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	return snapshot, err
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordCollaboration(
	ctx context.Context,
	command recordcollaboration.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		command.Operation.LedgerSequence > math.MaxInt64 || uint64(command.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := validateLockedEvidencePurgeOperation(ctx, transaction.tx, command.Operation); err != nil {
			return err
		}
		existing, found, err := loadCollaborationPurgeReceipt(ctx, transaction.tx, command)
		if err != nil {
			return err
		}
		if found {
			if err := assertRecordCollaborationSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
				return err
			}
			receipt = existing
			return nil
		}
		var removed int64
		encoded, err := encodeCollaborationDeleteCommand(collaborationPurgeFunctionCommand{
			OperationID: command.Operation.OperationID, ReservationID: command.Operation.ReservationID,
			ProjectID: command.Operation.Object.ProjectID, RecordID: command.Operation.Object.ObjectID,
			FenceEpoch: int64(command.Operation.FenceEpoch), LedgerSequence: int64(command.Operation.LedgerSequence),
			LedgerEntryHash: encodeCollaborationLedgerHash(command.Operation.LedgerEntryHash),
		})
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := transaction.tx.QueryRow(ctx, `
			select public.record_collaboration_purge($1)`, encoded,
		).Scan(&removed); err != nil {
			return fmt.Errorf("purge record collaboration through constrained function: %w", err)
		}
		if removed < 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := assertRecordCollaborationSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID); err != nil {
			return err
		}
		removedRows := uint64(removed)
		receiptDigest := collaborationDeletionReceiptDigest(command, removedRows)
		var verifiedAt time.Time
		if err := transaction.tx.QueryRow(ctx, `
			insert into public.record_collaboration_purge_receipts (
				operation_id, removed_surface_digest, receipt_digest,
				removed_row_count, verified_absent_at
			) values ($1, $2, $3, $4, transaction_timestamp())
			returning verified_absent_at`, command.Operation.OperationID, command.SurfaceDigest[:],
			receiptDigest[:], removed,
		).Scan(&verifiedAt); err != nil {
			return fmt.Errorf("insert collaboration purge receipt: %w", err)
		}
		if verifiedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		receipt = recorddeletion.AdapterPurgeReceipt{
			AdapterName: recorddeletion.AdapterNameRecordCollaboration, OperationID: command.Operation.OperationID,
			SurfaceDigest: command.SurfaceDigest, ReceiptDigest: receiptDigest,
			RemovedRowCount: removedRows, VerifiedAbsentAt: verifiedAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.VerifyRecordCollaborationPurge(ctx, command, receipt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordCollaborationPurge(
	ctx context.Context,
	command recordcollaboration.DeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordCollaboration ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != collaborationDeletionReceiptDigest(command, receipt.RemovedRowCount) {
		return recordcollaboration.ErrInvalidDeletionAdapter
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
			from public.record_collaboration_purge_receipts
			where operation_id = $1 and adapter_name = 'record_collaboration'`,
			command.Operation.OperationID,
		).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if removed < 0 || uint64(removed) != receipt.RemovedRowCount ||
			!verifiedAt.Equal(receipt.VerifiedAbsentAt) ||
			!equalCollaborationDeletionDigest(rawSurface, receipt.SurfaceDigest) ||
			!equalCollaborationDeletionDigest(rawReceipt, receipt.ReceiptDigest) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return assertRecordCollaborationSurfacesAbsent(ctx, transaction.tx, command.Operation.Object.ObjectID)
	})
}

func assertRecordCollaborationSurfacesAbsent(ctx context.Context, tx pgx.Tx, recordID string) error {
	var remaining int64
	if err := tx.QueryRow(ctx, `
		select
		  (select count(*) from public.record_action_events where record_id = $1) +
		  (select count(*) from public.record_actions where record_id = $1) +
		  (select count(*) from public.record_comment_mentions where record_id = $1) +
		  (select count(*) from public.record_comment_replies where record_id = $1) +
		  (select count(*) from public.record_comment_revisions where record_id = $1) +
		  (select count(*) from public.record_comment_tombstones where record_id = $1) +
		  (select count(*) from public.record_comments where record_id = $1) +
		  (select count(*) from public.record_followers where record_id = $1) +
		  (select count(*) from public.record_notification_deliveries where record_id = $1) +
		  (select count(*) from public.record_notification_delivery_attempts where record_id = $1) +
		  (select count(*) from public.record_notification_recipients where record_id = $1) +
		  (select count(*) from public.record_notifications where record_id = $1)`, recordID,
	).Scan(&remaining); err != nil {
		return fmt.Errorf("verify record collaboration purge absence: %w", err)
	}
	if remaining != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func loadCollaborationPurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command recordcollaboration.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, bool, error) {
	var rawSurface, rawReceipt []byte
	var removed int64
	var verifiedAt time.Time
	err := tx.QueryRow(ctx, `
		select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
		from public.record_collaboration_purge_receipts
		where operation_id = $1 and adapter_name = 'record_collaboration'`,
		command.Operation.OperationID,
	).Scan(&rawSurface, &rawReceipt, &removed, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPurgeReceipt{}, false, nil
	}
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, false, fmt.Errorf("load collaboration purge receipt: %w", err)
	}
	if removed < 0 || verifiedAt.IsZero() || !equalCollaborationDeletionDigest(rawSurface, command.SurfaceDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	receipt := recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordCollaboration, OperationID: command.Operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: collaborationDeletionReceiptDigest(command, uint64(removed)),
		RemovedRowCount: uint64(removed), VerifiedAbsentAt: verifiedAt.UTC(),
	}
	if !equalCollaborationDeletionDigest(rawReceipt, receipt.ReceiptDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return receipt, true, nil
}

func collaborationDeletionReceiptDigest(
	command recordcollaboration.DeletionCommand,
	removed uint64,
) [sha256.Size]byte {
	return digestCollaborationDeletionBytes(
		collaborationDeletionReceiptDigestDomainV1,
		[]byte(command.Operation.OperationID),
		[]byte(command.Operation.Object.ProjectID),
		[]byte(command.Operation.Object.ObjectID),
		command.SurfaceDigest[:],
		collaborationDeletionUint64(removed),
	)
}

func digestCollaborationDeletionStrings(domain string, values ...string) [sha256.Size]byte {
	encoded := make([][]byte, len(values))
	for index, value := range values {
		encoded[index] = []byte(value)
	}
	return digestCollaborationDeletionBytes(domain, encoded...)
}

func digestCollaborationDeletionBytes(domain string, values ...[]byte) [sha256.Size]byte {
	hasher := sha256.New()
	writeCollaborationDeletionDigestField(hasher, []byte(domain))
	for _, value := range values {
		writeCollaborationDeletionDigestField(hasher, value)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeCollaborationDeletionDigestField(hasher interface{ Write([]byte) (int, error) }, value []byte) {
	_, _ = hasher.Write(collaborationDeletionUint64(uint64(len(value))))
	_, _ = hasher.Write(value)
}

func collaborationDeletionUint64(value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

func equalCollaborationDeletionDigest(raw []byte, digest [sha256.Size]byte) bool {
	if len(raw) != sha256.Size {
		return false
	}
	var difference byte
	for index := range raw {
		difference |= raw[index] ^ digest[index]
	}
	return difference == 0
}

var _ recordcollaboration.DeletionStore = (*PostgresRecordDeletionRepository)(nil)
