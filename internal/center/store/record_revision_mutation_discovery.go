package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// Future revisions are derived only from earlier commands in the same batch.
// They supply prior collaboration facts without pretending a row exists.
type revisionMutationFutureKey struct{}

type revisionMutationFutureRevision struct {
	recordID string
	input    records.CompleteRevisionInput
}

func validateRevisionMutationDiscovery(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) error {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) {
		return records.ErrInvalidRevisionCommand
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return command.Validate()
}

// This helper accepts only internal fixed SELECTs; callers cannot supply SQL
// through a declaration. RowToStructByPos checks the complete concrete DTO shape.
func readRevisionMutationRows[T any](ctx context.Context, tx pgx.Tx, query string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("discover revision resources: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[T])
	if err != nil {
		return nil, fmt.Errorf("decode revision resources: %w", err)
	}
	return items, nil
}

func discoverRevisionMutationMembers(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	return discoverRevisionMutationMembersForReplay(ctx, tx, command, false)
}

func discoverRevisionMutationMembersForReplay(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand, replay bool) (records.RevisionMutationResources, error) {
	ids := []string{command.Actor.UserID}
	if !replay {
		ids = append(ids, command.Input.AuthorID())
		if command.Input.OwnerID() != "" {
			ids = append(ids, command.Input.OwnerID())
		}
		for _, participant := range command.Input.Participants() {
			ids = append(ids, participant.ParticipantID)
		}
	}
	return discoverRecordMutationUsers(ctx, tx, command.Actor.UserID, ids)
}

func discoverRecordMutationUsers(ctx context.Context, tx pgx.Tx, actorID string, ids []string) (records.RevisionMutationResources, error) {
	sort.Strings(ids)
	ids = slices.Compact(ids)
	var resources records.RevisionMutationResources
	for _, userID := range ids {
		var user records.RevisionMutationUser
		if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, userID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if userID == actorID {
					return resources, recordauth.ErrDenied
				}
				return resources, recordcollaboration.ErrMembershipDenied
			}
			return resources, fmt.Errorf("discover revision user: %w", err)
		}
		resources.Users = append(resources.Users, user)
		// Persisted memberships, never command.Actor.GroupIDs or historical
		// revision participants, determine the current membership footprint.
		groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, recordauth.ProjectIDDefault, userID)
		if err != nil {
			return resources, err
		}
		for _, groupID := range groups {
			resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: "default", GroupID: groupID})
			resources.Memberships = append(resources.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: userID})
		}
	}
	return records.CombineRevisionMutationResources(resources)
}

func appendRevisionMutationVisibility(resources *records.RevisionMutationResources, scope recordauth.VisibilityScope) {
	for _, groupID := range scope.AllowedGroupIDs {
		resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: string(scope.ProjectID), GroupID: groupID})
	}
}
func appendRevisionMutationSource(resources *records.RevisionMutationResources, authorization recordauth.SourceAuthorization) {
	resources.Sources = append(resources.Sources, records.RevisionMutationSource{ProjectID: string(authorization.CaptureScope.ProjectID), SourceKind: records.SubjectKind(authorization.Kind), SourceID: authorization.SourceID})
	appendRevisionMutationVisibility(resources, authorization.CaptureScope)
	for _, scope := range []*recordauth.VisibilityScope{authorization.CurrentScope, authorization.FinalFloor, authorization.LastLiveScope} {
		if scope != nil {
			appendRevisionMutationVisibility(resources, *scope)
		}
	}
}

func discoverRevisionMutationBase(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand, participants records.RevisionMutationResources) (records.RevisionMutationResources, error) {
	return discoverRevisionMutationBaseForReplay(ctx, tx, command, participants, false)
}

