package ipquality_test

import (
	"context"
	"errors"
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
	calls  int
	report agentapi.IPQualityReportPayload
	wait   chan struct{}
}

func (c *channelCollector) Collect(context.Context, *agentapi.IPQualityPlan, time.Time) agentapi.IPQualityReportPayload {
	c.calls++
	if c.wait != nil {
		<-c.wait
	}
	return c.report
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
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.calls)
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
	if collector.calls != 0 {
		t.Fatalf("collector calls = %d, want 0", collector.calls)
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
	for collector.calls == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for collector")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want 1 while in flight", collector.calls)
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
	if collector.calls != 0 {
		t.Fatalf("collector calls = %d, want 0", collector.calls)
	}
}
