package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

// ConfigurePortabilityDeletionLocalCleanup binds a namespace without changing
// the repository's database identity. Purging requires a direct worker login.
func (repository *PostgresRecordDeletionRepository) ConfigurePortabilityDeletionLocalCleanup(coordinator *attachments.LocalCleanupCoordinator) error {
	if repository == nil || coordinator == nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if !repository.portabilityCleanup.CompareAndSwap(nil, coordinator) && repository.portabilityCleanup.Load() != coordinator {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func (repository *PostgresRecordDeletionRepository) localPortabilityPurgeCommand(ctx context.Context, statement string, body []byte, result any) error {
	return repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var encoded []byte
		if err := transaction.tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
			return err
		}
		return json.Unmarshal(encoded, result)
	})
}

func (repository *PostgresRecordDeletionRepository) purgeLocalRecordPortability(ctx context.Context, target recorddeletion.PurgeTarget, surfaceDigest [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error) {
	coordinator, request, body, err := localAdapterPurgeBinding(repository.portabilityCleanup.Load(), target, surfaceDigest)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	var preparation struct {
		Targets []struct {
			DeletionID string `json:"deletion_id"`
			Key        string `json:"key"`
		} `json:"targets"`
	}
	if err := repository.localPortabilityPurgeCommand(ctx, `select public.local_portability_purge_prepare($1)`, body, &preparation); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("prepare local portability purge: %w", err)
	}
	for _, item := range preparation.Targets {
		_, err := coordinator.Cleanup(ctx, attachments.LocalCleanupPreparation{
			LocalCleanupOperation: request.LocalCleanupOperation,
			DeletionID:            item.DeletionID, NamespaceID: request.NamespaceID,
			IncarnationID: request.IncarnationID, Key: item.Key,
		}, target.OwnerClaim.Owner.OwnerID)
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("clean local portability purge target: %w", err)
		}
	}
	var completed localAdapterPurgeReceipt
	if err := repository.localPortabilityPurgeCommand(ctx, `select public.local_portability_purge_complete($1)`, body, &completed); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("complete local portability purge: %w", err)
	}
	return completed.adapterReceipt(recorddeletion.AdapterNameRecordPortability, target, surfaceDigest)
}

func (repository *PostgresRecordDeletionRepository) verifyLocalRecordPortabilityPurge(ctx context.Context, target recorddeletion.PurgeTarget, surfaceDigest [sha256.Size]byte, receipt recorddeletion.AdapterPurgeReceipt) error {
	_, _, body, err := localAdapterPurgeBinding(repository.portabilityCleanup.Load(), target, surfaceDigest)
	if err != nil {
		return err
	}
	var completed localAdapterPurgeReceipt
	if err := repository.localPortabilityPurgeCommand(ctx, `select public.local_portability_purge_verify($1)`, body, &completed); err != nil {
		return fmt.Errorf("verify local portability purge: %w", err)
	}
	stored, err := completed.adapterReceipt(recorddeletion.AdapterNameRecordPortability, target, surfaceDigest)
	if err != nil {
		return err
	}
	if stored != receipt {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}
