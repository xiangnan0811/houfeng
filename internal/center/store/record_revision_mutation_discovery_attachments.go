package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

func (recordAttachmentRevisionParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
		return records.RevisionMutationResources{}, err
	}
	resources := records.RevisionMutationResources{Records: []records.RevisionMutationRecord{{ProjectID: "default", RecordID: command.RecordID}}}
	attachmentIDs := command.Input.AttachmentIDs()
	// One projection covers referenced AND unreferenced draft attachments. A
	// referenced survivor is discovered here but is not authorized for release.
	logical, err := readRevisionMutationRows[records.RevisionMutationAttachment](ctx, tx, `
		select project_id,attachment_id,coalesce(record_id,''),coalesce(draft_id,''),attachment_state,logical_size_bytes
		from public.record_attachments where attachment_id=any($1::text[]) or ($2::text<>'' and draft_id=$2)
		order by attachment_id`, attachmentIDs, command.DraftID)
	if err != nil {
		return resources, err
	}
	resources.Attachments = logical
	for _, item := range command.ImportedAttachments {
		if slices.ContainsFunc(logical, func(v records.RevisionMutationAttachment) bool { return v.AttachmentID == item.AttachmentID }) {
			return resources, records.ErrInvalidRevisionMutationResources
		}
		resources.Imports = append(resources.Imports, item) // Keep original GCObservation verbatim.
		resources.Attachments = append(resources.Attachments, records.RevisionMutationAttachment{ProjectID: "default", AttachmentID: item.AttachmentID, RecordID: command.RecordID, State: "available", LogicalSizeBytes: item.LogicalSizeBytes})
		resources.Blobs = append(resources.Blobs, attachments.BlobObject{Key: item.Object.Key, ObjectVersion: item.Object.VersionID, SHA256: item.Object.SHA256, SizeBytes: item.Object.SizeBytes, BackendKind: item.BackendKind, NamespaceID: item.NamespaceID})
	}
	var revisionID string
	if len(attachmentIDs) > 0 {
		revisionID, err = command.RevisionID()
		if err != nil {
			return resources, err
		}
	}
	for ordinal, attachmentID := range attachmentIDs {
		if !slices.ContainsFunc(resources.Attachments, func(v records.RevisionMutationAttachment) bool { return v.AttachmentID == attachmentID }) {
			return resources, attachments.ErrAttachmentConflict
		}
		resources.NewRevisionEffects = append(resources.NewRevisionEffects, records.RevisionMutationNewRevisionEffect{ProjectID: "default", RecordID: command.RecordID, RevisionID: revisionID, Kind: records.RevisionMutationNewAttachmentReference, Ordinal: int64(ordinal), AttachmentID: attachmentID})
	}
	if len(command.ImportedAttachments) != 0 {
		resources.Projects = append(resources.Projects, records.RevisionMutationProject{ProjectID: "default", Kind: records.RevisionMutationAttachmentQuota})
		resources.BlobTables = append(resources.BlobTables, records.RevisionMutationBlobMetadataTable)
	}
	if command.DraftID != "" {
		var draft records.RevisionMutationDraft
		var etag []byte
		err := tx.QueryRow(ctx, `select project_id,draft_id,coalesce(record_id,''),author_id,etag_digest,coalesce(base_revision_id,'') from public.record_drafts where draft_id=$1`, command.DraftID).Scan(&draft.ProjectID, &draft.DraftID, &draft.RecordID, &draft.AuthorID, &etag, &draft.BaseRevisionID)
		if err != nil {
			return resources, err
		}
		if len(etag) != 32 {
			return resources, records.ErrInvalidRevisionMutationResources
		}
		copy(draft.ETag[:], etag)
		resources.Drafts = append(resources.Drafts, draft)
		if draft.RecordID != "" {
			resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: draft.ProjectID, RecordID: draft.RecordID})
		}
		if err := discoverRevisionDraftRelease(ctx, tx, command.DraftID, &resources); err != nil {
			return resources, err
		}
	}
	for _, attachment := range resources.Attachments {
		if attachment.RecordID != "" {
			resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: attachment.ProjectID, RecordID: attachment.RecordID})
		}
	}
	return records.CombineRevisionMutationResources(resources)
}

// Draft cleanup also runs for a canonical no-op and when the attachment
// participant is absent, so its owned route must not depend on input refs.
func discoverRevisionDraftCleanup(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	var resources records.RevisionMutationResources
	if command.DraftID == "" {
		return resources, nil
	}
	var draft records.RevisionMutationDraft
	var etag []byte
	err := tx.QueryRow(ctx, `select project_id,draft_id,coalesce(record_id,''),author_id,etag_digest,coalesce(base_revision_id,'') from public.record_drafts where draft_id=$1`, command.DraftID).Scan(&draft.ProjectID, &draft.DraftID, &draft.RecordID, &draft.AuthorID, &etag, &draft.BaseRevisionID)
	if err != nil {
		return resources, err
	}
	if len(etag) != 32 {
		return resources, records.ErrInvalidRevisionMutationResources
	}
	copy(draft.ETag[:], etag)
	resources.Drafts = append(resources.Drafts, draft)
	if draft.RecordID != "" {
		resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: draft.ProjectID, RecordID: draft.RecordID})
	}
	resources.Attachments, err = readRevisionMutationRows[records.RevisionMutationAttachment](ctx, tx, `
		select project_id,attachment_id,coalesce(record_id,''),coalesce(draft_id,''),attachment_state,logical_size_bytes
		from public.record_attachments where draft_id=$1 order by attachment_id`, command.DraftID)
	if err != nil {
		return resources, err
	}
	if err := discoverRevisionDraftRelease(ctx, tx, command.DraftID, &resources); err != nil {
		return resources, err
	}
	return records.CombineRevisionMutationResources(resources)
}

