package ipquality_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

type scopedCollectionCall struct {
	ctx     context.Context
	plan    *agentapi.IPQualityPlan
	release chan struct{}
}

type scopedChannelCollector struct{ calls chan scopedCollectionCall }

func (c *scopedChannelCollector) Collect(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) []agentapi.IPQualityReportPayload {
	call := scopedCollectionCall{ctx: ctx, plan: plan, release: make(chan struct{})}
	c.calls <- call
	select {
	case <-call.release:
	case <-ctx.Done():
	}
	var reports []agentapi.IPQualityReportPayload
	for _, family := range agentapi.CollectionIPVersions(plan) {
		reports = append(reports, agentapi.IPQualityReportPayload{IPVersion: family, Status: agentapi.IPQualityStatusSuccess, ObservedAt: observedAt})
	}
	return reports
}

func receiveScopedCall(t *testing.T, c *scopedChannelCollector) scopedCollectionCall {
	t.Helper()
	select {
	case call := <-c.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("collector did not start")
		return scopedCollectionCall{}
	}
}

func TestManagerClaimsOnlyExactlyMatchingScopeAndNeverNarrowsPeriodic(t *testing.T) {
	for _, requested := range [][]int{{4}, {6}, {4, 6}} {
		t.Run(map[int]string{4: "four", 6: "six"}[requested[0]]+map[int]string{1: "single", 2: "dual"}[len(requested)], func(t *testing.T) {
			now := time.Now().UTC()
			collector := &scopedChannelCollector{calls: make(chan scopedCollectionCall, 4)}
			manager := agentipquality.NewManager(&memoryStateStore{}, collector)
			periodic := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}
			if err := manager.MaybeStart(context.Background(), periodic, now); err != nil {
				t.Fatal(err)
			}
			first := receiveScopedCall(t, collector)
			manual := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectionRevision: 5, CollectRequestID: "scope", CollectIPVersions: append([]int(nil), requested...)}
			if err := manager.MaybeStart(context.Background(), manual, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			close(first.release)
			reports := waitForReports(t, manager)
			if len(reports) != 2 {
				t.Fatalf("periodic narrowed: %#v", reports)
			}
			wantID := ""
			if len(requested) == 2 {
				wantID = "scope"
			}
			for _, report := range reports {
				if got := reportCollectRequestID(t, report); got != wantID {
					t.Fatalf("wrong scope adopted request: %q want %q", got, wantID)
				}
			}
			if len(requested) == 1 {
				if err := manager.MaybeStart(context.Background(), manual, now.Add(2*time.Second)); err != nil {
					t.Fatal(err)
				}
				second := receiveScopedCall(t, collector)
				if !slices.Equal(agentapi.CollectionIPVersions(second.plan), requested) {
					t.Fatalf("manual scope=%v", agentapi.CollectionIPVersions(second.plan))
				}
				manual.CollectIPVersions[0] = 99
				if !slices.Equal(agentapi.CollectionIPVersions(second.plan), requested) {
					t.Fatal("manager plan shares caller selector")
				}
				close(second.release)
				for _, report := range waitForReports(t, manager) {
					if reportCollectRequestID(t, report) != "scope" {
						t.Fatal("single run lost request ID")
					}
				}
				manual.CollectIPVersions[0] = requested[0]
			}
			// The already-handled manual selector cannot narrow the next due period.
			if err := manager.MaybeStart(context.Background(), manual, now.Add(25*time.Hour)); err != nil {
				t.Fatal(err)
			}
			third := receiveScopedCall(t, collector)
			if third.plan.CollectRequestID != "" || !slices.Equal(agentapi.CollectionIPVersions(third.plan), []int{4, 6}) {
				t.Fatalf("handled ID narrowed period: %#v", third.plan)
			}
			close(third.release)
			for _, report := range waitForReports(t, manager) {
				if reportCollectRequestID(t, report) != "" {
					t.Fatal("periodic report carried handled ID")
				}
			}
		})
	}
}

