package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
)

const blobGCReceiptDigestDomainV1 = "houfeng.attachments.blob-gc-receipt.v1"

type blobGCStoredDeletion struct {
	claim          attachments.BlobGCClaim
	state          string
	retryAt        *time.Time
	physicalResult *string
	receiptDigest  []byte
	completedAt    *time.Time
}

// ClaimBlobGC commits a durable deletion fence before any caller performs
// physical Blob I/O. The claim transaction removes blob_objects metadata so
// its foreign keys reject new logical references while deletion is retried.
func (repository *PostgresAttachmentRepository) ClaimBlobGC(
	ctx context.Context,
	request attachments.BlobGCClaimRequest,
) (*attachments.BlobGCClaim, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return nil, attachments.ErrInvalidBlobGCRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin durable Blob GC claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reclaimed, err := reclaimBlobGCDeletion(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if reclaimed != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit reclaimed Blob GC deletion: %w", err)
		}
		return reclaimed, nil
	}

	if _, err := tx.Exec(ctx, `lock table public.blob_objects in share row exclusive mode`); err != nil {
		return nil, fmt.Errorf("lock Blob metadata for durable GC claim: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.attachment_upload_parts in share row exclusive mode`); err != nil {
		return nil, fmt.Errorf("lock attachment upload parts for durable GC claim: %w", err)
	}
	candidate, err := lockDurableBlobGCCandidate(ctx, tx, request)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty durable Blob GC claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		delete from public.blob_gc_pins
		where blob_key = $1 and blob_object_version = $2
		  and expires_at <= transaction_timestamp()`,
		candidate.Object.Key, candidate.Object.ObjectVersion,
	); err != nil {
		return nil, fmt.Errorf("delete expired Blob GC pins before claim: %w", err)
	}

	newDeletionID := repository.newBlobGCDeletionID
	if newDeletionID == nil {
		newDeletionID = func() (string, error) { return ids.New("bgd") }
	}
	deletionID, err := newDeletionID()
	if err != nil {
		return nil, fmt.Errorf("create Blob GC deletion ID: %w", err)
	}
	claim, err := insertBlobGCDeletionClaim(ctx, tx, deletionID, request, candidate)
	if err != nil {
		return nil, err
	}
	deleted, err := tx.Exec(ctx, `
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
		    select 1 from public.record_revision_attachments as revision_ref
		    join public.record_attachments as attachment
		      on attachment.attachment_id = revision_ref.attachment_id
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
		      and pin.blob_object_version = blob.object_version)`,
		candidate.Object.Key,
		candidate.Object.ObjectVersion,
		candidate.Object.SHA256[:],
		candidate.Object.SizeBytes,
		candidate.Object.BackendKind,
	)
	if err != nil {
		return nil, mapAttachmentWriteError("delete Blob metadata behind durable GC fence", err)
	}
	if deleted.RowsAffected() != 1 {
		return nil, attachments.ErrBlobGCConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit durable Blob GC claim: %w", err)
	}
	return &claim, nil
}

func (repository *PostgresAttachmentRepository) CompleteBlobGC(
	ctx context.Context,
	request attachments.BlobGCCompletionRequest,
) (attachments.BlobGCPurgeResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.BlobGCPurgeResult{}, attachments.ErrInvalidBlobGCRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobGCPurgeResult{}, fmt.Errorf("begin Blob GC completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stored, err := lockBlobGCDeletion(ctx, tx, request.Claim.DeletionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.BlobGCPurgeResult{}, attachments.ErrBlobGCClaimLost
	}
	if err != nil {
		return attachments.BlobGCPurgeResult{}, err
	}
	if stored.claim != request.Claim {
		return attachments.BlobGCPurgeResult{}, attachments.ErrBlobGCClaimLost
	}
	if stored.state == "completed" {
		if !blobGCStoredReceiptMatches(stored, request.Receipt) {
			return attachments.BlobGCPurgeResult{}, attachments.ErrBlobGCConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.BlobGCPurgeResult{}, fmt.Errorf("commit Blob GC completion replay: %w", err)
		}
		return newBlobGCPurgeResult(stored.claim, request.Receipt), nil
	}
	if stored.state != "claimed" {
		return attachments.BlobGCPurgeResult{}, attachments.ErrBlobGCClaimLost
	}

	usage, quotaVersion, err := lockBlobGCQuotaAccount(ctx, tx, request.Claim.ProjectID)
	if err != nil {
		return attachments.BlobGCPurgeResult{}, err
	}
	if usage.PhysicalBytes < request.Claim.Candidate.Object.SizeBytes {
		return attachments.BlobGCPurgeResult{}, attachments.ErrInvalidQuotaUsage
	}
	receiptDigest := blobGCReceiptDigest(request.Claim, request.Receipt)
	physicalResult := blobGCPhysicalDeleteResult(request.Receipt)
	object := request.Claim.Candidate.Object
	updated, err := tx.Exec(ctx, `
		update public.blob_gc_deletions
		set deletion_state = 'completed', retry_at = null,
		    physical_delete_result = $7, receipt_digest = $8,
		    completed_at = transaction_timestamp(), updated_at = transaction_timestamp()
		where deletion_id = $1 and owner_id = $2 and owner_generation = $3
		  and lease_expires_at = $4 and lease_expires_at > transaction_timestamp()
		  and blob_key = $5 and object_version = $6
		  and sha256_digest = $9 and size_bytes = $10 and backend_kind = $11
		  and deletion_state = 'claimed'`,
		request.Claim.DeletionID,
		request.Claim.OwnerID,
		request.Claim.OwnerGeneration,
		request.Claim.LeaseExpiresAt.UTC(),
		object.Key,
		object.ObjectVersion,
		physicalResult,
		receiptDigest[:],
		object.SHA256[:],
		object.SizeBytes,
		object.BackendKind,
	)
	if err != nil {
		return attachments.BlobGCPurgeResult{}, fmt.Errorf("complete durable Blob GC deletion: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.BlobGCPurgeResult{}, attachments.ErrBlobGCClaimLost
	}
	usage.PhysicalBytes -= object.SizeBytes
	if err := updateAttachmentQuotaAccount(ctx, tx, request.Claim.ProjectID, quotaVersion, usage); err != nil {
		return attachments.BlobGCPurgeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobGCPurgeResult{}, fmt.Errorf("commit Blob GC completion: %w", err)
	}
	return newBlobGCPurgeResult(request.Claim, request.Receipt), nil
}

func (repository *PostgresAttachmentRepository) RetryBlobGC(
	ctx context.Context,
	request attachments.BlobGCRetryRequest,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.ErrInvalidBlobGCRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Blob GC retry scheduling: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	object := request.Claim.Candidate.Object
	updated, err := tx.Exec(ctx, `
		update public.blob_gc_deletions
		set deletion_state = 'retry_wait', retry_at = $5,
		    updated_at = transaction_timestamp()
		where deletion_id = $1 and owner_id = $2 and owner_generation = $3
		  and lease_expires_at = $4 and lease_expires_at > transaction_timestamp()
		  and deletion_state = 'claimed'
		  and blob_key = $6 and object_version = $7
		  and sha256_digest = $8 and size_bytes = $9 and backend_kind = $10`,
		request.Claim.DeletionID,
		request.Claim.OwnerID,
		request.Claim.OwnerGeneration,
		request.Claim.LeaseExpiresAt.UTC(),
		request.RetryAt.UTC(),
		object.Key,
		object.ObjectVersion,
		object.SHA256[:],
		object.SizeBytes,
		object.BackendKind,
	)
	if err != nil {
		return fmt.Errorf("schedule durable Blob GC retry: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return attachments.ErrBlobGCClaimLost
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Blob GC retry scheduling: %w", err)
	}
	return nil
}

func (repository *PostgresAttachmentRepository) ResolveBlobGC(
	ctx context.Context,
	request attachments.BlobGCResolveRequest,
) (*attachments.BlobGCPurgeResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return nil, attachments.ErrInvalidBlobGCRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin Blob GC completion resolution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	object := request.Claim.Candidate.Object
	stored, err := scanBlobGCStoredDeletion(tx.QueryRow(ctx, `
		select deletion.deletion_id, deletion.project_id, deletion.purge_mode,
		       deletion.blob_key, deletion.sha256_digest, deletion.object_version,
		       deletion.size_bytes, deletion.backend_kind, deletion.blob_created_at,
		       deletion.owner_id, deletion.owner_generation, deletion.attempt,
		       deletion.lease_expires_at, deletion.deletion_state, deletion.retry_at,
		       deletion.physical_delete_result, deletion.receipt_digest,
		       deletion.completed_at
		from public.blob_gc_deletions as deletion
		where deletion.deletion_id = $1
		  and deletion.blob_key = $2 and deletion.object_version = $3
		  and deletion.sha256_digest = $4 and deletion.size_bytes = $5
		  and deletion.backend_kind = $6 and deletion.deletion_state = 'completed'`,
		request.Claim.DeletionID,
		object.Key,
		object.ObjectVersion,
		object.SHA256[:],
		object.SizeBytes,
		object.BackendKind,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty Blob GC completion resolution: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stored.claim != request.Claim || !blobGCStoredReceiptMatches(stored, request.Receipt) {
		return nil, attachments.ErrBlobGCConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Blob GC completion resolution: %w", err)
	}
	result := newBlobGCPurgeResult(stored.claim, request.Receipt)
	return &result, nil
}

func reclaimBlobGCDeletion(
	ctx context.Context,
	tx attachmentTx,
	request attachments.BlobGCClaimRequest,
) (*attachments.BlobGCClaim, error) {
	exactFilter := ""
	args := []any{
		request.ProjectID, request.BackendKind, request.Mode,
		request.OwnerID, request.OwnerLeaseDuration.Microseconds(),
	}
	if request.Mode == attachments.BlobGCPurgeModePermanent {
		exactFilter = `
			  and deletion.blob_key = $6 and deletion.object_version = $7
			  and deletion.sha256_digest = $8 and deletion.size_bytes = $9`
		args = append(args, request.Object.Key, request.Object.ObjectVersion, request.Object.SHA256[:], request.Object.SizeBytes)
	}
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		with candidate as (
			select deletion.deletion_id
			from public.blob_gc_deletions as deletion
			where deletion.project_id = $1 and deletion.backend_kind = $2
			  and deletion.purge_mode = $3
			  and ((deletion.deletion_state = 'retry_wait'
			        and deletion.retry_at <= transaction_timestamp())
			    or (deletion.deletion_state = 'claimed'
			        and deletion.lease_expires_at <= transaction_timestamp()))
			  %s
			order by coalesce(deletion.retry_at, deletion.lease_expires_at), deletion.deletion_id
			for update skip locked
			limit 1
		)
		update public.blob_gc_deletions as deletion
		set deletion_state = 'claimed', owner_id = $4,
		    owner_generation = deletion.owner_generation + 1,
		    attempt = deletion.attempt + 1,
		    lease_expires_at = transaction_timestamp() + ($5 * interval '1 microsecond'),
		    retry_at = null, updated_at = transaction_timestamp()
		from candidate
		where deletion.deletion_id = candidate.deletion_id
		returning deletion.deletion_id, deletion.project_id, deletion.purge_mode,
		          deletion.blob_key, deletion.sha256_digest, deletion.object_version,
		          deletion.size_bytes, deletion.backend_kind, deletion.blob_created_at,
		          deletion.owner_id, deletion.owner_generation, deletion.attempt,
		          deletion.lease_expires_at`, exactFilter), args...)
	claim, err := scanBlobGCClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reclaim durable Blob GC deletion: %w", err)
	}
	if validateBlobGCClaimForRequest(claim, request) != nil {
		return nil, attachments.ErrBlobGCConflict
	}
	return &claim, nil
}

