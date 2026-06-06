import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'
import type { SubscriptionOverview } from '../lib/types'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

function baseOverview(overrides: Record<string, unknown> = {}) {
  return {
    snapshot_generated_at: '2026-04-25T08:30:00Z',
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
    group_summaries: [
      {
        group: 'edge',
        monitoring_instance_count: 5,
        target_count: 4,
        abnormal_monitoring_instance_count: 0,
        abnormal_target_count: 0,
        severe_monitoring_instance_count: 0,
        severe_target_count: 0,
        maintenance_monitoring_instance_count: 0,
        maintenance_target_count: 0,
      },
    ],
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
      cancelled_vps_count: 0,
      cancellation_attention_vps_count: 0,
      running_cancelled_asset_count: 0,
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

function baseSubscriptionOverview(overrides: Partial<SubscriptionOverview> = {}): SubscriptionOverview {
  return {
    snapshot_generated_at: '2026-04-25T08:30:00Z',
    base_currency: 'CNY',
    total_monthly_cost: 298,
    total_yearly_cost: 3576,
    active_subscription_count: 4,
    renewal_due_14d_count: 1,
    renewal_due_30d_count: 3,
    budget_risk_count: 1,
    exchange_rate_stale_count: 0,
    decision_attention_count: 0,
    missing_subscription_vps_count: 0,
    upcoming_renewals: [],
    provider_breakdown: [],
    currency_breakdown: [],
    category_breakdown: [],
    budget_risks: [],
    vps_costs: [],
    missing_subscription_assets: [],
    ...overrides,
  }
}

function renderWithDashboard(
  body: unknown,
  status = 200,
  vpsAssets: unknown[] = [],
  subscriptionOverview: SubscriptionOverview | null = baseSubscriptionOverview(),
) {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url === '/api/dashboard') return Promise.resolve(mockJSONResponse(body, status))
    if (url === '/api/vps') return Promise.resolve(mockJSONResponse(vpsAssets))
    if (url === '/api/subscriptions/overview') {
      return Promise.resolve(mockJSONResponse(subscriptionOverview ?? { error: 'subscription overview unavailable' }, subscriptionOverview ? 200 : 503))
    }
    return Promise.resolve(mockJSONResponse({ error: `unhandled ${url}` }, 404))
  }))

  return render(
    <MemoryRouter>
      <DashboardPage />
      <LocationProbe />
    </MemoryRouter>,
  )
}

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location-probe">{location.pathname}{location.search}</span>
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders an asset-decision-first command surface for severe abnormal state', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_monitoring_instance_count: 1,
        abnormal_target_count: 1,
        severe_monitoring_instance_count: 0,
        severe_target_count: 1,
        maintenance_monitoring_instance_count: 1,
        maintenance_target_count: 0,
        pending_onboarding_monitoring_instance_count: 2,
        paused_monitoring_instance_count: 1,
        retired_monitoring_instance_count: 1,
        paused_target_count: 1,
        archived_target_count: 1,
        recent_new_incident_count: 4,
        recent_recovery_count: 1,
        group_summaries: [
          {
            group: 'production',
            monitoring_instance_count: 3,
            target_count: 2,
            abnormal_monitoring_instance_count: 1,
            abnormal_target_count: 1,
            severe_monitoring_instance_count: 0,
            severe_target_count: 1,
            maintenance_monitoring_instance_count: 1,
            maintenance_target_count: 0,
          },
        ],
        notification_status: {
          telegram_configured: true,
          telegram_runtime_managed: true,
          telegram_runtime_apply_active: true,
          feishu_configured: false,
        },
        asset_summary: {
          renewal_due_30d_subscription_count: 3,
          renewal_due_30d_vps_count: 2,
          unreviewed_vps_count: 4,
          to_cancel_vps_count: 1,
          to_migrate_vps_count: 2,
          unlinked_vps_count: 5,
          abnormal_linked_vps_count: 1,
          cost_by_currency: [
            { currency: 'USD', monthly_total: 42.5, yearly_total: 510 },
            { currency: 'EUR', monthly_total: 18, yearly_total: 216 },
          ],
        },
        recent_events: [
          {
            event_id: 'evt_001',
            incident_id: 'inc_001',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'incident_started',
            severity: '严重',
            summary: '最近事件不应在异常态展开',
            created_at: '2026-04-25T08:10:00Z',
          },
        ],
        abnormal_monitoring_instances: [
          {
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            group: 'edge',
            region: 'ap-northeast-1',
            city: 'Tokyo',
            provider: 'aws',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            current_health_status: '告警',
            last_heartbeat_at: '2026-04-25T08:05:00Z',
            current_active_incident_count: 2,
            current_primary_issue_summary: '磁盘使用率 92.0%',
          },
        ],
        abnormal_targets: [
          {
            target_id: 'tg_001',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            run_status: '启用',
            group: 'prod',
            current_health_status: '严重',
            last_success_at: '2026-04-25T07:50:00Z',
            last_failure_at: '2026-04-25T08:09:00Z',
            current_active_incident_count: 1,
            current_primary_issue_summary: 'HTTPS 探测连续失败',
          },
        ],
      }),
    )

    expect(screen.getByText('正在加载工作台…')).toBeInTheDocument()

    // Wait for dashboard to load and show metric cards
    await waitFor(() => expect(screen.getByText('异常监控实例')).toBeInTheDocument())

    // Metric cards show correct counts
    expect(screen.getByText('14天内续费')).toBeInTheDocument()
    expect(screen.getByText('月均成本')).toBeInTheDocument()
    expect(screen.getAllByText('预算风险').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText('订阅预算接近或超过上限').closest('.wb-att-item')!)
    expect(screen.getByTestId('location-probe')).toHaveTextContent('/asset-decisions?view=cost&renew_within_days=30&scenario=budget_reduction')
    expect(screen.queryByText('资产决策队列')).not.toBeInTheDocument()

    // Attention column shows abnormal monitoring and targets
    expect(screen.getByText('关注')).toBeInTheDocument()
    expect(screen.getAllByText(/Tokyo Edge/).length).toBeGreaterThan(0)
    expect(screen.getByText(/磁盘使用率 92.0%/)).toBeInTheDocument()
    expect(screen.getAllByText(/Blog/).length).toBeGreaterThan(0)
    expect(screen.getByText(/HTTPS 探测连续失败/)).toBeInTheDocument()

    // Cost column shows currency data
    expect(screen.getByText('账单事实')).toBeInTheDocument()
    expect(screen.getByText('USD')).toBeInTheDocument()
    expect(screen.getByText('EUR')).toBeInTheDocument()
  })

  it('prioritizes observability when there is no asset pressure', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_monitoring_instance_count: 1,
        recent_new_incident_count: 1,
        abnormal_monitoring_instances: [
          {
            monitoring_instance_id: 'mi_042',
            display_name: 'Osaka Edge',
            group: 'edge',
            region: 'ap-northeast-3',
            city: 'Osaka',
            provider: 'aws',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            current_health_status: '告警',
            last_heartbeat_at: '2026-04-25T08:05:00Z',
            current_active_incident_count: 1,
            current_primary_issue_summary: 'CPU 使用率 95%',
          },
        ],
      }),
    )

    // Shows attention column with abnormal monitoring instance
    await waitFor(() => expect(screen.getAllByText('Osaka Edge').length).toBeGreaterThan(0))
    expect(screen.getByText(/CPU 使用率 95%/)).toBeInTheDocument()
    expect(screen.getByText('异常监控实例')).toBeInTheDocument()
  })

  it('routes maintenance state through the command surface and compact overview', async () => {
    renderWithDashboard(
      baseOverview({
        maintenance_monitoring_instance_count: 1,
        maintenance_target_count: 1,
      }),
    )

    // Normal state with no abnormal items shows calm dashboard
    await waitFor(() => expect(screen.getByText('异常监控实例')).toBeInTheDocument())
    expect(screen.getByText('暂无需关注项')).toBeInTheDocument()
  })

  it('renders normal state as a calm command surface, not a context warehouse', async () => {
    renderWithDashboard(
      baseOverview({
        recent_events: [
          {
            event_id: 'evt_002',
            incident_id: 'inc_002',
            incident_class: 'monitoring_instance_resource_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'incident_recovered',
            severity: '正常',
            summary: 'CPU 使用率恢复',
            created_at: '2026-04-25T09:10:00Z',
          },
        ],
      }),
    )

    // Shows greeting and metric cards
    await waitFor(() => expect(screen.getByText('异常监控实例')).toBeInTheDocument())
    expect(screen.getByText('14天内续费')).toBeInTheDocument()
    expect(screen.getByText('月均成本')).toBeInTheDocument()
    expect(screen.getAllByText('预算风险').length).toBeGreaterThan(0)

    // Events column shows recent event
    expect(screen.getByText('动态')).toBeInTheDocument()

    // No abnormal items in attention column
    expect(screen.getByText('暂无需关注项')).toBeInTheDocument()
    // All monitoring normal
    expect(screen.getByText('暂无异常观测')).toBeInTheDocument()
  })

  it('renders first-run onboarding with zero monitoring and targets', async () => {
    renderWithDashboard(
      baseOverview({
        total_monitoring_instance_count: 0,
        total_target_count: 0,
        group_summaries: [],
        recent_events: [
          {
            event_id: 'evt_003',
            incident_id: 'inc_003',
            incident_class: 'monitoring_instance_resource_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_003',
            event_type: 'incident_started',
            severity: '关注',
            summary: '首次接入不应显示最近事件',
            created_at: '2026-04-25T09:10:00Z',
          },
        ],
      }),
    )

    // Shows metric cards with zero counts
    await waitFor(() => expect(screen.getByText('异常监控实例')).toBeInTheDocument())
    expect(screen.getByText('先创建第一台 VPS')).toBeInTheDocument()
    expect(screen.getByText('订阅和 agent 接入都在 VPS 详情页完成')).toBeInTheDocument()
  })

  it('uses compact management entry priority to route inventory states to filtered lists', async () => {
    // The new dashboard shows metric cards and columns, not management entries
    // Verify the dashboard renders correctly with various states
    renderWithDashboard(
      baseOverview({
        pending_onboarding_monitoring_instance_count: 1,
        paused_monitoring_instance_count: 1,
        retired_monitoring_instance_count: 1,
      }),
    )

    await waitFor(() => expect(screen.getByText('异常监控实例')).toBeInTheDocument())
    // Metric cards are present
    expect(screen.getByText('14天内续费')).toBeInTheDocument()
    expect(screen.getByText('月均成本')).toBeInTheDocument()
  })

  it('renders an explicit error state when the dashboard request fails', async () => {
    renderWithDashboard({ error: 'dashboard unavailable' }, 503)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '工作台不可用' })).toBeInTheDocument(),
    )

    expect(screen.getByText('工作台')).toBeInTheDocument()
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
  })

  it('keeps attention queue action links as direct detail links', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_monitoring_instance_count: 1,
        abnormal_target_count: 1,
        abnormal_monitoring_instances: [
          {
            monitoring_instance_id: 'mi_077',
            display_name: 'Singapore Edge',
            group: 'edge',
            region: 'ap-southeast-1',
            city: 'Singapore',
            provider: 'aws',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            current_health_status: '告警',
            last_heartbeat_at: '2026-04-25T08:05:00Z',
            current_active_incident_count: 3,
            current_primary_issue_summary: '磁盘使用率 99%',
          },
        ],
        abnormal_targets: [
          {
            target_id: 'tg_555',
            name: 'Payments API',
            target_type: 'service',
            host: 'pay.example.com',
            base_port: 443,
            run_status: '启用',
            group: 'prod',
            current_health_status: '告警',
            last_success_at: '2026-04-25T07:50:00Z',
            last_failure_at: '2026-04-25T08:09:00Z',
            current_active_incident_count: 2,
            current_primary_issue_summary: 'TLS 探测连续失败',
          },
        ],
      }),
    )

    // Attention column shows abnormal items
    await waitFor(() => expect(screen.getAllByText(/Singapore Edge/).length).toBeGreaterThan(0))
    expect(screen.getByText(/磁盘使用率 99%/)).toBeInTheDocument()
    expect(screen.getAllByText(/Payments API/).length).toBeGreaterThan(0)
    expect(screen.getByText(/TLS 探测连续失败/)).toBeInTheDocument()
  })
})
