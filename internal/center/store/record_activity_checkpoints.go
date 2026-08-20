package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordplatform"
)

// ActivityProjectionRepository is the projector's Postgres side: it publishes
// batches and persists each source's position. It exists so the projector can
// depend on narrow interfaces in its own package while the SQL stays here.
type ActivityProjectionRepository struct {
	pool *pgxpool.Pool
}

var (
	_ activity.Publisher       = (*ActivityProjectionRepository)(nil)
	_ activity.CheckpointStore = (*ActivityProjectionRepository)(nil)
	_ activity.SourceLeases    = (*ActivityProjectionRepository)(nil)
	_ activity.GenerationStore = (*ActivityProjectionRepository)(nil)
)

func NewActivityProjectionRepository(pool *pgxpool.Pool) (*ActivityProjectionRepository, error) {
	if pool == nil {
		return nil, errors.New("new activity projection repository: nil pool")
	}
	return &ActivityProjectionRepository{pool: pool}, nil
}

// PublishBatch satisfies activity.Publisher.
func (repository *ActivityProjectionRepository) PublishBatch(
	ctx context.Context,
	generation uint64,
	candidates []activity.CandidateEvent,
) (activity.PublishOutcome, error) {
	result, err := PublishActivityBatch(ctx, repository.pool, generation, candidates)
	if err != nil {
		return activity.PublishOutcome{}, err
	}
	return activity.PublishOutcome{
		Inserted:         result.Inserted,
		AlreadyPresent:   result.AlreadyPresent,
		PublishedThrough: result.PublishedThrough,
	}, nil
}

// LoadCheckpoint returns the stored position, or an empty one for a source that
// has never run in this generation. An empty position is not an error: it is what
// makes the first pass cover all history.
func (repository *ActivityProjectionRepository) LoadCheckpoint(
	ctx context.Context,
	generation uint64,
	kind activity.SourceKind,
) (activity.SourceCheckpoint, error) {
	if generation == 0 {
		return activity.SourceCheckpoint{}, ErrActivityGenerationInactive
	}
	var (
		recordedThrough *time.Time
		caughtUp        bool
		attempt         int64
		lastErrorCode   string
	)
	err := repository.pool.QueryRow(ctx, `
		select recorded_through, caught_up, attempt, last_error_code
		from public.record_activity_projection_checkpoints
		where project_id = $1 and projection_generation = $2 and source_kind = $3`,
		recordplatform.ProjectIDDefault, generation, string(kind),
	).Scan(&recordedThrough, &caughtUp, &attempt, &lastErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return activity.SourceCheckpoint{Kind: kind}, nil
	}
	if err != nil {
		return activity.SourceCheckpoint{}, fmt.Errorf("load activity checkpoint for %s: %w", kind, err)
	}

	checkpoint := activity.SourceCheckpoint{
		Kind:          kind,
		CaughtUp:      caughtUp,
		Attempt:       uint64(attempt),
		LastErrorCode: lastErrorCode,
	}
	if recordedThrough != nil {
		checkpoint.RecordedThrough = recordedThrough.UTC()
	}
	return checkpoint, nil
}

// SaveCheckpoint persists a position. The update refuses to move a position
// backwards: the projector already declines to do that, and enforcing it here as
// well means a stale worker writing an old position cannot open a gap that no
// later pass would ever re-read.
func (repository *ActivityProjectionRepository) SaveCheckpoint(
	ctx context.Context,
	generation uint64,
	checkpoint activity.SourceCheckpoint,
) error {
	if generation == 0 {
		return ErrActivityGenerationInactive
	}
	if !activity.ValidSourceKind(checkpoint.Kind) {
		return fmt.Errorf("save activity checkpoint: unknown source kind %q", checkpoint.Kind)
	}

	var recordedThrough *time.Time
	if !checkpoint.RecordedThrough.IsZero() {
		normalized := checkpoint.RecordedThrough.UTC()
		recordedThrough = &normalized
	}
	var lastSuccessAt *time.Time
	if checkpoint.LastErrorCode == "" {
		now := time.Now().UTC()
		lastSuccessAt = &now
	}

	if _, err := repository.pool.Exec(ctx, `
		insert into public.record_activity_projection_checkpoints (
		  project_id, projection_generation, source_kind,
		  recorded_through, caught_up, attempt, last_error_code, last_success_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (project_id, projection_generation, source_kind) do update
		set recorded_through = greatest(
		      public.record_activity_projection_checkpoints.recorded_through,
		      excluded.recorded_through
		    ),
		    caught_up = excluded.caught_up,
		    attempt = excluded.attempt,
		    last_error_code = excluded.last_error_code,
		    last_success_at = coalesce(
		      excluded.last_success_at,
		      public.record_activity_projection_checkpoints.last_success_at
		    ),
		    updated_at = now()`,
		recordplatform.ProjectIDDefault, generation, string(checkpoint.Kind),
		recordedThrough, checkpoint.CaughtUp, int64(checkpoint.Attempt), checkpoint.LastErrorCode, lastSuccessAt,
	); err != nil {
		return fmt.Errorf("save activity checkpoint for %s: %w", checkpoint.Kind, err)
	}
	return nil
}

