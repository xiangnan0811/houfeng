import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  DataTable,
  type DataTableColumn,
  Hostname,
  MonoDigits,
  Sparkline,
  StatusGlyph,
  Timestamp,
  type HealthState,
  type SparklineTone,
} from '../components/atoms'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { ApiError, getDashboard } from '../lib/api'
import type { DashboardNodeSummary, DashboardOverview, DashboardTargetSummary } from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
}

function StatTile({
  label,
  value,
  ariaLabel,
  trend,
  trendTone = 'accent',
  trendAriaLabel,
}: {
  label: string
  value: number | string
  ariaLabel?: string
  /** Optional 24-bucket time series. When non-empty renders a Sparkline below the value. */
  trend?: number[]
  trendTone?: SparklineTone
  trendAriaLabel?: string
}) {
  return (
    <article
      className="summary-card stat-tile"
      role="group"
      aria-label={ariaLabel ?? label}
    >
      <p className="summary-card__label stat-tile__label">{label}</p>
      <p className="summary-card__value stat-tile__value">
        <MonoDigits>{value}</MonoDigits>
      </p>
      {trend && trend.length > 0 ? (
        <div className="stat-tile__trend">
          <Sparkline
            values={trend}
            tone={trendTone}
            width={120}
            height={20}
            ariaLabel={trendAriaLabel ?? `${label} 近 24h 趋势`}
          />
        </div>
      ) : null}
    </article>
  )
}

function statusGlyph(value: string): HealthState {
  if (value === '正常') return 'normal'
  if (value === '关注') return 'notice'
  if (value === '告警') return 'alert'
  if (value === '严重') return 'critical'
  if (value === '维护中') return 'maintenance'
  return 'offline'
}

const SEVERITY_RANK = ['严重', '告警', '关注', '维护中', '正常'] as const
function severityWeight(v: string): number {
  const idx = (SEVERITY_RANK as readonly string[]).indexOf(v)
  return idx === -1 ? 999 : idx
}

function hostPortSummary(target: DashboardTargetSummary) {
  return typeof target.base_port === 'number' ? `${target.host}:${target.base_port}` : target.host
}

