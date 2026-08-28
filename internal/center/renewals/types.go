package renewals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/createidempotency"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

var ErrAssetTimelineNotFound = errors.New("asset timeline not found")
var ErrInvalidAssetHistoryInput = errors.New("invalid asset history input")

var ErrRenewalTimelineNotFound = ErrAssetTimelineNotFound
var ErrInvalidRenewalDecisionInput = ErrInvalidAssetHistoryInput

type DecisionRecord struct {
	DecisionID   string                     `json:"decision_id"`
	VPSID        string                     `json:"vps_id"`
	FromDecision *vpsassets.RenewalDecision `json:"from_decision"`
	ToDecision   vpsassets.RenewalDecision  `json:"to_decision"`
	Reason       string                     `json:"reason"`
	DecidedAt    time.Time                  `json:"decided_at"`
	CreatedAt    time.Time                  `json:"created_at"`
}

type CreateDecisionInput struct {
	VPSID        string
	FromDecision *vpsassets.RenewalDecision
	ToDecision   vpsassets.RenewalDecision
	Reason       string
	DecidedAt    *time.Time
}

type PriceHistoryRecord struct {
	PriceHistoryID          string               `json:"price_history_id"`
	SubscriptionID          string               `json:"subscription_id"`
	VPSID                   string               `json:"vps_id"`
	FromPrice               float64              `json:"from_price"`
	ToPrice                 float64              `json:"to_price"`
	FromCurrency            string               `json:"from_currency"`
	ToCurrency              string               `json:"to_currency"`
	FromBillingCycle        string               `json:"from_billing_cycle"`
	ToBillingCycle          string               `json:"to_billing_cycle"`
	FromBillingMonths       int                  `json:"from_billing_months"`
	ToBillingMonths         int                  `json:"to_billing_months"`
	FromBillingPeriodUnit   string               `json:"from_billing_period_unit"`
	ToBillingPeriodUnit     string               `json:"to_billing_period_unit"`
	FromBillingPeriodLength int                  `json:"from_billing_period_length"`
	ToBillingPeriodLength   int                  `json:"to_billing_period_length"`
	FromMonthlyPrice        float64              `json:"from_monthly_price"`
	ToMonthlyPrice          float64              `json:"to_monthly_price"`
	FromRenewAt             *subscriptions.Date  `json:"from_renew_at"`
	ToRenewAt               *subscriptions.Date  `json:"to_renew_at"`
	FromAutoRenew           bool                 `json:"from_auto_renew"`
	ToAutoRenew             bool                 `json:"to_auto_renew"`
	FromAutoRenewCancelled  bool                 `json:"from_auto_renew_cancelled"`
	ToAutoRenewCancelled    bool                 `json:"to_auto_renew_cancelled"`
	FromRenewalMode         string               `json:"from_renewal_mode"`
	ToRenewalMode           string               `json:"to_renewal_mode"`
	FromStatus              subscriptions.Status `json:"from_status"`
	ToStatus                subscriptions.Status `json:"to_status"`
	ChangedAt               time.Time            `json:"changed_at"`
	CreatedAt               time.Time            `json:"created_at"`
}

type CreatePriceHistoryInput struct {
	From      subscriptions.Record
	To        subscriptions.Record
	ChangedAt *time.Time
}