func discoverRevisionMutationBaseForReplay(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand, participants records.RevisionMutationResources, replay bool) (records.RevisionMutationResources, error) {
	resources, err := discoverRevisionMutationMembersForReplay(ctx, tx, command, replay)
	if err != nil {
		return records.RevisionMutationResources{}, err
	}
	resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: command.RecordID})
	resources.Idempotency = append(resources.Idempotency, records.RevisionMutationIdempotency{Key: command.Idempotency.Key})
	resources.Records = append(resources.Records, participants.Records...)
	for _, snapshot := range participants.Snapshots {
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: snapshot.RecordID})
	}
	for _, attachment := range participants.Attachments {
		if attachment.RecordID != "" {
			resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: attachment.ProjectID, RecordID: attachment.RecordID})
		}
	}
	for _, draft := range participants.Drafts {
		if draft.RecordID != "" {
			resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: draft.ProjectID, RecordID: draft.RecordID})
		}
	}
	for _, dependency := range participants.Dependencies {
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: dependency.SourceRecordID})
	}
	if !replay {
		if command.Idempotency.Key.OperationKind == recordplatform.OperationKindRecordCreate {
			if command.BirthPermit.AllocationID == "" {
				return records.RevisionMutationResources{}, records.ErrInvalidRevisionCommand
			}
			resources.Births = append(resources.Births, records.RevisionMutationBirth{
				AllocationID: command.BirthPermit.AllocationID,
				Request:      command.BirthRequest,
				Creation:     command.BirthPermit.Creation,
			})
		}
		appendRevisionMutationVisibility(&resources, command.Input.VisibilityScope())
		for _, subject := range command.Input.Subjects() {
			appendRevisionMutationSource(&resources, subject.CaptureAuthorization)
		}
		revisionID, err := command.RevisionID()
		if err != nil {
			return records.RevisionMutationResources{}, err
		}
		activityID, err := command.ActivityID()
		if err != nil {
			return records.RevisionMutationResources{}, err
		}
		resources.NewRevisionEffects = append(resources.NewRevisionEffects, records.RevisionMutationNewRevisionEffect{ProjectID: "default", RecordID: command.RecordID, RevisionID: revisionID, Kind: records.RevisionMutationNewRevisionRows, ActivityID: activityID, EventKind: records.RevisionMutationEventKind(command.ActivityKind)})
	}
	return discoverRecordMutationRoutes(ctx, tx, resources)
}

