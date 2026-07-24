package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

const cleanupExpiredOutboxSQL = `
	delete from public.record_outbox
	where expires_at <= transaction_timestamp()
	  and (owner_expires_at is null or owner_expires_at <= transaction_timestamp())`

// CleanupExpiredRecordPlatformPrimitives removes only expired outbox rows.
// Idempotency and lease rows retain expired generation tombstones because
// deleting their reusable keys would permit ABA owner-token reuse. Durable
// reservations, purge operations, and ledger evidence are also outside this
// cleanup boundary.
func (repository *PostgresRecordPlatformRepository) CleanupExpiredRecordPlatformPrimitives(ctx context.Context) (recordplatform.ExpiredPrimitiveCleanupResultV1, error) {
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ExpiredPrimitiveCleanupResultV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ExpiredPrimitiveCleanupResultV1{}, err
	}
	var result recordplatform.ExpiredPrimitiveCleanupResultV1
	if result.OutboxEvents, err = execCleanup(ctx, tx, cleanupExpiredOutboxSQL, "outbox events"); err != nil {
		return recordplatform.ExpiredPrimitiveCleanupResultV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ExpiredPrimitiveCleanupResultV1{}, fmt.Errorf("commit record platform primitive cleanup transaction: %w", err)
	}
	return result, nil
}

func execCleanup(ctx context.Context, tx pgx.Tx, sql, relation string) (int64, error) {
	command, err := tx.Exec(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired %s: %w", relation, err)
	}
	return command.RowsAffected(), nil
}
