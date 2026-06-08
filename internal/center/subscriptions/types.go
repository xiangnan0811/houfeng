package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"houfeng/internal/center/vpsassets"
)

var ErrSubscriptionNotFound = errors.New("subscription not found")
var ErrInvalidSubscriptionInput = errors.New("invalid subscription input")

type Status string

type BillingPeriodUnit string

type RenewalMode string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
	StatusUnknown   Status = "unknown"

	DefaultStatus = StatusActive

	BillingPeriodDay   BillingPeriodUnit = "day"
	BillingPeriodWeek  BillingPeriodUnit = "week"
	BillingPeriodMonth BillingPeriodUnit = "month"
	BillingPeriodYear  BillingPeriodUnit = "year"

	DefaultBillingPeriodUnit   = BillingPeriodMonth
	DefaultBillingPeriodLength = 1

	RenewalModeAuto          RenewalMode = "auto"
	RenewalModeManual        RenewalMode = "manual"
	RenewalModeAutoCancelled RenewalMode = "auto_cancelled"
	RenewalModeLottery       RenewalMode = "lottery"
	RenewalModeBonus         RenewalMode = "bonus"
	RenewalModeOther         RenewalMode = "other"

	DefaultRenewalMode = RenewalModeManual

	DateLayout = "2006-01-02"

	SortRenewAt = "renew_at"
	OrderAsc    = "asc"
	OrderDesc   = "desc"
)

type Date struct {
	Time time.Time
}

