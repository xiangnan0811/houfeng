import { formatMoney, formatOptional } from '../../lib/format'
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
import { latestDecisionReason, primaryMonitoringInstance } from './vpsDecisionModel'
import type { VPSDetailModalMode } from './types'

export type VPSOverviewTone = 'normal' | 'notice' | 'alert' | 'critical'

export type VPSOverviewFact = {
  label: string
  value: string
  meta?: string
  tone?: VPSOverviewTone
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
  primaryAction: VPSOverviewAction
  secondaryActions: VPSOverviewAction[]
}

export type VPSRelatedOverviewItem = {
  key: 'subscription' | 'monitoring' | 'ip-quality' | 'services' | 'domains' | 'history'
  title: string
  tone: VPSOverviewTone
  primary: string
  secondary?: string
  titleAction: { kind: 'link'; to: string } | { kind: 'modal'; mode: NonNullable<VPSDetailModalMode> }
  quickActions: VPSOverviewAction[]
}

export type VPSSingleMachineLedgerModel = {
  operationFacts: Array<{ label: string; value: string }>
  records: Array<{ key: string; date: string; kind: string; summary: string }>
  carriers: Array<{ key: string; kind: 'service' | 'domain'; name: string; meta: string }>
  changes: Array<{ key: string; summary: string; meta: string }>
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
  facts: VPSOverviewFact[]
  judgement: {
    tone: VPSOverviewTone
    rows: Array<{ label: string; value: string }>
    attentionItems: VPSContextAction[]
    primaryAction: VPSOverviewAction | null
  }
  relatedItems: VPSRelatedOverviewItem[]
  ledger: VPSSingleMachineLedgerModel
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
  const subscriptionSummary = buildSubscriptionSummary(input.primarySubscription, input.subscriptionLoadFailed, input.subscriptionError)
  const ipOverview = buildIPQualityOverview(input.detail, input.ipQuality, input.ipQualityError)
  const relatedItems = buildRelatedItems(input, subscriptionSummary, ipOverview)
  const cancellationWork = input.cancellationAttention ?? needsCancellationWork(input.detail)
  const attentionItems = buildAttentionItems(input, subscriptionSummary, ipOverview, cancellationWork)
  const actionValue = cancellationWork ? '取消/退役' : attentionItems[0]?.primaryAction.label ?? '无'

  return {
    title: input.detail.display_name,
    badges: [
      lifecycleLabel(input.detail.lifecycle_status),
      usageLabel(input.detail.usage_status),
      renewalLabel(input.detail.renewal_decision),
      `${input.detail.active_monitoring_instance_link_count} 个监控实例`,
    ],
    facts: [
      { label: 'Provider', value: formatOptional(input.detail.provider_name) },
      { label: '地区 / 数据中心', value: locationLabel(input.detail) },
      { label: '规格', value: formatOptional(input.detail.product_name) },
      { label: '访问', value: accessLabel(input.detail) },
      { label: '系统', value: formatOptional(input.detail.os_name) },
      { label: '订阅', value: subscriptionSummary.factValue, tone: subscriptionSummary.tone },
      { label: '监控', value: monitoringFactValue(input.detail), tone: monitoringTone(monitoringInstance) },
      { label: 'IP 质量', value: ipOverview.titleValue, tone: ipOverviewTone(ipOverview) },
    ],
    judgement: {
      tone: strongestOverviewTone(attentionItems.map((item) => item.tone)),
      rows: [
        { label: '决策', value: renewalLabel(input.detail.renewal_decision) },
        { label: '续费', value: input.primarySubscription ? renewalDueLabel(input.primarySubscription) : subscriptionSummary.shortValue },
        { label: '动作', value: actionValue },
      ],
      attentionItems,
      primaryAction: cancellationWork
        ? { kind: 'modal', label: '处理取消/退役', mode: 'cancellation' }
        : null,
    },
    relatedItems,
    ledger: buildLedger(input),
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
  const latestRecord = latestLedgerRecord(input.timeline)
  return [
    {
      key: 'subscription',
      title: '订阅',
      tone: subscriptionSummary.tone,
      primary: subscriptionSummary.relatedPrimary,
      secondary: subscriptionSummary.relatedSecondary,
      titleAction: { kind: 'link', to: `/subscriptions?vps_id=${encodeURIComponent(input.detail.vps_id)}` },
      quickActions: input.subscriptionLoadFailed
        ? []
        : [
            { kind: 'modal', label: '创建/更新订阅', mode: 'subscription' },
            { kind: 'modal', label: '延长', mode: 'validity-extension' },
          ],
    },
    {
      key: 'monitoring',
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
      title: 'IP 质量',
      tone: ipOverviewTone(ipOverview),
      primary: ipOverview.titleValue,
      secondary: ipOverview.riskSummary,
      titleAction: { kind: 'link', to: ipOverview.reportTo },
      quickActions: [],
    },
    {
      key: 'services',
      title: '服务',
      tone: 'normal',
      primary: `${input.services.length} 个服务`,
      secondary: input.services[0]?.name ?? '未记录服务',
      titleAction: { kind: 'modal', mode: 'services-detail' },
      quickActions: [{ kind: 'modal', label: '新增服务', mode: 'service' }],
    },
    {
      key: 'domains',
      title: '域名',
      tone: 'normal',
      primary: `${input.domains.length} 个域名`,
      secondary: input.domains[0]?.domain_name ?? '未记录域名',
      titleAction: { kind: 'modal', mode: 'domains-detail' },
      quickActions: [{ kind: 'modal', label: '新增域名', mode: 'domain' }],
    },
    {
      key: 'history',
      title: '资产历史',
      tone: latestRecord ? 'normal' : 'notice',
      primary: `${ledgerRecordCount(input.timeline)} 条记录`,
      secondary: latestRecord?.summary ?? '尚无资产历史',
      titleAction: { kind: 'modal', mode: 'timeline-detail' },
      quickActions: [{ kind: 'modal', label: '记录', mode: 'experience' }],
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
      primaryAction: { kind: 'modal', label: '处理取消/退役', mode: 'cancellation' },
      secondaryActions: [],
    })
  }

  if (monitoringInstance && monitoringTone(monitoringInstance) !== 'normal') {
    items.push({
      title: '运行观测需要核对',
      reason: `${monitoringInstance.display_name} · ${monitoringInstance.current_active_incident_count} 个活跃异常`,
      tone: monitoringTone(monitoringInstance),
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
      primaryAction: { kind: 'link', label: '核对订阅', to: `/subscriptions?vps_id=${encodeURIComponent(input.detail.vps_id)}` },
      secondaryActions: [],
    })
  } else if (!input.primarySubscription) {
    items.push({
      title: '缺少当前订阅',
      reason: '需要补齐成本和续费日',
      tone: 'critical',
      primaryAction: { kind: 'modal', label: '创建/更新订阅', mode: 'subscription' },
      secondaryActions: [],
    })
  } else if (subscriptionSummary.tone === 'critical' || subscriptionSummary.tone === 'notice') {
    items.push({
      title: input.primarySubscription.auto_renew_cancelled ? '自动续费已取消' : '续费时间需要关注',
      reason: renewalDueLabel(input.primarySubscription),
      tone: subscriptionSummary.tone,
      primaryAction: { kind: 'modal', label: '调整决策', mode: 'decision' },
      secondaryActions: [{ kind: 'modal', label: '延长有效期', mode: 'validity-extension' }],
    })
  }

  if (!monitoringInstance) {
    items.push({
      title: '缺少运行观测',
      reason: '尚未关联监控实例',
      tone: 'alert',
      primaryAction: { kind: 'modal', label: '接入/升级 agent', mode: 'monitoring-instance-create' },
      secondaryActions: [{ kind: 'modal', label: '关联已有监控实例', mode: 'monitoring-instance-link' }],
    })
  }

  if (ipOverview.status === 'error') {
    items.push({
      title: 'IP 质量暂不可用',
      reason: ipOverview.verdict,
      tone: 'notice',
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

function buildLedger(input: VPSDetailOverviewModelInput): VPSSingleMachineLedgerModel {
  const records = [
    ...input.timeline.experience_logs.slice(0, 3).map((log) => ({
      key: log.experience_log_id,
      date: log.occurred_at ?? log.created_at,
      kind: '经验',
      summary: log.summary,
    })),
    ...input.timeline.renewal_decisions.slice(0, 3).map((decision) => ({
      key: decision.decision_id,
      date: decision.decided_at,
      kind: '决策',
      summary: decision.reason || renewalLabel(decision.to_decision),
    })),
  ].slice(0, 3)

  const carriers = [
    ...input.services.slice(0, 3).map((service) => ({
      key: service.service_id,
      kind: 'service' as const,
      name: service.name,
      meta: service.url || (service.port ? `端口 ${service.port}` : '未记录入口'),
    })),
    ...input.domains.slice(0, 3).map((domain) => ({
      key: domain.domain_id,
      kind: 'domain' as const,
      name: domain.domain_name,
      meta: domain.purpose || domain.registrar || '未记录用途',
    })),
  ].slice(0, 3)

  const changes = [
    ...input.timeline.price_histories.slice(0, 1).map((history) => ({
      key: history.price_history_id,
      summary: `${formatMoney(history.from_price, history.from_currency)} -> ${formatMoney(history.to_price, history.to_currency)}`,
      meta: '价格变化',
    })),
    ...input.timeline.ip_histories.slice(0, 1).map((history) => ({
      key: history.ip_history_id,
      summary: `${formatOptional(history.from_ipv4)} -> ${formatOptional(history.to_ipv4)}`,
      meta: 'IP 变化',
    })),
  ]

  return {
    operationFacts: [
      { label: '用途', value: usageLabel(input.detail.usage_status) },
      { label: '重要性', value: formatOptional(input.detail.importance) },
      { label: '标签', value: input.detail.labels.length > 0 ? input.detail.labels.join(' · ') : '无标签' },
      { label: '备注', value: input.detail.note || '未记录' },
    ],
    records,
    carriers,
    changes,
  }
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

function accessLabel(detail: VPSAssetDetail): string {
  const host = detail.ssh_host || detail.ipv4 || detail.ipv6 || detail.display_name
  return detail.ssh_port ? `${host}:${detail.ssh_port}` : host
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

function ledgerRecordCount(timeline: VPSTimeline): number {
  return timeline.renewal_decisions.length +
    timeline.price_histories.length +
    timeline.ip_histories.length +
    timeline.spec_snapshots.length +
    timeline.experience_logs.length
}

function latestLedgerRecord(timeline: VPSTimeline): { summary: string } | null {
  const experience = timeline.experience_logs[0]
  if (experience) return { summary: experience.summary }
  const decision = timeline.renewal_decisions[0]
  if (decision) return { summary: decision.reason || latestDecisionReason(timeline) }
  const price = timeline.price_histories[0]
  if (price) return { summary: `${formatMoney(price.from_price, price.from_currency)} -> ${formatMoney(price.to_price, price.to_currency)}` }
  return null
}
