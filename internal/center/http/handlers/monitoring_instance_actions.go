package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/contracts/agentapi"
)

type monitoringInstanceActionRepository interface {
	QueueCommandAction(ctx context.Context, monitoringInstanceID string, input monitoringinstances.QueueCommandActionInput) error
	RecordRejectedCommandAction(ctx context.Context, monitoringInstanceID string, input monitoringinstances.RejectedCommandActionInput) error
	GetMonitoringInstance(ctx context.Context, monitoringInstanceID string) (monitoringinstances.Record, error)
}

// MonitoringInstanceActions handles POST /api/monitoring-instances/{id}/actions. The body must contain a
// command_id string. The handler validates that the monitoring instance exists, its agent has
// been bound, and monitoring is not paused, then queues a pending action that
// will be dispatched to the agent on its next sync.
func MonitoringInstanceActions(repo monitoringInstanceActionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceIDFromActionsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		var body struct {
			CommandID          string `json:"command_id"`
			ConfirmedSensitive bool   `json:"confirmed_sensitive,omitempty"`
		}
		if err := decodeJSON(r, &body); err != nil || body.CommandID == "" {
			writeError(w, http.StatusBadRequest, "command_id required")
			return
		}
		sensitivity, ok := agentapi.SensitivityForCommand(body.CommandID)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid command_id")
			return
		}
		if sensitivity == agentapi.CommandSensitivitySensitive && !body.ConfirmedSensitive {
			record, err := repo.GetMonitoringInstance(r.Context(), monitoringInstanceID)
			if err != nil && !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if err == nil && commandActionExecutable(record) {
				actorUserID, actorOK := sessionctx.UserIDFromContext(r.Context())
				if !actorOK || strings.TrimSpace(actorUserID) == "" {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				if err := repo.RecordRejectedCommandAction(r.Context(), monitoringInstanceID, monitoringinstances.RejectedCommandActionInput{
					CommandID:   body.CommandID,
					Sensitivity: string(sensitivity),
					ActorUserID: actorUserID,
					Source:      monitoringinstances.CommandActionSourceWeb,
					OccurredAt:  time.Now().UTC(),
				}); err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
			writeError(w, http.StatusBadRequest, "sensitive command confirmation required")
			return
		}

		record, err := repo.GetMonitoringInstance(r.Context(), monitoringInstanceID)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		if record.ArchivedAt != nil {
			writeError(w, http.StatusConflict, "archived monitoring instance")
			return
		}
		if record.BindingStatus != monitoringinstances.BindingBound {
			writeError(w, http.StatusConflict, "monitoring instance agent not bound")
			return
		}
		if record.MonitoringStatus == monitoringinstances.MonitoringPaused {
			writeError(w, http.StatusConflict, "monitoring instance monitoring is paused")
			return
		}

		actionID, err := ids.New("act")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		actorUserID, _ := sessionctx.UserIDFromContext(r.Context())
		if err := repo.QueueCommandAction(r.Context(), monitoringInstanceID, monitoringinstances.QueueCommandActionInput{
			ActionID:    actionID,
			CommandID:   body.CommandID,
			Sensitivity: string(sensitivity),
			ActorUserID: actorUserID,
			Source:      monitoringinstances.CommandActionSourceWeb,
			QueuedAt:    time.Now().UTC(),
		}); err != nil {
			if errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
				writeError(w, http.StatusConflict, "archived monitoring instance")
				return
			}
			if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
				writeError(w, http.StatusNotFound, "monitoring instance not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"action_id":  actionID,
			"command_id": body.CommandID,
			"status":     "pending",
		})
	})
}

func commandActionExecutable(record monitoringinstances.Record) bool {
	return record.ArchivedAt == nil &&
		record.BindingStatus == monitoringinstances.BindingBound &&
		record.MonitoringStatus != monitoringinstances.MonitoringPaused
}

// monitoringInstanceIDFromActionsPath extracts the monitoring instance ID from a /api/monitoring-instances/{id}/actions path.
func monitoringInstanceIDFromActionsPath(path string) (string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	if trimmed == "" {
		return "", false
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "actions" {
		return "", false
	}
	return segments[0], true
}
