import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getOnboardingTokenCache } from '../lib/onboardingTokenCache'
import { NodeOnboardingPage } from './NodeOnboardingPage'
import { NodesPage } from './NodesPage'

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

    expect(screen.getByRole('heading', { name: '节点列表' })).toBeInTheDocument()
    expect(screen.getAllByText('列表视图').length).toBeGreaterThanOrEqual(2)

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
    expect(screen.getByRole('heading', { name: '节点列表' })).toBeInTheDocument()
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

    expect(screen.getByRole('button', { name: '绑定异常 1' })).toBeInTheDocument()
    const conflictRow = screen.getByText('Tokyo Edge').closest('article')
    expect(conflictRow).not.toBeNull()
    expect(within(conflictRow!).getByText('指纹变更待确认')).toBeInTheDocument()
    expect(within(conflictRow!).getByText('等待绑定确认')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认重绑定' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '拒绝新指纹' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重置绑定' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '绑定异常 1' }))

    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.queryByText('Seoul Edge')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '绑定异常 1' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    fireEvent.click(screen.getByRole('button', { name: '全部节点 2' }))
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

    fireEvent.click(screen.getByRole('button', { name: '绑定异常 0' }))

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

    const enabledRow = screen.getByText('Tokyo Edge').closest('article')
    const pausedRow = screen.getByText('Seoul Edge').closest('article')
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
      expect(screen.getByText('invalid runtime transition')).toBeInTheDocument(),
    )
    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '节点列表' })).toBeInTheDocument()
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
    expect(screen.getByText('标签：edge')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '快速编辑标签' }))
    const editor = screen.getByRole('textbox', { name: '标签' })
    fireEvent.change(editor, { target: { value: 'edge, core, edge' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: '保存标签' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core, edge' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签' }))

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
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
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

    const row = screen.getAllByRole('article').find((item) =>
      within(item).queryByText('Tokyo Edge'),
    )
    expect(row).toBeDefined()

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

    await waitFor(() => expect(within(row!).getByText('标签：edge · core')).toBeInTheDocument())

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
    expect(within(row!).getByText('标签：edge · core')).toBeInTheDocument()
    expect(within(row!).queryByText('标签：edge')).not.toBeInTheDocument()
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

    const row = screen.getAllByRole('article').find((item) =>
      within(item).queryByText('Tokyo Edge'),
    )
    expect(row).toBeDefined()

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

    await waitFor(() => expect(within(row!).getByText('标签：edge · core')).toBeInTheDocument())
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

    const rows = screen.getAllByRole('article')
    const tokyoRow = rows.find((row) => within(row).queryByText('Tokyo Edge'))
    const seoulRow = rows.find((row) => within(row).queryByText('Seoul Edge'))
    expect(tokyoRow).toBeDefined()
    expect(seoulRow).toBeDefined()

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
    expect(
      screen.getByRole('button', { name: '移除筛选 仅看异常' }),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空所有' }))

    await waitFor(() => expect(screen.getByText('Healthy Edge')).toBeInTheDocument())
    expect(screen.getByText('Alerting Edge')).toBeInTheDocument()
  })

})