// Discover current root/source routes without treating a missing row as an
// authorization or absence proof. Lifecycle and revision use the same model.
func discoverRecordMutationRoutes(ctx context.Context, tx pgx.Tx, resources records.RevisionMutationResources) (records.RevisionMutationResources, error) {
	normalized, err := records.CombineRevisionMutationResources(resources)
	if err != nil {
		return records.RevisionMutationResources{}, err
	}
	for _, record := range normalized.Records {
		normalized.RecordGateScopes = append(normalized.RecordGateScopes, records.RevisionMutationRecordGateScope{ProjectID: record.ProjectID, RecordID: record.RecordID})
		reservations, err := readRevisionMutationRows[records.RevisionMutationReservation](ctx, tx, `
			select project_id,object_id,reservation_id,state,fence_epoch,owner_id,owner_generation,coalesce(owner_expires_at,'0001-01-01 00:00:00+00'::timestamptz) from public.deletion_reservations
			where project_id=$1 and object_kind='record' and object_id=$2
			  and state in ('previewed','fenced','committed') order by reservation_id collate "C"`, record.ProjectID, record.RecordID)
		if err != nil {
			return records.RevisionMutationResources{}, err
		}
		normalized.Reservations = append(normalized.Reservations, reservations...)
		// Discover the current source routes as well as desired subjects. A
		// missing root remains a declared route, not proof of safe absence.
		root := records.RevisionMutationRootObservation{ProjectID: record.ProjectID, RecordID: record.RecordID}
		var visibility, visibilityDigest []byte
		err = tx.QueryRow(ctx, `select coalesce(current_revision_id,''),current_visibility_scope,current_visibility_digest,lifecycle,lock_version,authorization_epoch from public.records where project_id=$1 and record_id=$2`, record.ProjectID, record.RecordID).Scan(&root.CurrentRevisionID, &visibility, &visibilityDigest, &root.Lifecycle, &root.LockVersion, &root.AuthorizationEpoch)
		if errors.Is(err, pgx.ErrNoRows) {
			normalized.RootObservations = append(normalized.RootObservations, root)
			continue
		}
		if err != nil {
			return records.RevisionMutationResources{}, err
		}
		root.Exists = true
		if speculative, ok := ctx.Value(revisionMutationSpeculativeRootsKey{}).(map[string]bool); ok && speculative[record.RecordID] && root.CurrentRevisionID == "" && root.LockVersion == 0 && root.AuthorizationEpoch == 0 {
			root = records.RevisionMutationRootObservation{ProjectID: record.ProjectID, RecordID: record.RecordID}
		}
		normalized.RootObservations = append(normalized.RootObservations, root)
		revisionID := root.CurrentRevisionID
		if len(visibility) != 0 {
			scope, err := decodeStoredRecordVisibility(visibility, visibilityDigest)
			if err != nil {
				return records.RevisionMutationResources{}, err
			}
			appendRevisionMutationVisibility(&normalized, scope)
		}
		if revisionID == "" {
			continue
		}
		normalized.Revisions = append(normalized.Revisions, records.RevisionMutationRevision{RecordID: record.RecordID, RevisionID: revisionID})
		rows, err := tx.Query(ctx, `select subject_kind,source_id,capture_authorization,capture_authorization_digest from public.record_revision_subjects where revision_id=$1 order by ordinal`, revisionID)
		if err != nil {
			return records.RevisionMutationResources{}, err
		}
		for rows.Next() {
			var kind records.SubjectKind
			var sourceID string
			var raw, digest []byte
			if err := rows.Scan(&kind, &sourceID, &raw, &digest); err != nil {
				rows.Close()
				return records.RevisionMutationResources{}, err
			}
			var authorization recordauth.SourceAuthorization
			if err := decodeStoredRecordJSON(raw, &authorization); err != nil {
				rows.Close()
				return records.RevisionMutationResources{}, err
			}
			authorization, err = normalizeCanonicalRecordSubjectAuthorization(authorization)
			if err != nil || !sameStoredRecordDigest(authorization.Digest, digest) || authorization.Kind != recordauth.SourceKind(kind) || authorization.SourceID != sourceID {
				rows.Close()
				return records.RevisionMutationResources{}, records.ErrInvalidRevisionMutationResources
			}
			appendRevisionMutationSource(&normalized, authorization)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return records.RevisionMutationResources{}, err
		}
	}
	return records.CombineRevisionMutationResources(normalized)
}

