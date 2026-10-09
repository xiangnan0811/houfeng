import { useSyncExternalStore, type PropsWithChildren } from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  PRODUCT_FULL_NAME_ZH,
  PRODUCT_NAME_ZH,
} from '../metadata'
import { AppShell } from './AppShell'
import * as authCtx from '../../lib/auth-context'
import type { User } from '../../lib/auth-client'
import { useVPSWriteRegistry } from '../../lib/vpsWriteRegistry-context'

const delayedRegistryProvider = vi.hoisted(() => ({
  pending: null as Promise<void> | null,
}))

vi.mock('../../lib/vpsWriteRegistry-context', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/vpsWriteRegistry-context')>()
  const { createElement } = await import('react')
  return {
    ...actual,
    VPSWriteRegistryProvider: ({ children }: PropsWithChildren) => {
      if (delayedRegistryProvider.pending) throw delayedRegistryProvider.pending
      return createElement(actual.VPSWriteRegistryProvider, null, children)
    },
  }
})

const baseAuth = {
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
  retry: vi.fn(),
  status: 'ready' as const,
  error: null,
}
const user = {
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '',
  runtime_capabilities: { records: true, comparison: true, portability: true },
  management_capabilities: { access: false },
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function stubDashboardFetch(
  dashboardFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
) {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (path === '/api/record-notifications/unread-count') {
      return Promise.resolve(mockJSONResponse({ unread_count: 0 }))
    }
    return dashboardFetch(input, init)
  }))
}

function baseOverview(overrides: Record<string, unknown> = {}) {
  return {
    snapshot_generated_at: new Date(Date.now() - 60_000).toISOString(),
    total_monitoring_instance_count: 5,
    total_target_count: 4,
    abnormal_monitoring_instance_count: 0,
    abnormal_target_count: 0,
    unobserved_target_count: 0,
    stale_target_count: 0,
    severe_monitoring_instance_count: 0,
    severe_target_count: 0,
    maintenance_monitoring_instance_count: 0,
    maintenance_target_count: 0,
    pending_onboarding_monitoring_instance_count: 0,
    paused_monitoring_instance_count: 0,
    retired_monitoring_instance_count: 0,
    paused_target_count: 0,
    archived_target_count: 0,
    recent_new_incident_count: 0,
    recent_recovery_count: 0,
    group_summaries: [],
    notification_status: {
      telegram_configured: false,
      telegram_runtime_managed: false,
      telegram_runtime_apply_active: false,
      feishu_configured: false,
    },
    asset_summary: {
      renewal_due_30d_subscription_count: 0,
      renewal_due_30d_vps_count: 0,
      unreviewed_vps_count: 0,
      to_cancel_vps_count: 0,
      to_migrate_vps_count: 0,
      unlinked_vps_count: 0,
      abnormal_linked_vps_count: 0,
      cost_by_currency: [],
    },
    recent_events: [],
    abnormal_monitoring_instances: [],
    abnormal_targets: [],
    ...overrides,
  }
}

function renderAuthenticatedAppShell(authUser: User = user) {
  vi.spyOn(authCtx, 'useAuth').mockReturnValue({
    ...baseAuth,
    user: authUser,
    loading: false,
  })

  return render(
    <MemoryRouter>
      <AppShell />
    </MemoryRouter>,
  )
}

function WriteRegistryProbe({ page }: { page: 'detail' | 'dashboard' }) {
  const registry = useVPSWriteRegistry()
  const owners = useSyncExternalStore(registry.subscribe, registry.getSnapshot, registry.getSnapshot)
  const owner = owners.get('vps_a')

  return (
    <div>
      <p>{page}:{owner ? 'owned' : 'idle'}</p>
      <button
        type="button"
        onClick={() => registry.begin({
          vpsId: 'vps_a',
          viewToken: `${page}-view`,
          generation: 1,
          operation: 'subscription',
        })}
      >
        开始写入
      </button>
      <Link to={page === 'detail' ? '/dashboard' : '/vps/vps_a'}>
        {page === 'detail' ? '前往工作台' : '返回 VPS'}
      </Link>
    </div>
  )
}

