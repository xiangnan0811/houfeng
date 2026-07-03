import { Badge, type BadgeTone, MonoDigits } from '../../components/atoms'
import type {
  AssetDecisionComparisonSignal,
  AssetDecisionEvidenceAssessment,
  AssetDecisionEvidenceChip,
  AssetDecisionExecutionReadbackStatus,
  AssetDecisionMemberExecutionPlan,
  AssetDecisionMemberExecutionReadback,
  AssetDecisionRecordMember,
  AssetDecisionRecommendation,
} from '../../lib/types'

import {
  EVIDENCE_BIAS_LABELS,
  EVIDENCE_KIND_LABELS,
  EVIDENCE_TIER_LABELS,
  EXECUTION_PLAN_LANE_LABELS,
  READBACK_STATUS_LABELS,
} from './constants'
import {
  chipTone,
  currentFactsLabel,
  currentFactsStateLabel,
  evidenceBiasTone,
  evidenceTierTone,
  executionPlanTone,
  readbackStatusTone,
  actionLabelForMember,
} from './formatters'

type ScoreStyle = React.CSSProperties & {
  '--score': number
}

export function renderReadbackBadge(readback?: { status: AssetDecisionExecutionReadbackStatus }) {
  const status = readback?.status
  if (!status) {
    return (
      <Badge variant="state" tone="neutral">
        等待回读
      </Badge>
    )
  }
  return (
    <Badge variant="state" tone={readbackStatusTone(status)}>
      {READBACK_STATUS_LABELS[status] ?? status}
    </Badge>
  )
}

export function renderExecutionPlanBadge(plan?: AssetDecisionMemberExecutionPlan) {
  if (!plan) {
    return (
      <Badge variant="state" tone="neutral">
        等待编排
      </Badge>
    )
  }
  return (
    <Badge variant="state" tone={executionPlanTone(plan.tone)}>
      {EXECUTION_PLAN_LANE_LABELS[plan.lane] ?? plan.lane}
    </Badge>
  )
}

export function renderMemberExecutionPlan(member: AssetDecisionRecordMember) {
  const plan = member.execution_plan
  return (
    <div className="asset-table__stack asset-decision-plan-cell">
      <span className="asset-decision-chip-row">
        {renderExecutionPlanBadge(plan)}
        {plan?.actionable && (
          <Badge variant="count" tone={executionPlanTone(plan.tone)}>
            可推进
          </Badge>
        )}
        {plan?.blocked && (
          <Badge variant="count" tone="critical">
            阻塞
          </Badge>
        )}
      </span>
      <strong>{plan?.summary || '等待执行编排'}</strong>
      <span>{actionLabelForMember(member)}</span>
      {plan?.issue_count ? <span>关联问题 {plan.issue_count} 项</span> : <span>无额外问题</span>}
    </div>
  )
}

export function renderMemberReadback(readback?: AssetDecisionMemberExecutionReadback) {
  if (!readback) {
    return (
      <div className="asset-table__stack asset-decision-readback-cell">
        <span className="asset-decision-chip-row">{renderReadbackBadge()}</span>
        <strong>等待执行证据回读</strong>
        <span>当前事实尚未返回</span>
      </div>
    )
  }
  const issues = readback.issues ?? []
  return (
    <div className="asset-table__stack asset-decision-readback-cell">
      <span className="asset-decision-chip-row">
        {renderReadbackBadge(readback)}
        {issues.length > 0 && (
          <Badge variant="count" tone={readbackStatusTone(readback.status)}>
            {issues.length} 项
          </Badge>
        )}
      </span>
      <strong>{readback.summary || '等待执行回读'}</strong>
      <span>{currentFactsLabel(readback.current_facts)}</span>
      <span>{currentFactsStateLabel(readback.current_facts)}</span>
      {issues.length > 0 && (
        <span className="asset-decision-chip-row">
          {issues.slice(0, 3).map((issue) => (
            <Badge key={`${issue.kind}-${issue.label}`} variant="info" tone={chipTone(issue.tone)}>
              {issue.label}
            </Badge>
          ))}
          {issues.length > 3 && (
            <Badge variant="count" tone="neutral">
              +{issues.length - 3}
            </Badge>
          )}
        </span>
      )}
    </div>
  )
}

export function renderEvidenceChips(chips: AssetDecisionEvidenceChip[], limit = 5) {
  if (chips.length === 0) return <span className="empty-inline">暂无证据标签</span>
  const visible = chips.slice(0, limit)
  return (
    <span className="asset-decision-chip-row">
      {visible.map((chip) => (
        <Badge key={`${chip.kind}-${chip.label}`} variant="info" tone={chipTone(chip.tone)}>
          {chip.label || EVIDENCE_KIND_LABELS[chip.kind] || chip.kind}
        </Badge>
      ))}
      {chips.length > visible.length && (
        <Badge variant="count" tone="neutral">
          +{chips.length - visible.length}
        </Badge>
      )}
    </span>
  )
}

