package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

var ErrRecordDeletionStoreUnavailable = errors.New("record deletion store unavailable")

const corePurgeReceiptDigestDomainV1 = "houfeng.record-deletion.core-purge-receipt.v1"

const (
	recordCoreHealthDigestDomainV1  = "houfeng.record-deletion.core-health.v1"
	recordCorePreviewDigestDomainV1 = "houfeng.record-deletion.core-preview.v1"
	recordCoreImpactDigestDomainV1  = "houfeng.record-deletion.core-impact.v1"
)

type PostgresRecordDeletionRepository struct {
	platform           *PostgresRecordPlatformRepository
	pool               *pgxpool.Pool
	portabilityCleanup atomic.Pointer[attachments.LocalCleanupCoordinator]
	newReservationID   func() (string, error)
	newOperationID     func() (string, error)
	newAuditID         func() (string, error)
}

var (
	_ recorddeletion.DeletionPreviewRepository  = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.DeletionWorkerRepository   = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.V2DeletionWorkerRepository = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.RecordCoreStore            = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.RecoveryStore              = (*PostgresRecordDeletionRepository)(nil)
)

func NewPostgresRecordDeletionRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordDeletionRepository {
	return &PostgresRecordDeletionRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate),
		pool:     pool,
		newReservationID: func() (string, error) {
			return ids.New("drs")
		},
		newOperationID: func() (string, error) {
			return ids.New("rpo")
		},
		newAuditID: func() (string, error) {
			return ids.New("rda")
		},
	}
}

const resolveDeletionPreviewSQL = `
	select reservation.reservation_id,
	       reservation.project_id,
	       reservation.object_kind,
	       reservation.object_id,
	       reservation.actor_scope_digest,
	       reservation.deletion_token_commitment,
	       reservation.request_fingerprint,
	       reservation.preview_binding_digest,
	       reservation.preview_witness_sequence,
	       reservation.preview_witness_entry_hash,
	       reservation.expires_at,
	       operation.operation_id is not null,
	       coalesce(operation.operation_id, ''),
	       coalesce(operation.reason_code, ''),
	       coalesce(operation.operation_state, ''),
	       case when operation.operation_id is null then 0 else reservation.fence_epoch end,
	       coalesce(operation.ledger_sequence, 0),
	       coalesce(operation.ledger_entry_hash, ''::bytea),
	       coalesce(operation.release_epoch, 0),
	       coalesce(operation.receipt_digest, ''::bytea)
	from public.deletion_reservations as reservation
	left join public.record_purge_operations as operation
	  on operation.reservation_id = reservation.reservation_id
	 and operation.project_id = reservation.project_id
	where reservation.reservation_id = $1
	  and reservation.project_id = $2
	  and reservation.object_kind = $3
	  and reservation.object_id = $4
	  and (
		operation.operation_id is not null
		or (reservation.state = 'previewed'
		  and reservation.expires_at > transaction_timestamp())
	  )`

const resolveRecordDeletionStatusSQL = `
	select operation.operation_id,
	       operation.reservation_id,
	       operation.project_id,
	       reservation.object_kind,
	       reservation.object_id,
	       operation.actor_id,
	       operation.reason_code,
	       operation.operation_state,
	       reservation.fence_epoch,
	       coalesce(operation.ledger_sequence, 0),
	       coalesce(operation.ledger_entry_hash, ''::bytea),
	       coalesce(operation.release_epoch, 0),
	       coalesce(operation.receipt_digest, ''::bytea)
	from public.record_purge_operations as operation
	join public.deletion_reservations as reservation
	  on reservation.reservation_id = operation.reservation_id
	 and reservation.project_id = operation.project_id
	where operation.operation_id = $1
	  and operation.project_id = $2
	  and reservation.project_id = operation.project_id`

func (repository *PostgresRecordDeletionRepository) CreatePreview(
	ctx context.Context,
	command recorddeletion.CreatePreviewCommand,
) (recorddeletion.StoredPreview, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	// New deletion issuance uses the V2 issuer. V1 is continuation-only.
	return recorddeletion.StoredPreview{}, recorddeletion.ErrDeletionSafetyUnavailable
}

