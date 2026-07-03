import { formatMoney } from '../../lib/format'
import type { BadgeTone } from '../../components/atoms'
import type {
  AssetDecisionComparisonLane,
  AssetDecisionEvidenceDecisionBias,
  AssetDecisionEvidenceKind,
  AssetDecisionEvidenceQualityTier,
  AssetDecisionEvidenceSnapshot,
  AssetDecisionExecutionCurrentFacts,
  AssetDecisionExecutionPlanTone,
  AssetDecisionExecutionReadbackStatus,
  AssetDecisionFollowupStatus,
  AssetDecisionGroupMember,
  AssetDecisionGroupSummary,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupScenario,
  AssetDecisionManualGroupStatus,
  AssetDecisionMemberExecutionReadback,
  AssetDecisionOverview,
  AssetDecisionRecordDetail,
  AssetDecisionRecordExecutionPlan,
  AssetDecisionRecordExecutionReadback,
  AssetDecisionRecordMember,
  AssetDecisionRecordStatus,
  AssetDecisionRecordSummary,
  AssetDecisionScenarioTemplateStatus,
  AssetDecisionSuggestedAction,
  AssetDecisionSuggestedRole,
  AssetDecisionView,
  VPSAssetRecord,
  VPSRenewalDecision,
} from '../../lib/types'
import {
  lifecycleLabel,
  renewalLabel,
  usageLabel,
} from '../assetPageUtils'

// 常量映射表
export const VIEW_LABELS: Record<AssetDecisionView, string> = {
  needs_decision: '需要决策',
  renewal: '续费取舍',
  region: '同区比较',
  provider: '服务商组合',
  cost: '预算压力',
  evidence: '资料缺口',
}

export const ROLE_LABELS: Record<AssetDecisionSuggestedRole, string> = {
  primary_candidate: '主力候选',
  standby_candidate: '备用候选',
  observe_candidate: '观察候选',
  retire_candidate: '退役候选',
  evidence_needed: '补证据',
}

export const ACTION_LABELS: Record<AssetDecisionSuggestedAction, string> = {
  review: '复核',
  keep: '保留',
  observe: '观察',
  migrate: '迁移',
  cancel: '取消',
  open_cancellation_workbench: '进入取消台',
  complete_evidence: '补齐资料',
}

export const MANUAL_GROUP_STATUS_LABELS: Record<AssetDecisionManualGroupStatus, string> = {
  active: '进行中',
  archived: '已归档',
}

export const SCENARIO_TEMPLATE_STATUS_LABELS: Record<AssetDecisionScenarioTemplateStatus, string> = {
  active: '启用',
  archived: '已归档',
}

export const MANUAL_GROUP_SCENARIO_LABELS: Record<AssetDecisionManualGroupScenario, string> = {
  general: '通用组合',
  primary_standby: '主备取舍',
  budget_reduction: '预算压缩',
  provider_review: '服务商评估',
  region_review: '同区比较',
  migration_retirement: '迁移退役',
  evidence_cleanup: '资料清理',
}

export const RECORD_STATUS_LABELS: Record<AssetDecisionRecordStatus, string> = {
  draft: '草稿',
  decided: '已决策',
  in_progress: '推进中',
  completed: '已完成',
  abandoned: '已放弃',
}

export const FOLLOWUP_STATUS_LABELS: Record<AssetDecisionFollowupStatus, string> = {
  todo: '待处理',
  in_progress: '处理中',
  blocked: '阻塞',
  done: '已完成',
  skipped: '跳过',
}

export const READBACK_STATUS_LABELS: Record<AssetDecisionExecutionReadbackStatus, string> = {
  open: '待回读',
  aligned: '已对齐',
  drift: '有漂移',
  blocked: '阻塞',
  needs_evidence: '需补证据',
  inactive: '不活跃',
}

export const EXECUTION_PLAN_LANE_LABELS: Record<string, string> = {
  cancel_retire: '取消退役',
  migration: '迁移',
  keep_observe: '保留观察',
  evidence: '补证据',
  review: '复核',
}

export const COMPARISON_LANE_LABELS: Record<AssetDecisionComparisonLane, string> = {
  primary: '主力',
  standby: '备用',
  observe: '观察',
  retire: '退役',
  evidence: '补证据',
  review: '复核',
}

