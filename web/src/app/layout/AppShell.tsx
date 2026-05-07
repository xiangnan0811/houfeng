import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
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
      <Sidebar
        user={user}
        sync={sync}
        anomalyCounts={anomalyCounts}
        onLogout={() => {
          void logout()
        }}
        onChangePassword={() => setChangePwOpen(true)}
      />
      <main className="app-shell__main">
        <TopBar />
        <Outlet />
      </main>
      {changePwOpen && <ChangePasswordModal onClose={() => setChangePwOpen(false)} />}
    </div>
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
