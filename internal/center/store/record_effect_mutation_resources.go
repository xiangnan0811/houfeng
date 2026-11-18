package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// Collaboration writes claim their idempotency key between the two acquisition
// phases. A missing idempotency row is not treated as an acquired claim.
func claimCollaborationEffect(ctx context.Context, transaction *RecordPlatformTransaction, recordID, actorID string, userIDs []string, actionID string, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	discover := func() (records.RevisionMutationResources, error) {
		users := append([]string(nil), userIDs...)
		var actions []records.RevisionMutationAction
		if actionID != "" {
			var assignee *string
			err := transaction.tx.QueryRow(ctx, `select assignee_id from public.record_actions where record_id=$1 and action_id=$2`, recordID, actionID).Scan(&assignee)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return records.RevisionMutationResources{}, err
			}
			if assignee != nil {
				users = append(users, *assignee)
			}
			actions, err = readRevisionMutationRows[records.RevisionMutationAction](ctx, transaction.tx, `
				select project_id,record_id,action_id,status,record_fence_epoch,coalesce(due_at,'0001-01-01 00:00:00+00'::timestamptz)
				from public.record_actions where record_id=$1 and action_id=$2`, recordID, actionID)
			if err != nil {
				return records.RevisionMutationResources{}, err
			}
		}
		resources, err := discoverEffectRecordResources(ctx, transaction.tx, []string{recordID}, users)
		if err != nil {
			var missing *recordMutationPrincipalMissing
			if errors.As(err, &missing) {
				if missing.userID == actorID {
					return resources, recordauth.ErrDenied
				}
				return resources, recordcollaboration.ErrMembershipDenied
			}
			return resources, err
		}
		followers, err := readRevisionMutationRows[records.RevisionMutationFollower](ctx, transaction.tx, `
			select project_id,record_id,user_id,record_fence_epoch,manual_preference,
			       follows_author,follows_owner,follows_participant,follows_comment,follows_mention,follows_action,
			       coalesce(encode(preference_result_fingerprint,'hex'),'')
			from public.record_followers where record_id=$1 order by user_id`, recordID)
		if err != nil {
			return resources, err
		}
		resources.Followers = followers
		resources.Actions = actions
		resources.Idempotency = []records.RevisionMutationIdempotency{{Key: input.Key}}
		return records.CombineRevisionMutationResources(resources)
	}
	before, err := discover()
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	declared, err := normalizeRecordMutationDeclarations(before)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if err := lockComparisonDecisionForWriter(ctx, transaction.tx); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if err := acquireRecordMutationResourcesBeforeClaims(ctx, transaction.tx, declared); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	transaction.registerAcquiredIdempotencyScopes(declared)
	claim, err := transaction.ClaimIdempotency(ctx, input)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if err := acquireRecordMutationResourcesAfterClaims(ctx, transaction.tx, declared); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	after, err := discover()
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return recordplatform.IdempotencyClaimResultV1{}, &pgconn.PgError{Code: "40001", Message: "collaboration route changed; restart transaction"}
	}
	transaction.registerAcquiredRecordMutationResources(declared)
	return claim, nil
}

type recordMutationPrincipalMissing struct {
	userID string
}

func (*recordMutationPrincipalMissing) Error() string {
	return "record mutation principal unavailable"
}