type Record struct {
	SubscriptionID      string     `json:"subscription_id"`
	VPSID               string     `json:"vps_id"`
	Price               float64    `json:"price"`
	Currency            string     `json:"currency"`
	BillingCycle        string     `json:"billing_cycle"`
	BillingMonths       int        `json:"billing_months"`
	BillingPeriodUnit   string     `json:"billing_period_unit"`
	BillingPeriodLength int        `json:"billing_period_length"`
	MonthlyPrice        float64    `json:"monthly_price"`
	StartedAt           *Date      `json:"started_at"`
	RenewAt             *Date      `json:"renew_at"`
	AutoRenew           bool       `json:"auto_renew"`
	AutoRenewCancelled  bool       `json:"auto_renew_cancelled"`
	RenewalMode         string     `json:"renewal_mode"`
	Status              Status     `json:"status"`
	PaymentMethod       string     `json:"payment_method"`
	DisplayName         string     `json:"display_name"`
	CostCategory        string     `json:"cost_category"`
	Labels              []string   `json:"labels"`
	TrialEndsAt         *Date      `json:"trial_ends_at"`
	EndsAt              *Date      `json:"ends_at"`
	Note                string     `json:"note"`
	MonthlyPriceBase    *float64   `json:"monthly_price_base,omitempty"`
	YearlyPriceBase     *float64   `json:"yearly_price_base,omitempty"`
	BaseCurrency        string     `json:"base_currency,omitempty"`
	ExchangeRate        *float64   `json:"exchange_rate,omitempty"`
	ExchangeRateDate    *Date      `json:"exchange_rate_date,omitempty"`
	ExchangeRateStale   bool       `json:"exchange_rate_stale,omitempty"`
	BudgetStatus        string     `json:"budget_status,omitempty"`
	NextReminderAt      *time.Time `json:"next_reminder_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CreateInput struct {
	VPSID               string   `json:"vps_id"`
	Price               float64  `json:"price"`
	Currency            string   `json:"currency"`
	BillingCycle        string   `json:"billing_cycle"`
	BillingMonths       int      `json:"billing_months"`
	BillingPeriodUnit   string   `json:"billing_period_unit"`
	BillingPeriodLength int      `json:"billing_period_length"`
	StartedAt           *Date    `json:"started_at"`
	RenewAt             *Date    `json:"renew_at"`
	AutoRenew           bool     `json:"auto_renew"`
	AutoRenewCancelled  bool     `json:"auto_renew_cancelled"`
	RenewalMode         string   `json:"renewal_mode"`
	Status              Status   `json:"status"`
	PaymentMethod       string   `json:"payment_method"`
	DisplayName         string   `json:"display_name"`
	CostCategory        string   `json:"cost_category"`
	Labels              []string `json:"labels"`
	TrialEndsAt         *Date    `json:"trial_ends_at"`
	EndsAt              *Date    `json:"ends_at"`
	Note                string   `json:"note"`
}

type PatchInput struct {
	VPSID               OptionalString `json:"vps_id"`
	Price               OptionalFloat  `json:"price"`
	Currency            OptionalString `json:"currency"`
	BillingCycle        OptionalString `json:"billing_cycle"`
	BillingMonths       OptionalInt    `json:"billing_months"`
	BillingPeriodUnit   OptionalString `json:"billing_period_unit"`
	BillingPeriodLength OptionalInt    `json:"billing_period_length"`
	StartedAt           OptionalDate   `json:"started_at"`
	RenewAt             OptionalDate   `json:"renew_at"`
	AutoRenew           OptionalBool   `json:"auto_renew"`
	AutoRenewCancelled  OptionalBool   `json:"auto_renew_cancelled"`
	RenewalMode         OptionalString `json:"renewal_mode"`
	Status              OptionalStatus `json:"status"`
	PaymentMethod       OptionalString `json:"payment_method"`
	DisplayName         OptionalString `json:"display_name"`
	CostCategory        OptionalString `json:"cost_category"`
	Labels              OptionalLabels `json:"labels"`
	TrialEndsAt         OptionalDate   `json:"trial_ends_at"`
	EndsAt              OptionalDate   `json:"ends_at"`
	Note                OptionalString `json:"note"`
}

type ListFilters struct {
	VPSID           string
	Status          Status
	RenewBefore     *Date
	RenewAfter      *Date
	RenewWithinDays *int
	Currency        string
	ProviderID      string
	BudgetStatus    string
	AutoRenew       *bool
	PaymentMethod   string
	Label           string
	RenewalDecision string
	Sort            string
	Order           string
	AssetScope      vpsassets.AssetScope
}

type OptionalString struct {
	Set   bool
	Value string
}

type OptionalFloat struct {
	Set   bool
	Value float64
}

type OptionalInt struct {
	Set   bool
	Value int
}

type OptionalDate struct {
	Set   bool
	Value *Date
}

type OptionalBool struct {
	Set   bool
	Value bool
}

type OptionalStatus struct {
	Set   bool
	Value Status
}

type OptionalLabels struct {
	Set    bool
	Values []string
}

type Repository interface {
	ListSubscriptions(context.Context, ListFilters) ([]Record, error)
	GetSubscription(context.Context, string) (Record, error)
	CreateSubscription(context.Context, CreateInput) (Record, error)
	PatchSubscription(context.Context, string, PatchInput) (Record, error)
}

func ParseDate(value string) (Date, error) {
	parsed, err := time.Parse(DateLayout, strings.TrimSpace(value))
	if err != nil {
		return Date{}, fmt.Errorf("%w: invalid date", ErrInvalidSubscriptionInput)
	}
	return Date{Time: parsed}, nil
}

func NewDate(t time.Time) Date {
	year, month, day := t.Date()
	return Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Time.Format(DateLayout))
}

func (d *Date) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("date value cannot be null")
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseDate(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func PatchString(value string) OptionalString {
	return OptionalString{Set: true, Value: value}
}

func PatchFloat(value float64) OptionalFloat {
	return OptionalFloat{Set: true, Value: value}
}

func PatchInt(value int) OptionalInt {
	return OptionalInt{Set: true, Value: value}
}

func PatchDate(value *Date) OptionalDate {
	return OptionalDate{Set: true, Value: cloneDate(value)}
}

func PatchBool(value bool) OptionalBool {
	return OptionalBool{Set: true, Value: value}
}

func PatchStatus(value Status) OptionalStatus {
	return OptionalStatus{Set: true, Value: value}
}

func PatchLabels(values []string) OptionalLabels {
	return OptionalLabels{Set: true, Values: append([]string(nil), values...)}
}

func (v *OptionalString) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("string value cannot be null")
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalFloat) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("number value cannot be null")
	}

	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalInt) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("integer value cannot be null")
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalDate) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		v.Value = nil
		return nil
	}

	var value Date
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
	return nil
}

func (v *OptionalBool) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("boolean value cannot be null")
	}

	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalStatus) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("status cannot be null")
	}

	var value Status
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalLabels) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("labels cannot be null")
	}

	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	v.Values = values
	return nil
}

func NormalizeCreateInput(input CreateInput) CreateInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.Currency = NormalizeCurrency(input.Currency)
	input.BillingCycle = strings.TrimSpace(input.BillingCycle)
	input.BillingPeriodUnit = NormalizeBillingPeriodUnit(input.BillingPeriodUnit)
	if input.BillingPeriodUnit == "" {
		input.BillingPeriodUnit = string(DefaultBillingPeriodUnit)
	}
	if input.BillingPeriodLength <= 0 {
		if input.BillingMonths > 0 && input.BillingPeriodUnit == string(BillingPeriodMonth) {
			input.BillingPeriodLength = input.BillingMonths
		}
	}
	if input.BillingPeriodLength > 0 {
		input.BillingMonths = BillingMonthsForPeriod(input.BillingPeriodUnit, input.BillingPeriodLength)
	}
	if input.BillingCycle == "" && input.BillingPeriodLength > 0 {
		input.BillingCycle = BillingCycleForPeriod(input.BillingPeriodUnit, input.BillingPeriodLength)
	}
	input.Status = Status(strings.TrimSpace(string(input.Status)))
	if input.Status == "" {
		input.Status = DefaultStatus
	}
	input.RenewalMode = NormalizeRenewalMode(input.RenewalMode)
	if input.RenewalMode == "" {
		input.RenewalMode = string(RenewalModeFromLegacyFlags(input.AutoRenew, input.AutoRenewCancelled))
	}
	input.AutoRenew, input.AutoRenewCancelled = LegacyRenewalFlags(input.RenewalMode)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.CostCategory = strings.TrimSpace(input.CostCategory)
	input.Labels = NormalizeLabels(input.Labels)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidSubscriptionInput)
	}
	if !IsValidPrice(input.Price) {
		return fmt.Errorf("%w: price must be non-negative", ErrInvalidSubscriptionInput)
	}
	if input.BillingMonths <= 0 {
		return fmt.Errorf("%w: billing_months must be greater than zero", ErrInvalidSubscriptionInput)
	}
	if !IsValidBillingPeriodUnit(input.BillingPeriodUnit) {
		return fmt.Errorf("%w: invalid billing_period_unit", ErrInvalidSubscriptionInput)
	}
	if input.BillingPeriodLength <= 0 {
		return fmt.Errorf("%w: billing_period_length must be greater than zero", ErrInvalidSubscriptionInput)
	}
	if !IsValidCurrency(input.Currency) {
		return fmt.Errorf("%w: currency must be a 3-letter uppercase code", ErrInvalidSubscriptionInput)
	}
	if !IsValidRenewalMode(input.RenewalMode) {
		return fmt.Errorf("%w: invalid renewal_mode", ErrInvalidSubscriptionInput)
	}
	if !IsValidStatus(input.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidSubscriptionInput)
	}
	return nil
}

func NormalizePatchInput(input PatchInput) PatchInput {
	input.VPSID = normalizeOptionalString(input.VPSID)
	input.Currency = normalizeOptionalCurrency(input.Currency)
	input.BillingCycle = normalizeOptionalString(input.BillingCycle)
	input.BillingPeriodUnit = normalizeOptionalBillingPeriodUnit(input.BillingPeriodUnit)
	if input.BillingMonths.Set || input.BillingPeriodUnit.Set || input.BillingPeriodLength.Set {
		unit := input.BillingPeriodUnit.Value
		if !input.BillingPeriodUnit.Set || unit == "" {
			unit = string(BillingPeriodMonth)
		}
		length := input.BillingPeriodLength.Value
		if !input.BillingPeriodLength.Set {
			if input.BillingMonths.Set && unit == string(BillingPeriodMonth) {
				length = input.BillingMonths.Value
			} else {
				length = DefaultBillingPeriodLength
			}
		}
		input.BillingPeriodUnit = PatchString(unit)
		input.BillingPeriodLength = PatchInt(length)
		if length > 0 {
			input.BillingMonths = PatchInt(BillingMonthsForPeriod(unit, length))
		}
		if (!input.BillingCycle.Set || input.BillingCycle.Value == "") && length > 0 {
			input.BillingCycle = PatchString(BillingCycleForPeriod(unit, length))
		}
	}
	if input.Status.Set {
		input.Status.Value = Status(strings.TrimSpace(string(input.Status.Value)))
	}
	input.RenewalMode = normalizeOptionalRenewalMode(input.RenewalMode)
	if input.RenewalMode.Set {
		autoRenew, autoRenewCancelled := LegacyRenewalFlags(input.RenewalMode.Value)
		input.AutoRenew = PatchBool(autoRenew)
		input.AutoRenewCancelled = PatchBool(autoRenewCancelled)
	} else if input.AutoRenew.Set || input.AutoRenewCancelled.Set {
		renewalMode := string(RenewalModeFromLegacyFlags(input.AutoRenew.Set && input.AutoRenew.Value, input.AutoRenewCancelled.Set && input.AutoRenewCancelled.Value))
		input.RenewalMode = PatchString(renewalMode)
		autoRenew, autoRenewCancelled := LegacyRenewalFlags(renewalMode)
		input.AutoRenew = PatchBool(autoRenew)
		input.AutoRenewCancelled = PatchBool(autoRenewCancelled)
	}
	input.PaymentMethod = normalizeOptionalString(input.PaymentMethod)
	input.DisplayName = normalizeOptionalString(input.DisplayName)
	input.CostCategory = normalizeOptionalString(input.CostCategory)
	if input.Labels.Set {
		input.Labels.Values = NormalizeLabels(input.Labels.Values)
	}
	input.Note = normalizeOptionalString(input.Note)
	return input
}

func ValidatePatchInput(input PatchInput) error {
	if input.VPSID.Set && input.VPSID.Value == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidSubscriptionInput)
	}
	if input.Price.Set && !IsValidPrice(input.Price.Value) {
		return fmt.Errorf("%w: price must be non-negative", ErrInvalidSubscriptionInput)
	}
	if input.BillingMonths.Set && input.BillingMonths.Value <= 0 {
		return fmt.Errorf("%w: billing_months must be greater than zero", ErrInvalidSubscriptionInput)
	}
	if input.BillingPeriodUnit.Set && !IsValidBillingPeriodUnit(input.BillingPeriodUnit.Value) {
		return fmt.Errorf("%w: invalid billing_period_unit", ErrInvalidSubscriptionInput)
	}
	if input.BillingPeriodLength.Set && input.BillingPeriodLength.Value <= 0 {
		return fmt.Errorf("%w: billing_period_length must be greater than zero", ErrInvalidSubscriptionInput)
	}
	if input.Currency.Set && !IsValidCurrency(input.Currency.Value) {
		return fmt.Errorf("%w: currency must be a 3-letter uppercase code", ErrInvalidSubscriptionInput)
	}
	if input.RenewalMode.Set && !IsValidRenewalMode(input.RenewalMode.Value) {
		return fmt.Errorf("%w: invalid renewal_mode", ErrInvalidSubscriptionInput)
	}
	if input.Status.Set && !IsValidStatus(input.Status.Value) {
		return fmt.Errorf("%w: invalid status", ErrInvalidSubscriptionInput)
	}
	return nil
}

func (input PatchInput) HasChanges() bool {
	return input.VPSID.Set ||
		input.Price.Set ||
		input.Currency.Set ||
		input.BillingCycle.Set ||
		input.BillingMonths.Set ||
		input.BillingPeriodUnit.Set ||
		input.BillingPeriodLength.Set ||
		input.StartedAt.Set ||
		input.RenewAt.Set ||
		input.AutoRenew.Set ||
		input.AutoRenewCancelled.Set ||
		input.RenewalMode.Set ||
		input.Status.Set ||
		input.PaymentMethod.Set ||
		input.DisplayName.Set ||
		input.CostCategory.Set ||
		input.Labels.Set ||
		input.TrialEndsAt.Set ||
		input.EndsAt.Set ||
		input.Note.Set
}

func NormalizeListFilters(filters ListFilters) ListFilters {
	filters.VPSID = strings.TrimSpace(filters.VPSID)
	filters.Status = Status(strings.TrimSpace(string(filters.Status)))
	filters.Currency = NormalizeCurrency(filters.Currency)
	filters.ProviderID = strings.TrimSpace(filters.ProviderID)
	filters.BudgetStatus = strings.ToLower(strings.TrimSpace(filters.BudgetStatus))
	filters.PaymentMethod = strings.TrimSpace(filters.PaymentMethod)
	filters.Label = strings.TrimSpace(filters.Label)
	filters.RenewalDecision = NormalizeRenewalMode(filters.RenewalDecision)
	filters.AssetScope = vpsassets.AssetScope(strings.TrimSpace(string(filters.AssetScope)))
	filters.Sort = strings.ToLower(strings.TrimSpace(filters.Sort))
	if filters.Sort == "" {
		filters.Sort = SortRenewAt
	}
	filters.Order = strings.ToLower(strings.TrimSpace(filters.Order))
	if filters.Order == "" {
		filters.Order = OrderAsc
	}
	return filters
}

func ValidateListFilters(filters ListFilters) error {
	if filters.Status != "" && !IsValidStatus(filters.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidSubscriptionInput)
	}
	if filters.RenewWithinDays != nil && *filters.RenewWithinDays < 0 {
		return fmt.Errorf("%w: renew_within_days must be non-negative", ErrInvalidSubscriptionInput)
	}
	if filters.Currency != "" && !IsValidCurrency(filters.Currency) {
		return fmt.Errorf("%w: invalid currency", ErrInvalidSubscriptionInput)
	}
	if filters.BudgetStatus != "" {
		switch filters.BudgetStatus {
		case "ok", "warning", "over", "unknown", "disabled":
		default:
			return fmt.Errorf("%w: invalid budget_status", ErrInvalidSubscriptionInput)
		}
	}
	if filters.RenewalDecision != "" && !IsValidRenewalMode(filters.RenewalDecision) {
		return fmt.Errorf("%w: invalid renewal_decision", ErrInvalidSubscriptionInput)
	}
	if filters.AssetScope != "" && !vpsassets.IsValidAssetScope(filters.AssetScope) {
		return fmt.Errorf("%w: invalid asset_scope", ErrInvalidSubscriptionInput)
	}
	if filters.Sort != SortRenewAt {
		return fmt.Errorf("%w: invalid sort", ErrInvalidSubscriptionInput)
	}
	if filters.Order != OrderAsc && filters.Order != OrderDesc {
		return fmt.Errorf("%w: invalid order", ErrInvalidSubscriptionInput)
	}
	return nil
}

func NormalizeLabels(labels []string) []string {
	normalized := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		normalized = append(normalized, label)
	}
	return normalized
}

func NormalizeCurrency(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func NormalizeBillingPeriodUnit(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeRenewalMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidCurrency(value string) bool {
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

func IsValidPrice(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maxPrice {
		return false
	}
	scaled := value * 100
	return math.Abs(scaled-math.Round(scaled)) <= priceScaleEpsilon
}

func IsValidStatus(status Status) bool {
	switch status {
	case StatusActive, StatusPaused, StatusCancelled, StatusExpired, StatusUnknown:
		return true
	default:
		return false
	}
}

func IsValidBillingPeriodUnit(value string) bool {
	switch BillingPeriodUnit(NormalizeBillingPeriodUnit(value)) {
	case BillingPeriodDay, BillingPeriodWeek, BillingPeriodMonth, BillingPeriodYear:
		return true
	default:
		return false
	}
}

func IsValidRenewalMode(value string) bool {
	switch RenewalMode(NormalizeRenewalMode(value)) {
	case RenewalModeAuto, RenewalModeManual, RenewalModeAutoCancelled, RenewalModeLottery, RenewalModeBonus, RenewalModeOther:
		return true
	default:
		return false
	}
}

func RenewalModeFromLegacyFlags(autoRenew, autoRenewCancelled bool) RenewalMode {
	switch {
	case autoRenewCancelled:
		return RenewalModeAutoCancelled
	case autoRenew:
		return RenewalModeAuto
	default:
		return DefaultRenewalMode
	}
}

func LegacyRenewalFlags(mode string) (bool, bool) {
	switch RenewalMode(NormalizeRenewalMode(mode)) {
	case RenewalModeAuto:
		return true, false
	case RenewalModeAutoCancelled:
		return false, true
	default:
		return false, false
	}
}

func BillingMonthsForPeriod(unit string, length int) int {
	if length <= 0 {
		length = DefaultBillingPeriodLength
	}
	switch BillingPeriodUnit(NormalizeBillingPeriodUnit(unit)) {
	case BillingPeriodYear:
		return length * 12
	case BillingPeriodMonth:
		return length
	case BillingPeriodWeek:
		return max(1, int(math.Ceil(float64(length*7)/30)))
	case BillingPeriodDay:
		return max(1, int(math.Ceil(float64(length)/30)))
	default:
		return length
	}
}

func BillingCycleForPeriod(unit string, length int) string {
	if length <= 0 {
		length = DefaultBillingPeriodLength
	}
	switch BillingPeriodUnit(NormalizeBillingPeriodUnit(unit)) {
	case BillingPeriodDay:
		if length == 1 {
			return "daily"
		}
		return fmt.Sprintf("%d days", length)
	case BillingPeriodWeek:
		if length == 1 {
			return "weekly"
		}
		return fmt.Sprintf("%d weeks", length)
	case BillingPeriodMonth:
		if length == 1 {
			return "monthly"
		}
		return fmt.Sprintf("%d months", length)
	case BillingPeriodYear:
		if length == 1 {
			return "annual"
		}
		return fmt.Sprintf("%d years", length)
	default:
		return ""
	}
}

func CalculateMonthlyPrice(price float64, billingMonths int) float64 {
	if billingMonths <= 0 {
		return 0
	}
	return math.Round((price/float64(billingMonths))*10000) / 10000
}

func CalculateMonthlyPriceForPeriod(price float64, unit string, length int) float64 {
	if length <= 0 {
		return 0
	}
	var months float64
	switch BillingPeriodUnit(NormalizeBillingPeriodUnit(unit)) {
	case BillingPeriodDay:
		months = float64(length) / 30
	case BillingPeriodWeek:
		months = float64(length*7) / 30
	case BillingPeriodMonth:
		months = float64(length)
	case BillingPeriodYear:
		months = float64(length * 12)
	default:
		return CalculateMonthlyPrice(price, length)
	}
	if months <= 0 {
		return 0
	}
	return math.Round((price/months)*10000) / 10000
}

func DateFromTimePtr(value *time.Time) *Date {
	if value == nil {
		return nil
	}
	date := NewDate(*value)
	return &date
}

func TimePtrFromDate(value *Date) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.Time
	return &cloned
}

func normalizeOptionalString(value OptionalString) OptionalString {
	if value.Set {
		value.Value = strings.TrimSpace(value.Value)
	}
	return value
}

func normalizeOptionalCurrency(value OptionalString) OptionalString {
	if value.Set {
		value.Value = NormalizeCurrency(value.Value)
	}
	return value
}

func normalizeOptionalBillingPeriodUnit(value OptionalString) OptionalString {
	if value.Set {
		value.Value = NormalizeBillingPeriodUnit(value.Value)
	}
	return value
}

func normalizeOptionalRenewalMode(value OptionalString) OptionalString {
	if value.Set {
		value.Value = NormalizeRenewalMode(value.Value)
	}
	return value
}

func cloneDate(value *Date) *Date {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

const (
	maxPrice          = 9999999999.99
	priceScaleEpsilon = 1e-9
)
