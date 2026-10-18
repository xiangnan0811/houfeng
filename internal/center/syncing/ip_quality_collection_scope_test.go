package syncing

import (
	"context"
	"slices"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func TestSelectiveCollectionDispatchRequiresAcceptedWritableLiveCapability(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		live, allowed, stop, enabled bool
		revision                     int
		wantStatus                   string
		wantDispatch                 bool
	}{
		{"capable", true, true, false, true, 5, "dispatched", true},
		{"old agent", true, true, false, true, 0, "failed", false},
		{"future unsupported", true, true, false, true, 6, "failed", false},
		{"no live", false, true, false, true, 5, "pending", false},
		{"not writable", true, false, false, true, 5, "pending", false},
		{"stop", true, true, true, true, 5, "pending", false},
		{"disabled", true, true, false, false, 5, "pending", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			registry := ipquality.NewCollectRequests(func() (string, error) { return "scope", nil })
			_, _ = registry.Request("mi", now, []int{6})
			plan := &agentplan.IPQualityPlan{Enabled: tc.enabled, Services: []string{"reddit"}}
			repo := &fakeSyncRepository{result: Result{Disposition: ResultDispositionRecorded, StopCollection: tc.stop, LiveCollectionAllowed: tc.allowed, Plan: agentplan.SyncPlan{IPQualityPlan: plan}}}
			service := NewService(repo).WithIPQualityCollectCoordinator(registry)
			batch := validSyncBatch("mi")
			batch.IPQualityCollectionRevision = tc.revision
			if tc.live {
				batch.LiveSignal = &agentapi.LiveSignal{Sequence: 1, Fingerprint: "fp"}
			}
			result, err := service.SyncBatch(context.Background(), batch)
			if err != nil {
				t.Fatal(err)
			}
			current, _ := registry.Latest("mi", now)
			if current.Status != tc.wantStatus {
				t.Fatalf("request=%#v", current)
			}
			if tc.wantStatus == "failed" && current.ErrorCode != "unsupported_agent" {
				t.Fatalf("missing actionable error: %#v", current)
			}
			if (result.Plan.IPQualityPlan.CollectRequestID != "") != tc.wantDispatch {
				t.Fatalf("plan=%#v", result.Plan.IPQualityPlan)
			}
			if tc.wantDispatch {
				if result.Plan.IPQualityPlan.CollectionRevision != 5 || !slices.Equal(result.Plan.IPQualityPlan.CollectIPVersions, []int{6}) {
					t.Fatalf("negotiation lost: %#v", result.Plan.IPQualityPlan)
				}
				result.Plan.IPQualityPlan.CollectIPVersions[0] = 4
				result.Plan.IPQualityPlan.Services[0] = "netflix"
				current, _ = registry.Latest("mi", now)
				if !slices.Equal(current.RequestedIPVersions, []int{6}) || plan.Services[0] != "reddit" || plan.CollectRequestID != "" || plan.CollectionRevision != 0 {
					t.Fatal("plan copy mutated repository or coordinator")
				}
			}
		})
	}
}

func TestDualCollectionNegotiationIsPerLiveRequestAndLegacyCompatible(t *testing.T) {
	registry := ipquality.NewCollectRequests(func() (string, error) { return "dual", nil })
	_, _ = registry.Request("mi", time.Now())
	repo := &fakeSyncRepository{result: Result{Disposition: ResultDispositionRecorded, LiveCollectionAllowed: true, Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}}}}
	service := NewService(repo).WithIPQualityCollectCoordinator(registry)
	capableBatch := validSyncBatch("mi")
	capableBatch.LiveSignal = &agentapi.LiveSignal{Sequence: 1, Fingerprint: "fp"}
	capableBatch.IPQualityCollectionRevision = 5
	capable, err := service.SyncBatch(context.Background(), capableBatch)
	if err != nil {
		t.Fatal(err)
	}
	if capable.Plan.IPQualityPlan.CollectionRevision != 5 || !slices.Equal(capable.Plan.IPQualityPlan.CollectIPVersions, []int{4, 6}) {
		t.Fatalf("capable plan=%#v", capable.Plan.IPQualityPlan)
	}
	legacyLiveBatch := validSyncBatch("mi")
	legacyLiveBatch.LiveSignal = &agentapi.LiveSignal{Sequence: 2, Fingerprint: "fp"}
	for _, batch := range []Batch{validSyncBatch("mi"), legacyLiveBatch} {
		legacy, err := service.SyncBatch(context.Background(), batch)
		if err != nil {
			t.Fatal(err)
		}
		plan := legacy.Plan.IPQualityPlan
		if plan.CollectRequestID != "dual" || plan.CollectionRevision != 0 || len(plan.CollectIPVersions) != 0 {
			t.Fatalf("capability leaked across requests or legacy dispatch broken: %#v", plan)
		}
	}
}

func TestSelectiveRequestCompletesOnlyRecordedMatchingTerminal(t *testing.T) {
	registry := ipquality.NewCollectRequests(func() (string, error) { return "single", nil })
	now := time.Now().UTC()
	_, _ = registry.Request("mi", now, []int{4})
	repo := &fakeSyncRepository{result: Result{LiveCollectionAllowed: true, Disposition: ResultDispositionRecorded, Plan: agentplan.SyncPlan{IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true}}}}
	service := NewService(repo).WithIPQualityCollectCoordinator(registry)
	liveBatch := validSyncBatch("mi")
	liveBatch.LiveSignal = &agentapi.LiveSignal{Sequence: 1, Fingerprint: "fp"}
	liveBatch.IPQualityCollectionRevision = 5
	_, err := service.SyncBatch(context.Background(), liveBatch)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"collection_revision":5,"collection_id":"round","expected_ip_versions":[4],"ip_address_family":4,"family_availability":"available","collect_request_id":"single"}`)
	batch := validSyncBatch("mi", ipquality.ReportWrite{MonitoringInstanceID: "mi", IPVersion: 4, IPAddress: "198.51.100.10", Status: "success", ObservedAt: now, CollectionMetadata: agentapi.ParseCollectionMetadata(raw)})
	for _, disposition := range []ResultDisposition{ResultDispositionSuppressed, ResultDispositionExactDuplicate, ResultDispositionDeferred} {
		repo.result.Disposition = disposition
		if _, err := service.SyncBatch(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
		current, _ := registry.Latest("mi", now)
		if current.Status != ipquality.CollectRequestDispatched {
			t.Fatalf("unrecorded terminal completed: %#v", current)
		}
	}
	repo.result.Disposition = ResultDispositionRecorded
	if _, err := service.SyncBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	current, _ := registry.Latest("mi", now)
	if current.Status != ipquality.CollectRequestCompleted || current.CollectionScope != ipquality.CollectScopeSingleFamily || len(current.FamilyResults) != 1 {
		t.Fatalf("recorded single terminal failed: %#v", current)
	}
}
