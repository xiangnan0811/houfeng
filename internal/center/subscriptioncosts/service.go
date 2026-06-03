package subscriptioncosts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
)

type Service struct {
	repo         Repository
	settingsRepo SettingsRepository
	providers    map[string]ExchangeRateProvider
	now          func() time.Time
}

func NewService(repo Repository, settingsRepo SettingsRepository, providers map[string]ExchangeRateProvider) *Service {
	return &Service{
		repo:         repo,
		settingsRepo: settingsRepo,
		providers:    providers,
		now:          time.Now,
	}
}

func (s *Service) GetSettings(ctx context.Context) (centersettings.SubscriptionCostSettings, error) {
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return centersettings.SubscriptionCostSettings{}, fmt.Errorf("get center settings: %w", err)
	}
	return settings.SubscriptionCost, nil
}

func (s *Service) PutSettings(ctx context.Context, input centersettings.SubscriptionCostSettings) (centersettings.SubscriptionCostSettings, error) {
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return centersettings.SubscriptionCostSettings{}, fmt.Errorf("get center settings: %w", err)
	}
	settings.SubscriptionCost = input
	updated, err := s.settingsRepo.PutSettings(ctx, settings)
	if err != nil {
		return centersettings.SubscriptionCostSettings{}, err
	}
	return updated.SubscriptionCost, nil
}

func (s *Service) ListCostRows(ctx context.Context) ([]CostRow, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListCostRows(ctx, settings)
	if err != nil {
		return nil, err
	}
	budgets, err := s.repo.ListBudgets(ctx, BudgetListFilters{})
	if err != nil {
		return nil, err
	}
	budgets = applyBudgetSpend(rows, budgets)
	applyRowBudgetStatus(rows, budgets)
	return rows, nil
}

func (s *Service) GetOverview(ctx context.Context) (Overview, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return Overview{}, err
	}
	rows, err := s.repo.ListCostRows(ctx, settings)
	if err != nil {
		return Overview{}, fmt.Errorf("list subscription costs: %w", err)
	}
	missing, err := s.repo.ListMissingSubscriptionAssets(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("list vps assets missing subscriptions: %w", err)
	}
	budgets, err := s.repo.ListBudgets(ctx, BudgetListFilters{})
	if err != nil {
		return Overview{}, fmt.Errorf("list subscription budgets: %w", err)
	}
	budgets = applyBudgetSpend(rows, budgets)
	applyRowBudgetStatus(rows, budgets)
	budgetMonthBuckets, err := s.repo.ListBudgetMonthBuckets(ctx, settings, 1, s.now())
	if err != nil {
		return Overview{}, fmt.Errorf("list subscription budget month buckets: %w", err)
	}

	today := subscriptionDay(s.now())
	overview := Overview{
		SnapshotGeneratedAt:         s.now().UTC(),
		BaseCurrency:                settings.BaseCurrency,
		ActiveSubscriptionCount:     len(rows),
		MissingSubscriptionVPSCount: len(missing),
		MissingSubscriptionAssets:   missing,
		UpcomingRenewals:            make([]RenewalQueueItem, 0),
		ProviderBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.ProviderID, row.ProviderName, "未记录服务商"), emptyAs(row.ProviderName, row.ProviderID, "未记录服务商")
		}),
		CurrencyBreakdown: breakdown(rows, func(row CostRow) (string, string) { return row.Currency, row.Currency }),
		CategoryBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.CostCategory, row.CostCategory, "未分类"), emptyAs(row.CostCategory, row.CostCategory, "未分类")
		}),
		VPSCosts: rows,
	}

	for _, row := range rows {
		if row.MonthlyPriceBase != nil {
			overview.TotalMonthlyCost += *row.MonthlyPriceBase
			overview.TotalYearlyCost += *row.YearlyPriceBase
		}
		if row.ExchangeRateStale {
			overview.ExchangeRateStaleCount++
		}
		if isDecisionAttention(row) {
			overview.DecisionAttentionCount++
		}
		if row.RenewAt != nil {
			days := int(row.RenewAt.Time.Sub(today.Time).Hours() / 24)
			if days >= 0 && days <= 14 {
				overview.RenewalDue14dCount++
			}
			if days >= 0 && days <= 30 {
				overview.RenewalDue30dCount++
			}
			if days >= 0 && days <= 90 {
				overview.UpcomingRenewals = append(overview.UpcomingRenewals, renewalQueueItem(row))
			}
		}
	}

	overview.BudgetRiskCount = currentMonthBudgetRiskCount(overview.TotalMonthlyCost, budgetMonthBuckets)
	overview.BudgetRisks = monthlyBudgetRisks(overview.TotalMonthlyCost, budgetMonthBuckets)
	sortRenewalQueue(overview.UpcomingRenewals)
	if len(overview.UpcomingRenewals) > 12 {
		overview.UpcomingRenewals = overview.UpcomingRenewals[:12]
	}
	return overview, nil
}

