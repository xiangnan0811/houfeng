package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const maxRecordReadPageSize = uint64(200)

var _ records.RecordReadStore = (*PostgresRecordRepository)(nil)

func (repository *PostgresRecordRepository) ListRecordCandidates(
	ctx context.Context,
	page records.RecordCandidatePage,
) ([]records.RecordCandidate, error) {
	if ctx == nil || repository == nil || repository.platform == nil || page.Limit == 0 ||
		page.Limit > maxRecordReadPageSize ||
		(page.Sort != records.RecordSortUpdatedDesc && page.Sort != records.RecordSortUpdatedAsc) {
		return nil, records.ErrInvalidRecordReadRequest
	}
	var afterTime any
	afterID := ""
	if page.After != nil {
		if page.After.UpdatedAt.IsZero() || !validStoredRecordIdentity(page.After.RecordID, "rec_") {
			return nil, records.ErrInvalidRecordReadRequest
		}
		afterTime = page.After.UpdatedAt.UTC()
		afterID = page.After.RecordID
	}

	var candidates []records.RecordCandidate
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		comparison := "<"
		direction := "desc"
		if page.Sort == records.RecordSortUpdatedAsc {
			comparison = ">"
			direction = "asc"
		}
		sql := fmt.Sprintf(`
			select records.record_id, records.updated_at
			from public.records records
			where records.current_revision_id is not null
			  and ($1::timestamptz is null or (records.updated_at, records.record_id) %s ($1, $2))
			  and not exists (
				select 1
				from public.deletion_reservations reservations
				where reservations.project_id = $4
				  and reservations.object_kind = $5
				  and reservations.object_id = records.record_id
				  and reservations.state in ('fenced', 'committed')
			  )
			order by records.updated_at %s, records.record_id %s
			limit $3`, comparison, direction, direction)
		rows, err := transaction.tx.Query(
			ctx,
			sql,
			afterTime,
			afterID,
			int64(page.Limit),
			recordplatform.ProjectIDDefault,
			recordObjectKind,
		)
		if err != nil {
			return fmt.Errorf("list record read candidates: %w", err)
		}
		if rows == nil {
			return records.ErrInvalidRecordReadRequest
		}
		defer rows.Close()
		for rows.Next() {
			var candidate records.RecordCandidate
			if err := rows.Scan(&candidate.RecordID, &candidate.UpdatedAt); err != nil {
				return fmt.Errorf("scan record read candidate: %w", err)
			}
			candidate.UpdatedAt = candidate.UpdatedAt.UTC()
			if !validStoredRecordIdentity(candidate.RecordID, "rec_") || candidate.UpdatedAt.IsZero() {
				return records.ErrInvalidRecordReadRequest
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate record read candidates: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func (repository *PostgresRecordRepository) ListRevisionCandidates(
	ctx context.Context,
	page records.RecordRevisionCandidatePage,
) ([]records.RecordRevisionCandidate, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		!validStoredRecordIdentity(page.RecordID, "rec_") ||
		!validStoredRecordIdentity(page.CurrentRevisionID, "rrv_") ||
		page.LockVersion == 0 || page.AuthorizationEpoch == 0 || page.Limit == 0 ||
		page.Limit > maxRecordReadPageSize {
		return nil, records.ErrInvalidRecordReadRequest
	}
	var candidates []records.RecordRevisionCandidate
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, page.RecordID); err != nil {
			return err
		}
		rows, err := transaction.tx.Query(ctx, `
			select revisions.revision_id, revisions.revision_no
			from public.records records
			join public.record_revisions revisions
			  on revisions.record_id = records.record_id
			where records.record_id = $1
			  and records.current_revision_id = $2
			  and records.lock_version = $3
			  and records.authorization_epoch = $4
			order by revisions.revision_no desc
			limit $5`,
			page.RecordID,
			page.CurrentRevisionID,
			int64(page.LockVersion),
			int64(page.AuthorizationEpoch),
			int64(page.Limit),
		)
		if err != nil {
			return fmt.Errorf("list record revision candidates: %w", err)
		}
		if rows == nil {
			return records.ErrInvalidRecordReadRequest
		}
		defer rows.Close()
		for rows.Next() {
			var revisionID string
			var revisionNo int64
			if err := rows.Scan(&revisionID, &revisionNo); err != nil {
				return fmt.Errorf("scan record revision candidate: %w", err)
			}
			if !validStoredRecordIdentity(revisionID, "rrv_") || revisionNo <= 0 {
				return records.ErrInvalidRecordReadRequest
			}
			candidates = append(candidates, records.RecordRevisionCandidate{
				RevisionID: revisionID,
				RevisionNo: uint64(revisionNo),
			})
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate record revision candidates: %w", err)
		}
		if len(candidates) == 0 {
			return records.ErrRecordRevisionConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func (repository *PostgresRecordRepository) ReadRecordRevision(
	ctx context.Context,
	request records.StoredRecordRevisionRequest,
) (records.StoredRecordRevision, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		!validStoredRecordIdentity(request.RecordID, "rec_") ||
		!validStoredRecordIdentity(request.RevisionID, "rrv_") ||
		!validStoredRecordIdentity(request.CurrentRevisionID, "rrv_") ||
		request.LockVersion == 0 || request.AuthorizationEpoch == 0 {
		return records.StoredRecordRevision{}, records.ErrInvalidRecordReadRequest
	}

	var result records.StoredRecordRevision
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, request.RecordID); err != nil {
			return err
		}
		loaded, err := loadStoredRecordRevision(ctx, transaction.tx, request)
		if err != nil {
			return err
		}
		result = loaded
		return nil
	})
	if err != nil {
		return records.StoredRecordRevision{}, err
	}
	return result, nil
}

func assertRecordReadFence(ctx context.Context, tx pgx.Tx, recordID string) error {
	var reservationState string
	err := tx.QueryRow(ctx, `
		select state
		from public.deletion_reservations
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and state in ('fenced', 'committed')
		order by reservation_id
		limit 1`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&reservationState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read record deletion reservation: %w", err)
	}
	if err == nil {
		return records.ErrRecordDeletionReserved
	}

	var deliveryEpoch int64
	err = tx.QueryRow(ctx, `
		select delivery_epoch
		from public.content_delivery_epochs
		where project_id = $1 and object_kind = $2 and object_id = $3
		for share`, recordplatform.ProjectIDDefault, recordObjectKind, recordID).Scan(&deliveryEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.ErrRecordDeletionReserved
	}
	if err != nil {
		return fmt.Errorf("lock record content delivery epoch for read: %w", err)
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
		  and expires_at > transaction_timestamp()`,
		recordplatform.ProjectIDDefault, recordObjectKind, recordID,
	).Scan(&liveFence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read record deletion fence lease: %w", err)
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
		return fmt.Errorf("recheck record deletion reservation for read: %w", err)
	}
	if err == nil {
		return records.ErrRecordDeletionReserved
	}
	return nil
}

func loadStoredRecordRevision(
	ctx context.Context,
	tx pgx.Tx,
	request records.StoredRecordRevisionRequest,
) (records.StoredRecordRevision, error) {
	var (
		rootProjectID     string
		revisionProjectID string
		lifecycle         string
		baseRevisionID    *string
		revisionNo        int64
		title             string
		bodyMarkdown      string
		markdownVersion   int64
		recordType        string
		businessStatus    *string
		persistedGroup    *string
		impactLevel       string
		occurredAt        *time.Time
		completedAt       *time.Time
		visibilityJSON    []byte
		visibilityDigest  []byte
		ownerID           *string
		followUpAt        *time.Time
		templateID        *string
		templateVersion   *int64
		authorID          string
		saveReason        string
		canonicalHash     []byte
		revisionCreatedAt time.Time
		recordCreatedAt   time.Time
		recordUpdatedAt   time.Time
		archivedAt        *time.Time
	)
	err := tx.QueryRow(ctx, `
		select roots.project_id,
		       roots.lifecycle,
		       roots.created_at,
		       roots.updated_at,
		       roots.archived_at,
		       revisions.project_id,
		       revisions.base_revision_id,
		       revisions.revision_no,
		       revisions.title,
		       revisions.body_markdown,
		       revisions.markdown_dialect_version,
		       revisions.record_type,
		       revisions.business_status,
		       revisions.status_group,
		       revisions.impact_level,
		       revisions.occurred_at,
		       revisions.completed_at,
		       revisions.visibility_scope,
		       revisions.visibility_digest,
		       revisions.owner_id,
		       revisions.follow_up_at,
		       revisions.template_id,
		       revisions.template_version,
		       revisions.author_id,
		       revisions.save_reason,
		       revisions.canonical_hash,
		       revisions.created_at
		from public.records roots
		join public.record_revisions revisions
		  on revisions.record_id = roots.record_id
		 and revisions.revision_id = $2
		where roots.record_id = $1
		  and roots.current_revision_id = $3
		  and roots.lock_version = $4
		  and roots.authorization_epoch = $5`,
		request.RecordID,
		request.RevisionID,
		request.CurrentRevisionID,
		int64(request.LockVersion),
		int64(request.AuthorizationEpoch),
	).Scan(
		&rootProjectID,
		&lifecycle,
		&recordCreatedAt,
		&recordUpdatedAt,
		&archivedAt,
		&revisionProjectID,
		&baseRevisionID,
		&revisionNo,
		&title,
		&bodyMarkdown,
		&markdownVersion,
		&recordType,
		&businessStatus,
		&persistedGroup,
		&impactLevel,
		&occurredAt,
		&completedAt,
		&visibilityJSON,
		&visibilityDigest,
		&ownerID,
		&followUpAt,
		&templateID,
		&templateVersion,
		&authorID,
		&saveReason,
		&canonicalHash,
		&revisionCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return records.StoredRecordRevision{}, records.ErrRecordRevisionConflict
	}
	if err != nil {
		return records.StoredRecordRevision{}, fmt.Errorf("load record revision content: %w", err)
	}
	if rootProjectID != string(recordauth.ProjectIDDefault) || revisionProjectID != rootProjectID ||
		revisionNo <= 0 || markdownVersion <= 0 || recordCreatedAt.IsZero() ||
		recordUpdatedAt.IsZero() || revisionCreatedAt.IsZero() {
		return records.StoredRecordRevision{}, records.ErrInvalidRecordReadRequest
	}
	visibility, err := decodeStoredRecordVisibility(visibilityJSON, visibilityDigest)
	if err != nil {
		return records.StoredRecordRevision{}, err
	}
	subjectInputs, err := loadRecordRevisionAuthorizationSubjects(ctx, tx, request.RevisionID)
	if err != nil {
		return records.StoredRecordRevision{}, err
	}
	subjects := make([]records.RevisionSubject, 0, len(subjectInputs))
	for _, subject := range subjectInputs {
		subjects = append(subjects, records.RevisionSubject{
			RegistryVersion:      subject.Reference.RegistryVersion,
			Kind:                 subject.Reference.Kind,
			Role:                 subject.Reference.Role,
			SourceID:             subject.Reference.SourceID,
			Primary:              subject.Reference.Primary,
			IdentitySnapshot:     subject.IdentitySnapshot.Fields(),
			CaptureAuthorization: subject.CaptureAuthorization,
		})
	}
	tags, err := loadStoredRecordRevisionTags(ctx, tx, request.RevisionID)
	if err != nil {
		return records.StoredRecordRevision{}, err
	}
	participants, err := loadStoredRecordRevisionParticipants(ctx, tx, request.RevisionID)
	if err != nil {
		return records.StoredRecordRevision{}, err
	}
	values := records.CompleteRevisionValues{
		Title:                  title,
		BodyMarkdown:           bodyMarkdown,
		MarkdownDialectVersion: records.MarkdownDialectVersion(markdownVersion),
		RecordType:             records.RecordType(recordType),
		ImpactLevel:            records.ImpactLevel(impactLevel),
		OccurredAt:             occurredAt,
		CompletedAt:            completedAt,
		VisibilityScope:        visibility,
		Subjects:               subjects,
		Tags:                   tags,
		Participants:           participants,
		FollowUpAt:             followUpAt,
		AuthorID:               authorID,
		SaveReason:             saveReason,
	}
	if businessStatus != nil {
		values.BusinessStatus = records.BusinessStatus(*businessStatus)
	}
	if ownerID != nil {
		values.OwnerID = *ownerID
	}
	if templateID != nil && templateVersion != nil && *templateVersion > 0 {
		values.Template = &records.TemplateProvenance{ID: *templateID, Version: uint64(*templateVersion)}
	} else if templateID != nil || templateVersion != nil {
		return records.StoredRecordRevision{}, records.ErrInvalidRecordReadRequest
	}
	input, err := records.NormalizeCompleteRevisionInput(values)
	if err != nil {
		return records.StoredRecordRevision{}, fmt.Errorf("validate stored record revision content: %w", err)
	}
	hash := input.CanonicalHash()
	if !bytes.Equal(canonicalHash, hash[:]) ||
		(persistedGroup == nil) != (input.StatusGroup() == "") ||
		(persistedGroup != nil && *persistedGroup != string(input.StatusGroup())) {
		return records.StoredRecordRevision{}, records.ErrInvalidRecordReadRequest
	}
	result := records.StoredRecordRevision{
		RecordID:           request.RecordID,
		RevisionID:         request.RevisionID,
		RevisionNo:         uint64(revisionNo),
		LockVersion:        request.LockVersion,
		AuthorizationEpoch: request.AuthorizationEpoch,
		Lifecycle:          records.Lifecycle(lifecycle),
		Input:              input,
		CreatedAt:          revisionCreatedAt.UTC(),
		RecordCreatedAt:    recordCreatedAt.UTC(),
		RecordUpdatedAt:    recordUpdatedAt.UTC(),
		ArchivedAt:         normalizedStoreTimePointer(archivedAt),
	}
	if baseRevisionID != nil {
		result.BaseRevisionID = *baseRevisionID
	}
	if records.ValidateLifecycle(result.Lifecycle) != nil ||
		(result.Lifecycle == records.LifecycleArchived) != (result.ArchivedAt != nil) {
		return records.StoredRecordRevision{}, records.ErrInvalidRecordReadRequest
	}
	return result, nil
}

func loadStoredRecordRevisionTags(ctx context.Context, tx pgx.Tx, revisionID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		select ordinal, tag_value
		from public.record_revision_tags
		where revision_id = $1
		order by ordinal asc`, revisionID)
	if err != nil {
		return nil, fmt.Errorf("load record revision tags: %w", err)
	}
	if rows == nil {
		return nil, records.ErrInvalidRecordReadRequest
	}
	defer rows.Close()
	var tags []string
	for expected := int64(0); rows.Next(); expected++ {
		var ordinal int64
		var tag string
		if err := rows.Scan(&ordinal, &tag); err != nil {
			return nil, fmt.Errorf("scan record revision tag: %w", err)
		}
		if ordinal != expected {
			return nil, records.ErrInvalidRecordReadRequest
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record revision tags: %w", err)
	}
	return tags, nil
}

func loadStoredRecordRevisionParticipants(
	ctx context.Context,
	tx pgx.Tx,
	revisionID string,
) ([]records.RevisionParticipantSnapshot, error) {
	rows, err := tx.Query(ctx, `
		select ordinal, participant_id, identity_snapshot
		from public.record_revision_participants
		where revision_id = $1
		order by ordinal asc`, revisionID)
	if err != nil {
		return nil, fmt.Errorf("load record revision participants: %w", err)
	}
	if rows == nil {
		return nil, records.ErrInvalidRecordReadRequest
	}
	defer rows.Close()
	var participants []records.RevisionParticipantSnapshot
	for expected := int64(0); rows.Next(); expected++ {
		var ordinal int64
		var participantID string
		var identityJSON []byte
		if err := rows.Scan(&ordinal, &participantID, &identityJSON); err != nil {
			return nil, fmt.Errorf("scan record revision participant: %w", err)
		}
		identity := make(map[string]string)
		if ordinal != expected || decodeStoredRecordJSON(identityJSON, &identity) != nil {
			return nil, records.ErrInvalidRecordReadRequest
		}
		participants = append(participants, records.RevisionParticipantSnapshot{
			ParticipantID:    participantID,
			IdentitySnapshot: identity,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record revision participants: %w", err)
	}
	return participants, nil
}

func normalizedStoreTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
