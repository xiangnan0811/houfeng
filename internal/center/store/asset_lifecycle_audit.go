package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/vpsassets"

	"github.com/jackc/pgx/v5"
)

func insertAssetLifecycleAudit(ctx context.Context, tx pgx.Tx, vpsID string, actionType assetlifecycle.ActionType, status, reason string, summary map[string]any) (assetlifecycle.LifecycleActionRecord, error) {
	id, err := ids.New("ala")
	if err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("generate lifecycle audit id: %w", err)
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("encode lifecycle audit: %w", err)
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `insert into asset_lifecycle_actions (action_id,vps_id,action_type,status,reason,summary,created_at,confirmed_at,completed_at) values ($1,$2,$3,$4,$5,$6::jsonb,$7,$7,$7)`, id, vpsID, string(actionType), status, reason, data, now); err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("insert lifecycle audit: %w", err)
	}
	return assetlifecycle.LifecycleActionRecord{ActionID: id, VPSID: vpsID, ActionType: actionType, Status: status, Reason: reason, Summary: summary, CreatedAt: now, ConfirmedAt: &now, CompletedAt: &now}, nil
}

func (r *PostgresAssetLifecycleRepository) finishVPSStateFailure(ctx context.Context, tx pgx.Tx, current vpsassets.Record, kind assetlifecycle.ActionType, reason string, resultErr *error) {
	if *resultErr == nil {
		return
	}
	_ = tx.Rollback(ctx)
	cause := *resultErr
	auditTx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err == nil {
		defer func() { _ = auditTx.Rollback(ctx) }()
		var action assetlifecycle.LifecycleActionRecord
		action, err = insertAssetLifecycleAudit(ctx, auditTx, current.VPSID, kind, assetlifecycle.ActionStatusFailed, reason, map[string]any{"failure_reason": cause.Error()})
		if err == nil {
			_, err = insertLifecycleStep(ctx, auditTx, action.ActionID, assetlifecycle.ObjectTypeVPS, current.VPSID, assetlifecycle.StepTypeVPSLifecycle, assetlifecycle.StepStatusFailed, vpsLifecycleAuditState(current), map[string]any{"error": cause.Error()}, reason)
		}
		if err == nil {
			err = auditTx.Commit(ctx)
		}
	}
	if err != nil {
		*resultErr = fmt.Errorf("lifecycle action failed (%v), and failure audit could not be persisted: %w", cause, err)
	}
}
