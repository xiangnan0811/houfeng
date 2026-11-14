package deletionledger

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// AppendDeletionEntry appends a delete_commit or resolves its exact prior
// commit. The ordinary interface cannot create attempt_not_committed entries;
// those require the separately fenced administrative absence ceremony.
func (primary *Primary) AppendDeletionEntry(ctx context.Context, request recorddeletion.LedgerAppendRequest) (recorddeletion.DeletionLedgerEntry, error) {
	if ctx == nil || primary == nil || primary.client.pool == nil || request.Validate() != nil || request.EntryType != recorddeletion.LedgerEntryDeleteCommit {
		return recorddeletion.DeletionLedgerEntry{}, fmt.Errorf("%w: append request", recorddeletion.ErrInvalidLedgerEvidence)
	}
	tx, err := primary.client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, failure("begin append", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, rows, err := primary.client.snapshotTx(ctx, tx, true)
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, err
	}
	entry, err := primary.appendLocked(ctx, tx, snapshot, rows, request)
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.DeletionLedgerEntry{}, failure("commit append", err)
	}
	return entry, nil
}

// ResolveDeletionEntry reads back the exact operation. A missing row is always
// unresolved; a lookup does not prove that an old producer was fenced.
func (primary *Primary) ResolveDeletionEntry(ctx context.Context, request recorddeletion.LedgerAppendRequest) (recorddeletion.LedgerResolution, error) {
	if ctx == nil || primary == nil || primary.client.pool == nil || request.Validate() != nil {
		return recorddeletion.LedgerResolution{}, fmt.Errorf("%w: resolve request", recorddeletion.ErrInvalidLedgerEvidence)
	}
	tx, err := primary.client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.LedgerResolution{}, failure("begin resolve", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, rows, err := primary.client.snapshotTx(ctx, tx, false)
	if err != nil {
		return recorddeletion.LedgerResolution{}, err
	}
	index := findOperation(rows, request)
	resolution := recorddeletion.LedgerResolution{Kind: recorddeletion.LedgerResolutionUnresolved}
	if index >= 0 {
		row := snapshot.Entries[index]
		if row.Record == nil || !equalRequest(row.Record.Request, request) {
			return recorddeletion.LedgerResolution{}, fmt.Errorf("%w: operation conflict", recorddeletion.ErrInvalidLedgerEvidence)
		}
		resolution = recorddeletion.NewCommittedLedgerResolution(row.Record.DeletionLedgerEntry)
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.LedgerResolution{}, failure("commit resolve", err)
	}
	return resolution, nil
}

func (primary *Primary) appendLocked(ctx context.Context, tx pgx.Tx, snapshot recorddeletion.LedgerSnapshot, rows []storedRow, request recorddeletion.LedgerAppendRequest) (recorddeletion.DeletionLedgerEntry, error) {
	if index := findOperation(rows, request); index >= 0 {
		row := snapshot.Entries[index]
		if row.Record == nil || !equalRequest(row.Record.Request, request) {
			return recorddeletion.DeletionLedgerEntry{}, fmt.Errorf("%w: operation conflict", recorddeletion.ErrInvalidLedgerEvidence)
		}
		return row.Record.DeletionLedgerEntry, nil
	}
	if snapshot.Head.Sequence >= math.MaxInt64 {
		return recorddeletion.DeletionLedgerEntry{}, fmt.Errorf("%w: sequence exhausted", recorddeletion.ErrInvalidLedgerEvidence)
	}
	var confirmed time.Time
	if err := tx.QueryRow(ctx, `select pg_catalog.clock_timestamp()`).Scan(&confirmed); err != nil {
		return recorddeletion.DeletionLedgerEntry{}, failure("read confirmation time", err)
	}
	candidate := recorddeletion.CanonicalLedgerEntry{DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: request, Sequence: snapshot.Head.Sequence + 1},
		LedgerDomain: primary.client.ledgerDomain, PreviousHash: snapshot.Head.Hash, ConfirmedAt: confirmed.UTC()}
	canonical, err := recorddeletion.EncodeCanonicalLedgerEntry(candidate)
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, err
	}
	candidate.Canonical = canonical
	candidate.EntryHash = sha256.Sum256(canonical)
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, err
	}
	var contract, release *int64
	if request.DeletionContractVersion != 0 {
		value := int64(request.DeletionContractVersion)
		contract = &value
	}
	if request.ReleaseEpoch != 0 {
		value := int64(request.ReleaseEpoch)
		release = &value
	}
	row := storedRow{sequence: int64(candidate.Sequence), version: 1, entryType: string(request.EntryType),
		deployment: string(request.DeploymentID), project: string(request.ProjectID), operation: request.OperationID,
		actor: request.ActorID, route: recorddeletion.LedgerRecordRoute, kind: request.Object.ObjectKind, object: request.Object.ObjectID,
		token: request.TokenCommitment[:], fingerprint: fingerprint[:], contract: contract, reason: string(request.ReasonCode),
		release: release, confirmed: candidate.ConfirmedAt, previous: candidate.PreviousHash[:], canonical: canonical, hash: candidate.EntryHash[:]}
	if err := insertPrimary(ctx, tx, row); err != nil {
		return recorddeletion.DeletionLedgerEntry{}, err
	}
	tag, err := tx.Exec(ctx, `update public.deletion_ledger_head set sequence=$3, entry_hash=$4, updated_at=pg_catalog.clock_timestamp()
 where singleton and sequence=$1 and entry_hash=$2`, int64(snapshot.Head.Sequence), snapshot.Head.Hash[:], int64(candidate.Sequence), candidate.EntryHash[:])
	if err != nil {
		return recorddeletion.DeletionLedgerEntry{}, failure("advance primary head", err)
	}
	if tag.RowsAffected() != 1 {
		return recorddeletion.DeletionLedgerEntry{}, fmt.Errorf("%w: primary head changed", recorddeletion.ErrLedgerContinuity)
	}
	return candidate.DeletionLedgerEntry, nil
}

