import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { MonitoringInstanceRecord } from '../../lib/types'
import { MonitoringInstanceOnboardingDrawer } from './MonitoringInstanceOnboardingDrawer'

const { copyMock } = vi.hoisted(() => ({
  copyMock: vi.fn(async () => true),
}))

vi.mock('../../lib/useCopyToClipboard', () => ({
  useCopyToClipboard: () => ({ copy: copyMock, copied: false }),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

const INSTALL_COMMAND =
  'curl -fsSL https://center.example.invalid/api/agent/install.sh | sudo bash'

function installIssue(command = INSTALL_COMMAND) {
  return {
    command,
    issued_at: '2026-04-24T09:00:00Z',
    expires_at: '2026-04-24T09:30:00Z',
    installer_url: 'https://center.example.invalid/api/agent/install.sh',
    public_base_url: 'https://center.example.invalid',
    agent_version: 'v1.0.0',
    release_repo: 'houfeng/houfeng',
  }
}

function instance(id = 'mi_001', name = 'Tokyo Monitor'): MonitoringInstanceRecord {
  return {
    monitoring_instance_id: id,
    display_name: name,
    group: '',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '待接入',
    monitoring_status: '未启用',
    binding_status: '未绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
  }
}

function stubInstallCommand(handler: (id: string) => Promise<Response> | Response) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const method = init?.method ?? 'GET'
    const match = path.match(/^\/api\/monitoring-instances\/([^/]+)\/install-command$/)
    const monitoringInstanceId = match?.[1]
    if (method === 'POST' && monitoringInstanceId) {
      return handler(monitoringInstanceId)
    }
    throw new Error(`unexpected ${method} ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('retired monitoring reenrollment', () => {
  afterEach(() => {
    copyMock.mockReset()
    copyMock.mockResolvedValue(true)
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })
  it.each(['已接入', '已退役'])('creates a new binding session for %s only after explicit command generation', async (lifecycle_status) => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/binding/reset')) return mockJSONResponse({})
      if (String(input).endsWith('/install-command')) return mockJSONResponse(installIssue())
      if (String(input).endsWith('/phases')) return mockJSONResponse([])
      throw new Error(`unexpected ${String(input)}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryRouter><MonitoringInstanceOnboardingDrawer open onClose={vi.fn()} mode="upgrade"
      monitoringInstance={{ ...instance(), lifecycle_status, vps_lifecycle_status: 'active' }} /></MemoryRouter>)
    expect(fetchMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '生成升级/重新接入命令' }))
    await screen.findByText('安装命令已自动复制到剪贴板。')
    // 先重置、再签发，签发成功后才读取一次会话基线。
    await waitFor(() => expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual([
      '/api/monitoring-instances/mi_001/binding/reset', '/api/monitoring-instances/mi_001/install-command',
      '/api/monitoring-instances/mi_001/phases',
    ]))
  })

  it('blocks install command generation for an archived owner', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    render(<MemoryRouter><MonitoringInstanceOnboardingDrawer open onClose={vi.fn()}
      monitoringInstance={{ ...instance(), lifecycle_status: '已退役', vps_lifecycle_status: 'archived' }} /></MemoryRouter>)
    expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeDisabled()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

function DrawerHarness({
  monitoringInstance,
  open,
  onClose,
}: {
  monitoringInstance: MonitoringInstanceRecord
  open: boolean
  onClose: () => void
}) {
  return (
    <MemoryRouter>
      <MonitoringInstanceOnboardingDrawer
        monitoringInstance={monitoringInstance}
        open={open}
        onClose={onClose}
      />
    </MemoryRouter>
  )
}

describe('MonitoringInstanceOnboardingDrawer issue lifecycle', () => {
  afterEach(() => {
    copyMock.mockReset()
    copyMock.mockResolvedValue(true)
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('makes completion primary after issuing and advances only for a new full session missing from the baseline', async () => {
    // 与签发并发、在签发返回前提交的旧接入：开始时间晚于 issued_at，但出现在签发后的基线里。
    const racing = { session_id: 's_racing', capability: 'full', fingerprint_hash: 'r', started_at: '2026-04-24T09:00:01Z', last_trusted_online_at: '2026-04-24T09:00:02Z', ever_connected: true }
    let phases: unknown[] = [racing]
    let phaseReads = 0
    let releaseSlowRead: (() => void) | null = null
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if ((init?.method ?? 'GET') === 'POST' && path === '/api/monitoring-instances/mi_001/install-command') return mockJSONResponse(installIssue())
      if (path === '/api/monitoring-instances/mi_001/phases') {
        phaseReads += 1
        // 第三次读取故意卡住：其间不应发起新的轮询。
        if (phaseReads === 3) return new Promise<Response>((resolve) => { releaseSlowRead = () => resolve(mockJSONResponse(phases)) })
        return mockJSONResponse(phases)
      }
      throw new Error(`unexpected ${path}`)
    }))
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    try {
      render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
      const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
      fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))
      const complete = await within(drawer).findByRole('button', { name: '完成并查看监控实例' })
      expect(complete).toHaveClass('primary')
      expect(within(drawer).getByRole('button', { name: '重新生成安装命令' })).toHaveClass('secondary')
      expect(within(drawer).getByText('执行命令后，本窗口会自动检测首次心跳。')).toBeInTheDocument()
      // 签发后立即读取一次作为基线。
      await waitFor(() => expect(phaseReads).toBe(1))

      // 基线里的会话、签发前开始的完整会话与降级的旧会话都不算本次接入。
      phases = [
        racing,
        { session_id: 's_old_full', capability: 'full', fingerprint_hash: 'a', started_at: '2026-04-24T08:00:00Z', last_trusted_online_at: '2026-04-24T09:05:00Z', ever_connected: true },
        { session_id: 's_degraded', capability: 'evidence_only', fingerprint_hash: 'b', started_at: '2026-04-24T09:01:00Z', last_trusted_online_at: '2026-04-24T09:05:00Z', ever_connected: true },
      ]
      await act(async () => { vi.advanceTimersByTime(5_000) })
      expect(phaseReads).toBe(2)
      expect(within(drawer).getByRole('button', { name: '重新生成安装命令' })).toBeInTheDocument()

      await act(async () => { vi.advanceTimersByTime(5_000) })
      expect(phaseReads).toBe(3)
      await act(async () => { vi.advanceTimersByTime(15_000) })
      expect(phaseReads).toBe(3)

      phases = [...phases, { session_id: 's_new', capability: 'full', fingerprint_hash: 'c', started_at: '2026-04-24T09:02:00Z', last_trusted_online_at: '2026-04-24T09:03:00Z', ever_connected: true }]
      await act(async () => { releaseSlowRead?.() })
      expect(await within(drawer).findByText(/接入完成/)).toBeInTheDocument()
      expect(within(drawer).queryByRole('button', { name: /重新生成/ })).not.toBeInTheDocument()
      expect(drawer.querySelector('[aria-label="一键安装命令"]')).toBeNull()
      expect(within(drawer).getByRole('button', { name: '完成并查看监控实例' })).toHaveClass('primary')
    } finally {
      vi.useRealTimers()
    }
  })

  it('reads a fresh session baseline right away after regenerating, even while an old read hangs', async () => {
    let phaseReads = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if ((init?.method ?? 'GET') === 'POST' && path === '/api/monitoring-instances/mi_001/install-command') return mockJSONResponse(installIssue())
      if (path === '/api/monitoring-instances/mi_001/phases') {
        phaseReads += 1
        // 第一次签发后的基线读取一直不返回。
        if (phaseReads === 1) return new Promise<Response>(() => undefined)
        return mockJSONResponse([])
      }
      throw new Error(`unexpected ${path}`)
    }))
    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(phaseReads).toBe(1))
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    try {
      const issueCalls = () => vi.mocked(fetch).mock.calls.filter(([url]) => String(url).endsWith('/install-command')).length
      fireEvent.click(await within(drawer).findByRole('button', { name: '重新生成安装命令' }))
      // 新签发的基线读取不等旧请求结束；旧请求一直挂起也不阻塞之后的例行轮询。
      for (let i = 0; i < 10 && (issueCalls() < 2 || phaseReads < 2); i += 1) {
        await act(async () => { await Promise.resolve() })
      }
      expect(issueCalls()).toBe(2)
      expect(phaseReads).toBe(2)
      await act(async () => { vi.advanceTimersByTime(5_000) })
      expect(phaseReads).toBe(3)
    } finally {
      vi.useRealTimers()
    }
  })

  it('blocks X, Escape, and backdrop dismissal while install command issue is pending', async () => {
    const pending = deferred<Response>()
    stubInstallCommand(() => pending.promise)
    const onClose = vi.fn()
    render(<DrawerHarness monitoringInstance={instance()} open onClose={onClose} />)

    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))
    expect(within(drawer).getByRole('button', { name: '正在生成…' })).toBeDisabled()
    expect(within(drawer).queryByRole('button', { name: '完成并查看监控实例' })).not.toBeInTheDocument()

    fireEvent.click(within(drawer).getByRole('button', { name: '关闭' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    const overlay = document.body.querySelector('.modal-overlay')
    expect(overlay).not.toBeNull()
    fireEvent.click(overlay!)

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: '监控实例接入抽屉' })).toBeInTheDocument()
    expect(copyMock).not.toHaveBeenCalled()
  })

  it('blocks complete, X, Escape, and backdrop while regeneration copy is pending', async () => {
    const regenerate = deferred<Response>()
    let issueCalls = 0
    stubInstallCommand(() => {
      issueCalls += 1
      if (issueCalls === 1) return mockJSONResponse(installIssue())
      return regenerate.promise
    })
    const onClose = vi.fn()
    render(<DrawerHarness monitoringInstance={instance()} open onClose={onClose} />)

    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(copyMock).toHaveBeenCalledTimes(1))
    expect(copyMock).toHaveBeenCalledWith(INSTALL_COMMAND)
    const complete = await within(drawer).findByRole('button', { name: '完成并查看监控实例' })

    fireEvent.click(within(drawer).getByRole('button', { name: '重新生成安装命令' }))
    expect(within(drawer).getByRole('button', { name: '正在生成…' })).toBeDisabled()
    expect(complete).toBeDisabled()

    fireEvent.click(complete)
    fireEvent.click(within(drawer).getByRole('button', { name: '关闭' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.click(document.body.querySelector('.modal-overlay')!)

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: '监控实例接入抽屉' })).toBeInTheDocument()
    expect(copyMock).toHaveBeenCalledTimes(1)
  })

  it('does not copy or reveal a command when unmounted before the issue response, including remount', async () => {
    const pending = deferred<Response>()
    stubInstallCommand(() => pending.promise)
    const view = render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    expect(screen.getByRole('button', { name: '正在生成…' })).toBeDisabled()
    view.unmount()

    await act(async () => {
      pending.resolve(mockJSONResponse(installIssue()))
    })
    expect(copyMock).not.toHaveBeenCalled()

    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeEnabled()
    expect(screen.queryByRole('region', { name: '一键安装命令' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('一键安装命令')).not.toBeInTheDocument()
    expect(screen.queryByText(INSTALL_COMMAND)).not.toBeInTheDocument()
  })

  it('does not copy or reveal after parent close or subject switch of a deferred issue', async () => {
    const pending = deferred<Response>()
    stubInstallCommand(() => pending.promise)
    const onClose = vi.fn()
    const first = instance('mi_001', 'Tokyo Monitor')
    const view = render(<DrawerHarness monitoringInstance={first} open onClose={onClose} />)

    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    expect(screen.getByRole('button', { name: '正在生成…' })).toBeDisabled()

    await act(async () => {
      pending.resolve(mockJSONResponse(installIssue()))
      view.rerender(<DrawerHarness monitoringInstance={first} open={false} onClose={onClose} />)
    })
    expect(copyMock).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: '监控实例接入抽屉' })).not.toBeInTheDocument()

    const switched = deferred<Response>()
    stubInstallCommand(() => switched.promise)
    view.rerender(
      <DrawerHarness monitoringInstance={instance('mi_002', 'Osaka Monitor')} open onClose={onClose} />,
    )
    fireEvent.click(screen.getByRole('button', { name: '生成一键安装命令' }))
    await act(async () => {
      switched.resolve(mockJSONResponse(installIssue('stale-mi_002-command')))
      view.rerender(
        <DrawerHarness monitoringInstance={instance('mi_003', 'Seoul Monitor')} open onClose={onClose} />,
      )
    })
    expect(copyMock).not.toHaveBeenCalled()
    expect(screen.queryByText('stale-mi_002-command')).not.toBeInTheDocument()
    expect(screen.queryByText(INSTALL_COMMAND)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Seoul Monitor · 接入 agent' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeEnabled()
  })

  it('blocks dismissal while clipboard copy is pending and does not reveal after unmount', async () => {
    const copyPending = deferred<boolean>()
    copyMock.mockImplementation(() => copyPending.promise)
    stubInstallCommand(() => mockJSONResponse(installIssue()))
    const onClose = vi.fn()
    const view = render(<DrawerHarness monitoringInstance={instance()} open onClose={onClose} />)

    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))
    await waitFor(() => expect(copyMock).toHaveBeenCalledTimes(1))
    expect(copyMock).toHaveBeenCalledWith(INSTALL_COMMAND)
    expect(within(drawer).getByRole('button', { name: '正在生成…' })).toBeDisabled()
    expect(within(drawer).queryByRole('button', { name: '完成并查看监控实例' })).not.toBeInTheDocument()

    fireEvent.click(within(drawer).getByRole('button', { name: '关闭' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.click(document.body.querySelector('.modal-overlay')!)
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: '监控实例接入抽屉' })).toBeInTheDocument()

    view.unmount()
    await act(async () => {
      copyPending.resolve(true)
    })

    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    expect(screen.getByRole('button', { name: '生成一键安装命令' })).toBeEnabled()
    expect(screen.queryByLabelText('一键安装命令')).not.toBeInTheDocument()
    expect(screen.queryByText(INSTALL_COMMAND)).not.toBeInTheDocument()
  })
})

