package assetdecisions

import (
	"fmt"
	"sort"
	"strings"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type ComparisonPrimaryAxis string

const (
	ComparisonAxisRenewal        ComparisonPrimaryAxis = "renewal"
	ComparisonAxisCost           ComparisonPrimaryAxis = "cost"
	ComparisonAxisServiceContext ComparisonPrimaryAxis = "service_context"
	ComparisonAxisMonitoring     ComparisonPrimaryAxis = "monitoring"
	ComparisonAxisEvidence       ComparisonPrimaryAxis = "evidence"
	ComparisonAxisLifecycle      ComparisonPrimaryAxis = "lifecycle"
	ComparisonAxisReview         ComparisonPrimaryAxis = "review"
)

type ComparisonLane string

const (
	ComparisonLanePrimary  ComparisonLane = "primary"
	ComparisonLaneStandby  ComparisonLane = "standby"
	ComparisonLaneObserve  ComparisonLane = "observe"
	ComparisonLaneRetire   ComparisonLane = "retire"
	ComparisonLaneEvidence ComparisonLane = "evidence"
	ComparisonLaneReview   ComparisonLane = "review"
)

type ComparisonSignal struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Tone    string `json:"tone"`
	Details string `json:"details,omitempty"`
}

type ComparisonLaneCount struct {
	Lane  ComparisonLane `json:"lane"`
	Count int            `json:"count"`
}

type ComparisonInsight struct {
	Summary        string                `json:"summary"`
	PrimaryAxis    ComparisonPrimaryAxis `json:"primary_axis"`
	LaneCounts     []ComparisonLaneCount `json:"lane_counts"`
	PriorityVPSIDs []string              `json:"priority_vps_ids"`
	Tradeoffs      []ComparisonSignal    `json:"tradeoffs"`
}

type MemberComparisonInsight struct {
	Rank      int                `json:"rank"`
	Lane      ComparisonLane     `json:"lane"`
	Summary   string             `json:"summary"`
	Strengths []ComparisonSignal `json:"strengths"`
	Risks     []ComparisonSignal `json:"risks"`
	Gaps      []ComparisonSignal `json:"gaps"`
	Tradeoffs []ComparisonSignal `json:"tradeoffs"`
}

func CompareGroup(group GroupDetail) ComparisonInsight {
	laneCounts := comparisonLaneCounts(group.Members)
	axis := groupComparisonAxis(group, laneCounts)
	priorityIDs := comparisonPriorityVPSIDs(group.Members, 3)
	return ComparisonInsight{
		Summary:        groupComparisonSummary(group, axis, laneCounts),
		PrimaryAxis:    axis,
		LaneCounts:     laneCounts,
		PriorityVPSIDs: priorityIDs,
		Tradeoffs:      groupComparisonTradeoffs(group, axis, laneCounts),
	}
}

func CompareManualGroup(group ManualGroupDetail) ComparisonInsight {
	laneCounts := manualComparisonLaneCounts(group.Members)
	axis := manualGroupComparisonAxis(group, laneCounts)
	return ComparisonInsight{
		Summary:        manualGroupComparisonSummary(group, axis, laneCounts),
		PrimaryAxis:    axis,
		LaneCounts:     laneCounts,
		PriorityVPSIDs: manualComparisonPriorityVPSIDs(group.Members, 3),
		Tradeoffs:      manualGroupComparisonTradeoffs(group, axis, laneCounts),
	}
}

func CompareMember(member GroupMember, rank int) MemberComparisonInsight {
	lane := memberComparisonLane(member)
	strengths := memberComparisonStrengths(member)
	risks := memberComparisonRisks(member)
	gaps := memberComparisonGaps(member)
	return MemberComparisonInsight{
		Rank:      rank,
		Lane:      lane,
		Summary:   memberComparisonSummary(member, lane, strengths, risks, gaps),
		Strengths: strengths,
		Risks:     risks,
		Gaps:      gaps,
		Tradeoffs: memberComparisonTradeoffs(member, lane),
	}
}

func MissingFactComparison(vpsID string) MemberComparisonInsight {
	return MemberComparisonInsight{
		Rank:    0,
		Lane:    ComparisonLaneEvidence,
		Summary: "当前事实缺失，先核对资产是否仍存在",
		Gaps: []ComparisonSignal{{
			Kind:    string(EvidenceCurrentFactMissing),
			Label:   "当前事实缺失",
			Tone:    "critical",
			Details: strings.TrimSpace(vpsID),
		}},
		Tradeoffs: []ComparisonSignal{{
			Kind:  "review_first",
			Label: "先复核资产事实",
			Tone:  "critical",
		}},
	}
}