// discoverEffectRecordResources observes the actual record and membership routes.
// These observations are only candidates until acquisition and the second read.
func discoverEffectRecordResources(ctx context.Context, tx pgx.Tx, recordIDs, userIDs []string) (records.RevisionMutationResources, error) {
	var resources records.RevisionMutationResources
	recordIDs = append([]string(nil), recordIDs...)
	userIDs = append([]string(nil), userIDs...)
	sort.Strings(recordIDs)
	recordIDs = slices.Compact(recordIDs)
	sort.Strings(userIDs)
	userIDs = slices.Compact(userIDs)
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		var user records.RevisionMutationUser
		if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, userID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return resources, &recordMutationPrincipalMissing{userID: userID}
			}
			return resources, fmt.Errorf("discover effect user: %w", err)
		}
		resources.Users = append(resources.Users, user)
		groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, recordauth.ProjectIDDefault, userID)
		if err != nil {
			return resources, err
		}
		for _, groupID := range groups {
			resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: string(recordplatform.ProjectIDDefault), GroupID: groupID})
			resources.Memberships = append(resources.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: userID})
		}
	}
	for _, recordID := range recordIDs {
		if recordID == "" {
			continue
		}
		projectID := string(recordplatform.ProjectIDDefault)
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: projectID, RecordID: recordID})
		reservations, err := readRevisionMutationRows[records.RevisionMutationReservation](ctx, tx, `
			select project_id,object_id,reservation_id,state,fence_epoch,owner_id,owner_generation,
			       coalesce(owner_expires_at,'0001-01-01 00:00:00+00'::timestamptz) from public.deletion_reservations
			where project_id=$1 and object_kind='record' and object_id=$2
			  and state in ('previewed','fenced','committed') order by reservation_id`, projectID, recordID)
		if err != nil {
			return resources, err
		}
		resources.Reservations = append(resources.Reservations, reservations...)
		root := records.RevisionMutationRootObservation{ProjectID: projectID, RecordID: recordID}
		var visibility, visibilityDigest []byte
		err = tx.QueryRow(ctx, `select coalesce(current_revision_id,''),lifecycle,lock_version,authorization_epoch,current_visibility_scope,current_visibility_digest
			from public.records where project_id=$1 and record_id=$2`, projectID, recordID).Scan(
			&root.CurrentRevisionID, &root.Lifecycle, &root.LockVersion, &root.AuthorizationEpoch, &visibility, &visibilityDigest)
		if errors.Is(err, pgx.ErrNoRows) {
			resources.RootObservations = append(resources.RootObservations, root)
			continue
		}
		if err != nil {
			return resources, err
		}
		root.Exists = true
		resources.RootObservations = append(resources.RootObservations, root)
		if len(visibility) != 0 {
			scope, err := decodeStoredRecordVisibility(visibility, visibilityDigest)
			if err != nil {
				return resources, err
			}
			appendRevisionMutationVisibility(&resources, scope)
		}
		if root.CurrentRevisionID == "" {
			continue
		}
		resources.Revisions = append(resources.Revisions, records.RevisionMutationRevision{RecordID: recordID, RevisionID: root.CurrentRevisionID})
		rows, err := tx.Query(ctx, `select subject_kind,source_id,capture_authorization,capture_authorization_digest
			from public.record_revision_subjects where revision_id=$1 order by ordinal`, root.CurrentRevisionID)
		if err != nil {
			return resources, err
		}
		for rows.Next() {
			var kind records.SubjectKind
			var sourceID string
			var raw, digest []byte
			if err := rows.Scan(&kind, &sourceID, &raw, &digest); err != nil {
				rows.Close()
				return resources, err
			}
			var authorization recordauth.SourceAuthorization
			if err := decodeStoredRecordJSON(raw, &authorization); err != nil {
				rows.Close()
				return resources, err
			}
			authorization, err = normalizeCanonicalRecordSubjectAuthorization(authorization)
			if err != nil || !sameStoredRecordDigest(authorization.Digest, digest) || authorization.Kind != recordauth.SourceKind(kind) || authorization.SourceID != sourceID {
				rows.Close()
				return resources, records.ErrInvalidRevisionMutationResources
			}
			appendRevisionMutationSource(&resources, authorization)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return resources, err
		}
	}
	return records.CombineRevisionMutationResources(resources)
}

// The caller must enter an admitted source/asset-graph transaction first and
// must recheck its own routes and effect-specific observations afterwards.
func acquireEffectMutationResources(ctx context.Context, tx pgx.Tx, recordIDs, userIDs []string, extra records.RevisionMutationResources) (recordMutationResources, error) {
	before, err := discoverEffectRecordResources(ctx, tx, recordIDs, userIDs)
	if err != nil {
		return recordMutationResources{}, err
	}
	declared, err := normalizeRecordMutationDeclarations(before, extra)
	if err != nil {
		return recordMutationResources{}, err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return recordMutationResources{}, err
	}
	if err := acquireRecordMutationResources(ctx, tx, declared); err != nil {
		return recordMutationResources{}, err
	}
	after, err := discoverEffectRecordResources(ctx, tx, recordIDs, userIDs)
	if err != nil {
		return recordMutationResources{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "effect record or authorization route changed; restart transaction"}
	}
	return declared, nil
}
