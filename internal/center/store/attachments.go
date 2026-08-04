package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
)

type attachmentTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresAttachmentRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (attachmentTx, error)
}

func NewPostgresAttachmentRepository(pool *pgxpool.Pool) *PostgresAttachmentRepository {
	return &PostgresAttachmentRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
			return pool.BeginTx(ctx, options)
		},
	}
}

type attachmentDraftRoute struct {
	projectID string
	recordID  *string
}

type attachmentUploadRoute struct {
	projectID     string
	uploadID      string
	attachmentID  string
	originDraftID string
	authorID      string
}

type lockedAttachmentUpload struct {
	attachmentUploadRoute
	recordID               *string
	state                  attachments.UploadState
	declaredSizeBytes      int64
	reservedSizeBytes      int64
	actualSizeBytes        *int64
	actualSHA256           []byte
	temporaryObjectKey     *string
	temporaryObjectVersion *string
	completionFingerprint  []byte
}

type lockedCopyAttachmentSource struct {
	displayName  string
	mediaType    string
	logicalBytes int64
	blobKey      string
	blobVersion  string
}

func (repository *PostgresAttachmentRepository) ReserveUpload(
	ctx context.Context,
	command attachments.ReserveUploadCommand,
) (attachments.UploadReservationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadReservationResult{}, attachments.ErrInvalidAttachmentCommand
	}

	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("begin attachment upload reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	route, err := loadAttachmentDraftRoute(ctx, tx, command)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	if route.recordID != nil {
		if err := lockAttachmentRecordOwner(ctx, tx, command.ProjectID, *route.recordID); err != nil {
			return attachments.UploadReservationResult{}, err
		}
	}
	lockedRoute, err := lockAttachmentDraftOwner(ctx, tx, command)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	if lockedRoute.projectID != route.projectID || !equalOptionalAttachmentRecordID(lockedRoute.recordID, route.recordID) {
		return attachments.UploadReservationResult{}, attachments.ErrAttachmentConflict
	}

	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, command.ProjectID)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	effectiveRecordBytes, err := readEffectiveAttachmentUsage(ctx, tx, command.ProjectID, command.DraftID, route.recordID)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	decision, err := attachments.EvaluateUploadReservationQuota(
		usage,
		effectiveRecordBytes,
		command.DeclaredSizeBytes,
		command.Limits,
	)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.UploadReservationResult{}, attachments.ErrQuotaOverflow
	}

	if _, err := tx.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, draft_id, origin_draft_id,
			attachment_state, display_name, media_type, logical_size_bytes, created_by
		) values ($1, $2, $3, $3, $4, $5, $6, $7, $8)`,
		command.AttachmentID,
		command.ProjectID,
		command.DraftID,
		attachments.UploadStateCreated,
		command.DisplayName,
		command.MediaType,
		command.DeclaredSizeBytes,
		command.AuthorID,
	); err != nil {
		return attachments.UploadReservationResult{}, mapAttachmentWriteError("insert logical attachment", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_uploads (
			upload_id, project_id, attachment_id, origin_draft_id, author_id,
			upload_state, transport_kind, declared_size_bytes,
			reserved_size_bytes, expires_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)`,
		command.UploadID,
		command.ProjectID,
		command.AttachmentID,
		command.DraftID,
		command.AuthorID,
		attachments.UploadStateCreated,
		command.TransportKind,
		command.DeclaredSizeBytes,
		command.ExpiresAt.UTC(),
	); err != nil {
		return attachments.UploadReservationResult{}, mapAttachmentWriteError("insert attachment upload reservation", err)
	}
	updated, err := tx.Exec(ctx, `
		update public.attachment_quota_accounts
		set reserved_bytes = $2, quota_version = $3, updated_at = now()
		where project_id = $1 and quota_version = $4`,
		command.ProjectID,
		decision.ProjectReservedBytes,
		quotaVersion+1,
		quotaVersion,
	)
	if err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("update attachment quota reservation: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.UploadReservationResult{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("commit attachment upload reservation: %w", err)
	}

	usage.ReservedBytes = decision.ProjectReservedBytes
	return attachments.UploadReservationResult{
		UploadID:     command.UploadID,
		AttachmentID: command.AttachmentID,
		State:        attachments.UploadStateCreated,
		Quota: attachments.QuotaSnapshot{
			Usage:                usage,
			EffectiveRecordBytes: decision.EffectiveRecordBytes,
			ProjectWarning:       decision.ProjectWarning,
		},
	}, nil
}

