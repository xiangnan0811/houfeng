package subscriptioncosts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
)

var ErrInvalidInput = errors.New("invalid subscription cost input")
var ErrBudgetNotFound = errors.New("subscription budget not found")

type BudgetScopeType string
type BudgetStatus string
type ReminderKind string

const (
	BudgetScopeGlobal   BudgetScopeType = "global"
	BudgetScopeProvider BudgetScopeType = "provider"
	BudgetScopeLabel    BudgetScopeType = "label"
	BudgetScopeCategory BudgetScopeType = "category"
	BudgetScopeVPS      BudgetScopeType = "vps"

	BudgetStatusDisabled BudgetStatus = "disabled"
	BudgetStatusOK       BudgetStatus = "ok"
	BudgetStatusWarning  BudgetStatus = "warning"
	BudgetStatusOver     BudgetStatus = "over"
	BudgetStatusUnknown  BudgetStatus = "unknown"

	ReminderKindRenewal           ReminderKind = "renewal"
	ReminderKindDecisionAttention ReminderKind = "decision_attention"

	StatisticsWindowMonth   = "month"
	StatisticsWindowQuarter = "quarter"
	StatisticsWindowYear    = "year"
)

type SettingsRepository interface {
	GetSettings(context.Context) (centersettings.CenterSettings, error)
	PutSettings(context.Context, centersettings.CenterSettings) (centersettings.CenterSettings, error)
}

type Repository interface {
	ListCostRows(context.Context, centersettings.SubscriptionCostSettings) ([]CostRow, error)
	ListMissingSubscriptionAssets(context.Context) ([]MissingSubscriptionAsset, error)
	ListBudgets(context.Context, BudgetListFilters) ([]BudgetRecord, error)
	CreateBudget(context.Context, CreateBudgetInput) (BudgetRecord, error)
	PatchBudget(context.Context, PatchBudgetInput) (BudgetRecord, error)
	ListActiveCurrencies(context.Context) ([]string, error)
	UpsertExchangeRate(context.Context, ExchangeRateUpsert) (ExchangeRateRecord, error)
	ListReminderCandidates(context.Context, centersettings.SubscriptionCostSettings, []int) ([]ReminderCandidate, error)
	TryCreateReminderDelivery(context.Context, ReminderDeliveryInput) (string, bool, error)
	UpdateReminderDelivery(context.Context, string, ReminderDeliveryUpdate) error
}

type CostRow struct {
	SubscriptionID    string              `json:"subscription_id"`
	VPSID             string              `json:"vps_id"`
	VPSDisplayName    string              `json:"vps_display_name"`
	ProviderID        string              `json:"provider_id"`
	ProviderName      string              `json:"provider_name"`
	DisplayName       string              `json:"display_name"`
	CostCategory      string              `json:"cost_category"`
	Labels            []string            `json:"labels"`
	Price             float64             `json:"price"`
	Currency          string              `json:"currency"`
	MonthlyPrice      float64             `json:"monthly_price"`
	MonthlyPriceBase  *float64            `json:"monthly_price_base"`
	YearlyPriceBase   *float64            `json:"yearly_price_base"`
	BaseCurrency      string              `json:"base_currency"`
	ExchangeRate      *float64            `json:"exchange_rate"`
	ExchangeRateDate  *subscriptions.Date `json:"exchange_rate_date"`
	ExchangeRateStale bool                `json:"exchange_rate_stale"`
	RenewAt           *subscriptions.Date `json:"renew_at"`
	NextReminderAt    *time.Time          `json:"next_reminder_at"`
	Status            string              `json:"status"`
	PaymentMethod     string              `json:"payment_method"`
	LifecycleStatus   string              `json:"lifecycle_status"`
	RenewalDecision   string              `json:"renewal_decision"`
	BudgetStatus      BudgetStatus        `json:"budget_status"`
}

