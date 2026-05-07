import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

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

  it('keeps severe abnormal state focused on the attention queue and PR4 links only', async () => {
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

    expect(screen.getByText('正在加载首页 / Dashboard…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '需要处理严重异常' })).toBeInTheDocument(),
    )

    const statusBar = screen.getByLabelText('Dashboard 状态')
    expect(statusBar).toHaveTextContent('摘要生成')
    expect(statusBar).toHaveTextContent('2026/04/25 16:30')
    expect(statusBar).toHaveTextContent('2 个对象异常，其中 1 个严重')
    const keyMetrics = screen.getByLabelText('关键状态指标')
    expect(within(keyMetrics).getAllByRole('link')).toHaveLength(4)
    expect(within(keyMetrics).getByRole('link', { name: '异常对象：节点 1 · 目标 1' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(keyMetrics).getByRole('link', { name: '严重：节点 0 · 目标 1' })).toHaveAttribute(
      'href',
      '/events?severity=严重',
    )
    expect(within(keyMetrics).getByRole('link', { name: '24h 变化：新增异常 / 恢复' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(within(keyMetrics).getByRole('link', { name: '维护：节点 1 · 目标 0' })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '查看当前异常' })).toHaveAttribute(
      'href',
      '/events?severity=严重',
    )
    expect(within(fleetActions).getByRole('link', { name: '查看事件流' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(within(fleetActions).getByRole('link', { name: '进入设置' })).toHaveAttribute(
      'href',
      '/settings',
    )

    expect(screen.queryByText('已加载 /api/dashboard')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('首页数据可信度')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()

    expect(screen.getByRole('heading', { name: '当前需要处理' })).toBeInTheDocument()
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率 92.0%')).toBeInTheDocument()
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('blog.example.com:443')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看节点 Tokyo Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
    expect(screen.getByRole('link', { name: '查看目标 Blog' })).toHaveAttribute(
      'href',
      '/targets/tg_001',
    )
    const attentionSection = screen.getByRole('heading', { name: '当前需要处理' }).closest('section') as HTMLElement
    expect(attentionSection).toHaveClass('detail-section')
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1)
    expect(within(attentionSection).getByRole('link', { name: '查看全部异常节点' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(attentionSection).getByRole('link', { name: '查看全部异常目标' })).toHaveAttribute(
      'href',
      '/targets?abnormal=1',
    )
    expect(within(attentionSection).getByRole('link', { name: '查看事件流' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    const context = screen.getByLabelText('运行上下文')
    expect(within(context).getAllByRole('link')).toHaveLength(3)
    expect(within(context).getByRole('link', { name: '影响范围：1 个分组受影响，最高影响 production' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(within(context).getByRole('link', { name: /库存状态：节点 5 · 目标 4/ })).toHaveAttribute(
      'href',
      '/nodes?onboarding=pending',
    )
    expect(within(context).getByRole('link', { name: /最近活动：异常开始 · 严重 · 目标 tg_001/ })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )

    expect(screen.queryByLabelText('工作台上下文')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
    expect(screen.queryByText('production')).not.toBeInTheDocument()
    expect(screen.queryByText('最近事件不应在异常态展开')).not.toBeInTheDocument()
    expect(screen.queryByText(/管理服务器、agent 接入/)).not.toBeInTheDocument()
  })

  it('renders abnormal-but-not-severe state without restoring global cards', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_node_count: 1,
        abnormal_target_count: 0,
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
      expect(screen.getByRole('heading', { name: '存在活跃异常' })).toBeInTheDocument(),
    )

    expect(screen.getByText('1 个对象需要关注；最近 24h 新增 1 次异常，恢复 0 次。')).toBeInTheDocument()
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '查看当前异常' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(screen.getByText('Osaka Edge')).toBeInTheDocument()
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
  })

  it('routes maintenance state through a compact management surface', async () => {
    renderWithDashboard(
      baseOverview({
        maintenance_node_count: 1,
        maintenance_target_count: 1,
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '系统处于维护观察中' })).toBeInTheDocument(),
    )

    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '查看维护事件' })).toHaveAttribute(
      'href',
      '/events?maintenance_only=1',
    )
    const keyMetrics = screen.getByLabelText('关键状态指标')
    expect(within(keyMetrics).getByRole('link', { name: '维护：节点 1 · 目标 1' })).toHaveAttribute(
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
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '管理入口' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
  })

  it('renders normal state as management entries, not a context warehouse', async () => {
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
      expect(screen.getByRole('heading', { name: '系统运行正常' })).toBeInTheDocument(),
    )

    expect(screen.getByText('当前没有活跃异常；最近 24h 新增 0 次异常，恢复 0 次。')).toBeInTheDocument()
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '查看节点' })).toHaveAttribute('href', '/nodes')
    const keyMetrics = screen.getByLabelText('关键状态指标')
    expect(within(keyMetrics).getAllByRole('link')).toHaveLength(4)
    expect(within(keyMetrics).getByRole('link', { name: '节点：待接入 0 · 暂停 0 · 退役 0' })).toHaveAttribute(
      'href',
      '/nodes',
    )
    expect(within(keyMetrics).getByRole('link', { name: '目标：异常 0 · 暂停 0 · 归档 0' })).toHaveAttribute(
      'href',
      '/targets',
    )
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()

    expect(screen.getByRole('heading', { name: '运行概览' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '当前没有活跃异常' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '当前需要处理' })).not.toBeInTheDocument()
    const runningMetrics = screen.getByLabelText('运行概览指标')
    expect(within(runningMetrics).getByRole('link', { name: /节点库存/ })).toHaveAttribute('href', '/nodes')
    expect(within(runningMetrics).getByRole('link', { name: /目标库存/ })).toHaveAttribute('href', '/targets')
    expect(within(runningMetrics).getByRole('link', { name: /24h 变化/ })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    const management = screen.getByLabelText('管理入口')
    expect(within(management).getByRole('link', { name: '节点：待接入 0 · 暂停 0 · 退役 0' })).toHaveAttribute(
      'href',
      '/nodes',
    )
    expect(within(management).getByRole('link', { name: '事件：24h 新增 0 · 恢复 0' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    const context = screen.getByLabelText('运行上下文')
    expect(within(context).getByRole('link', { name: '影响范围：覆盖 1 个分组，当前无异常分组' })).toHaveAttribute(
      'href',
      '/nodes',
    )
    expect(within(context).getByRole('link', { name: /最近活动：异常恢复 · 正常 · 节点 nd_001/ })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
    expect(screen.queryByText('CPU 使用率恢复')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Group 摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '最近事件摘要' })).not.toBeInTheDocument()
  })

  it('renders first-run onboarding only, without KPI, Group, Recent, or API facts', async () => {
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
      expect(screen.getByRole('heading', { name: '开始接入第一台服务器' })).toBeInTheDocument(),
    )

    expect(
      screen.getByText('候风还没有节点与目标。先创建节点并接入 agent，再创建观测目标与 ProbeItem。'),
    ).toBeInTheDocument()
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getAllByRole('link')).toHaveLength(1)
    expect(within(fleetActions).getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('heading', { name: '首次接入工作台' })).toBeInTheDocument()
    const onboardingSection = screen.getByRole('heading', { name: '首次接入工作台' }).closest('section') as HTMLElement
    expect(within(onboardingSection).getByRole('heading', { name: '创建节点' })).toBeInTheDocument()
    expect(within(onboardingSection).getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    expect(within(onboardingSection).getByRole('heading', { name: '接入 agent' })).toBeInTheDocument()
    expect(within(onboardingSection).getByRole('link', { name: '查看节点接入' })).toHaveAttribute(
      'href',
      '/nodes?onboarding=pending',
    )
    expect(within(onboardingSection).getByRole('heading', { name: '创建目标' })).toBeInTheDocument()
    expect(within(onboardingSection).getByRole('link', { name: '创建第一个目标' })).toHaveAttribute('href', '/targets')
    expect(within(onboardingSection).getByRole('heading', { name: '添加 ProbeItem' })).toBeInTheDocument()
    expect(within(onboardingSection).getByRole('link', { name: '添加 ProbeItem' })).toHaveAttribute('href', '/targets')

    expect(screen.queryByLabelText('关键状态指标')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('运行上下文')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Dashboard 摘要指标')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('系统全局指标')).not.toBeInTheDocument()
    expect(screen.queryByText('已加载 /api/dashboard')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '当前需要处理' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '管理入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '系统快捷入口' })).not.toBeInTheDocument()
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
      expect(screen.getByRole('heading', { name: '首页不可用' })).toBeInTheDocument(),
    )

    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
  })

  it('uses links for node and target detail navigation from the unified attention queue', async () => {
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

    await waitFor(() => expect(screen.getByText('Singapore Edge')).toBeInTheDocument())

    expect(screen.getByRole('link', { name: '进入节点 Singapore Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_077',
    )
    expect(screen.getByRole('link', { name: '进入目标 Payments API' })).toHaveAttribute(
      'href',
      '/targets/tg_555',
    )
  })

  it('keeps secondary attention queue action links as direct detail links', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_node_count: 1,
        abnormal_nodes: [
          {
            node_id: 'nd_088',
            display_name: 'Taipei Edge',
            group: 'edge',
            region: 'ap-east-1',
            city: 'Taipei',
            provider: 'aws',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            current_health_status: '严重',
            last_heartbeat_at: '2026-04-25T08:05:00Z',
            current_active_incident_count: 4,
            current_primary_issue_summary: '心跳缺失',
          },
        ],
      }),
    )

    await waitFor(() => expect(screen.getByText('Taipei Edge')).toBeInTheDocument())

    expect(screen.getByRole('link', { name: '查看节点 Taipei Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_088',
    )
  })

  it('keeps target attention queue action links as direct detail links', async () => {
    renderWithDashboard(
      baseOverview({
        abnormal_target_count: 1,
        abnormal_targets: [
          {
            target_id: 'tg_909',
            name: 'Status API',
            target_type: 'service',
            host: 'status.example.com',
            base_port: 443,
            run_status: '启用',
            group: 'prod',
            current_health_status: '严重',
            last_success_at: '2026-04-25T07:50:00Z',
            last_failure_at: '2026-04-25T08:09:00Z',
            current_active_incident_count: 2,
            current_primary_issue_summary: '状态页不可达',
          },
        ],
      }),
    )

    await waitFor(() => expect(screen.getByText('Status API')).toBeInTheDocument())

    expect(screen.getByRole('link', { name: '查看目标 Status API' })).toHaveAttribute(
      'href',
      '/targets/tg_909',
    )
  })
})