describe('MonitoringInstanceOnboardingDrawer install command error diagnosis', () => {
  afterEach(() => {
    copyMock.mockReset()
    copyMock.mockResolvedValue(true)
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('surfaces configuration guidance when install command is unconfigured', async () => {
    stubInstallCommand(() =>
      mockJSONResponse(
        {
          error: 'public base URL is not configured',
          code: 'install_command_unconfigured',
        },
        409,
      ),
    )

    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))

    const errorAlert = await within(drawer).findByRole('alert')
    expect(errorAlert).toHaveTextContent('HOUFENG_PUBLIC_BASE_URL')
    expect(errorAlert).toHaveTextContent('public base URL is not configured')
  })

  it('explains archived instance cannot be installed and avoids configuration guidance', async () => {
    stubInstallCommand(() =>
      mockJSONResponse(
        {
          error: 'archived monitoring instance',
          code: 'monitoring_instance_archived',
        },
        409,
      ),
    )

    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))

    const errorAlert = await within(drawer).findByRole('alert')
    expect(errorAlert).toHaveTextContent('已归档')
    expect(errorAlert).toHaveTextContent('无法生成安装命令')
    expect(errorAlert).not.toHaveTextContent('HOUFENG_PUBLIC_BASE_URL')
    expect(errorAlert).not.toHaveTextContent('中心一键安装配置不完整')
  })

  it('surfaces actual error for unknown 409 conflict and avoids configuration guidance', async () => {
    stubInstallCommand(() =>
      mockJSONResponse(
        {
          error: 'resource conflict occurred',
        },
        409,
      ),
    )

    render(<DrawerHarness monitoringInstance={instance()} open onClose={vi.fn()} />)
    const drawer = screen.getByRole('dialog', { name: '监控实例接入抽屉' })
    fireEvent.click(within(drawer).getByRole('button', { name: '生成一键安装命令' }))

    const errorAlert = await within(drawer).findByRole('alert')
    expect(errorAlert).toHaveTextContent('resource conflict occurred')
    expect(errorAlert).not.toHaveTextContent('HOUFENG_PUBLIC_BASE_URL')
    expect(errorAlert).not.toHaveTextContent('中心一键安装配置不完整')
  })
})
