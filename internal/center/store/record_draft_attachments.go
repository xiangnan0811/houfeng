package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

// draftOwnedAttachment 是删除草稿前仍归该草稿所有、未随发布转给记录的附件。
type draftOwnedAttachment struct {
	attachmentID string
	state        attachments.UploadState
	logicalSize  int64
	upload       *draftOwnedUpload
}

type draftOwnedUpload struct {
	uploadID         string
	state            attachments.UploadState
	transport        attachments.TransportKind
	reservedSize     int64
	temporaryPending bool
}

// releaseDraftOwnedAttachments 在删除草稿行之前释放草稿名下的附件：record_attachments.draft_id
// 是不可延迟的 on delete restrict 外键，不先释放，丢弃、发布清理或过期清理都会被外键拦住。
//
// 调用方必须已持有草稿行锁，因此不会有新的上传预留挂到这份草稿上。锁顺序与处理器、
// 放弃上传过期一致：先锁配额账户，再锁附件、上传与处理任务行；登记 Blob 时先锁
// blob_objects 再删上传分片，与 Blob GC 的 blob_objects -> parts 顺序一致。
//
// 仍有工作在进行时返回 ErrDraftAttachmentsBusy 且不改动任何行：处理器持有未过期租约、
// 处理工作区尚未清除，或 S3 临时对象尚未删除——这些清理都依赖要删除的行。
//
// 配额：未完成的上传释放预留，可用附件释放逻辑字节，已拒绝或已过期的早已释放；
// 物理字节等 Blob GC 回收时再扣。只被上传分片引用的对象登记为 Blob，留给 GC 回收。
func releaseDraftOwnedAttachments(ctx context.Context, tx pgx.Tx, draftID string) error {
	projectID, owns, err := draftOwnedAttachmentProject(ctx, tx, draftID)
	if err != nil || !owns {
		return err
	}

	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, projectID)
	if err != nil {
		return err
	}
	owned, err := lockDraftOwnedAttachments(ctx, tx, projectID, draftID)
	if err != nil {
		return err
	}
	attachmentIDs := make([]string, 0, len(owned))
	uploadIDs := make([]string, 0, len(owned))
	for _, attachment := range owned {
		attachmentIDs = append(attachmentIDs, attachment.attachmentID)
		if attachment.upload != nil {
			uploadIDs = append(uploadIDs, attachment.upload.uploadID)
		}
	}
	if err := rejectBusyDraftAttachments(ctx, tx, owned, attachmentIDs, uploadIDs); err != nil {
		return err
	}

	next, err := releasedDraftAttachmentUsage(usage, owned)
	if err != nil {
		return err
	}
	registeredPhysical, err := registerDraftUploadPartBlobs(ctx, tx, owned, uploadIDs)
	if err != nil {
		return err
	}
	if next.PhysicalBytes > math.MaxInt64-registeredPhysical {
		return attachments.ErrQuotaOverflow
	}
	next.PhysicalBytes += registeredPhysical

	if err := deleteDraftOwnedAttachmentRows(ctx, tx, draftID, attachmentIDs, uploadIDs); err != nil {
		return err
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.ErrQuotaOverflow
	}
	return updateAttachmentQuotaAccount(ctx, tx, projectID, quotaVersion, next)
}

// draftOwnedAttachmentProject 返回草稿名下附件所属项目；草稿没有附件时 owns 为 false。
func draftOwnedAttachmentProject(ctx context.Context, tx pgx.Tx, draftID string) (string, bool, error) {
	var projectID string
	err := tx.QueryRow(ctx, `
		select project_id from public.record_attachments
		where draft_id = $1
		order by attachment_id
		limit 1`, draftID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find draft-owned attachments: %w", err)
	}
	return projectID, true, nil
}

