package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// PostgresRecordActionRepository composes the existing record-platform
// transaction primitives with the 0055 action tables. It owns no alternate
// idempotency, authorization, outbox, lease, or deletion mechanism.
type PostgresRecordActionRepository struct {
	platform *PostgresRecordPlatformRepository
	members  CollaborationMembershipReader
}

func NewPostgresRecordActionRepository(pool *pgxpool.Pool, gate AdmissionGate, members CollaborationMembershipReader) *PostgresRecordActionRepository {
	return &PostgresRecordActionRepository{platform: NewPostgresRecordPlatformRepository(pool, gate), members: members}
}

func (repository *PostgresRecordActionRepository) CommitAction(ctx context.Context, command recordcollaboration.ActionCommand) (recordcollaboration.ActionMutationResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) {
		return recordcollaboration.ActionMutationResult{}, recordcollaboration.ErrInvalidActionCommand
	}
	if err := command.Validate(); err != nil {
		return recordcollaboration.ActionMutationResult{}, err
	}

	var result recordcollaboration.ActionMutationResult
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return recordcollaboration.ErrInvalidActionCommand
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
			return recordcollaboration.ErrActionConflict
		}
		if err := records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordUpdate, command.AuthorizationEvidence); err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			if !command.ResultFingerprint.MatchesPersisted(*claim.ReplayResult) {
				return recordplatform.ErrIdempotencyConflictState
			}
			result, err = loadRecordActionReplay(ctx, transaction.tx, command)
			return err
		}

		if command.Kind == recordcollaboration.ActionMutationCreate || command.Kind == recordcollaboration.ActionMutationUpdate {
			if err := repository.validateActionFieldsInTransaction(ctx, transaction.tx, command, binding); err != nil {
				return err
			}
		}

		var previousStatus *recordcollaboration.ActionStatus
		var currentStatus recordcollaboration.ActionStatus
		var version uint64
		var subjectRevisionID string
		var previousAssigneeID string
		var currentAssigneeID string
		switch command.Kind {
		case recordcollaboration.ActionMutationCreate:
			version = 1
			currentStatus = recordcollaboration.ActionStatusOpen
			subjectRevisionID = command.Fields.SubjectRevisionID()
			currentAssigneeID = command.Fields.AssigneeID()
			if err := insertRecordAction(ctx, transaction.tx, command, binding); err != nil {
				return err
			}
		default:
			persisted, err := lockRecordAction(ctx, transaction.tx, command.RecordID, command.ActionID)
			if err != nil {
				return err
			}
			if persisted.version != command.ExpectedVersion {
				return recordcollaboration.ErrActionConflict
			}
			status := persisted.status
			previousAssigneeID = persisted.assigneeID
			currentAssigneeID = persisted.assigneeID
			previousStatus = &status
			version = persisted.version + 1
			currentStatus = persisted.status
			subjectRevisionID = persisted.subjectRevisionID
			if command.Kind == recordcollaboration.ActionMutationUpdate {
				subjectRevisionID = command.Fields.SubjectRevisionID()
				currentAssigneeID = command.Fields.AssigneeID()
			} else {
				currentStatus = targetActionStatus(command.Kind)
				if err := recordcollaboration.ValidateActionMutationTransition(command.Kind, persisted.status, currentStatus); err != nil {
					return err
				}
			}
			if err := updateRecordAction(ctx, transaction.tx, command, binding, persisted, version, currentStatus); err != nil {
				return err
			}
		}

		eventID := recordActionEventID(command.ActionID, version, command.Kind)
		occurredAt, err := insertRecordActionEvent(ctx, transaction.tx, command, binding, eventID, version, previousStatus, currentStatus, currentAssigneeID)
		if err != nil {
			return err
		}
		activityKind, err := recordcollaboration.ActivityKindForActionMutation(command.Kind)
		if err != nil {
			return err
		}
		if err := insertRecordActionActivity(ctx, transaction.tx, command, eventID, version, subjectRevisionID, activityKind, occurredAt); err != nil {
			return err
		}
		automaticSources := []collaborationAutomaticFollowerSources{{userID: command.Actor.UserID, action: true}}
		if currentAssigneeID != "" {
			automaticSources = append(automaticSources, collaborationAutomaticFollowerSources{userID: currentAssigneeID, action: true})
		}
		if err := upsertCollaborationAutomaticFollowerSources(ctx, transaction.tx, binding, automaticSources); err != nil {
			return err
		}
		for _, outboxKind := range recordActionNotificationOutboxKinds(command.Kind, previousAssigneeID, currentAssigneeID) {
			sourceVersion := uint64(0)
			recordFenceEpoch := uint64(0)
			if outboxKind == recordplatform.OutboxEventKindRecordActionAssigned ||
				outboxKind == recordplatform.OutboxEventKindRecordActionCompleted ||
				outboxKind == recordplatform.OutboxEventKindRecordActionCancelled {
				sourceVersion = version
				recordFenceEpoch = uint64(binding.Epoch())
			}
			if _, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
				Event: recordplatform.OutboxEvent{
					ProjectID: string(recordplatform.ProjectIDDefault), EventKind: outboxKind,
					SubjectKind: recordplatform.OutboxSubjectKindAction, SubjectID: command.ActionID,
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
		result = recordcollaboration.ActionMutationResult{
			ActionID: command.ActionID, RecordID: command.RecordID, Version: version,
			Status: currentStatus, EventKind: command.Kind, ChangedAt: occurredAt.UTC(),
		}
		return result.Validate()
	})
	if err != nil {
		return recordcollaboration.ActionMutationResult{}, err
	}
	return result, nil
}

