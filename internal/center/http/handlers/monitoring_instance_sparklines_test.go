package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
)

type fakeMonitoringInstanceSparklinesRepository struct {
	result map[string]map[string][]float64
	err    error
}

func (f *fakeMonitoringInstanceSparklinesRepository) GetMonitoringInstanceSparklines(_ context.Context, _ []string, _ time.Time, _ int) (map[string]map[string][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestMonitoringInstanceSparklinesHandlerBasic(t *testing.T) {
	repo := &fakeMonitoringInstanceSparklinesRepository{
		result: map[string]map[string][]float64{
			"mi_001": {
				"cpu_usage_pct": make24(12.5, 13.0, 14.2),
				"mem_used_pct":  make24(65.0, 64.2, 63.8),
				"disk_used_pct": make24(52.0, 52.1, 52.2),
			},
			"mi_002": {
				"cpu_usage_pct": make24(25.0, 24.5, 26.0),
				"mem_used_pct":  make24(70.0, 71.0, 69.5),
				"disk_used_pct": make24(45.0, 45.1, 44.9),
			},
		},
	}

	handler := handlers.MonitoringInstanceSparklines(repo)
	req := httptest.NewRequest(http.MethodGet,
		"/api/monitoring-instances/sparklines?metrics=cpu_usage_pct,mem_used_pct,disk_used_pct&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body struct {
		MonitoringInstances map[string]map[string][]float64 `json:"monitoring_instances"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(body.MonitoringInstances) != 2 {
		t.Fatalf("expected 2 monitoringInstances, got %d", len(body.MonitoringInstances))
	}

	for _, monitoringInstanceID := range []string{"mi_001", "mi_002"} {
		monitoringInstanceData, ok := body.MonitoringInstances[monitoringInstanceID]
		if !ok {
			t.Fatalf("missing monitoring instance %s in response", monitoringInstanceID)
		}
		for _, metric := range []string{"cpu_usage_pct", "mem_used_pct", "disk_used_pct"} {
			values, ok := monitoringInstanceData[metric]
			if !ok {
				t.Fatalf("monitoringInstance %s missing metric %s", monitoringInstanceID, metric)
			}
			if len(values) != 24 {
				t.Fatalf("monitoringInstance %s metric %s has %d values, want 24", monitoringInstanceID, metric, len(values))
			}
		}
	}
}

func TestMonitoringInstanceSparklinesHandlerEmpty(t *testing.T) {
	repo := &fakeMonitoringInstanceSparklinesRepository{
		result: map[string]map[string][]float64{},
	}

	handler := handlers.MonitoringInstanceSparklines(repo)
	req := httptest.NewRequest(http.MethodGet,
		"/api/monitoring-instances/sparklines?metrics=cpu_usage_pct&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, `"monitoring_instances":`) {
		t.Fatalf("response should contain monitoringInstances key, got: %s", responseBody)
	}
	if !strings.Contains(responseBody, `{}`) && !strings.Contains(responseBody, `"monitoring_instances":{}`) {
		t.Fatalf("response should have empty monitoringInstances object, got: %s", responseBody)
	}
}

func TestMonitoringInstanceSparklinesHandlerInvalidMetrics(t *testing.T) {
	repo := &fakeMonitoringInstanceSparklinesRepository{}

	tests := []struct {
		name    string
		metrics string
		wantErr string
	}{
		{
			name:    "empty metrics",
			metrics: "",
			wantErr: "metrics required",
		},
		{
			name:    "unknown metric",
			metrics: "cpu_usage_pct,invalid_metric",
			wantErr: "unknown metric: invalid_metric",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := handlers.MonitoringInstanceSparklines(repo)
			qs := "/api/monitoring-instances/sparklines?window=24h&downsample=24"
			if tc.metrics != "" {
				qs += "&metrics=" + tc.metrics
			}
			req := httptest.NewRequest(http.MethodGet, qs, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tc.wantErr) {
				t.Fatalf("error message should contain %q, got: %s", tc.wantErr, recorder.Body.String())
			}
		})
	}
}

// make24 creates a 24-element slice with the given values repeated.
func make24(values ...float64) []float64 {
	out := make([]float64, 24)
	for i := range out {
		out[i] = values[i%len(values)]
	}
	return out
}
