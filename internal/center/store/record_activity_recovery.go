package store

import (
	"context"
	"fmt"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordplatform"
)

// RebuildActivityProjection retires the active generation and opens the next
// one empty. Purge receipts are intentionally left alone: they are the proof
// that a deleted record's presentation must stay absent after the rebuild.
func (repository *ActivityProjectionRepository) RebuildActivityProjection(
	ctx context.Context,
) (activity.RecoveryResult, error) {
	if ctx == nil || repository == nil || repository.pool == nil {
		return activity.RecoveryResult{}, activity.ErrInvalidRecoveryAdapter
	}

	transaction, err := repository.pool.Begin(ctx)
	if err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: begin: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var retired uint64
	err = transaction.QueryRow(ctx, `
		select projection_generation
		from public.record_activity_projection_heads
		where project_id = $1 and head_state = 'active'
		for update`,
		recordplatform.ProjectIDDefault,
	).Scan(&retired)
	if err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: lock active head: %w", err)
	}

	if _, err := transaction.Exec(ctx, `
		update public.record_activity_projection_heads
		set head_state = 'retired', retired_at = now(), updated_at = now()
		where project_id = $1 and projection_generation = $2 and head_state = 'active'`,
		recordplatform.ProjectIDDefault, retired,
	); err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: retire generation: %w", err)
	}

	active := retired + 1
	if _, err := transaction.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ($1, $2, 0, 0)`,
		recordplatform.ProjectIDDefault, active,
	); err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: open generation: %w", err)
	}

	var removed int64
	for _, statement := range []string{
		`delete from public.record_activity_subjects where projection_generation = $1`,
		`delete from public.record_activity_projection where projection_generation = $1`,
		`delete from public.record_activity_revision_intervals where projection_generation = $1`,
		`delete from public.record_activity_projection_checkpoints where projection_generation = $1`,
	} {
		tag, err := transaction.Exec(ctx, statement, retired)
		if err != nil {
			return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: clear retired rows: %w", err)
		}
		removed += tag.RowsAffected()
	}

	if err := transaction.Commit(ctx); err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: commit: %w", err)
	}
	return activity.RecoveryResult{
		RetiredGeneration: retired,
		ActiveGeneration:  active,
		RemovedRowCount:   uint64(removed),
	}, nil
}

var _ activity.RecoveryStore = (*ActivityProjectionRepository)(nil)