type IPHistoryRecord struct {
	IPHistoryID string    `json:"ip_history_id"`
	VPSID       string    `json:"vps_id"`
	FromIPv4    string    `json:"from_ipv4"`
	ToIPv4      string    `json:"to_ipv4"`
	FromIPv6    string    `json:"from_ipv6"`
	ToIPv6      string    `json:"to_ipv6"`
	ChangedAt   time.Time `json:"changed_at"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateIPHistoryInput struct {
	VPSID     string
	FromIPv4  string
	ToIPv4    string
	FromIPv6  string
	ToIPv6    string
	ChangedAt *time.Time
}

type SpecSnapshotRecord struct {
	SnapshotID     string    `json:"snapshot_id"`
	VPSID          string    `json:"vps_id"`
	ProductName    string    `json:"product_name"`
	SSHHost        string    `json:"ssh_host"`
	SSHPort        int       `json:"ssh_port"`
	SSHUser        string    `json:"ssh_user"`
	OSName         string    `json:"os_name"`
	Virtualization string    `json:"virtualization"`
	CapturedAt     time.Time `json:"captured_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type ExperienceCategory string

const (
	ExperienceNote         ExperienceCategory = "note"
	ExperienceStability    ExperienceCategory = "stability"
	ExperienceNetwork      ExperienceCategory = "network"
	ExperienceSupport      ExperienceCategory = "support"
	ExperienceBilling      ExperienceCategory = "billing"
	ExperienceMigration    ExperienceCategory = "migration"
	ExperienceCancellation ExperienceCategory = "cancellation"
)

type ExperienceSeverity string

const (
	ExperienceSeverityInfo     ExperienceSeverity = "info"
	ExperienceSeverityWarning  ExperienceSeverity = "warning"
	ExperienceSeverityCritical ExperienceSeverity = "critical"
)

type ExperienceLogRecord struct {
	ExperienceLogID string             `json:"experience_log_id"`
	VPSID           string             `json:"vps_id"`
	Category        ExperienceCategory `json:"category"`
	Severity        ExperienceSeverity `json:"severity"`
	Summary         string             `json:"summary"`
	Details         string             `json:"details"`
	OccurredAt      time.Time          `json:"occurred_at"`
	CreatedAt       time.Time          `json:"created_at"`
}

type CreateExperienceLogInput struct {
	VPSID      string             `json:"-"`
	Category   ExperienceCategory `json:"category"`
	Severity   ExperienceSeverity `json:"severity"`
	Summary    string             `json:"summary"`
	Details    string             `json:"details"`
	OccurredAt *time.Time         `json:"occurred_at"`
}

type CreateSpecSnapshotInput struct {
	VPSID          string
	ProductName    string
	SSHHost        string
	SSHPort        int
	SSHUser        string
	OSName         string
	Virtualization string
	CapturedAt     *time.Time
}

type VPSTimeline struct {
	VPSID            string                `json:"vps_id"`
	RenewalDecisions []DecisionRecord      `json:"renewal_decisions"`
	PriceHistories   []PriceHistoryRecord  `json:"price_histories"`
	IPHistories      []IPHistoryRecord     `json:"ip_histories"`
	SpecSnapshots    []SpecSnapshotRecord  `json:"spec_snapshots"`
	ExperienceLogs   []ExperienceLogRecord `json:"experience_logs"`
}

type Repository interface {
	CreateRenewalDecision(context.Context, CreateDecisionInput) (DecisionRecord, error)
	ListRenewalDecisionsForVPS(context.Context, string) ([]DecisionRecord, error)
	CreatePriceHistory(context.Context, CreatePriceHistoryInput) (PriceHistoryRecord, error)
	ListPriceHistoriesForVPS(context.Context, string) ([]PriceHistoryRecord, error)
	CreateIPHistory(context.Context, CreateIPHistoryInput) (IPHistoryRecord, error)
	ListIPHistoriesForVPS(context.Context, string) ([]IPHistoryRecord, error)
	CreateSpecSnapshot(context.Context, CreateSpecSnapshotInput) (SpecSnapshotRecord, error)
	ListSpecSnapshotsForVPS(context.Context, string) ([]SpecSnapshotRecord, error)
	CreateExperienceLog(context.Context, CreateExperienceLogInput) (ExperienceLogRecord, error)
	ListExperienceLogsForVPS(context.Context, string) ([]ExperienceLogRecord, error)
	GetVPSTimeline(context.Context, string) (VPSTimeline, error)
}

type ExperienceLogRepository interface {
	CreateExperienceLog(context.Context, CreateExperienceLogInput) (ExperienceLogRecord, error)
	ListExperienceLogsForVPS(context.Context, string) ([]ExperienceLogRecord, error)
}

type IdempotentExperienceLogRepository interface {
	CreateExperienceLogIdempotent(context.Context, CreateExperienceLogInput, string) (ExperienceLogRecord, bool, error)
}

var ErrInvalidIdempotencyKey = createidempotency.ErrInvalidIdempotencyKey
var ErrIdempotencyKeyReused = createidempotency.ErrIdempotencyKeyReused

type TimelineRepository interface {
	GetVPSTimeline(context.Context, string) (VPSTimeline, error)
}

func NormalizeCreateDecisionInput(input CreateDecisionInput) CreateDecisionInput {
	input.VPSID = NormalizeVPSID(input.VPSID)
	if input.FromDecision != nil {
		normalized := vpsassets.RenewalDecision(strings.TrimSpace(string(*input.FromDecision)))
		input.FromDecision = &normalized
	}
	input.ToDecision = vpsassets.RenewalDecision(strings.TrimSpace(string(input.ToDecision)))
	input.Reason = strings.TrimSpace(input.Reason)
	if input.DecidedAt != nil {
		decidedAt := input.DecidedAt.UTC()
		input.DecidedAt = &decidedAt
	}
	return input
}

func ValidateCreateDecisionInput(input CreateDecisionInput) error {
	if NormalizeVPSID(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidAssetHistoryInput)
	}
	if input.FromDecision != nil && !vpsassets.IsValidRenewalDecision(*input.FromDecision) {
		return fmt.Errorf("%w: invalid from_decision", ErrInvalidAssetHistoryInput)
	}
	if !vpsassets.IsValidRenewalDecision(input.ToDecision) {
		return fmt.Errorf("%w: invalid to_decision", ErrInvalidAssetHistoryInput)
	}
	if input.DecidedAt != nil && input.DecidedAt.IsZero() {
		return fmt.Errorf("%w: decided_at is required", ErrInvalidAssetHistoryInput)
	}
	return nil
}

