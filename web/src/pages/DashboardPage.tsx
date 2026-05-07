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
import { EventList } from '../components/EventList'
import { ApiError, getDashboard } from '../lib/api'
import type {
  DashboardNodeSummary,
  DashboardOverview,
  DashboardGroupSummary,
  DashboardTargetSummary,
  IncidentSeverity,
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
      primaryCta: { label: '创建第一个节点', to: '/nodes' },
    }
  }

  if (severeTotal > 0) {
    return {
      title: '需要处理严重异常',
      description: `${abnormalTotal} 个对象异常，其中 ${severeTotal} 个严重；${recentSummary}`,
      tone: 'critical',
      primaryCta: { label: '查看当前异常', to: '/events' },
    }
  }

  if (abnormalTotal > 0) {
    return {
      title: '存在活跃异常',
      description: `${abnormalTotal} 个对象需要关注；${recentSummary}`,
      tone: 'alert',
      primaryCta: { label: '查看当前异常', to: '/events' },
    }
  }

  if (maintenanceTotal > 0) {
    return {
      title: '系统处于维护观察中',
      description: `${maintenanceTotal} 个对象处于维护相关状态；${recentSummary}`,
      tone: 'maintenance',
      primaryCta: { label: '查看节点', to: '/nodes' },
    }
  }

  return {
    title: '系统运行正常',
    description: `当前没有活跃异常；${recentSummary}`,
    tone: 'normal',
    primaryCta: { label: '查看节点', to: '/nodes' },
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
            <Link className="btn btn--secondary btn--md" to="/events">
              查看事件流
            </Link>
            <Link className="btn btn--ghost btn--md" to="/settings">
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
        to="/nodes"
        tone={overview.abnormal_node_count > 0 ? 'alert' : 'normal'}
      />
      <KpiLink
        label="目标"
        value={overview.total_target_count}
        description={`${overview.abnormal_target_count} 个异常`}
        to="/targets"
        tone={overview.abnormal_target_count > 0 ? 'alert' : 'normal'}
      />
      <KpiLink
        label="严重"
        value={severeTotal}
        description={`节点 ${overview.severe_node_count} · 目标 ${overview.severe_target_count}`}
        to="/events"
        tone={severeTone}
      />
      <KpiLink
        label="维护"
        value={maintenanceTotal}
        description={`节点 ${overview.maintenance_node_count} · 目标 ${overview.maintenance_target_count}`}
        to="/nodes"
        tone={maintenanceTone}
      />
      <KpiLink
        label="24h 变化"
        value={changeValue}
        description="新增异常 / 恢复"
        to="/events"
        trend={overview.new_incident_trend_24h?.length ? overview.new_incident_trend_24h : overview.recovery_trend_24h}
        trendTone={overview.recent_new_incident_count > 0 ? 'critical' : 'normal'}
      />
    </section>
  )
}

function AttentionQueue({
  items,
  recentEventCount,
}: {
  items: AttentionItem[]
  recentEventCount: number
}) {
  const navigate = useNavigate()

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
          >
            进入
          </Link>
        </span>
      ),
    },
  ]

  return (
    <DetailSection
      eyebrow="处理队列"
      title="当前需要处理"
      ribbon={items.length > 0 ? 'alert' : 'normal'}
      aside={
        <div className="dashboard-section-actions">
          <Link className="text-link" to="/nodes">
            查看全部异常节点
          </Link>
          <Link className="text-link" to="/targets">
            查看全部异常目标
          </Link>
          <Link className="text-link" to="/events">
            查看事件流
          </Link>
        </div>
      }
    >
      <DataTable<AttentionItem>
        columns={columns}
        rows={items}
        rowKey={(item) => `${item.kind}-${item.id}`}
        density="compact"
        className="dashboard-table dashboard-attention-table"
        onRowClick={(item) => navigate(item.route)}
        emptyContent={
          <div className="empty-state dashboard-empty-state">
            <h3>当前没有活跃异常</h3>
            <p>
              处理队列为空；最近事件中仍有{' '}
              <MonoDigits>{recentEventCount}</MonoDigits> 条状态变化可供回溯。
            </p>
          </div>
        }
      />
    </DetailSection>
  )
}

