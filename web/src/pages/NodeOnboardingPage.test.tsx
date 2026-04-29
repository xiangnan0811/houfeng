import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getOnboardingTokenCache, setOnboardingTokenCache } from '../lib/onboardingTokenCache'
import { NodeOnboardingPage } from './NodeOnboardingPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

function renderOnboardingPage(initialEntry = '/nodes/nd_001/onboarding') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/nodes/:nodeId/onboarding" element={<NodeOnboardingPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function createOnboardingState(
  overrides: Record<string, unknown> = {},
  pendingBinding?: Record<string, unknown>,
) {
  return {
    node_id: 'nd_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '待接入',
    monitoring_status: '启用',
    binding_status: '未绑定',
    labels: ['edge'],
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
    ...(pendingBinding ? { pending_binding: pendingBinding } : {}),
    ...overrides,
  }
}

describe('NodeOnboardingPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.sessionStorage.clear()
  })

  it('shows the cached token and installation steps while onboarding has not started', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '未绑定',
          labels: ['edge'],
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
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('节点接入')).toBeInTheDocument()
    expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument()
    expect(screen.getByText('在服务器上安装 agent')).toBeInTheDocument()
    expect(screen.getByText('写入该节点专属 token')).toBeInTheDocument()
    expect(screen.getByText('启动 systemd 服务')).toBeInTheDocument()
    expect(screen.getByText('等待首次同步与绑定完成')).toBeInTheDocument()
    expect(screen.getByText('等待首次接入')).toBeInTheDocument()
  })

  it('shows the bound-but-waiting state before stable observation arrives', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-26T09:20:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:20:00Z',
          phase: '已绑定，等待稳定观测',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('已完成指纹绑定，等待稳定观测')).toBeInTheDocument(),
    )

    expect(
      screen.getByText('绑定已经建立，系统正在等待首批主机采样（HostSample）或已接收观测到达。'),
    ).toBeInTheDocument()
    expect(screen.getByText('已接收观测')).toBeInTheDocument()
    expect(screen.getByText('首批样本：未到达')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '查看节点详情' })).not.toBeInTheDocument()
  })

  it('shows completion state with a CTA back to node detail', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-26T09:20:00Z',
          last_sync_at: '2026-04-26T09:21:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:21:00Z',
          phase: '接入完成',
          has_host_sample: true,
          has_accepted_observation: true,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() => expect(screen.getByText('接入已完成，可以转入日常观察。')).toBeInTheDocument())

    expect(screen.getByRole('link', { name: '查看节点详情' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
  })

  it('clears a stale cached token when the server reports a different issuance time', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'stale_token_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '未绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:00:00Z',
          phase: '未开始接入',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:10:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('当前会话里没有可显示的 Token 明文。')).toBeInTheDocument(),
    )

    expect(screen.queryByText('stale_token_001')).not.toBeInTheDocument()
    await waitFor(() => expect(getOnboardingTokenCache('nd_001')).toBeNull())
  })

  it('asks for regeneration when the plaintext token is no longer cached', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '未绑定',
          labels: ['edge'],
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
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('当前会话里没有可显示的 Token 明文。')).toBeInTheDocument(),
    )

    expect(screen.getByText('请重新生成接入 Token，再继续安装或核对配置。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新生成接入 Token' })).toBeInTheDocument()
    expect(screen.queryByText('enroll_tokyo_001')).not.toBeInTheDocument()
  })

  it('reissues the enrollment token and refreshes onboarding state', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState({
            enrollment_token_issued_at: '2026-04-26T09:05:00Z',
          }),
        ),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          token: 'enroll_tokyo_002',
          issued_at: '2026-04-26T09:25:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState({
            enrollment_token_issued_at: '2026-04-26T09:25:00Z',
            updated_at: '2026-04-26T09:25:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('当前会话里没有可显示的 Token 明文。')).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '重新生成接入 Token' }))

    await waitFor(() => expect(screen.getByText('enroll_tokyo_002')).toBeInTheDocument())

    expect(getOnboardingTokenCache('nd_001')).toEqual({
      token: 'enroll_tokyo_002',
      issued_at: '2026-04-26T09:25:00Z',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/enrollment-token', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('shows a high-priority conflict card with masked fingerprint summaries and conflict metadata', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState(
            {
              binding_status: '指纹变更待确认',
              phase: '绑定冲突待处理',
              current_binding_fingerprint_summary: 'sha256:c…abcdef',
            },
            {
              fingerprint: 'sha256:pendabcdef1234567890',
              first_seen_at: '2026-04-26T09:15:00Z',
              last_seen_at: '2026-04-26T09:18:00Z',
              attempt_count: 4,
            },
          ),
        ),
      ),
    )

    renderOnboardingPage()

    const conflictCard = await screen.findByRole('article', {
      name: '高优先级：绑定冲突待处理',
    })

    expect(within(conflictCard).getByText('sha256:c…abcdef')).toBeInTheDocument()
    expect(within(conflictCard).getByText('sha256:p…567890')).toBeInTheDocument()
    expect(within(conflictCard).getByText('2026/04/26 17:15')).toBeInTheDocument()
    expect(within(conflictCard).getByText('2026/04/26 17:18')).toBeInTheDocument()
    expect(within(conflictCard).getByText('4')).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '确认重新绑定' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '拒绝该指纹' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '重置绑定关系' })).toBeInTheDocument()
  })

  it('keeps action failures local to the conflict section', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState(
            {
              binding_status: '指纹变更待确认',
              phase: '绑定冲突待处理',
              current_binding_fingerprint_summary: 'sha256:c…abcdef',
            },
            {
              fingerprint: 'sha256:pendabcdef1234567890',
              first_seen_at: '2026-04-26T09:15:00Z',
              last_seen_at: '2026-04-26T09:18:00Z',
              attempt_count: 4,
            },
          ),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse({ error: 'reject failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    const conflictCard = await screen.findByRole('article', {
      name: '高优先级：绑定冲突待处理',
    })

    fireEvent.click(within(conflictCard).getByRole('button', { name: '拒绝该指纹' }))

    await waitFor(() => expect(within(conflictCard).getByText('reject failed')).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.queryByText('节点接入工作台不可用')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('refreshes onboarding state after a successful binding action', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState(
            {
              binding_status: '指纹变更待确认',
              phase: '绑定冲突待处理',
              current_binding_fingerprint_summary: 'sha256:c…abcdef',
            },
            {
              fingerprint: 'sha256:pendabcdef1234567890',
              first_seen_at: '2026-04-26T09:15:00Z',
              last_seen_at: '2026-04-26T09:18:00Z',
              attempt_count: 4,
            },
          ),
        ),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '已绑定',
            phase: '已绑定，等待稳定观测',
            current_binding_fingerprint_summary: 'sha256:p…567890',
            updated_at: '2026-04-26T09:20:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    const conflictCard = await screen.findByRole('article', {
      name: '高优先级：绑定冲突待处理',
    })

    fireEvent.click(within(conflictCard).getByRole('button', { name: '确认重新绑定' }))

    await waitFor(() =>
      expect(screen.getByText('已完成指纹绑定，等待稳定观测')).toBeInTheDocument(),
    )

    expect(screen.queryByRole('article', { name: '高优先级：绑定冲突待处理' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the successful binding result visible without treating refresh as a failure', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState(
            {
              binding_status: '指纹变更待确认',
              phase: '绑定冲突待处理',
              current_binding_fingerprint_summary: 'sha256:c…abcdef',
            },
            {
              fingerprint: 'sha256:pendabcdef1234567890',
              first_seen_at: '2026-04-26T09:15:00Z',
              last_seen_at: '2026-04-26T09:18:00Z',
              attempt_count: 4,
            },
          ),
        ),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '已绑定',
            phase: '已绑定，等待稳定观测',
            current_binding_fingerprint_summary: 'sha256:p…567890',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    const conflictCard = await screen.findByRole('article', {
      name: '高优先级：绑定冲突待处理',
    })

    fireEvent.click(within(conflictCard).getByRole('button', { name: '确认重新绑定' }))

    await waitFor(() =>
      expect(screen.getByText('已完成指纹绑定，等待稳定观测')).toBeInTheDocument(),
    )

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
