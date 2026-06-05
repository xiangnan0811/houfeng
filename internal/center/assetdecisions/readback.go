package assetdecisions

import (
	"fmt"

	"houfeng/internal/center/vpsassets"
)

type ExecutionReadbackStatus string

const (
	ReadbackOpen          ExecutionReadbackStatus = "open"
	ReadbackAligned       ExecutionReadbackStatus = "aligned"
	ReadbackDrift         ExecutionReadbackStatus = "drift"
	ReadbackBlocked       ExecutionReadbackStatus = "blocked"
	ReadbackNeedsEvidence ExecutionReadbackStatus = "needs_evidence"
	ReadbackInactive      ExecutionReadbackStatus = "inactive"
)

type ExecutionReadbackIssue struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Tone    string `json:"tone"`
	Details string `json:"details,omitempty"`
}

type ExecutionCurrentFacts struct {
	Found                   bool                      `json:"found"`
	LifecycleStatus         vpsassets.LifecycleStatus `json:"lifecycle_status,omitempty"`
	UsageStatus             vpsassets.UsageStatus     `json:"usage_status,omitempty"`
	RenewalDecision         vpsassets.RenewalDecision `json:"renewal_decision,omitempty"`
	ActiveSubscriptionCount int                       `json:"active_subscription_count"`
	ServiceCount            int                       `json:"service_count"`
	DomainCount             int                       `json:"domain_count"`
	TargetCount             int                       `json:"target_count"`
	RunningTargetCount      int                       `json:"running_target_count"`
	MonitoringLinkCount     int                       `json:"monitoring_link_count"`
	RunningMonitoringCount  int                       `json:"running_monitoring_count"`
	AbnormalMonitoringCount int                       `json:"abnormal_monitoring_count"`
	ActiveIncidentCount     int                       `json:"active_incident_count"`
	SourceAvailability      SourceAvailability        `json:"source_availability"`
}

type MemberExecutionReadback struct {
	Status       ExecutionReadbackStatus  `json:"status"`
	Summary      string                   `json:"summary"`
	Issues       []ExecutionReadbackIssue `json:"issues"`
	CurrentFacts ExecutionCurrentFacts    `json:"current_facts"`
}

type RecordExecutionReadback struct {
	Status             ExecutionReadbackStatus `json:"status"`
	Summary            string                  `json:"summary"`
	OpenCount          int                     `json:"open_count"`
	AlignedCount       int                     `json:"aligned_count"`
	DriftCount         int                     `json:"drift_count"`
	BlockedCount       int                     `json:"blocked_count"`
	NeedsEvidenceCount int                     `json:"needs_evidence_count"`
}

func ApplyExecutionReadback(detail RecordDetail, facts []Fact) RecordDetail {
	factMap := FactsByVPSID(facts)
	for index := range detail.Members {
		detail.Members[index].ExecutionReadback = EvaluateMemberExecutionReadback(detail.Members[index], factMap)
	}
	detail.RecordSummary.ExecutionReadback = EvaluateRecordExecutionReadback(detail.Status, detail.Members)
	return detail
}

func ApplyExecutionReadbackToSummaries(records []RecordSummary, membersByRecord map[string][]RecordMember, facts []Fact) []RecordSummary {
	factMap := FactsByVPSID(facts)
	withReadback := make([]RecordSummary, 0, len(records))
	for _, record := range records {
		members := append([]RecordMember(nil), membersByRecord[record.RecordID]...)
		for index := range members {
			members[index].ExecutionReadback = EvaluateMemberExecutionReadback(members[index], factMap)
		}
		record.ExecutionReadback = EvaluateRecordExecutionReadback(record.Status, members)
		withReadback = append(withReadback, record)
	}
	return withReadback
}

func FactsByVPSID(facts []Fact) map[string]Fact {
	byVPS := make(map[string]Fact, len(facts))
	for _, fact := range facts {
		byVPS[fact.VPS.VPSID] = fact
	}
	return byVPS
}

func EvaluateRecordExecutionReadback(status RecordStatus, members []RecordMember) RecordExecutionReadback {
	readback := RecordExecutionReadback{Status: ReadbackOpen}
	if status == RecordStatusAbandoned {
		readback.Status = ReadbackInactive
		readback.Summary = "记录已放弃，不参与执行回读"
		return readback
	}
	if len(members) == 0 {
		readback.Status = ReadbackAligned
		readback.Summary = "暂无待回读成员"
		return readback
	}

	allClosed := true
	for _, member := range members {
		switch member.ExecutionReadback.Status {
		case ReadbackDrift:
			readback.DriftCount++
			allClosed = false
		case ReadbackBlocked:
			readback.BlockedCount++
			allClosed = false
		case ReadbackNeedsEvidence:
			readback.NeedsEvidenceCount++
			allClosed = false
		case ReadbackAligned, ReadbackInactive:
			readback.AlignedCount++
		default:
			readback.OpenCount++
			allClosed = false
		}
	}

	switch {
	case readback.DriftCount > 0:
		readback.Status = ReadbackDrift
		readback.Summary = fmt.Sprintf("%d 台 VPS 与当前事实不一致", readback.DriftCount)
	case readback.BlockedCount > 0:
		readback.Status = ReadbackBlocked
		readback.Summary = fmt.Sprintf("%d 台 VPS 跟进阻塞", readback.BlockedCount)
	case readback.NeedsEvidenceCount > 0:
		readback.Status = ReadbackNeedsEvidence
		readback.Summary = fmt.Sprintf("%d 台 VPS 仍需补证据", readback.NeedsEvidenceCount)
	case allClosed:
		readback.Status = ReadbackAligned
		readback.Summary = "当前事实与组合判断一致"
	default:
		readback.Status = ReadbackOpen
		readback.Summary = fmt.Sprintf("%d 台 VPS 仍待执行或复核", readback.OpenCount)
	}
	return readback
}

