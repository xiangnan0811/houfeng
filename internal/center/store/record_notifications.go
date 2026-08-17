package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type PostgresRecordNotificationRepository struct {
	platform      *PostgresRecordPlatformRepository
	members       CollaborationMembershipReader
	authorization *PostgresCurrentRecordAuthorizationSource
	retention     time.Duration
}

func NewPostgresRecordNotificationRepository(
	pool *pgxpool.Pool,
	gate AdmissionGate,
	members CollaborationMembershipReader,
	authorization *PostgresCurrentRecordAuthorizationSource,
	retention time.Duration,
) *PostgresRecordNotificationRepository {
	return &PostgresRecordNotificationRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate), members: members,
		authorization: authorization, retention: retention,
	}
}

func (repository *PostgresRecordNotificationRepository) ProjectNotification(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) (recordcollaboration.NotificationProjectionResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) ||
		repository.authorization == nil || nilRecordSubjectDependency(repository.authorization.resolver) ||
		repository.retention.Microseconds() <= 0 || claim.Validate() != nil {
		return recordcollaboration.NotificationProjectionResult{}, recordcollaboration.ErrInvalidNotificationProjector
	}
	kind, supported := recordcollaboration.NotificationEventKindFromOutbox(claim.Event.EventKind)
	if !supported {
		return recordcollaboration.NotificationProjectionResult{}, recordcollaboration.ErrNotificationSourceMissing
	}
	var result recordcollaboration.NotificationProjectionResult
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := transaction.AssertOutboxClaim(ctx, claim); err != nil {
			return err
		}
		facts, direct, err := loadNotificationSourceFacts(ctx, transaction.tx, claim.Event, kind)
		if err != nil {
			return err
		}
		if err := assertRecordReadFence(ctx, transaction.tx, facts.RecordID); err != nil {
			return err
		}
		binding, err := loadCollaborationRecordReadFenceBinding(ctx, transaction.tx, facts.RecordID)
		if err != nil {
			return err
		}
		root, err := lockRecordRootForCommentRead(ctx, transaction.tx, facts.RecordID)
		if err != nil {
			return err
		}
		if facts.AuthorizationEpoch != root.authorizationEpoch || facts.RecordFenceEpoch != uint64(binding.Epoch()) {
			return recordcollaboration.ErrNotificationSourceStale
		}
		followers, err := loadNotificationFollowers(ctx, transaction.tx, facts.RecordID, facts.RecordFenceEpoch)
		if err != nil {
			return err
		}
		candidates, err := recordcollaboration.NormalizeNotificationRecipients(facts, followers, direct)
		if err != nil {
			return err
		}
		allowed := make([]recordcollaboration.NotificationRecipientFacts, 0, len(candidates))
		for _, candidate := range candidates {
			actor, err := repository.members.ReadMemberActor(ctx, transaction.tx, recordauth.ProjectIDDefault, candidate.UserID)
			if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
				continue
			}
			if err != nil {
				return err
			}
			current, err := repository.resolveCurrentAuthorizationInTransaction(ctx, transaction.tx, actor, facts.RecordID)
			if err != nil {
				return err
			}
			if current.Lifecycle != records.LifecycleActive || records.AuthorizeRecordResource(actor, recordauth.CapabilityNotificationRead, current.Evidence) != nil {
				continue
			}
			if current.AuthorizationEpoch != facts.AuthorizationEpoch {
				return recordcollaboration.ErrNotificationSourceStale
			}
			allowed = append(allowed, candidate)
		}
		if len(allowed) == 0 {
			result = recordcollaboration.NotificationProjectionResult{}
			return nil
		}
		if facts.Validate() != nil {
			return recordcollaboration.ErrInvalidNotificationFacts
		}
		notificationID := facts.NotificationID()
		if err := insertNotificationProjection(ctx, transaction.tx, facts, notificationID, repository.retention, allowed); err != nil {
			return err
		}
		result = recordcollaboration.NotificationProjectionResult{NotificationID: notificationID, RecipientCount: len(allowed)}
		return result.Validate()
	})
	if err != nil {
		return recordcollaboration.NotificationProjectionResult{}, err
	}
	return result, nil
}

