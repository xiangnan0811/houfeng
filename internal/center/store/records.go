package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const recordObjectKind = "record"

type PostgresRecordRepository struct {
	platform     *PostgresRecordPlatformRepository
	participants records.RevisionParticipantRegistry
}

func NewPostgresRecordRepository(
	pool *pgxpool.Pool,
	gate AdmissionGate,
	participants []records.RevisionParticipant,
) (*PostgresRecordRepository, error) {
	registry, err := records.NewRevisionParticipantRegistry(participants)
	if err != nil {
		return nil, err
	}
	return &PostgresRecordRepository{
		platform:     NewPostgresRecordPlatformRepository(pool, gate),
		participants: registry,
	}, nil
}

func (repository *PostgresRecordRepository) CommitRevision(
	ctx context.Context,
	command records.RevisionCommitCommand,
) (records.RevisionCommitResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return records.RevisionCommitResult{}, fmt.Errorf("%w: repository", records.ErrInvalidRevisionCommand)
	}
	if err := command.Validate(); err != nil {
		return records.RevisionCommitResult{}, err
	}
	prepared, err := prepareRecordRevision(command)
	if err != nil {
		return records.RevisionCommitResult{}, err
	}

	var result records.RevisionCommitResult
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return fmt.Errorf("%w: missing idempotency owner", records.ErrInvalidRevisionCommand)
		}

		if err := assertRecordMutationFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			result, err = loadRecordRevisionReplayResult(ctx, transaction.tx, command, prepared)
			return err
		}
		var root lockedRecordRoot
		switch command.Idempotency.Key.OperationKind {
		case recordplatform.OperationKindRecordCreate:
			root, err = createAndLockRecordRoot(ctx, transaction.tx, command.RecordID)
			if err != nil {
				return err
			}
			if root.currentRevisionID != nil || root.currentRevisionNo != 0 || root.lockVersion != command.LockVersion ||
				root.authorizationEpoch != command.AuthorizationEpoch || command.BaseRevisionID != "" {
				return records.ErrRecordRevisionConflict
			}
		case recordplatform.OperationKindRecordUpdate:
			root, err = lockRecordRoot(ctx, transaction.tx, command.RecordID)
			if err != nil {
				return err
			}
			if root.currentRevisionID == nil || *root.currentRevisionID != command.BaseRevisionID ||
				root.currentRevisionNo == 0 || root.lockVersion != command.LockVersion ||
				root.authorizationEpoch != command.AuthorizationEpoch {
				return records.ErrRecordRevisionConflict
			}
		default:
			return fmt.Errorf("%w: operation", records.ErrInvalidRevisionCommand)
		}
		if err := lockPublishedRecordDraft(ctx, transaction.tx, command); err != nil {
			return err
		}

		if root.currentRevisionID != nil && bytes.Equal(root.canonicalHash, prepared.canonicalHash[:]) {
			result = records.RevisionCommitResult{
				RecordID:           command.RecordID,
				RevisionID:         *root.currentRevisionID,
				RevisionNo:         root.currentRevisionNo,
				LockVersion:        root.lockVersion,
				AuthorizationEpoch: root.authorizationEpoch,
				Lifecycle:          root.lifecycle,
				Created:            false,
				CommittedAt:        root.currentRevisionCreatedAt,
			}
			if err := cleanupPublishedRecordDraft(ctx, transaction.tx, command); err != nil {
				return err
			}
			return transaction.CompleteIdempotency(
				ctx,
				command.Idempotency.Key,
				*claim.Owner,
				command.Idempotency.RequestFingerprint,
			)
		}

		revisionNo := root.currentRevisionNo + 1
		nextLockVersion := root.lockVersion + 1
		nextAuthorizationEpoch := root.authorizationEpoch + 1
		createdAt, err := insertRecordRevision(ctx, transaction.tx, command, prepared, revisionNo)
		if err != nil {
			return err
		}
		if err := insertRecordRevisionRelations(ctx, transaction.tx, prepared); err != nil {
			return err
		}
		if err := updateRecordCurrentProjection(ctx, transaction.tx, command, prepared, root, nextLockVersion, nextAuthorizationEpoch); err != nil {
			return err
		}
		if _, err := insertRecordDomainActivity(ctx, transaction.tx, command, prepared, nextLockVersion, nextAuthorizationEpoch); err != nil {
			return err
		}

		result = records.RevisionCommitResult{
			RecordID:           command.RecordID,
			RevisionID:         prepared.revisionID,
			RevisionNo:         revisionNo,
			LockVersion:        nextLockVersion,
			AuthorizationEpoch: nextAuthorizationEpoch,
			Lifecycle:          root.lifecycle,
			Created:            true,
			CommittedAt:        createdAt,
		}
		if err := repository.participants.ApplyRevision(ctx, transaction.tx, records.RevisionCommitted{
			Result:  result,
			Input:   command.Input,
			DraftID: command.DraftID,
		}); err != nil {
			return err
		}
		outboxEventKind := recordplatform.OutboxEventKindRecordCreated
		if command.Idempotency.Key.OperationKind == recordplatform.OperationKindRecordUpdate {
			outboxEventKind = recordplatform.OutboxEventKindRecordUpdated
		}
		if _, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          outboxEventKind,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          command.RecordID,
				AuthorizationEpoch: result.AuthorizationEpoch,
			},
			ExpiresAfter: command.OutboxTTL,
		}); err != nil {
			return err
		}
		if err := cleanupPublishedRecordDraft(ctx, transaction.tx, command); err != nil {
			return err
		}
		if err := transaction.CompleteIdempotency(
			ctx,
			command.Idempotency.Key,
			*claim.Owner,
			command.Idempotency.RequestFingerprint,
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return records.RevisionCommitResult{}, err
	}
	return result, nil
}