func lockDraftOwnedAttachments(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	draftID string,
) ([]draftOwnedAttachment, error) {
	rows, err := tx.Query(ctx, `
		select attachment_id, project_id, attachment_state, logical_size_bytes
		from public.record_attachments
		where draft_id = $1
		order by attachment_id
		for update`, draftID)
	if err != nil {
		return nil, fmt.Errorf("lock draft-owned attachments: %w", err)
	}
	owned := make([]draftOwnedAttachment, 0)
	index := make(map[string]int)
	for rows.Next() {
		var attachment draftOwnedAttachment
		var rowProjectID string
		if err := rows.Scan(&attachment.attachmentID, &rowProjectID, &attachment.state, &attachment.logicalSize); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan draft-owned attachment: %w", err)
		}
		if rowProjectID != projectID || attachment.logicalSize <= 0 {
			rows.Close()
			return nil, attachments.ErrAttachmentConflict
		}
		index[attachment.attachmentID] = len(owned)
		owned = append(owned, attachment)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft-owned attachments: %w", err)
	}
	if len(owned) == 0 {
		return owned, nil
	}
	attachmentIDs := make([]string, 0, len(owned))
	for _, attachment := range owned {
		attachmentIDs = append(attachmentIDs, attachment.attachmentID)
	}

	uploads, err := tx.Query(ctx, `
		select upload_id, attachment_id, origin_draft_id, upload_state, transport_kind, reserved_size_bytes,
		       temporary_object_key is not null and temporary_object_deleted_at is null
		from public.attachment_uploads
		where attachment_id = any($1::text[])
		order by attachment_id
		for update`, attachmentIDs)
	if err != nil {
		return nil, fmt.Errorf("lock draft-owned attachment uploads: %w", err)
	}
	for uploads.Next() {
		var upload draftOwnedUpload
		var attachmentID, originDraftID string
		if err := uploads.Scan(
			&upload.uploadID, &attachmentID, &originDraftID, &upload.state, &upload.transport,
			&upload.reservedSize, &upload.temporaryPending,
		); err != nil {
			uploads.Close()
			return nil, fmt.Errorf("scan draft-owned attachment upload: %w", err)
		}
		position, ok := index[attachmentID]
		if !ok || originDraftID != draftID || upload.reservedSize <= 0 || owned[position].upload != nil ||
			upload.state != owned[position].state {
			uploads.Close()
			return nil, attachments.ErrAttachmentConflict
		}
		owned[position].upload = &upload
	}
	uploads.Close()
	if err := uploads.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft-owned attachment uploads: %w", err)
	}
	// 先锁已有工作区再锁处理任务，与工作区清理/回执重放的 workspace -> job 顺序一致。
	if _, err := tx.Exec(ctx, `
		select workspace.workspace_id
		from public.content_processor_workspaces as workspace
		join public.attachment_processor_jobs as job
		  on job.processor_job_id = workspace.processor_job_id
		where job.attachment_id = any($1::text[])
		order by workspace.workspace_id
		for update of workspace`, attachmentIDs); err != nil {
		return nil, fmt.Errorf("lock draft-owned attachment processor workspaces: %w", err)
	}
	// 处理任务行一并锁住：处理器以 SKIP LOCKED 认领，工作区登记要对任务行取外键锁，
	// 判定空闲之后不会再有新的认领或工作区。
	if _, err := tx.Exec(ctx, `
		select processor_job_id from public.attachment_processor_jobs
		where attachment_id = any($1::text[])
		order by attachment_id
		for update`, attachmentIDs); err != nil {
		return nil, fmt.Errorf("lock draft-owned attachment processor jobs: %w", err)
	}
	return owned, nil
}

