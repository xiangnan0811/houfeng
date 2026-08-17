package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// PostgresRecordCommentRepository composes the existing record-platform
// primitives with the 0055 comment relations. It stores no alternate
// idempotency, authorization, outbox, lease, or deletion authority.
type PostgresRecordCommentRepository struct {
	platform *PostgresRecordPlatformRepository
	members  CollaborationMembershipReader
}

func NewPostgresRecordCommentRepository(pool *pgxpool.Pool, gate AdmissionGate, members CollaborationMembershipReader) *PostgresRecordCommentRepository {
	return &PostgresRecordCommentRepository{platform: NewPostgresRecordPlatformRepository(pool, gate), members: members}
}

func (repository *PostgresRecordCommentRepository) CommitComment(ctx context.Context, command recordcollaboration.CommentCommand) (recordcollaboration.CommentMutationResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) {
		return recordcollaboration.CommentMutationResult{}, recordcollaboration.ErrInvalidCommentCommand
	}
	if err := command.Validate(); err != nil {
		return recordcollaboration.CommentMutationResult{}, err
	}

	var result recordcollaboration.CommentMutationResult
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return recordcollaboration.ErrInvalidCommentCommand
		}
		if err := assertRecordMutationFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		binding, err := loadCollaborationRecordFenceBinding(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		root, err := lockRecordRoot(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		if root.currentRevisionID == nil || *root.currentRevisionID != command.CurrentRevisionID ||
			root.lockVersion != command.RecordLockVersion || root.authorizationEpoch != command.AuthorizationEpoch ||
			root.lifecycle != records.LifecycleActive || root.projectID != string(recordplatform.ProjectIDDefault) {
			return recordcollaboration.ErrCommentConflict
		}
		persistedActor, err := repository.members.ReadMemberActor(
			ctx, transaction.tx, command.Actor.ProjectID, command.Actor.UserID,
		)
		if err != nil {
			return err
		}
		if persistedActor.UserID != command.Actor.UserID || persistedActor.ProjectID != command.Actor.ProjectID {
			return recordcollaboration.ErrMembershipDenied
		}
		if err := records.AuthorizeRecordResource(persistedActor, recordauth.CapabilityRecordUpdate, command.AuthorizationEvidence); err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			if !command.ResultFingerprint.MatchesPersisted(*claim.ReplayResult) {
				return recordplatform.ErrIdempotencyConflictState
			}
			result, err = loadRecordCommentReplay(ctx, transaction.tx, command)
			return err
		}

		version := command.ExpectedVersion + 1
		if command.Kind == recordcollaboration.CommentMutationCreate {
			version = 1
		}
		var sourceEventID string
		var changedAt time.Time
		switch command.Kind {
		case recordcollaboration.CommentMutationCreate:
			if err := repository.validateCommentRelationsInTransaction(ctx, transaction.tx, command, binding); err != nil {
				return err
			}
			sourceEventID = recordCommentRevisionID(command.CommentID, version)
			changedAt, err = insertRecordComment(ctx, transaction.tx, command, binding, sourceEventID)
		case recordcollaboration.CommentMutationEdit:
			persisted, lockErr := lockRecordComment(ctx, transaction.tx, command.RecordID, command.CommentID)
			if lockErr != nil {
				return lockErr
			}
			if persisted.version != command.ExpectedVersion {
				return recordcollaboration.ErrCommentConflict
			}
			if persisted.state != recordcollaboration.CommentStateActive || persisted.authorID != command.Actor.UserID {
				return recordcollaboration.ErrCommentPolicyDenied
			}
			if err := repository.validateCommentMentionsInTransaction(ctx, transaction.tx, command, binding); err != nil {
				return err
			}
			sourceEventID = recordCommentRevisionID(command.CommentID, version)
			changedAt, err = editRecordComment(ctx, transaction.tx, command, binding, sourceEventID, version)
		case recordcollaboration.CommentMutationRedact:
			persisted, lockErr := lockRecordComment(ctx, transaction.tx, command.RecordID, command.CommentID)
			if lockErr != nil {
				return lockErr
			}
			if persisted.version != command.ExpectedVersion || persisted.state != recordcollaboration.CommentStateActive {
				return recordcollaboration.ErrCommentConflict
			}
			if persisted.authorID != persistedActor.UserID && persistedActor.Role != recordauth.RoleProjectAdmin {
				return recordcollaboration.ErrCommentPolicyDenied
			}
			sourceEventID = recordCommentTombstoneID(command.CommentID, version)
			changedAt, err = redactRecordComment(ctx, transaction.tx, command, binding, sourceEventID, version, persisted.authorID == command.Actor.UserID)
		default:
			return recordcollaboration.ErrInvalidCommentCommand
		}
		if err != nil {
			return err
		}

		activityKind, err := recordcollaboration.ActivityKindForCommentMutation(command.Kind)
		if err != nil {
			return err
		}
		if err := insertRecordCommentActivity(ctx, transaction.tx, command, sourceEventID, version, activityKind, changedAt); err != nil {
			return err
		}
		automaticSources := []collaborationAutomaticFollowerSources{{userID: command.Actor.UserID, comment: true}}
		for _, mentionedUserID := range command.MentionUserIDs {
			automaticSources = append(automaticSources, collaborationAutomaticFollowerSources{userID: mentionedUserID, mention: true})
		}
		if err := upsertCollaborationAutomaticFollowerSources(ctx, transaction.tx, binding, automaticSources); err != nil {
			return err
		}
		for _, outboxKind := range recordCommentNotificationOutboxKinds(
			command.Kind, command.ReplyToCommentID != "", len(command.MentionUserIDs) != 0,
		) {
			sourceVersion := uint64(0)
			recordFenceEpoch := uint64(0)
			if outboxKind == recordplatform.OutboxEventKindRecordCommentReplied ||
				outboxKind == recordplatform.OutboxEventKindRecordCommentMentioned {
				sourceVersion = version
				recordFenceEpoch = uint64(binding.Epoch())
			}
			if _, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
				Event: recordplatform.OutboxEvent{
					ProjectID: string(recordplatform.ProjectIDDefault), EventKind: outboxKind,
					SubjectKind: recordplatform.OutboxSubjectKindComment, SubjectID: command.CommentID,
					SourceVersion:      sourceVersion,
					AuthorizationEpoch: command.AuthorizationEpoch,
					RecordFenceEpoch:   recordFenceEpoch,
				},
				ExpiresAfter: command.OutboxTTL,
			}); err != nil {
				return err
			}
		}
		if err := transaction.CompleteIdempotency(ctx, command.Idempotency.Key, *claim.Owner, command.ResultFingerprint); err != nil {
			return err
		}
		state := recordcollaboration.CommentStateActive
		if command.Kind == recordcollaboration.CommentMutationRedact {
			state = recordcollaboration.CommentStateRedacted
		}
		result = recordcollaboration.CommentMutationResult{
			CommentID: command.CommentID, RecordID: command.RecordID, Version: version,
			State: state, EventKind: command.Kind, ChangedAt: changedAt.UTC(),
		}
		return result.Validate()
	})
	if err != nil {
		return recordcollaboration.CommentMutationResult{}, err
	}
	return result, nil
}