func assignMemberComparisonRanks(members []GroupMember) {
	for index := range members {
		members[index].ComparisonInsight = CompareMember(members[index], index+1)
	}
}

func assignManualMemberComparisonRanks(members []ManualGroupMember) {
	for index := range members {
		if members[index].CurrentFactFound {
			members[index].ComparisonInsight = CompareMember(members[index].GroupMember, index+1)
		} else {
			members[index].ComparisonInsight = MissingFactComparison(members[index].VPSID)
			members[index].ComparisonInsight.Rank = index + 1
		}
	}
}

func memberComparisonLane(member GroupMember) ComparisonLane {
	if hasAnyEvidence(member, EvidenceCurrentFactMissing, EvidenceSubscriptionUnavailable) {
		return ComparisonLaneEvidence
	}
	if member.CancellationAttentionReason != "" ||
		hasEvidence(member, EvidenceIdlePaid) ||
		member.SuggestedAction == ActionCancel ||
		member.SuggestedAction == ActionOpenCancellationWorkbench ||
		member.VPS.RenewalDecision == vpsassets.RenewalCancel ||
		member.VPS.RenewalDecision == vpsassets.RenewalAutoRenewCancelled ||
		member.VPS.LifecycleStatus == vpsassets.LifecycleToCancel ||
		member.VPS.LifecycleStatus == vpsassets.LifecycleCancelled {
		return ComparisonLaneRetire
	}
	if member.SuggestedAction == ActionCompleteEvidence || hasAnyEvidence(member, EvidenceMissingSubscription, EvidenceMissingMonitoring, EvidenceMissingProvider, EvidenceMissingLocation, EvidenceMissingAccess, EvidenceNoServiceContext, EvidenceIPQualityMissing, EvidenceIPQualityStale) {
		return ComparisonLaneEvidence
	}
	if hasAnyEvidence(member, EvidenceIPQualityRisk, EvidenceIPEgressMismatch, EvidenceMediaUnlockBlocked) {
		return ComparisonLaneReview
	}
	if member.SuggestedAction == ActionMigrate || member.VPS.RenewalDecision == vpsassets.RenewalMigrate || member.VPS.LifecycleStatus == vpsassets.LifecycleToMigrate {
		return ComparisonLaneObserve
	}
	if member.SuggestedRole == RolePrimaryCandidate || (member.VPS.UsageStatus == vpsassets.UsageInUse && member.ServiceCount+member.DomainCount+member.RunningTargetCount > 0 && member.EvidenceAssessment.QualityTier != EvidenceTierBlocked) {
		return ComparisonLanePrimary
	}
	if member.SuggestedRole == RoleStandbyCandidate || member.VPS.UsageStatus == vpsassets.UsageStandby || member.SuggestedAction == ActionObserve {
		return ComparisonLaneStandby
	}
	if member.SuggestedRole == RoleObserveCandidate {
		return ComparisonLaneObserve
	}
	return ComparisonLaneReview
}

func memberComparisonStrengths(member GroupMember) []ComparisonSignal {
	signals := []ComparisonSignal{}
	if member.ActiveSubscriptionCount > 0 {
		signals = append(signals, ComparisonSignal{Kind: "active_subscription", Label: "订阅有效", Tone: "normal"})
	}
	if member.ServiceCount > 0 || member.DomainCount > 0 || member.RunningTargetCount > 0 {
		signals = append(signals, ComparisonSignal{
			Kind:    "service_context",
			Label:   "承载上下文",
			Tone:    "normal",
			Details: fmt.Sprintf("服务 %d · 域名 %d · Target %d/%d", member.ServiceCount, member.DomainCount, member.RunningTargetCount, member.TargetCount),
		})
	}
	if member.RunningMonitoringCount > 0 || member.MonitoringLinkCount > 0 {
		signals = append(signals, ComparisonSignal{
			Kind:    "monitoring_context",
			Label:   "监控已关联",
			Tone:    "normal",
			Details: fmt.Sprintf("运行 %d · 关联 %d", member.RunningMonitoringCount, member.MonitoringLinkCount),
		})
	}
	if member.EvidenceAssessment.QualityTier == EvidenceTierStrong || member.EvidenceAssessment.QualityTier == EvidenceTierUsable {
		signals = append(signals, ComparisonSignal{
			Kind:    "evidence_quality",
			Label:   "证据可用",
			Tone:    "normal",
			Details: member.EvidenceAssessment.Summary,
		})
	}
	if member.VPS.UsageStatus == vpsassets.UsageInUse {
		signals = append(signals, ComparisonSignal{Kind: "usage_in_use", Label: "承载业务", Tone: "normal"})
	}
	if member.VPS.UsageStatus == vpsassets.UsageStandby {
		signals = append(signals, ComparisonSignal{Kind: "usage_standby", Label: "备用角色", Tone: "notice"})
	}
	return signals
}

