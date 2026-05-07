import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  Badge,
  DataTable,
  type DataTableColumn,
  Hostname,
  MonoDigits,
  Sparkline,
  StatusGlyph,
  Timestamp,
  type BadgeTone,
  type HealthState,
  type SparklineTone,
} from '../components/atoms'
import { DetailSection } from '../components/DetailSection'
import { ApiError, getDashboard } from '../lib/api'
import {
  STATE_CHANGE_EVENT_TYPE_LABELS,
  type DashboardGroupSummary,
  type DashboardNodeSummary,
  type DashboardOverview,
  type DashboardTargetSummary,
  type IncidentSeverity,
  type StateChangeEventRecord,
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

const SEVERITY_RANK = ['严重', '告警', '关注', '维护中', '正常'] as const
const MAX_ATTENTION_ITEMS = 6
const MAX_CONTEXT_GROUPS = 3
const MAX_CONTEXT_EVENTS = 4
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
    }
  }

  if (severeTotal > 0) {
    return {
      title: '需要处理严重异常',
      description: `${abnormalTotal} 个对象异常，其中 ${severeTotal} 个严重；${recentSummary}`,
      tone: 'critical',
      primaryCta: { label: '查看当前异常', to: DASHBOARD_LINKS.eventsSevere },
    }
  }

  if (abnormalTotal > 0) {
    return {
      title: '存在活跃异常',
      description: `${abnormalTotal} 个对象需要关注；${recentSummary}`,
      tone: 'alert',
      primaryCta: { label: '查看当前异常', to: DASHBOARD_LINKS.events24h },
    }
  }

  if (maintenanceTotal > 0) {
    return {
      title: '系统处于维护观察中',
      description: `${maintenanceTotal} 个对象处于维护相关状态；${recentSummary}`,
      tone: 'maintenance',
      primaryCta: { label: '查看维护事件', to: DASHBOARD_LINKS.eventsMaintenance },
    }
  }

  return {
    title: '系统运行正常',
    description: `当前没有活跃异常；${recentSummary}`,
    tone: 'normal',
    primaryCta: { label: '查看节点', to: DASHBOARD_LINKS.nodes },
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

function groupObjectCount(group: DashboardGroupSummary) {
  return group.node_count + group.target_count
}

function groupAttentionCount(group: DashboardGroupSummary) {
  return (
    group.abnormal_node_count +
    group.abnormal_target_count +
    group.severe_node_count +
    group.severe_target_count +
    group.maintenance_node_count +
    group.maintenance_target_count
  )
}

function topGroups(groups: DashboardGroupSummary[]) {
  return [...groups]
    .sort((a, b) => {
      const attentionDelta = groupAttentionCount(b) - groupAttentionCount(a)
      if (attentionDelta !== 0) return attentionDelta
      return groupObjectCount(b) - groupObjectCount(a)
    })
    .slice(0, MAX_CONTEXT_GROUPS)
}

function eventTypeLabel(value: StateChangeEventRecord['event_type']) {
  return STATE_CHANGE_EVENT_TYPE_LABELS[value] ?? value
}

function objectTypeLabel(value: StateChangeEventRecord['object_type']) {
  if (value === 'node') return '节点'
  return '目标'
}

function FleetStatePanel({
  overview,
  fleetState,
  abnormalTotal,
  severeTotal,
  maintenanceTotal,
}: {
  overview: DashboardOverview
  fleetState: FleetState
  abnormalTotal: number
  severeTotal: number
  maintenanceTotal: number
}) {
  return (
    <section className={`hero-panel dashboard-fleet dashboard-fleet--${fleetState.tone}`}>
      <div className="dashboard-fleet__content">
        <div className="dashboard-fleet__main">
          <p className="hero-panel__eyebrow">Fleet State</p>
          <h1 className="hero-panel__title">{fleetState.title}</h1>
          <p className="hero-panel__description">{fleetState.description}</p>
          <div className="dashboard-fleet__actions" aria-label="首页主要入口">
            <Link className="btn btn--primary btn--md" to={fleetState.primaryCta.to}>
              {fleetState.primaryCta.label}
            </Link>
            <Link className="btn btn--secondary btn--md" to={DASHBOARD_LINKS.events24h}>
              查看事件流
            </Link>
            <Link className="btn btn--ghost btn--md" to={DASHBOARD_LINKS.settings}>
              进入设置
            </Link>
          </div>
        </div>
        <dl className="dashboard-fleet__facts" aria-label="首页数据可信度">
          <div>
            <dt>API</dt>
            <dd>已加载 /api/dashboard</dd>
          </div>
          <div>
            <dt>生成时间</dt>
            <dd>
              Dashboard 摘要 <Timestamp value={overview.snapshot_generated_at} mode="absolute" />
            </dd>
          </div>
          <div>
            <dt>库存</dt>
            <dd>
              节点 <MonoDigits>{overview.total_node_count}</MonoDigits> · 目标{' '}
              <MonoDigits>{overview.total_target_count}</MonoDigits>
            </dd>
          </div>
          <div>
            <dt>当前队列</dt>
            <dd>
              异常 <MonoDigits>{abnormalTotal}</MonoDigits> · 严重{' '}
              <MonoDigits>{severeTotal}</MonoDigits> · 维护{' '}
              <MonoDigits>{maintenanceTotal}</MonoDigits>
            </dd>
          </div>
        </dl>
      </div>
    </section>
  )
}

function KpiLink({
  label,
  value,
  description,
  to,
  tone = 'neutral',
  trend,
  trendTone = 'accent',
}: {
  label: string
  value: number | string
  description: string
  to: string
  tone?: BadgeTone
  trend?: number[]
  trendTone?: SparklineTone
}) {
  return (
    <Link className="dashboard-kpi" to={to} aria-label={`${label}：${description}`}>
      <span className="dashboard-kpi__label">{label}</span>
      <span className="dashboard-kpi__value">
        <MonoDigits>{value}</MonoDigits>
      </span>
      <span className="dashboard-kpi__description">{description}</span>
      {trend && trend.length > 0 ? (
        <span className="dashboard-kpi__trend">
          <Sparkline
            values={trend}
            tone={trendTone}
            width={120}
            height={20}
            ariaLabel={`${label} 近 24h 趋势`}
          />
        </span>
      ) : null}
      {tone !== 'neutral' ? <span className={`dashboard-kpi__rail tone--${tone}`} aria-hidden /> : null}
    </Link>
  )
}

function GlobalKpiStrip({
  overview,
  severeTotal,
  maintenanceTotal,
}: {
  overview: DashboardOverview
  severeTotal: number
  maintenanceTotal: number
}) {
  const changeValue = `${overview.recent_new_incident_count}/${overview.recent_recovery_count}`
  const severeTone: BadgeTone = severeTotal > 0 ? 'critical' : 'neutral'
  const maintenanceTone: BadgeTone = maintenanceTotal > 0 ? 'maintenance' : 'neutral'

  return (
    <section className="dashboard-kpi-strip" aria-label="系统全局指标">
      <KpiLink
        label="节点"
        value={overview.total_node_count}
        description={`${overview.abnormal_node_count} 个异常`}
        to={overview.abnormal_node_count > 0 ? DASHBOARD_LINKS.nodesAbnormal : DASHBOARD_LINKS.nodes}
        tone={overview.abnormal_node_count > 0 ? 'alert' : 'normal'}
      />
      <KpiLink
        label="目标"
        value={overview.total_target_count}
        description={`${overview.abnormal_target_count} 个异常`}
        to={overview.abnormal_target_count > 0 ? DASHBOARD_LINKS.targetsAbnormal : DASHBOARD_LINKS.targets}
        tone={overview.abnormal_target_count > 0 ? 'alert' : 'normal'}
      />
      <KpiLink
        label="严重"
        value={severeTotal}
        description={`节点 ${overview.severe_node_count} · 目标 ${overview.severe_target_count}`}
        to={DASHBOARD_LINKS.eventsSevere}
        tone={severeTone}
      />
      <KpiLink
        label="维护"
        value={maintenanceTotal}
        description={`节点 ${overview.maintenance_node_count} · 目标 ${overview.maintenance_target_count}`}
        to={DASHBOARD_LINKS.eventsMaintenance}
        tone={maintenanceTone}
      />
      <KpiLink
        label="24h 变化"
        value={changeValue}
        description="新增异常 / 恢复"
        to={DASHBOARD_LINKS.events24h}
        trend={overview.new_incident_trend_24h?.length ? overview.new_incident_trend_24h : overview.recovery_trend_24h}
        trendTone={overview.recent_new_incident_count > 0 ? 'critical' : 'normal'}
      />
    </section>
  )
}

function AttentionQueue({
  items,
}: {
  items: AttentionItem[]
}) {
  const navigate = useNavigate()
  const visibleItems = items.slice(0, MAX_ATTENTION_ITEMS)

  const columns: DataTableColumn<AttentionItem>[] = [
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (item) => (
        <StatusGlyph
          state={statusGlyph(item.health)}
          size="md"
          ariaLabel={`${item.name} 健康 ${item.health}`}
        />
      ),
    },
    {
      key: 'object',
      label: '对象',
      render: (item) => (
        <div className="dashboard-table__identity">
          <Hostname truncate maxChars={24} className="dashboard-table__id">
            {item.technicalId}
          </Hostname>
          <span className="dashboard-table__display-name">{item.name}</span>
          <span className="dashboard-table__freshness">
            {item.freshnessLabel} <Timestamp value={item.freshnessAt ?? null} mode="relative" />
          </span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型',
      render: (item) => (
        <div className="dashboard-table__stack">
          <Badge variant="info" tone={item.kind === 'node' ? 'notice' : 'normal'}>
            {item.meta}
          </Badge>
          <span className="dashboard-table__location">{item.location}</span>
        </div>
      ),
    },
    {
      key: 'health',
      label: '状态',
      render: (item) => (
        <Badge variant="state" tone={statusTone(item.health)} withDot>
          {item.health}
        </Badge>
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      render: (item) => (
        <div className="dashboard-table__issue">
          <MonoDigits className="dashboard-table__issue-count">
            {item.incidentCount}
          </MonoDigits>
          <span className="dashboard-table__issue-summary">{item.issueSummary}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      width: 92,
      cellClassName: 'dashboard-table__actions-cell',
      render: (item) => (
        <span className="dashboard-table__actions">
          <Link
            className="text-link"
            to={item.route}
            aria-label={`查看${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => event.stopPropagation()}
          >
            进入
          </Link>
        </span>
      ),
    },
  ]

  return (
    <div className="dashboard-attention" aria-label="异常处理队列">
      <DataTable<AttentionItem>
        columns={columns}
        rows={visibleItems}
        rowKey={(item) => `${item.kind}-${item.id}`}
        density="compact"
        className="dashboard-table dashboard-attention-table"
        onRowClick={(item) => navigate(item.route)}
      />
      {items.length > visibleItems.length ? (
        <p className="dashboard-attention__limit">
          首页显示最高优先级 <MonoDigits>{visibleItems.length}</MonoDigits> 项；完整队列请进入节点、目标或事件页处理。
        </p>
      ) : null}
    </div>
  )
}

function ShortcutRail({
  overview,
}: {
  overview: DashboardOverview
}) {
  const entries = [
    {
      title: '节点',
      description: '管理服务器、agent 接入、维护与暂停。',
      to: nodeEntryLink(overview),
      stat: (
        <>
          待接入 <MonoDigits>{overview.pending_onboarding_node_count}</MonoDigits> · 暂停{' '}
          <MonoDigits>{overview.paused_node_count}</MonoDigits> · 退役{' '}
          <MonoDigits>{overview.retired_node_count}</MonoDigits>
        </>
      ),
    },
    {
      title: '目标',
      description: '管理观测目标、ProbeItem 与运行状态。',
      to: targetEntryLink(overview),
      stat: (
        <>
          暂停 <MonoDigits>{overview.paused_target_count}</MonoDigits> · 归档{' '}
          <MonoDigits>{overview.archived_target_count}</MonoDigits> · 异常{' '}
          <MonoDigits>{overview.abnormal_target_count}</MonoDigits>
        </>
      ),
    },
    {
      title: '事件',
      description: '查看异常开始、升级、恢复与维护历史。',
      to: DASHBOARD_LINKS.events24h,
      stat: (
        <>
          24h 新增 <MonoDigits>{overview.recent_new_incident_count}</MonoDigits> · 恢复{' '}
          <MonoDigits>{overview.recent_recovery_count}</MonoDigits>
        </>
      ),
    },
    {
      title: '设置',
      description: '进入通知、阈值、频率与保留策略配置。',
      to: DASHBOARD_LINKS.settings,
      stat: notificationSummary(overview),
    },
  ]

  return (
    <div className="dashboard-context-card" aria-label="系统快捷入口">
      <div className="dashboard-context-card__header">
        <p className="dashboard-context-card__eyebrow">Shortcuts</p>
        <h3 className="dashboard-context-card__title">系统快捷入口</h3>
      </div>
      <div className="dashboard-shortcut-list">
        {entries.map((entry) => (
          <Link className="dashboard-shortcut" to={entry.to} key={entry.title}>
            <span className="dashboard-shortcut__title">{entry.title}</span>
            <span className="dashboard-shortcut__description">{entry.description}</span>
            <span className="dashboard-shortcut__stat">{entry.stat}</span>
          </Link>
        ))}
      </div>
    </div>
  )
}

function GroupContextSummary({ groups }: { groups: DashboardGroupSummary[] }) {
  const visibleGroups = topGroups(groups)

  return (
    <div className="dashboard-context-card" aria-label="Group 上下文摘要">
      <div className="dashboard-context-card__header">
        <p className="dashboard-context-card__eyebrow">Inventory Context</p>
        <h3 className="dashboard-context-card__title">Group 摘要</h3>
      </div>
      {visibleGroups.length > 0 ? (
        <div className="dashboard-context-list">
          {visibleGroups.map((group) => (
            <div className="dashboard-context-row" key={group.group}>
              <div className="dashboard-context-row__main">
                <Hostname truncate maxChars={24}>
                  {group.group}
                </Hostname>
                <span>
                  节点 <MonoDigits>{group.node_count}</MonoDigits> · 目标{' '}
                  <MonoDigits>{group.target_count}</MonoDigits>
                </span>
              </div>
              <span className="dashboard-context-row__meta">
                异常 <MonoDigits>{group.abnormal_node_count + group.abnormal_target_count}</MonoDigits>
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className="dashboard-context-card__empty">暂无 Group 摘要；Dashboard 不生成空的未分组行。</p>
      )}
    </div>
  )
}

function RecentEventsContext({ events }: { events: StateChangeEventRecord[] }) {
  const visibleEvents = events.slice(0, MAX_CONTEXT_EVENTS)

  return (
    <div className="dashboard-context-card" aria-label="最近事件上下文摘要">
      <div className="dashboard-context-card__header dashboard-context-card__header--split">
        <div>
          <p className="dashboard-context-card__eyebrow">Recent Events</p>
          <h3 className="dashboard-context-card__title">最近事件摘要</h3>
        </div>
        <Link className="text-link" to={DASHBOARD_LINKS.events24h}>
          查看全部事件
        </Link>
      </div>
      {visibleEvents.length > 0 ? (
        <div className="dashboard-event-list">
          {visibleEvents.map((event) => (
            <article
              className="dashboard-event"
              key={event.event_id ?? `${event.created_at}-${event.incident_id}-${event.event_type}`}
            >
              <StatusGlyph state={statusGlyph(event.severity)} size="sm" ariaLabel={event.severity || '事件'} />
              <div className="dashboard-event__body">
                <div className="dashboard-event__title">
                  <span>{eventTypeLabel(event.event_type)}</span>
                  <Badge variant="info" tone={event.object_type === 'node' ? 'notice' : 'normal'}>
                    {objectTypeLabel(event.object_type)}
                  </Badge>
                </div>
                <p>{event.summary || '暂无摘要'}</p>
                <span className="dashboard-event__meta">
                  <Hostname truncate maxChars={22}>{event.object_id}</Hostname> ·{' '}
                  <Timestamp value={event.created_at} mode="relative" />
                </span>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <p className="dashboard-context-card__empty">最近没有状态变更事件。</p>
      )}
    </div>
  )
}

function RunningOverview({
  overview,
  maintenanceTotal,
}: {
  overview: DashboardOverview
  maintenanceTotal: number
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
    ? 'First Run'
    : hasAbnormal
      ? '处理队列'
      : isMaintenance
        ? 'Maintenance Watch'
        : 'Running Overview'
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
        <div className="dashboard-workbench__main">
          {isFreshInstall ? (
            <OnboardingWorkbench />
          ) : hasAbnormal ? (
            <AttentionQueue items={attentionItems} />
          ) : (
            <RunningOverview overview={overview} maintenanceTotal={maintenanceTotal} />
          )}
        </div>
        <aside className="dashboard-workbench__context" aria-label="工作台上下文">
          <ShortcutRail overview={overview} />
          {isFreshInstall ? null : (
            <>
              <GroupContextSummary groups={overview.group_summaries} />
              <RecentEventsContext events={overview.recent_events} />
            </>
          )}
        </aside>
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
        <p className="page-panel__eyebrow">Fleet State</p>
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
  const attentionItems = buildAttentionItems(overview)

  return (
    <div className="page-stack dashboard-page">
      <FleetStatePanel
        overview={overview}
        fleetState={fleetState}
        abnormalTotal={abnormalTotal}
        severeTotal={severeTotal}
        maintenanceTotal={maintenanceTotal}
      />

      <GlobalKpiStrip
        overview={overview}
        severeTotal={severeTotal}
        maintenanceTotal={maintenanceTotal}
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
