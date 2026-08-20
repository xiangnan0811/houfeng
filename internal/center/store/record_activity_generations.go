package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
)

// EnsureActiveActivityProjectionGeneration makes the activity projection
// writable at startup. The worker waits when no generation is active, so without
// this a fresh install would never project anything until someone remembered to
// run a rebuild by hand.
//
// It is idempotent: it inserts generation 1 only when no active head exists, and
// never touches an existing one. Covering history that predates the generation
// is the worker's job on its first pass.
func EnsureActiveActivityProjectionGeneration(ctx context.Context, pool *pgxpool.Pool) error {
	if ctx == nil || pool == nil {
		return errors.New("ensure active activity projection generation: nil dependency")
	}
	_, err := pool.Exec(ctx, `
		with active as (
		  select 1
		  from public.record_activity_projection_heads
		  where project_id = $1 and head_state = 'active'
		),
		next_generation as (
		  select coalesce(max(projection_generation), 0) + 1 as generation
		  from public.record_activity_projection_heads
		)
		insert into public.record_activity_projection_heads (
		  project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence
		)
		select $1, next_generation.generation, 0, 0
		from next_generation
		where not exists (select 1 from active)`,
		recordplatform.ProjectIDDefault,
	)
	if err == nil {
		return nil
	}
	// A concurrent center that opened the first generation is the outcome this
	// wants, so a unique violation on the single-active-head index is success.
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return nil
	}
	return fmt.Errorf("ensure active activity projection generation: %w", err)
}
