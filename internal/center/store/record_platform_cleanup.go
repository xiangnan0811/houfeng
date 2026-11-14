package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

// CleanupExpiredRecordPlatformPrimitives removes only expired terminal outbox
// rows. Expiry alone does not settle a pending or processing effect. Each
// candidate has its own transaction so locks from one record are never held
// while acquiring an earlier rank for another record.
func (repository *PostgresRecordPlatformRepository) CleanupExpiredRecordPlatformPrimitives(ctx context.Context) (recordplatform.ExpiredPrimitiveCleanupResultV1, error) {
	var result recordplatform.ExpiredPrimitiveCleanupResultV1
	var ceiling int64
	if err := repository.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		return transaction.tx.QueryRow(ctx, `select coalesce(max(outbox_row_id),0) from public.record_outbox`).Scan(&ceiling)
	}); err != nil {
		return result, err
	}
	var after int64
	for after < ceiling {
		var candidates []int64
		err := repository.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			candidates = nil
			rows, err := transaction.tx.Query(ctx, `select outbox_row_id from public.record_outbox
				where outbox_row_id>$1 and outbox_row_id<=$2 and status in ('sent','cancelled')
				  and expires_at<=clock_timestamp() and (owner_expires_at is null or owner_expires_at<=clock_timestamp())
				order by outbox_row_id limit 128`, after, ceiling)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				candidates = append(candidates, id)
			}
			return rows.Err()
		})
		if err != nil {
			return result, fmt.Errorf("discover expired outbox rows: %w", err)
		}
		if len(candidates) == 0 {
			break
		}
		for _, id := range candidates {
			after = id
			removed := false
			err := repository.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
				removed = false
				candidate, err := readOutboxCandidate(ctx, transaction.tx, id, false)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				candidate, recordID, err := acquireOutboxCandidateRoute(ctx, transaction, candidate, true)
				if err != nil {
					return err
				}
				body, err := outboxCandidateCommand(candidate, recordID, "", 0)
				if err != nil {
					return err
				}
				var deletedID int64
				err = transaction.tx.QueryRow(ctx, `select outbox_row_id from public.record_outbox_cleanup($1::bytea)`, body).Scan(&deletedID)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				removed = deletedID == id
				return nil
			})
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.Is(err, recordplatform.ErrLostOwnerLease) || errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) ||
					errors.Is(err, recordplatform.ErrInvalidOutboxEvent) ||
					(errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "55P03" || pgErr.Code == "55000")) {
					continue
				}
				return result, fmt.Errorf("cleanup expired outbox row: %w", err)
			}
			if removed {
				result.OutboxEvents++
			}
		}
	}
	return result, nil
}
