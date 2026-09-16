import { formatDate, formatMoney, formatOptional } from '../../lib/format'
import { periodLabel, renewalModeLabel } from '../../lib/assetOptions'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  SubscriptionRecord,
  VPSAssetDetail,
  VPSIPQualityReport,
  VPSTimeline,
} from '../../lib/types'
import {
  deriveQualityScore,
  serviceUnlockCounts,
  strongestRiskFlags,
} from '../../components/ip-quality/ipQualityPresentation'
import {
  daysUntilDate,
  lifecycleLabel,
  renewalLabel,
  usageLabel,
} from '../assetPageUtils'
import { primaryMonitoringInstance } from './vpsDecisionModel'

import type { VPSDetailModalMode } from './types'

export type VPSOverviewTone = 'normal' | 'notice' | 'alert' | 'critical'

export type VPSOverviewDomain = 'identity' | 'ops' | 'monitoring' | 'relations' | 'activity'

export type VPSOverviewFact = {
  label: string
  value: string
  meta?: string
  tone?: VPSOverviewTone
  domain: VPSOverviewDomain
  copyValue?: string
}


export type VPSOverviewAction = {
  kind: 'modal' | 'link'
  label: string
  mode?: NonNullable<VPSDetailModalMode>
  to?: string
}

export type VPSContextAction = {
  title: string
  reason: string
  tone: VPSOverviewTone
  domain: VPSOverviewDomain
  primaryAction: VPSOverviewAction
  secondaryActions: VPSOverviewAction[]
}

export type VPSRelatedOverviewItem = {
  key: 'subscription' | 'monitoring' | 'ip-quality'
  domain: VPSOverviewDomain
  title: string
  tone: VPSOverviewTone
  primary: string
  secondary?: string
  titleAction: { kind: 'link'; to: string } | { kind: 'modal'; mode: NonNullable<VPSDetailModalMode> }
  quickActions: VPSOverviewAction[]
}

export type VPSRecentActivityItem = {
  key: string
  date: string
  kind: string
  summary: string
}

export type VPSIPQualityOverviewModel = {
  status: 'ready' | 'empty' | 'error'
  titleValue: string
  verdict: string
  riskSummary: string
  unlockSummary: string
  observedAt?: string
  reportTo: string
}

export type VPSDetailOverviewModel = {
  title: string
  badges: string[]
  updatedAt: string
  monitoringFreshness: {
    lastHeartbeatAt: string | null
    lastSyncAt: string | null
  } | null
  facts: VPSOverviewFact[]
  judgement: {
    tone: VPSOverviewTone
    rows: Array<{ label: string; value: string }>
    attentionItems: VPSContextAction[]
    primaryAction: VPSOverviewAction | null
  }
  monitoringAttentionItems: VPSContextAction[]
  relatedItems: VPSRelatedOverviewItem[]
  recentActivity: VPSRecentActivityItem[]

  ipOverview: VPSIPQualityOverviewModel
}

export type VPSDetailOverviewModelInput = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  activeSubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  ipQuality: VPSIPQualityReport | null
  ipQualityError: string | null
  cancellationAttention?: boolean
}

export function renewalDueLabel(subscription: SubscriptionRecord | null): string {
  if (!subscription?.renew_at && !subscription?.ends_at) return '尚无续费日'

  const targetDate = subscription.auto_renew_cancelled
    ? subscription.ends_at ?? subscription.renew_at
    : subscription.renew_at
  const unitLabel = relativeDayMonthLabel(daysUntilDate(targetDate))

  if (subscription.auto_renew_cancelled) {
    return `已取消自动续费 · ${unitLabel}到期`
  }
  return `${unitLabel}续费`
}

function relativeDayMonthLabel(days: number | null): string {
  if (days == null) return '未知时间'
  if (days < 0) return `已过期 ${Math.abs(days)} 天`
  if (days === 0) return '今天'
  if (days <= 30) return `${days} 天后`
  return `${Math.ceil(days / 30)} 个月后`
}