type MissingSubscriptionAsset struct {
	VPSID           string `json:"vps_id"`
	DisplayName     string `json:"display_name"`
	ProviderID      string `json:"provider_id,omitempty"`
	ProviderName    string `json:"provider_name"`
	LifecycleStatus string `json:"lifecycle_status"`
	RenewalDecision string `json:"renewal_decision"`
}

type Overview struct {
	SnapshotGeneratedAt         time.Time                  `json:"snapshot_generated_at"`
	BaseCurrency                string                     `json:"base_currency"`
	TotalMonthlyCost            float64                    `json:"total_monthly_cost"`
	TotalYearlyCost             float64                    `json:"total_yearly_cost"`
	ActiveSubscriptionCount     int                        `json:"active_subscription_count"`
	RenewalDue14dCount          int                        `json:"renewal_due_14d_count"`
	RenewalDue30dCount          int                        `json:"renewal_due_30d_count"`
	BudgetRiskCount             int                        `json:"budget_risk_count"`
	ExchangeRateStaleCount      int                        `json:"exchange_rate_stale_count"`
	DecisionAttentionCount      int                        `json:"decision_attention_count"`
	MissingSubscriptionVPSCount int                        `json:"missing_subscription_vps_count"`
	UpcomingRenewals            []RenewalQueueItem         `json:"upcoming_renewals"`
	ProviderBreakdown           []BreakdownItem            `json:"provider_breakdown"`
	CurrencyBreakdown           []BreakdownItem            `json:"currency_breakdown"`
	CategoryBreakdown           []BreakdownItem            `json:"category_breakdown"`
	BudgetRisks                 []BudgetRecord             `json:"budget_risks"`
	VPSCosts                    []CostRow                  `json:"vps_costs"`
	MissingSubscriptionAssets   []MissingSubscriptionAsset `json:"missing_subscription_assets"`
}

type Statistics struct {
	Window              string          `json:"window"`
	BaseCurrency        string          `json:"base_currency"`
	TotalMonthlyCost    float64         `json:"total_monthly_cost"`
	TotalYearlyCost     float64         `json:"total_yearly_cost"`
	ProviderBreakdown   []BreakdownItem `json:"provider_breakdown"`
	CurrencyBreakdown   []BreakdownItem `json:"currency_breakdown"`
	CategoryBreakdown   []BreakdownItem `json:"category_breakdown"`
	RenewalMonthBuckets []SeriesPoint   `json:"renewal_month_buckets"`
	BudgetStatuses      []BudgetRecord  `json:"budget_statuses"`
}

type RenewalQueueItem struct {
	SubscriptionID    string              `json:"subscription_id"`
	VPSID             string              `json:"vps_id"`
	VPSDisplayName    string              `json:"vps_display_name"`
	DisplayName       string              `json:"display_name"`
	ProviderName      string              `json:"provider_name"`
	RenewAt           *subscriptions.Date `json:"renew_at"`
	MonthlyPriceBase  *float64            `json:"monthly_price_base"`
	YearlyPriceBase   *float64            `json:"yearly_price_base"`
	BaseCurrency      string              `json:"base_currency"`
	Currency          string              `json:"currency"`
	RenewalDecision   string              `json:"renewal_decision"`
	LifecycleStatus   string              `json:"lifecycle_status"`
	ExchangeRateStale bool                `json:"exchange_rate_stale"`
}

type BreakdownItem struct {
	Key               string  `json:"key"`
	Label             string  `json:"label"`
	MonthlyCost       float64 `json:"monthly_cost"`
	YearlyCost        float64 `json:"yearly_cost"`
	SubscriptionCount int     `json:"subscription_count"`
}

type SeriesPoint struct {
	Bucket       string  `json:"bucket"`
	MonthlyCost  float64 `json:"monthly_cost"`
	RenewalCount int     `json:"renewal_count"`
}