func lockDurableBlobGCCandidate(
	ctx context.Context,
	tx attachmentTx,
	request attachments.BlobGCClaimRequest,
) (attachments.BlobGCCandidate, error) {
	baseSQL := `
		select blob.blob_key, blob.sha256_digest, blob.object_version,
		       blob.size_bytes, blob.backend_kind, blob.created_at
		from public.blob_objects as blob
		where blob.backend_kind = $1
		  %s
		  and not exists (
		    select 1 from public.record_attachments as attachment
		    where (attachment.blob_key = blob.blob_key
		        and attachment.blob_object_version = blob.object_version)
		       or (attachment.preview_blob_key = blob.blob_key
		        and attachment.preview_blob_object_version = blob.object_version))
		  and not exists (
		    select 1 from public.record_revision_attachments as revision_ref
		    join public.record_attachments as attachment
		      on attachment.attachment_id = revision_ref.attachment_id
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
		      and pin.blob_object_version = blob.object_version
		      and pin.expires_at > transaction_timestamp())
		  and not exists (
		    select 1 from public.blob_gc_deletions as deletion
		    where deletion.blob_key = blob.blob_key
		      and deletion.object_version = blob.object_version
		      and deletion.deletion_state <> 'completed')
		  and not exists (
		    select 1 from public.blob_publication_intents as publication
		    where publication.blob_key = blob.blob_key
		      and publication.publication_state <> 'completed')
		order by blob.created_at, blob.blob_key
		limit 1`
	var row pgx.Row
	switch request.Mode {
	case attachments.BlobGCPurgeModeOrdinary:
		row = tx.QueryRow(ctx, fmt.Sprintf(baseSQL, `
			and blob.created_at <= $2
			and blob.created_at <= transaction_timestamp() - interval '24 hours'`),
			request.BackendKind, request.OrphanedBefore.UTC())
	case attachments.BlobGCPurgeModePermanent:
		row = tx.QueryRow(ctx, fmt.Sprintf(baseSQL, `
			and blob.blob_key = $2 and blob.object_version = $3
			and blob.sha256_digest = $4 and blob.size_bytes = $5`),
			request.BackendKind, request.Object.Key, request.Object.ObjectVersion,
			request.Object.SHA256[:], request.Object.SizeBytes)
	default:
		return attachments.BlobGCCandidate{}, attachments.ErrInvalidBlobGCRequest
	}
	var candidate attachments.BlobGCCandidate
	var digest []byte
	if err := row.Scan(
		&candidate.Object.Key,
		&digest,
		&candidate.Object.ObjectVersion,
		&candidate.Object.SizeBytes,
		&candidate.Object.BackendKind,
		&candidate.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return attachments.BlobGCCandidate{}, pgx.ErrNoRows
		}
		return attachments.BlobGCCandidate{}, fmt.Errorf("lock durable Blob GC candidate: %w", err)
	}
	if len(digest) != sha256.Size {
		return attachments.BlobGCCandidate{}, attachments.ErrBlobGCConflict
	}
	copy(candidate.Object.SHA256[:], digest)
	candidate.CreatedAt = candidate.CreatedAt.UTC()
	if candidate.Validate() != nil || candidate.Object.BackendKind != request.BackendKind ||
		(request.Mode == attachments.BlobGCPurgeModeOrdinary &&
			candidate.CreatedAt.After(request.OrphanedBefore.UTC())) ||
		(request.Mode == attachments.BlobGCPurgeModePermanent && candidate.Object != request.Object) {
		return attachments.BlobGCCandidate{}, attachments.ErrBlobGCConflict
	}
	return candidate, nil
}

