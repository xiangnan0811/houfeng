package subscriptioncosts

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/incidents"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
)

func TestEvaluateBudgetStatus(t *testing.T) {
	t.Parallel()
	limit := 100.0
	yearlyLimit := 1200.0
	tests := []struct {
		name           string
		enabled        bool
		currentMonthly float64
		monthlyLimit   *float64
		yearlyLimit    *float64
		want           BudgetStatus
	}{
		{name: "disabled", enabled: false, currentMonthly: 120, monthlyLimit: &limit, want: BudgetStatusDisabled},
		{name: "unknown without limit", enabled: true, currentMonthly: 20, want: BudgetStatusUnknown},
		{name: "ok", enabled: true, currentMonthly: 70, monthlyLimit: &limit, want: BudgetStatusOK},
		{name: "warning", enabled: true, currentMonthly: 80, monthlyLimit: &limit, want: BudgetStatusWarning},
		{name: "over", enabled: true, currentMonthly: 100, monthlyLimit: &limit, want: BudgetStatusOver},
		{name: "yearly limit", enabled: true, currentMonthly: 90, yearlyLimit: &yearlyLimit, want: BudgetStatusWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateBudgetStatus(tt.enabled, tt.currentMonthly, tt.monthlyLimit, tt.yearlyLimit, 80)
			if got != tt.want {
				t.Fatalf("EvaluateBudgetStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyBudgetSpendDerivesNullableAmounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		baseCurrency string
		rows         []CostRow
		budget       BudgetRecord
		wantStatus   BudgetStatus
		wantMonthly  *float64
		wantYearly   *float64
	}{
		{
			name:         "complete ok",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(70)), "CNY", "CNY")},
			budget:       testBudget("CNY", new(float64(100)), nil),
			wantStatus:   BudgetStatusOK,
			wantMonthly:  new(float64(70)),
			wantYearly:   new(float64(840)),
		},
		{
			name:         "complete warning",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(80)), "CNY", "CNY")},
			budget:       testBudget("CNY", new(float64(100)), nil),
			wantStatus:   BudgetStatusWarning,
			wantMonthly:  new(float64(80)),
			wantYearly:   new(float64(960)),
		},
		{
			name:         "complete over",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(100)), "CNY", "CNY")},
			budget:       testBudget("CNY", new(float64(100)), nil),
			wantStatus:   BudgetStatusOver,
			wantMonthly:  new(float64(100)),
			wantYearly:   new(float64(1200)),
		},
		{
			name:         "budget currency differs from query currency",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(100)), "CNY", "CNY")},
			budget:       testBudget("USD", new(float64(20)), nil),
			wantStatus:   BudgetStatusUnknown,
		},
		{
			name:         "query currency differs from budget currency",
			baseCurrency: "USD",
			rows:         []CostRow{testCostRow(new(float64(20)), "USD", "USD")},
			budget:       testBudget("CNY", new(float64(100)), nil),
			wantStatus:   BudgetStatusUnknown,
		},
		{
			name:         "original billing currency does not determine unit",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(70)), "USD", "CNY")},
			budget:       testBudget("CNY", new(float64(100)), nil),
			wantStatus:   BudgetStatusOK,
			wantMonthly:  new(float64(70)),
			wantYearly:   new(float64(840)),
		},
		{
			name:         "matching original currency does not override base mismatch",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(70)), "USD", "CNY")},
			budget:       testBudget("USD", new(float64(20)), nil),
			wantStatus:   BudgetStatusUnknown,
		},
		{
			name:         "incomplete below limit is unknown",
			baseCurrency: "CNY",
			rows: []CostRow{
				testCostRow(new(float64(70)), "CNY", "CNY"),
				testCostRow(nil, "CNY", "CNY"),
			},
			budget:     testBudget("CNY", new(float64(100)), nil),
			wantStatus: BudgetStatusUnknown,
		},
		{
			name:         "incomplete at limit preserves over without partial amount",
			baseCurrency: "CNY",
			rows: []CostRow{
				testCostRow(new(float64(100)), "CNY", "CNY"),
				testCostRow(nil, "CNY", "CNY"),
			},
			budget:     testBudget("CNY", new(float64(100)), nil),
			wantStatus: BudgetStatusOver,
		},
		{
			name:         "matching row with another base unit invalidates whole budget",
			baseCurrency: "CNY",
			rows: []CostRow{
				testCostRow(new(float64(100)), "CNY", "CNY"),
				testCostRow(new(float64(5)), "USD", "USD"),
			},
			budget:     testBudget("CNY", new(float64(100)), nil),
			wantStatus: BudgetStatusUnknown,
		},
		{
			name:         "archived priced row does not affect current budget",
			baseCurrency: "CNY",
			rows: []CostRow{
				testCostRow(new(float64(70)), "CNY", "CNY"),
				{
					MonthlyPriceBase: new(float64(100)),
					Currency:         "CNY",
					BaseCurrency:     "CNY",
					LifecycleStatus:  "archived",
					AutoRenewCheck:   "enabled",
				},
			},
			budget:      testBudget("CNY", new(float64(100)), nil),
			wantStatus:  BudgetStatusOK,
			wantMonthly: new(float64(70)),
			wantYearly:  new(float64(840)),
		},
		{
			name:         "archived missing amount does not make current budget incomplete",
			baseCurrency: "CNY",
			rows: []CostRow{
				testCostRow(new(float64(70)), "CNY", "CNY"),
				{
					Currency:        "CNY",
					BaseCurrency:    "CNY",
					LifecycleStatus: "archived",
					AutoRenewCheck:  "enabled",
				},
			},
			budget:      testBudget("CNY", new(float64(100)), nil),
			wantStatus:  BudgetStatusOK,
			wantMonthly: new(float64(70)),
			wantYearly:  new(float64(840)),
		},
		{
			name:         "same unit with no matching rows is zero",
			baseCurrency: "CNY",
			rows: []CostRow{{
				VPSID:            "other-vps",
				MonthlyPriceBase: new(float64(40)),
				Currency:         "CNY",
				BaseCurrency:     "CNY",
				LifecycleStatus:  "active",
			}},
			budget: func() BudgetRecord {
				budget := testBudget("CNY", new(float64(100)), nil)
				budget.ScopeType = string(BudgetScopeVPS)
				budget.ScopeID = "target-vps"
				return budget
			}(),
			wantStatus:  BudgetStatusOK,
			wantMonthly: new(float64(0)),
			wantYearly:  new(float64(0)),
		},
		{
			name:         "unmatched budget currency mismatch remains unknown",
			baseCurrency: "CNY",
			rows: []CostRow{{
				VPSID:            "other-vps",
				MonthlyPriceBase: new(float64(40)),
				Currency:         "CNY",
				BaseCurrency:     "CNY",
				LifecycleStatus:  "active",
			}},
			budget: func() BudgetRecord {
				budget := testBudget("USD", new(float64(100)), nil)
				budget.ScopeType = string(BudgetScopeVPS)
				budget.ScopeID = "target-vps"
				return budget
			}(),
			wantStatus: BudgetStatusUnknown,
		},
		{
			name:         "disabled budget keeps zero derived amounts",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(100)), "USD", "USD")},
			budget: func() BudgetRecord {
				budget := testBudget("USD", new(float64(20)), nil)
				budget.Enabled = false
				return budget
			}(),
			wantStatus:  BudgetStatusDisabled,
			wantMonthly: new(float64(0)),
			wantYearly:  new(float64(0)),
		},
		{
			name:         "yearly limit warning",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(80)), "CNY", "CNY")},
			budget:       testBudget("CNY", nil, new(float64(1200))),
			wantStatus:   BudgetStatusWarning,
			wantMonthly:  new(float64(80)),
			wantYearly:   new(float64(960)),
		},
		{
			name:         "yearly limit over",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(100)), "CNY", "CNY")},
			budget:       testBudget("CNY", nil, new(float64(1200))),
			wantStatus:   BudgetStatusOver,
			wantMonthly:  new(float64(100)),
			wantYearly:   new(float64(1200)),
		},
		{
			name:         "monthly limit takes priority over yearly limit",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(80)), "CNY", "CNY")},
			budget:       testBudget("CNY", new(float64(100)), new(float64(600))),
			wantStatus:   BudgetStatusWarning,
			wantMonthly:  new(float64(80)),
			wantYearly:   new(float64(960)),
		},
		{
			name:         "zero limit keeps known zero amount",
			baseCurrency: "CNY",
			rows:         []CostRow{testCostRow(new(float64(0)), "CNY", "CNY")},
			budget:       testBudget("CNY", new(float64(0)), nil),
			wantStatus:   BudgetStatusUnknown,
			wantMonthly:  new(float64(0)),
			wantYearly:   new(float64(0)),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := applyBudgetSpend(tt.rows, []BudgetRecord{tt.budget}, tt.baseCurrency)
			if len(got) != 1 {
				t.Fatalf("applyBudgetSpend() returned %d budgets, want 1", len(got))
			}
			if got[0].Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got[0].Status, tt.wantStatus)
			}
			assertSpendPointer(t, "monthly", got[0].CurrentMonthlySpend, tt.wantMonthly)
			assertSpendPointer(t, "yearly", got[0].CurrentYearlySpend, tt.wantYearly)
		})
	}
}

