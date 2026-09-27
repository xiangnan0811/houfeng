package vpsassets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/ipquality"
)

var ErrVPSAssetNotFound = errors.New("vps asset not found")
var ErrInvalidVPSAssetInput = errors.New("invalid vps asset input")
var ErrVPSAssetConflict = errors.New("vps asset conflict")
var ErrVPSAssetReadonly = errors.New("vps asset readonly")

type LifecycleStatus string

const (
	LifecycleActive    LifecycleStatus = "active"
	LifecycleIdle      LifecycleStatus = "idle"
	LifecycleTesting   LifecycleStatus = "testing"
	LifecycleToMigrate LifecycleStatus = "to_migrate"
	LifecycleToCancel  LifecycleStatus = "to_cancel"
	LifecycleCancelled LifecycleStatus = "cancelled"
	LifecycleArchived  LifecycleStatus = "archived"
)

type UsageStatus string

type AssetScope string

const (
	UsageInUse   UsageStatus = "in_use"
	UsageIdle    UsageStatus = "idle"
	UsageStandby UsageStatus = "standby"
	UsageTesting UsageStatus = "testing"
	UsageUnknown UsageStatus = "unknown"

	AssetScopeCurrent    AssetScope = "current"
	AssetScopeArchived   AssetScope = "archived"
	AssetScopeHistorical AssetScope = "historical"
	AssetScopeAll        AssetScope = "all"
)

type RenewalDecision string

type RenewalSubscriptionLinkageStatus string

const (
	RenewalUnreviewed         RenewalDecision = "unreviewed"
	RenewalKeep               RenewalDecision = "keep"
	RenewalObserve            RenewalDecision = "observe"
	RenewalMigrate            RenewalDecision = "migrate"
	RenewalCancel             RenewalDecision = "cancel"
	RenewalAutoRenewCancelled RenewalDecision = "auto_renew_cancelled"
	RenewalReplaced           RenewalDecision = "replaced"

	RenewalSubscriptionLinkageNone                       RenewalSubscriptionLinkageStatus = "none"
	RenewalSubscriptionLinkageUpdated                    RenewalSubscriptionLinkageStatus = "subscription_updated"
	RenewalSubscriptionLinkageAlreadyCancelled           RenewalSubscriptionLinkageStatus = "subscription_already_cancelled"
	RenewalSubscriptionLinkageNoActiveSubscription       RenewalSubscriptionLinkageStatus = "no_active_subscription"
	RenewalSubscriptionLinkageMultipleActiveSubscription RenewalSubscriptionLinkageStatus = "multiple_active_subscriptions"
)

const (
	DefaultSSHPort         = 22
	DefaultLifecycleStatus = LifecycleActive
	DefaultUsageStatus     = UsageUnknown
	DefaultRenewalDecision = RenewalUnreviewed
	DefaultImportance      = "normal"
)

