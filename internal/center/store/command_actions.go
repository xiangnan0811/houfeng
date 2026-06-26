package store

import (
	"encoding/json"

	"houfeng/internal/security/redact"
)

const (
	commandActionStatusPending = "pending"
	commandActionStatusDone    = "done"
)

func marshalPendingLastAction(actionID, commandID string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"action_id":  actionID,
		"command_id": commandID,
		"status":     commandActionStatusPending,
	})
}

func marshalCompletedLastAction(actionID, commandID, stdout, stderr string, exitCode int) ([]byte, error) {
	return json.Marshal(map[string]any{
		"action_id":  actionID,
		"command_id": commandID,
		"status":     commandActionStatusDone,
		"stdout":     redact.Secrets(stdout),
		"stderr":     redact.Secrets(stderr),
		"exit_code":  exitCode,
	})
}