func TestApplyBudgetSpendMatchesBudgetScopes(t *testing.T) {
	t.Parallel()
	rows := []CostRow{
		{
			VPSID:            "vps_a",
			ProviderID:       "provider_a",
			ProviderName:     "Hetzner",
			Labels:           []string{"edge"},
			CostCategory:     "compute",
			MonthlyPriceBase: new(float64(10)),
			BaseCurrency:     "CNY",
			Currency:         "USD",
			LifecycleStatus:  "active",
		},
		{
			VPSID:            "vps_b",
			ProviderID:       "provider_b",
			ProviderName:     "AWS",
			Labels:           []string{"backup"},
			CostCategory:     "storage",
			MonthlyPriceBase: new(float64(20)),
			BaseCurrency:     "CNY",
			Currency:         "EUR",
			LifecycleStatus:  "active",
		},
	}
	tests := []struct {
		name      string
		scopeType BudgetScopeType
		scopeID   string
		wantSpend float64
	}{
		{name: "global", scopeType: BudgetScopeGlobal, wantSpend: 30},
		{name: "provider id", scopeType: BudgetScopeProvider, scopeID: "provider_a", wantSpend: 10},
		{name: "provider name", scopeType: BudgetScopeProvider, scopeID: "AWS", wantSpend: 20},
		{name: "label", scopeType: BudgetScopeLabel, scopeID: "edge", wantSpend: 10},
		{name: "category", scopeType: BudgetScopeCategory, scopeID: "storage", wantSpend: 20},
		{name: "vps", scopeType: BudgetScopeVPS, scopeID: "vps_a", wantSpend: 10},
		{name: "no match", scopeType: BudgetScopeVPS, scopeID: "vps_missing", wantSpend: 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			budget := testBudget("CNY", new(float64(100)), nil)
			budget.ScopeType = string(tt.scopeType)
			budget.ScopeID = tt.scopeID
			got := applyBudgetSpend(rows, []BudgetRecord{budget}, "CNY")[0]
			if got.Status != BudgetStatusOK {
				t.Fatalf("status = %q, want ok", got.Status)
			}
			assertSpendPointer(t, "monthly", got.CurrentMonthlySpend, new(float64(tt.wantSpend)))
			assertSpendPointer(t, "yearly", got.CurrentYearlySpend, new(float64(tt.wantSpend*12)))
		})
	}
}