func insertBlobGCDeletionClaim(
	ctx context.Context,
	tx attachmentTx,
	deletionID string,
	request attachments.BlobGCClaimRequest,
	candidate attachments.BlobGCCandidate,
) (attachments.BlobGCClaim, error) {
	object := candidate.Object
	claim, err := scanBlobGCClaim(tx.QueryRow(ctx, `
		insert into public.blob_gc_deletions (
			deletion_id, project_id, purge_mode, blob_key, sha256_digest,
			object_version, size_bytes, backend_kind, blob_created_at,
			deletion_state, owner_id, owner_generation, attempt, lease_expires_at
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			'claimed', $10, 1, 1,
			transaction_timestamp() + ($11 * interval '1 microsecond')
		)
		returning deletion_id, project_id, purge_mode, blob_key, sha256_digest,
		          object_version, size_bytes, backend_kind, blob_created_at,
		          owner_id, owner_generation, attempt, lease_expires_at`,
		deletionID,
		request.ProjectID,
		request.Mode,
		object.Key,
		object.SHA256[:],
		object.ObjectVersion,
		object.SizeBytes,
		object.BackendKind,
		candidate.CreatedAt.UTC(),
		request.OwnerID,
		request.OwnerLeaseDuration.Microseconds(),
	))
	if err != nil {
		return attachments.BlobGCClaim{}, mapAttachmentWriteError("insert durable Blob GC claim", err)
	}
	if claim.DeletionID != deletionID || claim.Candidate != candidate ||
		validateBlobGCClaimForRequest(claim, request) != nil {
		return attachments.BlobGCClaim{}, attachments.ErrBlobGCConflict
	}
	return claim, nil
}