func lockPublishedRecordDraft(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) error {
	if command.DraftID == "" {
		return nil
	}
	draft, err := loadRecordDraftForUpdate(ctx, tx, command.DraftID, command.Input.AuthorID())
	if err != nil {
		return err
	}
	if draft.ETag != command.DraftETag {
		return records.ErrDraftConflict
	}
	switch command.Idempotency.Key.OperationKind {
	case recordplatform.OperationKindRecordCreate:
		if draft.RecordID != "" || draft.BaseRevisionID != "" {
			return records.ErrDraftRevisionConflict
		}
	case recordplatform.OperationKindRecordUpdate:
		if draft.RecordID != command.RecordID || draft.BaseRevisionID != command.BaseRevisionID {
			return records.ErrDraftRevisionConflict
		}
	default:
		return fmt.Errorf("%w: operation", records.ErrInvalidRevisionCommand)
	}
	return nil
}

func cleanupPublishedRecordDraft(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) error {
	if command.DraftID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		delete from public.record_draft_checkpoints
		where draft_id = $1`, command.DraftID); err != nil {
		return fmt.Errorf("delete published record draft checkpoints: %w", err)
	}
	etagDigest, err := command.DraftETag.Digest()
	if err != nil {
		return err
	}
	deleted, err := tx.Exec(ctx, `
		delete from public.record_drafts
		where draft_id = $1 and author_id = $2 and etag_digest = $3`,
		command.DraftID, command.Input.AuthorID(), etagDigest[:])
	if err != nil {
		return fmt.Errorf("delete published record draft: %w", err)
	}
	if deleted.RowsAffected() != 1 {
		return records.ErrDraftConflict
	}
	return nil
}

func (repository *PostgresRecordRepository) CommitRecordLifecycle(
	ctx context.Context,
	command records.RecordLifecycleCommand,
) (records.RecordLifecycleResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return records.RecordLifecycleResult{}, fmt.Errorf("%w: repository", records.ErrInvalidRecordLifecycleCommand)
	}
	if err := command.Validate(); err != nil {
		return records.RecordLifecycleResult{}, err
	}
	activityKind, err := command.ActivityKind()
	if err != nil {
		return records.RecordLifecycleResult{}, err
	}
	activityID, err := command.ActivityID()
	if err != nil {
		return records.RecordLifecycleResult{}, err
	}

	var result records.RecordLifecycleResult
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		claim, err := transaction.ClaimIdempotency(ctx, command.Idempotency)
		if err != nil {
			return err
		}
		if (claim.ReplayResult == nil) == (claim.Owner == nil) {
			return fmt.Errorf("%w: missing idempotency owner", records.ErrInvalidRecordLifecycleCommand)
		}
		if err := assertRecordMutationFence(ctx, transaction.tx, command.RecordID); err != nil {
			return err
		}
		if claim.ReplayResult != nil {
			result, err = loadRecordLifecycleReplayResult(
				ctx,
				transaction.tx,
				command,
				activityID,
				activityKind,
			)
			return err
		}

		root, err := lockRecordRoot(ctx, transaction.tx, command.RecordID)
		if err != nil {
			return err
		}
		expectedLifecycle := records.LifecycleActive
		if command.TargetLifecycle == records.LifecycleActive {
			expectedLifecycle = records.LifecycleArchived
		}
		if root.currentRevisionID == nil || *root.currentRevisionID != command.CurrentRevisionID ||
			root.lockVersion != command.LockVersion || root.authorizationEpoch != command.AuthorizationEpoch ||
			root.lifecycle != expectedLifecycle {
			return records.ErrRecordRevisionConflict
		}

		nextLockVersion := root.lockVersion + 1
		nextAuthorizationEpoch := root.authorizationEpoch + 1
		changedAt, err := updateRecordLifecycle(
			ctx,
			transaction.tx,
			command,
			expectedLifecycle,
			nextLockVersion,
			nextAuthorizationEpoch,
		)
		if err != nil {
			return err
		}
		if _, err := insertRecordLifecycleActivity(
			ctx,
			transaction.tx,
			command,
			activityID,
			activityKind,
			nextLockVersion,
			nextAuthorizationEpoch,
		); err != nil {
			return err
		}

		result = records.RecordLifecycleResult{
			RecordID:           command.RecordID,
			CurrentRevisionID:  command.CurrentRevisionID,
			LockVersion:        nextLockVersion,
			AuthorizationEpoch: nextAuthorizationEpoch,
			Lifecycle:          command.TargetLifecycle,
			ChangedAt:          changedAt,
		}
		if _, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          recordplatform.OutboxEventKindRecordUpdated,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          command.RecordID,
				AuthorizationEpoch: result.AuthorizationEpoch,
			},
			ExpiresAfter: command.OutboxTTL,
		}); err != nil {
			return err
		}
		return transaction.CompleteIdempotency(
			ctx,
			command.Idempotency.Key,
			*claim.Owner,
			command.Idempotency.RequestFingerprint,
		)
	})
	if err != nil {
		return records.RecordLifecycleResult{}, err
	}
	return result, nil
}

func loadRecordLifecycleReplayResult(
	ctx context.Context,
	tx pgx.Tx,
	command records.RecordLifecycleCommand,
	activityID string,
	activityKind records.DomainActivityKind,
) (records.RecordLifecycleResult, error) {
	var revisionID string
	var observedActivityKind string
	var lockVersion int64
	var authorizationEpoch int64
	var changedAt time.Time
	err := tx.QueryRow(ctx, `
		select revision_id,
		       event_kind,
		       record_lock_version,
		       authorization_epoch,
		       event_at
		from public.record_domain_activities
		where activity_id = $1
		  and project_id = $2
		  and record_id = $3`,
		activityID,
		recordplatform.ProjectIDDefault,
		command.RecordID,
	).Scan(
		&revisionID,
		&observedActivityKind,
		&lockVersion,
		&authorizationEpoch,
		&changedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.RecordLifecycleResult{}, records.ErrRecordRevisionConflict
	}
	if err != nil {
		return records.RecordLifecycleResult{}, fmt.Errorf("load record lifecycle replay: %w", err)
	}
	if revisionID != command.CurrentRevisionID || observedActivityKind != string(activityKind) ||
		lockVersion != int64(command.LockVersion+1) || authorizationEpoch != int64(command.AuthorizationEpoch+1) ||
		changedAt.IsZero() {
		return records.RecordLifecycleResult{}, records.ErrRecordRevisionConflict
	}
	return records.RecordLifecycleResult{
		RecordID:           command.RecordID,
		CurrentRevisionID:  revisionID,
		LockVersion:        uint64(lockVersion),
		AuthorizationEpoch: uint64(authorizationEpoch),
		Lifecycle:          command.TargetLifecycle,
		Replayed:           true,
		ChangedAt:          changedAt.UTC(),
	}, nil
}

type preparedRecordRevision struct {
	revisionID     string
	activityID     string
	visibilityJSON []byte
	visibilityHash [32]byte
	canonicalHash  [32]byte
	subjects       []preparedRecordRevisionSubject
	tags           []string
	participants   []preparedRecordRevisionParticipant
}

type preparedRecordRevisionSubject struct {
	subject           records.RevisionSubject
	identityJSON      []byte
	authorizationJSON []byte
}

type preparedRecordRevisionParticipant struct {
	participant  records.RevisionParticipantSnapshot
	identityJSON []byte
}

func prepareRecordRevision(command records.RevisionCommitCommand) (preparedRecordRevision, error) {
	revisionID, err := command.RevisionID()
	if err != nil {
		return preparedRecordRevision{}, err
	}
	activityID, err := command.ActivityID()
	if err != nil {
		return preparedRecordRevision{}, err
	}
	visibility := command.Input.VisibilityScope()
	visibilityJSON, err := json.Marshal(visibility)
	if err != nil {
		return preparedRecordRevision{}, fmt.Errorf("marshal record visibility: %w", err)
	}
	prepared := preparedRecordRevision{
		revisionID:     revisionID,
		activityID:     activityID,
		visibilityJSON: visibilityJSON,
		visibilityHash: visibility.CanonicalHash,
		canonicalHash:  command.Input.CanonicalHash(),
		tags:           command.Input.Tags(),
	}
	for _, subject := range command.Input.Subjects() {
		identityJSON, err := json.Marshal(subject.IdentitySnapshot)
		if err != nil {
			return preparedRecordRevision{}, fmt.Errorf("marshal record subject identity: %w", err)
		}
		authorizationJSON, err := json.Marshal(subject.CaptureAuthorization)
		if err != nil {
			return preparedRecordRevision{}, fmt.Errorf("marshal record subject authorization: %w", err)
		}
		prepared.subjects = append(prepared.subjects, preparedRecordRevisionSubject{
			subject:           subject,
			identityJSON:      identityJSON,
			authorizationJSON: authorizationJSON,
		})
	}
	for _, participant := range command.Input.Participants() {
		identityJSON, err := json.Marshal(participant.IdentitySnapshot)
		if err != nil {
			return preparedRecordRevision{}, fmt.Errorf("marshal record participant identity: %w", err)
		}
		prepared.participants = append(prepared.participants, preparedRecordRevisionParticipant{
			participant:  participant,
			identityJSON: identityJSON,
		})
	}
	return prepared, nil
}

func loadRecordRevisionReplayResult(
	ctx context.Context,
	tx pgx.Tx,
	command records.RevisionCommitCommand,
	prepared preparedRecordRevision,
) (records.RevisionCommitResult, error) {
	var revisionID string
	var revisionNo int64
	var committedAt time.Time
	var created bool
	err := tx.QueryRow(ctx, `
		select revision_id,
		       revision_no,
		       created_at,
		       revision_id = $2
		from public.record_revisions
		where record_id = $1
		  and (
		    revision_id = $2
		    or ($3 <> '' and revision_id = $3)
		  )
		order by case when revision_id = $2 then 0 else 1 end
		limit 1`,
		command.RecordID,
		prepared.revisionID,
		command.BaseRevisionID,
	).Scan(&revisionID, &revisionNo, &committedAt, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.RevisionCommitResult{}, records.ErrRecordRevisionConflict
	}
	if err != nil {
		return records.RevisionCommitResult{}, fmt.Errorf("load record revision replay: %w", err)
	}
	if revisionNo <= 0 || committedAt.IsZero() ||
		(command.Idempotency.Key.OperationKind == recordplatform.OperationKindRecordCreate && !created) ||
		(!created && revisionID != command.BaseRevisionID) {
		return records.RevisionCommitResult{}, records.ErrRecordRevisionConflict
	}

	lockVersion := command.LockVersion
	authorizationEpoch := command.AuthorizationEpoch
	if created {
		lockVersion++
		authorizationEpoch++
	}
	return records.RevisionCommitResult{
		RecordID:           command.RecordID,
		RevisionID:         revisionID,
		RevisionNo:         uint64(revisionNo),
		LockVersion:        lockVersion,
		AuthorizationEpoch: authorizationEpoch,
		Lifecycle:          records.LifecycleActive,
		Created:            created,
		Replayed:           true,
		CommittedAt:        committedAt.UTC(),
	}, nil
}

func assertRecordMutationFence(ctx context.Context, tx pgx.Tx, recordID string) error {
	var reservationState string
	err := tx.QueryRow(ctx, `
		select state
		from public.deletion_reservations
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and state in ('previewed', 'fenced', 'committed')
		order by case when state in ('fenced', 'committed') then 0 else 1 end,
		         reservation_id
		limit 1
		for update`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&reservationState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock record deletion reservation: %w", err)
	}
	if err == nil && reservationState != "previewed" {
		return records.ErrRecordDeletionReserved
	}

	if _, err := tx.Exec(ctx, `
		insert into public.content_delivery_epochs (
			project_id, object_kind, object_id, delivery_epoch
		) values ($1, $2, $3, 0)
		on conflict (project_id, object_kind, object_id) do nothing`,
		recordplatform.ProjectIDDefault, recordObjectKind, recordID,
	); err != nil {
		return fmt.Errorf("initialize record content delivery epoch: %w", err)
	}
	var deliveryEpoch int64
	if err := tx.QueryRow(ctx, `
		select delivery_epoch
		from public.content_delivery_epochs
		where project_id = $1 and object_kind = $2 and object_id = $3
		for update`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&deliveryEpoch); err != nil {
		return fmt.Errorf("lock record content delivery epoch: %w", err)
	}
	if deliveryEpoch < 0 {
		return records.ErrRecordDeletionReserved
	}

	var liveFence int
	err = tx.QueryRow(ctx, `
		select 1
		from public.deletion_fence_leases
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and expires_at > transaction_timestamp()
		for update`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&liveFence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock record deletion fence lease: %w", err)
	}
	if err == nil {
		return records.ErrRecordDeletionReserved
	}

	err = tx.QueryRow(ctx, `
		select state
		from public.deletion_reservations
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and state in ('fenced', 'committed')
		order by reservation_id
		limit 1`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&reservationState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("recheck record deletion reservation: %w", err)
	}
	if err == nil {
		return records.ErrRecordDeletionReserved
	}
	return nil
}

type lockedRecordRoot struct {
	recordID                 string
	projectID                string
	lifecycle                records.Lifecycle
	currentRevisionID        *string
	currentRevisionNo        uint64
	currentRevisionCreatedAt time.Time
	lockVersion              uint64
	authorizationEpoch       uint64
	canonicalHash            []byte
}

func createAndLockRecordRoot(ctx context.Context, tx pgx.Tx, recordID string) (lockedRecordRoot, error) {
	command, err := tx.Exec(ctx, `
		insert into public.records (record_id, project_id, lifecycle)
		values ($1, $2, 'active')
		on conflict (record_id) do nothing`, recordID, recordplatform.ProjectIDDefault)
	if err != nil {
		return lockedRecordRoot{}, fmt.Errorf("create record root: %w", err)
	}
	if command.RowsAffected() != 1 {
		return lockedRecordRoot{}, records.ErrRecordAlreadyExists
	}
	return lockRecordRoot(ctx, tx, recordID)
}

func lockRecordRoot(ctx context.Context, tx pgx.Tx, recordID string) (lockedRecordRoot, error) {
	var root lockedRecordRoot
	var lifecycle string
	var lockVersion int64
	var authorizationEpoch int64
	var currentRevisionNo *int64
	var currentRevisionCreatedAt *time.Time
	err := tx.QueryRow(ctx, `
		select root.record_id,
		       root.project_id,
		       root.lifecycle,
		       root.current_revision_id,
		       root.lock_version,
		       root.authorization_epoch,
		       current_revision.canonical_hash,
		       current_revision.revision_no,
		       current_revision.created_at
		from public.records as root
		left join public.record_revisions as current_revision
		  on current_revision.record_id = root.record_id
		 and current_revision.revision_id = root.current_revision_id
		where root.record_id = $1
		for update of root`, recordID).Scan(
		&root.recordID,
		&root.projectID,
		&lifecycle,
		&root.currentRevisionID,
		&lockVersion,
		&authorizationEpoch,
		&root.canonicalHash,
		&currentRevisionNo,
		&currentRevisionCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRecordRoot{}, records.ErrRecordNotFound
	}
	if err != nil {
		return lockedRecordRoot{}, fmt.Errorf("lock record root: %w", err)
	}
	if lockVersion < 0 || authorizationEpoch < 0 {
		return lockedRecordRoot{}, records.ErrRecordRevisionConflict
	}
	root.lifecycle = records.Lifecycle(lifecycle)
	root.lockVersion = uint64(lockVersion)
	root.authorizationEpoch = uint64(authorizationEpoch)
	if root.projectID != string(recordplatform.ProjectIDDefault) || records.ValidateLifecycle(root.lifecycle) != nil {
		return lockedRecordRoot{}, records.ErrRecordRevisionConflict
	}
	if root.currentRevisionID == nil {
		if currentRevisionNo != nil || currentRevisionCreatedAt != nil || len(root.canonicalHash) != 0 {
			return lockedRecordRoot{}, records.ErrRecordRevisionConflict
		}
		return root, nil
	}
	if currentRevisionNo == nil || *currentRevisionNo <= 0 || currentRevisionCreatedAt == nil ||
		currentRevisionCreatedAt.IsZero() || len(root.canonicalHash) != 32 {
		return lockedRecordRoot{}, records.ErrRecordRevisionConflict
	}
	root.currentRevisionNo = uint64(*currentRevisionNo)
	root.currentRevisionCreatedAt = currentRevisionCreatedAt.UTC()
	return root, nil
}

func insertRecordRevision(
	ctx context.Context,
	tx pgx.Tx,
	command records.RevisionCommitCommand,
	prepared preparedRecordRevision,
	revisionNo uint64,
) (time.Time, error) {
	template := command.Input.Template()
	var templateID any
	var templateVersion any
	if template != nil {
		templateID = template.ID
		templateVersion = int64(template.Version)
	}
	var createdAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_revisions (
			revision_id, record_id, project_id, base_revision_id, revision_no,
			title, body_markdown, markdown_dialect_version, record_type,
			business_status, status_group, impact_level, occurred_at, completed_at,
			visibility_scope, visibility_digest, owner_id, follow_up_at,
			template_id, template_version, author_id, save_reason, canonical_hash
		) values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13, $14,
			$15, $16, $17, $18,
			$19, $20, $21, $22, $23
		)
		returning created_at`,
		prepared.revisionID,
		command.RecordID,
		recordplatform.ProjectIDDefault,
		nullableRecordString(command.BaseRevisionID),
		int64(revisionNo),
		command.Input.Title(),
		command.Input.BodyMarkdown(),
		int64(command.Input.MarkdownDialectVersion()),
		string(command.Input.RecordType()),
		nullableRecordString(string(command.Input.BusinessStatus())),
		nullableRecordString(string(command.Input.StatusGroup())),
		string(command.Input.ImpactLevel()),
		command.Input.OccurredAt(),
		command.Input.CompletedAt(),
		prepared.visibilityJSON,
		prepared.visibilityHash[:],
		nullableRecordString(command.Input.OwnerID()),
		command.Input.FollowUpAt(),
		templateID,
		templateVersion,
		command.Input.AuthorID(),
		command.Input.SaveReason(),
		prepared.canonicalHash[:],
	).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record revision: %w", err)
	}
	return createdAt.UTC(), nil
}