func (participant *collaborationRevisionParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return records.RevisionMutationResources{}, err
	}
	if participant == nil || nilCollaborationDependency(participant.members) {
		return records.RevisionMutationResources{}, records.ErrInvalidRevisionParticipant
	}
	resources, err := discoverRevisionMutationMembers(ctx, tx, command)
	if err != nil {
		return resources, err
	}
	resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: command.RecordID})
	for _, subject := range command.Input.Subjects() {
		appendRevisionMutationSource(&resources, subject.CaptureAuthorization)
	}
	var previousFacts recordcollaboration.RevisionFilterFacts
	future, _ := ctx.Value(revisionMutationFutureKey{}).(map[string]revisionMutationFutureRevision)
	predecessor, inBatch := future[command.BaseRevisionID]
	if command.BaseRevisionID != "" && inBatch {
		if predecessor.recordID != command.RecordID {
			return resources, records.ErrInvalidRevisionMutationResources
		}
		previousFacts, err = collaborationRevisionFilterFacts(predecessor.input)
	} else {
		if command.BaseRevisionID != "" {
			var recordID string
			if err := tx.QueryRow(ctx, `select record_id from public.record_revisions where revision_id=$1`, command.BaseRevisionID).Scan(&recordID); err != nil {
				return resources, err
			}
			if recordID != command.RecordID {
				return resources, records.ErrInvalidRevisionMutationResources
			}
			resources.Revisions = append(resources.Revisions, records.RevisionMutationRevision{RecordID: recordID, RevisionID: command.BaseRevisionID})
		}
		previousFacts, err = loadPreviousCollaborationRevisionFacts(ctx, tx, command.BaseRevisionID)
		if err == nil {
			for _, userID := range previousFacts.ParticipantIDs() {
				resources.RevisionParticipants = append(resources.RevisionParticipants, records.RevisionMutationRevisionParticipant{RevisionID: command.BaseRevisionID, UserID: userID})
			}
		}
	}
	if err != nil {
		return resources, err
	}
	currentFacts, err := collaborationRevisionFilterFacts(command.Input)
	if err != nil {
		return resources, err
	}
	changedFields := recordcollaboration.DiffRevisionFilterFacts(previousFacts, currentFacts)
	var revisionID string
	if len(changedFields) > 0 {
		revisionID, err = command.RevisionID()
		if err != nil {
			return resources, err
		}
	}
	followers, err := readRevisionMutationRows[records.RevisionMutationFollower](ctx, tx, `
		select project_id,record_id,user_id,record_fence_epoch,manual_preference,
		       follows_author,follows_owner,follows_participant,follows_comment,follows_mention,follows_action,
		       coalesce(encode(preference_result_fingerprint,'hex'),'')
		from public.record_followers where record_id=$1 order by user_id`, command.RecordID)
	if err != nil {
		return resources, err
	}
	resources.Followers = followers
	plan, err := newCollaborationRevisionFollowerPlan(command.Input)
	if err != nil {
		return resources, err
	}
	for _, userID := range plan.userIDs {
		if !slices.ContainsFunc(followers, func(f records.RevisionMutationFollower) bool { return f.UserID == userID }) {
			resources.Followers = append(resources.Followers, records.RevisionMutationFollower{ProjectID: "default", RecordID: command.RecordID, UserID: userID})
		}
	}
	for _, field := range changedFields {
		activityKind, ok := collaborationRevisionActivityKind(field)
		if !ok {
			return resources, records.ErrInvalidRevisionMutationResources
		}
		resources.NewRevisionEffects = append(resources.NewRevisionEffects, records.RevisionMutationNewRevisionEffect{
			ProjectID: "default", RecordID: command.RecordID, RevisionID: revisionID, Kind: records.RevisionMutationNewCollaborationActivity,
			ActivityID: collaborationRevisionActivityID(revisionID, activityKind), EventKind: records.RevisionMutationEventKind(activityKind),
		})
		if eventKind, ok := collaborationRevisionOutboxKind(field); ok {
			resources.NewRevisionEffects = append(resources.NewRevisionEffects, records.RevisionMutationNewRevisionEffect{ProjectID: "default", RecordID: command.RecordID, RevisionID: revisionID, Kind: records.RevisionMutationNewCollaborationOutbox, EventKind: records.RevisionMutationEventKind(eventKind)})
		}
	}
	return records.CombineRevisionMutationResources(resources)
}

