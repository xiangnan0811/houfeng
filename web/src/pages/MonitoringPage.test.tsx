import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getSettings, listMonitoringInstanceRuntimeSummaries, listMonitoringInstanceSparklines } from '../lib/api'
import { MonitoringPage } from './MonitoringPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    getSettings: vi.fn().mockResolvedValue({
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 12,
        sweep_interval_seconds: 5,
        cpu_warning_pct: 80,
        cpu_alert_pct: 90,
        cpu_critical_pct: 95,
        mem_warning_pct: 85,
        mem_alert_pct: 92,
        mem_critical_pct: 95,
        disk_warning_pct: 85,
        disk_alert_pct: 92,
        disk_critical_pct: 97,
        inode_warning_pct: 80,
        inode_alert_pct: 90,
        inode_critical_pct: 95,
        iowait_warning_pct: 20,
        iowait_critical_pct: 50,
        load5_warning: 4,
        load5_critical: 8,
      },
    }),
    listMonitoringInstanceSparklines: vi.fn().mockResolvedValue({ monitoring_instances: {} }),
    listMonitoringInstanceRuntimeSummaries: vi.fn().mockResolvedValue({
      read_at: '2026-04-26T09:00:00Z',
      monitoring_instances: {},
    }),
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

function monitoringInstanceRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-26T09:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  }
}

function LocationProbe() {
  const location = useLocation()
  return (
    <span data-testid="location" data-state={JSON.stringify(location.state)}>
      {location.pathname}{location.search}
    </span>
  )
}

function HistoryBack() {
  const navigate = useNavigate()
  return <button type="button" onClick={() => navigate(-1)}>history-back</button>
}

function getMonitoringHeaderVPSLink() {
  return screen.getByRole('link', { name: '从未关联 VPS 接入' })
}

function renderMonitoring(initialEntries: string[] | Array<{ pathname: string; search?: string; state?: unknown }> = ['/monitoring']) {
  return render(
    <MemoryRouter initialEntries={initialEntries as never}>
      <HistoryBack />
      <Routes>
        <Route path="/monitoring" element={<MonitoringPage />} />
        <Route path="/monitoring/compare" element={<div>compare</div>} />
        <Route path="/monitoring/:monitoringInstanceId" element={<><div>monitoring detail</div><LocationProbe /></>} />
        <Route path="/vps" element={<><div>vps inventory</div><LocationProbe /></>} />
      </Routes>
    </MemoryRouter>,
  )
}

function listFetch(records: unknown[] | ((path: string) => unknown[])) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (init?.method === 'POST' && path === '/api/monitoring-instances/batch') {
      return Promise.resolve(mockJSONResponse({ results: [{ monitoring_instance_id: 'mi_001', ok: true }] }))
    }
    if (init?.method === 'POST' && /\/api\/monitoring-instances\/[^/]+\/actions$/.test(path)) {
      return Promise.resolve(mockJSONResponse({ action_id: 'act_001', command_id: 'uptime', status: 'pending' }))
    }
    if (typeof records === 'function') {
      return Promise.resolve(mockJSONResponse(records(path)))
    }
    if (path === '/api/monitoring-instances' || path.startsWith('/api/monitoring-instances?')) {
      return Promise.resolve(mockJSONResponse(records))
    }
    if (path === '/api/monitoring-instances/runtime-summaries') {
      return Promise.resolve(mockJSONResponse({
        read_at: '2026-04-26T09:00:00Z',
        monitoring_instances: {},
      }))
    }
    return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
  })
}

