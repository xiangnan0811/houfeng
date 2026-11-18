//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

type ledgerResolution struct {
	Version       int                       `json:"version"`
	Key           string                    `json:"key"`
	IntentDigest  [32]byte                  `json:"intent_digest"`
	Original      []byte                    `json:"original"`
	Outcome       []byte                    `json:"outcome"`
	ReleaseEpoch  uint64                    `json:"release_epoch"`
	Primary       recorddeletion.LedgerHead `json:"primary"`
	Witness       recorddeletion.LedgerHead `json:"witness"`
	ReservationID string                    `json:"reservation_id"`
	FenceEpoch    uint64                    `json:"fence_epoch"`
}

func ledgerReadResolution(dir int, key string) (ledgerResolution, bool, error) {
	var result ledgerResolution
	data, exists, err := fenceReadFile(dir, "resolution-"+key+".json")
	if err != nil || !exists {
		return result, exists, err
	}
	if fenceDecode(data, &result) != nil || result.Version != 1 || result.Key != key || result.ReleaseEpoch == 0 || result.ReleaseEpoch > math.MaxInt64 || result.ReservationID == "" {
		return ledgerResolution{}, false, ErrRecordLedgerFence
	}
	original, e1 := recorddeletion.DecodeCanonicalLedgerEntry(result.Original)
	outcome, e2 := recorddeletion.DecodeCanonicalLedgerEntry(result.Outcome)
	if e1 != nil || e2 != nil || ledgerIntentKey(original.Request) != key || ledgerIntentKey(outcome.Request) != key ||
		original.Request.EntryType != recorddeletion.LedgerEntryDeleteCommit || outcome.Request.EntryType != recorddeletion.LedgerEntryAttemptNotCommitted || outcome.Request.ReleaseEpoch != result.ReleaseEpoch {
		return ledgerResolution{}, false, ErrRecordLedgerFence
	}
	expected, err := ledgerRequestCanonical(original.Request.AttemptNotCommitted(result.ReleaseEpoch), original.LedgerDomain)
	if err != nil || !bytes.Equal(expected, result.Outcome) {
		return ledgerResolution{}, false, ErrRecordLedgerFence
	}
	return result, true, nil
}

func ledgerResolutionHighWater(dir int) (uint64, error) {
	names, err := ledgerNames(dir)
	if err != nil {
		return 0, err
	}
	var highest uint64
	seen := map[uint64]bool{}
	for _, name := range names {
		if !strings.HasPrefix(name, "resolution-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return 0, ErrRecordLedgerFence
		}
		key := strings.TrimSuffix(strings.TrimPrefix(name, "resolution-"), ".json")
		resolution, ok, err := ledgerReadResolution(dir, key)
		if err != nil || !ok || seen[resolution.ReleaseEpoch] {
			return 0, ErrRecordLedgerFence
		}
		seen[resolution.ReleaseEpoch] = true
		if resolution.ReleaseEpoch > highest {
			highest = resolution.ReleaseEpoch
		}
	}
	return highest, nil
}

func ledgerLockedHead(ctx context.Context, tx pgx.Tx, table string) (recorddeletion.LedgerHead, error) {
	var result recorddeletion.LedgerHead
	var sequence int64
	var digest []byte
	if tx.QueryRow(ctx, "select sequence,entry_hash from "+table+" where singleton for update").Scan(&sequence, &digest) != nil || sequence < 0 || len(digest) != sha256.Size {
		return result, ErrRecordLedgerFence
	}
	result.Sequence = uint64(sequence)
	copy(result.Hash[:], digest)
	return result, nil
}

func ledgerCheckUnknownOperation(ctx context.Context, f *RecordLedgerFence, request recorddeletion.LedgerAppendRequest) (string, uint64, uint64, error) {
	var deployment, project, actor, reason, state, reservationState, kind, objectID, reservation string
	var contract, operationRelease, reservationRelease, fenceEpoch int64
	var sequence *int64
	var token, fingerprint []byte
	err := f.app.QueryRow(ctx, `select o.deployment_id,o.project_id,o.actor_id,o.reason_code,o.deletion_contract_version,
		o.operation_state,o.ledger_sequence,o.release_epoch,r.state,r.object_kind,r.object_id,
		r.reservation_id,r.deletion_token_commitment,r.request_fingerprint,r.release_epoch,r.fence_epoch
		from public.record_purge_operations o join public.deletion_reservations r on r.reservation_id=o.reservation_id and r.project_id=o.project_id
		where o.operation_id=$1 and o.project_id=$2`, request.OperationID, string(request.ProjectID)).Scan(
		&deployment, &project, &actor, &reason, &contract, &state, &sequence, &operationRelease, &reservationState, &kind, &objectID,
		&reservation, &token, &fingerprint, &reservationRelease, &fenceEpoch)
	if err != nil || request.EntryType != recorddeletion.LedgerEntryDeleteCommit || request.Validate() != nil ||
		deployment != string(request.DeploymentID) || project != string(request.ProjectID) || actor != request.ActorID || reason != string(request.ReasonCode) ||
		contract != int64(request.DeletionContractVersion) || sequence != nil ||
		!((state == "ledger_commit_unknown" && operationRelease == 0) || (state == "release_pending" && operationRelease > 0)) ||
		reservationState != "fenced" || kind != request.Object.ObjectKind || objectID != request.Object.ObjectID || fenceEpoch < 0 || reservationRelease < 0 ||
		!bytes.Equal(token, request.TokenCommitment[:]) {
		return "", 0, 0, ErrRecordLedgerFence
	}
	fp, err := request.RequestFingerprint.PersistedBytes()
	if err != nil || !bytes.Equal(fingerprint, fp[:]) {
		return "", 0, 0, ErrRecordLedgerFence
	}
	return reservation, uint64(fenceEpoch), uint64(operationRelease), nil
}