export const EVIDENCE_TIER_LABELS: Record<AssetDecisionEvidenceQualityTier, string> = {
  strong: '证据强',
  usable: '可决策',
  weak: '证据弱',
  blocked: '先补证据',
}

export const EVIDENCE_BIAS_LABELS: Record<AssetDecisionEvidenceDecisionBias, string> = {
  keep: '偏保留',
  observe: '偏观察',
  complete_evidence: '补证据',
  retire: '偏退役',
  migrate: '偏迁移',
  review: '待复核',
}

export const EVIDENCE_KIND_LABELS: Partial<Record<AssetDecisionEvidenceKind, string>> = {
  ip_quality_missing: 'IP 质量缺失',
  ip_quality_stale: 'IP 质量过期',
  ip_quality_risk: 'IP 质量风险',
  ip_egress_mismatch: '出口 IP 不一致',
  media_unlock_blocked: '服务解锁受阻',
}

// === 金额格式化 ===

export function baseMoney(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '—'
  return formatMoney(value, currency)
}

type CostSummaryLike = {
  monthly_cost_by_currency: AssetDecisionGroupSummary['monthly_cost_by_currency']
  monthly_cost_base?: number | null
  yearly_cost_base?: number | null
  base_currency?: string
}

export function formatGroupMonthlyCost(group: CostSummaryLike): string {
  if (group.monthly_cost_base != null) {
    return `${formatMoney(group.monthly_cost_base, group.base_currency ?? 'CNY')}/月`
  }
  if (group.monthly_cost_by_currency.length > 0) {
    return group.monthly_cost_by_currency
      .map((item) => `${formatMoney(item.monthly_total, item.currency)}/月`)
      .join(' / ')
  }
  return '暂无成本'
}

export function formatGroupYearlyCost(group: CostSummaryLike): string {
  if (group.yearly_cost_base != null) {
    return `${formatMoney(group.yearly_cost_base, group.base_currency ?? 'CNY')}/年`
  }
  if (group.monthly_cost_by_currency.length > 0) {
    return group.monthly_cost_by_currency
      .map((item) => `${formatMoney(item.yearly_total, item.currency)}/年`)
      .join(' / ')
  }
  return '成本证据不足'
}

// === Tone 映射函数 ===

export function chipTone(tone?: string): BadgeTone {
  if (tone === 'normal' || tone === 'notice' || tone === 'alert' || tone === 'critical' || tone === 'maintenance' || tone === 'offline') {
    return tone
  }
  return 'neutral'
}

export function roleTone(role: AssetDecisionSuggestedRole): BadgeTone {
  if (role === 'primary_candidate') return 'normal'
  if (role === 'standby_candidate' || role === 'observe_candidate') return 'maintenance'
  if (role === 'retire_candidate') return 'critical'
  return 'notice'
}

export function actionTone(action: AssetDecisionSuggestedAction): BadgeTone {
  if (action === 'keep') return 'normal'
  if (action === 'open_cancellation_workbench' || action === 'cancel') return 'critical'
  if (action === 'migrate' || action === 'observe') return 'maintenance'
  return 'notice'
}

export function manualGroupStatusTone(status: AssetDecisionManualGroupStatus): BadgeTone {
  return status === 'active' ? 'maintenance' : 'offline'
}

export function recordStatusTone(status: AssetDecisionRecordStatus): BadgeTone {
  if (status === 'completed') return 'normal'
  if (status === 'in_progress') return 'maintenance'
  if (status === 'decided') return 'notice'
  if (status === 'abandoned') return 'offline'
  return 'neutral'
}

export function followupStatusTone(status: AssetDecisionFollowupStatus): BadgeTone {
  if (status === 'done' || status === 'skipped') return 'normal'
  if (status === 'blocked') return 'critical'
  if (status === 'in_progress') return 'maintenance'
  return 'notice'
}

export function readbackStatusTone(status?: AssetDecisionExecutionReadbackStatus): BadgeTone {
  if (status === 'aligned') return 'normal'
  if (status === 'drift') return 'critical'
  if (status === 'blocked') return 'critical'
  if (status === 'needs_evidence') return 'alert'
  if (status === 'inactive') return 'offline'
  if (status === 'open') return 'maintenance'
  return 'neutral'
}

