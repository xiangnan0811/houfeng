package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

type fakeRuntimeFactsRepository struct {
	getMonitoringInstanceRuntimeFactsResult runtimefacts.MonitoringInstanceRuntimeFacts
	getMonitoringInstanceRuntimeFactsErr    error
	getTargetRuntimeFactsResult             runtimefacts.TargetRuntimeFacts
	getTargetRuntimeFactsErr                error
}

func (f *fakeRuntimeFactsRepository) GetMonitoringInstanceRuntimeFacts(_ context.Context, _ string, _ time.Time, _ int) (runtimefacts.MonitoringInstanceRuntimeFacts, error) {
	if f.getMonitoringInstanceRuntimeFactsErr != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, f.getMonitoringInstanceRuntimeFactsErr
	}
	return f.getMonitoringInstanceRuntimeFactsResult, nil
}

func (f *fakeRuntimeFactsRepository) GetTargetRuntimeFacts(context.Context, string, time.Time, int) (runtimefacts.TargetRuntimeFacts, error) {
	if f.getTargetRuntimeFactsErr != nil {
		return runtimefacts.TargetRuntimeFacts{}, f.getTargetRuntimeFactsErr
	}
	return f.getTargetRuntimeFactsResult, nil
}

func TestMonitoringInstanceRuntimeFactsReturnsLatestHostSample(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	repo := &fakeRuntimeFactsRepository{
		getMonitoringInstanceRuntimeFactsResult: runtimefacts.MonitoringInstanceRuntimeFacts{
			MonitoringInstanceID: "mi_001",
			LatestHostSample: &runtimefacts.HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
				MemTotalBytes:        8589934592,
				DiskTotalBytes:       107374182400,
			},
		},
	}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body runtimefacts.MonitoringInstanceRuntimeFacts
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.MonitoringInstanceID != "mi_001" {
		t.Fatalf("expected monitoring_instance_id %q, got %q", "mi_001", body.MonitoringInstanceID)
	}
	if body.LatestHostSample == nil || body.LatestHostSample.AgentVersion != "1.0.0" {
		t.Fatalf("expected latest host sample, got %#v", body.LatestHostSample)
	}
	if body.LatestHostSample.MemTotalBytes != 8589934592 {
		t.Fatalf("mem_total_bytes = %d, want %d", body.LatestHostSample.MemTotalBytes, int64(8589934592))
	}
	if body.LatestHostSample.DiskTotalBytes != 107374182400 {
		t.Fatalf("disk_total_bytes = %d, want %d", body.LatestHostSample.DiskTotalBytes, int64(107374182400))
	}
}

func TestMonitoringInstanceRuntimeFactsMapsMonitoringInstanceNotFound(t *testing.T) {
	repo := &fakeRuntimeFactsRepository{getMonitoringInstanceRuntimeFactsErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_missing/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if !errors.Is(repo.getMonitoringInstanceRuntimeFactsErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("expected fake repo error to match ErrMonitoringInstanceNotFound")
	}
	if body["error"] != "monitoring instance not found" {
		t.Fatalf("expected error %q, got %q", "monitoring instance not found", body["error"])
	}
}

func TestMonitoringInstanceRuntimeFactsRejectsDeeperPaths(t *testing.T) {
	repo := &fakeRuntimeFactsRepository{}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts/extra", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestTargetRuntimeFactsReturnsLatestProbeObservations(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	latency := 123
	httpStatus := 204
	repo := &fakeRuntimeFactsRepository{
		getTargetRuntimeFactsResult: runtimefacts.TargetRuntimeFacts{
			TargetID: "tg_001",
			LatestProbeObservations: []runtimefacts.ProbeObservation{{
				MonitoringInstanceID: "mi_001",
				TargetID:             "tg_001",
				ProbeItemID:          "pb_001",
				ProbeKind:            "http",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
				ResultKind:           "success",
				LatencyMS:            &latency,
				HTTPStatus:           &httpStatus,
			}},
		},
	}

	handler := handlers.TargetRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body runtimefacts.TargetRuntimeFacts
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.TargetID != "tg_001" {
		t.Fatalf("expected target_id %q, got %q", "tg_001", body.TargetID)
	}
	if len(body.LatestProbeObservations) != 1 || body.LatestProbeObservations[0].ProbeItemID != "pb_001" {
		t.Fatalf("expected latest probe observations, got %#v", body.LatestProbeObservations)
	}
}

func TestTargetRuntimeFactsMapsTargetNotFound(t *testing.T) {
	repo := &fakeRuntimeFactsRepository{getTargetRuntimeFactsErr: targets.ErrTargetNotFound}

	handler := handlers.TargetRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_missing/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if !errors.Is(repo.getTargetRuntimeFactsErr, targets.ErrTargetNotFound) {
		t.Fatalf("expected fake repo error to match ErrTargetNotFound")
	}
	if body["error"] != "target not found" {
		t.Fatalf("expected error %q, got %q", "target not found", body["error"])
	}
}

func TestTargetRuntimeFactsRejectsDeeperPaths(t *testing.T) {
	repo := &fakeRuntimeFactsRepository{}

	handler := handlers.TargetRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/runtime-facts/extra", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestMonitoringInstanceRuntimeFactsDefaultWindowIs24h(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	repo := &fakeRuntimeFactsRepository{
		getMonitoringInstanceRuntimeFactsResult: runtimefacts.MonitoringInstanceRuntimeFacts{
			MonitoringInstanceID: "mi_001",
			LatestHostSample: &runtimefacts.HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
				MemTotalBytes:        8589934592,
				DiskTotalBytes:       107374182400,
			},
		},
	}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	// No window query param — should default to 24h.
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body=%s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringInstanceRuntimeFactsWith7dWindow(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	repo := &fakeRuntimeFactsRepository{
		getMonitoringInstanceRuntimeFactsResult: runtimefacts.MonitoringInstanceRuntimeFacts{
			MonitoringInstanceID: "mi_001",
			LatestHostSample: &runtimefacts.HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
				MemTotalBytes:        8589934592,
				DiskTotalBytes:       107374182400,
			},
		},
	}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts?window=7d", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body=%s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringInstanceRuntimeFactsWith30dWindow(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	repo := &fakeRuntimeFactsRepository{
		getMonitoringInstanceRuntimeFactsResult: runtimefacts.MonitoringInstanceRuntimeFacts{
			MonitoringInstanceID: "mi_001",
			LatestHostSample: &runtimefacts.HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
				MemTotalBytes:        8589934592,
				DiskTotalBytes:       107374182400,
			},
		},
	}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts?window=30d", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body=%s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringInstanceRuntimeFactsRejectsInvalidWindow(t *testing.T) {
	repo := &fakeRuntimeFactsRepository{}

	tests := []struct {
		name   string
		window string
	}{
		{name: "arbitrary duration", window: "1h"},
		{name: "unsupported duration", window: "5d"},
		{name: "empty but parseable string", window: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.MonitoringInstanceRuntimeFacts(repo)
			req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts?window="+tt.window, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d; body=%s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
		})
	}
}
