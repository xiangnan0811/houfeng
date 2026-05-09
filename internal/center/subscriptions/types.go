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
)

var ErrSubscriptionNotFound = errors.New("subscription not found")
var ErrInvalidSubscriptionInput = errors.New("invalid subscription input")

type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
	StatusUnknown   Status = "unknown"

	DefaultStatus = StatusActive

	DateLayout = "2006-01-02"

	SortRenewAt = "renew_at"
	OrderAsc    = "asc"
	OrderDesc   = "desc"
)

type Date struct {
	Time time.Time
}

type Record struct {
	SubscriptionID     string    `json:"subscription_id"`
	VPSID              string    `json:"vps_id"`
	Price              float64   `json:"price"`
	Currency           string    `json:"currency"`
	BillingCycle       string    `json:"billing_cycle"`
	BillingMonths      int       `json:"billing_months"`
	MonthlyPrice       float64   `json:"monthly_price"`
	StartedAt          *Date     `json:"started_at"`
	RenewAt            *Date     `json:"renew_at"`
	AutoRenew          bool      `json:"auto_renew"`
	AutoRenewCancelled bool      `json:"auto_renew_cancelled"`
	Status             Status    `json:"status"`
	PaymentMethod      string    `json:"payment_method"`
	Note               string    `json:"note"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateInput struct {
	VPSID              string  `json:"vps_id"`
	Price              float64 `json:"price"`
	Currency           string  `json:"currency"`
	BillingCycle       string  `json:"billing_cycle"`
	BillingMonths      int     `json:"billing_months"`
	StartedAt          *Date   `json:"started_at"`
	RenewAt            *Date   `json:"renew_at"`
	AutoRenew          bool    `json:"auto_renew"`
	AutoRenewCancelled bool    `json:"auto_renew_cancelled"`
	Status             Status  `json:"status"`
	PaymentMethod      string  `json:"payment_method"`
	Note               string  `json:"note"`
}

type PatchInput struct {
	VPSID              OptionalString `json:"vps_id"`
	Price              OptionalFloat  `json:"price"`
	Currency           OptionalString `json:"currency"`
	BillingCycle       OptionalString `json:"billing_cycle"`
	BillingMonths      OptionalInt    `json:"billing_months"`
	StartedAt          OptionalDate   `json:"started_at"`
	RenewAt            OptionalDate   `json:"renew_at"`
	AutoRenew          OptionalBool   `json:"auto_renew"`
	AutoRenewCancelled OptionalBool   `json:"auto_renew_cancelled"`
	Status             OptionalStatus `json:"status"`
	PaymentMethod      OptionalString `json:"payment_method"`
	Note               OptionalString `json:"note"`
}

type ListFilters struct {
	VPSID           string
	Status          Status
	RenewBefore     *Date
	RenewAfter      *Date
	RenewWithinDays *int
	Sort            string
	Order           string
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

func NormalizeCreateInput(input CreateInput) CreateInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.Currency = NormalizeCurrency(input.Currency)
	input.BillingCycle = strings.TrimSpace(input.BillingCycle)
	input.Status = Status(strings.TrimSpace(string(input.Status)))
	if input.Status == "" {
		input.Status = DefaultStatus
	}
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
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
	if !IsValidCurrency(input.Currency) {
		return fmt.Errorf("%w: currency must be a 3-letter uppercase code", ErrInvalidSubscriptionInput)
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
	if input.Status.Set {
		input.Status.Value = Status(strings.TrimSpace(string(input.Status.Value)))
	}
	input.PaymentMethod = normalizeOptionalString(input.PaymentMethod)
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
	if input.Currency.Set && !IsValidCurrency(input.Currency.Value) {
		return fmt.Errorf("%w: currency must be a 3-letter uppercase code", ErrInvalidSubscriptionInput)
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
		input.StartedAt.Set ||
		input.RenewAt.Set ||
		input.AutoRenew.Set ||
		input.AutoRenewCancelled.Set ||
		input.Status.Set ||
		input.PaymentMethod.Set ||
		input.Note.Set
}

func NormalizeListFilters(filters ListFilters) ListFilters {
	filters.VPSID = strings.TrimSpace(filters.VPSID)
	filters.Status = Status(strings.TrimSpace(string(filters.Status)))
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
	if filters.Sort != SortRenewAt {
		return fmt.Errorf("%w: invalid sort", ErrInvalidSubscriptionInput)
	}
	if filters.Order != OrderAsc && filters.Order != OrderDesc {
		return fmt.Errorf("%w: invalid order", ErrInvalidSubscriptionInput)
	}
	return nil
}

func NormalizeCurrency(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
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

func CalculateMonthlyPrice(price float64, billingMonths int) float64 {
	if billingMonths <= 0 {
		return 0
	}
	return math.Round((price/float64(billingMonths))*10000) / 10000
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