func TestApplyRowBudgetStatusCombinesMatchingBudgetRisks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		statuses      []BudgetStatus
		want          BudgetStatus
		disabled      bool
		archived      bool
		missingAmount bool
	}{
		{name: "single unknown", statuses: []BudgetStatus{BudgetStatusUnknown}, want: BudgetStatusUnknown},
		{name: "unknown plus ok", statuses: []BudgetStatus{BudgetStatusUnknown, BudgetStatusOK}, want: BudgetStatusUnknown},
		{name: "unknown plus warning", statuses: []BudgetStatus{BudgetStatusUnknown, BudgetStatusWarning}, want: BudgetStatusWarning},
		{name: "unknown plus over", statuses: []BudgetStatus{BudgetStatusUnknown, BudgetStatusOver}, want: BudgetStatusOver},
		{name: "multiple unknown", statuses: []BudgetStatus{BudgetStatusUnknown, BudgetStatusUnknown}, want: BudgetStatusUnknown},
		{name: "all ok", statuses: []BudgetStatus{BudgetStatusOK, BudgetStatusOK}, want: BudgetStatusOK},
		{name: "warning dominates unknown and ok", statuses: []BudgetStatus{BudgetStatusOK, BudgetStatusUnknown, BudgetStatusWarning}, want: BudgetStatusWarning},
		{name: "over dominates every status", statuses: []BudgetStatus{BudgetStatusOK, BudgetStatusUnknown, BudgetStatusWarning, BudgetStatusOver}, want: BudgetStatusOver},
		{name: "unknown plus other status", statuses: []BudgetStatus{BudgetStatusUnknown, BudgetStatusDisabled}, want: BudgetStatusUnknown},
		{name: "ok plus other status", statuses: []BudgetStatus{BudgetStatusOK, BudgetStatusDisabled}, want: BudgetStatusOK},
		{name: "no budgets", want: BudgetStatusUnknown},
		{name: "only disabled budget", statuses: []BudgetStatus{BudgetStatusDisabled}, want: BudgetStatusUnknown, disabled: true},
		{name: "archived row", statuses: []BudgetStatus{BudgetStatusOver}, want: BudgetStatusUnknown, archived: true},
		{name: "row without amount", statuses: []BudgetStatus{BudgetStatusOver}, want: BudgetStatusUnknown, missingAmount: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orders := [][]BudgetStatus{append([]BudgetStatus(nil), tt.statuses...)}
			if len(tt.statuses) > 1 {
				reversed := append([]BudgetStatus(nil), tt.statuses...)
				for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
					reversed[left], reversed[right] = reversed[right], reversed[left]
				}
				orders = append(orders, reversed)
			}
			for order, statuses := range orders {
				row := testCostRow(new(float64(10)), "CNY", "CNY")
				if tt.archived {
					row.LifecycleStatus = "archived"
				}
				if tt.missingAmount {
					row.MonthlyPriceBase = nil
				}
				budgets := make([]BudgetRecord, 0, len(statuses))
				for _, budgetStatus := range statuses {
					budget := testBudget("CNY", new(float64(100)), nil)
					budget.Status = budgetStatus
					if tt.disabled {
						budget.Enabled = false
					}
					budgets = append(budgets, budget)
				}
				rows := []CostRow{row}
				applyRowBudgetStatus(rows, budgets)
				if rows[0].BudgetStatus != tt.want {
					t.Fatalf("order %d status = %q, want %q", order, rows[0].BudgetStatus, tt.want)
				}
			}
		})
	}
}

func TestServiceBudgetSpendUsesSettingsBaseCurrencyAcrossEntrypoints(t *testing.T) {
	t.Parallel()
	settings := defaultCenterSettings()
	settings.SubscriptionCost.BaseCurrency = "USD"
	settingsRepo := &fakeSettingsRepo{settings: settings}
	repo := &fakeSubscriptionCostRepo{}
	service := NewService(repo, settingsRepo, nil)
	ctx := context.Background()

	configureMismatch := func() {
		repo.rows = []CostRow{testCostRow(new(float64(70)), "USD", "CNY")}
		repo.budgets = []BudgetRecord{testBudget("USD", new(float64(100)), nil)}
		repo.budgets[0].BudgetID = "budget_base_currency"
		repo.budgetMonthBuckets = nil
		repo.costMonthBuckets = nil
	}
	assertUnknownBudget := func(t *testing.T, budget BudgetRecord) {
		t.Helper()
		if budget.Status != BudgetStatusUnknown {
			t.Fatalf("status = %q, want unknown", budget.Status)
		}
		if budget.CurrentMonthlySpend != nil || budget.CurrentYearlySpend != nil {
			t.Fatalf("spend = %v/%v, want nil/nil", budget.CurrentMonthlySpend, budget.CurrentYearlySpend)
		}
	}

	t.Run("ListCostRows", func(t *testing.T) {
		configureMismatch()
		if _, err := service.ListCostRows(ctx); err != nil {
			t.Fatalf("ListCostRows() error = %v", err)
		}
		assertUnknownBudget(t, repo.budgets[0])
	})
	t.Run("GetOverview", func(t *testing.T) {
		configureMismatch()
		if _, err := service.GetOverview(ctx); err != nil {
			t.Fatalf("GetOverview() error = %v", err)
		}
		assertUnknownBudget(t, repo.budgets[0])
	})
	t.Run("GetStatistics", func(t *testing.T) {
		configureMismatch()
		got, err := service.GetStatistics(ctx, StatisticsWindowMonth)
		if err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}
		if len(got.BudgetStatuses) != 1 {
			t.Fatalf("budget statuses = %d, want 1", len(got.BudgetStatuses))
		}
		assertUnknownBudget(t, got.BudgetStatuses[0])
	})
	t.Run("ListBudgets", func(t *testing.T) {
		configureMismatch()
		got, err := service.ListBudgets(ctx, BudgetListFilters{})
		if err != nil {
			t.Fatalf("ListBudgets() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("budgets = %d, want 1", len(got))
		}
		assertUnknownBudget(t, got[0])
	})
	t.Run("CreateBudget hydrates", func(t *testing.T) {
		configureMismatch()
		repo.budgets = nil
		got, err := service.CreateBudget(ctx, CreateBudgetInput{
			ScopeType:    string(BudgetScopeGlobal),
			Name:         "created",
			BaseCurrency: "USD",
			MonthlyLimit: new(float64(100)),
			WarningPct:   80,
			Enabled:      true,
		})
		if err != nil {
			t.Fatalf("CreateBudget() error = %v", err)
		}
		assertUnknownBudget(t, got)
	})
	t.Run("PatchBudget mutates and hydrates", func(t *testing.T) {
		repo.rows = []CostRow{testCostRow(new(float64(10)), "USD", "USD")}
		repo.budgets = []BudgetRecord{testBudget("CNY", new(float64(100)), nil)}
		repo.budgets[0].BudgetID = "patchable"
		got, err := service.PatchBudget(ctx, PatchBudgetInput{
			BudgetID:     "patchable",
			BaseCurrency: PatchString("USD"),
			Name:         PatchString("patched"),
		})
		if err != nil {
			t.Fatalf("PatchBudget() error = %v", err)
		}
		if got.Name != "patched" || got.BaseCurrency != "USD" {
			t.Fatalf("patched budget = %#v, want changed name/currency", got)
		}
		if got.MonthlyLimit == nil || *got.MonthlyLimit != 100 {
			t.Fatalf("monthly limit = %v, want preserved 100", got.MonthlyLimit)
		}
		if got.Status != BudgetStatusOK {
			t.Fatalf("status = %q, want ok after patch", got.Status)
		}
		assertSpendPointer(t, "monthly", got.CurrentMonthlySpend, new(float64(10)))
		assertSpendPointer(t, "yearly", got.CurrentYearlySpend, new(float64(120)))
	})
}

