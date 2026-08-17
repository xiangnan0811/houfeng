import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  PRODUCT_FULL_NAME_ZH,
  PRODUCT_NAME_ZH,
} from '../metadata'
import { AppShell } from './AppShell'
import * as authCtx from '../../lib/auth-context'
import type { User } from '../../lib/auth-client'

const baseAuth = {
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}
const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

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

describe('AppShell', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
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

    expect(screen.getByRole('link', { name: '记录通知' })).toHaveAttribute(
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
      expect(syncEl).toHaveAttribute('title', '摘要有异常')
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
      expect(syncEl).toHaveAttribute('title', '摘要有异常')
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
      expect(syncEl).toHaveAttribute('title', '摘要有异常')
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
      expect(syncEl).toHaveAttribute('title', '摘要有异常')
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
      expect(syncEl).toHaveAttribute('title', '摘要无异常')
    })
    expect(screen.getByText('摘要无异常')).toBeInTheDocument()
    expect(screen.getByText(/摘要生成/)).toBeInTheDocument()
    expect(document.querySelector('.layout')).not.toHaveTextContent('系统正常')
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
    expect(syncEl).toHaveAttribute('title', '摘要已过期')
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
      expect(syncEl).toHaveAttribute('title', '摘要已过期')
    })
    expect(screen.getByText(/摘要生成/)).toBeInTheDocument()
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
      expect(syncEl).toHaveAttribute('title', '摘要不可用')
    })
    expect(document.querySelectorAll('.nav-badge')).toHaveLength(0)
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
