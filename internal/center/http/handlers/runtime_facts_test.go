package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
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

type testRuntimeSessionValidator struct {
	err error
}

func (v testRuntimeSessionValidator) ValidateSession(context.Context, string) error {
	return v.err
}

func runtimeStreamTestHandler(repo interface {
	GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error)
	GetMonitoringInstanceOnlineEvidence(context.Context, string) (runtimefacts.OnlineEvidenceStreamMessage, error)
}, hub interface {
	SubscribeHostSamples(string) runtimefacts.HostSampleSubscription
}) http.Handler {
	handler := handlers.MonitoringInstanceRuntimeStream(repo, hub, testRuntimeSessionValidator{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := sessionctx.WithSessionID(r.Context(), "runtime-test-session")
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (f *fakeMonitoringInstanceRepository) GetMonitoringInstanceOnlineEvidence(ctx context.Context, id string) (runtimefacts.OnlineEvidenceStreamMessage, error) {
	record, err := f.GetMonitoringInstance(ctx, id)
	return runtimefacts.OnlineEvidenceStreamMessage{
		Type: "online_evidence", MonitoringInstanceID: id,
		LastTrustedOnlineAt: record.LastTrustedOnlineAt, ReadAt: time.Now().UTC(),
	}, err
}

func readInitialOnlineEvidence(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	var message runtimefacts.OnlineEvidenceStreamMessage
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatalf("read initial online evidence: %v", err)
	}
	if message.Type != "online_evidence" || message.ReadAt.IsZero() {
		t.Fatalf("initial message = %#v, want online evidence", message)
	}
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
	epoch := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	hub := &notifyingHostSampleHub{
		StreamHub:  runtimefacts.NewStreamHub(),
		subscribed: make(chan string, 1),
	}
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID:  "mi_001",
			BindingStatus:         monitoringinstances.BindingBound,
			BindingFingerprint:    "fp-001",
			BindingEpochStartedAt: &epoch,
		},
	}
	server := httptest.NewServer(runtimeStreamTestHandler(repo, hub))
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
	readInitialOnlineEvidence(t, ctx, conn)
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

func TestMonitoringInstanceRuntimeStreamWritesAgentShapedSamplesWithZeroReceivedAt(t *testing.T) {
	epoch := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	hub := &notifyingHostSampleHub{
		StreamHub:  runtimefacts.NewStreamHub(),
		subscribed: make(chan string, 1),
	}
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID:  "mi_001",
			BindingStatus:         monitoringinstances.BindingBound,
			BindingFingerprint:    "fp-001",
			BindingEpochStartedAt: &epoch,
		},
	}
	server := httptest.NewServer(runtimeStreamTestHandler(repo, hub))
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

	batch := agentShapedSyncingBatchWithHostSample("mi_001", 17)
	result := runtimefactsTestResult()
	if !batch.Observations.HostSamples[0].ReceivedAt.IsZero() {
		t.Fatal("agent-shaped fixture must leave ReceivedAt zero")
	}
	if err := hub.AfterSuccessfulSync(ctx, batch, result); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	var message runtimefacts.HostSampleStreamMessage
	readInitialOnlineEvidence(t, ctx, conn)
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatalf("wsjson.Read() error = %v", err)
	}
	if message.Type != "host_sample" || message.MonitoringInstanceID != "mi_001" {
		t.Fatalf("message = %#v, want mi_001 host sample", message)
	}
	if message.Sample.CPUUsagePct != 17 {
		t.Fatalf("CPUUsagePct = %v, want 17", message.Sample.CPUUsagePct)
	}
	if !message.Sample.ReceivedAt.Equal(result.AcceptedAt) || !message.ReceivedAt.Equal(result.AcceptedAt) {
		t.Fatalf("received timestamps = sample=%v message=%v, want AcceptedAt %v", message.Sample.ReceivedAt, message.ReceivedAt, result.AcceptedAt)
	}
	if !message.Sample.ObservedAt.Equal(batch.Observations.HostSamples[0].ObservedAt) {
		t.Fatalf("ObservedAt = %v, want agent observed_at", message.Sample.ObservedAt)
	}
}