func (repository *PostgresRecordCommentRepository) ListComments(ctx context.Context, command recordcollaboration.CommentReadCommand) ([]recordcollaboration.CommentRecord, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, recordcollaboration.ErrInvalidCommentRequest
	}
	if err := command.Validate(); err != nil {
		return nil, err
	}
	var result []recordcollaboration.CommentRecord
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		binding, err := loadCollaborationRecordReadFenceBinding(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		root, err := lockRecordRootForCommentRead(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		if root.currentRevisionID == nil || *root.currentRevisionID != command.CurrentRevisionID ||
			root.lockVersion != command.RecordLockVersion || root.authorizationEpoch != command.AuthorizationEpoch ||
			root.lifecycle != records.LifecycleActive || root.projectID != string(recordplatform.ProjectIDDefault) {
			return recordcollaboration.ErrCommentConflict
		}
		if err := records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordRead, command.AuthorizationEvidence); err != nil {
			return err
		}
		loaded, err := listRecordComments(ctx, transaction.tx, command.RecordID, binding, command.Limit)
		if err != nil {
			return err
		}
		result = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (repository *PostgresRecordCommentRepository) validateCommentRelationsInTransaction(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding) error {
	if command.ReplyToCommentID != "" {
		if command.ReplyToCommentID == command.CommentID {
			return recordcollaboration.ErrInvalidCommentContent
		}
		var state string
		var parentIsReply bool
		err := tx.QueryRow(ctx, `
			select parent.comment_state,
			       exists (select 1 from public.record_comment_replies as reply where reply.child_comment_id = parent.comment_id)
			from public.record_comments as parent
			where parent.record_id = $1 and parent.comment_id = $2 and parent.record_fence_epoch = $3
			for update of parent`, command.RecordID, command.ReplyToCommentID, int64(binding.Epoch())).Scan(&state, &parentIsReply)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recordcollaboration.ErrInvalidCommentContent
			}
			return fmt.Errorf("validate record comment reply: %w", err)
		}
		if state != string(recordcollaboration.CommentStateActive) || parentIsReply {
			return recordcollaboration.ErrInvalidCommentContent
		}
	}
	return repository.validateCommentMentionsInTransaction(ctx, tx, command, binding)
}

