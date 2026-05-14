import { useEffect, useState } from 'react'
import { Link, Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { ChangePasswordModal } from './ChangePasswordModal'
import { TopBar } from './TopBar'
import { useAuth } from '../../lib/auth-context'
import { ApiError, getDashboard } from '../../lib/api'
import type { User } from '../../lib/auth-client'
import type { DashboardOverview } from '../../lib/types'
import { PRODUCT_FULL_NAME_ZH } from '../metadata'
import type { SyncStatusProps } from './SyncStatus'

type DashboardSummaryState = {
  loading: boolean
  error: string | null
  overview: DashboardOverview | null
  loadedAt: string | null
}

const INITIAL_DASHBOARD_SUMMARY: DashboardSummaryState = {
  loading: true,
  error: null,
  overview: null,
  loadedAt: null,
}

export function AppShell() {
  const { user, logout } = useAuth()

  useEffect(() => {
    document.title = PRODUCT_FULL_NAME_ZH
  }, [])

  if (!user) return null

  return <AuthenticatedAppShell key={user.user_id} user={user} logout={logout} />
}

type AuthenticatedAppShellProps = {
  user: User
  logout: () => Promise<void>
}

function AuthenticatedAppShell({ user, logout }: AuthenticatedAppShellProps) {
  const [changePwOpen, setChangePwOpen] = useState(false)
  const [dashboardSummary, setDashboardSummary] =
    useState<DashboardSummaryState>(INITIAL_DASHBOARD_SUMMARY)

  useEffect(() => {
    let cancelled = false

    getDashboard()
      .then((overview) => {
        if (cancelled) return
        setDashboardSummary({
          loading: false,
          error: null,
          overview,
          loadedAt: new Date().toISOString(),
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = error instanceof ApiError ? error.message : '读取系统摘要失败'
        setDashboardSummary({
          loading: false,
          error: message,
          overview: null,
          loadedAt: null,
        })
      })

    return () => {
      cancelled = true
    }
  }, [])

  const sync = deriveSyncStatus(dashboardSummary)
  const anomalyCounts = {
    nodes: dashboardSummary.overview?.abnormal_node_count ?? 0,
    targets: dashboardSummary.overview?.abnormal_target_count ?? 0,
  }

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到主内容</a>
      <Sidebar
        user={user}
        sync={sync}
        anomalyCounts={anomalyCounts}
        onLogout={() => {
          void logout()
        }}
        onChangePassword={() => setChangePwOpen(true)}
      />
      <main className="app-shell__main" id="main-content" tabIndex={-1}>
        <TopBar />
        <GlobalCriticalAlert summary={dashboardSummary} />
        <Outlet />
      </main>
      {changePwOpen && <ChangePasswordModal onClose={() => setChangePwOpen(false)} />}
    </div>
  )
}

type GlobalCriticalAlertProps = {
  summary: DashboardSummaryState
}

function GlobalCriticalAlert({ summary }: GlobalCriticalAlertProps) {
  const overview = summary.overview
  if (summary.loading || summary.error || !overview) return null

  const severeNodeCount = overview.severe_node_count
  const severeTargetCount = overview.severe_target_count
  const severeTotal = severeNodeCount + severeTargetCount
  const abnormalNodeCount = overview.abnormal_node_count
  const abnormalTargetCount = overview.abnormal_target_count
  const abnormalTotal = abnormalNodeCount + abnormalTargetCount

  if (severeTotal <= 0 && abnormalTotal <= 0) return null

  const critical = severeTotal > 0
  const title = critical ? `严重异常 ${severeTotal} 个` : `活跃异常 ${abnormalTotal} 个`
  const detail = critical
    ? `节点 ${severeNodeCount} · 入口 ${severeTargetCount}`
    : `节点 ${abnormalNodeCount} · 入口 ${abnormalTargetCount}`

  return (
    <section
      className={`global-critical-alert ${critical ? 'global-critical-alert--critical' : 'global-critical-alert--notice'}`}
      role={critical ? 'alert' : 'status'}
      aria-label={critical ? '全局严重异常' : '全局活跃异常'}
    >
      <div className="global-critical-alert__body">
        <span className="global-critical-alert__eyebrow">全局状态</span>
        <strong>{title}</strong>
        <span>{detail}</span>
      </div>
      <div className="global-critical-alert__actions" aria-label="异常处理入口">
        {critical ? <Link to="/events?severity=严重">查看严重事件</Link> : null}
        <Link to="/nodes?abnormal=1">异常节点</Link>
        <Link to="/targets?abnormal=1">异常入口</Link>
      </div>
    </section>
  )
}

function deriveSyncStatus(summary: DashboardSummaryState): SyncStatusProps {
  if (summary.loading) {
    return {
      state: 'degraded',
      label: '正在读取系统摘要',
      meta: 'v1.0 · dashboard loading',
    }
  }

  if (summary.error) {
    return {
      state: 'down',
      label: '摘要不可用',
      meta: 'v1.0 · dashboard unavailable',
    }
  }

  const overview = summary.overview
  if (!overview) {
    return {
      state: 'down',
      label: '摘要不可用',
      meta: 'v1.0 · dashboard unavailable',
    }
  }

  const severeCount = overview.severe_node_count + overview.severe_target_count
  const abnormalCount = overview.abnormal_node_count + overview.abnormal_target_count
  const label =
    severeCount > 0
      ? '摘要已加载 · 存在严重异常'
      : abnormalCount > 0
        ? '摘要已加载 · 存在活跃异常'
        : '摘要已加载'

  return {
    state: severeCount > 0 || abnormalCount > 0 ? 'degraded' : 'ok',
    label,
    meta: `v1.0 · dashboard ${formatDashboardLoadedAt(summary.loadedAt)}`,
  }
}

function formatDashboardLoadedAt(iso: string | null): string {
  if (!iso) return 'loaded'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return 'loaded'
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  const ss = String(date.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}
