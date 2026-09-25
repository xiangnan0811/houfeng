package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
)

type monitoringInstanceRuntimeControlRepository interface {
	SetMonitoringInstanceMonitoringMaintenance(context.Context, string) (monitoringinstances.Record, error)
	PauseMonitoringInstanceMonitoring(context.Context, string, ...monitoringinstances.RuntimeControlInput) (monitoringinstances.Record, error)
	ResumeMonitoringInstanceMonitoring(context.Context, string) (monitoringinstances.Record, error)
}

type targetRuntimeControlRepository interface {
	SetTargetMaintenance(context.Context, string, ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error)
	PauseTargetRun(context.Context, string, ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error)
	ResumeTargetRun(context.Context, string, ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error)
	ArchiveTarget(context.Context, string, ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error)
	RestoreArchivedTargetToPaused(context.Context, string, ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error)
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
			confirmation, ok := decodeMonitoringInstanceRuntimeConfirmation(w, r)
			if !ok {
				return
			}
			record, err = repo.PauseMonitoringInstanceMonitoring(r.Context(), monitoringInstanceID, confirmation)
		default:
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		switch {
		case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		case errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance):
			writeError(w, http.StatusConflict, "archived monitoring instance")
			return
		case errors.Is(err, monitoringinstances.ErrRetiredMonitoringInstance):
			writeError(w, http.StatusConflict, "retired monitoring instance")
			return
		case errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired):
			writeCodedError(w, http.StatusConflict, "shared impact confirmation required", "shared_impact_confirmation_required")
			return
		case errors.Is(err, assetlifecycle.ErrStaleCancellationPreview):
			writeCodedError(w, http.StatusConflict, "monitoring instance management review stale", "management_review_stale")
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

func decodeMonitoringInstanceRuntimeConfirmation(w http.ResponseWriter, r *http.Request) (monitoringinstances.RuntimeControlInput, bool) {
	var input monitoringinstances.RuntimeControlInput
	if r.Body == nil {
		return input, true
	}
	if err := decodeJSON(r, &input); err != nil {
		if errors.Is(err, io.EOF) {
			return input, true
		}
		writeError(w, http.StatusBadRequest, "invalid monitoring instance runtime input")
		return monitoringinstances.RuntimeControlInput{}, false
	}
	return input, true
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
		switch action {
		case "enter-maintenance", "exit-maintenance", "resume", "pause", "archive", "restore-to-paused":
		default:
			writeError(w, http.StatusBadRequest, "invalid target action")
			return
		}

		confirmation, ok := decodeTargetRuntimeConfirmation(w, r)
		if !ok {
			return
		}

		var (
			record targets.TargetRecord
			err    error
		)
		switch action {
		case "enter-maintenance":
			record, err = repo.SetTargetMaintenance(r.Context(), targetID, confirmation)
		case "exit-maintenance", "resume":
			record, err = repo.ResumeTargetRun(r.Context(), targetID, confirmation)
		case "pause":
			record, err = repo.PauseTargetRun(r.Context(), targetID, confirmation)
		case "archive":
			record, err = repo.ArchiveTarget(r.Context(), targetID, confirmation)
		case "restore-to-paused":
			record, err = repo.RestoreArchivedTargetToPaused(r.Context(), targetID, confirmation)
		}

		switch {
		case errors.Is(err, targets.ErrTargetNotFound):
			writeError(w, http.StatusNotFound, "target not found")
			return
		case errors.Is(err, store.ErrInvalidTargetRuntimeAction):
			writeError(w, http.StatusBadRequest, "invalid target action")
			return
		case errors.Is(err, store.ErrInvalidTargetRuntimeTransition):
			writeError(w, http.StatusConflict, "invalid runtime transition")
			return
		case errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired):
			writeCodedError(w, http.StatusConflict, "shared impact confirmation required", "shared_impact_confirmation_required")
			return
		case errors.Is(err, assetlifecycle.ErrStaleCancellationPreview):
			writeCodedError(w, http.StatusConflict, "target management review stale", "management_review_stale")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func decodeTargetRuntimeConfirmation(w http.ResponseWriter, r *http.Request) (assetlinks.GlobalActionConfirmation, bool) {
	var confirmation assetlinks.GlobalActionConfirmation
	if r.Body == nil {
		return confirmation, true
	}
	if err := decodeJSON(r, &confirmation); err != nil {
		if errors.Is(err, io.EOF) {
			return confirmation, true
		}
		writeError(w, http.StatusBadRequest, "invalid target lifecycle action input")
		return assetlinks.GlobalActionConfirmation{}, false
	}
	return confirmation, true
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
