import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type * as ApiModule from '../lib/api'
import { listTargetSparklines } from '../lib/api'
import { TargetsPage } from './TargetsPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof ApiModule>()
  return {
    ...actual,
    listTargetSparklines: vi.fn().mockResolvedValue({ targets: {} }),
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

function targetRecord(overrides: Record<string, unknown> = {}) {
  return {
    target_id: 'tg_001',
    name: 'Existing API',
    target_type: 'service',
    host: 'api.example.com',
    base_port: 443,
    execution_node_labels: ['edge'],
    run_status: '启用',
    labels: ['public'],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: '2026-04-26T09:00:00Z',
    last_failure_at: '2026-04-26T08:00:00Z',
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-26T09:05:00Z',
    ...overrides,
  }
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((innerResolve) => {
    resolve = innerResolve
  })
  return { promise, resolve }
}

describe('TargetsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('creates the first target and navigates to its detail page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          {
            target_id: 'tg_new',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            execution_node_labels: ['edge', 'core'],
            run_status: '启用',
            labels: ['public'],
            note: 'primary blog',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-27T09:00:00Z',
            updated_at: '2026-04-27T09:00:00Z',
          },
          201,
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/targets/:targetId" element={<div>target detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument(),
    )

    expect(screen.getAllByText('目标').length).toBeGreaterThanOrEqual(1)

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
    expect(screen.getByText('目标创建')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('目标类型'), { target: { value: 'service' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(screen.getByLabelText('基础端口'), { target: { value: '443' } })
    fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge, core' } })
    fireEvent.change(screen.getByLabelText('运行状态'), { target: { value: '启用' } })
    fireEvent.change(screen.getByLabelText('目标标签'), { target: { value: 'public' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'primary blog' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(screen.getByText('target detail route')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge', 'core'],
        run_status: '启用',
        labels: ['public'],
        note: 'primary blog',
      }),
    })
  })

  it('keeps target creation errors inside the create panel', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    expect(screen.getByText('执行节点标签至少需要填写一个。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('uses Chinese-first validation for base port', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(screen.getByLabelText('基础端口'), { target: { value: 'abc' } })
    fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    expect(screen.getByText('基础端口必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('resets stale create panel state when closed from the header', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([targetRecord()]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Existing API')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Stale target' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'stale.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    expect(screen.getByText('执行节点标签至少需要填写一个。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    expect(screen.queryByLabelText('目标名称')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))

    expect(screen.queryByText('执行节点标签至少需要填写一个。')).not.toBeInTheDocument()
    expect(screen.getByLabelText('目标名称')).toHaveValue('')
    expect(screen.getByLabelText('主机地址')).toHaveValue('')
  })

  it('keeps failed target creation API errors local while preserving the loaded list', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([targetRecord()]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'target already exists' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Existing API')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(screen.getByText('target already exists')).toBeInTheDocument())
    expect(screen.getByText('Existing API')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '目标列表' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does not navigate from a late target creation response after leaving the page', async () => {
    const createResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockReturnValueOnce(createResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Link to="/other">离开目标页</Link>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/other" element={<div>left targets route</div>} />
          <Route path="/targets/:targetId" element={<div>target detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    fireEvent.click(screen.getByRole('link', { name: '离开目标页' }))
    await waitFor(() => expect(screen.getByText('left targets route')).toBeInTheDocument())

    await act(async () => {
      createResponse.resolve(
        mockJSONResponse(
          {
            target_id: 'tg_new',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-27T09:00:00Z',
            updated_at: '2026-04-27T09:00:00Z',
          },
          201,
        ),
      )
      await createResponse.promise
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(screen.getByText('left targets route')).toBeInTheDocument()
    expect(screen.queryByText('target detail route')).not.toBeInTheDocument()
  })

  it('ignores a late target creation response after the create panel is closed', async () => {
    const createResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockReturnValueOnce(createResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/targets/:targetId" element={<div>target detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))
    fireEvent.change(screen.getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(screen.getByLabelText('执行节点标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: '正在创建…' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    expect(screen.queryByLabelText('目标名称')).not.toBeInTheDocument()

    await act(async () => {
      createResponse.resolve(
        mockJSONResponse(
          {
            target_id: 'tg_late',
            name: 'Late Blog',
            target_type: 'service',
            host: 'late.example.com',
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-27T09:00:00Z',
            updated_at: '2026-04-27T09:00:00Z',
          },
          201,
        ),
      )
      await createResponse.promise
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(screen.queryByText('target detail route')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建第一个目标' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '创建第一个目标' }))

    expect(screen.queryByText('Late Blog')).not.toBeInTheDocument()
    expect(screen.queryByText('target detail route')).not.toBeInTheDocument()
    expect(screen.queryByText('执行节点标签至少需要填写一个。')).not.toBeInTheDocument()
    expect(screen.getByLabelText('目标名称')).toHaveValue('')
    expect(screen.getByRole('button', { name: '创建目标' })).toBeEnabled()
  })

  it('renders runtime quick actions by target run status and restores archived targets to paused', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            target_id: 'tg_enabled',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: ['public'],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-26T09:00:00Z',
            last_failure_at: '2026-04-26T08:00:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-26T09:05:00Z',
          },
          {
            target_id: 'tg_archived',
            name: 'Legacy API',
            target_type: 'service',
            host: 'legacy.example.com',
            execution_node_labels: ['edge'],
            run_status: '已归档',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-26T09:00:00Z',
            last_failure_at: '2026-04-26T08:00:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-26T09:05:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archived',
          name: 'Legacy API',
          target_type: 'service',
          host: 'legacy.example.com',
          execution_node_labels: ['edge'],
          run_status: '暂停',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-26T09:00:00Z',
          last_failure_at: '2026-04-26T08:00:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-26T09:10:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const enabledRow = screen.getByText('Blog').closest('tr')
    const archivedRow = screen.getByText('Legacy API').closest('tr')
    expect(enabledRow).not.toBeNull()
    expect(archivedRow).not.toBeNull()

    expect(within(enabledRow!).getByRole('button', { name: '进入维护' })).toBeInTheDocument()
    expect(within(enabledRow!).getByRole('button', { name: '暂停' })).toBeInTheDocument()
    expect(within(enabledRow!).getByRole('button', { name: '归档' })).toBeInTheDocument()
    expect(within(archivedRow!).queryByRole('button', { name: '归档' })).not.toBeInTheDocument()
    expect(within(archivedRow!).getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument()

    fireEvent.click(within(archivedRow!).getByRole('button', { name: '恢复到暂停' }))

    await waitFor(() =>
      expect(within(archivedRow!).getByRole('button', { name: '恢复' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_archived/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })


  it('keeps pause confirmation open and shows row-local error when pause API fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_pause_fail', name: 'Blog' })]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'pause failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))

    await waitFor(() => expect(screen.getByText('pause failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_pause_fail/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps archive confirmation open and shows row-local error when archive API fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_archive_fail', name: 'Blog' })]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'archive failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(screen.getByText('archive failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认归档目标' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_archive_fail/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps a target confirmation open while a different target restores focus after a light action succeeds', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({ target_id: 'tg_pause', name: 'Blog' }),
          targetRecord({ target_id: 'tg_maintenance', name: 'Storefront' }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_maintenance',
            name: 'Storefront',
            run_status: '维护中',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const pauseRow = screen.getByText('Blog').closest('tr')
    const maintenanceRow = screen.getByText('Storefront').closest('tr')
    expect(pauseRow).not.toBeNull()
    expect(maintenanceRow).not.toBeNull()

    fireEvent.click(within(pauseRow!).getByRole('button', { name: '暂停' }))
    fireEvent.click(within(maintenanceRow!).getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(within(maintenanceRow!).getByRole('button', { name: '退出维护' })).toBeInTheDocument(),
    )
    await waitFor(() =>
      expect(within(maintenanceRow!).getByRole('button', { name: '退出维护' })).toHaveFocus(),
    )
    // v2 layout: confirmation card renders below the DataTable as a row-overlay
    // sibling, no longer inside the row's <tr>. The behavioural guarantee
    // (Blog's pause confirmation persists while Storefront completes its light
    // action) is asserted at page scope rather than within the table row.
    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
  })

  it('keeps a pause confirmation open when another action succeeds on the same target', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_same', name: 'Blog' })]))
      .mockResolvedValueOnce(
        mockJSONResponse(targetRecord({ target_id: 'tg_same', name: 'Blog', run_status: '维护中' })),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() => expect(screen.getByRole('button', { name: '退出维护' })).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
  })

  it('uses an inline stateful confirmation before pausing a target from the list', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([targetRecord({ target_id: 'tg_pause', name: 'Blog' })]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          targetRecord({ target_id: 'tg_pause', name: 'Blog', run_status: '暂停' }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：目标运行状态为启用或维护中。')).toBeInTheDocument()
    expect(screen.getByText('操作后：目标运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText('会停止该目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。'),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 ProbeItem 配置。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '暂停' })).toHaveFocus())
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_pause/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('uses an inline stateful confirmation before archiving a target from the list', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([targetRecord({ target_id: 'tg_archive', name: 'Blog' })]))
      .mockResolvedValueOnce(mockJSONResponse(targetRecord({ target_id: 'tg_archive', name: 'Blog', run_status: '已归档' })))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    expect(screen.getByRole('alertdialog', { name: '确认归档目标' })).toBeInTheDocument()
    expect(screen.getByText('当前：目标仍在当前工作集中。')).toBeInTheDocument()
    expect(screen.getByText('操作后：目标退出当前工作集，运行状态变为已归档。')).toBeInTheDocument()
    expect(screen.getByText('归档后不会继续作为活跃目标参与观测、异常判定或通知。')).toBeInTheDocument()
    expect(
      screen.getByText('不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。'),
    ).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '归档' })).toHaveFocus())
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复到暂停' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_archive/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })


  it('quickly edits target labels from the list and preserves the existing note', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({
            target_id: 'tg_001',
            name: 'Blog',
            labels: ['公开'],
            note: '现网入口',
          }),
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_001',
            name: 'Blog',
            labels: ['alpha', 'beta'],
            note: '现网入口',
            updated_at: '2026-04-27T09:30:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByLabelText('标签'), {
      target: { value: 'alpha, beta, alpha, beta' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '保存标签' }))

    await waitFor(() => expect(within(row!).getByText('标签：alpha · beta')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': '"2026-04-26T09:05:00Z"',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        labels: ['alpha', 'beta'],
        note: '现网入口',
      }),
    })
  })

  it('keeps target label edit failures local to the row', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({
            target_id: 'tg_001',
            name: 'Blog',
            labels: ['公开'],
            note: '现网入口',
          }),
          targetRecord({
            target_id: 'tg_002',
            name: 'Cache',
            labels: ['内部'],
            note: '',
          }),
        ]),
      )
      .mockResolvedValueOnce(mockJSONResponse({ error: 'metadata failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const blogRow = screen.getByText('Blog').closest('tr')
    const cacheRow = screen.getByText('Cache').closest('tr')
    expect(blogRow).not.toBeNull()
    expect(cacheRow).not.toBeNull()

    fireEvent.click(within(blogRow!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(blogRow!).getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(within(blogRow!).getByRole('button', { name: '保存标签' }))

    await waitFor(() =>
      expect(within(blogRow!).getByRole('alert')).toHaveTextContent('metadata failed'),
    )
    expect(within(blogRow!).getByRole('button', { name: '保存标签' })).toBeInTheDocument()
    expect(within(cacheRow!).queryByText('metadata failed')).not.toBeInTheDocument()
  })

  it('blocks opening another row label editor while a metadata save is in flight', async () => {
    const saveResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({
            target_id: 'tg_001',
            name: 'Blog',
            labels: ['公开'],
            note: '现网入口',
          }),
          targetRecord({
            target_id: 'tg_002',
            name: 'Cache',
            labels: ['内部'],
            note: '',
          }),
        ]),
      )
      .mockReturnValueOnce(saveResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const blogRow = screen.getByText('Blog').closest('tr')
    const cacheRow = screen.getByText('Cache').closest('tr')
    expect(blogRow).not.toBeNull()
    expect(cacheRow).not.toBeNull()

    fireEvent.click(within(blogRow!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(blogRow!).getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(within(blogRow!).getByRole('button', { name: '保存标签' }))

    await waitFor(() =>
      expect(within(cacheRow!).getByRole('button', { name: '快速编辑标签' })).toBeDisabled(),
    )

    fireEvent.click(within(cacheRow!).getByRole('button', { name: '快速编辑标签' }))
    expect(within(cacheRow!).queryByLabelText('标签')).not.toBeInTheDocument()

    await act(async () => {
      saveResponse.resolve(
        mockJSONResponse({
          ...targetRecord({
            target_id: 'tg_001',
            name: 'Blog',
            labels: ['alpha', 'beta'],
            note: '现网入口',
          }),
          updated_at: '2026-04-27T10:00:00Z',
        }),
      )
    })

    await waitFor(() =>
      expect(within(blogRow!).queryByRole('button', { name: '保存标签' })).not.toBeInTheDocument(),
    )
    expect(within(cacheRow!).getByRole('button', { name: '快速编辑标签' })).toBeEnabled()
  })

  it('preserves saved labels when a later runtime response returns stale metadata', async () => {
    const metadataResponse = deferred<Response>()
    const runtimeResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '启用',
            labels: ['公开'],
            note: '现网入口',
          }),
        ]),
      )
      .mockImplementationOnce(() => metadataResponse.promise)
      .mockImplementationOnce(() => runtimeResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '保存标签' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    fireEvent.click(within(row!).getByRole('button', { name: '进入维护' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))

    await act(async () => {
      metadataResponse.resolve(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '启用',
            labels: ['alpha', 'beta'],
            note: '现网入口',
            updated_at: '2026-04-27T10:00:00Z',
          }),
        ),
      )
    })

    await waitFor(() => expect(within(row!).getByText('标签：alpha · beta')).toBeInTheDocument())

    await act(async () => {
      runtimeResponse.resolve(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '维护中',
            labels: ['公开'],
            note: '过期备注',
            updated_at: '2026-04-27T10:05:00Z',
          }),
        ),
      )
    })

    await waitFor(() => expect(within(row!).getByText('维护中')).toBeInTheDocument())
    expect(within(row!).getByText('标签：alpha · beta')).toBeInTheDocument()
    expect(within(row!).queryByText('标签：公开')).not.toBeInTheDocument()
  })

  it('preserves a newer runtime status when a later metadata response returns stale runtime fields', async () => {
    const metadataResponse = deferred<Response>()
    const runtimeResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '启用',
            labels: ['公开'],
            note: '现网入口',
          }),
        ]),
      )
      .mockImplementationOnce(() => metadataResponse.promise)
      .mockImplementationOnce(() => runtimeResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '保存标签' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    fireEvent.click(within(row!).getByRole('button', { name: '进入维护' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))

    await act(async () => {
      runtimeResponse.resolve(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '维护中',
            labels: ['公开'],
            note: '现网入口',
            updated_at: '2026-04-27T10:05:00Z',
          }),
        ),
      )
    })

    await waitFor(() => expect(within(row!).getByText('维护中')).toBeInTheDocument())

    await act(async () => {
      metadataResponse.resolve(
        mockJSONResponse(
          targetRecord({
            target_id: 'tg_overlap',
            name: 'Blog',
            run_status: '启用',
            labels: ['alpha', 'beta'],
            note: '现网入口',
            updated_at: '2026-04-27T10:10:00Z',
          }),
        ),
      )
    })

    await waitFor(() => expect(within(row!).getByText('标签：alpha · beta')).toBeInTheDocument())
    expect(within(row!).getByText('维护中')).toBeInTheDocument()
    expect(within(row!).queryByText('启用')).not.toBeInTheDocument()
  })

  it('filters the list by target type via the FilterBar select', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({ target_id: 'tg_service', name: 'Service Blog', target_type: 'service' }),
        targetRecord({
          target_id: 'tg_china',
          name: 'China Reference',
          target_type: 'china_reference',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Service Blog')).toBeInTheDocument())
    expect(screen.getByText('China Reference')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: 'service' } })

    await waitFor(() =>
      expect(screen.queryByText('China Reference')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Service Blog')).toBeInTheDocument()
    expect(screen.getByText('类型: service')).toBeInTheDocument()
  })

  it('toggles "仅看异常" and clears all filters via FilterBar', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({
          target_id: 'tg_normal',
          name: 'Healthy API',
          current_health_status: '正常',
        }),
        targetRecord({
          target_id: 'tg_alert',
          name: 'Failing API',
          current_health_status: '告警',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('switch', { name: '仅看异常' }))

    await waitFor(() =>
      expect(screen.queryByText('Healthy API')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Failing API')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: '移除筛选 仅看异常' }),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空所有' }))

    await waitFor(() =>
      expect(screen.getByText('Healthy API')).toBeInTheDocument(),
    )
    expect(screen.getByText('Failing API')).toBeInTheDocument()
  })

  it('cancels target quick label editing without sending PATCH', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({
          target_id: 'tg_001',
          name: 'Blog',
          labels: ['公开'],
          note: '现网入口',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    fireEvent.change(within(row!).getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(within(row!).getByRole('button', { name: '取消' }))

    expect(within(row!).queryByLabelText('标签')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  // ─── PR1: DataTable 迁移新增覆盖 ──────────────────────────────────────────

  it('navigates to the target detail page when a row is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({ target_id: 'tg_click', name: 'Blog' }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/targets/:targetId" element={<div>target detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(row!)
    await waitFor(() => expect(screen.getByText('target detail')).toBeInTheDocument())
  })

  it('does not navigate when a row action button is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({ target_id: 'tg_actions', name: 'Blog' }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/targets/:targetId" element={<div>target detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const row = screen.getByText('Blog').closest('tr')
    expect(row).not.toBeNull()

    fireEvent.click(within(row!).getByRole('button', { name: '快速编辑标签' }))
    // Inline editor opened on the same row, navigation must NOT have triggered.
    expect(within(row!).getByRole('textbox', { name: '标签' })).toBeInTheDocument()
    expect(screen.queryByText('target detail')).not.toBeInTheDocument()
  })

  it('toggles the create target form panel via the section heading button', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(mockJSONResponse([targetRecord()])),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '新建目标' })).toBeInTheDocument(),
    )

    expect(screen.queryByText('目标创建')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    expect(screen.getByText('目标创建')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    expect(screen.queryByText('目标创建')).not.toBeInTheDocument()
  })

  // ─── PR2: sparkline strip ──────────────────────────────────────────────

  it('renders latency sparkline in trends column when sparklines data is loaded', async () => {
    const make24 = (base: number, jitter: number) =>
      Array.from({ length: 24 }, (_, i) => base + Math.sin(i * 0.5) * jitter)
    const mocked = vi.mocked(listTargetSparklines)
    mocked.mockResolvedValueOnce({
      targets: {
        tg_001: { latency: make24(25, 10) },
        tg_002: { latency: make24(150, 30) },
      },
    })

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({ target_id: 'tg_001', name: 'API A' }),
          targetRecord({ target_id: 'tg_002', name: 'API B' }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('API A')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('API B')).toBeInTheDocument())

    // Each Sparkline with >1 point renders a <polyline> element
    const polylines = document.querySelectorAll('polyline')
    expect(polylines.length).toBe(2)

    // Each row should have the trend column visible
    const trendCells = document.querySelectorAll('.targets-table__trends')
    expect(trendCells.length).toBe(2)

    // Trend values should show latency in ms (at least one value per row)
    const msValues = screen.getAllByText(/\.\d ms/)
    expect(msValues.length).toBeGreaterThanOrEqual(2)
  })

  it('shows placeholder dash in trends column when sparklines data is missing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValueOnce(
        mockJSONResponse([
          targetRecord({ target_id: 'tg_no_data', name: 'New Target' }),
        ]),
      ),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('New Target')).toBeInTheDocument())

    // Default mock for listTargetSparklines returns { targets: {} },
    // so the row should show placeholder dash in trends column.
    const trendCells = document.querySelectorAll('.targets-table__trends')
    expect(trendCells.length).toBe(1)
    expect(trendCells[0].textContent).toContain('—')
  })
})
