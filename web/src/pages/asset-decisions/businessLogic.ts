/**
 * 业务逻辑函数模块
 *
 * 包含资产决策的核心业务逻辑：
 * - 决策队列构建和更新
 * - 闭环指标计算
 * - 下一步工作项推导
 * - 组合引导信息构建
 * - 手动组合进度计算
 */

import type {
  AssetDecisionGroupSummary,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupMember,
  AssetDecisionManualGroupSummary,
  AssetDecisionOverview,
  AssetDecisionRecordSummary,
  SubscriptionRecord,
  VPSAssetRecord,
} from '../../lib/types'
import { VIEW_LABELS } from './constants'
import type {
  AssetDecisionNextWorkItem,
  AssetDecisionPortfolioLead,
  ClosedLoopMetrics,
  ClosedLoopSourceErrors,
  ContextFilterChip,
  DecisionQueueItem,
  MainWorkbenchView,
  ManualGroupProgress,
  QueueState,
  RenewalWindow,
} from './types'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  isSubscriptionInRenewalWindow,
  selectPrimarySubscription,
  sourceAvailabilityLabel,
} from './utils'

/**
 * 计算队列优先级
 */
function queuePriority(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
  qualityIssues: ReturnType<typeof buildVPSQualityIssues>,
  renewalDue: boolean,
): number {
  let priority = 0
  if (vps.renewal_decision === 'unreviewed') priority += 500
  if (renewalDue) priority += 300
  if (vps.renewal_decision === 'migrate' || vps.renewal_decision === 'cancel') priority += 180
  if (subscription?.exchange_rate_stale) priority += 60
  if (vps.active_monitoring_instance_link_count <= 0) priority += 90
  if (!subscription) priority += 80
  return priority + qualityIssues.length * 8
}

/**
 * 构建决策队列
 */