type BudgetRecord struct {
	BudgetID            string       `json:"budget_id"`
	ScopeType           string       `json:"scope_type"`
	ScopeID             string       `json:"scope_id"`
	Name                string       `json:"name"`
	BaseCurrency        string       `json:"base_currency"`
	MonthlyLimit        *float64     `json:"monthly_limit"`
	YearlyLimit         *float64     `json:"yearly_limit"`
	WarningPct          int          `json:"warning_pct"`
	Enabled             bool         `json:"enabled"`
	Note                string       `json:"note"`
	CurrentMonthlySpend float64      `json:"current_monthly_spend"`
	CurrentYearlySpend  float64      `json:"current_yearly_spend"`
	Status              BudgetStatus `json:"status"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type BudgetListFilters struct {
	ScopeType string
	ScopeID   string
	Enabled   *bool
}

type CreateBudgetInput struct {
	ScopeType    string   `json:"scope_type"`
	ScopeID      string   `json:"scope_id"`
	Name         string   `json:"name"`
	BaseCurrency string   `json:"base_currency"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	YearlyLimit  *float64 `json:"yearly_limit"`
	WarningPct   int      `json:"warning_pct"`
	Enabled      bool     `json:"enabled"`
	Note         string   `json:"note"`
}

type PatchBudgetInput struct {
	BudgetID     string
	ScopeType    OptionalString
	ScopeID      OptionalString
	Name         OptionalString
	BaseCurrency OptionalString
	MonthlyLimit OptionalNullableFloat
	YearlyLimit  OptionalNullableFloat
	WarningPct   OptionalInt
	Enabled      OptionalBool
	Note         OptionalString
}

type OptionalString struct {
	Set   bool
	Value string
}

type OptionalNullableFloat struct {
	Set   bool
	Value *float64
}

type OptionalInt struct {
	Set   bool
	Value int
}

type OptionalBool struct {
	Set   bool
	Value bool
}