export function buildVPSDetailOverviewModel(input: VPSDetailOverviewModelInput): VPSDetailOverviewModel {
  const monitoringInstance = primaryMonitoringInstance(input.detail)
  const monitoringFreshness = monitoringInstance
    ? {
        lastHeartbeatAt: monitoringInstance.last_heartbeat_at ?? null,
        lastSyncAt: monitoringInstance.last_sync_at ?? null,
      }
    : null
  const subscriptionSummary = buildSubscriptionSummary(input.primarySubscription, input.subscriptionLoadFailed, input.subscriptionError)
  const ipOverview = buildIPQualityOverview(input.detail, input.ipQuality, input.ipQualityError)
  const recentActivity = buildRecentActivity(input.timeline)
  const relatedItems = buildRelatedItems(input, subscriptionSummary, ipOverview)

  const cancellationWork = input.cancellationAttention ?? needsCancellationWork(input.detail)
  const attentionItems = buildAttentionItems(input, subscriptionSummary, ipOverview, cancellationWork)
  const opsAttention = attentionItems.filter((item) => item.domain === 'ops')
  const monitoringAttentionItems = attentionItems.filter((item) => item.domain === 'monitoring')

  return {
    title: input.detail.display_name,
    updatedAt: input.detail.updated_at,
    monitoringFreshness,
    badges: [
      lifecycleLabel(input.detail.lifecycle_status),
      usageLabel(input.detail.usage_status),
      renewalLabel(input.detail.renewal_decision),
      `${input.detail.active_monitoring_instance_link_count} 个监控实例`,
    ],
    facts: [
      { domain: 'identity', label: '服务商', value: formatOptional(input.detail.provider_name) },
      { domain: 'identity', label: '地区 / 数据中心', value: locationLabel(input.detail) },
      { domain: 'identity', label: '规格', value: formatOptional(input.detail.product_name) },
      ...copyableAccessFacts(input.detail),
      { domain: 'identity', label: '系统', value: formatOptional(input.detail.os_name) },
      { domain: 'identity', label: '重要性', value: formatOptional(input.detail.importance) },
      { domain: 'identity', label: '标签', value: input.detail.labels.length > 0 ? input.detail.labels.join(' · ') : '无标签' },
      { domain: 'identity', label: '备注', value: input.detail.note || '未记录' },
      { domain: 'monitoring', label: '监控', value: monitoringFactValue(input.detail), tone: monitoringTone(monitoringInstance) },
      { domain: 'monitoring', label: 'IP 质量', value: ipOverview.titleValue, tone: ipOverviewTone(ipOverview) },
    ],
    judgement: {
      tone: strongestOverviewTone(opsAttention.map((item) => item.tone)),
      rows: [
        { label: '决策', value: renewalLabel(input.detail.renewal_decision) },
        { label: '续费', value: input.primarySubscription ? renewalDueLabel(input.primarySubscription) : subscriptionSummary.shortValue },
      ],
      attentionItems: opsAttention,
      primaryAction: cancellationWork
        ? { kind: 'modal', label: '处理取消/退役', mode: 'cancellation' }
        : null,
    },
    monitoringAttentionItems,
    recentActivity,
    relatedItems,
    ipOverview,
  }
}

function strongestOverviewTone(tones: VPSOverviewTone[]): VPSOverviewTone {
  if (tones.includes('critical')) return 'critical'
  if (tones.includes('alert')) return 'alert'
  if (tones.includes('notice')) return 'notice'
  return 'normal'
}

type SubscriptionSummary = {
  tone: VPSOverviewTone
  factValue: string
  shortValue: string
  relatedPrimary: string
  relatedSecondary: string
}

function buildSubscriptionSummary(
  subscription: SubscriptionRecord | null,
  loadFailed: boolean,
  error: string | null,
): SubscriptionSummary {
  if (loadFailed) {
    return {
      tone: 'notice',
      factValue: '订阅证据暂不可用',
      shortValue: '订阅未知',
      relatedPrimary: '订阅证据暂不可用',
      relatedSecondary: error ?? '读取失败，暂不判断缺订阅',
    }
  }
  if (!subscription) {
    return {
      tone: 'critical',
      factValue: '未记录当前订阅',
      shortValue: '缺订阅',
      relatedPrimary: '未记录当前订阅',
      relatedSecondary: '需要补齐成本和续费日',
    }
  }
  return {
    tone: renewalToneForSubscription(subscription),
    factValue: `${formatMoney(subscription.monthly_price, subscription.currency)} · ${renewalDueLabel(subscription)}`,
    shortValue: renewalDueLabel(subscription),
    relatedPrimary: formatMoney(subscription.monthly_price, subscription.currency),
    relatedSecondary: renewalDueLabel(subscription),
  }
}

function renewalToneForSubscription(subscription: SubscriptionRecord): VPSOverviewTone {
  const days = daysUntilDate(subscription.auto_renew_cancelled ? subscription.ends_at ?? subscription.renew_at : subscription.renew_at)
  if (days != null && days <= 7) return 'critical'
  if (days != null && days <= 30) return 'notice'
  if (subscription.auto_renew_cancelled) return 'notice'
  return 'normal'
}

