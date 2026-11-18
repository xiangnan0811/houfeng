package deletionledger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// ConfirmDeletionEntry copies the complete missing primary prefix to W2 in one
// transaction, including uninterpreted non-record rows. A prior successful
// copy is exact-readback only. The returned binding is not authority to replay:
// the trusted verifier must still check both snapshots and external continuity.
func (witness *Witness) ConfirmDeletionEntry(ctx context.Context, entry recorddeletion.DeletionLedgerEntry) (recorddeletion.DeletionWitnessReceipt, error) {
	if ctx == nil || witness == nil || witness.client.pool == nil || witness.primary == nil || entry.Validate() != nil {
		return recorddeletion.DeletionWitnessReceipt{}, fmt.Errorf("%w: confirmation request", recorddeletion.ErrInvalidLedgerEvidence)
	}
	primaryTx, err := witness.primary.client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.DeletionWitnessReceipt{}, failure("begin primary confirmation read", err)
	}
	defer func() { _ = primaryTx.Rollback(ctx) }()
	primary, rows, err := witness.primary.client.snapshotTx(ctx, primaryTx, false)
	if err != nil {
		return recorddeletion.DeletionWitnessReceipt{}, err
	}
	if entry.Sequence > primary.Head.Sequence {
		return recorddeletion.DeletionWitnessReceipt{}, recorddeletion.ErrLedgerPending
	}
	original := primary.Entries[entry.Sequence-1]
	if original.Record == nil || original.EntryHash != entry.EntryHash || !equalRequest(original.Record.Request, entry.Request) {
		return recorddeletion.DeletionWitnessReceipt{}, fmt.Errorf("%w: primary entry mismatch", recorddeletion.ErrInvalidLedgerEvidence)
	}
	if err := primaryTx.Commit(ctx); err != nil {
		return recorddeletion.DeletionWitnessReceipt{}, failure("commit primary confirmation read", err)
	}

	if err := witness.copyPrefix(ctx, primary, rows, entry.Sequence, entry.EntryHash, sourceCommitObserverFromContext(ctx)); err != nil {
		return recorddeletion.DeletionWitnessReceipt{}, err
	}
	proof, err := recorddeletion.LedgerWitnessProofDigest(witness.client.ledgerDomain, entry.Sequence, entry.EntryHash, witness.client.expected.ID)
	if err != nil {
		return recorddeletion.DeletionWitnessReceipt{}, err
	}
	return recorddeletion.DeletionWitnessReceipt{Sequence: entry.Sequence, EntryHash: entry.EntryHash, ProofDigest: proof}, nil
}

// copyPrefix is shared by the record and source paths after each has checked
// its own target against an independent primary snapshot. Even opaque rows
// must match every SQL projection field already present at the witness.
func (witness *Witness) copyPrefix(ctx context.Context, primary recorddeletion.LedgerSnapshot, rows []storedRow, sequence uint64, hash [32]byte, observer sourceCommitObserverFunc) error {
	if sequence == 0 || sequence > uint64(len(rows)) || primary.Entries[sequence-1].EntryHash != hash {
		return fmt.Errorf("%w: witness target", recorddeletion.ErrInvalidLedgerEvidence)
	}
	tx, err := witness.client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return failure("begin witness confirmation", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	observed, copiedRows, err := witness.client.snapshotTx(ctx, tx, true)
	if err != nil {
		return err
	}
	if observed.Head.Sequence > primary.Head.Sequence {
		return fmt.Errorf("%w: witness ahead of primary", recorddeletion.ErrLedgerContinuity)
	}
	for index := range copiedRows {
		if !equalStoredRow(copiedRows[index], rows[index]) {
			return fmt.Errorf("%w: witness projection fork", recorddeletion.ErrLedgerContinuity)
		}
	}
	if sequence > observed.Head.Sequence {
		for index := observed.Head.Sequence; index < sequence; index++ {
			if err := insertWitness(ctx, tx, rows[index]); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `update public.deletion_witness_head set sequence=$3,entry_hash=$4,updated_at=pg_catalog.clock_timestamp()
 where singleton and sequence=$1 and entry_hash=$2`, int64(observed.Head.Sequence), observed.Head.Hash[:], int64(sequence), hash[:])
		if err != nil {
			return failure("advance witness head", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: witness head changed", recorddeletion.ErrLedgerContinuity)
		}
		if observer != nil {
			return commitObservedSource(ctx, tx, "w2-commit", "commit witness confirmation", observer)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return failure("commit witness confirmation", err)
	}
	return nil
}
