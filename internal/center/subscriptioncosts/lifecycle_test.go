package subscriptioncosts

import (
	"context"
	"testing"
	"time"
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

func TestOverviewBudgetRiskUsesCurrentCompleteness(t *testing.T) {
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	limit := 100.0
	current90 := 90.0
	current1080 := 1080.0
	current120 := 120.0
	currentZero := 0.0
	tests := []struct {
		name                string
		rows                []CostRow
		missing             []MissingSubscriptionAsset
		wantTotalMonthly    float64
		wantCurrentUnknown  int
		wantCurrentMissing  int
		wantCurrentStale    int
		wantArchivedUnknown int
		wantStatus          BudgetStatus
		wantMonthlySpend    *float64
		wantYearlySpend     *float64
	}{
		{
			name: "current missing exchange rate at warning subtotal",
			rows: []CostRow{
				{VPSID: "known", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &current90, ExchangeRateStatus: ExchangeRateStatusIdentity},
				{VPSID: "missing-rate", LifecycleStatus: "active", Currency: "EUR", BaseCurrency: "CNY", ExchangeRateStatus: ExchangeRateStatusMissing},
			},
			wantTotalMonthly: 90, wantCurrentUnknown: 1, wantCurrentMissing: 1,
		},
		{
			name: "current missing exchange rate retains proven over",
			rows: []CostRow{
				{VPSID: "known", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &current120, ExchangeRateStatus: ExchangeRateStatusIdentity},
				{VPSID: "missing-rate", LifecycleStatus: "active", Currency: "EUR", BaseCurrency: "CNY", ExchangeRateStatus: ExchangeRateStatusMissing},
			},
			wantTotalMonthly: 120, wantCurrentUnknown: 1, wantCurrentMissing: 1,
			wantStatus: BudgetStatusOver,
		},
		{
			name:               "current missing subscription at warning subtotal",
			rows:               []CostRow{{VPSID: "known", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &current90, ExchangeRateStatus: ExchangeRateStatusIdentity}},
			missing:            []MissingSubscriptionAsset{{VPSID: "missing-subscription", LifecycleStatus: "active"}},
			wantTotalMonthly:   90,
			wantCurrentUnknown: 1,
		},
		{
			name:               "current missing subscription retains proven over",
			rows:               []CostRow{{VPSID: "known", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &current120, ExchangeRateStatus: ExchangeRateStatusIdentity}},
			missing:            []MissingSubscriptionAsset{{VPSID: "missing-subscription", LifecycleStatus: "active"}},
			wantTotalMonthly:   120,
			wantCurrentUnknown: 1,
			wantStatus:         BudgetStatusOver,
		},
		{
			name:             "current stale rate keeps numeric amount known",
			rows:             []CostRow{{VPSID: "stale-rate", LifecycleStatus: "active", Currency: "EUR", BaseCurrency: "CNY", MonthlyPriceBase: &current90, ExchangeRateStatus: ExchangeRateStatusStale}},
			wantTotalMonthly: 90,
			wantCurrentStale: 1,
			wantStatus:       BudgetStatusWarning,
			wantMonthlySpend: &current90,
			wantYearlySpend:  &current1080,
		},
		{
			name:               "identity zero is known",
			rows:               []CostRow{{VPSID: "identity-zero", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &currentZero, ExchangeRateStatus: ExchangeRateStatusIdentity}},
			wantCurrentUnknown: 0,
		},
		{
			name: "archived gaps do not affect complete current warning",
			rows: []CostRow{
				{VPSID: "known", LifecycleStatus: "active", Currency: "CNY", BaseCurrency: "CNY", MonthlyPriceBase: &current90},
				{VPSID: "archived-rate", LifecycleStatus: "archived", Currency: "EUR", BaseCurrency: "CNY"},
			},
			missing:             []MissingSubscriptionAsset{{VPSID: "archived-subscription", LifecycleStatus: "archived", AutoRenewCheck: "unchecked"}},
			wantTotalMonthly:    90,
			wantArchivedUnknown: 2,
			wantStatus:          BudgetStatusWarning,
			wantMonthlySpend:    &current90,
			wantYearlySpend:     &current1080,
		},
		{
			name:               "empty current collection",
			wantCurrentUnknown: 0,
		},
		{
			name:               "only current missing subscription",
			missing:            []MissingSubscriptionAsset{{VPSID: "missing-subscription", LifecycleStatus: "active"}},
			wantCurrentUnknown: 1,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			service, repo := newTestService()
			service.now = func() time.Time { return now }
			repo.rows = tt.rows
			repo.missing = tt.missing
			repo.budgetMonthBuckets = []SeriesPoint{{
				Bucket:           "2026-06",
				BudgetLimit:      &limit,
				BudgetCurrency:   "CNY",
				BudgetWarningPct: 80,
			}}

			got, err := service.GetOverview(context.Background())
			if err != nil {
				t.Fatalf("GetOverview() error = %v", err)
			}
			if got.TotalMonthlyCost != tt.wantTotalMonthly {
				t.Fatalf("total_monthly_cost = %.2f, want %.2f", got.TotalMonthlyCost, tt.wantTotalMonthly)
			}
			if got.CurrentUnknownAmountCount != tt.wantCurrentUnknown {
				t.Fatalf("current_unknown_amount_count = %d, want %d", got.CurrentUnknownAmountCount, tt.wantCurrentUnknown)
			}
			if got.CurrentMissingRateCount != tt.wantCurrentMissing {
				t.Fatalf("current_missing_rate_count = %d, want %d", got.CurrentMissingRateCount, tt.wantCurrentMissing)
			}
			if got.CurrentStaleRateCount != tt.wantCurrentStale {
				t.Fatalf("current_stale_rate_count = %d, want %d", got.CurrentStaleRateCount, tt.wantCurrentStale)
			}
			if got.ArchivedUnknownAmountCount != tt.wantArchivedUnknown {
				t.Fatalf("archived_unknown_amount_count = %d, want %d", got.ArchivedUnknownAmountCount, tt.wantArchivedUnknown)
			}
			if got.BudgetRiskCount != len(got.BudgetRisks) {
				t.Fatalf("budget risk count = %d, list length = %d", got.BudgetRiskCount, len(got.BudgetRisks))
			}
			if tt.wantStatus == "" {
				if len(got.BudgetRisks) != 0 {
					t.Fatalf("budget risks = %#v, want none", got.BudgetRisks)
				}
				return
			}
			if len(got.BudgetRisks) != 1 || got.BudgetRisks[0].Status != tt.wantStatus {
				t.Fatalf("budget risks = %#v, want one %q", got.BudgetRisks, tt.wantStatus)
			}
			assertSpendPointer(t, "monthly", got.BudgetRisks[0].CurrentMonthlySpend, tt.wantMonthlySpend)
			assertSpendPointer(t, "yearly", got.BudgetRisks[0].CurrentYearlySpend, tt.wantYearlySpend)
		})
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
