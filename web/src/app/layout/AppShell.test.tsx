import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  PRIMARY_NAV_GROUPS,
  PRIMARY_NAV_ITEMS,
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

function baseOverview(overrides: Record<string, unknown> = {}) {
  return {
    snapshot_generated_at: '2026-04-25T08:30:00Z',
    total_node_count: 5,
    total_target_count: 4,
    abnormal_node_count: 0,
    abnormal_target_count: 0,
    severe_node_count: 0,
    severe_target_count: 0,
    maintenance_node_count: 0,
    maintenance_target_count: 0,
    pending_onboarding_node_count: 0,
    paused_node_count: 0,
    retired_node_count: 0,
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
    abnormal_nodes: [],
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
    vi.restoreAllMocks()
  })

  it('renders sidebar chrome and sets document title when authenticated', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))
    const { container } = renderAuthenticatedAppShell()

    expect(screen.getByText(PRODUCT_NAME_ZH)).toBeInTheDocument()
    PRIMARY_NAV_GROUPS.forEach((group) => {
      expect(
        Array.from(container.querySelectorAll('.sidebar__nav-group-title')).some(
          (title) => title.textContent === group.label,
        ),
      ).toBe(true)
    })
    PRIMARY_NAV_ITEMS.forEach((item) => {
      expect(screen.getByRole('link', { name: item.label })).toBeInTheDocument()
    })
    expect(screen.queryByRole('link', { name: '首页' })).not.toBeInTheDocument()
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(document.title).toBe(PRODUCT_FULL_NAME_ZH)
  })

  it('does not surface single-user phrasing', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))

    renderAuthenticatedAppShell()
    const appShell = screen.getByText(PRODUCT_NAME_ZH).closest('.app-shell')
    expect(appShell).not.toBeNull()
    const container = appShell as HTMLElement
    expect(container.textContent).not.toMatch(/单用户|全权限|个人系统|V1 冻结基线/)
  })

  it('requests dashboard summary when authenticated and shows loading as degraded', () => {
    const fetchMock = vi.fn().mockReturnValue(new Promise(() => {}))
    vi.stubGlobal('fetch', fetchMock)

    renderAuthenticatedAppShell()

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(screen.getByText('正在读取系统摘要')).toBeInTheDocument()
    expect(screen.getByText('v1.0 · dashboard loading')).toBeInTheDocument()
    expect(screen.queryByText('中心运行正常')).not.toBeInTheDocument()
  })

  it('shows dashboard anomaly counts in sidebar after summary loads', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_node_count: 3,
            abnormal_target_count: 2,
            severe_node_count: 1,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() =>
      expect(screen.getByText('摘要已加载 · 存在严重异常')).toBeInTheDocument(),
    )
    expect(screen.getByText('3')).toHaveClass('badge--count')
    expect(screen.getByText('3')).not.toHaveClass('tone--alert', 'tone--critical')
    expect(screen.getByText('2')).toHaveClass('badge--count')
    expect(screen.getByText('2')).not.toHaveClass('tone--alert', 'tone--critical')
    expect(screen.getByText(/v1\.0 · dashboard \d{2}:\d{2}:\d{2}/)).toBeInTheDocument()
    expect(screen.queryByText('中心运行正常')).not.toBeInTheDocument()
  })

  it('resets the shell summary to loading instead of reusing a previous load', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          baseOverview({
            abnormal_node_count: 3,
            abnormal_target_count: 2,
          }),
        ),
      )
      .mockReturnValueOnce(new Promise(() => {}))
    vi.stubGlobal('fetch', fetchMock)

    const { unmount } = renderAuthenticatedAppShell()
    await waitFor(() =>
      expect(screen.getByText('摘要已加载 · 存在活跃异常')).toBeInTheDocument(),
    )
    unmount()

    renderAuthenticatedAppShell()

    expect(screen.getByText('正在读取系统摘要')).toBeInTheDocument()
    expect(screen.queryByText('摘要已加载 · 存在活跃异常')).not.toBeInTheDocument()
    expect(document.querySelectorAll('.badge--count')).toHaveLength(0)
  })

  it('marks loaded summaries with active anomalies as degraded', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          baseOverview({
            abnormal_node_count: 1,
            abnormal_target_count: 0,
          }),
        ),
      ),
    )

    renderAuthenticatedAppShell()

    await waitFor(() =>
      expect(screen.getByText('摘要已加载 · 存在活跃异常')).toBeInTheDocument(),
    )
    expect(document.querySelector('.sync-status')).toHaveClass('sync-status--degraded')
  })

  it('marks loaded summaries without anomalies as ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(baseOverview())))

    renderAuthenticatedAppShell()

    await waitFor(() => expect(screen.getByText('摘要已加载')).toBeInTheDocument())
    expect(document.querySelector('.sync-status')).toHaveClass('sync-status--ok')
  })

  it('shows dashboard unavailable when the shell summary request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'dashboard unavailable' }, 503)),
    )

    renderAuthenticatedAppShell()

    await waitFor(() => expect(screen.getByText('摘要不可用')).toBeInTheDocument())
    expect(screen.getByText('v1.0 · dashboard unavailable')).toBeInTheDocument()
    expect(document.querySelector('.sync-status')).toHaveClass('sync-status--down')
    expect(document.querySelectorAll('.badge--count')).toHaveLength(0)
    expect(screen.queryByText('中心运行正常')).not.toBeInTheDocument()
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
    expect(container.querySelector('.app-shell')).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })
})
