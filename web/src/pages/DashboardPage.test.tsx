import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatDateTime } from '../lib/format'
import { DashboardPage } from './DashboardPage'

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
    group_summaries: [
      {
        group: 'edge',
        node_count: 5,
        target_count: 4,
        abnormal_node_count: 0,
        abnormal_target_count: 0,
        severe_node_count: 0,
        severe_target_count: 0,
        maintenance_node_count: 0,
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

function renderWithDashboard(body: unknown, status = 200) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(body, status)))

  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders an asset-decision-first command surface for severe abnormal state', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_node_count: 1,
        abnormal_target_count: 1,
        severe_node_count: 0,
        severe_target_count: 1,
        maintenance_node_count: 1,
        maintenance_target_count: 0,
        pending_onboarding_node_count: 2,
        paused_node_count: 1,
        retired_node_count: 1,
        paused_target_count: 1,
        archived_target_count: 1,
        recent_new_incident_count: 4,
        recent_recovery_count: 1,
        new_incident_trend_24h: [0, 0, 1, 0, 2, 1, 4],
        recovery_trend_24h: [0, 0, 0, 1, 0, 0, 1],
        group_summaries: [
          {
            group: 'production',
            node_count: 3,
            target_count: 2,
            abnormal_node_count: 1,
            abnormal_target_count: 1,
            severe_node_count: 0,
            severe_target_count: 1,
            maintenance_node_count: 1,
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
        abnormal_nodes: [
          {
            node_id: 'nd_001',
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

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '先处理资产压力与严重异常' })).toBeInTheDocument(),
    )

    const commandSurface = screen.getByLabelText('工作台 command surface')
    expect(commandSurface).toHaveTextContent(`摘要生成 ${formatDateTime('2026-04-25T08:30:00Z')}`)
    expect(commandSurface).toHaveTextContent('资产侧 15 项信号')
    expect(commandSurface).toHaveTextContent('观测侧 2 个异常对象，其中严重 1')
    expect(commandSurface).toHaveTextContent('30 天续费 2 台 VPS')

    const controls = within(commandSurface).getByLabelText('工作台主要动作')
    expect(within(controls).getByRole('link', { name: '进入资产决策队列' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )

    const assetLane = within(commandSurface).getByLabelText('资产决策队列')
    expect(within(assetLane).getByRole('heading', { name: '续费、决策与缺信息' })).toBeInTheDocument()
    expect(within(assetLane).getByRole('link', { name: '30 天续费：订阅 3' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )
    expect(within(assetLane).getByRole('link', { name: '待决策：续费状态未评估' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )
    expect(within(assetLane).getByRole('link', { name: '取消 / 迁移：取消 1 · 迁移 2' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )
    expect(within(assetLane).getByRole('link', { name: '未关联 Node：需人工核对' })).toHaveAttribute(
      'href',
      '/vps',
    )
    expect(within(assetLane).getByRole('link', { name: '关联异常：VPS 关联异常 Node' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(assetLane).getByRole('link', { name: '成本：USD 42.50/月 · EUR 18.00/月' })).toHaveAttribute(
      'href',
      '/subscriptions?renew_within_days=30',
    )

    const observationLane = within(commandSurface).getByLabelText('观测异常队列')
    expect(within(observationLane).getByRole('heading', { name: '事件、节点与目标证据' })).toBeInTheDocument()
    expect(within(observationLane).getByRole('link', { name: '异常对象：节点 1 · 目标 1' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(observationLane).getByRole('link', { name: '严重：节点 0 · 目标 1' })).toHaveAttribute(
      'href',
      '/events?severity=严重',
    )
    expect(within(observationLane).getByRole('link', { name: '24h 变化：新增异常 / 恢复' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(within(observationLane).getByRole('link', { name: '维护：节点 1 · 目标 0' })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    expect(within(observationLane).getByRole('link', { name: '处理目标 Blog' })).toHaveAttribute(
      'href',
      '/targets/tg_001',
    )

    const nextActions = within(commandSurface).getByLabelText('下一步动作')
    expect(within(nextActions).getByRole('heading', { name: '今天先做什么' })).toBeInTheDocument()
    expect(within(nextActions).getByRole('link', { name: /进入资产决策队列：待决策 4/ })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )
    expect(within(nextActions).getByRole('link', { name: /处理严重事件：严重对象 1/ })).toHaveAttribute(
      'href',
      '/events?severity=严重',
    )
    expect(within(nextActions).getByRole('link', { name: /处理观测异常：异常节点 1 · 异常目标 1/ })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(nextActions).getByRole('link', { name: /核对未关联 VPS/ })).toHaveAttribute(
      'href',
      '/vps',
    )

    const attentionSection = screen.getByRole('heading', { name: '当前需要处理' }).closest('section') as HTMLElement
    expect(attentionSection).toHaveClass('detail-section')
    expect(within(attentionSection).getByText('Tokyo Edge')).toBeInTheDocument()
    expect(within(attentionSection).getByText('nd_001')).toBeInTheDocument()
    expect(within(attentionSection).getAllByText('当前问题')).toHaveLength(2)
    expect(within(attentionSection).getByText('磁盘使用率 92.0%')).toBeInTheDocument()
    expect(within(attentionSection).getByText('HTTPS 探测连续失败')).toBeInTheDocument()
    expect(within(attentionSection).getByRole('link', { name: '查看节点 Tokyo Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
    expect(within(attentionSection).getByRole('link', { name: '查看目标 Blog' })).toHaveAttribute(
      'href',
      '/targets/tg_001',
    )

    const context = within(attentionSection).getByLabelText('运行上下文')
    expect(within(context).getAllByRole('link')).toHaveLength(3)
    expect(within(context).getByRole('link', { name: '影响范围：1 个分组受影响，最高影响 production' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )

    expect(screen.queryByLabelText('资产决策摘要')).not.toBeInTheDocument()
    expect(screen.queryByText('已加载 /api/dashboard')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('首页数据可信度')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
    expect(screen.queryByText('最近事件不应在异常态展开')).not.toBeInTheDocument()
  })

  it('prioritizes observability when there is no asset pressure', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_node_count: 1,
        recent_new_incident_count: 1,
        abnormal_nodes: [
          {
            node_id: 'nd_042',
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

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '先处理观测异常' })).toBeInTheDocument(),
    )

    const commandSurface = screen.getByLabelText('工作台 command surface')
    expect(commandSurface).toHaveTextContent('资产侧暂无待处理信号')
    const controls = within(commandSurface).getByLabelText('工作台主要动作')
    expect(within(controls).getByRole('link', { name: '处理观测异常' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(commandSurface).getByRole('link', { name: /处理观测异常：异常节点 1 · 异常目标 0/ })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(screen.getAllByText('Osaka Edge').length).toBeGreaterThan(0)
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
  })

  it('routes maintenance state through the command surface and compact overview', async () => {
    renderWithDashboard(
      baseOverview({
        maintenance_node_count: 1,
        maintenance_target_count: 1,
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '维护对象正在观察' })).toBeInTheDocument(),
    )

    const commandSurface = screen.getByLabelText('工作台 command surface')
    const controls = within(commandSurface).getByLabelText('工作台主要动作')
    expect(within(controls).getByRole('link', { name: '查看维护事件' })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    const observationLane = within(commandSurface).getByLabelText('观测异常队列')
    expect(within(observationLane).getByRole('link', { name: '维护：节点 1 · 目标 1' })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    expect(screen.getByRole('heading', { name: '维护观察' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '维护观察中' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '当前需要处理' })).not.toBeInTheDocument()
    const maintenanceMetrics = screen.getByLabelText('维护观察指标')
    expect(within(maintenanceMetrics).getByRole('link', { name: /维护事件/ })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
  })

  it('renders normal state as a calm command surface, not a context warehouse', async () => {
    renderWithDashboard(
      baseOverview({
        recent_events: [
          {
            event_id: 'evt_002',
            incident_id: 'inc_002',
            incident_class: 'node_resource_pressure',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'incident_recovered',
            severity: '正常',
            summary: 'CPU 使用率恢复',
            created_at: '2026-04-25T09:10:00Z',
          },
        ],
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '今日没有紧急处理项' })).toBeInTheDocument(),
    )

    const commandSurface = screen.getByLabelText('工作台 command surface')
    expect(commandSurface).toHaveTextContent('资产侧暂无待处理信号')
    expect(commandSurface).toHaveTextContent('观测侧暂无活跃异常')
    const controls = within(commandSurface).getByLabelText('工作台主要动作')
    expect(within(controls).getByRole('link', { name: '核对 VPS 库存' })).toHaveAttribute('href', '/vps')
    const nextActions = within(commandSurface).getByLabelText('下一步动作')
    expect(within(nextActions).getByRole('link', { name: /核对 VPS 库存/ })).toHaveAttribute('href', '/vps')
    expect(within(nextActions).getByRole('link', { name: /查看 24h 事件流/ })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )

    expect(screen.getByRole('heading', { name: '运行概览' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '当前没有活跃异常' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '当前需要处理' })).not.toBeInTheDocument()
    const management = screen.getByLabelText('管理入口')
    expect(within(management).getByRole('link', { name: '节点：待接入 0 · 暂停 0 · 退役 0' })).toHaveAttribute(
      'href',
      '/nodes',
    )
    const context = screen.getByLabelText('运行上下文')
    expect(within(context).getByRole('link', { name: /最近活动：异常恢复 · 正常 · 节点 nd_001/ })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(screen.queryByText('CPU 使用率恢复')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
  })

  it('renders first-run onboarding with command actions and without API facts', async () => {
    renderWithDashboard(
      baseOverview({
        total_node_count: 0,
        total_target_count: 0,
        group_summaries: [],
        recent_events: [
          {
            event_id: 'evt_003',
            incident_id: 'inc_003',
            incident_class: 'node_resource_pressure',
            object_type: 'node',
            object_id: 'nd_003',
            event_type: 'incident_started',
            severity: '关注',
            summary: '首次接入不应显示最近事件',
            created_at: '2026-04-25T09:10:00Z',
          },
        ],
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '建立第一条资产与观测链路' })).toBeInTheDocument(),
    )

    const commandSurface = screen.getByLabelText('工作台 command surface')
    const controls = within(commandSurface).getByLabelText('工作台主要动作')
    expect(within(controls).getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    const nextActions = within(commandSurface).getByLabelText('下一步动作')
    expect(within(nextActions).getByRole('link', { name: /查看节点接入/ })).toHaveAttribute(
      'href',
      '/nodes?onboarding=pending',
    )
    expect(within(nextActions).getByRole('link', { name: /创建第一个目标/ })).toHaveAttribute('href', '/targets')

    expect(screen.getByRole('heading', { name: '首次接入工作台' })).toBeInTheDocument()
    const onboardingSection = screen.getByRole('heading', { name: '首次接入工作台' }).closest('section') as HTMLElement
    expect(within(onboardingSection).getByRole('heading', { name: '创建节点' })).toBeInTheDocument()
    expect(within(onboardingSection).getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.queryByLabelText('运行上下文')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByText('已加载 /api/dashboard')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '管理入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
    expect(screen.queryByText('首次接入不应显示最近事件')).not.toBeInTheDocument()
  })

  it('uses compact management entry priority to route inventory states to filtered lists', async () => {
    const managementLinks = async (overrides: Record<string, unknown>) => {
      const rendered = renderWithDashboard(baseOverview(overrides))

      await waitFor(() =>
        expect(screen.getByRole('heading', { name: '管理入口' })).toBeInTheDocument(),
      )

      const management = screen.getByLabelText('管理入口')
      const nodeEntry = within(management).getByRole('link', { name: /节点：/ })
      const targetEntry = within(management).getByRole('link', { name: /目标：/ })
      const result = {
        nodeHref: nodeEntry.getAttribute('href'),
        targetHref: targetEntry.getAttribute('href'),
      }

      rendered.unmount()
      vi.restoreAllMocks()
      return result
    }

    await expect(managementLinks({ pending_onboarding_node_count: 1 })).resolves.toEqual({
      nodeHref: '/nodes?onboarding=pending',
      targetHref: '/targets',
    })

    await expect(managementLinks({ paused_node_count: 1, paused_target_count: 1 })).resolves.toEqual({
      nodeHref: '/nodes?run_status=暂停',
      targetHref: '/targets?run_status=暂停',
    })

    await expect(managementLinks({ retired_node_count: 1, archived_target_count: 1 })).resolves.toEqual({
      nodeHref: '/nodes?lifecycle=已退役',
      targetHref: '/targets?run_status=已归档',
    })
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
        abnormal_node_count: 1,
        abnormal_target_count: 1,
        abnormal_nodes: [
          {
            node_id: 'nd_077',
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

    await waitFor(() => expect(screen.getAllByText('Singapore Edge').length).toBeGreaterThan(0))

    expect(screen.getByRole('link', { name: '进入节点 Singapore Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_077',
    )
    expect(screen.getByRole('link', { name: '进入目标 Payments API' })).toHaveAttribute(
      'href',
      '/targets/tg_555',
    )
    expect(screen.getByRole('link', { name: '查看节点 Singapore Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_077',
    )
    expect(screen.getByRole('link', { name: '查看目标 Payments API' })).toHaveAttribute(
      'href',
      '/targets/tg_555',
    )
  })
})
