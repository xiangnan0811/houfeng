package assetdecisions

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

var ErrAssetDecisionGroupNotFound = errors.New("asset decision group not found")
var ErrAssetDecisionRecordNotFound = errors.New("asset decision record not found")
var ErrInvalidAssetDecisionInput = errors.New("invalid asset decision input")

type GroupType string

const (
	GroupRenewalAttention      GroupType = "renewal_attention"
	GroupCancellationAttention GroupType = "cancellation_attention"
	GroupRegionPortfolio       GroupType = "region_portfolio"
	GroupProviderPortfolio     GroupType = "provider_portfolio"
	GroupCostPressure          GroupType = "cost_pressure"
	GroupEvidenceGap           GroupType = "evidence_gap"
)

type View string

const (
	ViewNeedsDecision View = "needs_decision"
	ViewRenewal       View = "renewal"
	ViewRegion        View = "region"
	ViewProvider      View = "provider"
	ViewCost          View = "cost"
	ViewEvidence      View = "evidence"
)

type SuggestedRole string

const (
	RolePrimaryCandidate SuggestedRole = "primary_candidate"
	RoleStandbyCandidate SuggestedRole = "standby_candidate"
	RoleObserveCandidate SuggestedRole = "observe_candidate"
	RoleRetireCandidate  SuggestedRole = "retire_candidate"
	RoleEvidenceNeeded   SuggestedRole = "evidence_needed"
)

type SuggestedAction string

const (
	ActionReview                    SuggestedAction = "review"
	ActionKeep                      SuggestedAction = "keep"
	ActionObserve                   SuggestedAction = "observe"
	ActionMigrate                   SuggestedAction = "migrate"
	ActionCancel                    SuggestedAction = "cancel"
	ActionOpenCancellationWorkbench SuggestedAction = "open_cancellation_workbench"
	ActionCompleteEvidence          SuggestedAction = "complete_evidence"
)

type RecordStatus string

const (
	RecordStatusDraft      RecordStatus = "draft"
	RecordStatusDecided    RecordStatus = "decided"
	RecordStatusInProgress RecordStatus = "in_progress"
	RecordStatusCompleted  RecordStatus = "completed"
	RecordStatusAbandoned  RecordStatus = "abandoned"
)

type FollowupStatus string

const (
	FollowupTodo       FollowupStatus = "todo"
	FollowupInProgress FollowupStatus = "in_progress"
	FollowupBlocked    FollowupStatus = "blocked"
	FollowupDone       FollowupStatus = "done"
	FollowupSkipped    FollowupStatus = "skipped"
)

type EvidenceKind string

const (
	EvidenceRenewalDue              EvidenceKind = "renewal_due"
	EvidenceIdlePaid                EvidenceKind = "idle_paid"
	EvidenceMissingSubscription     EvidenceKind = "missing_subscription"
	EvidenceMissingMonitoring       EvidenceKind = "missing_monitoring"
	EvidenceCarriesService          EvidenceKind = "carries_service"
	EvidenceCancellationLinkage     EvidenceKind = "cancellation_linkage"
	EvidenceBudgetRisk              EvidenceKind = "budget_risk"
	EvidenceAbnormalMonitoring      EvidenceKind = "abnormal_monitoring"
	EvidenceMissingProvider         EvidenceKind = "missing_provider"
	EvidenceMissingLocation         EvidenceKind = "missing_location"
	EvidenceMissingAccess           EvidenceKind = "missing_access"
	EvidenceExchangeRateStale       EvidenceKind = "exchange_rate_stale"
	EvidenceNoServiceContext        EvidenceKind = "no_service_context"
	EvidenceSubscriptionUnavailable EvidenceKind = "subscription_unavailable"
)

type ListFilters struct {
	View            View
	RenewWithinDays int
}

type Overview struct {
	SnapshotGeneratedAt time.Time          `json:"snapshot_generated_at"`
	RenewWithinDays     int                `json:"renew_within_days"`
	GroupCount          int                `json:"group_count"`
	MemberVPSCount      int                `json:"member_vps_count"`
	NeedsDecisionCount  int                `json:"needs_decision_count"`
	RenewalGroupCount   int                `json:"renewal_group_count"`
	RegionGroupCount    int                `json:"region_group_count"`
	ProviderGroupCount  int                `json:"provider_group_count"`
	CostGroupCount      int                `json:"cost_group_count"`
	EvidenceGroupCount  int                `json:"evidence_group_count"`
	TopGroups           []GroupSummary     `json:"top_groups"`
	TypeCounts          map[GroupType]int  `json:"type_counts"`
	ViewCounts          map[View]int       `json:"view_counts"`
	SourceAvailability  SourceAvailability `json:"source_availability"`
}

type SourceAvailability struct {
	Subscriptions bool `json:"subscriptions"`
	Services      bool `json:"services"`
	Domains       bool `json:"domains"`
	Monitoring    bool `json:"monitoring"`
	Targets       bool `json:"targets"`
}

type CostByCurrency struct {
	Currency     string  `json:"currency"`
	MonthlyTotal float64 `json:"monthly_total"`
	YearlyTotal  float64 `json:"yearly_total"`
}

type EvidenceChip struct {
	Kind    EvidenceKind `json:"kind"`
	Label   string       `json:"label"`
	Tone    string       `json:"tone"`
	Details string       `json:"details,omitempty"`
}

type EvidenceSnapshot map[string]any

