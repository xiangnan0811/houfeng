package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// processorClaimCandidate is an unlocked observation, not a job claim. Its
// route and owner tuple are compared again after acquiring the earlier ranks.
type processorClaimCandidate struct {
	jobID, uploadID, attachmentID, projectID, authorID, recordID, draftID string
	sourceObligationID                                                    string
	state, ownerID                                                        string
	ownerGeneration, attempt, maxAttempts                                 int64
	leaseExpiresAt                                                        *time.Time
	expiresAt                                                             time.Time
	source                                                                attachments.BlobObject
}

func (repository *PostgresAttachmentRepository) discoverProcessorClaimCandidate(ctx context.Context, excluded []string) (*processorClaimCandidate, error) {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	candidate, err := readProcessorClaimCandidate(ctx, tx, "", excluded, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return candidate, nil
}

// Completion discovers the persisted route before W2 prefetch and starts its
// writer transaction only after that external preparation has finished.
func (repository *PostgresAttachmentRepository) discoverProcessorCompletionCandidate(ctx context.Context, jobID string) (*processorClaimCandidate, error) {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	candidate, err := readProcessorClaimCandidate(ctx, tx, jobID, nil, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return candidate, nil
}

func readProcessorClaimCandidate(ctx context.Context, tx attachmentTx, jobID string, excluded []string, eligibleOnly bool) (*processorClaimCandidate, error) {
	if excluded == nil {
		excluded = []string{}
	}
	var candidate processorClaimCandidate
	var digest []byte
	var transport attachments.TransportKind
	err := tx.QueryRow(ctx, `select j.processor_job_id,j.upload_id,j.attachment_id,u.project_id,u.author_id,
		coalesce(a.record_id,d.record_id,''),coalesce(a.draft_id,''),j.processor_state,coalesce(j.owner_id,''),
		j.owner_generation,j.lease_expires_at,j.attempt,j.max_attempts,j.expires_at,
		p.sha256_digest,p.object_version,p.size_bytes,coalesce(p.namespace_id::text,''),u.transport_kind,coalesce(p.obligation_id,'')
		from public.attachment_processor_jobs j
		join public.attachment_uploads u on u.upload_id=j.upload_id and u.attachment_id=j.attachment_id
		join public.record_attachments a on a.attachment_id=j.attachment_id and a.project_id=u.project_id
		left join public.record_drafts d on d.draft_id=a.draft_id
		join public.attachment_upload_parts p on p.upload_id=u.upload_id and p.part_number=1
		where ($1::text='' or j.processor_job_id=$1)
		  and not (j.processor_job_id=any($2::text[]))
		  and (not $3::boolean or ((j.processor_state='queued' or (j.processor_state='retry_wait' and j.retry_at<=clock_timestamp())
		       or (j.processor_state='claimed' and j.lease_expires_at<=clock_timestamp()))
		  and j.attempt<j.max_attempts and j.expires_at>clock_timestamp()
		  and u.upload_state='quarantined' and a.attachment_state='quarantined'))
		order by j.created_at,j.processor_job_id limit 1`, jobID, excluded, eligibleOnly).Scan(
		&candidate.jobID, &candidate.uploadID, &candidate.attachmentID, &candidate.projectID, &candidate.authorID,
		&candidate.recordID, &candidate.draftID, &candidate.state, &candidate.ownerID,
		&candidate.ownerGeneration, &candidate.leaseExpiresAt, &candidate.attempt, &candidate.maxAttempts, &candidate.expiresAt,
		&digest, &candidate.source.ObjectVersion, &candidate.source.SizeBytes, &candidate.source.NamespaceID, &transport, &candidate.sourceObligationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover attachment processor candidate: %w", err)
	}
	if len(digest) != sha256.Size {
		return nil, attachments.ErrAttachmentConflict
	}
	copy(candidate.source.SHA256[:], digest)
	candidate.source.Key = "sha256/" + hex.EncodeToString(digest)
	switch transport {
	case attachments.TransportKindLocal:
		candidate.source.BackendKind = attachments.BackendKindLocal
	case attachments.TransportKindS3:
		candidate.source.BackendKind = attachments.BackendKindS3
	default:
		return nil, attachments.ErrAttachmentConflict
	}
	if candidate.source.Validate() != nil {
		return nil, attachments.ErrAttachmentConflict
	}
	return &candidate, nil
}

func discoverProcessorClaimResources(ctx context.Context, tx pgx.Tx, candidate processorClaimCandidate, publicationID string, includeJob bool) (recordMutationResources, error) {
	var resources records.RevisionMutationResources
	var user records.RevisionMutationUser
	if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, candidate.authorID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
		return recordMutationResources{}, err
	}
	resources.Users = append(resources.Users, user)
	groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, recordauth.ProjectID(candidate.projectID), candidate.authorID)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, groupID := range groups {
		resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: candidate.projectID, GroupID: groupID})
		resources.Memberships = append(resources.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: candidate.authorID})
	}
	if candidate.recordID != "" {
		record := records.RevisionMutationRecord{ProjectID: candidate.projectID, RecordID: candidate.recordID}
		resources.Records = append(resources.Records, record)
		if err := appendEvidenceCurrentRecordResources(ctx, tx, &resources, record); err != nil {
			return recordMutationResources{}, err
		}
	}
	var attachment records.RevisionMutationAttachment
	if err := tx.QueryRow(ctx, `select project_id,attachment_id,coalesce(record_id,''),coalesce(draft_id,''),attachment_state,logical_size_bytes from public.record_attachments where attachment_id=$1`, candidate.attachmentID).Scan(
		&attachment.ProjectID, &attachment.AttachmentID, &attachment.RecordID, &attachment.DraftID, &attachment.State, &attachment.LogicalSizeBytes); err != nil {
		return recordMutationResources{}, err
	}
	resources.Attachments = append(resources.Attachments, attachment)
	if candidate.draftID != "" {
		var draft records.RevisionMutationDraft
		var etag []byte
		if err := tx.QueryRow(ctx, `select project_id,draft_id,coalesce(record_id,''),author_id,etag_digest,coalesce(base_revision_id,'') from public.record_drafts where draft_id=$1`, candidate.draftID).Scan(
			&draft.ProjectID, &draft.DraftID, &draft.RecordID, &draft.AuthorID, &etag, &draft.BaseRevisionID); err != nil {
			return recordMutationResources{}, err
		}
		if len(etag) != sha256.Size {
			return recordMutationResources{}, attachments.ErrAttachmentConflict
		}
		copy(draft.ETag[:], etag)
		resources.Drafts = append(resources.Drafts, draft)
	}
	uploads, err := readRevisionMutationRows[records.RevisionMutationUpload](ctx, tx, `select project_id,upload_id,attachment_id,origin_draft_id,upload_state,transport_kind,reserved_size_bytes,
		coalesce(temporary_object_key,''),coalesce(temporary_object_deleted_at,'0001-01-01 00:00:00+00'::timestamptz)
		from public.attachment_uploads where upload_id=$1`, candidate.uploadID)
	if err != nil || len(uploads) != 1 {
		return recordMutationResources{}, attachments.ErrAttachmentConflict
	}
	resources.Uploads = append(resources.Uploads, uploads...)
	workspaces, err := readRevisionMutationRows[records.RevisionMutationWorkspace](ctx, tx, `select workspace_id,processor_job_id,workspace_state from public.content_processor_workspaces where processor_job_id=$1 order by workspace_id`, candidate.jobID)
	if err != nil {
		return recordMutationResources{}, err
	}
	resources.Workspaces = append(resources.Workspaces, workspaces...)
	publications, err := readRevisionMutationRows[records.RevisionMutationPublication](ctx, tx, `select publication_id,owner_kind,owner_id,blob_key,coalesce(object_version,''),publication_state,
		project_id,owner_generation,backend_kind,coalesce(namespace_id::text,''),size_bytes,cleanup_owner_id,cleanup_generation,
		coalesce(cleanup_lease_expires_at,'0001-01-01 00:00:00+00'::timestamptz),coalesce(obligation_id,'')
		from public.blob_publication_intents where owner_kind='processor_preview' and owner_id=$1 and (publication_state<>'completed' or publication_id=$2) order by publication_id`, candidate.jobID, publicationID)
	if err != nil {
		return recordMutationResources{}, err
	}
	resources.Publications = append(resources.Publications, publications...)
	if includeJob {
		job := records.RevisionMutationJob{ProcessorJobID: candidate.jobID, AttachmentID: candidate.attachmentID,
			UploadID: candidate.uploadID, State: candidate.state, OwnerID: candidate.ownerID, OwnerGeneration: candidate.ownerGeneration}
		if candidate.leaseExpiresAt != nil {
			job.LeaseExpiresAt = *candidate.leaseExpiresAt
		}
		resources.Jobs = append(resources.Jobs, job)
	}
	obligationIDs := make([]string, 0, len(publications)+1)
	if candidate.sourceObligationID != "" {
		obligationIDs = append(obligationIDs, candidate.sourceObligationID)
	}
	for _, publication := range publications {
		if publication.ObligationID != "" {
			obligationIDs = append(obligationIDs, publication.ObligationID)
		}
	}
	obligations, err := readAttachmentWriteObligations(ctx, tx, obligationIDs)
	if err != nil {
		return recordMutationResources{}, err
	}
	resources.WriteObligations = append(resources.WriteObligations, obligations...)
	resources.Parts = append(resources.Parts, records.RevisionMutationPart{UploadID: candidate.uploadID, PartNumber: 1, Object: candidate.source})
	resources.Blobs = append(resources.Blobs, candidate.source)
	return normalizeRecordMutationDeclarations(resources)
}

