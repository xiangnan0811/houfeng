import { formatMoney } from '../../lib/format'
import { renewalModeFromLegacy, renewalModeLabel } from '../../lib/assetOptions'
import type { SubscriptionRecord, VPSAssetDetail, VPSMonitoringInstanceSummary, VPSTimeline } from '../../lib/types'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  renewalLabel,
  renewalTimingLabel,
  type AssetQualityIssue,
} from '../assetPageUtils'

export type WorkbenchTone = 'normal' | 'notice' | 'alert' | 'critical' | 'neutral'

export type WorkbenchEvidence = {
  label: string
  value: string
  meta: string
  tone: Exclude<WorkbenchTone, 'neutral'>
}

export type NextAction = {
  title: string
  summary: string
  tone: Exclude<WorkbenchTone, 'neutral'>
  buttonLabel?: string
  onAction?: () => void
  linkLabel?: string
  to?: string
}

type BuildDecisionModelInput = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  servicesCount: number
  domainsCount: number
  onDecisionEdit: () => void
  onFactEdit: () => void
  onExperienceLog: () => void
  onSubscriptionCreate: () => void
  onMonitoringInstanceCreate: () => void
}

export function countDecisionRecords(timeline: VPSTimeline): number {
  return timeline.renewal_decisions.length + timeline.experience_logs.length
}

export function latestDecisionReason(timeline: VPSTimeline): string {
  return timeline.renewal_decisions[0]?.reason || '尚未记录决策理由'
}

export function renewalTone(
  subscription: SubscriptionRecord | null,
  subscriptionLoadFailed: boolean,
): Exclude<WorkbenchTone, 'neutral' | 'alert'> {
  if (subscriptionLoadFailed) return 'notice'
  if (!subscription) return 'critical'
  const days = daysUntilDate(subscription.renew_at)
  if (days != null && days <= 7) return 'critical'
  if (days != null && days <= 30) return 'notice'
  return 'normal'
}

export function primaryMonitoringInstance(detail: VPSAssetDetail): VPSMonitoringInstanceSummary | null {
  return detail.monitoring_instance_links?.[0] ?? null
}

export function toneToGlyphState(tone: Exclude<WorkbenchTone, 'neutral'>) {
  if (tone === 'critical') return 'critical'
  if (tone === 'alert') return 'alert'
  if (tone === 'notice') return 'notice'
  return 'normal'
}

export function buildVPSDecisionModel(input: BuildDecisionModelInput) {
  const knownQualityIssues = input.subscriptionLoadFailed
    ? buildVPSQualityIssues(input.detail, input.primarySubscription, { includeMissingSubscription: false })
    : buildVPSQualityIssues(input.detail, input.primarySubscription)
  const qualityIssues: AssetQualityIssue[] = input.subscriptionLoadFailed
    ? [
        { key: 'subscription-unknown', label: '订阅未知', tone: 'notice' },
        ...knownQualityIssues,
      ]
    : knownQualityIssues
  const monitoringInstance = primaryMonitoringInstance(input.detail)
  const renewalDays = daysUntilDate(input.primarySubscription?.renew_at)
  const subscriptionTone = renewalTone(input.primarySubscription, input.subscriptionLoadFailed)
  const nextAction = buildNextAction(
    input.detail,
    monitoringInstance,
    input.primarySubscription,
    input.subscriptionLoadFailed,
    qualityIssues.length,
    input.onDecisionEdit,
    input.onFactEdit,
    input.onExperienceLog,
    input.onSubscriptionCreate,
    input.onMonitoringInstanceCreate,
  )
  const evidenceItems: WorkbenchEvidence[] = [
    {
      label: '当前决策',
      value: renewalLabel(input.detail.renewal_decision),
      meta: latestDecisionReason(input.timeline),
      tone: decisionTone(input.detail),
    },
    buildSubscriptionEvidence(input.primarySubscription, input.subscriptionLoadFailed, input.subscriptionError),
    {
      label: '监控实例证据',
      value: monitoringInstance ? monitoringInstance.display_name : '尚未关联',
      meta: monitoringInstance
        ? `${monitoringInstance.current_health_status} · ${monitoringInstance.current_active_incident_count} 个活跃异常`
        : '缺少运行侧证据',
      tone: monitoringInstanceTone(monitoringInstance),
    },
    {
      label: '资料质量',
      value: qualityIssues.length > 0 ? `${qualityIssues.length} 个缺口` : '资料可用',
      meta: qualityIssues.length > 0 ? qualityIssues.map((issue) => issue.label).join(' · ') : '关键核对字段已具备',
      tone: qualityTone(qualityIssues.length),
    },
    {
      label: '上下文',
      value: `${input.servicesCount} 服务 · ${input.domainsCount} 域名`,
      meta: `${countDecisionRecords(input.timeline)} 条判断记录`,
      tone: input.servicesCount + input.domainsCount > 0 ? 'normal' : 'notice',
    },
  ]

  return {
    qualityIssues,
    monitoringInstance,
    renewalDays,
    subscriptionTone,
    nextAction,
    evidenceItems,
  }
}

function decisionTone(detail: VPSAssetDetail): Exclude<WorkbenchTone, 'neutral'> {
  if (detail.renewal_decision === 'unreviewed') return 'alert'
  if (detail.renewal_decision === 'migrate' || detail.renewal_decision === 'cancel') return 'notice'
  if (detail.renewal_decision === 'auto_renew_cancelled' || detail.renewal_decision === 'replaced') return 'notice'
  if (detail.renewal_decision === 'observe') return 'notice'
  return 'normal'
}