func memberComparisonRisks(member GroupMember) []ComparisonSignal {
	signals := []ComparisonSignal{}
	if member.RenewalWithinWindow {
		signals = append(signals, ComparisonSignal{Kind: string(EvidenceRenewalDue), Label: "续费临近", Tone: "alert"})
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalUnreviewed {
		signals = append(signals, ComparisonSignal{Kind: "renewal_unreviewed", Label: "续费未评估", Tone: "alert"})
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalMigrate || member.VPS.LifecycleStatus == vpsassets.LifecycleToMigrate {
		signals = append(signals, ComparisonSignal{Kind: "migration_pending", Label: "迁移信号", Tone: "alert"})
	}
	if member.CancellationAttentionReason != "" {
		signals = append(signals, ComparisonSignal{
			Kind:    string(EvidenceCancellationLinkage),
			Label:   "取消联动",
			Tone:    "critical",
			Details: member.CancellationAttentionReason,
		})
	}
	for _, chip := range member.EvidenceChips {
		switch chip.Kind {
		case EvidenceIdlePaid, EvidenceBudgetRisk, EvidenceExchangeRateStale, EvidenceAbnormalMonitoring, EvidenceIPQualityRisk, EvidenceIPEgressMismatch, EvidenceMediaUnlockBlocked:
			signals = appendUniqueComparisonSignal(signals, ComparisonSignal{
				Kind:    string(chip.Kind),
				Label:   chip.Label,
				Tone:    chip.Tone,
				Details: chip.Details,
			})
		}
	}
	if member.ActiveIncidentCount > 0 {
		signals = appendUniqueComparisonSignal(signals, ComparisonSignal{
			Kind:    "active_incident",
			Label:   "存在 active incident",
			Tone:    "critical",
			Details: fmt.Sprintf("%d 个事件", member.ActiveIncidentCount),
		})
	}
	return signals
}

func memberComparisonGaps(member GroupMember) []ComparisonSignal {
	signals := []ComparisonSignal{}
	for _, chip := range member.EvidenceChips {
		switch chip.Kind {
		case EvidenceMissingSubscription,
			EvidenceMissingMonitoring,
			EvidenceMissingProvider,
			EvidenceMissingLocation,
			EvidenceMissingAccess,
			EvidenceNoServiceContext,
			EvidenceSubscriptionUnavailable,
			EvidenceCurrentFactMissing,
			EvidenceIPQualityMissing,
			EvidenceIPQualityStale:
			signals = appendUniqueComparisonSignal(signals, ComparisonSignal{
				Kind:    string(chip.Kind),
				Label:   chip.Label,
				Tone:    chip.Tone,
				Details: chip.Details,
			})
		}
	}
	if !member.SourceAvailability.Services {
		signals = appendUniqueComparisonSignal(signals, ComparisonSignal{Kind: "services_unavailable", Label: "服务证据不可用", Tone: "notice"})
	}
	if !member.SourceAvailability.Domains {
		signals = appendUniqueComparisonSignal(signals, ComparisonSignal{Kind: "domains_unavailable", Label: "域名证据不可用", Tone: "notice"})
	}
	if !member.SourceAvailability.Monitoring {
		signals = appendUniqueComparisonSignal(signals, ComparisonSignal{Kind: "monitoring_unavailable", Label: "监控证据不可用", Tone: "notice"})
	}
	if !member.SourceAvailability.Targets {
		signals = appendUniqueComparisonSignal(signals, ComparisonSignal{Kind: "targets_unavailable", Label: "Target 证据不可用", Tone: "notice"})
	}
	return signals
}

func memberComparisonTradeoffs(member GroupMember, lane ComparisonLane) []ComparisonSignal {
	signals := []ComparisonSignal{}
	switch lane {
	case ComparisonLanePrimary:
		signals = append(signals, ComparisonSignal{Kind: "protect_carrier", Label: "优先保护承载", Tone: "normal"})
	case ComparisonLaneStandby:
		signals = append(signals, ComparisonSignal{Kind: "standby_value", Label: "保留容灾价值", Tone: "notice"})
	case ComparisonLaneObserve:
		signals = append(signals, ComparisonSignal{Kind: "observe_before_change", Label: "先观察再调整", Tone: "notice"})
	case ComparisonLaneRetire:
		signals = append(signals, ComparisonSignal{Kind: "close_before_retire", Label: "先闭环再退役", Tone: "critical"})
	case ComparisonLaneEvidence:
		signals = append(signals, ComparisonSignal{Kind: "complete_evidence_first", Label: "先补证据", Tone: "alert"})
	default:
		signals = append(signals, ComparisonSignal{Kind: "manual_review", Label: "人工复核", Tone: "notice"})
	}
	if member.PrimarySubscription != nil && member.PrimarySubscription.Status == subscriptions.StatusActive {
		if member.PrimarySubscription.MonthlyPriceBase != nil {
			signals = append(signals, ComparisonSignal{
				Kind:    "monthly_cost_base",
				Label:   "月成本",
				Tone:    "neutral",
				Details: fmt.Sprintf("%.2f %s", *member.PrimarySubscription.MonthlyPriceBase, member.PrimarySubscription.BaseCurrency),
			})
		} else if member.PrimarySubscription.Currency != "" {
			signals = append(signals, ComparisonSignal{
				Kind:    "monthly_cost",
				Label:   "月成本",
				Tone:    "neutral",
				Details: fmt.Sprintf("%.2f %s", member.PrimarySubscription.MonthlyPrice, member.PrimarySubscription.Currency),
			})
		}
	}
	return signals
}

func memberComparisonSummary(member GroupMember, lane ComparisonLane, strengths, risks, gaps []ComparisonSignal) string {
	prefix := strings.TrimSpace(member.VPS.DisplayName)
	if prefix == "" {
		prefix = strings.TrimSpace(member.VPS.VPSID)
	}
	if prefix != "" {
		prefix += "："
	}
	switch lane {
	case ComparisonLanePrimary:
		return prefix + "承载与证据较完整，可作为主力保留候选"
	case ComparisonLaneStandby:
		return prefix + "适合备用或观察，重点核对成本与容灾价值"
	case ComparisonLaneObserve:
		return prefix + "存在迁移或观察信号，先保留复核空间"
	case ComparisonLaneRetire:
		if len(risks) > 0 {
			return prefix + risks[0].Label + "，先核对收尾风险"
		}
		return prefix + "存在退役信号，先核对订阅、Target 和监控闭环"
	case ComparisonLaneEvidence:
		if len(gaps) > 0 {
			return prefix + gaps[0].Label + "，先补齐资料再做取舍"
		}
		return prefix + "证据不足，先补齐资料再做取舍"
	default:
		if len(strengths) == 0 && len(risks) == 0 && len(gaps) == 0 {
			return prefix + "缺少明显差异，需人工复核"
		}
		return prefix + "信号混合，需同组比较后记录判断"
	}
}

func comparisonLaneCounts(members []GroupMember) []ComparisonLaneCount {
	counts := map[ComparisonLane]int{}
	for _, member := range members {
		lane := member.ComparisonInsight.Lane
		if lane == "" {
			lane = memberComparisonLane(member)
		}
		counts[lane]++
	}
	return orderedLaneCounts(counts)
}

func manualComparisonLaneCounts(members []ManualGroupMember) []ComparisonLaneCount {
	counts := map[ComparisonLane]int{}
	for _, member := range members {
		lane := member.ComparisonInsight.Lane
		if lane == "" {
			if member.CurrentFactFound {
				lane = memberComparisonLane(member.GroupMember)
			} else {
				lane = ComparisonLaneEvidence
			}
		}
		counts[lane]++
	}
	return orderedLaneCounts(counts)
}

func orderedLaneCounts(counts map[ComparisonLane]int) []ComparisonLaneCount {
	ordered := []ComparisonLane{
		ComparisonLanePrimary,
		ComparisonLaneStandby,
		ComparisonLaneObserve,
		ComparisonLaneRetire,
		ComparisonLaneEvidence,
		ComparisonLaneReview,
	}
	result := []ComparisonLaneCount{}
	for _, lane := range ordered {
		if count := counts[lane]; count > 0 {
			result = append(result, ComparisonLaneCount{Lane: lane, Count: count})
		}
	}
	return result
}

func groupComparisonAxis(group GroupDetail, laneCounts []ComparisonLaneCount) ComparisonPrimaryAxis {
	switch group.GroupType {
	case GroupCancellationAttention:
		return ComparisonAxisLifecycle
	case GroupCostPressure:
		return ComparisonAxisCost
	case GroupEvidenceGap:
		return ComparisonAxisEvidence
	case GroupRenewalAttention:
		return ComparisonAxisRenewal
	}
	if laneCount(laneCounts, ComparisonLaneEvidence) > 0 {
		return ComparisonAxisEvidence
	}
	if group.ServiceCount+group.DomainCount+group.RunningTargetCount > 0 {
		return ComparisonAxisServiceContext
	}
	if group.AbnormalMonitoringCount > 0 || group.ActiveIncidentCount > 0 || group.MonitoringLinkCount > 0 {
		return ComparisonAxisMonitoring
	}
	if group.MonthlyCostBase != nil || len(group.MonthlyCostByCurrency) > 0 {
		return ComparisonAxisCost
	}
	return ComparisonAxisReview
}

func manualGroupComparisonAxis(group ManualGroupDetail, laneCounts []ComparisonLaneCount) ComparisonPrimaryAxis {
	if laneCount(laneCounts, ComparisonLaneEvidence) > 0 {
		return ComparisonAxisEvidence
	}
	switch group.Scenario {
	case ManualGroupScenarioBudgetReduction:
		return ComparisonAxisCost
	case ManualGroupScenarioMigrationRetirement:
		return ComparisonAxisLifecycle
	case ManualGroupScenarioProviderReview, ManualGroupScenarioRegionReview, ManualGroupScenarioPrimaryStandby:
		if group.ServiceCount+group.DomainCount+group.RunningTargetCount > 0 {
			return ComparisonAxisServiceContext
		}
		if group.AbnormalMonitoringCount > 0 || group.ActiveIncidentCount > 0 || group.MonitoringLinkCount > 0 {
			return ComparisonAxisMonitoring
		}
		if group.MonthlyCostBase != nil || len(group.MonthlyCostByCurrency) > 0 {
			return ComparisonAxisCost
		}
	case ManualGroupScenarioEvidenceCleanup:
		return ComparisonAxisEvidence
	}
	if group.SourceGroupType == GroupCancellationAttention {
		return ComparisonAxisLifecycle
	}
	if group.SourceGroupType == GroupCostPressure {
		return ComparisonAxisCost
	}
	if group.RenewalWindowCount > 0 {
		return ComparisonAxisRenewal
	}
	return ComparisonAxisReview
}

func groupComparisonSummary(group GroupDetail, axis ComparisonPrimaryAxis, laneCounts []ComparisonLaneCount) string {
	if evidence := laneCount(laneCounts, ComparisonLaneEvidence); evidence > 0 {
		return fmt.Sprintf("%d 台需要补齐资料，先修正证据缺口再做组合取舍", evidence)
	}
	if retire := laneCount(laneCounts, ComparisonLaneRetire); retire > 0 {
		return fmt.Sprintf("%d 台存在退役或取消联动信号，优先核对收尾风险", retire)
	}
	primary := laneCount(laneCounts, ComparisonLanePrimary)
	standby := laneCount(laneCounts, ComparisonLaneStandby)
	if primary > 0 && standby > 0 {
		return fmt.Sprintf("已形成 %d 台主力候选和 %d 台备用候选，可比较成本与承载后保存判断", primary, standby)
	}
	switch axis {
	case ComparisonAxisCost:
		return "成本压力是当前主轴，优先比较月成本、承载和闲置付费"
	case ComparisonAxisMonitoring:
		return "监控与事件信号是当前主轴，优先核对异常关联和运行对象"
	case ComparisonAxisServiceContext:
		return "承载上下文是当前主轴，优先区分主力、备用和弱承载资产"
	case ComparisonAxisRenewal:
		return "续费窗口是当前主轴，优先完成保留、观察、迁移或取消判断"
	case ComparisonAxisLifecycle:
		return "生命周期闭环是当前主轴，优先核对取消、订阅、Target 和监控"
	default:
		if group.PrimaryIssueSummary != "" {
			return group.PrimaryIssueSummary
		}
		return "当前组合需要人工复核后记录取舍依据"
	}
}

func manualGroupComparisonSummary(group ManualGroupDetail, axis ComparisonPrimaryAxis, laneCounts []ComparisonLaneCount) string {
	if group.Status == ManualGroupStatusArchived {
		return "组合已归档，保留为历史比较依据"
	}
	if evidence := laneCount(laneCounts, ComparisonLaneEvidence); evidence > 0 {
		return fmt.Sprintf("自定义组合中 %d 台需要补证据或核对当前事实", evidence)
	}
	if retire := laneCount(laneCounts, ComparisonLaneRetire); retire > 0 {
		return fmt.Sprintf("自定义组合中 %d 台是退役/取消候选，保存记录前先确认收尾边界", retire)
	}
	primary := laneCount(laneCounts, ComparisonLanePrimary)
	standby := laneCount(laneCounts, ComparisonLaneStandby)
	if primary > 0 && standby > 0 {
		return fmt.Sprintf("已形成 %d 台主力候选和 %d 台备用候选，可保存为一次组合判断", primary, standby)
	}
	switch axis {
	case ComparisonAxisCost:
		return "当前自定义组合以成本取舍为主，优先比较承载与闲置付费"
	case ComparisonAxisLifecycle:
		return "当前自定义组合以迁移/退役闭环为主，优先确认旧承载和运行对象"
	case ComparisonAxisServiceContext:
		return "当前自定义组合以承载角色为主，优先确认主力、备用和观察分层"
	case ComparisonAxisMonitoring:
		return "当前自定义组合以监控和事件信号为主，优先核对异常关联"
	case ComparisonAxisRenewal:
		return "当前自定义组合包含续费窗口压力，优先确认续费取舍"
	default:
		return "当前自定义组合需要人工复核后再保存判断"
	}
}

func groupComparisonTradeoffs(group GroupDetail, axis ComparisonPrimaryAxis, laneCounts []ComparisonLaneCount) []ComparisonSignal {
	signals := []ComparisonSignal{{
		Kind:  "primary_axis",
		Label: primaryAxisLabel(axis),
		Tone:  "notice",
	}}
	for _, lane := range laneCounts {
		signals = append(signals, ComparisonSignal{
			Kind:    "lane_" + string(lane.Lane),
			Label:   comparisonLaneLabel(lane.Lane),
			Tone:    laneTone(lane.Lane),
			Details: fmt.Sprintf("%d 台 VPS", lane.Count),
		})
	}
	if group.EvidenceAssessment.Summary != "" {
		signals = append(signals, ComparisonSignal{
			Kind:    "evidence_assessment",
			Label:   "判断尺度",
			Tone:    "normal",
			Details: group.EvidenceAssessment.Summary,
		})
	}
	if group.PrimaryIssueSummary != "" {
		signals = append(signals, ComparisonSignal{
			Kind:    "primary_issue",
			Label:   "主要问题",
			Tone:    "alert",
			Details: group.PrimaryIssueSummary,
		})
	}
	return signals
}

func manualGroupComparisonTradeoffs(group ManualGroupDetail, axis ComparisonPrimaryAxis, laneCounts []ComparisonLaneCount) []ComparisonSignal {
	signals := []ComparisonSignal{{
		Kind:    "scenario",
		Label:   scenarioLabel(group.Scenario),
		Tone:    "normal",
		Details: group.Goal,
	}, {
		Kind:  "primary_axis",
		Label: primaryAxisLabel(axis),
		Tone:  "notice",
	}}
	for _, lane := range laneCounts {
		signals = append(signals, ComparisonSignal{
			Kind:    "lane_" + string(lane.Lane),
			Label:   comparisonLaneLabel(lane.Lane),
			Tone:    laneTone(lane.Lane),
			Details: fmt.Sprintf("%d 台 VPS", lane.Count),
		})
	}
	if group.EvidenceAssessment.Summary != "" {
		signals = append(signals, ComparisonSignal{
			Kind:    "evidence_assessment",
			Label:   "判断尺度",
			Tone:    "normal",
			Details: group.EvidenceAssessment.Summary,
		})
	}
	return signals
}

func comparisonPriorityVPSIDs(members []GroupMember, limit int) []string {
	ordered := append([]GroupMember(nil), members...)
	sortComparisonMembers(ordered)
	ids := make([]string, 0, minInt(len(ordered), limit))
	for _, member := range ordered {
		if member.VPS.VPSID == "" {
			continue
		}
		ids = append(ids, member.VPS.VPSID)
		if len(ids) >= limit {
			break
		}
	}
	return ids
}

func manualComparisonPriorityVPSIDs(members []ManualGroupMember, limit int) []string {
	ordered := append([]ManualGroupMember(nil), members...)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftLane := ordered[i].ComparisonInsight.Lane
		rightLane := ordered[j].ComparisonInsight.Lane
		if laneWeight(leftLane) != laneWeight(rightLane) {
			return laneWeight(leftLane) > laneWeight(rightLane)
		}
		if ordered[i].CurrentFactFound != ordered[j].CurrentFactFound {
			return !ordered[i].CurrentFactFound
		}
		if ordered[i].SortOrder != ordered[j].SortOrder {
			return ordered[i].SortOrder < ordered[j].SortOrder
		}
		return ordered[i].VPSID < ordered[j].VPSID
	})
	ids := make([]string, 0, minInt(len(ordered), limit))
	for _, member := range ordered {
		if member.VPSID == "" {
			continue
		}
		ids = append(ids, member.VPSID)
		if len(ids) >= limit {
			break
		}
	}
	return ids
}

