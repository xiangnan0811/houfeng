package handlers

import (
	"net/http"
	"time"

	"houfeng/internal/center/store"
)

// MonitoringInstanceRuntimeSummaries returns an HTTP handler for GET
// /api/monitoring-instances/runtime-summaries.
func MonitoringInstanceRuntimeSummaries(repo store.MonitoringInstanceRuntimeSummariesRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		result, err := repo.GetMonitoringInstanceRuntimeSummaries(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if result.MonitoringInstances == nil {
			result.MonitoringInstances = map[string]*store.MonitoringInstanceRuntimeSummary{}
		}
		writeJSON(w, http.StatusOK, monitoringInstanceRuntimeSummariesResponse{
			ReadAt:              result.ReadAt,
			MonitoringInstances: result.MonitoringInstances,
		})
	})
}

type monitoringInstanceRuntimeSummariesResponse struct {
	ReadAt              time.Time                                          `json:"read_at"`
	MonitoringInstances map[string]*store.MonitoringInstanceRuntimeSummary `json:"monitoring_instances"`
}
