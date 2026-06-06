package assetdecisions

import (
	"fmt"
	"sort"
)

type DecisionRecommendationReason struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Tone    string `json:"tone"`
	Details string `json:"details,omitempty"`
}

type DecisionRecommendation struct {
	Summary         string                         `json:"summary"`
	NextStep        string                         `json:"next_step"`
	Reasons         []DecisionRecommendationReason `json:"reasons"`
	Blockers        []DecisionRecommendationReason `json:"blockers"`
	PriorityVPSIDs  []string                       `json:"priority_vps_ids"`
	ConfidenceLabel string                         `json:"confidence_label"`
}

func RecommendMember(member GroupMember) DecisionRecommendation {
	assessment := member.EvidenceAssessment
	recommendation := DecisionRecommendation{
		Summary:         memberRecommendationSummary(member),
		NextStep:        memberRecommendationNextStep(member),
		ConfidenceLabel: confidenceLabel(assessment.QualityTier),
	}
	if member.VPS.VPSID != "" {
		recommendation.PriorityVPSIDs = []string{member.VPS.VPSID}
	}
	recommendation.Reasons = recommendationReasonsFromChips(member.EvidenceChips, 4)
	recommendation.Blockers = recommendationBlockers(member.EvidenceChips, assessment)
	if len(recommendation.Reasons) == 0 {
		recommendation.Reasons = append(recommendation.Reasons, assessmentReason(assessment))
	}
	return recommendation
}

func RecommendGroup(group GroupDetail) DecisionRecommendation {
	assessment := group.EvidenceAssessment
	recommendation := DecisionRecommendation{
		Summary:         groupRecommendationSummary(group),
		NextStep:        groupRecommendationNextStep(group),
		Reasons:         recommendationReasonsFromChips(group.EvidenceChips, 5),
		Blockers:        recommendationBlockers(group.EvidenceChips, assessment),
		PriorityVPSIDs:  priorityVPSIDs(group.Members, 3),
		ConfidenceLabel: confidenceLabel(assessment.QualityTier),
	}
	if len(recommendation.Reasons) == 0 {
		recommendation.Reasons = append(recommendation.Reasons, assessmentReason(assessment))
	}
	if group.MemberCount > 0 {
		recommendation.Reasons = append(recommendation.Reasons, DecisionRecommendationReason{
			Kind:    "portfolio_size",
			Label:   fmt.Sprintf("%d 台 VPS 参与比较", group.MemberCount),
			Tone:    "normal",
			Details: group.ScopeLabel,
		})
	}
	return recommendation
}

func RecommendManualGroup(group ManualGroupDetail) DecisionRecommendation {
	detail := ManualGroupAsGroupDetail(group)
	recommendation := RecommendGroup(detail)
	if group.Scenario != "" {
		recommendation.Reasons = append([]DecisionRecommendationReason{{
			Kind:    "scenario",
			Label:   scenarioLabel(group.Scenario),
			Tone:    "normal",
			Details: group.Goal,
		}}, recommendation.Reasons...)
	}
	if group.Status == ManualGroupStatusArchived {
		recommendation.Blockers = append(recommendation.Blockers, DecisionRecommendationReason{
			Kind:  "manual_group_archived",
			Label: "组合已归档",
			Tone:  "notice",
		})
	}
	return recommendation
}

func MissingFactRecommendation(vpsID string) DecisionRecommendation {
	return DecisionRecommendation{
		Summary:         "当前事实缺失，先核对资产是否仍存在",
		NextStep:        "回到 VPS 库存或数据导入链路补齐当前事实",
		ConfidenceLabel: confidenceLabel(EvidenceTierBlocked),
		PriorityVPSIDs:  []string{vpsID},
		Blockers: []DecisionRecommendationReason{{
			Kind:  string(EvidenceCurrentFactMissing),
			Label: "当前事实缺失",
			Tone:  "critical",
		}},
	}
}

func memberRecommendationSummary(member GroupMember) string {
	switch member.EvidenceAssessment.DecisionBias {
	case EvidenceBiasCompleteEvidence:
		return "证据不足，先补齐资料再做取舍"
	case EvidenceBiasRetire:
		return "存在退役或取消信号，优先核对收尾风险"
	case EvidenceBiasMigrate:
		return "存在迁移信号，确认旧承载清理计划"
	case EvidenceBiasKeep:
		return "证据较完整，可作为保留候选"
	case EvidenceBiasObserve:
		return "适合观察或备用，不宜立即退役"
	default:
		if member.CancellationAttentionReason != "" {
			return "状态割裂，需要先处理取消联动"
		}
		return "需要人工复核后再保存判断"
	}
}

func memberRecommendationNextStep(member GroupMember) string {
	switch member.SuggestedAction {
	case ActionKeep:
		return "确认该 VPS 是否继续承担主力或保留角色"
	case ActionObserve:
		return "设为观察或备用候选，并补齐后续复核备注"
	case ActionMigrate:
		return "进入迁移计划核对，确认旧服务和 Target 清理"
	case ActionCancel:
		return "确认闲置成本和承载对象后进入取消链路"
	case ActionOpenCancellationWorkbench:
		return "打开 VPS 详情生命周期工作台核对取消影响"
	case ActionCompleteEvidence:
		return "补齐订阅、监控、服务或基础资料证据"
	default:
		return "和同组成员一起比较后记录组合判断"
	}
}

