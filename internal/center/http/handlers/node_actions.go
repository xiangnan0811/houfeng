package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/nodes"
)

type nodeActionRepository interface {
	SetPendingAction(ctx context.Context, nodeID, actionID, commandID string) error
	GetNode(ctx context.Context, nodeID string) (nodes.Record, error)
}

// NodeActions handles POST /api/nodes/{id}/actions. The body must contain a
// command_id string. The handler validates that the node exists, its agent has
// been bound, and monitoring is not paused, then queues a pending action that
// will be dispatched to the agent on its next sync.
func NodeActions(repo nodeActionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID, ok := nodeActionsNodeID(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		var body struct {
			CommandID string `json:"command_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CommandID == "" {
			writeError(w, http.StatusBadRequest, "command_id required")
			return
		}

		record, err := repo.GetNode(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if record.BindingStatus != nodes.BindingBound {
			writeError(w, http.StatusConflict, "node agent not bound")
			return
		}
		if record.MonitoringStatus == nodes.MonitoringPaused {
			writeError(w, http.StatusConflict, "node monitoring is paused")
			return
		}

		actionID, err := ids.New("act")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if err := repo.SetPendingAction(r.Context(), nodeID, actionID, body.CommandID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"action_id": actionID,
			"status":    "pending",
		})
	})
}

// nodeActionsNodeID extracts the node ID from a /api/nodes/{id}/actions path.
func nodeActionsNodeID(path string) (string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/nodes/"), "/")
	if trimmed == "" {
		return "", false
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "actions" {
		return "", false
	}
	return segments[0], true
}
