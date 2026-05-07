import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'

const navigateMock = vi.fn()

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => navigateMock,
  }
})

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

  render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    navigateMock.mockReset()
  })

  it('renders severe fleet state, global KPIs, system entry points, and recent events from /api/dashboard', async () => {
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
        new_incident_trend_24h: [0, 1, 0, 3],
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
          {
            group: '未分组',
            node_count: 2,
            target_count: 2,
            abnormal_node_count: 0,
            abnormal_target_count: 0,
            severe_node_count: 0,
            severe_target_count: 0,
            maintenance_node_count: 0,
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
            summary: 'HTTPS 探测连续失败',
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

    expect(
      screen.getByText('2 个对象异常，其中 1 个严重；最近 24h 新增 4 次异常，恢复 1 次。'),
    ).toBeInTheDocument()
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '查看当前异常' })).toHaveAttribute('href', '/events')
    expect(within(fleetActions).getByRole('link', { name: '查看事件流' })).toHaveAttribute('href', '/events')
    expect(within(fleetActions).getByRole('link', { name: '进入设置' })).toHaveAttribute('href', '/settings')
    expect(screen.getByText('已加载 /api/dashboard')).toBeInTheDocument()
    expect(screen.getByText('Dashboard 摘要')).toBeInTheDocument()
    expect(screen.getByText('2026/04/25 16:30')).toBeInTheDocument()
    expect(screen.queryByText('接口暂未提供')).not.toBeInTheDocument()

    const kpiStrip = screen.getByLabelText('系统全局指标')
    expect(within(kpiStrip).getByRole('link', { name: '节点：1 个异常' })).toHaveAttribute('href', '/nodes')
    expect(within(kpiStrip).getByRole('link', { name: '目标：1 个异常' })).toHaveAttribute('href', '/targets')
    expect(within(kpiStrip).getByRole('link', { name: '严重：节点 0 · 目标 1' })).toHaveAttribute('href', '/events')
    expect(within(kpiStrip).getByRole('link', { name: '维护：节点 1 · 目标 0' })).toHaveAttribute('href', '/nodes')
    expect(within(kpiStrip).getByRole('link', { name: '24h 变化：新增异常 / 恢复' })).toHaveAttribute('href', '/events')

    expect(screen.getByRole('heading', { name: '当前需要处理' })).toBeInTheDocument()
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率 92.0%')).toBeInTheDocument()
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('blog.example.com:443')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看节点 Tokyo Edge' })).toHaveAttribute('href', '/nodes/nd_001')
    expect(screen.getByRole('link', { name: '查看目标 Blog' })).toHaveAttribute('href', '/targets/tg_001')
    expect(screen.getByRole('link', { name: '查看全部异常节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('link', { name: '查看全部异常目标' })).toHaveAttribute('href', '/targets')

    expect(screen.getByRole('heading', { name: '系统入口' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /管理服务器、agent 接入、维护与暂停/ })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('link', { name: /管理观测目标、ProbeItem 与运行状态/ })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: /查看异常开始、升级、恢复与维护历史/ })).toHaveAttribute('href', '/events')
    expect(screen.getByRole('link', { name: /进入通知、阈值、频率与保留策略配置/ })).toHaveAttribute('href', '/settings')
    expect(screen.getByRole('link', { name: /管理服务器、agent 接入、维护与暂停/ })).toHaveTextContent('待接入 2 · 暂停 1 · 退役 1')
    expect(screen.getByRole('link', { name: /管理观测目标、ProbeItem 与运行状态/ })).toHaveTextContent('暂停 1 · 归档 1 · 异常 1')
    expect(screen.getByText('通知通道 1/2 已配置：Telegram · Telegram runtime 生效')).toBeInTheDocument()

    expect(screen.getByRole('heading', { name: '按 Group 分布' })).toBeInTheDocument()
    expect(screen.getByText('production')).toBeInTheDocument()
    expect(screen.getByText('未分组')).toBeInTheDocument()
    const groupSection = screen.getByRole('heading', { name: '按 Group 分布' }).closest('section')
    expect(groupSection).toHaveTextContent('节点 3 · 目标 2')
    expect(groupSection).toHaveTextContent('节点 1 · 目标 1')

    expect(screen.getAllByText('HTTPS 探测连续失败').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByRole('link', { name: '查看全部事件' })).toHaveAttribute('href', '/events')
  })

  it('renders abnormal-but-not-severe fleet state', async () => {
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
    expect(screen.getByText('Osaka Edge')).toBeInTheDocument()
  })

  it('renders normal fleet state with an empty attention queue', async () => {
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
    expect(screen.getByRole('link', { name: '查看节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('heading', { name: '当前没有活跃异常' })).toBeInTheDocument()
    expect(screen.getByText(/处理队列为空/)).toBeInTheDocument()
    expect(screen.getByText('CPU 使用率恢复')).toBeInTheDocument()
  })

  it('renders first-run onboarding workbench when no nodes or targets exist', async () => {
    renderWithDashboard(
      baseOverview({
        total_node_count: 0,
        total_target_count: 0,
        group_summaries: [],
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '开始接入第一台服务器' })).toBeInTheDocument(),
    )

    expect(
      screen.getByText('候风还没有节点与目标。先创建节点并接入 agent，再创建观测目标与 ProbeItem。'),
    ).toBeInTheDocument()
    const fleetActions = screen.getByLabelText('首页主要入口')
    expect(within(fleetActions).getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('heading', { name: '首次接入工作台' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '创建节点' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '接入 agent' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '创建目标' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '添加 ProbeItem' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '当前需要处理' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '暂无 Group 分布' })).toBeInTheDocument()
  })

  it('renders an explicit error state when the dashboard request fails', async () => {
    renderWithDashboard({ error: 'dashboard unavailable' }, 503)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '首页不可用' })).toBeInTheDocument(),
    )

    expect(screen.getByText('Fleet State')).toBeInTheDocument()
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
  })

  it('navigates to node and target detail pages from the unified attention queue rows', async () => {
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

    const nodeRow = screen.getByText('Singapore Edge').closest('tr')
    expect(nodeRow).not.toBeNull()
    fireEvent.click(nodeRow as HTMLElement)
    expect(navigateMock).toHaveBeenCalledWith('/nodes/nd_077')

    const targetRow = screen.getByText('Payments API').closest('tr')
    expect(targetRow).not.toBeNull()
    fireEvent.click(targetRow as HTMLElement)
    expect(navigateMock).toHaveBeenCalledWith('/targets/tg_555')
  })

  it('does not trigger row navigation when an attention queue action link is clicked', async () => {
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

    fireEvent.click(screen.getByRole('link', { name: '查看节点 Taipei Edge' }))
    expect(navigateMock).not.toHaveBeenCalled()
  })
})
