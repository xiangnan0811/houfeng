package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// ApplyRecoveryEntry runs only under the adapter's live maintenance fence.
// The dedicated admin command establishes recovering admission in this same
// transaction; ordinary runtime admission cannot authorize a closed source.
func (repository *PostgresRecordDeletionRepository) ApplyRecoveryEntry(
	ctx context.Context,
	command recorddeletion.RecoveryReplayCommand,
) (recorddeletion.RecoveryReplayReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.RecoveryReplayReceipt{}, ErrRecordDeletionStoreUnavailable
	}
	var receipt recorddeletion.RecoveryReplayReceipt
	err := command.WithHeldMaintenance(ctx, func(heldCtx context.Context) error {
		locator := command.VerifiedEntry.Locator()
		entry := command.VerifiedEntry.Entry()
		fingerprint, err := entry.Request.RequestFingerprint.PersistedBytes()
		if err != nil {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		begin, err := encodeRecordPurgeCommand(struct {
			LedgerDomain          string `json:"ledger_domain"`
			LedgerSequence        uint64 `json:"ledger_sequence"`
			LedgerEntryHash       string `json:"ledger_entry_hash"`
			MaintenanceRID        string `json:"maintenance_rid"`
			MaintenanceGeneration uint64 `json:"maintenance_generation"`
		}{hex.EncodeToString(locator.LedgerDomain[:]), locator.Sequence, hex.EncodeToString(locator.EntryHash[:]),
			command.MaintenanceRID, command.MaintenanceGeneration})
		if err != nil {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		body, err := encodeRecordPurgeCommand(struct {
			LedgerDomain          string `json:"ledger_domain"`
			LedgerSequence        uint64 `json:"ledger_sequence"`
			LedgerEntryHash       string `json:"ledger_entry_hash"`
			PreviousSequence      uint64 `json:"previous_sequence"`
			PreviousHash          string `json:"previous_hash"`
			RequestFingerprint    string `json:"request_fingerprint"`
			SurfaceDigest         string `json:"surface_digest"`
			MaintenanceRID        string `json:"maintenance_rid"`
			MaintenanceGeneration uint64 `json:"maintenance_generation"`
		}{hex.EncodeToString(locator.LedgerDomain[:]), locator.Sequence, hex.EncodeToString(locator.EntryHash[:]),
			command.Cursor.Sequence, hex.EncodeToString(command.Cursor.EntryHash[:]), hex.EncodeToString(fingerprint[:]),
			hex.EncodeToString(command.SurfaceDigest[:]), command.MaintenanceRID, command.MaintenanceGeneration})
		if err != nil {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		tx, err := repository.platform.startTransactionWithOptions(heldCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(heldCtx) }()
		if _, err := tx.Exec(heldCtx, `select public.record_deletion_recovery_begin($1)`, begin); err != nil {
			return fmt.Errorf("begin record deletion recovery admission: %w", err)
		}
		var sequence int64
		var entryHash, surfaceDigest []byte
		var purged bool
		if err := tx.QueryRow(heldCtx, `select sequence, entry_hash, surface_digest, content_purged
			from public.record_deletion_replay_v1($1)`, body).Scan(&sequence, &entryHash, &surfaceDigest, &purged); err != nil {
			return fmt.Errorf("replay verified record deletion: %w", err)
		}
		if sequence <= 0 || uint64(sequence) != locator.Sequence || !equalRecordPurgeDigest(entryHash, locator.EntryHash) ||
			!equalRecordPurgeDigest(surfaceDigest, command.SurfaceDigest) || purged != command.PurgeContent {
			return recorddeletion.ErrRecoveryContractUnavailable
		}
		if err := command.Validate(); err != nil {
			return err
		}
		if err := tx.Commit(heldCtx); err != nil {
			return fmt.Errorf("commit record deletion replay: %w", err)
		}
		var storedHash, storedSurface [sha256.Size]byte
		copy(storedHash[:], entryHash)
		copy(storedSurface[:], surfaceDigest)
		receipt = recorddeletion.RecoveryReplayReceipt{
			Sequence: uint64(sequence), EntryHash: storedHash, SurfaceDigest: storedSurface, ContentPurged: purged,
		}
		return nil
	})
	if err != nil {
		return recorddeletion.RecoveryReplayReceipt{}, err
	}
	return receipt, nil
}
