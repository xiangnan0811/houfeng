package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type collaborationRevisionParticipant struct {
	members CollaborationMembershipReader
}

func NewCollaborationRevisionParticipant(members CollaborationMembershipReader) records.RevisionParticipant {
	return &collaborationRevisionParticipant{members: members}
}

func (*collaborationRevisionParticipant) Name() string { return "collaboration" }

func (participant *collaborationRevisionParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	if ctx == nil || participant == nil || nilCollaborationDependency(participant.members) ||
		nilCollaborationMembershipTx(tx) || nilCollaborationDependency(committed.Outbox) {
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	if err := validateCollaborationRevisionCommitted(committed); err != nil {
		return err
	}

	currentFacts, err := collaborationRevisionFilterFacts(committed.Input)
	if err != nil {
		return err
	}
	resource := records.RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: committed.Input.VisibilityScope(),
		Sources:    collaborationRevisionSources(committed.Input),
	}
	memberIDs := append(currentFacts.ParticipantIDs(), currentFacts.OwnerID())
	sort.Strings(memberIDs)
	memberIDs = slices.Compact(memberIDs)
	for _, memberID := range memberIDs {
		if memberID == "" {
			continue
		}
		actor, err := participant.members.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, memberID)
		if err != nil {
			return fmt.Errorf("validate collaboration revision member: %w", err)
		}
		if err := records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, resource); err != nil {
			return fmt.Errorf("authorize collaboration revision member: %w", err)
		}
	}

	previousFacts, err := loadPreviousCollaborationRevisionFacts(ctx, tx, committed.BaseRevisionID)
	if err != nil {
		return err
	}
	if err := assertRecordMutationFence(ctx, tx, committed.Result.RecordID); err != nil {
		return fmt.Errorf("recheck collaboration record deletion fence: %w", err)
	}
	binding, err := loadCollaborationRecordFenceBinding(ctx, tx, committed.Result.RecordID)
	if err != nil {
		return err
	}
	if err := reconcileCollaborationRevisionFollowers(ctx, tx, binding, committed.Input); err != nil {
		return err
	}

	changedFields := recordcollaboration.DiffRevisionFilterFacts(previousFacts, currentFacts)
	for _, field := range changedFields {
		if err := insertCollaborationRevisionActivity(ctx, tx, committed, field); err != nil {
			return err
		}
	}
	for _, field := range changedFields {
		outboxKind, ok := collaborationRevisionOutboxKind(field)
		if !ok {
			continue
		}
		if _, err := committed.Outbox.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          outboxKind,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          committed.Result.RecordID,
				AuthorizationEpoch: committed.Result.AuthorizationEpoch,
			},
			ExpiresAfter: committed.OutboxTTL,
		}); err != nil {
			return fmt.Errorf("enqueue collaboration revision outbox fact: %w", err)
		}
	}
	return nil
}