func (repository *PostgresRecordActionRepository) ListActions(ctx context.Context, command recordcollaboration.ActionReadCommand) ([]recordcollaboration.ActionRecord, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, recordcollaboration.ErrInvalidActionRequest
	}
	if err := command.Validate(); err != nil {
		return nil, err
	}
	var result []recordcollaboration.ActionRecord
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
			return recordcollaboration.ErrActionConflict
		}
		if err := records.AuthorizeRecordResource(command.Actor, recordauth.CapabilityRecordRead, command.AuthorizationEvidence); err != nil {
			return err
		}
		loaded, err := listRecordActions(ctx, transaction.tx, command.RecordID, binding, command.Limit)
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

func listRecordActions(ctx context.Context, tx pgx.Tx, recordID string, binding recordcollaboration.RecordFenceBinding, limit uint64) ([]recordcollaboration.ActionRecord, error) {
	rows, err := tx.Query(ctx, `
		select action_id, record_id, action_version, status, title, assignee_id,
		       due_at, completed_at, subject_revision_id, created_at, updated_at
		from public.record_actions
		where record_id = $1 and record_fence_epoch = $2
		order by (status = 'open') desc, due_at asc nulls last, updated_at desc, action_id
		limit $3`, recordID, int64(binding.Epoch()), int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list record actions: %w", err)
	}
	defer rows.Close()
	result := make([]recordcollaboration.ActionRecord, 0)
	for rows.Next() {
		var action recordcollaboration.ActionRecord
		var version int64
		var status string
		var assigneeID, subjectRevisionID *string
		if err := rows.Scan(&action.ActionID, &action.RecordID, &version, &status, &action.Title,
			&assigneeID, &action.DueAt, &action.CompletedAt, &subjectRevisionID, &action.CreatedAt, &action.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan record action: %w", err)
		}
		if version <= 0 {
			return nil, recordcollaboration.ErrActionConflict
		}
		action.Version = uint64(version)
		action.Status = recordcollaboration.ActionStatus(status)
		if assigneeID != nil {
			action.AssigneeID = *assigneeID
		}
		if subjectRevisionID != nil {
			action.SubjectRevisionID = *subjectRevisionID
		}
		action.CreatedAt = action.CreatedAt.UTC()
		action.UpdatedAt = action.UpdatedAt.UTC()
		if action.DueAt != nil {
			value := action.DueAt.UTC()
			action.DueAt = &value
		}
		if action.CompletedAt != nil {
			value := action.CompletedAt.UTC()
			action.CompletedAt = &value
		}
		if action.Validate() != nil {
			return nil, recordcollaboration.ErrActionConflict
		}
		result = append(result, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record actions: %w", err)
	}
	return result, nil
}

func (repository *PostgresRecordActionRepository) validateActionFieldsInTransaction(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand, binding recordcollaboration.RecordFenceBinding) error {
	if command.Fields.SubjectRevisionID() != "" {
		var present int
		err := tx.QueryRow(ctx, `
			select 1 from public.record_revisions
			where record_id = $1 and revision_id = $2`, command.RecordID, command.Fields.SubjectRevisionID()).Scan(&present)
		if errors.Is(err, pgx.ErrNoRows) {
			return recordcollaboration.ErrInvalidActionFields
		}
		if err != nil {
			return fmt.Errorf("validate record action revision subject: %w", err)
		}
		if present != 1 {
			return recordcollaboration.ErrInvalidActionFields
		}
	}
	if command.Fields.AssigneeID() != "" {
		member, err := repository.members.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, command.Fields.AssigneeID())
		if err != nil {
			return fmt.Errorf("validate record action assignee: %w", err)
		}
		if err := records.AuthorizeRecordResource(member, recordauth.CapabilityRecordRead, command.AuthorizationEvidence); err != nil {
			if errors.Is(err, recordauth.ErrDenied) {
				return recordcollaboration.ErrMembershipDenied
			}
			return fmt.Errorf("authorize record action assignee: %w", err)
		}
	}
	if binding.RecordID() != command.RecordID || binding.ProjectID() != recordplatform.ProjectIDDefault {
		return recordcollaboration.ErrInvalidRecordFenceBinding
	}
	return nil
}