func rejectBusyDraftAttachments(
	ctx context.Context,
	tx pgx.Tx,
	owned []draftOwnedAttachment,
	attachmentIDs []string,
	uploadIDs []string,
) error {
	for _, attachment := range owned {
		if attachment.upload != nil && attachment.upload.temporaryPending {
			return records.ErrDraftAttachmentsBusy
		}
	}
	// 未完成的 publication intent（PUT 或预览写入中途）也算忙：删掉上传行后，intent 过期清理
	// 无法再确认同一内容是否已被别的附件引用，可能误删去重共用的对象。
	var liveClaim, liveWorkspace, livePublication bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from public.attachment_processor_jobs
			where attachment_id = any($1::text[])
			  and processor_state = 'claimed'
			  and lease_expires_at > transaction_timestamp()
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
			  and ((publication.owner_kind = 'upload' and publication.owner_id = any($2::text[]))
			    or (publication.owner_kind = 'processor_preview' and exists (
				  select 1 from public.attachment_processor_jobs as job
				  where job.processor_job_id = publication.owner_id
				    and job.attachment_id = any($1::text[])
			    )))
		)`, attachmentIDs, uploadIDs).Scan(&liveClaim, &liveWorkspace, &livePublication); err != nil {
		return fmt.Errorf("check draft-owned attachment processing: %w", err)
	}
	if liveClaim || liveWorkspace || livePublication {
		return records.ErrDraftAttachmentsBusy
	}
	return nil
}

func releasedDraftAttachmentUsage(
	usage attachments.QuotaUsage,
	owned []draftOwnedAttachment,
) (attachments.QuotaUsage, error) {
	var reserved, logical int64
	for _, attachment := range owned {
		switch attachment.state {
		case attachments.UploadStateCreated, attachments.UploadStateUploading, attachments.UploadStateQuarantined:
			// 未完成的附件一定有上传行，否则无从得知要退回的预留。
			if attachment.upload == nil {
				return attachments.QuotaUsage{}, attachments.ErrAttachmentConflict
			}
			if reserved > math.MaxInt64-attachment.upload.reservedSize {
				return attachments.QuotaUsage{}, attachments.ErrQuotaOverflow
			}
			reserved += attachment.upload.reservedSize
		case attachments.UploadStateAvailable:
			if logical > math.MaxInt64-attachment.logicalSize {
				return attachments.QuotaUsage{}, attachments.ErrQuotaOverflow
			}
			logical += attachment.logicalSize
		case attachments.UploadStateRejected, attachments.UploadStateExpired:
		default:
			return attachments.QuotaUsage{}, attachments.ErrAttachmentConflict
		}
	}
	next, err := usage.ReleaseReservation(reserved)
	if err != nil {
		return attachments.QuotaUsage{}, err
	}
	if logical > next.LogicalBytes {
		return attachments.QuotaUsage{}, attachments.ErrInvalidQuotaUsage
	}
	next.LogicalBytes -= logical
	return next, nil
}

// registerDraftUploadPartBlobs 把只被上传分片引用的对象登记进 blob_objects，返回新增的物理字节。
// 已登记的对象（例如可用附件的原件）保持不变。即使没有分片也先锁 blob_objects，
// 保证随后删除分片时与 Blob GC 的加锁顺序一致。
func registerDraftUploadPartBlobs(
	ctx context.Context,
	tx pgx.Tx,
	owned []draftOwnedAttachment,
	uploadIDs []string,
) (int64, error) {
	if _, err := tx.Exec(ctx, `lock table public.blob_objects in row exclusive mode`); err != nil {
		return 0, fmt.Errorf("lock Blob metadata for draft attachment release: %w", err)
	}
	if len(uploadIDs) == 0 {
		return 0, nil
	}
	transports := make(map[string]attachments.TransportKind, len(owned))
	for _, attachment := range owned {
		if attachment.upload != nil {
			transports[attachment.upload.uploadID] = attachment.upload.transport
		}
	}
	rows, err := tx.Query(ctx, `
		select upload_id, size_bytes, sha256_digest, object_version
		from public.attachment_upload_parts
		where upload_id = any($1::text[])
		order by upload_id, part_number`, uploadIDs)
	if err != nil {
		return 0, fmt.Errorf("load draft attachment upload parts: %w", err)
	}
	objects := make([]attachments.BlobObject, 0)
	for rows.Next() {
		var uploadID string
		var digest []byte
		var object attachments.BlobObject
		if err := rows.Scan(&uploadID, &object.SizeBytes, &digest, &object.ObjectVersion); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan draft attachment upload part: %w", err)
		}
		if len(digest) != len(object.SHA256) {
			rows.Close()
			return 0, attachments.ErrAttachmentConflict
		}
		copy(object.SHA256[:], digest)
		object.Key = "sha256/" + hex.EncodeToString(digest)
		switch transports[uploadID] {
		case attachments.TransportKindLocal:
			object.BackendKind = attachments.BackendKindLocal
		case attachments.TransportKindS3:
			object.BackendKind = attachments.BackendKindS3
		default:
			rows.Close()
			return 0, attachments.ErrAttachmentConflict
		}
		if object.Validate() != nil {
			rows.Close()
			return 0, attachments.ErrAttachmentConflict
		}
		objects = append(objects, object)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate draft attachment upload parts: %w", err)
	}

	var physical int64
	for _, object := range objects {
		inserted, err := ensureAttachmentBlob(ctx, tx, object)
		if errors.Is(err, attachments.ErrBlobGCProtected) {
			// 同一对象正被 GC 删除：等删除完成后再释放，避免登记一个即将消失的对象。
			return 0, records.ErrDraftAttachmentsBusy
		}
		if err != nil {
			return 0, err
		}
		if inserted {
			if physical > math.MaxInt64-object.SizeBytes {
				return 0, attachments.ErrQuotaOverflow
			}
			physical += object.SizeBytes
		}
	}
	return physical, nil
}

func deleteDraftOwnedAttachmentRows(
	ctx context.Context,
	tx pgx.Tx,
	draftID string,
	attachmentIDs []string,
	uploadIDs []string,
) error {
	steps := []struct {
		name string
		sql  string
		args []any
	}{
		{name: "content processor workspaces", sql: `
			delete from public.content_processor_workspaces as workspace
			using public.attachment_processor_jobs as job
			where workspace.processor_job_id = job.processor_job_id
			  and job.attachment_id = any($1::text[])`, args: []any{attachmentIDs}},
		{name: "attachment processor jobs", sql: `
			delete from public.attachment_processor_jobs
			where attachment_id = any($1::text[])`, args: []any{attachmentIDs}},
		{name: "attachment upload parts", sql: `
			delete from public.attachment_upload_parts
			where upload_id = any($1::text[])`, args: []any{uploadIDs}},
		{name: "attachment uploads", sql: `
			delete from public.attachment_uploads
			where upload_id = any($1::text[])`, args: []any{uploadIDs}},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return fmt.Errorf("delete draft-owned %s: %w", step.name, err)
		}
	}
	deleted, err := tx.Exec(ctx, `
		delete from public.record_attachments
		where draft_id = $1 and attachment_id = any($2::text[])`, draftID, attachmentIDs)
	if err != nil {
		return fmt.Errorf("delete draft-owned attachments: %w", err)
	}
	if deleted.RowsAffected() != int64(len(attachmentIDs)) {
		return attachments.ErrAttachmentConflict
	}
	return nil
}
