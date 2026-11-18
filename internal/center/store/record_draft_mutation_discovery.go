package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func discoverDraftCreationResources(ctx context.Context, tx pgx.Tx, projectID, authorID, recordID string) (recordMutationResources, error) {
	var set records.RevisionMutationResources
	var user records.RevisionMutationUser
	if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, authorID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
		return recordMutationResources{}, err
	}
	set.Users = append(set.Users, user)
	groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, recordauth.ProjectID(projectID), authorID)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, groupID := range groups {
		set.Groups = append(set.Groups, records.RevisionMutationGroup{ProjectID: projectID, GroupID: groupID})
		set.Memberships = append(set.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: authorID})
	}
	if recordID != "" {
		record := records.RevisionMutationRecord{ProjectID: projectID, RecordID: recordID}
		set.Records = append(set.Records, record)
		if err := appendEvidenceCurrentRecordResources(ctx, tx, &set, record); err != nil {
			return recordMutationResources{}, err
		}
		if len(set.RootObservations) != 1 || !set.RootObservations[0].Exists {
			return recordMutationResources{}, records.ErrRecordNotFound
		}
	}
	return normalizeRecordMutationDeclarations(set)
}

// discoverDraftMutationResources observes the draft and every durable owner it
// can release. Discovery alone neither locks these rows nor authorizes them.
func discoverDraftMutationResources(ctx context.Context, tx pgx.Tx, draftIDs []string, includeActors bool) (recordMutationResources, error) {
	var sets []records.RevisionMutationResources
	for _, draftID := range draftIDs {
		set, err := discoverRevisionDraftCleanup(ctx, tx, records.RevisionCommitCommand{DraftID: draftID})
		if err != nil {
			return recordMutationResources{}, err
		}
		if len(set.Drafts) != 1 {
			return recordMutationResources{}, records.ErrDraftNotFound
		}
		draft := set.Drafts[0]
		if includeActors {
			var user records.RevisionMutationUser
			if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, draft.AuthorID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
				return recordMutationResources{}, err
			}
			set.Users = append(set.Users, user)
			groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, recordauth.ProjectID(draft.ProjectID), draft.AuthorID)
			if err != nil {
				return recordMutationResources{}, err
			}
			for _, groupID := range groups {
				set.Groups = append(set.Groups, records.RevisionMutationGroup{ProjectID: draft.ProjectID, GroupID: groupID})
				set.Memberships = append(set.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: draft.AuthorID})
			}
		}
		if draft.RecordID != "" {
			if err := appendEvidenceCurrentRecordResources(ctx, tx, &set, records.RevisionMutationRecord{ProjectID: draft.ProjectID, RecordID: draft.RecordID}); err != nil {
				return recordMutationResources{}, err
			}
		}
		uploadIDs := make([]string, 0, len(set.Uploads))
		for _, upload := range set.Uploads {
			uploadIDs = append(uploadIDs, upload.UploadID)
		}
		jobIDs := make([]string, 0, len(set.Jobs))
		for _, job := range set.Jobs {
			jobIDs = append(jobIDs, job.ProcessorJobID)
		}
		rows, err := tx.Query(ctx, `select obligation_id from public.record_write_obligations
			where (origin_kind='upload' and origin_id=any($1::text[]))
			   or (origin_kind='processor_preview' and origin_id=any($2::text[]))
			order by obligation_id collate "C"`, uploadIDs, jobIDs)
		if err != nil {
			return recordMutationResources{}, err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return recordMutationResources{}, err
		}
		obligations, err := readAttachmentWriteObligations(ctx, tx, ids)
		if err != nil {
			return recordMutationResources{}, err
		}
		set.WriteObligations = append(set.WriteObligations, obligations...)
		rows, err = tx.Query(ctx, `select distinct b.blob_key,b.sha256_digest,b.object_version,b.size_bytes,b.backend_kind,coalesce(b.namespace_id::text,'')
			from public.record_attachments a join public.blob_objects b on b.blob_key=a.blob_key or b.blob_key=a.preview_blob_key
			where a.draft_id=$1 order by b.blob_key`, draftID)
		if err != nil {
			return recordMutationResources{}, err
		}
		for rows.Next() {
			var blob attachments.BlobObject
			var digest []byte
			if err := rows.Scan(&blob.Key, &digest, &blob.ObjectVersion, &blob.SizeBytes, &blob.BackendKind, &blob.NamespaceID); err != nil {
				rows.Close()
				return recordMutationResources{}, err
			}
			if len(digest) != sha256.Size {
				rows.Close()
				return recordMutationResources{}, records.ErrInvalidRevisionMutationResources
			}
			copy(blob.SHA256[:], digest)
			set.Blobs = append(set.Blobs, blob)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return recordMutationResources{}, err
		}
		sets = append(sets, set)
	}
	return normalizeRecordMutationDeclarations(sets...)
}

