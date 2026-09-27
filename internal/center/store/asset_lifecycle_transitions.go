package store

import (
	"context"

	"houfeng/internal/center/assetlifecycle"

	"houfeng/internal/center/vpsassets"

	"github.com/jackc/pgx/v5"
)

func vpsLifecycleAuditState(record vpsassets.Record) map[string]any {
	return map[string]any{"lifecycle_status": record.LifecycleStatus, "usage_tags": record.UsageTags, "renewal_decision": record.RenewalDecision, "archived_at": record.ArchivedAt, "archived_state_snapshot": record.ArchivedStateSnapshot}
}

func auditVPSStateTransition(ctx context.Context, tx pgx.Tx, vpsID string, kind assetlifecycle.ActionType, reason string, before map[string]any, after vpsassets.Record) (assetlifecycle.LifecycleActionResult, error) {
	action, err := insertAssetLifecycleAudit(ctx, tx, vpsID, kind, assetlifecycle.ActionStatusCompleted, reason, map[string]any{"vps_lifecycle_status": after.LifecycleStatus})
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	step, err := insertLifecycleStep(ctx, tx, action.ActionID, assetlifecycle.ObjectTypeVPS, vpsID, assetlifecycle.StepTypeVPSLifecycle, assetlifecycle.StepStatusCompleted, before, vpsLifecycleAuditState(after), reason)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	return assetlifecycle.LifecycleActionResult{Action: action, Steps: []assetlifecycle.LifecycleActionStep{step}}, nil
}
