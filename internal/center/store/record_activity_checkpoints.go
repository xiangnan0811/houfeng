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
	lease activity.SourceLease,
	candidates []activity.CandidateEvent,
) (activity.PublishOutcome, error) {
	result, err := PublishActivityBatch(ctx, repository.pool, lease, candidates)
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

// SaveCheckpoint advances only the exact live source claim.
func (repository *ActivityProjectionRepository) SaveCheckpoint(ctx context.Context, lease activity.SourceLease, checkpoint activity.SourceCheckpoint) error {
	if err := lease.Validate(); err != nil || checkpoint.Kind != lease.Kind {
		return activity.ErrSourceLeaseLost
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save activity checkpoint: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		return err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return err
	}
	if err := lockActivityLeaseHead(ctx, tx, lease.Generation); err != nil {
		return err
	}
	if err := assertActivitySourceLease(ctx, tx, lease); err != nil {
		return err
	}
	var recordedThrough *time.Time
	if !checkpoint.RecordedThrough.IsZero() {
		normalized := checkpoint.RecordedThrough.UTC()
		recordedThrough = &normalized
	}
	command, err := tx.Exec(ctx, `
		update public.record_activity_projection_checkpoints
		set recorded_through = greatest(recorded_through, $4), caught_up = $5,
		    attempt = $6, last_error_code = $7,
		    last_success_at = case when $7 = '' then clock_timestamp() else last_success_at end,
		    updated_at = clock_timestamp()
		where project_id=$1 and projection_generation=$2 and source_kind=$3
		  and lease_owner_id=$8 and lease_expires_at=$9 and lease_expires_at > clock_timestamp()`,
		recordplatform.ProjectIDDefault, lease.Generation, string(lease.Kind), recordedThrough,
		checkpoint.CaughtUp, int64(checkpoint.Attempt), checkpoint.LastErrorCode, lease.OwnerID, lease.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save activity checkpoint for %s: %w", checkpoint.Kind, err)
	}
	if command.RowsAffected() != 1 {
		return activity.ErrSourceLeaseLost
	}
	if err := assertActivitySourceLease(ctx, tx, lease); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save activity checkpoint: commit: %w", err)
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

// AcquireSourceLease commits an exact owner and database expiry before a pass.
func (repository *ActivityProjectionRepository) AcquireSourceLease(ctx context.Context, generation uint64, kind activity.SourceKind, ownerID string, ttl time.Duration) (activity.SourceLease, bool, error) {
	if generation == 0 {
		return activity.SourceLease{}, false, ErrActivityGenerationInactive
	}
	if !activity.ValidSourceKind(kind) || !activity.ValidOwnerID(ownerID) || ttl.Microseconds() <= 0 {
		return activity.SourceLease{}, false, errors.New("acquire activity source lease: invalid claim")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return activity.SourceLease{}, false, fmt.Errorf("acquire activity source lease: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		return activity.SourceLease{}, false, err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return activity.SourceLease{}, false, err
	}
	if err := lockActivityLeaseHead(ctx, tx, generation); err != nil {
		return activity.SourceLease{}, false, err
	}
	lease := activity.SourceLease{Generation: generation, Kind: kind, OwnerID: ownerID}
	err = tx.QueryRow(ctx, `
		insert into public.record_activity_projection_checkpoints (
		  project_id, projection_generation, source_kind, lease_owner_id, lease_expires_at
		) values ($1, $2, $3, $4, clock_timestamp() + ($5 * interval '1 microsecond'))
		on conflict (project_id, projection_generation, source_kind) do update
		set lease_owner_id = excluded.lease_owner_id,
		    lease_expires_at = greatest(excluded.lease_expires_at, public.record_activity_projection_checkpoints.lease_expires_at + interval '1 microsecond'),
		    updated_at = clock_timestamp()
		where public.record_activity_projection_checkpoints.lease_owner_id is null
		   or public.record_activity_projection_checkpoints.lease_owner_id = excluded.lease_owner_id
		   or public.record_activity_projection_checkpoints.lease_expires_at <= clock_timestamp()
		returning lease_expires_at`,
		recordplatform.ProjectIDDefault, generation, string(kind), ownerID, ttl.Microseconds(),
	).Scan(&lease.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return activity.SourceLease{}, false, nil
	}
	if err != nil {
		return activity.SourceLease{}, false, fmt.Errorf("acquire activity source lease for %s: %w", kind, err)
	}
	lease.ExpiresAt = lease.ExpiresAt.UTC()
	if err := tx.Commit(ctx); err != nil {
		return activity.SourceLease{}, false, fmt.Errorf("acquire activity source lease: commit: %w", err)
	}
	return lease, true, nil
}

// ReleaseSourceLease cannot clear a renewed or taken-over source lease.
func (repository *ActivityProjectionRepository) ReleaseSourceLease(ctx context.Context, lease activity.SourceLease) error {
	if err := lease.Validate(); err != nil {
		return activity.ErrSourceLeaseLost
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("release activity source lease: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		return err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return err
	}
	if err := lockActivityLeaseHead(ctx, tx, lease.Generation); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		update public.record_activity_projection_checkpoints
		set lease_owner_id=null, lease_expires_at=null, updated_at=clock_timestamp()
		where project_id=$1 and projection_generation=$2 and source_kind=$3
		  and lease_owner_id=$4 and lease_expires_at=$5 and lease_expires_at > clock_timestamp()`,
		recordplatform.ProjectIDDefault, lease.Generation, string(lease.Kind), lease.OwnerID, lease.ExpiresAt)
	if err != nil {
		return fmt.Errorf("release activity source lease for %s: %w", lease.Kind, err)
	}
	if command.RowsAffected() != 1 {
		return activity.ErrSourceLeaseLost
	}
	return tx.Commit(ctx)
}

func lockActivityLeaseHead(ctx context.Context, tx pgx.Tx, generation uint64) error {
	var exists bool
	err := tx.QueryRow(ctx, `select true from public.record_activity_projection_heads
		where project_id=$1 and projection_generation=$2 and head_state='active' for share`,
		recordplatform.ProjectIDDefault, generation).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrActivityGenerationInactive
	}
	return err
}

// The caller already holds the generation head; expiry is checked using the
// current database clock after any wait and immediately before commit.
func assertActivitySourceLease(ctx context.Context, tx pgx.Tx, lease activity.SourceLease) error {
	var owned bool
	err := tx.QueryRow(ctx, `select lease_owner_id=$4 and lease_expires_at=$5 and lease_expires_at > clock_timestamp()
		from public.record_activity_projection_checkpoints
		where project_id=$1 and projection_generation=$2 and source_kind=$3 for update`,
		recordplatform.ProjectIDDefault, lease.Generation, string(lease.Kind), lease.OwnerID, lease.ExpiresAt).Scan(&owned)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !owned {
		return activity.ErrSourceLeaseLost
	}
	if err != nil {
		return fmt.Errorf("check activity source lease: %w", err)
	}
	return nil
}