func sortComparisonMembers(members []GroupMember) {
	sort.SliceStable(members, func(i, j int) bool {
		leftLane := members[i].ComparisonInsight.Lane
		if leftLane == "" {
			leftLane = memberComparisonLane(members[i])
		}
		rightLane := members[j].ComparisonInsight.Lane
		if rightLane == "" {
			rightLane = memberComparisonLane(members[j])
		}
		if laneWeight(leftLane) != laneWeight(rightLane) {
			return laneWeight(leftLane) > laneWeight(rightLane)
		}
		if memberPriority(members[i]) != memberPriority(members[j]) {
			return memberPriority(members[i]) > memberPriority(members[j])
		}
		return members[i].VPS.DisplayName < members[j].VPS.DisplayName
	})
}

func laneWeight(lane ComparisonLane) int {
	switch lane {
	case ComparisonLaneRetire:
		return 90
	case ComparisonLaneEvidence:
		return 80
	case ComparisonLanePrimary:
		return 70
	case ComparisonLaneObserve:
		return 60
	case ComparisonLaneStandby:
		return 50
	default:
		return 40
	}
}

func laneCount(counts []ComparisonLaneCount, lane ComparisonLane) int {
	for _, count := range counts {
		if count.Lane == lane {
			return count.Count
		}
	}
	return 0
}