func (repository *PostgresRecordNotificationRepository) resolveCurrentAuthorizationInTransaction(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, recordID string) (records.CurrentRecordAuthorization, error) {
	loader := &PostgresCurrentRecordAuthorizationSource{db: tx}
	snapshot, err := loader.loadCurrentRecordAuthorizationSnapshot(ctx, recordID)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	normalized, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, actor.ProjectID, snapshot)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	sources, err := repository.authorization.resolveRecordAuthorizationSubjects(ctx, actor, normalized.projectID, normalized.subjects)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	return records.CurrentRecordAuthorization{
		RecordID: normalized.recordID, CurrentRevisionID: normalized.currentRevisionID,
		LockVersion: normalized.lockVersion, AuthorizationEpoch: normalized.authorizationEpoch,
		Lifecycle: normalized.lifecycle,
		Evidence:  records.RecordAuthorizationEvidence{ProjectID: normalized.projectID, Visibility: normalized.visibility, Sources: sources},
	}, nil
}

func loadNotificationSourceFacts(ctx context.Context, tx pgx.Tx, event recordplatform.OutboxEvent, kind recordcollaboration.NotificationEventKind) (recordcollaboration.NotificationEventFacts, []recordcollaboration.NotificationRecipientCandidate, error) {
	facts := recordcollaboration.NotificationEventFacts{
		Kind: kind, SubjectKind: recordcollaboration.NotificationSubjectKind(event.SubjectKind),
		SubjectID: event.SubjectID, SourceVersion: event.SourceVersion,
		AuthorizationEpoch: event.AuthorizationEpoch, RecordFenceEpoch: event.RecordFenceEpoch,
	}
	var direct []recordcollaboration.NotificationRecipientCandidate
	switch kind {
	case recordcollaboration.NotificationEventRecordOwnerChanged, recordcollaboration.NotificationEventRecordParticipantChanged:
		var revisionID, actorID string
		var ownerID *string
		err := tx.QueryRow(ctx, `
			select revision_id, record_id, author_id, owner_id, created_at
			from public.record_revisions
			where record_id = $1 and revision_no = $2`, event.SubjectID, int64(event.SourceVersion)).Scan(
			&revisionID, &facts.RecordID, &actorID, &ownerID, &facts.OccurredAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, nil, recordcollaboration.ErrNotificationSourceMissing
		}
		if err != nil {
			return facts, nil, fmt.Errorf("load notification revision source: %w", err)
		}
		facts.ActorID = actorID
		if kind == recordcollaboration.NotificationEventRecordOwnerChanged && ownerID != nil {
			direct = append(direct, recordcollaboration.NotificationRecipientCandidate{UserID: *ownerID, Reason: recordcollaboration.NotificationReasonOwner})
		}
		if kind == recordcollaboration.NotificationEventRecordParticipantChanged {
			rows, err := tx.Query(ctx, `select participant_id from public.record_revision_participants where revision_id = $1 order by participant_id`, revisionID)
			if err != nil {
				return facts, nil, fmt.Errorf("load notification revision participants: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var userID string
				if err := rows.Scan(&userID); err != nil {
					return facts, nil, fmt.Errorf("scan notification revision participant: %w", err)
				}
				direct = append(direct, recordcollaboration.NotificationRecipientCandidate{UserID: userID, Reason: recordcollaboration.NotificationReasonParticipant})
			}
			if err := rows.Err(); err != nil {
				return facts, nil, fmt.Errorf("iterate notification revision participants: %w", err)
			}
		}
	case recordcollaboration.NotificationEventActionAssigned, recordcollaboration.NotificationEventActionCompleted, recordcollaboration.NotificationEventActionCancelled:
		var mutationKind, actorID string
		var assigneeID *string
		err := tx.QueryRow(ctx, `
			select record_id, event_kind, actor_id, assignee_id, occurred_at
			from public.record_action_events
			where action_id = $1 and action_version = $2`, event.SubjectID, int64(event.SourceVersion)).Scan(
			&facts.RecordID, &mutationKind, &actorID, &assigneeID, &facts.OccurredAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, nil, recordcollaboration.ErrNotificationSourceMissing
		}
		if err != nil {
			return facts, nil, fmt.Errorf("load notification action source: %w", err)
		}
		if (kind == recordcollaboration.NotificationEventActionAssigned && mutationKind != "created" && mutationKind != "updated") ||
			(kind == recordcollaboration.NotificationEventActionCompleted && mutationKind != "completed") ||
			(kind == recordcollaboration.NotificationEventActionCancelled && mutationKind != "cancelled") ||
			(kind == recordcollaboration.NotificationEventActionAssigned && assigneeID == nil) {
			return facts, nil, recordcollaboration.ErrNotificationSourceMissing
		}
		facts.ActorID = actorID
		if assigneeID != nil {
			direct = append(direct, recordcollaboration.NotificationRecipientCandidate{UserID: *assigneeID, Reason: recordcollaboration.NotificationReasonAssignee})
		}
	case recordcollaboration.NotificationEventCommentReplied, recordcollaboration.NotificationEventCommentMentioned:
		var actorID string
		err := tx.QueryRow(ctx, `
			select record_id, edited_by, created_at
			from public.record_comment_revisions
			where comment_id = $1 and comment_version = $2`, event.SubjectID, int64(event.SourceVersion)).Scan(
			&facts.RecordID, &actorID, &facts.OccurredAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, nil, recordcollaboration.ErrNotificationSourceMissing
		}
		if err != nil {
			return facts, nil, fmt.Errorf("load notification comment source: %w", err)
		}
		facts.ActorID = actorID
		if kind == recordcollaboration.NotificationEventCommentReplied {
			var parentAuthor string
			err := tx.QueryRow(ctx, `
				select parent.author_id
				from public.record_comment_replies replies
				join public.record_comments parent on parent.record_id = replies.record_id and parent.comment_id = replies.parent_comment_id
				where replies.child_comment_id = $1 and replies.record_id = $2`, event.SubjectID, facts.RecordID).Scan(&parentAuthor)
			if errors.Is(err, pgx.ErrNoRows) {
				return facts, nil, recordcollaboration.ErrNotificationSourceMissing
			}
			if err != nil {
				return facts, nil, fmt.Errorf("load notification reply source: %w", err)
			}
			direct = append(direct, recordcollaboration.NotificationRecipientCandidate{UserID: parentAuthor, Reason: recordcollaboration.NotificationReasonReply})
		} else {
			rows, err := tx.Query(ctx, `
				select mentioned_user_id
				from public.record_comment_mentions
				where comment_id = $1 and comment_version = $2
				order by mentioned_user_id`, event.SubjectID, int64(event.SourceVersion))
			if err != nil {
				return facts, nil, fmt.Errorf("load notification mention source: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var userID string
				if err := rows.Scan(&userID); err != nil {
					return facts, nil, fmt.Errorf("scan notification mention: %w", err)
				}
				direct = append(direct, recordcollaboration.NotificationRecipientCandidate{UserID: userID, Reason: recordcollaboration.NotificationReasonMention})
			}
			if err := rows.Err(); err != nil {
				return facts, nil, fmt.Errorf("iterate notification mentions: %w", err)
			}
			if len(direct) == 0 {
				return facts, nil, recordcollaboration.ErrNotificationSourceMissing
			}
		}
	default:
		return facts, nil, recordcollaboration.ErrNotificationSourceMissing
	}
	facts.OccurredAt = facts.OccurredAt.UTC()
	if facts.Validate() != nil {
		return facts, nil, recordcollaboration.ErrInvalidNotificationFacts
	}
	return facts, direct, nil
}

func loadNotificationFollowers(ctx context.Context, tx pgx.Tx, recordID string, fenceEpoch uint64) ([]recordcollaboration.FollowerFacts, error) {
	if fenceEpoch > math.MaxInt64 {
		return nil, recordcollaboration.ErrInvalidNotificationFacts
	}
	rows, err := tx.Query(ctx, `
		select user_id, follower_version, manual_preference,
		       follows_author, follows_owner, follows_participant,
		       follows_comment, follows_mention, follows_action
		from public.record_followers
		where record_id = $1 and record_fence_epoch = $2
		order by user_id`, recordID, int64(fenceEpoch))
	if err != nil {
		return nil, fmt.Errorf("load notification followers: %w", err)
	}
	defer rows.Close()
	result := make([]recordcollaboration.FollowerFacts, 0)
	for rows.Next() {
		var version int64
		var preference string
		facts := recordcollaboration.FollowerFacts{}
		if err := rows.Scan(&facts.UserID, &version, &preference, &facts.Sources.Author, &facts.Sources.Owner,
			&facts.Sources.Participant, &facts.Sources.Comment, &facts.Sources.Mention, &facts.Sources.Action); err != nil {
			return nil, fmt.Errorf("scan notification follower: %w", err)
		}
		if version <= 0 {
			return nil, recordcollaboration.ErrInvalidNotificationFacts
		}
		facts.Version = uint64(version)
		facts.Preference = recordcollaboration.FollowerPreference(preference)
		if facts.Validate() != nil {
			return nil, recordcollaboration.ErrInvalidNotificationFacts
		}
		result = append(result, facts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification followers: %w", err)
	}
	return result, nil
}

func insertNotificationProjection(ctx context.Context, tx pgx.Tx, facts recordcollaboration.NotificationEventFacts, notificationID string, retention time.Duration, recipients []recordcollaboration.NotificationRecipientFacts) error {
	if facts.Validate() != nil || notificationID != facts.NotificationID() || retention.Microseconds() <= 0 || len(recipients) == 0 {
		return recordcollaboration.ErrInvalidNotificationFacts
	}
	_, err := tx.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, project_id, record_id, event_kind, subject_kind,
			subject_id, source_version, actor_id, authorization_epoch,
			record_fence_epoch, event_at, details_delete_after
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			transaction_timestamp() + ($12 * interval '1 microsecond'))
		on conflict (notification_id) do nothing`,
		notificationID, recordplatform.ProjectIDDefault, facts.RecordID, string(facts.Kind), string(facts.SubjectKind),
		facts.SubjectID, int64(facts.SourceVersion), facts.ActorID, int64(facts.AuthorizationEpoch),
		int64(facts.RecordFenceEpoch), facts.OccurredAt, retention.Microseconds(),
	)
	if err != nil {
		return fmt.Errorf("insert record notification: %w", err)
	}
	var recordID, eventKind, subjectKind, subjectID, actorID string
	var sourceVersion, authorizationEpoch, recordFenceEpoch int64
	if err := tx.QueryRow(ctx, `
		select record_id, event_kind, subject_kind, subject_id, source_version, actor_id,
		       authorization_epoch, record_fence_epoch
		from public.record_notifications where notification_id = $1`, notificationID).Scan(
		&recordID, &eventKind, &subjectKind, &subjectID, &sourceVersion, &actorID,
		&authorizationEpoch, &recordFenceEpoch,
	); err != nil {
		return fmt.Errorf("verify record notification identity: %w", err)
	}
	if recordID != facts.RecordID || eventKind != string(facts.Kind) || subjectKind != string(facts.SubjectKind) ||
		subjectID != facts.SubjectID || sourceVersion != int64(facts.SourceVersion) || actorID != facts.ActorID ||
		authorizationEpoch != int64(facts.AuthorizationEpoch) || recordFenceEpoch != int64(facts.RecordFenceEpoch) {
		return recordcollaboration.ErrInvalidNotificationFacts
	}
	for _, recipient := range recipients {
		if recipient.Validate() != nil {
			return recordcollaboration.ErrInvalidNotificationFacts
		}
		_, err := tx.Exec(ctx, `
			insert into public.record_notification_recipients (
				notification_id, record_id, recipient_user_id, reason_kind, mandatory,
				authorization_epoch, record_fence_epoch
			) values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (notification_id, recipient_user_id) do nothing`,
			notificationID, facts.RecordID, recipient.UserID, string(recipient.Reason), recipient.Mandatory,
			int64(facts.AuthorizationEpoch), int64(facts.RecordFenceEpoch),
		)
		if err != nil {
			return fmt.Errorf("insert record notification recipient: %w", err)
		}
	}
	return nil
}

var _ recordcollaboration.NotificationProjectionStore = (*PostgresRecordNotificationRepository)(nil)

type inboxCandidate struct {
	item               recordcollaboration.InboxItem
	authorizationEpoch uint64
	recordFenceEpoch   uint64
}

func (repository *PostgresRecordNotificationRepository) ListInbox(ctx context.Context, request recordcollaboration.InboxListRequest) ([]recordcollaboration.InboxItem, error) {
	if ctx == nil || repository == nil || repository.platform == nil || request.Validate() != nil {
		return nil, recordcollaboration.ErrInvalidInboxRequest
	}
	result := make([]recordcollaboration.InboxItem, 0)
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		candidates, err := queryInboxCandidates(ctx, transaction.tx, request.Actor.UserID, "and recipients.dismissed_at is null")
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			if len(result) == request.Limit {
				break
			}
			if err := repository.authorizeInboxCandidate(ctx, transaction.tx, request.Actor, candidate); err != nil {
				if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
					continue
				}
				return err
			}
			result = append(result, candidate.item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (repository *PostgresRecordNotificationRepository) GetInboxItem(ctx context.Context, request recordcollaboration.InboxItemRequest) (recordcollaboration.InboxItem, error) {
	if ctx == nil || repository == nil || repository.platform == nil || request.Validate() != nil {
		return recordcollaboration.InboxItem{}, recordcollaboration.ErrInvalidInboxRequest
	}
	var result recordcollaboration.InboxItem
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		candidate, err := queryInboxCandidate(ctx, transaction.tx, request.Actor.UserID, request.NotificationID, false)
		if err != nil {
			return err
		}
		if err := repository.authorizeInboxCandidate(ctx, transaction.tx, request.Actor, candidate); err != nil {
			return err
		}
		result = candidate.item
		return nil
	})
	if err != nil {
		return recordcollaboration.InboxItem{}, err
	}
	return result, nil
}

func (repository *PostgresRecordNotificationRepository) GetInboxDeepLink(ctx context.Context, request recordcollaboration.InboxItemRequest) (recordcollaboration.InboxDeepLinkTarget, error) {
	item, err := repository.GetInboxItem(ctx, request)
	if err != nil {
		return recordcollaboration.InboxDeepLinkTarget{}, err
	}
	target := recordcollaboration.InboxDeepLinkTarget{RecordID: item.RecordID, SubjectKind: item.SubjectKind, SubjectID: item.SubjectID}
	if target.Validate() != nil {
		return recordcollaboration.InboxDeepLinkTarget{}, recordcollaboration.ErrInboxNotFound
	}
	return target, nil
}

func (repository *PostgresRecordNotificationRepository) TransitionInbox(ctx context.Context, request recordcollaboration.InboxTransitionRequest) (recordcollaboration.InboxItem, error) {
	if ctx == nil || repository == nil || repository.platform == nil || request.Validate() != nil {
		return recordcollaboration.InboxItem{}, recordcollaboration.ErrInvalidInboxRequest
	}
	var result recordcollaboration.InboxItem
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		candidate, err := queryInboxCandidate(ctx, transaction.tx, request.Actor.UserID, request.NotificationID, true)
		if err != nil {
			return err
		}
		if err := repository.authorizeInboxCandidate(ctx, transaction.tx, request.Actor, candidate); err != nil {
			return err
		}
		var updateSQL string
		switch request.Kind {
		case recordcollaboration.InboxTransitionUnread:
			updateSQL = `update public.record_notification_recipients set read_at = null, dismissed_at = null where notification_id = $1 and recipient_user_id = $2 returning read_at, dismissed_at`
		case recordcollaboration.InboxTransitionRead:
			updateSQL = `update public.record_notification_recipients set read_at = coalesce(read_at, transaction_timestamp()) where notification_id = $1 and recipient_user_id = $2 returning read_at, dismissed_at`
		case recordcollaboration.InboxTransitionDismiss:
			updateSQL = `update public.record_notification_recipients set read_at = coalesce(read_at, transaction_timestamp()), dismissed_at = coalesce(dismissed_at, transaction_timestamp()) where notification_id = $1 and recipient_user_id = $2 returning read_at, dismissed_at`
		default:
			return recordcollaboration.ErrInvalidInboxRequest
		}
		if err := transaction.tx.QueryRow(ctx, updateSQL, request.NotificationID, request.Actor.UserID).Scan(&candidate.item.ReadAt, &candidate.item.DismissedAt); errors.Is(err, pgx.ErrNoRows) {
			return recordcollaboration.ErrInboxNotFound
		} else if err != nil {
			return fmt.Errorf("transition record notification recipient: %w", err)
		}
		if candidate.item.Validate() != nil {
			return recordcollaboration.ErrInvalidInboxRequest
		}
		result = candidate.item
		return nil
	})
	if err != nil {
		return recordcollaboration.InboxItem{}, err
	}
	return result, nil
}

func (repository *PostgresRecordNotificationRepository) CountUnreadInbox(ctx context.Context, request recordcollaboration.InboxListRequest) (int, error) {
	if ctx == nil || repository == nil || repository.platform == nil || request.Validate() != nil {
		return 0, recordcollaboration.ErrInvalidInboxRequest
	}
	count := 0
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		candidates, err := queryInboxCandidates(ctx, transaction.tx, request.Actor.UserID, "and recipients.read_at is null and recipients.dismissed_at is null")
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			if err := repository.authorizeInboxCandidate(ctx, transaction.tx, request.Actor, candidate); err != nil {
				if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
					continue
				}
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func queryInboxCandidates(ctx context.Context, tx pgx.Tx, userID, predicate string) ([]inboxCandidate, error) {
	rows, err := tx.Query(ctx, `
		select notifications.notification_id, notifications.record_id, notifications.event_kind,
		       notifications.subject_kind, notifications.subject_id, notifications.source_version,
		       recipients.reason_kind, recipients.mandatory, notifications.event_at,
		       recipients.read_at, recipients.dismissed_at,
		       recipients.authorization_epoch, recipients.record_fence_epoch
		from public.record_notification_recipients recipients
		join public.record_notifications notifications on notifications.notification_id = recipients.notification_id
		where recipients.recipient_user_id = $1 `+predicate+`
		order by notifications.event_at desc, notifications.notification_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list record notification inbox: %w", err)
	}
	defer rows.Close()
	result := make([]inboxCandidate, 0)
	for rows.Next() {
		candidate, err := scanInboxCandidate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record notification inbox: %w", err)
	}
	return result, nil
}

func queryInboxCandidate(ctx context.Context, tx pgx.Tx, userID, notificationID string, forUpdate bool) (inboxCandidate, error) {
	sql := `
		select notifications.notification_id, notifications.record_id, notifications.event_kind,
		       notifications.subject_kind, notifications.subject_id, notifications.source_version,
		       recipients.reason_kind, recipients.mandatory, notifications.event_at,
		       recipients.read_at, recipients.dismissed_at,
		       recipients.authorization_epoch, recipients.record_fence_epoch
		from public.record_notification_recipients recipients
		join public.record_notifications notifications on notifications.notification_id = recipients.notification_id
		where recipients.recipient_user_id = $1 and notifications.notification_id = $2`
	if forUpdate {
		sql += " for update of recipients"
	}
	candidate, err := scanInboxCandidate(tx.QueryRow(ctx, sql, userID, notificationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return inboxCandidate{}, recordcollaboration.ErrInboxNotFound
	}
	return candidate, err
}

type inboxScanner interface{ Scan(...any) error }

func scanInboxCandidate(scanner inboxScanner) (inboxCandidate, error) {
	var eventKind, subjectKind, reason string
	var sourceVersion, authorizationEpoch, fenceEpoch int64
	candidate := inboxCandidate{}
	err := scanner.Scan(
		&candidate.item.NotificationID, &candidate.item.RecordID, &eventKind, &subjectKind,
		&candidate.item.SubjectID, &sourceVersion, &reason, &candidate.item.Mandatory,
		&candidate.item.EventAt, &candidate.item.ReadAt, &candidate.item.DismissedAt,
		&authorizationEpoch, &fenceEpoch,
	)
	if err != nil {
		return inboxCandidate{}, err
	}
	if sourceVersion <= 0 || authorizationEpoch <= 0 || fenceEpoch < 0 {
		return inboxCandidate{}, recordcollaboration.ErrInvalidInboxRequest
	}
	candidate.item.EventKind = recordcollaboration.NotificationEventKind(eventKind)
	candidate.item.SubjectKind = recordcollaboration.NotificationSubjectKind(subjectKind)
	candidate.item.SourceVersion = uint64(sourceVersion)
	candidate.item.Reason = recordcollaboration.NotificationReason(reason)
	candidate.item.EventAt = candidate.item.EventAt.UTC()
	if candidate.item.ReadAt != nil {
		value := candidate.item.ReadAt.UTC()
		candidate.item.ReadAt = &value
	}
	if candidate.item.DismissedAt != nil {
		value := candidate.item.DismissedAt.UTC()
		candidate.item.DismissedAt = &value
	}
	candidate.authorizationEpoch = uint64(authorizationEpoch)
	candidate.recordFenceEpoch = uint64(fenceEpoch)
	if candidate.item.Validate() != nil {
		return inboxCandidate{}, recordcollaboration.ErrInvalidInboxRequest
	}
	return candidate, nil
}

func (repository *PostgresRecordNotificationRepository) authorizeInboxCandidate(ctx context.Context, tx pgx.Tx, actorInput recordauth.ActorScope, candidate inboxCandidate) error {
	actor, err := repository.members.ReadMemberActor(ctx, tx, actorInput.ProjectID, actorInput.UserID)
	if errors.Is(err, recordcollaboration.ErrMembershipDenied) {
		return recordcollaboration.ErrInboxNotFound
	}
	if err != nil {
		return err
	}
	if actor.UserID != actorInput.UserID || actor.ProjectID != recordauth.ProjectIDDefault {
		return recordcollaboration.ErrInboxNotFound
	}
	if err := assertRecordReadFence(ctx, tx, candidate.item.RecordID); err != nil {
		if errors.Is(err, records.ErrRecordDeletionReserved) || errors.Is(err, records.ErrRecordNotFound) {
			return recordcollaboration.ErrInboxNotFound
		}
		return err
	}
	binding, err := loadCollaborationRecordReadFenceBinding(ctx, tx, candidate.item.RecordID)
	if err != nil {
		return err
	}
	if candidate.recordFenceEpoch != uint64(binding.Epoch()) {
		return recordcollaboration.ErrInboxNotFound
	}
	current, err := repository.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, candidate.item.RecordID)
	if err != nil {
		return err
	}
	if current.AuthorizationEpoch != candidate.authorizationEpoch || current.Lifecycle != records.LifecycleActive ||
		records.AuthorizeRecordResource(actor, recordauth.CapabilityNotificationRead, current.Evidence) != nil {
		return recordcollaboration.ErrInboxNotFound
	}
	var present int
	switch candidate.item.SubjectKind {
	case recordcollaboration.NotificationSubjectRecord:
		if candidate.item.SubjectID != candidate.item.RecordID {
			return recordcollaboration.ErrInboxNotFound
		}
		present = 1
	case recordcollaboration.NotificationSubjectAction:
		err = tx.QueryRow(ctx, `select 1 from public.record_actions where record_id = $1 and action_id = $2 and record_fence_epoch = $3`,
			candidate.item.RecordID, candidate.item.SubjectID, int64(binding.Epoch())).Scan(&present)
	case recordcollaboration.NotificationSubjectComment:
		err = tx.QueryRow(ctx, `select 1 from public.record_comments where record_id = $1 and comment_id = $2 and record_fence_epoch = $3`,
			candidate.item.RecordID, candidate.item.SubjectID, int64(binding.Epoch())).Scan(&present)
	default:
		return recordcollaboration.ErrInboxNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) || present != 1 {
		return recordcollaboration.ErrInboxNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize inbox subject identity: %w", err)
	}
	return nil
}

var _ recordcollaboration.InboxStore = (*PostgresRecordNotificationRepository)(nil)