type Record struct {
	VPSID                             string                 `json:"vps_id"`
	DisplayName                       string                 `json:"display_name"`
	ProviderID                        *string                `json:"provider_id"`
	ProviderName                      string                 `json:"provider_name"`
	ProductName                       string                 `json:"product_name"`
	OrderRef                          string                 `json:"order_ref"`
	Country                           string                 `json:"country"`
	Region                            string                 `json:"region"`
	City                              string                 `json:"city"`
	Datacenter                        string                 `json:"datacenter"`
	IPv4                              string                 `json:"ipv4"`
	IPv6                              string                 `json:"ipv6"`
	SSHHost                           string                 `json:"ssh_host"`
	SSHPort                           int                    `json:"ssh_port"`
	SSHUser                           string                 `json:"ssh_user"`
	OSName                            string                 `json:"os_name"`
	Virtualization                    string                 `json:"virtualization"`
	LifecycleStatus                   LifecycleStatus        `json:"lifecycle_status"`
	UsageStatus                       UsageStatus            `json:"-"`
	UsageTags                         []string               `json:"usage_tags"`
	ValidityMode                      string                 `json:"validity_mode"`
	ExpiresAt                         *string                `json:"expires_at"`
	AutoRenewCheck                    string                 `json:"auto_renew_check"`
	AutoRenewCheckedAt                *time.Time             `json:"auto_renew_checked_at"`
	RenewalReason                     string                 `json:"renewal_reason"`
	RenewalReviewAt                   *time.Time             `json:"renewal_review_at"`
	AcquisitionSource                 string                 `json:"acquisition_source"`
	RenewalDecision                   RenewalDecision        `json:"renewal_decision"`
	Importance                        string                 `json:"importance"`
	Labels                            []string               `json:"labels"`
	Note                              string                 `json:"note"`
	ActiveMonitoringInstanceLinkCount int                    `json:"active_monitoring_instance_link_count"`
	RunningMonitoringInstanceCount    int                    `json:"running_monitoring_instance_count"`
	RunningTargetCount                int                    `json:"running_target_count"`
	IPQualitySummary                  *ipquality.Summary     `json:"ip_quality_summary,omitempty"`
	CreatedAt                         time.Time              `json:"created_at"`
	UpdatedAt                         time.Time              `json:"updated_at"`
	ArchivedAt                        *time.Time             `json:"archived_at"`
	ArchivedStateSnapshot             *ArchivedStateSnapshot `json:"archived_state_snapshot"`
}

type ArchivedStateSnapshot struct {
	LifecycleStatus LifecycleStatus `json:"lifecycle_status"`
	UsageStatus     UsageStatus     `json:"-"`
	UsageTags       []string        `json:"usage_tags"`
	RenewalDecision RenewalDecision `json:"renewal_decision"`
	CapturedAt      time.Time       `json:"captured_at"`
	Source          string          `json:"source"`
}

type RenewalSubscriptionLinkage struct {
	Status         RenewalSubscriptionLinkageStatus `json:"status"`
	CandidateCount int                              `json:"candidate_count"`
	SubscriptionID string                           `json:"subscription_id,omitempty"`
	Updated        bool                             `json:"updated"`
	Message        string                           `json:"message"`
}

type CreateInput struct {
	DisplayName        string          `json:"display_name"`
	ProviderID         *string         `json:"provider_id"`
	ProviderName       string          `json:"provider_name"`
	ProductName        string          `json:"product_name"`
	OrderRef           string          `json:"order_ref"`
	Country            string          `json:"country"`
	Region             string          `json:"region"`
	City               string          `json:"city"`
	Datacenter         string          `json:"datacenter"`
	IPv4               string          `json:"ipv4"`
	IPv6               string          `json:"ipv6"`
	SSHHost            string          `json:"ssh_host"`
	SSHPort            int             `json:"ssh_port"`
	SSHUser            string          `json:"ssh_user"`
	OSName             string          `json:"os_name"`
	Virtualization     string          `json:"virtualization"`
	LifecycleStatus    LifecycleStatus `json:"lifecycle_status"`
	UsageStatus        UsageStatus     `json:"-"`
	UsageTags          []string        `json:"usage_tags"`
	ValidityMode       string          `json:"validity_mode"`
	ExpiresAt          *string         `json:"expires_at"`
	AutoRenewCheck     string          `json:"auto_renew_check"`
	AutoRenewCheckedAt *time.Time      `json:"auto_renew_checked_at"`
	RenewalReason      string          `json:"renewal_reason"`
	RenewalReviewAt    *time.Time      `json:"renewal_review_at"`
	AcquisitionSource  string          `json:"acquisition_source"`
	RenewalDecision    RenewalDecision `json:"renewal_decision"`
	Importance         string          `json:"importance"`
	Labels             []string        `json:"labels"`
	Note               string          `json:"note"`
}