func EvaluateMemberExecutionReadback(member RecordMember, factsByVPS map[string]Fact) MemberExecutionReadback {
	fact, ok := factsByVPS[member.VPSID]
	if !ok {
		return MemberExecutionReadback{
			Status:  ReadbackDrift,
			Summary: "当前 VPS 事实缺失，需人工核对",
			Issues: []ExecutionReadbackIssue{{
				Kind:    "current_fact_missing",
				Label:   "当前事实缺失",
				Tone:    "critical",
				Details: "记录成员仍存在，但当前资产聚合事实中找不到对应 VPS",
			}},
			CurrentFacts: ExecutionCurrentFacts{Found: false},
		}
	}

	issues := executionIssuesForAction(memberAction(member), fact)
	critical := hasCriticalExecutionIssue(issues)
	facts := currentFactsFromFact(fact)
	status, summary := memberReadbackStatus(member, issues, critical)
	return MemberExecutionReadback{
		Status:       status,
		Summary:      summary,
		Issues:       issues,
		CurrentFacts: facts,
	}
}

func memberReadbackStatus(member RecordMember, issues []ExecutionReadbackIssue, critical bool) (ExecutionReadbackStatus, string) {
	if critical && member.FollowupStatus == FollowupDone {
		return ReadbackDrift, "跟进已完成，但当前事实仍未闭环"
	}
	if critical && member.FollowupStatus == FollowupSkipped {
		return ReadbackDrift, "已跳过跟进，但关键事实仍未闭环"
	}
	if member.FollowupStatus == FollowupBlocked {
		return ReadbackBlocked, "成员跟进阻塞"
	}
	if critical {
		return ReadbackOpen, "当前事实仍需处理"
	}
	if hasIssueKind(issues, "evidence_gap") {
		if member.FollowupStatus == FollowupDone {
			return ReadbackDrift, "跟进已完成，但证据缺口仍存在"
		}
		return ReadbackNeedsEvidence, "仍需补齐证据"
	}
	if len(issues) > 0 {
		if member.FollowupStatus == FollowupDone {
			return ReadbackDrift, "跟进已完成，但当前事实不匹配"
		}
		if member.FollowupStatus == FollowupSkipped {
			return ReadbackAligned, "已跳过普通跟进，未发现关键割裂"
		}
		return ReadbackOpen, "当前事实仍待执行或复核"
	}
	if member.FollowupStatus == FollowupSkipped {
		return ReadbackAligned, "已跳过，未发现当前事实冲突"
	}
	if member.FollowupStatus == FollowupDone {
		return ReadbackAligned, "跟进完成，当前事实一致"
	}
	return ReadbackAligned, "当前事实与判断一致"
}

