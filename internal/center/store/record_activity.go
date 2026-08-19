package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordplatform"
)

var (
	// ErrActivityGenerationInactive means the target generation is no longer the
	// one being published, so a stale worker must not add rows to it.
	ErrActivityGenerationInactive = errors.New("activity projection generation is not active")
	// ErrActivitySourceHashMismatch means the same source event arrived with
	// different canonical bytes. That is a source contract violation, not a
	// retry, and it must stop the source rather than overwrite stored history.
	ErrActivitySourceHashMismatch = errors.New("activity source event canonical hash mismatch")
)

// ActivityPublishResult reports what one batch did. The caller needs to
// distinguish "already there" from "inserted" because only the latter consumes
// sequence numbers.
type ActivityPublishResult struct {
	Inserted         int
	AlreadyPresent   int
	AssignedFrom     uint64
	AssignedThrough  uint64
	PublishedThrough uint64
}

// PublishActivityBatch projects a batch in one transaction.
//
// The ordering guarantee the whole read path depends on is built here. Sequence
// numbers are not taken from a PostgreSQL sequence, because two transactions
// would take 5 and 6 and could commit in the other order, leaving a reader that
// trusts max() to page over a row that had not committed yet. Instead the batch
// takes a row lock on the generation head, classifies every candidate against
// the stored unique key while holding it, allocates a contiguous range for only
// the genuinely missing rows, and advances the published watermark in the same
// commit. A rollback therefore releases its numbers instead of burning them.
func PublishActivityBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	generation uint64,
	candidates []activity.CandidateEvent,
) (ActivityPublishResult, error) {
	if ctx == nil || pool == nil {
		return ActivityPublishResult{}, errors.New("publish activity batch: nil dependency")
	}
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return ActivityPublishResult{}, fmt.Errorf("publish activity batch: begin: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	result, err := publishActivityBatchInTx(ctx, transaction, generation, candidates)
	if err != nil {
		return ActivityPublishResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return ActivityPublishResult{}, fmt.Errorf("publish activity batch: commit: %w", err)
	}
	return result, nil
}

func publishActivityBatchInTx(
	ctx context.Context,
	transaction pgx.Tx,
	generation uint64,
	candidates []activity.CandidateEvent,
) (ActivityPublishResult, error) {
	if generation == 0 {
		return ActivityPublishResult{}, ErrActivityGenerationInactive
	}

	// Locking the head first is what serializes allocation. Everything below
	// runs with the guarantee that no other publisher can be numbering rows in
	// this generation at the same time.
	var publishedThrough, allocatedThrough uint64
	err := transaction.QueryRow(ctx, `
		select published_ingest_sequence, allocated_ingest_sequence
		from public.record_activity_projection_heads
		where project_id = $1 and projection_generation = $2 and head_state = 'active'
		for update`,
		recordplatform.ProjectIDDefault, generation,
	).Scan(&publishedThrough, &allocatedThrough)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActivityPublishResult{}, ErrActivityGenerationInactive
	}
	if err != nil {
		return ActivityPublishResult{}, fmt.Errorf("publish activity batch: lock generation head: %w", err)
	}

	existing, err := loadExistingActivityHashes(ctx, transaction, candidates)
	if err != nil {
		return ActivityPublishResult{}, err
	}

	missing := make([]activity.CandidateEvent, 0, len(candidates))
	alreadyPresent := 0
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate.ActivityID] {
			continue
		}
		seen[candidate.ActivityID] = true
		storedHash, found := existing[candidate.ActivityID]
		if !found {
			missing = append(missing, candidate)
			continue
		}
		if storedHash != candidate.CanonicalHash {
			return ActivityPublishResult{}, fmt.Errorf("%w: %s", ErrActivitySourceHashMismatch, candidate.ActivityID)
		}
		alreadyPresent++
	}

	result := ActivityPublishResult{
		AlreadyPresent:   alreadyPresent,
		PublishedThrough: publishedThrough,
	}
	if len(missing) == 0 {
		return result, nil
	}

	// Insert in business order so the sequence roughly tracks chronology, and so
	// two runs over the same batch always number it the same way.
	sort.Slice(missing, func(i, j int) bool {
		left, right := missing[i], missing[j]
		if !left.EventAt.Equal(right.EventAt) {
			return left.EventAt.Before(right.EventAt)
		}
		if !left.RecordedAt.Equal(right.RecordedAt) {
			return left.RecordedAt.Before(right.RecordedAt)
		}
		if left.Source.Kind != right.Source.Kind {
			return left.Source.Kind < right.Source.Kind
		}
		return left.ActivityID < right.ActivityID
	})

	assignedFrom := publishedThrough + 1
	for offset, candidate := range missing {
		sequence := assignedFrom + uint64(offset)
		if err := insertActivityRow(ctx, transaction, generation, sequence, candidate); err != nil {
			return ActivityPublishResult{}, err
		}
	}
	assignedThrough := assignedFrom + uint64(len(missing)) - 1

	if _, err := transaction.Exec(ctx, `
		update public.record_activity_projection_heads
		set published_ingest_sequence = $3,
		    allocated_ingest_sequence = greatest(allocated_ingest_sequence, $3),
		    updated_at = now()
		where project_id = $1 and projection_generation = $2 and head_state = 'active'`,
		recordplatform.ProjectIDDefault, generation, assignedThrough,
	); err != nil {
		return ActivityPublishResult{}, fmt.Errorf("publish activity batch: advance published head: %w", err)
	}

	result.Inserted = len(missing)
	result.AssignedFrom = assignedFrom
	result.AssignedThrough = assignedThrough
	result.PublishedThrough = assignedThrough
	return result, nil
}

