package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
)

type attachmentTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresAttachmentRepository struct {
	beginTx                      func(context.Context, pgx.TxOptions) (attachmentTx, error)
	newBlobGCDeletionID          func() (string, error)
	newBlobPublicationID         func() (string, error)
	attachmentDeletionBlobMu     sync.RWMutex
	attachmentDeletionBlobStores map[attachments.BackendKind]attachments.BlobStore
}

func NewPostgresAttachmentRepository(pool *pgxpool.Pool) *PostgresAttachmentRepository {
	return &PostgresAttachmentRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
			return pool.BeginTx(ctx, options)
		},
		newBlobGCDeletionID: func() (string, error) {
			return ids.New("bgd")
		},
		newBlobPublicationID: func() (string, error) {
			return ids.New("bpi")
		},
		attachmentDeletionBlobStores: make(map[attachments.BackendKind]attachments.BlobStore),
	}
}

// GetAttachmentForDownload reads only the immutable attachment route and the
// exact versioned Blob identities needed by the content service. It never
// returns a mutable upload or a caller-supplied object key.
func (repository *PostgresAttachmentRepository) GetAttachmentForDownload(
	ctx context.Context,
	lookup attachments.ContentLookup,
) (attachments.AttachmentContent, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || lookup.Validate() != nil {
		return attachments.AttachmentContent{}, attachments.ErrInvalidDownloadRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return attachments.AttachmentContent{}, fmt.Errorf("begin attachment download read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	content, err := readAttachmentContentForDownload(ctx, tx, lookup)
	if err != nil {
		return attachments.AttachmentContent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.AttachmentContent{}, fmt.Errorf("commit attachment download read: %w", err)
	}
	return content, nil
}

// AssertAttachmentContent is the per-write logical ownership fence used by a
// content stream. The record-platform serving lease supplies the separate
// record authorization/deletion epoch fence; this assertion closes the gap
// for draft-to-record transfers and exact Blob identity drift.
func (repository *PostgresAttachmentRepository) AssertAttachmentContent(
	ctx context.Context,
	assertion attachments.ContentAssertion,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || assertion.Validate() != nil {
		return attachments.ErrInvalidDownloadRequest
	}
	content, err := repository.GetAttachmentForDownload(ctx, attachments.ContentLookup{
		ProjectID: assertion.ProjectID, AttachmentID: assertion.AttachmentID,
	})
	if err != nil {
		return err
	}
	if content.State != attachments.UploadStateAvailable || content.ProjectID != assertion.ProjectID ||
		content.AttachmentID != assertion.AttachmentID || content.DraftID != assertion.DraftID ||
		content.RecordID != assertion.RecordID || content.AuthorID != assertion.AuthorID {
		return attachments.ErrAttachmentConflict
	}
	selected := content.Original
	if assertion.Variant == attachments.ContentVariantPreview {
		if content.Preview == nil {
			return attachments.ErrContentVariantUnavailable
		}
		selected = content.Preview.Object
	}
	if selected != assertion.Object {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func readAttachmentContentForDownload(
	ctx context.Context,
	tx attachmentTx,
	lookup attachments.ContentLookup,
) (attachments.AttachmentContent, error) {
	var projectID, attachmentID, draftID, recordID, authorID string
	var state string
	var displayName, mediaType string
	var logicalSize int64
	var originalKey, originalVersion *string
	var originalDigest []byte
	var originalSize *int64
	var previewKey, previewVersion *string
	var previewDigest []byte
	var previewSize *int64
	var previewMediaType *string
	err := tx.QueryRow(ctx, `
		select attachment.project_id,
		       attachment.attachment_id,
		       coalesce(attachment.draft_id, ''),
		       coalesce(attachment.record_id, ''),
		       attachment.created_by,
		       attachment.attachment_state,
		       attachment.display_name,
		       attachment.media_type,
		       attachment.logical_size_bytes,
		       original.blob_key,
		       original.object_version,
		       original.sha256_digest,
		       original.size_bytes,
		       preview.blob_key,
		       preview.object_version,
		       preview.sha256_digest,
		       preview.size_bytes,
		       attachment.preview_media_type
		from public.record_attachments attachment
		left join public.blob_objects original
		  on original.blob_key = attachment.blob_key
		 and original.object_version = attachment.blob_object_version
		left join public.blob_objects preview
		  on preview.blob_key = attachment.preview_blob_key
		 and preview.object_version = attachment.preview_blob_object_version
		where attachment.project_id = $1
		  and attachment.attachment_id = $2`,
		lookup.ProjectID, lookup.AttachmentID,
	).Scan(
		&projectID, &attachmentID, &draftID, &recordID, &authorID, &state,
		&displayName, &mediaType, &logicalSize,
		&originalKey, &originalVersion, &originalDigest, &originalSize,
		&previewKey, &previewVersion, &previewDigest, &previewSize, &previewMediaType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.AttachmentContent{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachments.AttachmentContent{}, fmt.Errorf("read attachment download row: %w", err)
	}
	content := attachments.AttachmentContent{
		ProjectID: projectID, AttachmentID: attachmentID, DraftID: draftID, RecordID: recordID,
		AuthorID: authorID, State: attachments.UploadState(state), DisplayName: displayName,
		MediaType: mediaType, LogicalSizeBytes: logicalSize,
	}
	if originalKey != nil || originalVersion != nil || len(originalDigest) != 0 || originalSize != nil {
		if originalKey == nil || originalVersion == nil || len(originalDigest) != sha256.Size || originalSize == nil {
			return attachments.AttachmentContent{}, attachments.ErrInvalidDownloadContent
		}
		var digest [sha256.Size]byte
		copy(digest[:], originalDigest)
		content.Original = attachments.ObjectVersion{
			Key: *originalKey, VersionID: *originalVersion, SHA256: digest, SizeBytes: *originalSize,
		}
	}
	if previewKey != nil || previewVersion != nil || len(previewDigest) != 0 || previewSize != nil || previewMediaType != nil {
		if previewKey == nil || previewVersion == nil || len(previewDigest) != sha256.Size || previewSize == nil || previewMediaType == nil {
			return attachments.AttachmentContent{}, attachments.ErrInvalidDownloadContent
		}
		var digest [sha256.Size]byte
		copy(digest[:], previewDigest)
		content.Preview = &attachments.ManagedPreviewContent{
			Object: attachments.ObjectVersion{
				Key: *previewKey, VersionID: *previewVersion, SHA256: digest, SizeBytes: *previewSize,
			},
			MediaType: *previewMediaType,
		}
	}
	if err := content.Validate(); err != nil {
		return attachments.AttachmentContent{}, err
	}
	return content, nil
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

type attachmentProcessorJobSnapshot struct {
	processorJobID       string
	uploadID             string
	attachmentID         string
	state                attachments.ProcessorState
	profile              attachments.ProcessorProfile
	attempt              int64
	maxAttempts          int64
	ownerID              string
	ownerGeneration      int64
	leaseExpiresAt       *time.Time
	retryAt              *time.Time
	resultCode           *attachments.ProcessorResultCode
	resultDigest         []byte
	resultOwnerID        string
	resultLeaseExpiresAt *time.Time
	expiresAt            time.Time
	databaseNow          time.Time
}

type attachmentProcessorUploadSnapshot struct {
	upload               lockedAttachmentUpload
	attachmentState      attachments.UploadState
	source               attachments.BlobObject
	blobKey              *string
	blobObjectVersion    *string
	previewBlobKey       *string
	previewObjectVersion *string
	previewMediaType     *string
	previewSizeBytes     *int64
}

type attachmentProcessorExpiryOutcome struct {
	code           attachments.ProcessorResultCode
	digest         [sha256.Size]byte
	resultOwnerID  string
	resultLeaseEnd time.Time
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
	replay, err := attachmentUploadReservationExists(ctx, tx, command.UploadID)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	if replay {
		_ = tx.Rollback(ctx)
		return repository.readUploadReservationReplay(ctx, command)
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
		if isAttachmentConstraintConflict(err) {
			_ = tx.Rollback(ctx)
			return repository.readUploadReservationReplay(ctx, command)
		}
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
		if isAttachmentConstraintConflict(err) {
			_ = tx.Rollback(ctx)
			return repository.readUploadReservationReplay(ctx, command)
		}
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
	preparation, err := repository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
	})
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	return attachments.UploadMutationResult{
		UploadID: preparation.UploadID, AttachmentID: preparation.AttachmentID, State: preparation.State,
	}, nil
}

func (repository *PostgresAttachmentRepository) PrepareUpload(
	ctx context.Context,
	command attachments.PrepareUploadCommand,
) (attachments.UploadPreparation, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadPreparation{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadPreparation{}, fmt.Errorf("begin attachment upload preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, attachments.UploadMutationCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
	})
	if err != nil {
		return attachments.UploadPreparation{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadPreparation{}, err
	}
	if attachmentState != upload.state {
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	expired, err := attachmentUploadExpired(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadPreparation{}, err
	}
	if expired {
		return attachments.UploadPreparation{}, attachments.ErrUploadExpired
	}
	if upload.state == attachments.UploadStateUploading {
		if err := validatePreparedAttachmentUpload(preparation, command); err != nil {
			return attachments.UploadPreparation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.UploadPreparation{}, fmt.Errorf("commit attachment upload preparation replay: %w", err)
		}
		return preparation, nil
	}
	if upload.state != attachments.UploadStateCreated {
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	switch preparation.TransportKind {
	case attachments.TransportKindLocal:
		if command.CandidateTemporaryObjectKey != "" || preparation.TemporaryObjectKey != "" ||
			preparation.TemporaryObjectVersion != "" {
			return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
		}
		if err := updateAttachmentUploadStateOnly(ctx, tx, upload, attachments.UploadStateUploading); err != nil {
			return attachments.UploadPreparation{}, err
		}
	case attachments.TransportKindS3:
		if command.CandidateTemporaryObjectKey == "" || preparation.TemporaryObjectVersion != "" {
			return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
		}
		key := preparation.TemporaryObjectKey
		if key == "" {
			key = command.CandidateTemporaryObjectKey
		}
		updated, err := tx.Exec(ctx, `
			update public.attachment_uploads
			set upload_state = $4, temporary_object_key = $5, updated_at = now()
			where project_id = $1 and upload_id = $2 and author_id = $3
			  and upload_state = $6
			  and (temporary_object_key is null or temporary_object_key = $5)
			  and temporary_object_version is null`,
			upload.projectID, upload.uploadID, upload.authorID,
			attachments.UploadStateUploading, key, attachments.UploadStateCreated,
		)
		if err != nil {
			return attachments.UploadPreparation{}, mapAttachmentWriteError("prepare S3 attachment upload", err)
		}
		if updated.RowsAffected() != 1 {
			return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
		}
		preparation.TemporaryObjectKey = key
	default:
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	if err := updateLogicalAttachmentState(ctx, tx, upload, attachments.UploadStateUploading); err != nil {
		return attachments.UploadPreparation{}, err
	}
	preparation.State = attachments.UploadStateUploading
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadPreparation{}, fmt.Errorf("commit attachment upload preparation: %w", err)
	}
	return preparation, nil
}

func (repository *PostgresAttachmentRepository) RecordTemporaryObjectVersion(
	ctx context.Context,
	command attachments.RecordTemporaryObjectVersionCommand,
) (attachments.UploadPreparation, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadPreparation{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadPreparation{}, fmt.Errorf("begin attachment temporary version record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, attachments.UploadMutationCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
	})
	if err != nil {
		return attachments.UploadPreparation{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadPreparation{}, err
	}
	if attachmentState != upload.state || preparation.TransportKind != attachments.TransportKindS3 ||
		(upload.state != attachments.UploadStateUploading && upload.state != attachments.UploadStateExpired) ||
		preparation.TemporaryObjectKey != command.TemporaryObjectKey {
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	if preparation.TemporaryObjectVersion == "" {
		updated, err := tx.Exec(ctx, `
			update public.attachment_uploads
			set temporary_object_version = $5, updated_at = now()
			where project_id = $1 and upload_id = $2 and author_id = $3
			  and temporary_object_key = $4 and temporary_object_version is null`,
			command.ProjectID, command.UploadID, command.AuthorID,
			command.TemporaryObjectKey, command.TemporaryObjectVersion,
		)
		if err != nil {
			return attachments.UploadPreparation{}, mapAttachmentWriteError("record attachment temporary object version", err)
		}
		if updated.RowsAffected() != 1 {
			return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
		}
		preparation.TemporaryObjectVersion = command.TemporaryObjectVersion
	} else if preparation.TemporaryObjectVersion != command.TemporaryObjectVersion {
		return attachments.UploadPreparation{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadPreparation{}, fmt.Errorf("commit attachment temporary version record: %w", err)
	}
	return preparation, nil
}

func (repository *PostgresAttachmentRepository) ClaimTemporaryObjectCleanup(
	ctx context.Context,
	input attachments.TemporaryObjectCleanupClaimInput,
) (*attachments.TemporaryObjectCleanupCandidate, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return nil, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin attachment temporary object cleanup claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var candidate attachments.TemporaryObjectCleanupCandidate
	err = tx.QueryRow(ctx, `
		with candidate as (
			select upload_id
			from public.attachment_uploads
			where project_id = $1
			  and transport_kind = 's3'
			  and temporary_object_key is not null
			  and temporary_object_deleted_at is null
			  and (temporary_object_cleanup_retry_at is null
			       or temporary_object_cleanup_retry_at <= transaction_timestamp())
			  and (upload_state in ('quarantined', 'available', 'rejected', 'expired')
			       or expires_at <= transaction_timestamp())
			order by coalesce(temporary_object_cleanup_retry_at, created_at), expires_at, upload_id
			for update skip locked
			limit 1
		)
		update public.attachment_uploads as upload
		set temporary_object_cleanup_retry_at = transaction_timestamp()
			+ ($2 * interval '1 microsecond'),
			updated_at = transaction_timestamp()
		from candidate
		where upload.project_id = $1 and upload.upload_id = candidate.upload_id
		returning upload.project_id, upload.upload_id, upload.author_id,
		          upload.temporary_object_key, coalesce(upload.temporary_object_version, ''),
		          upload.upload_state, upload.expires_at`,
		input.ProjectID, input.RetryDelay.Microseconds(),
	).Scan(
		&candidate.ProjectID, &candidate.UploadID, &candidate.AuthorID,
		&candidate.TemporaryObjectKey, &candidate.TemporaryObjectVersion,
		&candidate.State, &candidate.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty attachment temporary object cleanup claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan attachment temporary object cleanup claim: %w", err)
	}
	if candidate.Validate() != nil {
		return nil, attachments.ErrTemporaryObjectReconciliationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit attachment temporary object cleanup claim: %w", err)
	}
	return &candidate, nil
}

func (repository *PostgresAttachmentRepository) MarkTemporaryObjectCleaned(
	ctx context.Context,
	candidate attachments.TemporaryObjectCleanupCandidate,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || candidate.Validate() != nil ||
		candidate.TemporaryObjectVersion == "" {
		return attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin attachment temporary object cleanup mark: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	updated, err := tx.Exec(ctx, `
		update public.attachment_uploads
		set temporary_object_deleted_at = transaction_timestamp(),
		    temporary_object_cleanup_retry_at = null,
		    updated_at = transaction_timestamp()
		where project_id = $1 and upload_id = $2 and author_id = $3
		  and transport_kind = 's3'
		  and temporary_object_key = $4
		  and temporary_object_version = $5
		  and temporary_object_deleted_at is null`,
		candidate.ProjectID, candidate.UploadID, candidate.AuthorID,
		candidate.TemporaryObjectKey, candidate.TemporaryObjectVersion,
	)
	if err != nil {
		return mapAttachmentWriteError("mark attachment temporary object cleanup", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrTemporaryObjectReconciliationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment temporary object cleanup mark: %w", err)
	}
	return nil
}

func (repository *PostgresAttachmentRepository) RecordUploadedContent(
	ctx context.Context,
	command attachments.RecordUploadedContentCommand,
) (attachments.UploadedContent, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadedContent{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadedContent{}, fmt.Errorf("begin attachment uploaded content record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, attachments.UploadMutationCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
	})
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	expired, err := attachmentUploadExpired(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	if expired {
		return attachments.UploadedContent{}, attachments.ErrUploadExpired
	}
	if attachmentState != upload.state || upload.state != attachments.UploadStateUploading ||
		!attachmentUploadTemporaryKeyMatches(preparation, command.TemporaryObjectKey) {
		return attachments.UploadedContent{}, attachments.ErrAttachmentConflict
	}
	if err := consumeBlobPublicationForUploadPart(
		ctx, tx, upload.uploadID, command.Object, command.PublicationIntent,
	); err != nil {
		return attachments.UploadedContent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadedContent{}, fmt.Errorf("commit attachment uploaded content record: %w", err)
	}
	return newAttachmentUploadedContent(preparation, command.Object), nil
}

func (repository *PostgresAttachmentRepository) GetUploadedContent(
	ctx context.Context,
	command attachments.UploadMutationCommand,
) (attachments.UploadedContent, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadedContent{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadedContent{}, fmt.Errorf("begin attachment uploaded content read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, command)
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	if attachmentState != upload.state || (upload.state != attachments.UploadStateUploading &&
		upload.state != attachments.UploadStateQuarantined && upload.state != attachments.UploadStateAvailable) {
		return attachments.UploadedContent{}, attachments.ErrAttachmentConflict
	}
	object, err := readAttachmentUploadPart(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadedContent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadedContent{}, fmt.Errorf("commit attachment uploaded content read: %w", err)
	}
	return newAttachmentUploadedContent(preparation, object), nil
}

func (repository *PostgresAttachmentRepository) GetUploadCompletionPreparation(
	ctx context.Context,
	command attachments.UploadMutationCommand,
) (attachments.UploadCompletionPreparation, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadCompletionPreparation{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadCompletionPreparation{}, fmt.Errorf("begin attachment upload completion preparation read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, command)
	if err != nil {
		return attachments.UploadCompletionPreparation{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadCompletionPreparation{}, err
	}
	if attachmentState != upload.state || (upload.state != attachments.UploadStateUploading &&
		upload.state != attachments.UploadStateQuarantined && upload.state != attachments.UploadStateAvailable) {
		return attachments.UploadCompletionPreparation{}, attachments.ErrAttachmentConflict
	}
	object, hasObject, err := readOptionalAttachmentUploadPart(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadCompletionPreparation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadCompletionPreparation{}, fmt.Errorf("commit attachment upload completion preparation read: %w", err)
	}
	return attachments.UploadCompletionPreparation{
		UploadPreparation: preparation, Object: object, HasObject: hasObject,
	}, nil
}

func (repository *PostgresAttachmentRepository) CompleteUploadAndEnqueue(
	ctx context.Context,
	command attachments.CompleteUploadAndEnqueueCommand,
) (attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || command.Validate() != nil {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("begin attachment upload completion and enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	upload, err := lockAttachmentUpload(ctx, tx, attachments.UploadMutationCommand{
		ProjectID: command.ProjectID, UploadID: command.UploadID, AuthorID: command.AuthorID,
	})
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	preparation, attachmentState, err := readAttachmentUploadPreparation(ctx, tx, upload)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	object, hasObject, err := readOptionalAttachmentUploadPart(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if attachmentState != upload.state ||
		preparation.TemporaryObjectKey != command.TemporaryObjectKey ||
		preparation.TemporaryObjectVersion != command.TemporaryObjectVersion {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if upload.state == attachments.UploadStateQuarantined || upload.state == attachments.UploadStateAvailable {
		if !hasObject || object != command.Object || !attachmentUploadCompletionAndPartMatches(upload, command) {
			return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
		}
		if err := verifyAttachmentProcessorJobReplay(ctx, tx, upload, command); err != nil {
			return attachments.UploadMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload completion enqueue replay: %w", err)
		}
		return newAttachmentUploadMutationResult(upload, upload.state), nil
	}
	if upload.state != attachments.UploadStateUploading {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if !hasObject && (preparation.TransportKind != attachments.TransportKindS3 ||
		preparation.TemporaryObjectVersion == "") {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if hasObject && object != command.Object {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	expired, err := attachmentUploadExpired(ctx, tx, upload.uploadID)
	if err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if expired {
		return attachments.UploadMutationResult{}, attachments.ErrUploadExpired
	}
	if command.ActualSizeBytes != upload.declaredSizeBytes || command.ActualSHA256 != command.Object.SHA256 {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if command.PublicationIntent != (attachments.BlobPublicationIntent{}) {
		if err := consumeBlobPublicationForUploadPart(
			ctx, tx, upload.uploadID, command.Object, command.PublicationIntent,
		); err != nil {
			return attachments.UploadMutationResult{}, err
		}
	} else if !hasObject {
		return attachments.UploadMutationResult{}, attachments.ErrInvalidAttachmentCommand
	}
	updated, err := tx.Exec(ctx, `
		update public.attachment_uploads
		set upload_state = $4, actual_size_bytes = $5, actual_sha256_digest = $6,
		    completion_fingerprint = $7, completed_at = now(), updated_at = now()
		where project_id = $1 and upload_id = $2 and author_id = $3 and upload_state = $8
		  and temporary_object_key is not distinct from $9
		  and temporary_object_version is not distinct from $10`,
		command.ProjectID, command.UploadID, command.AuthorID,
		attachments.UploadStateQuarantined, command.ActualSizeBytes, command.ActualSHA256[:],
		command.CompletionFingerprint[:], attachments.UploadStateUploading,
		nullableAttachmentString(command.TemporaryObjectKey), nullableAttachmentString(command.TemporaryObjectVersion),
	)
	if err != nil {
		return attachments.UploadMutationResult{}, mapAttachmentWriteError("complete attachment upload and enqueue", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if err := updateLogicalAttachmentState(ctx, tx, upload, attachments.UploadStateQuarantined); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into public.attachment_processor_jobs (
			processor_job_id, upload_id, attachment_id, processor_profile,
			max_attempts, expires_at
		) values ($1, $2, $3, $4, $5, $6)`,
		command.ProcessorJobID, upload.uploadID, upload.attachmentID,
		command.ProcessorProfile, command.ProcessorMaxAttempts, command.ProcessorExpiresAt.UTC(),
	); err != nil {
		return attachments.UploadMutationResult{}, mapAttachmentWriteError("enqueue attachment processor job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload completion and enqueue: %w", err)
	}
	return newAttachmentUploadMutationResult(upload, attachments.UploadStateQuarantined), nil
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
	if (upload.state != attachments.UploadStateQuarantined && upload.state != attachments.UploadStateAvailable) ||
		!attachmentUploadCompletionMatches(upload, command) {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if _, err := readAttachmentUploadPart(ctx, tx, upload.uploadID); err != nil {
		return attachments.UploadMutationResult{}, err
	}
	var processorJobExists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from public.attachment_processor_jobs
			where upload_id = $1 and attachment_id = $2
		)`, upload.uploadID, upload.attachmentID).Scan(&processorJobExists); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("verify attachment processor enqueue: %w", err)
	}
	if !processorJobExists {
		return attachments.UploadMutationResult{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadMutationResult{}, fmt.Errorf("commit attachment upload completion replay: %w", err)
	}
	return newAttachmentUploadMutationResult(upload, upload.state), nil
}

func (repository *PostgresAttachmentRepository) ClaimProcessorJob(
	ctx context.Context,
	input attachments.ProcessorClaimInput,
) (*attachments.ProcessorClaim, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return nil, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin attachment processor claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		with candidate as (
			select job.processor_job_id
			from public.attachment_processor_jobs as job
			where (
				job.processor_state = 'queued'
				or (job.processor_state = 'retry_wait'
					and job.retry_at <= transaction_timestamp())
				or (job.processor_state = 'claimed'
					and job.lease_expires_at <= transaction_timestamp())
			)
			  and attempt < max_attempts
			  and job.expires_at > transaction_timestamp()
			  and exists (
				select 1
				from public.attachment_uploads as upload
				join public.record_attachments as attachment
				  on attachment.attachment_id = upload.attachment_id
				join public.attachment_upload_parts as part
				  on part.upload_id = upload.upload_id and part.part_number = 1
				where upload.upload_id = job.upload_id
				  and upload.attachment_id = job.attachment_id
				  and upload.upload_state = 'quarantined'
				  and attachment.attachment_state = 'quarantined'
			  )
			order by job.created_at, job.processor_job_id
			for update skip locked
			limit 1
		), claimed as (
			update public.attachment_processor_jobs as job
			set processor_state = 'claimed',
			    attempt = job.attempt + 1,
			    owner_id = $1,
			    owner_generation = job.owner_generation + 1,
			    lease_expires_at = least(
					transaction_timestamp() + ($2 * interval '1 microsecond'),
					job.expires_at
				),
			    retry_at = null,
			    result_code = null,
			    result_digest = null,
			    result_owner_id = '',
			    result_lease_expires_at = null,
			    updated_at = transaction_timestamp()
			from candidate
			where job.processor_job_id = candidate.processor_job_id
			returning job.processor_job_id, job.upload_id, job.attachment_id,
			          job.processor_profile, job.attempt, job.max_attempts,
			          job.owner_id, job.owner_generation, job.lease_expires_at,
			          job.expires_at
		)
		select upload.project_id, claimed.processor_job_id, claimed.upload_id,
		       claimed.attachment_id, attachment.display_name, attachment.media_type,
		       part.sha256_digest, part.object_version,
		       part.size_bytes, upload.transport_kind, claimed.processor_profile,
		       claimed.attempt, claimed.max_attempts, claimed.owner_id,
		       claimed.owner_generation, claimed.lease_expires_at, claimed.expires_at
		from claimed
		join public.attachment_uploads as upload on upload.upload_id = claimed.upload_id
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = claimed.attachment_id
		join public.attachment_upload_parts as part
		  on part.upload_id = claimed.upload_id and part.part_number = 1`,
		input.OwnerID,
		input.OwnerLeaseDuration.Microseconds(),
	)
	claim, err := scanAttachmentProcessorClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty attachment processor claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit attachment processor claim: %w", err)
	}
	return &claim, nil
}

func (repository *PostgresAttachmentRepository) RenewProcessorClaim(
	ctx context.Context,
	input attachments.ProcessorRenewInput,
) (attachments.ProcessorClaim, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return attachments.ProcessorClaim{}, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorClaim{}, fmt.Errorf("begin attachment processor renewal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var renewedExpiry time.Time
	err = tx.QueryRow(ctx, `
		update public.attachment_processor_jobs as job
		set lease_expires_at = least(
			transaction_timestamp() + ($2 * interval '1 microsecond'),
			job.expires_at
		), updated_at = transaction_timestamp()
		where job.processor_job_id = $1
		  and job.processor_state = 'claimed'
		  and job.owner_id = $3
		  and job.owner_generation = $4
		  and job.lease_expires_at = $5
		  and job.lease_expires_at > transaction_timestamp()
		  and job.attempt = $6
		  and job.upload_id = $7
		  and job.attachment_id = $8
		  and job.processor_profile = $9
		  and job.max_attempts = $10
		  and job.expires_at = $11
		  and exists (
			select 1
			from public.attachment_uploads as upload
			join public.attachment_upload_parts as part
			  on part.upload_id = upload.upload_id and part.part_number = 1
			where upload.project_id = $12
			  and upload.upload_id = job.upload_id
			  and upload.attachment_id = job.attachment_id
			  and part.sha256_digest = $13
			  and part.object_version = $14
			  and part.size_bytes = $15
			  and upload.transport_kind = $16
		  )
		returning job.lease_expires_at`,
		input.Claim.ProcessorJobID,
		input.OwnerLeaseDuration.Microseconds(),
		input.Claim.OwnerID,
		input.Claim.OwnerGeneration,
		input.Claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond),
		input.Claim.Attempt,
		input.Claim.UploadID,
		input.Claim.AttachmentID,
		input.Claim.Profile,
		input.Claim.MaxAttempts,
		input.Claim.ExpiresAt.UTC().Truncate(time.Microsecond),
		input.Claim.ProjectID,
		input.Claim.Source.SHA256[:],
		input.Claim.Source.ObjectVersion,
		input.Claim.Source.SizeBytes,
		input.Claim.Source.BackendKind,
	).Scan(&renewedExpiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ProcessorClaim{}, attachments.ErrProcessorClaimLost
	}
	if err != nil {
		return attachments.ProcessorClaim{}, fmt.Errorf("renew attachment processor claim: %w", err)
	}
	renewed := input.Claim
	renewed.LeaseExpiresAt = renewedExpiry.UTC()
	if renewed.Validate() != nil {
		return attachments.ProcessorClaim{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorClaim{}, fmt.Errorf("commit attachment processor renewal: %w", err)
	}
	return renewed, nil
}

func (repository *PostgresAttachmentRepository) ClaimProcessorWorkspaceCleanup(
	ctx context.Context,
	input attachments.ProcessorWorkspaceCleanupClaimInput,
) (*attachments.ProcessorWorkspaceCleanupCandidate, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return nil, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin attachment processor workspace cleanup claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var candidate attachments.ProcessorWorkspaceCleanupCandidate
	var pathDigest []byte
	err = tx.QueryRow(ctx, `
		with candidate as (
			select workspace.workspace_id
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			join public.attachment_uploads as upload on upload.upload_id = job.upload_id
			left join public.content_workspace_purge_receipts as receipt
			  on receipt.workspace_id = workspace.workspace_id
			where upload.project_id = $1
			  and receipt.workspace_id is null
			  and workspace.workspace_state in ('registered', 'materialized', 'purging')
			  and workspace.updated_at <= transaction_timestamp() - ($2 * interval '1 microsecond')
			  and (
				job.processor_state in ('succeeded', 'rejected', 'expired')
				or job.expires_at <= transaction_timestamp()
				or workspace.expires_at <= transaction_timestamp()
				or (job.processor_state = 'claimed'
				    and (job.lease_expires_at <= transaction_timestamp()
				         or workspace.attempt < job.attempt))
			  )
			order by workspace.expires_at, workspace.workspace_id
			for update of workspace skip locked
			limit 1
		), claimed as (
			update public.content_processor_workspaces as workspace
			set workspace_state = 'purging', updated_at = transaction_timestamp()
			from candidate
			where workspace.workspace_id = candidate.workspace_id
			returning workspace.workspace_id, workspace.workspace_path_digest
		)
		select workspace_id, workspace_path_digest from claimed`,
		input.ProjectID,
		input.RetryDelay.Microseconds(),
	).Scan(&candidate.WorkspaceID, &pathDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty attachment processor workspace cleanup claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim attachment processor workspace cleanup: %w", err)
	}
	if len(pathDigest) != sha256.Size {
		return nil, attachments.ErrProcessorWorkspaceReconciliationConflict
	}
	copy(candidate.WorkspacePathDigest[:], pathDigest)
	if candidate.Validate() != nil {
		return nil, attachments.ErrProcessorWorkspaceReconciliationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit attachment processor workspace cleanup claim: %w", err)
	}
	return &candidate, nil
}

func (repository *PostgresAttachmentRepository) RegisterProcessorWorkspace(
	ctx context.Context,
	registration attachments.ProcessorWorkspaceRegistration,
) (attachments.ProcessorWorkspace, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || registration.Validate() != nil {
		return attachments.ProcessorWorkspace{}, attachments.ErrInvalidProcessorCommand
	}
	registration.ExpiresAt = registration.ExpiresAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorWorkspace{}, fmt.Errorf("begin attachment processor workspace registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workspace attachments.ProcessorWorkspace
	var pathDigest []byte
	err = tx.QueryRow(ctx, `
		with live_claim as (
			select job.processor_job_id, job.attempt
			from public.attachment_processor_jobs as job
			where job.processor_job_id = $1
			  and job.processor_state = 'claimed'
			  and job.owner_id = $2
			  and job.owner_generation = $3
			  and job.lease_expires_at = $4
			  and job.lease_expires_at > transaction_timestamp()
			  and job.attempt = $5
			  and job.upload_id = $9
			  and job.attachment_id = $10
			  and job.processor_profile = $11
			  and job.max_attempts = $12
			  and job.expires_at = $13
			  and exists (
				select 1
				from public.attachment_uploads as upload
				join public.attachment_upload_parts as part
				  on part.upload_id = upload.upload_id and part.part_number = 1
				where upload.project_id = $14
				  and upload.upload_id = job.upload_id
				  and upload.attachment_id = job.attachment_id
				  and part.sha256_digest = $15
				  and part.object_version = $16
				  and part.size_bytes = $17
				  and upload.transport_kind = $18
			  )
			for update
		), inserted as (
			insert into public.content_processor_workspaces (
				workspace_id, processor_job_id, attempt, workspace_state,
				workspace_path_digest, expires_at
			)
			select $6, live_claim.processor_job_id, live_claim.attempt, 'registered', $7, $8
			from live_claim
			on conflict (processor_job_id, attempt) do nothing
			returning workspace_id, processor_job_id, attempt, workspace_state,
			          workspace_path_digest, expires_at
		)
		select workspace_id, processor_job_id, attempt, workspace_state,
		       workspace_path_digest, expires_at
		from inserted
		union all
		select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
		from public.content_processor_workspaces as workspace
		join live_claim
		  on live_claim.processor_job_id = workspace.processor_job_id
		 and live_claim.attempt = workspace.attempt
		where not exists (select 1 from inserted)
		limit 1`,
		registration.Claim.ProcessorJobID,
		registration.Claim.OwnerID,
		registration.Claim.OwnerGeneration,
		registration.Claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond),
		registration.Claim.Attempt,
		registration.WorkspaceID,
		registration.WorkspacePathDigest[:],
		registration.ExpiresAt,
		registration.Claim.UploadID,
		registration.Claim.AttachmentID,
		registration.Claim.Profile,
		registration.Claim.MaxAttempts,
		registration.Claim.ExpiresAt.UTC().Truncate(time.Microsecond),
		registration.Claim.ProjectID,
		registration.Claim.Source.SHA256[:],
		registration.Claim.Source.ObjectVersion,
		registration.Claim.Source.SizeBytes,
		registration.Claim.Source.BackendKind,
	).Scan(
		&workspace.WorkspaceID,
		&workspace.ProcessorJobID,
		&workspace.Attempt,
		&workspace.State,
		&pathDigest,
		&workspace.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ProcessorWorkspace{}, attachments.ErrProcessorClaimLost
	}
	if err != nil {
		return attachments.ProcessorWorkspace{}, mapAttachmentWriteError("register attachment processor workspace", err)
	}
	if len(pathDigest) != sha256.Size {
		return attachments.ProcessorWorkspace{}, attachments.ErrAttachmentConflict
	}
	copy(workspace.WorkspacePathDigest[:], pathDigest)
	workspace.ExpiresAt = workspace.ExpiresAt.UTC()
	want := attachments.ProcessorWorkspace{
		WorkspaceID: registration.WorkspaceID, ProcessorJobID: registration.Claim.ProcessorJobID,
		Attempt: registration.Claim.Attempt, State: attachments.ProcessorWorkspaceStateRegistered,
		WorkspacePathDigest: registration.WorkspacePathDigest, ExpiresAt: registration.ExpiresAt,
	}
	if workspace.Validate() != nil || workspace != want {
		return attachments.ProcessorWorkspace{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorWorkspace{}, fmt.Errorf("commit attachment processor workspace registration: %w", err)
	}
	return workspace, nil
}

func (repository *PostgresAttachmentRepository) MaterializeProcessorWorkspace(
	ctx context.Context,
	transition attachments.ProcessorWorkspaceTransition,
) (attachments.ProcessorWorkspace, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || transition.Validate() != nil {
		return attachments.ProcessorWorkspace{}, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorWorkspace{}, fmt.Errorf("begin attachment processor workspace materialization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if transition.Authorization.Mode != attachments.ProcessorWorkspaceAuthorizationWorker {
		return attachments.ProcessorWorkspace{}, attachments.ErrInvalidProcessorCommand
	}
	claim := transition.Authorization.Claim
	workspace, err := scanAttachmentProcessorWorkspace(tx.QueryRow(ctx, `
		with authorized_workspace as (
			select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			where workspace.workspace_id = $1
			  and workspace.workspace_path_digest = $2
			  and workspace.attempt = $7
			  and job.processor_job_id = $3
			  and job.upload_id = $4
			  and job.attachment_id = $5
			  and job.processor_profile = $6
			  and job.attempt = $7
			  and job.max_attempts = $8
			  and job.owner_id = $9
			  and job.owner_generation = $10
			  and job.lease_expires_at = $11
			  and job.lease_expires_at > transaction_timestamp()
			  and job.expires_at = $12
			  and job.processor_state = 'claimed'
			  and exists (
				select 1
				from public.attachment_uploads as upload
				join public.attachment_upload_parts as part
				  on part.upload_id = upload.upload_id and part.part_number = 1
				where upload.project_id = $13
				  and upload.upload_id = job.upload_id
				  and upload.attachment_id = job.attachment_id
				  and upload.transport_kind = $14
				  and part.sha256_digest = $15
				  and part.object_version = $16
				  and part.size_bytes = $17
			  )
			for update of workspace, job
		), materialized as (
			update public.content_processor_workspaces as workspace
			set workspace_state = 'materialized', updated_at = transaction_timestamp()
			from authorized_workspace
			where workspace.workspace_id = authorized_workspace.workspace_id
			  and authorized_workspace.workspace_state = 'registered'
			returning workspace.workspace_id, workspace.processor_job_id,
			          workspace.attempt, workspace.workspace_state,
			          workspace.workspace_path_digest, workspace.expires_at
		)
		select workspace_id, processor_job_id, attempt, workspace_state,
		       workspace_path_digest, expires_at
		from materialized
		union all
		select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
		from public.content_processor_workspaces as workspace
		join authorized_workspace on authorized_workspace.workspace_id = workspace.workspace_id
		where workspace.workspace_state = 'materialized'
		  and not exists (select 1 from materialized)
		limit 1`,
		transition.WorkspaceID,
		transition.WorkspacePathDigest[:],
		claim.ProcessorJobID, claim.UploadID, claim.AttachmentID, claim.Profile,
		claim.Attempt, claim.MaxAttempts, claim.OwnerID, claim.OwnerGeneration,
		claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond), claim.ExpiresAt.UTC().Truncate(time.Microsecond),
		claim.ProjectID, claim.Source.BackendKind, claim.Source.SHA256[:], claim.Source.ObjectVersion,
		claim.Source.SizeBytes,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ProcessorWorkspace{}, attachments.ErrProcessorClaimLost
	}
	if err != nil {
		return attachments.ProcessorWorkspace{}, mapAttachmentWriteError("materialize attachment processor workspace", err)
	}
	if workspace.WorkspaceID != transition.WorkspaceID ||
		workspace.WorkspacePathDigest != transition.WorkspacePathDigest ||
		workspace.State != attachments.ProcessorWorkspaceStateMaterialized {
		return attachments.ProcessorWorkspace{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorWorkspace{}, fmt.Errorf("commit attachment processor workspace materialization: %w", err)
	}
	return workspace, nil
}

func (repository *PostgresAttachmentRepository) BeginProcessorWorkspacePurge(
	ctx context.Context,
	transition attachments.ProcessorWorkspaceTransition,
) (attachments.ProcessorWorkspacePurgePlan, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || transition.Validate() != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, fmt.Errorf("begin attachment processor workspace purge transition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var row pgx.Row
	workerAuthorization := transition.Authorization.Mode == attachments.ProcessorWorkspaceAuthorizationWorker
	if workerAuthorization {
		claim := transition.Authorization.Claim
		row = tx.QueryRow(ctx, `
		with locked as (
			select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			where workspace.workspace_id = $1
			  and workspace.workspace_path_digest = $2
			  and workspace.attempt = $7
			  and job.processor_job_id = $3
			  and job.upload_id = $4
			  and job.attachment_id = $5
			  and job.processor_profile = $6
			  and job.attempt = $7
			  and job.max_attempts = $8
			  and job.owner_id = $9
			  and job.owner_generation = $10
			  and job.lease_expires_at = $11
			  and job.expires_at = $12
			  and job.processor_state = 'claimed'
			  and exists (
				select 1
				from public.attachment_uploads as upload
				join public.attachment_upload_parts as part
				  on part.upload_id = upload.upload_id and part.part_number = 1
				where upload.project_id = $13
				  and upload.upload_id = job.upload_id
				  and upload.attachment_id = job.attachment_id
				  and upload.transport_kind = $14
				  and part.sha256_digest = $15
				  and part.object_version = $16
				  and part.size_bytes = $17
			  )
			for update of workspace, job
		), purging as (
			update public.content_processor_workspaces as workspace
			set workspace_state = 'purging', updated_at = transaction_timestamp()
			from locked
			where workspace.workspace_id = locked.workspace_id
			  and locked.workspace_state in ('registered', 'materialized')
			returning workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			          workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
		), selected_workspace as (
			select * from purging
			union all
			select * from locked where not exists (select 1 from purging)
		)
		select true, workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at,
		       (receipt.workspace_id is not null), coalesce(receipt.workspace_id, ''),
		       coalesce(receipt.removed_surface_digest, '\x'::bytea),
		       coalesce(receipt.receipt_digest, '\x'::bytea),
		       coalesce(receipt.removed_row_count, 0),
		       coalesce(receipt.verified_absent_at, transaction_timestamp())
		from selected_workspace as workspace
		left join public.content_workspace_purge_receipts as receipt
		  on receipt.workspace_id = workspace.workspace_id
		limit 1`,
			transition.WorkspaceID,
			transition.WorkspacePathDigest[:],
			claim.ProcessorJobID, claim.UploadID, claim.AttachmentID, claim.Profile,
			claim.Attempt, claim.MaxAttempts, claim.OwnerID, claim.OwnerGeneration,
			claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond), claim.ExpiresAt.UTC().Truncate(time.Microsecond),
			claim.ProjectID, claim.Source.BackendKind, claim.Source.SHA256[:], claim.Source.ObjectVersion,
			claim.Source.SizeBytes,
		)
	} else {
		row = tx.QueryRow(ctx, `
		with locked as (
			select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			where workspace.workspace_id = $1
			  and workspace.workspace_path_digest = $2
			  and (
				job.processor_state in ('succeeded', 'rejected', 'expired')
				or job.expires_at <= transaction_timestamp()
				or workspace.expires_at <= transaction_timestamp()
				or (job.processor_state = 'claimed'
				    and (job.lease_expires_at <= transaction_timestamp()
				         or workspace.attempt < job.attempt))
			  )
			for update of workspace, job
		), purging as (
			update public.content_processor_workspaces as workspace
			set workspace_state = 'purging', updated_at = transaction_timestamp()
			from locked
			where workspace.workspace_id = locked.workspace_id
			  and locked.workspace_state in ('registered', 'materialized')
			returning workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			          workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
		), selected_workspace as (
			select * from purging
			union all
			select * from locked where not exists (select 1 from purging)
		)
		select true, workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at,
		       (receipt.workspace_id is not null), coalesce(receipt.workspace_id, ''),
		       coalesce(receipt.removed_surface_digest, '\x'::bytea),
		       coalesce(receipt.receipt_digest, '\x'::bytea),
		       coalesce(receipt.removed_row_count, 0),
		       coalesce(receipt.verified_absent_at, transaction_timestamp())
		from selected_workspace as workspace
		left join public.content_workspace_purge_receipts as receipt
		  on receipt.workspace_id = workspace.workspace_id
		union all
		select false, '', '', 0::bigint, ''::text, '\x'::bytea, transaction_timestamp(),
		       true, receipt.workspace_id, receipt.removed_surface_digest,
		       receipt.receipt_digest, receipt.removed_row_count, receipt.verified_absent_at
		from public.content_workspace_purge_receipts as receipt
		where receipt.workspace_id = $1
		  and not exists (select 1 from selected_workspace)
		  and not exists (
			select 1 from public.content_processor_workspaces as remaining_workspace
			where remaining_workspace.workspace_id = $1
		  )
		limit 1`,
			transition.WorkspaceID,
			transition.WorkspacePathDigest[:],
		)
	}
	plan, err := scanAttachmentProcessorWorkspacePurgePlan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if workerAuthorization {
			return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrProcessorClaimLost
		}
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, mapAttachmentWriteError("begin attachment processor workspace purge", err)
	}
	if plan.Receipt != nil && plan.Receipt.WorkspaceID != transition.WorkspaceID {
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
	}
	if plan.Workspace == (attachments.ProcessorWorkspace{}) {
		if plan.Receipt == nil {
			return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
		}
	} else if plan.Workspace.WorkspaceID != transition.WorkspaceID ||
		plan.Workspace.WorkspacePathDigest != transition.WorkspacePathDigest ||
		(plan.Workspace.State != attachments.ProcessorWorkspaceStatePurging &&
			plan.Workspace.State != attachments.ProcessorWorkspaceStatePurged) ||
		(plan.Workspace.State == attachments.ProcessorWorkspaceStatePurged) != (plan.Receipt != nil) {
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, fmt.Errorf("commit attachment processor workspace purge transition: %w", err)
	}
	return plan, nil
}

func (repository *PostgresAttachmentRepository) CompleteProcessorWorkspacePurge(
	ctx context.Context,
	completion attachments.ProcessorWorkspacePurgeCompletion,
) (attachments.ProcessorWorkspacePurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || completion.Validate() != nil {
		return attachments.ProcessorWorkspacePurgeReceipt{}, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorWorkspacePurgeReceipt{}, fmt.Errorf("begin attachment processor workspace purge completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	claim := completion.Workspace.Authorization.Claim
	plan, err := scanAttachmentProcessorWorkspacePurgePlan(tx.QueryRow(ctx, `
		with locked as (
			select workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
			from public.content_processor_workspaces as workspace
			join public.attachment_processor_jobs as job
			  on job.processor_job_id = workspace.processor_job_id
			where workspace.workspace_id = $1
			  and workspace.workspace_path_digest = $2
			  and workspace.workspace_state in ('purging', 'purged')
			  and (
				($7 = 'worker'
				  and workspace.attempt = $12
				  and job.processor_job_id = $8
				  and job.upload_id = $9
				  and job.attachment_id = $10
				  and job.processor_profile = $11
				  and job.attempt = $12
				  and job.max_attempts = $13
				  and job.owner_id = $14
				  and job.owner_generation = $15
				  and job.lease_expires_at = $16
				  and job.expires_at = $17
				  and job.processor_state = 'claimed'
				  and exists (
					select 1
					from public.attachment_uploads as upload
					join public.attachment_upload_parts as part
					  on part.upload_id = upload.upload_id and part.part_number = 1
					where upload.project_id = $18
					  and upload.upload_id = job.upload_id
					  and upload.attachment_id = job.attachment_id
					  and upload.transport_kind = $19
					  and part.sha256_digest = $20
					  and part.object_version = $21
					  and part.size_bytes = $22
				  ))
				or ($7 = 'reconciliation'
				  and (
					job.processor_state in ('succeeded', 'rejected', 'expired')
					or job.expires_at <= transaction_timestamp()
					or workspace.expires_at <= transaction_timestamp()
					or (job.processor_state = 'claimed'
					    and (job.lease_expires_at <= transaction_timestamp()
					         or workspace.attempt < job.attempt))
				  ))
			  )
			for update of workspace, job
		), inserted_receipt as (
			insert into public.content_workspace_purge_receipts (
				workspace_id, removed_surface_digest, receipt_digest,
				removed_row_count, verified_absent_at
			)
			select locked.workspace_id, $3, $4, $5, $6
			from locked
			where locked.workspace_state = 'purging'
			on conflict (workspace_id) do nothing
			returning workspace_id, removed_surface_digest, receipt_digest,
			          removed_row_count, verified_absent_at
		), purged as (
			update public.content_processor_workspaces as workspace
			set workspace_state = 'purged', purged_at = $6,
			    updated_at = transaction_timestamp()
			from inserted_receipt
			where workspace.workspace_id = inserted_receipt.workspace_id
			returning workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
			          workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at
		)
		select true, workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at,
		       true, receipt.workspace_id, receipt.removed_surface_digest,
		       receipt.receipt_digest, receipt.removed_row_count, receipt.verified_absent_at
		from purged as workspace
		join inserted_receipt as receipt on receipt.workspace_id = workspace.workspace_id
		union all
		select true, workspace.workspace_id, workspace.processor_job_id, workspace.attempt,
		       workspace.workspace_state, workspace.workspace_path_digest, workspace.expires_at,
		       true, receipt.workspace_id, receipt.removed_surface_digest,
		       receipt.receipt_digest, receipt.removed_row_count, receipt.verified_absent_at
		from public.content_processor_workspaces as workspace
		join locked on locked.workspace_id = workspace.workspace_id
		join public.content_workspace_purge_receipts as receipt
		  on receipt.workspace_id = workspace.workspace_id
		where not exists (select 1 from inserted_receipt)
		limit 1`,
		completion.Workspace.WorkspaceID,
		completion.Workspace.WorkspacePathDigest[:],
		completion.Receipt.RemovedSurfaceDigest[:],
		completion.Receipt.ReceiptDigest[:],
		completion.Receipt.RemovedRowCount,
		completion.Receipt.VerifiedAbsentAt.UTC().Truncate(time.Microsecond),
		completion.Workspace.Authorization.Mode,
		claim.ProcessorJobID, claim.UploadID, claim.AttachmentID, claim.Profile,
		claim.Attempt, claim.MaxAttempts, claim.OwnerID, claim.OwnerGeneration,
		claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond), claim.ExpiresAt.UTC().Truncate(time.Microsecond),
		claim.ProjectID, claim.Source.BackendKind, claim.Source.SHA256[:], claim.Source.ObjectVersion,
		claim.Source.SizeBytes,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if completion.Workspace.Authorization.Mode == attachments.ProcessorWorkspaceAuthorizationWorker {
			return attachments.ProcessorWorkspacePurgeReceipt{}, attachments.ErrProcessorClaimLost
		}
		return attachments.ProcessorWorkspacePurgeReceipt{}, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.ProcessorWorkspacePurgeReceipt{}, mapAttachmentWriteError("complete attachment processor workspace purge", err)
	}
	if plan.Workspace.State != attachments.ProcessorWorkspaceStatePurged || plan.Receipt == nil ||
		*plan.Receipt != completion.Receipt {
		return attachments.ProcessorWorkspacePurgeReceipt{}, attachments.ErrAttachmentConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorWorkspacePurgeReceipt{}, fmt.Errorf("commit attachment processor workspace purge completion: %w", err)
	}
	return *plan.Receipt, nil
}

func scanAttachmentProcessorWorkspace(row pgx.Row) (attachments.ProcessorWorkspace, error) {
	var workspace attachments.ProcessorWorkspace
	var pathDigest []byte
	err := row.Scan(
		&workspace.WorkspaceID,
		&workspace.ProcessorJobID,
		&workspace.Attempt,
		&workspace.State,
		&pathDigest,
		&workspace.ExpiresAt,
	)
	if err != nil {
		return attachments.ProcessorWorkspace{}, err
	}
	if len(pathDigest) != sha256.Size {
		return attachments.ProcessorWorkspace{}, attachments.ErrAttachmentConflict
	}
	copy(workspace.WorkspacePathDigest[:], pathDigest)
	workspace.ExpiresAt = workspace.ExpiresAt.UTC()
	if workspace.Validate() != nil {
		return attachments.ProcessorWorkspace{}, attachments.ErrAttachmentConflict
	}
	return workspace, nil
}

func scanAttachmentProcessorWorkspacePurgePlan(row pgx.Row) (attachments.ProcessorWorkspacePurgePlan, error) {
	var plan attachments.ProcessorWorkspacePurgePlan
	var pathDigest, removedSurfaceDigest, receiptDigest []byte
	var hasWorkspace, hasReceipt bool
	var receiptWorkspaceID string
	var removedRowCount int64
	var verifiedAbsentAt time.Time
	err := row.Scan(
		&hasWorkspace,
		&plan.Workspace.WorkspaceID,
		&plan.Workspace.ProcessorJobID,
		&plan.Workspace.Attempt,
		&plan.Workspace.State,
		&pathDigest,
		&plan.Workspace.ExpiresAt,
		&hasReceipt,
		&receiptWorkspaceID,
		&removedSurfaceDigest,
		&receiptDigest,
		&removedRowCount,
		&verifiedAbsentAt,
	)
	if err != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, err
	}
	if hasWorkspace {
		if len(pathDigest) != sha256.Size {
			return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
		}
		copy(plan.Workspace.WorkspacePathDigest[:], pathDigest)
		plan.Workspace.ExpiresAt = plan.Workspace.ExpiresAt.UTC()
		if plan.Workspace.Validate() != nil {
			return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
		}
	} else {
		plan.Workspace = attachments.ProcessorWorkspace{}
		if !hasReceipt {
			return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
		}
	}
	if !hasReceipt {
		return plan, nil
	}
	if len(removedSurfaceDigest) != sha256.Size || len(receiptDigest) != sha256.Size {
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
	}
	receipt := attachments.ProcessorWorkspacePurgeReceipt{
		WorkspaceID: receiptWorkspaceID, RemovedRowCount: removedRowCount,
		VerifiedAbsentAt: verifiedAbsentAt.UTC(),
	}
	copy(receipt.RemovedSurfaceDigest[:], removedSurfaceDigest)
	copy(receipt.ReceiptDigest[:], receiptDigest)
	if receipt.Validate() != nil {
		return attachments.ProcessorWorkspacePurgePlan{}, attachments.ErrAttachmentConflict
	}
	plan.Receipt = &receipt
	return plan, nil
}

func (repository *PostgresAttachmentRepository) CompleteProcessorJob(
	ctx context.Context,
	input attachments.ProcessorCompletionInput,
) (attachments.ProcessorCompletionResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return attachments.ProcessorCompletionResult{}, attachments.ErrInvalidProcessorCommand
	}
	if input.Result.Source != input.Claim.Source || input.Result.Profile != input.Claim.Profile {
		return attachments.ProcessorCompletionResult{}, attachments.ErrAttachmentConflict
	}
	// Re-check the typed preview contract at the durable boundary. The same
	// guard runs before both first completion and terminal/retry replay reads;
	// a fabricated or oversized result must never reach Blob/attachment writes.
	if err := attachments.ValidateProcessorResultForCompletion(input.Result, input.Limits); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	resultDigest, err := input.Result.Digest()
	if err != nil {
		return attachments.ProcessorCompletionResult{}, attachments.ErrInvalidProcessorCommand
	}
	input.RetryAt = input.RetryAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.ProcessorCompletionResult{}, fmt.Errorf("begin attachment processor completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	preflight, err := preflightAttachmentProcessorCompletion(ctx, tx, input.Claim)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if err := validateAttachmentProcessorJobIdentity(preflight, input.Claim); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if preflight.state != attachments.ProcessorStateClaimed {
		if err := validateAttachmentProcessorResultClaim(preflight, input.Claim); err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		result, err := readAttachmentProcessorCompletionReplay(
			ctx, tx, input, resultDigest, preflight,
		)
		if err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.ProcessorCompletionResult{}, fmt.Errorf("commit attachment processor result replay: %w", err)
		}
		return result, nil
	}
	if input.Result.HasPreview &&
		input.PreviewPublicationIntent == (attachments.BlobPublicationIntent{}) {
		return attachments.ProcessorCompletionResult{}, attachments.ErrInvalidProcessorCommand
	}
	if retryableAttachmentProcessorResult(input.Result.Code) &&
		!input.RetryAt.After(preflight.databaseNow) {
		return attachments.ProcessorCompletionResult{}, attachments.ErrInvalidProcessorCommand
	}

	usage, quotaVersion, err := lockAttachmentQuotaAccount(ctx, tx, input.Claim.ProjectID)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	upload, err := loadAttachmentProcessorUpload(ctx, tx, input.Claim, true)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	job, err := loadAttachmentProcessorJob(ctx, tx, input.Claim.ProcessorJobID, true)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if err := validateLiveAttachmentProcessorClaim(job, input.Claim); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if err := validateAttachmentProcessorUploadForClaim(upload, input.Claim); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	result, err := commitAttachmentProcessorResult(
		ctx, tx, input, resultDigest, job, upload, usage, quotaVersion,
	)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.ProcessorCompletionResult{}, fmt.Errorf("commit attachment processor result: %w", err)
	}
	return result, nil
}

func (repository *PostgresAttachmentRepository) ExpireBoundedProcessorJob(
	ctx context.Context,
	input attachments.ProcessorExpiryInput,
) (*attachments.ProcessorCompletionResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return nil, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin bounded attachment processor expiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	usage, quotaVersion, quotaExists, err := lockExistingAttachmentProcessorQuotaAccount(
		ctx, tx, input.ProjectID,
	)
	if err != nil {
		return nil, err
	}
	if !quotaExists {
		activeJobExists, err := attachmentProcessorActiveJobExists(ctx, tx, input.ProjectID)
		if err != nil {
			return nil, err
		}
		if activeJobExists {
			return nil, attachments.ErrAttachmentConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty bounded attachment processor expiry: %w", err)
		}
		return nil, nil
	}
	job, err := lockBoundedAttachmentProcessorJob(ctx, tx, input.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty bounded attachment processor expiry: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validateBoundedAttachmentProcessorJob(job); err != nil {
		return nil, err
	}

	identity := attachments.ProcessorClaim{
		ProjectID: input.ProjectID, ProcessorJobID: job.processorJobID,
		UploadID: job.uploadID, AttachmentID: job.attachmentID,
	}
	upload, err := loadAttachmentProcessorUpload(ctx, tx, identity, true)
	if err != nil {
		return nil, err
	}
	lockedPart, err := lockAttachmentUploadPart(ctx, tx, job.uploadID)
	if err != nil {
		return nil, err
	}
	if lockedPart.Key != upload.source.Key || lockedPart.SHA256 != upload.source.SHA256 ||
		lockedPart.VersionID != upload.source.ObjectVersion ||
		lockedPart.SizeBytes != upload.source.SizeBytes {
		return nil, attachments.ErrAttachmentConflict
	}
	if err := validateAttachmentProcessorUploadForBoundedExpiry(
		upload, job, input.ProjectID,
	); err != nil {
		return nil, err
	}
	outcome, err := buildAttachmentProcessorExpiryOutcome(job, upload.source, input.OwnerID)
	if err != nil {
		return nil, err
	}
	if quotaVersion == math.MaxInt64 {
		return nil, attachments.ErrQuotaOverflow
	}
	nextUsage, err := usage.ReleaseReservation(upload.upload.reservedSizeBytes)
	if err != nil {
		return nil, err
	}
	if err := updateAttachmentUploadStates(
		ctx, tx, upload.upload, attachments.UploadStateExpired,
	); err != nil {
		return nil, err
	}
	if err := updateBoundedAttachmentProcessorJobExpiry(ctx, tx, job, outcome); err != nil {
		return nil, err
	}
	if err := updateAttachmentQuotaAccount(
		ctx, tx, input.ProjectID, quotaVersion, nextUsage,
	); err != nil {
		return nil, err
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload.upload, nextUsage, input.Limits)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit bounded attachment processor expiry: %w", err)
	}
	return &attachments.ProcessorCompletionResult{
		ProjectID:       input.ProjectID,
		ProcessorJobID:  job.processorJobID,
		UploadID:        job.uploadID,
		AttachmentID:    job.attachmentID,
		ProcessorState:  attachments.ProcessorStateExpired,
		UploadState:     attachments.UploadStateExpired,
		AttachmentState: attachments.UploadStateExpired,
		ResultCode:      outcome.code,
		ResultDigest:    outcome.digest,
		Quota:           quota,
	}, nil
}

func (repository *PostgresAttachmentRepository) ExpireAbandonedUpload(
	ctx context.Context,
	input attachments.AbandonedUploadExpiryInput,
) (*attachments.UploadMutationResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || input.Validate() != nil {
		return nil, attachments.ErrInvalidProcessorCommand
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin abandoned attachment upload expiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	usage, quotaVersion, quotaExists, err := lockExistingAttachmentProcessorQuotaAccount(
		ctx, tx, input.ProjectID,
	)
	if err != nil {
		return nil, err
	}
	upload, attachmentState, err := lockExpiredAbandonedAttachmentUpload(ctx, tx, input.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty abandoned attachment upload expiry: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !quotaExists {
		return nil, attachments.ErrAttachmentConflict
	}
	if attachmentState != upload.state ||
		(upload.state != attachments.UploadStateCreated && upload.state != attachments.UploadStateUploading) ||
		attachments.ValidateUploadStateTransition(upload.state, attachments.UploadStateExpired) != nil {
		return nil, attachments.ErrAttachmentConflict
	}
	if quotaVersion == math.MaxInt64 {
		return nil, attachments.ErrQuotaOverflow
	}
	nextUsage, err := usage.ReleaseReservation(upload.reservedSizeBytes)
	if err != nil {
		return nil, err
	}
	if err := updateAttachmentUploadStates(ctx, tx, upload, attachments.UploadStateExpired); err != nil {
		return nil, err
	}
	if err := updateAttachmentQuotaAccount(ctx, tx, input.ProjectID, quotaVersion, nextUsage); err != nil {
		return nil, err
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload, nextUsage, input.Limits)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit abandoned attachment upload expiry: %w", err)
	}
	result := newAttachmentUploadMutationResult(upload, attachments.UploadStateExpired)
	result.Quota = quota
	return &result, nil
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
		        where (attachment.blob_key = blob.blob_key
		           and attachment.blob_object_version = blob.object_version)
		           or (attachment.preview_blob_key = blob.blob_key
		           and attachment.preview_blob_object_version = blob.object_version)),
		       (select count(*)::bigint
		        from public.record_revision_attachments revision_ref
		        join public.record_attachments attachment
		          on attachment.attachment_id = revision_ref.attachment_id
		        where (attachment.blob_key = blob.blob_key
		           and attachment.blob_object_version = blob.object_version)
		           or (attachment.preview_blob_key = blob.blob_key
		           and attachment.preview_blob_object_version = blob.object_version)),
		       (select count(*)::bigint
		        from public.attachment_upload_parts part
		        where part.sha256_digest = blob.sha256_digest
		          and part.object_version = blob.object_version),
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
		&protection.UploadPartReferenceCount,
		&protection.ActivePinCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.BlobProtection{}, attachments.ErrAttachmentOwnerNotFound
	}
	if err != nil {
		return attachments.BlobProtection{}, fmt.Errorf("read Blob protection: %w", err)
	}
	if protection.LogicalAttachmentCount < 0 || protection.RevisionReferenceCount < 0 ||
		protection.UploadPartReferenceCount < 0 || protection.ActivePinCount < 0 {
		return attachments.BlobProtection{}, attachments.ErrInvalidQuotaUsage
	}
	protection.Protected = protection.LogicalAttachmentCount > 0 ||
		protection.RevisionReferenceCount > 0 || protection.UploadPartReferenceCount > 0 ||
		protection.ActivePinCount > 0
	return protection, nil
}

func (repository *PostgresAttachmentRepository) readUploadReservationReplay(
	ctx context.Context,
	command attachments.ReserveUploadCommand,
) (attachments.UploadReservationResult, error) {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("begin attachment reservation replay: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var projectID, uploadID, attachmentID, originDraftID, authorID string
	var state, attachmentState attachments.UploadState
	var transport attachments.TransportKind
	var declaredSize, logicalSize int64
	var expiresAt time.Time
	var displayName, mediaType, draftID, createdBy string
	err = tx.QueryRow(ctx, `
		select upload.project_id, upload.upload_id, upload.attachment_id,
		       upload.origin_draft_id, upload.author_id, upload.upload_state,
		       upload.transport_kind, upload.declared_size_bytes, upload.expires_at,
		       attachment.attachment_state, attachment.display_name,
		       attachment.media_type, attachment.logical_size_bytes,
		       attachment.draft_id, attachment.created_by
		from public.attachment_uploads upload
		join public.record_attachments attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		where upload.upload_id = $1`, command.UploadID).Scan(
		&projectID, &uploadID, &attachmentID, &originDraftID, &authorID, &state,
		&transport, &declaredSize, &expiresAt, &attachmentState, &displayName,
		&mediaType, &logicalSize, &draftID, &createdBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.UploadReservationResult{}, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("read attachment reservation replay: %w", err)
	}
	if projectID != command.ProjectID || uploadID != command.UploadID || attachmentID != command.AttachmentID ||
		originDraftID != command.DraftID || authorID != command.AuthorID || createdBy != command.AuthorID ||
		(state != attachments.UploadStateCreated && state != attachments.UploadStateUploading) ||
		attachmentState != state || transport != command.TransportKind || declaredSize != command.DeclaredSizeBytes ||
		logicalSize != command.DeclaredSizeBytes || displayName != command.DisplayName || mediaType != command.MediaType ||
		draftID != command.DraftID || !expiresAt.Equal(command.ExpiresAt.UTC().Truncate(time.Microsecond)) {
		return attachments.UploadReservationResult{}, attachments.ErrAttachmentConflict
	}
	var usage attachments.QuotaUsage
	if err := tx.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes
		from public.attachment_quota_accounts
		where project_id = $1`, command.ProjectID).Scan(
		&usage.LogicalBytes, &usage.ReservedBytes, &usage.PhysicalBytes,
	); err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("read attachment reservation replay quota: %w", err)
	}
	route, err := loadAttachmentDraftRoute(ctx, tx, command)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	effectiveRecordBytes, err := readEffectiveAttachmentUsage(
		ctx, tx, command.ProjectID, command.DraftID, route.recordID,
	)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	warning, err := usage.ProjectWarning(command.Limits)
	if err != nil {
		return attachments.UploadReservationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.UploadReservationResult{}, fmt.Errorf("commit attachment reservation replay: %w", err)
	}
	return attachments.UploadReservationResult{
		UploadID: uploadID, AttachmentID: attachmentID, State: state,
		Quota: attachments.QuotaSnapshot{
			Usage: usage, EffectiveRecordBytes: effectiveRecordBytes, ProjectWarning: warning,
		},
	}, nil
}

func readAttachmentUploadPreparation(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
) (attachments.UploadPreparation, attachments.UploadState, error) {
	var preparation attachments.UploadPreparation
	var attachmentState attachments.UploadState
	var attachmentID, projectID, originDraftID string
	err := tx.QueryRow(ctx, `
		select upload.transport_kind, upload.expires_at,
		       attachment.project_id, attachment.attachment_id,
		       attachment.origin_draft_id, attachment.attachment_state,
		       attachment.media_type
		from public.attachment_uploads upload
		join public.record_attachments attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		where upload.project_id = $1 and upload.upload_id = $2 and upload.author_id = $3`,
		upload.projectID, upload.uploadID, upload.authorID,
	).Scan(
		&preparation.TransportKind, &preparation.ExpiresAt,
		&projectID, &attachmentID, &originDraftID, &attachmentState, &preparation.MediaType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.UploadPreparation{}, "", attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.UploadPreparation{}, "", fmt.Errorf("read attachment upload preparation: %w", err)
	}
	if projectID != upload.projectID || attachmentID != upload.attachmentID || originDraftID != upload.originDraftID {
		return attachments.UploadPreparation{}, "", attachments.ErrAttachmentConflict
	}
	preparation.ProjectID = upload.projectID
	preparation.UploadID = upload.uploadID
	preparation.AttachmentID = upload.attachmentID
	preparation.DraftID = upload.originDraftID
	preparation.AuthorID = upload.authorID
	preparation.State = upload.state
	preparation.DeclaredSizeBytes = upload.declaredSizeBytes
	if upload.temporaryObjectKey != nil {
		preparation.TemporaryObjectKey = *upload.temporaryObjectKey
	}
	if upload.temporaryObjectVersion != nil {
		preparation.TemporaryObjectVersion = *upload.temporaryObjectVersion
	}
	return preparation, attachmentState, nil
}

func validatePreparedAttachmentUpload(
	preparation attachments.UploadPreparation,
	command attachments.PrepareUploadCommand,
) error {
	switch preparation.TransportKind {
	case attachments.TransportKindLocal:
		if command.CandidateTemporaryObjectKey != "" || preparation.TemporaryObjectKey != "" ||
			preparation.TemporaryObjectVersion != "" {
			return attachments.ErrAttachmentConflict
		}
	case attachments.TransportKindS3:
		if command.CandidateTemporaryObjectKey == "" || preparation.TemporaryObjectKey == "" {
			return attachments.ErrAttachmentConflict
		}
	default:
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func attachmentUploadTemporaryKeyMatches(
	preparation attachments.UploadPreparation,
	key string,
) bool {
	switch preparation.TransportKind {
	case attachments.TransportKindLocal:
		return preparation.TemporaryObjectKey == "" && preparation.TemporaryObjectVersion == "" && key == ""
	case attachments.TransportKindS3:
		return preparation.TemporaryObjectKey != "" && preparation.TemporaryObjectKey == key &&
			preparation.TemporaryObjectVersion == ""
	default:
		return false
	}
}

func attachmentUploadExpired(ctx context.Context, tx attachmentTx, uploadID string) (bool, error) {
	var expired bool
	if err := tx.QueryRow(ctx, `
		select expires_at <= transaction_timestamp()
		from public.attachment_uploads
		where upload_id = $1`, uploadID).Scan(&expired); err != nil {
		return false, fmt.Errorf("read attachment upload expiry: %w", err)
	}
	return expired, nil
}

func attachmentUploadReservationExists(ctx context.Context, tx attachmentTx, uploadID string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from public.attachment_uploads where upload_id = $1
		)`, uploadID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check attachment upload reservation replay: %w", err)
	}
	return exists, nil
}

func ensureAttachmentUploadPart(
	ctx context.Context,
	tx attachmentTx,
	uploadID string,
	object attachments.ObjectVersion,
) error {
	if _, err := tx.Exec(ctx, `lock table public.attachment_upload_parts in row exclusive mode`); err != nil {
		return fmt.Errorf("lock attachment upload parts for publication: %w", err)
	}
	activeDeletion, err := activeBlobGCDeletionExists(ctx, tx, object.Key, object.VersionID)
	if err != nil {
		return err
	}
	if activeDeletion {
		return attachments.ErrBlobGCProtected
	}
	inserted, err := tx.Exec(ctx, `
		insert into public.attachment_upload_parts (
			upload_id, part_number, size_bytes, sha256_digest, object_version
		)
		select $1, 1, $2, $3, $4
		where not exists (
			select 1 from public.blob_gc_deletions as deletion
			where deletion.blob_key = $5 and deletion.object_version = $4
			  and deletion.deletion_state <> 'completed'
		)
		on conflict (upload_id, part_number) do nothing`,
		uploadID, object.SizeBytes, object.SHA256[:], object.VersionID, object.Key,
	)
	if err != nil {
		return mapAttachmentWriteError("record attachment upload final identity", err)
	}
	if inserted.RowsAffected() == 1 {
		return nil
	}
	if inserted.RowsAffected() != 0 {
		return attachments.ErrAttachmentConflict
	}
	activeDeletion, err = activeBlobGCDeletionExists(ctx, tx, object.Key, object.VersionID)
	if err != nil {
		return err
	}
	if activeDeletion {
		return attachments.ErrBlobGCProtected
	}
	existing, err := readAttachmentUploadPart(ctx, tx, uploadID)
	if err != nil {
		return err
	}
	if existing != object {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func readAttachmentUploadPart(
	ctx context.Context,
	tx attachmentTx,
	uploadID string,
) (attachments.ObjectVersion, error) {
	object, found, err := readOptionalAttachmentUploadPart(ctx, tx, uploadID)
	if err != nil {
		return attachments.ObjectVersion{}, err
	}
	if !found {
		return attachments.ObjectVersion{}, attachments.ErrAttachmentConflict
	}
	return object, nil
}

func lockAttachmentUploadPart(
	ctx context.Context,
	tx attachmentTx,
	uploadID string,
) (attachments.ObjectVersion, error) {
	if _, err := tx.Exec(ctx, `
		lock table public.attachment_upload_parts in share mode`); err != nil {
		return attachments.ObjectVersion{}, fmt.Errorf("lock attachment upload parts for bounded expiry: %w", err)
	}
	return readAttachmentUploadPart(ctx, tx, uploadID)
}

func readOptionalAttachmentUploadPart(
	ctx context.Context,
	tx attachmentTx,
	uploadID string,
) (attachments.ObjectVersion, bool, error) {
	var object attachments.ObjectVersion
	var digest []byte
	err := tx.QueryRow(ctx, `
		select size_bytes, sha256_digest, object_version
		from public.attachment_upload_parts
		where upload_id = $1 and part_number = 1`, uploadID).Scan(
		&object.SizeBytes, &digest, &object.VersionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ObjectVersion{}, false, nil
	}
	if err != nil {
		return attachments.ObjectVersion{}, false, fmt.Errorf("read attachment upload final identity: %w", err)
	}
	if len(digest) != len(object.SHA256) {
		return attachments.ObjectVersion{}, false, attachments.ErrAttachmentConflict
	}
	copy(object.SHA256[:], digest)
	object.Key = fmt.Sprintf("sha256/%x", object.SHA256)
	if object.Validate() != nil {
		return attachments.ObjectVersion{}, false, attachments.ErrAttachmentConflict
	}
	return object, true, nil
}

func newAttachmentUploadedContent(
	preparation attachments.UploadPreparation,
	object attachments.ObjectVersion,
) attachments.UploadedContent {
	return attachments.UploadedContent{
		ProjectID: preparation.ProjectID, UploadID: preparation.UploadID,
		AttachmentID: preparation.AttachmentID, DraftID: preparation.DraftID,
		AuthorID: preparation.AuthorID, State: preparation.State,
		TransportKind: preparation.TransportKind, MediaType: preparation.MediaType,
		ExpiresAt: preparation.ExpiresAt, TemporaryObjectKey: preparation.TemporaryObjectKey,
		TemporaryObjectVersion: preparation.TemporaryObjectVersion, Object: object,
	}
}

func attachmentUploadCompletionAndPartMatches(
	upload lockedAttachmentUpload,
	command attachments.CompleteUploadAndEnqueueCommand,
) bool {
	return upload.actualSizeBytes != nil && *upload.actualSizeBytes == command.ActualSizeBytes &&
		len(upload.actualSHA256) == len(command.ActualSHA256) &&
		bytes.Equal(upload.actualSHA256, command.ActualSHA256[:]) &&
		len(upload.completionFingerprint) == len(command.CompletionFingerprint) &&
		bytes.Equal(upload.completionFingerprint, command.CompletionFingerprint[:])
}

func verifyAttachmentProcessorJobReplay(
	ctx context.Context,
	tx attachmentTx,
	upload lockedAttachmentUpload,
	command attachments.CompleteUploadAndEnqueueCommand,
) error {
	var jobID, uploadID, attachmentID string
	var profile attachments.ProcessorProfile
	var maxAttempts int64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
		select processor_job_id, upload_id, attachment_id, processor_profile,
		       max_attempts, expires_at
		from public.attachment_processor_jobs
		where upload_id = $1`, upload.uploadID).Scan(
		&jobID, &uploadID, &attachmentID, &profile, &maxAttempts, &expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ErrAttachmentConflict
	}
	if err != nil {
		return fmt.Errorf("read attachment processor job replay: %w", err)
	}
	if jobID != command.ProcessorJobID || uploadID != upload.uploadID || attachmentID != upload.attachmentID ||
		profile != command.ProcessorProfile || maxAttempts != command.ProcessorMaxAttempts ||
		!expiresAt.Equal(command.ProcessorExpiresAt.UTC().Truncate(time.Microsecond)) {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func nullableAttachmentString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableAttachmentTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Truncate(time.Microsecond)
}

func nullableAttachmentProcessorResultCode(value *attachments.ProcessorResultCode) any {
	if value == nil {
		return nil
	}
	return *value
}

func isAttachmentConstraintConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
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

func lockExistingAttachmentProcessorQuotaAccount(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (attachments.QuotaUsage, int64, bool, error) {
	var usage attachments.QuotaUsage
	var quotaVersion int64
	err := tx.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts
		where project_id = $1
		for update`, projectID).Scan(
		&usage.LogicalBytes,
		&usage.ReservedBytes,
		&usage.PhysicalBytes,
		&quotaVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.QuotaUsage{}, 0, false, nil
	}
	if err != nil {
		return attachments.QuotaUsage{}, 0, false,
			fmt.Errorf("lock bounded attachment processor quota account: %w", err)
	}
	if usage.LogicalBytes < 0 || usage.ReservedBytes < 0 || usage.PhysicalBytes < 0 ||
		quotaVersion < 0 {
		return attachments.QuotaUsage{}, 0, false, attachments.ErrInvalidQuotaUsage
	}
	return usage, quotaVersion, true, nil
}

func lockExpiredAbandonedAttachmentUpload(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (lockedAttachmentUpload, attachments.UploadState, error) {
	var upload lockedAttachmentUpload
	var attachmentState attachments.UploadState
	err := tx.QueryRow(ctx, `
		select upload.project_id, upload.upload_id, upload.attachment_id,
		       upload.origin_draft_id, upload.author_id, upload.upload_state,
		       upload.declared_size_bytes, upload.reserved_size_bytes,
		       upload.actual_size_bytes, upload.actual_sha256_digest,
		       upload.temporary_object_key, upload.temporary_object_version,
		       upload.completion_fingerprint, attachment.attachment_state
		from public.attachment_uploads as upload
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		 and attachment.draft_id = upload.origin_draft_id
		where upload.project_id = $1
		  and upload.upload_state in ('created', 'uploading')
		  and upload.expires_at <= transaction_timestamp()
		  and not exists (
			select 1 from public.attachment_processor_jobs as job
			where job.upload_id = upload.upload_id
			  and job.attachment_id = upload.attachment_id
		  )
		order by upload.expires_at, upload.created_at, upload.upload_id
		for update of upload skip locked
		limit 1`, projectID).Scan(
		&upload.projectID,
		&upload.uploadID,
		&upload.attachmentID,
		&upload.originDraftID,
		&upload.authorID,
		&upload.state,
		&upload.declaredSizeBytes,
		&upload.reservedSizeBytes,
		&upload.actualSizeBytes,
		&upload.actualSHA256,
		&upload.temporaryObjectKey,
		&upload.temporaryObjectVersion,
		&upload.completionFingerprint,
		&attachmentState,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedAttachmentUpload{}, "", pgx.ErrNoRows
		}
		return lockedAttachmentUpload{}, "", fmt.Errorf("lock expired abandoned attachment upload: %w", err)
	}
	if upload.projectID != projectID || upload.declaredSizeBytes <= 0 || upload.reservedSizeBytes <= 0 {
		return lockedAttachmentUpload{}, "", attachments.ErrInvalidQuotaUsage
	}
	return upload, attachmentState, nil
}

func attachmentProcessorActiveJobExists(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from public.attachment_processor_jobs as job
			join public.attachment_uploads as upload
			  on upload.upload_id = job.upload_id
			 and upload.attachment_id = job.attachment_id
			where upload.project_id = $1
			  and job.processor_state in ('queued', 'retry_wait', 'claimed')
		)`, projectID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check active attachment processor job without quota account: %w", err)
	}
	return exists, nil
}

func lockBoundedAttachmentProcessorJob(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (attachmentProcessorJobSnapshot, error) {
	return scanAttachmentProcessorJob(tx.QueryRow(ctx, `
		select job.processor_job_id, job.upload_id, job.attachment_id,
		       job.processor_state, job.processor_profile, job.attempt,
		       job.max_attempts, job.owner_id, job.owner_generation,
		       job.lease_expires_at, job.retry_at, job.result_code,
		       job.result_digest, job.result_owner_id,
		       job.result_lease_expires_at, job.expires_at,
		       transaction_timestamp()
		from public.attachment_processor_jobs as job
		join public.attachment_uploads as upload
		  on upload.upload_id = job.upload_id
		 and upload.attachment_id = job.attachment_id
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = job.attachment_id
		where upload.project_id = $1
		  and job.processor_state in ('queued', 'retry_wait', 'claimed')
		  and (job.expires_at <= transaction_timestamp()
		       or job.attempt >= job.max_attempts)
		  and (job.processor_state <> 'claimed'
		       or job.lease_expires_at <= transaction_timestamp())
		  and upload.upload_state = 'quarantined'
		  and attachment.attachment_state = 'quarantined'
		  and exists (
			select 1
			from public.attachment_upload_parts as part
			where part.upload_id = job.upload_id and part.part_number = 1
		  )
		order by job.created_at, job.processor_job_id
		for update of job skip locked
		limit 1`, projectID), "lock bounded attachment processor job")
}

func validateBoundedAttachmentProcessorJob(job attachmentProcessorJobSnapshot) error {
	if attachments.ValidateProcessorJobID(job.processorJobID) != nil ||
		attachments.ValidateUploadID(job.uploadID) != nil ||
		attachments.ValidateAttachmentID(job.attachmentID) != nil ||
		job.attempt < 0 || job.maxAttempts <= 0 || job.ownerGeneration < 0 ||
		job.expiresAt.IsZero() || job.databaseNow.IsZero() ||
		(job.expiresAt.After(job.databaseNow) && job.attempt < job.maxAttempts) {
		return attachments.ErrAttachmentConflict
	}
	switch job.state {
	case attachments.ProcessorStateQueued:
		if job.ownerID != "" || job.leaseExpiresAt != nil || job.retryAt != nil ||
			job.resultCode != nil || len(job.resultDigest) != 0 || job.resultOwnerID != "" ||
			job.resultLeaseExpiresAt != nil {
			return attachments.ErrAttachmentConflict
		}
	case attachments.ProcessorStateClaimed:
		if job.ownerGeneration <= 0 || job.leaseExpiresAt == nil || job.retryAt != nil ||
			job.leaseExpiresAt.After(job.databaseNow) ||
			(attachments.ProcessorClaimInput{
				OwnerID: job.ownerID, OwnerLeaseDuration: time.Microsecond,
			}).Validate() != nil || job.resultCode != nil || len(job.resultDigest) != 0 ||
			job.resultOwnerID != "" || job.resultLeaseExpiresAt != nil {
			return attachments.ErrAttachmentConflict
		}
	case attachments.ProcessorStateRetryWait:
		if job.ownerID != "" || job.leaseExpiresAt != nil || job.retryAt == nil ||
			job.resultCode == nil || len(job.resultDigest) != sha256.Size ||
			job.resultLeaseExpiresAt == nil ||
			(attachments.ProcessorClaimInput{
				OwnerID: job.resultOwnerID, OwnerLeaseDuration: time.Microsecond,
			}).Validate() != nil {
			return attachments.ErrAttachmentConflict
		}
	default:
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func validateAttachmentProcessorUploadForBoundedExpiry(
	upload attachmentProcessorUploadSnapshot,
	job attachmentProcessorJobSnapshot,
	projectID string,
) error {
	if upload.upload.projectID != projectID || upload.upload.uploadID != job.uploadID ||
		upload.upload.attachmentID != job.attachmentID || upload.source.Validate() != nil ||
		upload.upload.state != attachments.UploadStateQuarantined ||
		upload.attachmentState != attachments.UploadStateQuarantined ||
		upload.blobKey != nil || upload.blobObjectVersion != nil ||
		upload.previewBlobKey != nil || upload.previewObjectVersion != nil ||
		upload.previewMediaType != nil || upload.previewSizeBytes != nil {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func buildAttachmentProcessorExpiryOutcome(
	job attachmentProcessorJobSnapshot,
	source attachments.BlobObject,
	systemOwnerID string,
) (attachmentProcessorExpiryOutcome, error) {
	outcome := attachmentProcessorExpiryOutcome{}
	if job.state == attachments.ProcessorStateRetryWait {
		if job.resultCode == nil || job.resultLeaseExpiresAt == nil {
			return attachmentProcessorExpiryOutcome{}, attachments.ErrAttachmentConflict
		}
		outcome.code = *job.resultCode
		outcome.resultOwnerID = job.resultOwnerID
		outcome.resultLeaseEnd = job.resultLeaseExpiresAt.UTC()
	} else {
		outcome.code = attachments.ProcessorResultCodeProcessingError
		if !job.expiresAt.After(job.databaseNow) {
			outcome.code = attachments.ProcessorResultCodeTimeout
		}
		if job.state == attachments.ProcessorStateClaimed {
			if job.leaseExpiresAt == nil {
				return attachmentProcessorExpiryOutcome{}, attachments.ErrAttachmentConflict
			}
			outcome.resultOwnerID = job.ownerID
			outcome.resultLeaseEnd = job.leaseExpiresAt.UTC()
		} else {
			outcome.resultOwnerID = systemOwnerID
			outcome.resultLeaseEnd = job.expiresAt.UTC()
		}
	}
	result := attachments.ProcessorResult{Source: source, Profile: job.profile, Code: outcome.code}
	digest, err := result.Digest()
	if err != nil || outcome.resultLeaseEnd.IsZero() ||
		(attachments.ProcessorClaimInput{
			OwnerID: outcome.resultOwnerID, OwnerLeaseDuration: time.Microsecond,
		}).Validate() != nil {
		return attachmentProcessorExpiryOutcome{}, attachments.ErrAttachmentConflict
	}
	outcome.digest = digest
	if job.state == attachments.ProcessorStateRetryWait &&
		!bytes.Equal(job.resultDigest, outcome.digest[:]) {
		return attachmentProcessorExpiryOutcome{}, attachments.ErrAttachmentConflict
	}
	return outcome, nil
}

func updateBoundedAttachmentProcessorJobExpiry(
	ctx context.Context,
	tx attachmentTx,
	job attachmentProcessorJobSnapshot,
	outcome attachmentProcessorExpiryOutcome,
) error {
	updated, err := tx.Exec(ctx, `
		update public.attachment_processor_jobs
		set processor_state = 'expired', owner_id = '', lease_expires_at = null,
		    retry_at = null, result_code = $17, result_digest = $18,
		    result_owner_id = $19, result_lease_expires_at = $20,
		    updated_at = transaction_timestamp()
		where processor_job_id = $1 and upload_id = $2 and attachment_id = $3
		  and processor_profile = $4 and processor_state = $5
		  and attempt = $6 and max_attempts = $7 and owner_id = $8
		  and owner_generation = $9
		  and lease_expires_at is not distinct from $10
		  and retry_at is not distinct from $11
		  and result_code is not distinct from $12
		  and result_digest is not distinct from $13
		  and result_owner_id = $14
		  and result_lease_expires_at is not distinct from $15
		  and expires_at = $16
		  and (expires_at <= transaction_timestamp() or attempt >= max_attempts)
		  and (processor_state <> 'claimed'
		       or lease_expires_at <= transaction_timestamp())`,
		job.processorJobID,
		job.uploadID,
		job.attachmentID,
		job.profile,
		job.state,
		job.attempt,
		job.maxAttempts,
		job.ownerID,
		job.ownerGeneration,
		nullableAttachmentTime(job.leaseExpiresAt),
		nullableAttachmentTime(job.retryAt),
		nullableAttachmentProcessorResultCode(job.resultCode),
		job.resultDigest,
		job.resultOwnerID,
		nullableAttachmentTime(job.resultLeaseExpiresAt),
		job.expiresAt.UTC().Truncate(time.Microsecond),
		outcome.code,
		outcome.digest[:],
		outcome.resultOwnerID,
		outcome.resultLeaseEnd.UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return mapAttachmentWriteError("expire bounded attachment processor job", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func preflightAttachmentProcessorCompletion(
	ctx context.Context,
	tx attachmentTx,
	claim attachments.ProcessorClaim,
) (attachmentProcessorJobSnapshot, error) {
	row := tx.QueryRow(ctx, `
		select processor_job_id, upload_id, attachment_id, processor_state,
		       processor_profile, attempt, max_attempts, owner_id,
		       owner_generation, lease_expires_at, retry_at,
		       result_code, result_digest, result_owner_id,
		       result_lease_expires_at, expires_at, transaction_timestamp()
		from public.attachment_processor_jobs
		where processor_job_id = $1
		  and (
			(processor_state = 'claimed'
			  and owner_id = $2
			  and owner_generation = $3
			  and lease_expires_at = $4
			  and lease_expires_at > transaction_timestamp()
			  and attempt = $5)
			or processor_state in ('retry_wait', 'succeeded', 'rejected', 'expired')
		  )`,
		claim.ProcessorJobID,
		claim.OwnerID,
		claim.OwnerGeneration,
		claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond),
		claim.Attempt,
	)
	job, err := scanAttachmentProcessorJob(row, "preflight attachment processor completion")
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentProcessorJobSnapshot{}, attachments.ErrProcessorClaimLost
	}
	return job, err
}

func loadAttachmentProcessorJob(
	ctx context.Context,
	tx attachmentTx,
	processorJobID string,
	forUpdate bool,
) (attachmentProcessorJobSnapshot, error) {
	query := `
		select processor_job_id, upload_id, attachment_id, processor_state,
		       processor_profile, attempt, max_attempts, owner_id,
		       owner_generation, lease_expires_at, retry_at,
		       result_code, result_digest, result_owner_id,
		       result_lease_expires_at, expires_at, transaction_timestamp()
		from public.attachment_processor_jobs
		where processor_job_id = $1`
	if forUpdate {
		query += " for update"
	} else {
		query += " for share"
	}
	job, err := scanAttachmentProcessorJob(
		tx.QueryRow(ctx, query, processorJobID),
		"load attachment processor job",
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentProcessorJobSnapshot{}, attachments.ErrProcessorClaimLost
	}
	return job, err
}

func scanAttachmentProcessorJob(row pgx.Row, operation string) (attachmentProcessorJobSnapshot, error) {
	var job attachmentProcessorJobSnapshot
	if err := row.Scan(
		&job.processorJobID,
		&job.uploadID,
		&job.attachmentID,
		&job.state,
		&job.profile,
		&job.attempt,
		&job.maxAttempts,
		&job.ownerID,
		&job.ownerGeneration,
		&job.leaseExpiresAt,
		&job.retryAt,
		&job.resultCode,
		&job.resultDigest,
		&job.resultOwnerID,
		&job.resultLeaseExpiresAt,
		&job.expiresAt,
		&job.databaseNow,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return attachmentProcessorJobSnapshot{}, pgx.ErrNoRows
		}
		return attachmentProcessorJobSnapshot{}, fmt.Errorf("%s: %w", operation, err)
	}
	job.expiresAt = job.expiresAt.UTC()
	job.databaseNow = job.databaseNow.UTC()
	if job.leaseExpiresAt != nil {
		value := job.leaseExpiresAt.UTC()
		job.leaseExpiresAt = &value
	}
	if job.retryAt != nil {
		value := job.retryAt.UTC()
		job.retryAt = &value
	}
	if job.resultLeaseExpiresAt != nil {
		value := job.resultLeaseExpiresAt.UTC()
		job.resultLeaseExpiresAt = &value
	}
	return job, nil
}

func validateAttachmentProcessorJobIdentity(
	job attachmentProcessorJobSnapshot,
	claim attachments.ProcessorClaim,
) error {
	if job.processorJobID != claim.ProcessorJobID || job.uploadID != claim.UploadID ||
		job.attachmentID != claim.AttachmentID || job.profile != claim.Profile ||
		job.maxAttempts != claim.MaxAttempts ||
		!job.expiresAt.Equal(claim.ExpiresAt.UTC().Truncate(time.Microsecond)) {
		return attachments.ErrAttachmentConflict
	}
	if job.attempt != claim.Attempt || job.ownerGeneration != claim.OwnerGeneration {
		return attachments.ErrProcessorClaimLost
	}
	return nil
}

func validateLiveAttachmentProcessorClaim(
	job attachmentProcessorJobSnapshot,
	claim attachments.ProcessorClaim,
) error {
	if err := validateAttachmentProcessorJobIdentity(job, claim); err != nil {
		return err
	}
	if job.state != attachments.ProcessorStateClaimed || job.ownerID != claim.OwnerID ||
		job.leaseExpiresAt == nil ||
		!job.leaseExpiresAt.Equal(claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond)) ||
		!job.leaseExpiresAt.After(job.databaseNow) {
		return attachments.ErrProcessorClaimLost
	}
	if job.retryAt != nil || job.resultCode != nil || job.resultDigest != nil ||
		job.resultOwnerID != "" || job.resultLeaseExpiresAt != nil {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func retryableAttachmentProcessorResult(code attachments.ProcessorResultCode) bool {
	return code == attachments.ProcessorResultCodeScannerUnavailable ||
		code == attachments.ProcessorResultCodeTimeout ||
		code == attachments.ProcessorResultCodeProcessingError
}

func readAttachmentProcessorCompletionReplay(
	ctx context.Context,
	tx attachmentTx,
	input attachments.ProcessorCompletionInput,
	resultDigest [sha256.Size]byte,
	preflight attachmentProcessorJobSnapshot,
) (attachments.ProcessorCompletionResult, error) {
	upload, err := loadAttachmentProcessorUpload(ctx, tx, input.Claim, false)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	job, err := loadAttachmentProcessorJob(ctx, tx, input.Claim.ProcessorJobID, false)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if err := validateAttachmentProcessorJobIdentity(job, input.Claim); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if job.state != preflight.state || job.attempt != preflight.attempt ||
		job.ownerGeneration != preflight.ownerGeneration {
		return attachments.ProcessorCompletionResult{}, attachments.ErrProcessorClaimLost
	}
	if err := validateAttachmentProcessorResultClaim(job, input.Claim); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}

	processorState, uploadState, err := attachmentProcessorCompletionStates(input, job)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if job.state != processorState || job.ownerID != "" || job.leaseExpiresAt != nil ||
		job.resultCode == nil || *job.resultCode != input.Result.Code ||
		len(job.resultDigest) != sha256.Size || !bytes.Equal(job.resultDigest, resultDigest[:]) {
		return attachments.ProcessorCompletionResult{}, attachments.ErrAttachmentConflict
	}
	if processorState == attachments.ProcessorStateRetryWait {
		if job.retryAt == nil || !job.retryAt.Equal(input.RetryAt) {
			return attachments.ProcessorCompletionResult{}, attachments.ErrAttachmentConflict
		}
	} else if job.retryAt != nil {
		return attachments.ProcessorCompletionResult{}, attachments.ErrAttachmentConflict
	}
	if err := validateAttachmentProcessorCompletionUpload(upload, input.Result, uploadState); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if processorState == attachments.ProcessorStateSucceeded {
		if err := verifyAttachmentProcessorBlob(ctx, tx, input.Result.Source); err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		if input.Result.HasPreview {
			if err := verifyAttachmentProcessorBlob(ctx, tx, input.Result.Preview.Blob); err != nil {
				return attachments.ProcessorCompletionResult{}, err
			}
		}
	}

	usage, err := readAttachmentProcessorQuotaAccount(ctx, tx, input.Claim.ProjectID)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload.upload, usage, input.Limits)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	return newAttachmentProcessorCompletionResult(
		input, processorState, uploadState, resultDigest, quota,
	), nil
}

func commitAttachmentProcessorResult(
	ctx context.Context,
	tx attachmentTx,
	input attachments.ProcessorCompletionInput,
	resultDigest [sha256.Size]byte,
	job attachmentProcessorJobSnapshot,
	upload attachmentProcessorUploadSnapshot,
	usage attachments.QuotaUsage,
	quotaVersion int64,
) (attachments.ProcessorCompletionResult, error) {
	processorState, uploadState, err := attachmentProcessorCompletionStates(input, job)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}

	nextUsage := usage
	updateQuota := false
	switch processorState {
	case attachments.ProcessorStateSucceeded:
		if quotaVersion == math.MaxInt64 {
			return attachments.ProcessorCompletionResult{}, attachments.ErrQuotaOverflow
		}
		physicalDelta := int64(0)
		sourceInserted, err := ensureAttachmentBlob(ctx, tx, input.Result.Source)
		if err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		if sourceInserted {
			physicalDelta = input.Result.Source.SizeBytes
		}
		if input.Result.HasPreview {
			previewInserted, err := consumeBlobPublicationForBlobObject(
				ctx, tx, input.Result.Preview.Blob, input.PreviewPublicationIntent,
			)
			if err != nil {
				return attachments.ProcessorCompletionResult{}, err
			}
			if previewInserted {
				if physicalDelta > math.MaxInt64-input.Result.Preview.Blob.SizeBytes {
					return attachments.ProcessorCompletionResult{}, attachments.ErrQuotaOverflow
				}
				physicalDelta += input.Result.Preview.Blob.SizeBytes
			}
		}
		nextUsage, err = usage.SolidifyReservation(
			upload.upload.reservedSizeBytes,
			input.Result.Source.SizeBytes,
			physicalDelta,
		)
		if err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		if err := commitCleanAttachmentProcessorUpload(ctx, tx, upload, input.Result); err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		updateQuota = true
	case attachments.ProcessorStateRejected, attachments.ProcessorStateExpired:
		if quotaVersion == math.MaxInt64 {
			return attachments.ProcessorCompletionResult{}, attachments.ErrQuotaOverflow
		}
		nextUsage, err = usage.ReleaseReservation(upload.upload.reservedSizeBytes)
		if err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		if err := updateAttachmentUploadStates(ctx, tx, upload.upload, uploadState); err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
		updateQuota = true
	case attachments.ProcessorStateRetryWait:
		// Retryable results retain both quarantined rows and the reservation.
	default:
		return attachments.ProcessorCompletionResult{}, attachments.ErrAttachmentConflict
	}

	if err := updateAttachmentProcessorJobResult(
		ctx, tx, input, resultDigest, processorState,
	); err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	if updateQuota {
		if err := updateAttachmentQuotaAccount(
			ctx, tx, input.Claim.ProjectID, quotaVersion, nextUsage,
		); err != nil {
			return attachments.ProcessorCompletionResult{}, err
		}
	}
	quota, err := buildAttachmentQuotaSnapshot(ctx, tx, upload.upload, nextUsage, input.Limits)
	if err != nil {
		return attachments.ProcessorCompletionResult{}, err
	}
	return newAttachmentProcessorCompletionResult(
		input, processorState, uploadState, resultDigest, quota,
	), nil
}

func validateAttachmentProcessorResultClaim(
	job attachmentProcessorJobSnapshot,
	claim attachments.ProcessorClaim,
) error {
	if job.resultOwnerID != claim.OwnerID || job.resultLeaseExpiresAt == nil ||
		!job.resultLeaseExpiresAt.Equal(claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond)) {
		return attachments.ErrProcessorClaimLost
	}
	return nil
}

func attachmentProcessorCompletionStates(
	input attachments.ProcessorCompletionInput,
	job attachmentProcessorJobSnapshot,
) (attachments.ProcessorState, attachments.UploadState, error) {
	switch input.Result.Code {
	case attachments.ProcessorResultCodeClean:
		return attachments.ProcessorStateSucceeded, attachments.UploadStateAvailable, nil
	case attachments.ProcessorResultCodeMalware, attachments.ProcessorResultCodeUnsafeContent:
		return attachments.ProcessorStateRejected, attachments.UploadStateRejected, nil
	case attachments.ProcessorResultCodeScannerUnavailable,
		attachments.ProcessorResultCodeTimeout,
		attachments.ProcessorResultCodeProcessingError:
		if job.attempt >= job.maxAttempts || !input.RetryAt.Before(job.expiresAt) {
			return attachments.ProcessorStateExpired, attachments.UploadStateExpired, nil
		}
		return attachments.ProcessorStateRetryWait, attachments.UploadStateQuarantined, nil
	default:
		return "", "", attachments.ErrInvalidProcessorCommand
	}
}

func validateAttachmentProcessorCompletionUpload(
	upload attachmentProcessorUploadSnapshot,
	result attachments.ProcessorResult,
	wantState attachments.UploadState,
) error {
	if upload.upload.state != wantState || upload.attachmentState != wantState {
		return attachments.ErrAttachmentConflict
	}
	if wantState != attachments.UploadStateAvailable {
		if upload.blobKey != nil || upload.blobObjectVersion != nil ||
			upload.previewBlobKey != nil || upload.previewObjectVersion != nil ||
			upload.previewMediaType != nil || upload.previewSizeBytes != nil {
			return attachments.ErrAttachmentConflict
		}
		return nil
	}
	if upload.blobKey == nil || *upload.blobKey != result.Source.Key ||
		upload.blobObjectVersion == nil || *upload.blobObjectVersion != result.Source.ObjectVersion {
		return attachments.ErrAttachmentConflict
	}
	if !result.HasPreview {
		if upload.previewBlobKey != nil || upload.previewObjectVersion != nil ||
			upload.previewMediaType != nil || upload.previewSizeBytes != nil {
			return attachments.ErrAttachmentConflict
		}
		return nil
	}
	if upload.previewBlobKey == nil || *upload.previewBlobKey != result.Preview.Blob.Key ||
		upload.previewObjectVersion == nil ||
		*upload.previewObjectVersion != result.Preview.Blob.ObjectVersion ||
		upload.previewMediaType == nil || *upload.previewMediaType != result.Preview.MediaType ||
		upload.previewSizeBytes == nil || *upload.previewSizeBytes != result.Preview.Blob.SizeBytes {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func verifyAttachmentProcessorBlob(
	ctx context.Context,
	tx attachmentTx,
	object attachments.BlobObject,
) error {
	var digest []byte
	var objectVersion string
	var sizeBytes int64
	var backendKind attachments.BackendKind
	err := tx.QueryRow(ctx, `
		select sha256_digest, object_version, size_bytes, backend_kind
		from public.blob_objects
		where blob_key = $1`, object.Key).Scan(
		&digest, &objectVersion, &sizeBytes, &backendKind,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ErrAttachmentConflict
	}
	if err != nil {
		return fmt.Errorf("verify attachment processor Blob: %w", err)
	}
	if len(digest) != sha256.Size || !bytes.Equal(digest, object.SHA256[:]) ||
		objectVersion != object.ObjectVersion || sizeBytes != object.SizeBytes ||
		backendKind != object.BackendKind {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func readAttachmentProcessorQuotaAccount(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (attachments.QuotaUsage, error) {
	var usage attachments.QuotaUsage
	var quotaVersion int64
	err := tx.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes, quota_version
		from public.attachment_quota_accounts
		where project_id = $1`, projectID).Scan(
		&usage.LogicalBytes,
		&usage.ReservedBytes,
		&usage.PhysicalBytes,
		&quotaVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.QuotaUsage{}, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.QuotaUsage{}, fmt.Errorf("read attachment processor quota account: %w", err)
	}
	if usage.LogicalBytes < 0 || usage.ReservedBytes < 0 || usage.PhysicalBytes < 0 || quotaVersion < 0 {
		return attachments.QuotaUsage{}, attachments.ErrInvalidQuotaUsage
	}
	return usage, nil
}

func commitCleanAttachmentProcessorUpload(
	ctx context.Context,
	tx attachmentTx,
	upload attachmentProcessorUploadSnapshot,
	result attachments.ProcessorResult,
) error {
	var previewBlobKey, previewObjectVersion, previewMediaType any
	var previewSizeBytes any
	if result.HasPreview {
		previewBlobKey = result.Preview.Blob.Key
		previewObjectVersion = result.Preview.Blob.ObjectVersion
		previewMediaType = result.Preview.MediaType
		previewSizeBytes = result.Preview.Blob.SizeBytes
	}
	updated, err := tx.Exec(ctx, `
		update public.record_attachments
		set attachment_state = $4, blob_key = $5, blob_object_version = $6,
		    preview_blob_key = $7, preview_blob_object_version = $8,
		    preview_media_type = $9, preview_size_bytes = $10,
		    updated_at = transaction_timestamp()
		where project_id = $1 and attachment_id = $2 and draft_id = $3
		  and attachment_state = $11
		  and blob_key is null and blob_object_version is null
		  and preview_blob_key is null and preview_blob_object_version is null
		  and preview_media_type is null and preview_size_bytes is null`,
		upload.upload.projectID,
		upload.upload.attachmentID,
		upload.upload.originDraftID,
		attachments.UploadStateAvailable,
		result.Source.Key,
		result.Source.ObjectVersion,
		previewBlobKey,
		previewObjectVersion,
		previewMediaType,
		previewSizeBytes,
		attachments.UploadStateQuarantined,
	)
	if err != nil {
		return mapAttachmentWriteError("commit clean attachment processor result", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrAttachmentConflict
	}
	return updateAttachmentUploadStateOnly(
		ctx, tx, upload.upload, attachments.UploadStateAvailable,
	)
}

func updateAttachmentProcessorJobResult(
	ctx context.Context,
	tx attachmentTx,
	input attachments.ProcessorCompletionInput,
	resultDigest [sha256.Size]byte,
	processorState attachments.ProcessorState,
) error {
	var retryAt any
	if processorState == attachments.ProcessorStateRetryWait {
		retryAt = input.RetryAt
	}
	updated, err := tx.Exec(ctx, `
		update public.attachment_processor_jobs
		set processor_state = $6, owner_id = '', lease_expires_at = null,
		    retry_at = $7, result_code = $8, result_digest = $9,
		    result_owner_id = $10, result_lease_expires_at = $12,
		    updated_at = transaction_timestamp()
		where processor_job_id = $1 and upload_id = $2 and attachment_id = $3
		  and processor_profile = $4 and attempt = $5
		  and processor_state = 'claimed' and owner_id = $10
		  and owner_generation = $11 and lease_expires_at = $12
		  and lease_expires_at > transaction_timestamp()
		  and max_attempts = $13 and expires_at = $14`,
		input.Claim.ProcessorJobID,
		input.Claim.UploadID,
		input.Claim.AttachmentID,
		input.Claim.Profile,
		input.Claim.Attempt,
		processorState,
		retryAt,
		input.Result.Code,
		resultDigest[:],
		input.Claim.OwnerID,
		input.Claim.OwnerGeneration,
		input.Claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond),
		input.Claim.MaxAttempts,
		input.Claim.ExpiresAt.UTC().Truncate(time.Microsecond),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.ErrProcessorClaimLost
	}
	if err != nil {
		return mapAttachmentWriteError("commit attachment processor job result", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrProcessorClaimLost
	}
	return nil
}

func newAttachmentProcessorCompletionResult(
	input attachments.ProcessorCompletionInput,
	processorState attachments.ProcessorState,
	uploadState attachments.UploadState,
	resultDigest [sha256.Size]byte,
	quota attachments.QuotaSnapshot,
) attachments.ProcessorCompletionResult {
	return attachments.ProcessorCompletionResult{
		ProjectID:       input.Claim.ProjectID,
		ProcessorJobID:  input.Claim.ProcessorJobID,
		UploadID:        input.Claim.UploadID,
		AttachmentID:    input.Claim.AttachmentID,
		ProcessorState:  processorState,
		UploadState:     uploadState,
		AttachmentState: uploadState,
		ResultCode:      input.Result.Code,
		ResultDigest:    resultDigest,
		Quota:           quota,
	}
}

func loadAttachmentProcessorUpload(
	ctx context.Context,
	tx attachmentTx,
	claim attachments.ProcessorClaim,
	forUpdate bool,
) (attachmentProcessorUploadSnapshot, error) {
	query := `
		select upload.project_id, upload.upload_id, upload.attachment_id,
		       upload.origin_draft_id, upload.author_id, upload.upload_state,
		       upload.declared_size_bytes, upload.reserved_size_bytes,
		       upload.actual_size_bytes, upload.actual_sha256_digest,
		       upload.temporary_object_key, upload.temporary_object_version,
		       upload.completion_fingerprint, upload.transport_kind,
		       attachment.attachment_state, attachment.record_id, attachment.draft_id,
		       attachment.blob_key, attachment.blob_object_version,
		       attachment.preview_blob_key, attachment.preview_blob_object_version,
		       attachment.preview_media_type, attachment.preview_size_bytes
		from public.attachment_uploads as upload
		join public.record_attachments as attachment
		  on attachment.project_id = upload.project_id
		 and attachment.attachment_id = upload.attachment_id
		where upload.project_id = $1
		  and upload.upload_id = $2
		  and upload.attachment_id = $3`
	if forUpdate {
		query += " for update of upload, attachment"
	} else {
		query += " for share of upload, attachment"
	}

	var snapshot attachmentProcessorUploadSnapshot
	var route attachmentUploadRoute
	var transport attachments.TransportKind
	var attachmentRecordID, attachmentDraftID *string
	err := tx.QueryRow(ctx, query, claim.ProjectID, claim.UploadID, claim.AttachmentID).Scan(
		&route.projectID,
		&route.uploadID,
		&route.attachmentID,
		&route.originDraftID,
		&route.authorID,
		&snapshot.upload.state,
		&snapshot.upload.declaredSizeBytes,
		&snapshot.upload.reservedSizeBytes,
		&snapshot.upload.actualSizeBytes,
		&snapshot.upload.actualSHA256,
		&snapshot.upload.temporaryObjectKey,
		&snapshot.upload.temporaryObjectVersion,
		&snapshot.upload.completionFingerprint,
		&transport,
		&snapshot.attachmentState,
		&attachmentRecordID,
		&attachmentDraftID,
		&snapshot.blobKey,
		&snapshot.blobObjectVersion,
		&snapshot.previewBlobKey,
		&snapshot.previewObjectVersion,
		&snapshot.previewMediaType,
		&snapshot.previewSizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentProcessorUploadSnapshot{}, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachmentProcessorUploadSnapshot{}, fmt.Errorf("load attachment processor upload: %w", err)
	}
	if route.projectID != claim.ProjectID || route.uploadID != claim.UploadID ||
		route.attachmentID != claim.AttachmentID || route.originDraftID == "" || route.authorID == "" ||
		attachmentRecordID != nil || attachmentDraftID == nil || *attachmentDraftID != route.originDraftID ||
		snapshot.upload.declaredSizeBytes <= 0 || snapshot.upload.reservedSizeBytes <= 0 {
		return attachmentProcessorUploadSnapshot{}, attachments.ErrAttachmentConflict
	}
	snapshot.upload.attachmentUploadRoute = route
	snapshot.upload.recordID = attachmentRecordID

	part, err := readAttachmentUploadPart(ctx, tx, route.uploadID)
	if err != nil {
		return attachmentProcessorUploadSnapshot{}, err
	}
	snapshot.source = attachments.BlobObject{
		Key: part.Key, SHA256: part.SHA256, ObjectVersion: part.VersionID,
		SizeBytes: part.SizeBytes,
	}
	switch transport {
	case attachments.TransportKindLocal:
		snapshot.source.BackendKind = attachments.BackendKindLocal
	case attachments.TransportKindS3:
		snapshot.source.BackendKind = attachments.BackendKindS3
	default:
		return attachmentProcessorUploadSnapshot{}, attachments.ErrAttachmentConflict
	}
	if snapshot.source.Validate() != nil || snapshot.upload.actualSizeBytes == nil ||
		*snapshot.upload.actualSizeBytes != snapshot.source.SizeBytes ||
		len(snapshot.upload.actualSHA256) != sha256.Size ||
		!bytes.Equal(snapshot.upload.actualSHA256, snapshot.source.SHA256[:]) {
		return attachmentProcessorUploadSnapshot{}, attachments.ErrAttachmentConflict
	}
	return snapshot, nil
}

func validateAttachmentProcessorUploadForClaim(
	upload attachmentProcessorUploadSnapshot,
	claim attachments.ProcessorClaim,
) error {
	if upload.upload.projectID != claim.ProjectID || upload.upload.uploadID != claim.UploadID ||
		upload.upload.attachmentID != claim.AttachmentID || upload.source != claim.Source {
		return attachments.ErrAttachmentConflict
	}
	if upload.upload.state != attachments.UploadStateQuarantined ||
		upload.attachmentState != attachments.UploadStateQuarantined ||
		upload.blobKey != nil || upload.blobObjectVersion != nil ||
		upload.previewBlobKey != nil || upload.previewObjectVersion != nil ||
		upload.previewMediaType != nil || upload.previewSizeBytes != nil {
		return attachments.ErrAttachmentConflict
	}
	return nil
}

func scanAttachmentProcessorClaim(row pgx.Row) (attachments.ProcessorClaim, error) {
	var claim attachments.ProcessorClaim
	var digest []byte
	if err := row.Scan(
		&claim.ProjectID,
		&claim.ProcessorJobID,
		&claim.UploadID,
		&claim.AttachmentID,
		&claim.DisplayName,
		&claim.DeclaredMediaType,
		&digest,
		&claim.Source.ObjectVersion,
		&claim.Source.SizeBytes,
		&claim.Source.BackendKind,
		&claim.Profile,
		&claim.Attempt,
		&claim.MaxAttempts,
		&claim.OwnerID,
		&claim.OwnerGeneration,
		&claim.LeaseExpiresAt,
		&claim.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return attachments.ProcessorClaim{}, pgx.ErrNoRows
		}
		return attachments.ProcessorClaim{}, fmt.Errorf("scan attachment processor claim: %w", err)
	}
	if len(digest) != sha256.Size {
		return attachments.ProcessorClaim{}, attachments.ErrAttachmentConflict
	}
	copy(claim.Source.SHA256[:], digest)
	claim.Source.Key = fmt.Sprintf("sha256/%x", digest)
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.UTC()
	claim.ExpiresAt = claim.ExpiresAt.UTC()
	if claim.Validate() != nil {
		return attachments.ProcessorClaim{}, attachments.ErrAttachmentConflict
	}
	return claim, nil
}

func ensureAttachmentBlob(ctx context.Context, tx attachmentTx, object attachments.BlobObject) (bool, error) {
	if _, err := tx.Exec(ctx, `lock table public.blob_objects in row exclusive mode`); err != nil {
		return false, fmt.Errorf("lock Blob metadata for publication: %w", err)
	}
	activeDeletion, err := activeBlobGCDeletionExists(ctx, tx, object.Key, object.ObjectVersion)
	if err != nil {
		return false, err
	}
	if activeDeletion {
		return false, attachments.ErrBlobGCProtected
	}
	inserted, err := tx.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		)
		select $1, $2, $3, $4, $5
		where not exists (
			select 1 from public.blob_gc_deletions as deletion
			where deletion.blob_key = $1 and deletion.object_version = $3
			  and deletion.deletion_state <> 'completed'
		)
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
	activeDeletion, err = activeBlobGCDeletionExists(ctx, tx, object.Key, object.ObjectVersion)
	if err != nil {
		return false, err
	}
	if activeDeletion {
		return false, attachments.ErrBlobGCProtected
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

func activeBlobGCDeletionExists(
	ctx context.Context,
	tx attachmentTx,
	blobKey string,
	objectVersion string,
) (bool, error) {
	var active bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from public.blob_gc_deletions as deletion
			where deletion.blob_key = $1 and deletion.object_version = $2
			  and deletion.deletion_state <> 'completed'
		)`, blobKey, objectVersion).Scan(&active); err != nil {
		return false, fmt.Errorf("check active Blob GC deletion fence: %w", err)
	}
	return active, nil
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
