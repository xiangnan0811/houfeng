import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CommandAuditPage } from './CommandAuditPage'

function mockJSONResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function auditAction(overrides: Record<string, unknown> = {}) {
  return {
    id: 'act_001',
    action_id: 'act_001',
    monitoring_instance: { id: 'mi_001', name: 'Tokyo Edge', deleted: false },
    command_id: 'uptime',
    sensitivity: 'standard',
    outcome: 'succeeded',
    actor: { user_id: 'usr_001', username: 'admin', display_name: '管理员' },
    started_at: '2026-07-12T10:00:00Z',
    events: [
      {
        audit_id: 'cmd_aud_queued',
        event_type: 'queued',
        source: 'web',
        occurred_at: '2026-07-12T10:00:00Z',
      },
      {
        audit_id: 'cmd_aud_completed',
        event_type: 'completed',
        source: 'agent_sync',
        exit_code: 0,
        occurred_at: '2026-07-12T10:00:02Z',
      },
    ],
    ...overrides,
  }
}

function LocationProbe() {
  const location = useLocation()
  return <output aria-label="当前查询参数">{location.search}</output>
}

function renderPage(initialEntry = '/command-audit') {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path="/command-audit"
          element={(
            <>
              <CommandAuditPage />
              <LocationProbe />
            </>
          )}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('CommandAuditPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('requests the default snapshot without URL parameters and renders metadata-only copy', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({ items: [auditAction()] }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    expect(screen.getByRole('heading', { name: '命令审计' })).toBeInTheDocument()
    expect(screen.getByText('正在加载命令审计')).toBeInTheDocument()
    expect(screen.getByText(/只展示命令、身份、时间和结果元数据/)).toBeInTheDocument()
    await screen.findByText('Tokyo Edge')
    expect(fetchMock).toHaveBeenCalledWith('/api/command-audits', expect.objectContaining({
      credentials: 'include',
    }))
    expect(screen.getByLabelText('当前查询参数')).toHaveTextContent('')
  })

  it('canonicalizes invalid and default URL values without a duplicate request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/command-audit?window=30d&outcome=done&command_id=rm_rf&unknown=1')

    await screen.findByText('没有匹配的命令审计')
    await waitFor(() => expect(screen.getByLabelText('当前查询参数')).toHaveTextContent(''))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/command-audits', expect.any(Object))
  })

  it('applies primary filters through canonical URL state', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [] }))
      .mockResolvedValueOnce(mockJSONResponse({ items: [auditAction()] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('没有匹配的命令审计')

    fireEvent.change(screen.getByLabelText('监控实例'), { target: { value: ' Tokyo Edge ' } })
    fireEvent.change(screen.getByLabelText('结果'), { target: { value: 'succeeded' } })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await screen.findByText('Tokyo Edge')
    await waitFor(() => expect(screen.getByLabelText('当前查询参数')).toHaveTextContent(
      '?monitoring_instance=Tokyo+Edge&outcome=succeeded',
    ))
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/command-audits?monitoring_instance=Tokyo+Edge&outcome=succeeded',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('keeps advanced filter edits as a cancellable draft and supports reset/apply', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [] }))
      .mockResolvedValue(mockJSONResponse({ items: [auditAction()] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage('/command-audit?actor=existing')
    await screen.findByText('没有匹配的命令审计')

    fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))
    let drawer = screen.getByRole('dialog', { name: '命令审计高级筛选' })
    expect(within(drawer).getByLabelText('操作者')).toHaveValue('existing')
    fireEvent.change(within(drawer).getByLabelText('操作者'), { target: { value: 'draft' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '取消' }))
    expect(screen.getByLabelText('当前查询参数')).toHaveTextContent('?actor=existing')

    fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))
    drawer = screen.getByRole('dialog', { name: '命令审计高级筛选' })
    expect(within(drawer).getByLabelText('操作者')).toHaveValue('existing')
    fireEvent.click(within(drawer).getByRole('button', { name: '重置高级筛选' }))
    fireEvent.change(within(drawer).getByLabelText('Action ID'), { target: { value: ' act_001 ' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用高级筛选' }))

    await screen.findByText('Tokyo Edge')
    await waitFor(() => expect(screen.getByLabelText('当前查询参数')).toHaveTextContent('?action_id=act_001'))
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/command-audits?action_id=act_001',
      expect.any(Object),
    )
  })

  it('loads cursor pages, deduplicates action ids, and stops when cursor is absent', async () => {
    const second = auditAction({ id: 'act_002', action_id: 'act_002', started_at: '2026-07-12T09:00:00Z' })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [auditAction()], next_cursor: 'cursor_2' }))
      .mockResolvedValueOnce(mockJSONResponse({ items: [auditAction(), second] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('Tokyo Edge')

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge')).toHaveLength(2))
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/command-audits?cursor=cursor_2',
      expect.any(Object),
    )
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the current page and cursor retry available when loading more fails', async () => {
    const second = auditAction({
      id: 'act_002',
      action_id: 'act_002',
      monitoring_instance: { id: 'mi_002', name: 'Osaka Relay', deleted: false },
      started_at: '2026-07-12T09:00:00Z',
    })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [auditAction()], next_cursor: 'cursor_retry' }))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'temporary failure' }, 500))
      .mockResolvedValueOnce(mockJSONResponse({ items: [second] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('Tokyo Edge')

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await screen.findByText(/加载更多失败/)
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '加载更多' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await screen.findByText('Osaka Relay')
    expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/command-audits?cursor=cursor_retry',
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/command-audits?cursor=cursor_retry',
      expect.any(Object),
    )
    expect(screen.queryByText(/加载更多失败/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument()
  })

  it('clears expanded rows and old results when filters change', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [auditAction()] }))
      .mockResolvedValueOnce(mockJSONResponse({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('Tokyo Edge')

    fireEvent.click(screen.getByRole('button', { name: '展开 2 个事件' }))
    expect(screen.getByText('已完成')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('监控实例'), { target: { value: 'Osaka' } })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.queryByText('已完成')).not.toBeInTheDocument()
    await screen.findByText('没有匹配的命令审计')
  })

  it('ignores stale initial responses after a filter change', async () => {
    let resolveFirst: ((value: Response) => void) | undefined
    const first = new Promise<Response>((resolve) => {
      resolveFirst = resolve
    })
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => first)
      .mockResolvedValueOnce(mockJSONResponse({
        items: [auditAction({ id: 'act_new', action_id: 'act_new', monitoring_instance: { id: 'mi_new', name: 'New Result', deleted: false } })],
      }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

    fireEvent.change(screen.getByLabelText('监控实例'), { target: { value: 'New' } })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))
    await screen.findByText('New Result')

    resolveFirst?.(mockJSONResponse({
      items: [auditAction({ id: 'act_old', action_id: 'act_old', monitoring_instance: { id: 'mi_old', name: 'Stale Result', deleted: false } })],
    }))
    await waitFor(() => expect(screen.queryByText('Stale Result')).not.toBeInTheDocument())
    expect(screen.getByText('New Result')).toBeInTheDocument()
  })

  it('renders bounded error and empty states', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'failed' }, 500))
      .mockResolvedValueOnce(mockJSONResponse({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

    await screen.findByRole('heading', { name: '命令审计不可用' })
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await screen.findByText('没有匹配的命令审计')
  })
})
