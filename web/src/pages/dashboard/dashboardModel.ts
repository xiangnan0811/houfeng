import { formatMoney } from '../../lib/format'
import type {
  DashboardAssetSummary,
  DashboardOverview,
  IncidentSeverity,
  SubscriptionOverview,
  VPSAssetRecord,
} from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import type { RemoteState } from './dashboardRemoteState'

export type DashboardMode =
  | 'onboarding'
  | 'critical'
  | 'abnormal'
  | 'maintenance'
  | 'stable'

export type DashboardTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'neutral'

export type DashboardAction = {
  label: string
  to: string
}

export type DashboardJudgement = {
  id: 'observability' | 'assets' | 'billing'
  label: string
  value: string
  detail: string
  to: string
  tone: DashboardTone
}

export type DashboardEvidenceStatus = 'loading' | 'available' | 'unavailable'

export type DashboardAssetEvidence = {
  status: DashboardEvidenceStatus
  title: string
  detail: string
  source: 'vps-list'
  loadedAt?: string
  vpsCount?: number
}

export type DashboardBillingEvidence = {
  status: DashboardEvidenceStatus
  title: string
  detail: string
  source: 'subscription-overview' | 'dashboard-fallback'
  generatedAt: string
}

export type DashboardAttentionItem = {
  id: string
  kind: 'monitoring_instance' | 'target'
  name: string
  detail: string
  meta: string
  to: string
  tone: Exclude<DashboardTone, 'neutral'>
  severity: IncidentSeverity
  incidentCount: number
}

export type DashboardObservabilityModel = {
  abnormalMonitoringCount: number
  severeMonitoringCount: number
  abnormalTargetCount: number
  severeTargetCount: number
  abnormalTotal: number
  severeTotal: number
  maintenanceTotal: number
  attentionItems: DashboardAttentionItem[]
}

export type DashboardDegradation = {
  resource: 'vps' | 'subscription'
  message: string
}

export type DashboardReadyModel = {
  status: 'ready'
  mode: DashboardMode
  tone: Exclude<DashboardTone, 'neutral'>
  title: string
  summary: string
  snapshotGeneratedAt: string
  primaryAction: DashboardAction
  judgements: DashboardJudgement[]
  observability: DashboardObservabilityModel
  assetEvidence: DashboardAssetEvidence
  billingEvidence: DashboardBillingEvidence
  degradations: DashboardDegradation[]
}

export type DashboardModel =
  | { status: 'loading' }
  | { status: 'error'; error: string }
  | DashboardReadyModel

type BuildDashboardModelInput = {
  overview: RemoteState<DashboardOverview>
  vps: RemoteState<VPSAssetRecord[]>
  subscription: RemoteState<SubscriptionOverview>
}

const SEVERITY_RANK: Record<IncidentSeverity, number> = {
  正常: 0,
  关注: 1,
  告警: 2,
  严重: 3,
}

function toneForSeverity(severity: IncidentSeverity): Exclude<DashboardTone, 'neutral'> {
  if (severity === '严重') return 'critical'
  if (severity === '告警') return 'alert'
  if (severity === '关注') return 'notice'
  return 'normal'
}

function buildAttentionItems(overview: DashboardOverview): DashboardAttentionItem[] {
  const monitoringItems = overview.abnormal_monitoring_instances.map((item): DashboardAttentionItem => ({
    id: item.monitoring_instance_id,
    kind: 'monitoring_instance',
    name: item.display_name,
    detail: item.current_primary_issue_summary || '暂无关键异常摘要',
    meta: [item.region, item.city, item.provider].filter(Boolean).join(' · ') || '位置未标记',
    to: `/monitoring/${item.monitoring_instance_id}`,
    tone: toneForSeverity(item.current_health_status),
    severity: item.current_health_status,
    incidentCount: item.current_active_incident_count,
  }))
  const targetItems = overview.abnormal_targets.map((item): DashboardAttentionItem => ({
    id: item.target_id,
    kind: 'target',
    name: item.name,
    detail: item.current_primary_issue_summary || '暂无关键异常摘要',
    meta: `${item.host}${item.base_port == null ? '' : `:${item.base_port}`}`,
    to: `/targets/${item.target_id}`,
    tone: toneForSeverity(item.current_health_status),
    severity: item.current_health_status,
    incidentCount: item.current_active_incident_count,
  }))

  return [...monitoringItems, ...targetItems]
    .sort((a, b) => {
      const severityDelta = SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity]
      return severityDelta !== 0 ? severityDelta : b.incidentCount - a.incidentCount
    })
    .slice(0, 3)
}