func validateCollaborationRevisionCommitted(committed records.RevisionCommitted) error {
	if committed.Result.RevisionNo == 0 || committed.Result.LockVersion == 0 ||
		committed.Result.AuthorizationEpoch == 0 || !committed.Result.Created ||
		committed.Result.CommittedAt.IsZero() || committed.OutboxTTL.Microseconds() <= 0 ||
		!validCollaborationRevisionIdentity(committed.Result.RecordID, "rec_") ||
		!validCollaborationRevisionIdentity(committed.Result.RevisionID, "rrv_") {
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	switch committed.ActivityKind {
	case records.DomainActivityRecordCreated:
		if committed.BaseRevisionID != "" {
			return recordcollaboration.ErrRevisionParticipationUnavailable
		}
	case records.DomainActivityRecordRevised, records.DomainActivityRecordRestored:
		if !validCollaborationRevisionIdentity(committed.BaseRevisionID, "rrv_") {
			return recordcollaboration.ErrRevisionParticipationUnavailable
		}
	default:
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	if committed.Input.AuthorID() == "" || recordauth.ValidateActorUserID(committed.Input.AuthorID()) != nil ||
		committed.Input.VisibilityScope().ProjectID != recordauth.ProjectIDDefault ||
		len(committed.Input.Subjects()) == 0 {
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	return nil
}

func collaborationRevisionFilterFacts(input records.CompleteRevisionInput) (recordcollaboration.RevisionFilterFacts, error) {
	participantIDs := make([]string, 0, len(input.Participants()))
	for _, participant := range input.Participants() {
		participantIDs = append(participantIDs, participant.ParticipantID)
	}
	facts, err := recordcollaboration.NormalizeRevisionFilterFacts(recordcollaboration.RevisionFilterFactValues{
		OwnerID: input.OwnerID(), ParticipantIDs: participantIDs, FollowUpAt: input.FollowUpAt(),
	})
	if err != nil {
		return recordcollaboration.RevisionFilterFacts{}, err
	}
	return facts, nil
}

func collaborationRevisionSources(input records.CompleteRevisionInput) []recordauth.SourceAuthorization {
	subjects := input.Subjects()
	sources := make([]recordauth.SourceAuthorization, 0, len(subjects))
	for _, subject := range subjects {
		sources = append(sources, subject.CaptureAuthorization)
	}
	return sources
}

func loadPreviousCollaborationRevisionFacts(
	ctx context.Context,
	tx pgx.Tx,
	baseRevisionID string,
) (recordcollaboration.RevisionFilterFacts, error) {
	if baseRevisionID == "" {
		return recordcollaboration.NormalizeRevisionFilterFacts(recordcollaboration.RevisionFilterFactValues{})
	}
	var ownerID string
	var followUpAt *time.Time
	if err := tx.QueryRow(ctx, `
		select coalesce(owner_id, ''), follow_up_at
		from public.record_revisions
		where revision_id = $1`, baseRevisionID).Scan(&ownerID, &followUpAt); err != nil {
		return recordcollaboration.RevisionFilterFacts{}, fmt.Errorf("read previous collaboration revision facts: %w", err)
	}
	rows, err := tx.Query(ctx, `
		select participant_id
		from public.record_revision_participants
		where revision_id = $1
		order by participant_id`, baseRevisionID)
	if err != nil {
		return recordcollaboration.RevisionFilterFacts{}, fmt.Errorf("query previous collaboration participants: %w", err)
	}
	defer rows.Close()
	participantIDs := make([]string, 0)
	for rows.Next() {
		var participantID string
		if err := rows.Scan(&participantID); err != nil {
			return recordcollaboration.RevisionFilterFacts{}, fmt.Errorf("scan previous collaboration participant: %w", err)
		}
		participantIDs = append(participantIDs, participantID)
	}
	if err := rows.Err(); err != nil {
		return recordcollaboration.RevisionFilterFacts{}, fmt.Errorf("iterate previous collaboration participants: %w", err)
	}
	facts, err := recordcollaboration.NormalizeRevisionFilterFacts(recordcollaboration.RevisionFilterFactValues{
		OwnerID: ownerID, ParticipantIDs: participantIDs, FollowUpAt: followUpAt,
	})
	if err != nil {
		return recordcollaboration.RevisionFilterFacts{}, fmt.Errorf("normalize previous collaboration revision facts: %w", err)
	}
	return facts, nil
}

func loadCollaborationRecordFenceBinding(
	ctx context.Context,
	tx pgx.Tx,
	recordID string,
) (recordcollaboration.RecordFenceBinding, error) {
	var epoch int64
	if err := tx.QueryRow(ctx, `
		select delivery_epoch
		from public.content_delivery_epochs
		where project_id = $1 and object_kind = 'record' and object_id = $2
		for update`, recordplatform.ProjectIDDefault, recordID).Scan(&epoch); err != nil {
		return recordcollaboration.RecordFenceBinding{}, fmt.Errorf("read collaboration record fence: %w", err)
	}
	if epoch < 0 {
		return recordcollaboration.RecordFenceBinding{}, recordcollaboration.ErrInvalidRecordFenceBinding
	}
	binding, err := recordcollaboration.NewRecordFenceBinding(
		recordplatform.ProjectIDDefault,
		recordID,
		recordplatform.ContentEpoch(epoch),
	)
	if err != nil {
		return recordcollaboration.RecordFenceBinding{}, err
	}
	return binding, nil
}

type collaborationFollowerSources struct {
	author      bool
	owner       bool
	participant bool
}

func reconcileCollaborationRevisionFollowers(
	ctx context.Context,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	input records.CompleteRevisionInput,
) error {
	desired := map[string]collaborationFollowerSources{
		input.AuthorID(): {author: true},
	}
	if ownerID := input.OwnerID(); ownerID != "" {
		sources := desired[ownerID]
		sources.owner = true
		desired[ownerID] = sources
	}
	for _, participant := range input.Participants() {
		sources := desired[participant.ParticipantID]
		sources.participant = true
		desired[participant.ParticipantID] = sources
	}
	userIDs := make([]string, 0, len(desired))
	for userID := range desired {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	for _, userID := range userIDs {
		sources := desired[userID]
		if _, err := tx.Exec(ctx, `
			insert into public.record_followers (
				project_id, record_id, user_id, follower_version,
				follows_author, follows_owner, follows_participant, record_fence_epoch
			) values ($1, $2, $3, 1, $4, $5, $6, $7)
			on conflict (record_id, user_id) do update
			set follower_version = record_followers.follower_version + 1,
			    follows_author = excluded.follows_author,
			    follows_owner = excluded.follows_owner,
			    follows_participant = excluded.follows_participant,
			    record_fence_epoch = excluded.record_fence_epoch,
			    updated_at = transaction_timestamp()
			where (record_followers.follows_author,
			       record_followers.follows_owner,
			       record_followers.follows_participant,
			       record_followers.record_fence_epoch)
			      is distinct from
			      (excluded.follows_author, excluded.follows_owner,
			       excluded.follows_participant, excluded.record_fence_epoch)`,
			binding.ProjectID(), binding.RecordID(), userID,
			sources.author, sources.owner, sources.participant, int64(binding.Epoch()),
		); err != nil {
			return fmt.Errorf("reconcile collaboration revision follower: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		delete from public.record_followers
		where record_id = $1
		  and not (user_id = any($2::text[]))
		  and manual_preference = 'default'
		  and not follows_comment and not follows_mention and not follows_action`,
		binding.RecordID(), userIDs,
	); err != nil {
		return fmt.Errorf("delete stale collaboration revision follower: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update public.record_followers
		set follower_version = follower_version + 1,
		    follows_author = false,
		    follows_owner = false,
		    follows_participant = false,
		    record_fence_epoch = $3,
		    updated_at = transaction_timestamp()
		where record_id = $1
		  and not (user_id = any($2::text[]))
		  and (follows_author or follows_owner or follows_participant)`,
		binding.RecordID(), userIDs, int64(binding.Epoch()),
	); err != nil {
		return fmt.Errorf("update stale collaboration revision follower: %w", err)
	}
	return nil
}

func insertCollaborationRevisionActivity(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
	field recordcollaboration.RevisionFieldKind,
) error {
	activityKind, ok := collaborationRevisionActivityKind(field)
	if !ok {
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	activityID := collaborationRevisionActivityID(committed.Result.RevisionID, activityKind)
	var eventAt time.Time
	if err := tx.QueryRow(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind,
			source_event_id, source_version, actor_id, authorization_epoch,
			record_lock_version, event_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, transaction_timestamp())
		returning event_at`,
		activityID,
		recordplatform.ProjectIDDefault,
		committed.Result.RecordID,
		committed.Result.RevisionID,
		string(activityKind),
		committed.Result.RevisionID+":"+string(field),
		int64(committed.Result.RevisionNo),
		committed.Input.AuthorID(),
		int64(committed.Result.AuthorizationEpoch),
		int64(committed.Result.LockVersion),
	).Scan(&eventAt); err != nil {
		return fmt.Errorf("insert collaboration revision activity fact: %w", err)
	}
	if eventAt.IsZero() {
		return recordcollaboration.ErrRevisionParticipationUnavailable
	}
	return nil
}

func collaborationRevisionActivityKind(field recordcollaboration.RevisionFieldKind) (recordcollaboration.RevisionActivityKind, bool) {
	switch field {
	case recordcollaboration.RevisionFieldOwner:
		return recordcollaboration.RevisionActivityRecordOwnerChanged, true
	case recordcollaboration.RevisionFieldParticipants:
		return recordcollaboration.RevisionActivityRecordParticipantChanged, true
	case recordcollaboration.RevisionFieldFollowUp:
		return recordcollaboration.RevisionActivityRecordFollowUpChanged, true
	default:
		return "", false
	}
}

func collaborationRevisionOutboxKind(field recordcollaboration.RevisionFieldKind) (string, bool) {
	switch field {
	case recordcollaboration.RevisionFieldOwner:
		return recordplatform.OutboxEventKindRecordOwnerChanged, true
	case recordcollaboration.RevisionFieldParticipants:
		return recordplatform.OutboxEventKindRecordParticipantChanged, true
	default:
		return "", false
	}
}

func collaborationRevisionActivityID(revisionID string, kind recordcollaboration.RevisionActivityKind) string {
	digest := sha256.Sum256([]byte("houfeng.record-collaboration.revision-activity.v1\x00" + revisionID + "\x00" + string(kind)))
	return "rac_" + hex.EncodeToString(digest[:])
}

func validCollaborationRevisionIdentity(value, prefix string) bool {
	if len(value) <= len(prefix) || len(value) > len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func nilCollaborationDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ records.RevisionParticipant = (*collaborationRevisionParticipant)(nil)
