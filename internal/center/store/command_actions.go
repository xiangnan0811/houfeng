package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/security/redact"
)

const (
	commandActionStatusPending = "pending"
	commandActionStatusDone    = "done"

	commandActionOutputTTL = 24 * time.Hour
)

type commandActionAuditExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type commandActionAuditEvent struct {
	ActionID             string
	MonitoringInstanceID string
	CommandID            string
	Sensitivity          string
	EventType            string
	ActorUserID          string
	Source               string
	ExitCode             *int
	OccurredAt           time.Time
}

func marshalPendingLastAction(actionID, commandID, sensitivity string, queuedAt time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{
		"action_id":   actionID,
		"command_id":  commandID,
		"status":      commandActionStatusPending,
		"sensitivity": sensitivity,
		"queued_at":   queuedAt.Format(time.RFC3339),
	})
}

func marshalDispatchedPendingLastAction(actionID, commandID string, existingRaw []byte, dispatchedAt time.Time) ([]byte, error) {
	sensitivity := sensitivityForKnownCommand(commandID)
	if sensitivity == "" {
		sensitivity = "standard"
	}
	queuedAt := dispatchedAt.UTC()
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	var existing monitoringinstances.LastAction
	if len(existingRaw) > 0 {
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return nil, fmt.Errorf("decode pending last_action: %w", err)
		}
		if existing.ActionID == actionID && existing.CommandID == commandID && existing.Status == commandActionStatusPending {
			if existing.Sensitivity != "" {
				sensitivity = existing.Sensitivity
			}
			if existing.QueuedAt != nil && !existing.QueuedAt.IsZero() {
				queuedAt = existing.QueuedAt.UTC()
			}
		}
	}

	return marshalPendingLastAction(actionID, commandID, sensitivity, queuedAt)
}

func marshalCompletedLastAction(actionID, commandID, stdout, stderr string, exitCode int, completedAt time.Time) ([]byte, error) {
	completedAt = completedAt.UTC()
	outputExpiresAt := completedAt.Add(commandActionOutputTTL)
	return json.Marshal(map[string]any{
		"action_id":         actionID,
		"command_id":        commandID,
		"status":            commandActionStatusDone,
		"stdout":            redact.Secrets(stdout),
		"stderr":            redact.Secrets(stderr),
		"exit_code":         exitCode,
		"completed_at":      completedAt.Format(time.RFC3339),
		"output_expires_at": outputExpiresAt.Format(time.RFC3339),
		"output_expired":    false,
	})
}

func sensitivityForKnownCommand(commandID string) string {
	sensitivity, ok := agentapi.SensitivityForCommand(commandID)
	if !ok {
		return ""
	}
	return string(sensitivity)
}

func insertCommandActionAudit(ctx context.Context, exec commandActionAuditExecutor, event commandActionAuditEvent) error {
	source := strings.TrimSpace(event.Source)
	if source == "" && event.EventType != "rejected" {
		source = monitoringinstances.CommandActionSourceAgentSync
	}
	event.Source = source
	if err := validateCommandActionAuditEvent(event); err != nil {
		return err
	}

	auditID, err := ids.New("cmd_aud")
	if err != nil {
		return fmt.Errorf("generate command action audit id: %w", err)
	}
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	var exitCode any
	if event.ExitCode != nil {
		exitCode = *event.ExitCode
	}
	var actionID any = event.ActionID
	if event.EventType == "rejected" {
		actionID = nil
	}

	eventSQL, err := commandActionAuditEventSQL(event.EventType)
	if err != nil {
		return err
	}
	tag, err := exec.Exec(ctx, eventSQL,
		auditID,
		actionID,
		event.MonitoringInstanceID,
		event.CommandID,
		event.Sensitivity,
		event.ActorUserID,
		source,
		exitCode,
		occurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert command action audit: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("command action audit must have inserted exactly one row: inserted %d", tag.RowsAffected())
	}
	return nil
}

func validateCommandActionAuditEvent(event commandActionAuditEvent) error {
	switch event.EventType {
	case "rejected":
		if event.ActionID != "" {
			return fmt.Errorf("rejected command action audit must not have an action id")
		}
		if event.Source != monitoringinstances.CommandActionSourceWeb {
			return fmt.Errorf("rejected command action audit must have web source")
		}
	case "queued", "dispatched", "completed":
		if event.ActionID == "" {
			return fmt.Errorf("%s command action audit must have an action id", event.EventType)
		}
	default:
		return fmt.Errorf("unsupported command action audit event type %q", event.EventType)
	}
	return nil
}

func commandActionAuditEventSQL(eventType string) (string, error) {
	switch eventType {
	case "queued":
		return commandActionAuditInsertSQL("queued"), nil
	case "dispatched":
		return commandActionAuditInsertSQL("dispatched"), nil
	case "completed":
		return commandActionAuditInsertSQL("completed"), nil
	case "rejected":
		return commandActionAuditInsertSQL("rejected"), nil
	default:
		return "", fmt.Errorf("unsupported command action audit event type %q", eventType)
	}
}

func commandActionAuditInsertSQL(eventType string) string {
	detailsSQL := "'{}'::jsonb"
	if eventType == "rejected" {
		detailsSQL = "jsonb_build_object('reason', 'sensitive_confirmation_required')"
	}
	return `
		insert into monitoring_instance_command_action_audit (
			audit_id,
			action_id,
			monitoring_instance_id,
			monitoring_instance_name_snapshot,
			command_id,
			sensitivity,
			event_type,
			actor_user_id,
			actor_username_snapshot,
			actor_display_name_snapshot,
			source,
			exit_code,
			occurred_at,
			details
		)
		select
			$1,
			$2,
			mi.monitoring_instance_id,
			mi.display_name,
			$4,
			$5,
			'` + eventType + `',
			nullif($6, ''),
			coalesce(actor.username, ''),
			coalesce(actor.display_name, ''),
			$7,
			$8,
			$9,
			` + detailsSQL + `
		from monitoring_instances mi
		left join users actor on actor.user_id = nullif($6, '')
		where mi.monitoring_instance_id = $3
			and (nullif($6, '') is null or actor.user_id is not null)`
}
