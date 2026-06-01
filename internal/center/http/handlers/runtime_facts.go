package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

func MonitoringInstanceRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		window, limit, err := parseWindow(r.URL.Query().Get("window"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window: "+err.Error())
			return
		}
		since := time.Now().Add(-window)

		record, err := repo.GetMonitoringInstanceRuntimeFacts(r.Context(), monitoringInstanceID, since, limit)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func TargetRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		targetID, ok := targetRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		window, limit, err := parseWindow(r.URL.Query().Get("window"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window: "+err.Error())
			return
		}
		since := time.Now().Add(-window)

		record, err := repo.GetTargetRuntimeFacts(r.Context(), targetID, since, limit)
		if errors.Is(err, targets.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func monitoringInstanceRuntimeFactsPath(path string) (monitoringInstanceID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}

func targetRuntimeFactsPath(path string) (targetID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}
