package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every graph participant locks before its first business read. The separate
// statement and READ COMMITTED isolation provide a fresh snapshot after waiting.
// Management must never upgrade a shared sync lock to an exclusive lock.
func lockAssetGraph(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(1213154899, 1)`); err != nil {
		return fmt.Errorf("lock asset graph: %w", err)
	}
	return nil
}

func lockAssetGraphForSync(ctx context.Context, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899, 1)`); err != nil {
		return fmt.Errorf("lock asset graph for sync: %w", err)
	}
	return nil
}

func beginAssetGraphTx(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error)) (pgx.Tx, error) {
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	if err := lockAssetGraph(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
