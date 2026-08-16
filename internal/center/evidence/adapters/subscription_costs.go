package adapters

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

const subscriptionCostCalculationVersion = "subscription-cost-evidence/v1"

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type SubscriptionCostSource interface {
	LoadSubscriptionCostEvidence(context.Context, string, evidence.TimeWindow) (SubscriptionCostCapture, error)
}

type SubscriptionCostCapture struct {
	SubscriptionID             string
	VPSID                      string
	SourceRevision             string
	ProducerVersion            string
	ObservedAt                 time.Time
	SourceWatermark            string
	OriginalAmount             float64
	OriginalCurrency           string
	BillingPeriodUnit          string
	BillingPeriodLength        int
	ConversionRate             float64
	ConversionProvider         string
	RateDate                   string
	RateFetchedAt              time.Time
	RateStale                  bool
	BaseAmount                 float64
	BaseCurrency               string
	BudgetSource               string
	BudgetCurrency             string
	BudgetMonth                string
	BudgetMonthlyLimit         float64
	BudgetWarningPct           int
	BudgetStatus               string
	BudgetActualSpend          float64
	CoverageStart              time.Time
	CoverageEnd                time.Time
	CoverageStatus             string
	CoveredDays                int
	TotalDays                  int
	ConvertedSubscriptionCount uint64
	MissingRateCount           uint64
}

type SubscriptionCostAdapter struct {
	source     SubscriptionCostSource
	resolver   EvidenceSourceResolver
	options    AdapterOptions
	descriptor evidence.Descriptor
}

func NewSubscriptionCostAdapter(source SubscriptionCostSource, resolver EvidenceSourceResolver, options AdapterOptions) (*SubscriptionCostAdapter, error) {
	if source == nil || resolver == nil {
		return nil, fmt.Errorf("%w: nil subscription cost adapter dependency", evidence.ErrInvalidKindDescriptor)
	}
	descriptor := subscriptionCostDescriptor()
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &SubscriptionCostAdapter{source: source, resolver: resolver, options: options, descriptor: descriptor}, nil
}

func (adapter *SubscriptionCostAdapter) Descriptor() evidence.Descriptor { return adapter.descriptor }

func (adapter *SubscriptionCostAdapter) ValidateSelection(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) error {
	if adapter == nil || selection.Key != evidence.SubscriptionCostV1Key() || selection.SourceType != string(recordauth.SourceKindVPS) ||
		!validSourceIdentifier(selection.SourceID) || !validEvidenceWindow(selection.RequestedWindow) || !calendarMonthWindow(selection.RequestedWindow) ||
		len(selection.Metrics) != 0 || selection.Precision != 0 || len(selection.SensitiveTopologyFields) != 0 {
		return fmt.Errorf("%w: subscription cost selection", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func (adapter *SubscriptionCostAdapter) PreviewCapture(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.Preview, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.Preview{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, selection)
	if err != nil {
		return evidence.Preview{}, err
	}
	return previewDiscreteEvidence(adapter.options, adapter.descriptor, selection, evaluated)
}

func (adapter *SubscriptionCostAdapter) Capture(ctx context.Context, actor evidence.ActorScope, intent evidence.Intent) (evidence.CanonicalSnapshot, error) {
	if err := validateDiscreteIntent(adapter.descriptor.Key, intent); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	if err := adapter.ValidateSelection(ctx, actor, intent.Selection); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, intent.Selection)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	return captureDiscreteEvidence(adapter.options, adapter.descriptor, intent.Selection, evaluated)
}

func (adapter *SubscriptionCostAdapter) Authorize(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.AuthorizationScope, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.AuthorizationScope{}, err
	}
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evidence.AuthorizationScope{}, err
	}
	return resolved.Authorization, nil
}

func (adapter *SubscriptionCostAdapter) Summarize(snapshot evidence.CanonicalSnapshot) evidence.Summary {
	if err := snapshot.Validate(adapter.descriptor); err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	payload, err := decodeEvidencePayload(snapshot.Bytes())
	if err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	readModel := map[string]any{"version": "subscription_cost_read_model/v1"}
	for _, field := range []string{"subscription_id", "vps_id", "original_amount", "original_currency", "billing_period_unit", "billing_period_length", "conversion_rate", "conversion_provider", "rate_date", "rate_fetched_at", "rate_stale", "base_amount", "base_currency", "budget_source", "budget_currency", "budget_month", "budget_monthly_limit", "budget_warning_pct", "budget_status", "budget_actual_spend", "coverage_start", "coverage_end", "coverage_status", "covered_days", "total_days", "converted_subscription_count", "missing_rate_count"} {
		readModel[field] = payload[field]
	}
	envelope := snapshot.Envelope()
	return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion, Title: "Subscription cost", SearchText: "subscription cost " + envelope.Source.ID + " " + stringValue(payload["original_currency"]) + " " + stringValue(payload["base_currency"]), ReadModel: readModel}
}

