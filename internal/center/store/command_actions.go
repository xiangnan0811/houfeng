package store

import "encoding/json"

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
		"stdout":     stdout,
		"stderr":     stderr,
		"exit_code":  exitCode,
	})
}