export function buildDecisionQueue(
  vpsRows: VPSAssetRecord[],
  subscriptionsByVPS: Map<string, SubscriptionRecord[]>,
  renewalWindow: RenewalWindow,
): DecisionQueueItem[] {
  const uniqueRows = new Map<string, VPSAssetRecord>()
  for (const vps of vpsRows) uniqueRows.set(vps.vps_id, vps)

  return [...uniqueRows.values()]
    .map((vps) => {
      const subscription = selectPrimarySubscription(subscriptionsByVPS, vps.vps_id)
      const qualityIssues = buildVPSQualityIssues(vps, subscription)
      const renewalDue = isSubscriptionInRenewalWindow(subscription, renewalWindow)
      return {
        vps,
        subscription,
        qualityIssues,
        renewalDue,
        priority: queuePriority(vps, subscription, qualityIssues, renewalDue),
      }
    })
    .sort((left, right) => {
      if (left.priority !== right.priority) return right.priority - left.priority
      const leftDays = daysUntilDate(left.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      const rightDays = daysUntilDate(right.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      if (leftDays !== rightDays) return leftDays - rightDays
      return left.vps.display_name.localeCompare(right.vps.display_name)
    })
}

/**
 * 更新决策队列
 */
export function updateDecisionQueues(
  state: QueueState,
  updated: VPSAssetRecord,
): Pick<QueueState, 'unreviewed' | 'migrate' | 'cancel'> {
  const next = {
    unreviewed: state.unreviewed.filter((vps) => vps.vps_id !== updated.vps_id),
    migrate: state.migrate.filter((vps) => vps.vps_id !== updated.vps_id),
    cancel: state.cancel.filter((vps) => vps.vps_id !== updated.vps_id),
  }

  if (updated.renewal_decision === 'unreviewed') next.unreviewed = [updated, ...next.unreviewed]
  if (updated.renewal_decision === 'migrate') next.migrate = [updated, ...next.migrate]
  if (updated.renewal_decision === 'cancel') next.cancel = [updated, ...next.cancel]

  return next
}

/**
 * 计算跟进未关闭数量
 */
function recordFollowupOpenCount(record: AssetDecisionRecordSummary): number {
  return (record.followup_todo_count ?? 0) + (record.followup_in_progress_count ?? 0) + (record.followup_blocked_count ?? 0)
}

/**
 * 生成回读计数摘要
 */
function readbackCountSummary(readback?: AssetDecisionRecordSummary['execution_readback']): string {
  if (!readback) return '等待回读'
  const parts = [
    readback.drift_count > 0 ? `漂移 ${readback.drift_count}` : '',
    readback.blocked_count > 0 ? `阻塞 ${readback.blocked_count}` : '',
    readback.needs_evidence_count > 0 ? `缺口 ${readback.needs_evidence_count}` : '',
    readback.open_count > 0 ? `待处理 ${readback.open_count}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : `对齐 ${readback.aligned_count ?? 0}`
}

/**
 * 推导闭环指标
 */
export function deriveClosedLoopMetrics(
  groups: AssetDecisionGroupSummary[],
  records: AssetDecisionRecordSummary[],
  manualGroups: AssetDecisionManualGroupSummary[],
  sourceErrors: ClosedLoopSourceErrors,
  overview?: AssetDecisionOverview | null,
): ClosedLoopMetrics {
  return {
    autoGroupCount: groups.length,
    manualActiveCount: manualGroups.filter((group) => group.status === 'active').length,
    recordActiveCount: records.filter((record) => record.status !== 'completed' && record.status !== 'abandoned').length,
    readbackDriftCount: records.reduce((total, record) => total + (record.execution_readback?.drift_count ?? 0), 0),
    readbackBlockedCount: records.reduce(
      (total, record) => total + (record.execution_readback?.blocked_count ?? 0) + (record.followup_blocked_count ?? 0),
      0,
    ),
    readbackNeedsEvidenceCount: records.reduce((total, record) => total + (record.execution_readback?.needs_evidence_count ?? 0), 0),
    readbackOpenCount: records.reduce((total, record) => total + (record.execution_readback?.open_count ?? 0), 0),
    costPressureGroupCount: overview?.cost_group_count ?? groups.filter((group) => group.view === 'cost' || group.group_type === 'cost_pressure').length,
    evidenceGapGroupCount: overview?.evidence_group_count ?? groups.filter((group) => group.view === 'evidence' || group.group_type === 'evidence_gap').length,
    partialErrorCount: Object.values(sourceErrors).filter(Boolean).length,
  }
}

/**
 * 推导下一步工作项
 */
export function deriveNextWorkItems(
  groups: AssetDecisionGroupSummary[],
  records: AssetDecisionRecordSummary[],
  sourceErrors: ClosedLoopSourceErrors,
): AssetDecisionNextWorkItem[] {
  const items: AssetDecisionNextWorkItem[] = []

  if (!sourceErrors.records) {
    for (const record of records) {
      const readback = record.execution_readback
      if (!readback) continue
      const scope = record.scope_label || record.source_group_id
      if (readback.drift_count > 0 || readback.status === 'drift') {
        items.push({
          id: `record-drift-${record.record_id}`,
          kind: 'record_drift',
          tone: 'critical',
          sourceLabel: '保存记录',
          kindLabel: '事实漂移',
          title: record.title,
          summary: readback.summary || '当前事实与已保存判断不一致，需要复核执行闭环。',
          meta: `${scope} · ${readbackCountSummary(readback)} · 跟进未关闭 ${recordFollowupOpenCount(record)}`,
          actionLabel: '复核记录',
          priority: 1000 + readback.drift_count * 16 + recordFollowupOpenCount(record),
          target: { type: 'record', id: record.record_id },
        })
      } else if (readback.blocked_count > 0 || record.followup_blocked_count > 0 || readback.status === 'blocked') {
        items.push({
          id: `record-blocked-${record.record_id}`,
          kind: 'record_blocked',
          tone: 'critical',
          sourceLabel: '保存记录',
          kindLabel: '跟进阻塞',
          title: record.title,
          summary: readback.summary || '记录中仍有成员阻塞，需要人工解除或调整跟进路径。',
          meta: `${scope} · 阻塞 ${(readback.blocked_count ?? 0) + (record.followup_blocked_count ?? 0)} · 未关闭 ${recordFollowupOpenCount(record)}`,
          actionLabel: '处理阻塞',
          priority: 900 + (readback.blocked_count ?? 0) * 12 + (record.followup_blocked_count ?? 0) * 12,
          target: { type: 'record', id: record.record_id },
        })
      } else if (readback.needs_evidence_count > 0 || readback.status === 'needs_evidence') {
        items.push({
          id: `record-needs-evidence-${record.record_id}`,
          kind: 'record_needs_evidence',
          tone: 'alert',
          sourceLabel: '保存记录',
          kindLabel: '回读缺证据',
          title: record.title,
          summary: readback.summary || '当前记录仍有证据缺口，先补齐资料再推进判断。',
          meta: `${scope} · ${readbackCountSummary(readback)} · 成员 ${record.member_count}`,
          actionLabel: '补证据',
          priority: 800 + readback.needs_evidence_count * 10,
          target: { type: 'record', id: record.record_id },
        })
      }
    }
  }

  if (!sourceErrors.groups) {
    for (const group of groups) {
      const pressure = group.evidence_assessment.pressure_score + group.evidence_assessment.gap_signal_count * 4
      items.push({
        id: `auto-group-${group.group_id}`,
        kind: 'auto_group',
        tone: group.evidence_assessment.quality_tier === 'blocked' ? 'alert' : 'notice',
        sourceLabel: '自动组合',
        kindLabel: VIEW_LABELS[group.view],
        title: group.title,
        summary: group.decision_recommendation?.summary || group.primary_issue_summary || '打开自动组比较成员、成本、服务承载和证据缺口。',
        meta: `${group.scope_label} · ${group.member_count} 台 VPS · 续费 ${group.renewal_window_count} · 缺口 ${group.evidence_assessment.gap_signal_count}`,
        actionLabel: '打开决策组',
        priority: 620 + group.priority + pressure,
        target: { type: 'group', id: group.group_id },
      })
    }
  }

  return items
    .sort((left, right) => {
      if (left.priority !== right.priority) return right.priority - left.priority
      return left.title.localeCompare(right.title)
    })
    .slice(0, 6)
}

/**
 * 生成组合上下文标签
 */
function portfolioContextLabel(chips: ContextFilterChip[], view: MainWorkbenchView, renewalWindow: RenewalWindow): string {
  if (chips.length === 0) {
    return `全局资产组合 · ${VIEW_LABELS[view]} · ${renewalWindow} 天续费窗口`
  }
  return chips.map((chip) => `${chip.label} ${chip.value}`).join(' / ')
}

/**
 * 生成组合风险标签
 */
function portfolioRiskLabel(metrics: ClosedLoopMetrics): string {
  if (metrics.partialErrorCount > 0) return '证据待确认'
  if (metrics.readbackDriftCount > 0) return `事实漂移 ${metrics.readbackDriftCount}`
  if (metrics.readbackBlockedCount > 0) return `阻塞 ${metrics.readbackBlockedCount}`
  const gapCount = metrics.readbackNeedsEvidenceCount + metrics.evidenceGapGroupCount
  if (gapCount > 0) return `资料缺口 ${gapCount}`
  if (metrics.readbackOpenCount > 0) return `待回读 ${metrics.readbackOpenCount}`
  if (metrics.recordActiveCount > 0) return `跟进中 ${metrics.recordActiveCount}`
  return '闭环稳定'
}

/**
 * 生成组合证据标签
 */
function portfolioEvidenceLabel(overview?: AssetDecisionOverview | null): string {
  if (!overview) return '等待资产证据聚合'
  return sourceAvailabilityLabel(overview.source_availability)
}

/**
 * 生成组合续费标签
 */
function portfolioRenewalLabel(overview: AssetDecisionOverview | null | undefined, renewalWindow: RenewalWindow): string {
  if (!overview) return `${renewalWindow} 天窗口等待聚合`
  return `${renewalWindow} 天窗口 · 续费组 ${overview.renewal_group_count} · 待决策 ${overview.needs_decision_count}`
}

/**
 * 构建组合引导信息
 */
export function buildPortfolioLead(
  view: MainWorkbenchView,
  renewalWindow: RenewalWindow,
  overview: AssetDecisionOverview | null,
  groups: AssetDecisionGroupSummary[],
  nextWorkItems: AssetDecisionNextWorkItem[],
  metrics: ClosedLoopMetrics,
  contextChips: ContextFilterChip[],
): AssetDecisionPortfolioLead {
  const first = nextWorkItems[0]
  const contextLabel = portfolioContextLabel(contextChips, view, renewalWindow)
  const riskLabel = portfolioRiskLabel(metrics)
  const evidenceLabel = portfolioEvidenceLabel(overview)
  const renewalLabelText = portfolioRenewalLabel(overview, renewalWindow)
  const fallbackGroup = groups[0]
  if (first) {
    return {
      kind: 'work',
      tone: first.tone,
      eyebrow: first.sourceLabel,
      title: `${first.kindLabel} · ${first.title}`,
      summary: first.summary,
      actionLabel: first.actionLabel,
      contextLabel,
      riskLabel,
      evidenceLabel,
      renewalLabel: renewalLabelText,
      primaryItem: first,
    }
  }
  if (fallbackGroup) {
    return {
      kind: 'work',
      tone: fallbackGroup.evidence_assessment.gap_signal_count > 0 ? 'alert' : 'notice',
      eyebrow: '自动组合',
      title: `先比较 ${fallbackGroup.title}`,
      summary: fallbackGroup.decision_recommendation?.summary || fallbackGroup.primary_issue_summary || '当前视图有可比较的自动决策组，先打开组详情核对成员事实。',
      actionLabel: '打开决策组',
      contextLabel,
      riskLabel,
      evidenceLabel,
      renewalLabel: renewalLabelText,
      primaryGroupID: fallbackGroup.group_id,
    }
  }
  if (metrics.partialErrorCount === 0) {
    return {
      kind: 'stable',
      tone: 'normal',
      eyebrow: '当前判断',
      title: '当前没有需要处理的组合决策',
      summary: '已加载视图内暂无待处理项；历史记录、场景模板和单台队列可按需打开。',
      contextLabel,
      riskLabel,
      evidenceLabel,
      renewalLabel: renewalLabelText,
    }
  }
  return {
    kind: 'work',
    tone: 'alert',
    eyebrow: '当前判断',
    title: '部分资产决策证据不可用',
    summary: '请先查看局部错误边界；已加载的自动组、记录和单台辅助队列仍可继续处理。',
    actionLabel: '查看已加载决策组',
    contextLabel,
    riskLabel,
    evidenceLabel,
    renewalLabel: renewalLabelText,
  }
}

/**
 * 检查成员意图是否已设置
 */
function memberIntentIsSet(member: AssetDecisionManualGroupMember): boolean {
  return Boolean(member.intended_role && member.intended_action && member.intended_action !== 'review')
}

/**
 * 构建手动组合进度
 */
export function buildManualGroupProgress(detail: AssetDecisionManualGroupDetail): ManualGroupProgress {
  const hasGoal = Boolean(detail.goal.trim() || detail.title.trim())
  const hasMembers = detail.members.length > 0
  const intentReadyCount = detail.members.filter(memberIntentIsSet).length
  const intentReady = hasMembers && intentReadyCount === detail.members.length
  const evidenceReady = detail.evidence_assessment.quality_tier !== 'blocked' && detail.evidence_assessment.gap_signal_count === 0
  const currentFactMissingCount = detail.members.filter((member) => !member.current_fact_found).length
  const factReady = currentFactMissingCount === 0
  const readyToRecord = hasGoal && hasMembers && intentReady && evidenceReady && factReady
  const items = [
    {
      key: 'goal',
      label: '目标',
      summary: hasGoal ? detail.goal || detail.title : '补齐组合目标后再沉淀判断',
      tone: hasGoal ? 'normal' as const : 'alert' as const,
      done: hasGoal,
    },
    {
      key: 'members',
      label: '成员',
      summary: hasMembers ? `${detail.members.length} 台 VPS 已加入比较` : '至少加入一台 VPS',
      tone: hasMembers ? 'normal' as const : 'alert' as const,
      done: hasMembers,
    },
    {
      key: 'intent',
      label: '意图',
      summary: hasMembers ? `已设置 ${intentReadyCount}/${detail.members.length} 个成员动作` : '等待成员后设置角色和动作',
      tone: intentReady ? 'normal' as const : hasMembers ? 'notice' as const : 'neutral' as const,
      done: intentReady,
    },
    {
      key: 'evidence',
      label: '证据',
      summary: evidenceReady ? detail.evidence_assessment.summary : `仍有 ${detail.evidence_assessment.gap_signal_count} 个资料缺口`,
      tone: evidenceReady ? 'normal' as const : detail.evidence_assessment.quality_tier === 'blocked' ? 'critical' as const : 'alert' as const,
      done: evidenceReady,
    },
    {
      key: 'facts',
      label: '当前事实',
      summary: factReady ? '成员当前事实可回读' : `${currentFactMissingCount} 个成员缺少当前事实`,
      tone: factReady ? 'normal' as const : 'critical' as const,
      done: factReady,
    },
  ]
  const doneCount = items.filter((item) => item.done).length
  return {
    readinessLabel: readyToRecord ? '可保存记录' : doneCount >= 3 ? '接近可保存' : '继续整理',
    readinessTone: readyToRecord ? 'normal' : doneCount >= 3 ? 'maintenance' : 'alert',
    readyToRecord,
    doneCount,
    totalCount: items.length,
    items,
  }
}
