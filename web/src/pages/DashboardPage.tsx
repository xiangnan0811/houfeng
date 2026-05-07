import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  Badge,
  Hostname,
  MonoDigits,
  StatusGlyph,
  Timestamp,
  type BadgeTone,
  type HealthState,
} from '../components/atoms'
import { DetailSection } from '../components/DetailSection'
import { ApiError, getDashboard } from '../lib/api'
import {
  type DashboardNodeSummary,
  type DashboardOverview,
  type DashboardTargetSummary,
  type IncidentSeverity,
  STATE_CHANGE_EVENT_TYPE_LABELS,
} from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
}

type FleetStateTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance'

type FleetState = {
  title: string
  description: string
  tone: FleetStateTone
  primaryCta: {
    label: string
    to: string
  }
  secondaryCtas: Array<{
    label: string
    to: string
  }>
}

type AttentionItem = {
  kind: 'node' | 'target'
  id: string
  name: string
  route: string
  health: IncidentSeverity
  incidentCount: number
  issueSummary: string
  location: string
  technicalId: string
  freshnessLabel: string
  freshnessAt?: string | null
  meta: string
}

type DashboardMetric = {
  label: string
  value: number | string
  detail: string
  to: string
  tone?: BadgeTone
}

type ManagementEntry = {
  title: string
  stat: string
  to: string
}

type ContextItem = {
  label: string
  title: string
  detail: string
  to: string
  tone?: BadgeTone
  timestampAt?: string | null
}

const SEVERITY_RANK = ['严重', '告警', '关注', '维护中', '正常'] as const
const MAX_ATTENTION_ITEMS = 6
const DASHBOARD_LINKS = {
  eventsSevere: '/events?severity=严重',
  events24h: '/events?time_range=24h',
  eventsMaintenance: '/events?maintenance_only=1',
  nodes: '/nodes',
  nodesAbnormal: '/nodes?abnormal=1',
  nodesPendingOnboarding: '/nodes?onboarding=pending',
  nodesPaused: '/nodes?run_status=暂停',
  nodesRetired: '/nodes?lifecycle=已退役',
  targets: '/targets',
  targetsAbnormal: '/targets?abnormal=1',
  targetsPaused: '/targets?run_status=暂停',
  targetsArchived: '/targets?run_status=已归档',
  settings: '/settings',
} as const

function severityWeight(value: string): number {
  const idx = (SEVERITY_RANK as readonly string[]).indexOf(value)
  return idx === -1 ? 999 : idx
}

function statusGlyph(value: string): HealthState {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  if (value === '维护中') return 'maintenance'
  return 'offline'
}

