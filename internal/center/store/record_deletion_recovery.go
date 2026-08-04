package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

const (
	recoveryReservationIDDomainV1 = "houfeng.record-deletion.recovery-reservation-id.v1"
	recoveryAuditIDDomainV1       = "houfeng.record-deletion.recovery-audit-id.v1"
)

const ensureRecordDeletionRecoveryCursorSQL = `
	insert into public.deletion_replay_state (
		project_id, applied_ledger_sequence, applied_ledger_hash, updated_at
	) values ($1, 0, decode(repeat('00', 32), 'hex'), transaction_timestamp())
	on conflict (project_id) do nothing`

const lockRecordDeletionRecoveryCursorSQL = `
	select applied_ledger_sequence,
	       applied_ledger_hash
	from public.deletion_replay_state
	where project_id = $1
	for update`

const loadRecordDeletionRecoveryProjectionSQL = `
	select operation.reservation_id,
	       reservation.recovery_replayed,
	       reservation.project_id,
	       reservation.object_kind,
	       reservation.object_id,
	       reservation.deletion_token_commitment,
	       reservation.request_fingerprint,
	       reservation.state,
	       reservation.fence_epoch,
	       reservation.release_epoch,
	       operation.deployment_id,
	       operation.actor_id,
	       operation.reason_code,
	       operation.deletion_contract_version,
	       operation.operation_state,
	       coalesce(operation.ledger_entry_type, ''),
	       coalesce(operation.ledger_sequence, 0),
	       coalesce(operation.ledger_entry_hash, ''::bytea),
	       operation.release_epoch,
	       coalesce(operation.witness_proof_digest, ''::bytea),
	       coalesce(operation.receipt_digest, ''::bytea)
	from public.record_purge_operations operation
	join public.deletion_reservations reservation
	  on reservation.reservation_id = operation.reservation_id
	 and reservation.project_id = operation.project_id
	where operation.operation_id = $1
	for update of operation, reservation`

const loadRecordDeletionRecoveryPreviewReservationSQL = `
	select reservation_id,
	       recovery_replayed,
	       project_id,
	       object_kind,
	       object_id,
	       deletion_token_commitment,
	       request_fingerprint,
	       state,
	       fence_epoch,
	       release_epoch,
	       owner_id,
	       owner_generation,
	       owner_expires_at
	from public.deletion_reservations
	where deletion_token_commitment = $1
	for update`

const loadRecordDeletionRecoveryReservationOperationSQL = `
	select operation_id
	from public.record_purge_operations
	where reservation_id = $1
	for update`

const insertRecordDeletionRecoveryReservationSQL = `
	insert into public.deletion_reservations (
		reservation_id,
		project_id,
		object_kind,
		object_id,
		deletion_token_commitment,
		request_fingerprint,
		state,
		fence_epoch,
		owner_id,
		owner_generation,
		owner_expires_at,
		created_at,
		expires_at,
		completed_at,
		release_epoch,
		recovery_replayed
	) values (
		$1, $2, $3, $4, $5, $6, $7, 1, '', 0, null,
		transaction_timestamp(), 'infinity'::timestamptz,
		transaction_timestamp(), $8, true
	)`

const updateRecordDeletionRecoveryReservationSQL = `
	update public.deletion_reservations
	set state = $2,
	    fence_epoch = greatest(fence_epoch, 1),
	    release_epoch = $3,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = coalesce(completed_at, transaction_timestamp())
	where reservation_id = $1`

const insertRecordDeletionRecoveryOperationSQL = `
	insert into public.record_purge_operations (
		operation_id,
		reservation_id,
		project_id,
		operation_state,
		ledger_sequence,
		ledger_entry_hash,
		started_at,
		completed_at,
		deployment_id,
		actor_id,
		reason_code,
		deletion_contract_version,
		ledger_entry_type,
		witness_proof_digest,
		release_epoch,
		receipt_digest,
		owner_id,
		owner_generation,
		owner_expires_at,
		updated_at
	) values (
		$1, $2, $3, $4, $5, $6,
		transaction_timestamp(), transaction_timestamp(),
		$7, $8, $9, $10, $11, $12, $13, $14,
		'', 0, null, transaction_timestamp()
	)`

const updateRecordDeletionRecoveryOperationSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    ledger_sequence = $3,
	    ledger_entry_hash = $4,
	    ledger_entry_type = $5,
	    witness_proof_digest = $6,
	    release_epoch = $7,
	    receipt_digest = $8,
	    retry_from = null,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = coalesce(completed_at, transaction_timestamp()),
	    updated_at = transaction_timestamp()
	where operation_id = $1`

const expireRecordDeletionRecoveryFenceSQL = `
	update public.deletion_fence_leases
	set created_at = least(created_at, transaction_timestamp() - interval '1 microsecond'),
	    expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and expires_at > transaction_timestamp()`

const insertRecordDeletionRecoveryAuditSQL = `
	insert into public.record_deletion_audits (
		audit_id, operation_id, project_id, event_kind, reason_code, occurred_at
	) values ($1, $2, $3, $4, $5, transaction_timestamp())
	on conflict (operation_id, event_kind) do nothing`

const loadRecordDeletionRecoveryAuditSQL = `
	select reason_code
	from public.record_deletion_audits
	where operation_id = $1
	  and event_kind = $2`

const loadRecordDeletionRecoveryFenceCountSQL = `
	select count(*)::bigint
	from public.deletion_fence_leases
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and expires_at > transaction_timestamp()`

const advanceRecordDeletionRecoveryCursorSQL = `
	update public.deletion_replay_state
	set applied_ledger_sequence = $2,
	    applied_ledger_hash = $3,
	    updated_at = transaction_timestamp()
	where project_id = $1
	  and applied_ledger_sequence = $4
	  and applied_ledger_hash = $5`

type recordDeletionRecoveryProjection struct {
	reservationID      string
	recoveryReplayed   bool
	projectID          string
	objectKind         string
	objectID           string
	tokenCommitment    []byte
	requestFingerprint []byte
	reservationState   string
	fenceEpoch         int64
	reservationRelease int64
	deploymentID       string
	actorID            string
	reasonCode         string
	deletionContract   int64
	operationState     string
	ledgerEntryType    string
	ledgerSequence     int64
	ledgerEntryHash    []byte
	operationRelease   int64
	witnessProofDigest []byte
	receiptDigest      []byte
}

type recordDeletionRecoveryPreviewReservation struct {
	reservationID      string
	recoveryReplayed   bool
	projectID          string
	objectKind         string
	objectID           string
	tokenCommitment    []byte
	requestFingerprint []byte
	state              string
	fenceEpoch         int64
	releaseEpoch       int64
	ownerID            string
	ownerGeneration    int64
	ownerExpiresAt     *time.Time
}

func (repository *PostgresRecordDeletionRepository) ApplyRecoveryEntry(
	ctx context.Context,
	command recorddeletion.RecoveryReplayCommand,
) (recorddeletion.RecoveryReplayReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.RecoveryReplayReceipt{}, ErrRecordDeletionStoreUnavailable
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.RecoveryReplayReceipt{}, err
	}
	entrySequence, ok := deletionStoreUint64(command.Entry.Sequence)
	if !ok {
		return recorddeletion.RecoveryReplayReceipt{}, recorddeletion.ErrRecoveryContractUnavailable
	}
	cursorSequence, ok := deletionStoreUint64(command.Cursor.Sequence)
	if !ok {
		return recorddeletion.RecoveryReplayReceipt{}, recorddeletion.ErrRecoveryContractUnavailable
	}

	receipt := recorddeletion.RecoveryReplayReceipt{
		Sequence:      command.Entry.Sequence,
		EntryHash:     command.Entry.EntryHash,
		SurfaceDigest: command.SurfaceDigest,
		ContentPurged: command.PurgeContent,
	}
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if _, err := transaction.tx.Exec(ctx, ensureRecordDeletionRecoveryCursorSQL, command.Entry.Request.Object.ProjectID); err != nil {
			return fmt.Errorf("ensure record deletion recovery cursor: %w", err)
		}
		storedCursor, err := lockRecordDeletionRecoveryCursor(ctx, transaction.tx, command.Entry.Request.Object.ProjectID)
		if err != nil {
			return err
		}
		if storedCursor.Sequence == command.Entry.Sequence && storedCursor.EntryHash == command.Entry.EntryHash {
			return verifyRecordDeletionRecoveryApplied(ctx, transaction.tx, command)
		}
		if storedCursor != command.Cursor {
			return fmt.Errorf("%w: stale recovery cursor", recorddeletion.ErrRecoveryContractUnavailable)
		}

		projection, found, err := loadRecordDeletionRecoveryProjection(ctx, transaction.tx, command.Entry.Request.OperationID)
		if err != nil {
			return err
		}
		if found {
			if err := projection.validateIdentity(command); err != nil {
				return err
			}
		}

		reservationID := projection.reservationID
		previewReservationFound := false
		if !found {
			previewReservation, previewFound, err := loadRecordDeletionRecoveryPreviewReservation(ctx, transaction.tx, command)
			if err != nil {
				return err
			}
			if previewFound {
				reservationID = previewReservation.reservationID
				previewReservationFound = true
			} else {
				reservationID = recordDeletionRecoverySyntheticID(
					"drs_",
					recoveryReservationIDDomainV1,
					command,
				)
			}
		}
		if !validStoredRecordIdentity(reservationID, "drs_") {
			return recorddeletion.ErrRecoveryContractUnavailable
		}

		terminalState := recorddeletion.DeletionStateNotCommitted
		reservationState := "not_committed"
		auditKind := "not_committed"
		releaseEpoch := command.Entry.Request.ReleaseEpoch
		var operationReceiptDigest []byte
		var coreReceipt *recorddeletion.AdapterPurgeReceipt
		if command.PurgeContent {
			terminalState = recorddeletion.DeletionStateOnlinePurged
			reservationState = "committed"
			auditKind = "committed"
			releaseEpoch = 0
			operation := recordDeletionRecoveryOperation(command, reservationID, terminalState)
			storedReceipt, err := loadRecordCorePurgeReceipt(ctx, transaction.tx, operation)
			switch {
			case err == nil:
				if err := validateRecordDeletionRecoveryCoreReceipt(ctx, transaction.tx, operation, storedReceipt); err != nil {
					return err
				}
				coreReceipt = &storedReceipt
			case errors.Is(err, pgx.ErrNoRows):
				removedRows, err := purgeRecordCoreForRecovery(ctx, transaction.tx, command.Entry.Request.Object.ObjectID, command.Entry.Request.Object.ProjectID)
				if err != nil {
					return err
				}
				surfaceDigest := recorddeletion.RecordCoreSurfaceDigest()
				if surfaceDigest == ([sha256.Size]byte{}) {
					return recorddeletion.ErrRecoveryContractUnavailable
				}
				receiptDigest := digestRecordCorePurgeReceipt(operation, surfaceDigest, removedRows)
				coreReceipt = &recorddeletion.AdapterPurgeReceipt{
					AdapterName:     recorddeletion.AdapterNameRecordCore,
					OperationID:     operation.OperationID,
					SurfaceDigest:   surfaceDigest,
					ReceiptDigest:   receiptDigest,
					RemovedRowCount: removedRows,
				}
			default:
				return err
			}
			operationReceiptDigest = coreReceipt.ReceiptDigest[:]
		}

		if found {
			if err := updateRecordDeletionRecoveryTerminal(
				ctx,
				transaction.tx,
				command,
				projection,
				reservationState,
				terminalState,
				releaseEpoch,
				operationReceiptDigest,
			); err != nil {
				return err
			}
		} else if previewReservationFound {
			if err := updateRecordDeletionRecoveryPreviewTerminal(
				ctx,
				transaction.tx,
				command,
				reservationID,
				reservationState,
				terminalState,
				releaseEpoch,
				operationReceiptDigest,
			); err != nil {
				return err
			}
		} else if err := insertRecordDeletionRecoveryTerminal(
			ctx,
			transaction.tx,
			command,
			reservationID,
			reservationState,
			terminalState,
			releaseEpoch,
			operationReceiptDigest,
		); err != nil {
			return err
		}

		if _, err := transaction.tx.Exec(ctx, expireRecordDeletionRecoveryFenceSQL,
			command.Entry.Request.Object.ProjectID,
			command.Entry.Request.Object.ObjectKind,
			command.Entry.Request.Object.ObjectID,
		); err != nil {
			return fmt.Errorf("expire record deletion recovery fence: %w", err)
		}

		if coreReceipt != nil && coreReceipt.VerifiedAbsentAt.IsZero() {
			var verifiedAbsentAt time.Time
			if err := transaction.tx.QueryRow(ctx, insertRecordCorePurgeReceiptSQL,
				command.Entry.Request.OperationID,
				command.Entry.Request.Object.ProjectID,
				command.Entry.Request.Object.ObjectID,
				coreReceipt.SurfaceDigest[:],
				coreReceipt.ReceiptDigest[:],
				int64(coreReceipt.RemovedRowCount),
			).Scan(&verifiedAbsentAt); err != nil {
				return fmt.Errorf("insert record deletion recovery core receipt: %w", err)
			}
			if verifiedAbsentAt.IsZero() {
				return recorddeletion.ErrRecoveryContractUnavailable
			}
			coreReceipt.VerifiedAbsentAt = verifiedAbsentAt.UTC()
		}

		auditID := recordDeletionRecoverySyntheticID("rda_", recoveryAuditIDDomainV1, command)
		if !validStoredRecordIdentity(auditID, "rda_") {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		if _, err := transaction.tx.Exec(ctx, insertRecordDeletionRecoveryAuditSQL,
			auditID,
			command.Entry.Request.OperationID,
			command.Entry.Request.Object.ProjectID,
			auditKind,
			string(command.Entry.Request.ReasonCode),
		); err != nil {
			return fmt.Errorf("insert record deletion recovery audit: %w", err)
		}
		var storedReasonCode string
		if err := transaction.tx.QueryRow(ctx, loadRecordDeletionRecoveryAuditSQL,
			command.Entry.Request.OperationID,
			auditKind,
		).Scan(&storedReasonCode); err != nil {
			return fmt.Errorf("load record deletion recovery audit: %w", err)
		}
		if storedReasonCode != string(command.Entry.Request.ReasonCode) {
			return recorddeletion.ErrRecoveryContractUnavailable
		}

		result, err := transaction.tx.Exec(ctx, advanceRecordDeletionRecoveryCursorSQL,
			command.Entry.Request.Object.ProjectID,
			entrySequence,
			command.Entry.EntryHash[:],
			cursorSequence,
			command.Cursor.EntryHash[:],
		)
		if err != nil {
			return fmt.Errorf("advance record deletion recovery cursor: %w", err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("%w: lost recovery cursor", recorddeletion.ErrRecoveryContractUnavailable)
		}
		return verifyRecordDeletionRecoveryApplied(ctx, transaction.tx, command)
	})
	if err != nil {
		return recorddeletion.RecoveryReplayReceipt{}, err
	}
	return receipt, nil
}

func lockRecordDeletionRecoveryCursor(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
) (recorddeletion.RecoveryReplayCursor, error) {
	var sequence int64
	var hashBytes []byte
	if err := tx.QueryRow(ctx, lockRecordDeletionRecoveryCursorSQL, projectID).Scan(&sequence, &hashBytes); err != nil {
		return recorddeletion.RecoveryReplayCursor{}, fmt.Errorf("lock record deletion recovery cursor: %w", err)
	}
	if sequence < 0 {
		return recorddeletion.RecoveryReplayCursor{}, recorddeletion.ErrRecoveryContractUnavailable
	}
	hash, ok := deletionStoreDigest(hashBytes)
	if !ok || (sequence == 0) != (hash == ([sha256.Size]byte{})) {
		return recorddeletion.RecoveryReplayCursor{}, recorddeletion.ErrRecoveryContractUnavailable
	}
	return recorddeletion.RecoveryReplayCursor{Sequence: uint64(sequence), EntryHash: hash}, nil
}

func loadRecordDeletionRecoveryProjection(
	ctx context.Context,
	tx pgx.Tx,
	operationID string,
) (recordDeletionRecoveryProjection, bool, error) {
	var projection recordDeletionRecoveryProjection
	err := tx.QueryRow(ctx, loadRecordDeletionRecoveryProjectionSQL, operationID).Scan(
		&projection.reservationID,
		&projection.recoveryReplayed,
		&projection.projectID,
		&projection.objectKind,
		&projection.objectID,
		&projection.tokenCommitment,
		&projection.requestFingerprint,
		&projection.reservationState,
		&projection.fenceEpoch,
		&projection.reservationRelease,
		&projection.deploymentID,
		&projection.actorID,
		&projection.reasonCode,
		&projection.deletionContract,
		&projection.operationState,
		&projection.ledgerEntryType,
		&projection.ledgerSequence,
		&projection.ledgerEntryHash,
		&projection.operationRelease,
		&projection.witnessProofDigest,
		&projection.receiptDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordDeletionRecoveryProjection{}, false, nil
	}
	if err != nil {
		return recordDeletionRecoveryProjection{}, false, fmt.Errorf("load record deletion recovery projection: %w", err)
	}
	return projection, true, nil
}

func loadRecordDeletionRecoveryPreviewReservation(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
) (recordDeletionRecoveryPreviewReservation, bool, error) {
	var reservation recordDeletionRecoveryPreviewReservation
	err := tx.QueryRow(ctx, loadRecordDeletionRecoveryPreviewReservationSQL, command.Entry.Request.TokenCommitment[:]).Scan(
		&reservation.reservationID,
		&reservation.recoveryReplayed,
		&reservation.projectID,
		&reservation.objectKind,
		&reservation.objectID,
		&reservation.tokenCommitment,
		&reservation.requestFingerprint,
		&reservation.state,
		&reservation.fenceEpoch,
		&reservation.releaseEpoch,
		&reservation.ownerID,
		&reservation.ownerGeneration,
		&reservation.ownerExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordDeletionRecoveryPreviewReservation{}, false, nil
	}
	if err != nil {
		return recordDeletionRecoveryPreviewReservation{}, false, fmt.Errorf("load record deletion recovery preview reservation: %w", err)
	}
	request := command.Entry.Request
	if !validStoredRecordIdentity(reservation.reservationID, "drs_") || reservation.recoveryReplayed ||
		reservation.projectID != request.Object.ProjectID || reservation.objectKind != request.Object.ObjectKind ||
		reservation.objectID != request.Object.ObjectID ||
		!deletionStoreDigestEqual(reservation.tokenCommitment, request.TokenCommitment) ||
		!deletionStoreDigestEqual(reservation.requestFingerprint, command.RequestFingerprintBytes) ||
		reservation.state != "previewed" || reservation.fenceEpoch != 0 || reservation.releaseEpoch != 0 ||
		reservation.ownerID != "" || reservation.ownerGeneration != 0 || reservation.ownerExpiresAt != nil {
		return recordDeletionRecoveryPreviewReservation{}, false, recorddeletion.ErrRecoveryContractUnavailable
	}

	var operationID string
	err = tx.QueryRow(ctx, loadRecordDeletionRecoveryReservationOperationSQL, reservation.reservationID).Scan(&operationID)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			return recordDeletionRecoveryPreviewReservation{}, false, fmt.Errorf("load record deletion recovery reservation operation: %w", err)
		}
		return recordDeletionRecoveryPreviewReservation{}, false, recorddeletion.ErrRecoveryContractUnavailable
	}
	return reservation, true, nil
}

func (projection recordDeletionRecoveryProjection) validateIdentity(command recorddeletion.RecoveryReplayCommand) error {
	request := command.Entry.Request
	if !validStoredRecordIdentity(projection.reservationID, "drs_") ||
		projection.projectID != request.Object.ProjectID ||
		projection.objectKind != request.Object.ObjectKind ||
		projection.objectID != request.Object.ObjectID ||
		!deletionStoreDigestEqual(projection.tokenCommitment, request.TokenCommitment) ||
		!deletionStoreDigestEqual(projection.requestFingerprint, command.RequestFingerprintBytes) ||
		projection.fenceEpoch <= 0 || projection.reservationRelease < 0 ||
		projection.deploymentID != string(request.DeploymentID) ||
		projection.actorID != request.ActorID || projection.reasonCode != string(request.ReasonCode) ||
		projection.deletionContract != int64(recorddeletion.RecordDeletionContractVersionV1) ||
		projection.ledgerSequence < 0 || projection.operationRelease < 0 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	hasLedger := projection.ledgerEntryType != "" || projection.ledgerSequence != 0 || len(projection.ledgerEntryHash) != 0
	if hasLedger && (projection.ledgerEntryType != string(request.EntryType) ||
		projection.ledgerSequence != int64(command.Entry.Sequence) ||
		!deletionStoreDigestEqual(projection.ledgerEntryHash, command.Entry.EntryHash)) {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	if len(projection.witnessProofDigest) != 0 && !deletionStoreDigestEqual(projection.witnessProofDigest, command.WitnessProofDigest) {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	if request.EntryType == recorddeletion.LedgerEntryDeleteCommit {
		if projection.reservationState == "not_committed" ||
			projection.operationState == string(recorddeletion.DeletionStateNotCommitted) ||
			projection.reservationRelease != 0 || projection.operationRelease != 0 {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
	} else if projection.reservationState == "committed" ||
		projection.operationState == string(recorddeletion.DeletionStateOnlinePurged) ||
		(projection.reservationRelease != 0 && projection.reservationRelease != int64(request.ReleaseEpoch)) ||
		(projection.operationRelease != 0 && projection.operationRelease != int64(request.ReleaseEpoch)) {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	return nil
}

func insertRecordDeletionRecoveryTerminal(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
	reservationID string,
	reservationState string,
	operationState recorddeletion.DeletionState,
	releaseEpoch uint64,
	receiptDigest []byte,
) error {
	release, ok := deletionStoreUint64(releaseEpoch)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	request := command.Entry.Request
	if _, err := tx.Exec(ctx, insertRecordDeletionRecoveryReservationSQL,
		reservationID,
		request.Object.ProjectID,
		request.Object.ObjectKind,
		request.Object.ObjectID,
		request.TokenCommitment[:],
		command.RequestFingerprintBytes[:],
		reservationState,
		release,
	); err != nil {
		return fmt.Errorf("insert record deletion recovery reservation: %w", err)
	}
	return insertRecordDeletionRecoveryOperation(ctx, tx, command, reservationID, operationState, releaseEpoch, receiptDigest)
}

func insertRecordDeletionRecoveryOperation(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
	reservationID string,
	operationState recorddeletion.DeletionState,
	releaseEpoch uint64,
	receiptDigest []byte,
) error {
	release, ok := deletionStoreUint64(releaseEpoch)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	sequence, ok := deletionStoreUint64(command.Entry.Sequence)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	request := command.Entry.Request
	if _, err := tx.Exec(ctx, insertRecordDeletionRecoveryOperationSQL,
		request.OperationID,
		reservationID,
		request.Object.ProjectID,
		string(operationState),
		sequence,
		command.Entry.EntryHash[:],
		string(request.DeploymentID),
		request.ActorID,
		string(request.ReasonCode),
		int64(recorddeletion.RecordDeletionContractVersionV1),
		string(request.EntryType),
		command.WitnessProofDigest[:],
		release,
		nilIfEmptyRecoveryDigest(receiptDigest),
	); err != nil {
		return fmt.Errorf("insert record deletion recovery operation: %w", err)
	}
	return nil
}

func updateRecordDeletionRecoveryPreviewTerminal(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
	reservationID string,
	reservationState string,
	operationState recorddeletion.DeletionState,
	releaseEpoch uint64,
	receiptDigest []byte,
) error {
	release, ok := deletionStoreUint64(releaseEpoch)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	result, err := tx.Exec(ctx, updateRecordDeletionRecoveryReservationSQL, reservationID, reservationState, release)
	if err != nil {
		return fmt.Errorf("update record deletion recovery preview reservation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	return insertRecordDeletionRecoveryOperation(ctx, tx, command, reservationID, operationState, releaseEpoch, receiptDigest)
}

func updateRecordDeletionRecoveryTerminal(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
	projection recordDeletionRecoveryProjection,
	reservationState string,
	operationState recorddeletion.DeletionState,
	releaseEpoch uint64,
	receiptDigest []byte,
) error {
	release, ok := deletionStoreUint64(releaseEpoch)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	sequence, ok := deletionStoreUint64(command.Entry.Sequence)
	if !ok {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	reservationResult, err := tx.Exec(ctx, updateRecordDeletionRecoveryReservationSQL,
		projection.reservationID,
		reservationState,
		release,
	)
	if err != nil {
		return fmt.Errorf("update record deletion recovery reservation: %w", err)
	}
	if reservationResult.RowsAffected() != 1 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	operationResult, err := tx.Exec(ctx, updateRecordDeletionRecoveryOperationSQL,
		command.Entry.Request.OperationID,
		string(operationState),
		sequence,
		command.Entry.EntryHash[:],
		string(command.Entry.Request.EntryType),
		command.WitnessProofDigest[:],
		release,
		nilIfEmptyRecoveryDigest(receiptDigest),
	)
	if err != nil {
		return fmt.Errorf("update record deletion recovery operation: %w", err)
	}
	if operationResult.RowsAffected() != 1 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	return nil
}

func purgeRecordCoreForRecovery(ctx context.Context, tx pgx.Tx, recordID, projectID string) (uint64, error) {
	projectionResult, err := tx.Exec(ctx, clearRecordCoreCurrentProjectionSQL, recordID, projectID)
	if err != nil {
		return 0, fmt.Errorf("clear recovery record core projection: %w", err)
	}
	if projectionResult.RowsAffected() < 0 || projectionResult.RowsAffected() > 1 {
		return 0, recorddeletion.ErrRecoveryContractUnavailable
	}

	var removedRows uint64
	for _, statement := range deleteRecordCoreSurfaceSQL {
		result, err := tx.Exec(ctx, statement, recordID)
		if err != nil {
			return 0, fmt.Errorf("purge recovery record core surface: %w", err)
		}
		rows := result.RowsAffected()
		if rows < 0 || uint64(rows) > math.MaxUint64-removedRows {
			return 0, recorddeletion.ErrRecoveryContractUnavailable
		}
		removedRows += uint64(rows)
	}
	result, err := tx.Exec(ctx, deleteRecordCoreDeliveryEpochSQL, recordID, projectID)
	if err != nil {
		return 0, fmt.Errorf("purge recovery content delivery epoch: %w", err)
	}
	rows := result.RowsAffected()
	if rows < 0 || uint64(rows) > math.MaxUint64-removedRows {
		return 0, recorddeletion.ErrRecoveryContractUnavailable
	}
	removedRows += uint64(rows)

	var contentPresent bool
	if err := tx.QueryRow(ctx, recordCoreContentPresentSQL, recordID, projectID).Scan(&contentPresent); err != nil {
		return 0, fmt.Errorf("verify recovery record core absence: %w", err)
	}
	if contentPresent {
		return 0, recorddeletion.ErrRecoveryContractUnavailable
	}
	return removedRows, nil
}

func validateRecordDeletionRecoveryCoreReceipt(
	ctx context.Context,
	tx pgx.Tx,
	operation recorddeletion.DeletionOperation,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	expectedSurface := recorddeletion.RecordCoreSurfaceDigest()
	if expectedSurface == ([sha256.Size]byte{}) || receipt.AdapterName != recorddeletion.AdapterNameRecordCore ||
		receipt.OperationID != operation.OperationID || receipt.SurfaceDigest != expectedSurface ||
		receipt.ReceiptDigest != digestRecordCorePurgeReceipt(operation, expectedSurface, receipt.RemovedRowCount) ||
		receipt.VerifiedAbsentAt.IsZero() {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	var contentPresent bool
	if err := tx.QueryRow(ctx, recordCoreContentPresentSQL,
		operation.Object.ObjectID,
		operation.Object.ProjectID,
	).Scan(&contentPresent); err != nil {
		return fmt.Errorf("verify existing recovery core receipt: %w", err)
	}
	if contentPresent {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	return nil
}

func verifyRecordDeletionRecoveryApplied(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.RecoveryReplayCommand,
) error {
	projection, found, err := loadRecordDeletionRecoveryProjection(ctx, tx, command.Entry.Request.OperationID)
	if err != nil {
		return err
	}
	if !found || projection.validateIdentity(command) != nil ||
		!deletionStoreDigestEqual(projection.witnessProofDigest, command.WitnessProofDigest) {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	auditKind := "not_committed"
	if command.PurgeContent {
		auditKind = "committed"
	}
	var storedReasonCode string
	if err := tx.QueryRow(ctx, loadRecordDeletionRecoveryAuditSQL,
		command.Entry.Request.OperationID,
		auditKind,
	).Scan(&storedReasonCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		return fmt.Errorf("verify applied recovery audit: %w", err)
	}
	if storedReasonCode != string(command.Entry.Request.ReasonCode) {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	var activeFenceCount int64
	if err := tx.QueryRow(ctx, loadRecordDeletionRecoveryFenceCountSQL,
		command.Entry.Request.Object.ProjectID,
		command.Entry.Request.Object.ObjectKind,
		command.Entry.Request.Object.ObjectID,
	).Scan(&activeFenceCount); err != nil {
		return fmt.Errorf("verify recovery fence release: %w", err)
	}
	if activeFenceCount != 0 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	if command.PurgeContent {
		if projection.reservationState != "committed" ||
			projection.operationState != string(recorddeletion.DeletionStateOnlinePurged) ||
			projection.reservationRelease != 0 || projection.operationRelease != 0 ||
			len(projection.receiptDigest) != sha256.Size {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		operation := recordDeletionRecoveryOperation(command, projection.reservationID, recorddeletion.DeletionStateOnlinePurged)
		storedReceipt, err := loadRecordCorePurgeReceipt(ctx, tx, operation)
		if err != nil {
			return fmt.Errorf("load applied recovery core receipt: %w", err)
		}
		if err := validateRecordDeletionRecoveryCoreReceipt(ctx, tx, operation, storedReceipt); err != nil ||
			!deletionStoreDigestEqual(projection.receiptDigest, storedReceipt.ReceiptDigest) {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		return nil
	}
	if projection.reservationState != "not_committed" ||
		projection.operationState != string(recorddeletion.DeletionStateNotCommitted) ||
		projection.reservationRelease != int64(command.Entry.Request.ReleaseEpoch) ||
		projection.operationRelease != int64(command.Entry.Request.ReleaseEpoch) || len(projection.receiptDigest) != 0 {
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	operation := recordDeletionRecoveryOperation(command, projection.reservationID, recorddeletion.DeletionStateNotCommitted)
	if _, err := loadRecordCorePurgeReceipt(ctx, tx, operation); !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			return fmt.Errorf("verify recovery outcome core receipt absence: %w", err)
		}
		return recorddeletion.ErrRecoveryContractUnavailable
	}
	return nil
}

func recordDeletionRecoveryOperation(
	command recorddeletion.RecoveryReplayCommand,
	reservationID string,
	state recorddeletion.DeletionState,
) recorddeletion.DeletionOperation {
	return recorddeletion.DeletionOperation{
		OperationID:     command.Entry.Request.OperationID,
		ReservationID:   reservationID,
		Object:          command.Entry.Request.Object,
		ReasonCode:      command.Entry.Request.ReasonCode,
		State:           state,
		FenceEpoch:      1,
		LedgerSequence:  command.Entry.Sequence,
		LedgerEntryHash: command.Entry.EntryHash,
		ReleaseEpoch:    command.Entry.Request.ReleaseEpoch,
	}
}

func recordDeletionRecoverySyntheticID(prefix, domain string, command recorddeletion.RecoveryReplayCommand) string {
	payload := make([]byte, 0, 256)
	payload = appendStoreDeletionLengthPrefixed(payload, domain)
	payload = appendStoreDeletionUint64(payload, 1)
	payload = appendStoreDeletionLengthPrefixed(payload, string(command.Entry.Request.DeploymentID))
	payload = appendStoreDeletionLengthPrefixed(payload, string(command.Entry.Request.ProjectID))
	payload = appendStoreDeletionLengthPrefixed(payload, command.Entry.Request.OperationID)
	payload = appendStoreDeletionLengthPrefixed(payload, string(command.Entry.Request.EntryType))
	digest := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(digest[:])
}

func nilIfEmptyRecoveryDigest(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