function buildObservability(overview: DashboardOverview): DashboardObservabilityModel {
  const abnormalMonitoringCount = overview.abnormal_monitoring_instance_count
  const abnormalTargetCount = overview.abnormal_target_count
  const severeMonitoringCount = overview.severe_monitoring_instance_count
  const severeTargetCount = overview.severe_target_count

  return {
    abnormalMonitoringCount,
    severeMonitoringCount,
    abnormalTargetCount,
    severeTargetCount,
    abnormalTotal: abnormalMonitoringCount + abnormalTargetCount,
    severeTotal: severeMonitoringCount + severeTargetCount,
    maintenanceTotal:
      overview.maintenance_monitoring_instance_count + overview.maintenance_target_count,
    attentionItems: buildAttentionItems(overview),
  }
}

function confirmedOnboarding(
  overview: DashboardOverview,
  vps: RemoteState<VPSAssetRecord[]>,
): boolean {
  return vps.status === 'success' &&
    vps.value.length === 0 &&
    overview.total_monitoring_instance_count === 0 &&
    overview.total_target_count === 0
}

function deriveMode(
  observability: DashboardObservabilityModel,
  isOnboarding: boolean,
): DashboardMode {
  if (observability.severeTotal > 0) return 'critical'
  if (observability.abnormalTotal > 0) return 'abnormal'
  if (observability.maintenanceTotal > 0) return 'maintenance'
  if (isOnboarding) return 'onboarding'
  return 'stable'
}

function assetSignal(summary: DashboardAssetSummary): {
  label: string
  detail: string
  action?: DashboardAction
  tone: DashboardTone
} {
  if (summary.cancellation_attention_vps_count > 0 || summary.running_cancelled_asset_count > 0) {
    return {
      label: '取消联动待处理',
      detail: `待核对 ${summary.cancellation_attention_vps_count} · 仍运行 ${summary.running_cancelled_asset_count}`,
      action: {
        label: '处理取消联动',
        to: DASHBOARD_LINKS.assetDecisionsMigrationRetirement,
      },
      tone: 'alert',
    }
  }
  if (summary.unreviewed_vps_count > 0 || summary.to_cancel_vps_count > 0 || summary.to_migrate_vps_count > 0) {
    return {
      label: '资产决策待核对',
      detail: `未评估 ${summary.unreviewed_vps_count} · 待取消 ${summary.to_cancel_vps_count} · 迁移意向 ${summary.to_migrate_vps_count}`,
      action: {
        label: '进入资产组合决策',
        to: DASHBOARD_LINKS.assetDecisionsNeedsDecision,
      },
      tone: 'notice',
    }
  }
  if (summary.renewal_due_30d_vps_count > 0) {
    return {
      label: '续费窗口待核对',
      detail: `30 天内 ${summary.renewal_due_30d_vps_count} 台 VPS · ${summary.renewal_due_30d_subscription_count} 条订阅`,
      action: {
        label: '核对 30 天续费',
        to: DASHBOARD_LINKS.assetDecisionsRenewal,
      },
      tone: 'notice',
    }
  }
  if (summary.unlinked_vps_count > 0 || summary.abnormal_linked_vps_count > 0) {
    return {
      label: '资产证据待补齐',
      detail: `未关联 ${summary.unlinked_vps_count} · 关联异常 ${summary.abnormal_linked_vps_count}`,
      action: {
        label: '补齐资产证据',
        to: DASHBOARD_LINKS.assetDecisionsEvidence,
      },
      tone: 'notice',
    }
  }
  return {
    label: '资产主线已读取',
    detail: '当前聚合摘要没有待处理资产信号',
    tone: 'normal',
  }
}

function buildAssetEvidence(
  vps: RemoteState<VPSAssetRecord[]>,
  summary: DashboardAssetSummary,
): DashboardAssetEvidence {
  if (vps.status === 'loading') {
    return {
      status: 'loading',
      title: 'VPS 清单读取中',
      detail: '尚未确认资产是否为空',
      source: 'vps-list',
    }
  }
  if (vps.status === 'error') {
    return {
      status: 'unavailable',
      title: 'VPS 清单不可用',
      detail: `${vps.error}；无法确认是否首次接入`,
      source: 'vps-list',
    }
  }

  const signal = assetSignal(summary)
  return {
    status: 'available',
    title: vps.value.length === 0 ? 'VPS 清单为空' : `VPS ${vps.value.length} 台`,
    detail: vps.value.length === 0 ? '接口已成功返回空清单' : signal.detail,
    source: 'vps-list',
    loadedAt: vps.loadedAt,
    vpsCount: vps.value.length,
  }
}