func insertRecordAction(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand, binding recordcollaboration.RecordFenceBinding) error {
	var subject any
	if command.Fields.SubjectRevisionID() != "" {
		subject = command.Fields.SubjectRevisionID()
	}
	var assignee any
	if command.Fields.AssigneeID() != "" {
		assignee = command.Fields.AssigneeID()
	}
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_actions (
			action_id, project_id, record_id, subject_revision_id, action_version,
			title, details, status, assignee_id, due_at, completed_at,
			created_by, updated_by, record_fence_epoch, created_at, updated_at
		) values ($1, $2, $3, $4, 1, $5, $6, 'open', $7, $8, null,
			$9, $9, $10, transaction_timestamp(), transaction_timestamp())
		returning created_at, updated_at`,
		command.ActionID, recordplatform.ProjectIDDefault, command.RecordID, subject,
		command.Fields.Title(), command.Fields.Details(), assignee, command.Fields.DueAt(),
		command.Actor.UserID, int64(binding.Epoch()),
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		return fmt.Errorf("insert record action: %w", err)
	}
	if createdAt.IsZero() || !createdAt.Equal(updatedAt) {
		return recordcollaboration.ErrInvalidActionCommand
	}
	return nil
}

type lockedRecordAction struct {
	recordID          string
	version           uint64
	status            recordcollaboration.ActionStatus
	title             string
	details           string
	assigneeID        string
	dueAt             *time.Time
	subjectRevisionID string
	createdAt         time.Time
	updatedAt         time.Time
}

func lockRecordAction(ctx context.Context, tx pgx.Tx, recordID, actionID string) (lockedRecordAction, error) {
	var action lockedRecordAction
	var version int64
	var assigneeID, subjectRevisionID *string
	var status string
	err := tx.QueryRow(ctx, `
		select record_id, action_version, status, title, details, assignee_id,
		       due_at, subject_revision_id, created_at, updated_at
		from public.record_actions
		where record_id = $1 and action_id = $2
		for update`, recordID, actionID).Scan(
		&action.recordID, &version, &status, &action.title, &action.details, &assigneeID,
		&action.dueAt, &subjectRevisionID, &action.createdAt, &action.updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRecordAction{}, recordcollaboration.ErrActionNotFound
	}
	if err != nil {
		return lockedRecordAction{}, fmt.Errorf("lock record action: %w", err)
	}
	if version <= 0 || action.recordID != recordID {
		return lockedRecordAction{}, recordcollaboration.ErrActionConflict
	}
	action.version = uint64(version)
	action.status = recordcollaboration.ActionStatus(status)
	if assigneeID != nil {
		action.assigneeID = *assigneeID
	}
	if subjectRevisionID != nil {
		action.subjectRevisionID = *subjectRevisionID
	}
	if _, err := recordcollaboration.NewActionFilterFacts(action.status, action.assigneeID, action.dueAt); err != nil {
		return lockedRecordAction{}, recordcollaboration.ErrActionConflict
	}
	return action, nil
}

func updateRecordAction(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand, binding recordcollaboration.RecordFenceBinding, persisted lockedRecordAction, version uint64, status recordcollaboration.ActionStatus) error {
	title, details, assigneeID, dueAt, subjectRevisionID := persisted.title, persisted.details, persisted.assigneeID, persisted.dueAt, persisted.subjectRevisionID
	if command.Kind == recordcollaboration.ActionMutationUpdate {
		title, details, assigneeID, dueAt, subjectRevisionID = command.Fields.Title(), command.Fields.Details(), command.Fields.AssigneeID(), command.Fields.DueAt(), command.Fields.SubjectRevisionID()
	}
	var assignee, subject any
	if assigneeID != "" {
		assignee = assigneeID
	}
	if subjectRevisionID != "" {
		subject = subjectRevisionID
	}
	var completedAt any
	if status == recordcollaboration.ActionStatusCompleted {
		if persisted.status == recordcollaboration.ActionStatusCompleted && command.Kind == recordcollaboration.ActionMutationUpdate {
			completedAt = persisted.updatedAt
		} else {
			completedAt = "transaction"
		}
	}
	var updatedAt time.Time
	err := tx.QueryRow(ctx, `
		update public.record_actions
		set subject_revision_id = $4, action_version = $5, title = $6, details = $7,
		    status = $8, assignee_id = $9, due_at = $10,
		    completed_at = case when $11::boolean then transaction_timestamp() when $12::boolean then completed_at else null end,
		    updated_by = $13, record_fence_epoch = $14, updated_at = transaction_timestamp()
		where record_id = $1 and action_id = $2 and action_version = $3
		returning updated_at`,
		command.RecordID, command.ActionID, int64(command.ExpectedVersion), subject, int64(version),
		title, details, string(status), assignee, dueAt,
		completedAt == "transaction", command.Kind == recordcollaboration.ActionMutationUpdate && status == recordcollaboration.ActionStatusCompleted,
		command.Actor.UserID, int64(binding.Epoch()),
	).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordcollaboration.ErrActionConflict
	}
	if err != nil {
		return fmt.Errorf("update record action: %w", err)
	}
	if updatedAt.IsZero() {
		return recordcollaboration.ErrActionConflict
	}
	return nil
}

func insertRecordActionEvent(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand, binding recordcollaboration.RecordFenceBinding, eventID string, version uint64, previous *recordcollaboration.ActionStatus, current recordcollaboration.ActionStatus, assigneeID string) (time.Time, error) {
	var previousValue any
	if previous != nil {
		previousValue = string(*previous)
	}
	var assigneeValue any
	if assigneeID != "" {
		assigneeValue = assigneeID
	}
	var occurredAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_action_events (
			action_event_id, project_id, record_id, action_id, action_version,
			event_kind, previous_status, current_status, actor_id, assignee_id,
			record_fence_epoch, occurred_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, transaction_timestamp())
		returning occurred_at`, eventID, recordplatform.ProjectIDDefault, command.RecordID,
		command.ActionID, int64(version), string(command.Kind), previousValue, string(current),
		command.Actor.UserID, assigneeValue, int64(binding.Epoch())).Scan(&occurredAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record action event: %w", err)
	}
	if occurredAt.IsZero() {
		return time.Time{}, recordcollaboration.ErrInvalidActionCommand
	}
	return occurredAt.UTC(), nil
}