type PatchInput struct {
	DisplayName        OptionalString         `json:"display_name"`
	ProviderID         OptionalNullableString `json:"provider_id"`
	ProviderName       OptionalString         `json:"provider_name"`
	ProductName        OptionalString         `json:"product_name"`
	OrderRef           OptionalString         `json:"order_ref"`
	Country            OptionalString         `json:"country"`
	Region             OptionalString         `json:"region"`
	City               OptionalString         `json:"city"`
	Datacenter         OptionalString         `json:"datacenter"`
	IPv4               OptionalString         `json:"ipv4"`
	IPv6               OptionalString         `json:"ipv6"`
	SSHHost            OptionalString         `json:"ssh_host"`
	SSHPort            OptionalInt            `json:"ssh_port"`
	SSHUser            OptionalString         `json:"ssh_user"`
	OSName             OptionalString         `json:"os_name"`
	Virtualization     OptionalString         `json:"virtualization"`
	LifecycleStatus    OptionalLifecycle      `json:"lifecycle_status"`
	UsageStatus        OptionalUsage          `json:"-"`
	UsageTags          OptionalLabels         `json:"usage_tags"`
	ValidityMode       OptionalString         `json:"validity_mode"`
	ExpiresAt          OptionalNullableString `json:"expires_at"`
	AutoRenewCheck     OptionalString         `json:"auto_renew_check"`
	AutoRenewCheckedAt OptionalTime           `json:"auto_renew_checked_at"`
	RenewalReviewAt    OptionalTime           `json:"renewal_review_at"`
	AcquisitionSource  OptionalString         `json:"acquisition_source"`
	RenewalDecision    OptionalRenewal        `json:"renewal_decision"`
	RenewalReason      OptionalString         `json:"renewal_reason"`
	Importance         OptionalString         `json:"importance"`
	Labels             OptionalLabels         `json:"labels"`
	Note               OptionalString         `json:"note"`
	ExpectedUpdatedAt  *time.Time             `json:"-"`
}

type ListFilters struct {
	UsageTag        string
	ProviderID      string
	LifecycleStatus LifecycleStatus
	UsageStatus     UsageStatus
	RenewalDecision RenewalDecision
	AssetScope      AssetScope
}

type OptionalString struct {
	Set   bool
	Value string
}

type OptionalTime struct {
	Set   bool
	Value *time.Time
}

func (v *OptionalTime) UnmarshalJSON(data []byte) error {
	v.Set = true
	return json.Unmarshal(data, &v.Value)
}

type OptionalNullableString struct {
	Set   bool
	Value *string
}

type OptionalInt struct {
	Set   bool
	Value int
}

type OptionalLifecycle struct {
	Set   bool
	Value LifecycleStatus
}

type OptionalUsage struct {
	Set   bool
	Value UsageStatus
}

type OptionalRenewal struct {
	Set   bool
	Value RenewalDecision
}

type OptionalLabels struct {
	Set    bool
	Values []string
}

type Repository interface {
	ListVPSAssets(context.Context, ListFilters) ([]Record, error)
	GetVPSAsset(context.Context, string) (Record, error)
	CreateVPSAsset(context.Context, CreateInput) (Record, error)
	PatchVPSAsset(context.Context, string, PatchInput) (Record, error)
}

func PatchString(value string) OptionalString {
	return OptionalString{Set: true, Value: value}
}

func PatchNullableString(value *string) OptionalNullableString {
	return OptionalNullableString{Set: true, Value: cloneString(value)}
}

func PatchInt(value int) OptionalInt {
	return OptionalInt{Set: true, Value: value}
}

func PatchLifecycle(value LifecycleStatus) OptionalLifecycle {
	return OptionalLifecycle{Set: true, Value: value}
}

func PatchUsage(value UsageStatus) OptionalUsage {
	return OptionalUsage{Set: true, Value: value}
}