describe('MonitoringPage', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    window.sessionStorage.clear()
  })

  it('routes the monitoring page onboarding CTA to VPS inventory without opening a standalone create form', async () => {
    vi.stubGlobal('fetch', listFetch([]))
    renderMonitoring()
    await waitFor(() => expect(getMonitoringHeaderVPSLink()).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '监控' })).toBeInTheDocument()
    expect(getMonitoringHeaderVPSLink()).toHaveAttribute('href', '/vps?view=unlinked')
    expect(screen.queryByRole('button', { name: '高级创建' })).not.toBeInTheDocument()
    fireEvent.click(getMonitoringHeaderVPSLink())
    await waitFor(() => expect(screen.getByText('vps inventory')).toBeInTheDocument())
    expect(screen.getByTestId('location')).toHaveTextContent('/vps?view=unlinked')
  })

  it('routes the empty monitoring list action to VPS inventory', async () => {
    vi.stubGlobal('fetch', listFetch([]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('尚无观测事实')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '选择未关联 VPS' }))
    await waitFor(() => expect(screen.getByText('vps inventory')).toBeInTheDocument())
  })

  it('ignores archived scope URLs and still fetches the active list', async () => {
    const fetchMock = listFetch([])
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring(['/monitoring?scope=archived'])
    await waitFor(() => expect(screen.getByText('尚无观测事实')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances', expect.anything())
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes('scope='))).toBe(false)
    expect(screen.queryByRole('button', { name: '已归档' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '当前' })).not.toBeInTheDocument()
  })

  it('retries a failed list load from the error surface', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'boom' }, 500))
      .mockResolvedValue(mockJSONResponse([monitoringInstanceRecord()]))
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('监控实例列表不可用')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
  })

  it('surfaces trend and settings failures without silent defaults', async () => {
    vi.mocked(listMonitoringInstanceSparklines).mockRejectedValueOnce(new Error('spark down'))
    vi.mocked(getSettings).mockRejectedValueOnce(new Error('settings down'))
    vi.stubGlobal('fetch', listFetch([monitoringInstanceRecord({ last_heartbeat_at: '2026-04-26T09:00:00Z' })]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(await screen.findByText(/24小时历史趋势不可用/)).toBeInTheDocument()
    expect(screen.getAllByText(/新鲜度策略不可用/).length).toBeGreaterThan(0)
    expect(screen.queryByText('心跳时间未超阈值')).not.toBeInTheDocument()
    expect(screen.queryByText('在线')).not.toBeInTheDocument()
    expect(screen.queryByText('失联')).not.toBeInTheDocument()
  })

  it('keeps unknown health out of the abnormal quick view', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ current_health_status: '告警' }),
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_alert',
        display_name: 'Alerting Edge',
        current_health_status: '告警',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('tab', { name: /^异常/ }))
    await waitFor(() => expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument())
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()
  })

  it('shows unknown health instead of green when there is no heartbeat, with pause independent', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        last_heartbeat_at: undefined,
        current_health_status: '正常',
        monitoring_status: '暂停',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('未知')).toBeInTheDocument()
    expect(within(row!).getAllByText('未收到心跳')).toHaveLength(1)
    expect(row!.querySelector('.monitoring-table__id')).toBeNull()
    expect(row!.querySelector('.monitoring-table__location')).toBeNull()
    expect(row!.querySelector('.monitoring-table__labels')).toBeNull()
    expect(within(row!).queryByText('详情')).not.toBeInTheDocument()
    expect(within(row!).getByText('暂停')).toHaveClass('badge', 'badge--state', 'tone--offline')
  })

  it('does not stack 正常 onto a paused or maintaining row', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        display_name: 'Paused Edge',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
        current_health_status: '正常',
        monitoring_status: '暂停',
      }),
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_maint',
        display_name: 'Maint Edge',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
        current_health_status: '正常',
        monitoring_status: '维护中',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Paused Edge')).toBeInTheDocument())
    const paused = screen.getByText('Paused Edge').closest('tr')!
    const maintaining = screen.getByText('Maint Edge').closest('tr')!
    expect(within(paused).getByText('暂停')).toHaveClass('badge', 'badge--state', 'tone--offline')
    expect(within(paused).queryByText('正常')).not.toBeInTheDocument()
    expect(within(maintaining).getByText('维护中')).toHaveClass('badge', 'badge--state', 'tone--maintenance')
    expect(within(maintaining).queryByText('正常')).not.toBeInTheDocument()
  })

  it('hides zero incident counts and empty explanations on healthy rows', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        last_heartbeat_at: '2026-04-26T09:00:00Z',
        current_health_status: '正常',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(within(row!).queryByText('正常')).not.toBeInTheDocument()
    expect(row!.querySelector('.monitoring-table__health-quiet')).toHaveTextContent('—')
    expect(row!.querySelector('.monitoring-table__issue-count')).toBeNull()
    expect(within(row!).queryByText('无活跃问题摘要')).not.toBeInTheDocument()
  })

  it('persists quick view, search and sort in the URL and preserves unrelated params', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge', current_health_status: '告警', last_heartbeat_at: '2026-04-26T09:00:00Z' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge', region: 'ap-northeast-2', city: 'Seoul' }),
    ]))
    render(
      <MemoryRouter initialEntries={['/monitoring?from=dashboard']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByRole('tab', { selected: true })).toHaveAttribute('aria-controls', screen.getByRole('tabpanel').id)
    fireEvent.click(screen.getByRole('tab', { name: /^异常/ }))
    expect(screen.getByRole('tab', { selected: true })).toHaveAttribute('aria-controls', screen.getByRole('tabpanel').id)
    expect(screen.getByRole('tabpanel')).toHaveAccessibleName(/^异常/)
    fireEvent.change(screen.getByLabelText('搜索监控实例'), { target: { value: 'Tokyo' } })
    fireEvent.click(screen.getByRole('button', { name: /监控实例/ }))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('from=dashboard'))
    expect(screen.getByTestId('location')).toHaveTextContent('view=abnormal')
    expect(screen.getByTestId('location')).toHaveTextContent('q=Tokyo')
    expect(screen.getByTestId('location')).toHaveTextContent('sort=identity')
  })

  it('applies lifecycle filters immediately through the shared filter bar', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ display_name: 'Tokyo Edge', lifecycle_status: '在用' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_pending', display_name: 'Seoul Edge', lifecycle_status: '待接入' }),
    ]))
    render(
      <MemoryRouter initialEntries={['/monitoring?from=dashboard']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('接入阶段'), { target: { value: '在用' } })
    await waitFor(() => expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument())
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('lifecycle=')
    expect(screen.getByTestId('location')).toHaveTextContent('from=dashboard')
    expect(screen.queryByRole('button', { name: '应用筛选' })).not.toBeInTheDocument()
  })
  it('supports visible filter bar with separate health and run status filters and no disclosure', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_tokyo',
        display_name: 'Tokyo Edge',
        current_health_status: '告警',
        monitoring_status: '启用',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
        group: 'prod',
      }),
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_seoul',
        display_name: 'Seoul Edge',
        current_health_status: '正常',
        monitoring_status: '暂停',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
        group: 'staging',
      }),
    ]))
    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()

    // All filter controls visible at all times
    expect(screen.getByLabelText('健康')).toBeInTheDocument()
    expect(screen.getByLabelText('运行')).toBeInTheDocument()
    expect(screen.getByLabelText('分组')).toBeInTheDocument()
    expect(screen.getByLabelText('地区')).toBeInTheDocument()
    expect(screen.getByLabelText('城市')).toBeInTheDocument()
    expect(screen.getByLabelText('供应商')).toBeInTheDocument()
    expect(screen.getByLabelText('接入阶段')).toBeInTheDocument()
    expect(screen.getByText('标签')).toBeInTheDocument()
    expect(screen.getByLabelText('搜索监控实例')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '批量操作' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /更多筛选/ })).not.toBeInTheDocument()

    // Health filter: select 告警
    fireEvent.change(screen.getByLabelText('健康'), { target: { value: '告警' } })
    await waitFor(() => expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument())
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('health=')
    expect(screen.getByText('健康状态: 告警')).toBeInTheDocument()

    // Clear health filter, select run_status 启用
    fireEvent.change(screen.getByLabelText('健康'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('运行'), { target: { value: '启用' } })
    await waitFor(() => expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument())
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('run_status=')
    expect(screen.getByText('运行状态: 启用')).toBeInTheDocument()

    // Switch run status to 暂停
    fireEvent.change(screen.getByLabelText('运行'), { target: { value: '暂停' } })
    await waitFor(() => expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('run_status=')
    expect(screen.getByText('运行状态: 暂停')).toBeInTheDocument()

    // Change group directly without disclosure
    fireEvent.change(screen.getByLabelText('分组'), { target: { value: 'staging' } })
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('group=staging')

    // Removable chips: remove group chip
    fireEvent.click(screen.getByRole('button', { name: '移除筛选 分组: staging' }))
    expect(screen.getByTestId('location')).not.toHaveTextContent('group=staging')
  })

  it('does not drop unrelated params when clearing filters', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord(),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge', lifecycle_status: '待接入' }),
    ]))
    render(
      <MemoryRouter initialEntries={['/monitoring?from=dashboard&lifecycle=在用']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '清空所有' }))
    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())
    expect(screen.getByTestId('location')).toHaveTextContent('from=dashboard')
    expect(screen.getByTestId('location')).not.toHaveTextContent('lifecycle=')
  })

  it('uses onboarding=pending from Dashboard deep links to filter monitoring', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ lifecycle_status: '待接入', binding_status: '已绑定', display_name: 'Pending Lifecycle' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_unbound', display_name: 'Unbound Edge', binding_status: '未绑定' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_ready', display_name: 'Healthy Edge' }),
    ]))
    renderMonitoring(['/monitoring?onboarding=pending'])
    await waitFor(() => expect(screen.getByText('Pending Lifecycle')).toBeInTheDocument())
    expect(screen.getByText('Unbound Edge')).toBeInTheDocument()
    expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument()
  })

  it('keeps batch runtime actions behind row selection and refreshes afterwards', async () => {
    const fetchMock = listFetch([monitoringInstanceRecord()])
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '批量操作' })).toBeDisabled()
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '进入维护' }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([url, init]) => String(url) === '/api/monitoring-instances/batch' && init?.method === 'POST')).toBe(true)
      expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/monitoring-instances').length).toBeGreaterThan(1)
    })
  })

  it('styles COMMAND_LIST picks on the list batch command dialog and posts uptime', async () => {
    const fetchMock = listFetch([monitoringInstanceRecord()])
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '执行命令…' }))

    const dialog = await screen.findByRole('dialog', { name: '批量执行命令' })
    expect(screen.queryByLabelText('命令 ID')).not.toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/whoami/i)).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /df -h/ })).toBeInTheDocument()
    const uptime = within(dialog).getByRole('button', { name: /uptime/ })
    expect(uptime).toHaveClass('monitoring-detail-commands__item')


    fireEvent.click(uptime)
    await waitFor(() => {
      const actionCall = fetchMock.mock.calls.find(([url, init]) =>
        String(url) === '/api/monitoring-instances/mi_001/actions' && init?.method === 'POST',
      )
      expect(actionCall).toBeTruthy()
      expect(String(actionCall?.[1]?.body)).toContain('"command_id":"uptime"')
    })
  })

  it('prunes hidden selections when filters change and supports header select-all', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge', lifecycle_status: '在用' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge', lifecycle_status: '待接入' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    expect((screen.getByLabelText('全选可见监控实例') as HTMLInputElement).indeterminate).toBe(true)
    fireEvent.click(screen.getByLabelText('选择 Seoul Edge'))
    const selectAll = screen.getByLabelText('全选可见监控实例') as HTMLInputElement
    expect(selectAll.checked).toBe(true)
    expect(selectAll.indeterminate).toBe(false)
    fireEvent.change(screen.getByLabelText('接入阶段'), { target: { value: '在用' } })
    await waitFor(() => expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument())
    expect(screen.getByLabelText('选择 Tokyo Edge')).toBeChecked()
    expect((screen.getByLabelText('全选可见监控实例') as HTMLInputElement).checked).toBe(true)
    expect(screen.getByRole('button', { name: '批量操作' })).toHaveTextContent('(1)')
  })

  it('offers compare only for exactly two selected rows in the batch menu', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_003', display_name: 'Osaka Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.queryByRole('menuitem', { name: '对比' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('选择 Seoul Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.getByRole('menuitem', { name: '对比' })).toHaveAttribute('href', '/monitoring/compare?id=mi_001&id=mi_002')
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('选择 Osaka Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.queryByRole('menuitem', { name: '对比' })).not.toBeInTheDocument()
  })
  it('keeps URL-backed selection through remount/detail return and prunes only after filtering', async () => {
    const records = [
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_003', display_name: 'Osaka Edge', lifecycle_status: '待接入' }),
    ]
    vi.stubGlobal('fetch', listFetch(records))
    const firstRender = render(
      <MemoryRouter initialEntries={['/monitoring?from=dashboard&return_vps=vps_001']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
          <Route path="/monitoring/:monitoringInstanceId" element={<><div>monitoring detail</div><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    fireEvent.click(screen.getByLabelText('选择 Seoul Edge'))
    fireEvent.click(screen.getByLabelText('选择 Osaka Edge'))
    await waitFor(() => {
      const href = screen.getByTestId('location').textContent ?? ''
      expect(href).toContain('return_vps=vps_001')
      expect(href).toContain('selected=mi_001')
      expect(href).toContain('selected=mi_002')
      expect(href).toContain('selected=mi_003')
    })
    const selectedHref = screen.getByTestId('location').textContent ?? ''
    firstRender.unmount()

    renderMonitoring([selectedHref])
    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())
    expect(screen.getByLabelText('选择 Tokyo Edge')).toBeChecked()
    expect(screen.getByLabelText('选择 Seoul Edge')).toBeChecked()
    expect(screen.getByLabelText('选择 Osaka Edge')).toBeChecked()

    fireEvent.click(screen.getByRole('link', { name: 'Tokyo Edge' }))
    await waitFor(() => expect(screen.getByText('monitoring detail')).toBeInTheDocument())
    expect(screen.getByTestId('location')).toHaveAttribute(
      'data-state',
      JSON.stringify({ monitoringListHref: selectedHref }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'history-back' }))
    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())
    expect(screen.getByLabelText('选择 Tokyo Edge')).toBeChecked()
    expect(screen.getByLabelText('选择 Seoul Edge')).toBeChecked()
    expect(screen.getByLabelText('选择 Osaka Edge')).toBeChecked()

    fireEvent.change(screen.getByLabelText('接入阶段'), { target: { value: '在用' } })
    await waitFor(() => expect(screen.queryByText('Osaka Edge')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '批量操作' })).toHaveTextContent('(2)')
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.getByRole('menuitem', { name: '对比' })).toHaveAttribute(
      'href',
      '/monitoring/compare?id=mi_001&id=mi_002',
    )
  })
  it('retains URL selection while the first list snapshot is pending', async () => {
    let resolveList: ((response: Response) => void) | undefined
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/monitoring-instances' && init?.method !== 'POST') {
        return new Promise<Response>((resolve) => {
          resolveList = resolve
        })
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    }))
    render(
      <MemoryRouter initialEntries={['/monitoring?return_vps=vps_001&selected=mi_001&selected=mi_002']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByTestId('location')).toHaveTextContent('selected=mi_001')
    expect(screen.getByTestId('location')).toHaveTextContent('selected=mi_002')
    expect(resolveList).toBeDefined()

    resolveList!(mockJSONResponse([monitoringInstanceRecord()]))
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('selected=mi_001')
      expect(screen.getByTestId('location')).not.toHaveTextContent('selected=mi_002')
    })
  })

  it('prunes hidden batch targets from a retained snapshot after refresh fails', async () => {
    let listCalls = 0
    const fallback = listFetch([
      monitoringInstanceRecord(),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge' }),
    ])
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/monitoring-instances' && ++listCalls > 1) {
        return Promise.resolve(mockJSONResponse({ error: 'refresh failed' }, 503))
      }
      return fallback(input, init)
    }))
    render(
      <MemoryRouter initialEntries={['/monitoring?selected=mi_001&selected=mi_002']}>
        <Routes>
          <Route path="/monitoring" element={<><MonitoringPage /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(screen.getByText(/列表刷新失败/)).toBeInTheDocument())
    fireEvent.change(screen.getByRole('searchbox', { name: '搜索监控实例' }), { target: { value: 'Tokyo Edge' } })
    await waitFor(() => expect(screen.getByTestId('location')).not.toHaveTextContent('selected=mi_002'))
    expect(screen.getByTestId('location')).toHaveTextContent('selected=mi_001')
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.queryByRole('menuitem', { name: '对比' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('menuitem', { name: '暂停监控' }))
    expect(screen.getByRole('alertdialog')).toHaveTextContent('1')
  })

  it('toggles the batch menu closed on a second click, Escape, and outside click', async () => {
    vi.stubGlobal('fetch', listFetch([monitoringInstanceRecord()]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('选择 Tokyo Edge'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('freezes batch targets while pause confirmation is open', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_active', display_name: 'Active Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_other', display_name: 'Other Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Active Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('全选可见监控实例'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '暂停监控' }))
    expect(screen.getByRole('button', { name: '确认批量暂停监控' })).toBeInTheDocument()
    expect(screen.getByLabelText('接入阶段')).toBeDisabled()
    expect(screen.getByLabelText('搜索监控实例')).toBeDisabled()
  })

  it('navigates to detail with validated list return state from the name link', async () => {
    vi.stubGlobal('fetch', listFetch([monitoringInstanceRecord()]))
    render(
      <MemoryRouter initialEntries={['/monitoring?q=Tokyo']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<><div>monitoring detail</div><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('link', { name: 'Tokyo Edge' }))
    await waitFor(() => expect(screen.getByText('monitoring detail')).toBeInTheDocument())
    expect(screen.getByTestId('location')).toHaveAttribute(
      'data-state',
      JSON.stringify({ monitoringListHref: '/monitoring?q=Tokyo' }),
    )
  })

  it('does not request or render monitoring instance asset context in the list', async () => {
    const fetchMock = listFetch([monitoringInstanceRecord({ display_name: 'Asset Edge' })])
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Asset Edge')).toBeInTheDocument())
    expect(screen.queryByRole('columnheader', { name: '资产上下文' })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/asset-context/monitoring-instances', expect.anything())
  })

  it('renders labeled 24h trend tracks when sparkline buckets are present', async () => {
    const make24 = (base: number) => Array.from({ length: 24 }, () => base)
    vi.mocked(listMonitoringInstanceSparklines).mockResolvedValueOnce({
      monitoring_instances: {
        mi_001: {
          cpu_usage_pct: make24(50),
          mem_used_pct: make24(70),
          disk_used_pct: make24(55),
        },
      },
    })
    vi.stubGlobal('fetch', listFetch([monitoringInstanceRecord()]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByRole('columnheader', { name: '24h 资源趋势' })).toBeInTheDocument()
    expect(document.querySelectorAll('.monitoring-table__trend-item')).toHaveLength(3)
    expect(document.querySelectorAll('polyline').length).toBe(3)
  })

  it('keeps binding conflict copy without exposing binding actions', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_conflict',
        binding_status: '指纹变更待确认',
        current_health_status: '关注',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getAllByText('等待绑定确认').length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: '确认重绑定' })).not.toBeInTheDocument()
  })

  it('does not show zero summary counts until a scoped list succeeds', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'unavailable' }, 503))
      .mockResolvedValueOnce(mockJSONResponse([monitoringInstanceRecord()]))
      .mockResolvedValue(mockJSONResponse({ error: 'refresh failed' }, 503))
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('监控实例列表不可用')).toBeInTheDocument())
    const quickViews = screen.getByRole('tablist', { name: '关注视图' })
    expect(within(quickViews).getByRole('tab', { name: '全部' })).not.toHaveTextContent('0')
    expect(within(quickViews).getByRole('tab', { name: '全部' })).not.toHaveTextContent('1')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(within(quickViews).getByRole('tab', { name: /全部/ })).toHaveTextContent('1')
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(screen.getByText(/列表刷新失败/)).toBeInTheDocument())
    expect(within(quickViews).getByRole('tab', { name: /全部/ })).toHaveTextContent('1')
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
  })

  it('freezes relative heartbeat age to the list snapshot across rerenders', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-04-26T09:00:50Z'))
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ last_heartbeat_at: '2026-04-26T09:00:00Z' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(within(row!).getByText('50 秒前')).toBeInTheDocument()
    expect(within(row!).queryByText('数据陈旧')).not.toBeInTheDocument()
    vi.setSystemTime(new Date('2026-04-26T09:02:00Z'))
    fireEvent.change(screen.getByLabelText('接入阶段'), { target: { value: '' } })
    expect(within(row!).getByText('50 秒前')).toBeInTheDocument()
    expect(within(row!).queryByText('数据陈旧')).not.toBeInTheDocument()
    expect(within(row!).queryByText('心跳时间未超阈值')).not.toBeInTheDocument()
  })

  it('keeps frozen batch confirmation count when an inflight refresh changes the list', async () => {
    let listCalls = 0
    let resolveRefresh: ((value: Response) => void) | undefined
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (init?.method === 'POST' && path === '/api/monitoring-instances/batch') {
        return Promise.resolve(mockJSONResponse({ results: [{ monitoring_instance_id: 'mi_001', ok: true }] }))
      }
      if (path === '/api/monitoring-instances' || path.startsWith('/api/monitoring-instances?')) {
        listCalls += 1
        if (listCalls === 1) {
          return Promise.resolve(mockJSONResponse([
            monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
            monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge' }),
          ]))
        }
        return new Promise<Response>((resolve) => {
          resolveRefresh = resolve
        })
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    fireEvent.click(screen.getByLabelText('全选可见监控实例'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '暂停监控' }))
    expect(screen.getByText('将对确认时选中的 2 个监控实例执行暂停操作。')).toBeInTheDocument()
    expect(resolveRefresh).toBeDefined()
    resolveRefresh!(mockJSONResponse([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
    ]))
    await waitFor(() => expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument())
    expect(screen.getByText('将对确认时选中的 2 个监控实例执行暂停操作。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '刷新' })).toBeDisabled()
  })

  it('marks stale heartbeat with the notice badge', async () => {
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ last_heartbeat_at: '2026-04-26T09:00:00Z' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    const stale = screen.getByText('数据陈旧')
    expect(stale).toHaveClass('badge', 'badge--state', 'tone--notice')
  })

  it('ignores stale column width storage keys and keeps checkbox fixed at 40px and trend auto', async () => {
    window.localStorage.setItem('houfeng.table-col-widths.monitoring-list', JSON.stringify([220, 168, 176, 240, 44]))
    window.localStorage.setItem('houfeng.table-col-widths.monitoring-list-v2', JSON.stringify([220, 168, 176, 240, 44]))
    window.localStorage.setItem('houfeng.table-col-widths.monitoring-data-cols-v3', JSON.stringify([220, 168, 176]))
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const table = screen.getByRole('table')
    const cols = table.querySelectorAll('colgroup col')
    expect(cols).toHaveLength(7)
    expect(cols[0]!).toHaveAttribute('width', '40')
    expect(cols[1]!).toHaveAttribute('width', '180')
    expect(cols[2]!).toHaveAttribute('width', '150')
    expect(cols[3]!).toHaveAttribute('width', '160')
    expect(cols[4]!).toHaveAttribute('width', '100')
    expect(cols[5]!).toHaveAttribute('width', '140')
    expect(cols[6]!).not.toHaveAttribute('width')

    const headers = screen.getAllByRole('columnheader')
    expect(headers[0]!.querySelector('.col-resize')).toBeNull()
    expect(headers[1]!.querySelector('.col-resize')).not.toBeNull()
    expect(headers[2]!.querySelector('.col-resize')).not.toBeNull()
    expect(headers[3]!.querySelector('.col-resize')).not.toBeNull()
    expect(headers[4]!.querySelector('.col-resize')).not.toBeNull()
    expect(headers[5]!.querySelector('.col-resize')).not.toBeNull()
    expect(headers[6]!.querySelector('.col-resize')).toBeNull()
  })

  it('renders sampled uptime and upload/download network rates with true zero preserved', async () => {
    vi.mocked(listMonitoringInstanceRuntimeSummaries).mockResolvedValueOnce({
      read_at: '2026-04-26T09:01:00Z',
      monitoring_instances: {
        mi_001: {
          observed_at: '2026-04-26T09:00:50Z',
          received_at: '2026-04-26T09:00:51Z',
          uptime_seconds: 3600,
          net_in_bytes_per_sec: 1048576,
          net_out_bytes_per_sec: 0,
        },
      },
    })
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    expect(screen.getByRole('columnheader', { name: '运行时长' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '网络速率' })).toBeInTheDocument()
    expect(screen.getByText('1小时 0分钟')).toBeInTheDocument()
    expect(screen.getByText('0 B/s')).toBeInTheDocument()
    expect(screen.getByText('1.0 MB/s')).toBeInTheDocument()
    expect(screen.getByLabelText('上行')).toBeInTheDocument()
    expect(screen.getByLabelText('下行')).toBeInTheDocument()
    expect(screen.getByText(/采样/)).toBeInTheDocument()
  })

  it('renders em-dash for missing or invalid rates without clientside clock increments', async () => {
    vi.mocked(listMonitoringInstanceRuntimeSummaries).mockResolvedValueOnce({
      read_at: '2026-04-26T09:01:00Z',
      monitoring_instances: {
        mi_001: {
          observed_at: '2026-04-26T09:00:50Z',
          received_at: '2026-04-26T09:00:51Z',
          uptime_seconds: -1,
          net_in_bytes_per_sec: null,
          net_out_bytes_per_sec: null,
        },
        mi_002: null,
      },
    })
    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_002', display_name: 'Seoul Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()

    const dashes = screen.getAllByText('—')
    expect(dashes.length).toBeGreaterThanOrEqual(4)
  })

  it('retains prior labeled snapshot when runtime summaries refresh fails and supports local retry', async () => {
    vi.mocked(listMonitoringInstanceRuntimeSummaries)
      .mockResolvedValueOnce({
        read_at: '2026-04-26T09:01:00Z',
        monitoring_instances: {
          mi_001: {
            observed_at: '2026-04-26T09:00:50Z',
            received_at: '2026-04-26T09:00:51Z',
            uptime_seconds: 7200,
            net_in_bytes_per_sec: 2048,
            net_out_bytes_per_sec: 4096,
          },
        },
      })
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValueOnce({
        read_at: '2026-04-26T09:05:00Z',
        monitoring_instances: {
          mi_001: {
            observed_at: '2026-04-26T09:04:50Z',
            received_at: '2026-04-26T09:04:51Z',
            uptime_seconds: 7440,
            net_in_bytes_per_sec: 8192,
            net_out_bytes_per_sec: 16384,
          },
        },
      })

    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({ monitoring_instance_id: 'mi_001', display_name: 'Tokyo Edge' }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('2小时 0分钟')).toBeInTheDocument())
    expect(screen.getByText('2.0 KB/s')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '刷新' }))

    await waitFor(() => {
      expect(screen.getByText(/运行时长与网络速率刷新失败，仍显示上次读取结果/)).toBeInTheDocument()
    })
    expect(screen.getByText('2小时 0分钟')).toBeInTheDocument()
    expect(screen.getByText('2.0 KB/s')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重试运行指标' }))

    await waitFor(() => {
      expect(screen.getByText('2小时 4分钟')).toBeInTheDocument()
    })
    expect(screen.getByText('8.0 KB/s')).toBeInTheDocument()
    expect(screen.queryByText(/运行时长与网络速率刷新失败/)).not.toBeInTheDocument()
  })

  it('anchors sample age strictly to summary read_at clock when independent retry advances while list stays fixed, and handles invalid read_at honestly', async () => {
    vi.mocked(listMonitoringInstanceRuntimeSummaries)
      .mockResolvedValueOnce({
        read_at: '2026-04-26T09:00:15Z',
        monitoring_instances: {
          mi_001: {
            observed_at: '2026-04-26T09:00:00Z',
            received_at: '2026-04-26T09:00:01Z',
            uptime_seconds: 3600,
            net_in_bytes_per_sec: 1024,
            net_out_bytes_per_sec: 2048,
          },
        },
      })
      .mockRejectedValueOnce(new Error('transient error'))
      .mockResolvedValueOnce({
        // Independent retry advances summary read_at to 09:05:00 while list snapshot stays at 09:00:00
        // observed_at (09:04:40) is in the future compared to list snapshot (09:00:00)
        // but is 20s prior to summary read_at (09:05:00)
        read_at: '2026-04-26T09:05:00Z',
        monitoring_instances: {
          mi_001: {
            observed_at: '2026-04-26T09:04:40Z',
            received_at: '2026-04-26T09:04:41Z',
            uptime_seconds: 3900,
            net_in_bytes_per_sec: 2048,
            net_out_bytes_per_sec: 4096,
          },
        },
      })
      .mockResolvedValueOnce({
        read_at: 'invalid-read-at',
        monitoring_instances: {
          mi_001: {
            observed_at: '2026-04-26T09:04:40Z',
            received_at: '2026-04-26T09:04:41Z',
            uptime_seconds: 3900,
            net_in_bytes_per_sec: 2048,
            net_out_bytes_per_sec: 4096,
          },
        },
      })

    vi.stubGlobal('fetch', listFetch([
      monitoringInstanceRecord({
        monitoring_instance_id: 'mi_001',
        display_name: 'Tokyo Edge',
        last_heartbeat_at: '2026-04-26T09:00:00Z',
      }),
    ]))
    renderMonitoring()
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('15 秒前')).toBeInTheDocument()

    // Trigger refreshAll to trigger transient error on summaries
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => {
      expect(screen.getByText(/运行时长与网络速率刷新失败/)).toBeInTheDocument()
    })

    // Local retry only reloads runtime summaries, advancing summary read_at clock to 09:05:00 while list snapshot remains 09:00:00
    fireEvent.click(screen.getByRole('button', { name: '重试运行指标' }))
    await waitFor(() => {
      // Must anchor to summary read_at (09:05:00 - 09:04:40 = 20s), not list snapshot
      expect(screen.getByText('20 秒前')).toBeInTheDocument()
    })

    // Next retry with invalid read_at keeps missing/invalid read_at honest as em-dash
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => {
      const sampleContainer = screen.getByText('1小时 5分钟').closest('.monitoring-table__uptime')
      expect(sampleContainer).not.toBeNull()
      expect(sampleContainer?.querySelector('.monitoring-table__uptime-sample')?.textContent).toContain('采样 —')
    })
  })
})
