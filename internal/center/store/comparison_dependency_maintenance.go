package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/records"
)

func (repository *PostgresEvidenceRepository) BackfillComparisonDependencyIndex(ctx context.Context, afterSnapshotID string, limit uint64) (evidence.ComparisonDependencyIndexBatchResult, error) {
	var result evidence.ComparisonDependencyIndexBatchResult
	if ctx == nil || repository == nil || limit == 0 || limit > evidence.MaxMaintenanceBatchSize ||
		(afterSnapshotID != "" && !evidence.ValidSnapshotID(afterSnapshotID)) {
		return result, evidence.ErrInvalidMaintenanceResult
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select snapshot_id, record_id from public.evidence_snapshots
		where kind = $1 and snapshot_id collate "C" > $2 collate "C"
		order by snapshot_id collate "C" limit $3`, string(evidence.ComparisonResultV1Key().Kind), afterSnapshotID, int64(limit))
	if err != nil {
		return result, fmt.Errorf("discover comparison index batch: %w", err)
	}
	type candidate struct{ SnapshotID, RecordID string }
	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByPos[candidate])
	if err != nil {
		return result, fmt.Errorf("read comparison index batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		var complete bool
		for attempt := 0; ; attempt++ {
			complete, err = repository.backfillComparisonCandidate(ctx, candidate.SnapshotID, candidate.RecordID)
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) && postgresError.Code == "40001" && attempt < 2 {
				continue
			}
			break
		}
		if err != nil {
			// No continuation is published for a partially failed batch. Already
			// committed indexes are revalidated safely on the next pass.
			return evidence.ComparisonDependencyIndexBatchResult{}, err
		}
		result.Examined++
		if !complete {
			result.Incomplete++
		}
	}
	if result.Examined == limit {
		result.AfterSnapshotID = candidates[len(candidates)-1].SnapshotID
	}
	return result, nil
}

func (repository *PostgresEvidenceRepository) backfillComparisonCandidate(ctx context.Context, snapshotID, recordID string) (bool, error) {
	tx, err := repository.startEvidenceMutationTransaction(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	observed, err := discoverComparisonBackfillResources(ctx, tx, snapshotID, recordID)
	if errors.Is(err, records.ErrRecordNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := acquireRecordMutationResources(ctx, tx, observed); err != nil {
		if errors.Is(err, records.ErrRecordDeletionReserved) {
			return false, nil
		}
		return false, err
	}
	current, err := discoverComparisonBackfillResources(ctx, tx, snapshotID, recordID)
	if err != nil || !reflect.DeepEqual(observed.resources, current.resources) {
		return false, &pgconn.PgError{Code: "40001", Message: "comparison backfill route changed; restart transaction"}
	}
	complete, err := backfillPersistedComparisonResult(ctx, tx, snapshotID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit comparison index backfill: %w", err)
	}
	return complete, nil
}

// VerifyComparisonDependencyIndex examines the actual result set in one
// read-only snapshot. A maintenance cursor or a matching row count is never a
// substitute for canonical validity and complete per-result coverage.
func (repository *PostgresEvidenceRepository) VerifyComparisonDependencyIndex(ctx context.Context) error {
	if ctx == nil || repository == nil || repository.platform == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	return repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		_, err := readComparisonDeletionDecision(ctx, transaction.tx, nil)
		return err
	})
}
