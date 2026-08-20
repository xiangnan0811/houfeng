package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordplatform"
)

// loadActiveSourceCaughtUp reads whether the active projection generation has
// caught up for one source. Missing checkpoint rows mean not caught up.
func loadActiveSourceCaughtUp(
	ctx context.Context,
	pool *pgxpool.Pool,
	kind activity.SourceKind,
) (bool, error) {
	var caughtUp bool
	err := pool.QueryRow(ctx, `
		select checkpoint.caught_up
		from public.record_activity_projection_checkpoints checkpoint
		join public.record_activity_projection_heads head
		  on head.project_id = checkpoint.project_id
		 and head.projection_generation = checkpoint.projection_generation
		where checkpoint.project_id = $1
		  and checkpoint.source_kind = $2
		  and head.head_state = 'active'`,
		recordplatform.ProjectIDDefault,
		string(kind),
	).Scan(&caughtUp)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load active caught_up for %s: %w", kind, err)
	}
	return caughtUp, nil
}
