package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

var errRecordDeletionAdmin = errors.New("record deletion management admission or operation failed")

type recordDeletionAdminStore struct {
	pool       *pgxpool.Pool
	deployment recordplatform.DeploymentID
	project    recordplatform.ProjectID
	fence      recorddeletion.RecoveryMaintenanceFence
}

type recordDeletionAdminTx struct {
	tx         pgx.Tx
	deployment recordplatform.DeploymentID
	project    recordplatform.ProjectID
}

func (installer recordDeletionAdminTx) InstallVerifiedEntry(ctx context.Context, payload []byte) error {
	if len(payload) == 0 {
		return errRecordDeletionAdmin
	}
	var binding struct {
		DeploymentID string `json:"deployment_id"`
		ProjectID    string `json:"project_id"`
	}
	if json.Unmarshal(payload, &binding) != nil || binding.DeploymentID != string(installer.deployment) || binding.ProjectID != string(installer.project) {
		return errRecordDeletionAdmin
	}
	if _, err := installer.tx.Exec(ctx, `select public.record_deletion_verify_legacy_entry($1::bytea)`, payload); err != nil {
		return fmt.Errorf("install legacy authority: %w", err)
	}
	return nil
}

func (store *recordDeletionAdminStore) WithLegacyOperation(ctx context.Context, operationID string, fn func(context.Context, recorddeletion.LegacyLedgerOperation, recorddeletion.LegacyVerifiedEntryInstaller) error) error {
	if ctx == nil || store == nil || store.pool == nil || store.fence == nil || fn == nil ||
		recordplatform.ValidateDeploymentID(store.deployment) != nil || recordplatform.ValidateProjectID(store.project) != nil {
		return errRecordDeletionAdmin
	}
	rid, generation, err := store.fence.RecoveryBinding()
	if err != nil || generation > math.MaxInt64 || (rid == "" && generation != 0) {
		return errRecordDeletionAdmin
	}
	command, err := json.Marshal(struct {
		DeploymentID          string `json:"deployment_id"`
		ProjectID             string `json:"project_id"`
		OperationID           string `json:"operation_id"`
		MaintenanceRID        string `json:"maintenance_rid"`
		MaintenanceGeneration uint64 `json:"maintenance_generation"`
	}{string(store.deployment), string(store.project), operationID, rid, generation})
	if err != nil {
		return errRecordDeletionAdmin
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errRecordDeletionAdmin
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var body []byte
	if err := tx.QueryRow(ctx, `select public.record_deletion_legacy_identity($1::bytea)`, command).Scan(&body); err != nil {
		return errRecordDeletionAdmin
	}
	operation, err := decodeRecordDeletionLegacyIdentity(body)
	if err != nil || operation.Request.DeploymentID != store.deployment || operation.Request.ProjectID != store.project || operation.Request.OperationID != operationID {
		return errRecordDeletionAdmin
	}
	if err := fn(ctx, operation, recordDeletionAdminTx{tx: tx, deployment: store.deployment, project: store.project}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verified legacy authority (outcome uncertain): %w", err)
	}
	return nil
}

func (store *recordDeletionAdminStore) WithLegacyRecovery(ctx context.Context, fn func(context.Context, recorddeletion.LegacyVerifiedEntryInstaller) error) error {
	if ctx == nil || store == nil || store.pool == nil || store.fence == nil || fn == nil {
		return errRecordDeletionAdmin
	}
	rid, generation, err := store.fence.RecoveryBinding()
	if err != nil || rid == "" || generation > math.MaxInt64 {
		return errRecordDeletionAdmin
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errRecordDeletionAdmin
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(ctx, recordDeletionAdminTx{tx: tx, deployment: store.deployment, project: store.project}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verified recovery authority (outcome uncertain): %w", err)
	}
	return nil
}

type recordDeletionLegacyProjection struct {
	DeploymentID            string `json:"deployment_id"`
	ProjectID               string `json:"project_id"`
	OperationID             string `json:"operation_id"`
	ActorID                 string `json:"actor_id"`
	ReasonCode              string `json:"reason_code"`
	DeletionContractVersion uint64 `json:"deletion_contract_version"`
	OperationState          string `json:"operation_state"`
	LedgerEntryType         string `json:"ledger_entry_type"`
	LedgerSequence          uint64 `json:"ledger_sequence"`
	LedgerEntryHash         string `json:"ledger_entry_hash"`
	ReleaseEpoch            uint64 `json:"release_epoch"`
	ObjectKind              string `json:"object_kind"`
	ObjectID                string `json:"object_id"`
	TokenCommitment         string `json:"token_commitment"`
	RequestFingerprint      string `json:"request_fingerprint"`
}

var recordDeletionLegacyProjectionSchema = sourceRequestSchema{
	"deployment_id": nil, "project_id": nil, "operation_id": nil, "actor_id": nil, "reason_code": nil,
	"deletion_contract_version": nil, "operation_state": nil, "ledger_entry_type": nil, "ledger_sequence": nil,
	"ledger_entry_hash": nil, "release_epoch": nil, "object_kind": nil, "object_id": nil,
	"token_commitment": nil, "request_fingerprint": nil,
}

func decodeRecordDeletionLegacyIdentity(body []byte) (recorddeletion.LegacyLedgerOperation, error) {
	var projection recordDeletionLegacyProjection
	if decodeSourceEnrollmentRequest(body, &projection, recordDeletionLegacyProjectionSchema) != nil ||
		projection.DeletionContractVersion != recorddeletion.RecordDeletionContractVersionV1 ||
		projection.LedgerSequence > math.MaxInt64 || projection.ReleaseEpoch > math.MaxInt64 {
		return recorddeletion.LegacyLedgerOperation{}, errRecordDeletionAdmin
	}
	token, err := decodeRecordDeletionDigest(projection.TokenCommitment, false)
	if err != nil {
		return recorddeletion.LegacyLedgerOperation{}, err
	}
	fingerprintBytes, err := decodeRecordDeletionDigest(projection.RequestFingerprint, false)
	if err != nil {
		return recorddeletion.LegacyLedgerOperation{}, err
	}
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprintBytes[:])
	if err != nil {
		return recorddeletion.LegacyLedgerOperation{}, errRecordDeletionAdmin
	}
	entryHash, err := decodeRecordDeletionDigest(projection.LedgerEntryHash, true)
	if err != nil || (projection.LedgerSequence == 0) != (projection.LedgerEntryHash == "") {
		return recorddeletion.LegacyLedgerOperation{}, errRecordDeletionAdmin
	}
	state := recorddeletion.DeletionState(projection.OperationState)
	entryType := recorddeletion.LedgerEntryDeleteCommit
	contract := projection.DeletionContractVersion
	if state == recorddeletion.DeletionStateReleasePending || state == recorddeletion.DeletionStateNotCommitted {
		entryType = recorddeletion.LedgerEntryAttemptNotCommitted
		contract = 0
	}
	if projection.LedgerEntryType != "" && projection.LedgerEntryType != string(entryType) ||
		(projection.LedgerSequence > 0 && projection.LedgerEntryType == "") {
		return recorddeletion.LegacyLedgerOperation{}, errRecordDeletionAdmin
	}
	operation := recorddeletion.LegacyLedgerOperation{
		State: state, LedgerSequence: projection.LedgerSequence, LedgerEntryHash: entryHash,
		Request: recorddeletion.LedgerAppendRequest{
			EntryType: entryType, DeploymentID: recordplatform.DeploymentID(projection.DeploymentID),
			ProjectID: recordplatform.ProjectID(projection.ProjectID), OperationID: projection.OperationID,
			ActorID: projection.ActorID, Object: recordplatform.ObjectRef{ProjectID: projection.ProjectID,
				ObjectKind: projection.ObjectKind, ObjectID: projection.ObjectID},
			TokenCommitment: token, RequestFingerprint: fingerprint, ReasonCode: recorddeletion.DeletionReasonCode(projection.ReasonCode),
			DeletionContractVersion: contract, ReleaseEpoch: projection.ReleaseEpoch,
		},
	}
	if operation.Request.Validate() != nil {
		return recorddeletion.LegacyLedgerOperation{}, errRecordDeletionAdmin
	}
	return operation, nil
}

func decodeRecordDeletionDigest(value string, optional bool) ([32]byte, error) {
	var result [32]byte
	if optional && value == "" {
		return result, nil
	}
	if len(value) != 64 || strings.ToLower(value) != value {
		return result, errRecordDeletionAdmin
	}
	data, err := hex.DecodeString(value)
	if err != nil || len(data) != len(result) {
		return result, errRecordDeletionAdmin
	}
	copy(result[:], data)
	if result == ([32]byte{}) {
		return [32]byte{}, errRecordDeletionAdmin
	}
	return result, nil
}
