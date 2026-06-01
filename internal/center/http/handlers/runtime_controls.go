package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
)

type monitoringInstanceRuntimeControlRepository interface {
	SetMonitoringInstanceMonitoringMaintenance(context.Context, string) (monitoringinstances.Record, error)
	PauseMonitoringInstanceMonitoring(context.Context, string) (monitoringinstances.Record, error)
	ResumeMonitoringInstanceMonitoring(context.Context, string) (monitoringinstances.Record, error)
}

type targetRuntimeControlRepository interface {
	SetTargetMaintenance(context.Context, string) (targets.TargetRecord, error)
	PauseTargetRun(context.Context, string) (targets.TargetRecord, error)
	ResumeTargetRun(context.Context, string) (targets.TargetRecord, error)
	ArchiveTarget(context.Context, string) (targets.TargetRecord, error)
	RestoreArchivedTargetToPaused(context.Context, string) (targets.TargetRecord, error)
}

func MonitoringInstanceRuntimeControls(repo monitoringInstanceRuntimeControlRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, action := monitoringInstanceRuntimeControlAction(r.URL.Path)
		if monitoringInstanceID == "" || action == "" {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		var (
			record monitoringinstances.Record
			err    error
		)
		switch action {
		case "enter-maintenance":
			record, err = repo.SetMonitoringInstanceMonitoringMaintenance(r.Context(), monitoringInstanceID)
		case "exit-maintenance", "resume":
			record, err = repo.ResumeMonitoringInstanceMonitoring(r.Context(), monitoringInstanceID)
		case "pause":
			record, err = repo.PauseMonitoringInstanceMonitoring(r.Context(), monitoringInstanceID)
		default:
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		switch {
		case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		case errors.Is(err, store.ErrInvalidMonitoringInstanceRuntimeTransition):
			writeError(w, http.StatusConflict, "invalid runtime transition")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func TargetRuntimeControls(repo targetRuntimeControlRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		targetID, action := targetRuntimeControlAction(r.URL.Path)
		if targetID == "" || action == "" {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		var (
			record targets.TargetRecord
			err    error
		)
		switch action {
		case "enter-maintenance":
			record, err = repo.SetTargetMaintenance(r.Context(), targetID)
		case "exit-maintenance", "resume":
			record, err = repo.ResumeTargetRun(r.Context(), targetID)
		case "pause":
			record, err = repo.PauseTargetRun(r.Context(), targetID)
		case "archive":
			record, err = repo.ArchiveTarget(r.Context(), targetID)
		case "restore-to-paused":
			record, err = repo.RestoreArchivedTargetToPaused(r.Context(), targetID)
		default:
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		switch {
		case errors.Is(err, targets.ErrTargetNotFound):
			writeError(w, http.StatusNotFound, "target not found")
			return
		case errors.Is(err, store.ErrInvalidTargetRuntimeTransition):
			writeError(w, http.StatusConflict, "invalid runtime transition")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func monitoringInstanceRuntimeControlAction(path string) (string, string) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	if trimmed == "" {
		return "", ""
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) != 3 || segments[0] == "" || segments[1] != "runtime" || segments[2] == "" {
		return "", ""
	}
	return segments[0], segments[2]
}

func targetRuntimeControlAction(path string) (string, string) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/")
	if trimmed == "" {
		return "", ""
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) != 3 || segments[0] == "" || segments[1] != "runtime" || segments[2] == "" {
		return "", ""
	}
	return segments[0], segments[2]
}