func (repository *PostgresRecordCommentRepository) validateCommentMentionsInTransaction(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding) error {
	if binding.Validate() != nil || binding.RecordID() != command.RecordID {
		return recordcollaboration.ErrInvalidCommentCommand
	}
	for _, userID := range command.MentionUserIDs {
		member, err := repository.members.ReadMemberActor(ctx, tx, command.Actor.ProjectID, userID)
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityRecordRead, command.AuthorizationEvidence); err != nil {
			return recordcollaboration.ErrMembershipDenied
		}
	}
	return nil
}

type lockedRecordComment struct {
	authorID string
	version  uint64
	state    recordcollaboration.CommentState
}

func lockRecordComment(ctx context.Context, tx pgx.Tx, recordID, commentID string) (lockedRecordComment, error) {
	var persisted lockedRecordComment
	var version int64
	var state string
	err := tx.QueryRow(ctx, `
		select author_id, comment_version, comment_state
		from public.record_comments
		where record_id = $1 and comment_id = $2
		for update`, recordID, commentID).Scan(&persisted.authorID, &version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRecordComment{}, recordcollaboration.ErrCommentNotFound
	}
	if err != nil {
		return lockedRecordComment{}, fmt.Errorf("lock record comment: %w", err)
	}
	if version <= 0 || uint64(version) > recordcollaboration.MaxCommentVersion || recordauth.ValidateActorUserID(persisted.authorID) != nil {
		return lockedRecordComment{}, recordcollaboration.ErrCommentConflict
	}
	persisted.version = uint64(version)
	persisted.state = recordcollaboration.CommentState(state)
	if persisted.state != recordcollaboration.CommentStateActive && persisted.state != recordcollaboration.CommentStateRedacted {
		return lockedRecordComment{}, recordcollaboration.ErrCommentConflict
	}
	return persisted, nil
}

