package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
)

const (
	collaborationDeletionHealthDigestDomainV2  = "houfeng.record-collaboration.deletion-health.v2"
	collaborationDeletionPreviewDigestDomainV2 = "houfeng.record-collaboration.deletion-preview.v2"
	collaborationDeletionImpactDigestDomainV2  = "houfeng.record-collaboration.deletion-impact.v2"
)

func (repository *PostgresRecordDeletionRepository) CollaborationDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		health, err = repository.collaborationDeletionHealthInTransaction(ctx, transaction.tx)
		return err
	})
	return health, err
}

func (repository *PostgresRecordDeletionRepository) collaborationDeletionHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	return collaborationDeletionHealthInTransaction(ctx, tx)
}

func collaborationDeletionHealthInTransaction(ctx context.Context, tx pgx.Tx) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	surfaces := recorddeletion.RecordCollaborationSurfaceNames()
	names := make([]string, 0, len(surfaces)+3)
	for _, surface := range surfaces {
		names = append(names, string(surface))
	}
	names = append(names, "record_notification_delivery_facts", "record_notification_delivery_completions", "record_notification_delivery_handoffs")
	var count int64
	if err := tx.QueryRow(ctx, `
			select count(*)::bigint
			from pg_catalog.pg_class as relation
			join pg_catalog.pg_namespace as namespace on namespace.oid = relation.relnamespace
			where namespace.nspname = 'public'
			  and relation.relkind in ('r', 'p')
			  and relation.relname = any($1::text[])`, names).Scan(&count); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read collaboration deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireCollaborationDeletionFunctions(ctx, tx); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(
		true, 2, digestRecordPurgeStrings(collaborationDeletionHealthDigestDomainV2, names...),
	)
	if err != nil {
		return health, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

// Catalog readiness is independent of which login executes a command. Role
// admission and purpose-specific EXECUTE grants are checked at their boundary.
func requireCollaborationDeletionFunctions(ctx context.Context, tx pgx.Tx) error {
	var invalid int64
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint
		from (values
		  ('public.record_collaboration_purge_v2(bytea)',
		   array['removed_surface_digest','receipt_digest','removed_row_count','verified_absent_at'],
		   array['bytea'::regtype::oid,'bytea'::regtype::oid,'bigint'::regtype::oid,'timestamptz'::regtype::oid]),
		  ('public.record_collaboration_purge_verify_v2(bytea)',
		   array['removed_surface_digest','receipt_digest','removed_row_count','verified_absent_at'],
		   array['bytea'::regtype::oid,'bytea'::regtype::oid,'bigint'::regtype::oid,'timestamptz'::regtype::oid]),
		  ('public.record_outbox_drain_v2(bytea)',
		   array['drained_count','remaining'], array['bigint'::regtype::oid,'boolean'::regtype::oid]),
		  ('public.record_collaboration_start_inventory(text,bigint)',
		   array['start_digest','start_count','mismatched'], array['bytea'::regtype::oid,'bigint'::regtype::oid,'boolean'::regtype::oid]),
		  ('public.record_collaboration_delivery_inventory(text,bigint)',
		   array['notification_id','delivery_id','classification','evidence_digest','mismatched'],
		   array['text'::regtype::oid,'text'::regtype::oid,'text'::regtype::oid,'bytea'::regtype::oid,'boolean'::regtype::oid])
		) as required(signature, output_names, output_types)
		left join pg_catalog.pg_proc as proc on proc.oid = pg_catalog.to_regprocedure(required.signature)
		where proc.oid is null or proc.prosecdef is not true or proc.proretset is not true
		   or proc.prorettype <> 'record'::regtype
		   or not coalesce(proc.proconfig @> array['search_path=pg_catalog'], false)
		   or proc.proargnames[(proc.pronargs::int + 1):] is distinct from required.output_names
		   or proc.proallargtypes[(proc.pronargs::int + 1):] is distinct from required.output_types
		   or proc.proargmodes[(proc.pronargs::int + 1):] is distinct from
		      array_fill('t'::"char", array[cardinality(required.output_names)])`).Scan(&invalid); err != nil {
		return fmt.Errorf("read collaboration deletion function contracts: %w", err)
	}
	if invalid != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func (repository *PostgresRecordDeletionRepository) PreviewCollaborationDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || !validClientDeletionPreviewTarget(target) {
		return recorddeletion.AdapterPreviewSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var snapshot recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		snapshot, err = previewCollaborationDeletionInTransaction(ctx, transaction.tx, target)
		return err
	})
	return snapshot, err
}

func previewCollaborationDeletionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || !validClientDeletionPreviewTarget(target) {
		return recorddeletion.AdapterPreviewSnapshot{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	var dependencyMaterial, impactMaterial []byte
	var deliveredCount, possibleCount int64
	var invalidInventory bool
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
			), responsibilities as materialized (
				select inventory.* from root
				cross join lateral public.record_collaboration_delivery_inventory(root.record_id, $6) as inventory
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
				  , 'delivery_starts', encode(starts.start_digest, 'hex')
				  , 'delivery_responsibilities', coalesce((select jsonb_agg(jsonb_build_array(
				    notification_id, delivery_id, classification, encode(evidence_digest, 'hex')
				  ) order by notification_id, delivery_id) from responsibilities), '[]'::jsonb)
				  , 'notification_audit_summaries', coalesce((select jsonb_agg(jsonb_build_array(
				    notification_id, event_kind, subject_kind, source_version, recipient_count,
				    delivery_count, sent_count, unknown_count, permanent_failed_count, record_fence_epoch
				  ) order by notification_id) from public.record_notification_audit_summaries where record_id = root.record_id), '[]'::jsonb)
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
				  , 'retained_delivery_starts', starts.start_count
				  , 'notification_audit_summaries', (select count(*) from public.record_notification_audit_summaries where record_id = root.record_id)
				) as impact_material,
				((select count(*) from responsibilities where classification = 'sent') +
				 (select coalesce(sum(sent_count), 0) from public.record_notification_audit_summaries
				  where record_id = root.record_id)) as delivered_count,
				((select count(*) from responsibilities where classification = 'possible_external') +
				 (select coalesce(sum(unknown_count), 0) from public.record_notification_audit_summaries
				  where record_id = root.record_id)) as possible_count,
				(starts.mismatched or exists (
				  select 1 from responsibilities
				  where mismatched is distinct from false or notification_id is null or notification_id = ''
				     or delivery_id is null or delivery_id = ''
				     or classification is null or classification not in ('sent', 'possible_external', 'permanent_failure', 'none')
				     or evidence_digest is null or octet_length(evidence_digest) <> 32
				) or (select count(*) <> count(distinct delivery_id) from responsibilities)
				  or exists (select 1 from public.record_notification_audit_summaries as audit
				    where audit.record_id = root.record_id and (
				      exists (select 1 from responsibilities where notification_id = audit.notification_id)
				      or exists (select 1 from public.record_notifications as notification
				        where notification.record_id = root.record_id and notification.notification_id = audit.notification_id)
				    ))) as invalid_inventory
				from root
				cross join lateral public.record_collaboration_start_inventory(root.record_id, $6) as starts
			)
			select pg_catalog.convert_to(dependency_material::text, 'UTF8'),
			       pg_catalog.convert_to(impact_material::text, 'UTF8'),
			       delivered_count, possible_count, invalid_inventory
			from material`,
		target.Object.ProjectID, target.Object.ObjectID, target.CurrentRevisionID,
		int64(target.LockVersion), int64(target.AuthorizationEpoch), int64(target.ContentDeliveryEpoch),
	).Scan(&dependencyMaterial, &impactMaterial, &deliveredCount, &possibleCount, &invalidInventory)
	if errors.Is(err, pgx.ErrNoRows) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview collaboration deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 || deliveredCount < 0 || possibleCount < 0 || invalidInventory {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	snapshot := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(
			collaborationDeletionPreviewDigestDomainV2, target.DependencyGraphDigest[:], dependencyMaterial,
		),
		ImpactDigest:    digestRecordPurgeBytes(collaborationDeletionImpactDigestDomainV2, impactMaterial),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
	}
	if deliveredCount > 0 {
		snapshot.SurvivingCopies = append(snapshot.SurvivingCopies, recorddeletion.KnownAdapterSurvivingCopy(
			recorddeletion.SurvivingCopyKindDeliveredNotification, uint64(deliveredCount),
		))
	}
	if possibleCount > 0 {
		snapshot.SurvivingCopies = append(snapshot.SurvivingCopies, recorddeletion.KnownAdapterSurvivingCopy(
			recorddeletion.SurvivingCopyKindPossibleExternalDelivery, uint64(possibleCount),
		))
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return snapshot, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordCollaboration(
	ctx context.Context,
	command recordcollaboration.DeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recordcollaboration.ErrInvalidDeletionAdapter
	}
	if err := repository.drainCollaborationOutbox(ctx, command); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		receipt, err = runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_collaboration_purge_v2($1)`, recorddeletion.AdapterNameRecordCollaboration,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

// Each transaction commits at most one bounded batch. A later owner can resume
// after an error or unknown commit, but only with its own exact claim.
func (repository *PostgresRecordDeletionRepository) drainCollaborationOutbox(
	ctx context.Context,
	command recordcollaboration.DeletionCommand,
) error {
	expires := command.OwnerClaim.Owner.ExpiresAt.UTC()
	expiresText := expires.Format(time.RFC3339Nano)
	parsed, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil || !parsed.Equal(expires) || expires.Nanosecond()%1000 != 0 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	encoded, err := encodeRecordPurgeCommand(clientComparisonPurgeEnvelope{
		OperationID: command.Operation.OperationID, RecordID: command.Operation.Object.ObjectID,
		ReservationID: command.Operation.ReservationID, FenceEpoch: uint64(command.Operation.FenceEpoch),
		OwnerID: command.OwnerClaim.Owner.OwnerID, OwnerGeneration: command.OwnerClaim.Owner.Generation,
		OwnerExpiresAt: expiresText, SurfaceDigest: hex.EncodeToString(command.SurfaceDigest[:]),
	})
	if err != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	for {
		var drained int64
		var remaining bool
		err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			if err := transaction.tx.QueryRow(ctx, `select drained_count, remaining from public.record_outbox_drain_v2($1)`, encoded).Scan(&drained, &remaining); err != nil {
				return fmt.Errorf("drain collaboration outbox: %w", err)
			}
			if drained < 0 || drained > 100 || (remaining && drained == 0) {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !remaining {
			return nil
		}
	}
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordCollaborationPurge(
	ctx context.Context,
	command recordcollaboration.DeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recordcollaboration.ErrInvalidDeletionAdapter
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(recorddeletion.AdapterNameRecordCollaboration, recorddeletion.RecordCollaborationSurfaceNames())
	target := recorddeletion.PurgeTarget{Operation: command.Operation, OwnerClaim: command.OwnerClaim}
	if err != nil || recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, descriptor) != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		actual, err := runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_collaboration_purge_verify_v2($1)`, recorddeletion.AdapterNameRecordCollaboration,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}

var _ recordcollaboration.DeletionStore = (*PostgresRecordDeletionRepository)(nil)
