package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
)

// EnsurePublishedRecordSearchGeneration makes the index writable at startup.
// The projector only writes generations that are published or building, so
// without a published generation every record would commit with no index entry
// and search would answer nothing on a fresh install.
//
// It is idempotent and cheap: it inserts an empty published generation only when
// none exists, and never touches an existing one. Records that predate the
// generation are not backfilled here; covering those is a rebuild's job, and a
// fresh install has none.
func EnsurePublishedRecordSearchGeneration(ctx context.Context, pool *pgxpool.Pool) error {
	if ctx == nil || pool == nil {
		return errors.New("ensure published record search generation: nil dependency")
	}
	_, err := pool.Exec(ctx, `
		with published as (
		  select 1
		  from public.record_search_generations
		  where project_id = $1 and generation_state = 'published'
		),
		next_generation as (
		  select coalesce(max(generation), 0) + 1 as generation
		  from public.record_search_generations
		)
		insert into public.record_search_generations (
		  generation, project_id, generation_state, published_at
		)
		select next_generation.generation, $1, 'published', transaction_timestamp()
		from next_generation
		where not exists (select 1 from published)`,
		recordplatform.ProjectIDDefault,
	)
	if err == nil {
		return nil
	}
	// A concurrent center that published first is the outcome this wants, so a
	// unique violation on the single-published-generation index is success.
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return nil
	}
	return fmt.Errorf("ensure published record search generation: %w", err)
}