function dashboardCostLabel(summary: DashboardAssetSummary): string {
  if (summary.cost_by_currency.length === 0) return '聚合成本暂无记录'
  return summary.cost_by_currency
    .slice(0, 2)
    .map((item) => formatMoney(item.monthly_total, item.currency))
    .join(' + ')
}

function buildBillingEvidence(
  subscription: RemoteState<SubscriptionOverview>,
  overview: DashboardOverview,
): DashboardBillingEvidence {
  if (subscription.status === 'success') {
    return {
      status: 'available',
      title: `${formatMoney(subscription.value.total_monthly_cost, subscription.value.base_currency)}/月`,
      detail: `30 天续费 ${subscription.value.renewal_due_30d_count} · 预算风险 ${subscription.value.budget_risk_count} · 汇率异常 ${subscription.value.exchange_rate_stale_count}`,
      source: 'subscription-overview',
      generatedAt: subscription.value.snapshot_generated_at,
    }
  }
  if (subscription.status === 'error') {
    return {
      status: 'unavailable',
      title: dashboardCostLabel(overview.asset_summary),
      detail: `${subscription.error}；暂用较低精度的 Dashboard 聚合摘要`,
      source: 'dashboard-fallback',
      generatedAt: overview.snapshot_generated_at,
    }
  }
  return {
    status: 'loading',
    title: '订阅摘要读取中',
    detail: '暂用 Dashboard 聚合摘要，不把加载中表示为真实空数据',
    source: 'dashboard-fallback',
    generatedAt: overview.snapshot_generated_at,
  }
}

function buildPrimaryAction(
  mode: DashboardMode,
  overview: DashboardOverview,
  observability: DashboardObservabilityModel,
): DashboardAction {
  if (mode === 'onboarding') return { label: '创建第一台 VPS', to: DASHBOARD_LINKS.vps }
  if (mode === 'critical') return { label: '处理严重异常', to: DASHBOARD_LINKS.eventsSevere }
  if (mode === 'abnormal') {
    return {
      label: '处理观测异常',
      to: observability.abnormalMonitoringCount > 0
        ? DASHBOARD_LINKS.monitoringAbnormal
        : DASHBOARD_LINKS.targetsAbnormal,
    }
  }
  if (mode === 'maintenance') {
    return { label: '查看维护事件', to: DASHBOARD_LINKS.eventsMaintenance }
  }

  return assetSignal(overview.asset_summary).action ?? {
    label: '核对 VPS 库存',
    to: DASHBOARD_LINKS.vps,
  }
}

function modeCopy(mode: DashboardMode, primaryAction: DashboardAction): {
  tone: Exclude<DashboardTone, 'neutral'>
  title: string
  summary: string
} {
  if (mode === 'onboarding') {
    return {
      tone: 'notice',
      title: '建立第一条资产与观测链路',
      summary: 'VPS 清单已确认为空；先登记服务器主体，再补账单事实并接入 agent。',
    }
  }
  if (mode === 'critical') {
    return {
      tone: 'critical',
      title: '严重异常需要立即处理',
      summary: '严重是异常集合中的优先级分层，不重复计入异常总数。',
    }
  }
  if (mode === 'abnormal') {
    return {
      tone: 'alert',
      title: '观测异常需要处理',
      summary: '先进入异常对象队列，再结合事件和资产上下文判断影响。',
    }
  }
  if (mode === 'maintenance') {
    return {
      tone: 'maintenance',
      title: '维护对象正在观察',
      summary: '当前没有活跃异常，维护对象仍需按维护事件核对。',
    }
  }
  return {
    tone: primaryAction.label === '核对 VPS 库存' ? 'normal' : 'notice',
    title: primaryAction.label === '核对 VPS 库存' ? '当前没有紧急处理项' : '资产判断等待核对',
    summary: 'Dashboard 只提供当前摘要；具体事实和操作由对应工作台承接。',
  }
}

