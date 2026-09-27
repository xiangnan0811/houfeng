package assetdecisions

import (
	"testing"

	"houfeng/internal/center/vpsassets"
)

func TestUsageTagsDoNotChangeLifecycleRenewalOrRecommendation(t *testing.T) {
	base := fact("vps_tags", "Tag independence", "provider", "Provider", "JP", "", "Tokyo", vpsassets.UsageInUse, sub("sub_tags", 10, 30))
	base.ServiceCount = 1
	base.MonitoringLinkCount = 1
	base.RunningMonitoringCount = 1
	base.VPS.UsageTags = []string{"业务", "自定义用途"}
	want := buildMember(base, ListFilters{RenewWithinDays: 30})
	base.VPS.UsageTags = []string{"闲置", "测试", "待迁移"}
	got := buildMember(base, ListFilters{RenewWithinDays: 30})
	if got.SuggestedAction != want.SuggestedAction || got.SuggestedRole != want.SuggestedRole || got.VPS.RenewalDecision != want.VPS.RenewalDecision || got.VPS.LifecycleStatus != want.VPS.LifecycleStatus {
		t.Fatalf("free usage tags changed business state or recommendation: got %#v want %#v", got, want)
	}
}

func TestNoRenewalDoesNotRequireArchivalOrSubscriptionCancellation(t *testing.T) {
	f := fact("vps_keep_running", "Continue until expiry", "provider", "Provider", "JP", "", "Tokyo", vpsassets.UsageInUse, sub("sub_live", 10, 30))
	f.VPS.RenewalDecision = vpsassets.RenewalCancel
	f.VPS.AutoRenewCheck = "disabled"
	f.RunningMonitoringCount = 1
	f.RunningTargetCount = 1
	if got := executionIssuesForAction(ActionCancel, f); len(got) != 0 {
		t.Fatalf("independent no-renewal decision forced resource termination: %#v", got)
	}
}
