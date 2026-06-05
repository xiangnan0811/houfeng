package assetdecisions

import (
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/vpsassets"
)

const ManualGroupSourceManual = "manual"

type ManualGroupStatus string

const (
	ManualGroupStatusActive   ManualGroupStatus = "active"
	ManualGroupStatusArchived ManualGroupStatus = "archived"
)

type ManualGroupScenario string

const (
	ManualGroupScenarioGeneral             ManualGroupScenario = "general"
	ManualGroupScenarioPrimaryStandby      ManualGroupScenario = "primary_standby"
	ManualGroupScenarioBudgetReduction     ManualGroupScenario = "budget_reduction"
	ManualGroupScenarioProviderReview      ManualGroupScenario = "provider_review"
	ManualGroupScenarioRegionReview        ManualGroupScenario = "region_review"
	ManualGroupScenarioMigrationRetirement ManualGroupScenario = "migration_retirement"
	ManualGroupScenarioEvidenceCleanup     ManualGroupScenario = "evidence_cleanup"
)

type ManualGroupRow struct {
	ManualGroupID   string
	Status          ManualGroupStatus
	Scenario        ManualGroupScenario
	Title           string
	Goal            string
	Note            string
	SourceType      string
	SourceGroupID   string
	SourceGroupType GroupType
	SourceView      View
	ScopeKey        string
	ScopeLabel      string
	RenewWithinDays int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
}