func TestMonthlyBudgetRisksReturnSpendPointers(t *testing.T) {
	t.Parallel()
	limit := 100.0
	risks := monthlyBudgetRisks(120, []SeriesPoint{{
		Bucket:           "2026-06",
		BudgetLimit:      &limit,
		BudgetCurrency:   "CNY",
		BudgetWarningPct: 80,
	}})
	if len(risks) != 1 {
		t.Fatalf("monthlyBudgetRisks() returned %d records, want 1", len(risks))
	}
	if risks[0].Status != BudgetStatusOver {
		t.Fatalf("status = %q, want over", risks[0].Status)
	}
	assertSpendPointer(t, "monthly", risks[0].CurrentMonthlySpend, new(float64(120)))
	assertSpendPointer(t, "yearly", risks[0].CurrentYearlySpend, new(float64(1440)))
}

func TestServiceOverviewAggregatesCostsBudgetsAndRenewals(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	monthlyA := 90.0
	yearlyA := 1080.0
	monthlyB := 20.0
	yearlyB := 240.0
	limit := 100.0
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	repo.rows = []CostRow{
		{
			SubscriptionID:    "sub_a",
			VPSID:             "vps_a",
			VPSDisplayName:    "Tokyo Edge",
			ProviderID:        "pv_hetzner",
			ProviderName:      "Hetzner",
			DisplayName:       "Tokyo yearly",
			CostCategory:      "compute",
			Labels:            []string{"edge"},
			Currency:          "USD",
			MonthlyPriceBase:  &monthlyA,
			YearlyPriceBase:   &yearlyA,
			BaseCurrency:      "CNY",
			RenewAt:           datePtr(t, "2026-06-10"),
			ExchangeRateStale: true,
			LifecycleStatus:   "active",
			RenewalDecision:   "keep",
		},
		{
			SubscriptionID:   "sub_b",
			VPSID:            "vps_b",
			VPSDisplayName:   "Frankfurt Legacy",
			ProviderID:       "pv_aws",
			ProviderName:     "AWS",
			CostCategory:     "backup",
			Labels:           []string{"archive"},
			Currency:         "CNY",
			MonthlyPriceBase: &monthlyB,
			YearlyPriceBase:  &yearlyB,
			BaseCurrency:     "CNY",
			RenewAt:          datePtr(t, "2026-07-01"),
			LifecycleStatus:  "to_cancel",
			RenewalDecision:  "cancel",
		},
	}
	repo.missing = []MissingSubscriptionAsset{{VPSID: "vps_missing", DisplayName: "Missing"}}
	repo.budgets = []BudgetRecord{{
		BudgetID:     "budget_global",
		ScopeType:    string(BudgetScopeGlobal),
		Name:         "Global",
		BaseCurrency: "CNY",
		MonthlyLimit: &limit,
		WarningPct:   80,
		Enabled:      true,
	}}
	repo.budgetMonthBuckets = []SeriesPoint{{
		Bucket:           "2026-06",
		BudgetLimit:      &limit,
		BudgetCurrency:   "CNY",
		BudgetWarningPct: 80,
	}}

	overview, err := service.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.BaseCurrency != "CNY" || overview.TotalMonthlyCost != 110 || overview.TotalYearlyCost != 1320 {
		t.Fatalf("overview totals = base %q monthly %.2f yearly %.2f", overview.BaseCurrency, overview.TotalMonthlyCost, overview.TotalYearlyCost)
	}
	if overview.RenewalDue14dCount != 1 || overview.RenewalDue30dCount != 2 {
		t.Fatalf("renewal counts = 14d %d 30d %d, want 1/2", overview.RenewalDue14dCount, overview.RenewalDue30dCount)
	}
	if overview.ExchangeRateStaleCount != 1 || overview.DecisionAttentionCount != 1 || overview.MissingSubscriptionVPSCount != 1 {
		t.Fatalf("signals = stale %d decision %d missing %d, want 1/1/1", overview.ExchangeRateStaleCount, overview.DecisionAttentionCount, overview.MissingSubscriptionVPSCount)
	}
	if overview.BudgetRiskCount != 1 || len(overview.BudgetRisks) != 1 || overview.BudgetRisks[0].Status != BudgetStatusOver {
		t.Fatalf("budget risks = count %d rows %#v, want one over risk", overview.BudgetRiskCount, overview.BudgetRisks)
	}
	if len(overview.UpcomingRenewals) != 2 || overview.UpcomingRenewals[0].SubscriptionID != "sub_a" {
		t.Fatalf("upcoming renewals = %#v, want sorted sub_a first", overview.UpcomingRenewals)
	}
	if len(overview.ProviderBreakdown) != 2 || overview.ProviderBreakdown[0].Label != "Hetzner" {
		t.Fatalf("provider breakdown = %#v, want Hetzner first by monthly cost", overview.ProviderBreakdown)
	}
	if overview.VPSCosts[0].BudgetStatus != BudgetStatusOver || overview.VPSCosts[1].BudgetStatus != BudgetStatusOver {
		t.Fatalf("row budget statuses = %q/%q, want over/over", overview.VPSCosts[0].BudgetStatus, overview.VPSCosts[1].BudgetStatus)
	}
}

// 前端以 snapshot_generated_at 的 UTC 日期计算续费剩余天数，窗口“今天”必须与之同源。
func TestServiceOverviewRenewalWindowSharesSnapshotInstant(t *testing.T) {
	ctx := context.Background()
	service, repo := newTestService()
	monthly := 10.0
	// 时钟在 UTC 午夜两侧交替：窗口与生成时间只要分两次读取，必然落在不同日期。
	calls := 0
	service.now = func() time.Time {
		calls++
		if calls%2 == 1 {
			return time.Date(2026, 6, 2, 23, 59, 59, 0, time.UTC)
		}
		return time.Date(2026, 6, 3, 0, 0, 1, 0, time.UTC)
	}
	repo.rows = []CostRow{{
		SubscriptionID:   "sub_today",
		VPSID:            "vps_today",
		Currency:         "CNY",
		MonthlyPriceBase: &monthly,
		BaseCurrency:     "CNY",
		RenewAt:          datePtr(t, "2026-06-02"),
		LifecycleStatus:  "active",
		RenewalDecision:  "keep",
	}}

	overview, err := service.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	generatedDay := overview.SnapshotGeneratedAt.UTC().Format("2006-01-02")
	included := len(overview.UpcomingRenewals) == 1
	if generatedDay == "2026-06-02" && !included {
		t.Fatalf("snapshot day %s but renewal due that day was excluded", generatedDay)
	}
	if generatedDay == "2026-06-03" && included {
		t.Fatalf("snapshot day %s but renewal due the previous day was included", generatedDay)
	}
}