func PatchRenewal(value RenewalDecision) OptionalRenewal {
	return OptionalRenewal{Set: true, Value: value}
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

func (v *OptionalNullableString) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		v.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
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

func (v *OptionalLifecycle) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("lifecycle_status cannot be null")
	}

	var value LifecycleStatus
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalUsage) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("usage_status cannot be null")
	}

	var value UsageStatus
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalRenewal) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("renewal_decision cannot be null")
	}

	var value RenewalDecision
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
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.ProviderID = normalizeNullableString(input.ProviderID)
	input.ProviderName = strings.TrimSpace(input.ProviderName)
	input.ProductName = strings.TrimSpace(input.ProductName)
	input.OrderRef = strings.TrimSpace(input.OrderRef)
	input.Country = strings.TrimSpace(input.Country)
	input.Region = strings.TrimSpace(input.Region)
	input.City = strings.TrimSpace(input.City)
	input.Datacenter = strings.TrimSpace(input.Datacenter)
	input.IPv4 = strings.TrimSpace(input.IPv4)
	input.IPv6 = strings.TrimSpace(input.IPv6)
	input.SSHHost = strings.TrimSpace(input.SSHHost)
	input.SSHUser = strings.TrimSpace(input.SSHUser)
	input.OSName = strings.TrimSpace(input.OSName)
	input.Virtualization = strings.TrimSpace(input.Virtualization)
	input.LifecycleStatus = LifecycleStatus(strings.TrimSpace(string(input.LifecycleStatus)))
	if input.LifecycleStatus == "" {
		input.LifecycleStatus = DefaultLifecycleStatus
	}
	input.UsageStatus = UsageStatus(strings.TrimSpace(string(input.UsageStatus)))
	if input.UsageStatus == "" {
		input.UsageStatus = DefaultUsageStatus
	}
	input.RenewalDecision = RenewalDecision(strings.TrimSpace(string(input.RenewalDecision)))
	if input.RenewalDecision == "" {
		input.RenewalDecision = DefaultRenewalDecision
	}
	input.Importance = strings.TrimSpace(input.Importance)
	if input.Importance == "" {
		input.Importance = DefaultImportance
	}
	if input.SSHPort == 0 {
		input.SSHPort = DefaultSSHPort
	}
	input.Labels = NormalizeLabels(input.Labels)
	input.UsageTags = NormalizeLabels(input.UsageTags)
	input.ValidityMode = strings.TrimSpace(input.ValidityMode)
	if input.ValidityMode == "" {
		input.ValidityMode = "unknown"
	}
	input.AutoRenewCheck = strings.TrimSpace(input.AutoRenewCheck)
	if input.AutoRenewCheck == "" {
		input.AutoRenewCheck = "unchecked"
	}
	input.ExpiresAt = normalizeNullableString(input.ExpiresAt)
	input.RenewalReason = strings.TrimSpace(input.RenewalReason)
	input.AcquisitionSource = strings.TrimSpace(input.AcquisitionSource)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.DisplayName) == "" {
		return fmt.Errorf("%w: display_name is required", ErrInvalidVPSAssetInput)
	}
	if !IsValidLifecycleStatus(input.LifecycleStatus) {
		return fmt.Errorf("%w: invalid lifecycle_status", ErrInvalidVPSAssetInput)
	}
	if !IsValidUsageStatus(input.UsageStatus) {
		return fmt.Errorf("%w: invalid usage_status", ErrInvalidVPSAssetInput)
	}
	if !IsValidRenewalDecision(input.RenewalDecision) {
		return fmt.Errorf("%w: invalid renewal_decision", ErrInvalidVPSAssetInput)
	}
	if !IsValidCreateLifecycleStatus(input.LifecycleStatus) {
		return fmt.Errorf("%w: lifecycle_status must be active on create", ErrInvalidVPSAssetInput)
	}
	if err := ValidateVPSStateCombination(input.LifecycleStatus, input.UsageStatus, input.RenewalDecision); err != nil {
		return err
	}
	if !IsValidSSHPort(input.SSHPort) {
		return fmt.Errorf("%w: ssh_port must be between 1 and 65535", ErrInvalidVPSAssetInput)
	}
	return ValidateIndependentFacts(input.ValidityMode, input.ExpiresAt, input.AutoRenewCheck, input.AutoRenewCheckedAt)
}