type GroupSummary struct {
	GroupID                    string                            `json:"group_id"`
	GroupType                  GroupType                         `json:"group_type"`
	View                       View                              `json:"view"`
	Title                      string                            `json:"title"`
	ScopeKey                   string                            `json:"scope_key"`
	ScopeLabel                 string                            `json:"scope_label"`
	Priority                   int                               `json:"priority"`
	MemberCount                int                               `json:"member_count"`
	LifecycleCounts            map[vpsassets.LifecycleStatus]int `json:"lifecycle_counts"`
	UsageCounts                map[vpsassets.UsageStatus]int     `json:"usage_counts"`
	RenewalDecisionCounts      map[vpsassets.RenewalDecision]int `json:"renewal_decision_counts"`
	RenewalWindowCount         int                               `json:"renewal_window_count"`
	UnreviewedCount            int                               `json:"unreviewed_count"`
	MigrateCount               int                               `json:"migrate_count"`
	CancelCount                int                               `json:"cancel_count"`
	CancellationAttentionCount int                               `json:"cancellation_attention_count"`
	IdleCount                  int                               `json:"idle_count"`
	StandbyCount               int                               `json:"standby_count"`
	InUseCount                 int                               `json:"in_use_count"`
	ServiceCount               int                               `json:"service_count"`
	DomainCount                int                               `json:"domain_count"`
	TargetCount                int                               `json:"target_count"`
	RunningTargetCount         int                               `json:"running_target_count"`
	MonitoringLinkCount        int                               `json:"monitoring_link_count"`
	AbnormalMonitoringCount    int                               `json:"abnormal_monitoring_count"`
	ActiveIncidentCount        int                               `json:"active_incident_count"`
	PrimaryIssueSummary        string                            `json:"primary_issue_summary"`
	MonthlyCostByCurrency      []CostByCurrency                  `json:"monthly_cost_by_currency"`
	MonthlyCostBase            *float64                          `json:"monthly_cost_base,omitempty"`
	YearlyCostBase             *float64                          `json:"yearly_cost_base,omitempty"`
	BaseCurrency               string                            `json:"base_currency,omitempty"`
	EvidenceChips              []EvidenceChip                    `json:"evidence_chips"`
	EvidenceAssessment         EvidenceAssessment                `json:"evidence_assessment"`
}

type GroupDetail struct {
	GroupSummary
	Members []GroupMember `json:"members"`
}

type GroupMember struct {
	VPS                         vpsassets.Record      `json:"vps"`
	PrimarySubscription         *subscriptions.Record `json:"primary_subscription,omitempty"`
	SubscriptionCount           int                   `json:"subscription_count"`
	ActiveSubscriptionCount     int                   `json:"active_subscription_count"`
	InactiveSubscriptionCount   int                   `json:"inactive_subscription_count"`
	ServiceCount                int                   `json:"service_count"`
	DomainCount                 int                   `json:"domain_count"`
	TargetCount                 int                   `json:"target_count"`
	RunningTargetCount          int                   `json:"running_target_count"`
	MonitoringLinkCount         int                   `json:"monitoring_link_count"`
	RunningMonitoringCount      int                   `json:"running_monitoring_count"`
	AbnormalMonitoringCount     int                   `json:"abnormal_monitoring_count"`
	ActiveIncidentCount         int                   `json:"active_incident_count"`
	PrimaryIssueSummary         string                `json:"primary_issue_summary"`
	CancellationAttentionReason string                `json:"cancellation_attention_reason,omitempty"`
	SuggestedRole               SuggestedRole         `json:"suggested_role"`
	SuggestedAction             SuggestedAction       `json:"suggested_action"`
	EvidenceChips               []EvidenceChip        `json:"evidence_chips"`
	EvidenceAssessment          EvidenceAssessment    `json:"evidence_assessment"`
	RenewalWithinWindow         bool                  `json:"renewal_within_window"`
	SourceAvailability          SourceAvailability    `json:"source_availability"`
}

type RecordSummary struct {
	RecordID                string                  `json:"record_id"`
	Title                   string                  `json:"title"`
	Goal                    string                  `json:"goal"`
	Status                  RecordStatus            `json:"status"`
	SourceType              string                  `json:"source_type"`
	SourceGroupID           string                  `json:"source_group_id"`
	SourceGroupType         GroupType               `json:"source_group_type"`
	SourceView              View                    `json:"source_view"`
	ScopeKey                string                  `json:"scope_key"`
	ScopeLabel              string                  `json:"scope_label"`
	RenewWithinDays         int                     `json:"renew_within_days"`
	MemberCount             int                     `json:"member_count"`
	FollowupTodoCount       int                     `json:"followup_todo_count"`
	FollowupInProgressCount int                     `json:"followup_in_progress_count"`
	FollowupBlockedCount    int                     `json:"followup_blocked_count"`
	FollowupDoneCount       int                     `json:"followup_done_count"`
	FollowupSkippedCount    int                     `json:"followup_skipped_count"`
	EvidenceSnapshot        EvidenceSnapshot        `json:"evidence_snapshot"`
	CreatedAt               time.Time               `json:"created_at"`
	UpdatedAt               time.Time               `json:"updated_at"`
	DecidedAt               *time.Time              `json:"decided_at,omitempty"`
	CompletedAt             *time.Time              `json:"completed_at,omitempty"`
	ExecutionReadback       RecordExecutionReadback `json:"execution_readback"`
}

type RecordDetail struct {
	RecordSummary
	Members []RecordMember `json:"members"`
}

