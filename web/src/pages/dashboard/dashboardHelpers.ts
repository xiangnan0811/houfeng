import type { BadgeTone, HealthState } from '../../components/atoms'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type DashboardAssetSummary,
  type DashboardNodeSummary,
  type DashboardOverview,
  type DashboardTargetSummary,
} from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import type { AttentionItem, ContextItem, DashboardMetric, FleetState, FleetStateTone } from './types'

const SEVERITY_RANK = ['严重', '告警', '关注', '维护中', '正常'] as const

function severityWeight(value: string): number {
  const idx = (SEVERITY_RANK as readonly string[]).indexOf(value)
  return idx === -1 ? 999 : idx
}

export function statusGlyph(value: string): HealthState {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  if (value === '维护中') return 'maintenance'
  return 'offline'
}

export function statusTone(value: string): BadgeTone {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  if (value === '维护中') return 'maintenance'
  return 'offline'
}

export function fleetGlyphState(tone: FleetStateTone): HealthState {
  if (tone === 'normal') return 'normal'
  if (tone === 'notice') return 'notice'
  if (tone === 'alert') return 'alert'
  if (tone === 'critical') return 'critical'
  return 'maintenance'
}

export function fleetSignalLabel(tone: FleetStateTone): string {
  if (tone === 'normal') return '正常'
  if (tone === 'notice') return '待接入'
  if (tone === 'alert') return '异常'
  if (tone === 'critical') return '严重'
  return '维护'
}

function hostPortSummary(target: DashboardTargetSummary) {
  return typeof target.base_port === 'number' ? `${target.host}:${target.base_port}` : target.host
}