func (repository *PostgresRecordDeletionRepository) ResolvePreview(
	ctx context.Context,
	lookup recorddeletion.PreviewLookup,
) (recorddeletion.StoredPreview, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	if err := lookup.Validate(); err != nil {
		return recorddeletion.StoredPreview{}, err
	}

	var stored recorddeletion.StoredPreview
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var reservationID string
		var projectID string
		var objectKind string
		var objectID string
		var actorScopeDigestBytes []byte
		var tokenCommitmentBytes []byte
		var requestFingerprintBytes []byte
		var bindingDigestBytes []byte
		var witnessSequence int64
		var witnessEntryHashBytes []byte
		var expiresAt time.Time
		var hasOperation bool
		var operationID string
		var reasonCode string
		var operationState string
		var fenceEpoch int64
		var ledgerSequence int64
		var ledgerEntryHashBytes []byte
		var releaseEpoch int64
		var receiptDigestBytes []byte
		if err := transaction.tx.QueryRow(ctx, resolveDeletionPreviewSQL,
			lookup.ReservationID,
			lookup.Object.ProjectID,
			lookup.Object.ObjectKind,
			lookup.Object.ObjectID,
		).Scan(
			&reservationID,
			&projectID,
			&objectKind,
			&objectID,
			&actorScopeDigestBytes,
			&tokenCommitmentBytes,
			&requestFingerprintBytes,
			&bindingDigestBytes,
			&witnessSequence,
			&witnessEntryHashBytes,
			&expiresAt,
			&hasOperation,
			&operationID,
			&reasonCode,
			&operationState,
			&fenceEpoch,
			&ledgerSequence,
			&ledgerEntryHashBytes,
			&releaseEpoch,
			&receiptDigestBytes,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionPreviewNotFound
			}
			return fmt.Errorf("resolve deletion preview: %w", err)
		}

		actorScopeDigest, ok := deletionStoreDigest(actorScopeDigestBytes)
		if !ok {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		tokenCommitment, ok := deletionStoreDigest(tokenCommitmentBytes)
		if !ok {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		bindingDigest, ok := deletionStoreDigest(bindingDigestBytes)
		if !ok || witnessSequence <= 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		witnessEntryHash, ok := deletionStoreDigest(witnessEntryHashBytes)
		if !ok {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		requestFingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(requestFingerprintBytes)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		stored = recorddeletion.StoredPreview{
			ReservationID: reservationID,
			Object: recordplatform.ObjectRef{
				ProjectID:  projectID,
				ObjectKind: objectKind,
				ObjectID:   objectID,
			},
			ActorScopeDigest:   actorScopeDigest,
			TokenCommitment:    tokenCommitment,
			RequestFingerprint: requestFingerprint,
			BindingDigest:      bindingDigest,
			WitnessHead: recorddeletion.WitnessHead{
				Sequence:  uint64(witnessSequence),
				EntryHash: witnessEntryHash,
			},
			ExpiresAt: expiresAt.UTC(),
		}
		if hasOperation {
			operation, err := scanDeletionOperation(
				operationID,
				stored.ReservationID,
				stored.Object,
				reasonCode,
				operationState,
				fenceEpoch,
				ledgerSequence,
				ledgerEntryHashBytes,
				releaseEpoch,
				receiptDigestBytes,
			)
			if err != nil {
				return err
			}
			stored.Operation = &operation
		} else if operationID != "" || reasonCode != "" || operationState != "" || fenceEpoch != 0 ||
			ledgerSequence != 0 || len(ledgerEntryHashBytes) != 0 || releaseEpoch != 0 || len(receiptDigestBytes) != 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := stored.Validate(); err != nil || stored.ReservationID != lookup.ReservationID || stored.Object != lookup.Object {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	if err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	return stored, nil
}

func (repository *PostgresRecordDeletionRepository) ResolveOperationStatus(
	ctx context.Context,
	projectID recordplatform.ProjectID,
	operationID string,
) (recorddeletion.DeletionOperationStatus, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.DeletionOperationStatus{}, ErrRecordDeletionStoreUnavailable
	}
	if recordplatform.ValidateProjectID(projectID) != nil || !validStoredRecordIdentity(operationID, "rpo_") {
		return recorddeletion.DeletionOperationStatus{}, recorddeletion.ErrDeletionOperationNotFound
	}

	var status recorddeletion.DeletionOperationStatus
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var storedOperationID string
		var reservationID string
		var storedProjectID string
		var objectKind string
		var objectID string
		var initiatorActorID string
		var reasonCode string
		var operationState string
		var fenceEpoch int64
		var ledgerSequence int64
		var ledgerEntryHashBytes []byte
		var releaseEpoch int64
		var receiptDigestBytes []byte
		if err := transaction.tx.QueryRow(ctx, resolveRecordDeletionStatusSQL, operationID, string(projectID)).Scan(
			&storedOperationID,
			&reservationID,
			&storedProjectID,
			&objectKind,
			&objectID,
			&initiatorActorID,
			&reasonCode,
			&operationState,
			&fenceEpoch,
			&ledgerSequence,
			&ledgerEntryHashBytes,
			&releaseEpoch,
			&receiptDigestBytes,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionStatusUnavailable
			}
			return fmt.Errorf("resolve record deletion status: %w", err)
		}
		operation, err := scanDeletionOperation(
			storedOperationID,
			reservationID,
			recordplatform.ObjectRef{ProjectID: storedProjectID, ObjectKind: objectKind, ObjectID: objectID},
			reasonCode,
			operationState,
			fenceEpoch,
			ledgerSequence,
			ledgerEntryHashBytes,
			releaseEpoch,
			receiptDigestBytes,
		)
		if err != nil || storedOperationID != operationID || storedProjectID != string(projectID) {
			return recorddeletion.ErrDeletionStatusUnavailable
		}
		status = recorddeletion.DeletionOperationStatus{
			Operation:        operation,
			InitiatorActorID: initiatorActorID,
		}
		if status.Validate() != nil {
			return recorddeletion.ErrDeletionStatusUnavailable
		}
		return nil
	})
	if err != nil {
		return recorddeletion.DeletionOperationStatus{}, err
	}
	return status, nil
}

func (repository *PostgresRecordDeletionRepository) ReservePreview(
	ctx context.Context,
	command recorddeletion.ReservePreviewCommand,
) (recorddeletion.DeletionOperation, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
}

type persistedRecordDeletionWork struct {
	operationID             string
	reservationID           string
	projectID               string
	operationState          string
	deploymentID            string
	actorID                 string
	reasonCode              string
	deletionContractVersion int64
	ledgerEntryType         string
	ledgerSequence          int64
	ledgerEntryHash         []byte
	releaseEpoch            int64
	receiptDigest           []byte
	retryFrom               string
	operationOwner          recordplatform.OwnerLease
	reservationState        string
	objectKind              string
	objectID                string
	tokenCommitment         []byte
	requestFingerprint      []byte
	fenceEpoch              int64
	reservationOwner        recordplatform.OwnerLease
	observedAt              time.Time
}

