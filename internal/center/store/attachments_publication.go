package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
)

const blobPublicationCleanupReceiptDigestDomainV1 = "houfeng.attachments.blob-publication-cleanup-receipt.v1"

func (repository *PostgresAttachmentRepository) PrepareBlobPublication(
	ctx context.Context,
	request attachments.BlobPublicationPrepareRequest,
) (attachments.BlobPublicationIntent, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.BlobPublicationIntent{}, attachments.ErrInvalidBlobPublicationRequest
	}
	request.PublishExpiresAt = request.PublishExpiresAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("begin Blob publication preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `lock table public.blob_objects in share row exclusive mode`); err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("lock Blob metadata for publication preparation: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.attachment_upload_parts in share row exclusive mode`); err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("lock attachment upload parts for publication preparation: %w", err)
	}
	activeGC, err := activeBlobGCDeletionForKeyExists(ctx, tx, request.Target.Key)
	if err != nil {
		return attachments.BlobPublicationIntent{}, err
	}
	if activeGC {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobGCProtected
	}

	newPublicationID := repository.newBlobPublicationID
	if newPublicationID == nil {
		newPublicationID = func() (string, error) { return ids.New("bpi") }
	}
	publicationID, err := newPublicationID()
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("create Blob publication ID: %w", err)
	}
	intent, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		insert into public.blob_publication_intents (
			publication_id, project_id, owner_kind, owner_id, owner_generation,
			blob_key, sha256_digest, size_bytes, backend_kind,
			publication_state, publish_expires_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'prepared', $10)
		on conflict (blob_key) where publication_state <> 'completed' do nothing
		returning publication_id, project_id, owner_kind, owner_id, owner_generation,
		          blob_key, sha256_digest, size_bytes, backend_kind,
		          coalesce(object_version, ''), publication_state, publish_expires_at`,
		publicationID,
		request.ProjectID,
		request.OwnerKind,
		request.OwnerID,
		request.OwnerGeneration,
		request.Target.Key,
		request.Target.SHA256[:],
		request.Target.SizeBytes,
		request.Target.BackendKind,
		request.PublishExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		var outcome *attachments.BlobPublicationCompletionOutcome
		intent, outcome, err = scanBlobPublicationIntentWithOutcome(tx.QueryRow(ctx, `
			select publication_id, project_id, owner_kind, owner_id, owner_generation,
			       blob_key, sha256_digest, size_bytes, backend_kind,
				       coalesce(object_version, ''), publication_state, publish_expires_at,
				       completion_outcome
				from public.blob_publication_intents
				where publication_state <> 'completed'
				  and project_id = $1 and owner_kind = $2 and owner_id = $3
				  and owner_generation = $4 and blob_key = $5 and sha256_digest = $6
				  and size_bytes = $7 and backend_kind = $8 and publish_expires_at = $9`,
			request.ProjectID,
			request.OwnerKind,
			request.OwnerID,
			request.OwnerGeneration,
			request.Target.Key,
			request.Target.SHA256[:],
			request.Target.SizeBytes,
			request.Target.BackendKind,
			request.PublishExpiresAt,
		))
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return attachments.BlobPublicationIntent{}, fmt.Errorf("read Blob publication preparation replay: %w", err)
			}
			if request.OwnerKind != attachments.BlobPublicationOwnerProcessorPreview {
				return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
			}
			intent, err = rebindProcessorPreviewPublication(ctx, tx, request)
			if err != nil {
				return attachments.BlobPublicationIntent{}, err
			}
		}
		if outcome != nil || !blobPublicationIntentMatchesPrepare(intent, request) ||
			(intent.State != attachments.BlobPublicationStatePrepared &&
				intent.State != attachments.BlobPublicationStatePublished) {
			return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
		}
	} else if err != nil {
		return attachments.BlobPublicationIntent{}, mapBlobPublicationWriteError("insert Blob publication intent", err)
	} else if intent.PublicationID != publicationID || !blobPublicationIntentMatchesPrepare(intent, request) ||
		intent.State != attachments.BlobPublicationStatePrepared {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}

	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("commit Blob publication preparation: %w", err)
	}
	return intent, nil
}