func lockBlobGCDeletion(
	ctx context.Context,
	tx attachmentTx,
	deletionID string,
) (blobGCStoredDeletion, error) {
	stored, err := scanBlobGCStoredDeletion(tx.QueryRow(ctx, `
		select deletion.deletion_id, deletion.project_id, deletion.purge_mode,
		       deletion.blob_key, deletion.sha256_digest, deletion.object_version,
		       deletion.size_bytes, deletion.backend_kind, deletion.blob_created_at,
		       deletion.owner_id, deletion.owner_generation, deletion.attempt,
		       deletion.lease_expires_at, deletion.deletion_state, deletion.retry_at,
		       deletion.physical_delete_result, deletion.receipt_digest,
		       deletion.completed_at
		from public.blob_gc_deletions as deletion
		where deletion.deletion_id = $1
		for update`, deletionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return blobGCStoredDeletion{}, pgx.ErrNoRows
		}
		return blobGCStoredDeletion{}, fmt.Errorf("lock durable Blob GC deletion: %w", err)
	}
	return stored, nil
}

func scanBlobGCClaim(row pgx.Row) (attachments.BlobGCClaim, error) {
	var claim attachments.BlobGCClaim
	var digest []byte
	if err := row.Scan(
		&claim.DeletionID,
		&claim.ProjectID,
		&claim.Mode,
		&claim.Candidate.Object.Key,
		&digest,
		&claim.Candidate.Object.ObjectVersion,
		&claim.Candidate.Object.SizeBytes,
		&claim.Candidate.Object.BackendKind,
		&claim.Candidate.CreatedAt,
		&claim.OwnerID,
		&claim.OwnerGeneration,
		&claim.Attempt,
		&claim.LeaseExpiresAt,
	); err != nil {
		return attachments.BlobGCClaim{}, err
	}
	if len(digest) != sha256.Size {
		return attachments.BlobGCClaim{}, attachments.ErrBlobGCConflict
	}
	copy(claim.Candidate.Object.SHA256[:], digest)
	claim.Candidate.CreatedAt = claim.Candidate.CreatedAt.UTC()
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.UTC()
	if claim.Validate() != nil {
		return attachments.BlobGCClaim{}, attachments.ErrBlobGCConflict
	}
	return claim, nil
}

