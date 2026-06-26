package store

import (
	"context"
	"encoding/json"
	"fmt"
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
	auditID, err := ids.New("cmd_aud")
	if err != nil {
		return fmt.Errorf("generate command action audit id: %w", err)
	}
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	source := event.Source
	if source == "" {
		source = monitoringinstances.CommandActionSourceAgentSync
	}
	var exitCode any
	if event.ExitCode != nil {
		exitCode = *event.ExitCode
	}

	eventSQL, err := commandActionAuditEventSQL(event.EventType)
	if err != nil {
		return err
	}
	_, err = exec.Exec(ctx, eventSQL,
		auditID,
		event.ActionID,
		event.MonitoringInstanceID,
		event.CommandID,
		event.Sensitivity,
		event.ActorUserID,
		source,
		exitCode,
		occurredAt,
	)
	return err
}

func commandActionAuditEventSQL(eventType string) (string, error) {
	switch eventType {
	case "queued":
		return commandActionAuditInsertSQL("queued"), nil
	case "dispatched":
		return commandActionAuditInsertSQL("dispatched"), nil
	case "completed":
		return commandActionAuditInsertSQL("completed"), nil
	default:
		return "", fmt.Errorf("unsupported command action audit event type %q", eventType)
	}
}

func commandActionAuditInsertSQL(eventType string) string {
	return `
		insert into monitoring_instance_command_action_audit (
			audit_id,
			action_id,
			monitoring_instance_id,
			command_id,
			sensitivity,
			event_type,
			actor_user_id,
			source,
			exit_code,
			occurred_at
		)
		values ($1, $2, $3, $4, $5, '` + eventType + `', nullif($6, ''), $7, $8, $9)`
}
