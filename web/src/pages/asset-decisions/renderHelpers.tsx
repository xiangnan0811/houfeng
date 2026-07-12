import type { ReactNode } from 'react'

import { Badge, type BadgeTone, MonoDigits } from '../../components/atoms'
import type {
  AssetDecisionComparisonLane,
  AssetDecisionComparisonSignal,
  AssetDecisionEvidenceAssessment,
  AssetDecisionEvidenceChip,
  AssetDecisionExecutionReadbackStatus,
  AssetDecisionGroupMember,
  AssetDecisionManualGroupMember,
  AssetDecisionMemberExecutionPlan,
  AssetDecisionMemberExecutionReadback,
  AssetDecisionRecordMember,
  AssetDecisionRecommendation,
} from '../../lib/types'
import { formatMoney, formatOptional } from '../../lib/format'
import { lifecycleLabel, renewalLabel, usageLabel, vpsLocationLabel } from '../assetPageUtils'

import {
  ASSET_DECISION_DETAIL_PREVIEW_LIMIT,
  COMPARISON_LANE_LABELS,
  EVIDENCE_BIAS_LABELS,
  EVIDENCE_KIND_LABELS,
  EVIDENCE_TIER_LABELS,
  EXECUTION_PLAN_LANE_LABELS,
  READBACK_STATUS_LABELS,
  ROLE_LABELS,
  ACTION_LABELS,
} from './constants'
import { vpsDetailPath } from './paths'
import {
  chipTone,
  compactDecisionText,
  comparisonLaneTone,
  currentFactsLabel,
  currentFactsStateLabel,
  evidenceBiasTone,
  evidenceTierTone,
  executionPlanTone,
  readbackStatusTone,
  actionLabelForMember,
  actionTone,
  roleTone,
  memberContextLabel,
  sourceAvailabilityLabel,
} from './formatters'
import type {
  ComparisonMatrixMember,
  DetailCommandOptions,
  MemberDecisionCardsOptions,
} from './types'

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

