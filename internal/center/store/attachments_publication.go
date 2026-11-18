package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/records"
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
	newPublicationID := repository.newBlobPublicationID
	if newPublicationID == nil {
		newPublicationID = func() (string, error) { return ids.New("bpi") }
	}
	publicationID, err := newPublicationID()
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("create Blob publication ID: %w", err)
	}
	var claimOwner *string
	var claimExpiry *time.Time
	if request.OwnerKind == attachments.BlobPublicationOwnerProcessorPreview {
		claimOwner = &request.ClaimOwnerID
		expiry := request.ClaimExpiresAt.UTC().Truncate(time.Microsecond)
		claimExpiry = &expiry
	}
	command, err := json.Marshal(struct {
		PublicationID    string                               `json:"publication_id"`
		ProjectID        string                               `json:"project_id"`
		OwnerKind        attachments.BlobPublicationOwnerKind `json:"owner_kind"`
		OwnerID          string                               `json:"owner_id"`
		OwnerGeneration  int64                                `json:"owner_generation"`
		Key              string                               `json:"key"`
		SHA256           string                               `json:"sha256"`
		Size             int64                                `json:"size"`
		BackendKind      attachments.BackendKind              `json:"backend_kind"`
		NamespaceID      string                               `json:"namespace_id"`
		PublishExpiresAt time.Time                            `json:"publish_expires_at"`
		ClaimOwnerID     *string                              `json:"origin_owner_id"`
		ClaimExpiresAt   *time.Time                           `json:"origin_owner_expires_at"`
	}{publicationID, request.ProjectID, request.OwnerKind, request.OwnerID, request.OwnerGeneration,
		request.Target.Key, hex.EncodeToString(request.Target.SHA256[:]), request.Target.SizeBytes, request.Target.BackendKind,
		request.NamespaceID, request.PublishExpiresAt, claimOwner, claimExpiry})
	if err != nil {
		return attachments.BlobPublicationIntent{}, err
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("begin Blob publication preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	route := attachmentWriterRoute{namespaces: []string{request.NamespaceID}}
	if request.OwnerKind == attachments.BlobPublicationOwnerUpload {
		route.uploadIDs = []string{request.OwnerID}
	}
	if err := lockAttachmentWriterPrefix(ctx, tx, route); err != nil {
		return attachments.BlobPublicationIntent{}, err
	}
	intent, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		select publication_id,project_id,owner_kind,owner_id,owner_generation,
		       blob_key,sha256_digest,size_bytes,backend_kind,
		       coalesce(namespace_id::text,''),coalesce(obligation_id,''),
		       coalesce(object_version,''),publication_state,publish_expires_at
		from public.record_blob_publication_prepare($1)`, command))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "55000" && postgresError.Message == "blob key has active cleanup responsibility" {
			return attachments.BlobPublicationIntent{}, attachments.ErrBlobGCProtected
		}
		return attachments.BlobPublicationIntent{}, mapBlobPublicationWriteError("prepare Blob publication", err)
	}
	if !blobPublicationIntentMatchesPrepare(intent, request) ||
		(intent.State != attachments.BlobPublicationStatePrepared && intent.State != attachments.BlobPublicationStatePublished) {
		return attachments.BlobPublicationIntent{}, attachments.ErrBlobPublicationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("commit Blob publication preparation: %w", err)
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
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return attachments.BlobPublicationIntent{}, fmt.Errorf("begin Blob publication version record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAttachmentWriterPrefix(ctx, tx, attachmentWriterRoute{
		obligationIDs: []string{request.ObligationID}, namespaces: []string{request.Intent.NamespaceID},
	}); err != nil {
		return attachments.BlobPublicationIntent{}, err
	}

	command, err := json.Marshal(struct {
		PublicationID string `json:"publication_id"`
		ObligationID  string `json:"obligation_id"`
	}{request.Intent.PublicationID, request.ObligationID})
	if err != nil {
		return attachments.BlobPublicationIntent{}, err
	}
	intent, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		select publication_id, project_id, owner_kind, owner_id, owner_generation,
		       blob_key, sha256_digest, size_bytes, backend_kind,
		       coalesce(namespace_id::text, ''), coalesce(obligation_id, ''),
		       coalesce(object_version, ''), publication_state, publish_expires_at
		from public.record_blob_publication_bind($1)`, command))
	if err != nil {
		return attachments.BlobPublicationIntent{}, mapBlobPublicationWriteError("record Blob publication version", err)
	}
	expected := request.Intent
	expected.ObjectVersion = request.Object.VersionID
	expected.ObligationID = request.ObligationID
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
		          coalesce(namespace_id::text, ''), coalesce(obligation_id, ''),
		          coalesce(object_version, ''), publication_state, publish_expires_at,
			          cleanup_owner_id, cleanup_generation, attempt, cleanup_lease_expires_at`,
		blobPublicationCleanupVersionArgs(request)...,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		var replay attachments.BlobPublicationCleanupClaim
		replay, replayErr := scanBlobPublicationCleanupClaim(tx.QueryRow(ctx, `
				select publication_id, project_id, owner_kind, owner_id, owner_generation,
				       blob_key, sha256_digest, size_bytes, backend_kind,
			       coalesce(namespace_id::text, ''), coalesce(obligation_id, ''),
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
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
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

	// A prepared intent may have deduplicated onto an already retained object
	// without recording its version. Protect matching content across all versions
	// in that case; a bound intent protects only its exact version. Apply these
	// exclusions to initial claims, retries and expired-owner takeovers alike.
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
			  and not exists (
			    select 1 from public.attachment_upload_parts as part
			    where part.sha256_digest = publication.sha256_digest
			      and part.size_bytes = publication.size_bytes
			      and (publication.object_version is null or part.object_version = publication.object_version)
			  )
			  and not exists (
			    select 1 from public.blob_objects as blob
			    where blob.blob_key = publication.blob_key
			      and blob.sha256_digest = publication.sha256_digest
			      and blob.size_bytes = publication.size_bytes
			      and blob.backend_kind = publication.backend_kind
			      and (publication.object_version is null or blob.object_version = publication.object_version)
			  )
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
		          publication.backend_kind, coalesce(publication.namespace_id::text, ''), coalesce(publication.obligation_id, ''), coalesce(publication.object_version, ''),
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

