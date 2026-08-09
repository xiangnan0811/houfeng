package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

const (
	attachmentDeletionHealthDigestDomainV1  = "houfeng.attachments.deletion-health.v1"
	attachmentDeletionPreviewDigestDomainV1 = "houfeng.attachments.deletion-preview.v1"
	attachmentDeletionImpactDigestDomainV1  = "houfeng.attachments.deletion-impact.v1"
	attachmentDeletionReceiptDigestDomainV1 = "houfeng.attachments.deletion-receipt.v1"
	attachmentDeletionMarkerDigestDomainV1  = "houfeng.attachments.deletion-marker.v1"
)

const (
	attachmentDeletionSurfaceLogicalAttachment = "logical_attachment"
	attachmentDeletionSurfaceUploadPart        = "upload_part"
	attachmentDeletionSurfaceBlobObject        = "blob_object"
)

var attachmentDeletionReceiptSurfaces = []string{
	attachmentDeletionSurfaceLogicalAttachment,
	attachmentDeletionSurfaceUploadPart,
	attachmentDeletionSurfaceBlobObject,
}

const attachmentDeletionHealthSQL = `
	select count(*)::bigint
	from pg_catalog.pg_class as relation
	join pg_catalog.pg_namespace as namespace
	  on namespace.oid = relation.relnamespace
	where namespace.nspname = 'public'
	  and relation.relkind in ('r', 'p')
	  and relation.relname = any($1::text[])`

const previewAttachmentDeletionSQL = `
	with root as materialized (
		select record.record_id, record.project_id
		from public.records as record
		join public.content_delivery_epochs as epoch
		  on epoch.project_id = record.project_id
		 and epoch.object_kind = 'record'
		 and epoch.object_id = record.record_id
		where record.project_id = $1
		  and record.current_revision_id = $2
		  and record.lock_version = $3
		  and record.authorization_epoch = $4
		  and epoch.delivery_epoch = $5
		  and record.record_id = $6
	), target_attachments as materialized (
		select attachment.*
		from public.record_attachments as attachment
		join root on root.project_id = attachment.project_id
		where attachment.record_id = root.record_id
		   or attachment.draft_id in (
			select draft.draft_id
			from public.record_drafts as draft
			where draft.record_id = root.record_id
		   )
	), attachment_material as (
		select coalesce(jsonb_agg(jsonb_build_array(
			attachment.attachment_id, attachment.attachment_state,
			attachment.logical_size_bytes, attachment.blob_key,
			attachment.blob_object_version, attachment.preview_blob_key,
			attachment.preview_blob_object_version
		) order by attachment.attachment_id), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from target_attachments as attachment
	), revision_material as (
		select coalesce(jsonb_agg(jsonb_build_array(
			revision_ref.revision_id, revision_ref.ordinal,
			revision_ref.attachment_id
		) order by revision_ref.revision_id, revision_ref.ordinal), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_revision_attachments as revision_ref
		join root on root.record_id = revision_ref.record_id
	), upload_material as (
		select coalesce(jsonb_agg(jsonb_build_array(
			upload.upload_id, upload.upload_state, upload.attachment_id,
			upload.temporary_object_key, upload.temporary_object_version
		) order by upload.upload_id), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.attachment_uploads as upload
		join target_attachments as attachment
		  on attachment.attachment_id = upload.attachment_id
	), processor_material as (
		select coalesce(jsonb_agg(jsonb_build_array(
			job.processor_job_id, job.processor_state, job.upload_id
		) order by job.processor_job_id), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.attachment_processor_jobs as job
		join target_attachments as attachment
		  on attachment.attachment_id = job.attachment_id
	), workspace_material as (
		select coalesce(jsonb_agg(jsonb_build_array(
			workspace.workspace_id, workspace.workspace_state,
			workspace.processor_job_id
		) order by workspace.workspace_id), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.content_processor_workspaces as workspace
		join public.attachment_processor_jobs as job
		  on job.processor_job_id = workspace.processor_job_id
		join target_attachments as attachment
		  on attachment.attachment_id = job.attachment_id
	), target_blobs as materialized (
		select attachment.blob_key, attachment.blob_object_version
		from target_attachments as attachment
		where attachment.blob_key is not null
		union
		select attachment.preview_blob_key, attachment.preview_blob_object_version
		from target_attachments as attachment
		where attachment.preview_blob_key is not null
	), surviving as (
		select count(distinct other.attachment_id)::bigint as copy_count
		from public.record_attachments as other
		join root on root.project_id = other.project_id
		join target_blobs as blob
		  on (other.blob_key = blob.blob_key
		      and other.blob_object_version = blob.blob_object_version)
		  or (other.preview_blob_key = blob.blob_key
		      and other.preview_blob_object_version = blob.blob_object_version)
		where other.record_id is not null
		  and other.record_id <> root.record_id
	)
	select pg_catalog.convert_to(jsonb_build_object(
		'attachments', attachment_material.material,
		'revision_refs', revision_material.material,
		'uploads', upload_material.material,
		'processor_jobs', processor_material.material,
		'workspaces', workspace_material.material
	)::text, 'UTF8') as attachment_dependency_material,
	pg_catalog.convert_to(jsonb_build_object(
		'attachment_count', attachment_material.row_count,
		'revision_ref_count', revision_material.row_count,
		'upload_count', upload_material.row_count,
		'processor_job_count', processor_material.row_count,
		'workspace_count', workspace_material.row_count,
		'surviving_other_record_copy_count', surviving.copy_count
	)::text, 'UTF8') as attachment_impact_material,
	surviving.copy_count
	from root
	cross join attachment_material
	cross join revision_material
	cross join upload_material
	cross join processor_material
	cross join workspace_material
	cross join surviving`