function observabilityJudgement(
  mode: DashboardMode,
  observability: DashboardObservabilityModel,
): DashboardJudgement {
  if (mode === 'critical') {
    return {
      id: 'observability',
      label: '严重异常',
      value: `${observability.severeTotal}`,
      detail: `异常总数 ${observability.abnormalTotal}（严重已包含）`,
      to: DASHBOARD_LINKS.eventsSevere,
      tone: 'critical',
    }
  }
  if (mode === 'abnormal') {
    return {
      id: 'observability',
      label: '观测异常',
      value: `${observability.abnormalTotal}`,
      detail: `监控实例 ${observability.abnormalMonitoringCount} · 目标 ${observability.abnormalTargetCount}`,
      to: observability.abnormalMonitoringCount > 0
        ? DASHBOARD_LINKS.monitoringAbnormal
        : DASHBOARD_LINKS.targetsAbnormal,
      tone: 'alert',
    }
  }
  if (mode === 'maintenance') {
    return {
      id: 'observability',
      label: '维护观察',
      value: `${observability.maintenanceTotal}`,
      detail: '当前无活跃异常',
      to: DASHBOARD_LINKS.eventsMaintenance,
      tone: 'maintenance',
    }
  }
  if (mode === 'onboarding') {
    return {
      id: 'observability',
      label: '观测链路',
      value: '待接入',
      detail: '监控实例 0 · 目标 0',
      to: DASHBOARD_LINKS.vps,
      tone: 'notice',
    }
  }
  return {
    id: 'observability',
    label: '观测状态',
    value: '无活跃异常',
    detail: '查看 24h 新增与恢复记录',
    to: DASHBOARD_LINKS.events24h,
    tone: 'normal',
  }
}

function evidenceJudgements(
  mode: DashboardMode,
  overview: DashboardOverview,
  observability: DashboardObservabilityModel,
  assetEvidence: DashboardAssetEvidence,
  billingEvidence: DashboardBillingEvidence,
): DashboardJudgement[] {
  const signal = assetSignal(overview.asset_summary)
  return [
    observabilityJudgement(mode, observability),
    {
      id: 'assets',
      label: assetEvidence.status === 'available' ? signal.label : assetEvidence.title,
      value: assetEvidence.vpsCount == null ? '待确认' : `${assetEvidence.vpsCount}`,
      detail: assetEvidence.status === 'unavailable'
        ? '资产是否为空仍待确认'
        : assetEvidence.detail,
      to: signal.action?.to ?? DASHBOARD_LINKS.vps,
      tone: assetEvidence.status === 'unavailable'
        ? 'notice'
        : assetEvidence.status === 'loading'
          ? 'neutral'
          : signal.tone,
    },
    {
      id: 'billing',
      label: billingEvidence.status === 'available' ? '订阅摘要' : billingEvidence.title,
      value: billingEvidence.status === 'available' ? billingEvidence.title : '降级',
      detail: billingEvidence.status === 'unavailable'
        ? '账单精度已降级'
        : billingEvidence.detail,
      to: DASHBOARD_LINKS.subscriptions,
      tone: billingEvidence.status === 'available' ? 'neutral' : 'notice',
    },
  ]
}

export function buildDashboardModel(input: BuildDashboardModelInput): DashboardModel {
  if (input.overview.status === 'loading') return { status: 'loading' }
  if (input.overview.status === 'error') {
    return { status: 'error', error: input.overview.error }
  }

  const overview = input.overview.value
  const observability = buildObservability(overview)
  const mode = deriveMode(observability, confirmedOnboarding(overview, input.vps))
  const primaryAction = buildPrimaryAction(mode, overview, observability)
  const copy = modeCopy(mode, primaryAction)
  const assetEvidence = buildAssetEvidence(input.vps, overview.asset_summary)
  const billingEvidence = buildBillingEvidence(input.subscription, overview)
  const judgements = evidenceJudgements(
    mode,
    overview,
    observability,
    assetEvidence,
    billingEvidence,
  )
  const degradations: DashboardDegradation[] = []
  if (input.vps.status === 'error') {
    degradations.push({ resource: 'vps', message: input.vps.error })
  }
  if (input.subscription.status === 'error') {
    degradations.push({ resource: 'subscription', message: input.subscription.error })
  }
  const partiallyUnavailable = copy.tone === 'normal' && degradations.length > 0

  return {
    status: 'ready',
    mode,
    tone: partiallyUnavailable ? 'notice' : copy.tone,
    title: partiallyUnavailable ? '部分事实待确认' : copy.title,
    summary: partiallyUnavailable
      ? 'Dashboard 聚合摘要仍可用；VPS 或订阅来源失败，相关结论保持待确认。'
      : copy.summary,
    snapshotGeneratedAt: overview.snapshot_generated_at,
    primaryAction,
    judgements,
    observability,
    assetEvidence,
    billingEvidence,
    degradations,
  }
}