function statusTone(value: string): BadgeTone {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  if (value === '维护中') return 'maintenance'
  return 'offline'
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

function notificationSummary(overview: DashboardOverview) {
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

function nodeEntryLink(overview: DashboardOverview) {
  if (overview.pending_onboarding_node_count > 0) return DASHBOARD_LINKS.nodesPendingOnboarding
  if (overview.paused_node_count > 0) return DASHBOARD_LINKS.nodesPaused
  if (overview.retired_node_count > 0) return DASHBOARD_LINKS.nodesRetired
  return DASHBOARD_LINKS.nodes
}

function targetEntryLink(overview: DashboardOverview) {
  if (overview.abnormal_target_count > 0) return DASHBOARD_LINKS.targetsAbnormal
  if (overview.paused_target_count > 0) return DASHBOARD_LINKS.targetsPaused
  if (overview.archived_target_count > 0) return DASHBOARD_LINKS.targetsArchived
  return DASHBOARD_LINKS.targets
}

function nodeManagementStat(overview: DashboardOverview) {
  return `待接入 ${overview.pending_onboarding_node_count} · 暂停 ${overview.paused_node_count} · 退役 ${overview.retired_node_count}`
}

function targetManagementStat(overview: DashboardOverview) {
  return `异常 ${overview.abnormal_target_count} · 暂停 ${overview.paused_target_count} · 归档 ${overview.archived_target_count}`
}

function eventManagementStat(overview: DashboardOverview) {
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

function buildContextItems(
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
      title: affectedGroupCount > 0 ? `${affectedGroupCount} 个分组` : '分组稳定',
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
      title: `${overview.total_node_count} 节点 / ${overview.total_target_count} 目标`,
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
      title: `${overview.recent_new_incident_count}/${overview.recent_recovery_count} 变化`,
      detail: latestEventSummary(overview),
      to: maintenanceTotal > 0 ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.events24h,
      tone: overview.recent_new_incident_count > 0 ? 'notice' : 'normal',
      timestampAt: latestEventTimestamp(overview),
    },
  ]
}

function buildFleetState(
  overview: DashboardOverview,
  abnormalTotal: number,
  severeTotal: number,
  maintenanceTotal: number,
  isFreshInstall: boolean,
): FleetState {
  const recentSummary = `最近 24h 新增 ${overview.recent_new_incident_count} 次异常，恢复 ${overview.recent_recovery_count} 次。`

  if (isFreshInstall) {
    return {
      title: '开始接入第一台服务器',
      description: '候风还没有节点与目标。先创建节点并接入 agent，再创建观测目标与 ProbeItem。',
      tone: 'notice',
      primaryCta: { label: '创建第一个节点', to: DASHBOARD_LINKS.nodes },
      secondaryCtas: [],
    }
  }

  if (severeTotal > 0) {
    return {
      title: '需要处理严重异常',
      description: `${abnormalTotal} 个对象异常，其中 ${severeTotal} 个严重；${recentSummary}`,
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
      description: `${abnormalTotal} 个对象需要关注；${recentSummary}`,
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
      description: `${maintenanceTotal} 个对象处于维护相关状态；${recentSummary}`,
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
    description: `当前没有活跃异常；${recentSummary}`,
    tone: 'normal',
    primaryCta: { label: '查看节点', to: DASHBOARD_LINKS.nodes },
    secondaryCtas: [
      { label: '查看事件流', to: DASHBOARD_LINKS.events24h },
      { label: '进入设置', to: DASHBOARD_LINKS.settings },
    ],
  }
}

function buildAttentionItems(overview: DashboardOverview): AttentionItem[] {
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

function buildDashboardMetrics(
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

function FleetStatePanel({
  overview,
  fleetState,
  metrics,
}: {
  overview: DashboardOverview
  fleetState: FleetState
  metrics: DashboardMetric[]
}) {
  return (
    <section
      className={`dashboard-status-bar dashboard-status-bar--${fleetState.tone}`}
      aria-label="Dashboard 状态"
    >
      <div className="dashboard-status-bar__body">
        <p className="dashboard-status-bar__eyebrow">全局状态</p>
        <h1 className="dashboard-status-bar__title">{fleetState.title}</h1>
        <p className="dashboard-status-bar__description">{fleetState.description}</p>
        <div className="dashboard-status-bar__meta">
          <span>
            摘要生成 <Timestamp value={overview.snapshot_generated_at} mode="absolute" />
          </span>
          {metrics.length > 0 ? (
            <div className="dashboard-status-bar__metrics" aria-label="关键状态指标">
              {metrics.map((metric) => (
                <Link
                  className={`dashboard-inline-metric${metric.tone ? ` dashboard-inline-metric--${metric.tone}` : ''}`}
                  to={metric.to}
                  key={metric.label}
                  aria-label={`${metric.label}：${metric.detail}`}
                >
                  <span className="dashboard-inline-metric__label">{metric.label}</span>
                  <strong className="dashboard-inline-metric__value">
                    <MonoDigits>{metric.value}</MonoDigits>
                  </strong>
                  <span className="dashboard-inline-metric__detail">{metric.detail}</span>
                </Link>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      <div className="dashboard-status-bar__actions" aria-label="首页主要入口">
        <Link className="btn btn--primary btn--md" to={fleetState.primaryCta.to}>
          {fleetState.primaryCta.label}
        </Link>
        {fleetState.secondaryCtas.map((cta, index) => (
          <Link
            className={`btn ${index === 0 ? 'btn--secondary' : 'btn--ghost'} btn--md`}
            to={cta.to}
            key={cta.label}
          >
            {cta.label}
          </Link>
        ))}
      </div>
    </section>
  )
}

function AttentionQueue({
  items,
}: {
  items: AttentionItem[]
}) {
  const visibleItems = items.slice(0, MAX_ATTENTION_ITEMS)

  return (
    <div className="dashboard-attention" aria-label="异常处理队列">
      <div className="dashboard-attention-list">
        {visibleItems.map((item) => (
          <article
            className={`dashboard-attention-item dashboard-attention-item--${statusTone(item.health)}`}
            key={`${item.kind}-${item.id}`}
          >
            <Link
              className="dashboard-attention-item__main"
              to={item.route}
              aria-label={`进入${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
            >
              <div className="dashboard-attention-item__status">
                <StatusGlyph
                  state={statusGlyph(item.health)}
                  size="md"
                  ariaLabel={`${item.name} 健康 ${item.health}`}
                />
              </div>
              <div className="dashboard-attention-item__identity">
                <Hostname truncate maxChars={28} className="dashboard-attention-item__technical-id">
                  {item.technicalId}
                </Hostname>
                <h3 className="dashboard-attention-item__name">{item.name}</h3>
                <p className="dashboard-attention-item__meta">
                  {item.meta} · {item.location} · {item.freshnessLabel}{' '}
                  <Timestamp value={item.freshnessAt ?? null} mode="relative" />
                </p>
              </div>
              <div className="dashboard-attention-item__issue">
                <Badge variant="state" tone={statusTone(item.health)} withDot>
                  {item.health}
                </Badge>
                <p>
                  <MonoDigits>{item.incidentCount}</MonoDigits> {item.issueSummary}
                </p>
              </div>
            </Link>
            <Link
              className="text-link dashboard-attention-item__link"
              to={item.route}
              aria-label={`查看${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => event.stopPropagation()}
            >
              进入
            </Link>
          </article>
        ))}
      </div>
      {items.length > visibleItems.length ? (
        <p className="dashboard-attention__limit">
          首页显示最高优先级 <MonoDigits>{visibleItems.length}</MonoDigits> 项；完整队列请进入节点、目标或事件页处理。
        </p>
      ) : null}
    </div>
  )
}

function ManagementEntries({ overview }: { overview: DashboardOverview }) {
  const entries: ManagementEntry[] = [
    {
      title: '节点',
      stat: nodeManagementStat(overview),
      to: nodeEntryLink(overview),
    },
    {
      title: '目标',
      stat: targetManagementStat(overview),
      to: targetEntryLink(overview),
    },
    {
      title: '事件',
      stat: eventManagementStat(overview),
      to: DASHBOARD_LINKS.events24h,
    },
    {
      title: '设置',
      stat: notificationSummary(overview),
      to: DASHBOARD_LINKS.settings,
    },
  ]

  return (
    <div className="dashboard-management" aria-label="管理入口">
      <div className="dashboard-management__header">
        <h3>管理入口</h3>
        <Link className="text-link" to={DASHBOARD_LINKS.events24h}>
          查看事件流
        </Link>
      </div>
      <div className="dashboard-management__grid">
        {entries.map((entry) => (
          <Link
            className="dashboard-management-entry"
            to={entry.to}
            key={entry.title}
            aria-label={`${entry.title}：${entry.stat}`}
          >
            <span className="dashboard-management-entry__title">{entry.title}</span>
            <span className="dashboard-management-entry__stat">{entry.stat}</span>
          </Link>
        ))}
      </div>
    </div>
  )
}

function DashboardContextStrip({ items }: { items: ContextItem[] }) {
  return (
    <div className="dashboard-context-strip" aria-label="运行上下文">
      {items.map((item) => (
        <Link
          className={`dashboard-context-item${item.tone ? ` dashboard-context-item--${item.tone}` : ''}`}
          to={item.to}
          key={item.label}
          aria-label={`${item.label}：${item.detail}`}
        >
          <span className="dashboard-context-item__label">{item.label}</span>
          <strong className="dashboard-context-item__title">{item.title}</strong>
          <span className="dashboard-context-item__detail">
            {item.detail}
            {item.timestampAt ? (
              <>
                {' · '}
                <Timestamp value={item.timestampAt} mode="relative" />
              </>
            ) : null}
          </span>
        </Link>
      ))}
    </div>
  )
}

function RunningOverview({
  overview,
  maintenanceTotal,
  contextItems,
}: {
  overview: DashboardOverview
  maintenanceTotal: number
  contextItems: ContextItem[]
}) {
  const isMaintenance = maintenanceTotal > 0

  return (
    <div className="dashboard-overview-panel">
      <div className="dashboard-overview-panel__summary">
        <StatusGlyph state={isMaintenance ? 'maintenance' : 'normal'} size="md" />
        <div>
          <h3>{isMaintenance ? '维护观察中' : '当前没有活跃异常'}</h3>
          <p>
            {isMaintenance
              ? '维护对象进入观察状态，首页保留事件和库存上下文，不把维护态提升为紧急异常。'
              : '处理队列保持为空，首页转为运行概览与管理入口。'}
          </p>
        </div>
      </div>
      <div className="dashboard-overview-metrics" aria-label={isMaintenance ? '维护观察指标' : '运行概览指标'}>
        <Link className="dashboard-overview-metric" to={DASHBOARD_LINKS.nodes}>
          <span>节点库存</span>
          <strong>
            <MonoDigits>{overview.total_node_count}</MonoDigits>
          </strong>
          <small>
            待接入 <MonoDigits>{overview.pending_onboarding_node_count}</MonoDigits> · 暂停{' '}
            <MonoDigits>{overview.paused_node_count}</MonoDigits>
          </small>
        </Link>
        <Link className="dashboard-overview-metric" to={DASHBOARD_LINKS.targets}>
          <span>目标库存</span>
          <strong>
            <MonoDigits>{overview.total_target_count}</MonoDigits>
          </strong>
          <small>
            暂停 <MonoDigits>{overview.paused_target_count}</MonoDigits> · 归档{' '}
            <MonoDigits>{overview.archived_target_count}</MonoDigits>
          </small>
        </Link>
        <Link
          className="dashboard-overview-metric"
          to={isMaintenance ? DASHBOARD_LINKS.eventsMaintenance : DASHBOARD_LINKS.events24h}
        >
          <span>{isMaintenance ? '维护事件' : '24h 变化'}</span>
          <strong>
            <MonoDigits>
              {isMaintenance ? maintenanceTotal : overview.recent_new_incident_count + overview.recent_recovery_count}
            </MonoDigits>
          </strong>
          <small>
            新增 <MonoDigits>{overview.recent_new_incident_count}</MonoDigits> · 恢复{' '}
            <MonoDigits>{overview.recent_recovery_count}</MonoDigits>
          </small>
        </Link>
      </div>
      <DashboardContextStrip items={contextItems} />
      <ManagementEntries overview={overview} />
    </div>
  )
}

function OnboardingWorkbench() {
  const steps = [
    {
      title: '创建节点',
      description: '登记第一台服务器。',
      to: DASHBOARD_LINKS.nodes,
      cta: '创建第一个节点',
    },
    {
      title: '接入 agent',
      description: '进入节点详情完成 agent 接入。',
      to: DASHBOARD_LINKS.nodesPendingOnboarding,
      cta: '查看节点接入',
    },
    {
      title: '创建目标',
      description: '添加需要观测的服务或端口。',
      to: DASHBOARD_LINKS.targets,
      cta: '创建第一个目标',
    },
    {
      title: '添加 ProbeItem',
      description: '在目标详情中补齐探测项。',
      to: DASHBOARD_LINKS.targets,
      cta: '添加 ProbeItem',
    },
  ]

  return (
    <div className="dashboard-onboarding">
      {steps.map((step, index) => (
        <article className="dashboard-onboarding__step" key={step.title}>
          <span className="dashboard-onboarding__index">
            <MonoDigits>{index + 1}</MonoDigits>
          </span>
          <div className="dashboard-onboarding__body">
            <h3>{step.title}</h3>
            <p>{step.description}</p>
            <Link className="text-link" to={step.to}>
              {step.cta}
            </Link>
          </div>
        </article>
      ))}
    </div>
  )
}

function DashboardWorkbench({
  overview,
  attentionItems,
  abnormalTotal,
  maintenanceTotal,
  isFreshInstall,
}: {
  overview: DashboardOverview
  attentionItems: AttentionItem[]
  abnormalTotal: number
  maintenanceTotal: number
  isFreshInstall: boolean
}) {
  const hasAbnormal = abnormalTotal > 0
  const isMaintenance = !hasAbnormal && maintenanceTotal > 0
  const title = isFreshInstall
    ? '首次接入工作台'
    : hasAbnormal
      ? '当前需要处理'
      : isMaintenance
        ? '维护观察'
        : '运行概览'
  const eyebrow = isFreshInstall
    ? '首次接入'
    : hasAbnormal
      ? undefined
      : isMaintenance
        ? '维护观察'
        : '运行概览'
  const ribbon: 'notice' | 'alert' | 'maintenance' | 'normal' = isFreshInstall
    ? 'notice'
    : hasAbnormal
      ? 'alert'
      : isMaintenance
        ? 'maintenance'
        : 'normal'
  const mode = isFreshInstall ? 'onboarding' : hasAbnormal ? 'abnormal' : isMaintenance ? 'maintenance' : 'normal'

  return (
    <DetailSection
      eyebrow={eyebrow}
      title={title}
      ribbon={ribbon}
      aside={
        hasAbnormal ? (
          <div className="dashboard-section-actions">
            <Link className="text-link" to={DASHBOARD_LINKS.nodesAbnormal}>
              查看全部异常节点
            </Link>
            <Link className="text-link" to={DASHBOARD_LINKS.targetsAbnormal}>
              查看全部异常目标
            </Link>
            <Link className="text-link" to={DASHBOARD_LINKS.events24h}>
              查看事件流
            </Link>
          </div>
        ) : null
      }
    >
      <div className={`dashboard-workbench dashboard-workbench--${mode}`}>
        {isFreshInstall ? (
          <OnboardingWorkbench />
        ) : hasAbnormal ? (
          <>
            <AttentionQueue items={attentionItems} />
            <DashboardContextStrip items={buildContextItems(overview, abnormalTotal, maintenanceTotal)} />
          </>
        ) : (
          <RunningOverview
            overview={overview}
            maintenanceTotal={maintenanceTotal}
            contextItems={buildContextItems(overview, abnormalTotal, maintenanceTotal)}
          />
        )}
      </div>
    </DetailSection>
  )
}

export function DashboardPage() {
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    overview: null,
  })

  useEffect(() => {
    let cancelled = false

    getDashboard()
      .then((overview) => {
        if (cancelled) return
        setState({ loading: false, error: null, overview })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = error instanceof ApiError ? error.message : '加载首页 / Dashboard 失败'
        setState({ loading: false, error: message, overview: null })
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (state.loading) {
    return <section className="page-panel">正在加载首页 / Dashboard…</section>
  }

  if (state.error || !state.overview) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Dashboard</p>
        <h2 className="page-panel__title">首页不可用</h2>
        <p className="page-panel__description">{state.error ?? '未获取到概览数据'}</p>
      </section>
    )
  }

  const overview = state.overview
  const isFreshInstall = overview.total_node_count === 0 && overview.total_target_count === 0
  const abnormalTotal = overview.abnormal_node_count + overview.abnormal_target_count
  const severeTotal = overview.severe_node_count + overview.severe_target_count
  const maintenanceTotal = overview.maintenance_node_count + overview.maintenance_target_count
  const fleetState = buildFleetState(
    overview,
    abnormalTotal,
    severeTotal,
    maintenanceTotal,
    isFreshInstall,
  )
  const metrics = buildDashboardMetrics(
    overview,
    abnormalTotal,
    severeTotal,
    maintenanceTotal,
    isFreshInstall,
  )
  const attentionItems = buildAttentionItems(overview)

  return (
    <div className="page-stack dashboard-page">
      <FleetStatePanel
        overview={overview}
        fleetState={fleetState}
        metrics={metrics}
      />

      <DashboardWorkbench
        overview={overview}
        attentionItems={attentionItems}
        abnormalTotal={abnormalTotal}
        maintenanceTotal={maintenanceTotal}
        isFreshInstall={isFreshInstall}
      />
    </div>
  )
}