export function executionPlanTone(tone?: AssetDecisionExecutionPlanTone): BadgeTone {
  if (tone === 'normal') return 'normal'
  if (tone === 'critical') return 'critical'
  if (tone === 'alert') return 'alert'
  if (tone === 'notice') return 'maintenance'
  return 'neutral'
}

export function comparisonLaneTone(lane?: AssetDecisionComparisonLane): BadgeTone {
  if (lane === 'primary') return 'normal'
  if (lane === 'standby' || lane === 'observe') return 'maintenance'
  if (lane === 'retire') return 'critical'
  if (lane === 'evidence') return 'alert'
  return 'neutral'
}

export function scenarioTemplateStatusTone(status: AssetDecisionScenarioTemplateStatus): BadgeTone {
  return status === 'active' ? 'maintenance' : 'offline'
}

export function evidenceTierTone(tier: AssetDecisionEvidenceQualityTier): BadgeTone {
  if (tier === 'strong') return 'normal'
  if (tier === 'usable') return 'notice'
  if (tier === 'blocked') return 'critical'
  return 'maintenance'
}

export function evidenceBiasTone(bias: AssetDecisionEvidenceDecisionBias): BadgeTone {
  if (bias === 'keep') return 'normal'
  if (bias === 'retire') return 'critical'
  if (bias === 'migrate' || bias === 'observe') return 'maintenance'
  if (bias === 'complete_evidence') return 'alert'
  return 'notice'
}

// === 队列标签 ===

const DECISION_QUEUE_VALUES: VPSRenewalDecision[] = ['unreviewed', 'migrate', 'cancel']

export function renewalQueueLabel(value: VPSRenewalDecision): string {
  return DECISION_QUEUE_VALUES.includes(value) ? renewalLabel(value) : '已处理'
}

// === 计数汇总 ===

export function recordFollowupDoneCount(record: AssetDecisionRecordSummary): number {
  return (record.followup_done_count ?? 0) + (record.followup_skipped_count ?? 0)
}

export function recordFollowupOpenCount(record: AssetDecisionRecordSummary): number {
  return (record.followup_todo_count ?? 0) + (record.followup_in_progress_count ?? 0) + (record.followup_blocked_count ?? 0)
}

