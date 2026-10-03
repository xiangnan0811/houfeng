package ipquality_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

type memoryStateStore struct {
	state     agentipquality.State
	loadErr   error
	saveErr   error
	saveCalls int
}

func (s *memoryStateStore) Load(context.Context) (agentipquality.State, error) {
	if s.loadErr != nil {
		return agentipquality.State{}, s.loadErr
	}
	return s.state, nil
}

func (s *memoryStateStore) Save(_ context.Context, state agentipquality.State) error {
	s.saveCalls++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.state = state
	return nil
}

type channelCollector struct {
	mu     sync.Mutex
	calls  int
	report agentapi.IPQualityReportPayload
	wait   chan struct{}
}

func (c *channelCollector) Collect(context.Context, *agentapi.IPQualityPlan, time.Time) agentapi.IPQualityReportPayload {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.wait != nil {
		<-c.wait
	}
	return c.report
}

func (c *channelCollector) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestManagerStartsDueCollectionAndDrainsReport(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	store := &memoryStateStore{}
	collector := &channelCollector{
		report: agentapi.IPQualityReportPayload{
			ObservedAt: now,
			IPAddress:  "203.0.113.10",
			IPVersion:  4,
			Status:     agentapi.IPQualityStatusSuccess,
		},
	}
	manager := agentipquality.NewManager(store, collector)

	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, now); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}

	deadline := time.After(time.Second)
	for {
		reports := manager.DrainReports()
		if len(reports) == 1 {
			if reports[0].IPAddress != "203.0.113.10" {
				t.Fatalf("report = %#v, want collected report", reports[0])
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for collected report")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if collector.Calls() != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.Calls())
	}
	if !store.state.LastAttemptedAt.Equal(now) || !store.state.LastSucceededAt.Equal(now) || store.state.LastStatus != agentapi.IPQualityStatusSuccess {
		t.Fatalf("state = %#v, want success timestamps", store.state)
	}
}

func TestManagerDoesNotStartWhenDisabledOrNotDue(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	store := &memoryStateStore{state: agentipquality.State{LastSucceededAt: now.Add(-time.Hour)}}
	collector := &channelCollector{}
	manager := agentipquality.NewManager(store, collector)

	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: false, FrequencySeconds: 86400}, now); err != nil {
		t.Fatalf("MaybeStart(disabled) error = %v", err)
	}
	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, now); err != nil {
		t.Fatalf("MaybeStart(not due) error = %v", err)
	}
	if collector.Calls() != 0 {
		t.Fatalf("collector calls = %d, want 0", collector.Calls())
	}
}

func TestManagerDoesNotStartSecondCollectionWhileInFlight(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	wait := make(chan struct{})
	store := &memoryStateStore{}
	collector := &channelCollector{wait: wait}
	manager := agentipquality.NewManager(store, collector)
	plan := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}

	if err := manager.MaybeStart(context.Background(), plan, now); err != nil {
		t.Fatalf("MaybeStart(first) error = %v", err)
	}
	if err := manager.MaybeStart(context.Background(), plan, now.Add(time.Second)); err != nil {
		t.Fatalf("MaybeStart(second) error = %v", err)
	}
	close(wait)

	deadline := time.After(time.Second)
	for collector.Calls() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for collector")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if collector.Calls() != 1 {
		t.Fatalf("collector calls = %d, want 1 while in flight", collector.Calls())
	}
}

func TestManagerThrottlesAfterFailedAttempt(t *testing.T) {
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)
	store := &memoryStateStore{
		state: agentipquality.State{
			LastAttemptedAt: now.Add(-time.Hour),
			LastStatus:      agentapi.IPQualityStatusFailure,
		},
	}
	collector := &channelCollector{}
	manager := agentipquality.NewManager(store, collector)

	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, now); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}

	if collector.Calls() != 0 {
		t.Fatalf("collector calls = %d, want 0 for recent failed attempt", collector.Calls())
	}
	if store.saveCalls != 0 {
		t.Fatalf("saveCalls = %d, want 0 when throttled", store.saveCalls)
	}
}

