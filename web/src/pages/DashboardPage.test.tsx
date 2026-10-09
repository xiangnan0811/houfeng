import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { targetObservationFixture } from '../lib/targetObservationFixture'
import { DashboardPage } from './DashboardPage'
import {
  dashboardGroupSummaryFixture,
  dashboardOverviewFixture,
  subscriptionOverviewFixture,
  vpsAssetFixture,
} from './dashboard/dashboardTestFixtures'
import type { DashboardOverview, SubscriptionOverview, VPSAssetRecord } from '../lib/types'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

type ResponseFixture<T> = {
  body: T
  status?: number
}

type DashboardResponses = {
  dashboard?: ResponseFixture<DashboardOverview | { error: string }>
  vps?: ResponseFixture<VPSAssetRecord[] | { error: string }>
  subscription?: ResponseFixture<SubscriptionOverview | { error: string }>
}

function renderDashboard(responses: DashboardResponses = {}) {
  const dashboard = responses.dashboard ?? {
    body: dashboardOverviewFixture(),
    status: 200,
  }
  const vps = responses.vps ?? {
    body: [vpsAssetFixture()],
    status: 200,
  }
  const subscription = responses.subscription ?? {
    body: subscriptionOverviewFixture(),
    status: 200,
  }
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url === '/api/dashboard') {
      return Promise.resolve(mockJSONResponse(dashboard.body, dashboard.status))
    }
    if (url === '/api/vps') {
      return Promise.resolve(mockJSONResponse(vps.body, vps.status))
    }
    if (url === '/api/subscriptions/overview') {
      return Promise.resolve(mockJSONResponse(subscription.body, subscription.status))
    }
    return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
  })
  vi.stubGlobal('fetch', fetchMock)

  return {
    fetchMock,
    ...render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    ),
  }
}

