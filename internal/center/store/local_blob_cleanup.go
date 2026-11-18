package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	"houfeng/internal/center/attachments"

	"github.com/jackc/pgx/v5"
)

func (repository *PostgresLocalBlobRepository) PrepareLocalCleanup(ctx context.Context, request attachments.LocalCleanupPreparation) (attachments.LocalCleanupTask, error) {
	statement := `select to_jsonb(g) from public.local_blob_gc_prepare($1) g`
	if request.OperationID != nil {
		statement = `select to_jsonb(g) from public.local_blob_purge_prepare($1) g`
	}
	var result attachments.LocalCleanupTask
	encoded, err := repository.localCleanupCommandBytes(ctx, statement, request)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, normalizeLocalCleanupDigests(&result)
}

func (repository *PostgresLocalBlobRepository) ClaimLocalCleanup(ctx context.Context, request attachments.LocalCleanupClaim) (attachments.LocalCleanupSnapshot, error) {
	statement := `select public.local_blob_gc_claim($1)`
	if request.OperationID != nil {
		statement = `select public.local_blob_purge_task_claim($1)`
	}
	return repository.localCleanupCommand(ctx, statement, request)
}
func (repository *PostgresLocalBlobRepository) ResolveLocalCleanup(ctx context.Context, request attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	statement := `select public.local_blob_gc_resolve($1)`
	if request.OperationID != nil {
		statement = `select public.local_blob_purge_task_resolve($1)`
	}
	return repository.localCleanupCommand(ctx, statement, request)
}
func (repository *PostgresLocalBlobRepository) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	statement := `select public.local_blob_gc_bind_final($1)`
	if request.OperationID != nil {
		statement = `select public.local_blob_purge_bind_final($1)`
	}
	return repository.localCleanupCommand(ctx, statement, request)
}
func (repository *PostgresLocalBlobRepository) CompleteLocalCleanup(ctx context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	statement := `select public.local_blob_gc_complete($1)`
	if request.OperationID != nil {
		statement = `select public.local_blob_purge_task_complete($1)`
	}
	return repository.localCleanupCommand(ctx, statement, request)
}
func (repository *PostgresLocalBlobRepository) localCleanupCommand(ctx context.Context, statement string, request any) (attachments.LocalCleanupSnapshot, error) {
	var result attachments.LocalCleanupSnapshot
	encoded, err := repository.localCleanupCommandBytes(ctx, statement, request)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, normalizeLocalCleanupDigests(&result.Task)
}

func (repository *PostgresLocalBlobRepository) localCleanupCommandBytes(ctx context.Context, statement string, request any) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var encoded []byte
	if err := tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return encoded, nil
}
func normalizeLocalCleanupDigests(task *attachments.LocalCleanupTask) error {
	for _, value := range []*string{task.InventoryDigest, task.ReceiptDigest} {
		if value == nil {
			continue
		}
		*value = strings.TrimPrefix(*value, `\x`)
		decoded, err := hex.DecodeString(*value)
		if err != nil || len(decoded) != 32 {
			return attachments.ErrLocalIdentityChanged
		}
	}
	return nil
}

var _ attachments.LocalCleanupRepository = (*PostgresLocalBlobRepository)(nil)