function buildRelatedItems(
  input: VPSDetailOverviewModelInput,
  subscriptionSummary: SubscriptionSummary,
  ipOverview: VPSIPQualityOverviewModel,
): VPSRelatedOverviewItem[] {
  const monitoringInstance = primaryMonitoringInstance(input.detail)
  return [
    {
      key: 'subscription',
      domain: 'ops',
      title: '订阅',
      tone: subscriptionSummary.tone,
      primary: subscriptionSummary.relatedPrimary,
      secondary: subscriptionSummary.relatedSecondary,
      titleAction: { kind: 'link', to: `/subscriptions?vps_id=${encodeURIComponent(input.detail.vps_id)}&view=details` },
      quickActions: input.subscriptionLoadFailed
        ? []
        : [
            { kind: 'modal', label: '新增订阅事实', mode: 'subscription' },
            { kind: 'modal', label: '延长', mode: 'validity-extension' },
          ],
    },
    {
      key: 'monitoring',
      domain: 'monitoring',
      title: '监控观测',
      tone: monitoringTone(monitoringInstance),
      primary: monitoringInstance ? `${input.detail.monitoring_instance_links.length} 个实例 · ${monitoringInstance.current_health_status}` : '未关联监控实例',
      secondary: monitoringInstance
        ? `${monitoringInstance.current_active_incident_count} 个活跃异常`
        : '缺少运行观测',
      titleAction: monitoringInstance && input.detail.monitoring_instance_links.length === 1
        ? {
            kind: 'link',
            to: `/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}?return_vps=${encodeURIComponent(input.detail.vps_id)}`,
          }
        : { kind: 'modal', mode: 'monitoring-instance-evidence' },
      quickActions: [
        { kind: 'modal', label: '接入/升级 agent', mode: 'monitoring-instance-create' },
        { kind: 'modal', label: '关联', mode: 'monitoring-instance-link' },
      ],
    },
    {
      key: 'ip-quality',
      domain: 'monitoring',
      title: 'IP 质量',
      tone: ipOverviewTone(ipOverview),
      primary: ipOverview.titleValue,
      secondary: ipOverview.riskSummary,
      titleAction: { kind: 'link', to: ipOverview.reportTo },
      quickActions: [],
    },
  ]
}

function buildAttentionItems(
  input: VPSDetailOverviewModelInput,
  subscriptionSummary: SubscriptionSummary,
  ipOverview: VPSIPQualityOverviewModel,
  cancellationWork: boolean,
): VPSContextAction[] {
  const monitoringInstance = primaryMonitoringInstance(input.detail)
  const items: VPSContextAction[] = []

  if (cancellationWork) {
    items.push({
      title: '取消/退役',
      reason: lifecycleLabel(input.detail.lifecycle_status),
      tone: 'critical',
      domain: 'ops',
      primaryAction: { kind: 'modal', label: '处理取消/退役', mode: 'cancellation' },
      secondaryActions: [],
    })
  }

  if (monitoringInstance && monitoringTone(monitoringInstance) !== 'normal') {
    items.push({
      title: '运行观测需要核对',
      reason: `${monitoringInstance.display_name} · ${monitoringInstance.current_active_incident_count} 个活跃异常`,
      tone: monitoringTone(monitoringInstance),
      domain: 'monitoring',
      primaryAction: {
        kind: 'link',
        label: '查看监控实例',
        to: `/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}?return_vps=${encodeURIComponent(input.detail.vps_id)}`,
      },
      secondaryActions: [{ kind: 'modal', label: '监控观测', mode: 'monitoring-instance-evidence' }],
    })
  }

  if (input.subscriptionLoadFailed) {
    items.push({
      title: '订阅证据暂不可用',
      reason: input.subscriptionError ?? '读取失败，暂不判断缺订阅',
      tone: 'notice',
      domain: 'ops',
      primaryAction: { kind: 'link', label: '核对订阅', to: `/subscriptions?vps_id=${encodeURIComponent(input.detail.vps_id)}&view=details` },
      secondaryActions: [],
    })
  } else if (!input.primarySubscription) {
    items.push({
      title: '缺少当前订阅',
      reason: '需要补齐成本和续费日',
      tone: 'critical',
      domain: 'ops',
      primaryAction: { kind: 'modal', label: '新增订阅事实', mode: 'subscription' },
      secondaryActions: [],
    })
  } else if (subscriptionSummary.tone === 'critical' || subscriptionSummary.tone === 'notice') {
    items.push({
      title: input.primarySubscription.auto_renew_cancelled ? '自动续费已取消' : '续费时间需要关注',
      reason: renewalDueLabel(input.primarySubscription),
      tone: subscriptionSummary.tone,
      domain: 'ops',
      primaryAction: { kind: 'modal', label: '调整决策', mode: 'decision' },
      secondaryActions: [{ kind: 'modal', label: '延长有效期', mode: 'validity-extension' }],
    })
  }

  if (!monitoringInstance) {
    items.push({
      title: '缺少运行观测',
      reason: '尚未关联监控实例',
      tone: 'alert',
      domain: 'monitoring',
      primaryAction: { kind: 'modal', label: '接入/升级 agent', mode: 'monitoring-instance-create' },
      secondaryActions: [{ kind: 'modal', label: '关联已有监控实例', mode: 'monitoring-instance-link' }],
    })
  }

  if (ipOverview.status === 'error') {
    items.push({
      title: 'IP 质量暂不可用',
      reason: ipOverview.verdict,
      tone: 'notice',
      domain: 'monitoring',
      primaryAction: { kind: 'link', label: '查看 IP 质量', to: ipOverview.reportTo },
      secondaryActions: [],
    })
  }

  return items
}

