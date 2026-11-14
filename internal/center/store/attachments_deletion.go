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

type attachmentDeletionReceiptCounts struct {
	counts     map[string]uint64
	verifiedAt time.Time
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
	health, err := attachmentDeletionHealthInTransaction(ctx, tx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("commit attachment deletion health read: %w", err)
	}
	return health, nil
}

func attachmentDeletionHealthInTransaction(
	ctx context.Context,
	tx attachmentTx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, attachments.ErrInvalidDeletionAdapter
	}
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
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.local_attachment_purge_prepare(bytea)", "public.local_attachment_purge_complete(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx, "public.local_attachment_purge_verify(bytea)", "public.local_attachment_purge_verify(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, digestAttachmentDeletionHealth(names))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
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
	preview, err := previewAttachmentDeletionInTransaction(ctx, tx, target)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("commit attachment deletion preview: %w", err)
	}
	return preview, nil
}

func previewAttachmentDeletionInTransaction(
	ctx context.Context,
	tx attachmentTx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, attachments.ErrInvalidDeletionAdapter
	}
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
		preview.SurvivingCopies = append(preview.SurvivingCopies, recorddeletion.KnownAdapterSurvivingCopy(
			recorddeletion.SurvivingCopyKindOtherRecord, uint64(survivingCopies),
		))
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return preview, nil
}

func (repository *PostgresAttachmentRepository) ConfigureAttachmentDeletionBlobStore(
	backend attachments.BackendKind,
	blobStore attachments.BlobGCDeleter,
) error {
	if repository == nil || nilAttachmentDeletionBlobStore(blobStore) ||
		(backend != attachments.BackendKindLocal && backend != attachments.BackendKindS3) {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionBlobMu.Lock()
	defer repository.attachmentDeletionBlobMu.Unlock()
	if repository.attachmentDeletionBlobStores == nil {
		repository.attachmentDeletionBlobStores = make(map[attachments.BackendKind]attachments.BlobGCDeleter)
	}
	if existing := repository.attachmentDeletionBlobStores[backend]; existing != nil && !sameAttachmentDeletionBlobStore(existing, blobStore) {
		return attachments.ErrInvalidDeletionAdapter
	}
	repository.attachmentDeletionBlobStores[backend] = blobStore
	return nil
}

func nilAttachmentDeletionBlobStore(blobStore attachments.BlobGCDeleter) bool {
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

func sameAttachmentDeletionBlobStore(left, right attachments.BlobGCDeleter) bool {
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
	if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
		return repository.purgeLocalRecordAttachments(ctx, command)
	}
	preparation, err := repository.prepareAttachmentDeletionV1(ctx, command)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if err := repository.executeAttachmentDeletionClaimsV1(ctx, command, preparation.claims); err != nil {
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
	if command.Operation.ContractVersion == recorddeletion.DeletionContractV2 {
		return repository.verifyLocalRecordAttachmentsPurge(ctx, command, receipt)
	}
	return repository.verifyAttachmentDeletionV1(ctx, command, receipt)
}

func (repository *PostgresAttachmentRepository) attachmentDeletionBlobStore(
	backend attachments.BackendKind,
) attachments.BlobGCDeleter {
	if repository == nil {
		return nil
	}
	repository.attachmentDeletionBlobMu.RLock()
	defer repository.attachmentDeletionBlobMu.RUnlock()
	return repository.attachmentDeletionBlobStores[backend]
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
