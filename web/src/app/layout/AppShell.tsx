import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { ChangePasswordModal } from './ChangePasswordModal'
import { TopBar } from './TopBar'
import { useAuth } from '../../lib/auth-context'
import { ApiError, getDashboard } from '../../lib/api'
import type { User } from '../../lib/auth-client'
import { PRODUCT_FULL_NAME_ZH } from '../metadata'
import {
  buildShellSummaryModel,
  INITIAL_DASHBOARD_SUMMARY,
  SHELL_SUMMARY_FRESHNESS_MS,
  type DashboardSummaryState,
} from './shellSummaryModel'

const LazyVPSWriteRegistryProvider = lazy(async () => {
  const { VPSWriteRegistryProvider } = await import('../../lib/vpsWriteRegistry-context')
  return { default: VPSWriteRegistryProvider }
})

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
  const [collapsed, setCollapsed] = useState(false)
  const [changePwOpen, setChangePwOpen] = useState(false)
  const [dashboardSummary, setDashboardSummary] =
    useState<DashboardSummaryState>(INITIAL_DASHBOARD_SUMMARY)
  const [summaryNow, setSummaryNow] = useState(() => Date.now())
  const mountedRef = useRef(false)
  const summaryRequestRef = useRef<ReturnType<typeof getDashboard> | null>(null)

  const refreshDashboardSummary = useCallback(() => {
    if (summaryRequestRef.current) return summaryRequestRef.current

    const request = getDashboard()
    summaryRequestRef.current = request
    request
      .then((overview) => {
        if (!mountedRef.current) return
        setSummaryNow(Date.now())
        setDashboardSummary({
          status: 'success',
          error: null,
          overview,
        })
      })
      .catch((error: unknown) => {
        if (!mountedRef.current) return
        const message = error instanceof ApiError ? error.message : '读取系统摘要失败'
        setSummaryNow(Date.now())
        setDashboardSummary((current) => ({
          status: 'error',
          error: message,
          overview: current.overview,
        }))
      })
      .finally(() => {
        if (summaryRequestRef.current === request) summaryRequestRef.current = null
      })

    return request
  }, [])

  useEffect(() => {
    mountedRef.current = true

    function refreshFromVisiblePage() {
      setSummaryNow(Date.now())
      void refreshDashboardSummary()
    }

    function handleVisibilityChange() {
      if (document.visibilityState === 'visible') refreshFromVisiblePage()
    }

    void refreshDashboardSummary()
    document.addEventListener('visibilitychange', handleVisibilityChange)
    window.addEventListener('focus', refreshFromVisiblePage)

    return () => {
      mountedRef.current = false
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('focus', refreshFromVisiblePage)
    }
  }, [refreshDashboardSummary])

  const generatedAt = dashboardSummary.overview?.snapshot_generated_at
  useEffect(() => {
    if (!generatedAt || dashboardSummary.status !== 'success') return
    const generatedAtMs = Date.parse(generatedAt)
    if (!Number.isFinite(generatedAtMs)) return
    const remaining = generatedAtMs + SHELL_SUMMARY_FRESHNESS_MS - Date.now()
    if (remaining <= 0) return

    const timer = window.setTimeout(() => {
      setSummaryNow(Date.now())
    }, remaining + 1)
    return () => window.clearTimeout(timer)
  }, [dashboardSummary.status, generatedAt])

  const sync = buildShellSummaryModel(dashboardSummary, summaryNow)
  const anomalyCounts = sync.showAnomalyCounts && dashboardSummary.overview
    ? {
        monitoring: dashboardSummary.overview.abnormal_monitoring_instance_count,
        targets: dashboardSummary.overview.abnormal_target_count,
      }
    : { monitoring: 0, targets: 0 }

  return (
    <>
      <a className="skip-link" href="#main-content">跳到主内容</a>
      <div className={`layout${collapsed ? ' sidebar-collapsed' : ''}`}>
        <Sidebar
          user={user}
          anomalyCounts={anomalyCounts}
          collapsed={collapsed}
          onToggle={() => setCollapsed((v) => !v)}
          onLogout={() => { void logout() }}
          onChangePassword={() => setChangePwOpen(true)}
        />
        <div className="main-wrap">
          <TopBar sync={sync} user={user} />
          <main className="main" id="main-content" tabIndex={-1}>
            <Suspense fallback={(
              <p className="asset-operation-feedback asset-operation-feedback--notice" role="status" aria-live="polite">
                正在加载页面…
              </p>
            )}>
              <LazyVPSWriteRegistryProvider>
                <Outlet />
              </LazyVPSWriteRegistryProvider>
            </Suspense>
          </main>
        </div>
        {changePwOpen && <ChangePasswordModal onClose={() => setChangePwOpen(false)} />}
      </div>
    </>
  )
}
