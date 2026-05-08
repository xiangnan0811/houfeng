import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatDateTime } from '../lib/format'
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

  it('shows the cached token and templated install steps while onboarding has not started', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(createOnboardingState())),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('节点接入')).toBeInTheDocument()
    // Token plaintext is rendered inside the warning Card.
    expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument()
    // Install steps mention the systemd path and template substitution.
    expect(
      screen.getByText('/etc/houfeng-agent/agent.env', { exact: false }),
    ).toBeInTheDocument()
    // Token is available, so the install snippet should embed the real token,
    // not the placeholder.
    expect(
      screen.getByText("echo 'enroll_tokyo_001' > /etc/houfeng-agent/token"),
    ).toBeInTheDocument()
    expect(screen.queryByText("echo '###TOKEN###' > /etc/houfeng-agent/token")).toBeNull()
  })

  it('shows the bound-but-waiting state before stable observation arrives', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '已绑定',
            phase: '已绑定，等待稳定观测',
            last_heartbeat_at: '2026-04-26T09:20:00Z',
            updated_at: '2026-04-26T09:20:00Z',
          }),
        ),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Phase stepper now carries the phase narrative; summary cards still
    // surface the host-sample / observation flags.
    expect(screen.getByText('首批样本')).toBeInTheDocument()
    expect(screen.getByText('未到达')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /查看节点详情/ })).not.toBeInTheDocument()
  })

  it('shows completion state with a CTA back to node detail', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState({
            lifecycle_status: '在用',
            binding_status: '已绑定',
            phase: '接入完成',
            last_heartbeat_at: '2026-04-26T09:20:00Z',
            last_sync_at: '2026-04-26T09:21:00Z',
            updated_at: '2026-04-26T09:21:00Z',
            has_host_sample: true,
            has_accepted_observation: true,
          }),
        ),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('link', { name: /查看节点详情/ })).toBeInTheDocument(),
    )

    expect(screen.getByRole('link', { name: /查看节点详情/ })).toHaveAttribute(
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
        mockJSONResponse(
          createOnboardingState({
            enrollment_token_issued_at: '2026-04-26T09:10:00Z',
          }),
        ),
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
      vi.fn().mockResolvedValue(mockJSONResponse(createOnboardingState())),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('当前会话里没有可显示的 Token 明文。')).toBeInTheDocument(),
    )

    expect(screen.getByText('请重新生成接入 Token，再继续安装或核对配置。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新生成接入 Token' })).toBeInTheDocument()
    expect(screen.queryByText('enroll_tokyo_001')).not.toBeInTheDocument()
    // The install snippet shows the placeholder since no token is available.
    expect(
      screen.getByText("echo '###TOKEN###' > /etc/houfeng-agent/token"),
    ).toBeInTheDocument()
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
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('collapses the token block when "已保存，关闭" is clicked and lets it be re-expanded', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(createOnboardingState())),
    )

    renderOnboardingPage()

    await waitFor(() => expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '已保存，关闭' }))

    await waitFor(() =>
      expect(
        screen.getByText('Token 明文已隐藏。本会话内可重新展开；离开页面后需要重新生成。'),
      ).toBeInTheDocument(),
    )

    expect(screen.queryByText('enroll_tokyo_001')).not.toBeInTheDocument()
    // Cache is intentionally NOT cleared when the user only collapses the UI
    // (PRD Q-CLOSE decision: cache 不清).
    expect(getOnboardingTokenCache('nd_001')).toEqual({
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    // Install snippet should fall back to the placeholder while collapsed.
    expect(
      screen.getByText("echo '###TOKEN###' > /etc/houfeng-agent/token"),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重新展开 token 明文' }))

    await waitFor(() => expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument())
    // Install snippet now embeds the real token again.
    expect(
      screen.getByText("echo 'enroll_tokyo_001' > /etc/houfeng-agent/token"),
    ).toBeInTheDocument()
  })

  it('copies the token via navigator.clipboard.writeText when the copy button is pressed', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    Object.defineProperty(window, 'isSecureContext', {
      configurable: true,
      get: () => true,
    })

    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(createOnboardingState())),
    )

    renderOnboardingPage()

    await waitFor(() => expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '复制 token' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('enroll_tokyo_001'))
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
    expect(within(conflictCard).getByText(formatDateTime('2026-04-26T09:15:00Z'))).toBeInTheDocument()
    expect(within(conflictCard).getByText(formatDateTime('2026-04-26T09:18:00Z'))).toBeInTheDocument()
    expect(within(conflictCard).getByText('4')).toBeInTheDocument()
    // Two-step UX: section shows ghost buttons by default; the actual
    // confirmation card only appears after a choice is made.
    expect(within(conflictCard).getByRole('button', { name: '确认重新绑定…' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '拒绝新指纹…' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '重置绑定…' })).toBeInTheDocument()
    // No ActionConfirmationCard yet.
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
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

    // Two-step UX: open the reject confirmation card first, then confirm.
    fireEvent.click(within(conflictCard).getByRole('button', { name: '拒绝新指纹…' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '确认拒绝该指纹' }))

    await waitFor(() => expect(within(conflictCard).getByText('reject failed')).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.queryByText('节点接入工作台不可用')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
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

    // Two-step UX: ghost button opens the confirm-rebind ActionConfirmationCard.
    fireEvent.click(within(conflictCard).getByRole('button', { name: '确认重新绑定…' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '确认重新绑定' }))

    await waitFor(() =>
      expect(screen.queryByRole('article', { name: '高优先级：绑定冲突待处理' })).not.toBeInTheDocument(),
    )

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('renders the phase stepper with the first step current when the node is unbound', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '未绑定',
            phase: '未开始接入',
            has_host_sample: false,
            has_accepted_observation: false,
          }),
        ),
      ),
    )

    renderOnboardingPage()

    const stepper = await screen.findByRole('list', { name: '节点接入进度' })
    const items = stepper.querySelectorAll('.stepper__step')
    expect(items.length).toBe(4)
    expect(items[0].classList.contains('stepper__step--current')).toBe(true)
    expect(items[1].classList.contains('stepper__step--pending')).toBe(true)
    expect(items[2].classList.contains('stepper__step--pending')).toBe(true)
    expect(items[3].classList.contains('stepper__step--pending')).toBe(true)
  })

  it('marks the third step current after binding completes but before observations arrive', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '已绑定',
            phase: '已绑定，等待稳定观测',
            has_host_sample: false,
            has_accepted_observation: false,
          }),
        ),
      ),
    )

    renderOnboardingPage()

    const stepper = await screen.findByRole('list', { name: '节点接入进度' })
    const items = stepper.querySelectorAll('.stepper__step')
    expect(items[0].classList.contains('stepper__step--done')).toBe(true)
    expect(items[1].classList.contains('stepper__step--done')).toBe(true)
    expect(items[2].classList.contains('stepper__step--current')).toBe(true)
    expect(items[3].classList.contains('stepper__step--pending')).toBe(true)
  })

  it('marks the second step error when a fingerprint conflict is pending', async () => {
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

    const stepper = await screen.findByRole('list', { name: '节点接入进度' })
    const items = stepper.querySelectorAll('.stepper__step')
    expect(items[0].classList.contains('stepper__step--done')).toBe(true)
    expect(items[1].classList.contains('stepper__step--error')).toBe(true)
    expect(items[2].classList.contains('stepper__step--pending')).toBe(true)
    expect(items[3].classList.contains('stepper__step--pending')).toBe(true)
  })

  it('marks every step done once observations have been accepted', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse(
          createOnboardingState({
            binding_status: '已绑定',
            phase: '接入完成',
            has_host_sample: true,
            has_accepted_observation: true,
          }),
        ),
      ),
    )

    renderOnboardingPage()

    const stepper = await screen.findByRole('list', { name: '节点接入进度' })
    const items = stepper.querySelectorAll('.stepper__step')
    expect(items[0].classList.contains('stepper__step--done')).toBe(true)
    expect(items[1].classList.contains('stepper__step--done')).toBe(true)
    expect(items[2].classList.contains('stepper__step--done')).toBe(true)
    expect(items[3].classList.contains('stepper__step--done')).toBe(true)
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

    // Two-step UX: open the confirm-rebind ActionConfirmationCard, then confirm.
    fireEvent.click(within(conflictCard).getByRole('button', { name: '确认重新绑定…' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '确认重新绑定' }))

    await waitFor(() =>
      expect(screen.queryByRole('article', { name: '高优先级：绑定冲突待处理' })).not.toBeInTheDocument(),
    )

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('opens the reset-binding ActionConfirmationCard when the ghost button is clicked', async () => {
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

    // No ActionConfirmationCard before the ghost choice.
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

    fireEvent.click(within(conflictCard).getByRole('button', { name: '重置绑定…' }))

    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('重置绑定关系')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '确认重置绑定' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeInTheDocument()
  })

  it('returns to the ghost-button row when the ActionConfirmationCard is cancelled', async () => {
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

    fireEvent.click(within(conflictCard).getByRole('button', { name: '确认重新绑定…' }))
    const dialog = await screen.findByRole('alertdialog')

    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(within(conflictCard).getByRole('button', { name: '确认重新绑定…' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '拒绝新指纹…' })).toBeInTheDocument()
    expect(within(conflictCard).getByRole('button', { name: '重置绑定…' })).toBeInTheDocument()
  })

  it('shows fingerprint metadata wrapped in mono atoms (Hostname / Timestamp / MonoDigits)', async () => {
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
              attempt_count: 7,
            },
          ),
        ),
      ),
    )

    renderOnboardingPage()

    const conflictCard = await screen.findByRole('article', {
      name: '高优先级：绑定冲突待处理',
    })

    // current binding fingerprint → Hostname (mono + hostname class)
    const currentFp = within(conflictCard).getByText('sha256:c…abcdef')
    expect(currentFp.className).toContain('mono')
    expect(currentFp.className).toContain('hostname')

    // pending fingerprint → Hostname (mono + hostname class)
    const pendingFp = within(conflictCard).getByText('sha256:p…567890')
    expect(pendingFp.className).toContain('mono')
    expect(pendingFp.className).toContain('hostname')

    // first / last seen timestamps → Timestamp (mono + timestamp class)
    const firstSeen = within(conflictCard).getByText(formatDateTime('2026-04-26T09:15:00Z'))
    expect(firstSeen.className).toContain('timestamp')
    expect(firstSeen.className).toContain('mono')

    const lastSeen = within(conflictCard).getByText(formatDateTime('2026-04-26T09:18:00Z'))
    expect(lastSeen.className).toContain('timestamp')

    // attempt count → MonoDigits (mono + tnum + mono-digits)
    const attempt = within(conflictCard).getByText('7')
    expect(attempt.className).toContain('mono-digits')
    expect(attempt.className).toContain('tnum')
  })
})