function AbnormalNodeList({ nodes }: { nodes: DashboardNodeSummary[] }) {
  const navigate = useNavigate()

  if (nodes.length === 0) {
    return (
      <div className="empty-state">
        <h3>当前没有异常节点</h3>
        <p>节点侧暂未发现需要处理的活跃异常。</p>
      </div>
    )
  }

  const sorted = [...nodes].sort(
    (a, b) =>
      severityWeight(a.current_health_status) - severityWeight(b.current_health_status),
  )

  const columns: DataTableColumn<DashboardNodeSummary>[] = [
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (node) => (
        <StatusGlyph
          state={statusGlyph(node.current_health_status)}
          size="md"
          ariaLabel={`${node.display_name} 健康 ${node.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '节点',
      render: (node) => (
        <div className="dashboard-table__identity">
          <Hostname truncate maxChars={14} className="dashboard-table__id">
            {node.node_id}
          </Hostname>
          <span className="dashboard-table__display-name">{node.display_name}</span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置',
      render: (node) => (
        <span className="dashboard-table__location">
          {[node.region, node.city, node.provider].filter(Boolean).join(' · ')}
        </span>
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      render: (node) => (
        <div className="dashboard-table__issue">
          <MonoDigits className="dashboard-table__issue-count">
            {node.current_active_incident_count}
          </MonoDigits>
          <span className="dashboard-table__issue-summary">
            {node.current_primary_issue_summary || '暂无关键异常摘要'}
          </span>
        </div>
      ),
    },
    {
      key: 'heartbeat',
      label: '心跳',
      render: (node) => (
        <Timestamp value={node.last_heartbeat_at ?? null} mode="relative" />
      ),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      cellClassName: 'dashboard-table__actions-cell',
      render: (node) => (
        <span className="dashboard-table__actions">
          <Link
            className="text-link"
            to={`/nodes/${node.node_id}`}
            aria-label={`查看节点 ${node.display_name}`}
            onClick={(event) => event.stopPropagation()}
          >
            查看节点
          </Link>
        </span>
      ),
    },
  ]

  return (
    <DataTable<DashboardNodeSummary>
      columns={columns}
      rows={sorted}
      rowKey={(node) => node.node_id}
      density="compact"
      className="dashboard-table"
      onRowClick={(node) => navigate(`/nodes/${node.node_id}`)}
    />
  )
}

function AbnormalTargetList({ targets }: { targets: DashboardTargetSummary[] }) {
  const navigate = useNavigate()

  if (targets.length === 0) {
    return (
      <div className="empty-state">
        <h3>当前没有异常目标</h3>
        <p>目标侧暂未发现需要处理的活跃异常。</p>
      </div>
    )
  }

  const sorted = [...targets].sort(
    (a, b) =>
      severityWeight(a.current_health_status) - severityWeight(b.current_health_status),
  )

  const columns: DataTableColumn<DashboardTargetSummary>[] = [
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (target) => (
        <StatusGlyph
          state={statusGlyph(target.current_health_status)}
          size="md"
          ariaLabel={`${target.name} 健康 ${target.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '目标',
      render: (target) => (
        <div className="dashboard-table__identity">
          <Hostname className="dashboard-table__id">{hostPortSummary(target)}</Hostname>
          <span className="dashboard-table__display-name">{target.name}</span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型',
      render: (target) => (
        <span className="dashboard-table__type">{target.target_type}</span>
      ),
    },
    {
      key: 'issue',
      label: '当前主问题',
      render: (target) => (
        <div className="dashboard-table__issue">
          <MonoDigits className="dashboard-table__issue-count">
            {target.current_active_incident_count}
          </MonoDigits>
          <span className="dashboard-table__issue-summary">
            {target.current_primary_issue_summary || '暂无关键异常摘要'}
          </span>
        </div>
      ),
    },
    {
      key: 'last-success',
      label: '最近成功',
      render: (target) => (
        <Timestamp value={target.last_success_at ?? null} mode="relative" />
      ),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      cellClassName: 'dashboard-table__actions-cell',
      render: (target) => (
        <span className="dashboard-table__actions">
          <Link
            className="text-link"
            to={`/targets/${target.target_id}`}
            aria-label={`查看目标 ${target.name}`}
            onClick={(event) => event.stopPropagation()}
          >
            查看目标
          </Link>
        </span>
      ),
    },
  ]

  return (
    <DataTable<DashboardTargetSummary>
      columns={columns}
      rows={sorted}
      rowKey={(target) => target.target_id}
      density="compact"
      className="dashboard-table"
      onRowClick={(target) => navigate(`/targets/${target.target_id}`)}
    />
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
        const message = error instanceof ApiError ? error.message : '加载首页 / Dashboard失败'
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
        <p className="page-panel__eyebrow">当前风险总览</p>
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

  if (isFreshInstall) {
    return (
      <div className="page-stack">
        <section className="hero-panel">
          <div className="hero-panel__content">
            <p className="hero-panel__eyebrow">当前风险总览</p>
            <h2 className="hero-panel__title">首页 / Dashboard</h2>
            <p className="hero-panel__description">先处理当前异常，再查看趋势与事件历史。</p>
          </div>
        </section>

        <section className="page-panel">
          <p className="page-panel__eyebrow">首次接入</p>
          <h3 className="page-panel__title">还没有节点与目标</h3>
          <p className="page-panel__description">
            这不是异常。候风需要先有一个节点接入 agent，然后才能创建目标并添加 ProbeItem。
          </p>
          <ol>
            <li>创建第一个节点</li>
            <li>接入 agent</li>
            <li>创建第一个目标</li>
            <li>添加第一个 ProbeItem</li>
          </ol>
          <Link className="text-link" to="/nodes">
            创建第一个节点
          </Link>
        </section>
      </div>
    )
  }

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">当前风险总览</p>
          <h2 className="hero-panel__title">首页 / Dashboard</h2>
          <p className="hero-panel__description">先处理当前异常，再查看趋势与事件历史。</p>
        </div>
      </section>

      <div className="summary-grid summary-grid--strip">
        <StatTile label="风险对象" value={abnormalTotal} />
        <StatTile label="严重对象总数" value={severeTotal} />
        <StatTile label="维护对象总数" value={maintenanceTotal} />
        <StatTile
          label="新增异常"
          value={overview.recent_new_incident_count}
          trend={overview.new_incident_trend_24h}
          trendTone="critical"
        />
        <StatTile
          label="恢复事件"
          value={overview.recent_recovery_count}
          trend={overview.recovery_trend_24h}
          trendTone="normal"
        />
      </div>

      <DetailSection eyebrow="节点" title="异常节点概览" ribbon="alert">
        <div className="summary-grid summary-grid--strip">
          <StatTile label="当前异常数" value={overview.abnormal_node_count} />
          <StatTile label="严重节点" value={overview.severe_node_count} />
          <StatTile label="维护节点" value={overview.maintenance_node_count} />
        </div>
        <AbnormalNodeList nodes={overview.abnormal_nodes} />
      </DetailSection>

      <DetailSection eyebrow="目标" title="异常目标概览" ribbon="alert">
        <div className="summary-grid summary-grid--strip">
          <StatTile label="当前异常数" value={overview.abnormal_target_count} />
          <StatTile label="严重目标" value={overview.severe_target_count} />
          <StatTile label="维护目标" value={overview.maintenance_target_count} />
        </div>
        <AbnormalTargetList targets={overview.abnormal_targets} />
      </DetailSection>

      <DetailSection eyebrow="最近事件" title="最近事件">
        <EventList
          events={overview.recent_events}
          emptyTitle="最近没有状态变更事件"
          emptyDescription="当前没有新的异常变化，首页事件流保持为空。"
        />
      </DetailSection>
    </div>
  )
}