func NormalizePatchInput(input PatchInput) PatchInput {
	input.DisplayName = normalizeOptionalString(input.DisplayName)
	input.ProviderID = normalizeOptionalNullableString(input.ProviderID)
	input.ProviderName = normalizeOptionalString(input.ProviderName)
	input.ProductName = normalizeOptionalString(input.ProductName)
	input.OrderRef = normalizeOptionalString(input.OrderRef)
	input.Country = normalizeOptionalString(input.Country)
	input.Region = normalizeOptionalString(input.Region)
	input.City = normalizeOptionalString(input.City)
	input.Datacenter = normalizeOptionalString(input.Datacenter)
	input.IPv4 = normalizeOptionalString(input.IPv4)
	input.IPv6 = normalizeOptionalString(input.IPv6)
	input.SSHHost = normalizeOptionalString(input.SSHHost)
	input.SSHUser = normalizeOptionalString(input.SSHUser)
	input.OSName = normalizeOptionalString(input.OSName)
	input.Virtualization = normalizeOptionalString(input.Virtualization)
	if input.LifecycleStatus.Set {
		input.LifecycleStatus.Value = LifecycleStatus(strings.TrimSpace(string(input.LifecycleStatus.Value)))
	}
	if input.UsageStatus.Set {
		input.UsageStatus.Value = UsageStatus(strings.TrimSpace(string(input.UsageStatus.Value)))
	}
	if input.RenewalDecision.Set {
		input.RenewalDecision.Value = RenewalDecision(strings.TrimSpace(string(input.RenewalDecision.Value)))
	}
	input.RenewalReason = normalizeOptionalString(input.RenewalReason)
	input.ValidityMode = normalizeOptionalString(input.ValidityMode)
	input.ExpiresAt = normalizeOptionalNullableString(input.ExpiresAt)
	input.AutoRenewCheck = normalizeOptionalString(input.AutoRenewCheck)
	input.AcquisitionSource = normalizeOptionalString(input.AcquisitionSource)
	if input.UsageTags.Set {
		input.UsageTags.Values = NormalizeLabels(input.UsageTags.Values)
	}
	input.Importance = normalizeOptionalString(input.Importance)
	if input.Labels.Set {
		input.Labels.Values = NormalizeLabels(input.Labels.Values)
	}
	input.Note = normalizeOptionalString(input.Note)
	return input
}

func ValidatePatchInput(input PatchInput) error {
	if input.DisplayName.Set && input.DisplayName.Value == "" {
		return fmt.Errorf("%w: display_name is required", ErrInvalidVPSAssetInput)
	}
	if input.LifecycleStatus.Set && !IsValidLifecycleStatus(input.LifecycleStatus.Value) {
		return fmt.Errorf("%w: invalid lifecycle_status", ErrInvalidVPSAssetInput)
	}
	if input.UsageStatus.Set && !IsValidUsageStatus(input.UsageStatus.Value) {
		return fmt.Errorf("%w: invalid usage_status", ErrInvalidVPSAssetInput)
	}
	if input.RenewalDecision.Set && !IsValidRenewalDecision(input.RenewalDecision.Value) {
		return fmt.Errorf("%w: invalid renewal_decision", ErrInvalidVPSAssetInput)
	}
	if err := ValidateVPSPatchStateCombination(input); err != nil {
		return err
	}
	if input.ValidityMode.Set && !IsValidValidityMode(input.ValidityMode.Value) {
		return fmt.Errorf("%w: invalid validity_mode", ErrInvalidVPSAssetInput)
	}
	if input.ExpiresAt.Set && input.ExpiresAt.Value != nil {
		if _, err := time.Parse("2006-01-02", *input.ExpiresAt.Value); err != nil {
			return fmt.Errorf("%w: invalid expires_at", ErrInvalidVPSAssetInput)
		}
	}
	if input.AutoRenewCheck.Set && !IsValidAutoRenewCheck(input.AutoRenewCheck.Value) {
		return fmt.Errorf("%w: invalid auto_renew_check", ErrInvalidVPSAssetInput)
	}
	if input.SSHPort.Set && !IsValidSSHPort(input.SSHPort.Value) {
		return fmt.Errorf("%w: ssh_port must be between 1 and 65535", ErrInvalidVPSAssetInput)
	}
	return nil
}