func insertRecordRevisionRelations(ctx context.Context, tx pgx.Tx, prepared preparedRecordRevision) error {
	for ordinal, subject := range prepared.subjects {
		commandTag, err := tx.Exec(ctx, `
			insert into public.record_revision_subjects (
				revision_id, ordinal, registry_version, subject_kind, relation_role,
				source_id, is_primary, identity_snapshot, capture_authorization,
				capture_authorization_digest
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			prepared.revisionID,
			int64(ordinal),
			int64(subject.subject.RegistryVersion),
			string(subject.subject.Kind),
			string(subject.subject.Role),
			subject.subject.SourceID,
			subject.subject.Primary,
			subject.identityJSON,
			subject.authorizationJSON,
			subject.subject.CaptureAuthorization.Digest[:],
		)
		if err := expectOneRecordRow(commandTag, err, "insert record revision subject"); err != nil {
			return err
		}
	}
	for ordinal, tag := range prepared.tags {
		commandTag, err := tx.Exec(ctx, `
			insert into public.record_revision_tags (revision_id, ordinal, tag_value)
			values ($1, $2, $3)`, prepared.revisionID, int64(ordinal), tag)
		if err := expectOneRecordRow(commandTag, err, "insert record revision tag"); err != nil {
			return err
		}
	}
	for ordinal, participant := range prepared.participants {
		commandTag, err := tx.Exec(ctx, `
			insert into public.record_revision_participants (
				revision_id, ordinal, participant_id, identity_snapshot
			) values ($1, $2, $3, $4)`,
			prepared.revisionID,
			int64(ordinal),
			participant.participant.ParticipantID,
			participant.identityJSON,
		)
		if err := expectOneRecordRow(commandTag, err, "insert record revision participant"); err != nil {
			return err
		}
	}
	return nil
}

func updateRecordCurrentProjection(
	ctx context.Context,
	tx pgx.Tx,
	command records.RevisionCommitCommand,
	prepared preparedRecordRevision,
	root lockedRecordRoot,
	nextLockVersion uint64,
	nextAuthorizationEpoch uint64,
) error {
	commandTag, err := tx.Exec(ctx, `
		update public.records
		set current_revision_id = $2,
		    current_title = $3,
		    current_record_type = $4,
		    current_business_status = $5,
		    current_status_group = $6,
		    current_impact_level = $7,
		    current_occurred_at = $8,
		    current_completed_at = $9,
		    current_visibility_scope = $10,
		    current_visibility_digest = $11,
		    current_owner_id = $12,
		    current_follow_up_at = $13,
		    lock_version = $14,
		    authorization_epoch = $15,
		    updated_at = transaction_timestamp()
		where record_id = $1
		  and lock_version = $16
		  and current_revision_id is not distinct from $17`,
		command.RecordID,
		prepared.revisionID,
		command.Input.Title(),
		string(command.Input.RecordType()),
		nullableRecordString(string(command.Input.BusinessStatus())),
		nullableRecordString(string(command.Input.StatusGroup())),
		string(command.Input.ImpactLevel()),
		command.Input.OccurredAt(),
		command.Input.CompletedAt(),
		prepared.visibilityJSON,
		prepared.visibilityHash[:],
		nullableRecordString(command.Input.OwnerID()),
		command.Input.FollowUpAt(),
		int64(nextLockVersion),
		int64(nextAuthorizationEpoch),
		int64(root.lockVersion),
		root.currentRevisionID,
	)
	if err != nil {
		return fmt.Errorf("update record current projection: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return records.ErrRecordRevisionConflict
	}
	return nil
}

func updateRecordLifecycle(
	ctx context.Context,
	tx pgx.Tx,
	command records.RecordLifecycleCommand,
	expectedLifecycle records.Lifecycle,
	nextLockVersion uint64,
	nextAuthorizationEpoch uint64,
) (time.Time, error) {
	var changedAt time.Time
	err := tx.QueryRow(ctx, `
		update public.records
		set lifecycle = $2,
		    archived_at = case when $2 = 'archived' then transaction_timestamp() else null end,
		    lock_version = $3,
		    authorization_epoch = $4,
		    updated_at = transaction_timestamp()
		where record_id = $1
		  and lifecycle = $5
		  and current_revision_id = $6
		  and lock_version = $7
		  and authorization_epoch = $8
		returning updated_at`,
		command.RecordID,
		string(command.TargetLifecycle),
		int64(nextLockVersion),
		int64(nextAuthorizationEpoch),
		string(expectedLifecycle),
		command.CurrentRevisionID,
		int64(command.LockVersion),
		int64(command.AuthorizationEpoch),
	).Scan(&changedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, records.ErrRecordRevisionConflict
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("update record lifecycle: %w", err)
	}
	return changedAt.UTC(), nil
}

func insertRecordDomainActivity(
	ctx context.Context,
	tx pgx.Tx,
	command records.RevisionCommitCommand,
	prepared preparedRecordRevision,
	lockVersion uint64,
	authorizationEpoch uint64,
) (time.Time, error) {
	var eventAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind,
			source_event_id, source_version, actor_id, authorization_epoch,
			record_lock_version, event_at
		) values ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, transaction_timestamp())
		returning event_at`,
		prepared.activityID,
		recordplatform.ProjectIDDefault,
		command.RecordID,
		prepared.revisionID,
		string(command.ActivityKind),
		prepared.revisionID,
		command.Input.AuthorID(),
		int64(authorizationEpoch),
		int64(lockVersion),
	).Scan(&eventAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record domain activity: %w", err)
	}
	return eventAt.UTC(), nil
}

func insertRecordLifecycleActivity(
	ctx context.Context,
	tx pgx.Tx,
	command records.RecordLifecycleCommand,
	activityID string,
	activityKind records.DomainActivityKind,
	lockVersion uint64,
	authorizationEpoch uint64,
) (time.Time, error) {
	var eventAt time.Time
	err := tx.QueryRow(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind,
			source_event_id, source_version, actor_id, authorization_epoch,
			record_lock_version, event_at
		) values ($1, $2, $3, $4, $5, $1, 1, $6, $7, $8, transaction_timestamp())
		returning event_at`,
		activityID,
		recordplatform.ProjectIDDefault,
		command.RecordID,
		command.CurrentRevisionID,
		string(activityKind),
		command.ActorID,
		int64(authorizationEpoch),
		int64(lockVersion),
	).Scan(&eventAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("insert record lifecycle activity: %w", err)
	}
	return eventAt.UTC(), nil
}

func expectOneRecordRow(commandTag pgconn.CommandTag, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("%s: %w", operation, records.ErrRecordRevisionConflict)
	}
	return nil
}

func nullableRecordString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