function nodeLocation(node: DashboardNodeSummary) {
  return [node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ') || '未标记位置'
}

function targetLocation(target: DashboardTargetSummary) {
  return [target.group, target.target_type].filter(Boolean).join(' · ') || '未标记分组'
}

export function notificationSummary(overview: DashboardOverview) {
  const status = overview.notification_status
  const configuredCount =
    (status.telegram_configured ? 1 : 0) + (status.feishu_configured ? 1 : 0)
  if (configuredCount === 0) {
    return '通知通道 0/2 已配置'
  }
  const channels = [
    status.telegram_configured ? 'Telegram' : null,
    status.feishu_configured ? 'Feishu' : null,
  ].filter(Boolean)
  const runtime = status.telegram_runtime_apply_active ? ' · Telegram runtime 生效' : ''
  return `通知通道 ${configuredCount}/2 已配置：${channels.join('、')}${runtime}`
}

export function nodeEntryLink(overview: DashboardOverview) {
  if (overview.pending_onboarding_node_count > 0) return DASHBOARD_LINKS.nodesPendingOnboarding
  if (overview.paused_node_count > 0) return DASHBOARD_LINKS.nodesPaused
  if (overview.retired_node_count > 0) return DASHBOARD_LINKS.nodesRetired
  return DASHBOARD_LINKS.nodes
}

export function targetEntryLink(overview: DashboardOverview) {
  if (overview.abnormal_target_count > 0) return DASHBOARD_LINKS.targetsAbnormal
  if (overview.paused_target_count > 0) return DASHBOARD_LINKS.targetsPaused
  if (overview.archived_target_count > 0) return DASHBOARD_LINKS.targetsArchived
  return DASHBOARD_LINKS.targets
}

export function nodeManagementStat(overview: DashboardOverview) {
  return `待接入 ${overview.pending_onboarding_node_count} · 暂停 ${overview.paused_node_count} · 退役 ${overview.retired_node_count}`
}

export function targetManagementStat(overview: DashboardOverview) {
  return `异常 ${overview.abnormal_target_count} · 暂停 ${overview.paused_target_count} · 归档 ${overview.archived_target_count}`
}

export function eventManagementStat(overview: DashboardOverview) {
  return `24h 新增 ${overview.recent_new_incident_count} · 恢复 ${overview.recent_recovery_count}`
}

function inventoryEntryLink(overview: DashboardOverview) {
  if (
    overview.pending_onboarding_node_count > 0 ||
    overview.paused_node_count > 0 ||
    overview.retired_node_count > 0
  ) {
    return nodeEntryLink(overview)
  }
  if (
    overview.abnormal_target_count > 0 ||
    overview.paused_target_count > 0 ||
    overview.archived_target_count > 0
  ) {
    return targetEntryLink(overview)
  }
  return DASHBOARD_LINKS.nodes
}

function activeGroupCount(overview: DashboardOverview) {
  return overview.group_summaries.filter(
    (group) => group.abnormal_node_count + group.abnormal_target_count > 0,
  ).length
}

function topAffectedGroup(overview: DashboardOverview) {
  return [...overview.group_summaries].sort((a, b) => {
    const activeDelta =
      b.abnormal_node_count + b.abnormal_target_count - (a.abnormal_node_count + a.abnormal_target_count)
    if (activeDelta !== 0) return activeDelta
    return b.severe_node_count + b.severe_target_count - (a.severe_node_count + a.severe_target_count)
  })[0]
}

function latestEventSummary(overview: DashboardOverview): string {
  const latestEvent = overview.recent_events[0]
  if (!latestEvent) return '24h 内没有事件记录'
  const eventLabel = STATE_CHANGE_EVENT_TYPE_LABELS[latestEvent.event_type] ?? '状态变化'
  const severity = latestEvent.severity ? ` · ${latestEvent.severity}` : ''
  return `${eventLabel}${severity} · ${latestEvent.object_type === 'node' ? '节点' : '目标'} ${latestEvent.object_id}`
}

function latestEventTimestamp(overview: DashboardOverview): string | null {
  return overview.recent_events[0]?.created_at ?? null
}

export function trendValues(overview: DashboardOverview) {
  const values = overview.new_incident_trend_24h?.filter((value) => Number.isFinite(value)) ?? []
  if (values.length > 0) return values
  if (overview.recent_new_incident_count > 0 || overview.recent_recovery_count > 0) {
    return [0, overview.recent_new_incident_count]
  }
  return []
}

export function trendBalanceLabel(overview: DashboardOverview) {
  const delta = overview.recent_new_incident_count - overview.recent_recovery_count
  if (delta > 0) return `净增 ${delta}`
  if (delta < 0) return `净恢复 ${Math.abs(delta)}`
  return '新增与恢复持平'
}

export function formatAssetCost(summary: DashboardAssetSummary) {
  if (summary.cost_by_currency.length === 0) return '暂无 active 订阅成本'
  return summary.cost_by_currency
    .slice(0, 2)
    .map((item) => `${item.currency} ${item.monthly_total.toFixed(2)}/月`)
    .join(' · ')
}

export function buildContextItems(
  overview: DashboardOverview,
  abnormalTotal: number,
  maintenanceTotal: number,
): ContextItem[] {
  const affectedGroupCount = activeGroupCount(overview)
  const topGroup = topAffectedGroup(overview)
  const impactDetail =
    affectedGroupCount > 0 && topGroup
      ? `${affectedGroupCount} 个分组受影响，最高影响 ${topGroup.group}`
      : `覆盖 ${overview.group_summaries.length} 个分组，当前无异常分组`
  const inventoryDetail = [
    `节点 ${overview.total_node_count}`,
    `目标 ${overview.total_target_count}`,
    overview.pending_onboarding_node_count > 0 ? `待接入 ${overview.pending_onboarding_node_count}` : null,
    overview.paused_node_count + overview.paused_target_count > 0
      ? `暂停 ${overview.paused_node_count + overview.paused_target_count}`
      : null,
    overview.retired_node_count + overview.archived_target_count > 0
      ? `退役/归档 ${overview.retired_node_count + overview.archived_target_count}`
      : null,
  ].filter(Boolean).join(' · ')

  return [
    {
      label: '影响范围',
      title: affectedGroupCount > 0 && topGroup ? `${topGroup.group} 受影响` : '分组稳定',
      detail: impactDetail,
      to: abnormalTotal > 0
        ? overview.abnormal_node_count > 0
          ? DASHBOARD_LINKS.nodesAbnormal
          : DASHBOARD_LINKS.targetsAbnormal
        : DASHBOARD_LINKS.nodes,
      tone: abnormalTotal > 0 ? 'alert' : 'normal',
    },
    {
      label: '库存状态',
      title: overview.pending_onboarding_node_count > 0
        ? `待接入 ${overview.pending_onboarding_node_count}`
        : `${overview.total_node_count} 节点 / ${overview.total_target_count} 目标`,
      detail: inventoryDetail,
      to: inventoryEntryLink(overview),
      tone: overview.pending_onboarding_node_count > 0 ||
        overview.paused_node_count > 0 ||
        overview.paused_target_count > 0 ||
        overview.archived_target_count > 0
        ? 'notice'
        : 'neutral',
    },
    {
      label: '最近活动',
      title: `新增 ${overview.recent_new_incident_count} / 恢复 ${overview.recent_recovery_count}`,
      detail: latestEventSummary(overview),
      to: maintenanceTotal > 0 ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.events24h,
      tone: overview.recent_new_incident_count > 0 ? 'notice' : 'normal',
      timestampAt: latestEventTimestamp(overview),
    },
  ]
}

export function buildFleetState(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
): FleetState {
  const recentSummary = `24h 新增 ${overview.recent_new_incident_count} / 恢复 ${overview.recent_recovery_count}`

  if (isFreshInstall) {
    return {
      title: '开始接入第一台服务器',
      description: '创建 Node，再配置目标。',
      tone: 'notice',
      primaryCta: { label: '创建第一个节点', to: DASHBOARD_LINKS.nodes },
      secondaryCtas: [],
    }
  }

  if (severeTotal > 0) {
    return {
      title: '需要处理严重异常',
      description: `异常 ${abnormalTotal} / 严重 ${severeTotal}；${recentSummary}`,
      tone: 'critical',
      primaryCta: { label: '查看当前异常', to: DASHBOARD_LINKS.eventsSevere },
      secondaryCtas: [
        { label: '查看事件流', to: DASHBOARD_LINKS.events24h },
        { label: '进入设置', to: DASHBOARD_LINKS.settings },
      ],
    }
  }

  if (abnormalTotal > 0) {
    return {
      title: '存在活跃异常',
      description: `异常 ${abnormalTotal}；${recentSummary}`,
      tone: 'alert',
      primaryCta: { label: '查看当前异常', to: DASHBOARD_LINKS.events24h },
      secondaryCtas: [
        { label: '查看事件流', to: DASHBOARD_LINKS.events24h },
        { label: '进入设置', to: DASHBOARD_LINKS.settings },
      ],
    }
  }

  if (maintenanceTotal > 0) {
    return {
      title: '系统处于维护观察中',
      description: `维护 ${maintenanceTotal}；${recentSummary}`,
      tone: 'maintenance',
      primaryCta: { label: '查看维护事件', to: DASHBOARD_LINKS.eventsMaintenance },
      secondaryCtas: [
        { label: '查看事件流', to: DASHBOARD_LINKS.events24h },
        { label: '进入设置', to: DASHBOARD_LINKS.settings },
      ],
    }
  }

  return {
    title: '系统运行正常',
    description: `无活跃异常；${recentSummary}`,
    tone: 'normal',
    primaryCta: { label: '查看节点', to: DASHBOARD_LINKS.nodes },
    secondaryCtas: [
      { label: '查看事件流', to: DASHBOARD_LINKS.events24h },
      { label: '进入设置', to: DASHBOARD_LINKS.settings },
    ],
  }
}

export function buildAttentionItems(overview: DashboardOverview): AttentionItem[] {
  const nodeItems = (overview.abnormal_nodes ?? []).map((node): AttentionItem => ({
    kind: 'node',
    id: node.node_id,
    name: node.display_name,
    route: `/nodes/${node.node_id}`,
    health: node.current_health_status,
    incidentCount: node.current_active_incident_count,
    issueSummary: node.current_primary_issue_summary || '暂无关键异常摘要',
    location: nodeLocation(node),
    technicalId: node.node_id,
    freshnessLabel: '心跳',
    freshnessAt: node.last_heartbeat_at ?? null,
    meta: '服务器节点',
  }))

  const targetItems = (overview.abnormal_targets ?? []).map((target): AttentionItem => ({
    kind: 'target',
    id: target.target_id,
    name: target.name,
    route: `/targets/${target.target_id}`,
    health: target.current_health_status,
    incidentCount: target.current_active_incident_count,
    issueSummary: target.current_primary_issue_summary || '暂无关键异常摘要',
    location: targetLocation(target),
    technicalId: hostPortSummary(target),
    freshnessLabel: target.last_failure_at ? '最近失败' : '最近成功',
    freshnessAt: target.last_failure_at ?? target.last_success_at ?? null,
    meta: '观测目标',
  }))

  return [...nodeItems, ...targetItems].sort((a, b) => {
    const severityDelta = severityWeight(a.health) - severityWeight(b.health)
    if (severityDelta !== 0) return severityDelta
    return b.incidentCount - a.incidentCount
  })
}

export function buildDashboardMetrics(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
): DashboardMetric[] {
  if (isFreshInstall) return []

  if (abnormalTotal > 0) {
    return [
      {
        label: '异常对象',
        value: abnormalTotal,
        detail: `节点 ${overview.abnormal_node_count} · 目标 ${overview.abnormal_target_count}`,
        to: overview.abnormal_node_count > 0 ? DASHBOARD_LINKS.nodesAbnormal : DASHBOARD_LINKS.targetsAbnormal,
        tone: 'alert',
      },
      {
        label: '严重',
        value: severeTotal,
        detail: `节点 ${overview.severe_node_count} · 目标 ${overview.severe_target_count}`,
        to: DASHBOARD_LINKS.eventsSevere,
        tone: severeTotal > 0 ? 'critical' : 'neutral',
      },
      {
        label: '24h 变化',
        value: `${overview.recent_new_incident_count}/${overview.recent_recovery_count}`,
        detail: '新增异常 / 恢复',
        to: DASHBOARD_LINKS.events24h,
        tone: overview.recent_new_incident_count > 0 ? 'notice' : 'normal',
      },
      {
        label: '维护',
        value: maintenanceTotal,
        detail: `节点 ${overview.maintenance_node_count} · 目标 ${overview.maintenance_target_count}`,
        to: DASHBOARD_LINKS.eventsMaintenance,
        tone: maintenanceTotal > 0 ? 'maintenance' : 'neutral',
      },
    ]
  }

  return [
    {
      label: '节点',
      value: overview.total_node_count,
      detail: nodeManagementStat(overview),
      to: nodeEntryLink(overview),
      tone: overview.pending_onboarding_node_count > 0 || overview.paused_node_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '目标',
      value: overview.total_target_count,
      detail: targetManagementStat(overview),
      to: targetEntryLink(overview),
      tone: overview.paused_target_count > 0 || overview.archived_target_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '24h 变化',
      value: `${overview.recent_new_incident_count}/${overview.recent_recovery_count}`,
      detail: '新增异常 / 恢复',
      to: DASHBOARD_LINKS.events24h,
      tone: overview.recent_new_incident_count > 0 ? 'notice' : 'normal',
    },
    {
      label: maintenanceTotal > 0 ? '维护' : '通知',
      value: maintenanceTotal > 0 ? maintenanceTotal : '配置',
      detail: maintenanceTotal > 0
        ? `节点 ${overview.maintenance_node_count} · 目标 ${overview.maintenance_target_count}`
        : notificationSummary(overview),
      to: maintenanceTotal > 0 ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.settings,
      tone: maintenanceTotal > 0 ? 'maintenance' : 'neutral',
    },
  ]
}