function needsCancellationWork(detail: VPSAssetDetail): boolean {
  return detail.renewal_decision === 'migrate' ||
    detail.renewal_decision === 'cancel' ||
    detail.renewal_decision === 'auto_renew_cancelled' ||
    detail.lifecycle_status === 'to_migrate' ||
    detail.lifecycle_status === 'to_cancel' ||
    detail.lifecycle_status === 'cancelled'
}


function changedTimelineValue(
  from: string | number | null | undefined,
  to: string | number | null | undefined,
): string {
  return `${formatOptional(from)} -> ${formatOptional(to)}`
}

function buildRecentActivity(timeline: VPSTimeline): VPSRecentActivityItem[] {
  const items: VPSRecentActivityItem[] = [
    ...timeline.experience_logs.map((log) => ({
      key: `experience:${log.experience_log_id}`,
      date: log.occurred_at,
      kind: '经验记录',
      summary: log.summary || '未记录经验',
    })),
    ...timeline.renewal_decisions.map((decision) => ({
      key: `renewal:${decision.decision_id}`,
      date: decision.decided_at,
      kind: '续费决策',
      summary: decision.reason || renewalLabel(decision.to_decision),
    })),
    ...timeline.price_histories.map((history) => ({
      key: `price:${history.price_history_id}`,
      date: history.changed_at,
      ...priceHistoryPresentation(history),
    })),
    ...timeline.ip_histories.map((history) => ({
      key: `ip:${history.ip_history_id}`,
      date: history.changed_at,
      kind: 'IP 变化',
      summary: `IPv4 ${changedTimelineValue(history.from_ipv4, history.to_ipv4)} · IPv6 ${changedTimelineValue(history.from_ipv6, history.to_ipv6)}`,
    })),
    ...timeline.spec_snapshots.map((snapshot) => ({
      key: `spec:${snapshot.snapshot_id}`,
      date: snapshot.captured_at,
      kind: '规格快照',
      summary: snapshot.product_name || '规格快照',
    })),
  ]

  return items
    .sort((left, right) => {
      const dateOrder = recentActivityTimestamp(right.date) - recentActivityTimestamp(left.date)
      return dateOrder || left.key.localeCompare(right.key)
    })
    .slice(0, 3)
}

function priceHistoryPresentation(history: VPSTimeline['price_histories'][number]): { kind: string; summary: string } {
  const changes: string[] = []
  const priceChanged = history.from_price !== history.to_price || history.from_currency !== history.to_currency
  if (priceChanged) {
    changes.push(`价格 ${formatMoney(history.from_price, history.from_currency)} -> ${formatMoney(history.to_price, history.to_currency)}`)
  }

  const fromCadence = periodLabel(
    history.from_billing_period_unit,
    history.from_billing_period_length,
    history.from_billing_months,
  )
  const toCadence = periodLabel(
    history.to_billing_period_unit,
    history.to_billing_period_length,
    history.to_billing_months,
  )
  if (fromCadence !== toCadence) {
    changes.push(`计费周期 ${fromCadence} -> ${toCadence}`)
  }

  if (history.from_renew_at !== history.to_renew_at) {
    changes.push(`续费日 ${formatDate(history.from_renew_at)} -> ${formatDate(history.to_renew_at)}`)
  }

  const fromRenewalMode = renewalModeLabel(history.from_renewal_mode)
  const toRenewalMode = renewalModeLabel(history.to_renewal_mode)
  if (fromRenewalMode !== toRenewalMode) {
    changes.push(`续费方式 ${fromRenewalMode} -> ${toRenewalMode}`)
  }

  return {
    kind: priceChanged ? '价格变化' : '账单更新',
    summary: changes.length > 0 ? changes.join(' · ') : '账单更新',
  }
}