export function readbackCountSummary(readback?: AssetDecisionRecordExecutionReadback): string {
  if (!readback) return '等待回读'
  const parts = [
    readback.drift_count > 0 ? `漂移 ${readback.drift_count}` : '',
    readback.blocked_count > 0 ? `阻塞 ${readback.blocked_count}` : '',
    readback.needs_evidence_count > 0 ? `缺口 ${readback.needs_evidence_count}` : '',
    readback.open_count > 0 ? `待处理 ${readback.open_count}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : `对齐 ${readback.aligned_count ?? 0}`
}

export function executionPlanCountSummary(plan?: AssetDecisionRecordExecutionPlan): string {
  if (!plan) return '等待编排'
  const laneSummary = (plan.lane_counts ?? [])
    .filter((item) => item.count > 0)
    .map((item) => `${EXECUTION_PLAN_LANE_LABELS[item.lane] ?? item.lane} ${item.count}`)
  const actionSummary = plan.actionable_count > 0 ? `可推进 ${plan.actionable_count}` : '无待推进'
  const blockedSummary = plan.blocked_count > 0 ? `阻塞 ${plan.blocked_count}` : ''
  return [actionSummary, blockedSummary, ...laneSummary].filter(Boolean).join(' · ')
}

export function countSummary<T extends string>(
  counts: Partial<Record<T, number>>,
  order: T[],
  labeler: (value: T) => string,
): string {
  const parts = order
    .map((key) => {
      const count = counts[key] ?? 0
      return count > 0 ? `${labeler(key)} ${count}` : ''
    })
    .filter(Boolean)
  return parts.length > 0 ? parts.join(' / ') : '暂无分布'
}

// === 上下文和证据标签 ===

export function sourceAvailabilityLabel(source: AssetDecisionOverview['source_availability'] | AssetDecisionGroupMember['source_availability']): string {
  const missing = [
    !source.subscriptions && '订阅',
    !source.services && '服务',
    !source.domains && '域名',
    !source.monitoring && '监控',
    !source.targets && 'Target',
  ].filter(Boolean)
  return missing.length > 0 ? `${missing.join('、')}证据不可用` : '证据源正常'
}

export function memberContextLabel(member: AssetDecisionGroupMember): string {
  return [
    `服务 ${member.service_count}`,
    `域名 ${member.domain_count}`,
    `Target ${member.running_target_count}/${member.target_count}`,
    `监控 ${member.running_monitoring_count}/${member.monitoring_link_count}`,
  ].join(' · ')
}

export function currentFactsLabel(facts?: AssetDecisionExecutionCurrentFacts): string {
  if (!facts) return '当前事实尚未返回'
  if (!facts.found) return '当前事实缺失'
  return [
    `订阅 ${facts.active_subscription_count}`,
    `服务 ${facts.service_count}`,
    `域名 ${facts.domain_count}`,
    `Target ${facts.running_target_count}/${facts.target_count}`,
    `监控 ${facts.running_monitoring_count}/${facts.monitoring_link_count}`,
    currentFactsIPQualityLabel(facts),
  ].filter(Boolean).join(' · ')
}

export function currentFactsStateLabel(facts?: AssetDecisionExecutionCurrentFacts): string {
  if (!facts) return '等待资产聚合事实'
  if (!facts.found) return '资产聚合中未找到当前 VPS'
  return [
    facts.lifecycle_status ? lifecycleLabel(facts.lifecycle_status) : '',
    facts.usage_status ? usageLabel(facts.usage_status) : '',
    facts.renewal_decision ? renewalLabel(facts.renewal_decision) : '',
    facts.abnormal_monitoring_count > 0 ? `异常监控 ${facts.abnormal_monitoring_count}` : '',
    facts.active_incident_count > 0 ? `事件 ${facts.active_incident_count}` : '',
    ...currentFactsIPQualityStateLabels(facts),
  ].filter(Boolean).join(' · ') || '基础状态正常'
}

export function currentFactsIPQualityLabel(facts: AssetDecisionExecutionCurrentFacts): string {
  const summary = facts.ip_quality_summary
  if (!summary) return ''
  const parts = [`IP ${summary.ip_address}`]
  if (summary.status === 'success') {
    parts.push(ipQualityRiskLabel(summary.risk_level))
    const region = summary.use_region_code || summary.use_region_name
    if (region) parts.push(region)
  } else {
    parts.push('采集未完成')
  }
  return parts.filter(Boolean).join(' ')
}

export function currentFactsIPQualityStateLabels(facts: AssetDecisionExecutionCurrentFacts): string[] {
  const labels: string[] = []
  const summary = facts.ip_quality_summary
  if (summary?.ambiguous) labels.push('IP 归属不唯一')
  if (summary?.stale) labels.push('IP 质量过期')
  if (summary && summary.status !== 'success') labels.push(summary.error_summary || 'IP 采集未完成')
  if ((facts.ip_quality_provider_risk_signal_count ?? 0) > 0) {
    labels.push(`风险 provider ${facts.ip_quality_provider_risk_signal_count}`)
  }
  if (facts.ip_quality_blocked_services && facts.ip_quality_blocked_services.length > 0) {
    const visible = facts.ip_quality_blocked_services.slice(0, 3).map(ipQualityServiceLabel)
    const suffix = facts.ip_quality_blocked_services.length > visible.length
      ? ` 等 ${facts.ip_quality_blocked_services.length} 项`
      : ''
    labels.push(`受阻 ${visible.join('、')}${suffix}`)
  }
  return labels
}

export function ipQualityRiskLabel(value?: string): string {
  const risk = (value ?? '').trim().toLowerCase()
  if (risk === 'low' || risk === 'clean' || risk === 'safe') return '低风险'
  if (risk === 'medium' || risk === 'moderate') return '中风险'
  if (risk === 'high') return '高风险'
  if (risk === 'critical') return '严重风险'
  return risk || '未评级'
}

export function ipQualityServiceLabel(value: string): string {
  const normalized = value.trim().toLowerCase()
  if (normalized === 'chatgpt' || normalized === 'openai') return 'ChatGPT'
  if (normalized === 'netflix') return 'Netflix'
  if (normalized === 'youtube-premium') return 'YouTube Premium'
  if (normalized === 'amazon-prime-video') return 'Amazon Prime Video'
  if (normalized === 'disney-plus') return 'Disney+'
  if (normalized === 'tiktok') return 'TikTok'
  if (normalized === 'reddit') return 'Reddit'
  return value
}

// === 成员执行计划和回读摘要 ===

export function compactMemberReadbackSummary(readback?: AssetDecisionMemberExecutionReadback): string {
  if (!readback) return '等待当前事实回读'
  return compactDecisionText(readback.summary || currentFactsLabel(readback.current_facts), '等待执行回读')
}

export function compactMemberPlanSummary(member: AssetDecisionRecordMember): string {
  const plan = member.execution_plan
  return compactDecisionText(plan?.summary || actionLabelForMember(member), '等待下一步')
}

export function actionLabelForMember(member: AssetDecisionRecordMember): string {
  const plan = member.execution_plan
  if (
    plan?.lane === 'migration'
    || plan?.step_label?.includes('推进迁移')
    || plan?.step_label?.includes('迁移流程')
    || plan?.step_label?.includes('迁移工作台')
  ) {
    return '复核迁移意向'
  }
  if (plan?.step_label) return plan.step_label
  if (member.decided_action === 'open_cancellation_workbench' || member.decided_action === 'cancel') return '取消/退役'
  return 'VPS 详情'
}

// === 紧凑文本格式化 ===

export function normalizeDecisionText(value?: string | null): string {
  return (value ?? '').replace(/\s+/g, ' ').trim()
}

export function compactDecisionText(value: string | null | undefined, fallback: string, maxLength = 18): string {
  const normalized = normalizeDecisionText(value) || fallback
  const firstSentence = normalized.split(/[。！？!?]/)[0]?.trim() || normalized
  if (firstSentence.length <= maxLength) return firstSentence
  return `${firstSentence.slice(0, maxLength)}…`
}

export function compactDirectoryMeta(value: string | null | undefined, fallback: string, maxLength = 12): string {
  const normalized = normalizeDecisionText(value)
  if (!normalized) return fallback
  if (/(?:adg|admg|adr|adt|adtm)_/i.test(normalized)) return fallback
  const firstSegment = normalized.split(/[。！？!?·/]/)[0]?.trim() || normalized
  if (firstSegment.length <= maxLength) return firstSegment
  return `${firstSegment.slice(0, maxLength)}…`
}

// === 组合和记录标签 ===

export function compactGroupJudgement(group: Pick<AssetDecisionGroupSummary, 'view' | 'evidence_assessment'>): string {
  if (group.evidence_assessment.quality_tier === 'blocked') return '证据阻塞，先补齐'
  if (group.evidence_assessment.gap_signal_count > 0) return '资料缺口，先复核'
  if (group.view === 'cost') return '预算压力，先比价'
  if (group.view === 'renewal') return '续费临近，先分层'
  if (group.view === 'region') return '同区比较，先排序'
  if (group.view === 'provider') return '服务商组合，先取舍'
  if (group.view === 'evidence') return '资料缺口，先补证'
  return '需要决策'
}

export function groupPressureLabel(group: AssetDecisionGroupSummary): string {
  const parts = [
    group.renewal_window_count > 0 ? `续费窗口 ${group.renewal_window_count}` : '',
    group.unreviewed_count > 0 ? `未评估 ${group.unreviewed_count}` : '',
    group.cancellation_attention_count > 0 ? `取消联动 ${group.cancellation_attention_count}` : '',
    group.evidence_assessment.gap_signal_count > 0 ? `缺口 ${group.evidence_assessment.gap_signal_count}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '暂无高压信号'
}

export function recordSourceLabel(record: Pick<AssetDecisionRecordSummary, 'source_type' | 'source_group_id' | 'source_group_type' | 'scope_label'>): string {
  const scope = record.scope_label || record.source_group_id
  if (record.source_type === 'manual_group') return `来自自定义组合 ${scope}`
  if (record.source_type === 'auto_group') return `来自自动组 ${scope}`
  return `来源 ${scope}`
}

export function recordSourceDetail(record: AssetDecisionRecordSummary): string {
  const sourceType = record.source_type === 'manual_group' ? '自定义组合' : record.source_type === 'auto_group' ? '自动组' : record.source_type
  const sourceView = VIEW_LABELS[record.source_view] ?? record.source_view
  return `${sourceType} · ${sourceView} · ${record.scope_label || '当前来源'}`
}

export function parseComparisonInsight(snapshot?: AssetDecisionEvidenceSnapshot | null) {
  // 此函数需要完整的解析逻辑，暂时作为占位
  // 完整实现在 mappers.ts 中
  return snapshot?.comparison_insight ?? null
}

export function recordCoverSummary(detail: AssetDecisionRecordDetail): string {
  const insight = parseComparisonInsight(detail.evidence_snapshot)
  return compactDecisionText((insight as any)?.summary
    || detail.execution_readback?.summary
    || `保存时判断：${detail.goal || '等待补齐组合判断'}`, '保存判断待复核')
}

export function recordCoverMeta(detail: AssetDecisionRecordDetail): string {
  const readback = detail.execution_readback?.status ? READBACK_STATUS_LABELS[detail.execution_readback.status] : '等待回读'
  return `${RECORD_STATUS_LABELS[detail.status]} · 跟进 ${recordFollowupDoneCount(detail)}/${detail.member_count} · ${readback}`
}

type ManualGroupProgress = {
  readinessLabel: string
  readinessTone: BadgeTone
  readyToRecord: boolean
  doneCount: number
  totalCount: number
  items: Array<{ key: string; label: string; summary: string; tone: BadgeTone; done: boolean }>
}

export function manualCoverMeta(detail: AssetDecisionManualGroupDetail, progress: ManualGroupProgress): string {
  return `${MANUAL_GROUP_SCENARIO_LABELS[detail.scenario]} · ${MANUAL_GROUP_STATUS_LABELS[detail.status]} · ${progress.readinessLabel} ${progress.doneCount}/${progress.totalCount}`
}

export function compactVPSOptionLabel(vps: VPSAssetRecord): string {
  const location = [vps.country, vps.city].filter(Boolean).join(' · ')
  return location ? `${vps.display_name} · ${location}` : vps.display_name
}

// === Portfolio 标签 ===

type RenewalWindow = 30 | 60 | 90
type MainWorkbenchView = AssetDecisionView

type ContextFilterChip = {
  key: string
  label: string
  value: string
}

export function portfolioContextLabel(chips: ContextFilterChip[], view: MainWorkbenchView, renewalWindow: RenewalWindow): string {
  if (chips.length === 0) {
    return `全局资产组合 · ${VIEW_LABELS[view]} · ${renewalWindow} 天续费窗口`
  }
  return chips.map((chip) => `${chip.label} ${chip.value}`).join(' / ')
}

type ClosedLoopMetrics = {
  autoGroupCount: number
  manualActiveCount: number
  recordActiveCount: number
  readbackDriftCount: number
  readbackBlockedCount: number
  readbackNeedsEvidenceCount: number
  readbackOpenCount: number
  costPressureGroupCount: number
  evidenceGapGroupCount: number
  partialErrorCount: number
}

export function portfolioRiskLabel(metrics: ClosedLoopMetrics): string {
  if (metrics.partialErrorCount > 0) return '证据待确认'
  if (metrics.readbackDriftCount > 0) return `事实漂移 ${metrics.readbackDriftCount}`
  if (metrics.readbackBlockedCount > 0) return `阻塞 ${metrics.readbackBlockedCount}`
  const gapCount = metrics.readbackNeedsEvidenceCount + metrics.evidenceGapGroupCount
  if (gapCount > 0) return `资料缺口 ${gapCount}`
  if (metrics.readbackOpenCount > 0) return `待回读 ${metrics.readbackOpenCount}`
  if (metrics.recordActiveCount > 0) return `跟进中 ${metrics.recordActiveCount}`
  return '闭环稳定'
}

export function portfolioEvidenceLabel(overview?: AssetDecisionOverview | null): string {
  if (!overview) return '等待资产证据聚合'
  return sourceAvailabilityLabel(overview.source_availability)
}

export function portfolioRenewalLabel(overview: AssetDecisionOverview | null | undefined, renewalWindow: RenewalWindow): string {
  if (!overview) return `${renewalWindow} 天窗口等待聚合`
  return `${renewalWindow} 天窗口 · 续费组 ${overview.renewal_group_count} · 待决策 ${overview.needs_decision_count}`
}