func (s *Service) GetStatistics(ctx context.Context, window string) (Statistics, error) {
	window = strings.ToLower(strings.TrimSpace(window))
	if window == "" {
		window = StatisticsWindowMonth
	}
	switch window {
	case StatisticsWindowMonth, StatisticsWindowQuarter, StatisticsWindowYear:
	default:
		return Statistics{}, fmt.Errorf("%w: invalid statistics window", ErrInvalidInput)
	}

	settings, err := s.GetSettings(ctx)
	if err != nil {
		return Statistics{}, err
	}
	rows, err := s.repo.ListCostRows(ctx, settings)
	if err != nil {
		return Statistics{}, fmt.Errorf("list subscription costs: %w", err)
	}
	costMonthBuckets, err := s.repo.ListCostMonthBuckets(ctx, settings, statisticsWindowMonths(window), s.now())
	if err != nil {
		return Statistics{}, fmt.Errorf("list subscription cost month buckets: %w", err)
	}
	budgetMonthBuckets, err := s.repo.ListBudgetMonthBuckets(ctx, settings, statisticsWindowMonths(window), s.now())
	if err != nil {
		return Statistics{}, fmt.Errorf("list subscription budget month buckets: %w", err)
	}
	costMonthBuckets = mergeBudgetMonthBuckets(costMonthBuckets, budgetMonthBuckets)
	budgets, err := s.repo.ListBudgets(ctx, BudgetListFilters{})
	if err != nil {
		return Statistics{}, fmt.Errorf("list subscription budgets: %w", err)
	}
	budgets = applyBudgetSpend(rows, budgets)

	stats := Statistics{
		Window:       window,
		BaseCurrency: settings.BaseCurrency,
		ProviderBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.ProviderID, row.ProviderName, "未记录服务商"), emptyAs(row.ProviderName, row.ProviderID, "未记录服务商")
		}),
		CurrencyBreakdown: breakdown(rows, func(row CostRow) (string, string) { return row.Currency, row.Currency }),
		CategoryBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.CostCategory, row.CostCategory, "未分类"), emptyAs(row.CostCategory, row.CostCategory, "未分类")
		}),
		PaymentBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.PaymentMethod, row.PaymentMethod, "未记录支付方式"), emptyAs(row.PaymentMethod, row.PaymentMethod, "未记录支付方式")
		}),
		RegionBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			label := strings.TrimSpace(row.Country)
			if region := strings.TrimSpace(row.Region); region != "" && region != label {
				if label != "" {
					label += " / "
				}
				label += region
			}
			return emptyAs(label, label, "未记录国家/地区"), emptyAs(label, label, "未记录国家/地区")
		}),
		CostMonthBuckets:    costMonthBuckets,
		RenewalMonthBuckets: renewalBuckets(rows, s.now(), window),
		BudgetStatuses:      budgets,
	}
	for _, row := range rows {
		if row.MonthlyPriceBase == nil {
			continue
		}
		stats.TotalMonthlyCost += *row.MonthlyPriceBase
		stats.TotalYearlyCost += *row.YearlyPriceBase
	}
	return stats, nil
}

func (s *Service) ListMonthlyBudgets(ctx context.Context) ([]MonthlyBudgetRecord, error) {
	return s.repo.ListMonthlyBudgets(ctx)
}

func (s *Service) UpsertMonthlyBudget(ctx context.Context, input UpsertMonthlyBudgetInput) (MonthlyBudgetRecord, error) {
	input = NormalizeUpsertMonthlyBudgetInput(input)
	if err := ValidateUpsertMonthlyBudgetInput(input); err != nil {
		return MonthlyBudgetRecord{}, err
	}
	return s.repo.UpsertMonthlyBudget(ctx, input)
}