func (repository *PostgresAttachmentRepository) GetProjectQuotaSnapshot(
	ctx context.Context,
	command attachments.ProjectQuotaSnapshotCommand,
) (attachments.ProjectQuotaSnapshot, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.ProjectQuotaSnapshot{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return attachments.ProjectQuotaSnapshot{}, fmt.Errorf("begin attachment project quota read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var usage attachments.QuotaUsage
	err = tx.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes
		from public.attachment_quota_accounts
		where project_id = $1`, command.ProjectID).Scan(
		&usage.LogicalBytes,
		&usage.ReservedBytes,
		&usage.PhysicalBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		usage = attachments.QuotaUsage{}
	} else if err != nil {
		return attachments.ProjectQuotaSnapshot{}, fmt.Errorf("read attachment project quota: %w", err)
	}
	warning, err := usage.ProjectWarning(command.Limits)
	if err != nil {
		return attachments.ProjectQuotaSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProjectQuotaSnapshot{}, fmt.Errorf("commit attachment project quota read: %w", err)
	}
	return attachments.ProjectQuotaSnapshot{Usage: usage, ProjectWarning: warning}, nil
}

func (repository *PostgresAttachmentRepository) StartUpload(
	ctx context.Context,
	command attachments.UploadMutationCommand,
) (attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("begin attachment upload start: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	upload, err := lockAttachmentUpload(ctx, tx, command)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := attachments.ValidateUploadStateTransition(upload.state, attachments.UploadStateUploading); err != nil {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if err := updateAttachmentUploadStates(ctx, tx, upload, attachments.UploadStateUploading); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload start: %w", err)
	}
	return newAttachmentUploadMutationResult(upload, attachments.UploadStateUploading), nil
}

func (repository *PostgresAttachmentRepository) CompleteUploadContent(
	ctx context.Context,
	command attachments.CompleteUploadContentCommand,
) (attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("begin attachment upload completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	base := attachments.UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	upload, err := lockAttachmentUpload(ctx, tx, base)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if upload.state == attachments.UploadStateQuarantined || upload.state == attachments.UploadStateAvailable {
		if !attachmentUploadCompletionMatches(upload, command) {
			return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload completion replay: %w", err)
		}
		return newAttachmentUploadMutationResult(upload, upload.state), nil
	}
	if err := attachments.ValidateUploadStateTransition(upload.state, attachments.UploadStateQuarantined); err != nil ||
		command.ActualSizeBytes > upload.declaredSizeBytes {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	updated, err := tx.Exec(ctx, `
		update public.attachment_uploads
		set upload_state = $4, actual_size_bytes = $5, actual_sha256_digest = $6,
		    temporary_object_key = $7, temporary_object_version = $8,
		    completion_fingerprint = $9, completed_at = now(), updated_at = now()
		where project_id = $1 and upload_id = $2 and author_id = $3 and upload_state = $10`,
		command.ProjectID,
		command.UploadID,
		command.AuthorID,
		attachments.UploadStateQuarantined,
		command.ActualSizeBytes,
		command.ActualSHA256[:],
		command.TemporaryObjectKey,
		command.TemporaryObjectVersion,
		command.CompletionFingerprint[:],
		upload.state,
	)
	if err != nil {
		return attachments.UploadMutationResult{}, mapAttachmentWriteError("complete attachment upload content", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if err := updateLogicalAttachmentState(ctx, tx, upload, attachments.UploadStateQuarantined); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload completion: %w", err)
	}
	return newAttachmentUploadMutationResult(upload, attachments.UploadStateQuarantined), nil
}

func (repository *PostgresAttachmentRepository) AdmitUpload(
	ctx context.Context,
	command attachments.AdmitUploadCommand,
) (attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("begin attachment upload admission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	base := attachments.UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	route, recordID, err := lockAttachmentUploadOwners(ctx, tx, base)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, command.ProjectID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	upload, err := lockAttachmentUploadRow(ctx, tx, base, route, recordID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if upload.state == attachments.UploadStateAvailable {
		if !attachmentUploadBlobMatches(upload, command.Blob) {
			return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
		}
		if err := verifyAdmittedAttachmentReplay(ctx, tx, upload, command.Blob); err != nil {
			return attachments.UploadMutationResult{}, err
		}
		quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload, usage, command.Limits)
		if err != nil {
			return attachments.UploadMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload admission replay: %w", err)
		}
		result := newAttachmentUploadMutationResult(upload, attachments.UploadStateAvailable)
		result.Quota = quota
		return result, nil
	}
	if err := attachments.ValidateUploadStateTransition(upload.state, attachments.UploadStateAvailable); err != nil ||
		!attachmentUploadBlobMatches(upload, command.Blob) {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.UploadMutationResult{}, attachments.ErrQuotaOverflow
	}
	blobInserted, err := ensureAttachmentBlob(ctx, tx, command.Blob)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	physicalDelta := int64(0)
	if blobInserted {
		physicalDelta = command.Blob.SizeBytes
	}
	nextUsage, err := usage.SolidifyReservation(upload.reservedSizeBytes, command.Blob.SizeBytes, physicalDelta)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	updated, err := tx.Exec(ctx, `
		update public.record_attachments
		set attachment_state = $4, logical_size_bytes = $5,
		    blob_key = $6, blob_object_version = $7, updated_at = now()
		where project_id = $1 and attachment_id = $2 and draft_id = $3 and attachment_state = $8`,
		command.ProjectID,
		upload.attachmentID,
		upload.originDraftID,
		attachments.UploadStateAvailable,
		command.Blob.SizeBytes,
		command.Blob.Key,
		command.Blob.ObjectVersion,
		upload.state,
	)
	if err != nil {
		return attachments.UploadMutationResult{}, mapAttachmentWriteError("admit logical attachment", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if err := updateAttachmentUploadStateOnly(ctx, tx, upload, attachments.UploadStateAvailable); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := updateAttachmentQuotaAccount(ctx, tx, command.ProjectID, quotaVersion, nextUsage); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload, nextUsage, command.Limits)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload admission: %w", err)
	}
	result := newAttachmentUploadMutationResult(upload, attachments.UploadStateAvailable)
	result.Quota = quota
	return result, nil
}

func (repository *PostgresAttachmentRepository) FailUpload(
	ctx context.Context,
	command attachments.FailUploadCommand,
) (attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("begin attachment upload failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	base := attachments.UploadMutationCommand{ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID}
	route, recordID, err := lockAttachmentUploadOwners(ctx, tx, base)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, command.ProjectID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	upload, err := lockAttachmentUploadRow(ctx, tx, base, route, recordID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if upload.state == command.TargetState {
		quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload, usage, command.Limits)
		if err != nil {
			return attachments.UploadMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload failure replay: %w", err)
		}
		result := newAttachmentUploadMutationResult(upload, command.TargetState)
		result.Quota = quota
		return result, nil
	}
	if err := attachments.ValidateUploadStateTransition(upload.state, command.TargetState); err != nil {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	nextUsage, err := usage.ReleaseReservation(upload.reservedSizeBytes)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.UploadMutationResult{}, attachments.ErrQuotaOverflow
	}
	if err := updateAttachmentUploadStates(ctx, tx, upload, command.TargetState); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := updateAttachmentQuotaAccount(ctx, tx, command.ProjectID, quotaVersion, nextUsage); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload, nextUsage, command.Limits)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload failure: %w", err)
	}
	result := newAttachmentUploadMutationResult(upload, command.TargetState)
	result.Quota = quota
	return result, nil
}

func (repository *PostgresAttachmentRepository) CopyAttachment(
	ctx context.Context,
	command attachments.CopyAttachmentCommand,
) (attachments.CopyAttachmentResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.CopyAttachmentResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.CopyAttachmentResult{}, fmt.Errorf("begin attachment copy: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	firstRecordID, secondRecordID := command.SourceRecordID, command.TargetRecordID
	if secondRecordID < firstRecordID {
		firstRecordID, secondRecordID = secondRecordID, firstRecordID
	}
	for _, recordID := range []string{firstRecordID, secondRecordID} {
		if err := lockAttachmentRecordOwner(ctx, tx, command.ProjectID, recordID); err != nil {
			return attachments.CopyAttachmentResult{}, err
		}
	}

	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, command.ProjectID)
	if err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	source, err := lockCopyAttachmentSource(ctx, tx, command)
	if err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	effectiveRecordBytes, err := readEffectiveAttachmentUsage(
		ctx, tx, command.ProjectID, "", &command.TargetRecordID,
	)
	if err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	decision, err := attachments.EvaluateUploadReservationQuota(
		usage, effectiveRecordBytes, source.logicalBytes, command.Limits,
	)
	if err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	nextUsage, err := usage.SolidifyReservation(0, source.logicalBytes, 0)
	if err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	if quotaVersion == math.MaxInt64 {
		return attachments.CopyAttachmentResult{}, attachments.ErrQuotaOverflow
	}

	if _, err := tx.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, record_id, copied_from_attachment_id,
			attachment_state, display_name, media_type, logical_size_bytes,
			blob_key, blob_object_version, created_by
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		command.TargetAttachmentID,
		command.ProjectID,
		command.TargetRecordID,
		command.SourceAttachmentID,
		attachments.UploadStateAvailable,
		source.displayName,
		source.mediaType,
		source.logicalBytes,
		source.blobKey,
		source.blobVersion,
		command.ActorID,
	); err != nil {
		return attachments.CopyAttachmentResult{}, mapAttachmentWriteError("insert copied logical attachment", err)
	}
	if err := updateAttachmentQuotaAccount(ctx, tx, command.ProjectID, quotaVersion, nextUsage); err != nil {
		return attachments.CopyAttachmentResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.CopyAttachmentResult{}, fmt.Errorf("commit attachment copy: %w", err)
	}
	return attachments.CopyAttachmentResult{
		AttachmentID:           command.TargetAttachmentID,
		CopiedFromAttachmentID: command.SourceAttachmentID,
		Quota: attachments.QuotaSnapshot{
			Usage:                nextUsage,
			EffectiveRecordBytes: decision.EffectiveRecordBytes,
			ProjectWarning:       decision.ProjectWarning,
		},
	}, nil
}

func lockCopyAttachmentSource(
	ctx context.Context,
	tx attachmentTx,
	command attachments.CopyAttachmentCommand,
) (lockedCopyAttachmentSource, error) {
	var source lockedCopyAttachmentSource
	err := tx.QueryRow(ctx, `
		select attachment.display_name, attachment.media_type,
		       attachment.logical_size_bytes, attachment.blob_key,
		       attachment.blob_object_version
		from public.record_attachments attachment
		join public.blob_objects blob
		  on blob.blob_key = attachment.blob_key
		 and blob.object_version = attachment.blob_object_version
		where attachment.project_id = $1 and attachment.record_id = $2
		  and attachment.attachment_id = $3 and attachment.attachment_state = $4
		for share of attachment`,
		command.ProjectID,
		command.SourceRecordID,
		command.SourceAttachmentID,
		attachments.UploadStateAvailable,
	).Scan(
		&source.displayName,
		&source.mediaType,
		&source.logicalBytes,
		&source.blobKey,
		&source.blobVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCopyAttachmentSource{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return lockedCopyAttachmentSource{}, fmt.Errorf("lock copied attachment source: %w", err)
	}
	if source.logicalBytes <= 0 {
		return lockedCopyAttachmentSource{}, attachments.ErrInvalidQuotaUsage
	}
	return source, nil
}

func (repository *PostgresAttachmentRepository) CreateBlobGCPin(
	ctx context.Context,
	command attachments.CreateBlobGCPinCommand,
) (attachments.BlobProtection, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.BlobProtection{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("begin Blob GC pin creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inserted, err := tx.Exec(ctx, `
		insert into public.blob_gc_pins (
			pin_id, pin_owner_kind, pin_owner_id, blob_key,
			blob_object_version, expires_at
		) values ($1, $2, $3, $4, $5, $6)
		on conflict (pin_id) do nothing`,
		command.PinID,
		command.OwnerKind,
		command.OwnerID,
		command.BlobKey,
		command.BlobObjectVersion,
		command.ExpiresAt.UTC(),
	)
	if err != nil {
		return attachments.BlobProtection{}, mapAttachmentWriteError("create Blob GC pin", err)
	}
	switch inserted.RowsAffected() {
	case 0:
		if err := verifyBlobGCPinReplay(ctx, tx, command); err != nil {
			return attachments.BlobProtection{}, err
		}
	case 1:
	default:
		return attachments.BlobProtection{}, attachments.ErrAttachmentConflict
	}
	protection, err := readBlobProtection(ctx, tx, command.BlobKey, command.BlobObjectVersion)
	if err != nil {
		return attachments.BlobProtection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("commit Blob GC pin creation: %w", err)
	}
	return protection, nil
}

func (repository *PostgresAttachmentRepository) ReleaseBlobGCPin(
	ctx context.Context,
	command attachments.ReleaseBlobGCPinCommand,
) (attachments.BlobProtection, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.BlobProtection{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("begin Blob GC pin release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	deleted, err := tx.Exec(ctx, `
		delete from public.blob_gc_pins
		where pin_id = $1 and pin_owner_kind = $2 and pin_owner_id = $3
		  and blob_key = $4 and blob_object_version = $5`,
		command.PinID,
		command.OwnerKind,
		command.OwnerID,
		command.BlobKey,
		command.BlobObjectVersion,
	)
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("release Blob GC pin: %w", err)
	}
	if deleted.RowsAffected() > 1 {
		return attachments.BlobProtection{}, attachments.ErrAttachmentConflict
	}
	if deleted.RowsAffected() == 0 {
		var pinIDExists bool
		if err := tx.QueryRow(ctx, `
			select exists(select 1 from public.blob_gc_pins where pin_id = $1)`,
			command.PinID,
		).Scan(&pinIDExists); err != nil {
			return attachments.BlobProtection{}, fmt.Errorf("check Blob GC pin release replay: %w", err)
		}
		if pinIDExists {
			return attachments.BlobProtection{}, attachments.ErrAttachmentConflict
		}
	}
	protection, err := readBlobProtection(ctx, tx, command.BlobKey, command.BlobObjectVersion)
	if err != nil {
		return attachments.BlobProtection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("commit Blob GC pin release: %w", err)
	}
	return protection, nil
}

func (repository *PostgresAttachmentRepository) GetBlobProtection(
	ctx context.Context,
	command attachments.BlobProtectionCommand,
) (attachments.BlobProtection, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.BlobProtection{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("begin Blob protection read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	protection, err := readBlobProtection(ctx, tx, command.BlobKey, command.BlobObjectVersion)
	if err != nil {
		return attachments.BlobProtection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("commit Blob protection read: %w", err)
	}
	return protection, nil
}

func verifyBlobGCPinReplay(
	ctx context.Context,
	tx attachmentTx,
	command attachments.CreateBlobGCPinCommand,
) error {
	var ownerKind attachments.BlobGCPinOwnerKind
	var ownerID, blobKey, blobVersion string
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `
		select pin_owner_kind, pin_owner_id, blob_key, blob_object_version, expires_at
		from public.blob_gc_pins
		where pin_id = $1`, command.PinID).Scan(
		&ownerKind,
		&ownerID,
		&blobKey,
		&blobVersion,
		&expiresAt,
	); err != nil {
		return fmt.Errorf("read Blob GC pin replay: %w", err)
	}
	if ownerKind != command.OwnerKind || ownerID != command.OwnerID || blobKey != command.BlobKey ||
		blobVersion != command.BlobObjectVersion || !expiresAt.Equal(command.ExpiresAt.UTC()) {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func readBlobProtection(
	ctx context.Context,
	tx attachmentTx,
	blobKey string,
	blobVersion string,
) (attachments.BlobProtection, error) {
	var protection attachments.BlobProtection
	err := tx.QueryRow(ctx, `
		select blob.blob_key, blob.object_version,
		       (select count(*)::bigint
		        from public.record_attachments attachment
		        where attachment.blob_key = blob.blob_key
		          and attachment.blob_object_version = blob.object_version),
		       (select count(*)::bigint
		        from public.record_revision_attachments revision_ref
		        join public.record_attachments attachment
		          on attachment.attachment_id = revision_ref.attachment_id
		        where attachment.blob_key = blob.blob_key
		          and attachment.blob_object_version = blob.object_version),
		       (select count(*)::bigint
		        from public.blob_gc_pins pin
		        where pin.blob_key = blob.blob_key
		          and pin.blob_object_version = blob.object_version
		          and pin.expires_at > transaction_timestamp())
		from public.blob_objects blob
		where blob.blob_key = $1 and blob.object_version = $2`,
		blobKey,
		blobVersion,
	).Scan(
		&protection.BlobKey,
		&protection.BlobObjectVersion,
		&protection.LogicalAttachmentCount,
		&protection.RevisionReferenceCount,
		&protection.ActivePinCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.BlobProtection{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("read Blob protection: %w", err)
	}
	if protection.LogicalAttachmentCount < 0 || protection.RevisionReferenceCount < 0 ||
		protection.ActivePinCount < 0 {
		return attachments.BlobProtection{}, attachments.ErrInvalidQuotaUsage
	}
	protection.Protected = protection.LogicalAttachmentCount > 0 ||
		protection.RevisionReferenceCount > 0 || protection.ActivePinCount > 0
	return protection, nil
}

func loadAttachmentDraftRoute(
	ctx context.Context,
	tx attachmentTx,
	command attachments.ReserveUploadCommand,
) (attachmentDraftRoute, error) {
	var route attachmentDraftRoute
	err := tx.QueryRow(ctx, `
		select project_id, record_id
		from public.record_drafts
		where draft_id = $1 and author_id = $2`,
		command.DraftID,
		command.AuthorID,
	).Scan(&route.projectID, &route.recordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentDraftRoute{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachmentDraftRoute{}, fmt.Errorf("read attachment draft routing: %w", err)
	}
	if route.projectID != command.ProjectID {
		return attachmentDraftRoute{}, attachments.ErrAttachmentOwnerNotFound
	}
	return route, nil
}

func lockAttachmentRecordOwner(ctx context.Context, tx attachmentTx, projectID, recordID string) error {
	var lockedRecordID string
	err := tx.QueryRow(ctx, `
		select record_id
		from public.records
		where project_id = $1 and record_id = $2
		for update`, projectID, recordID).Scan(&lockedRecordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return fmt.Errorf("lock attachment record owner: %w", err)
	}
	if lockedRecordID != recordID {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func lockAttachmentDraftOwner(
	ctx context.Context,
	tx attachmentTx,
	command attachments.ReserveUploadCommand,
) (attachmentDraftRoute, error) {
	var route attachmentDraftRoute
	err := tx.QueryRow(ctx, `
		select project_id, record_id
		from public.record_drafts
		where draft_id = $1 and author_id = $2
		for update`, command.DraftID, command.AuthorID).Scan(&route.projectID, &route.recordID)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentDraftRoute{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachmentDraftRoute{}, fmt.Errorf("lock attachment draft owner: %w", err)
	}
	return route, nil
}

func lockAttachmentQuotaAccount(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (attachments.QuotaUsage, int64, error) {
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_quota_accounts (project_id)
		values ($1)
		on conflict (project_id) do nothing`, projectID); err != nil {
		return attachments.QuotaUsage{}, 0, fmt.Errorf("ensure attachment quota account: %w", err)
	}
	var usage attachments.QuotaUsage
	var quotaVersion int64
	if err := tx.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts
		where project_id = $1
		for update`, projectID).Scan(
		&usage.LogicalBytes,
		&usage.ReservedBytes,
		&usage.PhysicalBytes,
		&quotaVersion,
	); err != nil {
		return attachments.QuotaUsage{}, 0, fmt.Errorf("lock attachment quota account: %w", err)
	}
	if quotaVersion < 0 {
		return attachments.QuotaUsage{}, 0, attachments.ErrInvalidQuotaUsage
	}
	return usage, quotaVersion, nil
}

func readEffectiveAttachmentUsage(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
	draftID string,
	recordID *string,
) (int64, error) {
	var usage int64
	var err error
	if recordID == nil {
		err = tx.QueryRow(ctx, `
			select coalesce(sum(logical_size_bytes), 0)::bigint
			from public.record_attachments
			where project_id = $1 and draft_id = $2
			  and attachment_state not in ('rejected', 'expired')`, projectID, draftID).Scan(&usage)
	} else {
		err = tx.QueryRow(ctx, `
			select coalesce(sum(logical_size_bytes), 0)::bigint
			from public.record_attachments
			where project_id = $1 and (record_id = $2 or draft_id = $3)
			  and attachment_state not in ('rejected', 'expired')`,
			projectID, *recordID, draftID).Scan(&usage)
	}
	if err != nil {
		return 0, fmt.Errorf("read effective record attachment usage: %w", err)
	}
	if usage < 0 {
		return 0, attachments.ErrInvalidQuotaUsage
	}
	return usage, nil
}

func lockAttachmentUpload(
	ctx context.Context,
	tx attachmentTx,
	command attachments.UploadMutationCommand,
) (lockedAttachmentUpload, error) {
	route, recordID, err := lockAttachmentUploadOwners(ctx, tx, command)
	if err != nil {
		return lockedAttachmentUpload{}, err
	}
	return lockAttachmentUploadRow(ctx, tx, command, route, recordID)
}

func lockAttachmentUploadOwners(
	ctx context.Context,
	tx attachmentTx,
	command attachments.UploadMutationCommand,
) (attachmentUploadRoute, *string, error) {
	route, err := loadAttachmentUploadRoute(ctx, tx, command)
	if err != nil {
		return attachmentUploadRoute{}, nil, err
	}
	draftCommand := attachments.ReserveUploadCommand{
		ProjectID: route.projectID,
		DraftID:   route.originDraftID,
		AuthorID:  route.authorID,
	}
	draftRoute, err := loadAttachmentDraftRoute(ctx, tx, draftCommand)
	if err != nil {
		return attachmentUploadRoute{}, nil, err
	}
	if draftRoute.recordID != nil {
		if err := lockAttachmentRecordOwner(ctx, tx, route.projectID, *draftRoute.recordID); err != nil {
			return attachmentUploadRoute{}, nil, err
		}
	}
	lockedDraft, err := lockAttachmentDraftOwner(ctx, tx, draftCommand)
	if err != nil {
		return attachmentUploadRoute{}, nil, err
	}
	if lockedDraft.projectID != draftRoute.projectID || !equalOptionalAttachmentRecordID(lockedDraft.recordID, draftRoute.recordID) {
		return attachmentUploadRoute{}, nil, attachments.ErrAttachmentConflict
	}
	return route, lockedDraft.recordID, nil
}

func lockAttachmentUploadRow(
	ctx context.Context,
	tx attachmentTx,
	command attachments.UploadMutationCommand,
	route attachmentUploadRoute,
	recordID *string,
) (lockedAttachmentUpload, error) {
	var upload lockedAttachmentUpload
	var lockedRoute attachmentUploadRoute
	err := tx.QueryRow(ctx, `
		select project_id, upload_id, attachment_id, origin_draft_id, author_id,
		       upload_state, declared_size_bytes, reserved_size_bytes,
		       actual_size_bytes, actual_sha256_digest,
		       temporary_object_key, temporary_object_version,
		       completion_fingerprint
		from public.attachment_uploads
		where project_id = $1 and upload_id = $2 and author_id = $3
		for update`, command.ProjectID, command.UploadID, command.AuthorID).Scan(
		&lockedRoute.projectID,
		&lockedRoute.uploadID,
		&lockedRoute.attachmentID,
		&lockedRoute.originDraftID,
		&lockedRoute.authorID,
		&upload.state,
		&upload.declaredSizeBytes,
		&upload.reservedSizeBytes,
		&upload.actualSizeBytes,
		&upload.actualSHA256,
		&upload.temporaryObjectKey,
		&upload.temporaryObjectVersion,
		&upload.completionFingerprint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedAttachmentUpload{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return lockedAttachmentUpload{}, fmt.Errorf("lock attachment upload: %w", err)
	}
	if lockedRoute != route {
		return lockedAttachmentUpload{}, attachments.ErrAttachmentConflict
	}
	if upload.declaredSizeBytes <= 0 || upload.reservedSizeBytes <= 0 {
		return lockedAttachmentUpload{}, attachments.ErrInvalidQuotaUsage
	}
	upload.attachmentUploadRoute = lockedRoute
	upload.recordID = recordID
	return upload, nil
}

func attachmentUploadCompletionMatches(
	upload lockedAttachmentUpload,
	command attachments.CompleteUploadContentCommand,
) bool {
	return upload.actualSizeBytes != nil && *upload.actualSizeBytes == command.ActualSizeBytes &&
		len(upload.actualSHA256) == len(command.ActualSHA256) && bytes.Equal(upload.actualSHA256, command.ActualSHA256[:]) &&
		upload.temporaryObjectKey != nil && *upload.temporaryObjectKey == command.TemporaryObjectKey &&
		upload.temporaryObjectVersion != nil && *upload.temporaryObjectVersion == command.TemporaryObjectVersion &&
		len(upload.completionFingerprint) == len(command.CompletionFingerprint) &&
		bytes.Equal(upload.completionFingerprint, command.CompletionFingerprint[:])
}

func attachmentUploadBlobMatches(upload lockedAttachmentUpload, blob attachments.BlobObject) bool {
	return upload.actualSizeBytes != nil && *upload.actualSizeBytes == blob.SizeBytes &&
		len(upload.actualSHA256) == len(blob.SHA256) && bytes.Equal(upload.actualSHA256, blob.SHA256[:])
}

func verifyAdmittedAttachmentReplay(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	blob attachments.BlobObject,
) error {
	var matches bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from public.record_attachments attachment
			join public.blob_objects object
			  on object.blob_key = attachment.blob_key
			 and object.object_version = attachment.blob_object_version
			where attachment.project_id = $1 and attachment.attachment_id = $2
			  and attachment.attachment_state = $3
			  and attachment.logical_size_bytes = $4
			  and object.blob_key = $5 and object.object_version = $6
			  and object.sha256_digest = $7 and object.size_bytes = $4
			  and object.backend_kind = $8
		)`,
		upload.projectID,
		upload.attachmentID,
		attachments.UploadStateAvailable,
		blob.SizeBytes,
		blob.Key,
		blob.ObjectVersion,
		blob.SHA256[:],
		blob.BackendKind,
	).Scan(&matches); err != nil {
		return fmt.Errorf("verify admitted attachment replay: %w", err)
	}
	if !matches {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func buildAttachmentQuotaSnapshot(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	usage attachments.QuotaUsage,
	limits attachments.Limits,
) (attachments.QuotaSnapshot, error) {
	effectiveRecordBytes, err := readEffectiveAttachmentUsage(
		ctx, tx, upload.projectID, upload.originDraftID, upload.recordID,
	)
	if err != nil {
		return attachments.QuotaSnapshot{}, err
	}
	warning, err := usage.ProjectWarning(limits)
	if err != nil {
		return attachments.QuotaSnapshot{}, err
	}
	return attachments.QuotaSnapshot{
		Usage:                usage,
		EffectiveRecordBytes: effectiveRecordBytes,
		ProjectWarning:       warning,
	}, nil
}

func loadAttachmentUploadRoute(
	ctx context.Context,
	tx attachmentTx,
	command attachments.UploadMutationCommand,
) (attachmentUploadRoute, error) {
	var route attachmentUploadRoute
	err := tx.QueryRow(ctx, `
		select project_id, upload_id, attachment_id, origin_draft_id, author_id
		from public.attachment_uploads
		where project_id = $1 and upload_id = $2 and author_id = $3`,
		command.ProjectID, command.UploadID, command.AuthorID).Scan(
		&route.projectID,
		&route.uploadID,
		&route.attachmentID,
		&route.originDraftID,
		&route.authorID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentUploadRoute{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachmentUploadRoute{}, fmt.Errorf("read attachment upload routing: %w", err)
	}
	if route.projectID != command.ProjectID || route.uploadID != command.UploadID || route.authorID != command.AuthorID {
		return attachmentUploadRoute{}, attachments.ErrAttachmentOwnerNotFound
	}
	return route, nil
}

func updateAttachmentUploadStates(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	target attachments.UploadState,
) error {
	if err := updateAttachmentUploadStateOnly(ctx, tx, upload, target); err != nil {
		return err
	}
	return updateLogicalAttachmentState(ctx, tx, upload, target)
}

func updateAttachmentUploadStateOnly(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	target attachments.UploadState,
) error {
	updated, err := tx.Exec(ctx, `
		update public.attachment_uploads
		set upload_state = $4, updated_at = now()
		where project_id = $1 and upload_id = $2 and author_id = $3 and upload_state = $5`,
		upload.projectID,
		upload.uploadID,
		upload.authorID,
		target,
		upload.state,
	)
	if err != nil {
		return mapAttachmentWriteError("update attachment upload state", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func updateLogicalAttachmentState(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	target attachments.UploadState,
) error {
	updated, err := tx.Exec(ctx, `
		update public.record_attachments
		set attachment_state = $4, updated_at = now()
		where project_id = $1 and attachment_id = $2 and draft_id = $3 and attachment_state = $5`,
		upload.projectID,
		upload.attachmentID,
		upload.originDraftID,
		target,
		upload.state,
	)
	if err != nil {
		return mapAttachmentWriteError("update logical attachment state", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func ensureAttachmentBlob(ctx context.Context, tx attachmentTx, object attachments.BlobObject) (bool, error) {
	inserted, err := tx.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values ($1, $2, $3, $4, $5)
		on conflict (blob_key) do nothing`,
		object.Key,
		object.SHA256[:],
		object.ObjectVersion,
		object.SizeBytes,
		object.BackendKind,
	)
	if err != nil {
		return false, mapAttachmentWriteError("insert attachment Blob metadata", err)
	}
	if inserted.RowsAffected() == 1 {
		return true, nil
	}
	if inserted.RowsAffected() != 0 {
		return false, attachments.ErrAttachmentConflict
	}
	var digest []byte
	var objectVersion string
	var sizeBytes int64
	var backendKind attachments.BackendKind
	if err := tx.QueryRow(ctx, `
		select sha256_digest, object_version, size_bytes, backend_kind
		from public.blob_objects
		where blob_key = $1`, object.Key).Scan(&digest, &objectVersion, &sizeBytes, &backendKind); err != nil {
		return false, fmt.Errorf("read existing attachment Blob metadata: %w", err)
	}
	if !bytes.Equal(digest, object.SHA256[:]) || objectVersion != object.ObjectVersion ||
		sizeBytes != object.SizeBytes || backendKind != object.BackendKind {
		return false, attachments.ErrAttachmentConflict
	}
	return false, nil
}

func updateAttachmentQuotaAccount(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
	quotaVersion int64,
	usage attachments.QuotaUsage,
) error {
	updated, err := tx.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = $2, reserved_bytes = $3, physical_bytes = $4,
		    quota_version = $5, updated_at = now()
		where project_id = $1 and quota_version = $6`,
		projectID,
		usage.LogicalBytes,
		usage.ReservedBytes,
		usage.PhysicalBytes,
		quotaVersion+1,
		quotaVersion,
	)
	if err != nil {
		return fmt.Errorf("update attachment quota account: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func newAttachmentUploadMutationResult(
	upload lockedAttachmentUpload,
	state attachments.UploadState,
) attachments.UploadMutationResult {
	return attachments.UploadMutationResult{
		UploadID:     upload.uploadID,
		AttachmentID: upload.attachmentID,
		State:        state,
	}
}

func equalOptionalAttachmentRecordID(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mapAttachmentWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23503" || postgresError.Code == "23505" || postgresError.Code == "23514") {
		return fmt.Errorf("%w: %s", attachments.ErrAttachmentConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