func (adapter *SubscriptionCostAdapter) Compare(left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	compatible, reason := compatibleDiscreteSnapshots(adapter.descriptor, left, right, alignment)
	values := map[string]any{"version": "subscription_cost_comparison/v1"}
	if compatible {
		leftPayload, leftErr := decodeEvidencePayload(left.Bytes())
		rightPayload, rightErr := decodeEvidencePayload(right.Bytes())
		if leftErr != nil || rightErr != nil || stringValue(leftPayload["original_currency"]) != stringValue(rightPayload["original_currency"]) || stringValue(leftPayload["base_currency"]) != stringValue(rightPayload["base_currency"]) || stringValue(leftPayload["billing_period_unit"]) != stringValue(rightPayload["billing_period_unit"]) || integerValue(leftPayload["billing_period_length"]) != integerValue(rightPayload["billing_period_length"]) {
			compatible, reason = false, "incompatible subscription cost semantics"
		} else {
			leftOriginal, rightOriginal := costNumberValue(leftPayload["original_amount"]), costNumberValue(rightPayload["original_amount"])
			leftBase, rightBase := costNumberValue(leftPayload["base_amount"]), costNumberValue(rightPayload["base_amount"])
			leftBudget, rightBudget := costNumberValue(leftPayload["budget_actual_spend"]), costNumberValue(rightPayload["budget_actual_spend"])
			values["original_amount_left"] = leftOriginal
			values["original_amount_right"] = rightOriginal
			values["original_amount_delta"] = rightOriginal - leftOriginal
			values["base_amount_left"] = leftBase
			values["base_amount_right"] = rightBase
			values["base_amount_delta"] = rightBase - leftBase
			values["budget_actual_spend_left"] = leftBudget
			values["budget_actual_spend_right"] = rightBudget
			values["budget_actual_spend_delta"] = rightBudget - leftBudget
			values["budget_status_left"] = stringValue(leftPayload["budget_status"])
			values["budget_status_right"] = stringValue(rightPayload["budget_status"])
			values["budget_status_changed"] = stringValue(leftPayload["budget_status"]) != stringValue(rightPayload["budget_status"])
			values["rate_stale_left"] = boolValue(leftPayload["rate_stale"])
			values["rate_stale_right"] = boolValue(rightPayload["rate_stale"])
			values["rate_stale_changed"] = boolValue(leftPayload["rate_stale"]) != boolValue(rightPayload["rate_stale"])
			leftMissing, rightMissing := integerValue(leftPayload["missing_rate_count"]), integerValue(rightPayload["missing_rate_count"])
			values["missing_rate_count_left"] = leftMissing
			values["missing_rate_count_right"] = rightMissing
			values["missing_rate_count_delta"] = rightMissing - leftMissing
		}
	}
	return evidence.Comparison{Key: adapter.descriptor.Key, Compatible: compatible, Reason: reason, Values: values}
}

func (adapter *SubscriptionCostAdapter) Export(snapshot evidence.CanonicalSnapshot, mode evidence.ExportMode) evidence.ExportMaterial {
	return exportEvidenceSnapshot(adapter.descriptor, snapshot, mode)
}

