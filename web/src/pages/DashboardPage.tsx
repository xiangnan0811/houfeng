import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { StatusBadge } from '../components/StatusBadge'
import { ApiError, getDashboard } from '../lib/api'
import { formatDateTime } from '../lib/format'
import type { DashboardNodeSummary, DashboardOverview, DashboardTargetSummary } from '../lib/types'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
}

function SummaryCard({ label, value }: { label: string; value: number }) {
  return (
    <article className="summary-card">
      <p className="summary-card__label">{label}</p>
      <p className="summary-card__value">{value}</p>
    </article>
  )
}

function statusTone(value: string) {
  if (value === '正常') return 'green'
  if (value === '严重') return 'red'
  if (value === '告警' || value === '关注') return 'yellow'
  return 'slate'
}

function formatOptionalDateTime(value: string | undefined, fallback: string) {
  return value ? formatDateTime(value) : fallback
}

function hostPortSummary(target: DashboardTargetSummary) {
  return typeof target.base_port === 'number' ? `${target.host}:${target.base_port}` : target.host
}

function AbnormalNodeList({ nodes }: { nodes: DashboardNodeSummary[] }) {
  if (nodes.length === 0) {
    return (
      <div className="empty-state">
        <h3>当前没有异常节点</h3>
        <p>节点侧暂未发现需要处理的活跃异常。</p>
      </div>
    )
  }

  return (
    <div className="probe-list">
      {nodes.map((node) => (
        <article key={node.node_id} className="probe-card">
          <header className="probe-card__header">
            <div>
              <h3>{node.display_name}</h3>
              <p>{node.current_primary_issue_summary || '暂无关键异常摘要'}</p>
            </div>
            <div className="badge-row badge-row--wrap">
              <StatusBadge label={node.current_health_status} tone={statusTone(node.current_health_status)} />
              <StatusBadge label={node.monitoring_status} tone="cyan" />
            </div>
          </header>
          <dl className="probe-card__meta">
            <div>
              <dt>位置</dt>
              <dd>
                {node.region} / {node.city}
              </dd>
            </div>
            <div>
              <dt>供应商</dt>
              <dd>{node.provider}</dd>
            </div>
            <div>
              <dt>生命周期</dt>
              <dd>{node.lifecycle_status}</dd>
            </div>
            <div>
              <dt>活跃异常</dt>
              <dd>{node.current_active_incident_count}</dd>
            </div>
            <div>
              <dt>最近心跳</dt>
              <dd>{formatOptionalDateTime(node.last_heartbeat_at, '暂无心跳')}</dd>
            </div>
          </dl>
          <Link className="text-link" to={`/nodes/${node.node_id}`} aria-label={`查看节点 ${node.display_name}`}>
            查看节点
          </Link>
        </article>
      ))}
    </div>
  )
}

function AbnormalTargetList({ targets }: { targets: DashboardTargetSummary[] }) {
  if (targets.length === 0) {
    return (
      <div className="empty-state">
        <h3>当前没有异常目标</h3>
        <p>目标侧暂未发现需要处理的活跃异常。</p>
      </div>
    )
  }

  return (
    <div className="probe-list">
      {targets.map((target) => (
        <article key={target.target_id} className="probe-card">
          <header className="probe-card__header">
            <div>
              <h3>{target.name}</h3>
              <p>{target.current_primary_issue_summary || '暂无关键异常摘要'}</p>
            </div>
            <div className="badge-row badge-row--wrap">
              <StatusBadge label={target.current_health_status} tone={statusTone(target.current_health_status)} />
              <StatusBadge label={target.run_status} tone="cyan" />
            </div>
          </header>
          <dl className="probe-card__meta">
            <div>
              <dt>类型</dt>
              <dd>{target.target_type}</dd>
            </div>
            <div>
              <dt>地址</dt>
              <dd>{hostPortSummary(target)}</dd>
            </div>
            <div>
              <dt>活跃异常</dt>
              <dd>{target.current_active_incident_count}</dd>
            </div>
            <div>
              <dt>最近成功</dt>
              <dd>{formatOptionalDateTime(target.last_success_at, '暂无成功观测')}</dd>
            </div>
            <div>
              <dt>最近失败</dt>
              <dd>{formatOptionalDateTime(target.last_failure_at, '暂无失败观测')}</dd>
            </div>
          </dl>
          <Link className="text-link" to={`/targets/${target.target_id}`} aria-label={`查看目标 ${target.name}`}>
            查看目标
          </Link>
        </article>
      ))}
    </div>
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
        <section className="page-panel">
          <p className="page-panel__eyebrow">当前风险总览</p>
          <h2 className="page-panel__title">首页 / Dashboard</h2>
          <p className="page-panel__description">
            先处理当前异常，再查看趋势与事件历史。
          </p>
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
      <section className="page-panel">
        <p className="page-panel__eyebrow">当前风险总览</p>
        <h2 className="page-panel__title">首页 / Dashboard</h2>
        <p className="page-panel__description">
          先处理当前异常，再查看趋势与事件历史。
        </p>
      </section>

      <div className="summary-grid">
        <SummaryCard label="风险对象" value={abnormalTotal} />
        <SummaryCard label="严重对象总数" value={severeTotal} />
        <SummaryCard label="维护对象总数" value={maintenanceTotal} />
        <SummaryCard label="新增异常" value={overview.recent_new_incident_count} />
        <SummaryCard label="恢复事件" value={overview.recent_recovery_count} />
      </div>

      <DetailSection eyebrow="节点" title="异常节点概览">
        <div className="summary-grid">
          <SummaryCard label="当前异常数" value={overview.abnormal_node_count} />
          <SummaryCard label="严重节点" value={overview.severe_node_count} />
          <SummaryCard label="维护节点" value={overview.maintenance_node_count} />
        </div>
        <AbnormalNodeList nodes={overview.abnormal_nodes} />
      </DetailSection>

      <DetailSection eyebrow="目标" title="异常目标概览">
        <div className="summary-grid">
          <SummaryCard label="当前异常数" value={overview.abnormal_target_count} />
          <SummaryCard label="严重目标" value={overview.severe_target_count} />
          <SummaryCard label="维护目标" value={overview.maintenance_target_count} />
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
