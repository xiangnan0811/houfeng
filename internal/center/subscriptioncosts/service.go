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

func (s *Service) PutSettings(ctx context.Context, mutate func(centersettings.SubscriptionCostSettings) (centersettings.SubscriptionCostSettings, error)) (centersettings.SubscriptionCostSettings, error) {
	if mutate == nil {
		return centersettings.SubscriptionCostSettings{}, centersettings.ErrInvalidSettings
	}
	updated, err := s.settingsRepo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		next, err := mutate(current.SubscriptionCost)
		if err != nil {
			return centersettings.CenterSettings{}, err
		}
		current.SubscriptionCost = next
		return current, nil
	})
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
	budgets = applyBudgetSpend(rows, budgets, settings.BaseCurrency)
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
	currentRows, archivedRows := splitCostRows(rows)
	rows = currentRows
	currentMissing := make([]MissingSubscriptionAsset, 0)
	archivedMissing := make([]MissingSubscriptionAsset, 0)
	for _, asset := range missing {
		if asset.LifecycleStatus == "archived" {
			if potentialProviderCharge(asset.AutoRenewCheck) {
				archivedMissing = append(archivedMissing, asset)
			}
		} else {
			currentMissing = append(currentMissing, asset)
		}
	}
	missing = currentMissing
	budgets, err := s.repo.ListBudgets(ctx, BudgetListFilters{})
	if err != nil {
		return Overview{}, fmt.Errorf("list subscription budgets: %w", err)
	}
	budgets = applyBudgetSpend(rows, budgets, settings.BaseCurrency)
	applyRowBudgetStatus(rows, budgets)
	// 预算月桶、续费窗口“今天”与 snapshot_generated_at 取自同一时刻，
	// 前端据此计算剩余天数时不会跨 UTC 午夜错位，月末也不会混用两个月份。
	generatedAt := s.now().UTC()
	budgetMonthBuckets, err := s.repo.ListBudgetMonthBuckets(ctx, settings, 1, generatedAt)
	if err != nil {
		return Overview{}, fmt.Errorf("list subscription budget month buckets: %w", err)
	}

	today := subscriptionDay(generatedAt)
	overview := Overview{
		ArchivedPotentialCosts:            archivedRows,
		ArchivedMissingSubscriptionAssets: archivedMissing,
		ArchivedUnknownAmountCount:        len(archivedMissing),
		CurrentUnknownAmountCount:         len(missing),
		SnapshotGeneratedAt:               generatedAt,
		BaseCurrency:                      settings.BaseCurrency,
		ActiveSubscriptionCount:           len(rows),
		MissingSubscriptionVPSCount:       len(missing),
		MissingSubscriptionAssets:         missing,
		UpcomingRenewals:                  make([]RenewalQueueItem, 0),
		ProviderBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.ProviderID, row.ProviderName, "未记录服务商"), emptyAs(row.ProviderName, row.ProviderID, "未记录服务商")
		}),
		CurrencyBreakdown: breakdown(rows, func(row CostRow) (string, string) { return row.Currency, row.Currency }),
		CategoryBreakdown: breakdown(rows, func(row CostRow) (string, string) {
			return emptyAs(row.CostCategory, row.CostCategory, "未分类"), emptyAs(row.CostCategory, row.CostCategory, "未分类")
		}),
		VPSCosts: rows,
	}

	var archivedTotal float64
	for _, row := range archivedRows {
		if row.MonthlyPriceBase == nil {
			overview.ArchivedUnknownAmountCount++
		} else {
			archivedTotal += *row.MonthlyPriceBase
		}
	}
	if overview.ArchivedUnknownAmountCount == 0 {
		overview.ArchivedPotentialMonthlyCost = &archivedTotal
	}

	for _, row := range rows {
		if row.MonthlyPriceBase != nil {
			overview.TotalMonthlyCost += *row.MonthlyPriceBase
			if row.YearlyPriceBase != nil {
				overview.TotalYearlyCost += *row.YearlyPriceBase
			} else {
				overview.TotalYearlyCost += *row.MonthlyPriceBase * 12
			}
		}
		if row.MonthlyPriceBase == nil {
			overview.CurrentUnknownAmountCount++
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
	rows, _ = splitCostRows(rows)
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
	budgets = applyBudgetSpend(rows, budgets, settings.BaseCurrency)

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
		if row.YearlyPriceBase != nil {
			stats.TotalYearlyCost += *row.YearlyPriceBase
		} else {
			stats.TotalYearlyCost += *row.MonthlyPriceBase * 12
		}
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

func (s *Service) BulkUpsertMonthlyBudgets(ctx context.Context, input BulkUpsertMonthlyBudgetInput) (BulkUpsertMonthlyBudgetResult, error) {
	input = NormalizeBulkUpsertMonthlyBudgetInput(input)
	if err := ValidateBulkUpsertMonthlyBudgetInput(input); err != nil {
		return BulkUpsertMonthlyBudgetResult{}, err
	}

	end := monthStart(s.now())
	start := end
	switch input.Scope {
	case MonthlyBudgetBulkScopeCurrentYear:
		start = subscriptions.NewDate(time.Date(end.Time.Year(), time.January, 1, 0, 0, 0, 0, time.UTC))
	case MonthlyBudgetBulkScopeRecentYear:
		start = subscriptions.NewDate(end.Time.AddDate(0, -11, 0))
	case MonthlyBudgetBulkScopeAllHistory:
		earliest, err := s.repo.EarliestSubscriptionMonth(ctx)
		if err != nil {
			return BulkUpsertMonthlyBudgetResult{}, err
		}
		if earliest != nil && !earliest.Time.IsZero() {
			start = monthStart(earliest.Time)
		}
	}
	if start.Time.After(end.Time) {
		start = end
	}

	months := monthsInRange(start, end)
	upserts := make([]UpsertMonthlyBudgetInput, 0, len(months))
	for _, month := range months {
		upserts = append(upserts, UpsertMonthlyBudgetInput{
			BudgetMonth:  month,
			BaseCurrency: input.BaseCurrency,
			MonthlyLimit: input.MonthlyLimit,
			WarningPct:   input.WarningPct,
			Note:         input.Note,
		})
	}
	records, err := s.repo.UpsertMonthlyBudgets(ctx, upserts)
	if err != nil {
		return BulkUpsertMonthlyBudgetResult{}, err
	}
	return BulkUpsertMonthlyBudgetResult{
		Scope:      input.Scope,
		StartMonth: start,
		EndMonth:   end,
		Records:    records,
	}, nil
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
	return applyBudgetSpend(rows, budgets, settings.BaseCurrency), nil
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
	budgets := applyBudgetSpend(rows, []BudgetRecord{budget}, settings.BaseCurrency)
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

func monthStart(value time.Time) subscriptions.Date {
	year, month, _ := value.UTC().Date()
	return subscriptions.NewDate(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC))
}

func monthsInRange(start, end subscriptions.Date) []subscriptions.Date {
	if start.Time.IsZero() || end.Time.IsZero() || start.Time.After(end.Time) {
		return nil
	}
	months := make([]subscriptions.Date, 0)
	for current := monthStart(start.Time); !current.Time.After(end.Time); current = subscriptions.NewDate(current.Time.AddDate(0, 1, 0)) {
		months = append(months, current)
	}
	return months
}

func applyBudgetSpend(rows []CostRow, budgets []BudgetRecord, baseCurrency string) []BudgetRecord {
	rows, _ = splitCostRows(rows)
	for i := range budgets {
		if !budgets[i].Enabled {
			budgets[i].CurrentMonthlySpend = new(0.0)
			budgets[i].CurrentYearlySpend = new(0.0)
			budgets[i].Status = BudgetStatusDisabled
			continue
		}
		budgets[i].CurrentMonthlySpend = nil
		budgets[i].CurrentYearlySpend = nil
		if budgets[i].BaseCurrency != baseCurrency {
			budgets[i].Status = BudgetStatusUnknown
			continue
		}

		var currentMonthly float64
		incomplete := false
		unitMismatch := false
		for _, row := range rows {
			if !budgetMatchesRow(budgets[i], row) {
				continue
			}
			if row.BaseCurrency != baseCurrency {
				unitMismatch = true
				break
			}
			if row.MonthlyPriceBase == nil {
				incomplete = true
				continue
			}
			currentMonthly += *row.MonthlyPriceBase
		}
		if unitMismatch {
			budgets[i].Status = BudgetStatusUnknown
			continue
		}

		status := EvaluateBudgetStatus(
			budgets[i].Enabled,
			currentMonthly,
			budgets[i].MonthlyLimit,
			budgets[i].YearlyLimit,
			budgets[i].WarningPct,
		)
		budgets[i].Status = status
		if incomplete {
			if status != BudgetStatusOver {
				budgets[i].Status = BudgetStatusUnknown
			}
			continue
		}
		budgets[i].CurrentMonthlySpend = new(currentMonthly)
		budgets[i].CurrentYearlySpend = new(currentMonthly * 12)
	}
	return budgets
}

func applyRowBudgetStatus(rows []CostRow, budgets []BudgetRecord) {
	for i := range rows {
		if rows[i].LifecycleStatus == "archived" || rows[i].MonthlyPriceBase == nil {
			rows[i].BudgetStatus = BudgetStatusUnknown
			continue
		}
		var status BudgetStatus
		matched := false
		for _, budget := range budgets {
			if !budgetMatchesRow(budget, rows[i]) {
				continue
			}
			if !matched {
				status = budget.Status
				matched = true
				continue
			}
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
		case BudgetStatusUnknown:
			return 2
		case BudgetStatusOK:
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
		if row.YearlyPriceBase != nil {
			item.YearlyCost += *row.YearlyPriceBase
		} else {
			item.YearlyCost += *row.MonthlyPriceBase * 12
		}
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
		CurrentMonthlySpend: new(monthlyCost),
		CurrentYearlySpend:  new(monthlyCost * 12),
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
	return row.RenewalDecision == "cancel" && row.AutoRenewCheck != "disabled" && row.AutoRenewCheck != "never_enabled" && row.AutoRenewCheck != "unsupported"
}

func splitCostRows(rows []CostRow) (current, archived []CostRow) {
	current = make([]CostRow, 0)
	archived = make([]CostRow, 0)
	for _, row := range rows {
		if row.LifecycleStatus == "archived" {
			if potentialProviderCharge(row.AutoRenewCheck) {
				archived = append(archived, row)
			}
		} else {
			current = append(current, row)
		}
	}
	return current, archived
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

func potentialProviderCharge(check string) bool {
	return check != "disabled" && check != "never_enabled" && check != "unsupported"
}