func (repository *PostgresRecordDeletionRepository) ClaimDeletionWork(
	ctx context.Context,
	input recorddeletion.DeletionWorkClaimInput,
) (*recorddeletion.ClaimedDeletionWork, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if input.OwnerLeaseDuration%time.Microsecond != 0 {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	body, err := encodeRecordPurgeCommand(struct {
		OwnerID                string `json:"owner_id"`
		OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
	}{input.OwnerID, input.OwnerLeaseDuration.Microseconds()})
	if err != nil {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	var claim *recorddeletion.ClaimedDeletionWork
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var row persistedRecordDeletionWork
		var operationOwnerGeneration, reservationOwnerGeneration int64
		var reservationOwnerExpiresAt *time.Time
		err := transaction.tx.QueryRow(ctx, `select * from public.record_deletion_claim_v1($1)`, body).Scan(
			&row.operationID, &row.reservationID, &row.projectID, &row.operationState,
			&row.deploymentID, &row.actorID, &row.reasonCode, &row.deletionContractVersion,
			&row.ledgerEntryType, &row.ledgerSequence, &row.ledgerEntryHash, &row.releaseEpoch,
			&row.receiptDigest, &row.retryFrom, &row.operationOwner.OwnerID,
			&operationOwnerGeneration, &row.operationOwner.ExpiresAt, &row.reservationState,
			&row.objectKind, &row.objectID, &row.tokenCommitment, &row.requestFingerprint,
			&row.fenceEpoch, &row.reservationOwner.OwnerID, &reservationOwnerGeneration,
			&reservationOwnerExpiresAt, &row.observedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim restricted record deletion work: %w", err)
		}
		if operationOwnerGeneration <= 0 || reservationOwnerGeneration < 0 || row.observedAt.IsZero() ||
			row.operationOwner.OwnerID != input.OwnerID || !row.operationOwner.ExpiresAt.After(row.observedAt) ||
			row.operationOwner.ExpiresAt.Nanosecond()%1000 != 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		row.operationOwner.Generation = uint64(operationOwnerGeneration)
		row.reservationOwner.Generation = uint64(reservationOwnerGeneration)
		if reservationOwnerExpiresAt != nil {
			row.reservationOwner.ExpiresAt = *reservationOwnerExpiresAt
		}
		compoundFence, err := row.requiresCompoundFence()
		if err != nil {
			return err
		}
		if compoundFence && row.operationOwner != row.reservationOwner {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		built, err := row.claim(row.operationOwner)
		if err != nil {
			return err
		}
		claim = &built
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// ClaimV2DeletionWork scans the V2 projection independently of the V1 ledger projection.
func (repository *PostgresRecordDeletionRepository) ClaimV2DeletionWork(ctx context.Context, input recorddeletion.DeletionWorkClaimInput) (*recorddeletion.ClaimedDeletionWork, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if input.OwnerLeaseDuration%time.Microsecond != 0 {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	body, err := encodeRecordPurgeCommand(struct {
		OwnerID                string `json:"owner_id"`
		OwnerLeaseMicroseconds int64  `json:"owner_lease_microseconds"`
	}{input.OwnerID, input.OwnerLeaseDuration.Microseconds()})
	if err != nil {
		return nil, ErrRecordDeletionStoreUnavailable
	}
	var claim *recorddeletion.ClaimedDeletionWork
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		claim, err = scanV2DeletionWork(transaction.tx.QueryRow(ctx, `select * from public.record_deletion_claim_v2($1)`, body), input.OwnerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// scanV2DeletionWork consumes only the fixed 20-column V2 claim projection.
func scanV2DeletionWork(row pgx.Row, expectedOwner string) (*recorddeletion.ClaimedDeletionWork, error) {
	var operationID, reservationID, projectID, state, recordID, deploymentID, actorID, reason string
	var epoch, version, generation, reservationGeneration, fenceGeneration int64
	var ownerID, reservationOwnerID, fenceOwnerID string
	var expiry, reservationExpiry, fenceExpiry, observed time.Time
	err := row.Scan(&operationID, &reservationID, &projectID, &state, &recordID, &epoch, &deploymentID, &actorID, &reason, &version,
		&ownerID, &generation, &expiry, &reservationOwnerID, &reservationGeneration, &reservationExpiry,
		&fenceOwnerID, &fenceGeneration, &fenceExpiry, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim restricted V2 record deletion work: %w", err)
	}
	if version != int64(recorddeletion.DeletionContractV2) || epoch <= 0 || generation <= 0 ||
		ownerID != expectedOwner || ownerID != reservationOwnerID || ownerID != fenceOwnerID ||
		generation != reservationGeneration || generation != fenceGeneration || !expiry.Equal(reservationExpiry) ||
		!expiry.Equal(fenceExpiry) || observed.IsZero() || !expiry.After(observed) || expiry.Nanosecond()%1000 != 0 {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	claim := recorddeletion.ClaimedDeletionWork{
		Operation: recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: operationID,
			ReservationID: reservationID, Object: recordplatform.ObjectRef{ProjectID: projectID, ObjectKind: "record", ObjectID: recordID},
			ReasonCode: recorddeletion.DeletionReasonCode(reason), State: recorddeletion.DeletionState(state), FenceEpoch: recordplatform.ContentEpoch(epoch)},
		Owner: recordplatform.OwnerLease{OwnerID: ownerID, Generation: uint64(generation), ExpiresAt: expiry.UTC()}, Stage: recorddeletion.DeletionWorkPurgeOnline,
	}
	if claim.Validate() != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return &claim, nil
}

type deletionV2OwnerEnvelope struct {
	OperationID            string `json:"operation_id"`
	ReservationID          string `json:"reservation_id"`
	RecordID               string `json:"record_id"`
	FenceEpoch             uint64 `json:"fence_epoch"`
	OwnerID                string `json:"owner_id"`
	OwnerGeneration        uint64 `json:"owner_generation"`
	ObservedOwnerExpiresAt string `json:"observed_owner_expires_at"`
}

func deletionV2Owner(claim recorddeletion.ClaimedDeletionWork) (deletionV2OwnerEnvelope, error) {
	if claim.Operation.ContractVersion != recorddeletion.DeletionContractV2 || claim.Validate() != nil ||
		uint64(claim.Operation.FenceEpoch) > math.MaxInt64 || claim.Owner.Generation > math.MaxInt64 || claim.Owner.ExpiresAt.Nanosecond()%1000 != 0 {
		return deletionV2OwnerEnvelope{}, ErrRecordDeletionStoreUnavailable
	}
	return deletionV2OwnerEnvelope{claim.Operation.OperationID, claim.Operation.ReservationID, claim.Operation.Object.ObjectID,
		uint64(claim.Operation.FenceEpoch), claim.Owner.OwnerID, claim.Owner.Generation, claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano)}, nil
}

// RenewV2DeletionWork is only for a stopped owner; callers must discard the old tuple.
func (repository *PostgresRecordDeletionRepository) RenewV2DeletionWork(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, duration time.Duration) (recorddeletion.ClaimedDeletionWork, error) {
	if ctx == nil || repository == nil || repository.platform == nil || duration%time.Microsecond != 0 ||
		(recorddeletion.DeletionWorkClaimInput{OwnerID: claim.Owner.OwnerID, OwnerLeaseDuration: duration}).Validate() != nil {
		return recorddeletion.ClaimedDeletionWork{}, ErrRecordDeletionStoreUnavailable
	}
	owner, err := deletionV2Owner(claim)
	if err != nil {
		return recorddeletion.ClaimedDeletionWork{}, err
	}
	body, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		OwnerLeaseMicroseconds int64 `json:"owner_lease_microseconds"`
	}{owner, duration.Microseconds()})
	if err != nil {
		return recorddeletion.ClaimedDeletionWork{}, ErrRecordDeletionStoreUnavailable
	}
	var renewed recorddeletion.ClaimedDeletionWork
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var ownerID string
		var generation int64
		var expiry, observed time.Time
		if err := transaction.tx.QueryRow(ctx, `select * from public.record_deletion_renew_v2($1)`, body).Scan(&ownerID, &generation, &expiry, &observed); err != nil {
			return fmt.Errorf("renew restricted V2 record deletion work: %w", err)
		}
		if ownerID != claim.Owner.OwnerID || generation <= 0 || uint64(generation) != claim.Owner.Generation ||
			observed.IsZero() || !expiry.After(observed) || expiry.Nanosecond()%1000 != 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		renewed = claim
		renewed.Owner.ExpiresAt = expiry.UTC()
		return nil
	})
	if err != nil {
		return recorddeletion.ClaimedDeletionWork{}, err
	}
	return renewed, nil
}

func (repository *PostgresRecordDeletionRepository) CompleteV2OnlinePurge(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, receipt recorddeletion.OnlinePurgeReceipt) error {
	if ctx == nil || repository == nil || repository.platform == nil || receipt.OperationID != claim.Operation.OperationID || receipt.ReceiptDigest == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	owner, err := deletionV2Owner(claim)
	if err != nil {
		return err
	}
	body, err := encodeRecordPurgeCommand(struct {
		deletionV2OwnerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{owner, hex.EncodeToString(receipt.ReceiptDigest[:])})
	if err != nil {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var applied bool
		if err := transaction.tx.QueryRow(ctx, `select public.record_deletion_finish_v2($1)`, body).Scan(&applied); err != nil {
			return fmt.Errorf("finish restricted V2 record deletion: %w", err)
		}
		if !applied {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
}

func (row persistedRecordDeletionWork) requiresCompoundFence() (bool, error) {
	state := recorddeletion.DeletionState(row.operationState)
	wantFenced := false
	switch state {
	case recorddeletion.DeletionStateProvisionalFenced,
		recorddeletion.DeletionStateLedgerCommitUnknown,
		recorddeletion.DeletionStateWitnessPending,
		recorddeletion.DeletionStateDeleteRequested,
		recorddeletion.DeletionStateReleasePending:
		wantFenced = true
	case recorddeletion.DeletionStateRetryRequired:
		wantFenced = recorddeletion.DeletionWorkStage(row.retryFrom) == recorddeletion.DeletionWorkPromotePermanentFence
	case recorddeletion.DeletionStateFencePropagating,
		recorddeletion.DeletionStateReadFenced,
		recorddeletion.DeletionStateOnlinePurging:
	default:
		return false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if wantFenced && row.reservationState != "fenced" || !wantFenced && row.reservationState != "committed" {
		return false, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return wantFenced, nil
}

func (row persistedRecordDeletionWork) claim(owner recordplatform.OwnerLease) (recorddeletion.ClaimedDeletionWork, error) {
	if row.deletionContractVersion != int64(recorddeletion.RecordDeletionContractVersionV1) ||
		row.ledgerSequence < 0 || row.releaseEpoch < 0 || row.fenceEpoch <= 0 || owner.Validate() != nil {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	tokenCommitment, ok := deletionStoreDigest(row.tokenCommitment)
	if !ok {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	fingerprint, err := recordPlatformFingerprint(row.requestFingerprint)
	if err != nil {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	operation, err := scanDeletionOperation(
		row.operationID,
		row.reservationID,
		recordplatform.ObjectRef{ProjectID: row.projectID, ObjectKind: row.objectKind, ObjectID: row.objectID},
		row.reasonCode,
		row.operationState,
		row.fenceEpoch,
		row.ledgerSequence,
		row.ledgerEntryHash,
		row.releaseEpoch,
		row.receiptDigest,
	)
	if err != nil {
		return recorddeletion.ClaimedDeletionWork{}, err
	}
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            recordplatform.DeploymentID(row.deploymentID),
		ProjectID:               recordplatform.ProjectID(row.projectID),
		OperationID:             row.operationID,
		ActorID:                 row.actorID,
		Object:                  operation.Object,
		TokenCommitment:         tokenCommitment,
		RequestFingerprint:      fingerprint,
		ReasonCode:              recorddeletion.DeletionReasonCode(row.reasonCode),
		DeletionContractVersion: uint64(row.deletionContractVersion),
	}
	if operation.State == recorddeletion.DeletionStateReleasePending {
		request.EntryType = recorddeletion.LedgerEntryAttemptNotCommitted
		request.DeletionContractVersion = 0
		request.ReleaseEpoch = operation.ReleaseEpoch
	}
	if request.Validate() != nil {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}

	claim := recorddeletion.ClaimedDeletionWork{Operation: operation, Owner: owner, Request: request}
	switch operation.State {
	case recorddeletion.DeletionStateProvisionalFenced:
		claim.Stage = recorddeletion.DeletionWorkAppendDeleteCommit
	case recorddeletion.DeletionStateLedgerCommitUnknown:
		claim.Stage = recorddeletion.DeletionWorkResolveDeleteCommit
	case recorddeletion.DeletionStateWitnessPending:
		claim.Stage = recorddeletion.DeletionWorkConfirmDeleteWitness
	case recorddeletion.DeletionStateDeleteRequested:
		claim.Stage = recorddeletion.DeletionWorkPromotePermanentFence
	case recorddeletion.DeletionStateFencePropagating:
		claim.Stage = recorddeletion.DeletionWorkPropagatePermanentFence
	case recorddeletion.DeletionStateReadFenced:
		claim.Stage = recorddeletion.DeletionWorkBeginOnlinePurge
	case recorddeletion.DeletionStateOnlinePurging:
		claim.Stage = recorddeletion.DeletionWorkPurgeOnline
	case recorddeletion.DeletionStateRetryRequired:
		claim.Stage = recorddeletion.DeletionWorkResolveRetry
		claim.RetryStage = recorddeletion.DeletionWorkStage(row.retryFrom)
	case recorddeletion.DeletionStateReleasePending:
		if row.ledgerSequence == 0 {
			claim.Stage = recorddeletion.DeletionWorkResolveNotCommitted
		} else {
			claim.Stage = recorddeletion.DeletionWorkConfirmNotCommittedWitness
		}
	default:
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}

	if row.ledgerSequence > 0 {
		if row.ledgerEntryType != string(request.EntryType) {
			return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		entryHash, ok := deletionStoreDigest(row.ledgerEntryHash)
		if !ok {
			return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		entry := recorddeletion.DeletionLedgerEntry{Request: request, Sequence: uint64(row.ledgerSequence), EntryHash: entryHash}
		if entry.Validate() != nil {
			return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if claim.Stage == recorddeletion.DeletionWorkConfirmDeleteWitness ||
			claim.Stage == recorddeletion.DeletionWorkConfirmNotCommittedWitness {
			claim.Entry = &entry
		}
	} else if row.ledgerEntryType != "" || len(row.ledgerEntryHash) != 0 {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := claim.Validate(); err != nil {
		return recorddeletion.ClaimedDeletionWork{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return claim, nil
}

const recordDeletionPermanentFenceAppliedSQL = `
	select exists (
		select 1
		from public.record_purge_operations as operation
		join public.deletion_reservations as reservation
		  on reservation.reservation_id = operation.reservation_id
		 and reservation.project_id = operation.project_id
		where operation.operation_id = $1
		  and operation.operation_state = $2
		  and operation.ledger_entry_type = 'delete_commit'
		  and operation.ledger_sequence = $3
		  and operation.ledger_entry_hash = $4
		  and operation.witness_proof_digest is not null
		  and operation.owner_id = $5
		  and operation.owner_generation = $6
		  and operation.owner_expires_at = $7
		  and reservation.project_id = $8
		  and reservation.object_kind = $9
		  and reservation.object_id = $10
		  and reservation.fence_epoch = $11
		  and reservation.state = 'committed'
		  and not exists (
			select 1
			from public.deletion_fence_leases as fence
			where fence.project_id = reservation.project_id
			  and fence.object_kind = reservation.object_kind
			  and fence.object_id = reservation.object_id
			  and fence.expires_at > transaction_timestamp()
		  )
		  and not exists (
			select 1
			from public.object_content_leases as content_lease
			where content_lease.project_id = reservation.project_id
			  and content_lease.object_kind = reservation.object_kind
			  and content_lease.object_id = reservation.object_id
			  and content_lease.expires_at > transaction_timestamp()
		  )
	) as permanent_fence_applied`

func (repository *PostgresRecordDeletionRepository) MarkDeleteCommitUnknown(ctx context.Context, claim recorddeletion.ClaimedDeletionWork) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkAppendDeleteCommit {
		return ErrRecordDeletionStoreUnavailable
	}
	owner, err := deletionV1Owner(claim)
	if err != nil {
		return err
	}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_mark_delete_commit_unknown_v1($1)`, owner)
}

func (repository *PostgresRecordDeletionRepository) RecordDeleteEntry(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, entry recorddeletion.DeletionLedgerEntry) error {
	if ctx == nil || repository == nil || repository.platform == nil ||
		(claim.Stage != recorddeletion.DeletionWorkAppendDeleteCommit && claim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit) ||
		entry.Request.EntryType != recorddeletion.LedgerEntryDeleteCommit || !sameStoreDeletionLedgerRequest(entry.Request, claim.Request) {
		return ErrRecordDeletionStoreUnavailable
	}
	payload, err := deletionV1Ledger(claim, entry)
	if err != nil {
		return err
	}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_record_delete_entry_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) MarkDeleteWitnessed(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, receipt recorddeletion.DeletionWitnessReceipt) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkConfirmDeleteWitness ||
		claim.Entry == nil || receipt.Sequence != claim.Entry.Sequence || receipt.EntryHash != claim.Entry.EntryHash || receipt.ProofDigest == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1Ledger(claim, *claim.Entry)
	if err != nil {
		return err
	}
	payload := deletionV1WitnessEnvelope{deletionV1LedgerEnvelope: ledger, WitnessProofDigest: hex.EncodeToString(receipt.ProofDigest[:])}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_mark_delete_witnessed_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) MarkOutcomeCommitUnknown(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, releaseEpoch uint64) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit ||
		releaseEpoch == 0 || releaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	owner, err := deletionV1Owner(claim)
	if err != nil {
		return err
	}
	payload := struct {
		deletionV1OwnerEnvelope
		ReleaseEpoch uint64 `json:"release_epoch"`
	}{owner, releaseEpoch}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_mark_outcome_commit_unknown_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) RecordOutcomeEntry(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, entry recorddeletion.DeletionLedgerEntry) error {
	if ctx == nil || repository == nil || repository.platform == nil || entry.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted ||
		entry.Request.ReleaseEpoch == 0 || entry.Request.ReleaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	var expected recorddeletion.LedgerAppendRequest
	switch claim.Stage {
	case recorddeletion.DeletionWorkResolveDeleteCommit:
		expected = claim.Request.AttemptNotCommitted(entry.Request.ReleaseEpoch)
	case recorddeletion.DeletionWorkResolveNotCommitted:
		expected = claim.Request
	default:
		return ErrRecordDeletionStoreUnavailable
	}
	if !sameStoreDeletionLedgerRequest(entry.Request, expected) {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1Ledger(claim, entry)
	if err != nil {
		return err
	}
	payload := struct {
		deletionV1LedgerEnvelope
		ReleaseEpoch uint64 `json:"release_epoch"`
	}{ledger, entry.Request.ReleaseEpoch}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_record_outcome_entry_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) FinalizeNotCommitted(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, receipt recorddeletion.DeletionWitnessReceipt) error {
	if ctx == nil || repository == nil || repository.platform == nil || repository.newAuditID == nil ||
		claim.Stage != recorddeletion.DeletionWorkConfirmNotCommittedWitness || claim.Entry == nil ||
		claim.Entry.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted ||
		receipt.Sequence != claim.Entry.Sequence || receipt.EntryHash != claim.Entry.EntryHash ||
		receipt.ProofDigest == ([sha256.Size]byte{}) || claim.Operation.ReleaseEpoch == 0 || claim.Operation.ReleaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1Ledger(claim, *claim.Entry)
	if err != nil {
		return err
	}
	auditID, err := repository.newAuditID()
	if err != nil {
		return fmt.Errorf("generate not-committed deletion audit id: %w", err)
	}
	if !validStoredRecordIdentity(auditID, "rda_") {
		return ErrRecordDeletionStoreUnavailable
	}
	payload := struct {
		deletionV1WitnessEnvelope
		ReleaseEpoch uint64 `json:"release_epoch"`
		AuditID      string `json:"audit_id"`
	}{deletionV1WitnessEnvelope{ledger, hex.EncodeToString(receipt.ProofDigest[:])}, claim.Operation.ReleaseEpoch, auditID}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_finalize_not_committed_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) PromotePermanentFence(ctx context.Context, claim recorddeletion.ClaimedDeletionWork) error {
	if ctx == nil || repository == nil || repository.platform == nil || repository.newAuditID == nil ||
		claim.Stage != recorddeletion.DeletionWorkPromotePermanentFence {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	auditID, err := repository.newAuditID()
	if err != nil {
		return fmt.Errorf("generate committed deletion audit id: %w", err)
	}
	if !validStoredRecordIdentity(auditID, "rda_") {
		return ErrRecordDeletionStoreUnavailable
	}
	payload := struct {
		deletionV1LedgerEnvelope
		AuditID string `json:"audit_id"`
	}{ledger, auditID}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_promote_permanent_fence_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) PermanentFenceApplied(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
) (bool, error) {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence {
		return false, ErrRecordDeletionStoreUnavailable
	}
	var ready bool
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := transaction.tx.QueryRow(ctx, recordDeletionPermanentFenceAppliedSQL,
			claim.Operation.OperationID,
			recorddeletion.DeletionStateFencePropagating,
			claim.Operation.LedgerSequence,
			claim.Operation.LedgerEntryHash[:],
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
			claim.Operation.Object.ProjectID,
			claim.Operation.Object.ObjectKind,
			claim.Operation.Object.ObjectID,
			claim.Operation.FenceEpoch,
		).Scan(&ready); err != nil {
			return fmt.Errorf("read permanent deletion fence propagation: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return ready, nil
}

func (repository *PostgresRecordDeletionRepository) MarkReadFenced(ctx context.Context, claim recorddeletion.ClaimedDeletionWork) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_mark_read_fenced_v1($1)`, ledger)
}

func (repository *PostgresRecordDeletionRepository) BeginOnlinePurge(ctx context.Context, claim recorddeletion.ClaimedDeletionWork) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkBeginOnlinePurge {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_begin_online_purge_v1($1)`, ledger)
}

func (repository *PostgresRecordDeletionRepository) CompleteOnlinePurge(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, receipt recorddeletion.OnlinePurgeReceipt) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkPurgeOnline ||
		receipt.OperationID != claim.Operation.OperationID || receipt.ReceiptDigest == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	payload := struct {
		deletionV1LedgerEnvelope
		ReceiptDigest string `json:"receipt_digest"`
	}{ledger, hex.EncodeToString(receipt.ReceiptDigest[:])}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_complete_online_purge_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) MarkRetryRequired(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, retryStage recorddeletion.DeletionWorkStage) error {
	if ctx == nil || repository == nil || repository.platform == nil {
		return ErrRecordDeletionStoreUnavailable
	}
	from, expectedStage, ok := recordDeletionRetryState(retryStage)
	if !ok || claim.Stage != expectedStage || claim.Operation.State != from {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	payload := struct {
		deletionV1LedgerEnvelope
		RetryStage recorddeletion.DeletionWorkStage `json:"retry_stage"`
	}{ledger, retryStage}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_mark_retry_required_v1($1)`, payload)
}

func (repository *PostgresRecordDeletionRepository) ResumeRetry(ctx context.Context, claim recorddeletion.ClaimedDeletionWork, retryStage recorddeletion.DeletionWorkStage) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Stage != recorddeletion.DeletionWorkResolveRetry || claim.RetryStage != retryStage {
		return ErrRecordDeletionStoreUnavailable
	}
	if _, _, ok := recordDeletionRetryState(retryStage); !ok {
		return ErrRecordDeletionStoreUnavailable
	}
	ledger, err := deletionV1OperationLedger(claim)
	if err != nil {
		return err
	}
	payload := struct {
		deletionV1LedgerEnvelope
		RetryStage recorddeletion.DeletionWorkStage `json:"retry_stage"`
	}{ledger, retryStage}
	return repository.runV1DeletionCommand(ctx, `select public.record_deletion_resume_retry_v1($1)`, payload)
}

func recordDeletionRetryState(stage recorddeletion.DeletionWorkStage) (
	recorddeletion.DeletionState,
	recorddeletion.DeletionWorkStage,
	bool,
) {
	switch stage {
	case recorddeletion.DeletionWorkPromotePermanentFence:
		return recorddeletion.DeletionStateDeleteRequested, recorddeletion.DeletionWorkPromotePermanentFence, true
	case recorddeletion.DeletionWorkPropagatePermanentFence:
		return recorddeletion.DeletionStateFencePropagating, recorddeletion.DeletionWorkPropagatePermanentFence, true
	case recorddeletion.DeletionWorkBeginOnlinePurge:
		return recorddeletion.DeletionStateReadFenced, recorddeletion.DeletionWorkBeginOnlinePurge, true
	case recorddeletion.DeletionWorkPurgeOnline:
		return recorddeletion.DeletionStateOnlinePurging, recorddeletion.DeletionWorkPurgeOnline, true
	default:
		return "", "", false
	}
}

func sameStoreDeletionLedgerRequest(left, right recorddeletion.LedgerAppendRequest) bool {
	return left.EntryType == right.EntryType && left.DeploymentID == right.DeploymentID &&
		left.ProjectID == right.ProjectID && left.OperationID == right.OperationID &&
		left.ActorID == right.ActorID && left.Object == right.Object &&
		left.TokenCommitment == right.TokenCommitment &&
		left.RequestFingerprint.Equal(right.RequestFingerprint) &&
		left.ReasonCode == right.ReasonCode &&
		left.DeletionContractVersion == right.DeletionContractVersion &&
		left.ReleaseEpoch == right.ReleaseEpoch
}

const recordCoreHealthSQL = `
	select count(*)::bigint as record_core_surface_count
	from pg_catalog.pg_class relation
	join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
	where namespace.nspname = 'public'
	  and relation.relkind in ('r', 'p')
	  and relation.relname = any($1::text[])`

const previewRecordCoreSQL = `
	with root as (
		select root.record_id,
		       root.project_id,
		       root.lifecycle,
		       root.current_revision_id,
		       root.lock_version,
		       root.authorization_epoch,
		       epoch.delivery_epoch
		from public.records root
		join public.content_delivery_epochs epoch
		  on epoch.project_id = root.project_id
		 and epoch.object_kind = 'record'
		 and epoch.object_id = root.record_id
		where root.project_id = $1
		  and root.record_id = $6
		  and root.current_revision_id = $2
		  and root.lock_version = $3
		  and root.authorization_epoch = $4
		  and epoch.delivery_epoch = $5
		  and not exists (
			select 1
			from public.deletion_reservations reservation
			where reservation.project_id = root.project_id
			  and reservation.object_kind = 'record'
			  and reservation.object_id = root.record_id
			  and reservation.state in ('fenced', 'committed')
		  )
	), revision_material as (
		select coalesce(jsonb_agg(
			jsonb_build_array(revision.revision_id, revision.revision_no,
				pg_catalog.encode(revision.canonical_hash, 'hex'))
			order by revision.revision_no, revision.revision_id
		), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_revisions revision
		join root on root.record_id = revision.record_id
	), draft_material as (
		select coalesce(jsonb_agg(
			jsonb_build_array(draft.draft_id, draft.base_revision_id,
				draft.draft_version, pg_catalog.encode(draft.payload_hash, 'hex'),
				pg_catalog.encode(draft.etag_digest, 'hex'))
			order by draft.draft_id
		), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_drafts draft
		join root on root.record_id = draft.record_id
	), checkpoint_material as (
		select coalesce(jsonb_agg(
			jsonb_build_array(checkpoint.checkpoint_id, checkpoint.draft_id,
				checkpoint.checkpoint_draft_version,
				pg_catalog.encode(checkpoint.checkpoint_payload_hash, 'hex'))
			order by checkpoint.draft_id, checkpoint.created_at, checkpoint.checkpoint_id
		), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_draft_checkpoints checkpoint
		join public.record_drafts draft on draft.draft_id = checkpoint.draft_id
		join root on root.record_id = draft.record_id
	), activity_material as (
		select coalesce(jsonb_agg(
			jsonb_build_array(activity.activity_id, activity.revision_id,
				activity.event_kind, activity.source_event_id, activity.source_version,
				activity.actor_id, activity.authorization_epoch,
				activity.record_lock_version, activity.event_at)
			order by activity.event_at, activity.activity_id
		), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_domain_activities activity
		join root on root.record_id = activity.record_id
	), receipt_material as (
		select coalesce(jsonb_agg(
			jsonb_build_array(receipt.operation_id,
				pg_catalog.encode(receipt.receipt_digest, 'hex'))
			order by receipt.operation_id
		), '[]'::jsonb) as material,
		count(*)::bigint as row_count
		from public.record_core_purge_receipts receipt
		join public.record_purge_operations operation
		  on operation.operation_id = receipt.operation_id
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		 and reservation.project_id = operation.project_id
		join root
		  on root.project_id = operation.project_id
		 and root.record_id = reservation.object_id
	)
	select pg_catalog.convert_to(jsonb_build_object(
		'record_id', root.record_id,
		'lifecycle', root.lifecycle,
		'current_revision_id', root.current_revision_id,
		'lock_version', root.lock_version,
		'authorization_epoch', root.authorization_epoch,
		'delivery_epoch', root.delivery_epoch,
		'revisions', revision_material.material,
		'drafts', draft_material.material,
		'checkpoints', checkpoint_material.material,
		'activities', activity_material.material,
		'receipts', receipt_material.material
	)::text, 'UTF8') as core_dependency_material,
	pg_catalog.convert_to(jsonb_build_object(
		'revision_count', revision_material.row_count,
		'draft_count', draft_material.row_count,
		'checkpoint_count', checkpoint_material.row_count,
		'activity_count', activity_material.row_count,
		'receipt_count', receipt_material.row_count
	)::text, 'UTF8') as core_impact_material
	from root
	cross join revision_material
	cross join draft_material
	cross join checkpoint_material
	cross join activity_material
	cross join receipt_material`

func (repository *PostgresRecordDeletionRepository) RecordCoreHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, ErrRecordDeletionStoreUnavailable
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		health, err = recordCoreHealthInTransaction(ctx, transaction.tx)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	return health, nil
}

func recordCoreHealthInTransaction(
	ctx context.Context,
	tx pgx.Tx,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || tx == nil {
		return recorddeletion.AdapterHealthSnapshot{}, ErrRecordDeletionStoreUnavailable
	}
	surfaces := recorddeletion.RecordCoreSurfaceNames()
	names := make([]string, len(surfaces))
	for index, surface := range surfaces {
		names[index] = string(surface)
	}
	var count int64
	if err := tx.QueryRow(ctx, recordCoreHealthSQL, names).Scan(&count); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, fmt.Errorf("read record core health: %w", err)
	}
	if count != int64(len(names)) {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx,
		"public.record_core_purge_v1(bytea)", "public.record_core_purge_verify_v1(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	if err := requireClientComparisonPurgeFunctions(ctx, tx,
		"public.record_core_purge_v2(bytea)", "public.record_core_purge_verify_v2(bytea)"); err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	health, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, digestRecordCoreHealth(names))
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return health, nil
}

func (repository *PostgresRecordDeletionRepository) PreviewRecordCore(
	ctx context.Context,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrRecordDeletionStoreUnavailable
	}
	var preview recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		preview, err = previewRecordCoreInTransaction(ctx, transaction.tx, target)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	return preview, nil
}

func previewRecordCoreInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	target recorddeletion.PreviewTarget,
) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || tx == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, ErrRecordDeletionStoreUnavailable
	}
	var dependencyMaterial []byte
	var impactMaterial []byte
	if err := tx.QueryRow(ctx, previewRecordCoreSQL,
		target.Object.ProjectID,
		target.CurrentRevisionID,
		int64(target.LockVersion),
		int64(target.AuthorizationEpoch),
		int64(target.ContentDeliveryEpoch),
		target.Object.ObjectID,
	).Scan(&dependencyMaterial, &impactMaterial); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
		}
		return recorddeletion.AdapterPreviewSnapshot{}, fmt.Errorf("preview record core: %w", err)
	}
	if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordCorePreview(target.DependencyGraphDigest, dependencyMaterial),
		ImpactDigest:     digestRecordCoreImpact(impactMaterial),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return preview, nil
}

func (repository *PostgresRecordDeletionRepository) PurgeRecordCore(
	ctx context.Context,
	command recorddeletion.CorePurgeCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, ErrRecordDeletionStoreUnavailable
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		receipt, err = runRestrictedCorePurge(ctx, transaction.tx, command, false)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresRecordDeletionRepository) VerifyRecordCorePurge(
	ctx context.Context,
	command recorddeletion.CorePurgeCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil ||
		receipt.AdapterName != recorddeletion.AdapterNameRecordCore ||
		receipt.OperationID != command.Operation.OperationID || receipt.SurfaceDigest != command.SurfaceDigest ||
		receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != digestRecordCorePurgeReceipt(command.Operation, command.SurfaceDigest, receipt.RemovedRowCount) {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		stored, err := runRestrictedCorePurge(ctx, transaction.tx, command, true)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, stored)
	})
}