export function renderCompactRiskChips(chips: AssetDecisionEvidenceChip[] | null | undefined, assessment?: AssetDecisionEvidenceAssessment | null) {
  const risks = chips
    ? chips.filter((chip) => chip.tone === 'critical' || chip.tone === 'alert').map((chip) => ({
        key: `${chip.kind}-${chip.label}`,
        label: chip.label || EVIDENCE_KIND_LABELS[chip.kind] || chip.kind,
        tone: chipTone(chip.tone),
      }))
    : []
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
        {renderAssessmentScore('可信', assessment.confidence_score)}
        {renderAssessmentScore('压力', assessment.pressure_score)}
        {renderAssessmentScore('准备', assessment.readiness_score)}
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

function renderAssessmentScore(label: string, value: number) {
  return (
    <div className="asset-decision-assessment__bar">
      <progress aria-label={label} max={100} value={value} />
      <span className="asset-decision-assessment__bar-label" aria-hidden="true">
        {label} <MonoDigits>{value}</MonoDigits>
      </span>
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

// ---- 详情面板渲染函数（从 AssetDecisionsPage 提取，供弹窗组件复用）----

export function previewItems<T>(items: T[]): { visible: T[]; hiddenCount: number } {
  return {
    visible: items.slice(0, ASSET_DECISION_DETAIL_PREVIEW_LIMIT),
    hiddenCount: Math.max(0, items.length - ASSET_DECISION_DETAIL_PREVIEW_LIMIT),
  }
}

export function memberComparisonTitle(member: ComparisonMatrixMember) {
  return <strong>{member.displayName}</strong>
}

export function renderDetailCommand(options: DetailCommandOptions) {
  return (
    <section className="asset-decision-detail-command" aria-label={options.ariaLabel}>
      <div className="asset-decision-detail-command__main">
        <div>
          <strong>{options.summary}</strong>
        </div>
        <div className="asset-decision-detail-command__meta">
          {options.badge ?? null}
          {renderCompactRiskChips(options.chips, options.assessment)}
        </div>
      </div>
      {(options.actions || options.footer) && (
        <div className="asset-decision-detail-command__footer">
          {options.footer}
          {options.actions && <div className="asset-decision-detail-command__actions">{options.actions}</div>}
        </div>
      )}
    </section>
  )
}

export function renderMemberDecisionRows(members: ComparisonMatrixMember[], options: MemberDecisionCardsOptions) {
  const sortedMembers = [...members].sort((left, right) => {
    const leftRank = left.comparison?.rank ?? Number.POSITIVE_INFINITY
    const rightRank = right.comparison?.rank ?? Number.POSITIVE_INFINITY
    if (leftRank !== rightRank) return leftRank - rightRank
    return left.displayName.localeCompare(right.displayName)
  })
  const memberPreview = previewItems(sortedMembers)
  return (
    <section className="asset-decision-member-decisions">
      {renderCompactTaskHeader(options.title, `成员 ${sortedMembers.length}`)}
      {sortedMembers.length > 0 ? (
        <div className="asset-decision-member-rows" aria-label={options.ariaLabel}>
          {memberPreview.visible.map((member) => {
            const comparison = member.comparison
            const lane: AssetDecisionComparisonLane = comparison?.lane ?? 'review'
            const laneTone = comparisonLaneTone(lane)
            const intentMismatch = options.showIntent
              && comparison
              && (
                (member.intendedAction === 'cancel' || member.intendedAction === 'open_cancellation_workbench') !== (lane === 'retire')
                || (member.intendedAction === 'complete_evidence') !== (lane === 'evidence')
                || (member.intendedRole === 'primary_candidate') !== (lane === 'primary')
              )
            return (
              <article key={member.key} className={`asset-decision-member-row asset-decision-member-row--${laneTone}`}>
                <div className="asset-decision-member-row__identity">
                  <span>{COMPARISON_LANE_LABELS[lane] ?? lane}</span>
                  {memberComparisonTitle(member)}
                </div>
                <div className="asset-decision-member-row__decision">
                  <div>
                    <span className="asset-decision-chip-row">
                    <Badge variant="state" tone={roleTone(member.intendedRole ?? member.role)}>
                      {ROLE_LABELS[member.intendedRole ?? member.role]}
                    </Badge>
                    <Badge variant="state" tone={actionTone(member.intendedAction ?? member.action)}>
                      {ACTION_LABELS[member.intendedAction ?? member.action]}
                    </Badge>
                    {options.showIntent && (
                      <Badge variant="state" tone={intentMismatch ? 'alert' : 'normal'}>
                        {intentMismatch ? '需复核意图' : '意图匹配'}
                      </Badge>
                    )}
                    </span>
                    <strong>{compactDecisionText(comparison?.summary, '等待复核')}</strong>
                  </div>
                </div>
                {((comparison?.risks?.length ?? 0) + (comparison?.gaps?.length ?? 0)) > 0 && (
                  <div className="asset-decision-member-row__signals">
                    {renderComparisonSignals([...(comparison?.risks ?? []), ...(comparison?.gaps ?? [])], 2)}
                  </div>
                )}
                {options.action && <div className="asset-decision-member-row__actions">{options.action(member)}</div>}
              </article>
            )
          })}
          {memberPreview.hiddenCount > 0 && (
            <div className="asset-decision-preview-more" role="note">
              <span>另有 {memberPreview.hiddenCount} 台在底稿中查看</span>
              {options.hiddenAction}
            </div>
          )}
          {options.footerAction && (
            <div className="asset-operation-actions">
              {options.footerAction}
            </div>
          )}
        </div>
      ) : (
        <>
          <div className="asset-decision-member-decisions__empty">
            <strong>暂无可取舍成员</strong>
            <span>当前组合没有可展示的成员判断。</span>
          </div>
          {options.footerAction && (
            <div className="asset-operation-actions">
              {options.footerAction}
            </div>
          )}
        </>
      )}
    </section>
  )
}

export function renderDetailPanel(title: string, children: ReactNode, includeHiddenHeading = false) {
  return (
    <section className="asset-decision-detail-panel" aria-label={title}>
      {includeHiddenHeading ? <h4 className="visually-hidden">{title}</h4> : null}
      {children}
    </section>
  )
}

export function renderCompactTaskHeader(title: string, meta?: ReactNode) {
  return (
    <div className="asset-decision-task-header" role="heading" aria-level={4} aria-label={title}>
      <strong>{title}</strong>
      {meta ? <span aria-hidden="true">{meta}</span> : null}
    </div>
  )
}

export function groupMemberComparisonMatrixMember(member: AssetDecisionGroupMember): ComparisonMatrixMember {
  const monthlyCost = member.primary_subscription
    ? `${formatMoney(member.primary_subscription.monthly_price, member.primary_subscription.currency)}/月`
    : member.source_availability.subscriptions ? '缺订阅成本' : '订阅证据不可用'
  return {
    key: member.vps.vps_id,
    displayName: member.vps.display_name || member.vps.vps_id,
    href: vpsDetailPath(member.vps.vps_id),
    meta: `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}`,
    product: `${monthlyCost} · ${member.vps.product_name || member.vps.vps_id}`,
    facts: memberContextLabel(member),
    statusFacts: `${lifecycleLabel(member.vps.lifecycle_status)} · ${usageLabel(member.vps.usage_status)} · ${renewalLabel(member.vps.renewal_decision)}`,
    sourceLabel: sourceAvailabilityLabel(member.source_availability),
    role: member.suggested_role,
    action: member.suggested_action,
    ...(member.comparison_insight === undefined
      ? {}
      : { comparison: member.comparison_insight }),
    evidenceChips: member.evidence_chips,
    currentFactFound: true,
  }
}

export function manualMemberComparisonMatrixMember(member: AssetDecisionManualGroupMember): ComparisonMatrixMember {
  if (!member.current_fact_found) {
    return {
      key: member.vps_id,
      displayName: member.vps_id,
      meta: '当前资产事实缺失',
      product: '无法回读成本和产品',
      facts: '无法回读承载和监控',
      statusFacts: '当前 facts 未返回',
      sourceLabel: '当前事实缺失',
      role: member.suggested_role,
      action: member.suggested_action,
      intendedRole: member.intended_role,
      intendedAction: member.intended_action,
      ...(member.comparison_insight === undefined
        ? {}
        : { comparison: member.comparison_insight }),
      evidenceChips: member.evidence_chips,
      currentFactFound: false,
    }
  }
  return {
    ...groupMemberComparisonMatrixMember(member),
    key: member.vps_id,
    href: vpsDetailPath(member.vps_id),
    intendedRole: member.intended_role,
    intendedAction: member.intended_action,
    currentFactFound: member.current_fact_found,
  }
}