function monitoringInstanceTone(monitoringInstance: VPSMonitoringInstanceSummary | null): Exclude<WorkbenchTone, 'neutral'> {
  if (!monitoringInstance) return 'alert'
  if (monitoringInstance.current_health_status === '严重') return 'critical'
  if (monitoringInstance.current_health_status === '告警') return 'alert'
  if (monitoringInstance.current_health_status === '关注' || monitoringInstance.current_active_incident_count > 0) return 'notice'
  return 'normal'
}

function qualityTone(issuesCount: number): Exclude<WorkbenchTone, 'neutral'> {
  if (issuesCount >= 3) return 'alert'
  if (issuesCount > 0) return 'notice'
  return 'normal'
}

function buildSubscriptionEvidence(
  subscription: SubscriptionRecord | null,
  subscriptionLoadFailed: boolean,
  subscriptionError: string | null,
): WorkbenchEvidence {
  if (subscriptionLoadFailed) {
    return {
      label: '订阅证据',
      value: '读取失败',
      meta: subscriptionError ?? '订阅读取失败，暂不判断缺订阅',
      tone: 'notice',
    }
  }
  if (!subscription) {
    return {
      label: '订阅证据',
      value: '缺订阅',
      meta: '接口已返回空结果，需要补录成本与续费日',
      tone: 'critical',
    }
  }
  const days = daysUntilDate(subscription.renew_at)
  return {
    label: '订阅证据',
    value: formatMoney(subscription.monthly_price, subscription.currency),
    meta: `${renewalTimingLabel(days)} · ${renewalModeLabel(subscription.renewal_mode ?? renewalModeFromLegacy(subscription))}`,
    tone: renewalTone(subscription, false),
  }
}

function buildNextAction(
  detail: VPSAssetDetail,
  monitoringInstance: VPSMonitoringInstanceSummary | null,
  primarySubscription: SubscriptionRecord | null,
  subscriptionLoadFailed: boolean,
  qualityIssuesCount: number,
  onDecisionEdit: () => void,
  onFactEdit: () => void,
  onExperienceLog: () => void,
  onSubscriptionCreate: () => void,
  onMonitoringInstanceCreate: () => void,
): NextAction {
  if (detail.renewal_decision === 'unreviewed') {
    return {
      title: '先完成续费判断',
      summary: '当前仍是未评估。先根据成本、监控实例证据和上下文决定保留、观察、迁移或取消。',
      tone: 'alert',
      buttonLabel: '调整决策',
      onAction: onDecisionEdit,
    }
  }
  if (subscriptionLoadFailed) {
    return {
      title: '先恢复订阅证据',
      summary: '续费和成本证据不可用。页面不会把读取失败误判为真实缺订阅。',
      tone: 'notice',
      linkLabel: '核对订阅',
      to: `/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}`,
    }
  }
  if (!primarySubscription) {
    return {
      title: '补录续费成本',
      summary: '订阅接口已成功返回空结果，当前缺少真实续费日和月化成本。',
      tone: 'critical',
      buttonLabel: '快速创建订阅',
      onAction: onSubscriptionCreate,
    }
  }
  if (!monitoringInstance) {
    return {
      title: '补监控实例运行证据',
      summary: '这台 VPS 尚未关联监控实例，资产判断缺少心跳、健康和异常证据。',
      tone: 'alert',
      buttonLabel: '创建并接入 agent',
      onAction: onMonitoringInstanceCreate,
    }
  }
  if (monitoringInstance.current_active_incident_count > 0 || monitoringInstance.current_health_status === '告警' || monitoringInstance.current_health_status === '严重') {
    return {
      title: '先核对运行异常',
      summary: `${monitoringInstance.display_name} 当前有 ${monitoringInstance.current_active_incident_count} 个活跃异常，先确认是否影响保留或迁移判断。`,
      tone: monitoringInstanceTone(monitoringInstance),
      linkLabel: '查看监控实例',
      to: `/monitoring/${monitoringInstance.monitoring_instance_id}`,
    }
  }
  if (qualityIssuesCount > 0) {
    return {
      title: '补全资产资料',
      summary: '还有资料缺口会降低后续真实数据核对的可信度。',
      tone: 'notice',
      buttonLabel: '编辑资料',
      onAction: onFactEdit,
    }
  }
  if (detail.renewal_decision === 'migrate' || detail.renewal_decision === 'cancel') {
    return {
      title: '推进生命周期收尾',
      summary: `当前决策是${renewalLabel(detail.renewal_decision)}。继续记录迁移、取消或替换过程中的关键经验。`,
      tone: 'notice',
      buttonLabel: '补充经验',
      onAction: onExperienceLog,
    }
  }
  if (detail.renewal_decision === 'observe') {
    return {
      title: '保持观察并记录经验',
      summary: '核心证据已可读，后续重点是把稳定性、网络和账单经验写入历史。',
      tone: 'notice',
      buttonLabel: '补充经验',
      onAction: onExperienceLog,
    }
  }
  return {
    title: '保持当前决策',
    summary: '续费、监控实例和资料证据都可读。可继续补充经验记录，方便下一次续费复盘。',
    tone: 'normal',
    buttonLabel: '补充经验',
    onAction: onExperienceLog,
  }
}