// ActiveGeneration returns the generation currently being published into.
//
// The projector needs it on every pass rather than at startup: a disaster
// rebuild increments the generation, and a worker still writing into the old one
// would be filling a projection nobody reads.
func (repository *ActivityProjectionRepository) ActiveGeneration(ctx context.Context) (uint64, error) {
	var generation int64
	err := repository.pool.QueryRow(ctx, `
		select projection_generation
		from public.record_activity_projection_heads
		where project_id = $1 and head_state = 'active'`,
		recordplatform.ProjectIDDefault,
	).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		// No head row means nothing has ever been published, which is a deployment
		// state rather than a failure: the worker waits instead of inventing one.
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read active activity generation: %w", err)
	}
	if generation <= 0 {
		return 0, ErrActivityGenerationInactive
	}
	return uint64(generation), nil
}

// AcquireSourceLease takes the right to project one source, and reports whether
// it got it.
//
// The lease is not what makes concurrent projection safe — the head lock keeps
// allocation contiguous, publication is keyed on source identity, and a
// checkpoint refuses to move backwards, so two workers on one source would
// produce a correct result. What it prevents is the waste: two processes scanning
// the same window and contending on the same head row for rows one of them has
// already published.
//
// An expired lease is reclaimable, because a worker that died holding one must
// not park its source until someone notices.
func (repository *ActivityProjectionRepository) AcquireSourceLease(
	ctx context.Context,
	generation uint64,
	kind activity.SourceKind,
	ownerID string,
	ttl time.Duration,
) (bool, error) {
	if generation == 0 {
		return false, ErrActivityGenerationInactive
	}
	if !activity.ValidSourceKind(kind) {
		return false, fmt.Errorf("acquire activity source lease: unknown source kind %q", kind)
	}
	if !activity.ValidOwnerID(ownerID) {
		return false, fmt.Errorf("acquire activity source lease: invalid owner %q", ownerID)
	}
	if ttl <= 0 {
		return false, errors.New("acquire activity source lease: non-positive ttl")
	}

	// The insert covers a source that has never run; the update covers taking over
	// an unheld, expired, or already-ours lease. Re-acquiring our own extends it,
	// which is how a long pass keeps the lease alive.
	var claimed bool
	err := repository.pool.QueryRow(ctx, `
		insert into public.record_activity_projection_checkpoints (
		  project_id, projection_generation, source_kind, lease_owner_id, lease_expires_at
		) values ($1, $2, $3, $4, transaction_timestamp() + ($5 * interval '1 microsecond'))
		on conflict (project_id, projection_generation, source_kind) do update
		set lease_owner_id = excluded.lease_owner_id,
		    lease_expires_at = excluded.lease_expires_at,
		    updated_at = now()
		where public.record_activity_projection_checkpoints.lease_owner_id is null
		   or public.record_activity_projection_checkpoints.lease_owner_id = excluded.lease_owner_id
		   or public.record_activity_projection_checkpoints.lease_expires_at <= transaction_timestamp()
		returning true`,
		recordplatform.ProjectIDDefault, generation, string(kind), ownerID, ttl.Microseconds(),
	).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		// Someone else holds a live lease.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acquire activity source lease for %s: %w", kind, err)
	}
	return claimed, nil
}

// ReleaseSourceLease hands a source back. It only clears a lease this owner still
// holds, so a worker whose lease already expired and was taken over cannot cut
// the new holder's short.
func (repository *ActivityProjectionRepository) ReleaseSourceLease(
	ctx context.Context,
	generation uint64,
	kind activity.SourceKind,
	ownerID string,
) error {
	if generation == 0 {
		return ErrActivityGenerationInactive
	}
	if _, err := repository.pool.Exec(ctx, `
		update public.record_activity_projection_checkpoints
		set lease_owner_id = null, lease_expires_at = null, updated_at = now()
		where project_id = $1
		  and projection_generation = $2
		  and source_kind = $3
		  and lease_owner_id = $4`,
		recordplatform.ProjectIDDefault, generation, string(kind), ownerID,
	); err != nil {
		return fmt.Errorf("release activity source lease for %s: %w", kind, err)
	}
	return nil
}
