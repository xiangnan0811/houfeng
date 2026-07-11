import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { listMonitoringInstanceSparklines } from '../lib/api'
import { MonitoringPage } from './MonitoringPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    getSettings: vi.fn().mockResolvedValue({
      incident_defaults: {
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

function getMonitoringHeaderVPSLink() {
  return screen.getByRole('link', { name: '从 VPS 接入 agent' })
}

describe('MonitoringPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.sessionStorage.clear()
  })

  it('routes the monitoring page onboarding CTA to VPS inventory without opening a standalone create form', async () => {
    vi.stubGlobal('IntersectionObserver', vi.fn())
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances') return Promise.resolve(mockJSONResponse([]))
      if (path === '/api/asset-context/monitoring-instances') return Promise.resolve(mockJSONResponse([]))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/vps" element={<div>vps inventory</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(getMonitoringHeaderVPSLink()).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: '监控' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '资产判断支撑' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '资产组合决策' })).not.toBeInTheDocument()
    expect(screen.queryByText('资产上下文')).not.toBeInTheDocument()
    expect(screen.getByText('观察 agent 接入后的监控实例、心跳、主机性能与运行控制。')).toBeInTheDocument()
    expect(getMonitoringHeaderVPSLink()).toHaveAttribute('href', '/vps')
    expect(screen.queryByRole('button', { name: '高级创建' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '高级创建监控实例表单' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('显示名称')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '创建并接入' })).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).not.toHaveBeenCalledWith('/api/asset-context/monitoring-instances', expect.anything())

    fireEvent.click(getMonitoringHeaderVPSLink())

    await waitFor(() => expect(screen.getByText('vps inventory')).toBeInTheDocument())
  })

  it('routes the empty monitoring list action to VPS inventory', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/vps" element={<div>vps inventory</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('尚无观测事实')).toBeInTheDocument())

    expect(screen.getByRole('button', { name: '创建第一台 VPS' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '高级创建监控实例表单' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '创建第一台 VPS' }))

    await waitFor(() => expect(screen.getByText('vps inventory')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('routes an existing pending onboarding monitoring instance to detail for onboarding work', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          {
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            region: 'ap-northeast-1',
            city: 'Tokyo',
            provider: 'Vultr',
            lifecycle_status: '待接入',
            monitoring_status: '启用',
            binding_status: '未绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-26T09:00:00Z',
            updated_at: '2026-04-26T09:00:00Z',
          },
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    expect(screen.queryByRole('link', { name: '接入 agent' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001',
    )
  })

  it('surfaces and filters binding-conflict monitoring without exposing final binding actions', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_conflict',
            display_name: 'Tokyo Edge',
            binding_status: '指纹变更待确认',
            current_health_status: '关注',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_normal',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const conflictRow = screen.getByText('Tokyo Edge').closest('tr')
    expect(conflictRow).not.toBeNull()
    expect(within(conflictRow!).queryByText('指纹变更待确认')).not.toBeInTheDocument()
    expect(within(conflictRow!).getByText('等待绑定确认')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认重绑定' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '拒绝新指纹' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重置绑定' })).not.toBeInTheDocument()
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
  })

  it('renders an empty state when no binding-conflict monitoring exist', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_normal',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
  })

  it('keeps monitoring rows scan-focused without row-level operations', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_enabled',
          display_name: 'Tokyo Edge',
          monitoring_status: '启用',
          labels: ['edge'],
          last_heartbeat_at: '2026-04-26T09:00:00Z',
          last_sync_at: '2026-04-26T08:55:00Z',
        }),
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_paused',
          display_name: 'Seoul Edge',
          region: 'ap-northeast-2',
          city: 'Seoul',
          provider: 'Hetzner',
          monitoring_status: '暂停',
          labels: ['seoul'],
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<div>monitoring detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const tokyoRow = screen.getByText('Tokyo Edge').closest('tr')
    const seoulRow = screen.getByText('Seoul Edge').closest('tr')
    expect(tokyoRow).not.toBeNull()
    expect(seoulRow).not.toBeNull()

    expect(screen.queryByRole('columnheader', { name: '操作' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '快速编辑标签' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '接入 agent' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '暂停监控' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '恢复监控' })).not.toBeInTheDocument()

    expect(within(tokyoRow!).getByText('edge')).toBeInTheDocument()
    expect(within(seoulRow!).getByText('seoul')).toBeInTheDocument()
    expect(within(tokyoRow!).queryByText('同步')).not.toBeInTheDocument()
    expect(within(tokyoRow!).queryByText('暂停')).not.toBeInTheDocument()
    expect(document.querySelector('col[width="28"]')).toBeInTheDocument()
    expect(document.querySelector('col[style]')).not.toBeInTheDocument()

    fireEvent.click(tokyoRow!)
    await waitFor(() => expect(screen.getByText('monitoring detail')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps batch runtime actions available behind explicit selection', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ results: [{ monitoring_instance_id: 'mi_001', ok: true }] }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('全选 (1)'))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/batch', {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ monitoring_instance_ids: ['mi_001'], action: 'enter-maintenance' }),
      }),
    )
  })

  it('switches monitoring instance list scope between active archived and all', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances') {
        return Promise.resolve(
          mockJSONResponse([
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_active',
              display_name: 'Active Edge',
            }),
          ]),
        )
      }
      if (path === '/api/monitoring-instances?scope=archived') {
        return Promise.resolve(
          mockJSONResponse([
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_archived',
              display_name: 'Archived Edge',
              lifecycle_status: '已退役',
              monitoring_status: '暂停',
              archived_at: '2026-06-10T09:00:00Z',
              archived_reason: '重复创建',
            }),
          ]),
        )
      }
      if (path === '/api/monitoring-instances?scope=all') {
        return Promise.resolve(
          mockJSONResponse([
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_active',
              display_name: 'Active Edge',
            }),
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_archived',
              display_name: 'Archived Edge',
              lifecycle_status: '已退役',
              monitoring_status: '暂停',
              archived_at: '2026-06-10T09:00:00Z',
              archived_reason: '重复创建',
            }),
          ]),
        )
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Active Edge')).toBeInTheDocument())
    expect(screen.queryByText('Archived Edge')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '已归档' }))
    await waitFor(() => expect(screen.getByText('Archived Edge')).toBeInTheDocument())
    expect(screen.queryByText('Active Edge')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    await waitFor(() => expect(screen.getByText('Active Edge')).toBeInTheDocument())
    expect(screen.getByText('Archived Edge')).toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances?scope=archived', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances?scope=all', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('excludes archived monitoring instances from batch runtime actions', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_active',
            display_name: 'Active Edge',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_archived',
            display_name: 'Archived Edge',
            lifecycle_status: '已退役',
            monitoring_status: '暂停',
            archived_at: '2026-06-10T09:00:00Z',
            archived_reason: '重复创建',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ results: [{ monitoring_instance_id: 'mi_active', ok: true }] }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring?scope=all']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Archived Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('全选 (1)'))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/monitoring-instances/batch', {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ monitoring_instance_ids: ['mi_active'], action: 'enter-maintenance' }),
      }),
    )
  })

  it('does not leave batch actions submitting when filters remove every eligible monitoring instance', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_active',
          display_name: 'Active Edge',
        }),
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_archived',
          display_name: 'Archived Edge',
          lifecycle_status: '已退役',
          monitoring_status: '暂停',
          archived_at: '2026-06-10T09:00:00Z',
          archived_reason: '重复创建',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring?scope=all']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Active Edge')).toBeInTheDocument())
    expect(screen.getByText('Archived Edge')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('全选 (1)'))
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))
    fireEvent.change(screen.getByLabelText('命令 ID'), { target: { value: 'uptime' } })

    const lifecycleSelect = screen.getByDisplayValue('接入阶段: 全部')
    fireEvent.change(lifecycleSelect, { target: { value: '已退役' } })
    await waitFor(() => expect(screen.queryByText('Active Edge')).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '下发命令' }))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('批量操作中…')).not.toBeInTheDocument()

    fireEvent.change(lifecycleSelect, { target: { value: '' } })
    await waitFor(() => expect(screen.getByText('Active Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByLabelText('全选 (1)'))
    expect(screen.getByRole('button', { name: '进入维护' })).toBeEnabled()
  })

  it('does not request or render monitoring instance asset context in the list', async () => {
    vi.stubGlobal('IntersectionObserver', vi.fn())
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances') {
        return Promise.resolve(
          mockJSONResponse([
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_asset',
              display_name: 'Asset Edge',
            }),
          ]),
        )
      }
      if (path === '/api/asset-context/monitoring-instances') {
        return Promise.resolve(mockJSONResponse({ error: 'monitoring list must not request asset context' }, 500))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Asset Edge')).toBeInTheDocument())

    const row = screen.getByText('Asset Edge').closest('tr')
    expect(row).not.toBeNull()
    expect(screen.queryByRole('columnheader', { name: '资产上下文' })).not.toBeInTheDocument()
    expect(within(row!).queryByText('未关联')).not.toBeInTheDocument()
    expect(within(row!).queryByText('已过期')).not.toBeInTheDocument()
    expect(within(row!).queryByText('Tokyo VPS')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/asset-context/monitoring-instances', expect.anything())
  })

  it('filters the list by lifecycle via the inline filter select', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_active',
            display_name: 'Tokyo Edge',
            lifecycle_status: '在用',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_pending',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            lifecycle_status: '待接入',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()

    const lifecycleSelect = screen.getByDisplayValue('接入阶段: 全部')
    fireEvent.change(lifecycleSelect, { target: { value: '在用' } })

    await waitFor(() =>
      expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
  })

  it('filters the list by unified Chinese monitoring status values', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_running',
            display_name: 'Running Edge',
            monitoring_status: '启用',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_paused',
            display_name: 'Paused Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            monitoring_status: '暂停',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Running Edge')).toBeInTheDocument())
    expect(screen.getByText('Paused Edge')).toBeInTheDocument()

    const runStatusSelect = screen.getByDisplayValue('运行状态: 全部')
    expect(within(runStatusSelect).getByRole('option', { name: '启用' })).toHaveValue('启用')
    expect(within(runStatusSelect).getByRole('option', { name: '暂停' })).toHaveValue('暂停')
    expect(within(runStatusSelect).getByRole('option', { name: '维护中' })).toHaveValue('维护中')
    expect(within(runStatusSelect).queryByRole('option', { name: 'paused' })).not.toBeInTheDocument()

    fireEvent.change(runStatusSelect, { target: { value: '暂停' } })

    await waitFor(() =>
      expect(screen.queryByText('Running Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Paused Edge')).toBeInTheDocument()
  })

  it('uses onboarding=pending from Dashboard deep links to filter monitoring', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_pending_lifecycle',
            display_name: 'Pending Lifecycle',
            lifecycle_status: '待接入',
            binding_status: '已绑定',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_unbound',
            display_name: 'Unbound Edge',
            lifecycle_status: '在用',
            binding_status: '未绑定',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_ready',
            display_name: 'Healthy Edge',
            lifecycle_status: '在用',
            binding_status: '已绑定',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring?onboarding=pending']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Pending Lifecycle')).toBeInTheDocument())
    expect(screen.getByText('Unbound Edge')).toBeInTheDocument()
    expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument()
  })

  it('filters abnormal monitoring via the inline checkbox', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_normal',
            display_name: 'Healthy Edge',
            current_health_status: '正常',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_alert',
            display_name: 'Alerting Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            current_health_status: '告警',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()

    const checkbox = screen.getByLabelText('仅看异常')
    fireEvent.click(checkbox)

    await waitFor(() =>
      expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()
  })

  it('navigates to the monitoring detail page when a row is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_click',
            display_name: 'Tokyo Edge',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<div>monitoring detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(row!)
    await waitFor(() => expect(screen.getByText('monitoring detail')).toBeInTheDocument())
  })

  it('does not expose the standalone monitoringInstance create drawer from the page chrome', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(getMonitoringHeaderVPSLink()).toBeInTheDocument())

    expect(getMonitoringHeaderVPSLink()).toHaveAttribute('href', '/vps')
    expect(screen.queryByRole('button', { name: '高级创建' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '高级创建监控实例表单' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('显示名称')).not.toBeInTheDocument()
  })

  it('renders three mini sparklines per row when sparklines data is loaded', async () => {
    const make24 = (base: number, jitter: number) =>
      Array.from({ length: 24 }, (_, i) => base + Math.sin(i * 0.5) * jitter)
    const mocked = vi.mocked(listMonitoringInstanceSparklines)
    mocked.mockResolvedValueOnce({
      monitoring_instances: {
        mi_001: {
          cpu_usage_pct: make24(50, 15),
          mem_used_pct: make24(70, 8),
          disk_used_pct: make24(55, 5),
        },
        mi_002: {
          cpu_usage_pct: make24(30, 10),
          mem_used_pct: make24(60, 6),
          disk_used_pct: make24(45, 4),
        },
      },
    })

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())

    // Each Sparkline with >1 point renders a <polyline> element
    const polylines = document.querySelectorAll('polyline')
    // 2 monitoring x 3 metrics = 6 polylines
    expect(polylines.length).toBe(6)

    // Each row should have the trend column visible
    const trendCells = document.querySelectorAll('.monitoring-table__trends')
    expect(trendCells.length).toBe(2)

    // Trend strip should have 3 trend items per row
    const firstTrendCell = trendCells[0]
    if (!firstTrendCell) throw new Error('monitoring table must render the first trend cell')
    const trendItems = firstTrendCell.querySelectorAll('.monitoring-table__trend-item')
    expect(trendItems.length).toBe(3)
  })

  it('shows placeholder dash in trends column when sparklines data is missing for a monitoringInstance', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_no_data',
            display_name: 'New Monitoring Instance',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('New Monitoring Instance')).toBeInTheDocument())

    // Default mock for listMonitoringInstanceSparklines returns { monitoring_instances: {} }, so mi_no_data won't have data
    const trendCells = document.querySelectorAll('.monitoring-table__trends')
    expect(trendCells.length).toBe(1)
    // The placeholder should be rendered
    const firstTrendCell = trendCells[0]
    if (!firstTrendCell) throw new Error('monitoring table must render the trend cell')
    const placeholder = firstTrendCell.querySelector('.monitoring-table__trends-empty')
    expect(placeholder).not.toBeNull()
    if (!placeholder) throw new Error('missing trend data must render a placeholder')
    expect(placeholder.textContent).toBe('—')
  })

  it('shows heartbeat and missing-heartbeat problems in the current issue column, not under identity', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_healthy',
            display_name: 'Tokyo Edge',
            last_heartbeat_at: '2026-04-26T09:00:00Z',
            last_sync_at: '2026-04-26T08:55:00Z',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_missing',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            last_heartbeat_at: undefined,
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_issue',
            display_name: 'Osaka Edge',
            region: 'ap-northeast-3',
            city: 'Osaka',
            last_heartbeat_at: '2026-04-26T09:10:00Z',
            current_primary_issue_summary: 'CPU 使用率过高',
            current_active_incident_count: 2,
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const healthyRow = screen.getByText('Tokyo Edge').closest('tr')
    const missingRow = screen.getByText('Seoul Edge').closest('tr')
    const issueRow = screen.getByText('Osaka Edge').closest('tr')
    expect(healthyRow).not.toBeNull()
    expect(missingRow).not.toBeNull()
    expect(issueRow).not.toBeNull()

    expect(healthyRow!.querySelector('.monitoring-table__freshness')).toBeNull()
    expect(within(healthyRow!).getByText(/心跳/)).toBeInTheDocument()
    expect(within(healthyRow!).queryByText(/同步/)).not.toBeInTheDocument()
    expect(within(missingRow!).getByText('未收到心跳')).toBeInTheDocument()
    expect(within(issueRow!).getByText('CPU 使用率过高')).toBeInTheDocument()
    expect(within(issueRow!).getByText(/心跳/)).toBeInTheDocument()
  })

})
