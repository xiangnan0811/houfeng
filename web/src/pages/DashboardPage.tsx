import { useEffect, useRef, useState } from 'react'

import { ApiError, getDashboard } from '../lib/api'
import { type AutoRefreshOption, useAutoRefresh } from '../lib/useAutoRefresh'
import type { DashboardOverview } from '../lib/types'
import {
  buildAttentionItems,
  buildDashboardMetrics,
  buildFleetState,
} from './dashboard/dashboardHelpers'
import { DashboardCommandSurface } from './dashboard/DashboardCommandSurface'
import { DashboardWorkbench } from './dashboard/DashboardWorkbench'

type State = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
}

export function DashboardPage() {
  const [state, setState] = useState<State>({
    loading: true,
    error: null,
    overview: null,
  })
  const [refreshing, setRefreshing] = useState(false)
  const [autoRefresh, setAutoRefresh] = useState<AutoRefreshOption>(null)
  const mountedRef = useRef(true)

  function loadDashboard() {
    getDashboard()
      .then((overview) => {
        if (!mountedRef.current) return
        setState({ loading: false, error: null, overview })
        setRefreshing(false)
      })
      .catch((error: unknown) => {
        if (!mountedRef.current) return
        const message = error instanceof ApiError ? error.message : '加载工作台失败'
        setState({ loading: false, error: message, overview: null })
        setRefreshing(false)
      })
  }

  useEffect(() => {
    mountedRef.current = true
    loadDashboard()
    return () => { mountedRef.current = false }
  }, [])

  useAutoRefresh(autoRefresh, loadDashboard)

  function handleRefresh() {
    setRefreshing(true)
    loadDashboard()
  }

  if (state.loading) {
    return <section className="page-panel">正在加载工作台…</section>
  }

  if (state.error || !state.overview) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">工作台</p>
        <h2 className="page-panel__title">工作台不可用</h2>
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
      <DashboardCommandSurface
        overview={overview}
        fleetState={fleetState}
        metrics={metrics}
        attentionItems={attentionItems}
        abnormalTotal={abnormalTotal}
        severeTotal={severeTotal}
        maintenanceTotal={maintenanceTotal}
        isFreshInstall={isFreshInstall}
        refreshing={refreshing}
        onRefresh={handleRefresh}
        autoRefresh={autoRefresh}
        onAutoRefreshChange={setAutoRefresh}
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
