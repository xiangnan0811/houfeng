package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/store"
)

type nodeBatchRepository interface {
	SetNodeMonitoringMaintenance(context.Context, string) (nodes.Record, error)
	PauseNodeMonitoring(context.Context, string) (nodes.Record, error)
	ResumeNodeMonitoring(context.Context, string) (nodes.Record, error)
}

type batchActionRequest struct {
	NodeIDs []string `json:"node_ids"`
	Action  string   `json:"action"`
}

type batchActionResult struct {
	NodeID string `json:"node_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type batchActionResponse struct {
	Results []batchActionResult `json:"results"`
}

var validBatchActions = map[string]bool{
	"enter-maintenance": true,
	"exit-maintenance":  true,
	"pause":             true,
	"resume":            true,
}

// NodeBatch handles POST /api/nodes/batch. It accepts node_ids and an action,
// executes the action on each node independently, and returns per-node results.
func NodeBatch(repo nodeBatchRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req batchActionRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if len(req.NodeIDs) == 0 {
			writeError(w, http.StatusBadRequest, "node_ids required")
			return
		}

		if !validBatchActions[req.Action] {
			writeError(w, http.StatusBadRequest, "invalid action")
			return
		}

		results := make([]batchActionResult, 0, len(req.NodeIDs))
		for _, nodeID := range req.NodeIDs {
			result := executeBatchAction(r.Context(), repo, nodeID, req.Action)
			results = append(results, result)
		}

		writeJSON(w, http.StatusOK, batchActionResponse{Results: results})
	})
}

func executeBatchAction(
	ctx context.Context,
	repo nodeBatchRepository,
	nodeID string,
	action string,
) batchActionResult {
	var err error
	switch action {
	case "enter-maintenance":
		_, err = repo.SetNodeMonitoringMaintenance(ctx, nodeID)
	case "exit-maintenance":
		_, err = repo.ResumeNodeMonitoring(ctx, nodeID)
	case "pause":
		_, err = repo.PauseNodeMonitoring(ctx, nodeID)
	case "resume":
		_, err = repo.ResumeNodeMonitoring(ctx, nodeID)
	default:
		err = errors.New("unknown action")
	}

	if err == nil {
		return batchActionResult{NodeID: nodeID, OK: true}
	}

	message := "internal server error"
	switch {
	case errors.Is(err, nodes.ErrNodeNotFound):
		message = "node not found"
	case errors.Is(err, store.ErrInvalidNodeRuntimeTransition):
		message = "invalid runtime transition"
	}

	return batchActionResult{
		NodeID: nodeID,
		OK:     false,
		Error:  message,
	}
}