func NormalizeCreatePriceHistoryInput(input CreatePriceHistoryInput) CreatePriceHistoryInput {
	input.From = normalizeSubscriptionRecord(input.From)
	input.To = normalizeSubscriptionRecord(input.To)
	if input.ChangedAt != nil {
		changedAt := input.ChangedAt.UTC()
		input.ChangedAt = &changedAt
	}
	return input
}

func ValidateCreatePriceHistoryInput(input CreatePriceHistoryInput) error {
	if strings.TrimSpace(input.From.SubscriptionID) == "" || strings.TrimSpace(input.To.SubscriptionID) == "" {
		return fmt.Errorf("%w: subscription_id is required", ErrInvalidAssetHistoryInput)
	}
	if input.From.SubscriptionID != input.To.SubscriptionID {
		return fmt.Errorf("%w: subscription_id changed", ErrInvalidAssetHistoryInput)
	}
	if NormalizeVPSID(input.To.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidAssetHistoryInput)
	}
	if !subscriptions.IsValidPrice(input.From.Price) || !subscriptions.IsValidPrice(input.To.Price) {
		return fmt.Errorf("%w: invalid price", ErrInvalidAssetHistoryInput)
	}
	if input.From.BillingMonths <= 0 || input.To.BillingMonths <= 0 {
		return fmt.Errorf("%w: invalid billing_months", ErrInvalidAssetHistoryInput)
	}
	if !subscriptions.IsValidBillingPeriodUnit(input.From.BillingPeriodUnit) || !subscriptions.IsValidBillingPeriodUnit(input.To.BillingPeriodUnit) {
		return fmt.Errorf("%w: invalid billing_period_unit", ErrInvalidAssetHistoryInput)
	}
	if input.From.BillingPeriodLength <= 0 || input.To.BillingPeriodLength <= 0 {
		return fmt.Errorf("%w: invalid billing_period_length", ErrInvalidAssetHistoryInput)
	}
	if !subscriptions.IsValidCurrency(input.From.Currency) || !subscriptions.IsValidCurrency(input.To.Currency) {
		return fmt.Errorf("%w: invalid currency", ErrInvalidAssetHistoryInput)
	}
	if !subscriptions.IsValidRenewalMode(input.From.RenewalMode) || !subscriptions.IsValidRenewalMode(input.To.RenewalMode) {
		return fmt.Errorf("%w: invalid renewal_mode", ErrInvalidAssetHistoryInput)
	}
	if !subscriptions.IsValidStatus(input.From.Status) || !subscriptions.IsValidStatus(input.To.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidAssetHistoryInput)
	}
	if input.ChangedAt != nil && input.ChangedAt.IsZero() {
		return fmt.Errorf("%w: changed_at is required", ErrInvalidAssetHistoryInput)
	}
	return nil
}

func NormalizeCreateIPHistoryInput(input CreateIPHistoryInput) CreateIPHistoryInput {
	input.VPSID = NormalizeVPSID(input.VPSID)
	input.FromIPv4 = strings.TrimSpace(input.FromIPv4)
	input.ToIPv4 = strings.TrimSpace(input.ToIPv4)
	input.FromIPv6 = strings.TrimSpace(input.FromIPv6)
	input.ToIPv6 = strings.TrimSpace(input.ToIPv6)
	if input.ChangedAt != nil {
		changedAt := input.ChangedAt.UTC()
		input.ChangedAt = &changedAt
	}
	return input
}

func ValidateCreateIPHistoryInput(input CreateIPHistoryInput) error {
	if NormalizeVPSID(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidAssetHistoryInput)
	}
	if input.FromIPv4 == input.ToIPv4 && input.FromIPv6 == input.ToIPv6 {
		return fmt.Errorf("%w: ip address is unchanged", ErrInvalidAssetHistoryInput)
	}
	if input.ChangedAt != nil && input.ChangedAt.IsZero() {
		return fmt.Errorf("%w: changed_at is required", ErrInvalidAssetHistoryInput)
	}
	return nil
}