func TestServiceStatisticsReturnsCostMonthBuckets(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	monthly := 90.0
	yearly := 1080.0
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	repo.rows = []CostRow{{
		SubscriptionID:   "sub_a",
		VPSID:            "vps_a",
		VPSDisplayName:   "Tokyo Edge",
		ProviderID:       "pv_hetzner",
		ProviderName:     "Hetzner",
		CostCategory:     "compute",
		Currency:         "USD",
		MonthlyPriceBase: &monthly,
		YearlyPriceBase:  &yearly,
		RenewAt:          datePtr(t, "2026-06-10"),
		PaymentMethod:    "card",
		Country:          "JP",
		Region:           "Tokyo",
	}}
	repo.costMonthBuckets = []SeriesPoint{
		{Bucket: "2025-07", MonthlyCost: 40},
		{Bucket: "2025-08", MonthlyCost: 60, DataInsufficient: true},
		{Bucket: "2026-06", MonthlyCost: 90},
	}
	budgetLimit := 100.0
	repo.budgetMonthBuckets = []SeriesPoint{
		{Bucket: "2025-07", BudgetLimit: &budgetLimit, BudgetCurrency: "CNY", BudgetWarningPct: 80},
		{Bucket: "2025-08", BudgetLimit: &budgetLimit, BudgetCurrency: "USD", BudgetWarningPct: 80, DataInsufficient: true},
		{Bucket: "2026-06", BudgetLimit: &budgetLimit, BudgetCurrency: "CNY", BudgetWarningPct: 80},
	}

	stats, err := service.GetStatistics(ctx, StatisticsWindowYear)
	if err != nil {
		t.Fatalf("GetStatistics() error = %v", err)
	}
	if repo.costBucketMonths != 12 {
		t.Fatalf("cost bucket months = %d, want 12", repo.costBucketMonths)
	}
	if !repo.costBucketNow.Equal(now) {
		t.Fatalf("cost bucket now = %v, want %v", repo.costBucketNow, now)
	}
	if len(stats.CostMonthBuckets) != 3 || stats.CostMonthBuckets[2].Bucket != "2026-06" || stats.CostMonthBuckets[2].MonthlyCost != 90 {
		t.Fatalf("cost month buckets = %#v, want populated historical series", stats.CostMonthBuckets)
	}
	if !stats.CostMonthBuckets[1].DataInsufficient {
		t.Fatalf("cost month bucket = %#v, want data_insufficient passthrough", stats.CostMonthBuckets[1])
	}
	if stats.CostMonthBuckets[2].BudgetLimit == nil || *stats.CostMonthBuckets[2].BudgetLimit != 100 || stats.CostMonthBuckets[2].BudgetCurrency != "CNY" {
		t.Fatalf("merged budget bucket = %#v, want CNY 100 budget", stats.CostMonthBuckets[2])
	}
	if len(stats.PaymentBreakdown) != 1 || stats.PaymentBreakdown[0].Label != "card" {
		t.Fatalf("payment breakdown = %#v, want card", stats.PaymentBreakdown)
	}
	if len(stats.RegionBreakdown) != 1 || stats.RegionBreakdown[0].Label != "JP / Tokyo" {
		t.Fatalf("region breakdown = %#v, want JP / Tokyo", stats.RegionBreakdown)
	}
	if len(stats.RenewalMonthBuckets) != 12 {
		t.Fatalf("renewal month buckets = %d, want 12", len(stats.RenewalMonthBuckets))
	}
}

func TestServiceMonthlyBudgetRisksIgnoreCurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	monthly := 120.0
	yearly := 1440.0
	limit := 100.0
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	repo.rows = []CostRow{{
		SubscriptionID:   "sub_a",
		VPSID:            "vps_a",
		VPSDisplayName:   "Tokyo Edge",
		Currency:         "USD",
		MonthlyPriceBase: &monthly,
		YearlyPriceBase:  &yearly,
		BaseCurrency:     "CNY",
	}}
	repo.budgetMonthBuckets = []SeriesPoint{{
		Bucket:           "2026-06",
		BudgetLimit:      &limit,
		BudgetCurrency:   "USD",
		BudgetWarningPct: 80,
		DataInsufficient: true,
	}}

	overview, err := service.GetOverview(ctx)
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.BudgetRiskCount != 0 || len(overview.BudgetRisks) != 0 {
		t.Fatalf("budget risks = count %d rows %#v, want ignored for currency mismatch", overview.BudgetRiskCount, overview.BudgetRisks)
	}
}

func TestServiceBulkUpsertMonthlyBudgetsCurrentYear(t *testing.T) {
	service, repo := newTestService()
	service.now = func() time.Time { return time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC) }

	result, err := service.BulkUpsertMonthlyBudgets(context.Background(), BulkUpsertMonthlyBudgetInput{
		Scope:        MonthlyBudgetBulkScopeCurrentYear,
		BaseCurrency: "usd",
		MonthlyLimit: 88.5,
		WarningPct:   70,
		Note:         " baseline ",
	})
	if err != nil {
		t.Fatalf("BulkUpsertMonthlyBudgets() error = %v", err)
	}
	if result.StartMonth.Time.Format("2006-01-02") != "2026-01-01" || result.EndMonth.Time.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("range = %s..%s, want current year through current month", result.StartMonth.Time.Format("2006-01-02"), result.EndMonth.Time.Format("2006-01-02"))
	}
	if len(repo.upsertMonthlyBudgetInputs) != 6 || len(result.Records) != 6 {
		t.Fatalf("upserted %d inputs and %d records, want 6", len(repo.upsertMonthlyBudgetInputs), len(result.Records))
	}
	first := repo.upsertMonthlyBudgetInputs[0]
	last := repo.upsertMonthlyBudgetInputs[len(repo.upsertMonthlyBudgetInputs)-1]
	if first.BudgetMonth.Time.Format("2006-01-02") != "2026-01-01" || last.BudgetMonth.Time.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("months = %s..%s, want Jan..Jun", first.BudgetMonth.Time.Format("2006-01-02"), last.BudgetMonth.Time.Format("2006-01-02"))
	}
	if first.BaseCurrency != "USD" || first.MonthlyLimit != 88.5 || first.WarningPct != 70 || first.Note != "baseline" {
		t.Fatalf("normalized first input = %#v", first)
	}
}

