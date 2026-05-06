package handlers

import (
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/store"
)

// TargetSparklines returns an HTTP handler for GET /api/targets/sparklines.
func TargetSparklines(repo store.TargetSparklinesRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		q := r.URL.Query()

		rawMetrics := strings.TrimSpace(q.Get("metrics"))
		if rawMetrics == "" {
			writeError(w, http.StatusBadRequest, "metrics required")
			return
		}

		metrics, err := parseTargetMetricList(rawMetrics)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		window, _, err := parseWindow(q.Get("window"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window: "+err.Error())
			return
		}

		downsample, err := parseDownsample(q.Get("downsample"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid downsample: "+err.Error())
			return
		}

		since := time.Now().Add(-window)

		result, err := repo.GetTargetSparklines(r.Context(), metrics, since, downsample)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Ensure non-nil map for JSON "targets": {}
		if result == nil {
			result = map[string]map[string][]float64{}
		}

		writeJSON(w, http.StatusOK, targetSparklinesResponse{Targets: result})
	})
}

type targetSparklinesResponse struct {
	Targets map[string]map[string][]float64 `json:"targets"`
}

// parseTargetMetricList splits a comma-separated metrics string and validates
// each metric name against the known target sparkline whitelist.
func parseTargetMetricList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return nil, errMetric("empty metrics list")
	}

	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		if !store.ValidTargetSparklineMetrics[name] {
			return nil, errMetric("unknown metric: " + name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}

	if len(out) == 0 {
		return nil, errMetric("metrics required")
	}

	return out, nil
}