func NormalizeCreateSpecSnapshotInput(input CreateSpecSnapshotInput) CreateSpecSnapshotInput {
	input.VPSID = NormalizeVPSID(input.VPSID)
	input.ProductName = strings.TrimSpace(input.ProductName)
	input.SSHHost = strings.TrimSpace(input.SSHHost)
	input.SSHUser = strings.TrimSpace(input.SSHUser)
	input.OSName = strings.TrimSpace(input.OSName)
	input.Virtualization = strings.TrimSpace(input.Virtualization)
	if input.CapturedAt != nil {
		capturedAt := input.CapturedAt.UTC()
		input.CapturedAt = &capturedAt
	}
	return input
}

func ValidateCreateSpecSnapshotInput(input CreateSpecSnapshotInput) error {
	if NormalizeVPSID(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidAssetHistoryInput)
	}
	if !vpsassets.IsValidSSHPort(input.SSHPort) {
		return fmt.Errorf("%w: ssh_port must be between 1 and 65535", ErrInvalidAssetHistoryInput)
	}
	if input.CapturedAt != nil && input.CapturedAt.IsZero() {
		return fmt.Errorf("%w: captured_at is required", ErrInvalidAssetHistoryInput)
	}
	return nil
}

func NormalizeCreateExperienceLogInput(input CreateExperienceLogInput) CreateExperienceLogInput {
	input.VPSID = NormalizeVPSID(input.VPSID)
	input.Category = ExperienceCategory(strings.TrimSpace(string(input.Category)))
	input.Severity = ExperienceSeverity(strings.TrimSpace(string(input.Severity)))
	input.Summary = strings.TrimSpace(input.Summary)
	input.Details = strings.TrimSpace(input.Details)
	if input.OccurredAt != nil {
		occurredAt := input.OccurredAt.UTC()
		input.OccurredAt = &occurredAt
	}
	return input
}

func ValidateCreateExperienceLogInput(input CreateExperienceLogInput) error {
	if NormalizeVPSID(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidAssetHistoryInput)
	}
	if !IsValidExperienceCategory(input.Category) {
		return fmt.Errorf("%w: invalid category", ErrInvalidAssetHistoryInput)
	}
	if !IsValidExperienceSeverity(input.Severity) {
		return fmt.Errorf("%w: invalid severity", ErrInvalidAssetHistoryInput)
	}
	if strings.TrimSpace(input.Summary) == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalidAssetHistoryInput)
	}
	if input.OccurredAt != nil && input.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurred_at is required", ErrInvalidAssetHistoryInput)
	}
	return nil
}

func IsValidExperienceCategory(value ExperienceCategory) bool {
	switch value {
	case ExperienceNote, ExperienceStability, ExperienceNetwork, ExperienceSupport, ExperienceBilling, ExperienceMigration, ExperienceCancellation:
		return true
	default:
		return false
	}
}

func IsValidExperienceSeverity(value ExperienceSeverity) bool {
	switch value {
	case ExperienceSeverityInfo, ExperienceSeverityWarning, ExperienceSeverityCritical:
		return true
	default:
		return false
	}
}

func NormalizeVPSID(vpsID string) string {
	return strings.TrimSpace(vpsID)
}

func normalizeSubscriptionRecord(record subscriptions.Record) subscriptions.Record {
	record.SubscriptionID = strings.TrimSpace(record.SubscriptionID)
	record.VPSID = NormalizeVPSID(record.VPSID)
	record.Currency = subscriptions.NormalizeCurrency(record.Currency)
	record.BillingCycle = strings.TrimSpace(record.BillingCycle)
	record.BillingPeriodUnit = subscriptions.NormalizeBillingPeriodUnit(record.BillingPeriodUnit)
	if record.BillingPeriodUnit == "" {
		record.BillingPeriodUnit = string(subscriptions.DefaultBillingPeriodUnit)
	}
	if record.BillingPeriodLength <= 0 {
		if record.BillingMonths > 0 && record.BillingPeriodUnit == string(subscriptions.BillingPeriodMonth) {
			record.BillingPeriodLength = record.BillingMonths
		} else {
			record.BillingPeriodLength = subscriptions.DefaultBillingPeriodLength
		}
	}
	if record.BillingMonths <= 0 {
		record.BillingMonths = subscriptions.BillingMonthsForPeriod(record.BillingPeriodUnit, record.BillingPeriodLength)
	}
	if record.BillingCycle == "" {
		record.BillingCycle = subscriptions.BillingCycleForPeriod(record.BillingPeriodUnit, record.BillingPeriodLength)
	}
	record.RenewalMode = subscriptions.NormalizeRenewalMode(record.RenewalMode)
	if record.RenewalMode == "" {
		record.RenewalMode = string(subscriptions.RenewalModeFromLegacyFlags(record.AutoRenew, record.AutoRenewCancelled))
	}
	record.AutoRenew, record.AutoRenewCancelled = subscriptions.LegacyRenewalFlags(record.RenewalMode)
	record.Status = subscriptions.Status(strings.TrimSpace(string(record.Status)))
	return record
}
