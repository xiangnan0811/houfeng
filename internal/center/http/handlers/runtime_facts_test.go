package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
)

type fakeRuntimeFactsRepository struct {
	getMonitoringInstanceRuntimeFactsResult runtimefacts.MonitoringInstanceRuntimeFacts
	getMonitoringInstanceRuntimeFactsErr    error
	getMonitoringInstanceRuntimeFactsWindow runtimefacts.WindowRequest
	getTargetRuntimeFactsResult             runtimefacts.TargetRuntimeFacts
	getTargetRuntimeFactsErr                error
}

func (f *fakeRuntimeFactsRepository) GetMonitoringInstanceRuntimeFacts(_ context.Context, _ string, window runtimefacts.WindowRequest) (runtimefacts.MonitoringInstanceRuntimeFacts, error) {
	f.getMonitoringInstanceRuntimeFactsWindow = window
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
	if repo.getMonitoringInstanceRuntimeFactsWindow.Key != "24h" || repo.getMonitoringInstanceRuntimeFactsWindow.BucketCount != 288 {
		t.Fatalf("window = %#v, want 24h/288 buckets", repo.getMonitoringInstanceRuntimeFactsWindow)
	}
}

func TestMonitoringInstanceRuntimeFactsWithRealtimeWindow(t *testing.T) {
	now := time.Date(2026, time.April, 24, 1, 2, 3, 0, time.UTC)
	repo := &fakeRuntimeFactsRepository{
		getMonitoringInstanceRuntimeFactsResult: runtimefacts.MonitoringInstanceRuntimeFacts{
			MonitoringInstanceID: "mi_001",
			LatestHostSample: &runtimefacts.HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now,
				ReceivedAt:           now,
				AgentVersion:         "1.0.0",
			},
		},
	}

	handler := handlers.MonitoringInstanceRuntimeFacts(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts?window=realtime", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body=%s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	window := repo.getMonitoringInstanceRuntimeFactsWindow
	if window.Key != "realtime" || window.BucketCount != 720 {
		t.Fatalf("window = %#v, want realtime/720 buckets", window)
	}
	if got := window.EndedAt.Sub(window.StartedAt); got != time.Hour {
		t.Fatalf("window duration = %v, want 1h", got)
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
	if repo.getMonitoringInstanceRuntimeFactsWindow.Key != "7d" || repo.getMonitoringInstanceRuntimeFactsWindow.BucketCount != 336 {
		t.Fatalf("window = %#v, want 7d/336 buckets", repo.getMonitoringInstanceRuntimeFactsWindow)
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
	if repo.getMonitoringInstanceRuntimeFactsWindow.Key != "30d" || repo.getMonitoringInstanceRuntimeFactsWindow.BucketCount != 720 {
		t.Fatalf("window = %#v, want 30d/720 buckets", repo.getMonitoringInstanceRuntimeFactsWindow)
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

func TestMonitoringInstanceRuntimeStreamSendsMatchingHostSamples(t *testing.T) {
	hub := &notifyingHostSampleHub{
		StreamHub:  runtimefacts.NewStreamHub(),
		subscribed: make(chan string, 1),
	}
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001"},
	}
	server := httptest.NewServer(handlers.MonitoringInstanceRuntimeStream(repo, hub))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/monitoring-instances/mi_001/runtime-stream"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{server.URL}},
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	select {
	case monitoringInstanceID := <-hub.subscribed:
		if monitoringInstanceID != "mi_001" {
			t.Fatalf("subscription monitoring instance = %q, want mi_001", monitoringInstanceID)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for runtime stream subscription: %v", ctx.Err())
	}

	if err := hub.AfterSuccessfulSync(ctx, syncingBatchWithHostSample("mi_001", 42), runtimefactsTestResult()); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	var message runtimefacts.HostSampleStreamMessage
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatalf("wsjson.Read() error = %v", err)
	}
	if message.Type != "host_sample" || message.MonitoringInstanceID != "mi_001" {
		t.Fatalf("message = %#v, want mi_001 host sample", message)
	}
	if message.Sample.CPUUsagePct != 42 {
		t.Fatalf("CPUUsagePct = %v, want 42", message.Sample.CPUUsagePct)
	}
}

type notifyingHostSampleHub struct {
	*runtimefacts.StreamHub
	subscribed chan string
}

func (h *notifyingHostSampleHub) SubscribeHostSamples(monitoringInstanceID string) runtimefacts.HostSampleSubscription {
	subscription := h.StreamHub.SubscribeHostSamples(monitoringInstanceID)
	select {
	case h.subscribed <- monitoringInstanceID:
	default:
	}
	return subscription
}

func TestMonitoringInstanceRuntimeStreamMapsMonitoringInstanceNotFound(t *testing.T) {
	handler := handlers.MonitoringInstanceRuntimeStream(
		&fakeMonitoringInstanceRepository{getMonitoringInstanceErr: monitoringinstances.ErrMonitoringInstanceNotFound},
		runtimefacts.NewStreamHub(),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_missing/runtime-stream", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestMonitoringInstanceRuntimeStreamRejectsUnsupportedMethod(t *testing.T) {
	handler := handlers.MonitoringInstanceRuntimeStream(&fakeMonitoringInstanceRepository{}, runtimefacts.NewStreamHub())
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/runtime-stream", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func syncingBatchWithHostSample(monitoringInstanceID string, cpuUsagePct float64) syncing.Batch {
	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	return syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		Observations: observations.BatchWrite{
			HostSamples: []observations.HostSampleWrite{{
				MonitoringInstanceID: monitoringInstanceID,
				ObservedAt:           observedAt,
				ReceivedAt:           observedAt.Add(time.Second),
				AgentVersion:         "agent/v0.1.0",
				CPUUsagePct:          cpuUsagePct,
			}},
		},
	}
}

func runtimefactsTestResult() syncing.Result {
	return syncing.Result{AcceptedAt: time.Date(2026, time.April, 24, 9, 0, 1, 0, time.UTC)}
}