type ManualGroupMemberRow struct {
	ManualGroupID    string
	VPSID            string
	IntendedRole     SuggestedRole
	IntendedAction   SuggestedAction
	Reason           string
	Note             string
	SortOrder        int
	EvidenceSnapshot EvidenceSnapshot
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ManualGroupSummary struct {
	ManualGroupID              string              `json:"manual_group_id"`
	Status                     ManualGroupStatus   `json:"status"`
	Scenario                   ManualGroupScenario `json:"scenario"`
	Title                      string              `json:"title"`
	Goal                       string              `json:"goal"`
	Note                       string              `json:"note"`
	SourceType                 string              `json:"source_type"`
	SourceGroupID              string              `json:"source_group_id,omitempty"`
	SourceGroupType            GroupType           `json:"source_group_type,omitempty"`
	SourceView                 View                `json:"source_view,omitempty"`
	ScopeKey                   string              `json:"scope_key,omitempty"`
	ScopeLabel                 string              `json:"scope_label,omitempty"`
	RenewWithinDays            int                 `json:"renew_within_days"`
	MemberCount                int                 `json:"member_count"`
	LifecycleCounts            map[string]int      `json:"lifecycle_counts"`
	UsageCounts                map[string]int      `json:"usage_counts"`
	RenewalDecisionCounts      map[string]int      `json:"renewal_decision_counts"`
	RenewalWindowCount         int                 `json:"renewal_window_count"`
	UnreviewedCount            int                 `json:"unreviewed_count"`
	MigrateCount               int                 `json:"migrate_count"`
	CancelCount                int                 `json:"cancel_count"`
	CancellationAttentionCount int                 `json:"cancellation_attention_count"`
	IdleCount                  int                 `json:"idle_count"`
	StandbyCount               int                 `json:"standby_count"`
	InUseCount                 int                 `json:"in_use_count"`
	ServiceCount               int                 `json:"service_count"`
	DomainCount                int                 `json:"domain_count"`
	TargetCount                int                 `json:"target_count"`
	RunningTargetCount         int                 `json:"running_target_count"`
	MonitoringLinkCount        int                 `json:"monitoring_link_count"`
	AbnormalMonitoringCount    int                 `json:"abnormal_monitoring_count"`
	ActiveIncidentCount        int                 `json:"active_incident_count"`
	PrimaryIssueSummary        string              `json:"primary_issue_summary"`
	MonthlyCostByCurrency      []CostByCurrency    `json:"monthly_cost_by_currency"`
	MonthlyCostBase            *float64            `json:"monthly_cost_base,omitempty"`
	YearlyCostBase             *float64            `json:"yearly_cost_base,omitempty"`
	BaseCurrency               string              `json:"base_currency,omitempty"`
	EvidenceChips              []EvidenceChip      `json:"evidence_chips"`
	EvidenceAssessment         EvidenceAssessment  `json:"evidence_assessment"`
	SourceAvailability         SourceAvailability  `json:"source_availability"`
	CreatedAt                  time.Time           `json:"created_at"`
	UpdatedAt                  time.Time           `json:"updated_at"`
	ArchivedAt                 *time.Time          `json:"archived_at,omitempty"`
}

type ManualGroupDetail struct {
	ManualGroupSummary
	Members []ManualGroupMember `json:"members"`
}

type ManualGroupMember struct {
	GroupMember
	ManualGroupID    string           `json:"manual_group_id"`
	VPSID            string           `json:"vps_id"`
	IntendedRole     SuggestedRole    `json:"intended_role"`
	IntendedAction   SuggestedAction  `json:"intended_action"`
	Reason           string           `json:"reason"`
	Note             string           `json:"note"`
	SortOrder        int              `json:"sort_order"`
	EvidenceSnapshot EvidenceSnapshot `json:"evidence_snapshot"`
	CurrentFactFound bool             `json:"current_fact_found"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

type CreateManualGroupInput struct {
	SourceType      string                         `json:"source_type"`
	SourceGroupID   string                         `json:"source_group_id"`
	RenewWithinDays int                            `json:"renew_within_days"`
	Status          ManualGroupStatus              `json:"status"`
	Scenario        ManualGroupScenario            `json:"scenario"`
	Title           string                         `json:"title"`
	Goal            string                         `json:"goal"`
	Note            string                         `json:"note"`
	Members         []CreateManualGroupMemberInput `json:"members"`
}

type CreateManualGroupMemberInput struct {
	VPSID          string          `json:"vps_id"`
	IntendedRole   SuggestedRole   `json:"intended_role"`
	IntendedAction SuggestedAction `json:"intended_action"`
	Reason         string          `json:"reason"`
	Note           string          `json:"note"`
	SortOrder      int             `json:"sort_order"`
}

type PatchManualGroupInput struct {
	Status   PatchManualGroupStatus   `json:"status"`
	Scenario PatchManualGroupScenario `json:"scenario"`
	Title    PatchString              `json:"title"`
	Goal     PatchString              `json:"goal"`
	Note     PatchString              `json:"note"`
}

type PatchManualGroupMemberInput struct {
	IntendedRole   PatchSuggestedRole   `json:"intended_role"`
	IntendedAction PatchSuggestedAction `json:"intended_action"`
	Reason         PatchString          `json:"reason"`
	Note           PatchString          `json:"note"`
	SortOrder      PatchInt             `json:"sort_order"`
}

func NormalizeCreateManualGroupInput(input CreateManualGroupInput) CreateManualGroupInput {
	input.SourceType = strings.TrimSpace(input.SourceType)
	if input.SourceType == "" {
		input.SourceType = ManualGroupSourceManual
	}
	input.SourceGroupID = strings.TrimSpace(input.SourceGroupID)
	input.Title = strings.TrimSpace(input.Title)
	input.Goal = strings.TrimSpace(input.Goal)
	input.Note = strings.TrimSpace(input.Note)
	if input.RenewWithinDays == 0 {
		input.RenewWithinDays = 30
	}
	if input.Status == "" {
		input.Status = ManualGroupStatusActive
	}
	if input.Scenario == "" {
		input.Scenario = ManualGroupScenarioGeneral
	}
	members := make([]CreateManualGroupMemberInput, 0, len(input.Members))
	for _, member := range input.Members {
		member.VPSID = strings.TrimSpace(member.VPSID)
		member.Reason = strings.TrimSpace(member.Reason)
		member.Note = strings.TrimSpace(member.Note)
		members = append(members, member)
	}
	input.Members = members
	return input
}

func NormalizePatchManualGroupInput(input PatchManualGroupInput) PatchManualGroupInput {
	input.Title.Value = strings.TrimSpace(input.Title.Value)
	input.Goal.Value = strings.TrimSpace(input.Goal.Value)
	input.Note.Value = strings.TrimSpace(input.Note.Value)
	return input
}

func NormalizeCreateManualGroupMemberInput(input CreateManualGroupMemberInput) CreateManualGroupMemberInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func NormalizePatchManualGroupMemberInput(input PatchManualGroupMemberInput) PatchManualGroupMemberInput {
	input.Reason.Value = strings.TrimSpace(input.Reason.Value)
	input.Note.Value = strings.TrimSpace(input.Note.Value)
	return input
}

func ValidateCreateManualGroupInput(input CreateManualGroupInput) error {
	if err := ValidateManualGroupSourceType(input.SourceType); err != nil {
		return err
	}
	if input.SourceType == RecordSourceAutoGroup && input.SourceGroupID == "" {
		return ErrInvalidAssetDecisionInput
	}
	if input.SourceType == ManualGroupSourceManual && input.Title == "" {
		return ErrInvalidAssetDecisionInput
	}
	if _, err := validateRenewWindow(input.RenewWithinDays); err != nil {
		return err
	}
	if err := ValidateManualGroupStatus(input.Status); err != nil {
		return err
	}
	if err := ValidateManualGroupScenario(input.Scenario); err != nil {
		return err
	}
	return validateCreateManualGroupMembers(input.Members)
}

func ValidateCreateManualGroupMemberInput(input CreateManualGroupMemberInput) error {
	if input.VPSID == "" {
		return ErrInvalidAssetDecisionInput
	}
	if input.IntendedRole != "" {
		if err := ValidateSuggestedRole(input.IntendedRole); err != nil {
			return err
		}
	}
	if input.IntendedAction != "" {
		if err := ValidateSuggestedAction(input.IntendedAction); err != nil {
			return err
		}
	}
	return nil
}

func ValidatePatchManualGroupInput(input PatchManualGroupInput) error {
	if input.Title.Set && input.Title.Value == "" {
		return ErrInvalidAssetDecisionInput
	}
	if input.Status.Set {
		if err := ValidateManualGroupStatus(input.Status.Value); err != nil {
			return err
		}
	}
	if input.Scenario.Set {
		if err := ValidateManualGroupScenario(input.Scenario.Value); err != nil {
			return err
		}
	}
	if !input.Title.Set && !input.Goal.Set && !input.Note.Set && !input.Status.Set && !input.Scenario.Set {
		return ErrInvalidAssetDecisionInput
	}
	return nil
}

func ValidatePatchManualGroupMemberInput(input PatchManualGroupMemberInput) error {
	if input.IntendedRole.Set {
		if err := ValidateSuggestedRole(input.IntendedRole.Value); err != nil {
			return err
		}
	}
	if input.IntendedAction.Set {
		if err := ValidateSuggestedAction(input.IntendedAction.Value); err != nil {
			return err
		}
	}
	if !input.IntendedRole.Set && !input.IntendedAction.Set && !input.Reason.Set && !input.Note.Set && !input.SortOrder.Set {
		return ErrInvalidAssetDecisionInput
	}
	return nil
}

func ValidateManualGroupSourceType(sourceType string) error {
	switch sourceType {
	case ManualGroupSourceManual, RecordSourceAutoGroup:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ValidateManualGroupStatus(status ManualGroupStatus) error {
	switch status {
	case ManualGroupStatusActive, ManualGroupStatusArchived:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func ValidateManualGroupScenario(scenario ManualGroupScenario) error {
	switch scenario {
	case ManualGroupScenarioGeneral,
		ManualGroupScenarioPrimaryStandby,
		ManualGroupScenarioBudgetReduction,
		ManualGroupScenarioProviderReview,
		ManualGroupScenarioRegionReview,
		ManualGroupScenarioMigrationRetirement,
		ManualGroupScenarioEvidenceCleanup:
		return nil
	default:
		return ErrInvalidAssetDecisionInput
	}
}

func validateCreateManualGroupMembers(members []CreateManualGroupMemberInput) error {
	seen := map[string]struct{}{}
	for _, member := range members {
		if err := ValidateCreateManualGroupMemberInput(member); err != nil {
			return err
		}
		if _, ok := seen[member.VPSID]; ok {
			return ErrInvalidAssetDecisionInput
		}
		seen[member.VPSID] = struct{}{}
	}
	return nil
}

func ManualGroupDetailFromRows(row ManualGroupRow, memberRows []ManualGroupMemberRow, facts []Fact) ManualGroupDetail {
	filters := ListFilters{RenewWithinDays: row.RenewWithinDays}
	factMap := FactsByVPSID(facts)
	members := make([]ManualGroupMember, 0, len(memberRows))
	for _, memberRow := range memberRows {
		member := ManualGroupMember{
			ManualGroupID:    memberRow.ManualGroupID,
			VPSID:            memberRow.VPSID,
			IntendedRole:     memberRow.IntendedRole,
			IntendedAction:   memberRow.IntendedAction,
			Reason:           memberRow.Reason,
			Note:             memberRow.Note,
			SortOrder:        memberRow.SortOrder,
			EvidenceSnapshot: cloneSnapshot(memberRow.EvidenceSnapshot),
			CreatedAt:        memberRow.CreatedAt,
			UpdatedAt:        memberRow.UpdatedAt,
		}
		if fact, ok := factMap[memberRow.VPSID]; ok {
			current := GroupMemberFromFact(fact, filters)
			member.GroupMember = current
			member.CurrentFactFound = true
			if member.IntendedRole == "" {
				member.IntendedRole = current.SuggestedRole
			}
			if member.IntendedAction == "" {
				member.IntendedAction = current.SuggestedAction
			}
			if member.EvidenceSnapshot == nil {
				member.EvidenceSnapshot = RecordSnapshotFromMember(current)
			}
		} else {
			member.CurrentFactFound = false
			if member.EvidenceSnapshot == nil {
				member.EvidenceSnapshot = EvidenceSnapshot{}
			}
			member.EvidenceChips = []EvidenceChip{{
				Kind:    EvidenceCurrentFactMissing,
				Label:   "当前事实缺失",
				Tone:    "critical",
				Details: "手工组合成员仍存在，但当前资产聚合事实中找不到对应 VPS",
			}}
			member.EvidenceAssessment = EvidenceAssessment{
				ConfidenceScore: 0,
				PressureScore:   100,
				ReadinessScore:  0,
				QualityTier:     EvidenceTierBlocked,
				DecisionBias:    EvidenceBiasReview,
				RiskSignalCount: 1,
				Summary:         "当前事实缺失，需人工核对",
			}
		}
		members = append(members, member)
	}

	sort.SliceStable(members, func(i, j int) bool {
		if members[i].SortOrder != members[j].SortOrder {
			return members[i].SortOrder < members[j].SortOrder
		}
		left := strings.TrimSpace(members[i].VPS.DisplayName)
		if left == "" {
			left = members[i].VPSID
		}
		right := strings.TrimSpace(members[j].VPS.DisplayName)
		if right == "" {
			right = members[j].VPSID
		}
		return left < right
	})

	currentMembers := make([]GroupMember, 0, len(members))
	missingFactCount := 0
	for _, member := range members {
		if member.CurrentFactFound {
			currentMembers = append(currentMembers, member.GroupMember)
		} else {
			missingFactCount++
		}
	}
	groupType := row.SourceGroupType
	if groupType == "" {
		groupType = GroupEvidenceGap
	}
	view := row.SourceView
	if view == "" {
		view = ViewNeedsDecision
	}
	scopeKey := row.ScopeKey
	if scopeKey == "" {
		scopeKey = row.ManualGroupID
	}
	scopeLabel := row.ScopeLabel
	if scopeLabel == "" {
		scopeLabel = row.Title
	}
	detail := BuildGroupFromMembers(groupType, view, scopeKey, row.Title, scopeLabel, 64, currentMembers)
	summary := ManualGroupSummary{
		ManualGroupID:              row.ManualGroupID,
		Status:                     row.Status,
		Scenario:                   row.Scenario,
		Title:                      row.Title,
		Goal:                       row.Goal,
		Note:                       row.Note,
		SourceType:                 row.SourceType,
		SourceGroupID:              row.SourceGroupID,
		SourceGroupType:            row.SourceGroupType,
		SourceView:                 row.SourceView,
		ScopeKey:                   row.ScopeKey,
		ScopeLabel:                 row.ScopeLabel,
		RenewWithinDays:            row.RenewWithinDays,
		MemberCount:                len(members),
		LifecycleCounts:            stringifyLifecycleCounts(detail.LifecycleCounts),
		UsageCounts:                stringifyUsageCounts(detail.UsageCounts),
		RenewalDecisionCounts:      stringifyRenewalDecisionCounts(detail.RenewalDecisionCounts),
		RenewalWindowCount:         detail.RenewalWindowCount,
		UnreviewedCount:            detail.UnreviewedCount,
		MigrateCount:               detail.MigrateCount,
		CancelCount:                detail.CancelCount,
		CancellationAttentionCount: detail.CancellationAttentionCount,
		IdleCount:                  detail.IdleCount,
		StandbyCount:               detail.StandbyCount,
		InUseCount:                 detail.InUseCount,
		ServiceCount:               detail.ServiceCount,
		DomainCount:                detail.DomainCount,
		TargetCount:                detail.TargetCount,
		RunningTargetCount:         detail.RunningTargetCount,
		MonitoringLinkCount:        detail.MonitoringLinkCount,
		AbnormalMonitoringCount:    detail.AbnormalMonitoringCount,
		ActiveIncidentCount:        detail.ActiveIncidentCount,
		PrimaryIssueSummary:        detail.PrimaryIssueSummary,
		MonthlyCostByCurrency:      detail.MonthlyCostByCurrency,
		MonthlyCostBase:            detail.MonthlyCostBase,
		YearlyCostBase:             detail.YearlyCostBase,
		BaseCurrency:               detail.BaseCurrency,
		EvidenceChips:              detail.EvidenceChips,
		EvidenceAssessment:         detail.EvidenceAssessment,
		SourceAvailability:         mergeMemberSourceAvailability(currentMembers),
		CreatedAt:                  row.CreatedAt,
		UpdatedAt:                  row.UpdatedAt,
		ArchivedAt:                 row.ArchivedAt,
	}
	for _, member := range members {
		for _, chip := range member.EvidenceChips {
			appendUniqueChip(&summary.EvidenceChips, chip)
		}
	}
	if missingFactCount > 0 {
		summary.EvidenceAssessment.QualityTier = EvidenceTierBlocked
		summary.EvidenceAssessment.DecisionBias = EvidenceBiasReview
		summary.EvidenceAssessment.RiskSignalCount += missingFactCount
		summary.EvidenceAssessment.PressureScore = 100
		summary.EvidenceAssessment.ReadinessScore = 0
		summary.EvidenceAssessment.Summary = "存在当前事实缺失成员，需人工核对"
	}
	if len(members) == 0 {
		summary.SourceAvailability = mergeSourceAvailability(facts)
	}
	return ManualGroupDetail{ManualGroupSummary: summary, Members: members}
}

func GroupMemberFromFact(fact Fact, filters ListFilters) GroupMember {
	return buildMember(fact, NormalizeFilters(filters))
}

func BuildGroupFromMembers(groupType GroupType, view View, scopeKey, title, scopeLabel string, priority int, members []GroupMember) GroupDetail {
	return buildGroup(groupType, view, scopeKey, title, scopeLabel, priority, members)
}

func RecordSnapshotFromManualGroup(group ManualGroupDetail) EvidenceSnapshot {
	snapshot := RecordSnapshotFromGroup(ManualGroupAsGroupDetail(group))
	snapshot["manual_group_id"] = group.ManualGroupID
	snapshot["manual_group_status"] = string(group.Status)
	snapshot["manual_group_scenario"] = string(group.Scenario)
	snapshot["goal"] = group.Goal
	snapshot["note"] = group.Note
	return snapshot
}

func ManualGroupAsGroupDetail(group ManualGroupDetail) GroupDetail {
	groupType := group.SourceGroupType
	if groupType == "" {
		groupType = GroupEvidenceGap
	}
	view := group.SourceView
	if view == "" {
		view = ViewNeedsDecision
	}
	scopeKey := group.ScopeKey
	if scopeKey == "" {
		scopeKey = group.ManualGroupID
	}
	scopeLabel := group.ScopeLabel
	if scopeLabel == "" {
		scopeLabel = group.Title
	}
	members := make([]GroupMember, 0, len(group.Members))
	for _, member := range group.Members {
		if member.CurrentFactFound {
			members = append(members, member.GroupMember)
		}
	}
	detail := BuildGroupFromMembers(groupType, view, scopeKey, group.Title, scopeLabel, 64, members)
	detail.GroupID = group.ManualGroupID
	return detail
}

func ManualGroupMemberInputsByVPS(inputs []CreateManualGroupMemberInput) map[string]CreateManualGroupMemberInput {
	byVPS := make(map[string]CreateManualGroupMemberInput, len(inputs))
	for _, input := range inputs {
		byVPS[input.VPSID] = input
	}
	return byVPS
}

func cloneSnapshot(snapshot EvidenceSnapshot) EvidenceSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := make(EvidenceSnapshot, len(snapshot))
	for key, value := range snapshot {
		clone[key] = value
	}
	return clone
}

func stringifyLifecycleCounts(counts map[vpsassets.LifecycleStatus]int) map[string]int {
	out := make(map[string]int, len(counts))
	for status, count := range counts {
		out[string(status)] = count
	}
	return out
}

func stringifyUsageCounts(counts map[vpsassets.UsageStatus]int) map[string]int {
	out := make(map[string]int, len(counts))
	for status, count := range counts {
		out[string(status)] = count
	}
	return out
}

func stringifyRenewalDecisionCounts(counts map[vpsassets.RenewalDecision]int) map[string]int {
	out := make(map[string]int, len(counts))
	for decision, count := range counts {
		out[string(decision)] = count
	}
	return out
}
