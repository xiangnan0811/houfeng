package runtime

import (
	"slices"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestCloneSyncPlanPreservesAndOwnsCollectionScope(t *testing.T) {
	plan := &agentapi.SyncPlan{IPQualityPlan: &agentapi.IPQualityPlan{Enabled: true, CollectionRevision: 5, CollectRequestID: "single", CollectIPVersions: []int{6}}}
	copy := cloneSyncPlan(plan)
	if copy.IPQualityPlan.CollectionRevision != 5 || copy.IPQualityPlan.CollectRequestID != "single" || !slices.Equal(copy.IPQualityPlan.CollectIPVersions, []int{6}) {
		t.Fatalf("scope lost: %#v", copy.IPQualityPlan)
	}
	plan.IPQualityPlan.CollectIPVersions[0] = 4
	if copy.IPQualityPlan.CollectIPVersions[0] != 6 {
		t.Fatal("runtime shares incoming plan selector")
	}
	copy.IPQualityPlan.CollectIPVersions[0] = 99
	if plan.IPQualityPlan.CollectIPVersions[0] != 4 {
		t.Fatal("clone mutation changed caller")
	}
}