async function primaryAction() {
  const region = await screen.findByRole('region', { name: '今日第一步' })
  const links = within(region).getAllByRole('link')
  expect(links).toHaveLength(1)
  return links[0]
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
  })

  it.each([
    {
      name: 'onboarding',
      overview: dashboardOverviewFixture({
        total_monitoring_instance_count: 0,
        total_target_count: 0,
      }),
      vps: [] as VPSAssetRecord[],
      label: '创建第一台 VPS',
      href: '/vps',
      heading: '建立第一条资产与观测链路',
    },
    {
      name: 'critical',
      overview: dashboardOverviewFixture({
        abnormal_monitoring_instance_count: 2,
        severe_monitoring_instance_count: 1,
      }),
      vps: [vpsAssetFixture()],
      label: '处理严重异常',
      href: '/events?severity=严重',
      heading: '严重异常需要立即处理',
    },
    {
      name: 'abnormal',
      overview: dashboardOverviewFixture({
        abnormal_monitoring_instance_count: 1,
      }),
      vps: [vpsAssetFixture()],
      label: '处理观测异常',
      href: '/monitoring?abnormal=1',
      heading: '观测异常需要处理',
    },
    {
      name: 'maintenance',
      overview: dashboardOverviewFixture({
        maintenance_monitoring_instance_count: 1,
      }),
      vps: [vpsAssetFixture()],
      label: '查看维护事件',
      href: '/events?maintenance_only=1',
      heading: '维护对象正在观察',
    },
    {
      name: 'stable',
      overview: dashboardOverviewFixture(),
      vps: [vpsAssetFixture()],
      label: '核对 VPS 库存',
      href: '/vps',
      heading: '当前没有紧急处理项',
    },
  ])('renders the $name mode with one primary action', async ({ overview, vps, label, href, heading }) => {
    renderDashboard({ dashboard: { body: overview }, vps: { body: vps } })

    expect(screen.getByText('正在加载工作台…')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '工作台' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument()
    expect(await primaryAction()).toHaveAttribute('href', href)
    expect(screen.getByRole('link', { name: label })).toBeInTheDocument()

    const judgementRail = screen.getByRole('region', { name: '判断摘要' })
    expect(within(judgementRail).getAllByRole('link')).toHaveLength(3)
    expect(screen.queryByText('最近事件摘要')).not.toBeInTheDocument()
    expect(screen.queryByText('系统快捷入口')).not.toBeInTheDocument()
    expect(screen.queryByText('资产总览')).not.toBeInTheDocument()
    expect(screen.queryByText('14天内续费')).not.toBeInTheDocument()
  })

  it('shows abnormal=2 and severe=1 without double-counting severe instances', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          abnormal_monitoring_instance_count: 2,
          severe_monitoring_instance_count: 1,
        }),
      },
    })

    const judgementRail = await screen.findByRole('region', { name: '判断摘要' })
    expect(within(judgementRail).getByRole('link', { name: /异常总数 2（严重已包含）/ })).toBeInTheDocument()
    expect(within(judgementRail).queryByRole('link', { name: /异常总数 3/ })).not.toBeInTheDocument()
    const evidence = screen.getByRole('region', { name: '观测证据' })
    expect(within(evidence).getByText(/来源：工作台摘要/)).toBeInTheDocument()
    expect(within(evidence).getByText(/已有历史观测不推断当前健康/)).toBeInTheDocument()
    expect(within(evidence).queryByLabelText('异常监控实例 2')).not.toBeInTheDocument()
    expect(within(evidence).queryByLabelText('异常监控实例 3')).not.toBeInTheDocument()
  })

  it('keeps VPS failure local and never presents false onboarding', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          total_monitoring_instance_count: 0,
          total_target_count: 0,
        }),
      },
      vps: { body: { error: 'VPS unavailable' }, status: 503 },
    })

    const evidence = await screen.findByRole('region', { name: '资产与账单证据' })
    expect(within(evidence).getByRole('heading', { name: 'VPS 清单不可用' })).toBeInTheDocument()
    expect(within(evidence).getByText(/VPS unavailable/)).toBeInTheDocument()
    expect(within(evidence).getByText(/无法确认是否首次接入/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '创建第一台 VPS' })).not.toBeInTheDocument()
    expect(screen.queryByText('先创建第一台 VPS')).not.toBeInTheDocument()
    expect(screen.queryByText('摘要无异常')).not.toBeInTheDocument()
    expect(screen.queryByText('当前没有紧急处理项')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '部分事实待确认' })).toBeInTheDocument()
    expect(screen.getAllByText('局部数据不可用')).toHaveLength(2)
    expect(screen.getByRole('button', { name: '重试局部数据' })).toBeInTheDocument()
  })

  it('retries only supporting resources after a local failure', async () => {
    let vpsAttempts = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/dashboard') {
        return Promise.resolve(mockJSONResponse(dashboardOverviewFixture({
          total_monitoring_instance_count: 0,
          total_target_count: 0,
        })))
      }
      if (url === '/api/vps') {
        vpsAttempts += 1
        return Promise.resolve(
          vpsAttempts === 1
            ? mockJSONResponse({ error: 'VPS unavailable' }, 503)
            : mockJSONResponse([]),
        )
      }
      if (url === '/api/subscriptions/overview') {
        return Promise.resolve(mockJSONResponse(subscriptionOverviewFixture()))
      }
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByRole('button', { name: '重试局部数据' }))

    expect(await screen.findByRole('link', { name: '创建第一台 VPS' })).toHaveAttribute('href', '/vps')
    expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/dashboard')).toHaveLength(1)
    expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/vps')).toHaveLength(2)
  })

  it('labels subscription failure and its lower-precision dashboard fallback', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          asset_summary: {
            renewal_due_30d_vps_count: 2,
            cost_by_currency: [
              { currency: 'USD', monthly_total: 42.5, yearly_total: 510 },
            ],
          },
        }),
      },
      subscription: {
        body: { error: 'subscription overview unavailable' },
        status: 503,
      },
    })

    const evidence = await screen.findByRole('region', { name: '资产与账单证据' })
    expect(within(evidence).getByText('订阅摘要不可用')).toBeInTheDocument()
    expect(within(evidence).getByText(/subscription overview unavailable/)).toBeInTheDocument()
    expect(within(evidence).getAllByText(/Dashboard 聚合摘要/).length).toBeGreaterThan(0)
    expect(within(evidence).getByText(/USD/)).toBeInTheDocument()
  })

  it('does not label a stable observability mode with asset attention as anomaly-free', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          asset_summary: { unreviewed_vps_count: 2 },
        }),
      },
    })

    expect(await primaryAction()).toHaveAttribute(
      'href',
      '/asset-decisions?view=needs_decision&renew_within_days=30',
    )
    expect(screen.queryByText('摘要无异常')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '资产判断等待核对' })).toBeInTheDocument()
  })

  it('renders a retryable full-page error only for the dashboard request', async () => {
    renderDashboard({
      dashboard: { body: { error: 'dashboard unavailable' }, status: 503 },
    })

    expect(await screen.findByRole('heading', { name: '工作台不可用' })).toBeInTheDocument()
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '今日第一步' })).not.toBeInTheDocument()
  })

  it('shows the highest-priority abnormal objects as direct detail links', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          abnormal_monitoring_instance_count: 1,
          abnormal_target_count: 1,
          severe_target_count: 1,
          abnormal_monitoring_instances: [{
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            group: 'edge',
            region: 'ap-northeast-1',
            city: 'Tokyo',
            provider: 'aws',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            current_health_status: '告警',
            last_heartbeat_at: '2026-07-10T06:20:00Z',
            current_active_incident_count: 2,
            current_primary_issue_summary: '磁盘使用率 92%',
          }],
          abnormal_targets: [{
            target_id: 'tg_001',
            name: 'Payments API',
            target_type: 'service',
            host: 'pay.example.com',
            base_port: 443,
            run_status: '启用',
            group: 'prod',
            current_health_status: '严重',
            observation_freshness: targetObservationFixture({
              target_id: 'tg_001',
              run_status: '启用',
              lifecycle_status: 'active',
              evaluated_at: '2026-07-10T06:25:00Z',
              last_failure_at: '2026-07-10T06:22:00Z',
            }),
            last_failure_at: '2026-07-10T06:22:00Z',
            current_active_incident_count: 1,
            current_primary_issue_summary: 'HTTPS 探测连续失败',
          }],
        }),
      },
    })

    const queue = await screen.findByRole('list', { name: '最高优先级异常对象' })
    const links = within(queue).getAllByRole('link')
    expect(links[0]).toHaveAttribute('href', '/targets/tg_001')
    expect(within(queue).getByRole('link', { name: /Payments API/ })).toHaveAttribute('href', '/targets/tg_001')
    expect(within(queue).getByRole('link', { name: /Tokyo Edge/ })).toHaveAttribute('href', '/monitoring/mi_001')
  })

  it('requests dashboard, VPS, and subscription overview independently', async () => {
    const { fetchMock } = renderDashboard()

    await waitFor(() => expect(screen.getByRole('heading', { name: '工作台' })).toBeInTheDocument())
    expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual(expect.arrayContaining([
      '/api/dashboard',
      '/api/vps',
      '/api/subscriptions/overview',
    ]))
  })

  it('previews bounded renewals and recent activity beside the fixed three judgements', async () => {
    const renewals = Array.from({ length: 7 }, (_, index) => ({
      subscription_id: `sub_${index}`,
      vps_id: `vps_${index}`,
      vps_display_name: `VPS ${index}`,
      display_name: `Plan ${index}`,
      provider_name: 'Example Cloud',
      renew_at: `2099-0${index + 1}-01`,
      monthly_price_base: 20,
      base_currency: 'CNY',
      currency: 'USD',
      renewal_decision: 'keep',
      lifecycle_status: 'active',
      exchange_rate_status: 'fresh' as const,
    }))
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          recent_events: Array.from({ length: 6 }, (_, index) => ({
            event_id: `ev_${index}`,
            incident_id: `inc_${index}`,
            incident_class: 'heartbeat',
            object_type: 'monitoring_instance' as const,
            object_id: 'mi_001',
            event_type: 'incident_started' as const,
            severity: '告警' as const,
            summary: `Tokyo Edge 事件 ${index}`,
            created_at: `2026-07-10T0${index}:00:00Z`,
          })),
        }),
      },
      subscription: { body: subscriptionOverviewFixture({ upcoming_renewals: renewals }) },
    })

    const judgementRail = await screen.findByRole('region', { name: '判断摘要' })
    expect(within(judgementRail).getAllByRole('link')).toHaveLength(3)

    const renewalList = await screen.findByRole('list', { name: '即将续费的订阅' })
    const renewalLinks = within(renewalList).getAllByRole('link')
    expect(renewalLinks).toHaveLength(5)
    expect(renewalLinks[0]).toHaveAttribute('href', '/vps/vps_0')
    expect(renewalLinks[0]).toHaveTextContent('Plan 0')
    expect(screen.getByText('未来 90 天（UTC）· 7 项')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '订阅明细' })).toHaveAttribute('href', '/subscriptions?view=details')

    const activity = screen.getByRole('list', { name: '最近状态变化' })
    expect(within(activity).getAllByRole('listitem')).toHaveLength(5)
    expect(within(activity).getAllByRole('listitem')[0]).toHaveTextContent('Tokyo Edge 事件 5')
    expect(screen.getByRole('link', { name: '事件流' })).toHaveAttribute('href', '/events')
  })

  it('exposes the 24h incident trend total in the observation judgement name', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          new_incident_trend_24h: Array.from({ length: 24 }, (_, index) => (index === 3 || index === 20 ? 1 : 0)),
        }),
      },
    })

    const judgementRail = await screen.findByRole('region', { name: '判断摘要' })
    expect(within(judgementRail).getByRole('link', { name: /24 小时新增异常 2 次$/ })).toBeInTheDocument()
  })

  it('labels a renewal queue at the backend cap as a lower bound', async () => {
    const renewals = Array.from({ length: 12 }, (_, index) => ({
      subscription_id: `sub_${index}`,
      vps_id: `vps_${index}`,
      vps_display_name: `VPS ${index}`,
      display_name: `Plan ${index}`,
      provider_name: 'Example Cloud',
      renew_at: `2099-01-${String(index + 1).padStart(2, '0')}`,
      monthly_price_base: 20,
      base_currency: 'CNY',
      currency: 'USD',
      renewal_decision: 'keep',
      lifecycle_status: 'active',
      exchange_rate_status: 'fresh' as const,
    }))
    renderDashboard({ subscription: { body: subscriptionOverviewFixture({ upcoming_renewals: renewals }) } })

    expect(await screen.findByText('未来 90 天（UTC）· 至少 12 项')).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: '即将续费的订阅' })).getAllByRole('link')).toHaveLength(5)
  })

  it('labels renewal days as estimated when the subscription snapshot time is invalid', async () => {
    renderDashboard({
      subscription: {
        body: subscriptionOverviewFixture({
          snapshot_generated_at: 'invalid',
          upcoming_renewals: [{
            subscription_id: 'sub_1',
            vps_id: 'vps_1',
            vps_display_name: 'VPS 1',
            display_name: 'Plan 1',
            provider_name: 'Example Cloud',
            renew_at: '2099-01-01',
            monthly_price_base: 20,
            base_currency: 'CNY',
            currency: 'USD',
            renewal_decision: 'keep',
            lifecycle_status: 'active',
            exchange_rate_status: 'fresh',
          }],
        }),
      },
    })

    expect(await screen.findByText('未来 90 天（UTC）· 1 项 · 天数按接收时间估算')).toBeInTheDocument()
  })

  it('keeps the renewal preview honest when the subscription overview fails', async () => {
    renderDashboard({
      subscription: { body: { error: 'subscription unavailable' }, status: 503 },
    })

    expect(await screen.findByText(/续费队列暂不可用/)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: '即将续费的订阅' })).not.toBeInTheDocument()
    expect(screen.queryByText(/内没有待续费的订阅/)).not.toBeInTheDocument()
  })

  it('links stale targets separately from abnormal counts and keeps the group', async () => {
    renderDashboard({
      dashboard: {
        body: dashboardOverviewFixture({
          abnormal_target_count: 2,
          stale_target_count: 2,
          group_summaries: [
            dashboardGroupSummaryFixture({ group: 'edge', stale_target_count: 2 }),
            dashboardGroupSummaryFixture({ group: '未分组', stale_target_count: 0 }),
          ],
        }),
      },
    })

    const judgementRail = await screen.findByRole('region', { name: '判断摘要' })
    expect(within(judgementRail).getByRole('link', { name: /观测异常：2/ })).toBeInTheDocument()
    expect(within(judgementRail).queryByRole('link', { name: /观测异常：4/ })).not.toBeInTheDocument()
    const evidence = screen.getByRole('region', { name: '观测证据' })
    expect(within(evidence).getByRole('link', { name: '观测过期 2' })).toHaveAttribute('href', '/targets?view=stale')
    expect(within(evidence).getByRole('link', { name: 'edge 观测过期 2' })).toHaveAttribute('href', '/targets?view=stale&group=edge')
    expect(within(evidence).queryByRole('link', { name: /未分组/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '按 Group 分布' })).not.toBeInTheDocument()
  })

  it('keeps the last dashboard snapshot when a later read fails', async () => {
    let dashboardCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/dashboard') {
        dashboardCalls += 1
        if (dashboardCalls === 1) {
          return Promise.resolve(mockJSONResponse(dashboardOverviewFixture({
            abnormal_target_count: 2,
            stale_target_count: 3,
            snapshot_generated_at: '2026-07-10T06:25:00Z',
          })))
        }
        return Promise.resolve(mockJSONResponse({ error: 'dashboard unavailable' }, 503))
      }
      if (url === '/api/vps') return Promise.resolve(mockJSONResponse([vpsAssetFixture()]))
      if (url === '/api/subscriptions/overview') return Promise.resolve(mockJSONResponse(subscriptionOverviewFixture()))
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    expect(await screen.findByRole('link', { name: '观测过期 3' })).toBeInTheDocument()
    fireEvent(window, new Event('focus'))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('更新失败，显示上次结果'))
    expect(screen.getByRole('link', { name: /观测异常：2/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '观测过期 3' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '工作台不可用' })).not.toBeInTheDocument()
    expect(screen.queryByText('最新')).not.toBeInTheDocument()
  })

  it('clears the dashboard after an authorization or not-found refresh', async () => {
    let dashboardCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/dashboard') {
        dashboardCalls += 1
        if (dashboardCalls === 1) {
          return Promise.resolve(mockJSONResponse(dashboardOverviewFixture({ stale_target_count: 2 })))
        }
        return Promise.resolve(mockJSONResponse({ error: 'missing' }, 404))
      }
      if (url === '/api/vps') return Promise.resolve(mockJSONResponse([vpsAssetFixture()]))
      if (url === '/api/subscriptions/overview') return Promise.resolve(mockJSONResponse(subscriptionOverviewFixture()))
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    expect(await screen.findByRole('link', { name: /观测过期：2/ })).toBeInTheDocument()
    fireEvent(window, new Event('focus'))
    expect(await screen.findByRole('heading', { name: '工作台不可用' })).toBeInTheDocument()
    expect(screen.getByText('工作台不存在')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /观测过期/ })).not.toBeInTheDocument()
    expect(screen.queryByText('更新失败，显示上次结果')).not.toBeInTheDocument()
  })

  it('refreshes the visible dashboard every 30 seconds and skips the hidden interval', async () => {
    vi.useFakeTimers()
    let dashboardCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/dashboard') {
        dashboardCalls += 1
        return Promise.resolve(mockJSONResponse(dashboardOverviewFixture({
          stale_target_count: dashboardCalls,
        })))
      }
      if (url === '/api/vps') return Promise.resolve(mockJSONResponse([vpsAssetFixture()]))
      if (url === '/api/subscriptions/overview') return Promise.resolve(mockJSONResponse(subscriptionOverviewFixture()))
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )
    await act(async () => {})
    expect(dashboardCalls).toBe(1)

    await act(async () => {
      vi.advanceTimersByTime(30_000)
    })
    expect(dashboardCalls).toBe(2)

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    fireEvent(document, new Event('visibilitychange'))
    fireEvent(window, new Event('focus'))
    await act(async () => {
      vi.advanceTimersByTime(30_000)
    })
    expect(dashboardCalls).toBe(2)
  })

  it('does not apply a dashboard response after the page unmounts', async () => {
    let resolveDashboard: (response: Response) => void = () => {}
    const pending = new Promise<Response>((resolve) => {
      resolveDashboard = resolve
    })
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/dashboard') return pending
      if (url === '/api/vps') return Promise.resolve(mockJSONResponse([vpsAssetFixture()]))
      if (url === '/api/subscriptions/overview') return Promise.resolve(mockJSONResponse(subscriptionOverviewFixture()))
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    const view = render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )
    view.unmount()
    resolveDashboard(mockJSONResponse(dashboardOverviewFixture({
      abnormal_target_count: 9,
      stale_target_count: 9,
    })))
    await act(async () => {})
    expect(screen.queryByRole('heading', { name: '工作台' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /观测过期/ })).not.toBeInTheDocument()
  })
})
