package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type revisionMutationProjectedRoot struct {
	exists             bool
	revisionID         string
	canonicalHash      [32]byte
	lockVersion        int64
	authorizationEpoch int64
}

// Only roots inserted by this very transaction may be projected as absent
// during the post-acquisition rediscovery before its first revision is written.
type revisionMutationSpeculativeRootsKey struct{}

type revisionMutationBatchPlan struct {
	commands []recordMutationResources
	replays  []bool
	noops    []bool
}

// peekRevisionReplay is an unlocked discovery hint, not a claim or a grant of
// read authority. ClaimIdempotency resolves the same fingerprint again later.
func peekRevisionReplay(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (bool, error) {
	var status string
	var fingerprint []byte
	err := tx.QueryRow(ctx, `select status,request_fingerprint from public.record_idempotency_keys where project_id=$1 and operation_kind=$2 and idempotency_key=$3`, command.Idempotency.Key.ProjectID, command.Idempotency.Key.OperationKind, command.Idempotency.Key.Key).Scan(&status, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("discover revision replay: %w", err)
	}
	if status == string(recordplatform.IdempotencyStatusConflict) {
		return false, recordplatform.ErrIdempotencyConflictState
	}
	expected, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(fingerprint, expected[:]) {
		return false, recordplatform.ErrIdempotencyKeyReused
	}
	return status == string(recordplatform.IdempotencyStatusCompleted), nil
}

func discoverRevisionProjectedRoot(ctx context.Context, tx pgx.Tx, recordID string) (revisionMutationProjectedRoot, error) {
	var root revisionMutationProjectedRoot
	var canonical []byte
	err := tx.QueryRow(ctx, `select coalesce(r.current_revision_id,''),r.lock_version,r.authorization_epoch,v.canonical_hash
		from public.records r left join public.record_revisions v on v.record_id=r.record_id and v.revision_id=r.current_revision_id
		where r.project_id='default' and r.record_id=$1`, recordID).Scan(&root.revisionID, &root.lockVersion, &root.authorizationEpoch, &canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return root, nil
	}
	if err != nil {
		return root, fmt.Errorf("discover revision root: %w", err)
	}
	if root.lockVersion < 0 || root.authorizationEpoch < 0 || (root.revisionID != "" && len(canonical) != len(root.canonicalHash)) || (root.revisionID == "" && len(canonical) != 0) {
		return root, records.ErrRecordRevisionConflict
	}
	root.exists = true
	if speculative, ok := ctx.Value(revisionMutationSpeculativeRootsKey{}).(map[string]bool); ok && speculative[recordID] && root.revisionID == "" {
		root.exists = false
	}
	copy(root.canonicalHash[:], canonical)
	return root, nil
}

// discoverRevisionBatchResources projects the input order without materializing
// future rows. Completed replay avoids consumed participants; canonical no-ops
// still discover the draft cleanup route. A later command may refer to the
// authenticated input of an earlier in-batch revision.
func discoverRevisionBatchResources(ctx context.Context, tx pgx.Tx, registry records.RevisionParticipantRegistry, commands []records.RevisionCommitCommand, plan *revisionMutationBatchPlan) (recordMutationResources, error) {
	sets := make([]records.RevisionMutationResources, 0, len(commands)*3)
	if plan != nil {
		*plan = revisionMutationBatchPlan{}
	}
	roots := make(map[string]revisionMutationProjectedRoot, len(commands))
	seenRoots := make(map[string]bool, len(commands))
	seenKeys := make(map[recordplatform.IdempotencyKey]bool, len(commands))
	future := make(map[string]revisionMutationFutureRevision, len(commands))
	for _, command := range commands {
		if err := validateRevisionMutationDiscovery(ctx, tx, command); err != nil {
			return recordMutationResources{}, err
		}
		replay, err := peekRevisionReplay(ctx, tx, command)
		if err != nil {
			return recordMutationResources{}, err
		}
		if seenKeys[command.Idempotency.Key] {
			// The first occurrence acquires and completes the same key. The
			// claim step still checks this occurrence's exact fingerprint.
			replay = true
		}
		seenKeys[command.Idempotency.Key] = true
		if !seenRoots[command.RecordID] {
			roots[command.RecordID], err = discoverRevisionProjectedRoot(ctx, tx, command.RecordID)
			if err != nil {
				return recordMutationResources{}, err
			}
			seenRoots[command.RecordID] = true
		}
		root := roots[command.RecordID]
		noop := false
		if !replay {
			switch command.Idempotency.Key.OperationKind {
			case recordplatform.OperationKindRecordCreate:
				if root.exists {
					if completed, peekErr := peekRevisionReplay(ctx, tx, command); peekErr == nil && completed {
						return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "revision completed during root discovery; restart transaction"}
					}
					return recordMutationResources{}, records.ErrRecordAlreadyExists
				}
			case recordplatform.OperationKindRecordUpdate:
				if !root.exists || root.revisionID != command.BaseRevisionID || uint64(root.lockVersion) != command.LockVersion || uint64(root.authorizationEpoch) != command.AuthorizationEpoch {
					if completed, peekErr := peekRevisionReplay(ctx, tx, command); peekErr == nil && completed {
						return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "revision completed during root discovery; restart transaction"}
					}
					return recordMutationResources{}, records.ErrRecordRevisionConflict
				}
				noop = root.canonicalHash == command.Input.CanonicalHash()
			default:
				return recordMutationResources{}, records.ErrInvalidRevisionCommand
			}
		}
		var participantResources records.RevisionMutationResources
		if !replay && !noop {
			discoveryCtx := context.WithValue(ctx, revisionMutationFutureKey{}, future)
			participantResources, err = registry.MutationResources(discoveryCtx, tx, command)
			if err != nil {
				// A concurrent successful request may have consumed the live
				// route after the hint. Restart instead of requiring it for replay.
				if completed, peekErr := peekRevisionReplay(ctx, tx, command); peekErr == nil && completed {
					return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "revision completed during discovery; restart transaction"}
				}
				return recordMutationResources{}, err
			}
		}
		if !replay {
			cleanup, cleanupErr := discoverRevisionDraftCleanup(ctx, tx, command)
			if cleanupErr != nil {
				if completed, peekErr := peekRevisionReplay(ctx, tx, command); peekErr == nil && completed {
					return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "revision completed during draft discovery; restart transaction"}
				}
				return recordMutationResources{}, cleanupErr
			}
			participantResources, err = records.CombineRevisionMutationResources(participantResources, cleanup)
			if err != nil {
				return recordMutationResources{}, err
			}
		}
		base, err := discoverRevisionMutationBaseForReplay(ctx, tx, command, participantResources, replay)
		if err != nil {
			return recordMutationResources{}, err
		}
		if noop {
			base.NewRevisionEffects = nil
		}
		if plan != nil {
			perCommand, err := normalizeRecordMutationDeclarations(base, participantResources)
			if err != nil {
				return recordMutationResources{}, err
			}
			plan.commands = append(plan.commands, perCommand)
			plan.replays = append(plan.replays, replay)
			plan.noops = append(plan.noops, noop)
		}
		sets = append(sets, base, participantResources)
		if !replay && !noop {
			revisionID, err := command.RevisionID()
			if err != nil {
				return recordMutationResources{}, err
			}
			future[revisionID] = revisionMutationFutureRevision{recordID: command.RecordID, input: command.Input}
			root.exists, root.revisionID = true, revisionID
			root.canonicalHash = command.Input.CanonicalHash()
			root.lockVersion++
			root.authorizationEpoch++
			roots[command.RecordID] = root
		}
	}
	return normalizeRecordMutationDeclarations(sets...)
}

