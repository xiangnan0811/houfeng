package store

import (
	"context"
	"fmt"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/vpsassets"
	"strings"

	"github.com/jackc/pgx/v5"
)

func vpsLifecycleAuditState(record vpsassets.Record) map[string]any {
	return map[string]any{"lifecycle_status": record.LifecycleStatus, "usage_status": record.UsageStatus, "renewal_decision": record.RenewalDecision, "archived_at": record.ArchivedAt, "archived_state_snapshot": record.ArchivedStateSnapshot}
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

func (r *PostgresAssetLifecycleRepository) StartVPSMigration(ctx context.Context, vpsID string, input assetlifecycle.StartMigrationInput) (_ assetlifecycle.LifecycleActionResult, resultErr error) {
	vpsID = strings.TrimSpace(vpsID)
	input.Reason = strings.TrimSpace(input.Reason)
	if vpsID == "" {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	if err := assetlifecycle.ValidateLifecycleReason(input.Reason); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := getLifecycleVPSAsset(ctx, tx, vpsID, true)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	defer r.finishVPSStateFailure(ctx, tx, current, assetlifecycle.ActionTypeStartMigration, input.Reason, &resultErr)
	switch current.LifecycleStatus {
	case vpsassets.LifecycleActive, vpsassets.LifecycleIdle, vpsassets.LifecycleTesting:
	case vpsassets.LifecycleToMigrate:
		if current.RenewalDecision == vpsassets.RenewalMigrate {
			return assetlifecycle.LifecycleActionResult{Steps: []assetlifecycle.LifecycleActionStep{}}, nil
		}
		fallthrough
	default:
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("%w: current vps cannot start migration", assetlifecycle.ErrLifecycleActionBlocked)
	}
	if err := vpsassets.ValidateVPSStateCombination(vpsassets.LifecycleToMigrate, current.UsageStatus, vpsassets.RenewalMigrate); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	updated, err := patchVPSAssetRow(ctx, tx, vpsID, vpsassets.PatchInput{LifecycleStatus: vpsassets.PatchLifecycle(vpsassets.LifecycleToMigrate), RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalMigrate)}, false)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	result, err := auditVPSStateTransition(ctx, tx, vpsID, assetlifecycle.ActionTypeStartMigration, input.Reason, vpsLifecycleAuditState(current), updated)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	return result, nil
}
