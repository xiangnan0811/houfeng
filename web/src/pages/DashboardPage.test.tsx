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

function expectSummaryCard(label: string, value: string) {
  const card = screen.getByText(label).closest('.summary-card')
  expect(card).not.toBeNull()
  expect(within(card as HTMLElement).getByText(value)).toBeInTheDocument()
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    navigateMock.mockReset()
  })

  it('renders loading then aggregate overview counts and recent events from /api/dashboard', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          total_node_count: 5,
          total_target_count: 4,
          abnormal_node_count: 2,
          abnormal_target_count: 3,
          severe_node_count: 1,
          severe_target_count: 2,
          maintenance_node_count: 1,
          maintenance_target_count: 0,
          recent_new_incident_count: 4,
          recent_recovery_count: 1,
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
              current_health_status: '严重',
              last_success_at: '2026-04-25T07:50:00Z',
              last_failure_at: '2026-04-25T08:09:00Z',
              current_active_incident_count: 1,
              current_primary_issue_summary: 'HTTPS 探测连续失败',
            },
          ],
        }),
      ),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    expect(screen.getByText('正在加载首页 / Dashboard…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '首页 / Dashboard' })).toBeInTheDocument(),
    )

    expect(screen.getByText('当前风险总览')).toBeInTheDocument()
    expect(screen.getByText('先处理当前异常，再查看趋势与事件历史。')).toBeInTheDocument()
    expect(screen.getByText('风险对象')).toBeInTheDocument()
    expectSummaryCard('风险对象', '5')
    expectSummaryCard('严重对象总数', '3')
    expectSummaryCard('维护对象总数', '1')
    expectSummaryCard('新增异常', '4')
    expectSummaryCard('恢复事件', '1')
    expect(screen.getAllByText('节点').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('异常节点概览')).toBeInTheDocument()
    expect(screen.getAllByText('目标').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('异常目标概览')).toBeInTheDocument()
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率 92.0%')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看节点 Tokyo Edge' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('blog.example.com:443')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看目标 Blog' })).toHaveAttribute(
      'href',
      '/targets/tg_001',
    )
    expect(screen.getAllByText('最近事件').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('HTTPS 探测连续失败').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('tg_001').length).toBeGreaterThanOrEqual(1)
    // gap #20-Dashboard: AbnormalNodeList must surface node_id via Hostname
    expect(screen.getAllByText('nd_001').length).toBeGreaterThanOrEqual(1)
  })

  it('renders an explicit error state when the dashboard request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'dashboard unavailable' }, 503)),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '首页不可用' })).toBeInTheDocument(),
    )
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
  })

  it('renders the V1 first-run onboarding path when no nodes or targets exist', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          total_node_count: 0,
          total_target_count: 0,
          abnormal_node_count: 0,
          abnormal_target_count: 0,
          severe_node_count: 0,
          severe_target_count: 0,
          maintenance_node_count: 0,
          maintenance_target_count: 0,
          recent_new_incident_count: 0,
          recent_recovery_count: 0,
          recent_events: [],
        }),
      ),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '还没有节点与目标' })).toBeInTheDocument(),
    )

    expect(screen.getByText('首次接入')).toBeInTheDocument()
    expect(
      screen.getByText('这不是异常。候风需要先有一个节点接入 agent，然后才能创建目标并添加 ProbeItem。'),
    ).toBeInTheDocument()
    expect(screen.getAllByText('创建第一个节点')).toHaveLength(2)
    expect(screen.getByText('接入 agent')).toBeInTheDocument()
    expect(screen.getByText('创建第一个目标')).toBeInTheDocument()
    expect(screen.getByText('添加第一个 ProbeItem')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '创建第一个节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.queryByText('异常对象总数')).not.toBeInTheDocument()
  })

  it('navigates to the node detail page when an abnormal node row is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          total_node_count: 5,
          total_target_count: 4,
          abnormal_node_count: 1,
          abnormal_target_count: 0,
          severe_node_count: 0,
          severe_target_count: 0,
          maintenance_node_count: 0,
          maintenance_target_count: 0,
          recent_new_incident_count: 0,
          recent_recovery_count: 0,
          recent_events: [],
          abnormal_nodes: [
            {
              node_id: 'nd_042',
              display_name: 'Osaka Edge',
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
          abnormal_targets: [],
        }),
      ),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())

    const row = screen.getByText('Osaka Edge').closest('tr')
    expect(row).not.toBeNull()
    fireEvent.click(row as HTMLElement)
    expect(navigateMock).toHaveBeenCalledWith('/nodes/nd_042')
  })

  it('does not navigate when the row action link is clicked (stopPropagation)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          total_node_count: 5,
          total_target_count: 4,
          abnormal_node_count: 1,
          abnormal_target_count: 0,
          severe_node_count: 0,
          severe_target_count: 0,
          maintenance_node_count: 0,
          maintenance_target_count: 0,
          recent_new_incident_count: 0,
          recent_recovery_count: 0,
          recent_events: [],
          abnormal_nodes: [
            {
              node_id: 'nd_077',
              display_name: 'Singapore Edge',
              region: 'ap-southeast-1',
              city: 'Singapore',
              provider: 'aws',
              lifecycle_status: '在用',
              monitoring_status: '启用',
              current_health_status: '严重',
              last_heartbeat_at: '2026-04-25T08:05:00Z',
              current_active_incident_count: 3,
              current_primary_issue_summary: '磁盘使用率 99%',
            },
          ],
          abnormal_targets: [],
        }),
      ),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Singapore Edge')).toBeInTheDocument())

    const link = screen.getByRole('link', { name: '查看节点 Singapore Edge' })
    fireEvent.click(link)
    // stopPropagation: row's onClick must not fire when the action link is clicked
    expect(navigateMock).not.toHaveBeenCalled()
  })

  it('navigates to the target detail page when an abnormal target row is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          total_node_count: 5,
          total_target_count: 4,
          abnormal_node_count: 0,
          abnormal_target_count: 1,
          severe_node_count: 0,
          severe_target_count: 0,
          maintenance_node_count: 0,
          maintenance_target_count: 0,
          recent_new_incident_count: 0,
          recent_recovery_count: 0,
          recent_events: [],
          abnormal_nodes: [],
          abnormal_targets: [
            {
              target_id: 'tg_555',
              name: 'Payments API',
              target_type: 'service',
              host: 'pay.example.com',
              base_port: 443,
              run_status: '启用',
              current_health_status: '严重',
              last_success_at: '2026-04-25T07:50:00Z',
              last_failure_at: '2026-04-25T08:09:00Z',
              current_active_incident_count: 2,
              current_primary_issue_summary: 'TLS 探测连续失败',
            },
          ],
        }),
      ),
    )

    render(
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Payments API')).toBeInTheDocument())

    const row = screen.getByText('Payments API').closest('tr')
    expect(row).not.toBeNull()
    fireEvent.click(row as HTMLElement)
    expect(navigateMock).toHaveBeenCalledWith('/targets/tg_555')
  })
})