type RecordMember struct {
	RecordID          string                  `json:"record_id"`
	VPSID             string                  `json:"vps_id"`
	DisplayName       string                  `json:"display_name"`
	SuggestedRole     SuggestedRole           `json:"suggested_role"`
	DecidedRole       SuggestedRole           `json:"decided_role"`
	SuggestedAction   SuggestedAction         `json:"suggested_action"`
	DecidedAction     SuggestedAction         `json:"decided_action"`
	Reason            string                  `json:"reason"`
	FollowupStatus    FollowupStatus          `json:"followup_status"`
	FollowupNote      string                  `json:"followup_note"`
	FollowupUpdatedAt *time.Time              `json:"followup_updated_at,omitempty"`
	EvidenceSnapshot  EvidenceSnapshot        `json:"evidence_snapshot"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	ExecutionReadback MemberExecutionReadback `json:"execution_readback"`
}

type CreateRecordInput struct {
	SourceGroupID   string                    `json:"source_group_id"`
	RenewWithinDays int                       `json:"renew_within_days"`
	Title           string                    `json:"title"`
	Goal            string                    `json:"goal"`
	Status          RecordStatus              `json:"status"`
	Members         []CreateRecordMemberInput `json:"members"`
}

type CreateRecordMemberInput struct {
	VPSID         string          `json:"vps_id"`
	DecidedRole   SuggestedRole   `json:"decided_role"`
	DecidedAction SuggestedAction `json:"decided_action"`
	Reason        string          `json:"reason"`
}

type PatchRecordInput struct {
	Title   PatchString              `json:"title"`
	Goal    PatchString              `json:"goal"`
	Status  PatchRecordStatus        `json:"status"`
	Members []PatchRecordMemberInput `json:"members"`
}

type PatchRecordMemberInput struct {
	VPSID          string              `json:"vps_id"`
	FollowupStatus PatchFollowupStatus `json:"followup_status"`
	FollowupNote   PatchString         `json:"followup_note"`
}

type PatchString struct {
	Set   bool
	Value string
}

type PatchRecordStatus struct {
	Set   bool
	Value RecordStatus
}

type PatchFollowupStatus struct {
	Set   bool
	Value FollowupStatus
}

type Repository interface {
	GetOverview(context.Context, ListFilters) (Overview, error)
	ListGroups(context.Context, ListFilters) ([]GroupSummary, error)
	GetGroup(context.Context, string, ListFilters) (GroupDetail, error)
	ListRecords(context.Context) ([]RecordSummary, error)
	CreateRecord(context.Context, CreateRecordInput) (RecordDetail, error)
	GetRecord(context.Context, string) (RecordDetail, error)
	PatchRecord(context.Context, string, PatchRecordInput) (RecordDetail, error)
}

type Fact struct {
	VPS                       vpsassets.Record
	PrimarySubscription       *subscriptions.Record
	SubscriptionCount         int
	ActiveSubscriptionCount   int
	InactiveSubscriptionCount int
	ServiceCount              int
	DomainCount               int
	TargetCount               int
	RunningTargetCount        int
	MonitoringLinkCount       int
	RunningMonitoringCount    int
	AbnormalMonitoringCount   int
	ActiveIncidentCount       int
	PrimaryIssueSummary       string
	SourceAvailability        SourceAvailability
}

func NormalizeFilters(filters ListFilters) ListFilters {
	if filters.RenewWithinDays == 0 {
		filters.RenewWithinDays = 30
	}
	return filters
}

func ValidateFilters(filters ListFilters) error {
	switch filters.RenewWithinDays {
	case 30, 60, 90:
	default:
		return ErrInvalidAssetDecisionInput
	}
	switch filters.View {
	case "", ViewNeedsDecision, ViewRenewal, ViewRegion, ViewProvider, ViewCost, ViewEvidence:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func StableGroupID(groupType GroupType, parts ...string) string {
	seed := string(groupType) + "\x00" + strings.Join(parts, "\x00")
	sum := sha1.Sum([]byte(seed))
	return "adg_auto_" + hex.EncodeToString(sum[:])[:12]
}

func NormalizeCreateRecordInput(input CreateRecordInput) CreateRecordInput {
	input.SourceGroupID = strings.TrimSpace(input.SourceGroupID)
	input.Title = strings.TrimSpace(input.Title)
	input.Goal = strings.TrimSpace(input.Goal)
	if input.RenewWithinDays == 0 {
		input.RenewWithinDays = 30
	}
	if input.Status == "" {
		input.Status = RecordStatusDraft
	}
	normalizedMembers := make([]CreateRecordMemberInput, 0, len(input.Members))
	for _, member := range input.Members {
		member.VPSID = strings.TrimSpace(member.VPSID)
		member.Reason = strings.TrimSpace(member.Reason)
		normalizedMembers = append(normalizedMembers, member)
	}
	input.Members = normalizedMembers
	return input
}

func NormalizePatchRecordInput(input PatchRecordInput) PatchRecordInput {
	input.Title.Value = strings.TrimSpace(input.Title.Value)
	input.Goal.Value = strings.TrimSpace(input.Goal.Value)
	normalizedMembers := make([]PatchRecordMemberInput, 0, len(input.Members))
	for _, member := range input.Members {
		member.VPSID = strings.TrimSpace(member.VPSID)
		member.FollowupNote.Value = strings.TrimSpace(member.FollowupNote.Value)
		normalizedMembers = append(normalizedMembers, member)
	}
	input.Members = normalizedMembers
	return input
}

func ValidateCreateRecordInput(input CreateRecordInput) error {
	if input.SourceGroupID == "" {
		return ErrInvalidAssetDecisionInput
	}
	if _, err := validateRenewWindow(input.RenewWithinDays); err != nil {
		return err
	}
	if err := ValidateRecordStatus(input.Status); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, member := range input.Members {
		if member.VPSID == "" {
			return ErrInvalidAssetDecisionInput
		}
		if _, ok := seen[member.VPSID]; ok {
			return ErrInvalidAssetDecisionInput
		}
		seen[member.VPSID] = struct{}{}
		if member.DecidedRole != "" {
			if err := ValidateSuggestedRole(member.DecidedRole); err != nil {
				return err
			}
		}
		if member.DecidedAction != "" {
			if err := ValidateSuggestedAction(member.DecidedAction); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidatePatchRecordInput(input PatchRecordInput) error {
	if input.Title.Set && strings.TrimSpace(input.Title.Value) == "" {
		return ErrInvalidAssetDecisionInput
	}
	if input.Status.Set {
		if err := ValidateRecordStatus(input.Status.Value); err != nil {
			return err
		}
	}
	seen := map[string]struct{}{}
	for _, member := range input.Members {
		if member.VPSID == "" {
			return ErrInvalidAssetDecisionInput
		}
		if !member.FollowupStatus.Set && !member.FollowupNote.Set {
			return ErrInvalidAssetDecisionInput
		}
		if _, ok := seen[member.VPSID]; ok {
			return ErrInvalidAssetDecisionInput
		}
		seen[member.VPSID] = struct{}{}
		if member.FollowupStatus.Set {
			if err := ValidateFollowupStatus(member.FollowupStatus.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidateRecordStatus(status RecordStatus) error {
	switch status {
	case RecordStatusDraft, RecordStatusDecided, RecordStatusInProgress, RecordStatusCompleted, RecordStatusAbandoned:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ValidateFollowupStatus(status FollowupStatus) error {
	switch status {
	case FollowupTodo, FollowupInProgress, FollowupBlocked, FollowupDone, FollowupSkipped:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ValidateSuggestedRole(role SuggestedRole) error {
	switch role {
	case RolePrimaryCandidate, RoleStandbyCandidate, RoleObserveCandidate, RoleRetireCandidate, RoleEvidenceNeeded:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ValidateSuggestedAction(action SuggestedAction) error {
	switch action {
	case ActionReview, ActionKeep, ActionObserve, ActionMigrate, ActionCancel, ActionOpenCancellationWorkbench, ActionCompleteEvidence:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func RecordSnapshotFromGroup(group GroupDetail) EvidenceSnapshot {
	snapshot := EvidenceSnapshot{
		"group_id":                     group.GroupID,
		"group_type":                   string(group.GroupType),
		"view":                         string(group.View),
		"title":                        group.Title,
		"scope_key":                    group.ScopeKey,
		"scope_label":                  group.ScopeLabel,
		"member_count":                 group.MemberCount,
		"renewal_window_count":         group.RenewalWindowCount,
		"unreviewed_count":             group.UnreviewedCount,
		"migrate_count":                group.MigrateCount,
		"cancel_count":                 group.CancelCount,
		"cancellation_attention_count": group.CancellationAttentionCount,
		"idle_count":                   group.IdleCount,
		"standby_count":                group.StandbyCount,
		"in_use_count":                 group.InUseCount,
		"service_count":                group.ServiceCount,
		"domain_count":                 group.DomainCount,
		"target_count":                 group.TargetCount,
		"running_target_count":         group.RunningTargetCount,
		"monitoring_link_count":        group.MonitoringLinkCount,
		"abnormal_monitoring_count":    group.AbnormalMonitoringCount,
		"active_incident_count":        group.ActiveIncidentCount,
		"primary_issue_summary":        group.PrimaryIssueSummary,
		"evidence_chips":               group.EvidenceChips,
		"evidence_assessment":          group.EvidenceAssessment,
		"source_availability":          mergeMemberSourceAvailability(group.Members),
	}
	if group.MonthlyCostBase != nil {
		snapshot["monthly_cost_base"] = *group.MonthlyCostBase
	}
	if group.YearlyCostBase != nil {
		snapshot["yearly_cost_base"] = *group.YearlyCostBase
	}
	if group.BaseCurrency != "" {
		snapshot["base_currency"] = group.BaseCurrency
	}
	if len(group.MonthlyCostByCurrency) > 0 {
		snapshot["monthly_cost_by_currency"] = group.MonthlyCostByCurrency
	}
	return snapshot
}

func RecordSnapshotFromMember(member GroupMember) EvidenceSnapshot {
	snapshot := EvidenceSnapshot{
		"vps_id":                        member.VPS.VPSID,
		"display_name":                  member.VPS.DisplayName,
		"provider_name":                 member.VPS.ProviderName,
		"country":                       member.VPS.Country,
		"region":                        member.VPS.Region,
		"city":                          member.VPS.City,
		"lifecycle_status":              string(member.VPS.LifecycleStatus),
		"usage_status":                  string(member.VPS.UsageStatus),
		"renewal_decision":              string(member.VPS.RenewalDecision),
		"subscription_count":            member.SubscriptionCount,
		"active_subscription_count":     member.ActiveSubscriptionCount,
		"inactive_subscription_count":   member.InactiveSubscriptionCount,
		"service_count":                 member.ServiceCount,
		"domain_count":                  member.DomainCount,
		"target_count":                  member.TargetCount,
		"running_target_count":          member.RunningTargetCount,
		"monitoring_link_count":         member.MonitoringLinkCount,
		"running_monitoring_count":      member.RunningMonitoringCount,
		"abnormal_monitoring_count":     member.AbnormalMonitoringCount,
		"active_incident_count":         member.ActiveIncidentCount,
		"primary_issue_summary":         member.PrimaryIssueSummary,
		"cancellation_attention_reason": member.CancellationAttentionReason,
		"renewal_within_window":         member.RenewalWithinWindow,
		"evidence_chips":                member.EvidenceChips,
		"evidence_assessment":           member.EvidenceAssessment,
		"source_availability":           member.SourceAvailability,
	}
	if member.PrimarySubscription != nil {
		snapshot["primary_subscription"] = map[string]any{
			"subscription_id":     member.PrimarySubscription.SubscriptionID,
			"status":              string(member.PrimarySubscription.Status),
			"renew_at":            member.PrimarySubscription.RenewAt,
			"monthly_price":       member.PrimarySubscription.MonthlyPrice,
			"currency":            member.PrimarySubscription.Currency,
			"monthly_price_base":  member.PrimarySubscription.MonthlyPriceBase,
			"yearly_price_base":   member.PrimarySubscription.YearlyPriceBase,
			"base_currency":       member.PrimarySubscription.BaseCurrency,
			"budget_status":       member.PrimarySubscription.BudgetStatus,
			"exchange_rate_stale": member.PrimarySubscription.ExchangeRateStale,
		}
	}
	return snapshot
}

func CreateMemberInputsByVPS(inputs []CreateRecordMemberInput) map[string]CreateRecordMemberInput {
	byVPS := make(map[string]CreateRecordMemberInput, len(inputs))
	for _, input := range inputs {
		byVPS[input.VPSID] = input
	}
	return byVPS
}

func DeriveOverview(facts []Fact, filters ListFilters) (Overview, error) {
	filters = NormalizeFilters(filters)
	if err := ValidateFilters(filters); err != nil {
		return Overview{}, err
	}
	groups, err := DeriveGroups(facts, filters)
	if err != nil {
		return Overview{}, err
	}
	allGroups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: filters.RenewWithinDays})
	if err != nil {
		return Overview{}, err
	}

	typeCounts := map[GroupType]int{}
	viewCounts := map[View]int{}
	memberIDs := map[string]struct{}{}
	for _, group := range allGroups {
		typeCounts[group.GroupType]++
		viewCounts[group.View]++
		for _, member := range group.Members {
			memberIDs[member.VPS.VPSID] = struct{}{}
		}
	}

	topGroups := make([]GroupSummary, 0, len(groups))
	for _, group := range groups {
		topGroups = append(topGroups, group.GroupSummary)
		if len(topGroups) >= 6 {
			break
		}
	}

	return Overview{
		SnapshotGeneratedAt: time.Now().UTC(),
		RenewWithinDays:     filters.RenewWithinDays,
		GroupCount:          len(allGroups),
		MemberVPSCount:      len(memberIDs),
		NeedsDecisionCount:  viewCounts[ViewNeedsDecision],
		RenewalGroupCount:   typeCounts[GroupRenewalAttention],
		RegionGroupCount:    typeCounts[GroupRegionPortfolio],
		ProviderGroupCount:  typeCounts[GroupProviderPortfolio],
		CostGroupCount:      typeCounts[GroupCostPressure],
		EvidenceGroupCount:  typeCounts[GroupEvidenceGap],
		TopGroups:           topGroups,
		TypeCounts:          typeCounts,
		ViewCounts:          viewCounts,
		SourceAvailability:  mergeSourceAvailability(facts),
	}, nil
}

func DeriveGroups(facts []Fact, filters ListFilters) ([]GroupDetail, error) {
	filters = NormalizeFilters(filters)
	if err := ValidateFilters(filters); err != nil {
		return nil, err
	}

	groups := []GroupDetail{}
	groups = append(groups, deriveRenewalGroups(facts, filters)...)
	groups = append(groups, deriveCancellationGroups(facts, filters)...)
	groups = append(groups, deriveRegionGroups(facts, filters)...)
	groups = append(groups, deriveProviderGroups(facts, filters)...)
	groups = append(groups, deriveCostGroups(facts, filters)...)
	groups = append(groups, deriveEvidenceGroups(facts, filters)...)

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Priority != groups[j].Priority {
			return groups[i].Priority > groups[j].Priority
		}
		if groups[i].RenewalWindowCount != groups[j].RenewalWindowCount {
			return groups[i].RenewalWindowCount > groups[j].RenewalWindowCount
		}
		if groups[i].MonthlyCostBase != nil && groups[j].MonthlyCostBase != nil && *groups[i].MonthlyCostBase != *groups[j].MonthlyCostBase {
			return *groups[i].MonthlyCostBase > *groups[j].MonthlyCostBase
		}
		if groups[i].MemberCount != groups[j].MemberCount {
			return groups[i].MemberCount > groups[j].MemberCount
		}
		return groups[i].Title < groups[j].Title
	})

	if filters.View == "" {
		return groups, nil
	}
	filtered := make([]GroupDetail, 0, len(groups))
	for _, group := range groups {
		if group.View == filters.View {
			filtered = append(filtered, group)
		}
	}
	return filtered, nil
}

func FindGroup(facts []Fact, groupID string, filters ListFilters) (GroupDetail, error) {
	groups, err := DeriveGroups(facts, ListFilters{RenewWithinDays: NormalizeFilters(filters).RenewWithinDays})
	if err != nil {
		return GroupDetail{}, err
	}
	for _, group := range groups {
		if group.GroupID == groupID {
			return group, nil
		}
	}
	return GroupDetail{}, ErrAssetDecisionGroupNotFound
}

func deriveRenewalGroups(facts []Fact, filters ListFilters) []GroupDetail {
	members := []GroupMember{}
	for _, fact := range facts {
		if !ordinaryPortfolioCandidate(fact) {
			continue
		}
		member := buildMember(fact, filters)
		if member.RenewalWithinWindow && fact.VPS.RenewalDecision != vpsassets.RenewalKeep {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return nil
	}
	title := fmt.Sprintf("%d 天内续费取舍", filters.RenewWithinDays)
	detail := buildGroup(GroupRenewalAttention, ViewRenewal, "window", title, title, 90, members)
	detail.GroupID = StableGroupID(GroupRenewalAttention, fmt.Sprintf("%d", filters.RenewWithinDays))
	return []GroupDetail{detail}
}

func deriveCancellationGroups(facts []Fact, filters ListFilters) []GroupDetail {
	members := []GroupMember{}
	for _, fact := range facts {
		member := buildMember(fact, filters)
		if member.CancellationAttentionReason != "" {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return nil
	}
	title := "取消与迁移联动核对"
	return []GroupDetail{buildGroup(GroupCancellationAttention, ViewNeedsDecision, "cancellation", title, title, 100, members)}
}

func deriveRegionGroups(facts []Fact, filters ListFilters) []GroupDetail {
	grouped := map[string][]GroupMember{}
	labels := map[string]string{}
	for _, fact := range facts {
		if !ordinaryPortfolioCandidate(fact) {
			continue
		}
		key, label := regionKey(fact.VPS)
		if key == "" {
			continue
		}
		grouped[key] = append(grouped[key], buildMember(fact, filters))
		labels[key] = label
	}
	return buildPortfolioGroups(GroupRegionPortfolio, ViewRegion, grouped, labels, 62, "同区取舍")
}

func deriveProviderGroups(facts []Fact, filters ListFilters) []GroupDetail {
	grouped := map[string][]GroupMember{}
	labels := map[string]string{}
	for _, fact := range facts {
		if !ordinaryPortfolioCandidate(fact) {
			continue
		}
		key := strings.TrimSpace(pointerOr(fact.VPS.ProviderID, fact.VPS.ProviderName))
		if key == "" {
			key = strings.TrimSpace(fact.VPS.ProviderName)
		}
		if key == "" {
			continue
		}
		label := strings.TrimSpace(fact.VPS.ProviderName)
		if label == "" {
			label = key
		}
		grouped[key] = append(grouped[key], buildMember(fact, filters))
		labels[key] = label
	}
	return buildPortfolioGroups(GroupProviderPortfolio, ViewProvider, grouped, labels, 58, "服务商组合")
}

func deriveCostGroups(facts []Fact, filters ListFilters) []GroupDetail {
	members := []GroupMember{}
	for _, fact := range facts {
		if !ordinaryPortfolioCandidate(fact) {
			continue
		}
		member := buildMember(fact, filters)
		if hasEvidence(member, EvidenceBudgetRisk) || hasEvidence(member, EvidenceIdlePaid) || highWeakCost(fact) {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return nil
	}
	title := "预算压力与弱承载"
	return []GroupDetail{buildGroup(GroupCostPressure, ViewCost, "cost", title, title, 82, members)}
}

func deriveEvidenceGroups(facts []Fact, filters ListFilters) []GroupDetail {
	members := []GroupMember{}
	for _, fact := range facts {
		if fact.VPS.LifecycleStatus == vpsassets.LifecycleArchived {
			continue
		}
		member := buildMember(fact, filters)
		if hasAnyEvidence(member, EvidenceMissingSubscription, EvidenceMissingMonitoring, EvidenceMissingProvider, EvidenceMissingLocation, EvidenceMissingAccess, EvidenceNoServiceContext, EvidenceSubscriptionUnavailable) {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return nil
	}
	title := "资料缺口"
	return []GroupDetail{buildGroup(GroupEvidenceGap, ViewEvidence, "evidence", title, title, 70, members)}
}

func buildPortfolioGroups(groupType GroupType, view View, grouped map[string][]GroupMember, labels map[string]string, priority int, suffix string) []GroupDetail {
	groups := []GroupDetail{}
	for key, members := range grouped {
		if len(members) < 2 {
			continue
		}
		label := labels[key]
		title := label + " · " + suffix
		groups = append(groups, buildGroup(groupType, view, key, title, label, priority, members))
	}
	return groups
}

func buildGroup(groupType GroupType, view View, scopeKey, title, scopeLabel string, priority int, members []GroupMember) GroupDetail {
	sort.SliceStable(members, func(i, j int) bool {
		return memberPriority(members[i]) > memberPriority(members[j])
	})
	summary := GroupSummary{
		GroupID:               StableGroupID(groupType, scopeKey),
		GroupType:             groupType,
		View:                  view,
		Title:                 title,
		ScopeKey:              scopeKey,
		ScopeLabel:            scopeLabel,
		Priority:              priority,
		MemberCount:           len(members),
		LifecycleCounts:       map[vpsassets.LifecycleStatus]int{},
		UsageCounts:           map[vpsassets.UsageStatus]int{},
		RenewalDecisionCounts: map[vpsassets.RenewalDecision]int{},
	}
	currencyTotals := map[string]float64{}
	var monthlyBase float64
	var yearlyBase float64
	baseCurrency := ""
	hasBase := false
	issueCounts := map[string]int{}
	for _, member := range members {
		vps := member.VPS
		summary.LifecycleCounts[vps.LifecycleStatus]++
		summary.UsageCounts[vps.UsageStatus]++
		summary.RenewalDecisionCounts[vps.RenewalDecision]++
		if member.RenewalWithinWindow {
			summary.RenewalWindowCount++
		}
		if vps.RenewalDecision == vpsassets.RenewalUnreviewed {
			summary.UnreviewedCount++
		}
		if vps.RenewalDecision == vpsassets.RenewalMigrate || vps.LifecycleStatus == vpsassets.LifecycleToMigrate {
			summary.MigrateCount++
		}
		if vps.RenewalDecision == vpsassets.RenewalCancel || vps.RenewalDecision == vpsassets.RenewalAutoRenewCancelled || vps.LifecycleStatus == vpsassets.LifecycleToCancel || vps.LifecycleStatus == vpsassets.LifecycleCancelled {
			summary.CancelCount++
		}
		if member.CancellationAttentionReason != "" {
			summary.CancellationAttentionCount++
		}
		if vps.UsageStatus == vpsassets.UsageIdle {
			summary.IdleCount++
		}
		if vps.UsageStatus == vpsassets.UsageStandby {
			summary.StandbyCount++
		}
		if vps.UsageStatus == vpsassets.UsageInUse {
			summary.InUseCount++
		}
		summary.ServiceCount += member.ServiceCount
		summary.DomainCount += member.DomainCount
		summary.TargetCount += member.TargetCount
		summary.RunningTargetCount += member.RunningTargetCount
		summary.MonitoringLinkCount += member.MonitoringLinkCount
		summary.AbnormalMonitoringCount += member.AbnormalMonitoringCount
		summary.ActiveIncidentCount += member.ActiveIncidentCount
		if member.PrimaryIssueSummary != "" {
			issueCounts[member.PrimaryIssueSummary]++
		}
		for _, chip := range member.EvidenceChips {
			appendUniqueChip(&summary.EvidenceChips, chip)
		}
		if member.PrimarySubscription != nil && member.PrimarySubscription.Status == subscriptions.StatusActive {
			sub := member.PrimarySubscription
			if sub.Currency != "" {
				currencyTotals[sub.Currency] += sub.MonthlyPrice
			}
			if sub.MonthlyPriceBase != nil {
				monthlyBase += *sub.MonthlyPriceBase
				if sub.YearlyPriceBase != nil {
					yearlyBase += *sub.YearlyPriceBase
				} else {
					yearlyBase += *sub.MonthlyPriceBase * 12
				}
				hasBase = true
				if baseCurrency == "" {
					baseCurrency = sub.BaseCurrency
				}
			}
		}
	}
	for currency, monthly := range currencyTotals {
		summary.MonthlyCostByCurrency = append(summary.MonthlyCostByCurrency, CostByCurrency{
			Currency:     currency,
			MonthlyTotal: monthly,
			YearlyTotal:  monthly * 12,
		})
	}
	sort.Slice(summary.MonthlyCostByCurrency, func(i, j int) bool {
		return summary.MonthlyCostByCurrency[i].Currency < summary.MonthlyCostByCurrency[j].Currency
	})
	if hasBase {
		summary.MonthlyCostBase = &monthlyBase
		summary.YearlyCostBase = &yearlyBase
		summary.BaseCurrency = baseCurrency
	}
	summary.PrimaryIssueSummary = topIssue(issueCounts)
	summary.EvidenceAssessment = assessGroup(groupType, priority, members)
	return GroupDetail{GroupSummary: summary, Members: members}
}

func buildMember(fact Fact, filters ListFilters) GroupMember {
	vps := fact.VPS
	vps.ActiveMonitoringInstanceLinkCount = fact.MonitoringLinkCount
	vps.RunningMonitoringInstanceCount = fact.RunningMonitoringCount
	vps.RunningTargetCount = fact.RunningTargetCount
	member := GroupMember{
		VPS:                       vps,
		PrimarySubscription:       cloneSubscription(fact.PrimarySubscription),
		SubscriptionCount:         fact.SubscriptionCount,
		ActiveSubscriptionCount:   fact.ActiveSubscriptionCount,
		InactiveSubscriptionCount: fact.InactiveSubscriptionCount,
		ServiceCount:              fact.ServiceCount,
		DomainCount:               fact.DomainCount,
		TargetCount:               fact.TargetCount,
		RunningTargetCount:        fact.RunningTargetCount,
		MonitoringLinkCount:       fact.MonitoringLinkCount,
		RunningMonitoringCount:    fact.RunningMonitoringCount,
		AbnormalMonitoringCount:   fact.AbnormalMonitoringCount,
		ActiveIncidentCount:       fact.ActiveIncidentCount,
		PrimaryIssueSummary:       fact.PrimaryIssueSummary,
		SourceAvailability:        fact.SourceAvailability,
	}
	member.RenewalWithinWindow = renewalWithinWindow(fact.PrimarySubscription, filters.RenewWithinDays)
	member.CancellationAttentionReason = cancellationReason(fact)
	member.EvidenceChips = buildEvidenceChips(fact, member.RenewalWithinWindow)
	member.SuggestedRole, member.SuggestedAction = suggestMember(member)
	member.EvidenceAssessment = assessMember(member)
	return member
}

func buildEvidenceChips(fact Fact, renewalWindow bool) []EvidenceChip {
	chips := []EvidenceChip{}
	if renewalWindow {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceRenewalDue, Label: "续费临近", Tone: "alert"})
	}
	if fact.PrimarySubscription != nil && fact.PrimarySubscription.Status == subscriptions.StatusActive && fact.VPS.UsageStatus == vpsassets.UsageIdle {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceIdlePaid, Label: "闲置付费", Tone: "alert"})
	}
	if !fact.SourceAvailability.Subscriptions {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceSubscriptionUnavailable, Label: "订阅证据不可用", Tone: "notice"})
	} else if fact.SubscriptionCount == 0 && fact.VPS.LifecycleStatus != vpsassets.LifecycleArchived {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceMissingSubscription, Label: "缺订阅", Tone: "alert"})
	}
	if fact.SourceAvailability.Monitoring && fact.MonitoringLinkCount == 0 && fact.VPS.LifecycleStatus != vpsassets.LifecycleArchived {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceMissingMonitoring, Label: "未关联监控", Tone: "notice"})
	}
	if fact.ServiceCount > 0 || fact.DomainCount > 0 || fact.TargetCount > 0 {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceCarriesService, Label: "承载服务", Tone: "normal"})
	} else if fact.SourceAvailability.Services && fact.SourceAvailability.Domains && fact.VPS.UsageStatus == vpsassets.UsageInUse {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceNoServiceContext, Label: "缺服务上下文", Tone: "notice"})
	}
	if cancellationReason(fact) != "" {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceCancellationLinkage, Label: "取消联动", Tone: "critical", Details: cancellationReason(fact)})
	}
	if fact.PrimarySubscription != nil {
		if fact.PrimarySubscription.BudgetStatus == "warning" || fact.PrimarySubscription.BudgetStatus == "over" {
			appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceBudgetRisk, Label: "预算风险", Tone: "alert"})
		}
		if fact.PrimarySubscription.ExchangeRateStale {
			appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceExchangeRateStale, Label: "汇率异常", Tone: "notice"})
		}
	}
	if fact.AbnormalMonitoringCount > 0 || fact.ActiveIncidentCount > 0 {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceAbnormalMonitoring, Label: "异常关联", Tone: "critical", Details: fact.PrimaryIssueSummary})
	}
	if strings.TrimSpace(pointerOr(fact.VPS.ProviderID, fact.VPS.ProviderName)) == "" {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceMissingProvider, Label: "缺服务商", Tone: "notice"})
	}
	if strings.TrimSpace(fact.VPS.Country+fact.VPS.Region+fact.VPS.City) == "" {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceMissingLocation, Label: "缺地域", Tone: "notice"})
	}
	if strings.TrimSpace(fact.VPS.IPv4+fact.VPS.IPv6+fact.VPS.SSHHost) == "" {
		appendUniqueChip(&chips, EvidenceChip{Kind: EvidenceMissingAccess, Label: "缺访问资料", Tone: "notice"})
	}
	return chips
}

func suggestMember(member GroupMember) (SuggestedRole, SuggestedAction) {
	if member.CancellationAttentionReason != "" || member.VPS.LifecycleStatus == vpsassets.LifecycleToCancel || member.VPS.LifecycleStatus == vpsassets.LifecycleCancelled || member.VPS.RenewalDecision == vpsassets.RenewalCancel || member.VPS.RenewalDecision == vpsassets.RenewalAutoRenewCancelled {
		return RoleRetireCandidate, ActionOpenCancellationWorkbench
	}
	if hasAnyEvidence(member, EvidenceMissingSubscription, EvidenceMissingMonitoring, EvidenceMissingProvider, EvidenceMissingLocation, EvidenceMissingAccess, EvidenceNoServiceContext, EvidenceSubscriptionUnavailable) {
		return RoleEvidenceNeeded, ActionCompleteEvidence
	}
	if member.VPS.UsageStatus == vpsassets.UsageIdle && member.ActiveSubscriptionCount > 0 {
		return RoleRetireCandidate, ActionCancel
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalMigrate || member.VPS.LifecycleStatus == vpsassets.LifecycleToMigrate {
		return RoleObserveCandidate, ActionMigrate
	}
	if member.VPS.UsageStatus == vpsassets.UsageInUse && member.AbnormalMonitoringCount == 0 {
		return RolePrimaryCandidate, ActionKeep
	}
	if member.VPS.UsageStatus == vpsassets.UsageStandby {
		return RoleStandbyCandidate, ActionObserve
	}
	return RoleObserveCandidate, ActionReview
}

func cancellationReason(fact Fact) string {
	if fact.VPS.LifecycleStatus == vpsassets.LifecycleToCancel || fact.VPS.LifecycleStatus == vpsassets.LifecycleCancelled {
		if fact.RunningMonitoringCount > 0 || fact.RunningTargetCount > 0 {
			return "VPS 已进入取消链路但仍有关联运行对象"
		}
		if fact.ActiveSubscriptionCount > 0 {
			return "VPS 已取消或待取消但仍存在 active 订阅"
		}
	}
	if fact.InactiveSubscriptionCount > 0 && fact.VPS.LifecycleStatus != vpsassets.LifecycleToCancel && fact.VPS.LifecycleStatus != vpsassets.LifecycleCancelled {
		return "订阅已取消/过期/暂停但 VPS 仍未进入取消链路"
	}
	if (fact.VPS.RenewalDecision == vpsassets.RenewalCancel || fact.VPS.RenewalDecision == vpsassets.RenewalAutoRenewCancelled) &&
		fact.VPS.LifecycleStatus != vpsassets.LifecycleToCancel && fact.VPS.LifecycleStatus != vpsassets.LifecycleCancelled {
		return "续费决策为取消但 lifecycle 尚未同步"
	}
	return ""
}

func ordinaryPortfolioCandidate(fact Fact) bool {
	return fact.VPS.LifecycleStatus != vpsassets.LifecycleArchived && fact.VPS.LifecycleStatus != vpsassets.LifecycleCancelled
}

func renewalWithinWindow(sub *subscriptions.Record, days int) bool {
	if sub == nil || sub.RenewAt == nil || sub.Status != subscriptions.StatusActive {
		return false
	}
	now := subscriptions.NewDate(time.Now().UTC()).Time
	renewAt := sub.RenewAt.Time
	if renewAt.Before(now) {
		return false
	}
	return !renewAt.After(now.AddDate(0, 0, days))
}

func highWeakCost(fact Fact) bool {
	if fact.PrimarySubscription == nil || fact.PrimarySubscription.Status != subscriptions.StatusActive {
		return false
	}
	if fact.ServiceCount+fact.DomainCount+fact.TargetCount > 0 {
		return false
	}
	if fact.PrimarySubscription.MonthlyPriceBase != nil {
		return *fact.PrimarySubscription.MonthlyPriceBase >= 100
	}
	return fact.PrimarySubscription.MonthlyPrice >= 20
}

func regionKey(vps vpsassets.Record) (string, string) {
	parts := []string{}
	for _, part := range []string{vps.Country, vps.Region, vps.City} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", ""
	}
	label := strings.Join(parts, " / ")
	return strings.ToLower(label), label
}

func appendUniqueChip(chips *[]EvidenceChip, chip EvidenceChip) {
	for _, existing := range *chips {
		if existing.Kind == chip.Kind {
			return
		}
	}
	*chips = append(*chips, chip)
}

func validateRenewWindow(days int) (int, error) {
	switch days {
	case 30, 60, 90:
		return days, nil
	default:
		return 0, ErrInvalidAssetDecisionInput
	}
}

func mergeMemberSourceAvailability(members []GroupMember) SourceAvailability {
	availability := SourceAvailability{
		Subscriptions: true,
		Services:      true,
		Domains:       true,
		Monitoring:    true,
		Targets:       true,
	}
	for _, member := range members {
		availability.Subscriptions = availability.Subscriptions && member.SourceAvailability.Subscriptions
		availability.Services = availability.Services && member.SourceAvailability.Services
		availability.Domains = availability.Domains && member.SourceAvailability.Domains
		availability.Monitoring = availability.Monitoring && member.SourceAvailability.Monitoring
		availability.Targets = availability.Targets && member.SourceAvailability.Targets
	}
	return availability
}

func hasEvidence(member GroupMember, kind EvidenceKind) bool {
	for _, chip := range member.EvidenceChips {
		if chip.Kind == kind {
			return true
		}
	}
	return false
}

func hasAnyEvidence(member GroupMember, kinds ...EvidenceKind) bool {
	for _, kind := range kinds {
		if hasEvidence(member, kind) {
			return true
		}
	}
	return false
}

func memberPriority(member GroupMember) int {
	score := 0
	if member.CancellationAttentionReason != "" {
		score += 100
	}
	if member.RenewalWithinWindow {
		score += 50
	}
	score += member.ActiveIncidentCount * 10
	score += member.AbnormalMonitoringCount * 8
	if member.VPS.RenewalDecision == vpsassets.RenewalUnreviewed {
		score += 12
	}
	if member.VPS.UsageStatus == vpsassets.UsageIdle && member.ActiveSubscriptionCount > 0 {
		score += 18
	}
	return score
}

func topIssue(counts map[string]int) string {
	best := ""
	bestCount := 0
	for issue, count := range counts {
		if count > bestCount || (count == bestCount && issue < best) {
			best = issue
			bestCount = count
		}
	}
	return best
}

func pointerOr(value *string, fallback string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return strings.TrimSpace(*value)
	}
	return strings.TrimSpace(fallback)
}

func cloneSubscription(record *subscriptions.Record) *subscriptions.Record {
	if record == nil {
		return nil
	}
	cloned := *record
	cloned.Labels = append([]string(nil), record.Labels...)
	return &cloned
}

func mergeSourceAvailability(facts []Fact) SourceAvailability {
	availability := SourceAvailability{
		Subscriptions: true,
		Services:      true,
		Domains:       true,
		Monitoring:    true,
		Targets:       true,
	}
	if len(facts) == 0 {
		return availability
	}
	for _, fact := range facts {
		availability.Subscriptions = availability.Subscriptions && fact.SourceAvailability.Subscriptions
		availability.Services = availability.Services && fact.SourceAvailability.Services
		availability.Domains = availability.Domains && fact.SourceAvailability.Domains
		availability.Monitoring = availability.Monitoring && fact.SourceAvailability.Monitoring
		availability.Targets = availability.Targets && fact.SourceAvailability.Targets
	}
	return availability
}