func TestMonitoringInstanceRuntimeStreamDropsStaleAndFutureSamplesBeforeWriting(t *testing.T) {
	now := time.Now().UTC()
	epoch := now.Add(-time.Hour)
	hub := &notifyingHostSampleHub{
		StreamHub:  runtimefacts.NewStreamHub(),
		subscribed: make(chan string, 1),
	}
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID:  "mi_001",
			BindingStatus:         monitoringinstances.BindingBound,
			BindingFingerprint:    "fp-current",
			BindingEpochStartedAt: &epoch,
		},
	}
	server := httptest.NewServer(runtimeStreamTestHandler(repo, hub))
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
	case <-hub.subscribed:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for runtime stream subscription: %v", ctx.Err())
	}

	networkRatesValid := false
	base := now.Add(-5 * time.Minute)
	batch := syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{
			{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           base,
				ReceivedAt:           base.Add(time.Second),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-old",
				SyncBatchID:          "wrong-fingerprint",
			},
			{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           now.Add(time.Hour),
				ReceivedAt:           now.Add(time.Hour),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-current",
				SyncBatchID:          "future",
			},
			{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           epoch.Add(-time.Minute),
				ReceivedAt:           epoch.Add(-time.Second),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-current",
				SyncBatchID:          "pre-epoch",
			},
			{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           base,
				ReceivedAt:           base.Add(time.Second),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-current",
				SyncBatchID:          "current-backfill",
				IsBackfilled:         true,
				NetworkRatesValid:    &networkRatesValid,
			},
		}},
	}
	if err := hub.AfterSuccessfulSync(ctx, batch, syncing.Result{Disposition: syncing.ResultDispositionRecorded}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	var message runtimefacts.HostSampleStreamMessage
	readInitialOnlineEvidence(t, ctx, conn)
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatalf("wsjson.Read() error = %v", err)
	}
	if message.Sample.SyncBatchID != "current-backfill" || !message.Sample.IsBackfilled {
		t.Fatalf("message = %#v, want only eligible current-binding backfill", message)
	}
	if !message.Sample.ObservedAt.Equal(base) || !message.Sample.ReceivedAt.Equal(base.Add(time.Second)) ||
		!message.ReceivedAt.Equal(base.Add(time.Second)) {
		t.Fatalf("message timestamps = %#v, want source observed/received timestamps", message)
	}
	if message.Sample.NetworkRatesValid == nil || *message.Sample.NetworkRatesValid {
		t.Fatalf("NetworkRatesValid = %v, want explicit false", message.Sample.NetworkRatesValid)
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
	handler := runtimeStreamTestHandler(
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

func TestMonitoringInstanceRuntimeStreamRequiresTrustedSessionContext(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}
	handler := handlers.MonitoringInstanceRuntimeStream(repo, runtimefacts.NewStreamHub(), testRuntimeSessionValidator{})
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-stream", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if repo.getMonitoringInstanceCalls != 0 {
		t.Fatalf("instance lookup calls = %d, want 0 before authentication", repo.getMonitoringInstanceCalls)
	}
}

func TestMonitoringInstanceRuntimeStreamRejectsValidatorConfigurationAndFailures(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-stream", nil)
	req = req.WithContext(sessionctx.WithSessionID(req.Context(), "runtime-test-session"))

	t.Run("missing validator", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handlers.MonitoringInstanceRuntimeStream(repo, runtimefacts.NewStreamHub(), nil).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})

	t.Run("validator failure", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handlers.MonitoringInstanceRuntimeStream(repo, runtimefacts.NewStreamHub(), testRuntimeSessionValidator{err: errors.New("validator unavailable")}).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})
}

func TestMonitoringInstanceRuntimeStreamRejectsUnsupportedMethod(t *testing.T) {
	handler := handlers.MonitoringInstanceRuntimeStream(&fakeMonitoringInstanceRepository{}, runtimefacts.NewStreamHub(), testRuntimeSessionValidator{})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/runtime-stream", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func syncingBatchWithHostSample(monitoringInstanceID string, cpuUsagePct float64) syncing.Batch {
	batch := agentShapedSyncingBatchWithHostSample(monitoringInstanceID, cpuUsagePct)
	batch.Observations.HostSamples[0].ReceivedAt = batch.Observations.HostSamples[0].ObservedAt.Add(time.Second)
	return batch
}

func agentShapedSyncingBatchWithHostSample(monitoringInstanceID string, cpuUsagePct float64) syncing.Batch {
	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	return syncing.Batch{
		MonitoringInstanceID: monitoringInstanceID,
		Observations: observations.BatchWrite{
			HostSamples: []observations.HostSampleWrite{{
				MonitoringInstanceID: monitoringInstanceID,
				ObservedAt:           observedAt,
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-001",
				SyncBatchID:          "sync-001",
				CPUUsagePct:          cpuUsagePct,
			}},
		},
	}
}

func runtimefactsTestResult() syncing.Result {
	return syncing.Result{Disposition: syncing.ResultDispositionRecorded, AcceptedAt: time.Date(2026, time.April, 24, 9, 0, 1, 0, time.UTC)}
}

type onlineEvidenceTestRepository struct {
	readCount atomic.Int32
	failure   error
}

func (r *onlineEvidenceTestRepository) GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error) {
	return monitoringinstances.Record{MonitoringStatus: "暂停"}, nil
}