func scanBlobGCStoredDeletion(row pgx.Row) (blobGCStoredDeletion, error) {
	var stored blobGCStoredDeletion
	var digest []byte
	if err := row.Scan(
		&stored.claim.DeletionID,
		&stored.claim.ProjectID,
		&stored.claim.Mode,
		&stored.claim.Candidate.Object.Key,
		&digest,
		&stored.claim.Candidate.Object.ObjectVersion,
		&stored.claim.Candidate.Object.SizeBytes,
		&stored.claim.Candidate.Object.BackendKind,
		&stored.claim.Candidate.CreatedAt,
		&stored.claim.OwnerID,
		&stored.claim.OwnerGeneration,
		&stored.claim.Attempt,
		&stored.claim.LeaseExpiresAt,
		&stored.state,
		&stored.retryAt,
		&stored.physicalResult,
		&stored.receiptDigest,
		&stored.completedAt,
	); err != nil {
		return blobGCStoredDeletion{}, err
	}
	if len(digest) != sha256.Size {
		return blobGCStoredDeletion{}, attachments.ErrBlobGCConflict
	}
	copy(stored.claim.Candidate.Object.SHA256[:], digest)
	stored.claim.Candidate.CreatedAt = stored.claim.Candidate.CreatedAt.UTC()
	stored.claim.LeaseExpiresAt = stored.claim.LeaseExpiresAt.UTC()
	if stored.retryAt != nil {
		retryAt := stored.retryAt.UTC()
		stored.retryAt = &retryAt
	}
	if stored.completedAt != nil {
		completedAt := stored.completedAt.UTC()
		stored.completedAt = &completedAt
	}
	if stored.claim.Validate() != nil || !validBlobGCStoredState(stored) {
		return blobGCStoredDeletion{}, attachments.ErrBlobGCConflict
	}
	return stored, nil
}

func validBlobGCStoredState(stored blobGCStoredDeletion) bool {
	switch stored.state {
	case "claimed":
		return stored.retryAt == nil && stored.physicalResult == nil && len(stored.receiptDigest) == 0 && stored.completedAt == nil
	case "retry_wait":
		return stored.retryAt != nil && stored.physicalResult == nil && len(stored.receiptDigest) == 0 && stored.completedAt == nil
	case "completed":
		return stored.retryAt == nil && stored.physicalResult != nil &&
			(*stored.physicalResult == "deleted" || *stored.physicalResult == "already_absent") &&
			len(stored.receiptDigest) == sha256.Size && stored.completedAt != nil
	default:
		return false
	}
}

