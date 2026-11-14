package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

// Every V1 continuation names the persisted operation and the caller's exact
// claim. The database command owns the state change and repeats its checks
// after acquiring the ordered locks.
type deletionV1OwnerEnvelope struct {
	OperationID     string `json:"operation_id"`
	ReservationID   string `json:"reservation_id"`
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	FenceEpoch      uint64 `json:"fence_epoch"`
	OwnerID         string `json:"owner_id"`
	OwnerGeneration uint64 `json:"owner_generation"`
	OwnerExpiresAt  string `json:"owner_expires_at"`
}

type deletionV1LedgerEnvelope struct {
	deletionV1OwnerEnvelope
	LedgerSequence  uint64 `json:"ledger_sequence"`
	LedgerEntryHash string `json:"ledger_entry_hash"`
}

type deletionV1WitnessEnvelope struct {
	deletionV1LedgerEnvelope
	WitnessProofDigest string `json:"witness_proof_digest"`
}

func deletionV1Owner(claim recorddeletion.ClaimedDeletionWork) (deletionV1OwnerEnvelope, error) {
	if claim.Validate() != nil || claim.Operation.ContractVersion != recorddeletion.DeletionContractV1 ||
		uint64(claim.Operation.FenceEpoch) > math.MaxInt64 || claim.Owner.Generation > math.MaxInt64 ||
		claim.Owner.ExpiresAt.Nanosecond()%1000 != 0 {
		return deletionV1OwnerEnvelope{}, ErrRecordDeletionStoreUnavailable
	}
	return deletionV1OwnerEnvelope{
		OperationID: claim.Operation.OperationID, ReservationID: claim.Operation.ReservationID,
		ProjectID: claim.Operation.Object.ProjectID, RecordID: claim.Operation.Object.ObjectID,
		FenceEpoch: uint64(claim.Operation.FenceEpoch), OwnerID: claim.Owner.OwnerID,
		OwnerGeneration: claim.Owner.Generation, OwnerExpiresAt: claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func deletionV1Ledger(claim recorddeletion.ClaimedDeletionWork, entry recorddeletion.DeletionLedgerEntry) (deletionV1LedgerEnvelope, error) {
	owner, err := deletionV1Owner(claim)
	if err != nil || entry.Validate() != nil || entry.Sequence > math.MaxInt64 {
		return deletionV1LedgerEnvelope{}, ErrRecordDeletionStoreUnavailable
	}
	return deletionV1LedgerEnvelope{deletionV1OwnerEnvelope: owner, LedgerSequence: entry.Sequence,
		LedgerEntryHash: hex.EncodeToString(entry.EntryHash[:])}, nil
}

func deletionV1OperationLedger(claim recorddeletion.ClaimedDeletionWork) (deletionV1LedgerEnvelope, error) {
	owner, err := deletionV1Owner(claim)
	if err != nil || claim.Operation.LedgerSequence == 0 || claim.Operation.LedgerSequence > math.MaxInt64 ||
		claim.Operation.LedgerEntryHash == ([32]byte{}) {
		return deletionV1LedgerEnvelope{}, ErrRecordDeletionStoreUnavailable
	}
	return deletionV1LedgerEnvelope{deletionV1OwnerEnvelope: owner, LedgerSequence: claim.Operation.LedgerSequence,
		LedgerEntryHash: hex.EncodeToString(claim.Operation.LedgerEntryHash[:])}, nil
}

func (repository *PostgresRecordDeletionRepository) runV1DeletionCommand(ctx context.Context, statement string, payload any) error {
	body, err := encodeRecordPurgeCommand(payload)
	if err != nil {
		return ErrRecordDeletionStoreUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var applied bool
		if err := transaction.tx.QueryRow(ctx, statement, body).Scan(&applied); err != nil {
			return fmt.Errorf("run restricted record deletion continuation: %w", err)
		}
		if !applied {
			return recordplatform.ErrLostOwnerLease
		}
		return nil
	})
}