func (r *onlineEvidenceTestRepository) GetMonitoringInstanceOnlineEvidence(_ context.Context, id string) (runtimefacts.OnlineEvidenceStreamMessage, error) {
	n := r.readCount.Add(1)
	if n > 1 && r.failure != nil {
		return runtimefacts.OnlineEvidenceStreamMessage{}, r.failure
	}
	readAt := time.Date(2026, 10, 13, 1, 0, int(n)*5, 0, time.UTC)
	var onlineAt *time.Time
	if n > 1 {
		onlineAt = &readAt
	}
	return runtimefacts.OnlineEvidenceStreamMessage{
		Type: "online_evidence", MonitoringInstanceID: id, LastTrustedOnlineAt: onlineAt, ReadAt: readAt,
	}, nil
}

func TestMonitoringInstanceRuntimeStreamOnlineEvidenceWithoutSamples(t *testing.T) {
	t.Parallel()
	repo := &onlineEvidenceTestRepository{}
	hub := &notifyingHostSampleHub{StreamHub: runtimefacts.NewStreamHub(), subscribed: make(chan string, 1)}
	server := httptest.NewServer(runtimeStreamTestHandler(repo, hub))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/monitoring-instances/mi_001/runtime-stream?include_samples=false", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var initial map[string]json.RawMessage
	if err := wsjson.Read(ctx, conn, &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial) != 4 || string(initial["last_trusted_online_at"]) != "null" || string(initial["type"]) != `"online_evidence"` {
		t.Fatalf("initial evidence = %s, want exactly four fields including null trusted timestamp", initial)
	}
	if err := hub.AfterSuccessfulSync(ctx, syncingBatchWithHostSample("mi_001", 42), runtimefactsTestResult()); err != nil {
		t.Fatal(err)
	}
	var next runtimefacts.OnlineEvidenceStreamMessage
	if err := wsjson.Read(ctx, conn, &next); err != nil {
		t.Fatal(err)
	}
	if next.Type != "online_evidence" || next.MonitoringInstanceID != "mi_001" || next.LastTrustedOnlineAt == nil || !next.LastTrustedOnlineAt.Equal(next.ReadAt) {
		t.Fatalf("next = %#v, want refreshed evidence independently of paused/unbound collection", next)
	}
	select {
	case id := <-hub.subscribed:
		t.Fatalf("evidence-only stream subscribed to samples for %s", id)
	default:
	}
}

func TestMonitoringInstanceRuntimeStreamClosesWhenEvidenceReadFails(t *testing.T) {
	for _, failure := range []error{errors.New("database unavailable"), monitoringinstances.ErrMonitoringInstanceNotFound} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(runtimeStreamTestHandler(&onlineEvidenceTestRepository{failure: failure}, runtimefacts.NewStreamHub()))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/monitoring-instances/mi_001/runtime-stream?include_samples=false", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			readInitialOnlineEvidence(t, ctx, conn)
			var next runtimefacts.OnlineEvidenceStreamMessage
			if err := wsjson.Read(ctx, conn, &next); err == nil || ctx.Err() != nil {
				t.Fatalf("read after authority error = %v; ctx=%v, want prompt connection close", err, ctx.Err())
			}
		})
	}
}

func TestMonitoringInstanceRuntimeStreamRejectsInvalidIncludeSamples(t *testing.T) {
	for _, query := range []string{"include_samples=", "include_samples=0", "include_samples=false&include_samples=true"} {
		recorder := httptest.NewRecorder()
		runtimeStreamTestHandler(&fakeMonitoringInstanceRepository{}, runtimefacts.NewStreamHub()).ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-stream?"+query, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d, want 400", query, recorder.Code)
		}
	}
}

type expiringRuntimeSessionValidator struct {
	expired atomic.Bool
}

func (v *expiringRuntimeSessionValidator) ValidateSession(context.Context, string) error {
	if v.expired.Load() {
		return auth.ErrSessionExpired
	}
	return nil
}

func TestMonitoringInstanceRuntimeStreamClosesExpiredEvidenceOnlySession(t *testing.T) {
	t.Parallel()
	validator := &expiringRuntimeSessionValidator{}
	handler := handlers.MonitoringInstanceRuntimeStream(&onlineEvidenceTestRepository{}, runtimefacts.NewStreamHub(), validator)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(sessionctx.WithSessionID(r.Context(), "expiring-session")))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/monitoring-instances/mi_001/runtime-stream?include_samples=false", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	readInitialOnlineEvidence(t, ctx, conn)
	validator.expired.Store(true)
	var next runtimefacts.OnlineEvidenceStreamMessage
	if err := wsjson.Read(ctx, conn, &next); err == nil || ctx.Err() != nil {
		t.Fatalf("read after session expired = %v; ctx=%v, want prompt close without another evidence message", err, ctx.Err())
	}
}