func TestServiceBulkUpsertMonthlyBudgetsRecentYear(t *testing.T) {
	service, repo := newTestService()
	service.now = func() time.Time { return time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC) }

	result, err := service.BulkUpsertMonthlyBudgets(context.Background(), BulkUpsertMonthlyBudgetInput{
		Scope:        MonthlyBudgetBulkScopeRecentYear,
		BaseCurrency: "CNY",
		MonthlyLimit: 100,
	})
	if err != nil {
		t.Fatalf("BulkUpsertMonthlyBudgets() error = %v", err)
	}
	if result.StartMonth.Time.Format("2006-01-02") != "2025-07-01" || result.EndMonth.Time.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("range = %s..%s, want latest 12 months", result.StartMonth.Time.Format("2006-01-02"), result.EndMonth.Time.Format("2006-01-02"))
	}
	if len(repo.upsertMonthlyBudgetInputs) != 12 {
		t.Fatalf("upserted %d months, want 12", len(repo.upsertMonthlyBudgetInputs))
	}
	if repo.upsertMonthlyBudgetInputs[0].WarningPct != 80 {
		t.Fatalf("default warning = %d, want 80", repo.upsertMonthlyBudgetInputs[0].WarningPct)
	}
}

func TestServiceBulkUpsertMonthlyBudgetsAllHistoryUsesEarliestSubscriptionMonth(t *testing.T) {
	service, repo := newTestService()
	service.now = func() time.Time { return time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC) }
	earliest := subscriptions.NewDate(time.Date(2025, time.March, 18, 9, 0, 0, 0, time.UTC))
	repo.earliestSubscriptionMonth = &earliest

	result, err := service.BulkUpsertMonthlyBudgets(context.Background(), BulkUpsertMonthlyBudgetInput{
		Scope:        MonthlyBudgetBulkScopeAllHistory,
		BaseCurrency: "CNY",
		MonthlyLimit: 100,
	})
	if err != nil {
		t.Fatalf("BulkUpsertMonthlyBudgets() error = %v", err)
	}
	if result.StartMonth.Time.Format("2006-01-02") != "2025-03-01" || result.EndMonth.Time.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("range = %s..%s, want earliest subscription month through current month", result.StartMonth.Time.Format("2006-01-02"), result.EndMonth.Time.Format("2006-01-02"))
	}
	if len(repo.upsertMonthlyBudgetInputs) != 16 {
		t.Fatalf("upserted %d months, want 16", len(repo.upsertMonthlyBudgetInputs))
	}
}