const lockAttachmentPurgeOperationSQL = `
	select operation.operation_state,
	       operation.reason_code,
	       operation.ledger_sequence,
	       operation.ledger_entry_hash,
	       reservation.state,
	       reservation.fence_epoch,
	       reservation.object_id,
	       reservation.project_id
	from public.record_purge_operations as operation
	join public.deletion_reservations as reservation
	  on reservation.reservation_id = operation.reservation_id
	where operation.operation_id = $1
	  and operation.reservation_id = $2
	  and operation.project_id = $3
	  and reservation.project_id = $3
	  and reservation.object_kind = 'record'
	  and reservation.object_id = $4
	for update of operation, reservation`

const selectAttachmentPurgeTargetsSQL = `
	select attachment.attachment_id,
	       attachment.attachment_state,
	       attachment.logical_size_bytes
	from public.record_attachments as attachment
	where attachment.project_id = $1
	  and (attachment.record_id = $2
	    or attachment.draft_id in (
	      select draft.draft_id
	      from public.record_drafts as draft
	      where draft.record_id = $2
	    ))
	order by attachment.attachment_id
	for update`

const selectAttachmentPurgeObjectsSQL = `
	with target_object as (
		select attachment.blob_key as blob_key,
		       attachment.blob_object_version as object_version
		from public.record_attachments as attachment
		where attachment.attachment_id = any($1::text[])
		  and attachment.blob_key is not null
		union
		select attachment.preview_blob_key as blob_key,
		       attachment.preview_blob_object_version as object_version
		from public.record_attachments as attachment
		where attachment.attachment_id = any($1::text[])
		  and attachment.preview_blob_key is not null
	)
	select blob.blob_key,
	       blob.sha256_digest,
	       blob.object_version,
	       blob.size_bytes,
	       blob.backend_kind,
	       blob.created_at
	from target_object
	join public.blob_objects as blob
	  on blob.blob_key = target_object.blob_key
	 and blob.object_version = target_object.object_version
	order by blob.blob_key, blob.object_version`

const attachmentPurgeHasActivePartialSQL = `
	select exists (
		select 1
		from public.attachment_uploads as upload
		where upload.attachment_id = any($1::text[])
		  and (upload.upload_state not in ('available', 'rejected', 'expired')
		    or (upload.temporary_object_key is not null
		      and upload.temporary_object_deleted_at is null))
	), exists (
		select 1
		from public.attachment_processor_jobs as job
		where job.attachment_id = any($1::text[])
		  and job.processor_state not in ('succeeded', 'rejected', 'expired')
	), exists (
			select 1
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			where job.attachment_id = any($1::text[])
			  and workspace.workspace_state <> 'purged'
	), exists (
			select 1
			from public.blob_publication_intents as publication
			where publication.publication_state <> 'completed'
			  and ((publication.owner_kind = 'upload' and exists (
			    select 1
			    from public.attachment_uploads as upload
			    where upload.upload_id = publication.owner_id
			      and upload.attachment_id = any($1::text[])
			  )) or (publication.owner_kind = 'processor_preview' and exists (
			    select 1
			    from public.attachment_processor_jobs as job
			    where job.processor_job_id = publication.owner_id
			      and job.attachment_id = any($1::text[])
			  )))
	)`

const attachmentPurgeObjectBlockersSQL = `
	select exists (
		select 1
		from public.record_attachments as attachment
		where (attachment.blob_key = $1 and attachment.blob_object_version = $2)
		   or (attachment.preview_blob_key = $1 and attachment.preview_blob_object_version = $2)
	), exists (
		select 1
		from public.blob_gc_pins as pin
		where pin.blob_key = $1
		  and pin.blob_object_version = $2
		  and pin.expires_at > transaction_timestamp()
	), exists (
		select 1
		from public.attachment_upload_parts as part
		where part.sha256_digest = $3
		  and part.object_version = $2
	), exists (
		select 1
		from public.blob_publication_intents as publication
		where publication.blob_key = $1
		  and publication.publication_state <> 'completed'
	)`

const deleteAttachmentBlobMetadataForPurgeSQL = `
	delete from public.blob_objects as blob
	where blob.blob_key = $1 and blob.object_version = $2
	  and blob.sha256_digest = $3 and blob.size_bytes = $4
	  and blob.backend_kind = $5
	  and not exists (
	    select 1 from public.record_attachments as attachment
	    where (attachment.blob_key = blob.blob_key
	        and attachment.blob_object_version = blob.object_version)
	       or (attachment.preview_blob_key = blob.blob_key
	        and attachment.preview_blob_object_version = blob.object_version))
	  and not exists (
	    select 1 from public.attachment_upload_parts as part
	    where part.sha256_digest = blob.sha256_digest
	      and part.object_version = blob.object_version)
	  and not exists (
	    select 1 from public.blob_gc_pins as pin
	    where pin.blob_key = blob.blob_key
	      and pin.blob_object_version = blob.object_version)
	  and not exists (
	    select 1 from public.blob_publication_intents as publication
	    where publication.blob_key = blob.blob_key
	      and publication.publication_state <> 'completed')`

const attachmentPurgeContentPresentSQL = `
	select exists (
		select 1
		from public.record_attachments as attachment
		where attachment.project_id = $1
		  and (attachment.record_id = $2
		    or attachment.draft_id in (
		      select draft.draft_id
		      from public.record_drafts as draft
		      where draft.record_id = $2
		    ))
		union all
		select 1
		from public.record_revision_attachments as revision_ref
		where revision_ref.record_id = $2
	) as content_present`