function recentActivityTimestamp(value: string): number {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? Number.NEGATIVE_INFINITY : timestamp
}

function buildIPQualityOverview(
  detail: VPSAssetDetail,
  report: VPSIPQualityReport | null,
  error: string | null,
): VPSIPQualityOverviewModel {
  const reportTo = `/vps/${encodeURIComponent(detail.vps_id)}/ip-quality`
  if (error) {
    return {
      status: 'error',
      titleValue: '报告暂不可用',
      verdict: error,
      riskSummary: '报告暂不可用',
      unlockSummary: '—',
      reportTo,
    }
  }
  const summary = report?.summary ?? detail.ip_quality_summary ?? null
  if (!summary) {
    return {
      status: 'empty',
      titleValue: '尚无报告',
      verdict: '尚无 IP 质量报告',
      riskSummary: '无报告',
      unlockSummary: '—',
      reportTo,
    }
  }
  const score = report ? deriveQualityScore(report) : null
  const riskFlags = report ? strongestRiskFlags(report) : []
  const unlockCounts = report ? serviceUnlockCounts(report.service_unlocks) : null
  const riskCount = riskFlags.length
  const unlocked = unlockCounts?.unlocked ?? 0
  const titleValue = `${score ?? riskLevelSummary(summary.risk_level)} · ${riskCount} 风险 · ${unlocked} 可用`
  return {
    status: 'ready',
    titleValue,
    verdict: riskLevelSummary(summary.risk_level),
    riskSummary: riskFlags.length > 0 ? riskFlags.map((flag) => flag.label).join(' · ') : '无明显负面信号',
    unlockSummary: unlockCounts
      ? `${unlockCounts.unlocked} 可用 · ${unlockCounts.blocked} 受阻 · ${unlockCounts.partial} 部分 · ${unlockCounts.unknown} 未知`
      : `${summary.unlockable_count} 项可解锁证据`,
    observedAt: summary.observed_at,
    reportTo,
  }
}

function ipOverviewTone(overview: VPSIPQualityOverviewModel): VPSOverviewTone {
  if (overview.status === 'error' || overview.status === 'empty') return 'notice'
  if (overview.verdict === '严重风险') return 'critical'
  if (overview.verdict === '高风险') return 'alert'
  if (overview.verdict === '中风险') return 'notice'
  return 'normal'
}

function riskLevelSummary(value?: string): string {
  const normalized = (value ?? '').trim().toLowerCase()
  if (normalized === 'critical') return '严重风险'
  if (normalized === 'high') return '高风险'
  if (normalized === 'medium' || normalized === 'moderate') return '中风险'
  if (normalized === 'low' || normalized === 'clean' || normalized === 'safe') return '低风险'
  return '未评级'
}

function locationLabel(detail: VPSAssetDetail): string {
  return [detail.country, detail.region, detail.city, detail.datacenter].filter(Boolean).join(' · ') || '位置未确认'
}

function copyableAccessFacts(detail: VPSAssetDetail): VPSOverviewFact[] {
  const facts: VPSOverviewFact[] = []
  const ipv4 = detail.ipv4.trim()
  const ipv6 = detail.ipv6.trim()
  const host = detail.ssh_host.trim()
  if (ipv4) facts.push({ domain: 'identity', label: 'IPv4', value: ipv4, copyValue: ipv4 })
  if (ipv6) facts.push({ domain: 'identity', label: 'IPv6', value: ipv6, copyValue: ipv6 })
  if (host) {
    const ssh = `${detail.ssh_user.trim() || 'root'}@${host}:${detail.ssh_port || 22}`
    facts.push({ domain: 'identity', label: 'SSH', value: ssh, copyValue: ssh })
  }
  return facts
}


function monitoringFactValue(detail: VPSAssetDetail): string {
  const monitoringInstance = primaryMonitoringInstance(detail)
  if (!monitoringInstance) return `${detail.monitoring_instance_links.length} 个实例`
  return `${detail.monitoring_instance_links.length} 个实例 · ${monitoringInstance.current_health_status}`
}

function monitoringTone(monitoringInstance: ReturnType<typeof primaryMonitoringInstance>): VPSOverviewTone {
  if (!monitoringInstance) return 'alert'
  if (monitoringInstance.current_health_status === '严重') return 'critical'
  if (monitoringInstance.current_health_status === '告警') return 'alert'
  if (monitoringInstance.current_health_status === '关注' || monitoringInstance.current_active_incident_count > 0) return 'notice'
  return 'normal'
}

