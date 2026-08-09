package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	platform         *PostgresRecordPlatformRepository
	newReservationID func() (string, error)
	newOperationID   func() (string, error)
	newAuditID       func() (string, error)
}

var (
	_ recorddeletion.DeletionPreviewRepository = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.DeletionWorkerRepository  = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.RecordCoreStore           = (*PostgresRecordDeletionRepository)(nil)
	_ recorddeletion.RecoveryStore             = (*PostgresRecordDeletionRepository)(nil)
)

func NewPostgresRecordDeletionRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordDeletionRepository {
	return &PostgresRecordDeletionRepository{
		platform: NewPostgresRecordPlatformRepository(pool, gate),
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

const createDeletionPreviewSQL = `
	insert into public.deletion_reservations (
		reservation_id,
		project_id,
		object_kind,
		object_id,
		deletion_token_commitment,
		request_fingerprint,
		state,
		expires_at,
		actor_scope_digest,
		preview_binding_digest,
		preview_current_revision_id,
		preview_lock_version,
		preview_authorization_epoch,
		preview_content_delivery_epoch,
		preview_dependency_graph_digest,
		preview_backup_inventory_digest,
		preview_processor_inventory_digest,
		adapter_readiness_digest,
		adapter_preview_digest,
		preview_witness_sequence,
		preview_witness_entry_hash
	) values (
		$1, $2, $3, $4, $5, $6, 'previewed',
		transaction_timestamp() + ($7 * interval '1 microsecond'),
		$8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
	)
	returning expires_at`

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

const lockDeletionPreviewCASForReserveSQL = `
	select reservation.actor_scope_digest,
	       reservation.deletion_token_commitment,
	       reservation.request_fingerprint,
	       reservation.preview_binding_digest as preview_binding_digest,
	       reservation.preview_current_revision_id,
	       reservation.preview_lock_version,
	       reservation.preview_authorization_epoch,
	       reservation.preview_content_delivery_epoch,
	       reservation.preview_dependency_graph_digest,
	       reservation.preview_backup_inventory_digest,
	       reservation.preview_processor_inventory_digest,
	       reservation.preview_witness_sequence,
	       reservation.preview_witness_entry_hash
	from public.deletion_reservations as reservation
	where reservation.reservation_id = $1
	  and reservation.project_id = $2
	  and reservation.object_kind = $3
	  and reservation.object_id = $4
	  and reservation.state = 'previewed'
	  and reservation.expires_at > transaction_timestamp()
	for update`

const lockRecordCASForDeletionReserveSQL = `
	select record.current_revision_id,
	       record.lock_version,
	       record.authorization_epoch
	from public.records as record
	where record.project_id = $1
	  and record.record_id = $2
	for update`

const insertRecordPurgeOperationSQL = `
	insert into public.record_purge_operations (
		operation_id,
		reservation_id,
		project_id,
		operation_state,
		deployment_id,
		actor_id,
		reason_code,
		deletion_contract_version,
		owner_id,
		owner_generation,
		owner_expires_at,
		started_at,
		updated_at
	) values (
		$1, $2, $3, 'provisional_fenced', $4, $5, $6, $7, $8, $9, $10,
		transaction_timestamp(), transaction_timestamp()
	)
	returning started_at`

const insertRecordDeletionFencedAuditSQL = `
	insert into public.record_deletion_audits (
		audit_id,
		operation_id,
		project_id,
		event_kind,
		reason_code,
		occurred_at
	) values ($1, $2, $3, 'fenced', $4, transaction_timestamp())`

func (repository *PostgresRecordDeletionRepository) CreatePreview(
	ctx context.Context,
	command recorddeletion.CreatePreviewCommand,
) (recorddeletion.StoredPreview, error) {
	if ctx == nil || repository == nil || repository.platform == nil || repository.newReservationID == nil {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	reservationID, err := repository.newReservationID()
	if err != nil {
		return recorddeletion.StoredPreview{}, fmt.Errorf("generate deletion reservation id: %w", err)
	}
	if !validStoredRecordIdentity(reservationID, "drs_") {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	lockVersion, ok := deletionStoreUint64(command.Record.LockVersion)
	if !ok {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	authorizationEpoch, ok := deletionStoreUint64(command.Record.AuthorizationEpoch)
	if !ok {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	contentDeliveryEpoch, ok := deletionStoreUint64(uint64(command.Record.ContentDeliveryEpoch))
	if !ok {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	witnessSequence, ok := deletionStoreUint64(command.WitnessHead.Sequence)
	if !ok {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}

	var expiresAt time.Time
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := transaction.tx.QueryRow(ctx, createDeletionPreviewSQL,
			reservationID,
			command.Object.ProjectID,
			command.Object.ObjectKind,
			command.Object.ObjectID,
			command.TokenCommitment[:],
			fingerprintBytes[:],
			command.TTL.Microseconds(),
			command.ActorScopeDigest[:],
			command.BindingDigest[:],
			command.Record.CurrentRevisionID,
			lockVersion,
			authorizationEpoch,
			contentDeliveryEpoch,
			command.Record.DependencyGraphDigest[:],
			command.Record.BackupInventoryDigest[:],
			command.Record.ProcessorInventoryDigest[:],
			command.AdapterReadinessDigest[:],
			command.AdapterPreviewDigest[:],
			witnessSequence,
			command.WitnessHead.EntryHash[:],
		).Scan(&expiresAt); err != nil {
			return fmt.Errorf("create deletion preview: %w", err)
		}
		return nil
	})
	if err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	persistedFingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprintBytes[:])
	if err != nil {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	stored := recorddeletion.StoredPreview{
		ReservationID:      reservationID,
		Object:             command.Object,
		ActorScopeDigest:   command.ActorScopeDigest,
		TokenCommitment:    command.TokenCommitment,
		RequestFingerprint: persistedFingerprint,
		BindingDigest:      command.BindingDigest,
		WitnessHead:        command.WitnessHead,
		ExpiresAt:          expiresAt.UTC(),
	}
	if err := stored.Validate(); err != nil {
		return recorddeletion.StoredPreview{}, ErrRecordDeletionStoreUnavailable
	}
	return stored, nil
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
	if ctx == nil || repository == nil || repository.platform == nil ||
		repository.newOperationID == nil || repository.newAuditID == nil {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}
	if err := command.Validate(); err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	operationID, err := repository.newOperationID()
	if err != nil {
		return recorddeletion.DeletionOperation{}, fmt.Errorf("generate record purge operation id: %w", err)
	}
	auditID, err := repository.newAuditID()
	if err != nil {
		return recorddeletion.DeletionOperation{}, fmt.Errorf("generate record deletion audit id: %w", err)
	}
	if !validStoredRecordIdentity(operationID, "rpo_") || !validStoredRecordIdentity(auditID, "rda_") {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}
	lockVersion, ok := deletionStoreUint64(command.Record.LockVersion)
	if !ok {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}
	authorizationEpoch, ok := deletionStoreUint64(command.Record.AuthorizationEpoch)
	if !ok {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}
	contentDeliveryEpoch, ok := deletionStoreUint64(uint64(command.Record.ContentDeliveryEpoch))
	if !ok || contentDeliveryEpoch == math.MaxInt64 {
		return recorddeletion.DeletionOperation{}, ErrRecordDeletionStoreUnavailable
	}

	var operation recorddeletion.DeletionOperation
	err = repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var actorScopeDigestBytes []byte
		var tokenCommitmentBytes []byte
		var storedFingerprintBytes []byte
		var bindingDigestBytes []byte
		var currentRevisionID string
		var storedLockVersion int64
		var storedAuthorizationEpoch int64
		var storedContentDeliveryEpoch int64
		var dependencyGraphDigestBytes []byte
		var backupInventoryDigestBytes []byte
		var processorInventoryDigestBytes []byte
		var witnessSequence int64
		var witnessEntryHashBytes []byte
		if err := transaction.tx.QueryRow(ctx, lockDeletionPreviewCASForReserveSQL,
			command.Preview.ReservationID,
			command.Preview.Object.ProjectID,
			command.Preview.Object.ObjectKind,
			command.Preview.Object.ObjectID,
		).Scan(
			&actorScopeDigestBytes,
			&tokenCommitmentBytes,
			&storedFingerprintBytes,
			&bindingDigestBytes,
			&currentRevisionID,
			&storedLockVersion,
			&storedAuthorizationEpoch,
			&storedContentDeliveryEpoch,
			&dependencyGraphDigestBytes,
			&backupInventoryDigestBytes,
			&processorInventoryDigestBytes,
			&witnessSequence,
			&witnessEntryHashBytes,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionPreviewStale
			}
			return fmt.Errorf("lock deletion preview CAS: %w", err)
		}
		storedFingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(storedFingerprintBytes)
		if err != nil || !command.RequestFingerprint.MatchesPersisted(storedFingerprint) ||
			!deletionStoreDigestEqual(actorScopeDigestBytes, command.Preview.ActorScopeDigest) ||
			!deletionStoreDigestEqual(tokenCommitmentBytes, command.Preview.TokenCommitment) ||
			!deletionStoreDigestEqual(bindingDigestBytes, command.ExpectedBindingDigest) ||
			currentRevisionID != command.Record.CurrentRevisionID || storedLockVersion != lockVersion ||
			storedAuthorizationEpoch != authorizationEpoch || storedContentDeliveryEpoch != contentDeliveryEpoch ||
			!deletionStoreDigestEqual(dependencyGraphDigestBytes, command.Record.DependencyGraphDigest) ||
			!deletionStoreDigestEqual(backupInventoryDigestBytes, command.Record.BackupInventoryDigest) ||
			!deletionStoreDigestEqual(processorInventoryDigestBytes, command.Record.ProcessorInventoryDigest) ||
			witnessSequence <= 0 || uint64(witnessSequence) != command.Preview.WitnessHead.Sequence ||
			!deletionStoreDigestEqual(witnessEntryHashBytes, command.Preview.WitnessHead.EntryHash) {
			return recorddeletion.ErrDeletionPreviewStale
		}

		var currentRootRevisionID string
		var currentRootLockVersion int64
		var currentRootAuthorizationEpoch int64
		if err := transaction.tx.QueryRow(ctx, lockRecordCASForDeletionReserveSQL,
			command.Preview.Object.ProjectID,
			command.Preview.Object.ObjectID,
		).Scan(&currentRootRevisionID, &currentRootLockVersion, &currentRootAuthorizationEpoch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionPreviewStale
			}
			return fmt.Errorf("lock record deletion CAS: %w", err)
		}
		if currentRootRevisionID != command.Record.CurrentRevisionID || currentRootLockVersion != lockVersion ||
			currentRootAuthorizationEpoch != authorizationEpoch {
			return recorddeletion.ErrDeletionPreviewStale
		}

		fence, err := transaction.FenceDeletionReservation(ctx, recordplatform.ReservationFenceInputV1{
			ReservationID:      command.Preview.ReservationID,
			Object:             command.Preview.Object,
			OwnerID:            command.OwnerID,
			OwnerLeaseDuration: command.OwnerLeaseDuration,
		})
		if errors.Is(err, recordplatform.ErrDeletionReservationUnavailable) {
			return recorddeletion.ErrDeletionPreviewStale
		}
		if err != nil {
			return err
		}
		if uint64(fence.FenceEpoch) != uint64(command.Record.ContentDeliveryEpoch)+1 {
			return recorddeletion.ErrDeletionPreviewStale
		}

		var startedAt time.Time
		if err := transaction.tx.QueryRow(ctx, insertRecordPurgeOperationSQL,
			operationID,
			command.Preview.ReservationID,
			command.Preview.Object.ProjectID,
			command.DeploymentID,
			command.ActorID,
			command.ReasonCode,
			command.DeletionContractVersion,
			fence.Owner.OwnerID,
			fence.Owner.Generation,
			fence.Owner.ExpiresAt,
		).Scan(&startedAt); err != nil {
			return fmt.Errorf("create record purge operation: %w", err)
		}
		if startedAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		auditResult, err := transaction.tx.Exec(ctx, insertRecordDeletionFencedAuditSQL,
			auditID,
			operationID,
			command.Preview.Object.ProjectID,
			command.ReasonCode,
		)
		if err != nil {
			return fmt.Errorf("create record deletion fenced audit: %w", err)
		}
		if auditResult.RowsAffected() != 1 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}

		operation = recorddeletion.DeletionOperation{
			OperationID:   operationID,
			ReservationID: command.Preview.ReservationID,
			Object:        command.Preview.Object,
			ReasonCode:    command.ReasonCode,
			State:         recorddeletion.DeletionStateProvisionalFenced,
			FenceEpoch:    fence.FenceEpoch,
		}
		if err := operation.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	if err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	return operation, nil
}

const claimRecordDeletionWorkSQL = `
	select operation.operation_id,
	       operation.reservation_id,
	       operation.project_id,
	       operation.operation_state,
	       operation.deployment_id,
	       operation.actor_id,
	       operation.reason_code,
	       operation.deletion_contract_version,
	       coalesce(operation.ledger_entry_type, ''),
	       coalesce(operation.ledger_sequence, 0),
	       coalesce(operation.ledger_entry_hash, ''::bytea),
	       operation.release_epoch,
	       coalesce(operation.receipt_digest, ''::bytea),
	       coalesce(operation.retry_from, ''),
	       operation.owner_id,
	       operation.owner_generation,
	       operation.owner_expires_at,
	       reservation.state,
	       reservation.object_kind,
	       reservation.object_id,
	       reservation.deletion_token_commitment,
	       reservation.request_fingerprint,
	       reservation.fence_epoch,
	       reservation.owner_id,
	       reservation.owner_generation,
	       reservation.owner_expires_at,
	       transaction_timestamp()
	from public.record_purge_operations as operation
	join public.deletion_reservations as reservation
	  on reservation.reservation_id = operation.reservation_id
	 and reservation.project_id = operation.project_id
	where operation.operation_state not in ('online_purged', 'not_committed')
	  and (
		operation.owner_id = $1
		or operation.owner_expires_at <= transaction_timestamp()
	  )
	  and (
		operation.operation_state <> 'provisional_fenced'
		or not exists (
			select 1
			from public.object_content_leases as content_lease
			where content_lease.project_id = reservation.project_id
			  and content_lease.object_kind = reservation.object_kind
			  and content_lease.object_id = reservation.object_id
			  and content_lease.expires_at > transaction_timestamp()
		)
	  )
	order by operation.started_at, operation.operation_id
	for update of operation, reservation skip locked
	limit 1`

const lockRecordDeletionClaimFenceSQL = `
	select owner_id, owner_generation, expires_at
	from public.deletion_fence_leases
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	for update`

const takeOverRecordDeletionReservationSQL = `
	update public.deletion_reservations
	set owner_id = $7,
	    owner_generation = $8,
	    owner_expires_at = transaction_timestamp() + ($9 * interval '1 microsecond')
	where reservation_id = $1
	  and project_id = $2
	  and object_kind = $3
	  and object_id = $4
	  and state = 'fenced'
	  and owner_id = $5
	  and owner_generation = $6
	  and owner_expires_at = $10
	returning owner_id, owner_generation, owner_expires_at`

const takeOverRecordDeletionFenceSQL = `
	update public.deletion_fence_leases
	set owner_id = $7,
	    owner_generation = $8,
	    expires_at = transaction_timestamp() + ($9 * interval '1 microsecond')
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $6
	returning owner_id, owner_generation, expires_at`

const takeOverRecordDeletionOperationSQL = `
	update public.record_purge_operations
	set owner_id = $6,
	    owner_generation = $7,
	    owner_expires_at = transaction_timestamp() + ($8 * interval '1 microsecond'),
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $2
	  and owner_id = $3
	  and owner_generation = $4
	  and owner_expires_at = $5
	returning owner_id, owner_generation, owner_expires_at`

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

	var claim *recorddeletion.ClaimedDeletionWork
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var row persistedRecordDeletionWork
		var operationOwnerGeneration int64
		var reservationOwnerGeneration int64
		var reservationOwnerExpiresAt *time.Time
		err := transaction.tx.QueryRow(ctx, claimRecordDeletionWorkSQL, input.OwnerID).Scan(
			&row.operationID,
			&row.reservationID,
			&row.projectID,
			&row.operationState,
			&row.deploymentID,
			&row.actorID,
			&row.reasonCode,
			&row.deletionContractVersion,
			&row.ledgerEntryType,
			&row.ledgerSequence,
			&row.ledgerEntryHash,
			&row.releaseEpoch,
			&row.receiptDigest,
			&row.retryFrom,
			&row.operationOwner.OwnerID,
			&operationOwnerGeneration,
			&row.operationOwner.ExpiresAt,
			&row.reservationState,
			&row.objectKind,
			&row.objectID,
			&row.tokenCommitment,
			&row.requestFingerprint,
			&row.fenceEpoch,
			&row.reservationOwner.OwnerID,
			&reservationOwnerGeneration,
			&reservationOwnerExpiresAt,
			&row.observedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select record deletion work: %w", err)
		}
		if operationOwnerGeneration <= 0 || reservationOwnerGeneration < 0 {
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
		newOwner, err := nextRecordDeletionOwner(row.operationOwner, input, row.observedAt)
		if err != nil {
			return err
		}

		if compoundFence {
			var fenceOwner recordplatform.OwnerLease
			var fenceGeneration int64
			if err := transaction.tx.QueryRow(ctx, lockRecordDeletionClaimFenceSQL,
				row.projectID,
				row.objectKind,
				row.objectID,
			).Scan(&fenceOwner.OwnerID, &fenceGeneration, &fenceOwner.ExpiresAt); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return recordplatform.ErrLostOwnerLease
				}
				return fmt.Errorf("lock record deletion claim fence: %w", err)
			}
			if fenceGeneration <= 0 {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			fenceOwner.Generation = uint64(fenceGeneration)
			if fenceOwner != row.operationOwner {
				return recordplatform.ErrLostOwnerLease
			}

			reservationOwner, err := scanRecordDeletionOwner(transaction.tx.QueryRow(ctx,
				takeOverRecordDeletionReservationSQL,
				row.reservationID,
				row.projectID,
				row.objectKind,
				row.objectID,
				row.reservationOwner.OwnerID,
				row.reservationOwner.Generation,
				newOwner.OwnerID,
				newOwner.Generation,
				input.OwnerLeaseDuration.Microseconds(),
				row.reservationOwner.ExpiresAt,
			))
			if err != nil {
				return fmt.Errorf("take over record deletion reservation: %w", err)
			}
			fenceOwner, err = scanRecordDeletionOwner(transaction.tx.QueryRow(ctx,
				takeOverRecordDeletionFenceSQL,
				row.projectID,
				row.objectKind,
				row.objectID,
				row.operationOwner.OwnerID,
				row.operationOwner.Generation,
				row.operationOwner.ExpiresAt,
				newOwner.OwnerID,
				newOwner.Generation,
				input.OwnerLeaseDuration.Microseconds(),
			))
			if err != nil {
				return fmt.Errorf("take over record deletion fence: %w", err)
			}
			if reservationOwner != fenceOwner {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			newOwner = reservationOwner
		}

		operationOwner, err := scanRecordDeletionOwner(transaction.tx.QueryRow(ctx,
			takeOverRecordDeletionOperationSQL,
			row.operationID,
			row.operationState,
			row.operationOwner.OwnerID,
			row.operationOwner.Generation,
			row.operationOwner.ExpiresAt,
			newOwner.OwnerID,
			newOwner.Generation,
			input.OwnerLeaseDuration.Microseconds(),
		))
		if err != nil {
			return fmt.Errorf("take over record deletion operation: %w", err)
		}
		if operationOwner != newOwner {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		built, err := row.claim(operationOwner)
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

func nextRecordDeletionOwner(
	oldOwner recordplatform.OwnerLease,
	input recorddeletion.DeletionWorkClaimInput,
	observedAt time.Time,
) (recordplatform.OwnerLease, error) {
	if oldOwner.Validate() != nil || observedAt.IsZero() || input.Validate() != nil {
		return recordplatform.OwnerLease{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	generation := oldOwner.Generation
	if oldOwner.OwnerID != input.OwnerID || !oldOwner.ExpiresAt.After(observedAt) {
		if oldOwner.ExpiresAt.After(observedAt) || generation >= math.MaxInt64 {
			return recordplatform.OwnerLease{}, recordplatform.ErrLostOwnerLease
		}
		generation++
	}
	owner := recordplatform.OwnerLease{
		OwnerID:    input.OwnerID,
		Generation: generation,
		ExpiresAt:  observedAt.Add(input.OwnerLeaseDuration),
	}
	if owner.Validate() != nil {
		return recordplatform.OwnerLease{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return owner, nil
}

func scanRecordDeletionOwner(row pgx.Row) (recordplatform.OwnerLease, error) {
	var owner recordplatform.OwnerLease
	var generation int64
	if err := row.Scan(&owner.OwnerID, &generation, &owner.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recordplatform.OwnerLease{}, recordplatform.ErrLostOwnerLease
		}
		return recordplatform.OwnerLease{}, err
	}
	if generation <= 0 {
		return recordplatform.OwnerLease{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	owner.Generation = uint64(generation)
	if owner.Validate() != nil {
		return recordplatform.OwnerLease{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return owner, nil
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

const markRecordDeletionCommitUnknownSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and owner_expires_at = $6`

const recordDeletionDeleteEntrySQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    ledger_entry_type = $3,
	    ledger_sequence = $4,
	    ledger_entry_hash = $5,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $6
	  and owner_id = $7
	  and owner_generation = $8
	  and owner_expires_at = $9
	  and ledger_entry_type is null
	  and ledger_sequence is null
	  and ledger_entry_hash is null`

const markRecordDeletionDeleteWitnessedSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    witness_proof_digest = $3,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $4
	  and ledger_entry_type = $5
	  and ledger_sequence = $6
	  and ledger_entry_hash = $7
	  and witness_proof_digest is null
	  and owner_id = $8
	  and owner_generation = $9
	  and owner_expires_at = $10`

const markRecordDeletionOutcomeUnknownSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    release_epoch = $3,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $4
	  and release_epoch = 0
	  and ledger_entry_type is null
	  and ledger_sequence is null
	  and ledger_entry_hash is null
	  and witness_proof_digest is null
	  and owner_id = $5
	  and owner_generation = $6
	  and owner_expires_at = $7`

const recordDeletionOutcomeEntrySQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    ledger_entry_type = $3,
	    ledger_sequence = $4,
	    ledger_entry_hash = $5,
	    release_epoch = $6,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $7
	  and release_epoch = $8
	  and ledger_entry_type is null
	  and ledger_sequence is null
	  and ledger_entry_hash is null
	  and witness_proof_digest is null
	  and owner_id = $9
	  and owner_generation = $10
	  and owner_expires_at = $11`

const finalizeNotCommittedReservationSQL = `
	update public.deletion_reservations
	set state = $2,
	    release_epoch = $3,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = transaction_timestamp()
	where reservation_id = $1
	  and project_id = $4
	  and object_kind = $5
	  and object_id = $6
	  and fence_epoch = $7
	  and state = 'fenced'
	  and release_epoch = 0
	  and owner_id = $8
	  and owner_generation = $9
	  and owner_expires_at = $10`

const finalizeNotCommittedFenceSQL = `
	update public.deletion_fence_leases
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $6`

const finalizeNotCommittedOperationSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    witness_proof_digest = $3,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = transaction_timestamp(),
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $4
	  and ledger_entry_type = $5
	  and ledger_sequence = $6
	  and ledger_entry_hash = $7
	  and release_epoch = $8
	  and witness_proof_digest is null
	  and owner_id = $9
	  and owner_generation = $10
	  and owner_expires_at = $11`

const insertRecordDeletionNotCommittedAuditSQL = `
	insert into public.record_deletion_audits (
		audit_id, operation_id, project_id, event_kind, reason_code, occurred_at
	) values ($1, $2, $3, $4, $5, transaction_timestamp())`

const promotePermanentDeletionReservationSQL = `
	update public.deletion_reservations
	set state = $2,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = transaction_timestamp()
	where reservation_id = $1
	  and project_id = $3
	  and object_kind = $4
	  and object_id = $5
	  and fence_epoch = $6
	  and state = 'fenced'
	  and release_epoch = 0
	  and owner_id = $7
	  and owner_generation = $8
	  and owner_expires_at = $9`

const promotePermanentDeletionOperationSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $3
	  and ledger_entry_type = $4
	  and ledger_sequence = $5
	  and ledger_entry_hash = $6
	  and witness_proof_digest is not null
	  and release_epoch = 0
	  and owner_id = $7
	  and owner_generation = $8
	  and owner_expires_at = $9`

const insertRecordDeletionCommittedAuditSQL = `
	insert into public.record_deletion_audits (
		audit_id, operation_id, project_id, event_kind, reason_code, occurred_at
	) values ($1, $2, $3, $4, $5, transaction_timestamp())`

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

const advanceRecordDeletionStateSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $3
	  and ledger_entry_type = 'delete_commit'
	  and ledger_sequence = $4
	  and ledger_entry_hash = $5
	  and witness_proof_digest is not null
	  and release_epoch = 0
	  and owner_id = $6
	  and owner_generation = $7
	  and owner_expires_at = $8`

const completeRecordDeletionOnlinePurgeSQL = `
	update public.record_purge_operations as operation
	set operation_state = $2,
	    receipt_digest = $3,
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null,
	    completed_at = transaction_timestamp(),
	    updated_at = transaction_timestamp()
	where operation.operation_id = $1
	  and operation.operation_state = $4
	  and operation.ledger_entry_type = 'delete_commit'
	  and operation.ledger_sequence = $5
	  and operation.ledger_entry_hash = $6
	  and operation.witness_proof_digest is not null
	  and operation.release_epoch = 0
	  and operation.receipt_digest is null
	  and operation.owner_id = $7
	  and operation.owner_generation = $8
	  and operation.owner_expires_at = $9
	  and exists (
		select 1
		from public.deletion_reservations as reservation
		where reservation.reservation_id = operation.reservation_id
		  and reservation.project_id = operation.project_id
		  and reservation.project_id = $10
		  and reservation.object_kind = $11
		  and reservation.object_id = $12
		  and reservation.fence_epoch = $13
		  and reservation.state = 'committed'
	  )`

const markRecordDeletionRetryRequiredSQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    retry_from = $3,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $4
	  and retry_from is null
	  and ledger_entry_type = 'delete_commit'
	  and ledger_sequence = $5
	  and ledger_entry_hash = $6
	  and witness_proof_digest is not null
	  and release_epoch = 0
	  and owner_id = $7
	  and owner_generation = $8
	  and owner_expires_at = $9`

const resumeRecordDeletionRetrySQL = `
	update public.record_purge_operations
	set operation_state = $2,
	    retry_from = null,
	    updated_at = transaction_timestamp()
	where operation_id = $1
	  and operation_state = $3
	  and retry_from = $4
	  and ledger_entry_type = 'delete_commit'
	  and ledger_sequence = $5
	  and ledger_entry_hash = $6
	  and witness_proof_digest is not null
	  and release_epoch = 0
	  and owner_id = $7
	  and owner_generation = $8
	  and owner_expires_at = $9`

func (repository *PostgresRecordDeletionRepository) MarkDeleteCommitUnknown(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkAppendDeleteCommit {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, markRecordDeletionCommitUnknownSQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateLedgerCommitUnknown,
		recorddeletion.DeletionStateProvisionalFenced,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) RecordDeleteEntry(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	entry recorddeletion.DeletionLedgerEntry,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		entry.Validate() != nil || !sameStoreDeletionLedgerRequest(entry.Request, claim.Request) ||
		entry.Request.EntryType != recorddeletion.LedgerEntryDeleteCommit {
		return ErrRecordDeletionStoreUnavailable
	}
	var from recorddeletion.DeletionState
	switch claim.Stage {
	case recorddeletion.DeletionWorkAppendDeleteCommit:
		from = recorddeletion.DeletionStateProvisionalFenced
	case recorddeletion.DeletionWorkResolveDeleteCommit:
		from = recorddeletion.DeletionStateLedgerCommitUnknown
	default:
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, recordDeletionDeleteEntrySQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateWitnessPending,
		recorddeletion.LedgerEntryDeleteCommit,
		entry.Sequence,
		entry.EntryHash[:],
		from,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) MarkDeleteWitnessed(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	receipt recorddeletion.DeletionWitnessReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkConfirmDeleteWitness || claim.Entry == nil ||
		receipt.Sequence != claim.Entry.Sequence || receipt.EntryHash != claim.Entry.EntryHash ||
		receipt.ProofDigest == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, markRecordDeletionDeleteWitnessedSQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateDeleteRequested,
		receipt.ProofDigest[:],
		recorddeletion.DeletionStateWitnessPending,
		recorddeletion.LedgerEntryDeleteCommit,
		claim.Entry.Sequence,
		claim.Entry.EntryHash[:],
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) MarkOutcomeCommitUnknown(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	releaseEpoch uint64,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit || releaseEpoch == 0 || releaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, markRecordDeletionOutcomeUnknownSQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateReleasePending,
		int64(releaseEpoch),
		recorddeletion.DeletionStateLedgerCommitUnknown,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) RecordOutcomeEntry(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	entry recorddeletion.DeletionLedgerEntry,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		entry.Validate() != nil || entry.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted ||
		entry.Request.ReleaseEpoch == 0 || entry.Request.ReleaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	var from recorddeletion.DeletionState
	var expectedReleaseEpoch uint64
	var expectedRequest recorddeletion.LedgerAppendRequest
	switch claim.Stage {
	case recorddeletion.DeletionWorkResolveDeleteCommit:
		from = recorddeletion.DeletionStateLedgerCommitUnknown
		expectedRequest = claim.Request.AttemptNotCommitted(entry.Request.ReleaseEpoch)
	case recorddeletion.DeletionWorkResolveNotCommitted:
		from = recorddeletion.DeletionStateReleasePending
		expectedReleaseEpoch = claim.Operation.ReleaseEpoch
		expectedRequest = claim.Request
	default:
		return ErrRecordDeletionStoreUnavailable
	}
	if !sameStoreDeletionLedgerRequest(entry.Request, expectedRequest) {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, recordDeletionOutcomeEntrySQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateReleasePending,
		recorddeletion.LedgerEntryAttemptNotCommitted,
		entry.Sequence,
		entry.EntryHash[:],
		int64(entry.Request.ReleaseEpoch),
		from,
		int64(expectedReleaseEpoch),
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) FinalizeNotCommitted(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	receipt recorddeletion.DeletionWitnessReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || repository.newAuditID == nil ||
		claim.Validate() != nil || claim.Stage != recorddeletion.DeletionWorkConfirmNotCommittedWitness ||
		claim.Entry == nil || claim.Entry.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted ||
		receipt.Sequence != claim.Entry.Sequence || receipt.EntryHash != claim.Entry.EntryHash ||
		receipt.ProofDigest == ([sha256.Size]byte{}) || claim.Operation.ReleaseEpoch == 0 ||
		claim.Operation.ReleaseEpoch > math.MaxInt64 {
		return ErrRecordDeletionStoreUnavailable
	}
	auditID, err := repository.newAuditID()
	if err != nil {
		return fmt.Errorf("generate not-committed deletion audit id: %w", err)
	}
	if !validStoredRecordIdentity(auditID, "rda_") {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		reservationResult, err := transaction.tx.Exec(ctx, finalizeNotCommittedReservationSQL,
			claim.Operation.ReservationID,
			recorddeletion.DeletionStateNotCommitted,
			int64(claim.Operation.ReleaseEpoch),
			claim.Operation.Object.ProjectID,
			claim.Operation.Object.ObjectKind,
			claim.Operation.Object.ObjectID,
			claim.Operation.FenceEpoch,
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("finalize not-committed reservation: %w", err)
		}
		if reservationResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		fenceResult, err := transaction.tx.Exec(ctx, finalizeNotCommittedFenceSQL,
			claim.Operation.Object.ProjectID,
			claim.Operation.Object.ObjectKind,
			claim.Operation.Object.ObjectID,
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("finalize not-committed deletion fence: %w", err)
		}
		if fenceResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		operationResult, err := transaction.tx.Exec(ctx, finalizeNotCommittedOperationSQL,
			claim.Operation.OperationID,
			recorddeletion.DeletionStateNotCommitted,
			receipt.ProofDigest[:],
			recorddeletion.DeletionStateReleasePending,
			recorddeletion.LedgerEntryAttemptNotCommitted,
			claim.Entry.Sequence,
			claim.Entry.EntryHash[:],
			int64(claim.Operation.ReleaseEpoch),
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("finalize not-committed operation: %w", err)
		}
		if operationResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		auditResult, err := transaction.tx.Exec(ctx, insertRecordDeletionNotCommittedAuditSQL,
			auditID,
			claim.Operation.OperationID,
			claim.Operation.Object.ProjectID,
			"not_committed",
			claim.Operation.ReasonCode,
		)
		if err != nil {
			return fmt.Errorf("insert not-committed deletion audit: %w", err)
		}
		if auditResult.RowsAffected() != 1 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
}

func (repository *PostgresRecordDeletionRepository) PromotePermanentFence(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || repository.newAuditID == nil ||
		claim.Validate() != nil || claim.Stage != recorddeletion.DeletionWorkPromotePermanentFence ||
		claim.Operation.LedgerSequence == 0 || claim.Operation.LedgerEntryHash == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	auditID, err := repository.newAuditID()
	if err != nil {
		return fmt.Errorf("generate committed deletion audit id: %w", err)
	}
	if !validStoredRecordIdentity(auditID, "rda_") {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		reservationResult, err := transaction.tx.Exec(ctx, promotePermanentDeletionReservationSQL,
			claim.Operation.ReservationID,
			"committed",
			claim.Operation.Object.ProjectID,
			claim.Operation.Object.ObjectKind,
			claim.Operation.Object.ObjectID,
			claim.Operation.FenceEpoch,
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("promote permanent deletion reservation: %w", err)
		}
		if reservationResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		fenceResult, err := transaction.tx.Exec(ctx, finalizeNotCommittedFenceSQL,
			claim.Operation.Object.ProjectID,
			claim.Operation.Object.ObjectKind,
			claim.Operation.Object.ObjectID,
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("expire promoted deletion fence lease: %w", err)
		}
		if fenceResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		operationResult, err := transaction.tx.Exec(ctx, promotePermanentDeletionOperationSQL,
			claim.Operation.OperationID,
			recorddeletion.DeletionStateFencePropagating,
			recorddeletion.DeletionStateDeleteRequested,
			recorddeletion.LedgerEntryDeleteCommit,
			claim.Operation.LedgerSequence,
			claim.Operation.LedgerEntryHash[:],
			claim.Owner.OwnerID,
			claim.Owner.Generation,
			claim.Owner.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("promote permanent deletion operation: %w", err)
		}
		if operationResult.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}

		auditResult, err := transaction.tx.Exec(ctx, insertRecordDeletionCommittedAuditSQL,
			auditID,
			claim.Operation.OperationID,
			claim.Operation.Object.ProjectID,
			"committed",
			claim.Operation.ReasonCode,
		)
		if err != nil {
			return fmt.Errorf("insert committed deletion audit: %w", err)
		}
		if auditResult.RowsAffected() != 1 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
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

func (repository *PostgresRecordDeletionRepository) MarkReadFenced(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.advanceDeletionState(ctx, claim,
		recorddeletion.DeletionStateFencePropagating,
		recorddeletion.DeletionStateReadFenced,
	)
}

func (repository *PostgresRecordDeletionRepository) BeginOnlinePurge(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkBeginOnlinePurge {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.advanceDeletionState(ctx, claim,
		recorddeletion.DeletionStateReadFenced,
		recorddeletion.DeletionStateOnlinePurging,
	)
}

func (repository *PostgresRecordDeletionRepository) advanceDeletionState(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	from recorddeletion.DeletionState,
	to recorddeletion.DeletionState,
) error {
	return repository.execDeletionWorkerTransition(ctx, advanceRecordDeletionStateSQL,
		claim.Operation.OperationID,
		to,
		from,
		claim.Operation.LedgerSequence,
		claim.Operation.LedgerEntryHash[:],
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) CompleteOnlinePurge(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	receipt recorddeletion.OnlinePurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkPurgeOnline || receipt.OperationID != claim.Operation.OperationID ||
		receipt.ReceiptDigest == ([sha256.Size]byte{}) {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, completeRecordDeletionOnlinePurgeSQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateOnlinePurged,
		receipt.ReceiptDigest[:],
		recorddeletion.DeletionStateOnlinePurging,
		claim.Operation.LedgerSequence,
		claim.Operation.LedgerEntryHash[:],
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
		claim.Operation.Object.ProjectID,
		claim.Operation.Object.ObjectKind,
		claim.Operation.Object.ObjectID,
		claim.Operation.FenceEpoch,
	)
}

func (repository *PostgresRecordDeletionRepository) MarkRetryRequired(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	retryStage recorddeletion.DeletionWorkStage,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil {
		return ErrRecordDeletionStoreUnavailable
	}
	from, expectedClaimStage, ok := recordDeletionRetryState(retryStage)
	if !ok || claim.Stage != expectedClaimStage || claim.Operation.State != from {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, markRecordDeletionRetryRequiredSQL,
		claim.Operation.OperationID,
		recorddeletion.DeletionStateRetryRequired,
		retryStage,
		from,
		claim.Operation.LedgerSequence,
		claim.Operation.LedgerEntryHash[:],
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func (repository *PostgresRecordDeletionRepository) ResumeRetry(
	ctx context.Context,
	claim recorddeletion.ClaimedDeletionWork,
	retryStage recorddeletion.DeletionWorkStage,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil ||
		claim.Stage != recorddeletion.DeletionWorkResolveRetry || claim.RetryStage != retryStage {
		return ErrRecordDeletionStoreUnavailable
	}
	to, _, ok := recordDeletionRetryState(retryStage)
	if !ok {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.execDeletionWorkerTransition(ctx, resumeRecordDeletionRetrySQL,
		claim.Operation.OperationID,
		to,
		recorddeletion.DeletionStateRetryRequired,
		retryStage,
		claim.Operation.LedgerSequence,
		claim.Operation.LedgerEntryHash[:],
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
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

func (repository *PostgresRecordDeletionRepository) execDeletionWorkerTransition(
	ctx context.Context,
	statement string,
	arguments ...any,
) error {
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		result, err := transaction.tx.Exec(ctx, statement, arguments...)
		if err != nil {
			return fmt.Errorf("persist record deletion worker transition: %w", err)
		}
		if result.RowsAffected() != 1 {
			return recordplatform.ErrLostOwnerLease
		}
		return nil
	})
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

const lockRecordCorePurgeSQL = `
	select operation.operation_state,
	       reservation.state,
	       reservation.object_id,
	       reservation.fence_epoch
	from public.record_purge_operations as operation
	join public.deletion_reservations as reservation
	  on reservation.reservation_id = operation.reservation_id
	where operation.operation_id = $1
	  and operation.reservation_id = $2
	  and operation.project_id = $3
	  and reservation.project_id = $3
	  and reservation.object_kind = 'record'
	  and reservation.object_id = $4
	for update of operation, reservation`

const loadRecordCorePurgeReceiptSQL = `
	select removed_surface_digest,
	       receipt_digest,
	       removed_row_count,
	       verified_absent_at
	from public.record_core_purge_receipts
	where operation_id = $1
	  and adapter_name = 'record_core'`

const clearRecordCoreCurrentProjectionSQL = `
	update public.records
	set current_revision_id = null,
	    current_title = null,
	    current_record_type = null,
	    current_business_status = null,
	    current_status_group = null,
	    current_impact_level = null,
	    current_occurred_at = null,
	    current_completed_at = null,
	    current_visibility_scope = null,
	    current_visibility_digest = null,
	    current_owner_id = null,
	    current_follow_up_at = null,
	    updated_at = transaction_timestamp()
	where record_id = $1
	  and project_id = $2`

var deleteRecordCoreSurfaceSQL = []string{
	`delete from public.record_draft_checkpoints
	 where draft_id in (
		select draft_id from public.record_drafts where record_id = $1
	 )`,
	`delete from public.record_drafts where record_id = $1`,
	`delete from public.record_domain_activities where record_id = $1`,
	`delete from public.record_revision_participants
	 where revision_id in (
		select revision_id from public.record_revisions where record_id = $1
	 )`,
	`delete from public.record_revision_tags
	 where revision_id in (
		select revision_id from public.record_revisions where record_id = $1
	 )`,
	`delete from public.record_revision_subjects
	 where revision_id in (
		select revision_id from public.record_revisions where record_id = $1
	 )`,
	`delete from public.record_revisions where record_id = $1`,
	`delete from public.records where record_id = $1`,
}

const deleteRecordCoreDeliveryEpochSQL = `
	delete from public.content_delivery_epochs
	where object_id = $1
	  and project_id = $2
	  and object_kind = 'record'`

const recordCoreContentPresentSQL = `
	select exists (
		select 1 from public.records where record_id = $1
		union all
		select 1 from public.record_revisions where record_id = $1
		union all
		select 1
		from public.record_revision_subjects subject
		join public.record_revisions revision on revision.revision_id = subject.revision_id
		where revision.record_id = $1
		union all
		select 1
		from public.record_revision_tags tag
		join public.record_revisions revision on revision.revision_id = tag.revision_id
		where revision.record_id = $1
		union all
		select 1
		from public.record_revision_participants participant
		join public.record_revisions revision on revision.revision_id = participant.revision_id
		where revision.record_id = $1
		union all
		select 1 from public.record_drafts where record_id = $1
		union all
		select 1
		from public.record_draft_checkpoints checkpoint
		join public.record_drafts draft on draft.draft_id = checkpoint.draft_id
		where draft.record_id = $1
		union all
		select 1 from public.record_domain_activities where record_id = $1
		union all
		select 1
		from public.content_delivery_epochs
		where object_id = $1
		  and project_id = $2
		  and object_kind = 'record'
	) as content_present`

const insertRecordCorePurgeReceiptSQL = `
	insert into public.record_core_purge_receipts (
		operation_id,
		adapter_name,
		removed_surface_digest,
		receipt_digest,
		removed_row_count,
		verified_absent_at
	) values ($1, 'record_core', $2, $3, $4, transaction_timestamp())
	returning verified_absent_at`

func (repository *PostgresRecordDeletionRepository) RecordCoreHealth(
	ctx context.Context,
) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, ErrRecordDeletionStoreUnavailable
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		surfaces := recorddeletion.RecordCoreSurfaceNames()
		names := make([]string, len(surfaces))
		for index, surface := range surfaces {
			names[index] = string(surface)
		}
		var count int64
		if err := transaction.tx.QueryRow(ctx, recordCoreHealthSQL, names).Scan(&count); err != nil {
			return fmt.Errorf("read record core health: %w", err)
		}
		if count != int64(len(names)) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		proof := digestRecordCoreHealth(names)
		observed, err := recorddeletion.NewAdapterHealthSnapshot(true, 1, proof)
		if err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		health = observed
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
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
		var dependencyMaterial []byte
		var impactMaterial []byte
		if err := transaction.tx.QueryRow(ctx, previewRecordCoreSQL,
			target.Object.ProjectID,
			target.CurrentRevisionID,
			int64(target.LockVersion),
			int64(target.AuthorizationEpoch),
			int64(target.ContentDeliveryEpoch),
			target.Object.ObjectID,
		).Scan(&dependencyMaterial, &impactMaterial); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionPreviewStale
			}
			return fmt.Errorf("preview record core: %w", err)
		}
		if len(dependencyMaterial) == 0 || len(impactMaterial) == 0 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		preview = recorddeletion.AdapterPreviewSnapshot{
			DependencyDigest: digestRecordCorePreview(target.DependencyGraphDigest, dependencyMaterial),
			ImpactDigest:     digestRecordCoreImpact(impactMaterial),
			SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
		}
		if err := preview.Validate(); err != nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
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
		operation := command.Operation
		var operationState string
		var reservationState string
		var recordID string
		var fenceEpoch int64
		if err := transaction.tx.QueryRow(ctx, lockRecordCorePurgeSQL,
			operation.OperationID,
			operation.ReservationID,
			operation.Object.ProjectID,
			operation.Object.ObjectID,
		).Scan(&operationState, &reservationState, &recordID, &fenceEpoch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			return fmt.Errorf("lock record core purge: %w", err)
		}
		if operationState != string(recorddeletion.DeletionStateOnlinePurging) || reservationState != "committed" ||
			recordID != operation.Object.ObjectID || fenceEpoch < 1 || uint64(fenceEpoch) != uint64(operation.FenceEpoch) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}

		existing, err := loadRecordCorePurgeReceipt(ctx, transaction.tx, operation)
		if err == nil {
			if existing.SurfaceDigest != command.SurfaceDigest ||
				existing.ReceiptDigest != digestRecordCorePurgeReceipt(operation, command.SurfaceDigest, existing.RemovedRowCount) {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			receipt = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		projectionResult, err := transaction.tx.Exec(ctx, clearRecordCoreCurrentProjectionSQL,
			operation.Object.ObjectID,
			operation.Object.ProjectID,
		)
		if err != nil {
			return fmt.Errorf("clear record core current projection: %w", err)
		}
		if projectionResult.RowsAffected() != 1 {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}

		var removedRowCount uint64
		for _, statement := range deleteRecordCoreSurfaceSQL {
			result, err := transaction.tx.Exec(ctx, statement, operation.Object.ObjectID)
			if err != nil {
				return fmt.Errorf("purge record core surface: %w", err)
			}
			rows := result.RowsAffected()
			if rows < 0 || uint64(rows) > math.MaxUint64-removedRowCount {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			removedRowCount += uint64(rows)
		}
		result, err := transaction.tx.Exec(ctx, deleteRecordCoreDeliveryEpochSQL,
			operation.Object.ObjectID,
			operation.Object.ProjectID,
		)
		if err != nil {
			return fmt.Errorf("purge record core delivery epoch: %w", err)
		}
		rows := result.RowsAffected()
		if rows < 0 || uint64(rows) > math.MaxUint64-removedRowCount {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		removedRowCount += uint64(rows)

		var contentPresent bool
		if err := transaction.tx.QueryRow(ctx, recordCoreContentPresentSQL,
			operation.Object.ObjectID,
			operation.Object.ProjectID,
		).Scan(&contentPresent); err != nil {
			return fmt.Errorf("verify record core absence: %w", err)
		}
		if contentPresent {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}

		receiptDigest := digestRecordCorePurgeReceipt(operation, command.SurfaceDigest, removedRowCount)
		var verifiedAbsentAt time.Time
		if err := transaction.tx.QueryRow(ctx, insertRecordCorePurgeReceiptSQL,
			operation.OperationID,
			command.SurfaceDigest[:],
			receiptDigest[:],
			int64(removedRowCount),
		).Scan(&verifiedAbsentAt); err != nil {
			return fmt.Errorf("insert record core purge receipt: %w", err)
		}
		if verifiedAbsentAt.IsZero() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		receipt = recorddeletion.AdapterPurgeReceipt{
			AdapterName:      recorddeletion.AdapterNameRecordCore,
			OperationID:      operation.OperationID,
			SurfaceDigest:    command.SurfaceDigest,
			ReceiptDigest:    receiptDigest,
			RemovedRowCount:  removedRowCount,
			VerifiedAbsentAt: verifiedAbsentAt,
		}
		return nil
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
		receipt.OperationID != command.Operation.OperationID ||
		receipt.SurfaceDigest != command.SurfaceDigest || receipt.VerifiedAbsentAt.IsZero() ||
		receipt.ReceiptDigest != digestRecordCorePurgeReceipt(command.Operation, command.SurfaceDigest, receipt.RemovedRowCount) {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		stored, err := loadRecordCorePurgeReceipt(ctx, transaction.tx, command.Operation)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			return err
		}
		if stored.AdapterName != receipt.AdapterName || stored.OperationID != receipt.OperationID ||
			stored.SurfaceDigest != receipt.SurfaceDigest || stored.ReceiptDigest != receipt.ReceiptDigest ||
			stored.RemovedRowCount != receipt.RemovedRowCount || !stored.VerifiedAbsentAt.Equal(receipt.VerifiedAbsentAt) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		var contentPresent bool
		if err := transaction.tx.QueryRow(ctx, recordCoreContentPresentSQL,
			command.Operation.Object.ObjectID,
			command.Operation.Object.ProjectID,
		).Scan(&contentPresent); err != nil {
			return fmt.Errorf("verify persisted record core absence: %w", err)
		}
		if contentPresent {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		return nil
	})
}

func loadRecordCorePurgeReceipt(
	ctx context.Context,
	tx pgx.Tx,
	operation recorddeletion.DeletionOperation,
) (recorddeletion.AdapterPurgeReceipt, error) {
	var surfaceDigestBytes []byte
	var receiptDigestBytes []byte
	var removedRowCount int64
	var verifiedAbsentAt time.Time
	if err := tx.QueryRow(ctx, loadRecordCorePurgeReceiptSQL,
		operation.OperationID,
	).Scan(&surfaceDigestBytes, &receiptDigestBytes, &removedRowCount, &verifiedAbsentAt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if len(surfaceDigestBytes) != sha256.Size || len(receiptDigestBytes) != sha256.Size || removedRowCount < 0 || verifiedAbsentAt.IsZero() {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var surfaceDigest [sha256.Size]byte
	var receiptDigest [sha256.Size]byte
	copy(surfaceDigest[:], surfaceDigestBytes)
	copy(receiptDigest[:], receiptDigestBytes)
	return recorddeletion.AdapterPurgeReceipt{
		AdapterName:      recorddeletion.AdapterNameRecordCore,
		OperationID:      operation.OperationID,
		SurfaceDigest:    surfaceDigest,
		ReceiptDigest:    receiptDigest,
		RemovedRowCount:  uint64(removedRowCount),
		VerifiedAbsentAt: verifiedAbsentAt,
	}, nil
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
	operation := recorddeletion.DeletionOperation{
		OperationID:   operationID,
		ReservationID: reservationID,
		Object:        object,
		ReasonCode:    recorddeletion.DeletionReasonCode(reasonCode),
		State:         recorddeletion.DeletionState(operationState),
		FenceEpoch:    recordplatform.ContentEpoch(fenceEpoch),
		ReleaseEpoch:  uint64(releaseEpoch),
	}
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
