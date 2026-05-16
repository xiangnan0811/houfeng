import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatDateTime } from '../lib/format'
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

  it('makes one-command install primary before any token is generated', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(createOnboardingState())),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('节点接入')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument()
    expect(
      screen.getByText('命令由 center 后端生成，使用 HOUFENG_PUBLIC_BASE_URL，不会从浏览器地址猜测生产 URL。'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('安装命令包含 30 分钟有效的一次性 enrollment token。', { exact: false }),
    ).toBeInTheDocument()
    expect(screen.getByText('尚未生成一键安装命令。')).toBeInTheDocument()
    expect(screen.getByText('手工安装仍可作为排障路径')).toBeInTheDocument()
    expect(screen.getByText('HOUFENG_AGENT_SERVER_URL=<center public base URL>', { exact: false })).toBeInTheDocument()
    expect(
      screen.getByText("printf '%s' '<30-minute enrollment token>' | sudo tee /etc/houfeng-agent/token >/dev/null"),
    ).toBeInTheDocument()
    expect(screen.queryByText(window.location.origin)).not.toBeInTheDocument()
    expect(screen.queryByText('enroll_tokyo_001')).not.toBeInTheDocument()
  })

  it('shows the bound-but-waiting state before stable observation arrives', async () => {
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

  it('generates and displays the backend-issued one-command installer', async () => {
    const command = 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_tokyo_001" --version "v1.2.3" --release-repo "owner/repo"'
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(createOnboardingState()))
      .mockResolvedValueOnce(
        mockJSONResponse({
          command,
          issued_at: '2026-04-26T09:25:00Z',
          expires_at: '2026-04-26T09:55:00Z',
          installer_url: 'https://center.example.com/api/agent/install.sh',
          public_base_url: 'https://center.example.com',
          agent_version: 'v1.2.3',
          release_repo: 'owner/repo',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))

    await waitFor(() => expect(screen.getByText(command)).toBeInTheDocument())

    expect(screen.getByText('有效至：', { exact: false })).toBeInTheDocument()
    expect(screen.getAllByText(formatDateTime('2026-04-26T09:25:00Z')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(formatDateTime('2026-04-26T09:55:00Z')).length).toBeGreaterThan(0)
    expect(screen.getByText('https://center.example.com')).toBeInTheDocument()
    expect(screen.getByText('https://center.example.com/api/agent/install.sh')).toBeInTheDocument()
    expect(screen.getByText('v1.2.3')).toBeInTheDocument()
    expect(screen.getByText('owner/repo')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/install-command', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).not.toHaveBeenCalledWith('/api/nodes/nd_001/enrollment-token', expect.anything())
  })

  it('surfaces missing center install configuration without guessing a browser URL', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(createOnboardingState()))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'public base URL is not configured' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))

    await waitFor(() =>
      expect(
        screen.getByText('中心一键安装配置不完整：public base URL is not configured。请检查 HOUFENG_PUBLIC_BASE_URL 与发布版本配置后重新生成。'),
      ).toBeInTheDocument(),
    )

    expect(screen.queryByText(window.location.origin)).not.toBeInTheDocument()
    expect(screen.queryByText('/api/agent/install.sh')).not.toBeInTheDocument()
  })

  it('regenerates install commands and replaces the previously visible secret', async () => {
    const firstCommand = 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_tokyo_001" --version "v1.2.3" --release-repo "owner/repo"'
    const secondCommand = 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_tokyo_002" --version "v1.2.3" --release-repo "owner/repo"'
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(createOnboardingState()))
      .mockResolvedValueOnce(
        mockJSONResponse({
          command: firstCommand,
          issued_at: '2026-04-26T09:25:00Z',
          expires_at: '2026-04-26T09:55:00Z',
          installer_url: 'https://center.example.com/api/agent/install.sh',
          public_base_url: 'https://center.example.com',
          agent_version: 'v1.2.3',
          release_repo: 'owner/repo',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          command: secondCommand,
          issued_at: '2026-04-26T09:30:00Z',
          expires_at: '2026-04-26T10:00:00Z',
          installer_url: 'https://center.example.com/api/agent/install.sh',
          public_base_url: 'https://center.example.com',
          agent_version: 'v1.2.3',
          release_repo: 'owner/repo',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(screen.getByText(firstCommand)).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '重新生成安装命令' }))

    await waitFor(() => expect(screen.getByText(secondCommand)).toBeInTheDocument())
    expect(screen.queryByText(firstCommand)).not.toBeInTheDocument()
    expect(screen.getAllByText(formatDateTime('2026-04-26T10:00:00Z')).length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/install-command', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('hides and re-expands the generated install command within the current page session', async () => {
    const command = 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_tokyo_001" --version "v1.2.3" --release-repo "owner/repo"'
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(createOnboardingState()))
        .mockResolvedValueOnce(
          mockJSONResponse({
            command,
            issued_at: '2026-04-26T09:25:00Z',
            expires_at: '2026-04-26T09:55:00Z',
            installer_url: 'https://center.example.com/api/agent/install.sh',
            public_base_url: 'https://center.example.com',
            agent_version: 'v1.2.3',
            release_repo: 'owner/repo',
          }),
        ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(screen.getByText(command)).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '隐藏安装命令' }))

    await waitFor(() =>
      expect(screen.getByText('安装命令已隐藏。本页会话内可重新展开；如果已离开页面或命令过期，请重新生成。')).toBeInTheDocument(),
    )
    expect(screen.queryByText(command)).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重新展开命令' }))

    await waitFor(() => expect(screen.getByText(command)).toBeInTheDocument())
  })

  it('copies the generated install command via navigator.clipboard.writeText', async () => {
    const writeText = vi.fn(async () => {})
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    Object.defineProperty(window, 'isSecureContext', {
      configurable: true,
      get: () => true,
    })

    const command = 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_tokyo_001" --version "v1.2.3" --release-repo "owner/repo"'
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(createOnboardingState()))
        .mockResolvedValueOnce(
          mockJSONResponse({
            command,
            issued_at: '2026-04-26T09:25:00Z',
            expires_at: '2026-04-26T09:55:00Z',
            installer_url: 'https://center.example.com/api/agent/install.sh',
            public_base_url: 'https://center.example.com',
            agent_version: 'v1.2.3',
            release_repo: 'owner/repo',
          }),
        ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(screen.getByText(command)).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '复制安装命令' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(command))
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

  it('warns that conflict resolution requires a regenerated command after token consumption', async () => {
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
    expect(within(dialog).getByText('触发待确认的安装命令已消耗一次性 token', { exact: false })).toBeInTheDocument()
    expect(within(dialog).getByText('不会改变节点 ID 与历史观测数据。')).toBeInTheDocument()
    expect(within(dialog).queryByText(/enrollment token/)).not.toBeInTheDocument()
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