func executionIssuesForAction(action SuggestedAction, fact Fact) []ExecutionReadbackIssue {
	issues := []ExecutionReadbackIssue{}
	switch action {
	case ActionCancel, ActionOpenCancellationWorkbench:
		if !isCancellationLifecycle(fact.VPS.LifecycleStatus) {
			issues = append(issues, ExecutionReadbackIssue{Kind: "cancel_lifecycle_open", Label: "未进入取消链路", Tone: "critical", Details: "VPS lifecycle 尚未变为待取消、已取消或已归档"})
		}
		if fact.ActiveSubscriptionCount > 0 {
			issues = append(issues, ExecutionReadbackIssue{Kind: "active_subscription_remaining", Label: "仍有 active 订阅", Tone: "critical", Details: fmt.Sprintf("active subscription: %d", fact.ActiveSubscriptionCount)})
		}
		if fact.RunningMonitoringCount > 0 {
			issues = append(issues, ExecutionReadbackIssue{Kind: "running_monitoring_remaining", Label: "仍有关联监控运行", Tone: "critical", Details: fmt.Sprintf("running monitoring: %d", fact.RunningMonitoringCount)})
		}
		if fact.RunningTargetCount > 0 {
			issues = append(issues, ExecutionReadbackIssue{Kind: "running_target_remaining", Label: "仍有关联 Target 运行", Tone: "critical", Details: fmt.Sprintf("running target: %d", fact.RunningTargetCount)})
		}
	case ActionMigrate:
		if fact.VPS.RenewalDecision != vpsassets.RenewalMigrate && fact.VPS.RenewalDecision != vpsassets.RenewalReplaced && fact.VPS.LifecycleStatus != vpsassets.LifecycleToMigrate {
			issues = append(issues, ExecutionReadbackIssue{Kind: "migration_not_started", Label: "未进入迁移链路", Tone: "alert", Details: "续费决策或 lifecycle 尚未体现迁移/替换"})
		}
		if fact.ServiceCount+fact.DomainCount+fact.RunningTargetCount > 0 {
			issues = append(issues, ExecutionReadbackIssue{Kind: "old_carrier_remaining", Label: "旧 VPS 仍有承载", Tone: "critical", Details: fmt.Sprintf("service %d / domain %d / running target %d", fact.ServiceCount, fact.DomainCount, fact.RunningTargetCount)})
		}
	case ActionKeep:
		if isTerminalLifecycle(fact.VPS.LifecycleStatus) {
			issues = append(issues, ExecutionReadbackIssue{Kind: "keep_lifecycle_conflict", Label: "保留判断与 lifecycle 冲突", Tone: "critical", Details: string(fact.VPS.LifecycleStatus)})
		}
		if fact.VPS.RenewalDecision != vpsassets.RenewalKeep {
			issues = append(issues, ExecutionReadbackIssue{Kind: "keep_decision_missing", Label: "续费决策未保留", Tone: "alert", Details: string(fact.VPS.RenewalDecision)})
		}
	case ActionObserve:
		if isTerminalLifecycle(fact.VPS.LifecycleStatus) {
			issues = append(issues, ExecutionReadbackIssue{Kind: "observe_lifecycle_conflict", Label: "观察判断与 lifecycle 冲突", Tone: "critical", Details: string(fact.VPS.LifecycleStatus)})
		}
		if fact.VPS.RenewalDecision != vpsassets.RenewalObserve {
			issues = append(issues, ExecutionReadbackIssue{Kind: "observe_decision_missing", Label: "续费决策未观察", Tone: "notice", Details: string(fact.VPS.RenewalDecision)})
		}
	case ActionCompleteEvidence:
		if gaps := executionEvidenceGaps(fact); len(gaps) > 0 {
			issues = append(issues, gaps...)
		}
	case ActionReview, "":
		if reason := cancellationReason(fact); reason != "" {
			issues = append(issues, ExecutionReadbackIssue{Kind: "status_split", Label: "状态割裂", Tone: "critical", Details: reason})
		}
	}
	return issues
}

func executionEvidenceGaps(fact Fact) []ExecutionReadbackIssue {
	member := buildMember(fact, ListFilters{RenewWithinDays: 30})
	issues := []ExecutionReadbackIssue{}
	for _, chip := range member.EvidenceChips {
		switch chip.Kind {
		case EvidenceMissingSubscription, EvidenceMissingMonitoring, EvidenceMissingProvider, EvidenceMissingLocation, EvidenceMissingAccess, EvidenceNoServiceContext, EvidenceSubscriptionUnavailable:
			issues = append(issues, ExecutionReadbackIssue{Kind: "evidence_gap", Label: chip.Label, Tone: chip.Tone, Details: chip.Details})
		}
	}
	return issues
}

func currentFactsFromFact(fact Fact) ExecutionCurrentFacts {
	return ExecutionCurrentFacts{
		Found:                   true,
		LifecycleStatus:         fact.VPS.LifecycleStatus,
		UsageStatus:             fact.VPS.UsageStatus,
		RenewalDecision:         fact.VPS.RenewalDecision,
		ActiveSubscriptionCount: fact.ActiveSubscriptionCount,
		ServiceCount:            fact.ServiceCount,
		DomainCount:             fact.DomainCount,
		TargetCount:             fact.TargetCount,
		RunningTargetCount:      fact.RunningTargetCount,
		MonitoringLinkCount:     fact.MonitoringLinkCount,
		RunningMonitoringCount:  fact.RunningMonitoringCount,
		AbnormalMonitoringCount: fact.AbnormalMonitoringCount,
		ActiveIncidentCount:     fact.ActiveIncidentCount,
		SourceAvailability:      fact.SourceAvailability,
	}
}

func memberAction(member RecordMember) SuggestedAction {
	if member.DecidedAction != "" {
		return member.DecidedAction
	}
	return member.SuggestedAction
}

func isCancellationLifecycle(status vpsassets.LifecycleStatus) bool {
	switch status {
	case vpsassets.LifecycleToCancel, vpsassets.LifecycleCancelled, vpsassets.LifecycleArchived:
		return true
	default:
		return false
	}
}

func isTerminalLifecycle(status vpsassets.LifecycleStatus) bool {
	switch status {
	case vpsassets.LifecycleToCancel, vpsassets.LifecycleCancelled, vpsassets.LifecycleArchived:
		return true
	default:
		return false
	}
}

func hasCriticalExecutionIssue(issues []ExecutionReadbackIssue) bool {
	for _, issue := range issues {
		if issue.Tone == "critical" {
			return true
		}
	}
	return false
}

func hasIssueKind(issues []ExecutionReadbackIssue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}