func ValidateOrdinaryPatchInput(input PatchInput) error {
	if err := ValidatePatchInput(input); err != nil {
		return err
	}
	if input.LifecycleStatus.Set && !IsValidOrdinaryPatchLifecycleStatus(input.LifecycleStatus.Value) {
		return fmt.Errorf("%w: invalid lifecycle_status", ErrInvalidVPSAssetInput)
	}
	return nil
}

func (input PatchInput) HasChanges() bool {
	return input.UsageTags.Set || input.ValidityMode.Set || input.ExpiresAt.Set || input.AutoRenewCheck.Set || input.AutoRenewCheckedAt.Set || input.RenewalReason.Set || input.RenewalReviewAt.Set || input.AcquisitionSource.Set || input.DisplayName.Set ||
		input.ProviderID.Set ||
		input.ProviderName.Set ||
		input.ProductName.Set ||
		input.OrderRef.Set ||
		input.Country.Set ||
		input.Region.Set ||
		input.City.Set ||
		input.Datacenter.Set ||
		input.IPv4.Set ||
		input.IPv6.Set ||
		input.SSHHost.Set ||
		input.SSHPort.Set ||
		input.SSHUser.Set ||
		input.OSName.Set ||
		input.Virtualization.Set ||
		input.LifecycleStatus.Set ||
		input.UsageStatus.Set ||
		input.RenewalDecision.Set ||
		input.Importance.Set ||
		input.Labels.Set ||
		input.Note.Set
}

// IsArchivedSupplement permits financial review and historical annotations without reopening resources.
func (input PatchInput) IsArchivedSupplement() bool {
	if !input.HasChanges() {
		return false
	}
	input.AutoRenewCheck = OptionalString{}
	input.AutoRenewCheckedAt = OptionalTime{}
	input.RenewalReason = OptionalString{}
	input.RenewalReviewAt = OptionalTime{}
	input.Note = OptionalString{}
	return !input.HasChanges()
}

func NormalizeListFilters(filters ListFilters) ListFilters {
	filters.UsageTag = strings.TrimSpace(filters.UsageTag)
	filters.ProviderID = strings.TrimSpace(filters.ProviderID)
	filters.LifecycleStatus = LifecycleStatus(strings.TrimSpace(string(filters.LifecycleStatus)))
	filters.UsageStatus = UsageStatus(strings.TrimSpace(string(filters.UsageStatus)))
	filters.RenewalDecision = RenewalDecision(strings.TrimSpace(string(filters.RenewalDecision)))
	filters.AssetScope = AssetScope(strings.TrimSpace(string(filters.AssetScope)))
	return filters
}