func addRevisionImportJob(ctx context.Context, tx pgx.Tx, resources recordMutationResources, finish *records.RevisionCommitFinish) (recordMutationResources, error) {
	if finish == nil {
		return resources, nil
	}
	var job records.RevisionMutationImportJob
	var archive, plan []byte
	var expiry *time.Time
	err := tx.QueryRow(ctx, `select j.project_id,j.import_job_id,j.actor_id,j.job_state,j.owner_id,j.lock_version,j.owner_generation,j.lease_expires_at,j.archive_digest,p.plan_digest
		from public.record_import_jobs j left join public.record_import_plans p on p.import_job_id=j.import_job_id where j.import_job_id=$1`, finish.ImportJobID).Scan(
		&job.ProjectID, &job.JobID, &job.ActorID, &job.State, &job.OwnerID, &job.LockVersion, &job.OwnerGeneration, &expiry, &archive, &plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordMutationResources{}, ErrRecordImportNotFound
	}
	if err != nil {
		return recordMutationResources{}, fmt.Errorf("discover revision import job: %w", err)
	}
	if len(archive) != len(job.ArchiveDigest) || len(plan) != len(job.PlanDigest) {
		return recordMutationResources{}, ErrRecordImportCASConflict
	}
	copy(job.ArchiveDigest[:], archive)
	copy(job.PlanDigest[:], plan)
	if expiry != nil {
		job.LeaseExpiresAt = expiry.UTC()
	}
	if job.ActorID != finish.Actor.UserID || job.ArchiveDigest != finish.OriginDigest {
		return recordMutationResources{}, ErrRecordImportCASConflict
	}
	return normalizeRecordMutationDeclarations(resources.resources, records.RevisionMutationResources{ImportJobs: []records.RevisionMutationImportJob{job}})
}