// rebindProcessorPreviewPublication fences a crashed processor generation
// before the replacement worker resumes the same content-addressed preview.
// The target and job deadline remain immutable; only a strictly newer claim
// generation may take over a still-publishable intent.
func rebindProcessorPreviewPublication(
	ctx context.Context,
	tx attachmentTx,
	request attachments.BlobPublicationPrepareRequest,
) (attachments.BlobPublicationIntent, error) {
	intent, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		update public.blob_publication_intents
		set owner_generation = $1
		where project_id = $2 and owner_kind = $3 and owner_id = $4
		  and blob_key = $5 and sha256_digest = $6 and size_bytes = $7
		  and backend_kind = $8 and publish_expires_at = $9
		  and owner_generation < $1
		  and publication_state in ('prepared', 'published')
		returning publication_id, project_id, owner_kind, owner_id, owner_generation,
		          blob_key, sha256_digest, size_bytes, backend_kind,
		          coalesce(object_version, ''), publication_state, publish_expires_at`,
		request.OwnerGeneration,
		request.ProjectID,
		request.OwnerKind,
		request.OwnerID,
		request.Target.Key,
		request.Target.SHA256[:],
		request.Target.SizeBytes,
		request.Target.BackendKind,
		request.PublishExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	if err != nil {
		return attachments.BlobPublicationIntent{}, mapBlobPublicationWriteError("rebind processor preview Blob publication", err)
	}
	if !blobPublicationIntentMatchesPrepare(intent, request) ||
		(intent.State != attachments.BlobPublicationStatePrepared &&
			intent.State != attachments.BlobPublicationStatePublished) {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	return intent, nil
}

func (repository *PostgresAttachmentRepository) RecordBlobPublicationVersion(
	ctx context.Context,
	request attachments.BlobPublicationVersionRequest,
) (attachments.BlobPublicationIntent, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.BlobPublicationIntent{}, attachments.ErrInvalidBlobPublicationRequest
	}
	request.Intent.PublishExpiresAt = request.Intent.PublishExpiresAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("begin Blob publication version record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	intent, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		update public.blob_publication_intents
		set object_version = $2, publication_state = 'published'
		where publication_id = $1 and publication_state = 'prepared'
		  and object_version is null
		  and project_id = $3 and owner_kind = $4 and owner_id = $5
		  and owner_generation = $6 and blob_key = $7 and sha256_digest = $8
		  and size_bytes = $9 and backend_kind = $10 and publish_expires_at = $11
		returning publication_id, project_id, owner_kind, owner_id, owner_generation,
		          blob_key, sha256_digest, size_bytes, backend_kind,
		          coalesce(object_version, ''), publication_state, publish_expires_at`,
		request.Intent.PublicationID,
		request.Object.VersionID,
		request.Intent.ProjectID,
		request.Intent.OwnerKind,
		request.Intent.OwnerID,
		request.Intent.OwnerGeneration,
		request.Intent.Target.Key,
		request.Intent.Target.SHA256[:],
		request.Intent.Target.SizeBytes,
		request.Intent.Target.BackendKind,
		request.Intent.PublishExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		intent, err = scanBlobPublicationIntent(tx.QueryRow(ctx, `
			select publication_id, project_id, owner_kind, owner_id, owner_generation,
			       blob_key, sha256_digest, size_bytes, backend_kind,
			       coalesce(object_version, ''), publication_state, publish_expires_at
			from public.blob_publication_intents
			where publication_id = $1 and publication_state = 'published'`,
			request.Intent.PublicationID,
		))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
			}
			return attachments.BlobPublicationIntent{}, fmt.Errorf("read Blob publication version replay: %w", err)
		}
	} else if err != nil {
		return attachments.BlobPublicationIntent{}, mapBlobPublicationWriteError("record Blob publication version", err)
	}
	expected := request.Intent
	expected.ObjectVersion = request.Object.VersionID
	expected.State = attachments.BlobPublicationStatePublished
	if !sameBlobPublicationIntent(intent, expected) {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("commit Blob publication version record: %w", err)
	}
	return intent, nil
}