func insertRecordComment(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding, revisionID string) (time.Time, error) {
	renderModel, err := json.Marshal(command.Content.Model())
	if err != nil {
		return time.Time{}, recordcollaboration.ErrInvalidCommentCommand
	}
	digest := command.Content.Digest()
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		insert into public.record_comments (
			comment_id, project_id, record_id, author_id, comment_state, comment_version,
			body_markdown, render_contract_version, render_model, body_digest,
			record_fence_epoch, created_at, updated_at
		) values ($1, $2, $3, $4, 'active', 1, $5, $6, $7::jsonb, $8, $9,
			transaction_timestamp(), transaction_timestamp())
		returning created_at, updated_at`, command.CommentID, recordplatform.ProjectIDDefault,
		command.RecordID, command.Actor.UserID, command.Content.Source(), recordcollaboration.CommentRenderContractVersionV1,
		string(renderModel), digest[:], int64(binding.Epoch())).Scan(&createdAt, &updatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record comment: %w", err)
	}
	if !createdAt.Equal(updatedAt) {
		return time.Time{}, recordcollaboration.ErrCommentConflict
	}
	if err := insertRecordCommentRevision(ctx, tx, command, binding, revisionID, 1, createdAt); err != nil {
		return time.Time{}, err
	}
	if command.ReplyToCommentID != "" {
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_replies (
				record_id, child_comment_id, parent_comment_id, record_fence_epoch, created_at
			) values ($1, $2, $3, $4, $5)`, command.RecordID, command.CommentID,
			command.ReplyToCommentID, int64(binding.Epoch()), createdAt); err != nil {
			return time.Time{}, fmt.Errorf("insert record comment reply: %w", err)
		}
	}
	if err := insertRecordCommentMentions(ctx, tx, command, binding, 1, createdAt); err != nil {
		return time.Time{}, err
	}
	return createdAt.UTC(), nil
}

func editRecordComment(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding, revisionID string, version uint64) (time.Time, error) {
	var changedAt time.Time
	if err := tx.QueryRow(ctx, `select transaction_timestamp()`).Scan(&changedAt); err != nil {
		return time.Time{}, fmt.Errorf("read record comment edit time: %w", err)
	}
	if err := insertRecordCommentRevision(ctx, tx, command, binding, revisionID, version, changedAt); err != nil {
		return time.Time{}, err
	}
	renderModel, err := json.Marshal(command.Content.Model())
	if err != nil {
		return time.Time{}, recordcollaboration.ErrInvalidCommentCommand
	}
	digest := command.Content.Digest()
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		update public.record_comments
		set comment_version = $3, body_markdown = $4, render_contract_version = $5,
		    render_model = $6::jsonb, body_digest = $7, record_fence_epoch = $8, updated_at = $9
		where record_id = $1 and comment_id = $2 and comment_version = $10 and comment_state = 'active'
		returning updated_at`, command.RecordID, command.CommentID, int64(version), command.Content.Source(),
		recordcollaboration.CommentRenderContractVersionV1, string(renderModel), digest[:], int64(binding.Epoch()),
		changedAt, int64(command.ExpectedVersion)).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, recordcollaboration.ErrCommentConflict
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("update record comment: %w", err)
	}
	if err := insertRecordCommentMentions(ctx, tx, command, binding, version, changedAt); err != nil {
		return time.Time{}, err
	}
	return updatedAt.UTC(), nil
}

func insertRecordCommentRevision(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding, revisionID string, version uint64, createdAt time.Time) error {
	renderModel, err := json.Marshal(command.Content.Model())
	if err != nil {
		return recordcollaboration.ErrInvalidCommentCommand
	}
	digest := command.Content.Digest()
	if _, err := tx.Exec(ctx, `
		insert into public.record_comment_revisions (
			comment_revision_id, project_id, record_id, comment_id, comment_version,
			edited_by, body_markdown, render_contract_version, render_model, body_digest,
			record_fence_epoch, created_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12)`,
		revisionID, recordplatform.ProjectIDDefault, command.RecordID, command.CommentID, int64(version),
		command.Actor.UserID, command.Content.Source(), recordcollaboration.CommentRenderContractVersionV1,
		string(renderModel), digest[:], int64(binding.Epoch()), createdAt); err != nil {
		return fmt.Errorf("insert record comment revision: %w", err)
	}
	return nil
}

func insertRecordCommentMentions(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding, version uint64, createdAt time.Time) error {
	for _, userID := range command.MentionUserIDs {
		if _, err := tx.Exec(ctx, `
			insert into public.record_comment_mentions (
				record_id, comment_id, comment_version, mentioned_user_id, record_fence_epoch, created_at
			) values ($1, $2, $3, $4, $5, $6)`, command.RecordID, command.CommentID,
			int64(version), userID, int64(binding.Epoch()), createdAt); err != nil {
			return fmt.Errorf("insert record comment mention: %w", err)
		}
	}
	return nil
}

func redactRecordComment(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, binding recordcollaboration.RecordFenceBinding, tombstoneID string, version uint64, authorDelete bool) (time.Time, error) {
	reason := "moderator_deleted"
	if authorDelete {
		reason = "author_deleted"
	}
	var deletedAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_comment_tombstones (
			tombstone_id, record_id, comment_id, tombstone_version, deleted_by,
			reason_code, deleted_at, record_fence_epoch, created_at
		) values ($1, $2, $3, $4, $5, $6, transaction_timestamp(), $7, transaction_timestamp())
		returning deleted_at`, tombstoneID, command.RecordID, command.CommentID, int64(version),
		command.Actor.UserID, reason, int64(binding.Epoch())).Scan(&deletedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record comment tombstone: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		update public.record_comment_revisions
		set body_markdown = null, render_contract_version = null, render_model = null,
		    body_digest = null, tombstone_id = $3, redacted_at = $4
		where record_id = $1 and comment_id = $2 and redacted_at is null`, command.RecordID,
		command.CommentID, tombstoneID, deletedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("redact record comment revisions: %w", err)
	}
	if tag.RowsAffected() != int64(command.ExpectedVersion) {
		return time.Time{}, recordcollaboration.ErrCommentConflict
	}
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		update public.record_comments
		set comment_state = 'redacted', comment_version = $3, body_markdown = null,
		    render_contract_version = null, render_model = null, body_digest = null,
		    tombstone_id = $4, redacted_at = $5, record_fence_epoch = $6, updated_at = $5
		where record_id = $1 and comment_id = $2 and comment_version = $7 and comment_state = 'active'
		returning updated_at`, command.RecordID, command.CommentID, int64(version), tombstoneID,
		deletedAt, int64(binding.Epoch()), int64(command.ExpectedVersion)).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, recordcollaboration.ErrCommentConflict
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("redact record comment: %w", err)
	}
	return updatedAt.UTC(), nil
}

