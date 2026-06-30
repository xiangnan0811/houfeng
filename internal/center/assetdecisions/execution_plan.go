package assetdecisions

import (
	"fmt"
	"strings"
)

type ExecutionPlanLane string

const (
	PlanLaneCancelRetire ExecutionPlanLane = "cancel_retire"
	PlanLaneMigration    ExecutionPlanLane = "migration"
	PlanLaneKeepObserve  ExecutionPlanLane = "keep_observe"
	PlanLaneEvidence     ExecutionPlanLane = "evidence"
	PlanLaneReview       ExecutionPlanLane = "review"
)

type ExecutionPlanStepKind string

const (
	PlanStepOpenCancellationWorkbench ExecutionPlanStepKind = "open_cancellation_workbench"
	PlanStepOpenVPSDetail             ExecutionPlanStepKind = "open_vps_detail"
	PlanStepOpenSubscriptionContext   ExecutionPlanStepKind = "open_subscription_context"
	PlanStepReviewRecord              ExecutionPlanStepKind = "review_record"
)

type ExecutionPlanTone string

const (
	PlanToneCritical ExecutionPlanTone = "critical"
	PlanToneAlert    ExecutionPlanTone = "alert"
	PlanToneNotice   ExecutionPlanTone = "notice"
	PlanToneNormal   ExecutionPlanTone = "normal"
	PlanToneNeutral  ExecutionPlanTone = "neutral"
)

type ExecutionPlanLaneCount struct {
	Lane  ExecutionPlanLane `json:"lane"`
	Count int               `json:"count"`
}

type MemberExecutionPlan struct {
	Lane       ExecutionPlanLane     `json:"lane"`
	StepKind   ExecutionPlanStepKind `json:"step_kind"`
	Tone       ExecutionPlanTone     `json:"tone"`
	Summary    string                `json:"summary"`
	StepLabel  string                `json:"step_label"`
	IssueCount int                   `json:"issue_count"`
	Blocked    bool                  `json:"blocked"`
	Actionable bool                  `json:"actionable"`
}

type RecordExecutionPlan struct {
	Summary         string                   `json:"summary"`
	LaneCounts      []ExecutionPlanLaneCount `json:"lane_counts"`
	ActionableCount int                      `json:"actionable_count"`
	BlockedCount    int                      `json:"blocked_count"`
}

func EvaluateRecordExecutionPlan(status RecordStatus, readback RecordExecutionReadback, members []RecordMember) RecordExecutionPlan {
	plan := RecordExecutionPlan{LaneCounts: []ExecutionPlanLaneCount{}}
	if status == RecordStatusAbandoned {
		plan.Summary = "记录已放弃，不参与执行编排"
		return plan
	}
	if len(members) == 0 {
		plan.Summary = "暂无待编排成员"
		return plan
	}

	counts := map[ExecutionPlanLane]int{}
	for _, member := range members {
		memberPlan := member.ExecutionPlan
		if memberPlan.Lane == "" {
			memberPlan = EvaluateMemberExecutionPlan(status, member)
		}
		if memberPlan.Lane != "" {
			counts[memberPlan.Lane]++
		}
		if memberPlan.Actionable {
			plan.ActionableCount++
		}
		if memberPlan.Blocked {
			plan.BlockedCount++
		}
	}

	for _, lane := range []ExecutionPlanLane{PlanLaneCancelRetire, PlanLaneMigration, PlanLaneKeepObserve, PlanLaneEvidence, PlanLaneReview} {
		if count := counts[lane]; count > 0 {
			plan.LaneCounts = append(plan.LaneCounts, ExecutionPlanLaneCount{Lane: lane, Count: count})
		}
	}

	switch {
	case readback.DriftCount > 0:
		plan.Summary = fmt.Sprintf("%d 台 VPS 事实漂移，优先复核闭环", readback.DriftCount)
	case plan.BlockedCount > 0:
		plan.Summary = fmt.Sprintf("%d 台 VPS 跟进阻塞，需要解除阻塞", plan.BlockedCount)
	case readback.NeedsEvidenceCount > 0:
		plan.Summary = fmt.Sprintf("%d 台 VPS 需要补齐证据", readback.NeedsEvidenceCount)
	case plan.ActionableCount > 0:
		plan.Summary = fmt.Sprintf("%d 台 VPS 仍有执行步骤", plan.ActionableCount)
	default:
		plan.Summary = "执行计划已对齐"
	}
	return plan
}

