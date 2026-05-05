package handlers

import (
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/store"
)

// NodeSparklines returns an HTTP handler for GET /api/nodes/sparklines.
func NodeSparklines(repo store.NodeSparklinesRepository) http.Handler {
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

		window, err := parseWindow(q.Get("window"))
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

		result, err := repo.GetNodeSparklines(r.Context(), metrics, since, downsample)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Ensure non-nil map for JSON "nodes": {}
		if result == nil {
			result = map[string]map[string][]float64{}
		}

		writeJSON(w, http.StatusOK, sparklinesResponse{Nodes: result})
	})
}

type sparklinesResponse struct {
	Nodes map[string]map[string][]float64 `json:"nodes"`
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

func parseWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errMetric("window must be positive")
	}
	return d, nil
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
