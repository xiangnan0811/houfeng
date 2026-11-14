package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// LookupCompletedRecordCreate returns a content-free candidate only. The
// caller must still authorize the current record and historical revision and
// compare the reconstructed request fingerprint before returning any content.
// An absent claim is a point-in-time observation, not proof that an allocation
// or a concurrent create has never been started.
func (repository *PostgresRecordRepository) LookupCompletedRecordCreate(
	ctx context.Context, actor recordauth.ActorScope, key string,
) (records.CompletedCreateCandidate, bool, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return records.CompletedCreateCandidate{}, false, records.ErrInvalidRecordReadRequest
	}
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return records.CompletedCreateCandidate{}, false, fmt.Errorf("%w: actor", records.ErrInvalidRecordReadRequest)
	}
	claimKey := recordplatform.IdempotencyKey{
		ProjectID:     recordplatform.ProjectID(normalized.ProjectID),
		OperationKind: recordplatform.OperationKindRecordCreate,
		Key:           key,
	}
	if err := claimKey.Validate(); err != nil {
		return records.CompletedCreateCandidate{}, false, err
	}

	var candidate records.CompletedCreateCandidate
	var found bool
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var row observedIdempotencyRow
		var recordID, revisionProject, revisionAuthor, rootProject, currentRevision *string
		var revisionNo *int64
		var baseRevision *string
		err := transaction.tx.QueryRow(ctx, `
			select claims.request_fingerprint, claims.result_fingerprint, claims.status,
			       claims.owner_id, claims.owner_generation, claims.owner_expires_at, claims.expires_at,
			       revisions.record_id, revisions.project_id, revisions.author_id,
			       revisions.revision_no, revisions.base_revision_id,
			       roots.project_id, roots.current_revision_id
			from public.record_idempotency_keys claims
			left join public.record_revisions revisions
			  on revisions.revision_id = 'rrv_' || encode(claims.request_fingerprint, 'hex')
			left join public.records roots on roots.record_id = revisions.record_id
			where claims.project_id = $1 and claims.operation_kind = $2 and claims.idempotency_key = $3`,
			string(claimKey.ProjectID), string(claimKey.OperationKind), claimKey.Key,
		).Scan(&row.requestFingerprint, &row.resultFingerprint, &row.status,
			&row.ownerID, &row.ownerGeneration, &row.ownerExpiresAt, &row.expiresAt,
			&recordID, &revisionProject, &revisionAuthor, &revisionNo, &baseRevision,
			&rootProject, &currentRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read completed record create claim: %w", err)
		}
		claim, err := row.record(claimKey)
		if err != nil {
			return err
		}
		if err := claim.Validate(); err != nil {
			return err
		}
		switch claim.Status {
		case recordplatform.IdempotencyStatusInProgress:
			return recordplatform.ErrIdempotencyInProgress
		case recordplatform.IdempotencyStatusConflict:
			return recordplatform.ErrIdempotencyConflictState
		case recordplatform.IdempotencyStatusCompleted:
		default:
			return recordplatform.ErrInvalidIdempotencyRecord
		}
		if len(row.requestFingerprint) != 32 || !bytes.Equal(row.requestFingerprint, row.resultFingerprint) {
			return fmt.Errorf("%w: create request and result", recordplatform.ErrInvalidIdempotencyRecord)
		}
		var fingerprint [32]byte
		copy(fingerprint[:], row.requestFingerprint)
		if fingerprint == [32]byte{} {
			return fmt.Errorf("%w: create request", recordplatform.ErrInvalidIdempotencyRecord)
		}
		if recordID == nil || revisionProject == nil || revisionAuthor == nil || revisionNo == nil ||
			rootProject == nil || currentRevision == nil || !recordidentity.IsValid(*recordID) ||
			*revisionProject != string(normalized.ProjectID) || *rootProject != string(normalized.ProjectID) ||
			*revisionNo != 1 || baseRevision != nil || !records.ValidRevisionID(*currentRevision) {
			return records.ErrRecordRevisionConflict
		}
		if *revisionAuthor != normalized.UserID {
			return recordplatform.ErrIdempotencyKeyReused
		}
		if err := assertRecordReadFence(ctx, transaction.tx, *recordID); err != nil {
			return err
		}
		revisionID := "rrv_" + hex.EncodeToString(fingerprint[:])
		// The first lookup is an unlocked observation. Recheck its complete
		// content-free binding after obtaining the actual record read fence.
		var stillBound bool
		if err := transaction.tx.QueryRow(ctx, `
			select exists (
			  select 1 from public.record_idempotency_keys claims
			  join public.record_revisions revisions on revisions.revision_id = $4
			  join public.records roots on roots.record_id = revisions.record_id
			  where claims.project_id = $1 and claims.operation_kind = $2 and claims.idempotency_key = $3
			    and claims.status = 'completed' and claims.request_fingerprint = $5
			    and claims.result_fingerprint = $5 and claims.owner_id = ''
			    and claims.owner_generation = 0 and claims.owner_expires_at is null
			    and revisions.record_id = $6 and revisions.project_id = $1
			    and revisions.revision_no = 1 and revisions.base_revision_id is null
			    and revisions.author_id = $7 and roots.project_id = $1
			    and roots.current_revision_id is not null
			)`, string(claimKey.ProjectID), string(claimKey.OperationKind), claimKey.Key,
			revisionID, fingerprint[:], *recordID, normalized.UserID).Scan(&stillBound); err != nil {
			return fmt.Errorf("recheck completed record create: %w", err)
		}
		if !stillBound {
			return records.ErrRecordRevisionConflict
		}
		candidate = records.CompletedCreateCandidate{RecordID: *recordID, RevisionID: revisionID, RequestFingerprint: fingerprint}
		found = true
		return nil
	})
	if err != nil {
		return records.CompletedCreateCandidate{}, false, err
	}
	return candidate, found, nil
}
