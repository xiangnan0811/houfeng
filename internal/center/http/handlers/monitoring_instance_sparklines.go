package handlers

import (
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/store"
)

// MonitoringInstanceSparklines returns an HTTP handler for GET /api/monitoring-instances/sparklines.
func MonitoringInstanceSparklines(repo store.MonitoringInstanceSparklinesRepository) http.Handler {
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

		metrics, err := parseMetricList(rawMetrics)
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

		result, err := repo.GetMonitoringInstanceSparklines(r.Context(), metrics, since, downsample)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Ensure non-nil map for JSON "monitoring_instances": {}
		if result == nil {
			result = map[string]map[string][]float64{}
		}

		writeJSON(w, http.StatusOK, sparklinesResponse{MonitoringInstances: result})
	})
}

type sparklinesResponse struct {
	MonitoringInstances map[string]map[string][]float64 `json:"monitoring_instances"`
}

// parseMetricList splits a comma-separated metrics string and validates each
// metric name against the known whitelist.
func parseMetricList(raw string) ([]string, error) {
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
		if !store.ValidSparklineMetrics[name] {
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

// parseWindow parses a window query parameter into a duration and a
// recommended row limit for the runtime-facts endpoint.  Accepted values
// are "24h", "7d", and "30d" (case-sensitive).  An empty string defaults
// to "24h" for backward compatibility.
func parseWindow(raw string) (time.Duration, int, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "", "24h":
		return 24 * time.Hour, 288, nil
	case "7d":
		return 7 * 24 * time.Hour, 2016, nil
	case "30d":
		return 30 * 24 * time.Hour, 8640, nil
	default:
		return 0, 0, errMetric("invalid window: must be 24h, 7d, or 30d")
	}
}

func parseDownsample(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 24, nil
	}
	var n int
	// Simple int parsing without strconv to keep it lightweight.
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, errMetric("downsample must be a positive integer")
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return 0, errMetric("downsample must be a positive integer")
	}
	return n, nil
}

type metricError struct {
	msg string
}

func errMetric(msg string) error {
	return &metricError{msg: msg}
}

func (e *metricError) Error() string {
	return e.msg
}