type attachmentDeletionRowsTx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type attachmentDeletionObject struct {
	object    attachments.BlobObject
	createdAt time.Time
}

type attachmentDeletionReceiptCounts struct {
	counts     map[string]uint64
	verifiedAt time.Time
}

type attachmentDeletionPreparation struct {
	claims []blobGCStoredDeletion
	counts attachmentDeletionReceiptCounts
}

func (repository *PostgresAttachmentRepository) AttachmentDeletionHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, attachments.ErrInvalidDeletionAdapter
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("begin attachment deletion health read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	surfaces := recorddeletion.RecordAttachmentsSurfaceNames()
	names := make([]string, len(surfaces))
	for index, surface := range surfaces {
		names[index] = string(surface)
	}
	var count int64
	if err := tx.QueryRow(ctx, attachmentDeletionHealthSQL, names).Scan(&count); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read attachment deletion health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, digestAttachmentDeletionHealth(names))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("commit attachment deletion health read: %w", err)
	}
	return health, nil
}

func (repository *PostgresAttachmentRepository) PreviewAttachmentDeletion(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, attachments.ErrInvalidDeletionAdapter
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("begin attachment deletion preview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var dependencyMaterial, impactMaterial []byte
	var survivingCopies int64
	if err := tx.QueryRow(ctx, previewAttachmentDeletionSQL,
		target.Object.ProjectID,
		target.CurrentRevisionID,
		int64(target.LockVersion),
		int64(target.AuthorizationEpoch),
		int64(target.ContentDeliveryEpoch),
		target.Object.ObjectID,
	).Scan(&dependencyMaterial, &impactMaterial, &survivingCopies); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
		}
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview attachment deletion: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 || survivingCopies < 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestAttachmentDeletionPreview(target.DependencyGraphDigest, dependencyMaterial),
		ImpactDigest:     digestAttachmentDeletionImpact(impactMaterial),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	if survivingCopies > 0 {
		preview.SurvivingCopies = append(preview.SurvivingCopies, recorddeletion.AdapterSurvivingCopy{
			Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: uint64(survivingCopies),
		})
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("commit attachment deletion preview: %w", err)
	}
	return preview, nil
}