func (s *Service) ListBudgets(ctx context.Context, filters BudgetListFilters) ([]BudgetRecord, error) {
	filters = NormalizeBudgetListFilters(filters)
	if err := ValidateBudgetListFilters(filters); err != nil {
		return nil, err
	}
	budgets, err := s.repo.ListBudgets(ctx, filters)
	if err != nil {
		return nil, err
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListCostRows(ctx, settings)
	if err != nil {
		return nil, err
	}
	return applyBudgetSpend(rows, budgets), nil
}

func (s *Service) CreateBudget(ctx context.Context, input CreateBudgetInput) (BudgetRecord, error) {
	input = NormalizeCreateBudgetInput(input)
	if err := ValidateCreateBudgetInput(input); err != nil {
		return BudgetRecord{}, err
	}
	record, err := s.repo.CreateBudget(ctx, input)
	if err != nil {
		return BudgetRecord{}, err
	}
	return s.hydrateBudget(ctx, record)
}

func (s *Service) PatchBudget(ctx context.Context, input PatchBudgetInput) (BudgetRecord, error) {
	input = NormalizePatchBudgetInput(input)
	if err := ValidatePatchBudgetInput(input); err != nil {
		return BudgetRecord{}, err
	}
	record, err := s.repo.PatchBudget(ctx, input)
	if err != nil {
		return BudgetRecord{}, err
	}
	return s.hydrateBudget(ctx, record)
}

func (s *Service) hydrateBudget(ctx context.Context, budget BudgetRecord) (BudgetRecord, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return BudgetRecord{}, err
	}
	rows, err := s.repo.ListCostRows(ctx, settings)
	if err != nil {
		return BudgetRecord{}, err
	}
	budgets := applyBudgetSpend(rows, []BudgetRecord{budget})
	return budgets[0], nil
}