function SystemEntryPoints({
  overview,
}: {
  overview: DashboardOverview
}) {
  const entries = [
    {
      title: '节点',
      description: '管理服务器、agent 接入、维护与暂停。',
      to: '/nodes',
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
      to: '/targets',
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
      to: '/events',
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
      to: '/settings',
      stat: notificationSummary(overview),
    },
  ]

  return (
    <DetailSection eyebrow="System Entry Points" title="系统入口">
      <div className="dashboard-entry-grid">
        {entries.map((entry) => (
          <Link className="dashboard-entry" to={entry.to} key={entry.title}>
            <span className="dashboard-entry__title">{entry.title}</span>
            <span className="dashboard-entry__description">{entry.description}</span>
            <span className="dashboard-entry__stat">{entry.stat}</span>
          </Link>
        ))}
      </div>
    </DetailSection>
  )
}

function GroupDistribution({ groups }: { groups: DashboardGroupSummary[] }) {
  const columns: DataTableColumn<DashboardGroupSummary>[] = [
    {
      key: 'group',
      label: 'Group',
      render: (group) => (
        <div className="dashboard-table__identity">
          <Hostname truncate maxChars={28} className="dashboard-table__id">
            {group.group}
          </Hostname>
          <span className="dashboard-table__display-name">
            对象 <MonoDigits>{group.node_count + group.target_count}</MonoDigits>
          </span>
        </div>
      ),
    },
    {
      key: 'inventory',
      label: '全量库存',
      render: (group) => (
        <span className="dashboard-group__metric">
          节点 <MonoDigits>{group.node_count}</MonoDigits> · 目标{' '}
          <MonoDigits>{group.target_count}</MonoDigits>
        </span>
      ),
    },
    {
      key: 'abnormal',
      label: '异常',
      render: (group) => (
        <span className="dashboard-group__metric">
          节点 <MonoDigits>{group.abnormal_node_count}</MonoDigits> · 目标{' '}
          <MonoDigits>{group.abnormal_target_count}</MonoDigits>
        </span>
      ),
    },
    {
      key: 'severe',
      label: '严重',
      render: (group) => (
        <span className="dashboard-group__metric">
          节点 <MonoDigits>{group.severe_node_count}</MonoDigits> · 目标{' '}
          <MonoDigits>{group.severe_target_count}</MonoDigits>
        </span>
      ),
    },
    {
      key: 'maintenance',
      label: '维护',
      render: (group) => (
        <span className="dashboard-group__metric">
          节点 <MonoDigits>{group.maintenance_node_count}</MonoDigits> · 目标{' '}
          <MonoDigits>{group.maintenance_target_count}</MonoDigits>
        </span>
      ),
    },
  ]

  return (
    <DetailSection eyebrow="Inventory Distribution" title="按 Group 分布" ribbon="notice">
      <DataTable<DashboardGroupSummary>
        columns={columns}
        rows={groups}
        rowKey={(group) => group.group}
        density="compact"
        className="dashboard-table dashboard-group-table"
        emptyContent={
          <div className="empty-state dashboard-empty-state">
            <h3>暂无 Group 分布</h3>
            <p>当前还没有节点或目标库存，Dashboard 不生成空的未分组行。</p>
          </div>
        }
      />
    </DetailSection>
  )
}

function OnboardingWorkbench() {
  const steps = [
    {
      title: '创建节点',
      description: '登记第一台服务器。',
      to: '/nodes',
      cta: '创建第一个节点',
    },
    {
      title: '接入 agent',
      description: '进入节点详情完成 agent 接入。',
      to: '/nodes',
      cta: '查看节点接入',
    },
    {
      title: '创建目标',
      description: '添加需要观测的服务或端口。',
      to: '/targets',
      cta: '创建第一个目标',
    },
    {
      title: '添加 ProbeItem',
      description: '在目标详情中补齐探测项。',
      to: '/targets',
      cta: '添加 ProbeItem',
    },
  ]

  return (
    <DetailSection eyebrow="First Run" title="首次接入工作台" ribbon="notice">
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

      {isFreshInstall ? (
        <OnboardingWorkbench />
      ) : (
        <AttentionQueue items={attentionItems} recentEventCount={overview.recent_events.length} />
      )}

      <SystemEntryPoints overview={overview} />

      <GroupDistribution groups={overview.group_summaries} />

      <DetailSection
        eyebrow="Recent Events"
        title="最近事件"
        aside={
          <Link className="text-link" to="/events">
            查看全部事件
          </Link>
        }
      >
        <EventList
          events={overview.recent_events}
          emptyTitle="最近没有状态变更事件"
          emptyDescription="当前没有新的异常变化，首页事件流保持为空。"
        />
      </DetailSection>
    </div>
  )
}