func marshalProcessorPublicationCommand(input attachments.ProcessorCompletionInput, candidate processorClaimCandidate, source records.RevisionMutationWriteObligation) ([]byte, error) {
	var publicationID, mediaType *string
	if input.Result.HasPreview {
		publicationID = &input.PreviewPublicationIntent.PublicationID
		mediaType = &input.Result.Preview.MediaType
	}
	return json.Marshal(struct {
		JobID                string    `json:"job_id"`
		OwnerID              string    `json:"owner_id"`
		OwnerGeneration      int64     `json:"owner_generation"`
		LeaseExpiresAt       time.Time `json:"lease_expires_at"`
		Attempt              int64     `json:"attempt"`
		PreviewPublicationID *string   `json:"preview_publication_id"`
		PreviewMediaType     *string   `json:"preview_media_type"`
		SourceObligationID   string    `json:"source_obligation_id"`
		PreviewObligationID  any       `json:"preview_obligation_id"`
		SourceNamespaceID    any       `json:"source_namespace_id"`
		SourceIncarnationID  any       `json:"source_incarnation_id"`
		AttachmentID         string    `json:"attachment_id"`
		UploadID             string    `json:"upload_id"`
		DraftID              string    `json:"draft_id"`
		RecordID             any       `json:"record_id"`
	}{
		JobID: input.Claim.ProcessorJobID, OwnerID: input.Claim.OwnerID, OwnerGeneration: input.Claim.OwnerGeneration,
		LeaseExpiresAt: input.Claim.LeaseExpiresAt.UTC().Truncate(time.Microsecond), Attempt: input.Claim.Attempt,
		PreviewPublicationID: publicationID, PreviewMediaType: mediaType, SourceObligationID: source.ObligationID,
		PreviewObligationID: nullableAttachmentString(input.PreviewPublicationIntent.ObligationID),
		SourceNamespaceID:   nullableAttachmentString(source.NamespaceID), SourceIncarnationID: nullableAttachmentString(source.IncarnationID),
		AttachmentID: candidate.attachmentID, UploadID: candidate.uploadID, DraftID: candidate.draftID,
		RecordID: nullableAttachmentString(candidate.recordID),
	})
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
	command, err := json.Marshal(struct {
		PublicationID string `json:"publication_id"`
		ObligationID  string `json:"obligation_id"`
		UploadID      string `json:"upload_id"`
	}{intent.PublicationID, intent.ObligationID, uploadID})
	if err != nil {
		return err
	}
	consumed, err := scanBlobPublicationIntent(tx.QueryRow(ctx, `
		select publication_id,project_id,owner_kind,owner_id,owner_generation,
		       blob_key,sha256_digest,size_bytes,backend_kind,
		       coalesce(namespace_id::text,''),coalesce(obligation_id,''),
		       coalesce(object_version,''),publication_state,publish_expires_at
		from public.record_upload_part_publish($1)`, command))
	if err != nil {
		return mapBlobPublicationWriteError("publish upload part", err)
	}
	expected := intent
	expected.State = attachments.BlobPublicationStateCompleted
	if !sameBlobPublicationIntent(consumed, expected) {
		return attachments.ErrBlobPublicationConflict
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
		&intent.NamespaceID, &intent.ObligationID,
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
		&intent.NamespaceID, &intent.ObligationID,
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
		&claim.Intent.NamespaceID, &claim.Intent.ObligationID,
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
		intent.NamespaceID == request.NamespaceID &&
		intent.Target == request.Target && intent.PublishExpiresAt.Equal(request.PublishExpiresAt)
}

func sameBlobPublicationIntent(left, right attachments.BlobPublicationIntent) bool {
	return left.PublicationID == right.PublicationID && left.ProjectID == right.ProjectID &&
		left.OwnerKind == right.OwnerKind && left.OwnerID == right.OwnerID &&
		left.OwnerGeneration == right.OwnerGeneration && left.Target == right.Target &&
		left.NamespaceID == right.NamespaceID && left.ObligationID == right.ObligationID &&
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
	if errors.As(err, &postgresError) && postgresError.Code == "55000" {
		switch postgresError.Message {
		case "Blob metadata identity conflict":
			return fmt.Errorf("%w: %s", attachments.ErrAttachmentConflict, operation)
		case "upload write origin unavailable":
			return fmt.Errorf("%w: %s", attachments.ErrAttachmentConflict, operation)
		case "processor write claim unavailable":
			return fmt.Errorf("%w: %s", attachments.ErrProcessorClaimLost, operation)
		case "upload write owner unavailable":
			return fmt.Errorf("%w: %s", attachments.ErrAttachmentOwnerNotFound, operation)
		case "upload write owner expired":
			return fmt.Errorf("%w: %s", attachments.ErrUploadExpired, operation)
		}
	}
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "23505" || postgresError.Code == "23514" || postgresError.Code == "55000" || postgresError.Code == "P0002") {
		return fmt.Errorf("%w: %s", attachments.ErrBlobPublicationConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