func (adapter *SubscriptionCostAdapter) evaluate(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (discreteEvaluation, error) {
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return discreteEvaluation{}, err
	}
	capture, err := adapter.source.LoadSubscriptionCostEvidence(ctx, selection.SourceID, selection.RequestedWindow)
	if err != nil {
		return discreteEvaluation{}, err
	}
	now := adapterNow(adapter.options)
	if err := validateSubscriptionCostCapture(capture, selection, now); err != nil {
		return discreteEvaluation{}, err
	}
	payload := map[string]any{
		"subscription_id": capture.SubscriptionID, "vps_id": capture.VPSID, "original_amount": capture.OriginalAmount,
		"original_currency": capture.OriginalCurrency, "billing_period_unit": capture.BillingPeriodUnit, "billing_period_length": capture.BillingPeriodLength,
		"conversion_rate": capture.ConversionRate, "conversion_provider": capture.ConversionProvider, "rate_date": capture.RateDate,
		"rate_fetched_at": capture.RateFetchedAt.UTC().Format(time.RFC3339Nano), "rate_stale": capture.RateStale,
		"base_amount": capture.BaseAmount, "base_currency": capture.BaseCurrency, "budget_source": capture.BudgetSource, "budget_currency": capture.BudgetCurrency,
		"budget_month": capture.BudgetMonth, "budget_monthly_limit": capture.BudgetMonthlyLimit, "budget_warning_pct": capture.BudgetWarningPct,
		"budget_status": capture.BudgetStatus, "budget_actual_spend": capture.BudgetActualSpend,
		"coverage_start": capture.CoverageStart.UTC().Format(time.RFC3339Nano), "coverage_end": capture.CoverageEnd.UTC().Format(time.RFC3339Nano),
		"coverage_status": capture.CoverageStatus, "covered_days": capture.CoveredDays, "total_days": capture.TotalDays,
		"converted_subscription_count": capture.ConvertedSubscriptionCount, "missing_rate_count": capture.MissingRateCount,
	}
	canonical, redaction, err := evidence.CanonicalizePayload(adapter.descriptor, payload, evidence.RedactionNormalOnly)
	if err != nil {
		return discreteEvaluation{}, err
	}
	redaction.Decisions = appendForbiddenPreviewDecisions(adapter.descriptor, redaction.Decisions)
	quality := evidence.Quality{Status: evidence.QualityComplete, SampleCount: 1, DataPointCount: 1 + capture.ConvertedSubscriptionCount + capture.MissingRateCount}
	if capture.CoverageStatus != "complete" || capture.MissingRateCount > 0 {
		quality.Status, quality.Partial = evidence.QualityPartial, true
	} else if capture.RateStale {
		quality.Status = evidence.QualityDegraded
	}
	return discreteEvaluation{resolved: resolved, actualWindow: evidence.TimeWindow{Start: capture.CoverageStart, End: capture.CoverageEnd}, observedAt: capture.ObservedAt, sourceRevision: capture.SourceRevision, sourceWatermark: capture.SourceWatermark, producerVersion: capture.ProducerVersion, calculationVersion: subscriptionCostCalculationVersion, units: evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"original_amount": capture.OriginalCurrency, "base_amount": capture.BaseCurrency, "budget_monthly_limit": capture.BaseCurrency, "budget_actual_spend": capture.BaseCurrency}}, quality: quality, payload: payload, canonical: canonical, redaction: redaction.Decisions}, nil
}