func EvaluateMemberExecutionPlan(recordStatus RecordStatus, member RecordMember) MemberExecutionPlan {
	if recordStatus == RecordStatusAbandoned {
		return MemberExecutionPlan{
			Lane:       PlanLaneReview,
			StepKind:   PlanStepReviewRecord,
			Tone:       PlanToneNeutral,
			Summary:    "记录已放弃，不需要推进",
			StepLabel:  "留在记录中复核",
			IssueCount: len(member.ExecutionReadback.Issues),
		}
	}

	action := memberAction(member)
	lane := laneForAction(action)
	if hasReadbackIssueKind(member.ExecutionReadback.Issues, "current_fact_missing") {
		lane = PlanLaneReview
	}
	plan := MemberExecutionPlan{
		Lane:       lane,
		StepKind:   stepKindForMember(action, member.ExecutionReadback),
		Tone:       toneForReadback(member.ExecutionReadback.Status),
		Summary:    memberExecutionPlanSummary(member),
		IssueCount: len(member.ExecutionReadback.Issues),
		Blocked:    member.ExecutionReadback.Status == ReadbackBlocked || member.FollowupStatus == FollowupBlocked,
		Actionable: memberExecutionPlanActionable(member),
	}
	plan.StepLabel = stepLabelForPlan(plan.StepKind, action)
	return plan
}

func laneForAction(action SuggestedAction) ExecutionPlanLane {
	switch action {
	case ActionCancel, ActionOpenCancellationWorkbench:
		return PlanLaneCancelRetire
	case ActionMigrate:
		return PlanLaneMigration
	case ActionKeep, ActionObserve:
		return PlanLaneKeepObserve
	case ActionCompleteEvidence:
		return PlanLaneEvidence
	default:
		return PlanLaneReview
	}
}

func stepKindForMember(action SuggestedAction, readback MemberExecutionReadback) ExecutionPlanStepKind {
	if hasReadbackIssueKind(readback.Issues, "current_fact_missing") || action == "" || action == ActionReview {
		return PlanStepReviewRecord
	}
	switch action {
	case ActionCancel, ActionOpenCancellationWorkbench:
		return PlanStepOpenCancellationWorkbench
	case ActionCompleteEvidence:
		if hasSubscriptionEvidenceGap(readback.Issues) {
			return PlanStepOpenSubscriptionContext
		}
		return PlanStepOpenVPSDetail
	default:
		return PlanStepOpenVPSDetail
	}
}

func toneForReadback(status ExecutionReadbackStatus) ExecutionPlanTone {
	switch status {
	case ReadbackDrift:
		return PlanToneCritical
	case ReadbackBlocked:
		return PlanToneCritical
	case ReadbackNeedsEvidence:
		return PlanToneAlert
	case ReadbackOpen:
		return PlanToneNotice
	case ReadbackAligned:
		return PlanToneNormal
	case ReadbackInactive:
		return PlanToneNeutral
	default:
		return PlanToneNeutral
	}
}

func memberExecutionPlanSummary(member RecordMember) string {
	switch member.ExecutionReadback.Status {
	case ReadbackDrift:
		return "当前事实与判断不一致，需要复核闭环"
	case ReadbackBlocked:
		return "成员跟进阻塞，需要解除阻塞或调整路径"
	case ReadbackNeedsEvidence:
		return "证据仍未补齐，先补上下文再确认判断"
	case ReadbackOpen:
		return "当前事实仍待处理或复核"
	case ReadbackInactive:
		return "记录已失效，不需要推进"
	case ReadbackAligned:
		if followupClosed(member.FollowupStatus) {
			return "当前事实已对齐"
		}
		return "当前事实已对齐，待确认跟进状态"
	default:
		return "需要复核执行路径"
	}
}

func memberExecutionPlanActionable(member RecordMember) bool {
	switch member.ExecutionReadback.Status {
	case ReadbackDrift, ReadbackBlocked, ReadbackNeedsEvidence, ReadbackOpen:
		return true
	case ReadbackAligned:
		return !followupClosed(member.FollowupStatus)
	default:
		return false
	}
}

func stepLabelForPlan(stepKind ExecutionPlanStepKind, action SuggestedAction) string {
	switch stepKind {
	case PlanStepOpenCancellationWorkbench:
		return "打开取消/退役工作台"
	case PlanStepOpenSubscriptionContext:
		return "核对订阅上下文"
	case PlanStepOpenVPSDetail:
		switch action {
		case ActionMigrate:
			return "标记迁移意向并人工跟进"
		case ActionKeep, ActionObserve:
			return "打开 VPS 详情核对判断"
		case ActionCompleteEvidence:
			return "打开 VPS 详情补证据"
		default:
			return "打开 VPS 详情"
		}
	default:
		return "留在记录中复核"
	}
}

func followupClosed(status FollowupStatus) bool {
	return status == FollowupDone || status == FollowupSkipped
}

func hasSubscriptionEvidenceGap(issues []ExecutionReadbackIssue) bool {
	for _, issue := range issues {
		if issue.Kind != "evidence_gap" {
			continue
		}
		if strings.Contains(issue.Label, "订阅") || strings.Contains(issue.Details, "订阅") || strings.Contains(strings.ToLower(issue.Details), "subscription") {
			return true
		}
	}
	return false
}

func hasReadbackIssueKind(issues []ExecutionReadbackIssue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}
