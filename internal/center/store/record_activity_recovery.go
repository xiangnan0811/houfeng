package store

import (
	"context"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
	if err := lockAssetGraphForSync(ctx, transaction); err != nil {
		return activity.RecoveryResult{}, err
	}
	var observedGeneration uint64
	if err := transaction.QueryRow(ctx, `
		select projection_generation from public.record_activity_projection_heads
		where project_id=$1 and head_state='active'`, recordplatform.ProjectIDDefault).Scan(&observedGeneration); err != nil {
		return activity.RecoveryResult{}, fmt.Errorf("rebuild activity projection: discover active head: %w", err)
	}
	observedRecords, err := discoverActivityGenerationRecords(ctx, transaction, observedGeneration)
	if err != nil {
		return activity.RecoveryResult{}, err
	}
	before, err := discoverEffectRecordResources(ctx, transaction, observedRecords, nil)
	if err != nil {
		return activity.RecoveryResult{}, err
	}
	declared, err := normalizeRecordMutationDeclarations(before)
	if err != nil {
		return activity.RecoveryResult{}, err
	}
	if err := lockComparisonDecisionForWriter(ctx, transaction); err != nil {
		return activity.RecoveryResult{}, err
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction, declared, 10, 60); err != nil {
		return activity.RecoveryResult{}, err
	}

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
	if retired != observedGeneration {
		return activity.RecoveryResult{}, &pgconn.PgError{Code: "40001", Message: "activity generation changed; restart recovery"}
	}
	if err := acquireRecordMutationResourceRange(ctx, transaction, declared, 71, 75); err != nil {
		return activity.RecoveryResult{}, err
	}
	after, err := discoverEffectRecordResources(ctx, transaction, observedRecords, nil)
	if err != nil {
		return activity.RecoveryResult{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return activity.RecoveryResult{}, &pgconn.PgError{Code: "40001", Message: "activity recovery record routes changed; restart recovery"}
	}
	currentRecords, err := discoverActivityGenerationRecords(ctx, transaction, retired)
	if err != nil {
		return activity.RecoveryResult{}, err
	}
	if !reflect.DeepEqual(observedRecords, currentRecords) {
		return activity.RecoveryResult{}, &pgconn.PgError{Code: "40001", Message: "activity recovery routes changed; restart recovery"}
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

func discoverActivityGenerationRecords(ctx context.Context, tx pgx.Tx, generation uint64) ([]string, error) {
	rows, err := tx.Query(ctx, `
		select record_id,unresolved from (
		  select record_id,false as unresolved from public.record_activity_projection where projection_generation=$1 and record_id is not null
		  union select record_id,false from public.record_activity_subjects where projection_generation=$1 and record_id is not null
		  union select record_id,false from public.record_activity_revision_intervals where projection_generation=$1
		  union select snapshot.record_id,false from public.record_activity_projection projection
		    join public.evidence_snapshots snapshot on snapshot.snapshot_id=projection.evidence_snapshot_id
		    where projection.projection_generation=$1
		  union select snapshot.record_id,false from public.record_activity_subjects subject
		    join public.evidence_snapshots snapshot on snapshot.snapshot_id=subject.evidence_snapshot_id
		    where subject.projection_generation=$1
		  union select null::text,true where exists (
		    select 1 from public.record_activity_projection projection
		    left join public.evidence_snapshots snapshot on snapshot.snapshot_id=projection.evidence_snapshot_id
		    where projection.projection_generation=$1 and projection.evidence_snapshot_id is not null and snapshot.snapshot_id is null)
		  union select null::text,true where exists (
		    select 1 from public.record_activity_subjects subject
		    left join public.evidence_snapshots snapshot on snapshot.snapshot_id=subject.evidence_snapshot_id
		    where subject.projection_generation=$1 and subject.evidence_snapshot_id is not null and snapshot.snapshot_id is null)
		) as routes order by record_id nulls last`, generation)
	if err != nil {
		return nil, fmt.Errorf("discover activity recovery records: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var recordID *string
		var unresolved bool
		if err := rows.Scan(&recordID, &unresolved); err != nil {
			return nil, fmt.Errorf("scan activity recovery record: %w", err)
		}
		if unresolved || recordID == nil {
			return nil, fmt.Errorf("rebuild activity projection: evidence owner route unavailable")
		}
		result = append(result, *recordID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read activity recovery records: %w", err)
	}
	return result, nil
}

var _ activity.RecoveryStore = (*ActivityProjectionRepository)(nil)
