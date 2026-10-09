import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { ChangePasswordModal } from './ChangePasswordModal'
import { TopBar } from './TopBar'
import { useAuth } from '../../lib/auth-context'
import { ApiError } from '../../lib/apiRequest'
import { getDashboard } from '../../lib/observabilityApi'
import { useVisibleRefresh } from '../../lib/useVisibleRefresh'
import type { User } from '../../lib/auth-client'
import { PRODUCT_FULL_NAME_ZH } from '../metadata'
import {
  buildShellSummaryModel,
  INITIAL_DASHBOARD_SUMMARY,
  SHELL_SUMMARY_FRESHNESS_MS,
  type DashboardSummaryState,
} from './shellSummaryModel'

// 平板及以下宽度默认把侧栏收成图标栏，给内容让出宽度；跨过断点时回到该宽度的默认值，
// 折叠按钮仍可手动展开。≤760px 的窄栏由 CSS 固定，不受这里影响。
const COMPACT_SHELL_QUERY = '(max-width: 1100px)'

function compactShellQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  return window.matchMedia(COMPACT_SHELL_QUERY)
}

const LazyVPSWriteRegistryProvider = lazy(async () => {
  const { VPSWriteRegistryProvider } = await import('../../lib/vpsWriteRegistry-context')
  return { default: VPSWriteRegistryProvider }
})

function clearsShellSummary(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.status === 403 || error.status === 404)
}

function shellSummaryErrorMessage(error: unknown): string {
  if (error instanceof ApiError && (error.status === 401 || error.status === 403)) return '授权失败'
  if (error instanceof ApiError && error.status === 404) return '系统摘要不存在'
  return error instanceof ApiError ? error.message : '读取系统摘要失败'
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
  const [collapsed, setCollapsed] = useState(() => compactShellQuery()?.matches ?? false)
  const [changePwOpen, setChangePwOpen] = useState(false)

  useEffect(() => {
    const query = compactShellQuery()
    if (!query) return undefined
    const onChange = (event: MediaQueryListEvent) => setCollapsed(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  const [dashboardSummary, setDashboardSummary] =
    useState<DashboardSummaryState>(INITIAL_DASHBOARD_SUMMARY)
  const [summaryNow, setSummaryNow] = useState(() => Date.now())
  const loadDashboardSummary = useCallback(async ({ isCurrent }: { isCurrent: () => boolean }) => {
    try {
      const overview = await getDashboard()
      if (!isCurrent()) return
      setSummaryNow(Date.now())
      setDashboardSummary({
        status: 'success',
        error: null,
        overview,
      })
    } catch (error: unknown) {
      if (!isCurrent()) return
      if (clearsShellSummary(error)) {
        setDashboardSummary({
          status: 'error',
          error: shellSummaryErrorMessage(error),
          overview: null,
        })
        return
      }
      setDashboardSummary((current) => ({
        status: 'error',
        error: shellSummaryErrorMessage(error),
        overview: current.overview,
      }))
    }
  }, [])
  const { refresh } = useVisibleRefresh(loadDashboardSummary, { refreshKey: user.user_id })

  useEffect(() => {
    void refresh()
  }, [refresh])

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
        unobservedTargets: dashboardSummary.overview.unobserved_target_count,
        staleTargets: dashboardSummary.overview.stale_target_count,
      }
    : { monitoring: 0, targets: 0, unobservedTargets: 0, staleTargets: 0 }

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
