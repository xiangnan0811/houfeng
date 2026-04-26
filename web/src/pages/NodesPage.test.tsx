import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getOnboardingTokenCache } from '../lib/onboardingTokenCache'
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
        lifecycle_status: '待接入',
        labels: [],
        note: '',
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

  it('retries token issuance without creating a duplicate node after create succeeds once', async () => {
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
          token: 'enroll_tokyo_001',
          issued_at: '2026-04-26T09:06:00Z',
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
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'Tokyo Edge' } })
    fireEvent.change(screen.getByLabelText('地区'), { target: { value: 'ap-northeast-1' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Tokyo' } })
    fireEvent.change(screen.getByLabelText('供应商'), { target: { value: 'Vultr' } })

    fireEvent.click(screen.getByRole('button', { name: '创建并生成 Token' }))

    await waitFor(() =>
      expect(screen.getByText('节点已创建，但生成接入 Token 失败：token service unavailable')).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: /生成 Token/ }))

    await waitFor(() => expect(screen.getByText('onboarding workspace')).toBeInTheDocument())

    expect(fetchMock).toHaveBeenCalledTimes(4)
    const createCalls = fetchMock.mock.calls.filter(
      ([url, init]) => url == '/api/nodes' && (init as RequestInit | undefined)?.method == 'POST',
    )
    expect(createCalls).toHaveLength(1)
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/nodes/nd_001/enrollment-token', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(getOnboardingTokenCache('nd_001')).toEqual({
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:06:00Z',
    })
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
})