// RecordBlobPublicationCleanupVersion persists the one exact object version
// observed by a cleanup owner.  It is deliberately a separate CAS from the
// publisher's prepared->published transition: once cleanup is claimed, the
// publisher can no longer resume, but a restart reconciler may still bind the
// resolver's observation before deleting the object.
func (repository *PostgresAttachmentRepository) RecordBlobPublicationCleanupVersion(
	ctx context.Context,
	request attachments.BlobPublicationCleanupVersionRequest,
) (attachments.BlobPublicationCleanupClaim, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.BlobPublicationCleanupClaim{}, attachments.ErrInvalidBlobPublicationRequest
	}
	request.Claim.ObservedLeaseExpiresAt = request.Claim.ObservedLeaseExpiresAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobPublicationCleanupClaim{}, fmt.Errorf("begin Blob publication cleanup version record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	claim, err := scanBlobPublicationCleanupClaim(tx.QueryRow(ctx, `
		update public.blob_publication_intents
		set object_version = $2
		where publication_id = $1 and cleanup_owner_id = $3
		  and cleanup_generation = $4 and attempt = $5
		  and cleanup_lease_expires_at = $6
		  and cleanup_lease_expires_at > transaction_timestamp()
		  and publication_state = 'cleanup_claimed'
		  and object_version is null
			  and project_id = $7 and owner_kind = $8 and owner_id = $9
			  and owner_generation = $10 and blob_key = $11 and sha256_digest = $12
			  and size_bytes = $13 and backend_kind = $14
			returning publication_id, project_id, owner_kind, owner_id, owner_generation,
		          blob_key, sha256_digest, size_bytes, backend_kind,
		          coalesce(object_version, ''), publication_state, publish_expires_at,
			          cleanup_owner_id, cleanup_generation, attempt, cleanup_lease_expires_at`,
		blobPublicationCleanupVersionArgs(request)...,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		var replay attachments.BlobPublicationCleanupClaim
		replay, replayErr := scanBlobPublicationCleanupClaim(tx.QueryRow(ctx, `
				select publication_id, project_id, owner_kind, owner_id, owner_generation,
				       blob_key, sha256_digest, size_bytes, backend_kind,
				       coalesce(object_version, ''), publication_state, publish_expires_at,
				       cleanup_owner_id, cleanup_generation, attempt, cleanup_lease_expires_at
				from public.blob_publication_intents
				where publication_id = $1 and object_version = $2
				  and cleanup_owner_id = $3 and cleanup_generation = $4 and attempt = $5
				  and cleanup_lease_expires_at = $6
				  and cleanup_lease_expires_at > transaction_timestamp()
				  and publication_state = 'cleanup_claimed'
				  and project_id = $7 and owner_kind = $8 and owner_id = $9
				  and owner_generation = $10 and blob_key = $11 and sha256_digest = $12
				  and size_bytes = $13 and backend_kind = $14`,
			blobPublicationCleanupVersionArgs(request)...))
		if replayErr != nil {
			if errors.Is(replayErr, pgx.ErrNoRows) {
				return attachments.BlobPublicationCleanupClaim{}, attachments.ErrBlobPublicationClaimLost
			}
			return attachments.BlobPublicationCleanupClaim{}, fmt.Errorf("read Blob publication cleanup version replay: %w", replayErr)
		}
		expected := request.Claim
		expected.Intent.ObjectVersion = request.Object.VersionID
		if !sameBlobPublicationCleanupClaim(replay, expected) {
			return attachments.BlobPublicationCleanupClaim{}, attachments.ErrBlobPublicationClaimLost
		}
		if err := tx.Commit(ctx); err != nil {
			return attachments.BlobPublicationCleanupClaim{}, fmt.Errorf("commit Blob publication cleanup version replay: %w", err)
		}
		return replay, nil
	}
	if err != nil {
		return attachments.BlobPublicationCleanupClaim{}, mapBlobPublicationWriteError("record Blob publication cleanup version", err)
	}
	expected := request.Claim
	expected.Intent.ObjectVersion = request.Object.VersionID
	if !sameBlobPublicationCleanupClaim(claim, expected) {
		return attachments.BlobPublicationCleanupClaim{}, attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobPublicationCleanupClaim{}, fmt.Errorf("commit Blob publication cleanup version: %w", err)
	}
	return claim, nil
}

func (repository *PostgresAttachmentRepository) ClaimBlobPublicationCleanup(
	ctx context.Context,
	request attachments.BlobPublicationCleanupClaimRequest,
) (*attachments.BlobPublicationCleanupClaim, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return nil, attachments.ErrInvalidBlobPublicationRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin Blob publication cleanup claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `lock table public.blob_objects in share row exclusive mode`); err != nil {
		return nil, fmt.Errorf("lock Blob metadata for publication cleanup claim: %w", err)
	}
	if _, err := tx.Exec(ctx, `lock table public.attachment_upload_parts in share row exclusive mode`); err != nil {
		return nil, fmt.Errorf("lock attachment upload parts for publication cleanup claim: %w", err)
	}
	reconciled, err := tx.Exec(ctx, `
		with candidate as (
			select publication.publication_id
			from public.blob_publication_intents as publication
			where publication.project_id = $1 and publication.backend_kind = $2
			  and publication.publication_state = 'published'
			  and publication.publish_expires_at <= transaction_timestamp()
			  and (exists (
			    select 1 from public.attachment_upload_parts as part
			    where part.sha256_digest = publication.sha256_digest
			      and part.object_version = publication.object_version
			      and part.size_bytes = publication.size_bytes
			  ) or exists (
			    select 1 from public.blob_objects as blob
			    where blob.blob_key = publication.blob_key
			      and blob.sha256_digest = publication.sha256_digest
			      and blob.object_version = publication.object_version
			      and blob.size_bytes = publication.size_bytes
			      and blob.backend_kind = publication.backend_kind
			  ))
			order by publication.publish_expires_at, publication.publication_id
			for update skip locked
			limit 1
		)
		update public.blob_publication_intents as publication
		set publication_state = 'completed', completion_outcome = 'consumed',
		    receipt_digest = publication.sha256_digest,
		    completed_at = transaction_timestamp()
		from candidate
		where publication.publication_id = candidate.publication_id`,
		request.ProjectID, request.BackendKind,
	)
	if err != nil {
		return nil, mapBlobPublicationWriteError("reconcile consumed Blob publication", err)
	}
	if reconciled.RowsAffected() == 1 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit consumed Blob publication reconciliation: %w", err)
		}
		return nil, nil
	}
	if reconciled.RowsAffected() != 0 {
		return nil, attachments.ErrBlobPublicationConflict
	}

	claim, err := scanBlobPublicationCleanupClaim(tx.QueryRow(ctx, `
		with candidate as (
			select publication.publication_id
			from public.blob_publication_intents as publication
			where publication.project_id = $1 and publication.backend_kind = $2
			  and (((publication.publication_state = 'prepared'
			      or publication.publication_state = 'published')
			    and publication.publish_expires_at <= transaction_timestamp())
			    or (publication.publication_state = 'retry_wait'
			      and publication.retry_at <= transaction_timestamp())
			    or (publication.publication_state = 'cleanup_claimed'
			      and publication.cleanup_lease_expires_at <= transaction_timestamp()))
			order by coalesce(publication.retry_at, publication.cleanup_lease_expires_at,
			                  publication.publish_expires_at), publication.publication_id
			for update skip locked
			limit 1
		)
		update public.blob_publication_intents as publication
		set cleanup_owner_id = $3, publication_state = 'cleanup_claimed',
		    cleanup_generation = publication.cleanup_generation + 1,
		    attempt = publication.attempt + 1,
		    cleanup_lease_expires_at = transaction_timestamp() + make_interval(secs => $4),
		    retry_at = null
		from candidate
		where publication.publication_id = candidate.publication_id
		returning publication.publication_id, publication.project_id,
		          publication.owner_kind, publication.owner_id, publication.owner_generation,
		          publication.blob_key, publication.sha256_digest, publication.size_bytes,
		          publication.backend_kind, coalesce(publication.object_version, ''),
		          publication.publication_state, publication.publish_expires_at,
		          publication.cleanup_owner_id, publication.cleanup_generation,
		          publication.attempt, publication.cleanup_lease_expires_at`,
		request.ProjectID,
		request.BackendKind,
		request.CleanupOwnerID,
		int64(request.OwnerLeaseDuration/time.Second),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty Blob publication cleanup claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, mapBlobPublicationWriteError("claim Blob publication cleanup", err)
	}
	if claim.Intent.ProjectID != request.ProjectID || claim.Intent.Target.BackendKind != request.BackendKind ||
		claim.CleanupOwnerID != request.CleanupOwnerID {
		return nil, attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Blob publication cleanup claim: %w", err)
	}
	return &claim, nil
}

func (repository *PostgresAttachmentRepository) RetryBlobPublicationCleanup(
	ctx context.Context,
	request attachments.BlobPublicationCleanupRetryRequest,
) error {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.ErrInvalidBlobPublicationRequest
	}
	request.Claim.ObservedLeaseExpiresAt = request.Claim.ObservedLeaseExpiresAt.UTC().Truncate(time.Microsecond)
	request.RetryAt = request.RetryAt.UTC().Truncate(time.Microsecond)
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Blob publication cleanup retry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	args := blobPublicationCleanupRetryArgs(request)
	updated, err := tx.Exec(ctx, `
		update public.blob_publication_intents
		set publication_state = 'retry_wait', retry_at = $6
		where publication_id = $1 and cleanup_owner_id = $2 and cleanup_generation = $3
		  and attempt = $4 and cleanup_lease_expires_at = $5
		  and cleanup_lease_expires_at > transaction_timestamp()
		  and publication_state = 'cleanup_claimed'
		  and project_id = $7 and owner_kind = $8 and owner_id = $9
		  and owner_generation = $10 and blob_key = $11 and sha256_digest = $12
		  and object_version is not distinct from $13
		  and size_bytes = $14 and backend_kind = $15`, args...)
	if err != nil {
		return mapBlobPublicationWriteError("schedule Blob publication cleanup retry", err)
	}
	if updated.RowsAffected() == 0 {
		var replay bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1 from public.blob_publication_intents
				where publication_id = $1 and cleanup_owner_id = $2 and cleanup_generation = $3
				  and attempt = $4 and cleanup_lease_expires_at = $5
				  and retry_at = $6 and publication_state = 'retry_wait'
				  and project_id = $7 and owner_kind = $8 and owner_id = $9
				  and owner_generation = $10 and blob_key = $11 and sha256_digest = $12
				  and object_version is not distinct from $13
				  and size_bytes = $14 and backend_kind = $15
			)`, args...).Scan(&replay); err != nil {
			return fmt.Errorf("read Blob publication cleanup retry replay: %w", err)
		}
		if !replay {
			return attachments.ErrBlobPublicationClaimLost
		}
	} else if updated.RowsAffected() != 1 {
		return attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Blob publication cleanup retry: %w", err)
	}
	return nil
}

