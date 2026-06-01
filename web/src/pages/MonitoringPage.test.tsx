import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { listMonitoringInstanceSparklines } from '../lib/api'
import { MonitoringPage } from './MonitoringPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
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

function deferredResponse() {
  let resolve!: (response: Response) => void
  let reject!: (error?: unknown) => void
  const promise = new Promise<Response>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
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

    await waitFor(() => expect(getMonitoringHeaderVPSLink()).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: '监控' })).toBeInTheDocument()
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
    expect(fetchMock).toHaveBeenCalledTimes(1)

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

  it('shows a clear onboarding workspace path for an existing monitoringInstance', async () => {
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

    await waitFor(() =>
      expect(screen.getByRole('link', { name: '接入 agent' })).toBeInTheDocument(),
    )

    expect(screen.getByRole('link', { name: '接入 agent' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001?onboarding=1',
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
    expect(within(conflictRow!).getByText('指纹变更待确认')).toBeInTheDocument()
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

  it('renders runtime quick actions by monitoringInstance monitoring status and applies light actions immediately', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            monitoring_instance_id: 'mi_enabled',
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
          },
          {
            monitoring_instance_id: 'mi_paused',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '在用',
            monitoring_status: '暂停',
            binding_status: '已绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-26T09:00:00Z',
            updated_at: '2026-04-26T09:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_enabled',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '维护中',
          binding_status: '已绑定',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:10:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_paused',
          display_name: 'Seoul Edge',
          region: 'ap-northeast-2',
          city: 'Seoul',
          provider: 'Hetzner',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:12:00Z',
        }),
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

    const enabledRow = screen.getByText('Tokyo Edge').closest('tr')
    const pausedRow = screen.getByText('Seoul Edge').closest('tr')
    expect(enabledRow).not.toBeNull()
    expect(pausedRow).not.toBeNull()

    expect(within(enabledRow!).getByRole('button', { name: '进入维护' })).toBeInTheDocument()
    expect(within(enabledRow!).getByRole('button', { name: '暂停监控' })).toBeInTheDocument()
    expect(within(pausedRow!).queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
    expect(within(pausedRow!).getByRole('button', { name: '恢复监控' })).toBeInTheDocument()

    fireEvent.click(within(enabledRow!).getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(within(enabledRow!).getByRole('button', { name: '退出维护' })).toBeInTheDocument(),
    )

    fireEvent.click(within(pausedRow!).getByRole('button', { name: '恢复监控' }))

    await waitFor(() =>
      expect(within(pausedRow!).getByRole('button', { name: '进入维护' })).toBeInTheDocument(),
    )

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_enabled/runtime/enter-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances/mi_paused/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('uses an inline stateful confirmation before pausing monitoringInstance monitoring from an enabled row', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
            monitoring_status: '暂停',
          }),
        ),
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

    const pauseTrigger = screen.getByRole('button', { name: '暂停监控' })
    fireEvent.click(pauseTrigger)

    const confirmation = screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })
    expect(confirmation).toBeInTheDocument()
    expect(confirmation).toHaveFocus()
    expect(screen.getByText('当前：监控运行状态为启用。')).toBeInTheDocument()
    expect(screen.getByText('操作后：监控运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText('会停止主机指标采集，并停止该监控实例承担的探针执行。趋势图会从此开始出现数据空档。'),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 agent 绑定关系。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('heading', { name: '确认暂停监控实例监控' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: '暂停监控' })).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复监控' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows the maintenance current-state copy before pausing a maintenance row', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_maint',
          display_name: 'Osaka Edge',
          monitoring_status: '维护中',
          binding_status: '已绑定',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：监控运行状态为维护中。')).toBeInTheDocument()
    expect(screen.queryByText('当前：监控运行状态为启用。')).not.toBeInTheDocument()
    expect(confirmMock).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps the pause confirmation and local error visible when pause fails', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
          }),
        ]),
      )
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid runtime transition' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByText(/invalid runtime transition/)).toBeInTheDocument(),
    )
    expect(screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '监控' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })


  it('edits row labels without changing the existing note and cancels locally', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            labels: ['edge'],
            note: 'keep me',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            labels: ['edge', 'core'],
            note: 'keep me',
          }),
        ),
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
    const tokyoRow = screen.getByText('Tokyo Edge').closest('tr')
    expect(tokyoRow).not.toBeNull()
    expect(within(tokyoRow!).getByText('edge')).toBeInTheDocument()

    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '快速编辑标签' }))
    const editor = within(tokyoRow!).getByRole('textbox', { name: '标签' })
    fireEvent.change(editor, { target: { value: 'edge, core, edge' } })
    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '取消' }))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: '保存标签' })).not.toBeInTheDocument()

    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(tokyoRow!).getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core, edge' },
    })
    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '保存标签' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001', {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'If-Match': '"2026-04-26T09:00:00Z"',
        },
        cache: 'no-store',
      credentials: 'include',
        body: JSON.stringify({ labels: ['edge', 'core'], note: 'keep me' }),
      }),
    )
    expect(within(tokyoRow!).getByText('edge · core')).toBeInTheDocument()
  })


  it('preserves saved metadata when a later runtime response returns stale labels and note', async () => {
    const metadataSave = deferredResponse()
    const runtimeUpdate = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            monitoring_status: '启用',
            labels: ['edge'],
            note: 'keep me',
            updated_at: '2026-04-26T09:00:00Z',
          }),
        ]),
      )
      .mockImplementationOnce(() => metadataSave.promise)
      .mockImplementationOnce(() => runtimeUpdate.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '保存标签' }))

    fireEvent.click(within(row!).getByRole('button', { name: '进入维护' }))

    metadataSave.resolve(
      mockJSONResponse(
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Edge',
          monitoring_status: '启用',
          labels: ['edge', 'core'],
          note: 'keep me',
          updated_at: '2026-04-26T09:01:00Z',
        }),
      ),
    )

    await waitFor(() => expect(within(row!).getByText('edge · core')).toBeInTheDocument())

    runtimeUpdate.resolve(
      mockJSONResponse(
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Edge',
          monitoring_status: '维护中',
          labels: ['edge'],
          note: 'stale note',
          updated_at: '2026-04-26T09:02:00Z',
        }),
      ),
    )

    await waitFor(() =>
      expect(within(row!).getByRole('button', { name: '退出维护' })).toBeInTheDocument(),
    )
    expect(within(row!).getByText('edge · core')).toBeInTheDocument()
    expect(within(row!).queryByText(/^edge$/)).not.toBeInTheDocument()
  })

  it('preserves newer runtime fields when a stale metadata save resolves later', async () => {
    const metadataSave = deferredResponse()
    const runtimeUpdate = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            monitoring_status: '启用',
            labels: ['edge'],
            note: 'keep me',
            updated_at: '2026-04-26T09:00:00Z',
          }),
        ]),
      )
      .mockImplementationOnce(() => metadataSave.promise)
      .mockImplementationOnce(() => runtimeUpdate.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '保存标签' }))

    fireEvent.click(within(row!).getByRole('button', { name: '进入维护' }))

    runtimeUpdate.resolve(
      mockJSONResponse(
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Edge',
          monitoring_status: '维护中',
          labels: ['edge'],
          note: 'keep me',
          updated_at: '2026-04-26T09:02:00Z',
        }),
      ),
    )

    await waitFor(() =>
      expect(within(row!).getByRole('button', { name: '退出维护' })).toBeInTheDocument(),
    )

    metadataSave.resolve(
      mockJSONResponse(
        monitoringInstanceRecord({
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Edge',
          monitoring_status: '启用',
          labels: ['edge', 'core'],
          note: 'keep me',
          updated_at: '2026-04-26T09:01:00Z',
        }),
      ),
    )

    await waitFor(() => expect(within(row!).getByText('edge · core')).toBeInTheDocument())
    expect(within(row!).getByRole('button', { name: '退出维护' })).toBeInTheDocument()
    expect(within(row!).queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
  })

  it('blocks opening another row editor during metadata save and exposes row errors as alerts', async () => {
    const rowASave = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            labels: ['edge'],
            note: 'keep me',
          }),
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            labels: ['seoul'],
            note: 'other note',
          }),
        ]),
      )
      .mockImplementationOnce(() => rowASave.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring']}>
        <Routes>
          <Route path="/monitoring" element={<MonitoringPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const tokyoRow = screen.getByText('Tokyo Edge').closest('tr')
    const seoulRow = screen.getByText('Seoul Edge').closest('tr')
    expect(tokyoRow).not.toBeNull()
    expect(seoulRow).not.toBeNull()

    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(tokyoRow!).getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.click(within(tokyoRow!).getByRole('button', { name: '保存标签' }))

    expect(within(tokyoRow!).getByRole('button', { name: '正在保存…' })).toBeDisabled()
    expect(within(seoulRow!).getByRole('button', { name: '快速编辑标签' })).toBeDisabled()

    rowASave.resolve(mockJSONResponse({ error: 'metadata write failed' }, 409))

    await waitFor(() =>
      expect(within(tokyoRow!).getByRole('alert')).toHaveTextContent('metadata write failed'),
    )
    expect(within(seoulRow!).queryByRole('textbox', { name: '标签' })).not.toBeInTheDocument()
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

  it('does not navigate when a row action button is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse([
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_actions',
              display_name: 'Tokyo Edge',
              labels: ['edge'],
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

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    // Inline editor opened on the same row, navigation must NOT have triggered.
    expect(within(row!).getByRole('textbox', { name: '标签' })).toBeInTheDocument()
    expect(screen.queryByText('monitoring detail')).not.toBeInTheDocument()
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
    const trendItems = trendCells[0].querySelectorAll('.monitoring-table__trend-item')
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
    const placeholder = trendCells[0].querySelector('.monitoring-table__trends-empty')
    expect(placeholder).not.toBeNull()
    expect(placeholder!.textContent).toBe('—')
  })




  it('renders heartbeat and sync timestamps inside the identity column freshness row', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            last_heartbeat_at: '2026-04-26T09:00:00Z',
            last_sync_at: '2026-04-26T08:55:00Z',
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

    const freshnessEl = document.querySelector('.monitoring-table__freshness')
    expect(freshnessEl).not.toBeNull()
    // Should contain "心跳" label and timestamp content
    expect(freshnessEl!.textContent).toMatch(/心跳/)
    // Should contain "同步" label when last_sync_at is present
    expect(freshnessEl!.textContent).toMatch(/同步/)
  })

})
