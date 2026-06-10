package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
)

type monitoringInstanceBatchRepository interface {
	SetMonitoringInstanceMonitoringMaintenance(context.Context, string) (monitoringinstances.Record, error)
	PauseMonitoringInstanceMonitoring(context.Context, string) (monitoringinstances.Record, error)
	ResumeMonitoringInstanceMonitoring(context.Context, string) (monitoringinstances.Record, error)
}

type batchActionRequest struct {
	MonitoringInstanceIDs []string `json:"monitoring_instance_ids"`
	Action                string   `json:"action"`
}

type batchActionResult struct {
	MonitoringInstanceID string `json:"monitoring_instance_id"`
	OK                   bool   `json:"ok"`
	Error                string `json:"error,omitempty"`
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

// MonitoringInstanceBatch handles POST /api/monitoring-instances/batch. It accepts monitoring_instance_ids and an action,
// executes the action on each monitoringInstance independently, and returns per-monitoringInstance results.
func MonitoringInstanceBatch(repo monitoringInstanceBatchRepository) http.Handler {
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

		if len(req.MonitoringInstanceIDs) == 0 {
			writeError(w, http.StatusBadRequest, "monitoring_instance_ids required")
			return
		}

		if !validBatchActions[req.Action] {
			writeError(w, http.StatusBadRequest, "invalid action")
			return
		}

		results := make([]batchActionResult, 0, len(req.MonitoringInstanceIDs))
		for _, monitoringInstanceID := range req.MonitoringInstanceIDs {
			result := executeBatchAction(r.Context(), repo, monitoringInstanceID, req.Action)
			results = append(results, result)
		}

		writeJSON(w, http.StatusOK, batchActionResponse{Results: results})
	})
}

func executeBatchAction(
	ctx context.Context,
	repo monitoringInstanceBatchRepository,
	monitoringInstanceID string,
	action string,
) batchActionResult {
	var err error
	switch action {
	case "enter-maintenance":
		_, err = repo.SetMonitoringInstanceMonitoringMaintenance(ctx, monitoringInstanceID)
	case "exit-maintenance":
		_, err = repo.ResumeMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
	case "pause":
		_, err = repo.PauseMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
	case "resume":
		_, err = repo.ResumeMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
	default:
		err = errors.New("unknown action")
	}

	if err == nil {
		return batchActionResult{MonitoringInstanceID: monitoringInstanceID, OK: true}
	}

	message := "internal server error"
	switch {
	case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
		message = "monitoring instance not found"
	case errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance):
		message = "archived monitoring instance"
	case errors.Is(err, store.ErrInvalidMonitoringInstanceRuntimeTransition):
		message = "invalid runtime transition"
	}

	return batchActionResult{
		MonitoringInstanceID: monitoringInstanceID,
		OK:                   false,
		Error:                message,
	}
}
