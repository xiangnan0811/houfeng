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