func (recordSearchRevisionParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return records.RevisionMutationResources{}, err
	}
	resources := records.RevisionMutationResources{
		Records:  []records.RevisionMutationRecord{{ProjectID: "default", RecordID: command.RecordID}},
		Projects: []records.RevisionMutationProject{{ProjectID: "default", Kind: records.RevisionMutationSearchCoordination}},
	}
	generations, err := readRevisionMutationRows[records.RevisionMutationGeneration](ctx, tx, `select project_id,generation,generation_state from public.record_search_generations where project_id=$1 and generation_state in ('published','building') order by generation`, "default")
	if err != nil {
		return resources, err
	}
	resources.Generations = generations
	ids := make([]int64, 0, len(generations))
	for _, generation := range generations {
		ids = append(ids, generation.Generation)
		resources.Documents = append(resources.Documents, records.RevisionMutationDocument{ProjectID: "default", RecordID: command.RecordID, Generation: generation.Generation})
		for _, subject := range command.Input.Subjects() {
			resources.SearchSubjects = append(resources.SearchSubjects, records.RevisionMutationSearchSubject{ProjectID: "default", RecordID: command.RecordID, Generation: generation.Generation, SubjectKind: subject.Kind, SourceID: subject.SourceID, RelationRole: subject.Role})
		}
	}
	oldSubjects, err := readRevisionMutationRows[records.RevisionMutationSearchSubject](ctx, tx, `select $3::text,record_id,generation,subject_kind,source_id,relation_role from public.record_search_subjects where record_id=$1 and generation=any($2::bigint[]) order by generation,subject_kind,source_id,relation_role`, command.RecordID, ids, "default")
	if err != nil {
		return resources, err
	}
	resources.SearchSubjects = append(resources.SearchSubjects, oldSubjects...)
	actions, err := readRevisionMutationRows[records.RevisionMutationAction](ctx, tx, `select project_id,record_id,action_id,status,record_fence_epoch,coalesce(due_at,'0001-01-01 00:00:00+00'::timestamptz) from public.record_actions where record_id=$1 order by action_id`, command.RecordID)
	if err != nil {
		return resources, err
	}
	resources.Actions = actions
	return records.CombineRevisionMutationResources(resources)
}

func appendRevisionMutationSnapshot(resources *records.RevisionMutationResources, recordID, snapshotID string, snapshot evidence.CanonicalSnapshot) {
	envelope := snapshot.Envelope()
	resources.Snapshots = append(resources.Snapshots, records.RevisionMutationSnapshot{SnapshotID: snapshotID, RecordID: recordID, PayloadDigest: snapshot.Hash(), SourceKind: records.SubjectKind(envelope.Source.Type), SourceID: envelope.Source.ID, AuthorizationDigest: envelope.Authorization.Digest})
	resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: snapshot.Hash()})
	resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: recordID})
	appendRevisionMutationSource(resources, envelope.Authorization)
}

func readRevisionMutationSnapshot(ctx context.Context, tx pgx.Tx, snapshotID string) (records.RevisionMutationSnapshot, error) {
	var snapshot records.RevisionMutationSnapshot
	var payload, authorization []byte
	err := tx.QueryRow(ctx, `select snapshot_id,record_id,payload_digest,source_kind,source_id,capture_authorization_digest from public.evidence_snapshots where snapshot_id=$1`, snapshotID).Scan(&snapshot.SnapshotID, &snapshot.RecordID, &payload, &snapshot.SourceKind, &snapshot.SourceID, &authorization)
	if err != nil {
		return snapshot, err
	}
	if len(payload) != 32 || len(authorization) != 32 {
		return snapshot, records.ErrInvalidRevisionMutationResources
	}
	copy(snapshot.PayloadDigest[:], payload)
	copy(snapshot.AuthorizationDigest[:], authorization)
	return snapshot, nil
}

func appendRevisionMutationDependencies(resources *records.RevisionMutationResources, snapshotID string, digest [32]byte, dependencies []evidence.ComparisonDependency) {
	resources.Completions = append(resources.Completions, records.RevisionMutationCompletion{ResultSnapshotID: snapshotID, PayloadDigest: digest})
	for _, dependency := range dependencies {
		resources.Dependencies = append(resources.Dependencies, records.RevisionMutationDependency{ResultSnapshotID: snapshotID, SourceRecordID: dependency.SourceRecordID, SourceRevisionID: dependency.SourceRevisionID, SourceSnapshotID: dependency.SourceSnapshotID})
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: dependency.SourceRecordID})
	}
}