func validateBlobGCClaimForRequest(
	claim attachments.BlobGCClaim,
	request attachments.BlobGCClaimRequest,
) error {
	if claim.Validate() != nil || request.Validate() != nil || claim.ProjectID != request.ProjectID ||
		claim.Mode != request.Mode || claim.OwnerID != request.OwnerID ||
		claim.Candidate.Object.BackendKind != request.BackendKind {
		return attachments.ErrBlobGCConflict
	}
	switch request.Mode {
	case attachments.BlobGCPurgeModeOrdinary:
		if claim.Candidate.CreatedAt.After(request.OrphanedBefore.UTC()) {
			return attachments.ErrBlobGCConflict
		}
	case attachments.BlobGCPurgeModePermanent:
		if claim.Candidate.Object != request.Object {
			return attachments.ErrBlobGCConflict
		}
	default:
		return attachments.ErrBlobGCConflict
	}
	return nil
}

func lockBlobGCQuotaAccount(
	ctx context.Context,
	tx attachmentTx,
	projectID string,
) (attachments.QuotaUsage, int64, error) {
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
		return attachments.QuotaUsage{}, 0, attachments.ErrAttachmentConflict
	}
	if err != nil {
		return attachments.QuotaUsage{}, 0, fmt.Errorf("lock Blob GC quota account: %w", err)
	}
	if usage.LogicalBytes < 0 || usage.ReservedBytes < 0 || usage.PhysicalBytes < 0 || quotaVersion < 0 {
		return attachments.QuotaUsage{}, 0, attachments.ErrInvalidQuotaUsage
	}
	return usage, quotaVersion, nil
}

func blobGCStoredReceiptMatches(
	stored blobGCStoredDeletion,
	receipt attachments.DeletionReceipt,
) bool {
	if stored.state != "completed" || stored.physicalResult == nil || stored.completedAt == nil ||
		receipt.Version != storeObjectVersionFromBlob(stored.claim.Candidate.Object) {
		return false
	}
	if (*stored.physicalResult == "deleted") != receipt.Deleted {
		return false
	}
	wantDigest := blobGCReceiptDigest(stored.claim, receipt)
	return len(stored.receiptDigest) == len(wantDigest) &&
		string(stored.receiptDigest) == string(wantDigest[:])
}

func blobGCPhysicalDeleteResult(receipt attachments.DeletionReceipt) string {
	if receipt.Deleted {
		return "deleted"
	}
	return "already_absent"
}

func blobGCReceiptDigest(
	claim attachments.BlobGCClaim,
	receipt attachments.DeletionReceipt,
) [sha256.Size]byte {
	hasher := sha256.New()
	writeBlobGCDigestField(hasher, []byte(blobGCReceiptDigestDomainV1))
	writeBlobGCDigestField(hasher, []byte(claim.DeletionID))
	writeBlobGCDigestField(hasher, []byte(claim.ProjectID))
	writeBlobGCDigestField(hasher, []byte(claim.Mode))
	writeBlobGCDigestField(hasher, []byte(receipt.Version.Key))
	writeBlobGCDigestField(hasher, receipt.Version.SHA256[:])
	writeBlobGCDigestField(hasher, []byte(receipt.Version.VersionID))
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(receipt.Version.SizeBytes))
	writeBlobGCDigestField(hasher, size[:])
	writeBlobGCDigestField(hasher, []byte(claim.Candidate.Object.BackendKind))
	writeBlobGCDigestField(hasher, []byte(blobGCPhysicalDeleteResult(receipt)))
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeBlobGCDigestField(hasher hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write(value)
}

func storeObjectVersionFromBlob(object attachments.BlobObject) attachments.ObjectVersion {
	return attachments.ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion, SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}

func newBlobGCPurgeResult(
	claim attachments.BlobGCClaim,
	receipt attachments.DeletionReceipt,
) attachments.BlobGCPurgeResult {
	return attachments.BlobGCPurgeResult{
		DeletionID: claim.DeletionID, Candidate: claim.Candidate, Receipt: receipt,
	}
}