func (repository *PostgresAttachmentRepository) ConfigureAttachmentDeletionBlobStore(
	backend attachments.BackendKind,
	blobStore attachments.BlobStore,
) error {
	if repository == nil || nilAttachmentDeletionBlobStore(blobStore) ||
		(backend != attachments.BackendKindLocal && backend != attachments.BackendKindS3) {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionBlobMu.Lock()
	defer repository.attachmentDeletionBlobMu.Unlock()
	if repository.attachmentDeletionBlobStores == nil {
		repository.attachmentDeletionBlobStores = make(map[attachments.BackendKind]attachments.BlobStore)
	}
	if existing := repository.attachmentDeletionBlobStores[backend]; existing != nil && !sameAttachmentDeletionBlobStore(existing, blobStore) {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionBlobStores[backend] = blobStore
	return nil
}

func nilAttachmentDeletionBlobStore(blobStore attachments.BlobStore) bool {
	if blobStore == nil {
		return true
	}
	value := reflect.ValueOf(blobStore)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func sameAttachmentDeletionBlobStore(left, right attachments.BlobStore) bool {
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if !leftValue.IsValid() || !rightValue.IsValid() || leftValue.Type() != rightValue.Type() ||
		!leftValue.Comparable() || !rightValue.Comparable() {
		return false
	}
	return leftValue.Interface() == rightValue.Interface()
}

func (repository *PostgresAttachmentRepository) PurgeRecordAttachments(
	ctx context.Context,
	command attachments.AttachmentDeletionCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil ||
		command.Operation.LedgerSequence > math.MaxInt64 || uint64(command.Operation.FenceEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPurgeReceipt{}, attachments.ErrInvalidDeletionAdapter
	}
	preparation, err := repository.prepareAttachmentDeletion(ctx, command)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.executeAttachmentDeletionClaims(ctx, command, preparation.claims); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	receipt := attachmentDeletionAdapterReceipt(command, preparation.counts)
	if err := repository.VerifyRecordAttachmentsPurge(ctx, command, receipt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresAttachmentRepository) VerifyRecordAttachmentsPurge(
	ctx context.Context,
	command attachments.AttachmentDeletionCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil ||
		command.Operation.LedgerSequence > math.MaxInt64 || uint64(command.Operation.FenceEpoch) > math.MaxInt64 ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordAttachments ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() {
		return attachments.ErrInvalidDeletionAdapter
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin attachment purge verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateLockedAttachmentPurgeOperation(ctx, tx, command.Operation); err != nil {
		return err
	}
	counts, found, err := loadAttachmentDeletionReceiptCounts(ctx, tx, command)
	if err != nil {
		return err
	}
	if !found || attachmentDeletionAdapterReceipt(command, counts) != receipt {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := verifyAttachmentDeletionAbsent(ctx, tx, command.Operation); err != nil {
		return err
	}
	expectedGCClaims := counts.counts[attachmentDeletionSurfaceBlobObject]
	if expectedGCClaims > math.MaxInt64 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	var totalGCClaims, completedGCClaims int64
	var validGCProof bool
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint,
		       count(*) filter (where deletion.deletion_state = 'completed')::bigint,
		       coalesce(bool_and(
		         deletion.deletion_state = 'completed'
		         and deletion.retry_at is null
		         and deletion.physical_delete_result in ('deleted', 'already_absent')
		         and deletion.receipt_digest is not null
		         and octet_length(deletion.receipt_digest) = 32
		         and deletion.completed_at is not null
		         and octet_length(deletion.sha256_digest) = 32
		         and deletion.size_bytes > 0
		         and deletion.backend_kind in ('local', 's3')
		       ), true)
		from public.blob_gc_deletions as deletion
		where deletion.project_id = $1
		  and deletion.purge_mode = 'permanent'
		  and deletion.owner_id = $2`,
		command.Operation.Object.ProjectID,
		command.Operation.OperationID,
	).Scan(&totalGCClaims, &completedGCClaims, &validGCProof); err != nil {
		return fmt.Errorf("verify attachment permanent Blob GC completion: %w", err)
	}
	if totalGCClaims != int64(expectedGCClaims) || completedGCClaims != totalGCClaims || !validGCProof {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment purge verification: %w", err)
	}
	return nil
}

func (repository *PostgresAttachmentRepository) prepareAttachmentDeletion(
	ctx context.Context,
	command attachments.AttachmentDeletionCommand,
) (attachmentDeletionPreparation, error) {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachmentDeletionPreparation{}, fmt.Errorf("begin attachment purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateLockedAttachmentPurgeOperation(ctx, tx, command.Operation); err != nil {
		return attachmentDeletionPreparation{}, err
	}
	counts, found, err := loadAttachmentDeletionReceiptCounts(ctx, tx, command)
	if err != nil {
		return attachmentDeletionPreparation{}, err
	}
	if found {
		claims, err := loadAttachmentDeletionClaims(ctx, tx, command.Operation)
		if err != nil {
			return attachmentDeletionPreparation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachmentDeletionPreparation{}, fmt.Errorf("commit attachment purge replay read: %w", err)
		}
		return attachmentDeletionPreparation{claims: claims, counts: counts}, nil
	}
	if _, err := tx.Exec(ctx, `lock table public.blob_objects in share row exclusive mode`); err != nil {
		return attachmentDeletionPreparation{}, fmt.Errorf("lock Blob metadata for attachment purge: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.attachment_upload_parts in share row exclusive mode`); err != nil {
		return attachmentDeletionPreparation{}, fmt.Errorf("lock attachment upload parts for attachment purge: %w", err)
	}
	attachmentIDs, logicalBytes, err := loadAttachmentDeletionTargets(ctx, tx, command.Operation)
	if err != nil {
		return attachmentDeletionPreparation{}, err
	}
	objects, err := loadAttachmentDeletionObjects(ctx, tx, attachmentIDs)
	if err != nil {
		return attachmentDeletionPreparation{}, err
	}
	for _, candidate := range objects {
		if repository.attachmentDeletionBlobStore(candidate.object.BackendKind) == nil {
			return attachmentDeletionPreparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	if err := rejectActiveAttachmentDeletionPartials(ctx, tx, attachmentIDs); err != nil {
		return attachmentDeletionPreparation{}, err
	}
	removedLogicalRows, removedUploadParts, err := deleteAttachmentDeletionDependencies(ctx, tx, command.Operation, attachmentIDs)
	if err != nil {
		return attachmentDeletionPreparation{}, err
	}
	if len(attachmentIDs) > 0 {
		usage, quotaVersion, err := lockBlobGCQuotaAccount(ctx, tx, command.Operation.Object.ProjectID)
		if err != nil {
			return attachmentDeletionPreparation{}, err
		}
		if logicalBytes < 0 || usage.LogicalBytes < logicalBytes {
			return attachmentDeletionPreparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		usage.LogicalBytes -= logicalBytes
		if err := updateAttachmentQuotaAccount(ctx, tx, command.Operation.Object.ProjectID, quotaVersion, usage); err != nil {
			return attachmentDeletionPreparation{}, err
		}
	}
	claims, err := repository.claimAttachmentDeletionBlobs(ctx, tx, command.Operation, objects)
	if err != nil {
		return attachmentDeletionPreparation{}, err
	}
	if err := verifyAttachmentDeletionAbsent(ctx, tx, command.Operation); err != nil {
		return attachmentDeletionPreparation{}, err
	}
	counts = attachmentDeletionReceiptCounts{counts: map[string]uint64{
		attachmentDeletionSurfaceLogicalAttachment: removedLogicalRows,
		attachmentDeletionSurfaceUploadPart:        removedUploadParts,
		attachmentDeletionSurfaceBlobObject:        uint64(len(claims)),
	}}
	if err := insertAttachmentDeletionReceiptCounts(ctx, tx, command, &counts); err != nil {
		return attachmentDeletionPreparation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachmentDeletionPreparation{}, fmt.Errorf("commit attachment purge and durable GC claims: %w", err)
	}
	return attachmentDeletionPreparation{claims: claims, counts: counts}, nil
}

func validateLockedAttachmentPurgeOperation(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
) error {
	var operationState, reasonCode, reservationState, recordID, projectID string
	var ledgerSequence, fenceEpoch int64
	var ledgerEntryHash []byte
	if err := tx.QueryRow(ctx, lockAttachmentPurgeOperationSQL,
		operation.OperationID,
		operation.ReservationID,
		operation.Object.ProjectID,
		operation.Object.ObjectID,
	).Scan(
		&operationState,
		&reasonCode,
		&ledgerSequence,
		&ledgerEntryHash,
		&reservationState,
		&fenceEpoch,
		&recordID,
		&projectID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return fmt.Errorf("lock attachment purge operation: %w", err)
	}
	if len(ledgerEntryHash) != sha256.Size || ledgerSequence < 1 || fenceEpoch < 1 {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	var storedHash [sha256.Size]byte
	copy(storedHash[:], ledgerEntryHash)
	if operationState != string(recorddeletion.DeletionStateOnlinePurging) ||
		reasonCode != string(operation.ReasonCode) || uint64(ledgerSequence) != operation.LedgerSequence ||
		storedHash != operation.LedgerEntryHash || reservationState != "committed" ||
		uint64(fenceEpoch) != uint64(operation.FenceEpoch) || recordID != operation.Object.ObjectID ||
		projectID != operation.Object.ProjectID {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func loadAttachmentDeletionTargets(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
) ([]string, int64, error) {
	queryTx, ok := tx.(attachmentDeletionRowsTx)
	if !ok {
		return nil, 0, recorddeletion.ErrDeletionSafetyUnavailable
	}
	rows, err := queryTx.Query(ctx, selectAttachmentPurgeTargetsSQL,
		operation.Object.ProjectID, operation.Object.ObjectID)
	if err != nil {
		return nil, 0, fmt.Errorf("select attachment purge targets: %w", err)
	}
	defer rows.Close()
	attachmentIDs := make([]string, 0)
	var logicalBytes int64
	for rows.Next() {
		var attachmentID, state string
		var size int64
		if err := rows.Scan(&attachmentID, &state, &size); err != nil {
			return nil, 0, fmt.Errorf("scan attachment purge target: %w", err)
		}
		if state != string(attachments.UploadStateAvailable) &&
			state != string(attachments.UploadStateRejected) && state != string(attachments.UploadStateExpired) {
			return nil, 0, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if size <= 0 {
			return nil, 0, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if state == string(attachments.UploadStateAvailable) {
			if logicalBytes > math.MaxInt64-size {
				return nil, 0, recorddeletion.ErrDeletionSafetyUnavailable
			}
			logicalBytes += size
		}
		attachmentIDs = append(attachmentIDs, attachmentID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate attachment purge targets: %w", err)
	}
	return attachmentIDs, logicalBytes, nil
}

func loadAttachmentDeletionObjects(
	ctx context.Context,
	tx attachmentTx,
	attachmentIDs []string,
) ([]attachmentDeletionObject, error) {
	if len(attachmentIDs) == 0 {
		return []attachmentDeletionObject{}, nil
	}
	queryTx, ok := tx.(attachmentDeletionRowsTx)
	if !ok {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	rows, err := queryTx.Query(ctx, selectAttachmentPurgeObjectsSQL, attachmentIDs)
	if err != nil {
		return nil, fmt.Errorf("select attachment purge Blob identities: %w", err)
	}
	defer rows.Close()
	objects := make([]attachmentDeletionObject, 0)
	for rows.Next() {
		var candidate attachmentDeletionObject
		var digest []byte
		if err := rows.Scan(
			&candidate.object.Key,
			&digest,
			&candidate.object.ObjectVersion,
			&candidate.object.SizeBytes,
			&candidate.object.BackendKind,
			&candidate.createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan attachment purge Blob identity: %w", err)
		}
		if len(digest) != sha256.Size {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
		copy(candidate.object.SHA256[:], digest)
		candidate.createdAt = candidate.createdAt.UTC()
		if candidate.object.Validate() != nil || candidate.createdAt.IsZero() {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
		objects = append(objects, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment purge Blob identities: %w", err)
	}
	return objects, nil
}

func rejectActiveAttachmentDeletionPartials(
	ctx context.Context,
	tx attachmentTx,
	attachmentIDs []string,
) error {
	if len(attachmentIDs) == 0 {
		return nil
	}
	var activeUpload, activeJob, activeWorkspace, activePublication bool
	if err := tx.QueryRow(ctx, attachmentPurgeHasActivePartialSQL, attachmentIDs).Scan(
		&activeUpload, &activeJob, &activeWorkspace, &activePublication,
	); err != nil {
		return fmt.Errorf("read active attachment purge partials: %w", err)
	}
	if activeUpload || activeJob || activeWorkspace || activePublication {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func deleteAttachmentDeletionDependencies(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
	attachmentIDs []string,
) (uint64, uint64, error) {
	if len(attachmentIDs) == 0 {
		return 0, 0, nil
	}
	type deletionStep struct {
		name string
		sql  string
		part bool
	}
	steps := []deletionStep{
		{name: "content processor workspaces", sql: `
			delete from public.content_processor_workspaces as workspace
			using public.attachment_processor_jobs as job
			where workspace.processor_job_id = job.processor_job_id
			  and job.attachment_id = any($1::text[])`},
		{name: "attachment processor jobs", sql: `
			delete from public.attachment_processor_jobs
			where attachment_id = any($1::text[])`},
		{name: "attachment upload parts", part: true, sql: `
			delete from public.attachment_upload_parts as part
			using public.attachment_uploads as upload
			where part.upload_id = upload.upload_id
			  and upload.attachment_id = any($1::text[])`},
		{name: "attachment uploads", sql: `
			delete from public.attachment_uploads
			where attachment_id = any($1::text[])`},
		{name: "record revision attachment refs", sql: `
			delete from public.record_revision_attachments
			where attachment_id = any($1::text[])`},
	}
	var logicalRows, uploadPartRows uint64
	for _, step := range steps {
		result, err := tx.Exec(ctx, step.sql, attachmentIDs)
		if err != nil {
			return 0, 0, fmt.Errorf("delete %s for attachment purge: %w", step.name, err)
		}
		rows := result.RowsAffected()
		if rows < 0 {
			return 0, 0, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if step.part {
			if uint64(rows) > math.MaxUint64-uploadPartRows {
				return 0, 0, recorddeletion.ErrDeletionSafetyUnavailable
			}
			uploadPartRows += uint64(rows)
		} else {
			if uint64(rows) > math.MaxUint64-logicalRows {
				return 0, 0, recorddeletion.ErrDeletionSafetyUnavailable
			}
			logicalRows += uint64(rows)
		}
	}
	result, err := tx.Exec(ctx, `
		delete from public.record_attachments
		where project_id = $1
		  and attachment_id = any($3::text[])
		  and (record_id = $2
		    or draft_id in (
		      select draft.draft_id from public.record_drafts as draft where draft.record_id = $2
		    ))`, operation.Object.ProjectID, operation.Object.ObjectID, attachmentIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("delete logical attachments for attachment purge: %w", err)
	}
	if result.RowsAffected() != int64(len(attachmentIDs)) || uint64(len(attachmentIDs)) > math.MaxUint64-logicalRows {
		return 0, 0, recorddeletion.ErrDeletionSafetyUnavailable
	}
	logicalRows += uint64(len(attachmentIDs))
	return logicalRows, uploadPartRows, nil
}

func (repository *PostgresAttachmentRepository) claimAttachmentDeletionBlobs(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
	objects []attachmentDeletionObject,
) ([]blobGCStoredDeletion, error) {
	claims := make([]blobGCStoredDeletion, 0, len(objects))
	for _, candidate := range objects {
		if _, err := tx.Exec(ctx, `
			delete from public.blob_gc_pins
			where blob_key = $1 and blob_object_version = $2
			  and expires_at <= transaction_timestamp()`,
			candidate.object.Key, candidate.object.ObjectVersion,
		); err != nil {
			return nil, fmt.Errorf("delete expired attachment purge Blob pins: %w", err)
		}
		var referenced, pinned, uploadPart, publication bool
		if err := tx.QueryRow(ctx, attachmentPurgeObjectBlockersSQL,
			candidate.object.Key,
			candidate.object.ObjectVersion,
			candidate.object.SHA256[:],
		).Scan(&referenced, &pinned, &uploadPart, &publication); err != nil {
			return nil, fmt.Errorf("read attachment purge Blob blockers: %w", err)
		}
		if pinned || uploadPart || publication {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if referenced {
			continue
		}
		newDeletionID := repository.newBlobGCDeletionID
		if newDeletionID == nil {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
		deletionID, err := newDeletionID()
		if err != nil {
			return nil, fmt.Errorf("create attachment purge Blob GC deletion ID: %w", err)
		}
		request := attachmentDeletionBlobGCRequest(operation, candidate.object)
		claim, err := insertBlobGCDeletionClaim(ctx, tx, deletionID, request, attachments.BlobGCCandidate{
			Object: candidate.object, CreatedAt: candidate.createdAt,
		})
		if err != nil {
			return nil, err
		}
		deleted, err := tx.Exec(ctx, deleteAttachmentBlobMetadataForPurgeSQL,
			candidate.object.Key,
			candidate.object.ObjectVersion,
			candidate.object.SHA256[:],
			candidate.object.SizeBytes,
			candidate.object.BackendKind,
		)
		if err != nil {
			return nil, fmt.Errorf("delete attachment purge Blob metadata behind durable fence: %w", err)
		}
		if deleted.RowsAffected() != 1 {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
		claims = append(claims, blobGCStoredDeletion{claim: claim, state: "claimed"})
	}
	return claims, nil
}

func attachmentDeletionBlobGCRequest(
	operation recorddeletion.DeletionOperation,
	object attachments.BlobObject,
) attachments.BlobGCClaimRequest {
	return attachments.BlobGCClaimRequest{
		ProjectID:          operation.Object.ProjectID,
		BackendKind:        object.BackendKind,
		Mode:               attachments.BlobGCPurgeModePermanent,
		Object:             object,
		OwnerID:            operation.OperationID,
		OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration,
	}
}

func loadAttachmentDeletionClaims(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
) ([]blobGCStoredDeletion, error) {
	queryTx, ok := tx.(attachmentDeletionRowsTx)
	if !ok {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	rows, err := queryTx.Query(ctx, `
		select deletion.deletion_id, deletion.project_id, deletion.purge_mode,
		       deletion.blob_key, deletion.sha256_digest, deletion.object_version,
		       deletion.size_bytes, deletion.backend_kind, deletion.blob_created_at,
		       deletion.owner_id, deletion.owner_generation, deletion.attempt,
		       deletion.lease_expires_at, deletion.deletion_state, deletion.retry_at,
		       deletion.physical_delete_result, deletion.receipt_digest,
		       deletion.completed_at
		from public.blob_gc_deletions as deletion
		where deletion.project_id = $1
		  and deletion.purge_mode = 'permanent'
		  and deletion.owner_id = $2
		order by deletion.blob_key, deletion.object_version, deletion.deletion_id`,
		operation.Object.ProjectID, operation.OperationID)
	if err != nil {
		return nil, fmt.Errorf("load attachment purge Blob GC claims: %w", err)
	}
	defer rows.Close()
	claims := make([]blobGCStoredDeletion, 0)
	for rows.Next() {
		stored, err := scanBlobGCStoredDeletion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attachment purge Blob GC claim: %w", err)
		}
		claims = append(claims, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment purge Blob GC claims: %w", err)
	}
	return claims, nil
}

func (repository *PostgresAttachmentRepository) executeAttachmentDeletionClaims(
	ctx context.Context,
	command attachments.AttachmentDeletionCommand,
	claims []blobGCStoredDeletion,
) error {
	for _, stored := range claims {
		if stored.state == "completed" {
			continue
		}
		claim := stored.claim
		if stored.state == "retry_wait" {
			reclaimed, err := repository.ClaimBlobGC(ctx, attachmentDeletionBlobGCRequest(command.Operation, claim.Candidate.Object))
			if err != nil {
				return err
			}
			if reclaimed == nil {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			claim = *reclaimed
		}
		if err := repository.executeAttachmentDeletionClaim(ctx, command.Operation, claim); err != nil {
			return err
		}
	}
	return nil
}

func (repository *PostgresAttachmentRepository) executeAttachmentDeletionClaim(
	ctx context.Context,
	operation recorddeletion.DeletionOperation,
	claim attachments.BlobGCClaim,
) error {
	blobStore := repository.attachmentDeletionBlobStore(claim.Candidate.Object.BackendKind)
	if blobStore == nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	version := storeObjectVersionFromBlob(claim.Candidate.Object)
	receipt, err := blobStore.Delete(ctx, version)
	if err != nil {
		retryErr := repository.RetryBlobGC(ctx, attachments.BlobGCRetryRequest{
			Claim: claim, RetryAt: time.Now().UTC().Add(attachments.DefaultBlobGCRetryDelay),
		})
		if retryErr != nil {
			return errors.Join(fmt.Errorf("delete exact attachment purge Blob: %w", err), retryErr)
		}
		return fmt.Errorf("delete exact attachment purge Blob: %w", err)
	}
	if receipt.Version != version {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	completion := attachments.BlobGCCompletionRequest{Claim: claim, Receipt: receipt}
	if _, err := repository.CompleteBlobGC(ctx, completion); err == nil {
		return nil
	} else {
		resolved, resolveErr := repository.ResolveBlobGC(ctx, attachments.BlobGCResolveRequest(completion))
		if resolveErr == nil && resolved != nil {
			return nil
		}
		reclaimed, reclaimErr := repository.ClaimBlobGC(ctx, attachmentDeletionBlobGCRequest(operation, claim.Candidate.Object))
		if reclaimErr == nil && reclaimed != nil && *reclaimed != claim {
			return repository.executeAttachmentDeletionClaim(ctx, operation, *reclaimed)
		}
		if resolveErr != nil || reclaimErr != nil {
			return errors.Join(err, resolveErr, reclaimErr)
		}
		return err
	}
}

func (repository *PostgresAttachmentRepository) attachmentDeletionBlobStore(
	backend attachments.BackendKind,
) attachments.BlobStore {
	if repository == nil {
		return nil
	}
	repository.attachmentDeletionBlobMu.RLock()
	defer repository.attachmentDeletionBlobMu.RUnlock()
	return repository.attachmentDeletionBlobStores[backend]
}

func verifyAttachmentDeletionAbsent(
	ctx context.Context,
	tx attachmentTx,
	operation recorddeletion.DeletionOperation,
) error {
	var contentPresent bool
	if err := tx.QueryRow(ctx, attachmentPurgeContentPresentSQL,
		operation.Object.ProjectID, operation.Object.ObjectID).Scan(&contentPresent); err != nil {
		return fmt.Errorf("verify attachment purge SQL absence: %w", err)
	}
	if contentPresent {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func insertAttachmentDeletionReceiptCounts(
	ctx context.Context,
	tx attachmentTx,
	command attachments.AttachmentDeletionCommand,
	counts *attachmentDeletionReceiptCounts,
) error {
	for _, surface := range attachmentDeletionReceiptSurfaces {
		marker := attachmentDeletionReceiptMarker(command.Operation, surface)
		receiptDigest := attachmentDeletionSurfaceReceiptDigest(command, surface, marker, counts.counts[surface])
		var verifiedAt time.Time
		if err := tx.QueryRow(ctx, `
			insert into public.attachment_purge_receipts (
				operation_id, surface_kind, object_version_digest, adapter_name,
				removed_surface_digest, receipt_digest, removed_row_count,
				verified_absent_at
			) values ($1, $2, $3, 'record_attachments', $4, $5, $6, transaction_timestamp())
			returning verified_absent_at`,
			command.Operation.OperationID,
			surface,
			marker[:],
			command.SurfaceDigest[:],
			receiptDigest[:],
			int64(counts.counts[surface]),
		).Scan(&verifiedAt); err != nil {
			return fmt.Errorf("insert attachment purge %s receipt: %w", surface, err)
		}
		if verifiedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if counts.verifiedAt.IsZero() || verifiedAt.After(counts.verifiedAt) {
			counts.verifiedAt = verifiedAt
		}
	}
	return nil
}

func loadAttachmentDeletionReceiptCounts(
	ctx context.Context,
	tx attachmentTx,
	command attachments.AttachmentDeletionCommand,
) (attachmentDeletionReceiptCounts, bool, error) {
	var rowCount int64
	if err := tx.QueryRow(ctx, `
		select count(*)::bigint
		from public.attachment_purge_receipts
		where operation_id = $1 and adapter_name = 'record_attachments'`,
		command.Operation.OperationID).Scan(&rowCount); err != nil {
		return attachmentDeletionReceiptCounts{}, false, fmt.Errorf("count attachment purge receipts: %w", err)
	}
	if rowCount == 0 {
		return attachmentDeletionReceiptCounts{}, false, nil
	}
	if rowCount != int64(len(attachmentDeletionReceiptSurfaces)) {
		return attachmentDeletionReceiptCounts{}, false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	counts := attachmentDeletionReceiptCounts{counts: make(map[string]uint64, len(attachmentDeletionReceiptSurfaces))}
	for _, surface := range attachmentDeletionReceiptSurfaces {
		marker := attachmentDeletionReceiptMarker(command.Operation, surface)
		var storedSurfaceDigest, storedReceiptDigest []byte
		var removedRows int64
		var verifiedAt time.Time
		if err := tx.QueryRow(ctx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.attachment_purge_receipts
			where operation_id = $1 and adapter_name = 'record_attachments'
			  and surface_kind = $2 and object_version_digest = $3`,
			command.Operation.OperationID, surface, marker[:],
		).Scan(&storedSurfaceDigest, &storedReceiptDigest, &removedRows, &verifiedAt); err != nil {
			return attachmentDeletionReceiptCounts{}, false, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if len(storedSurfaceDigest) != sha256.Size || len(storedReceiptDigest) != sha256.Size ||
			removedRows < 0 || verifiedAt.IsZero() {
			return attachmentDeletionReceiptCounts{}, false, recorddeletion.ErrDeletionSafetyUnavailable
		}
		var surfaceDigest, receiptDigest [sha256.Size]byte
		copy(surfaceDigest[:], storedSurfaceDigest)
		copy(receiptDigest[:], storedReceiptDigest)
		count := uint64(removedRows)
		if surfaceDigest != command.SurfaceDigest ||
			receiptDigest != attachmentDeletionSurfaceReceiptDigest(command, surface, marker, count) {
			return attachmentDeletionReceiptCounts{}, false, recorddeletion.ErrDeletionSafetyUnavailable
		}
		counts.counts[surface] = count
		if counts.verifiedAt.IsZero() || verifiedAt.After(counts.verifiedAt) {
			counts.verifiedAt = verifiedAt
		}
	}
	return counts, true, nil
}

func attachmentDeletionAdapterReceipt(
	command attachments.AttachmentDeletionCommand,
	counts attachmentDeletionReceiptCounts,
) recorddeletion.AdapterPurgeReceipt {
	var removedRows uint64
	for _, surface := range attachmentDeletionReceiptSurfaces {
		count := counts.counts[surface]
		if count > math.MaxUint64-removedRows {
			return recorddeletion.AdapterPurgeReceipt{}
		}
		removedRows += count
	}
	return recorddeletion.AdapterPurgeReceipt{
		AdapterName:      recorddeletion.AdapterNameRecordAttachments,
		OperationID:      command.Operation.OperationID,
		SurfaceDigest:    command.SurfaceDigest,
		ReceiptDigest:    attachmentDeletionAggregateReceiptDigest(command, counts),
		RemovedRowCount:  removedRows,
		VerifiedAbsentAt: counts.verifiedAt,
	}
}

func attachmentDeletionReceiptMarker(
	operation recorddeletion.DeletionOperation,
	surface string,
) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionMarkerDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = appendAttachmentDeletionLengthPrefixed(payload, operation.OperationID)
	payload = appendAttachmentDeletionLengthPrefixed(payload, surface)
	return sha256.Sum256(payload)
}

func attachmentDeletionSurfaceReceiptDigest(
	command attachments.AttachmentDeletionCommand,
	surface string,
	marker [sha256.Size]byte,
	removedRows uint64,
) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionReceiptDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = appendAttachmentDeletionLengthPrefixed(payload, command.Operation.OperationID)
	payload = appendAttachmentDeletionLengthPrefixed(payload, surface)
	payload = append(payload, marker[:]...)
	payload = append(payload, command.SurfaceDigest[:]...)
	payload = appendAttachmentDeletionUint64(payload, removedRows)
	return sha256.Sum256(payload)
}

func attachmentDeletionAggregateReceiptDigest(
	command attachments.AttachmentDeletionCommand,
	counts attachmentDeletionReceiptCounts,
) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionReceiptDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = appendAttachmentDeletionLengthPrefixed(payload, command.Operation.OperationID)
	payload = append(payload, command.SurfaceDigest[:]...)
	for _, surface := range attachmentDeletionReceiptSurfaces {
		marker := attachmentDeletionReceiptMarker(command.Operation, surface)
		payload = appendAttachmentDeletionLengthPrefixed(payload, surface)
		payload = append(payload, marker[:]...)
		payload = appendAttachmentDeletionUint64(payload, counts.counts[surface])
	}
	return sha256.Sum256(payload)
}

func digestAttachmentDeletionHealth(surfaces []string) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionHealthDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = appendAttachmentDeletionUint64(payload, uint64(len(surfaces)))
	for _, surface := range surfaces {
		payload = appendAttachmentDeletionLengthPrefixed(payload, surface)
	}
	return sha256.Sum256(payload)
}

func digestAttachmentDeletionPreview(graphDigest [sha256.Size]byte, material []byte) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionPreviewDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = append(payload, graphDigest[:]...)
	payload = appendAttachmentDeletionUint64(payload, uint64(len(material)))
	payload = append(payload, material...)
	return sha256.Sum256(payload)
}

func digestAttachmentDeletionImpact(material []byte) [sha256.Size]byte {
	payload := appendAttachmentDeletionLengthPrefixed(nil, attachmentDeletionImpactDigestDomainV1)
	payload = appendAttachmentDeletionUint64(payload, 1)
	payload = appendAttachmentDeletionUint64(payload, uint64(len(material)))
	payload = append(payload, material...)
	return sha256.Sum256(payload)
}

func appendAttachmentDeletionLengthPrefixed(destination []byte, value string) []byte {
	destination = appendAttachmentDeletionUint64(destination, uint64(len(value)))
	return append(destination, value...)
}

func appendAttachmentDeletionUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}