func (repository *PostgresAttachmentRepository) CompleteBlobPublicationCleanup(
	ctx context.Context,
	request attachments.BlobPublicationCleanupCompletionRequest,
) (attachments.BlobPublicationCleanupResult, error) {
	if ctx == nil || repository == nil || repository.beginTx == nil || request.Validate() != nil {
		return attachments.BlobPublicationCleanupResult{}, attachments.ErrInvalidBlobPublicationRequest
	}
	result := newBlobPublicationCleanupResult(request)
	if result.ValidateAgainst(request) != nil {
		return attachments.BlobPublicationCleanupResult{}, attachments.ErrInvalidBlobPublicationRequest
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return attachments.BlobPublicationCleanupResult{}, fmt.Errorf("begin Blob publication cleanup completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	args := blobPublicationCleanupCompletionArgs(request)
	updated, err := tx.Exec(ctx, `
		update public.blob_publication_intents
		set publication_state = 'completed', completion_outcome = $6,
		    receipt_digest = $7, completed_at = transaction_timestamp(), retry_at = null
		where publication_id = $1 and cleanup_owner_id = $2 and cleanup_generation = $3
		  and attempt = $4 and cleanup_lease_expires_at = $5
		  and cleanup_lease_expires_at > transaction_timestamp()
		  and publication_state = 'cleanup_claimed'
		  and project_id = $8 and owner_kind = $9 and owner_id = $10
		  and owner_generation = $11 and blob_key = $12 and sha256_digest = $13
		  and object_version is not distinct from $14
		  and size_bytes = $15 and backend_kind = $16`, args...)
	if err != nil {
		return attachments.BlobPublicationCleanupResult{}, mapBlobPublicationWriteError("complete Blob publication cleanup", err)
	}
	if updated.RowsAffected() == 0 {
		var replay bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1 from public.blob_publication_intents
				where publication_id = $1 and cleanup_owner_id = $2 and cleanup_generation = $3
				  and attempt = $4 and cleanup_lease_expires_at = $5
				  and completion_outcome = $6 and receipt_digest = $7
				  and publication_state = 'completed'
				  and project_id = $8 and owner_kind = $9 and owner_id = $10
				  and owner_generation = $11 and blob_key = $12 and sha256_digest = $13
				  and object_version is not distinct from $14
				  and size_bytes = $15 and backend_kind = $16
			)`, args...).Scan(&replay); err != nil {
			return attachments.BlobPublicationCleanupResult{}, fmt.Errorf("read Blob publication completion replay: %w", err)
		}
		if !replay {
			return attachments.BlobPublicationCleanupResult{}, attachments.ErrBlobPublicationClaimLost
		}
	} else if updated.RowsAffected() != 1 {
		return attachments.BlobPublicationCleanupResult{}, attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobPublicationCleanupResult{}, fmt.Errorf("commit Blob publication cleanup completion: %w", err)
	}
	return result, nil
}

func consumeBlobPublicationForUploadPart(
	ctx context.Context,
	tx attachmentTx,
	uploadID string,
	object attachments.ObjectVersion,
	intent attachments.BlobPublicationIntent,
) error {
	if ctx == nil || tx == nil || intent.Validate() != nil ||
		intent.State != attachments.BlobPublicationStatePublished ||
		intent.OwnerKind != attachments.BlobPublicationOwnerUpload || intent.OwnerID != uploadID ||
		intent.ObjectVersion != object.VersionID || intent.Target.Key != object.Key ||
		intent.Target.SHA256 != object.SHA256 || intent.Target.SizeBytes != object.SizeBytes {
		return attachments.ErrInvalidBlobPublicationRequest
	}
	if err := ensureAttachmentUploadPart(ctx, tx, uploadID, object); err != nil {
		return err
	}
	return consumeExactBlobPublication(ctx, tx, intent)
}

func consumeBlobPublicationForBlobObject(
	ctx context.Context,
	tx attachmentTx,
	object attachments.BlobObject,
	intent attachments.BlobPublicationIntent,
) (bool, error) {
	expected, ok := intent.Object()
	if ctx == nil || tx == nil || !ok || expected != object ||
		intent.State != attachments.BlobPublicationStatePublished ||
		intent.OwnerKind != attachments.BlobPublicationOwnerProcessorPreview {
		return false, attachments.ErrInvalidBlobPublicationRequest
	}
	inserted, err := ensureAttachmentBlob(ctx, tx, object)
	if err != nil {
		return false, err
	}
	if err := consumeExactBlobPublication(ctx, tx, intent); err != nil {
		return false, err
	}
	return inserted, nil
}

func consumeExactBlobPublication(
	ctx context.Context,
	tx attachmentTx,
	intent attachments.BlobPublicationIntent,
) error {
	updated, err := tx.Exec(ctx, `
		update public.blob_publication_intents as publication
		set publication_state = 'completed', completion_outcome = 'consumed',
		    receipt_digest = publication.sha256_digest,
		    completed_at = transaction_timestamp()
		where publication_id = $1 and owner_kind = $2 and owner_id = $3
		  and owner_generation = $4 and blob_key = $5 and sha256_digest = $6
		  and object_version = $7 and size_bytes = $8 and backend_kind = $9
		  and publication_state = 'published'`,
		intent.PublicationID,
		intent.OwnerKind,
		intent.OwnerID,
		intent.OwnerGeneration,
		intent.Target.Key,
		intent.Target.SHA256[:],
		intent.ObjectVersion,
		intent.Target.SizeBytes,
		intent.Target.BackendKind,
	)
	if err != nil {
		return mapBlobPublicationWriteError("consume Blob publication intent", err)
	}
	if updated.RowsAffected() != 1 {
		var replay bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1 from public.blob_publication_intents
				where publication_id = $1 and owner_kind = $2 and owner_id = $3
				  and owner_generation = $4 and blob_key = $5 and sha256_digest = $6
				  and object_version = $7 and size_bytes = $8 and backend_kind = $9
				  and project_id = $10 and publish_expires_at = $11
				  and publication_state = 'completed'
				  and completion_outcome = 'consumed'
				  and receipt_digest = sha256_digest
			)`,
			intent.PublicationID,
			intent.OwnerKind,
			intent.OwnerID,
			intent.OwnerGeneration,
			intent.Target.Key,
			intent.Target.SHA256[:],
			intent.ObjectVersion,
			intent.Target.SizeBytes,
			intent.Target.BackendKind,
			intent.ProjectID,
			intent.PublishExpiresAt.UTC().Truncate(time.Microsecond),
		).Scan(&replay); err != nil {
			return fmt.Errorf("read consumed Blob publication replay: %w", err)
		}
		if !replay {
			return attachments.ErrBlobPublicationConflict
		}
	}
	return nil
}