// loadExistingActivityHashes locks and reads whatever is already stored for this
// batch. Locking here rather than relying on a conflict clause is deliberate: the
// final insert must be a strict INSERT so an unexpected conflict rolls the batch
// back instead of silently swallowing an allocated number.
func loadExistingActivityHashes(
	ctx context.Context,
	transaction pgx.Tx,
	candidates []activity.CandidateEvent,
) (map[string][32]byte, error) {
	if len(candidates) == 0 {
		return map[string][32]byte{}, nil
	}
	identifiers := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		identifiers = append(identifiers, candidate.ActivityID)
	}
	rows, err := transaction.Query(ctx, `
		select activity_id, canonical_hash
		from public.record_activity_projection
		where activity_id = any($1::text[])
		order by activity_id
		for update`,
		identifiers,
	)
	if err != nil {
		return nil, fmt.Errorf("publish activity batch: classify candidates: %w", err)
	}
	defer rows.Close()

	stored := make(map[string][32]byte, len(candidates))
	for rows.Next() {
		var activityID string
		var hash []byte
		if err := rows.Scan(&activityID, &hash); err != nil {
			return nil, fmt.Errorf("publish activity batch: scan candidate: %w", err)
		}
		if len(hash) != 32 {
			return nil, fmt.Errorf("publish activity batch: stored hash for %s is %d bytes", activityID, len(hash))
		}
		var fixed [32]byte
		copy(fixed[:], hash)
		stored[activityID] = fixed
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("publish activity batch: classify candidates: %w", err)
	}
	return stored, nil
}

func insertActivityRow(
	ctx context.Context,
	transaction pgx.Tx,
	generation uint64,
	sequence uint64,
	candidate activity.CandidateEvent,
) error {
	presentation, err := json.Marshal(candidate.Presentation)
	if err != nil {
		return fmt.Errorf("publish activity batch: encode presentation for %s: %w", candidate.ActivityID, err)
	}
	authDigest := candidate.AuthScopeDigest()
	severity := candidate.Severity
	if severity == "" {
		severity = "info"
	}

	if _, err := transaction.Exec(ctx, `
		insert into public.record_activity_projection (
		  activity_id, project_id, projection_generation, ingest_sequence,
		  event_kind, event_at, recorded_at,
		  source_kind, source_event_id, source_version,
		  record_id, revision_id, evidence_snapshot_id,
		  backfilled, actor_id, severity,
		  presentation_version, presentation_json,
		  auth_scope_digest, canonical_hash, corrects_activity_id
		) values (
		  $1, $2, $3, $4,
		  $5, $6, $7,
		  $8, $9, $10,
		  $11, $12, $13,
		  $14, $15, $16,
		  $17, $18::jsonb,
		  $19, $20, $21
		)`,
		candidate.ActivityID, recordplatform.ProjectIDDefault, generation, sequence,
		string(candidate.EventKind), candidate.EventAt.UTC(), candidate.RecordedAt.UTC(),
		string(candidate.Source.Kind), candidate.Source.EventID, candidate.Source.Version,
		nullableText(candidate.RecordID), nullableText(candidate.RevisionID), nullableText(candidate.EvidenceID),
		candidate.Backfilled, nullableActorID(candidate.Actor), severity,
		candidate.Presentation.Version, string(presentation),
		authDigest[:], candidate.CanonicalHash[:], nullableText(candidate.Corrects),
	); err != nil {
		return fmt.Errorf("publish activity batch: insert %s: %w", candidate.ActivityID, err)
	}

	for order, subject := range candidate.Subjects {
		identity, err := json.Marshal(subject.Identity)
		if err != nil {
			return fmt.Errorf("publish activity batch: encode subject identity for %s: %w", candidate.ActivityID, err)
		}
		if subject.Identity == nil {
			identity = []byte("{}")
		}
		relationHash := candidate.RelationHash(subject)
		if _, err := transaction.Exec(ctx, `
			insert into public.record_activity_subjects (
			  activity_id, subject_kind, subject_source_id, relation_role, is_primary, relation_order,
			  identity_snapshot, live_route, tombstoned,
			  projection_generation, ingest_sequence, event_kind, source_kind,
			  event_at, recorded_at, record_id, revision_id, evidence_snapshot_id,
			  auth_scope_digest, relation_hash
			) values (
			  $1, $2, $3, $4, $5, $6,
			  $7::jsonb, $8, $9,
			  $10, $11, $12, $13,
			  $14, $15, $16, $17, $18,
			  $19, $20
			)`,
			candidate.ActivityID, string(subject.Kind), subject.SourceID, string(subject.Role), subject.Primary, order,
			string(identity), nullableText(subject.LiveRoute), subject.Tombstoned,
			generation, sequence, string(candidate.EventKind), string(candidate.Source.Kind),
			candidate.EventAt.UTC(), candidate.RecordedAt.UTC(),
			nullableText(candidate.RecordID), nullableText(candidate.RevisionID), nullableText(candidate.EvidenceID),
			authDigest[:], relationHash[:],
		); err != nil {
			return fmt.Errorf("publish activity batch: insert subject for %s: %w", candidate.ActivityID, err)
		}
	}
	return nil
}

func nullableActorID(actor *activity.ActorSnapshot) *string {
	if actor == nil {
		return nil
	}
	return nullableText(actor.ActorID)
}