func digestRecordCorePurgeReceipt(
	operation recorddeletion.DeletionOperation,
	surfaceDigest [sha256.Size]byte,
	removedRowCount uint64,
) [sha256.Size]byte {
	payload := make([]byte, 0, 256)
	payload = appendStoreDeletionLengthPrefixed(payload, corePurgeReceiptDigestDomainV1)
	payload = appendStoreDeletionUint64(payload, 1)
	payload = appendStoreDeletionLengthPrefixed(payload, operation.OperationID)
	payload = append(payload, surfaceDigest[:]...)
	payload = appendStoreDeletionUint64(payload, removedRowCount)
	return sha256.Sum256(payload)
}

func digestRecordCoreHealth(surfaces []string) [sha256.Size]byte {
	payload := make([]byte, 0, 512)
	payload = appendStoreDeletionLengthPrefixed(payload, recordCoreHealthDigestDomainV1)
	payload = appendStoreDeletionUint64(payload, 1)
	payload = appendStoreDeletionUint64(payload, uint64(len(surfaces)))
	for _, surface := range surfaces {
		payload = appendStoreDeletionLengthPrefixed(payload, surface)
	}
	return sha256.Sum256(payload)
}

func digestRecordCorePreview(
	dependencyGraphDigest [sha256.Size]byte,
	material []byte,
) [sha256.Size]byte {
	payload := make([]byte, 0, len(material)+128)
	payload = appendStoreDeletionLengthPrefixed(payload, recordCorePreviewDigestDomainV1)
	payload = appendStoreDeletionUint64(payload, 1)
	payload = append(payload, dependencyGraphDigest[:]...)
	payload = appendStoreDeletionUint64(payload, uint64(len(material)))
	payload = append(payload, material...)
	return sha256.Sum256(payload)
}