func primaryAxisLabel(axis ComparisonPrimaryAxis) string {
	switch axis {
	case ComparisonAxisRenewal:
		return "续费窗口"
	case ComparisonAxisCost:
		return "成本压力"
	case ComparisonAxisServiceContext:
		return "承载上下文"
	case ComparisonAxisMonitoring:
		return "监控与事件"
	case ComparisonAxisEvidence:
		return "资料证据"
	case ComparisonAxisLifecycle:
		return "生命周期闭环"
	default:
		return "人工复核"
	}
}

func comparisonLaneLabel(lane ComparisonLane) string {
	switch lane {
	case ComparisonLanePrimary:
		return "主力候选"
	case ComparisonLaneStandby:
		return "备用候选"
	case ComparisonLaneObserve:
		return "观察/迁移"
	case ComparisonLaneRetire:
		return "退役候选"
	case ComparisonLaneEvidence:
		return "补证据"
	default:
		return "人工复核"
	}
}

func laneTone(lane ComparisonLane) string {
	switch lane {
	case ComparisonLanePrimary:
		return "normal"
	case ComparisonLaneStandby, ComparisonLaneObserve:
		return "notice"
	case ComparisonLaneRetire:
		return "critical"
	case ComparisonLaneEvidence:
		return "alert"
	default:
		return "neutral"
	}
}

func appendUniqueComparisonSignal(signals []ComparisonSignal, signal ComparisonSignal) []ComparisonSignal {
	for _, existing := range signals {
		if existing.Kind == signal.Kind {
			return signals
		}
	}
	return append(signals, signal)
}
