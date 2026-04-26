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

    fireEvent.click(screen.getByRole('button', { name: '新建节点' }))
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
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
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
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_paused/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('requires strong confirmation before pausing monitoring and keeps runtime errors local', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
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
            },
          ]),
        )
        .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid runtime transition' }, 409)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes']}>
        <Routes>
          <Route path="/nodes" element={<NodesPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(confirmMock).toHaveBeenCalledWith('暂停监控会停止采集并产生数据空档，确定继续吗？')
    await waitFor(() =>
      expect(screen.getByText('invalid runtime transition')).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '节点列表' })).toBeInTheDocument()
  })
})