// A preview can have been materialized by an earlier owner and legally rebound
// on takeover. Its persisted origin remains immutable; the current publication
// and job owner are checked independently after all rows have been acquired.
func validateProcessorCompletionObligations(candidate processorClaimCandidate, input attachments.ProcessorCompletionInput, resources records.RevisionMutationResources) (records.RevisionMutationWriteObligation, error) {
	var source, preview records.RevisionMutationWriteObligation
	for _, obligation := range resources.WriteObligations {
		if obligation.ObligationID == candidate.sourceObligationID {
			source = obligation
		}
		if obligation.ObligationID == input.PreviewPublicationIntent.ObligationID {
			preview = obligation
		}
	}
	if input.Result.Code != attachments.ProcessorResultCodeClean && candidate.sourceObligationID == "" {
		return source, nil
	}
	if candidate.sourceObligationID == "" || source.ObligationID == "" ||
		source.ProjectID != candidate.projectID || source.OriginKind != "upload" ||
		source.OriginID != candidate.uploadID || source.OriginGeneration != 1 ||
		source.RecordID != candidate.recordID || source.Outcome != "materialized" ||
		source.CanonicalKey != candidate.source.Key || source.SHA256 != candidate.source.SHA256 ||
		source.SizeBytes != candidate.source.SizeBytes || source.ObjectVersion != candidate.source.ObjectVersion ||
		source.BackendKind != candidate.source.BackendKind || source.NamespaceID != candidate.source.NamespaceID {
		return records.RevisionMutationWriteObligation{}, attachments.ErrAttachmentConflict
	}
	if !input.Result.HasPreview {
		return source, nil
	}
	intent := input.PreviewPublicationIntent
	if intent.ObligationID == "" || preview.ObligationID == "" || preview.ProjectID != candidate.projectID ||
		preview.OriginKind != "processor_preview" || preview.OriginID != candidate.jobID ||
		preview.RecordID != candidate.recordID || preview.PublicationID != intent.PublicationID ||
		preview.Outcome != "materialized" || preview.CanonicalKey != intent.Target.Key ||
		preview.SHA256 != intent.Target.SHA256 || preview.SizeBytes != intent.Target.SizeBytes ||
		preview.ObjectVersion != intent.ObjectVersion || preview.BackendKind != intent.Target.BackendKind ||
		preview.NamespaceID != intent.NamespaceID {
		return records.RevisionMutationWriteObligation{}, attachments.ErrAttachmentConflict
	}
	for _, publication := range resources.Publications {
		if publication.PublicationID == intent.PublicationID && publication.ObligationID == intent.ObligationID &&
			publication.ProjectID == intent.ProjectID && publication.OwnerKind == intent.OwnerKind &&
			publication.OwnerID == intent.OwnerID && publication.OwnerGeneration == intent.OwnerGeneration &&
			publication.BlobKey == intent.Target.Key && publication.ObjectVersion == intent.ObjectVersion &&
			publication.BackendKind == intent.Target.BackendKind && publication.NamespaceID == intent.NamespaceID &&
			publication.SizeBytes == intent.Target.SizeBytes {
			return source, nil
		}
	}
	return records.RevisionMutationWriteObligation{}, attachments.ErrAttachmentConflict
}