func insertRecordCommentActivity(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand, sourceEventID string, version uint64, kind recordcollaboration.CommentActivityKind, occurredAt time.Time) error {
	activityID := recordCommentActivityID(sourceEventID)
	var eventAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind,
			source_event_id, source_version, actor_id, authorization_epoch,
			record_lock_version, event_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		returning event_at`, activityID, recordplatform.ProjectIDDefault, command.RecordID,
		command.CurrentRevisionID, string(kind), sourceEventID, int64(version), command.Actor.UserID,
		int64(command.AuthorizationEpoch), int64(command.RecordLockVersion), occurredAt).Scan(&eventAt)
	if err != nil {
		return fmt.Errorf("insert record comment activity: %w", err)
	}
	if !eventAt.Equal(occurredAt) {
		return recordcollaboration.ErrCommentConflict
	}
	return nil
}

func loadRecordCommentReplay(ctx context.Context, tx pgx.Tx, command recordcollaboration.CommentCommand) (recordcollaboration.CommentMutationResult, error) {
	version := command.ExpectedVersion + 1
	if command.Kind == recordcollaboration.CommentMutationCreate {
		version = 1
	}
	var changedAt time.Time
	state := recordcollaboration.CommentStateActive
	if command.Kind == recordcollaboration.CommentMutationRedact {
		state = recordcollaboration.CommentStateRedacted
		err := tx.QueryRow(ctx, `
			select deleted_at
			from public.record_comment_tombstones
			where record_id = $1 and comment_id = $2 and tombstone_id = $3 and tombstone_version = $4`,
			command.RecordID, command.CommentID, recordCommentTombstoneID(command.CommentID, version), int64(version)).Scan(&changedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return recordcollaboration.CommentMutationResult{}, recordcollaboration.ErrCommentConflict
		}
		if err != nil {
			return recordcollaboration.CommentMutationResult{}, fmt.Errorf("load record comment redaction replay: %w", err)
		}
	} else {
		err := tx.QueryRow(ctx, `
			select created_at
			from public.record_comment_revisions
			where record_id = $1 and comment_id = $2 and comment_revision_id = $3 and comment_version = $4`,
			command.RecordID, command.CommentID, recordCommentRevisionID(command.CommentID, version), int64(version)).Scan(&changedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return recordcollaboration.CommentMutationResult{}, recordcollaboration.ErrCommentConflict
		}
		if err != nil {
			return recordcollaboration.CommentMutationResult{}, fmt.Errorf("load record comment revision replay: %w", err)
		}
	}
	result := recordcollaboration.CommentMutationResult{
		CommentID: command.CommentID, RecordID: command.RecordID, Version: version,
		State: state, EventKind: command.Kind, Replayed: true, ChangedAt: changedAt.UTC(),
	}
	if result.Validate() != nil {
		return recordcollaboration.CommentMutationResult{}, recordcollaboration.ErrCommentConflict
	}
	return result, nil
}

func loadCollaborationRecordReadFenceBinding(ctx context.Context, tx pgx.Tx, recordID string) (recordcollaboration.RecordFenceBinding, error) {
	var epoch int64
	err := tx.QueryRow(ctx, `
		select delivery_epoch
		from public.content_delivery_epochs
		where project_id = $1 and object_kind = 'record' and object_id = $2
		for share`, recordplatform.ProjectIDDefault, recordID).Scan(&epoch)
	if err != nil {
		return recordcollaboration.RecordFenceBinding{}, fmt.Errorf("read collaboration record fence for comments: %w", err)
	}
	if epoch < 0 {
		return recordcollaboration.RecordFenceBinding{}, recordcollaboration.ErrInvalidRecordFenceBinding
	}
	return recordcollaboration.NewRecordFenceBinding(recordplatform.ProjectIDDefault, recordID, recordplatform.ContentEpoch(epoch))
}

func lockRecordRootForCommentRead(ctx context.Context, tx pgx.Tx, recordID string) (lockedRecordRoot, error) {
	var root lockedRecordRoot
	var currentRevisionID *string
	var lockVersion, authorizationEpoch int64
	var lifecycle string
	var visibilityDigest []byte
	var revisionNo *int64
	var revisionCreatedAt *time.Time
	err := tx.QueryRow(ctx, `
		select record_id, project_id, lifecycle, current_revision_id, lock_version,
		       authorization_epoch, current_visibility_digest,
		       (select revision_no from public.record_revisions where revision_id = records.current_revision_id),
		       (select created_at from public.record_revisions where revision_id = records.current_revision_id)
		from public.records
		where record_id = $1
		for share`, recordID).Scan(&root.recordID, &root.projectID, &lifecycle, &currentRevisionID,
		&lockVersion, &authorizationEpoch, &visibilityDigest, &revisionNo, &revisionCreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRecordRoot{}, records.ErrRecordNotFound
	}
	if err != nil {
		return lockedRecordRoot{}, fmt.Errorf("lock record root for comment read: %w", err)
	}
	if lockVersion < 0 || authorizationEpoch < 0 {
		return lockedRecordRoot{}, recordcollaboration.ErrCommentConflict
	}
	root.currentRevisionID = currentRevisionID
	root.lockVersion = uint64(lockVersion)
	root.authorizationEpoch = uint64(authorizationEpoch)
	root.lifecycle = records.Lifecycle(lifecycle)
	return root, nil
}

func listRecordComments(ctx context.Context, tx pgx.Tx, recordID string, binding recordcollaboration.RecordFenceBinding, limit uint64) ([]recordcollaboration.CommentRecord, error) {
	rows, err := tx.Query(ctx, `
		select comment.comment_id, comment.record_id, comment.author_id, comment.comment_version,
		       comment.comment_state, comment.body_markdown, comment.render_model,
		       reply.parent_comment_id, comment.created_at, comment.updated_at, comment.redacted_at,
		       coalesce(array(
		         select mention.mentioned_user_id
		         from public.record_comment_mentions as mention
		         where mention.record_id = comment.record_id
		           and mention.comment_id = comment.comment_id
		           and mention.comment_version = comment.comment_version
		           and mention.record_fence_epoch = comment.record_fence_epoch
		         order by mention.mentioned_user_id
		       ), array[]::text[])
		from (
		  select * from public.record_comments
		  where record_id = $1 and record_fence_epoch = $2
		  order by created_at desc, comment_id desc
		  limit $3
		) as comment
		left join public.record_comment_replies as reply
		  on reply.record_id = comment.record_id and reply.child_comment_id = comment.comment_id
		order by comment.created_at, comment.comment_id`, recordID, int64(binding.Epoch()), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list record comments: %w", err)
	}
	defer rows.Close()
	result := make([]recordcollaboration.CommentRecord, 0)
	for rows.Next() {
		var record recordcollaboration.CommentRecord
		var version int64
		var state string
		var body, renderModel, replyTo *string
		var redactedAt *time.Time
		if err := rows.Scan(&record.CommentID, &record.RecordID, &record.AuthorID, &version, &state,
			&body, &renderModel, &replyTo, &record.CreatedAt, &record.UpdatedAt, &redactedAt, &record.MentionUserIDs); err != nil {
			return nil, fmt.Errorf("scan record comment: %w", err)
		}
		if version <= 0 {
			return nil, recordcollaboration.ErrCommentConflict
		}
		record.Version = uint64(version)
		record.State = recordcollaboration.CommentState(state)
		if replyTo != nil {
			record.ReplyToCommentID = *replyTo
		}
		if body != nil {
			record.BodyMarkdown = *body
		}
		if renderModel != nil {
			model, err := recordcollaboration.DecodeCommentRenderModelV1([]byte(*renderModel))
			if err != nil {
				return nil, recordcollaboration.ErrCommentConflict
			}
			record.RenderModel = model
		}
		if redactedAt != nil {
			value := redactedAt.UTC()
			record.RedactedAt = &value
			record.MentionUserIDs = nil
		}
		record.CreatedAt = record.CreatedAt.UTC()
		record.UpdatedAt = record.UpdatedAt.UTC()
		if record.Validate() != nil {
			return nil, recordcollaboration.ErrCommentConflict
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record comments: %w", err)
	}
	return result, nil
}

func recordCommentRevisionID(commentID string, version uint64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("houfeng.record-comment-revision.v1\x00%s\x00%d", commentID, version)))
	return "rcr_" + hex.EncodeToString(digest[:])
}

func recordCommentTombstoneID(commentID string, version uint64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("houfeng.record-comment-tombstone.v1\x00%s\x00%d", commentID, version)))
	return "rct_" + hex.EncodeToString(digest[:])
}

func recordCommentActivityID(sourceEventID string) string {
	digest := sha256.Sum256([]byte("houfeng.record-comment-activity.v1\x00" + sourceEventID))
	return "rac_" + hex.EncodeToString(digest[:])
}

func recordCommentOutboxKind(kind recordcollaboration.CommentMutationKind) string {
	switch kind {
	case recordcollaboration.CommentMutationCreate:
		return recordplatform.OutboxEventKindRecordCommentCreated
	case recordcollaboration.CommentMutationEdit:
		return recordplatform.OutboxEventKindRecordCommentEdited
	case recordcollaboration.CommentMutationRedact:
		return recordplatform.OutboxEventKindRecordCommentRedacted
	default:
		return ""
	}
}

func recordCommentNotificationOutboxKinds(kind recordcollaboration.CommentMutationKind, replied, mentioned bool) []string {
	base := recordCommentOutboxKind(kind)
	if base == "" {
		return nil
	}
	kinds := []string{base}
	if kind == recordcollaboration.CommentMutationCreate && replied {
		kinds = append(kinds, recordplatform.OutboxEventKindRecordCommentReplied)
	}
	if (kind == recordcollaboration.CommentMutationCreate || kind == recordcollaboration.CommentMutationEdit) && mentioned {
		kinds = append(kinds, recordplatform.OutboxEventKindRecordCommentMentioned)
	}
	return kinds
}

var _ recordcollaboration.CommentStore = (*PostgresRecordCommentRepository)(nil)