// WithAbsentOperation resolves an already journaled outcome when its exact W1
// row exists. Missing rows remain pending: locked absence cannot exclude a lost
// commit followed by rollback of an unanchored primary tail.
func (f *RecordLedgerFence) WithAbsentOperation(ctx context.Context, original recorddeletion.LedgerAppendRequest) (recorddeletion.DeletionLedgerEntry, error) {
	var empty recorddeletion.DeletionLedgerEntry
	if err := f.enter(ctx); err != nil {
		return empty, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return empty, err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if err := f.openClients(ctx); err != nil {
		return empty, err
	}
	if f.checkAnchorsLocked() != nil || original.EntryType != recorddeletion.LedgerEntryDeleteCommit || original.Validate() != nil {
		return empty, ErrRecordLedgerFence
	}
	reservation, fenceEpoch, fixedEpoch, err := ledgerCheckUnknownOperation(ctx, f, original)
	if err != nil {
		return empty, err
	}
	primaryTx, err := f.primary.writer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, ErrRecordLedgerFence
	}
	defer primaryTx.Rollback(context.Background())
	phead, err := ledgerLockedHead(ctx, primaryTx, "public.deletion_ledger_head")
	if err != nil {
		return empty, err
	}
	witnessTx, err := f.witness.writer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, ErrRecordLedgerFence
	}
	defer witnessTx.Rollback(context.Background())
	whead, err := ledgerLockedHead(ctx, witnessTx, "public.deletion_witness_head")
	if err != nil {
		return empty, err
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, f.readPrimary, f.readWitness, anchor)
	if err != nil || primary.Head != phead || witness.Head != whead || f.validateTail(primary, witness, anchor) != nil {
		return empty, ErrRecordLedgerFence
	}
	domain, err := recorddeletion.LedgerDomainDigest(f.request.Primary.Identity.ID)
	if err != nil {
		return empty, ErrRecordLedgerFence
	}
	originalCanonical, err := ledgerRequestCanonical(original, domain)
	if err != nil {
		return empty, ErrRecordLedgerFence
	}
	key := ledgerIntentKey(original)
	resolution, exists, err := ledgerReadResolution(f.control, key)
	if err != nil {
		return empty, err
	}
	if exists {
		if !bytes.Equal(resolution.Original, originalCanonical) || resolution.ReservationID != reservation || resolution.FenceEpoch != fenceEpoch || (fixedEpoch != 0 && fixedEpoch != resolution.ReleaseEpoch) {
			return empty, ErrRecordLedgerFence
		}
		intents, err := ledgerReadIntents(f.control)
		if err != nil {
			return empty, err
		}
		for _, intent := range intents {
			if intent.Key != key {
				continue
			}
			if !bytes.Equal(intent.Canonical, originalCanonical) {
				return empty, ErrRecordLedgerFence
			}
			row, found, err := ledgerFindIntentEntry(f.control, primary, intent, domain)
			if err != nil {
				return empty, err
			}
			if found {
				return row.Record.DeletionLedgerEntry, nil
			}
			return empty, recorddeletion.ErrLedgerPending
		}
		return empty, ErrRecordLedgerFence
	}
	var inPrimary, inWitness bool
	if primaryTx.QueryRow(ctx, `select exists(select 1 from public.deletion_ledger_entries where deployment_id=$1 and project_id=$2 and operation_id=$3)`, string(original.DeploymentID), string(original.ProjectID), original.OperationID).Scan(&inPrimary) != nil ||
		witnessTx.QueryRow(ctx, `select exists(select 1 from public.deletion_witness_entries where deployment_id=$1 and project_id=$2 and operation_id=$3)`, string(original.DeploymentID), string(original.ProjectID), original.OperationID).Scan(&inWitness) != nil || inPrimary || inWitness {
		return empty, ErrRecordLedgerFence
	}
	// Neither an old dispatch intent nor an old APP operation proves that an
	// unobserved primary tail did not commit and subsequently roll back. Keep
	// the original operation and any previously allocated release epoch fenced.
	return empty, recorddeletion.ErrLedgerPending
}