type ExchangeRateRecord struct {
	RateID        string             `json:"rate_id"`
	Provider      string             `json:"provider"`
	BaseCurrency  string             `json:"base_currency"`
	QuoteCurrency string             `json:"quote_currency"`
	Rate          float64            `json:"rate"`
	RateDate      subscriptions.Date `json:"rate_date"`
	FetchedAt     time.Time          `json:"fetched_at"`
	Stale         bool               `json:"stale"`
	ErrorSummary  string             `json:"error_summary"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type ExchangeRateUpsert struct {
	Provider      string
	BaseCurrency  string
	QuoteCurrency string
	Rate          float64
	RateDate      subscriptions.Date
	FetchedAt     time.Time
	ErrorSummary  string
}

type ExchangeRateFetchResult struct {
	QuoteCurrency string             `json:"quote_currency"`
	BaseCurrency  string             `json:"base_currency"`
	Rate          float64            `json:"rate"`
	RateDate      subscriptions.Date `json:"rate_date"`
	Error         string             `json:"error,omitempty"`
}

type ExchangeRateRefreshResult struct {
	Provider     string                    `json:"provider"`
	BaseCurrency string                    `json:"base_currency"`
	FetchedAt    time.Time                 `json:"fetched_at"`
	Succeeded    []ExchangeRateFetchResult `json:"succeeded"`
	Failed       []ExchangeRateFetchResult `json:"failed"`
}

type FetchedExchangeRate struct {
	Rate     float64
	RateDate subscriptions.Date
}

type ExchangeRateProvider interface {
	FetchRate(context.Context, string, string) (FetchedExchangeRate, error)
}

type ReminderCandidate struct {
	SubscriptionID   string
	VPSID            string
	VPSDisplayName   string
	DisplayName      string
	ProviderName     string
	RenewAt          subscriptions.Date
	OffsetDays       int
	Kind             ReminderKind
	BaseCurrency     string
	MonthlyPriceBase *float64
	RenewalDecision  string
	LifecycleStatus  string
}

type ReminderDeliveryInput struct {
	SubscriptionID string
	VPSID          string
	RenewAt        subscriptions.Date
	OffsetDays     int
	Kind           ReminderKind
	Channel        string
	Status         string
	Summary        string
	SentAt         *time.Time
}

type ReminderDeliveryUpdate struct {
	Status  string
	Summary string
	SentAt  *time.Time
}

func NormalizeCreateBudgetInput(input CreateBudgetInput) CreateBudgetInput {
	input.ScopeType = strings.ToLower(strings.TrimSpace(input.ScopeType))
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	input.Name = strings.TrimSpace(input.Name)
	input.BaseCurrency = strings.ToUpper(strings.TrimSpace(input.BaseCurrency))
	if input.BaseCurrency == "" {
		input.BaseCurrency = "CNY"
	}
	if input.WarningPct == 0 {
		input.WarningPct = 80
	}
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateCreateBudgetInput(input CreateBudgetInput) error {
	if !IsValidBudgetScopeType(input.ScopeType) {
		return fmt.Errorf("%w: invalid budget scope_type", ErrInvalidInput)
	}
	if BudgetScopeType(input.ScopeType) != BudgetScopeGlobal && strings.TrimSpace(input.ScopeID) == "" {
		return fmt.Errorf("%w: budget scope_id is required", ErrInvalidInput)
	}
	if input.Name == "" {
		return fmt.Errorf("%w: budget name is required", ErrInvalidInput)
	}
	if !isCurrencyCode(input.BaseCurrency) {
		return fmt.Errorf("%w: budget base_currency must be a 3-letter uppercase code", ErrInvalidInput)
	}
	if input.MonthlyLimit == nil && input.YearlyLimit == nil {
		return fmt.Errorf("%w: budget requires monthly_limit or yearly_limit", ErrInvalidInput)
	}
	if input.MonthlyLimit != nil && !isValidMoney(*input.MonthlyLimit) {
		return fmt.Errorf("%w: monthly_limit must be non-negative money", ErrInvalidInput)
	}
	if input.YearlyLimit != nil && !isValidMoney(*input.YearlyLimit) {
		return fmt.Errorf("%w: yearly_limit must be non-negative money", ErrInvalidInput)
	}
	if input.WarningPct < 1 || input.WarningPct > 100 {
		return fmt.Errorf("%w: warning_pct must be between 1 and 100", ErrInvalidInput)
	}
	return nil
}

func NormalizePatchBudgetInput(input PatchBudgetInput) PatchBudgetInput {
	input.BudgetID = strings.TrimSpace(input.BudgetID)
	input.ScopeType = normalizeOptionalString(input.ScopeType)
	input.ScopeType.Value = strings.ToLower(input.ScopeType.Value)
	input.ScopeID = normalizeOptionalString(input.ScopeID)
	input.Name = normalizeOptionalString(input.Name)
	input.BaseCurrency = normalizeOptionalString(input.BaseCurrency)
	input.BaseCurrency.Value = strings.ToUpper(input.BaseCurrency.Value)
	input.Note = normalizeOptionalString(input.Note)
	return input
}

func ValidatePatchBudgetInput(input PatchBudgetInput) error {
	if input.BudgetID == "" {
		return fmt.Errorf("%w: budget_id is required", ErrInvalidInput)
	}
	if input.ScopeType.Set && !IsValidBudgetScopeType(input.ScopeType.Value) {
		return fmt.Errorf("%w: invalid budget scope_type", ErrInvalidInput)
	}
	if input.ScopeType.Set && BudgetScopeType(input.ScopeType.Value) != BudgetScopeGlobal && input.ScopeID.Set && input.ScopeID.Value == "" {
		return fmt.Errorf("%w: budget scope_id is required", ErrInvalidInput)
	}
	if input.Name.Set && input.Name.Value == "" {
		return fmt.Errorf("%w: budget name is required", ErrInvalidInput)
	}
	if input.BaseCurrency.Set && !isCurrencyCode(input.BaseCurrency.Value) {
		return fmt.Errorf("%w: budget base_currency must be a 3-letter uppercase code", ErrInvalidInput)
	}
	if input.MonthlyLimit.Set && input.MonthlyLimit.Value != nil && !isValidMoney(*input.MonthlyLimit.Value) {
		return fmt.Errorf("%w: monthly_limit must be non-negative money", ErrInvalidInput)
	}
	if input.YearlyLimit.Set && input.YearlyLimit.Value != nil && !isValidMoney(*input.YearlyLimit.Value) {
		return fmt.Errorf("%w: yearly_limit must be non-negative money", ErrInvalidInput)
	}
	if input.WarningPct.Set && (input.WarningPct.Value < 1 || input.WarningPct.Value > 100) {
		return fmt.Errorf("%w: warning_pct must be between 1 and 100", ErrInvalidInput)
	}
	return nil
}

func NormalizeBudgetListFilters(filters BudgetListFilters) BudgetListFilters {
	filters.ScopeType = strings.ToLower(strings.TrimSpace(filters.ScopeType))
	filters.ScopeID = strings.TrimSpace(filters.ScopeID)
	return filters
}

func ValidateBudgetListFilters(filters BudgetListFilters) error {
	if filters.ScopeType != "" && !IsValidBudgetScopeType(filters.ScopeType) {
		return fmt.Errorf("%w: invalid budget scope_type", ErrInvalidInput)
	}
	return nil
}

func (input PatchBudgetInput) HasChanges() bool {
	return input.ScopeType.Set ||
		input.ScopeID.Set ||
		input.Name.Set ||
		input.BaseCurrency.Set ||
		input.MonthlyLimit.Set ||
		input.YearlyLimit.Set ||
		input.WarningPct.Set ||
		input.Enabled.Set ||
		input.Note.Set
}

func PatchString(value string) OptionalString {
	return OptionalString{Set: true, Value: value}
}

func PatchNullableFloat(value *float64) OptionalNullableFloat {
	if value == nil {
		return OptionalNullableFloat{Set: true}
	}
	cloned := *value
	return OptionalNullableFloat{Set: true, Value: &cloned}
}

func PatchInt(value int) OptionalInt {
	return OptionalInt{Set: true, Value: value}
}

func PatchBool(value bool) OptionalBool {
	return OptionalBool{Set: true, Value: value}
}

func IsValidBudgetScopeType(value string) bool {
	switch BudgetScopeType(strings.ToLower(strings.TrimSpace(value))) {
	case BudgetScopeGlobal, BudgetScopeProvider, BudgetScopeLabel, BudgetScopeCategory, BudgetScopeVPS:
		return true
	default:
		return false
	}
}

func EvaluateBudgetStatus(enabled bool, currentMonthly float64, monthlyLimit, yearlyLimit *float64, warningPct int) BudgetStatus {
	if !enabled {
		return BudgetStatusDisabled
	}
	limit := 0.0
	if monthlyLimit != nil {
		limit = *monthlyLimit
	} else if yearlyLimit != nil {
		limit = *yearlyLimit / 12
	}
	if limit <= 0 {
		return BudgetStatusUnknown
	}
	if currentMonthly >= limit {
		return BudgetStatusOver
	}
	warningRatio := float64(warningPct) / 100
	if warningRatio <= 0 {
		warningRatio = 0.8
	}
	if currentMonthly >= limit*warningRatio {
		return BudgetStatusWarning
	}
	return BudgetStatusOK
}

func SortBreakdowns(items []BreakdownItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MonthlyCost == items[j].MonthlyCost {
			return items[i].Label < items[j].Label
		}
		return items[i].MonthlyCost > items[j].MonthlyCost
	})
}

func normalizeOptionalString(value OptionalString) OptionalString {
	if value.Set {
		value.Value = strings.TrimSpace(value.Value)
	}
	return value
}

func isCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, ch := range value {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func isValidMoney(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return false
	}
	scaled := value * 100
	return math.Abs(scaled-math.Round(scaled)) <= 0.000001
}