func TestManagerReturnsStateLoadErrorWithoutCollecting(t *testing.T) {
	loadErr := errors.New("state unreadable")
	store := &memoryStateStore{loadErr: loadErr}
	collector := &channelCollector{}
	manager := agentipquality.NewManager(store, collector)

	err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, time.Now())
	if !errors.Is(err, loadErr) {
		t.Fatalf("MaybeStart() error = %v, want load error", err)
	}
	if collector.Calls() != 0 {
		t.Fatalf("collector calls = %d, want 0", collector.Calls())
	}
}

func waitForReports(t *testing.T, manager *agentipquality.Manager) []agentapi.IPQualityReportPayload {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if reports := manager.DrainReports(); len(reports) > 0 {
			return reports
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for collected report")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func reportCollectRequestID(t *testing.T, report agentapi.IPQualityReportPayload) string {
	t.Helper()
	if len(report.DiagnosticsJSON) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(report.DiagnosticsJSON, &fields); err != nil {
		t.Fatalf("diagnostics_json = %s, want JSON object: %v", report.DiagnosticsJSON, err)
	}
	value, _ := fields[agentapi.IPQualityDiagnosticsCollectRequestIDKey].(string)
	return value
}

func TestManagerCollectRequestBypassesFrequencyOnce(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	store := &memoryStateStore{state: agentipquality.State{
		LastAttemptedAt: now.Add(-time.Hour),
		LastSucceededAt: now.Add(-time.Hour),
		LastStatus:      agentapi.IPQualityStatusSuccess,
	}}
	collector := &channelCollector{report: agentapi.IPQualityReportPayload{
		Status:          agentapi.IPQualityStatusFailure,
		DiagnosticsJSON: json.RawMessage(`{"source_version":"v2"}`),
	}}
	manager := agentipquality.NewManager(store, collector)
	plan := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectRequestID: "ipqc_001"}

	if err := manager.MaybeStart(context.Background(), plan, now); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}
	reports := waitForReports(t, manager)
	if len(reports) != 1 || reportCollectRequestID(t, reports[0]) != "ipqc_001" {
		t.Fatalf("reports = %#v, want one report carrying the request id in diagnostics", reports)
	}
	if !strings.Contains(string(reports[0].DiagnosticsJSON), `"source_version":"v2"`) {
		t.Fatalf("diagnostics_json = %s, want existing diagnostics kept", reports[0].DiagnosticsJSON)
	}
	if !store.state.LastAttemptedAt.Equal(now) || !store.state.LastSucceededAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("state = %#v, want new attempt and previous success kept after a failed manual run", store.state)
	}

	// center 在收到报告前会继续下发同一请求，不能重复采集。
	if err := manager.MaybeStart(context.Background(), plan, now.Add(5*time.Second)); err != nil {
		t.Fatalf("MaybeStart(repeat) error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if collector.Calls() != 1 {
		t.Fatalf("collector calls = %d, want 1 for a repeated request id", collector.Calls())
	}

	// 去重只在内存：重启后的 agent 若仍收到该请求会再采一次，而不是漏采。
	restarted := agentipquality.NewManager(store, collector)
	if err := restarted.MaybeStart(context.Background(), plan, now.Add(10*time.Second)); err != nil {
		t.Fatalf("MaybeStart(after restart) error = %v", err)
	}
	if reports := waitForReports(t, restarted); len(reports) != 1 || collector.Calls() != 2 {
		t.Fatalf("reports = %#v calls = %d, want a restarted agent to answer the request again", reports, collector.Calls())
	}
}

