import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getOnboardingTokenCache } from '../lib/onboardingTokenCache'
import { listNodeSparklines } from '../lib/api'
import { NodeOnboardingPage } from './NodeOnboardingPage'
import { NodesPage } from './NodesPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    listNodeSparklines: vi.fn().mockResolvedValue({ nodes: {} }),
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

function mockTextResponse(body: string, status = 200) {
  return new Response(body, { status })
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

function nodeRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    node_id: 'nd_001',
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
  return <output aria-label="location">{location.pathname + location.search}</output>
}

describe('NodesPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.sessionStorage.clear()
  })

  it('creates a node, issues an onboarding token, caches it, and navigates to onboarding', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
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
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          token: 'enroll_tokyo_001',
          issued_at: '2026-04-26T09:05:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId/onboarding" element={<div>onboarding workspace</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建节点' })).toBeInTheDocument(),
    )

    expect(screen.getByRole('heading', { name: '节点观测' })).toBeInTheDocument()
    expect(screen.getByText('观测 · 节点')).toBeInTheDocument()
    expect(
      screen.getByText('以运行事实支撑 VPS 资产判断，优先扫描 VPS 关联、接入状态、维护 / 暂停与异常证据。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '资产判断支撑' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /全部节点/ })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    expect(screen.getByText('节点创建')).toBeInTheDocument()
    expect(screen.queryByLabelText('生命周期状态')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'Tokyo Edge' } })
    fireEvent.change(screen.getByLabelText('地区'), { target: { value: 'ap-northeast-1' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Tokyo' } })
    fireEvent.change(screen.getByLabelText('供应商'), { target: { value: 'Vultr' } })

    fireEvent.click(screen.getByRole('button', { name: '创建并生成 Token' }))

    await waitFor(() => expect(screen.getByText('onboarding workspace')).toBeInTheDocument())

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        display_name: 'Tokyo Edge',
        group: '',
        region: 'ap-northeast-1',
        city: 'Tokyo',
        provider: 'Vultr',
        labels: [],
        note: '',
        lifecycle_status: '待接入',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/enrollment-token', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(getOnboardingTokenCache('nd_001')).toEqual({
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
  })

  it('lands on onboarding with a recoverable error state when token issuance fails after create succeeds', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
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
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ error: 'token service unavailable' }, 503),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
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
          phase: '未开始接入',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId/onboarding" element={<NodeOnboardingPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建节点' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'Tokyo Edge' } })
    fireEvent.change(screen.getByLabelText('地区'), { target: { value: 'ap-northeast-1' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Tokyo' } })
    fireEvent.change(screen.getByLabelText('供应商'), { target: { value: 'Vultr' } })

    fireEvent.click(screen.getByRole('button', { name: '创建并生成 Token' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('接入 Token 生成失败：token service unavailable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新生成接入 Token' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/nodes/nd_001/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(getOnboardingTokenCache('nd_001')).toBeNull()
  })

  it('keeps create errors local to the page', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse({ error: 'display name already exists' }, 409)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId/onboarding" element={<div>onboarding workspace</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建节点' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'Tokyo Edge' } })
    fireEvent.change(screen.getByLabelText('地区'), { target: { value: 'ap-northeast-1' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Tokyo' } })
    fireEvent.change(screen.getByLabelText('供应商'), { target: { value: 'Vultr' } })

    fireEvent.click(screen.getByRole('button', { name: '创建并生成 Token' }))

    await waitFor(() =>
      expect(screen.getByText('display name already exists')).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '节点观测' })).toBeInTheDocument()
    expect(screen.queryByText('onboarding workspace')).not.toBeInTheDocument()
    expect(getOnboardingTokenCache('nd_001')).toBeNull()
  })

  it('surfaces the shared API fallback message when node creation fails without an error body', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockTextResponse('', 500)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId/onboarding" element={<div>onboarding workspace</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建节点' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'Tokyo Edge' } })
    fireEvent.change(screen.getByLabelText('地区'), { target: { value: 'ap-northeast-1' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Tokyo' } })
    fireEvent.change(screen.getByLabelText('供应商'), { target: { value: 'Vultr' } })

    fireEvent.click(screen.getByRole('button', { name: '创建并生成 Token' }))

    await waitFor(() => expect(screen.getByText('Request failed: 500')).toBeInTheDocument())
    expect(screen.queryByText('请求失败：状态码 500')).not.toBeInTheDocument()
    expect(screen.queryByText('onboarding workspace')).not.toBeInTheDocument()
  })

  it('shows a clear onboarding workspace path for an existing node', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          {
            node_id: 'nd_001',
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
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('link', { name: '接入工作台' })).toBeInTheDocument(),
    )

    expect(screen.getByRole('link', { name: '接入工作台' })).toHaveAttribute(
      'href',
      '/nodes/nd_001/onboarding',
    )
  })

  it('surfaces and filters binding-conflict nodes without exposing final binding actions', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_conflict',
            display_name: 'Tokyo Edge',
            binding_status: '指纹变更待确认',
            current_health_status: '关注',
          }),
          nodeRecord({
            node_id: 'nd_normal',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    expect(screen.getByRole('tab', { name: /绑定异常/ })).toBeInTheDocument()
    const conflictRow = screen.getByText('Tokyo Edge').closest('tr')
    expect(conflictRow).not.toBeNull()
    expect(within(conflictRow!).getByText('指纹变更待确认')).toBeInTheDocument()
    expect(within(conflictRow!).getByText('等待绑定确认')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认重绑定' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '拒绝新指纹' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重置绑定' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /绑定异常/ }))

    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /绑定异常/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )

    fireEvent.click(screen.getByRole('tab', { name: /全部节点/ }))
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()
  })

  it('renders an empty state when the binding-conflict filter has no rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_normal',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('tab', { name: /绑定异常/ }))

    expect(screen.getByText('没有绑定异常节点')).toBeInTheDocument()
    expect(screen.getByText('当前没有等待绑定确认的节点。')).toBeInTheDocument()
    expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
  })

  it('renders runtime quick actions by node monitoring status and applies light actions immediately', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            node_id: 'nd_enabled',
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
            node_id: 'nd_paused',
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
          node_id: 'nd_enabled',
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
          node_id: 'nd_paused',
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
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_enabled/runtime/enter-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_paused/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('uses an inline stateful confirmation before pausing node monitoring from an enabled row', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
            monitoring_status: '暂停',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const pauseTrigger = screen.getByRole('button', { name: '暂停监控' })
    fireEvent.click(pauseTrigger)

    const confirmation = screen.getByRole('alertdialog', { name: '确认暂停节点监控' })
    expect(confirmation).toBeInTheDocument()
    expect(confirmation).toHaveFocus()
    expect(screen.getByText('当前：监控运行状态为启用。')).toBeInTheDocument()
    expect(screen.getByText('操作后：监控运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText('会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。'),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 agent 绑定关系。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('heading', { name: '确认暂停节点监控' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: '暂停监控' })).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复监控' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime/pause', {
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
        nodeRecord({
          node_id: 'nd_maint',
          display_name: 'Osaka Edge',
          monitoring_status: '维护中',
          binding_status: '已绑定',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
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
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            binding_status: '已绑定',
          }),
        ]),
      )
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid runtime transition' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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
    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '节点观测' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime/pause', {
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
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            labels: ['edge'],
            note: 'keep me',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            labels: ['edge', 'core'],
            note: 'keep me',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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
      expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001', {
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
          nodeRecord({
            node_id: 'nd_001',
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
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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
        nodeRecord({
          node_id: 'nd_001',
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
        nodeRecord({
          node_id: 'nd_001',
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
          nodeRecord({
            node_id: 'nd_001',
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
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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
        nodeRecord({
          node_id: 'nd_001',
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
        nodeRecord({
          node_id: 'nd_001',
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
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            labels: ['edge'],
            note: 'keep me',
          }),
          nodeRecord({
            node_id: 'nd_002',
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
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
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

  it('filters the list by lifecycle via the FilterBar select', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_active',
            display_name: 'Tokyo Edge',
            lifecycle_status: '在用',
          }),
          nodeRecord({
            node_id: 'nd_pending',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            lifecycle_status: '待接入',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('Seoul Edge')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('生命周期'), { target: { value: '在用' } })

    await waitFor(() =>
      expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByText('生命周期: 在用')).toBeInTheDocument()
  })

  it('uses onboarding=pending from Dashboard deep links and clears the URL filter', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_pending_lifecycle',
            display_name: 'Pending Lifecycle',
            lifecycle_status: '待接入',
            binding_status: '已绑定',
          }),
          nodeRecord({
            node_id: 'nd_unbound',
            display_name: 'Unbound Edge',
            lifecycle_status: '在用',
            binding_status: '未绑定',
          }),
          nodeRecord({
            node_id: 'nd_binding_conflict',
            display_name: 'Conflict Edge',
            lifecycle_status: '在用',
            binding_status: '指纹变更待确认',
          }),
          nodeRecord({
            node_id: 'nd_ready',
            display_name: 'Healthy Edge',
            lifecycle_status: '在用',
            binding_status: '已绑定',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes?onboarding=pending']}>
        <Routes>
          <Route
            path="/nodes"
            element={
              <>
                <NodesPage />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Pending Lifecycle')).toBeInTheDocument())

    expect(screen.getByText('Unbound Edge')).toBeInTheDocument()
    expect(screen.getByText('Conflict Edge')).toBeInTheDocument()
    expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '补齐 3 个接入 / 绑定状态' })).toBeInTheDocument()
    expect(screen.getByLabelText('当前证据筛选')).toHaveTextContent('待接入/绑定')
    expect(screen.getByRole('heading', { name: '优先核对：Conflict Edge' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '处理接入' })).toHaveAttribute(
      'href',
      '/nodes/nd_binding_conflict/onboarding',
    )
    expect(screen.getByRole('switch', { name: '待接入/绑定待处理' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    expect(
      screen.getByRole('button', { name: '移除筛选 待接入/绑定待处理' }),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '移除筛选 待接入/绑定待处理' }))

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())
    expect(screen.getByLabelText('location')).toHaveTextContent('/nodes')
  })

  it('toggles "仅看异常" and clears all filters via FilterBar', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_normal',
            display_name: 'Healthy Edge',
            current_health_status: '正常',
          }),
          nodeRecord({
            node_id: 'nd_alert',
            display_name: 'Alerting Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            current_health_status: '告警',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('switch', { name: '仅看异常' }))

    await waitFor(() =>
      expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '先处理 1 个异常节点' })).toBeInTheDocument()
    expect(screen.getByLabelText('当前证据筛选')).toHaveTextContent('仅看异常')
    expect(
      screen.getByRole('button', { name: '移除筛选 仅看异常' }),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空所有' }))

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()
  })

  it('surfaces observability support lanes and applies support quick filters', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_healthy',
            display_name: 'Healthy Edge',
            current_health_status: '正常',
          }),
          nodeRecord({
            node_id: 'nd_alert',
            display_name: 'Alerting Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            current_health_status: '告警',
          }),
          nodeRecord({
            node_id: 'nd_pending',
            display_name: 'Pending Edge',
            lifecycle_status: '待接入',
            binding_status: '未绑定',
          }),
          nodeRecord({
            node_id: 'nd_maint',
            display_name: 'Maintenance Edge',
            monitoring_status: '维护中',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: '资产判断支撑' })).toBeInTheDocument()
    expect(
      screen.getByText('Node 页面不是资产主体；它保留运行事实、接入状态、维护窗口和 freshness，用来判断 VPS 是否有可信观测证据。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '先处理 1 个异常节点' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '优先核对：Alerting Edge' })).toBeInTheDocument()
    expect(screen.getByText('健康状态：告警')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute(
      'href',
      '/nodes/nd_alert',
    )
    expect(screen.getByText('VPS 关联')).toBeInTheDocument()
    expect(screen.getByText('列表不推导 linked VPS health；资产关联回到 VPS 台账和 Node 详情核对。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '未关联 VPS' })).toHaveAttribute(
      'href',
      '/vps?view=unlinked',
    )
    expect(screen.getByRole('link', { name: '决策队列' })).toHaveAttribute(
      'href',
      '/asset-decisions',
    )

    fireEvent.click(screen.getAllByRole('button', { name: '仅看异常' })[0])

    await waitFor(() =>
      expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空所有' }))
    await waitFor(() => expect(screen.getByText('Pending Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '待接入/绑定' }))

    await waitFor(() =>
      expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Pending Edge')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移除筛选 待接入/绑定待处理' })).toBeInTheDocument()
  })

  it('shows a clear empty-filter lead and clears filters from the support surface', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_healthy',
            display_name: 'Healthy Edge',
            current_health_status: '正常',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes?abnormal=1']}>
        <Routes>
          <Route
            path="/nodes"
            element={
              <>
                <NodesPage />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '没有匹配当前证据条件' })).toBeInTheDocument(),
    )
    expect(screen.getByText('当前筛选没有返回节点。先清空或收窄条件，再继续判断观测证据。')).toBeInTheDocument()
    expect(screen.queryByText('Healthy Edge')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空证据筛选' }))

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())
    expect(screen.getByLabelText('location')).toHaveTextContent('/nodes')
  })

  it('renders a stable evidence lead without inventing linked VPS health', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_healthy',
            display_name: 'Healthy Edge',
            current_health_status: '正常',
            monitoring_status: '启用',
            binding_status: '已绑定',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Node 运行证据当前稳定' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '没有需要优先核对的 Node' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看 VPS 库存' })).toHaveAttribute('href', '/vps')
    expect(screen.queryByText(/linked node health/i)).not.toBeInTheDocument()
  })

  it('navigates to the node detail page when a row is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_click',
            display_name: 'Tokyo Edge',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId" element={<div>node detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(row!)
    await waitFor(() => expect(screen.getByText('node detail')).toBeInTheDocument())
  })

  it('does not navigate when a row action button is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse([
            nodeRecord({
              node_id: 'nd_actions',
              display_name: 'Tokyo Edge',
              labels: ['edge'],
            }),
          ]),
        ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:nodeId" element={<div>node detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const row = screen.getByText('Tokyo Edge').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    // Inline editor opened on the same row, navigation must NOT have triggered.
    expect(within(row!).getByRole('textbox', { name: '标签' })).toBeInTheDocument()
    expect(screen.queryByText('node detail')).not.toBeInTheDocument()
  })

  it('toggles binding-conflict view via the segmented control tabs', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_normal_seg',
            display_name: 'Normal Edge',
          }),
          nodeRecord({
            node_id: 'nd_conflict_seg',
            display_name: 'Conflict Edge',
            binding_status: '指纹变更待确认',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Normal Edge')).toBeInTheDocument())
    expect(screen.getByText('Conflict Edge')).toBeInTheDocument()

    const allTab = screen.getByRole('tab', { name: /全部节点/ })
    const conflictTab = screen.getByRole('tab', { name: /绑定异常/ })
    expect(allTab).toHaveAttribute('aria-selected', 'true')
    expect(conflictTab).toHaveAttribute('aria-selected', 'false')

    fireEvent.click(conflictTab)

    await waitFor(() =>
      expect(screen.queryByText('Normal Edge')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Conflict Edge')).toBeInTheDocument()
    expect(conflictTab).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(allTab)
    await waitFor(() => expect(screen.getByText('Normal Edge')).toBeInTheDocument())
  })

  it('toggles the create node form panel via the section heading button', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建节点' })).toBeInTheDocument(),
    )

    expect(screen.queryByText('节点创建')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    expect(screen.getByText('节点创建')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
    expect(screen.queryByText('节点创建')).not.toBeInTheDocument()
  })

  it('renders three mini sparklines per row when sparklines data is loaded', async () => {
    const make24 = (base: number, jitter: number) =>
      Array.from({ length: 24 }, (_, i) => base + Math.sin(i * 0.5) * jitter)
    const mocked = vi.mocked(listNodeSparklines)
    mocked.mockResolvedValueOnce({
      nodes: {
        nd_001: {
          cpu_usage_pct: make24(50, 15),
          mem_used_pct: make24(70, 8),
          disk_used_pct: make24(55, 5),
        },
        nd_002: {
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
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
          }),
          nodeRecord({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('Seoul Edge')).toBeInTheDocument())

    // Each Sparkline with >1 point renders a <polyline> element
    const polylines = document.querySelectorAll('polyline')
    // 2 nodes x 3 metrics = 6 polylines
    expect(polylines.length).toBe(6)

    // Each row should have the trend column visible
    const trendCells = document.querySelectorAll('.nodes-table__trends')
    expect(trendCells.length).toBe(2)

    // Trend strip should have 3 trend items per row
    const trendItems = trendCells[0].querySelectorAll('.nodes-table__trend-item')
    expect(trendItems.length).toBe(3)
  })

  it('shows placeholder dash in trends column when sparklines data is missing for a node', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_no_data',
            display_name: 'New Node',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('New Node')).toBeInTheDocument())

    // Default mock for listNodeSparklines returns { nodes: {} }, so nd_no_data won't have data
    const trendCells = document.querySelectorAll('.nodes-table__trends')
    expect(trendCells.length).toBe(1)
    // The placeholder should be rendered
    const placeholder = trendCells[0].querySelector('.nodes-table__trends-empty')
    expect(placeholder).not.toBeNull()
    expect(placeholder!.textContent).toBe('—')
  })

  it('shows the batch bar with select-all toggle when a group filter is active', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_003',
            display_name: 'Batch Node 1',
            group: 'staging',
            monitoring_status: '暂停',
          }),
          nodeRecord({
            node_id: 'nd_004',
            display_name: 'Batch Node 2',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            group: 'staging',
            monitoring_status: '暂停',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes?group=staging']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Batch Node 1')).toBeInTheDocument())

    // Batch bar should be visible when group filter is active
    const batchBarEl = document.querySelector('.batch-bar')
    expect(batchBarEl).not.toBeNull()
    expect(screen.getByText('全选 (2)')).toBeInTheDocument()

    // Click the select-all checkbox within the batch bar
    const checkbox = batchBarEl!.querySelector('input[type="checkbox"]') as HTMLInputElement
    fireEvent.click(checkbox)

    // Action buttons should appear after select-all within batch bar
    await waitFor(() => {
      const buttons = batchBarEl!.querySelectorAll('.batch-bar__actions button')
      expect(buttons.length).toBe(5)
      expect(buttons[0].textContent).toBe('进入维护')
      expect(buttons[1].textContent).toBe('退出维护')
      expect(buttons[2].textContent).toBe('暂停监控')
      expect(buttons[3].textContent).toBe('恢复监控')
      expect(buttons[4].textContent).toBe('执行命令…')
    })
  })

  it('does not show batch bar when group filter is not active', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            group: 'production',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    // Batch bar should NOT be visible without group filter
    expect(screen.queryByText(/全选/)).not.toBeInTheDocument()
  })

  it('renders heartbeat and sync timestamps inside the identity column freshness row', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          nodeRecord({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            last_heartbeat_at: '2026-04-26T09:00:00Z',
            last_sync_at: '2026-04-26T08:55:00Z',
          }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    const freshnessEl = document.querySelector('.nodes-table__freshness')
    expect(freshnessEl).not.toBeNull()
    // Should contain "心跳" label and timestamp content
    expect(freshnessEl!.textContent).toMatch(/心跳/)
    // Should contain "同步" label when last_sync_at is present
    expect(freshnessEl!.textContent).toMatch(/同步/)
  })

})