func ValidateListFilters(filters ListFilters) error {
	if filters.LifecycleStatus != "" && !IsValidLifecycleStatus(filters.LifecycleStatus) {
		return fmt.Errorf("%w: invalid lifecycle_status", ErrInvalidVPSAssetInput)
	}
	if filters.UsageStatus != "" && !IsValidUsageStatus(filters.UsageStatus) {
		return fmt.Errorf("%w: invalid usage_status", ErrInvalidVPSAssetInput)
	}
	if filters.RenewalDecision != "" && !IsValidRenewalDecision(filters.RenewalDecision) {
		return fmt.Errorf("%w: invalid renewal_decision", ErrInvalidVPSAssetInput)
	}
	if filters.AssetScope != "" && !IsValidAssetScope(filters.AssetScope) {
		return fmt.Errorf("%w: invalid asset_scope", ErrInvalidVPSAssetInput)
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

func IsValidLifecycleStatus(status LifecycleStatus) bool {
	switch status {
	case LifecycleActive, LifecycleArchived:
		return true
	default:
		return false
	}
}

func IsValidOrdinaryPatchLifecycleStatus(status LifecycleStatus) bool {
	switch status {
	case LifecycleActive:
		return true
	default:
		return false
	}
}

func IsValidCreateLifecycleStatus(status LifecycleStatus) bool {
	return IsValidOrdinaryPatchLifecycleStatus(status)
}

func IsValidUsageStatus(status UsageStatus) bool {
	switch status {
	case UsageInUse, UsageIdle, UsageStandby, UsageTesting, UsageUnknown:
		return true
	default:
		return false
	}
}

func IsValidAssetScope(scope AssetScope) bool {
	switch scope {
	case AssetScopeCurrent, AssetScopeArchived, AssetScopeHistorical, AssetScopeAll:
		return true
	default:
		return false
	}
}

type StateCombinationError struct{ FieldErrors map[string]string }

func (e *StateCombinationError) Error() string { return "invalid vps state combination" }
func (e *StateCombinationError) Unwrap() error { return ErrInvalidVPSAssetInput }
func invalidStateCombination(message string) error {
	return &StateCombinationError{FieldErrors: map[string]string{"lifecycle_status": message, "usage_status": message, "renewal_decision": message}}
}

func ValidateVPSStateCombination(lifecycle LifecycleStatus, _ UsageStatus, renewal RenewalDecision) error {
	if !IsValidLifecycleStatus(lifecycle) || !IsValidRenewalDecision(renewal) {
		return invalidStateCombination("invalid lifecycle or renewal decision")
	}
	return nil
}

func ValidateVPSPatchStateCombination(input PatchInput) error { return nil }

func IsValidRenewalDecision(decision RenewalDecision) bool {
	switch decision {
	case RenewalUnreviewed, RenewalKeep, RenewalCancel:
		return true
	default:
		return false
	}
}

func IsCancellationRenewalDecision(decision RenewalDecision) bool {
	switch decision {
	case RenewalCancel, RenewalAutoRenewCancelled:
		return true
	default:
		return false
	}
}

func IsValidSSHPort(port int) bool {
	return port >= 1 && port <= 65535
}

func IsValidValidityMode(value string) bool {
	return value == "fixed" || value == "unlimited" || value == "unknown"
}

func IsValidAutoRenewCheck(value string) bool {
	switch value {
	case "unchecked", "enabled", "disabled", "never_enabled", "unsupported":
		return true
	}
	return false
}

// ValidateIndependentFacts never infers resource validity or provider settings from billing intent.
func ValidateIndependentFacts(mode string, expiresAt *string, check string, checkedAt *time.Time) error {
	if !IsValidValidityMode(mode) {
		return fmt.Errorf("%w: invalid validity_mode", ErrInvalidVPSAssetInput)
	}
	if (mode == "fixed") != (expiresAt != nil) {
		return fmt.Errorf("%w: fixed validity requires expires_at; other modes must not have expires_at", ErrInvalidVPSAssetInput)
	}
	if expiresAt != nil {
		if _, err := time.Parse("2006-01-02", *expiresAt); err != nil {
			return fmt.Errorf("%w: invalid expires_at", ErrInvalidVPSAssetInput)
		}
	}
	if !IsValidAutoRenewCheck(check) {
		return fmt.Errorf("%w: invalid auto_renew_check", ErrInvalidVPSAssetInput)
	}
	if (check == "unchecked") != (checkedAt == nil) {
		return fmt.Errorf("%w: a provider auto-renew check requires its observation time", ErrInvalidVPSAssetInput)
	}
	return nil
}

func DeriveArchivedAt(lifecycle LifecycleStatus, current *time.Time, now time.Time) *time.Time {
	if lifecycle != LifecycleArchived {
		return nil
	}
	if current != nil {
		return cloneTime(current)
	}
	return &now
}

func normalizeOptionalString(value OptionalString) OptionalString {
	if value.Set {
		value.Value = strings.TrimSpace(value.Value)
	}
	return value
}

func normalizeOptionalNullableString(value OptionalNullableString) OptionalNullableString {
	if value.Set {
		value.Value = normalizeNullableString(value.Value)
	}
	return value
}

func normalizeNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
