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
	if repo.rows[0].BudgetStatus != BudgetStatusOver || repo.rows[1].BudgetStatus != BudgetStatusOver {
		t.Fatalf("row budget statuses = %q/%q, want over/over", repo.rows[0].BudgetStatus, repo.rows[1].BudgetStatus)
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

type fakeSettingsRepo struct {
	settings centersettings.CenterSettings
}

func (r *fakeSettingsRepo) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	return r.settings, nil
}

func (r *fakeSettingsRepo) PutSettings(_ context.Context, settings centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	r.settings = settings
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
	return BudgetRecord{
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
	}, nil
}

func (r *fakeSubscriptionCostRepo) PatchBudget(context.Context, PatchBudgetInput) (BudgetRecord, error) {
	return BudgetRecord{}, nil
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