func TestServiceBulkUpsertMonthlyBudgetsRejectsInvalidScope(t *testing.T) {
	service, repo := newTestService()

	_, err := service.BulkUpsertMonthlyBudgets(context.Background(), BulkUpsertMonthlyBudgetInput{
		Scope:        "future",
		BaseCurrency: "CNY",
		MonthlyLimit: 100,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("BulkUpsertMonthlyBudgets() error = %v, want ErrInvalidInput", err)
	}
	if len(repo.upsertMonthlyBudgetInputs) != 0 {
		t.Fatalf("upserted inputs despite invalid scope: %#v", repo.upsertMonthlyBudgetInputs)
	}
}

func TestServiceRefreshExchangeRatesSanitizesProviderErrors(t *testing.T) {
	ctx := context.Background()
	service, repo := newTestService()
	service.providers["frankfurter"] = fakeProvider{
		errByQuote: map[string]error{
			"USD": errors.New("upstream failure with access_key=super-secret-value " + strings.Repeat("x", 200)),
		},
		rateByQuote: map[string]FetchedExchangeRate{
			"EUR": {Rate: 7.5, RateDate: *datePtr(t, "2026-06-02")},
		},
	}
	repo.currencies = []string{"CNY", " usd ", "EUR"}

	result, err := service.RefreshExchangeRates(ctx)
	if err != nil {
		t.Fatalf("RefreshExchangeRates() error = %v", err)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0].QuoteCurrency != "EUR" {
		t.Fatalf("succeeded = %#v, want EUR only", result.Succeeded)
	}
	if len(repo.upserts) != 1 || repo.upserts[0].QuoteCurrency != "EUR" {
		t.Fatalf("upserts = %#v, want EUR only", repo.upserts)
	}
	if len(result.Failed) != 1 || result.Failed[0].QuoteCurrency != "USD" {
		t.Fatalf("failed = %#v, want USD only", result.Failed)
	}
	if len(result.Failed[0].Error) > 160 {
		t.Fatalf("provider error length = %d, want <= 160", len(result.Failed[0].Error))
	}
	if strings.Contains(result.Failed[0].Error, "super-secret-value") {
		t.Fatalf("provider error = %q, leaked provider secret", result.Failed[0].Error)
	}
	if !strings.Contains(result.Failed[0].Error, "access_key=[redacted]") {
		t.Fatalf("provider error = %q, want redacted access_key", result.Failed[0].Error)
	}
}

func TestReminderServiceDedupesDeliveriesBeforeAudit(t *testing.T) {
	ctx := context.Background()
	monthly := 88.0
	renewAt := *datePtr(t, "2026-06-16")
	repo := &fakeSubscriptionCostRepo{
		candidates: []ReminderCandidate{{
			SubscriptionID:   "sub_001",
			VPSID:            "vps_001",
			VPSDisplayName:   "Tokyo Edge",
			RenewAt:          renewAt,
			OffsetDays:       14,
			Kind:             ReminderKindRenewal,
			BaseCurrency:     "CNY",
			MonthlyPriceBase: &monthly,
			RenewalDecision:  "keep",
			LifecycleStatus:  "active",
		}},
	}
	settings := &fakeSettingsRepo{settings: defaultCenterSettings()}
	dispatcher := &fakeDispatcher{deliveries: []incidents.NotificationDelivery{{
		Channel: incidents.NotificationChannelTelegram,
		Status:  incidents.DeliveryStatusSent,
	}}}
	audit := &fakeNotificationAudit{}
	service := NewReminderService(repo, settings, dispatcher, audit, slog.Default())

	if err := service.Scan(ctx); err != nil {
		t.Fatalf("first Scan() error = %v", err)
	}
	if err := service.Scan(ctx); err != nil {
		t.Fatalf("second Scan() error = %v", err)
	}
	if repo.deliveryAttempts != 2 {
		t.Fatalf("delivery attempts = %d, want two scans", repo.deliveryAttempts)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher calls = %d, want one after delivery reservation dedupe", dispatcher.calls)
	}
	if len(audit.records) != 1 {
		t.Fatalf("audit records = %d, want one after dedupe", len(audit.records))
	}
	if audit.records[0].ObjectType != incidents.ObjectTypeSubscription || audit.records[0].ObjectID != "sub_001" {
		t.Fatalf("audit object = %s/%s, want subscription sub_001", audit.records[0].ObjectType, audit.records[0].ObjectID)
	}
	if !strings.Contains(audit.records[0].Summary, "订阅续费提醒") {
		t.Fatalf("summary = %q, want subscription reminder text", audit.records[0].Summary)
	}
}

func newTestService() (*Service, *fakeSubscriptionCostRepo) {
	repo := &fakeSubscriptionCostRepo{}
	settings := &fakeSettingsRepo{settings: defaultCenterSettings()}
	service := NewService(repo, settings, map[string]ExchangeRateProvider{"frankfurter": fakeProvider{}})
	return service, repo
}

func TestServicePutSettingsUsesAtomicLatestValueCallback(t *testing.T) {
	settings := centersettings.Default()
	settings.SubscriptionCost.FixerAPIKey = "existing-secret"
	repo := &fakeSettingsRepo{settings: settings}
	service := NewService(&fakeSubscriptionCostRepo{}, repo, nil)
	calls := 0

	got, err := service.PutSettings(context.Background(), func(current centersettings.SubscriptionCostSettings) (centersettings.SubscriptionCostSettings, error) {
		calls++
		if current.FixerAPIKey != "existing-secret" {
			t.Fatalf("callback FixerAPIKey = %q, want existing secret", current.FixerAPIKey)
		}
		current.BaseCurrency = "USD"
		return current, nil
	})
	if err != nil {
		t.Fatalf("PutSettings() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1", calls)
	}
	if got.BaseCurrency != "USD" || got.FixerAPIKey != "existing-secret" {
		t.Fatalf("PutSettings() = %#v, want updated currency with preserved secret", got)
	}
}

func TestServicePutSettingsRejectsNilCallback(t *testing.T) {
	service, _ := newTestService()
	if _, err := service.PutSettings(context.Background(), nil); !errors.Is(err, centersettings.ErrInvalidSettings) {
		t.Fatalf("PutSettings(nil) error = %v, want ErrInvalidSettings", err)
	}
}

func defaultCenterSettings() centersettings.CenterSettings {
	return centersettings.Default()
}

func datePtr(t *testing.T, value string) *subscriptions.Date {
	t.Helper()
	date, err := subscriptions.ParseDate(value)
	if err != nil {
		t.Fatalf("parse date %q: %v", value, err)
	}
	return &date
}

func testCostRow(monthly *float64, currency, baseCurrency string) CostRow {
	return CostRow{
		MonthlyPriceBase: monthly,
		Currency:         currency,
		BaseCurrency:     baseCurrency,
		LifecycleStatus:  "active",
	}
}

func testBudget(baseCurrency string, monthlyLimit, yearlyLimit *float64) BudgetRecord {
	return BudgetRecord{
		ScopeType:    string(BudgetScopeGlobal),
		BaseCurrency: baseCurrency,
		MonthlyLimit: monthlyLimit,
		YearlyLimit:  yearlyLimit,
		WarningPct:   80,
		Enabled:      true,
	}
}

func assertSpendPointer(t *testing.T, field string, got, want *float64) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s spend = %v, want nil", field, got)
		}
		return
	}
	if got == nil {
		t.Fatalf("%s spend = nil, want %.2f", field, *want)
	}
	if *got != *want {
		t.Fatalf("%s spend = %.2f, want %.2f", field, *got, *want)
	}
}

type fakeSettingsRepo struct {
	settings centersettings.CenterSettings
}

func (r *fakeSettingsRepo) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	return r.settings, nil
}

func (r *fakeSettingsRepo) MutateSettings(_ context.Context, mutate centersettings.MutateSettingsFunc) (centersettings.CenterSettings, error) {
	next, err := mutate(r.settings)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}
	r.settings = next
	return r.settings, nil
}

type fakeSubscriptionCostRepo struct {
	rows                      []CostRow
	costMonthBuckets          []SeriesPoint
	budgetMonthBuckets        []SeriesPoint
	costBucketMonths          int
	costBucketNow             time.Time
	budgetBucketMonths        int
	budgetBucketNow           time.Time
	missing                   []MissingSubscriptionAsset
	budgets                   []BudgetRecord
	monthlyBudgets            []MonthlyBudgetRecord
	upsertMonthlyBudgetInput  UpsertMonthlyBudgetInput
	upsertMonthlyBudgetRecord MonthlyBudgetRecord
	earliestSubscriptionMonth *subscriptions.Date
	upsertMonthlyBudgetInputs []UpsertMonthlyBudgetInput
	currencies                []string
	upserts                   []ExchangeRateUpsert
	candidates                []ReminderCandidate
	deliveryKeys              map[string]string
	deliveryAttempts          int
}

func (r *fakeSubscriptionCostRepo) ListCostRows(context.Context, centersettings.SubscriptionCostSettings) ([]CostRow, error) {
	return r.rows, nil
}

func (r *fakeSubscriptionCostRepo) ListCostMonthBuckets(_ context.Context, _ centersettings.SubscriptionCostSettings, months int, now time.Time) ([]SeriesPoint, error) {
	r.costBucketMonths = months
	r.costBucketNow = now
	return r.costMonthBuckets, nil
}

func (r *fakeSubscriptionCostRepo) ListBudgetMonthBuckets(_ context.Context, _ centersettings.SubscriptionCostSettings, months int, now time.Time) ([]SeriesPoint, error) {
	r.budgetBucketMonths = months
	r.budgetBucketNow = now
	return r.budgetMonthBuckets, nil
}

func (r *fakeSubscriptionCostRepo) ListMissingSubscriptionAssets(context.Context) ([]MissingSubscriptionAsset, error) {
	return r.missing, nil
}

func (r *fakeSubscriptionCostRepo) ListBudgets(context.Context, BudgetListFilters) ([]BudgetRecord, error) {
	return r.budgets, nil
}

