package adapters

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var _ evidence.Kind = (*SubscriptionCostAdapter)(nil)

func TestSubscriptionCostAdapterFreezesRateBudgetAndCoverage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	capture := validSubscriptionCostCapture(window, now)
	adapter, err := NewSubscriptionCostAdapter(staticSubscriptionCostSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_222222222222222222222222", nil }})
	if err != nil {
		t.Fatalf("NewSubscriptionCostAdapter() error = %v", err)
	}
	selection := evidence.Selection{Key: evidence.SubscriptionCostV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: window}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.Quality.Status != evidence.QualityPartial || !preview.Quality.Partial || preview.Units.Values["base_amount"] != "CNY" {
		t.Fatalf("preview = %#v, want partial coverage and frozen base currency", preview)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	for _, required := range []string{`"original_amount":20`, `"original_currency":"USD"`, `"billing_period_unit":"month"`, `"conversion_provider":"frankfurter"`, `"rate_date":"2026-07-18"`, `"base_currency":"CNY"`, `"budget_source":"subscription_monthly_budgets"`, `"budget_currency":"CNY"`, `"budget_status":"warning"`, `"coverage_status":"partial"`} {
		if !containsBytes(snapshot.Bytes(), required) {
			t.Fatalf("snapshot = %s, want %s", snapshot.Bytes(), required)
		}
	}
	summary := adapter.Summarize(snapshot)
	if summary.ReadModel["version"] != "subscription_cost_read_model/v1" {
		t.Fatalf("summary = %#v, want versioned cost model", summary)
	}
	comparison := adapter.Compare(snapshot, snapshot, evidence.Alignment{Mode: evidence.AlignmentExact})
	if !comparison.Compatible || comparison.Values["version"] != "subscription_cost_comparison/v1" {
		t.Fatalf("comparison = %#v, want versioned cost comparison", comparison)
	}
	for _, field := range []string{"original_amount_delta", "base_amount_delta", "budget_actual_spend_delta", "missing_rate_count_delta", "budget_status_changed", "rate_stale_changed"} {
		if _, exists := comparison.Values[field]; !exists {
			t.Fatalf("comparison = %#v, want allowlisted %q", comparison.Values, field)
		}
	}
	if err := evidence.VerifyKindConformance(context.Background(), adapter, evidence.ConformanceFixture{Actor: monitoringTestActor(t), Selection: selection, Intent: evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil}, Alignment: evidence.Alignment{Mode: evidence.AlignmentExact}, ExportMode: evidence.ExportModeSafe}); err != nil {
		t.Fatalf("VerifyKindConformance() error = %v", err)
	}
}