export function renderCompactRiskChips(chips: AssetDecisionEvidenceChip[] = [], assessment?: AssetDecisionEvidenceAssessment | null) {
  const risks = chips
    .filter((chip) => chip.tone === 'critical' || chip.tone === 'alert')
    .map((chip) => ({
      key: `${chip.kind}-${chip.label}`,
      label: chip.label || EVIDENCE_KIND_LABELS[chip.kind] || chip.kind,
      tone: chipTone(chip.tone),
    }))
  if (risks.length < 2 && assessment && assessment.gap_signal_count > 0) {
    risks.push({ key: 'gap-signal-count', label: `缺口 ${assessment.gap_signal_count}`, tone: 'alert' as BadgeTone })
  }
  if (risks.length < 2 && assessment && assessment.risk_signal_count > 0) {
    risks.push({ key: 'risk-signal-count', label: `风险 ${assessment.risk_signal_count}`, tone: 'alert' as BadgeTone })
  }
  const compactRisks = risks.slice(0, 2)

  if (compactRisks.length === 0) {
    return (
      <Badge variant="state" tone={assessment ? evidenceTierTone(assessment.quality_tier) : 'normal'}>
        {assessment ? EVIDENCE_TIER_LABELS[assessment.quality_tier] : '证据稳定'}
      </Badge>
    )
  }
  return (
    <>
      {compactRisks.map((chip) => (
        <Badge key={chip.key} variant="info" tone={chip.tone}>
          {chip.label}
        </Badge>
      ))}
    </>
  )
}

export function renderEvidenceAssessment(assessment: AssetDecisionEvidenceAssessment | null, mode: 'compact' | 'detail' = 'compact') {
  if (!assessment) return <span className="empty-inline">无证据评估</span>
  return (
    <div className={`asset-decision-assessment asset-decision-assessment--${mode}`}>
      <div className="asset-decision-assessment__head">
        <Badge variant="state" tone={evidenceTierTone(assessment.quality_tier)}>
          {EVIDENCE_TIER_LABELS[assessment.quality_tier] ?? assessment.quality_tier}
        </Badge>
        <Badge variant="state" tone={evidenceBiasTone(assessment.decision_bias)}>
          {EVIDENCE_BIAS_LABELS[assessment.decision_bias] ?? assessment.decision_bias}
        </Badge>
      </div>
      <div className="asset-decision-assessment__bars" aria-label="证据评估刻度">
        <span style={{ '--score': assessment.confidence_score } as ScoreStyle}>
          可信 <MonoDigits>{assessment.confidence_score}</MonoDigits>
        </span>
        <span style={{ '--score': assessment.pressure_score } as ScoreStyle}>
          压力 <MonoDigits>{assessment.pressure_score}</MonoDigits>
        </span>
        <span style={{ '--score': assessment.readiness_score } as ScoreStyle}>
          准备 <MonoDigits>{assessment.readiness_score}</MonoDigits>
        </span>
      </div>
      {mode === 'detail' && (
        <div className="asset-decision-assessment__meta">
          <strong>{assessment.summary}</strong>
          <span>支撑 {assessment.support_signal_count} · 风险 {assessment.risk_signal_count} · 缺口 {assessment.gap_signal_count}</span>
        </div>
      )}
    </div>
  )
}

export function renderComparisonSignals(signals: AssetDecisionComparisonSignal[], limit = 3) {
  if (signals.length === 0) return null
  const visible = signals.slice(0, limit)
  return (
    <span className="asset-decision-chip-row">
      {visible.map((signal, index) => (
        <Badge key={`${signal.kind}-${signal.label}-${index}`} variant="info" tone={chipTone(signal.tone)}>
          {signal.label}
        </Badge>
      ))}
      {signals.length > visible.length && (
        <Badge variant="count" tone="neutral">
          +{signals.length - visible.length}
        </Badge>
      )}
    </span>
  )
}

export function renderDecisionRecommendation(
  recommendation?: AssetDecisionRecommendation | null,
  mode: 'compact' | 'detail' = 'compact',
) {
  if (!recommendation) return mode === 'compact' ? <span className="empty-inline">等待建议</span> : null
  const signalLimit = mode === 'detail' ? 5 : 3
  const reasons = recommendation.reasons ?? []
  const blockers = recommendation.blockers ?? []
  return (
    <div className={`asset-decision-recommendation asset-decision-recommendation--${mode}`}>
      <span className="asset-decision-chip-row">
        <Badge variant="state" tone={blockers.length > 0 ? 'critical' : 'notice'}>
          {recommendation.confidence_label || '建议'}
        </Badge>
        {blockers.length > 0 && (
          <Badge variant="count" tone="critical">
            阻塞 {blockers.length}
          </Badge>
        )}
      </span>
      <strong>{recommendation.summary || '等待系统建议'}</strong>
      <span>{recommendation.next_step || '继续比较同组成员后记录判断'}</span>
      {mode === 'detail' && recommendation.priority_vps_ids.length > 0 && (
        <small>优先核对 {recommendation.priority_vps_ids.join(' / ')}</small>
      )}
      {(reasons.length > 0 || blockers.length > 0) && (
        <span className="asset-decision-chip-row">
          {[...blockers, ...reasons].slice(0, signalLimit).map((reason) => (
            <Badge key={`${reason.kind}-${reason.label}`} variant="info" tone={chipTone(reason.tone)}>
              {reason.label}
            </Badge>
          ))}
          {blockers.length + reasons.length > signalLimit && (
            <Badge variant="count" tone="neutral">
              +{blockers.length + reasons.length - signalLimit}
            </Badge>
          )}
        </span>
      )}
    </div>
  )
}