func (r *fakeSubscriptionCostRepo) CreateBudget(_ context.Context, input CreateBudgetInput) (BudgetRecord, error) {
	record := BudgetRecord{
		BudgetID:     "budget_created",
		ScopeType:    input.ScopeType,
		ScopeID:      input.ScopeID,
		Name:         input.Name,
		BaseCurrency: input.BaseCurrency,
		MonthlyLimit: input.MonthlyLimit,
		YearlyLimit:  input.YearlyLimit,
		WarningPct:   input.WarningPct,
		Enabled:      input.Enabled,
		Note:         input.Note,
	}
	r.budgets = append(r.budgets, record)
	return record, nil
}

func (r *fakeSubscriptionCostRepo) PatchBudget(_ context.Context, input PatchBudgetInput) (BudgetRecord, error) {
	for i := range r.budgets {
		if r.budgets[i].BudgetID != input.BudgetID {
			continue
		}
		record := r.budgets[i]
		if input.ScopeType.Set {
			record.ScopeType = input.ScopeType.Value
		}
		if input.ScopeID.Set {
			record.ScopeID = input.ScopeID.Value
		}
		if input.Name.Set {
			record.Name = input.Name.Value
		}
		if input.BaseCurrency.Set {
			record.BaseCurrency = input.BaseCurrency.Value
		}
		if input.MonthlyLimit.Set {
			record.MonthlyLimit = input.MonthlyLimit.Value
		}
		if input.YearlyLimit.Set {
			record.YearlyLimit = input.YearlyLimit.Value
		}
		if input.WarningPct.Set {
			record.WarningPct = input.WarningPct.Value
		}
		if input.Enabled.Set {
			record.Enabled = input.Enabled.Value
		}
		if input.Note.Set {
			record.Note = input.Note.Value
		}
		r.budgets[i] = record
		return record, nil
	}
	return BudgetRecord{}, ErrBudgetNotFound
}

func (r *fakeSubscriptionCostRepo) ListMonthlyBudgets(context.Context) ([]MonthlyBudgetRecord, error) {
	return r.monthlyBudgets, nil
}

func (r *fakeSubscriptionCostRepo) UpsertMonthlyBudget(_ context.Context, input UpsertMonthlyBudgetInput) (MonthlyBudgetRecord, error) {
	r.upsertMonthlyBudgetInput = input
	if r.upsertMonthlyBudgetRecord.BudgetMonth.Time.IsZero() {
		return MonthlyBudgetRecord{
			BudgetMonth:  input.BudgetMonth,
			BaseCurrency: input.BaseCurrency,
			MonthlyLimit: input.MonthlyLimit,
			WarningPct:   input.WarningPct,
			Note:         input.Note,
		}, nil
	}
	return r.upsertMonthlyBudgetRecord, nil
}

func (r *fakeSubscriptionCostRepo) EarliestSubscriptionMonth(context.Context) (*subscriptions.Date, error) {
	return r.earliestSubscriptionMonth, nil
}

func (r *fakeSubscriptionCostRepo) UpsertMonthlyBudgets(_ context.Context, inputs []UpsertMonthlyBudgetInput) ([]MonthlyBudgetRecord, error) {
	r.upsertMonthlyBudgetInputs = append([]UpsertMonthlyBudgetInput(nil), inputs...)
	records := make([]MonthlyBudgetRecord, 0, len(inputs))
	for _, input := range inputs {
		records = append(records, MonthlyBudgetRecord{
			BudgetMonth:  input.BudgetMonth,
			BaseCurrency: input.BaseCurrency,
			MonthlyLimit: input.MonthlyLimit,
			WarningPct:   input.WarningPct,
			Note:         input.Note,
		})
	}
	return records, nil
}

func (r *fakeSubscriptionCostRepo) ListActiveCurrencies(context.Context) ([]string, error) {
	return r.currencies, nil
}

func (r *fakeSubscriptionCostRepo) UpsertExchangeRate(_ context.Context, input ExchangeRateUpsert) (ExchangeRateRecord, error) {
	r.upserts = append(r.upserts, input)
	return ExchangeRateRecord{
		Provider:      input.Provider,
		BaseCurrency:  input.BaseCurrency,
		QuoteCurrency: input.QuoteCurrency,
		Rate:          input.Rate,
		RateDate:      input.RateDate,
		FetchedAt:     input.FetchedAt,
	}, nil
}

func (r *fakeSubscriptionCostRepo) ListReminderCandidates(context.Context, centersettings.SubscriptionCostSettings, []int) ([]ReminderCandidate, error) {
	return r.candidates, nil
}

func (r *fakeSubscriptionCostRepo) TryCreateReminderDelivery(_ context.Context, input ReminderDeliveryInput) (string, bool, error) {
	r.deliveryAttempts++
	if r.deliveryKeys == nil {
		r.deliveryKeys = map[string]string{}
	}
	key := input.SubscriptionID + "|" + input.RenewAt.Time.Format("2006-01-02") + "|" + strconv.Itoa(input.OffsetDays)
	if existing, ok := r.deliveryKeys[key]; ok {
		return existing, false, nil
	}
	deliveryID := "delivery_" + input.SubscriptionID
	r.deliveryKeys[key] = deliveryID
	return deliveryID, true, nil
}

func (r *fakeSubscriptionCostRepo) UpdateReminderDelivery(context.Context, string, ReminderDeliveryUpdate) error {
	return nil
}

type fakeProvider struct {
	rateByQuote map[string]FetchedExchangeRate
	errByQuote  map[string]error
}

func (p fakeProvider) FetchRate(_ context.Context, quoteCurrency, _ string) (FetchedExchangeRate, error) {
	quoteCurrency = strings.ToUpper(strings.TrimSpace(quoteCurrency))
	if err := p.errByQuote[quoteCurrency]; err != nil {
		return FetchedExchangeRate{}, err
	}
	if rate, ok := p.rateByQuote[quoteCurrency]; ok {
		return rate, nil
	}
	return FetchedExchangeRate{}, errors.New("missing fake rate")
}

type fakeDispatcher struct {
	deliveries []incidents.NotificationDelivery
	calls      int
}

func (d *fakeDispatcher) Dispatch(context.Context, string) []incidents.NotificationDelivery {
	d.calls++
	return d.deliveries
}

type fakeNotificationAudit struct {
	records []incidents.NotificationRecordWrite
}

func (a *fakeNotificationAudit) AppendNotificationRecords(_ context.Context, records []incidents.NotificationRecordWrite) error {
	a.records = append(a.records, records...)
	return nil
}
