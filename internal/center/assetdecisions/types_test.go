package assetdecisions

import (
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestDeriveGroupsBuildsPortfolioDecisionGroups(t *testing.T) {
	providerID := "pv_hetzner"
	facts := []Fact{
		fact("vps_de_1", "DE Primary", providerID, "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageInUse, sub("sub_1", 12, 7)),
		fact("vps_de_2", "DE Standby", providerID, "Hetzner", "Germany", "Hesse", "Falkenstein", vpsassets.UsageStandby, sub("sub_2", 8, 21)),
		fact("vps_us_1", "US Idle", "pv_bandwagon", "Bandwagon", "United States", "CA", "Los Angeles", vpsassets.UsageIdle, sub("sub_3", 30, 14)),
	}
	facts[0].ServiceCount = 2
	facts[0].DomainCount = 1
	facts[0].TargetCount = 1
	facts[0].RunningTargetCount = 1
	facts[2].PrimarySubscription.BudgetStatus = "over"

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}

	if !hasGroupType(groups, GroupRenewalAttention) {
		t.Fatalf("groups = %#v, want renewal attention group", groups)
	}
	if !hasGroupType(groups, GroupRegionPortfolio) {
		t.Fatalf("groups = %#v, want region portfolio group", groups)
	}
	if !hasGroupType(groups, GroupProviderPortfolio) {
		t.Fatalf("groups = %#v, want provider portfolio group", groups)
	}
	if !hasGroupType(groups, GroupCostPressure) {
		t.Fatalf("groups = %#v, want cost pressure group", groups)
	}

	region := firstGroup(groups, GroupRegionPortfolio)
	if region.MemberCount != 2 || region.ScopeLabel != "Germany / Hesse / Falkenstein" {
		t.Fatalf("region group = %#v, want two same-region VPS", region.GroupSummary)
	}
	if region.ServiceCount != 2 || region.DomainCount != 1 || region.TargetCount != 1 || region.RunningTargetCount != 1 {
		t.Fatalf("region context = %#v, want service/domain/target rollup", region.GroupSummary)
	}
	if region.GroupID != StableGroupID(GroupRegionPortfolio, region.ScopeKey) {
		t.Fatalf("group id = %q, want stable derived id", region.GroupID)
	}
}

func TestDeriveGroupsCancellationAndArchivedBoundaries(t *testing.T) {
	facts := []Fact{
		fact("vps_cancelled_running", "Cancelled Runtime", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageInUse, sub("sub_running", 10, 30)),
		fact("vps_archived", "Archived", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageIdle, sub("sub_archived", 10, 30)),
	}
	facts[0].VPS.LifecycleStatus = vpsassets.LifecycleCancelled
	facts[0].RunningMonitoringCount = 1
	facts[0].RunningTargetCount = 1
	facts[1].VPS.LifecycleStatus = vpsassets.LifecycleArchived

	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}

	cancellation := firstGroup(groups, GroupCancellationAttention)
	if cancellation.MemberCount != 1 || cancellation.Members[0].VPS.VPSID != "vps_cancelled_running" {
		t.Fatalf("cancellation group = %#v, want cancelled runtime member only", cancellation)
	}
	if hasGroupType(groups, GroupRegionPortfolio) {
		t.Fatalf("groups = %#v, want archived/cancelled excluded from ordinary portfolio groups", groups)
	}
}

func TestDeriveGroupsEvidenceGapDoesNotMisreportUnavailableSubscriptions(t *testing.T) {
	f := fact("vps_001", "Evidence Gap", "", "", "", "", "", vpsassets.UsageInUse, nil)
	f.SourceAvailability.Subscriptions = false
	f.MonitoringLinkCount = 0

	groups, err := DeriveGroups([]Fact{f}, ListFilters{RenewWithinDays: 30, View: ViewEvidence})
	if err != nil {
		t.Fatalf("DeriveGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want evidence group", groups)
	}
	member := groups[0].Members[0]
	if !hasEvidence(member, EvidenceSubscriptionUnavailable) {
		t.Fatalf("member chips = %#v, want subscription unavailable evidence", member.EvidenceChips)
	}
	if hasEvidence(member, EvidenceMissingSubscription) {
		t.Fatalf("member chips = %#v, do not want missing subscription when source unavailable", member.EvidenceChips)
	}
	if member.SuggestedAction != ActionCompleteEvidence || member.SuggestedRole != RoleEvidenceNeeded {
		t.Fatalf("member suggestion = (%q,%q), want evidence completion", member.SuggestedRole, member.SuggestedAction)
	}
}

func TestFindGroupRecomputesStableGroups(t *testing.T) {
	facts := []Fact{
		fact("vps_de_1", "DE 1", "pv_1", "Provider", "Germany", "", "Frankfurt", vpsassets.UsageInUse, sub("sub_1", 12, 30)),
		fact("vps_de_2", "DE 2", "pv_2", "Other", "Germany", "", "Frankfurt", vpsassets.UsageStandby, sub("sub_2", 8, 30)),
	}
	groupID := StableGroupID(GroupRegionPortfolio, "germany / frankfurt")

	group, err := FindGroup(facts, groupID, ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("FindGroup() error = %v", err)
	}
	if group.GroupID != groupID || group.MemberCount != 2 {
		t.Fatalf("group = %#v, want recomputed same-region group", group.GroupSummary)
	}

	if _, err := FindGroup(facts, "adg_auto_missing", ListFilters{RenewWithinDays: 30}); err != ErrAssetDecisionGroupNotFound {
		t.Fatalf("FindGroup() error = %v, want not found sentinel", err)
	}
}

func fact(vpsID, name, providerID, providerName, country, region, city string, usage vpsassets.UsageStatus, subscription *subscriptions.Record) Fact {
	providerPtr := &providerID
	if providerID == "" {
		providerPtr = nil
	}
	return Fact{
		VPS: vpsassets.Record{
			VPSID:           vpsID,
			DisplayName:     name,
			ProviderID:      providerPtr,
			ProviderName:    providerName,
			Country:         country,
			Region:          region,
			City:            city,
			IPv4:            "192.0.2.1",
			SSHPort:         22,
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     usage,
			RenewalDecision: vpsassets.RenewalUnreviewed,
			CreatedAt:       time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		},
		PrimarySubscription: subscription,
		SubscriptionCount: func() int {
			if subscription == nil {
				return 0
			}
			return 1
		}(),
		ActiveSubscriptionCount: func() int {
			if subscription != nil && subscription.Status == subscriptions.StatusActive {
				return 1
			}
			return 0
		}(),
		SourceAvailability: SourceAvailability{
			Subscriptions: true,
			Services:      true,
			Domains:       true,
			Monitoring:    true,
			Targets:       true,
		},
	}
}

func sub(id string, monthly float64, renewInDays int) *subscriptions.Record {
	renewAt := subscriptions.NewDate(time.Now().UTC().AddDate(0, 0, renewInDays))
	return &subscriptions.Record{
		SubscriptionID: id,
		VPSID:          "vps",
		Currency:       "USD",
		MonthlyPrice:   monthly,
		RenewAt:        &renewAt,
		Status:         subscriptions.StatusActive,
		CreatedAt:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
	}
}

func hasGroupType(groups []GroupDetail, groupType GroupType) bool {
	for _, group := range groups {
		if group.GroupType == groupType {
			return true
		}
	}
	return false
}

func firstGroup(groups []GroupDetail, groupType GroupType) GroupDetail {
	for _, group := range groups {
		if group.GroupType == groupType {
			return group
		}
	}
	return GroupDetail{}
}
