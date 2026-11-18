package deletionledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// SourceRetirementResolution never asserts that an absent operation cannot
// still commit. An unobserved original request remains unresolved.
type SourceRetirementResolution struct {
	Committed bool
	Entry     SourceRetirementEntry
}

func claimsSourceCodec(raw []byte) bool {
	// A recognizable claimed domain must be decoded strictly, even if its
	// framing was damaged. Other source formats remain opaque chain entries.
	if len(raw) < 4 {
		return false
	}
	payload := raw[4:]
	if len(payload) >= len(sourceLedgerDomain) {
		return bytes.Equal(payload[:len(sourceLedgerDomain)], []byte(sourceLedgerDomain))
	}
	return binary.BigEndian.Uint32(raw[:4]) == uint32(len(sourceLedgerDomain)) && bytes.Equal(payload, []byte(sourceLedgerDomain)[:len(payload)])
}

func equalNullableBytes(a, b []byte) bool { return (a == nil) == (b == nil) && bytes.Equal(a, b) }
func equalNullableInt(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// equalStoredRow compares all 22 SQL fields, including NULL versus empty and
// the projections of source rows whose canonical bytes are unknown to us.
func equalStoredRow(a, b storedRow) bool {
	return a.sequence == b.sequence && a.version == b.version && a.entryType == b.entryType &&
		a.deployment == b.deployment && a.project == b.project && a.operation == b.operation && a.actor == b.actor &&
		a.route == b.route && a.kind == b.kind && a.object == b.object &&
		equalNullableBytes(a.origin, b.origin) && equalNullableBytes(a.floor, b.floor) && equalNullableBytes(a.floorHash, b.floorHash) &&
		equalNullableBytes(a.token, b.token) && equalNullableBytes(a.fingerprint, b.fingerprint) &&
		equalNullableInt(a.contract, b.contract) && a.reason == b.reason && equalNullableInt(a.release, b.release) &&
		a.confirmed.Equal(b.confirmed) && equalNullableBytes(a.previous, b.previous) &&
		equalNullableBytes(a.canonical, b.canonical) && equalNullableBytes(a.hash, b.hash)
}

func sourceStoredRow(entry SourceRetirementEntry) (storedRow, error) {
	if err := entry.Validate(); err != nil {
		return storedRow{}, err
	}
	origin, err := EncodeSourceOrigin(entry.Request.Origin)
	if err != nil {
		return storedRow{}, err
	}
	floor, err := sourceScope(entry.Request.FinalFloor, entry.Request.Origin.Source.ProjectID)
	if err != nil {
		return storedRow{}, err
	}
	floorHash := sha256.Sum256(floor)
	contract := int64(2)
	key := entry.Request.Origin.Source
	return storedRow{sequence: int64(entry.Sequence), version: 1, entryType: "delete_commit", deployment: key.DeploymentID,
		project: string(key.ProjectID), operation: entry.Request.OperationID, actor: entry.Request.ActorUserID, route: sourceRoute,
		kind: string(key.Kind), object: key.ID, origin: origin, floor: floor, floorHash: floorHash[:],
		token: entry.Request.TokenCommitment[:], fingerprint: entry.Request.RequestFingerprint[:], contract: &contract,
		reason: "user_confirmed", confirmed: entry.ConfirmedAt, previous: entry.PreviousHash[:], canonical: entry.Canonical, hash: entry.EntryHash[:]}, nil
}

func (client *client) sourceRow(row storedRow) (SourceRetirementEntry, error) {
	entry, err := DecodeSourceRetirementEntry(row.canonical)
	if err != nil {
		return SourceRetirementEntry{}, err
	}
	if entry.LedgerDomain != client.ledgerDomain {
		return SourceRetirementEntry{}, sourceInvalid("ledger domain")
	}
	expected, err := sourceStoredRow(entry)
	if err != nil || !equalStoredRow(row, expected) {
		return SourceRetirementEntry{}, sourceInvalid("SQL projection")
	}
	return entry, nil
}

func equalSourceRequest(a, b SourceRetirementRequest) bool {
	if a.Validate() != nil || b.Validate() != nil || a.OperationID != b.OperationID || a.ActorUserID != b.ActorUserID ||
		a.Origin.Source != b.Origin.Source || a.TokenCommitment != b.TokenCommitment || a.RequestFingerprint != b.RequestFingerprint {
		return false
	}
	aOrigin, errA := EncodeSourceOrigin(a.Origin)
	bOrigin, errB := EncodeSourceOrigin(b.Origin)
	return errA == nil && errB == nil && bytes.Equal(aOrigin, bOrigin) && bytes.Equal(a.FinalFloor.CanonicalBytes(), b.FinalFloor.CanonicalBytes())
}

func findSourceOperation(rows []storedRow, request SourceRetirementRequest) int {
	for i, row := range rows {
		if row.deployment == request.Origin.Source.DeploymentID && row.project == string(request.Origin.Source.ProjectID) && row.operation == request.OperationID {
			return i
		}
	}
	return -1
}

func (primary *Primary) AppendSourceRetirement(ctx context.Context, commit SourceRetirementCommit) (SourceRetirementEntry, error) {
	if ctx == nil || primary == nil || primary.client.pool == nil || !commit.Valid() {
		return SourceRetirementEntry{}, sourceInvalid("append capability")
	}
	observer := sourceCommitObserverFromContext(ctx)
	var result SourceRetirementEntry
	err := commit.withProjection(ctx, func(held context.Context) error {
		tx, err := primary.client.pool.BeginTx(held, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return failure("begin source append", err)
		}
		defer func() { _ = tx.Rollback(held) }()
		snapshot, rows, err := primary.client.snapshotTx(held, tx, true)
		if err != nil {
			return err
		}
		index := findSourceOperation(rows, commit.request)
		if index >= 0 {
			if !claimsSourceCodec(rows[index].canonical) {
				return sourceInvalid("operation conflict")
			}
			entry, err := primary.client.sourceRow(rows[index])
			if err != nil || !equalSourceRequest(entry.Request, commit.request) {
				return sourceInvalid("operation conflict")
			}
			result = cloneSourceEntry(entry)
		} else {
			if snapshot.Head.Sequence >= math.MaxInt64 {
				return sourceInvalid("sequence exhausted")
			}
			var confirmed time.Time
			if err := tx.QueryRow(held, `select pg_catalog.clock_timestamp()`).Scan(&confirmed); err != nil {
				return failure("read source confirmation time", err)
			}
			entry := SourceRetirementEntry{Request: cloneSourceRequest(commit.request), LedgerDomain: primary.client.ledgerDomain,
				Sequence: snapshot.Head.Sequence + 1, PreviousHash: snapshot.Head.Hash, ConfirmedAt: confirmed.UTC()}
			entry.Canonical, err = EncodeSourceRetirementEntry(entry)
			if err != nil {
				return err
			}
			entry.EntryHash = sha256.Sum256(entry.Canonical)
			row, err := sourceStoredRow(entry)
			if err != nil {
				return err
			}
			if err := insertPrimary(held, tx, row); err != nil {
				return err
			}
			tag, err := tx.Exec(held, `update public.deletion_ledger_head set sequence=$3,entry_hash=$4,updated_at=pg_catalog.clock_timestamp()
 where singleton and sequence=$1 and entry_hash=$2`, int64(snapshot.Head.Sequence), snapshot.Head.Hash[:], int64(entry.Sequence), entry.EntryHash[:])
			if err != nil {
				return failure("advance source primary head", err)
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("%w: primary head changed", recorddeletion.ErrLedgerContinuity)
			}
			result = cloneSourceEntry(entry)
		}
		if index < 0 && observer != nil {
			return commitObservedSource(held, tx, "w1-commit", "commit source append", observer)
		}
		if err := tx.Commit(held); err != nil {
			return failure("commit source append", err)
		}
		return nil
	})
	if err != nil {
		return SourceRetirementEntry{}, err
	}
	return cloneSourceEntry(result), nil
}

func (primary *Primary) ResolveSourceRetirement(ctx context.Context, request SourceRetirementRequest) (SourceRetirementResolution, error) {
	request = cloneSourceRequest(request)
	if ctx == nil || primary == nil || primary.client.pool == nil || request.Validate() != nil {
		return SourceRetirementResolution{}, sourceInvalid("resolve request")
	}
	_, rows, err := primary.client.readStoredSnapshot(ctx)
	if err != nil {
		return SourceRetirementResolution{}, err
	}
	index := findSourceOperation(rows, request)
	if index < 0 {
		return SourceRetirementResolution{}, nil
	}
	if !claimsSourceCodec(rows[index].canonical) {
		return SourceRetirementResolution{}, sourceInvalid("operation conflict")
	}
	entry, err := primary.client.sourceRow(rows[index])
	if err != nil || !equalSourceRequest(entry.Request, request) {
		return SourceRetirementResolution{}, sourceInvalid("operation conflict")
	}
	return SourceRetirementResolution{Committed: true, Entry: cloneSourceEntry(entry)}, nil
}

func (client *client) readStoredSnapshot(ctx context.Context) (recorddeletion.LedgerSnapshot, []storedRow, error) {
	if ctx == nil || client == nil || client.pool == nil {
		return recorddeletion.LedgerSnapshot{}, nil, recorddeletion.ErrLedgerContinuity
	}
	tx, err := client.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, failure("begin source snapshot", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, rows, err := client.snapshotTx(ctx, tx, false)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, failure("commit source snapshot", err)
	}
	return snapshot, rows, nil
}

func (client *client) readSources(ctx context.Context) ([]SourceRetirementEntry, error) {
	_, rows, err := client.readStoredSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	var entries []SourceRetirementEntry
	for _, row := range rows {
		if !claimsSourceCodec(row.canonical) {
			continue
		}
		entry, err := client.sourceRow(row)
		if err != nil {
			return nil, err
		}
		entries = append(entries, cloneSourceEntry(entry))
	}
	return entries, nil
}
func (primary *Primary) ReadSourceRetirements(ctx context.Context) ([]SourceRetirementEntry, error) {
	if primary == nil {
		return nil, recorddeletion.ErrLedgerContinuity
	}
	return primary.client.readSources(ctx)
}
func (witness *Witness) ReadSourceRetirements(ctx context.Context) ([]SourceRetirementEntry, error) {
	if witness == nil {
		return nil, recorddeletion.ErrLedgerContinuity
	}
	return witness.client.readSources(ctx)
}

func (witness *Witness) ConfirmSourceRetirement(ctx context.Context, commit SourceRetirementCommit, entry SourceRetirementEntry) (VerifiedSourceRetirement, error) {
	entry = cloneSourceEntry(entry)
	if ctx == nil || witness == nil || witness.client.pool == nil || witness.primary == nil || !commit.Valid() || entry.Validate() != nil || !equalSourceRequest(entry.Request, commit.request) {
		return VerifiedSourceRetirement{}, sourceInvalid("confirmation capability")
	}
	observer := sourceCommitObserverFromContext(ctx)
	var verified VerifiedSourceRetirement
	err := commit.withProjection(ctx, func(held context.Context) error {
		trust, err := commit.fence.Trust()
		if err != nil {
			return err
		}
		if !sameSourceFence(commit.fence, trust.Continuity) {
			return fmt.Errorf("%w: source continuity fence", recorddeletion.ErrLedgerContinuity)
		}
		verifier, err := NewSourceRetirementVerifier(witness.primary, witness, trust)
		if err != nil {
			return err
		}
		primary, rows, err := witness.primary.client.readStoredSnapshot(held)
		if err != nil {
			return err
		}
		if entry.Sequence > primary.Head.Sequence {
			return recorddeletion.ErrLedgerPending
		}
		actual, err := witness.primary.client.sourceRow(rows[entry.Sequence-1])
		if err != nil || !equalSourceEntry(actual, entry) {
			return sourceInvalid("confirmation target")
		}
		if err := witness.copyPrefix(held, primary, rows, entry.Sequence, entry.EntryHash, observer); err != nil {
			return err
		}
		verified, err = verifier.Verify(held, recorddeletion.LedgerLocator{LedgerDomain: entry.LedgerDomain, Sequence: entry.Sequence, EntryHash: entry.EntryHash})
		return err
	})
	if err != nil {
		return VerifiedSourceRetirement{}, err
	}
	return verified, nil
}

func equalSourceEntry(a, b SourceRetirementEntry) bool {
	return a.Validate() == nil && b.Validate() == nil && a.EntryHash == b.EntryHash && bytes.Equal(a.Canonical, b.Canonical)
}
