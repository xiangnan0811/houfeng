package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
)

// PostgresRecordCollaborationProvider is transactionless by construction.
// Activity, portability, backup, and restore callers must supply the same
// caller-owned transaction used by their aggregate operation.
type PostgresRecordCollaborationProvider struct{}

func NewPostgresRecordCollaborationProvider() *PostgresRecordCollaborationProvider {
	return &PostgresRecordCollaborationProvider{}
}

func (provider *PostgresRecordCollaborationProvider) ReadCollaborationActivityFacts(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
) ([]recordcollaboration.ActivityFact, error) {
	if provider == nil || ctx == nil || nilCollaborationDependency(tx) || binding.Validate() != nil {
		return nil, recordcollaboration.ErrInvalidActivityProvider
	}
	if err := assertCollaborationProviderBinding(ctx, tx, binding); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		select activity_id, record_id, coalesce(revision_id, ''), event_kind,
		       source_event_id, source_version, actor_id, authorization_epoch,
		       record_lock_version, event_at
		from public.record_domain_activities
		where project_id = $1 and record_id = $2
		  and event_kind = any($3::text[])
		order by event_at, activity_id`, binding.ProjectID(), binding.RecordID(), collaborationActivityFactKinds())
	if err != nil {
		return nil, fmt.Errorf("query collaboration activity facts: %w", err)
	}
	defer rows.Close()
	facts := make([]recordcollaboration.ActivityFact, 0)
	for rows.Next() {
		var fact recordcollaboration.ActivityFact
		var sourceVersion, authorizationEpoch, lockVersion int64
		if err := rows.Scan(
			&fact.ActivityID, &fact.RecordID, &fact.RevisionID, &fact.Kind,
			&fact.SourceEventID, &sourceVersion, &fact.ActorID, &authorizationEpoch,
			&lockVersion, &fact.EventAt,
		); err != nil || sourceVersion <= 0 || authorizationEpoch < 0 || lockVersion <= 0 {
			return nil, recordcollaboration.ErrInvalidActivityFact
		}
		fact.SourceVersion = uint64(sourceVersion)
		fact.AuthorizationEpoch = uint64(authorizationEpoch)
		fact.RecordLockVersion = uint64(lockVersion)
		fact.EventAt = fact.EventAt.UTC()
		if fact.Validate() != nil {
			return nil, recordcollaboration.ErrInvalidActivityFact
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collaboration activity facts: %w", err)
	}
	return facts, nil
}

func (provider *PostgresRecordCollaborationProvider) BackupCollaboration(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
) (recordcollaboration.PortabilitySnapshot, error) {
	if provider == nil || ctx == nil || nilCollaborationDependency(tx) || binding.Validate() != nil {
		return recordcollaboration.PortabilitySnapshot{}, recordcollaboration.ErrInvalidPortabilityAdapter
	}
	if err := assertCollaborationProviderBinding(ctx, tx, binding); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, err
	}
	if err := assertCollaborationPortabilityRowsBound(ctx, tx, binding); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, err
	}
	snapshot := emptyCollaborationPortabilitySnapshot()
	if err := backupPortableActions(ctx, tx, binding, &snapshot); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, err
	}
	if err := backupPortableComments(ctx, tx, binding, &snapshot); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, err
	}
	if err := backupPortableFollowersAndAudits(ctx, tx, binding, &snapshot); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return recordcollaboration.PortabilitySnapshot{}, fmt.Errorf("%w: persisted collaboration backup", err)
	}
	return snapshot.Clone(), nil
}

func (provider *PostgresRecordCollaborationProvider) RestoreCollaboration(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	snapshot recordcollaboration.PortabilitySnapshot,
) error {
	if provider == nil || ctx == nil || nilCollaborationDependency(tx) || binding.Validate() != nil || snapshot.Validate() != nil {
		return recordcollaboration.ErrInvalidPortabilityAdapter
	}
	if err := assertCollaborationProviderBinding(ctx, tx, binding); err != nil {
		return err
	}
	if err := assertCollaborationPortabilityRowsBound(ctx, tx, binding); err != nil {
		return err
	}
	if err := restorePortableActions(ctx, tx, binding, snapshot); err != nil {
		return err
	}
	if err := restorePortableComments(ctx, tx, binding, snapshot); err != nil {
		return err
	}
	if err := restorePortableFollowers(ctx, tx, binding, snapshot); err != nil {
		return err
	}
	// NotificationAudits are disclosure-only counts. Replaying them into the
	// live inbox/delivery tables would recreate recipients, scoped binding IDs,
	// or sendable work that the typed backup intentionally does not contain.
	restored, err := provider.BackupCollaboration(ctx, tx, binding)
	if err != nil {
		return err
	}
	want := snapshot.Clone()
	want.NotificationAudits = make([]recordcollaboration.PortableNotificationAudit, 0)
	restored.NotificationAudits = make([]recordcollaboration.PortableNotificationAudit, 0)
	if !reflect.DeepEqual(restored, want) {
		return recordcollaboration.ErrInvalidPortabilitySnapshot
	}
	return nil
}

func assertCollaborationProviderBinding(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
) error {
	if err := assertRecordReadFence(ctx, tx, binding.RecordID()); err != nil {
		return err
	}
	persisted, err := loadCollaborationRecordReadFenceBinding(ctx, tx, binding.RecordID())
	if err != nil {
		return fmt.Errorf("read collaboration provider fence: %w", err)
	}
	if persisted.ProjectID() != binding.ProjectID() || persisted.RecordID() != binding.RecordID() ||
		persisted.Epoch() != binding.Epoch() {
		return recordcollaboration.ErrInvalidRecordFenceBinding
	}
	return nil
}

func assertCollaborationPortabilityRowsBound(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
) error {
	var mismatched bool
	if err := tx.QueryRow(ctx, `
		select
		  exists (select 1 from public.record_actions where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_action_events where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_comments where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_comment_revisions where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_comment_tombstones where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_comment_replies where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_comment_mentions where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_followers where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_notifications where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_notification_recipients where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_notification_deliveries where record_id = $1 and record_fence_epoch <> $2) or
		  exists (select 1 from public.record_notification_delivery_attempts where record_id = $1 and record_fence_epoch <> $2)`,
		binding.RecordID(), int64(binding.Epoch()),
	).Scan(&mismatched); err != nil {
		return fmt.Errorf("verify collaboration portability row fences: %w", err)
	}
	if mismatched {
		return recordcollaboration.ErrInvalidRecordFenceBinding
	}
	return nil
}

func collaborationActivityFactKinds() []string {
	return []string{
		string(recordcollaboration.ActivityFactRecordOwnerChanged),
		string(recordcollaboration.ActivityFactRecordParticipantChanged),
		string(recordcollaboration.ActivityFactRecordFollowUpChanged),
		string(recordcollaboration.ActivityFactActionCreated),
		string(recordcollaboration.ActivityFactActionUpdated),
		string(recordcollaboration.ActivityFactActionCompleted),
		string(recordcollaboration.ActivityFactActionCancelled),
		string(recordcollaboration.ActivityFactActionReopened),
		string(recordcollaboration.ActivityFactCommentCreated),
		string(recordcollaboration.ActivityFactCommentEdited),
		string(recordcollaboration.ActivityFactCommentRedacted),
	}
}

func emptyCollaborationPortabilitySnapshot() recordcollaboration.PortabilitySnapshot {
	return recordcollaboration.PortabilitySnapshot{
		Actions:            make([]recordcollaboration.PortableAction, 0),
		ActionEvents:       make([]recordcollaboration.PortableActionEvent, 0),
		Comments:           make([]recordcollaboration.PortableComment, 0),
		CommentRevisions:   make([]recordcollaboration.PortableCommentRevision, 0),
		Tombstones:         make([]recordcollaboration.PortableCommentTombstone, 0),
		Replies:            make([]recordcollaboration.PortableCommentReply, 0),
		Mentions:           make([]recordcollaboration.PortableCommentMention, 0),
		Followers:          make([]recordcollaboration.PortableFollower, 0),
		NotificationAudits: make([]recordcollaboration.PortableNotificationAudit, 0),
	}
}

func backupPortableActions(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	snapshot *recordcollaboration.PortabilitySnapshot,
) error {
	rows, err := tx.Query(ctx, `
		select action_id, coalesce(subject_revision_id, ''), action_version,
		       title, details, status, coalesce(assignee_id, ''), due_at,
		       completed_at, created_by, updated_by, created_at, updated_at
		from public.record_actions
		where project_id = $1 and record_id = $2 and record_fence_epoch = $3
		order by action_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup actions: %w", err)
	}
	for rows.Next() {
		var action recordcollaboration.PortableAction
		var version int64
		if err := rows.Scan(
			&action.ActionID, &action.SubjectRevisionID, &version, &action.Title, &action.Details,
			&action.Status, &action.AssigneeID, &action.DueAt, &action.CompletedAt,
			&action.CreatedBy, &action.UpdatedBy, &action.CreatedAt, &action.UpdatedAt,
		); err != nil || version <= 0 {
			rows.Close()
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		action.Version = uint64(version)
		normalizePortableActionTimes(&action)
		snapshot.Actions = append(snapshot.Actions, action)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate collaboration backup actions: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		select action_event_id, action_id, action_version, event_kind,
		       previous_status, current_status, actor_id, coalesce(assignee_id, ''),
		       occurred_at, created_at
		from public.record_action_events
		where project_id = $1 and record_id = $2 and record_fence_epoch = $3
		order by action_event_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup action events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event recordcollaboration.PortableActionEvent
		var version int64
		var previous *string
		if err := rows.Scan(
			&event.EventID, &event.ActionID, &version, &event.Kind, &previous,
			&event.CurrentStatus, &event.ActorID, &event.AssigneeID, &event.OccurredAt, &event.CreatedAt,
		); err != nil || version <= 0 {
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		event.Version = uint64(version)
		if previous != nil {
			status := recordcollaboration.ActionStatus(*previous)
			event.PreviousStatus = &status
		}
		event.OccurredAt = event.OccurredAt.UTC()
		event.CreatedAt = event.CreatedAt.UTC()
		snapshot.ActionEvents = append(snapshot.ActionEvents, event)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate collaboration backup action events: %w", err)
	}
	return nil
}

func backupPortableComments(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	snapshot *recordcollaboration.PortabilitySnapshot,
) error {
	rows, err := tx.Query(ctx, `
		select comment_id, author_id, comment_version, comment_state,
		       body_markdown, render_model, body_digest, coalesce(tombstone_id, ''),
		       created_at, updated_at, redacted_at
		from public.record_comments
		where project_id = $1 and record_id = $2 and record_fence_epoch = $3
		order by comment_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup comments: %w", err)
	}
	for rows.Next() {
		var comment recordcollaboration.PortableComment
		var version int64
		var body *string
		var render, digest []byte
		if err := rows.Scan(
			&comment.CommentID, &comment.AuthorID, &version, &comment.State, &body,
			&render, &digest, &comment.TombstoneID, &comment.CreatedAt, &comment.UpdatedAt, &comment.RedactedAt,
		); err != nil || version <= 0 || !decodePortableCommentContent(body, render, digest, &comment.BodyMarkdown, &comment.RenderModel, &comment.BodyDigest) {
			rows.Close()
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		comment.Version = uint64(version)
		comment.CreatedAt = comment.CreatedAt.UTC()
		comment.UpdatedAt = comment.UpdatedAt.UTC()
		comment.RedactedAt = utcPortableTime(comment.RedactedAt)
		snapshot.Comments = append(snapshot.Comments, comment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate collaboration backup comments: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		select comment_revision_id, comment_id, comment_version, edited_by,
		       body_markdown, render_model, body_digest, coalesce(tombstone_id, ''),
		       created_at, redacted_at
		from public.record_comment_revisions
		where project_id = $1 and record_id = $2 and record_fence_epoch = $3
		order by comment_revision_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup comment revisions: %w", err)
	}
	for rows.Next() {
		var revision recordcollaboration.PortableCommentRevision
		var version int64
		var body *string
		var render, digest []byte
		if err := rows.Scan(
			&revision.RevisionID, &revision.CommentID, &version, &revision.EditedBy,
			&body, &render, &digest, &revision.TombstoneID, &revision.CreatedAt, &revision.RedactedAt,
		); err != nil || version <= 0 || !decodePortableCommentContent(body, render, digest, &revision.BodyMarkdown, &revision.RenderModel, &revision.BodyDigest) {
			rows.Close()
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		revision.Version = uint64(version)
		revision.CreatedAt = revision.CreatedAt.UTC()
		revision.RedactedAt = utcPortableTime(revision.RedactedAt)
		snapshot.CommentRevisions = append(snapshot.CommentRevisions, revision)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate collaboration backup comment revisions: %w", err)
	}
	rows.Close()

	if err := queryPortableTombstones(ctx, tx, binding, snapshot); err != nil {
		return err
	}
	if err := queryPortableReplies(ctx, tx, binding, snapshot); err != nil {
		return err
	}
	return queryPortableMentions(ctx, tx, binding, snapshot)
}

func queryPortableTombstones(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot *recordcollaboration.PortabilitySnapshot) error {
	rows, err := tx.Query(ctx, `
		select tombstone_id, comment_id, tombstone_version, deleted_by, reason_code, deleted_at
		from public.record_comment_tombstones
		where record_id = $1 and record_fence_epoch = $2
		order by tombstone_id`, binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup tombstones: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tombstone recordcollaboration.PortableCommentTombstone
		var version int64
		if err := rows.Scan(&tombstone.TombstoneID, &tombstone.CommentID, &version, &tombstone.DeletedBy, &tombstone.ReasonCode, &tombstone.DeletedAt); err != nil || version <= 0 {
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		tombstone.Version = uint64(version)
		tombstone.DeletedAt = tombstone.DeletedAt.UTC()
		snapshot.Tombstones = append(snapshot.Tombstones, tombstone)
	}
	return rows.Err()
}

func queryPortableReplies(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot *recordcollaboration.PortabilitySnapshot) error {
	rows, err := tx.Query(ctx, `
		select child_comment_id, parent_comment_id, created_at
		from public.record_comment_replies
		where record_id = $1 and record_fence_epoch = $2
		order by child_comment_id`, binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var reply recordcollaboration.PortableCommentReply
		if err := rows.Scan(&reply.ChildCommentID, &reply.ParentCommentID, &reply.CreatedAt); err != nil {
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		reply.CreatedAt = reply.CreatedAt.UTC()
		snapshot.Replies = append(snapshot.Replies, reply)
	}
	return rows.Err()
}

func queryPortableMentions(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot *recordcollaboration.PortabilitySnapshot) error {
	rows, err := tx.Query(ctx, `
		select comment_id, comment_version, mentioned_user_id, created_at
		from public.record_comment_mentions
		where record_id = $1 and record_fence_epoch = $2
		order by comment_id, comment_version, mentioned_user_id`, binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup mentions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mention recordcollaboration.PortableCommentMention
		var version int64
		if err := rows.Scan(&mention.CommentID, &version, &mention.MentionedUser, &mention.CreatedAt); err != nil || version <= 0 {
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		mention.CommentVersion = uint64(version)
		mention.CreatedAt = mention.CreatedAt.UTC()
		snapshot.Mentions = append(snapshot.Mentions, mention)
	}
	return rows.Err()
}

func backupPortableFollowersAndAudits(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot *recordcollaboration.PortabilitySnapshot) error {
	rows, err := tx.Query(ctx, `
		select user_id, follower_version, manual_preference,
		       follows_author, follows_owner, follows_participant,
		       follows_comment, follows_mention, follows_action,
		       created_at, updated_at
		from public.record_followers
		where project_id = $1 and record_id = $2 and record_fence_epoch = $3
		order by user_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup followers: %w", err)
	}
	for rows.Next() {
		var follower recordcollaboration.PortableFollower
		var version int64
		if err := rows.Scan(
			&follower.UserID, &version, &follower.Preference,
			&follower.Sources.Author, &follower.Sources.Owner, &follower.Sources.Participant,
			&follower.Sources.Comment, &follower.Sources.Mention, &follower.Sources.Action,
			&follower.CreatedAt, &follower.UpdatedAt,
		); err != nil || version <= 0 {
			rows.Close()
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		follower.Version = uint64(version)
		follower.CreatedAt = follower.CreatedAt.UTC()
		follower.UpdatedAt = follower.UpdatedAt.UTC()
		snapshot.Followers = append(snapshot.Followers, follower)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate collaboration backup followers: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		select notification.notification_id, notification.event_kind,
		       notification.subject_kind, notification.source_version, notification.event_at,
		       count(distinct recipient.recipient_user_id),
		       count(distinct delivery.delivery_id),
		       count(distinct delivery.delivery_id) filter (where delivery.delivery_state = 'sent'),
		       count(distinct delivery.delivery_id) filter (where delivery.delivery_state = 'unknown_outcome'),
		       count(distinct delivery.delivery_id) filter (where delivery.delivery_state = 'permanent_failure')
		from public.record_notifications as notification
		left join public.record_notification_recipients as recipient
		  on recipient.notification_id = notification.notification_id
		 and recipient.record_id = notification.record_id
		left join public.record_notification_deliveries as delivery
		  on delivery.notification_id = notification.notification_id
		 and delivery.record_id = notification.record_id
		where notification.project_id = $1 and notification.record_id = $2
		  and notification.record_fence_epoch = $3
		group by notification.notification_id, notification.event_kind,
		         notification.subject_kind, notification.source_version, notification.event_at
		order by notification.notification_id`, binding.ProjectID(), binding.RecordID(), int64(binding.Epoch()))
	if err != nil {
		return fmt.Errorf("query collaboration backup notification audits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var audit recordcollaboration.PortableNotificationAudit
		var sourceVersion, recipients, deliveries, sent, unknown, permanent int64
		if err := rows.Scan(
			&audit.NotificationID, &audit.Kind, &audit.SubjectKind, &sourceVersion, &audit.EventAt,
			&recipients, &deliveries, &sent, &unknown, &permanent,
		); err != nil || sourceVersion <= 0 || recipients < 0 || deliveries < 0 || sent < 0 || unknown < 0 || permanent < 0 {
			return recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		audit.SourceVersion = uint64(sourceVersion)
		audit.RecipientCount = uint64(recipients)
		audit.DeliveryCount = uint64(deliveries)
		audit.SentCount = uint64(sent)
		audit.UnknownCount = uint64(unknown)
		audit.PermanentFailed = uint64(permanent)
		audit.EventAt = audit.EventAt.UTC()
		snapshot.NotificationAudits = append(snapshot.NotificationAudits, audit)
	}
	return rows.Err()
}

func restorePortableActions(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot recordcollaboration.PortabilitySnapshot) error {
	for _, action := range snapshot.Actions {
		var subject, assignee any
		if action.SubjectRevisionID != "" {
			subject = action.SubjectRevisionID
		}
		if action.AssigneeID != "" {
			assignee = action.AssigneeID
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_actions (
				action_id, project_id, record_id, subject_revision_id, action_version,
				title, details, status, assignee_id, due_at, completed_at,
				created_by, updated_by, record_fence_epoch, created_at, updated_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			on conflict (action_id) do nothing`,
			action.ActionID, binding.ProjectID(), binding.RecordID(), subject, int64(action.Version),
			action.Title, action.Details, action.Status, assignee, action.DueAt, action.CompletedAt,
			action.CreatedBy, action.UpdatedBy, int64(binding.Epoch()), action.CreatedAt, action.UpdatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration action: %w", err)
		}
	}
	for _, event := range snapshot.ActionEvents {
		var previous, assignee any
		if event.PreviousStatus != nil {
			previous = *event.PreviousStatus
		}
		if event.AssigneeID != "" {
			assignee = event.AssigneeID
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_action_events (
				action_event_id, project_id, record_id, action_id, action_version,
				event_kind, previous_status, current_status, actor_id, assignee_id,
				record_fence_epoch, occurred_at, created_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			on conflict (action_event_id) do nothing`,
			event.EventID, binding.ProjectID(), binding.RecordID(), event.ActionID, int64(event.Version),
			event.Kind, previous, event.CurrentStatus, event.ActorID, assignee,
			int64(binding.Epoch()), event.OccurredAt, event.CreatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration action event: %w", err)
		}
	}
	return nil
}

func restorePortableComments(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot recordcollaboration.PortabilitySnapshot) error {
	comments := make(map[string]recordcollaboration.PortableComment, len(snapshot.Comments))
	for _, comment := range snapshot.Comments {
		comments[comment.CommentID] = comment
		body, render, digest, err := portableCommentInsertContent(comment.BodyMarkdown, comment.RenderModel, comment.BodyDigest, comment.State == recordcollaboration.CommentStateRedacted)
		if err != nil {
			return err
		}
		insertVersion := comment.Version
		insertUpdatedAt := comment.UpdatedAt
		if comment.State == recordcollaboration.CommentStateRedacted {
			insertVersion--
			insertUpdatedAt = comment.CreatedAt
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_comments (
				comment_id, project_id, record_id, author_id, comment_state, comment_version,
				body_markdown, render_contract_version, render_model, body_digest,
				record_fence_epoch, created_at, updated_at
			) values ($1,$2,$3,$4,'active',$5,$6,'comment_markdown/v1',$7,$8,$9,$10,$11)
			on conflict (comment_id) do nothing`,
			comment.CommentID, binding.ProjectID(), binding.RecordID(), comment.AuthorID, int64(insertVersion),
			body, render, digest, int64(binding.Epoch()), comment.CreatedAt, insertUpdatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration comment: %w", err)
		}
	}
	for _, revision := range snapshot.CommentRevisions {
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from public.record_comment_revisions
				where comment_revision_id = $1
			)`, revision.RevisionID).Scan(&exists); err != nil {
			return fmt.Errorf("check restored collaboration comment revision: %w", err)
		}
		if exists {
			continue
		}
		body, render, digest, err := portableCommentInsertContent(revision.BodyMarkdown, revision.RenderModel, revision.BodyDigest, revision.RedactedAt != nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_revisions (
				comment_revision_id, project_id, record_id, comment_id, comment_version,
				edited_by, body_markdown, render_contract_version, render_model,
				body_digest, record_fence_epoch, created_at
			) values ($1,$2,$3,$4,$5,$6,$7,'comment_markdown/v1',$8,$9,$10,$11)
			on conflict (comment_revision_id) do nothing`,
			revision.RevisionID, binding.ProjectID(), binding.RecordID(), revision.CommentID, int64(revision.Version),
			revision.EditedBy, body, render, digest, int64(binding.Epoch()), revision.CreatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration comment revision: %w", err)
		}
	}
	for _, tombstone := range snapshot.Tombstones {
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_tombstones (
				tombstone_id, record_id, comment_id, tombstone_version, deleted_by,
				reason_code, deleted_at, record_fence_epoch
			) values ($1,$2,$3,$4,$5,$6,$7,$8)
			on conflict (tombstone_id) do nothing`, tombstone.TombstoneID, binding.RecordID(),
			tombstone.CommentID, int64(tombstone.Version), tombstone.DeletedBy,
			tombstone.ReasonCode, tombstone.DeletedAt, int64(binding.Epoch()),
		); err != nil {
			return fmt.Errorf("restore collaboration comment tombstone: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update public.record_comment_revisions
			set body_markdown = null, render_contract_version = null, render_model = null,
			    body_digest = null, tombstone_id = $2, redacted_at = $3
			where record_id = $1 and comment_id = $4 and redacted_at is null`,
			binding.RecordID(), tombstone.TombstoneID, tombstone.DeletedAt, tombstone.CommentID,
		); err != nil {
			return fmt.Errorf("redact restored collaboration comment revisions: %w", err)
		}
		comment := comments[tombstone.CommentID]
		if _, err := tx.Exec(ctx, `
			update public.record_comments
			set comment_state = 'redacted', comment_version = $6, body_markdown = null,
			    render_contract_version = null, render_model = null, body_digest = null,
			    tombstone_id = $2, redacted_at = $3, updated_at = $4
			where record_id = $1 and comment_id = $5 and comment_state = 'active'`,
			binding.RecordID(), tombstone.TombstoneID, tombstone.DeletedAt, comment.UpdatedAt,
			tombstone.CommentID, int64(comment.Version),
		); err != nil {
			return fmt.Errorf("redact restored collaboration comment: %w", err)
		}
	}
	for _, reply := range snapshot.Replies {
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_replies (
				record_id, child_comment_id, parent_comment_id, record_fence_epoch, created_at
			) values ($1,$2,$3,$4,$5) on conflict (child_comment_id) do nothing`,
			binding.RecordID(), reply.ChildCommentID, reply.ParentCommentID, int64(binding.Epoch()), reply.CreatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration comment reply: %w", err)
		}
	}
	for _, mention := range snapshot.Mentions {
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_mentions (
				record_id, comment_id, comment_version, mentioned_user_id,
				record_fence_epoch, created_at
			) values ($1,$2,$3,$4,$5,$6)
			on conflict (comment_id, comment_version, mentioned_user_id) do nothing`,
			binding.RecordID(), mention.CommentID, int64(mention.CommentVersion), mention.MentionedUser,
			int64(binding.Epoch()), mention.CreatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration comment mention: %w", err)
		}
	}
	return nil
}

