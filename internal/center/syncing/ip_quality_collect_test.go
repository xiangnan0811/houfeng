package syncing

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/ipquality"
)

type fakeCollectCoordinator struct {
	requestID       string
	observed        []ipquality.ReportWrite
	observedFor     string
	pendingCalls    int
	pendingInstance string
}

func (f *fakeCollectCoordinator) ObserveReports(monitoringInstanceID string, reports []ipquality.ReportWrite, _ time.Time) {
	f.observedFor = monitoringInstanceID
	f.observed = append(f.observed, reports...)
}

func (f *fakeCollectCoordinator) PendingRequestID(monitoringInstanceID string, _ time.Time) string {
	f.pendingCalls++
	f.pendingInstance = monitoringInstanceID
	return f.requestID
}

func TestServiceAttachesPendingCollectRequestToPlanCopy(t *testing.T) {
	t.Parallel()

	repoPlan := &agentplan.IPQualityPlan{Enabled: true, FrequencySeconds: 86400, TimeoutSeconds: 15, Services: []string{"netflix"}}
	repo := &fakeSyncRepository{result: Result{Disposition: ResultDispositionRecorded, Plan: agentplan.SyncPlan{IPQualityPlan: repoPlan}}}
	coordinator := &fakeCollectCoordinator{requestID: "ipqc_001"}
	service := NewService(repo).WithIPQualityCollectCoordinator(coordinator)

	batch := Batch{
		MonitoringInstanceID: "mi_001",
		IPQualityReports:     []ipquality.ReportWrite{{Status: "success"}},
	}
	got, err := service.SyncBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("SyncBatch() error = %v", err)
	}
	if got.Plan.IPQualityPlan == nil || got.Plan.IPQualityPlan.CollectRequestID != "ipqc_001" {
		t.Fatalf("plan = %#v, want pending collect request attached", got.Plan.IPQualityPlan)
	}
	if got.Plan.IPQualityPlan == repoPlan || repoPlan.CollectRequestID != "" {
		t.Fatal("repository plan must not be mutated")
	}
	if coordinator.observedFor != "mi_001" || len(coordinator.observed) != 1 {
		t.Fatalf("observed = %q %#v, want batch reports forwarded", coordinator.observedFor, coordinator.observed)
	}
	if coordinator.pendingInstance != "mi_001" {
		t.Fatalf("pending instance = %q, want mi_001", coordinator.pendingInstance)
	}
}

func TestServiceDoesNotDispatchCollectRequestWhenCollectionOff(t *testing.T) {
	t.Parallel()

	for name, result := range map[string]Result{
		"stop collection": {StopCollection: true, Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}}},
		"disabled plan":   {Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: false}}},
		"no plan":         {},
	} {
		result := result
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			coordinator := &fakeCollectCoordinator{requestID: "ipqc_001"}
			service := NewService(&fakeSyncRepository{result: result}).WithIPQualityCollectCoordinator(coordinator)
			got, err := service.SyncBatch(context.Background(), Batch{MonitoringInstanceID: "mi_001"})
			if err != nil {
				t.Fatalf("SyncBatch() error = %v", err)
			}
			if coordinator.pendingCalls != 0 {
				t.Fatalf("pending calls = %d, want 0 so the request stays pending", coordinator.pendingCalls)
			}
			if got.Plan.IPQualityPlan != nil && got.Plan.IPQualityPlan.CollectRequestID != "" {
				t.Fatalf("plan = %#v, want no collect request", got.Plan.IPQualityPlan)
			}
		})
	}
}

func TestWithIPQualityCollectCoordinatorKeepsOriginalService(t *testing.T) {
	t.Parallel()

	repo := &fakeSyncRepository{result: Result{Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}}}}
	original := NewService(repo)
	_ = original.WithIPQualityCollectCoordinator(&fakeCollectCoordinator{requestID: "ipqc_001"})

	got, err := original.SyncBatch(context.Background(), Batch{MonitoringInstanceID: "mi_001"})
	if err != nil {
		t.Fatalf("SyncBatch() error = %v", err)
	}
	if got.Plan.IPQualityPlan.CollectRequestID != "" {
		t.Fatal("original service must not gain the coordinator")
	}
}

func TestServiceCompletesCollectRequestsOnlyFromRecordedReports(t *testing.T) {
	t.Parallel()

	for _, disposition := range []ResultDisposition{ResultDispositionSuppressed, ResultDispositionExactDuplicate, ResultDisposition("")} {
		disposition := disposition
		t.Run(string(disposition), func(t *testing.T) {
			t.Parallel()
			coordinator := &fakeCollectCoordinator{}
			service := NewService(&fakeSyncRepository{result: Result{Disposition: disposition}}).WithIPQualityCollectCoordinator(coordinator)
			batch := Batch{MonitoringInstanceID: "mi_001", IPQualityReports: []ipquality.ReportWrite{{Status: "success"}}}
			if _, err := service.SyncBatch(context.Background(), batch); err != nil {
				t.Fatalf("SyncBatch() error = %v", err)
			}
			if len(coordinator.observed) != 0 {
				t.Fatalf("observed = %#v, reports that were not stored must not complete requests", coordinator.observed)
			}
		})
	}
}

func TestServiceCollectRequestLifecycleWithRegistry(t *testing.T) {
	t.Parallel()

	registry := ipquality.NewCollectRequests(func() (string, error) { return "ipqc_001", nil })
	repo := &fakeSyncRepository{result: Result{
		Disposition: ResultDispositionRecorded,
		Plan:        agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}},
	}}
	service := NewService(repo).WithIPQualityCollectCoordinator(registry)
	if _, err := registry.Request("mi_001", time.Now()); err != nil {
		t.Fatalf("Request() error = %v", err)
	}

	got, err := service.SyncBatch(context.Background(), Batch{MonitoringInstanceID: "mi_001"})
	if err != nil || got.Plan.IPQualityPlan.CollectRequestID != "ipqc_001" {
		t.Fatalf("SyncBatch() = %#v, %v; want request dispatched", got.Plan.IPQualityPlan, err)
	}

	echoed := Batch{MonitoringInstanceID: "mi_001", IPQualityReports: []ipquality.ReportWrite{{Status: "success", CollectRequestID: "ipqc_001"}}}
	// 实例在采集期间被暂停：报告随 suppressed 批次到达、未入库，请求保持进行中。
	repo.result = Result{Disposition: ResultDispositionSuppressed, StopCollection: true}
	if _, err := service.SyncBatch(context.Background(), echoed); err != nil {
		t.Fatalf("SyncBatch(suppressed) error = %v", err)
	}
	if latest, _ := registry.Latest("mi_001", time.Now()); latest.Status != ipquality.CollectRequestDispatched {
		t.Fatalf("latest = %#v, suppressed report must not complete the request", latest)
	}

	repo.result = Result{Disposition: ResultDispositionRecorded, Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}}}
	got, err = service.SyncBatch(context.Background(), echoed)
	if err != nil {
		t.Fatalf("SyncBatch(recorded) error = %v", err)
	}
	if latest, _ := registry.Latest("mi_001", time.Now()); latest.Status != ipquality.CollectRequestCompleted {
		t.Fatalf("latest = %#v, want completed by the recorded echoed report", latest)
	}
	if got.Plan.IPQualityPlan.CollectRequestID != "" {
		t.Fatalf("plan = %#v, completed request must not be dispatched again", got.Plan.IPQualityPlan)
	}
}
