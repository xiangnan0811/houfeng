package handlers

import (
	"errors"
	"houfeng/internal/center/monitoringinstances"
	"net/http"
)

func MonitoringInstancePhases(repo monitoringinstances.PhaseRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "phases")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		phases, err := repo.ListMonitoringInstancePhases(r.Context(), id)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, phases)
	})
}
