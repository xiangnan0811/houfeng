package subscriptioncosts

import (
	"context"
	"testing"
)

func TestOverviewSeparatesArchivedPotentialChargesAndUnknownAmounts(t *testing.T) {
	service, repo := newTestService()
	current, archived := 10.0, 25.0
	repo.rows = []CostRow{
		{VPSID: "current", LifecycleStatus: "active", MonthlyPriceBase: &current},
		{VPSID: "archived", LifecycleStatus: "archived", MonthlyPriceBase: &archived},
		{VPSID: "unknown-rate", LifecycleStatus: "archived"},
	}
	repo.missing = []MissingSubscriptionAsset{{VPSID: "unknown-subscription", LifecycleStatus: "archived"}}
	got, err := service.GetOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalMonthlyCost != 10 || got.ActiveSubscriptionCount != 1 || len(got.VPSCosts) != 1 {
		t.Fatalf("archived charges entered current cost: %#v", got)
	}
	if len(got.ArchivedPotentialCosts) != 2 || got.ArchivedUnknownAmountCount != 2 || got.ArchivedPotentialMonthlyCost != nil {
		t.Fatalf("unknown archived amount was treated as zero: %#v", got)
	}
	if got.MissingSubscriptionVPSCount != 0 || len(got.ArchivedMissingSubscriptionAssets) != 1 {
		t.Fatalf("archived missing subscription entered current inventory: %#v", got)
	}
}

func TestNoRenewalAttentionDependsOnProviderCheckNotLifecycle(t *testing.T) {
	for _, check := range []string{"disabled", "never_enabled", "unsupported"} {
		if isDecisionAttention(CostRow{RenewalDecision: "cancel", LifecycleStatus: "active", AutoRenewCheck: check}) {
			t.Fatalf("completed provider check %q still requires attention", check)
		}
	}
	for _, check := range []string{"unchecked", "enabled"} {
		if !isDecisionAttention(CostRow{RenewalDecision: "cancel", AutoRenewCheck: check}) {
			t.Fatalf("unresolved provider check %q lost attention", check)
		}
	}
}
