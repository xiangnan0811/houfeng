import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { ApiError, getDashboard } from '../lib/api'
import type { DashboardOverview } from '../lib/types'

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
        const message = error instanceof ApiError ? error.message : '加载集群概览失败'
        setState({ loading: false, error: message, overview: null })
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (state.loading) {
    return <section className="page-panel">正在加载集群概览…</section>
  }

  if (state.error || !state.overview) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Dashboard</p>
        <h2 className="page-panel__title">集群概览不可用</h2>
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
          <p className="page-panel__eyebrow">Dashboard</p>
          <h2 className="page-panel__title">集群概览</h2>
          <p className="page-panel__description">
            查看当前异常、维护与最近状态变更，保持 V1 控制面总览页的信息密度与层级稳定。
          </p>
        </section>

        <section className="page-panel">
          <p className="page-panel__eyebrow">First Run</p>
          <h3 className="page-panel__title">还没有节点与目标</h3>
          <p className="page-panel__description">
            这不是异常。候风需要先有一个 Node 接入 agent，然后才能创建 Target 并添加 ProbeItem。
          </p>
          <ol>
            <li>创建第一个 Node</li>
            <li>接入 agent</li>
            <li>创建第一个 Target</li>
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
        <p className="page-panel__eyebrow">Dashboard</p>
        <h2 className="page-panel__title">集群概览</h2>
        <p className="page-panel__description">
          查看当前异常、维护与最近状态变更，保持 V1 控制面总览页的信息密度与层级稳定。
        </p>
      </section>

      <div className="summary-grid">
        <SummaryCard label="异常对象总数" value={abnormalTotal} />
        <SummaryCard label="严重对象总数" value={severeTotal} />
        <SummaryCard label="维护对象总数" value={maintenanceTotal} />
        <SummaryCard label="新增异常" value={overview.recent_new_incident_count} />
        <SummaryCard label="恢复事件" value={overview.recent_recovery_count} />
      </div>

      <DetailSection eyebrow="Nodes" title="异常节点概览">
        <div className="summary-grid">
          <SummaryCard label="当前异常数" value={overview.abnormal_node_count} />
          <SummaryCard label="严重节点" value={overview.severe_node_count} />
          <SummaryCard label="维护节点" value={overview.maintenance_node_count} />
        </div>
      </DetailSection>

      <DetailSection eyebrow="Targets" title="异常目标概览">
        <div className="summary-grid">
          <SummaryCard label="当前异常数" value={overview.abnormal_target_count} />
          <SummaryCard label="严重目标" value={overview.severe_target_count} />
          <SummaryCard label="维护目标" value={overview.maintenance_target_count} />
        </div>
      </DetailSection>

      <DetailSection eyebrow="Recent Events" title="最近事件">
        <EventList
          events={overview.recent_events}
          emptyTitle="最近没有状态变更事件"
          emptyDescription="当前没有新的 incident 变化，首页事件流保持为空。"
        />
      </DetailSection>
    </div>
  )
}