func digestRecordCoreImpact(material []byte) [sha256.Size]byte {
	payload := make([]byte, 0, len(material)+96)
	payload = appendStoreDeletionLengthPrefixed(payload, recordCoreImpactDigestDomainV1)
	payload = appendStoreDeletionUint64(payload, 1)
	payload = appendStoreDeletionUint64(payload, uint64(len(material)))
	payload = append(payload, material...)
	return sha256.Sum256(payload)
}

func appendStoreDeletionLengthPrefixed(destination []byte, value string) []byte {
	destination = appendStoreDeletionUint64(destination, uint64(len(value)))
	return append(destination, value...)
}

func appendStoreDeletionUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}

func deletionStoreUint64(value uint64) (int64, bool) {
	if value > math.MaxInt64 {
		return 0, false
	}
	return int64(value), true
}

func deletionStoreDigest(value []byte) ([sha256.Size]byte, bool) {
	if len(value) != sha256.Size {
		return [sha256.Size]byte{}, false
	}
	var digest [sha256.Size]byte
	copy(digest[:], value)
	return digest, true
}

func deletionStoreDigestEqual(value []byte, expected [sha256.Size]byte) bool {
	digest, ok := deletionStoreDigest(value)
	return ok && digest == expected
}

func scanDeletionOperation(
	operationID string,
	reservationID string,
	object recordplatform.ObjectRef,
	reasonCode string,
	operationState string,
	fenceEpoch int64,
	ledgerSequence int64,
	ledgerEntryHashBytes []byte,
	releaseEpoch int64,
	receiptDigestBytes []byte,
) (recorddeletion.DeletionOperation, error) {
	if fenceEpoch <= 0 || ledgerSequence < 0 || releaseEpoch < 0 {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: operationID, ReservationID: reservationID,
		Object:       object,
		ReasonCode:   recorddeletion.DeletionReasonCode(reasonCode),
		State:        recorddeletion.DeletionState(operationState),
		FenceEpoch:   recordplatform.ContentEpoch(fenceEpoch),
		ReleaseEpoch: uint64(releaseEpoch)}
	if ledgerSequence > 0 {
		ledgerEntryHash, ok := deletionStoreDigest(ledgerEntryHashBytes)
		if !ok {
			return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		operation.LedgerSequence = uint64(ledgerSequence)
		operation.LedgerEntryHash = ledgerEntryHash
	} else if len(ledgerEntryHashBytes) != 0 {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if len(receiptDigestBytes) > 0 {
		receiptDigest, ok := deletionStoreDigest(receiptDigestBytes)
		if !ok {
			return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		operation.ReceiptDigest = receiptDigest
	}
	if err := operation.Validate(); err != nil {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return operation, nil
}