func restorePortableFollowers(ctx context.Context, tx pgx.Tx, binding recordcollaboration.RecordFenceBinding, snapshot recordcollaboration.PortabilitySnapshot) error {
	for _, follower := range snapshot.Followers {
		if _, err := tx.Exec(ctx, `
			insert into public.record_followers (
				project_id, record_id, user_id, follower_version, manual_preference,
				follows_author, follows_owner, follows_participant,
				follows_comment, follows_mention, follows_action,
				record_fence_epoch, created_at, updated_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			on conflict (record_id, user_id) do nothing`, binding.ProjectID(), binding.RecordID(),
			follower.UserID, int64(follower.Version), follower.Preference,
			follower.Sources.Author, follower.Sources.Owner, follower.Sources.Participant,
			follower.Sources.Comment, follower.Sources.Mention, follower.Sources.Action,
			int64(binding.Epoch()), follower.CreatedAt, follower.UpdatedAt,
		); err != nil {
			return fmt.Errorf("restore collaboration follower: %w", err)
		}
	}
	return nil
}

func portableCommentInsertContent(source string, model recordcollaboration.CommentRenderModel, digest [sha256.Size]byte, redacted bool) (string, []byte, []byte, error) {
	if redacted {
		content, err := recordcollaboration.NewCommentContent("redacted")
		if err != nil {
			return "", nil, nil, recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		encoded, err := json.Marshal(content.Model())
		if err != nil {
			return "", nil, nil, recordcollaboration.ErrInvalidPortabilitySnapshot
		}
		value := content.Digest()
		return content.Source(), encoded, value[:], nil
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		return "", nil, nil, recordcollaboration.ErrInvalidPortabilitySnapshot
	}
	return source, encoded, digest[:], nil
}

func decodePortableCommentContent(body *string, render, digest []byte, source *string, model *recordcollaboration.CommentRenderModel, targetDigest *[sha256.Size]byte) bool {
	if body == nil {
		return len(render) == 0 && len(digest) == 0
	}
	if len(digest) != sha256.Size {
		return false
	}
	decoded, err := recordcollaboration.DecodeCommentRenderModelV1(render)
	if err != nil {
		return false
	}
	*source = *body
	*model = decoded
	copy(targetDigest[:], digest)
	return true
}

func normalizePortableActionTimes(action *recordcollaboration.PortableAction) {
	action.CreatedAt = action.CreatedAt.UTC()
	action.UpdatedAt = action.UpdatedAt.UTC()
	action.DueAt = utcPortableTime(action.DueAt)
	action.CompletedAt = utcPortableTime(action.CompletedAt)
}

func utcPortableTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

var (
	_ recordcollaboration.ActivityFactSource = (*PostgresRecordCollaborationProvider)(nil)
	_ recordcollaboration.PortabilityStore   = (*PostgresRecordCollaborationProvider)(nil)
)
