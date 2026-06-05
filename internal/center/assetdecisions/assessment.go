package assetdecisions

import (
	"fmt"

	"houfeng/internal/center/vpsassets"
)

type EvidenceQualityTier string

const (
	EvidenceTierStrong  EvidenceQualityTier = "strong"
	EvidenceTierUsable  EvidenceQualityTier = "usable"
	EvidenceTierWeak    EvidenceQualityTier = "weak"
	EvidenceTierBlocked EvidenceQualityTier = "blocked"
)

type EvidenceDecisionBias string

const (
	EvidenceBiasKeep             EvidenceDecisionBias = "keep"
	EvidenceBiasObserve          EvidenceDecisionBias = "observe"
	EvidenceBiasCompleteEvidence EvidenceDecisionBias = "complete_evidence"
	EvidenceBiasRetire           EvidenceDecisionBias = "retire"
	EvidenceBiasMigrate          EvidenceDecisionBias = "migrate"
	EvidenceBiasReview           EvidenceDecisionBias = "review"
)

type EvidenceAssessment struct {
	ConfidenceScore    int                  `json:"confidence_score"`
	PressureScore      int                  `json:"pressure_score"`
	ReadinessScore     int                  `json:"readiness_score"`
	QualityTier        EvidenceQualityTier  `json:"quality_tier"`
	DecisionBias       EvidenceDecisionBias `json:"decision_bias"`
	SupportSignalCount int                  `json:"support_signal_count"`
	RiskSignalCount    int                  `json:"risk_signal_count"`
	GapSignalCount     int                  `json:"gap_signal_count"`
	Summary            string               `json:"summary"`
}