describe('AppShell', () => {
  afterEach(() => {
    delayedRegistryProvider.pending = null
    vi.useRealTimers()
    vi.restoreAllMocks()
    // 断言中途失败时也拆掉 matchMedia 等全局 stub，避免泄漏到后续用例。
    vi.unstubAllGlobals()
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
  })

  it('renders sidebar chrome and sets document title when authenticated', () => {
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))
    const { container } = renderAuthenticatedAppShell()

    expect(container.querySelector('#main-content')).toBeInTheDocument()
    expect(screen.getByText(PRODUCT_NAME_ZH)).toBeInTheDocument()
    // New sidebar uses hardcoded nav sections
    expect(screen.getByRole('link', { name: '工作台' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '监控' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '入口探测' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '事件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(document.title).toBe(PRODUCT_FULL_NAME_ZH)
  })

  it('starts with the collapsed rail on tablet widths, follows breakpoint changes, and still lets the toggle expand', () => {
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))
    let compact = true
    const listeners = new Set<(event: MediaQueryListEvent) => void>()
    vi.stubGlobal('matchMedia', (query: string) => ({
      get matches() { return query === '(max-width: 1100px)' ? compact : false },
      media: query,
      addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.add(listener),
      removeEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener),
    }))
    const { container } = renderAuthenticatedAppShell()
    const layout = container.querySelector('.layout')!
    expect(layout).toHaveClass('sidebar-collapsed')
    const toggle = screen.getByRole('button', { name: '展开侧边栏' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(toggle)
    expect(layout).not.toHaveClass('sidebar-collapsed')
    expect(screen.getByRole('button', { name: '折叠侧边栏' })).toHaveAttribute('aria-expanded', 'true')

    // 跨到桌面宽度：保持展开；再回到平板宽度：回到收起的默认值。
    compact = false
    act(() => { for (const listener of listeners) listener({ matches: false } as MediaQueryListEvent) })
    expect(layout).not.toHaveClass('sidebar-collapsed')
    compact = true
    act(() => { for (const listener of listeners) listener({ matches: true } as MediaQueryListEvent) })
    expect(layout).toHaveClass('sidebar-collapsed')
  })

  it('owns one route-persistent VPS write registry and resets it when the authenticated user changes', async () => {
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))
    let currentUser: User = user
    vi.spyOn(authCtx, 'useAuth').mockImplementation(() => ({
      ...baseAuth,
      user: currentUser,
      loading: false,
    }))

    const routeTree = () => (
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="vps/:vpsId" element={<WriteRegistryProbe page="detail" />} />
            <Route path="dashboard" element={<WriteRegistryProbe page="dashboard" />} />
          </Route>
        </Routes>
      </MemoryRouter>
    )
    const { rerender } = render(routeTree())

    fireEvent.click(await screen.findByRole('button', { name: '开始写入' }))
    expect(screen.getByText('detail:owned')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('link', { name: '前往工作台' }))
    expect(screen.getByText('dashboard:owned')).toBeInTheDocument()

    currentUser = { ...user, user_id: 'u2', username: 'operator' }
    rerender(routeTree())
    expect(await screen.findByText('dashboard:idle')).toBeInTheDocument()
  })

  it('announces route loading while the lazy registry provider is pending', async () => {
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user,
      loading: false,
    })
    let releaseProvider!: () => void
    delayedRegistryProvider.pending = new Promise<void>((resolve) => {
      releaseProvider = resolve
    })

    render(
      <MemoryRouter initialEntries={['/dashboard']}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="dashboard" element={<p>工作台路由已加载</p>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    const loadingMessage = await screen.findByText('正在加载页面…')
    expect(loadingMessage).toHaveAttribute('role', 'status')
    expect(screen.queryByText('工作台路由已加载')).not.toBeInTheDocument()

    await act(async () => {
      delayedRegistryProvider.pending = null
      releaseProvider()
    })
    expect(await screen.findByText('工作台路由已加载')).toBeInTheDocument()
  })

  it('starts authenticated keyboard navigation with a skip link to a focusable main', () => {
    stubDashboardFetch(vi.fn().mockReturnValue(new Promise(() => {})))
    const { container } = renderAuthenticatedAppShell()

    const skipLink = screen.getByRole('link', { name: '跳到主内容' })
    const main = container.querySelector('main#main-content')
    const focusable = container.querySelectorAll('a[href],button,input,select,textarea,[tabindex]:not([tabindex="-1"])')
    expect(focusable[0]).toBe(skipLink)
    expect(skipLink).toHaveAttribute('href', '#main-content')
    expect(main).toHaveAttribute('tabindex', '-1')
  })

  it('does not surface single-user phrasing', () => {
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))

    renderAuthenticatedAppShell()
    const layout = document.querySelector('.layout')
    expect(layout).not.toBeNull()
    expect(layout!.textContent).not.toMatch(/单用户|全权限|个人系统|V1 冻结基线/)
  })

  it('links the notification control to the private record inbox without an invented count', () => {
    stubDashboardFetch(vi.fn().mockReturnValue(new Promise(() => {})))

    renderAuthenticatedAppShell()

    expect(screen.getByRole('link', { name: '记录通知，正在更新未读数' })).toHaveAttribute(
      'href',
      '/record-inbox',
    )
    expect(document.querySelector('.notif-count')).toBeNull()
  })

  it('requests dashboard summary when authenticated and shows loading as degraded', () => {
    const fetchMock = vi.fn().mockReturnValue(new Promise(() => {}))
    stubDashboardFetch(fetchMock)

    renderAuthenticatedAppShell()

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    // Loading is explicit and does not imply a known clear/anomaly result.
    const syncEl = document.querySelector('.tp-sync')
    expect(syncEl).toHaveClass('tp-sync--loading')
    expect(syncEl).toHaveAttribute('title', '正在读取系统摘要')
  })

  it('shows degraded sync when severe objects exist', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_monitoring_instance_count: 3,
            abnormal_target_count: 2,
            severe_monitoring_instance_count: 1,
            severe_target_count: 1,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--anomaly')
      expect(syncEl).toHaveAttribute('title', '运行异常 5')
    })
  })

  it('shows degraded sync when only non-severe anomalies exist', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_monitoring_instance_count: 1,
            abnormal_target_count: 2,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--anomaly')
      expect(syncEl).toHaveAttribute('title', '运行异常 3')
    })
  })

  it('shows dashboard anomaly counts in sidebar after summary loads', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_monitoring_instance_count: 3,
            abnormal_target_count: 2,
            severe_monitoring_instance_count: 1,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--anomaly')
    })
    // Sidebar shows anomaly count badges
    expect(screen.getByText('3')).toHaveClass('nav-badge')
    expect(screen.getByText('2')).toHaveClass('nav-badge')
  })

  it('resets the shell summary to loading instead of reusing a previous load', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          baseOverview({
            abnormal_monitoring_instance_count: 3,
            abnormal_target_count: 2,
          }),
        ),
      )
      .mockReturnValueOnce(new Promise(() => {}))
    stubDashboardFetch(fetchMock)

    const { unmount } = renderAuthenticatedAppShell()
    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--anomaly')
      expect(syncEl).toHaveAttribute('title', '运行异常 5')
    })
    unmount()

    renderAuthenticatedAppShell()

    // After remount, should be back to loading state
    const syncEl = document.querySelector('.tp-sync')
    expect(syncEl).toHaveClass('tp-sync--loading')
    expect(syncEl).toHaveAttribute('title', '正在读取系统摘要')
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('marks loaded summaries with active anomalies as degraded', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_monitoring_instance_count: 1,
            abnormal_target_count: 0,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--anomaly')
      expect(syncEl).toHaveAttribute('title', '运行异常 1')
    })
  })

  it('describes a clear dashboard snapshot without claiming the system is healthy', async () => {
    const generatedAt = new Date(Date.now() - 60_000).toISOString()
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(baseOverview({ snapshot_generated_at: generatedAt })),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--clear')
      expect(syncEl).toHaveAttribute('title', '当前运行异常计数为 0')
    })
    expect(screen.getByText('当前运行异常计数为 0')).toBeInTheDocument()
    expect(screen.queryByText('系统摘要无异常')).not.toBeInTheDocument()
    expect(screen.getByText(/系统摘要生成于/)).toBeInTheDocument()
    expect(document.querySelector('.layout')).not.toHaveTextContent('系统正常')
  })

  it('shows unobserved targets separately from known abnormalities', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(baseOverview({
          abnormal_monitoring_instance_count: 1,
          abnormal_target_count: 2,
          unobserved_target_count: 4,
        })),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      expect(document.querySelector('.tp-sync')).toHaveAttribute('title', '运行异常 3，尚有目标无观测 4')
    })
    expect(screen.getByRole('link', { name: '入口探测，2 个异常' })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: '尚无观测，4 个尚无观测' })).toHaveAttribute('href', '/targets?view=unobserved')
    expect(screen.queryByRole('link', { name: /6 个异常/ })).not.toBeInTheDocument()
  })

  it('marks a dashboard snapshot stale after the freshness window expires', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-25T08:30:00Z'))
    stubDashboardFetch(
      vi.fn().mockResolvedValue(
        mockJSONResponse(baseOverview({ snapshot_generated_at: '2026-04-25T08:30:00Z' })),
      ),
    )

    renderAuthenticatedAppShell()
    await act(async () => {})

    expect(document.querySelector('.tp-sync')).toHaveClass('tp-sync--clear')

    await act(async () => {
      vi.advanceTimersByTime(5 * 60_000 + 1)
    })

    const syncEl = document.querySelector('.tp-sync')
    expect(syncEl).toHaveClass('tp-sync--stale')
    expect(syncEl).toHaveAttribute('title', '系统摘要已过期')
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('refreshes on visibility and focus while deduplicating an in-flight request', async () => {
    let resolveRefresh: ((response: Response) => void) | undefined
    const refreshResponse = new Promise<Response>((resolve) => {
      resolveRefresh = resolve
    })
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(baseOverview()))
      .mockReturnValueOnce(refreshResponse)
    stubDashboardFetch(fetchMock)

    renderAuthenticatedAppShell()
    await waitFor(() => {
      expect(document.querySelector('.tp-sync')).toHaveClass('tp-sync--clear')
    })

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    })
    fireEvent(document, new Event('visibilitychange'))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    fireEvent(document, new Event('visibilitychange'))
    fireEvent(window, new Event('focus'))
    expect(fetchMock).toHaveBeenCalledTimes(2)

    resolveRefresh?.(
      mockJSONResponse(baseOverview({ abnormal_monitoring_instance_count: 2 })),
    )
    await waitFor(() => {
      expect(document.querySelector('.tp-sync')).toHaveClass('tp-sync--anomaly')
    })
    expect(screen.getByText('2')).toHaveClass('nav-badge')
  })

  it('keeps the last successful snapshot but marks it stale when refresh fails', async () => {
    const generatedAt = new Date(Date.now() - 60_000).toISOString()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          baseOverview({
            snapshot_generated_at: generatedAt,
            abnormal_monitoring_instance_count: 2,
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse({ error: 'dashboard unavailable' }, 503))
    stubDashboardFetch(fetchMock)

    renderAuthenticatedAppShell()
    await waitFor(() => {
      expect(screen.getByText('2')).toHaveClass('nav-badge')
    })

    fireEvent(window, new Event('focus'))

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--stale')
      expect(syncEl).toHaveAttribute('title', '更新失败，显示上次结果')
    })
    expect(screen.getByText(/系统摘要生成于/)).toBeInTheDocument()
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('shows dashboard unavailable when the shell summary request fails', async () => {
    stubDashboardFetch(
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'dashboard unavailable' }, 503)),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => {
      const syncEl = document.querySelector('.tp-sync')
      expect(syncEl).toHaveClass('tp-sync--unavailable')
      expect(syncEl).toHaveAttribute('title', '系统摘要不可用')
    })
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('shows stale targets as their own count and hides them when the snapshot expires', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-25T08:30:00Z'))
    stubDashboardFetch(vi.fn().mockResolvedValue(mockJSONResponse(baseOverview({
      snapshot_generated_at: '2026-04-25T08:30:00Z',
      abnormal_target_count: 2,
      unobserved_target_count: 1,
      stale_target_count: 3,
    }))))
    renderAuthenticatedAppShell()
    await act(async () => {})

    expect(document.querySelector('.tp-sync')).toHaveAttribute('title', '运行异常 2，尚有目标无观测 1，观测过期 3')
    expect(screen.getByRole('link', { name: '入口探测，2 个异常' })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: '尚无观测，1 个尚无观测' })).toHaveAttribute('href', '/targets?view=unobserved')
    expect(screen.getByRole('link', { name: '观测过期，3 个观测过期' })).toHaveAttribute('href', '/targets?view=stale')
    expect(screen.queryByRole('link', { name: /5 个异常/ })).not.toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(5 * 60_000 + 1)
    })
    expect(document.querySelector('.tp-sync')).toHaveAttribute('title', '系统摘要已过期')
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('refreshes a visible summary every 30 seconds and skips hidden focus or interval wakes', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-25T08:30:00Z'))
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse(baseOverview({
      snapshot_generated_at: '2026-04-25T08:30:00Z',
      stale_target_count: 1,
    })))
    stubDashboardFetch(fetchMock)
    renderAuthenticatedAppShell()
    await act(async () => {})
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(30_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    fireEvent(document, new Event('visibilitychange'))
    fireEvent(window, new Event('focus'))
    await act(async () => {
      vi.advanceTimersByTime(30_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('clears the previous summary after an authorization or not-found refresh', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(baseOverview({ abnormal_target_count: 2, stale_target_count: 4 })))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'missing' }, 404))
    stubDashboardFetch(fetchMock)
    renderAuthenticatedAppShell()
    await waitFor(() => {
      expect(screen.getByRole('link', { name: '观测过期，4 个观测过期' })).toBeInTheDocument()
    })

    fireEvent(window, new Event('focus'))
    await waitFor(() => {
      expect(document.querySelector('.tp-sync')).toHaveAttribute('title', '系统摘要不可用')
    })
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
    expect(screen.queryByText('更新失败，显示上次结果')).not.toBeInTheDocument()
  })

  it('ignores a late summary after the signed-in user changes', async () => {
    let resolveFirst: (response: Response) => void = () => {}
    const first = new Promise<Response>((resolve) => {
      resolveFirst = resolve
    })
    const fetchMock = vi.fn()
      .mockReturnValueOnce(first)
      .mockResolvedValue(mockJSONResponse(baseOverview({ stale_target_count: 1 })))
    stubDashboardFetch(fetchMock)
    const auth = {
      ...baseAuth,
      user,
      loading: false,
    }
    vi.spyOn(authCtx, 'useAuth').mockImplementation(() => auth)
    const view = render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    auth.user = { ...user, user_id: 'u2', username: 'other' }
    view.rerender(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )
    resolveFirst(mockJSONResponse(baseOverview({
      abnormal_target_count: 9,
      stale_target_count: 5,
    })))
    await waitFor(() => {
      expect(screen.getByRole('link', { name: '观测过期，1 个观测过期' })).toBeInTheDocument()
    })
    expect(screen.getByText('other')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /9 个异常/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /5 个观测过期/ })).not.toBeInTheDocument()
  })

  it('renders nothing when no authenticated user', () => {
    vi.stubGlobal('fetch', vi.fn())
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
    })
    const { container } = render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )
    expect(container.querySelector('.layout')).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })
})