func insertRecordActionActivity(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand, eventID string, version uint64, subjectRevisionID string, kind recordcollaboration.ActionActivityKind, occurredAt time.Time) error {
	var revision any
	if subjectRevisionID != "" {
		revision = subjectRevisionID
	}
	activityID := recordActionActivityID(eventID)
	var eventAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind,
			source_event_id, source_version, actor_id, authorization_epoch,
			record_lock_version, event_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		returning event_at`, activityID, recordplatform.ProjectIDDefault, command.RecordID, revision,
		string(kind), eventID, int64(version), command.Actor.UserID, int64(command.AuthorizationEpoch),
		int64(command.RecordLockVersion), occurredAt).Scan(&eventAt)
	if err != nil {
		return fmt.Errorf("insert record action activity: %w", err)
	}
	if !eventAt.Equal(occurredAt) {
		return recordcollaboration.ErrInvalidActionCommand
	}
	return nil
}

func loadRecordActionReplay(ctx context.Context, tx pgx.Tx, command recordcollaboration.ActionCommand) (recordcollaboration.ActionMutationResult, error) {
	version := command.ExpectedVersion + 1
	if command.Kind == recordcollaboration.ActionMutationCreate {
		version = 1
	}
	var observedKind, observedStatus string
	var occurredAt time.Time
	err := tx.QueryRow(ctx, `
		select event_kind, current_status, occurred_at
		from public.record_action_events
		where record_id = $1 and action_id = $2 and action_version = $3`,
		command.RecordID, command.ActionID, int64(version)).Scan(&observedKind, &observedStatus, &occurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordcollaboration.ActionMutationResult{}, recordcollaboration.ErrActionConflict
	}
	if err != nil {
		return recordcollaboration.ActionMutationResult{}, fmt.Errorf("load record action replay: %w", err)
	}
	result := recordcollaboration.ActionMutationResult{ActionID: command.ActionID, RecordID: command.RecordID, Version: version, Status: recordcollaboration.ActionStatus(observedStatus), EventKind: recordcollaboration.ActionMutationKind(observedKind), Replayed: true, ChangedAt: occurredAt.UTC()}
	if result.EventKind != command.Kind || result.Validate() != nil {
		return recordcollaboration.ActionMutationResult{}, recordcollaboration.ErrActionConflict
	}
	return result, nil
}

func targetActionStatus(kind recordcollaboration.ActionMutationKind) recordcollaboration.ActionStatus {
	switch kind {
	case recordcollaboration.ActionMutationComplete:
		return recordcollaboration.ActionStatusCompleted
	case recordcollaboration.ActionMutationCancel:
		return recordcollaboration.ActionStatusCancelled
	case recordcollaboration.ActionMutationReopen:
		return recordcollaboration.ActionStatusOpen
	default:
		return ""
	}
}

func recordActionOutboxKind(kind recordcollaboration.ActionMutationKind) string {
	switch kind {
	case recordcollaboration.ActionMutationCreate:
		return recordplatform.OutboxEventKindRecordActionCreated
	case recordcollaboration.ActionMutationUpdate:
		return recordplatform.OutboxEventKindRecordActionUpdated
	case recordcollaboration.ActionMutationComplete:
		return recordplatform.OutboxEventKindRecordActionCompleted
	case recordcollaboration.ActionMutationCancel:
		return recordplatform.OutboxEventKindRecordActionCancelled
	case recordcollaboration.ActionMutationReopen:
		return recordplatform.OutboxEventKindRecordActionReopened
	default:
		return ""
	}
}

func recordActionNotificationOutboxKinds(kind recordcollaboration.ActionMutationKind, previousAssigneeID, currentAssigneeID string) []string {
	base := recordActionOutboxKind(kind)
	if base == "" {
		return nil
	}
	kinds := []string{base}
	if (kind == recordcollaboration.ActionMutationCreate || kind == recordcollaboration.ActionMutationUpdate) &&
		currentAssigneeID != "" && currentAssigneeID != previousAssigneeID {
		kinds = append(kinds, recordplatform.OutboxEventKindRecordActionAssigned)
	}
	return kinds
}

func recordActionEventID(actionID string, version uint64, kind recordcollaboration.ActionMutationKind) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("houfeng.record-action-event.v1\x00%s\x00%d\x00%s", actionID, version, kind)))
	return "raev_" + hex.EncodeToString(digest[:])
}

func recordActionActivityID(eventID string) string {
	digest := sha256.Sum256([]byte("houfeng.record-action-activity.v1\x00" + eventID))
	return "rac_" + hex.EncodeToString(digest[:])
}

var _ recordcollaboration.ActionCommandStore = (*PostgresRecordActionRepository)(nil)