func assessMember(member GroupMember) EvidenceAssessment {
	supportSignals := 0
	riskSignals := 0
	gapSignals := 0
	confidence := 50
	pressure := 0

	if member.ActiveSubscriptionCount > 0 {
		supportSignals++
		confidence += 10
	}
	if member.ServiceCount+member.DomainCount+member.RunningTargetCount > 0 {
		supportSignals++
		confidence += 12
	}
	if member.RunningMonitoringCount > 0 {
		supportSignals++
		confidence += 10
	}
	if hasEvidence(member, EvidenceCarriesService) {
		supportSignals++
		confidence += 6
	}
	if isOrdinaryLifecycle(member.VPS.LifecycleStatus) {
		supportSignals++
		confidence += 4
	}

	if member.RenewalWithinWindow {
		riskSignals++
		pressure += 20
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalUnreviewed {
		riskSignals++
		pressure += 12
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalMigrate || member.VPS.LifecycleStatus == vpsassets.LifecycleToMigrate {
		riskSignals++
		pressure += 22
	}
	if member.VPS.RenewalDecision == vpsassets.RenewalCancel || member.VPS.RenewalDecision == vpsassets.RenewalAutoRenewCancelled || member.VPS.LifecycleStatus == vpsassets.LifecycleToCancel || member.VPS.LifecycleStatus == vpsassets.LifecycleCancelled {
		riskSignals++
		pressure += 34
	}
	if member.CancellationAttentionReason != "" {
		riskSignals += 2
		pressure += 42
	}
	if hasEvidence(member, EvidenceIdlePaid) {
		riskSignals++
		pressure += 24
	}
	if hasEvidence(member, EvidenceBudgetRisk) {
		riskSignals++
		pressure += 24
	}
	if hasEvidence(member, EvidenceExchangeRateStale) {
		riskSignals++
		pressure += 8
	}
	if hasEvidence(member, EvidenceAbnormalMonitoring) {
		riskSignals++
		pressure += 28 + minInt(member.ActiveIncidentCount*4, 16)
	}

	gapPenalty := 0
	for _, item := range []struct {
		kind       EvidenceKind
		confidence int
	}{
		{EvidenceMissingSubscription, 22},
		{EvidenceMissingMonitoring, 10},
		{EvidenceNoServiceContext, 8},
		{EvidenceMissingProvider, 7},
		{EvidenceMissingLocation, 7},
		{EvidenceMissingAccess, 8},
	} {
		if hasEvidence(member, item.kind) {
			gapSignals++
			gapPenalty += item.confidence
		}
	}

	unavailableSources := unavailableSourceCount(member.SourceAvailability)
	if unavailableSources > 0 {
		gapSignals += unavailableSources
		gapPenalty += unavailableSources * 6
	}
	confidence -= gapPenalty

	confidence = clampScore(confidence)
	pressure = clampScore(pressure)
	readiness := confidence + minInt(pressure/4, 18)
	if gapSignals > 0 {
		readiness -= gapSignals * 8
	}
	if member.CancellationAttentionReason != "" && gapSignals == 0 {
		readiness += 8
	}
	readiness = clampScore(readiness)

	tier := classifyEvidenceTier(confidence, readiness, gapSignals, unavailableSources, false)
	bias := memberDecisionBias(member, confidence, pressure, gapSignals)
	return EvidenceAssessment{
		ConfidenceScore:    confidence,
		PressureScore:      pressure,
		ReadinessScore:     readiness,
		QualityTier:        tier,
		DecisionBias:       bias,
		SupportSignalCount: supportSignals,
		RiskSignalCount:    riskSignals,
		GapSignalCount:     gapSignals,
		Summary:            evidenceSummary(tier, bias, supportSignals, riskSignals, gapSignals),
	}
}

func assessGroup(groupType GroupType, priority int, members []GroupMember) EvidenceAssessment {
	if len(members) == 0 {
		return EvidenceAssessment{
			ConfidenceScore: 0,
			PressureScore:   0,
			ReadinessScore:  0,
			QualityTier:     EvidenceTierWeak,
			DecisionBias:    EvidenceBiasReview,
			Summary:         "暂无成员证据",
		}
	}

	confidenceTotal := 0
	pressureTotal := 0
	readinessTotal := 0
	maxPressure := 0
	supportSignals := 0
	riskSignals := 0
	gapSignals := 0
	blockedMembers := 0
	weakMembers := 0
	biasCounts := map[EvidenceDecisionBias]int{}
	for _, member := range members {
		assessment := member.EvidenceAssessment
		confidenceTotal += assessment.ConfidenceScore
		pressureTotal += assessment.PressureScore
		readinessTotal += assessment.ReadinessScore
		maxPressure = maxInt(maxPressure, assessment.PressureScore)
		supportSignals += assessment.SupportSignalCount
		riskSignals += assessment.RiskSignalCount
		gapSignals += assessment.GapSignalCount
		biasCounts[assessment.DecisionBias]++
		if assessment.QualityTier == EvidenceTierBlocked {
			blockedMembers++
		}
		if assessment.QualityTier == EvidenceTierWeak {
			weakMembers++
		}
	}

	confidence := confidenceTotal / len(members)
	readiness := readinessTotal / len(members)
	pressure := (pressureTotal/len(members) + maxPressure) / 2
	pressure += groupPressureBoost(groupType, priority)
	confidence -= blockedMembers*6 + weakMembers*3
	readiness -= blockedMembers*8 + weakMembers*4

	confidence = clampScore(confidence)
	pressure = clampScore(pressure)
	readiness = clampScore(readiness)
	tier := classifyEvidenceTier(confidence, readiness, gapSignals, blockedMembers, blockedMembers == len(members))
	bias := groupDecisionBias(groupType, confidence, pressure, gapSignals, biasCounts)
	return EvidenceAssessment{
		ConfidenceScore:    confidence,
		PressureScore:      pressure,
		ReadinessScore:     readiness,
		QualityTier:        tier,
		DecisionBias:       bias,
		SupportSignalCount: supportSignals,
		RiskSignalCount:    riskSignals,
		GapSignalCount:     gapSignals,
		Summary:            evidenceSummary(tier, bias, supportSignals, riskSignals, gapSignals),
	}
}

func memberDecisionBias(member GroupMember, confidence, pressure, gapSignals int) EvidenceDecisionBias {
	switch member.SuggestedAction {
	case ActionKeep:
		if confidence >= 70 && gapSignals == 0 {
			return EvidenceBiasKeep
		}
	case ActionMigrate:
		return EvidenceBiasMigrate
	case ActionCancel, ActionOpenCancellationWorkbench:
		return EvidenceBiasRetire
	case ActionCompleteEvidence:
		return EvidenceBiasCompleteEvidence
	case ActionObserve:
		return EvidenceBiasObserve
	}
	if gapSignals > 0 && confidence < 60 {
		return EvidenceBiasCompleteEvidence
	}
	if pressure >= 70 {
		return EvidenceBiasReview
	}
	if member.VPS.UsageStatus == vpsassets.UsageStandby {
		return EvidenceBiasObserve
	}
	return EvidenceBiasReview
}

func groupDecisionBias(groupType GroupType, confidence, pressure, gapSignals int, biasCounts map[EvidenceDecisionBias]int) EvidenceDecisionBias {
	if groupType == GroupEvidenceGap || (gapSignals > 0 && confidence < 58 && gapSignals >= biasCounts[EvidenceBiasRetire]) {
		return EvidenceBiasCompleteEvidence
	}
	if groupType == GroupCancellationAttention || (biasCounts[EvidenceBiasRetire] > 0 && pressure >= 60) {
		return EvidenceBiasRetire
	}
	if biasCounts[EvidenceBiasMigrate] > 0 && pressure >= 50 {
		return EvidenceBiasMigrate
	}
	if biasCounts[EvidenceBiasKeep] > 0 && confidence >= 74 && pressure < 55 && gapSignals == 0 {
		return EvidenceBiasKeep
	}
	if groupType == GroupCostPressure || pressure >= 68 {
		return EvidenceBiasReview
	}
	if biasCounts[EvidenceBiasObserve] > 0 {
		return EvidenceBiasObserve
	}
	return EvidenceBiasReview
}

func classifyEvidenceTier(confidence, readiness, gapSignals, unavailableOrBlocked int, allBlocked bool) EvidenceQualityTier {
	if allBlocked || confidence < 35 || (gapSignals >= 5 && readiness < 45) || unavailableOrBlocked >= 3 {
		return EvidenceTierBlocked
	}
	if confidence >= 78 && readiness >= 70 && gapSignals == 0 {
		return EvidenceTierStrong
	}
	if confidence >= 58 && readiness >= 50 {
		return EvidenceTierUsable
	}
	return EvidenceTierWeak
}

func evidenceSummary(tier EvidenceQualityTier, bias EvidenceDecisionBias, supportSignals, riskSignals, gapSignals int) string {
	if tier == EvidenceTierBlocked {
		return fmt.Sprintf("证据阻塞：%d 项缺口，先补齐资料", gapSignals)
	}
	if gapSignals > 0 && (bias == EvidenceBiasCompleteEvidence || tier == EvidenceTierWeak) {
		return fmt.Sprintf("证据偏弱：%d 项缺口，先补证据", gapSignals)
	}
	if riskSignals > supportSignals && riskSignals > 0 {
		return fmt.Sprintf("压力较高：%d 项风险，优先复核", riskSignals)
	}
	if tier == EvidenceTierStrong {
		return "证据完整：可保存组合判断"
	}
	if bias == EvidenceBiasKeep {
		return "证据可用：偏向保留"
	}
	if bias == EvidenceBiasObserve {
		return "证据可用：继续观察"
	}
	return "证据可用：复核后决策"
}

func groupPressureBoost(groupType GroupType, priority int) int {
	boost := minInt(priority/10, 8)
	switch groupType {
	case GroupCancellationAttention:
		return boost + 20
	case GroupCostPressure:
		return boost + 18
	case GroupRenewalAttention:
		return boost + 12
	case GroupEvidenceGap:
		return boost + 8
	default:
		return boost
	}
}

func unavailableSourceCount(source SourceAvailability) int {
	count := 0
	if !source.Subscriptions {
		count++
	}
	if !source.Services {
		count++
	}
	if !source.Domains {
		count++
	}
	if !source.Monitoring {
		count++
	}
	if !source.Targets {
		count++
	}
	return count
}

func isOrdinaryLifecycle(status vpsassets.LifecycleStatus) bool {
	switch status {
	case vpsassets.LifecycleActive, vpsassets.LifecycleIdle, vpsassets.LifecycleTesting, vpsassets.LifecycleToMigrate:
		return true
	default:
		return false
	}
}

func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