func discoverInheritedRevisionMutationDependencies(ctx context.Context, tx pgx.Tx, resources *records.RevisionMutationResources, snapshotID string, snapshot evidence.CanonicalSnapshot, trustedSourceID string) error {
	if snapshot.Envelope().Key != evidence.ComparisonResultV1Key() {
		return nil
	}
	items, err := evidence.ExtractComparisonDependencyItems(snapshot)
	if err != nil {
		return nil
	} // Same unsupported/incomplete branch as effects.
	digest := snapshot.Hash()
	// Declare the exact completion being read even when absent. This is not a
	// certification claim. Only the real reader supplies certified source IDs.
	resources.Completions = append(resources.Completions, records.RevisionMutationCompletion{ResultSnapshotID: snapshotID, PayloadDigest: digest})
	dependencies, complete, err := readCertifiedComparisonDependencies(ctx, tx, snapshotID, digest, items)
	if err != nil {
		return err
	}
	if complete {
		appendRevisionMutationDependencies(resources, snapshotID, digest, dependencies)
		return nil
	}
	if trustedSourceID == "" || trustedSourceID == snapshotID {
		return nil
	}
	resources.Completions = append(resources.Completions, records.RevisionMutationCompletion{ResultSnapshotID: trustedSourceID, PayloadDigest: digest})
	dependencies, complete, err = readCertifiedComparisonDependencies(ctx, tx, trustedSourceID, digest, items)
	if err != nil {
		return err
	}
	if complete {
		appendRevisionMutationDependencies(resources, trustedSourceID, digest, dependencies)
		appendRevisionMutationDependencies(resources, snapshotID, digest, dependencies)
	}
	return nil
}

func (recordEvidenceRevisionParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return records.RevisionMutationResources{}, err
	}
	var resources records.RevisionMutationResources
	preparation := command.EvidencePreparation
	save := preparation.ComparisonSave()
	if len(evidenceCapacityCharges(preparation.Captures(), save, preparation.Imported())) > 0 {
		resources.Projects = append(resources.Projects, records.RevisionMutationProject{ProjectID: "default", Kind: records.RevisionMutationEvidenceCapacity})
	}
	for _, capture := range preparation.Captures() {
		intent := capture.Intent()
		var row records.RevisionMutationCaptureIntent
		var kind string
		var version int64
		var previewDigest, sourceDigest []byte
		err := tx.QueryRow(ctx, `select intent_id,record_id,coalesce(snapshot_id,''),kind,schema_version,preview_digest,source_digest,selection::text,preview::text,estimated_size_bytes,created_at,valid_until from public.evidence_capture_intents where intent_id=$1`, intent.ID).Scan(&row.IntentID, &row.RecordID, &row.SnapshotID, &kind, &version, &previewDigest, &sourceDigest, &row.SelectionJSON, &row.PreviewJSON, &row.EstimatedSizeBytes, &row.CreatedAt, &row.ValidUntil)
		if errors.Is(err, pgx.ErrNoRows) {
			return resources, ErrEvidencePersistenceConflict
		}
		if err != nil {
			return resources, err
		}
		if len(previewDigest) != 32 || len(sourceDigest) != 32 || version < 1 || version > 65535 {
			return resources, ErrEvidencePersistenceConflict
		}
		row.Kind = evidence.KindKey{Kind: evidence.KindName(kind), SchemaVersion: evidence.SchemaVersion(version)}
		copy(row.PreviewDigest[:], previewDigest)
		copy(row.SourceDigest[:], sourceDigest)
		if row.RecordID != capture.RecordID() || row.PreviewDigest != intent.PreviewDigest || row.SourceDigest != capture.Preview().SourceDigest || row.Kind != intent.Key || (row.SnapshotID != "" && row.SnapshotID != capture.SnapshotID()) {
			return resources, ErrEvidencePersistenceConflict
		}
		resources.CaptureIntents = append(resources.CaptureIntents, row)
		appendRevisionMutationSnapshot(&resources, capture.RecordID(), capture.SnapshotID(), capture.Snapshot())
	}
	for _, copy := range save.Copies {
		appendRevisionMutationSnapshot(&resources, copy.RecordID(), copy.SnapshotID(), copy.Snapshot())
		parent, err := readRevisionMutationSnapshot(ctx, tx, copy.CopiedFromSnapshotID())
		if err != nil {
			return resources, err
		}
		resources.Snapshots = append(resources.Snapshots, parent)
		resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: parent.PayloadDigest})
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: parent.RecordID})
		resources.Sources = append(resources.Sources, records.RevisionMutationSource{ProjectID: "default", SourceKind: parent.SourceKind, SourceID: parent.SourceID})
		resources.Lineages = append(resources.Lineages, records.RevisionMutationLineage{SnapshotID: copy.SnapshotID(), CopiedFromSnapshotID: copy.CopiedFromSnapshotID()})
		if err := discoverInheritedRevisionMutationDependencies(ctx, tx, &resources, copy.SnapshotID(), copy.Snapshot(), copy.CopiedFromSnapshotID()); err != nil {
			return resources, err
		}
	}
	if !save.Result.Empty() {
		appendRevisionMutationSnapshot(&resources, save.Result.RecordID(), save.Result.SnapshotID(), save.Result.Snapshot())
		dependencies, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, save.Claims)
		if err != nil {
			return resources, err
		}
		appendRevisionMutationDependencies(&resources, save.Result.SnapshotID(), save.Result.Snapshot().Hash(), dependencies)
	}
	for _, item := range preparation.Imported() {
		appendRevisionMutationSnapshot(&resources, item.RecordID(), item.SnapshotID(), item.Snapshot())
		if err := discoverInheritedRevisionMutationDependencies(ctx, tx, &resources, item.SnapshotID(), item.Snapshot(), ""); err != nil {
			return resources, err
		}
	}
	for _, reference := range preparation.References() {
		if err := validatePersistedEvidenceReference(ctx, tx, reference); err != nil {
			return resources, err
		}
		snapshot, err := readRevisionMutationSnapshot(ctx, tx, reference.SnapshotID())
		if err != nil {
			return resources, err
		}
		resources.Snapshots = append(resources.Snapshots, snapshot)
		resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: snapshot.PayloadDigest})
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: snapshot.RecordID})
		appendRevisionMutationSource(&resources, reference.Authorization())
	}
	snapshotIDs := preparation.SnapshotIDs()
	if len(snapshotIDs) > 0 {
		revisionID, err := command.RevisionID()
		if err != nil {
			return resources, err
		}
		for ordinal, snapshotID := range snapshotIDs {
			resources.NewRevisionEffects = append(resources.NewRevisionEffects, records.RevisionMutationNewRevisionEffect{ProjectID: "default", RecordID: command.RecordID, RevisionID: revisionID, Kind: records.RevisionMutationNewEvidenceReference, Ordinal: int64(ordinal), SnapshotID: snapshotID})
		}
	}
	return records.CombineRevisionMutationResources(resources)
}