func findOperation(rows []storedRow, request recorddeletion.LedgerAppendRequest) int {
	for i, row := range rows {
		if row.deployment == string(request.DeploymentID) && row.project == string(request.ProjectID) && row.operation == request.OperationID {
			return i
		}
	}
	return -1
}

func equalRequest(a, b recorddeletion.LedgerAppendRequest) bool {
	return a.EntryType == b.EntryType && a.DeploymentID == b.DeploymentID && a.ProjectID == b.ProjectID &&
		a.OperationID == b.OperationID && a.ActorID == b.ActorID && a.Object == b.Object && a.TokenCommitment == b.TokenCommitment &&
		a.RequestFingerprint.Equal(b.RequestFingerprint) && a.ReasonCode == b.ReasonCode &&
		a.DeletionContractVersion == b.DeletionContractVersion && a.ReleaseEpoch == b.ReleaseEpoch
}

func insertPrimary(ctx context.Context, tx pgx.Tx, row storedRow) error {
	_, err := tx.Exec(ctx, `insert into public.deletion_ledger_entries (`+ledgerColumns+`) values
 ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`, rowArgs(row)...)
	if err != nil {
		return failure("insert primary entry", err)
	}
	return nil
}

func insertWitness(ctx context.Context, tx pgx.Tx, row storedRow) error {
	_, err := tx.Exec(ctx, `insert into public.deletion_witness_entries (`+ledgerColumns+`) values
 ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`, rowArgs(row)...)
	if err != nil {
		return failure("insert witness entry", err)
	}
	return nil
}

func rowArgs(row storedRow) []any {
	return []any{row.sequence, row.version, row.entryType, row.deployment, row.project, row.operation, row.actor, row.route,
		row.kind, row.object, row.origin, row.floor, row.floorHash, row.token, row.fingerprint, row.contract, row.reason,
		row.release, row.confirmed, row.previous, row.canonical, row.hash}
}
