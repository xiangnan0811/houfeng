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

type fakeTargetSparklinesRepository struct {
	result map[string]map[string][]float64
	err    error
}

func (f *fakeTargetSparklinesRepository) GetTargetSparklines(_ context.Context, _ []string, _ time.Time, _ int) (map[string]map[string][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestTargetSparklinesHandlerBasic(t *testing.T) {
	repo := &fakeTargetSparklinesRepository{
		result: map[string]map[string][]float64{
			"tg_001": {
				"latency": make24Latency(12.5, 13.0, 14.2),
			},
			"tg_002": {
				"latency": make24Latency(25.0, 24.5, 26.0),
			},
		},
	}

	handler := handlers.TargetSparklines(repo)
	req := httptest.NewRequest(http.MethodGet,
		"/api/targets/sparklines?metrics=latency&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body struct {
		Targets map[string]map[string][]float64 `json:"targets"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(body.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(body.Targets))
	}

	for _, targetID := range []string{"tg_001", "tg_002"} {
		targetData, ok := body.Targets[targetID]
		if !ok {
			t.Fatalf("missing target %s in response", targetID)
		}
		values, ok := targetData["latency"]
		if !ok {
			t.Fatalf("target %s missing metric latency", targetID)
		}
		if len(values) != 24 {
			t.Fatalf("target %s metric latency has %d values, want 24", targetID, len(values))
		}
	}
}

func TestTargetSparklinesHandlerEmpty(t *testing.T) {
	repo := &fakeTargetSparklinesRepository{
		result: map[string]map[string][]float64{},
	}

	handler := handlers.TargetSparklines(repo)
	req := httptest.NewRequest(http.MethodGet,
		"/api/targets/sparklines?metrics=latency&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, `"targets":`) {
		t.Fatalf("response should contain targets key, got: %s", responseBody)
	}
	if !strings.Contains(responseBody, `{}`) && !strings.Contains(responseBody, `"targets":{}`) {
		t.Fatalf("response should have empty targets object, got: %s", responseBody)
	}
}

func TestTargetSparklinesHandlerInvalidMetrics(t *testing.T) {
	repo := &fakeTargetSparklinesRepository{}

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
			metrics: "latency,invalid_metric",
			wantErr: "unknown metric: invalid_metric",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := handlers.TargetSparklines(repo)
			qs := "/api/targets/sparklines?window=24h&downsample=24"
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

// make24Latency creates a 24-element slice with the given values repeated.
func make24Latency(values ...float64) []float64 {
	out := make([]float64, 24)
	for i := range out {
		out[i] = values[i%len(values)]
	}
	return out
}