func validateSubscriptionCostCapture(capture SubscriptionCostCapture, selection evidence.Selection, now time.Time) error {
	watermark, watermarkErr := parseCanonicalPostgresTimestamp(capture.SourceWatermark)
	rateDate, rateDateErr := time.Parse("2006-01-02", capture.RateDate)
	budgetMonth, budgetMonthErr := time.Parse("2006-01", capture.BudgetMonth)
	expectedTotalDays := int(selection.RequestedWindow.End.Sub(selection.RequestedWindow.Start) / (24 * time.Hour))
	coveredDays := int(capture.CoverageEnd.Sub(capture.CoverageStart) / (24 * time.Hour))
	if !validSourceIdentifier(capture.SubscriptionID) || capture.VPSID != selection.SourceID || !validVersionString(capture.SourceRevision) || !validVersionString(capture.ProducerVersion) ||
		!canonicalTask4Timestamp(capture.ObservedAt) || capture.ObservedAt.After(now) || watermarkErr != nil || watermark.Before(capture.ObservedAt) || watermark.After(now) ||
		!finiteNonNegative(capture.OriginalAmount) || !currencyCodePattern.MatchString(capture.OriginalCurrency) || !knownBillingPeriod(capture.BillingPeriodUnit) || capture.BillingPeriodLength <= 0 || capture.BillingPeriodLength > 120 ||
		!finitePositive(capture.ConversionRate) || !knownConversionProvider(capture.ConversionProvider) || rateDateErr != nil || rateDate.After(capture.ObservedAt) ||
		!canonicalTask4Timestamp(capture.RateFetchedAt) || rateDate.After(capture.RateFetchedAt) || capture.RateFetchedAt.After(capture.ObservedAt) ||
		!finiteNonNegative(capture.BaseAmount) || !currencyCodePattern.MatchString(capture.BaseCurrency) || math.Abs(capture.BaseAmount-capture.OriginalAmount*capture.ConversionRate) > 0.0001 ||
		capture.BudgetSource != "subscription_monthly_budgets" || capture.BudgetCurrency != capture.BaseCurrency || budgetMonthErr != nil || budgetMonth.After(selection.RequestedWindow.Start) || !finiteNonNegative(capture.BudgetMonthlyLimit) || capture.BudgetWarningPct < 1 || capture.BudgetWarningPct > 100 || !finiteNonNegative(capture.BudgetActualSpend) || !validBudgetStatus(capture) ||
		!canonicalTask4Timestamp(capture.CoverageStart) || !canonicalTask4Timestamp(capture.CoverageEnd) || capture.CoverageStart.Before(selection.RequestedWindow.Start) || capture.CoverageEnd.After(selection.RequestedWindow.End) || !capture.CoverageEnd.After(capture.CoverageStart) ||
		!knownCoverageStatus(capture.CoverageStatus) || capture.TotalDays != expectedTotalDays || capture.CoveredDays != coveredDays || capture.CoveredDays <= 0 || capture.CoveredDays > capture.TotalDays ||
		capture.ConvertedSubscriptionCount+capture.MissingRateCount > evidence.MaxSnapshotDataPoints {
		return fmt.Errorf("%w: subscription cost source", evidence.ErrInvalidCanonicalPayload)
	}
	identityConversion := capture.ConversionProvider == "identity"
	if identityConversion != (capture.OriginalCurrency == capture.BaseCurrency) || (identityConversion && capture.ConversionRate != 1) || (capture.MissingRateCount > 0 && capture.BudgetStatus != "unknown") {
		return fmt.Errorf("%w: subscription cost conversion or budget coverage", evidence.ErrInvalidCanonicalPayload)
	}
	if (capture.CoverageStatus == "complete") != (capture.CoveredDays == capture.TotalDays && capture.MissingRateCount == 0) {
		return fmt.Errorf("%w: subscription cost coverage", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func calendarMonthWindow(window evidence.TimeWindow) bool {
	start := window.Start.UTC()
	return start.Day() == 1 && start.Hour() == 0 && start.Minute() == 0 && start.Second() == 0 && start.Nanosecond() == 0 && window.End.Equal(start.AddDate(0, 1, 0))
}

func knownBillingPeriod(value string) bool {
	switch value {
	case "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func knownConversionProvider(value string) bool {
	switch value {
	case "identity", "frankfurter", "fixer":
		return true
	default:
		return false
	}
}

func knownCoverageStatus(value string) bool {
	switch value {
	case "complete", "partial", "missing_rate":
		return true
	default:
		return false
	}
}

func validBudgetStatus(capture SubscriptionCostCapture) bool {
	warningLimit := capture.BudgetMonthlyLimit * float64(capture.BudgetWarningPct) / 100
	switch capture.BudgetStatus {
	case "ok":
		return capture.BudgetMonthlyLimit > 0 && capture.BudgetActualSpend < warningLimit
	case "warning":
		return capture.BudgetMonthlyLimit > 0 && capture.BudgetActualSpend >= warningLimit && capture.BudgetActualSpend < capture.BudgetMonthlyLimit
	case "over":
		return capture.BudgetMonthlyLimit > 0 && capture.BudgetActualSpend >= capture.BudgetMonthlyLimit
	case "unknown":
		return capture.MissingRateCount > 0 || capture.BudgetMonthlyLimit <= 0
	default:
		return false
	}
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
func finitePositive(value float64) bool { return finiteNonNegative(value) && value > 0 }

func subscriptionCostDescriptor() evidence.Descriptor {
	normal := []string{"subscription_id", "vps_id", "original_amount", "original_currency", "billing_period_unit", "billing_period_length", "conversion_rate", "conversion_provider", "rate_date", "rate_fetched_at", "rate_stale", "base_amount", "base_currency", "budget_source", "budget_currency", "budget_month", "budget_monthly_limit", "budget_warning_pct", "budget_status", "budget_actual_spend", "coverage_start", "coverage_end", "coverage_status", "covered_days", "total_days", "converted_subscription_count", "missing_rate_count"}
	forbidden := []string{"provider_secret", "provider_response", "fixer_api_key", "raw_json", "details", "token", "password", "url", "stdout", "stderr"}
	return normalOnlyDescriptor(evidence.SubscriptionCostV1Key(), "subscription_cost_v1", normal, forbidden)
}

func costNumberValue(value any) float64 { number, _ := numberValue(value); return number }
func integerValue(value any) int64      { return int64(costNumberValue(value)) }