func discoverRevisionDraftRelease(ctx context.Context, tx pgx.Tx, draftID string, resources *records.RevisionMutationResources) error {
	owned := make([]string, 0)
	for _, attachment := range resources.Attachments {
		if attachment.DraftID == draftID {
			owned = append(owned, attachment.AttachmentID)
		}
	}
	if len(owned) == 0 {
		return nil
	}
	resources.Projects = append(resources.Projects, records.RevisionMutationProject{ProjectID: "default", Kind: records.RevisionMutationAttachmentQuota})
	resources.BlobTables = append(resources.BlobTables, records.RevisionMutationBlobMetadataTable, records.RevisionMutationUploadPartsTable)
	uploads, err := readRevisionMutationRows[records.RevisionMutationUpload](ctx, tx, `
		select project_id,upload_id,attachment_id,origin_draft_id,upload_state,transport_kind,reserved_size_bytes,
		       coalesce(temporary_object_key,''),coalesce(temporary_object_deleted_at,'0001-01-01 00:00:00+00'::timestamptz)
		from public.attachment_uploads where attachment_id=any($1::text[]) order by upload_id`, owned)
	if err != nil {
		return err
	}
	resources.Uploads = append(resources.Uploads, uploads...)
	uploadIDs := make([]string, 0, len(uploads))
	transports := make(map[string]attachments.TransportKind, len(uploads))
	seenAttachment := make(map[string]bool, len(uploads))
	for _, upload := range uploads {
		if upload.OriginDraftID != draftID || seenAttachment[upload.AttachmentID] {
			return records.ErrInvalidRevisionMutationResources
		}
		seenAttachment[upload.AttachmentID] = true
		uploadIDs = append(uploadIDs, upload.UploadID)
		transports[upload.UploadID] = upload.Transport
	}
	workspaces, err := readRevisionMutationRows[records.RevisionMutationWorkspace](ctx, tx, `
		select workspace.workspace_id,workspace.processor_job_id,workspace.workspace_state
		from public.content_processor_workspaces workspace
		join public.attachment_processor_jobs job on job.processor_job_id=workspace.processor_job_id
		where job.attachment_id=any($1::text[]) order by workspace.workspace_id`, owned)
	if err != nil {
		return err
	}
	resources.Workspaces = append(resources.Workspaces, workspaces...)
	jobs, err := readRevisionMutationRows[records.RevisionMutationJob](ctx, tx, `
		select processor_job_id,attachment_id,processor_state,coalesce(lease_expires_at,'0001-01-01 00:00:00+00'::timestamptz),upload_id,owner_id,owner_generation
		from public.attachment_processor_jobs where attachment_id=any($1::text[]) order by processor_job_id`, owned)
	if err != nil {
		return err
	}
	resources.Jobs = append(resources.Jobs, jobs...)
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ProcessorJobID)
	}
	publications, err := readRevisionMutationRows[records.RevisionMutationPublication](ctx, tx, `
		select publication_id,owner_kind,owner_id,blob_key,coalesce(object_version,''),publication_state,
		       project_id,owner_generation,backend_kind,coalesce(namespace_id::text,''),size_bytes,
		       cleanup_owner_id,cleanup_generation,coalesce(cleanup_lease_expires_at,'0001-01-01 00:00:00+00'::timestamptz),coalesce(obligation_id,'')
		from public.blob_publication_intents where publication_state<>'completed'
		  and ((owner_kind='upload' and owner_id=any($1::text[]))
		    or (owner_kind='processor_preview' and owner_id=any($2::text[]))) order by publication_id`, uploadIDs, jobIDs)
	if err != nil {
		return err
	}
	resources.Publications = append(resources.Publications, publications...)
	rows, err := tx.Query(ctx, `select upload_id,part_number,size_bytes,sha256_digest,object_version,coalesce(namespace_id::text,'') from public.attachment_upload_parts where upload_id=any($1::text[]) order by upload_id,part_number`, uploadIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var part records.RevisionMutationPart
		var digest []byte
		if err := rows.Scan(&part.UploadID, &part.PartNumber, &part.Object.SizeBytes, &digest, &part.Object.ObjectVersion, &part.Object.NamespaceID); err != nil {
			rows.Close()
			return err
		}
		if len(digest) != 32 {
			rows.Close()
			return records.ErrInvalidRevisionMutationResources
		}
		copy(part.Object.SHA256[:], digest)
		part.Object.Key = "sha256/" + hex.EncodeToString(digest)
		switch transports[part.UploadID] {
		case attachments.TransportKindLocal:
			part.Object.BackendKind = attachments.BackendKindLocal
		case attachments.TransportKindS3:
			part.Object.BackendKind = attachments.BackendKindS3
		default:
			rows.Close()
			return fmt.Errorf("%w: upload transport", records.ErrInvalidRevisionMutationResources)
		}
		resources.Parts = append(resources.Parts, part)
		resources.Blobs = append(resources.Blobs, part.Object)
	}
	rows.Close()
	return rows.Err()
}
