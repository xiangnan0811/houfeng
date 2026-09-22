package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
)

func TestPostgresIntegrationMonitoringInstanceRuntimeFactsAndStreamEligibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	db := openTemporaryMonitoringSparklinesDatabase(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	epoch := now.Add(-2 * time.Hour)
	insertRuntimeSummaryMonitoringInstance(t, ctx, db, "mi_runtime_facts", "fp-current", epoch, false)
	start := now.Add(-time.Hour)
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", now.Add(-3*time.Minute), now.Add(-3*time.Minute), "fp-old", 99, 11, 11, true, false, "old-identity")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", now.Add(-4*time.Minute), epoch.Add(-time.Second), "fp-current", 88, 12, 12, true, false, "old-epoch")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", now.Add(time.Minute), now.Add(time.Minute), "fp-current", 77, 13, 13, true, false, "future")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", start.Add(time.Minute), start.Add(time.Minute), "fp-current", 1, 10, 20, false, false, "bucket-invalid")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", start.Add(31*time.Minute), start.Add(31*time.Minute), "fp-current", 2, 0, 0, true, false, "bucket-true-zero")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", now.Add(-5*time.Minute), now.Add(-5*time.Minute), "fp-current", 50, 30, 40, true, true, "latest-backfill")
	insertRuntimeHostSample(t, ctx, db, "mi_runtime_facts", now.Add(-5*time.Minute), now.Add(-5*time.Minute), "fp-current", 60, 50, 60, true, false, "latest-live")

	repository := store.NewPostgresRuntimeFactsRepository(db)
	facts, err := repository.GetMonitoringInstanceRuntimeFacts(ctx, "mi_runtime_facts", runtimefacts.WindowRequest{
		Key:         "facts-test",
		StartedAt:   start,
		EndedAt:     now,
		BucketCount: 4,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts: %v", err)
	}
	if facts.ReadAt.IsZero() {
		t.Fatal("ReadAt is zero, want server snapshot timestamp")
	}
	if facts.LatestHostSample == nil || facts.LatestHostSample.UptimeSeconds != 60 || facts.LatestHostSample.IsBackfilled {
		t.Fatalf("latest host sample = %#v, want current live tie winner", facts.LatestHostSample)
	}
	if facts.Window.SampleCount != 6 {
		t.Fatalf("window sample count = %d, want all in-window raw observations", facts.Window.SampleCount)
	}
	if len(facts.HostMetricPoints) != 4 {
		t.Fatalf("len(host metric points) = %d, want full bucket count", len(facts.HostMetricPoints))
	}
	if facts.HostMetricPoints[0].SampleCount != 1 || facts.HostMetricPoints[0].NetInBytesPerSec != nil || facts.HostMetricPoints[0].NetOutBytesPerSec != nil {
		t.Fatalf("invalid network bucket = %#v, want null network averages", facts.HostMetricPoints[0])
	}
	if facts.HostMetricPoints[1].SampleCount != 0 || facts.HostMetricPoints[1].CPUUsagePct != nil || facts.HostMetricPoints[1].NetInBytesPerSec != nil {
		t.Fatalf("gap bucket = %#v, want count 0 and null metrics", facts.HostMetricPoints[1])
	}
	if facts.HostMetricPoints[2].SampleCount != 1 || facts.HostMetricPoints[2].NetInBytesPerSec == nil || *facts.HostMetricPoints[2].NetInBytesPerSec != 0 {
		t.Fatalf("true-zero network bucket = %#v, want retained zero", facts.HostMetricPoints[2])
	}

	handler := MonitoringInstanceRuntimeFacts(repository)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_runtime_facts/runtime-facts?window=24h", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("runtime facts HTTP status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response runtimefacts.MonitoringInstanceRuntimeFacts
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode runtime facts response: %v", err)
	}
	if response.ReadAt.IsZero() {
		t.Fatal("runtime facts response read_at is zero")
	}

	streamHub := &runtimeFactsIntegrationHostSampleHub{
		StreamHub:  runtimefacts.NewStreamHub(),
		subscribed: make(chan string, 1),
	}
	streamServer := httptest.NewServer(MonitoringInstanceRuntimeStream(
		store.NewPostgresMonitoringInstanceRepository(db),
		streamHub,
	))
	defer streamServer.Close()
	streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer streamCancel()
	wsURL := "ws" + strings.TrimPrefix(streamServer.URL, "http") + "/api/monitoring-instances/mi_runtime_facts/runtime-stream"
	conn, _, err := websocket.Dial(streamCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{streamServer.URL}},
	})
	if err != nil {
		t.Fatalf("stream Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	select {
	case got := <-streamHub.subscribed:
		if got != "mi_runtime_facts" {
			t.Fatalf("stream subscription = %q, want monitoring instance", got)
		}
	case <-streamCtx.Done():
		t.Fatalf("waiting for stream subscription: %v", streamCtx.Err())
	}

	streamObservedAt := now.Add(-time.Minute)
	streamReceivedAt := streamObservedAt.Add(time.Second)
	marker := false
	if err := streamHub.AfterSuccessfulSync(streamCtx, syncing.Batch{
		MonitoringInstanceID: "mi_runtime_facts",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{
			{
				MonitoringInstanceID: "mi_runtime_facts",
				ObservedAt:           streamObservedAt,
				ReceivedAt:           streamReceivedAt,
				AgentVersion:         "agent/v1",
				Fingerprint:          "fp-old",
				SyncBatchID:          "stream-wrong-binding",
			},
			{
				MonitoringInstanceID: "mi_runtime_facts",
				ObservedAt:           streamObservedAt,
				ReceivedAt:           streamReceivedAt,
				AgentVersion:         "agent/v1",
				Fingerprint:          "fp-current",
				SyncBatchID:          "stream-current-backfill",
				IsBackfilled:         true,
				NetworkRatesValid:    &marker,
			},
		}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded}); err != nil {
		t.Fatalf("publish stream samples: %v", err)
	}

	var message runtimefacts.HostSampleStreamMessage
	if err := wsjson.Read(streamCtx, conn, &message); err != nil {
		t.Fatalf("read stream message: %v", err)
	}
	if message.Sample.SyncBatchID != "stream-current-backfill" || !message.Sample.IsBackfilled {
		t.Fatalf("stream message = %#v, want only current-binding sample", message)
	}
	if !message.Sample.ObservedAt.Equal(streamObservedAt) || !message.Sample.ReceivedAt.Equal(streamReceivedAt) || !message.ReceivedAt.Equal(streamReceivedAt) {
		t.Fatalf("stream timestamps = %#v, want source timestamps", message)
	}
	if message.Sample.NetworkRatesValid == nil || *message.Sample.NetworkRatesValid {
		t.Fatalf("stream NetworkRatesValid = %v, want explicit false", message.Sample.NetworkRatesValid)
	}
}

type runtimeFactsIntegrationHostSampleHub struct {
	*runtimefacts.StreamHub
	subscribed chan string
}

func (h *runtimeFactsIntegrationHostSampleHub) SubscribeHostSamples(monitoringInstanceID string) runtimefacts.HostSampleSubscription {
	subscription := h.StreamHub.SubscribeHostSamples(monitoringInstanceID)
	select {
	case h.subscribed <- monitoringInstanceID:
	default:
	}
	return subscription
}