func (s *Service) RefreshExchangeRates(ctx context.Context) (ExchangeRateRefreshResult, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return ExchangeRateRefreshResult{}, err
	}
	provider := s.providers[settings.ExchangeRateProvider]
	if provider == nil {
		return ExchangeRateRefreshResult{}, fmt.Errorf("%w: exchange rate provider is not configured", ErrInvalidInput)
	}

	currencies, err := s.repo.ListActiveCurrencies(ctx)
	if err != nil {
		return ExchangeRateRefreshResult{}, fmt.Errorf("list active subscription currencies: %w", err)
	}

	now := s.now().UTC()
	result := ExchangeRateRefreshResult{
		Provider:     settings.ExchangeRateProvider,
		BaseCurrency: settings.BaseCurrency,
		FetchedAt:    now,
		Succeeded:    []ExchangeRateFetchResult{},
		Failed:       []ExchangeRateFetchResult{},
	}
	for _, currency := range currencies {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency == "" || currency == settings.BaseCurrency {
			continue
		}
		fetched, err := provider.FetchRate(ctx, currency, settings.BaseCurrency)
		if err != nil {
			result.Failed = append(result.Failed, ExchangeRateFetchResult{
				QuoteCurrency: currency,
				BaseCurrency:  settings.BaseCurrency,
				Error:         sanitizeProviderError(err),
			})
			continue
		}
		if _, err := s.repo.UpsertExchangeRate(ctx, ExchangeRateUpsert{
			Provider:      settings.ExchangeRateProvider,
			BaseCurrency:  settings.BaseCurrency,
			QuoteCurrency: currency,
			Rate:          fetched.Rate,
			RateDate:      fetched.RateDate,
			FetchedAt:     now,
		}); err != nil {
			result.Failed = append(result.Failed, ExchangeRateFetchResult{
				QuoteCurrency: currency,
				BaseCurrency:  settings.BaseCurrency,
				Rate:          fetched.Rate,
				RateDate:      fetched.RateDate,
				Error:         "store exchange rate failed",
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, ExchangeRateFetchResult{
			QuoteCurrency: currency,
			BaseCurrency:  settings.BaseCurrency,
			Rate:          fetched.Rate,
			RateDate:      fetched.RateDate,
		})
	}
	return result, nil
}

func subscriptionDay(now time.Time) subscriptions.Date {
	return subscriptions.NewDate(now.UTC())
}

func applyBudgetSpend(rows []CostRow, budgets []BudgetRecord) []BudgetRecord {
	for i := range budgets {
		budgets[i].CurrentMonthlySpend = 0
		for _, row := range rows {
			if !budgetMatchesRow(budgets[i], row) || row.MonthlyPriceBase == nil {
				continue
			}
			budgets[i].CurrentMonthlySpend += *row.MonthlyPriceBase
		}
		budgets[i].CurrentYearlySpend = budgets[i].CurrentMonthlySpend * 12
		budgets[i].Status = EvaluateBudgetStatus(
			budgets[i].Enabled,
			budgets[i].CurrentMonthlySpend,
			budgets[i].MonthlyLimit,
			budgets[i].YearlyLimit,
			budgets[i].WarningPct,
		)
	}
	return budgets
}

func applyRowBudgetStatus(rows []CostRow, budgets []BudgetRecord) {
	for i := range rows {
		status := BudgetStatusOK
		matched := false
		for _, budget := range budgets {
			if !budgetMatchesRow(budget, rows[i]) {
				continue
			}
			matched = true
			status = worseBudgetStatus(status, budget.Status)
		}
		if !matched {
			status = BudgetStatusUnknown
		}
		rows[i].BudgetStatus = status
	}
}

func budgetMatchesRow(budget BudgetRecord, row CostRow) bool {
	if !budget.Enabled {
		return false
	}
	switch BudgetScopeType(budget.ScopeType) {
	case BudgetScopeGlobal:
		return true
	case BudgetScopeProvider:
		return budget.ScopeID != "" && (budget.ScopeID == row.ProviderID || budget.ScopeID == row.ProviderName)
	case BudgetScopeLabel:
		for _, label := range row.Labels {
			if label == budget.ScopeID {
				return true
			}
		}
		return false
	case BudgetScopeCategory:
		return budget.ScopeID != "" && budget.ScopeID == row.CostCategory
	case BudgetScopeVPS:
		return budget.ScopeID != "" && budget.ScopeID == row.VPSID
	default:
		return false
	}
}

func worseBudgetStatus(left, right BudgetStatus) BudgetStatus {
	weight := func(status BudgetStatus) int {
		switch status {
		case BudgetStatusOver:
			return 4
		case BudgetStatusWarning:
			return 3
		case BudgetStatusOK:
			return 2
		case BudgetStatusUnknown:
			return 1
		default:
			return 0
		}
	}
	if weight(right) > weight(left) {
		return right
	}
	return left
}

func breakdown(rows []CostRow, keyFn func(CostRow) (string, string)) []BreakdownItem {
	items := map[string]BreakdownItem{}
	for _, row := range rows {
		key, label := keyFn(row)
		if row.MonthlyPriceBase == nil {
			continue
		}
		item := items[key]
		item.Key = key
		item.Label = label
		item.SubscriptionCount++
		item.MonthlyCost += *row.MonthlyPriceBase
		item.YearlyCost += *row.YearlyPriceBase
		items[key] = item
	}
	result := make([]BreakdownItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	SortBreakdowns(result)
	return result
}

func statisticsWindowMonths(window string) int {
	switch window {
	case StatisticsWindowYear:
		return 12
	case StatisticsWindowQuarter:
		return 6
	default:
		return 3
	}
}

func renewalBuckets(rows []CostRow, now time.Time, window string) []SeriesPoint {
	months := statisticsWindowMonths(window)
	start := subscriptions.NewDate(now.UTC()).Time
	buckets := make([]SeriesPoint, 0, months)
	index := make(map[string]int, months)
	for i := 0; i < months; i++ {
		month := time.Date(start.Year(), start.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		key := month.Format("2006-01")
		index[key] = i
		buckets = append(buckets, SeriesPoint{Bucket: key})
	}
	for _, row := range rows {
		if row.RenewAt == nil || row.MonthlyPriceBase == nil {
			continue
		}
		key := time.Date(row.RenewAt.Time.Year(), row.RenewAt.Time.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		i, ok := index[key]
		if !ok {
			continue
		}
		buckets[i].RenewalCount++
		buckets[i].MonthlyCost += *row.MonthlyPriceBase
	}
	return buckets
}

func mergeBudgetMonthBuckets(costBuckets, budgetBuckets []SeriesPoint) []SeriesPoint {
	byBucket := make(map[string]SeriesPoint, len(budgetBuckets))
	for _, budget := range budgetBuckets {
		byBucket[budget.Bucket] = budget
	}
	for i := range costBuckets {
		budget, ok := byBucket[costBuckets[i].Bucket]
		if !ok {
			continue
		}
		costBuckets[i].BudgetLimit = budget.BudgetLimit
		costBuckets[i].BudgetCurrency = budget.BudgetCurrency
		costBuckets[i].BudgetWarningPct = budget.BudgetWarningPct
		if budget.DataInsufficient {
			costBuckets[i].DataInsufficient = true
		}
	}
	return costBuckets
}

func currentMonthBudgetRiskCount(monthlyCost float64, budgetBuckets []SeriesPoint) int {
	if len(monthlyBudgetRisks(monthlyCost, budgetBuckets)) > 0 {
		return 1
	}
	return 0
}

func monthlyBudgetRisks(monthlyCost float64, budgetBuckets []SeriesPoint) []BudgetRecord {
	if len(budgetBuckets) == 0 {
		return nil
	}
	budget := budgetBuckets[len(budgetBuckets)-1]
	if budget.DataInsufficient {
		return nil
	}
	status := budgetStatusForMonthlySpend(monthlyCost, budget.BudgetLimit, budget.BudgetWarningPct)
	if status != BudgetStatusWarning && status != BudgetStatusOver {
		return nil
	}
	return []BudgetRecord{{
		BudgetID:            "monthly-" + budget.Bucket,
		ScopeType:           string(BudgetScopeGlobal),
		Name:                "月预算 " + budget.Bucket,
		BaseCurrency:        budget.BudgetCurrency,
		MonthlyLimit:        budget.BudgetLimit,
		WarningPct:          budget.BudgetWarningPct,
		Enabled:             true,
		CurrentMonthlySpend: monthlyCost,
		CurrentYearlySpend:  monthlyCost * 12,
		Status:              status,
	}}
}

func budgetStatusForMonthlySpend(monthlyCost float64, monthlyLimit *float64, warningPct int) BudgetStatus {
	if monthlyLimit == nil {
		return BudgetStatusUnknown
	}
	return EvaluateBudgetStatus(true, monthlyCost, monthlyLimit, nil, warningPct)
}

func renewalQueueItem(row CostRow) RenewalQueueItem {
	return RenewalQueueItem{
		SubscriptionID:    row.SubscriptionID,
		VPSID:             row.VPSID,
		VPSDisplayName:    row.VPSDisplayName,
		DisplayName:       row.DisplayName,
		ProviderName:      row.ProviderName,
		RenewAt:           row.RenewAt,
		MonthlyPriceBase:  row.MonthlyPriceBase,
		YearlyPriceBase:   row.YearlyPriceBase,
		BaseCurrency:      row.BaseCurrency,
		Currency:          row.Currency,
		RenewalDecision:   row.RenewalDecision,
		LifecycleStatus:   row.LifecycleStatus,
		ExchangeRateStale: row.ExchangeRateStale,
	}
}

func sortRenewalQueue(items []RenewalQueueItem) {
	sort.Slice(items, func(i, j int) bool {
		left := time.Time{}
		right := time.Time{}
		if items[i].RenewAt != nil {
			left = items[i].RenewAt.Time
		}
		if items[j].RenewAt != nil {
			right = items[j].RenewAt.Time
		}
		if left.Equal(right) {
			return items[i].SubscriptionID < items[j].SubscriptionID
		}
		return left.Before(right)
	})
}

func isDecisionAttention(row CostRow) bool {
	return row.RenewalDecision == "cancel" ||
		row.RenewalDecision == "auto_renew_cancelled" ||
		row.RenewalDecision == "migrate" ||
		row.LifecycleStatus == "to_cancel" ||
		row.LifecycleStatus == "to_migrate" ||
		row.LifecycleStatus == "cancelled"
}

func emptyAs(primary, secondary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return primary
	}
	secondary = strings.TrimSpace(secondary)
	if secondary != "" {
		return secondary
	}
	return fallback
}

func sanitizeProviderError(err error) string {
	if err == nil {
		return ""
	}
	message := sensitiveProviderErrorPattern.ReplaceAllString(err.Error(), "$1=[redacted]")
	if len(message) > 160 {
		message = message[:160]
	}
	return message
}

var sensitiveProviderErrorPattern = regexp.MustCompile(`(?i)\b(access_key|api[_-]?key|apikey|token)=([^&\s]+)`)

func MapSettingsError(err error) error {
	if errors.Is(err, centersettings.ErrInvalidSettings) {
		return ErrInvalidInput
	}
	return err
}