func groupRecommendationSummary(group GroupDetail) string {
	switch group.EvidenceAssessment.DecisionBias {
	case EvidenceBiasCompleteEvidence:
		return "组合证据不足，先补齐资料再决策"
	case EvidenceBiasRetire:
		return "组合存在退役/取消联动风险，优先核对收尾"
	case EvidenceBiasMigrate:
		return "组合存在迁移信号，优先确认替换与清理计划"
	case EvidenceBiasKeep:
		return "组合证据较完整，可保存保留判断"
	case EvidenceBiasObserve:
		return "组合适合观察或主备分层，不宜直接清退"
	default:
		switch group.GroupType {
		case GroupRegionPortfolio:
			return "同区多台 VPS 需要比较保留优先级"
		case GroupProviderPortfolio:
			return "同服务商多台 VPS 需要比较组合质量"
		case GroupCostPressure:
			return "成本压力较高，优先识别弱承载资产"
		case GroupRenewalAttention:
			return "续费窗口临近，需要完成保留/迁移/取消判断"
		default:
			return "需要人工复核后保存组合判断"
		}
	}
}

func groupRecommendationNextStep(group GroupDetail) string {
	switch group.GroupType {
	case GroupRenewalAttention:
		return "先处理续费窗口内的未评估、迁移和取消候选"
	case GroupCancellationAttention:
		return "逐台打开取消工作台，核对订阅、Target 和监控是否闭环"
	case GroupRegionPortfolio:
		return "比较同区成本、承载、备用关系，选出主力与备用"
	case GroupProviderPortfolio:
		return "比较同服务商的成本、服务承载和异常信号"
	case GroupCostPressure:
		return "优先处理闲置付费、预算风险和弱承载 VPS"
	case GroupEvidenceGap:
		return "补齐订阅、监控、服务上下文和基础资料"
	default:
		return "保存为自定义组合后细化成员意图"
	}
}

func recommendationReasonsFromChips(chips []EvidenceChip, limit int) []DecisionRecommendationReason {
	reasons := make([]DecisionRecommendationReason, 0, minInt(len(chips), limit))
	for _, chip := range chips {
		reasons = append(reasons, DecisionRecommendationReason{
			Kind:    string(chip.Kind),
			Label:   chip.Label,
			Tone:    chip.Tone,
			Details: chip.Details,
		})
		if len(reasons) >= limit {
			break
		}
	}
	return reasons
}

func recommendationBlockers(chips []EvidenceChip, assessment EvidenceAssessment) []DecisionRecommendationReason {
	blockers := []DecisionRecommendationReason{}
	if assessment.QualityTier == EvidenceTierBlocked {
		blockers = append(blockers, DecisionRecommendationReason{
			Kind:    "evidence_blocked",
			Label:   "证据阻塞",
			Tone:    "critical",
			Details: assessment.Summary,
		})
	}
	for _, chip := range chips {
		if chip.Tone == "critical" || chip.Kind == EvidenceSubscriptionUnavailable || chip.Kind == EvidenceCurrentFactMissing {
			blockers = append(blockers, DecisionRecommendationReason{
				Kind:    string(chip.Kind),
				Label:   chip.Label,
				Tone:    chip.Tone,
				Details: chip.Details,
			})
		}
	}
	return blockers
}

func assessmentReason(assessment EvidenceAssessment) DecisionRecommendationReason {
	return DecisionRecommendationReason{
		Kind: "evidence_assessment",
		Label: fmt.Sprintf("可信 %d / 压力 %d / 准备 %d",
			assessment.ConfidenceScore,
			assessment.PressureScore,
			assessment.ReadinessScore,
		),
		Tone:    "normal",
		Details: assessment.Summary,
	}
}

func priorityVPSIDs(members []GroupMember, limit int) []string {
	ordered := append([]GroupMember(nil), members...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return memberPriority(ordered[i]) > memberPriority(ordered[j])
	})
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

func confidenceLabel(tier EvidenceQualityTier) string {
	switch tier {
	case EvidenceTierStrong:
		return "高可信"
	case EvidenceTierUsable:
		return "可用"
	case EvidenceTierWeak:
		return "偏弱"
	case EvidenceTierBlocked:
		return "阻塞"
	default:
		return "待判断"
	}
}

func scenarioLabel(scenario ManualGroupScenario) string {
	switch scenario {
	case ManualGroupScenarioPrimaryStandby:
		return "主备组合"
	case ManualGroupScenarioBudgetReduction:
		return "预算压缩"
	case ManualGroupScenarioProviderReview:
		return "服务商复核"
	case ManualGroupScenarioRegionReview:
		return "同区取舍"
	case ManualGroupScenarioMigrationRetirement:
		return "迁移退役"
	case ManualGroupScenarioEvidenceCleanup:
		return "资料补齐"
	default:
		return "通用判断"
	}
}