func (participant comparisonRevisionParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return records.RevisionMutationResources{}, err
	}
	var resources records.RevisionMutationResources
	save := command.EvidencePreparation.ComparisonSave()
	if save.Empty() {
		return resources, nil
	}
	if participant.signer == nil {
		return resources, evidence.ErrComparisonIntentInvalid
	}
	claims, err := participant.signer.Verify(save.Token, time.Now().UTC())
	if err != nil {
		return resources, err
	}
	verified, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, claims)
	if err != nil {
		return resources, err
	}
	prepared, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, save.Claims)
	if err != nil || !slices.Equal(verified, prepared) {
		return resources, evidence.ErrComparisonIntentStale
	}
	appendRevisionMutationDependencies(&resources, save.Result.SnapshotID(), save.Result.Snapshot().Hash(), verified)
	resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: save.Result.RecordID()})
	for _, copy := range save.Copies {
		parent, err := readRevisionMutationSnapshot(ctx, tx, copy.CopiedFromSnapshotID())
		if err != nil {
			return resources, err
		}
		resources.Snapshots = append(resources.Snapshots, parent)
		resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: parent.PayloadDigest})
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: parent.RecordID})
		resources.Sources = append(resources.Sources, records.RevisionMutationSource{ProjectID: "default", SourceKind: parent.SourceKind, SourceID: parent.SourceID})
	}
	return records.CombineRevisionMutationResources(resources)
}