func (repository *PostgresRecordDraftRepository) prepareDraftAuthorization(ctx context.Context, recordID string) (currentRecordAuthorizationTransactionResolver, error) {
	if recordID == "" {
		return nil, nil
	}
	if nilRecordSubjectDependency(repository.authorization) {
		return nil, ErrRecordSubjectUnavailable
	}
	return repository.authorization.prepareCurrentAuthorization(ctx, []string{recordID})
}

func (repository *PostgresRecordDraftRepository) authorizeDraftMutation(ctx context.Context, tx pgx.Tx, projectID, authorID, recordID string, capability recordauth.Capability, prepared currentRecordAuthorizationTransactionResolver) error {
	members := repository.members
	if nilRecordSubjectDependency(members) {
		members = NewPostgresCollaborationMembershipReader()
	}
	actor, err := members.ReadMemberActor(ctx, tx, recordauth.ProjectID(projectID), authorID)
	if err != nil {
		return recordauth.ErrDenied
	}
	if recordID == "" {
		if actor.Role != recordauth.RoleProjectAdmin {
			return recordauth.ErrDenied
		}
		return nil
	}
	if nilRecordSubjectDependency(prepared) {
		return ErrRecordSubjectUnavailable
	}
	current, err := prepared.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, recordID)
	if err != nil {
		return err
	}
	return records.AuthorizeRecordResource(actor, capability, current.Evidence)
}

// acquireDraftMutation rechecks the entire observed set and namespace list only
// after every declared rank has been acquired. A changed route restarts the
// transaction; no missing earlier lock is taken late.
func acquireDraftMutation(ctx context.Context, tx pgx.Tx, draftIDs []string) (recordMutationResources, error) {
	return acquireDraftMutationWithQuota(ctx, tx, draftIDs, "")
}

// Upload reservation creates quota responsibility even when the draft has no
// attachments yet, so its quota row must be included before rank 72.
func acquireDraftMutationWithQuota(ctx context.Context, tx pgx.Tx, draftIDs []string, quotaProject string) (recordMutationResources, error) {
	var namespaces []string
	route := attachmentWriterRoute{draftIDs: draftIDs, heldNamespaces: &namespaces}
	if err := lockAttachmentWriterPrefix(ctx, tx, route); err != nil {
		return recordMutationResources{}, err
	}
	before, err := discoverDraftMutationResources(ctx, tx, draftIDs, true)
	if err != nil {
		return recordMutationResources{}, err
	}
	if quotaProject != "" {
		before, err = normalizeRecordMutationDeclarations(before.resources, records.RevisionMutationResources{
			Projects: []records.RevisionMutationProject{{ProjectID: quotaProject, Kind: records.RevisionMutationAttachmentQuota}},
		})
		if err != nil {
			return recordMutationResources{}, err
		}
	}
	if err := acquireRecordMutationResources(ctx, tx, before); err != nil {
		return recordMutationResources{}, err
	}
	after, err := discoverDraftMutationResources(ctx, tx, draftIDs, true)
	if err == nil && quotaProject != "" {
		after, err = normalizeRecordMutationDeclarations(after.resources, records.RevisionMutationResources{
			Projects: []records.RevisionMutationProject{{ProjectID: quotaProject, Kind: records.RevisionMutationAttachmentQuota}},
		})
	}
	if err != nil || !reflect.DeepEqual(before.resources, after.resources) {
		return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "draft resources changed; restart transaction"}
	}
	currentNamespaces, err := discoverAttachmentWriterNamespaces(ctx, tx, route)
	if err != nil {
		return recordMutationResources{}, err
	}
	if !slices.Equal(namespaces, currentNamespaces) {
		return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "draft namespace changed; restart transaction"}
	}
	return after, nil
}

func draftSerializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

func draftRouteChanged() error {
	return &pgconn.PgError{Code: "40001", Message: "draft route changed; restart transaction"}
}