func TestManagerConcurrentClaimsStopAndRetryPreserveSingleScope(t *testing.T) {
	now := time.Now().UTC()
	collector := &scopedChannelCollector{calls: make(chan scopedCollectionCall, 4)}
	manager := agentipquality.NewManager(&memoryStateStore{}, collector)
	manual := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectionRevision: 5, CollectRequestID: "retry", CollectIPVersions: []int{6}}
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := manager.MaybeStart(context.Background(), manual, now); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	first := receiveScopedCall(t, collector)
	select {
	case <-collector.calls:
		t.Fatal("concurrent starts created multiple rounds")
	default:
	}
	manager.Stop()
	select {
	case <-first.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel collection")
	}
	deadline := time.After(time.Second)
	var retry scopedCollectionCall
retryLoop:
	for {
		if err := manager.MaybeStart(context.Background(), manual, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		select {
		case retry = <-collector.calls:
			break retryLoop
		case <-deadline:
			t.Fatal("same request not retried after Stop")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if !slices.Equal(agentapi.CollectionIPVersions(retry.plan), []int{6}) {
		t.Fatal("retry lost scope")
	}
	close(retry.release)
	reports := waitForReports(t, manager)
	if len(reports) != 1 || reports[0].IPVersion != 6 || reportCollectRequestID(t, reports[0]) != "retry" {
		t.Fatalf("canceled output leaked or retry failed: %#v", reports)
	}
	// Memory-only de-duplication must not survive process restart.
	restarted := agentipquality.NewManager(&memoryStateStore{}, collector)
	if err := restarted.MaybeStart(context.Background(), manual, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	afterRestart := receiveScopedCall(t, collector)
	close(afterRestart.release)
	if reports := waitForReports(t, restarted); len(reports) != 1 || reports[0].IPVersion != 6 {
		t.Fatalf("restart did not retry same selected scope: %#v", reports)
	}
}

func TestManagerStopRacingCompletionAndClaimCannotLeakCanceledRound(t *testing.T) {
	for range 16 {
		now := time.Now().UTC()
		collector := &scopedChannelCollector{calls: make(chan scopedCollectionCall, 4)}
		manager := agentipquality.NewManager(&memoryStateStore{}, collector)
		periodic := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400}
		if err := manager.MaybeStart(context.Background(), periodic, now); err != nil {
			t.Fatal(err)
		}
		first := receiveScopedCall(t, collector)
		manual := &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, CollectionRevision: 5, CollectRequestID: "raced", CollectIPVersions: []int{6}}
		// Completion and scope-mismatched claiming race. Neither ordering may
		// attach a single-family request to the dual-stack periodic output.
		var racing sync.WaitGroup
		racing.Add(2)
		go func() { defer racing.Done(); close(first.release) }()
		go func() {
			defer racing.Done()
			if err := manager.MaybeStart(context.Background(), manual, now.Add(time.Second)); err != nil {
				t.Error(err)
			}
		}()
		racing.Wait()
		for _, report := range manager.DrainReports() {
			if reportCollectRequestID(t, report) != "" {
				t.Fatal("single scope claimed dual round during completion")
			}
		}
		manager.Stop()
		// Stop may have canceled a newly started matching round, or discarded
		// just-completed periodic output. Drain any observed canceled call.
		select {
		case canceled := <-collector.calls:
			<-canceled.ctx.Done()
		default:
		}
		deadline := time.After(time.Second)
		var retry scopedCollectionCall
	retryAfterStop:
		for {
			if err := manager.MaybeStart(context.Background(), manual, now.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			select {
			case candidate := <-collector.calls:
				if candidate.ctx.Err() == nil {
					retry = candidate
					break retryAfterStop
				}
			case <-deadline:
				t.Fatal("race lost pending request")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		close(retry.release)
		reports := waitForReports(t, manager)
		if len(reports) != 1 || reports[0].IPVersion != 6 || reportCollectRequestID(t, reports[0]) != "raced" {
			t.Fatalf("race leaked unrelated/canceled output: %#v", reports)
		}
	}
}