func TestSubscriptionCostAdapterRejectsMalformedCustomSourceFacts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name   string
		mutate func(*SubscriptionCostCapture)
	}{
		{name: "source mismatch", mutate: func(c *SubscriptionCostCapture) { c.VPSID = "vps_other" }},
		{name: "invalid currency", mutate: func(c *SubscriptionCostCapture) { c.OriginalCurrency = "usd" }},
		{name: "invalid billing period", mutate: func(c *SubscriptionCostCapture) { c.BillingPeriodUnit = "fortnight" }},
		{name: "invalid rate", mutate: func(c *SubscriptionCostCapture) { c.ConversionRate = 0 }},
		{name: "identity conversion across currencies", mutate: func(c *SubscriptionCostCapture) {
			c.ConversionProvider = "identity"
			c.ConversionRate = 1
			c.BaseAmount = c.OriginalAmount
		}},
		{name: "future rate fetch", mutate: func(c *SubscriptionCostCapture) { c.RateFetchedAt = now.Add(time.Microsecond) }},
		{name: "rate date after rate fetch", mutate: func(c *SubscriptionCostCapture) {
			c.RateFetchedAt = time.Date(2026, time.July, 17, 23, 59, 0, 0, time.UTC)
		}},
		{name: "submicrosecond observation", mutate: func(c *SubscriptionCostCapture) { c.ObservedAt = c.ObservedAt.Add(time.Nanosecond) }},
		{name: "non canonical UTC observation", mutate: func(c *SubscriptionCostCapture) {
			c.ObservedAt = c.ObservedAt.In(time.FixedZone("offset", 8*60*60))
		}},
		{name: "budget currency drift", mutate: func(c *SubscriptionCostCapture) { c.BudgetCurrency = "USD" }},
		{name: "invalid budget status", mutate: func(c *SubscriptionCostCapture) { c.BudgetStatus = "almost" }},
		{name: "warning at exact budget limit", mutate: func(c *SubscriptionCostCapture) {
			c.BudgetActualSpend = c.BudgetMonthlyLimit
			c.BudgetStatus = "warning"
		}},
		{name: "known zero budget", mutate: func(c *SubscriptionCostCapture) {
			c.BudgetMonthlyLimit = 0
			c.BudgetActualSpend = 0
			c.BudgetStatus = "warning"
		}},
		{name: "known budget despite missing rate", mutate: func(c *SubscriptionCostCapture) {
			c.MissingRateCount = 1
			c.BudgetStatus = "warning"
			c.CoverageStatus = "missing_rate"
		}},
		{name: "coverage count drift", mutate: func(c *SubscriptionCostCapture) { c.CoveredDays++ }},
		{name: "amount conversion drift", mutate: func(c *SubscriptionCostCapture) { c.BaseAmount++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := validSubscriptionCostCapture(window, now)
			tt.mutate(&capture)
			adapter, err := NewSubscriptionCostAdapter(staticSubscriptionCostSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatalf("NewSubscriptionCostAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.SubscriptionCostV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: window})
			if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
				t.Fatalf("PreviewCapture() error = %v, want ErrInvalidCanonicalPayload", err)
			}
		})
	}
}

func TestSubscriptionCostAdapterAcceptsUnknownZeroBudget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	capture := validSubscriptionCostCapture(window, now)
	capture.BudgetMonthlyLimit = 0
	capture.BudgetActualSpend = 0
	capture.BudgetStatus = "unknown"
	adapter, err := NewSubscriptionCostAdapter(staticSubscriptionCostSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSubscriptionCostAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.SubscriptionCostV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: window})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v, want zero budget to preserve authoritative unknown status", err)
	}
}

func TestSubscriptionCostAdapterAcceptsInheritedMonthlyBudget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)}
	capture := validSubscriptionCostCapture(window, now)
	capture.BudgetMonth = "2026-06"
	adapter, err := NewSubscriptionCostAdapter(staticSubscriptionCostSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSubscriptionCostAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.SubscriptionCostV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef", RequestedWindow: window})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v, want inherited prior-month budget accepted", err)
	}
}

type staticSubscriptionCostSource struct{ capture SubscriptionCostCapture }

func (source staticSubscriptionCostSource) LoadSubscriptionCostEvidence(context.Context, string, evidence.TimeWindow) (SubscriptionCostCapture, error) {
	return source.capture, nil
}

func validSubscriptionCostCapture(window evidence.TimeWindow, now time.Time) SubscriptionCostCapture {
	return SubscriptionCostCapture{
		SubscriptionID: "sub_0123456789abcdef", VPSID: "vps_0123456789abcdef", SourceRevision: "sub_0123456789abcdef/rate_01/2026-07", ProducerVersion: "subscription-cost-store/v1", ObservedAt: now.Add(-time.Hour), SourceWatermark: now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
		OriginalAmount: 20, OriginalCurrency: "USD", BillingPeriodUnit: "month", BillingPeriodLength: 1,
		ConversionRate: 7.2, ConversionProvider: "frankfurter", RateDate: "2026-07-18", RateFetchedAt: now.Add(-2 * time.Hour), RateStale: false,
		BaseAmount: 144, BaseCurrency: "CNY", BudgetSource: "subscription_monthly_budgets", BudgetCurrency: "CNY", BudgetMonth: "2026-07", BudgetMonthlyLimit: 1000, BudgetWarningPct: 80, BudgetStatus: "warning", BudgetActualSpend: 850,
		CoverageStart: window.Start.AddDate(0, 0, 1), CoverageEnd: window.End, CoverageStatus: "partial", CoveredDays: 30, TotalDays: 31, ConvertedSubscriptionCount: 4, MissingRateCount: 0,
	}
}
