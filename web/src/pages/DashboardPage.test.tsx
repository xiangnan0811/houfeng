import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'
import {
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
    vi.restoreAllMocks()
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

    const evidence = await screen.findByRole('region', { name: '观测证据' })
    expect(within(evidence).getByLabelText('异常监控实例 2')).toBeInTheDocument()
    expect(within(evidence).getByLabelText('其中严重监控实例 1')).toBeInTheDocument()
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
    expect(screen.getAllByText('资产判断等待核对').length).toBeGreaterThan(1)
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
})