func activeBlobGCDeletionForKeyExists(
	ctx context.Context,
	tx attachmentTx,
	blobKey string,
) (bool, error) {
	var active bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from public.blob_gc_deletions
			where blob_key = $1 and deletion_state <> 'completed'
		)`, blobKey).Scan(&active); err != nil {
		return false, fmt.Errorf("check active Blob GC key fence: %w", err)
	}
	return active, nil
}

func scanBlobPublicationIntent(row pgx.Row) (attachments.BlobPublicationIntent, error) {
	var intent attachments.BlobPublicationIntent
	var digest []byte
	if err := row.Scan(
		&intent.PublicationID,
		&intent.ProjectID,
		&intent.OwnerKind,
		&intent.OwnerID,
		&intent.OwnerGeneration,
		&intent.Target.Key,
		&digest,
		&intent.Target.SizeBytes,
		&intent.Target.BackendKind,
		&intent.ObjectVersion,
		&intent.State,
		&intent.PublishExpiresAt,
	); err != nil {
		return attachments.BlobPublicationIntent{}, err
	}
	if len(digest) != sha256.Size {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	copy(intent.Target.SHA256[:], digest)
	intent.PublishExpiresAt = intent.PublishExpiresAt.UTC()
	if intent.Validate() != nil {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	return intent, nil
}

func scanBlobPublicationIntentWithOutcome(
	row pgx.Row,
) (attachments.BlobPublicationIntent, *attachments.BlobPublicationCompletionOutcome, error) {
	var intent attachments.BlobPublicationIntent
	var digest []byte
	var outcome *attachments.BlobPublicationCompletionOutcome
	if err := row.Scan(
		&intent.PublicationID,
		&intent.ProjectID,
		&intent.OwnerKind,
		&intent.OwnerID,
		&intent.OwnerGeneration,
		&intent.Target.Key,
		&digest,
		&intent.Target.SizeBytes,
		&intent.Target.BackendKind,
		&intent.ObjectVersion,
		&intent.State,
		&intent.PublishExpiresAt,
		&outcome,
	); err != nil {
		return attachments.BlobPublicationIntent{}, nil, err
	}
	if len(digest) != sha256.Size {
		return attachments.BlobPublicationIntent{}, nil, attachments.ErrBlobPublicationConflict
	}
	copy(intent.Target.SHA256[:], digest)
	intent.PublishExpiresAt = intent.PublishExpiresAt.UTC()
	if intent.Validate() != nil {
		return attachments.BlobPublicationIntent{}, nil, attachments.ErrBlobPublicationConflict
	}
	return intent, outcome, nil
}

func scanBlobPublicationCleanupClaim(row pgx.Row) (attachments.BlobPublicationCleanupClaim, error) {
	var claim attachments.BlobPublicationCleanupClaim
	var digest []byte
	if err := row.Scan(
		&claim.Intent.PublicationID,
		&claim.Intent.ProjectID,
		&claim.Intent.OwnerKind,
		&claim.Intent.OwnerID,
		&claim.Intent.OwnerGeneration,
		&claim.Intent.Target.Key,
		&digest,
		&claim.Intent.Target.SizeBytes,
		&claim.Intent.Target.BackendKind,
		&claim.Intent.ObjectVersion,
		&claim.Intent.State,
		&claim.Intent.PublishExpiresAt,
		&claim.CleanupOwnerID,
		&claim.CleanupGeneration,
		&claim.Attempt,
		&claim.ObservedLeaseExpiresAt,
	); err != nil {
		return attachments.BlobPublicationCleanupClaim{}, err
	}
	if len(digest) != sha256.Size {
		return attachments.BlobPublicationCleanupClaim{}, attachments.ErrBlobPublicationConflict
	}
	copy(claim.Intent.Target.SHA256[:], digest)
	claim.Intent.PublishExpiresAt = claim.Intent.PublishExpiresAt.UTC()
	claim.ObservedLeaseExpiresAt = claim.ObservedLeaseExpiresAt.UTC()
	if claim.Validate() != nil {
		return attachments.BlobPublicationCleanupClaim{}, attachments.ErrBlobPublicationConflict
	}
	return claim, nil
}

func blobPublicationIntentMatchesPrepare(
	intent attachments.BlobPublicationIntent,
	request attachments.BlobPublicationPrepareRequest,
) bool {
	return intent.ProjectID == request.ProjectID && intent.OwnerKind == request.OwnerKind &&
		intent.OwnerID == request.OwnerID && intent.OwnerGeneration == request.OwnerGeneration &&
		intent.Target == request.Target && intent.PublishExpiresAt.Equal(request.PublishExpiresAt)
}

func sameBlobPublicationIntent(left, right attachments.BlobPublicationIntent) bool {
	return left.PublicationID == right.PublicationID && left.ProjectID == right.ProjectID &&
		left.OwnerKind == right.OwnerKind && left.OwnerID == right.OwnerID &&
		left.OwnerGeneration == right.OwnerGeneration && left.Target == right.Target &&
		left.ObjectVersion == right.ObjectVersion && left.State == right.State &&
		left.PublishExpiresAt.Equal(right.PublishExpiresAt)
}

func sameBlobPublicationCleanupClaim(
	left, right attachments.BlobPublicationCleanupClaim,
) bool {
	return sameBlobPublicationIntent(left.Intent, right.Intent) &&
		left.CleanupOwnerID == right.CleanupOwnerID &&
		left.CleanupGeneration == right.CleanupGeneration &&
		left.Attempt == right.Attempt &&
		left.ObservedLeaseExpiresAt.Equal(right.ObservedLeaseExpiresAt)
}

func blobPublicationCleanupRetryArgs(
	request attachments.BlobPublicationCleanupRetryRequest,
) []any {
	claim := request.Claim
	return []any{
		claim.Intent.PublicationID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		request.RetryAt,
		claim.Intent.ProjectID,
		claim.Intent.OwnerKind,
		claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration,
		claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:],
		nullableBlobPublicationObjectVersion(claim.Intent.ObjectVersion),
		claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	}
}

func blobPublicationCleanupVersionArgs(
	request attachments.BlobPublicationCleanupVersionRequest,
) []any {
	claim := request.Claim
	return []any{
		claim.Intent.PublicationID,
		request.Object.VersionID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		claim.Intent.ProjectID,
		claim.Intent.OwnerKind,
		claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration,
		claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:],
		claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	}
}

func blobPublicationCleanupCompletionArgs(
	request attachments.BlobPublicationCleanupCompletionRequest,
) []any {
	claim := request.Claim
	digest := blobPublicationCleanupReceiptDigest(claim.Intent, request.Outcome)
	return []any{
		claim.Intent.PublicationID,
		claim.CleanupOwnerID,
		claim.CleanupGeneration,
		claim.Attempt,
		claim.ObservedLeaseExpiresAt,
		request.Outcome,
		digest[:],
		claim.Intent.ProjectID,
		claim.Intent.OwnerKind,
		claim.Intent.OwnerID,
		claim.Intent.OwnerGeneration,
		claim.Intent.Target.Key,
		claim.Intent.Target.SHA256[:],
		nullableBlobPublicationObjectVersion(claim.Intent.ObjectVersion),
		claim.Intent.Target.SizeBytes,
		claim.Intent.Target.BackendKind,
	}
}

func nullableBlobPublicationObjectVersion(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func blobPublicationCleanupReceiptDigest(
	intent attachments.BlobPublicationIntent,
	outcome attachments.BlobPublicationCompletionOutcome,
) [sha256.Size]byte {
	encoded := make([]byte, 0, 512)
	appendField := func(value []byte) {
		encoded = binary.BigEndian.AppendUint64(encoded, uint64(len(value)))
		encoded = append(encoded, value...)
	}
	appendString := func(value string) { appendField([]byte(value)) }
	appendInt64 := func(value int64) {
		var field [8]byte
		binary.BigEndian.PutUint64(field[:], uint64(value))
		appendField(field[:])
	}
	appendString(blobPublicationCleanupReceiptDigestDomainV1)
	appendString(intent.PublicationID)
	appendString(intent.ProjectID)
	appendString(string(intent.OwnerKind))
	appendString(intent.OwnerID)
	appendInt64(intent.OwnerGeneration)
	appendString(intent.Target.Key)
	appendField(intent.Target.SHA256[:])
	appendString(intent.ObjectVersion)
	appendInt64(intent.Target.SizeBytes)
	appendString(string(intent.Target.BackendKind))
	appendString(string(outcome))
	return sha256.Sum256(encoded)
}

func newBlobPublicationCleanupResult(
	request attachments.BlobPublicationCleanupCompletionRequest,
) attachments.BlobPublicationCleanupResult {
	result := attachments.BlobPublicationCleanupResult{
		PublicationID: request.Claim.Intent.PublicationID,
		Outcome:       request.Outcome,
		Receipt:       request.Receipt,
	}
	if object, ok := request.Claim.Intent.Object(); ok {
		result.Object = object
	}
	return result
}

func mapBlobPublicationWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "23505" || postgresError.Code == "23514") {
		return fmt.Errorf("%w: %s", attachments.ErrBlobPublicationConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