func TestManagerAdoptsCollectRequestDuringScheduledRun(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	wait := make(chan struct{})
	store := &memoryStateStore{}
	collector := &channelCollector{wait: wait, report: agentapi.IPQualityReportPayload{Status: agentapi.IPQualityStatusSuccess}}
	manager := agentipquality.NewManager(store, collector)

	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, now); err != nil {
		t.Fatalf("MaybeStart(scheduled) error = %v", err)
	}
	manual := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectRequestID: "ipqc_002"}
	if err := manager.MaybeStart(context.Background(), manual, now.Add(time.Second)); err != nil {
		t.Fatalf("MaybeStart(manual while in flight) error = %v", err)
	}
	close(wait)

	reports := waitForReports(t, manager)
	if len(reports) != 1 || reportCollectRequestID(t, reports[0]) != "ipqc_002" {
		t.Fatalf("reports = %#v, want the in-flight run to answer the request", reports)
	}
	if err := manager.MaybeStart(context.Background(), manual, now.Add(2*time.Second)); err != nil {
		t.Fatalf("MaybeStart(after adoption) error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if collector.Calls() != 1 {
		t.Fatalf("collector calls = %d, want 1 for adopted request not to run again", collector.Calls())
	}
}

func TestManagerScheduledReportDoesNotCarryRequestID(t *testing.T) {
	store := &memoryStateStore{}
	collector := &channelCollector{report: agentapi.IPQualityReportPayload{Status: agentapi.IPQualityStatusSuccess}}
	manager := agentipquality.NewManager(store, collector)

	if err := manager.MaybeStart(context.Background(), &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}, time.Now()); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}
	if reports := waitForReports(t, manager); len(reports) != 1 || len(reports[0].DiagnosticsJSON) != 0 {
		t.Fatalf("reports = %#v, want scheduled diagnostics untouched", reports)
	}
}

func TestManagerIgnoresCollectRequestWhenDisabled(t *testing.T) {
	store := &memoryStateStore{}
	collector := &channelCollector{}
	manager := agentipquality.NewManager(store, collector)

	plan := &agentapi.IPQualityPlan{Enabled: false, FrequencySeconds: 86400, CollectRequestID: "ipqc_003"}
	if err := manager.MaybeStart(context.Background(), plan, time.Now()); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}
	if collector.Calls() != 0 || store.saveCalls != 0 {
		t.Fatalf("calls = %d saves = %d, want disabled plan to ignore requests", collector.Calls(), store.saveCalls)
	}
}

func TestManagerRetriesCollectRequestAfterStop(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	wait := make(chan struct{})
	store := &memoryStateStore{state: agentipquality.State{LastAttemptedAt: now.Add(-time.Minute)}}
	collector := &channelCollector{wait: wait, report: agentapi.IPQualityReportPayload{Status: agentapi.IPQualityStatusSuccess}}
	manager := agentipquality.NewManager(store, collector)
	plan := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectRequestID: "ipqc_004"}

	if err := manager.MaybeStart(context.Background(), plan, now); err != nil {
		t.Fatalf("MaybeStart() error = %v", err)
	}
	manager.Stop()
	close(wait)
	deadline := time.After(time.Second)
	for {
		// Stop 丢弃本轮输出，等待 inFlight 释放后同一请求可以再次执行。
		if err := manager.MaybeStart(context.Background(), plan, now.Add(time.Second)); err != nil {
			t.Fatalf("MaybeStart(retry) error = %v", err)
		}
		if collector.Calls() >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("collector calls = %d, want the cancelled request to run again", collector.Calls())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	reports := waitForReports(t, manager)
	if len(reports) != 1 || reportCollectRequestID(t, reports[0]) != "ipqc_004" {
		t.Fatalf("reports = %#v, want only the retried run", reports)
	}

	// 报告已产出、尚未被取走时 Stop：报告随之丢弃，请求也不再算已处理。
	manager.Stop()
	if err := manager.MaybeStart(context.Background(), plan, now.Add(2*time.Second)); err != nil {
		t.Fatalf("MaybeStart(after stop) error = %v", err)
	}
	if reports := waitForReports(t, manager); len(reports) != 1 || collector.Calls() != 3 {
		t.Fatalf("reports = %#v calls = %d, want the request answered again after Stop", reports, collector.Calls())
	}
}
