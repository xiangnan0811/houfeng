package store

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/monitoringinstances"
)

// Demotion preserves the identity and online evidence of every old installation.
// It never authorizes an old installation to collect again.
func demoteMonitoringSessionsTx(ctx context.Context, tx pgx.Tx, current monitoringinstances.Record) error {
	if _, err := tx.Exec(ctx, `update monitoring_agent_sessions set capability='evidence_only', ended_at=coalesce(ended_at,greatest(started_at,clock_timestamp())) where monitoring_instance_id=$1`, current.MonitoringInstanceID); err != nil {
		return fmt.Errorf("demote monitoring sessions: %w", err)
	}
	actionID, commandID := current.PendingActionID, current.PendingActionCommandID
	if actionID == "" && current.LastAction != nil && current.LastAction.Status == "pending" {
		actionID = current.LastAction.ActionID
		commandID = current.LastAction.CommandID
	}
	if actionID != "" {
		sensitivity := sensitivityForKnownCommand(commandID)
		if sensitivity == "" {
			sensitivity = "standard"
		}
		if err := insertCommandActionAudit(ctx, tx, commandActionAuditEvent{ActionID: actionID, MonitoringInstanceID: current.MonitoringInstanceID, CommandID: commandID, Sensitivity: sensitivity, EventType: "cancelled", Source: monitoringinstances.CommandActionSourceWeb}); err != nil {
			return err
		}
	}
	return nil
}

// Runtime control, enrollment, and unavailable evidence must never masquerade as
// a successful health observation in list/detail projections.
func projectMonitoringHealth(record *monitoringinstances.Record) {
	switch {
	case record.VPSLifecycleStatus == "archived":
		record.CurrentHealthStatus = "已归档"
	case record.LifecycleStatus == monitoringinstances.LifecycleRetired:
		record.CurrentHealthStatus = "已退役"
	case record.LifecycleStatus == monitoringinstances.LifecyclePendingEnrollment:
		record.CurrentHealthStatus = "未接入"
	case record.MonitoringStatus == monitoringinstances.MonitoringPaused:
		record.CurrentHealthStatus = "暂停"
	case record.MonitoringStatus == monitoringinstances.MonitoringMaintenance:
		record.CurrentHealthStatus = "维护中"
	case record.BindingStatus != monitoringinstances.BindingBound:
		record.CurrentHealthStatus = "绑定待确认"
	case record.LastTrustedOnlineAt == nil || record.CurrentHealthStatus == "unknown" || record.CurrentHealthStatus == "未接入" || record.CurrentHealthStatus == "":
		record.CurrentHealthStatus = "数据不可用"
	}
}
