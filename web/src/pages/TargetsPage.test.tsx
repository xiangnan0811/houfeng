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
    execution_monitoring_instance_labels: ['edge'],
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

function renderTargets(path = '/targets') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/targets" element={<TargetsPage />} />
        <Route path="/targets/:targetId" element={<div>target detail</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

function listFetch(records: ReturnType<typeof targetRecord>[]) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url === '/api/targets' && init?.method !== 'POST') {
      return mockJSONResponse(records)
    }
    if (url.includes('/runtime/')) {
      return mockJSONResponse(records[0] ?? targetRecord())
    }
    if (url.includes('/lifecycle-review')) {
      return mockJSONResponse({ dependency_impacts: [], preview_digest: 'target-digest' })
    }
    return mockJSONResponse({ error: `unexpected ${url}` }, 500)
  })
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
            execution_monitoring_instance_labels: ['edge', 'core'],
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
      expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument(),
    )

    expect(screen.getByRole('heading', { name: '入口探测' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建目标' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '服务入口支撑' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '组合决策' })).not.toBeInTheDocument()
    expect(screen.queryByText('探测 · PROBES')).not.toBeInTheDocument()
    expect(document.querySelector('.page-eyebrow')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    expect(within(createDrawer).queryByText('目标创建')).not.toBeInTheDocument()
    expect(within(createDrawer).queryByText('Group')).not.toBeInTheDocument()
    expect(createDrawer.querySelector('.target-create-drawer__eyebrow')).toBeNull()
    expect(within(createDrawer).queryAllByRole('heading', { name: '创建目标' })).toHaveLength(1)
    expect(within(createDrawer).getByLabelText('分组')).toBeInTheDocument()
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('目标类型'), { target: { value: 'service' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(within(createDrawer).getByLabelText('基础端口'), { target: { value: '443' } })
    fireEvent.change(within(createDrawer).getByLabelText('执行监控实例标签'), { target: { value: 'edge, core' } })
    fireEvent.change(within(createDrawer).getByLabelText('运行状态'), { target: { value: '启用' } })
    fireEvent.change(within(createDrawer).getByLabelText('目标标签'), { target: { value: 'public' } })
    fireEvent.change(within(createDrawer).getByLabelText('备注'), { target: { value: 'primary blog' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

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
        execution_monitoring_instance_labels: ['edge', 'core'],
        run_status: '启用',
        group: '',
        labels: ['public'],
        note: 'primary blog',
      }),
    })
  })

  it('keeps target creation errors inside the create drawer', async () => {
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
      expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

    expect(within(createDrawer).getByText('执行监控实例标签至少需要填写一个。')).toBeInTheDocument()
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
      expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(within(createDrawer).getByLabelText('基础端口'), { target: { value: 'abc' } })
    fireEvent.change(within(createDrawer).getByLabelText('执行监控实例标签'), { target: { value: 'edge' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

    expect(within(createDrawer).getByText('基础端口必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('resets stale create drawer state when cancelled from the drawer', async () => {
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
    let createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Stale target' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'stale.example.com' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

    expect(within(createDrawer).getByText('执行监控实例标签至少需要填写一个。')).toBeInTheDocument()

    fireEvent.click(within(createDrawer).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '创建目标' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    createDrawer = screen.getByRole('dialog', { name: '创建目标' })

    expect(within(createDrawer).queryByText('执行监控实例标签至少需要填写一个。')).not.toBeInTheDocument()
    expect(within(createDrawer).getByLabelText('目标名称')).toHaveValue('')
    expect(within(createDrawer).getByLabelText('主机地址')).toHaveValue('')
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
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(within(createDrawer).getByLabelText('执行监控实例标签'), { target: { value: 'edge' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(within(createDrawer).getByText('target already exists')).toBeInTheDocument())
    expect(screen.getByText('Existing API')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '入口探测' })).toBeInTheDocument()
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
      expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(within(createDrawer).getByLabelText('执行监控实例标签'), { target: { value: 'edge' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

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
            execution_monitoring_instance_labels: ['edge'],
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

  it('ignores a late target creation response after the create drawer is closed', async () => {
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
      expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    let createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.change(within(createDrawer).getByLabelText('目标名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(createDrawer).getByLabelText('主机地址'), { target: { value: 'blog.example.com' } })
    fireEvent.change(within(createDrawer).getByLabelText('执行监控实例标签'), { target: { value: 'edge' } })
    fireEvent.click(within(createDrawer).getByRole('button', { name: '创建目标' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(within(createDrawer).getByRole('button', { name: '正在创建…' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    expect(screen.queryByRole('dialog', { name: '创建目标' })).not.toBeInTheDocument()

    await act(async () => {
      createResponse.resolve(
        mockJSONResponse(
          {
            target_id: 'tg_late',
            name: 'Late Blog',
            target_type: 'service',
            host: 'late.example.com',
            execution_monitoring_instance_labels: ['edge'],
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
    expect(screen.getByRole('button', { name: '新建第一个目标' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '新建第一个目标' }))
    createDrawer = screen.getByRole('dialog', { name: '创建目标' })

    expect(within(createDrawer).queryByText('Late Blog')).not.toBeInTheDocument()
    expect(screen.queryByText('target detail route')).not.toBeInTheDocument()
    expect(within(createDrawer).queryByText('执行监控实例标签至少需要填写一个。')).not.toBeInTheDocument()
    expect(within(createDrawer).getByLabelText('目标名称')).toHaveValue('')
    expect(within(createDrawer).getByRole('button', { name: '创建目标' })).toBeEnabled()
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
  })

  it('uses abnormal=1 from Dashboard deep links as the initial target filter', async () => {
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
      <MemoryRouter initialEntries={['/targets?abnormal=1']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Failing API')).toBeInTheDocument())

    expect(screen.queryByText('Healthy API')).not.toBeInTheDocument()
  })

  it('focuses coverage-gap targets from the quick-view tab instead of a header count', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({
          target_id: 'tg_covered',
          name: 'Covered API',
          execution_monitoring_instance_labels: ['edge'],
        }),
        targetRecord({
          target_id: 'tg_gap',
          name: 'Coverage Gap API',
          execution_monitoring_instance_labels: [],
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

    await waitFor(() => expect(screen.getByText('Coverage Gap API')).toBeInTheDocument())
    expect(screen.getByText('Covered API')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /覆盖缺口/ }))

    await waitFor(() => expect(screen.queryByText('Covered API')).not.toBeInTheDocument())
    expect(screen.getByText('Coverage Gap API')).toBeInTheDocument()
  })

  it('uses run_status=暂停 from Dashboard deep links as the initial target filter', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({ target_id: 'tg_enabled', name: 'Enabled API' }),
        targetRecord({
          target_id: 'tg_paused',
          name: 'Paused API',
          run_status: '暂停',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets?run_status=暂停']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Paused API')).toBeInTheDocument())

    expect(screen.queryByText('Enabled API')).not.toBeInTheDocument()
  })

  it('uses run_status=已归档 from Dashboard deep links as the initial target filter', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      mockJSONResponse([
        targetRecord({ target_id: 'tg_enabled', name: 'Enabled API' }),
        targetRecord({
          target_id: 'tg_archived',
          name: 'Archived API',
          run_status: '已归档',
        }),
      ]),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets?run_status=已归档']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Archived API')).toBeInTheDocument())

    expect(screen.queryByText('Enabled API')).not.toBeInTheDocument()
  })

  it('filters by health via the FilterSelect', async () => {
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

    fireEvent.change(screen.getByLabelText('健康'), { target: { value: '告警' } })

    await waitFor(() =>
      expect(screen.queryByText('Healthy API')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Failing API')).toBeInTheDocument()
  })

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

  it('does not navigate when a row checkbox is clicked', async () => {
    vi.stubGlobal('fetch', listFetch([targetRecord({ target_id: 'tg_check', name: 'Blog' })]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    expect(screen.queryByText('target detail')).not.toBeInTheDocument()
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByLabelText('选择 Blog')).toBeChecked()
  })

  it('keeps the persistent create target dialog open on Escape and restores focus after explicit close', async () => {
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

    const trigger = screen.getByRole('button', { name: '新建目标' })
    trigger.focus()
    expect(screen.queryByRole('dialog', { name: '创建目标' })).not.toBeInTheDocument()

    fireEvent.click(trigger)
    let dialog = screen.getByRole('dialog', { name: '创建目标' })

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(dialog).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('dialog', { name: '创建目标' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: '新建目标' }))
    dialog = screen.getByRole('dialog', { name: '创建目标' })

    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '创建目标' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

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

    const identity = screen.getByText('API A').closest('.targets-table__identity')
    expect(identity).not.toBeNull()
    expect(identity?.querySelector('.status-glyph')).toBeNull()
    expect(within(identity as HTMLElement).queryByLabelText(/运行正常/)).not.toBeInTheDocument()
    expect(screen.queryByText('运行正常')).not.toBeInTheDocument()

    const polylines = document.querySelectorAll('polyline')
    expect(polylines.length).toBe(2)

    const trendCells = document.querySelectorAll('.targets-table__trends')
    expect(trendCells.length).toBe(2)

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

    const trendCells = document.querySelectorAll('.targets-table__trends')
    expect(trendCells.length).toBe(1)
    const firstTrendCell = trendCells[0]
    if (!firstTrendCell) throw new Error('targets table must render the trend cell')
    expect(firstTrendCell.textContent).toContain('—')
  })

  it('disables batch actions at zero selection and opens the action group after select-all', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_001', name: 'Blog' }),
      targetRecord({ target_id: 'tg_002', name: 'Cache' }),
    ]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    expect(screen.getByRole('button', { name: '批量操作' })).toBeDisabled()
    expect(screen.queryByRole('columnheader', { name: '操作' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '快速编辑标签' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('全选可见目标'))
    expect(screen.getByLabelText('选择 Blog')).toBeChecked()
    expect(screen.getByLabelText('选择 Cache')).toBeChecked()
    expect(screen.getByRole('button', { name: '批量操作' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '批量操作' })).toHaveTextContent('(2)')

    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    const actions = screen.getByRole('group', { name: '批量操作' })
    expect(within(actions).getByRole('button', { name: '进入维护' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '退出维护' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '暂停' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '恢复' })).toBeInTheDocument()
    expect(within(actions).getByRole('button', { name: '归档' })).toBeInTheDocument()
    expect(within(actions).queryByRole('button', { name: '恢复到暂停' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem')).not.toBeInTheDocument()
  })

  it('runs batch 进入维护 on selected rows', async () => {
    const fetchMock = listFetch([targetRecord({ target_id: 'tg_001', name: 'Blog' })])
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/targets/tg_001/runtime/enter-maintenance',
        expect.objectContaining({ method: 'POST' }),
      ),
    )
  })

  it('rejects batch 进入维护 on shared targets with error directing to detail page', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        return mockJSONResponse([targetRecord({ target_id: 'tg_shared_batch', name: 'Shared Target' })])
      }
      if (url.includes('/lifecycle-review')) {
        return mockJSONResponse({
          dependency_impacts: [
            { object_type: 'target', object_id: 'tg_shared_batch', vps_id: 'vps_001', vps_lifecycle_status: 'active', relation_type: 'service', relation_id: 'svc_001', relation_status: 'active', classification: 'current' },
            { object_type: 'target', object_id: 'tg_shared_batch', vps_id: 'vps_002', vps_lifecycle_status: 'active', relation_type: 'service', relation_id: 'svc_002', relation_status: 'active', classification: 'current' },
          ],
          preview_digest: 'batch-shared-digest',
        })
      }
      if (url.includes('/runtime/')) {
        return mockJSONResponse(targetRecord({ target_id: 'tg_shared_batch' }))
      }
      return mockJSONResponse({ error: 'not found' }, 404)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()

    await waitFor(() => expect(screen.getByText('Shared Target')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Shared Target'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() => expect(screen.getByText('1/1 个目标失败')).toBeInTheDocument())
    expect(fetchMock).not.toHaveBeenCalledWith(
      '/api/targets/tg_shared_batch/runtime/enter-maintenance',
      expect.anything(),
    )
  })

  it('confirms batch pause with the batch confirmation modal', async () => {
    const fetchMock = listFetch([targetRecord({ target_id: 'tg_pause', name: 'Blog' })])
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '暂停' }))

    expect(screen.getByRole('alertdialog', { name: '确认批量暂停目标' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认批量暂停' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/targets/tg_pause/runtime/pause',
        expect.objectContaining({ method: 'POST' }),
      ),
    )
  })

  it('confirms batch archive with the batch confirmation modal', async () => {
    const fetchMock = listFetch([targetRecord({ target_id: 'tg_archive', name: 'Blog' })])
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '归档' }))

    expect(screen.getByRole('alertdialog', { name: '确认批量归档目标' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认批量归档' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/targets/tg_archive/runtime/archive',
        expect.objectContaining({ method: 'POST' }),
      ),
    )
  })

  it('permanently drops a selection when the row is filtered out', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_001', name: 'Blog', target_type: 'service' }),
      targetRecord({ target_id: 'tg_002', name: 'Cache', target_type: 'china_reference' }),
    ]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    expect(screen.getByLabelText('选择 Blog')).toBeChecked()

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: 'china_reference' } })
    await waitFor(() => expect(screen.queryByText('Blog')).not.toBeInTheDocument())
    expect(screen.getByText('Cache')).toBeInTheDocument()
    expect(screen.getByLabelText('选择 Cache')).not.toBeChecked()

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: '' } })
    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())
    expect(screen.getByLabelText('选择 Blog')).not.toBeChecked()
    expect(screen.getByLabelText('选择 Cache')).not.toBeChecked()
  })

  it('selects only currently visible rows when using select-all', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_001', name: 'Blog', target_type: 'service' }),
      targetRecord({ target_id: 'tg_002', name: 'Cache', target_type: 'china_reference' }),
    ]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: 'service' } })
    await waitFor(() => expect(screen.queryByText('Cache')).not.toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('全选可见目标'))
    expect(screen.getByLabelText('选择 Blog')).toBeChecked()

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: '' } })
    await waitFor(() => expect(screen.getByText('Cache')).toBeInTheDocument())
    expect(screen.getByLabelText('选择 Blog')).toBeChecked()
    expect(screen.getByLabelText('选择 Cache')).not.toBeChecked()
    expect(screen.getByRole('button', { name: '批量操作' })).toHaveTextContent('(1)')
  })

  it('keeps frozen batch ids while the confirmation dialog is open', async () => {
    const fetchMock = listFetch([
      targetRecord({ target_id: 'tg_001', name: 'Blog', target_type: 'service' }),
      targetRecord({ target_id: 'tg_002', name: 'Cache', target_type: 'china_reference' }),
    ])
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('全选可见目标'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '暂停' }))

    expect(screen.getByRole('alertdialog', { name: '确认批量暂停目标' })).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('类型'), { target: { value: 'service' } })
    await waitFor(() => expect(screen.queryByText('Cache')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '确认批量暂停' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/targets/tg_001/runtime/pause',
        expect.objectContaining({ method: 'POST' }),
      )
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/targets/tg_002/runtime/pause',
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  it('does not reopen the action group after clearing and reselecting', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_001', name: 'Blog' }),
    ]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    expect(screen.getByRole('group', { name: '批量操作' })).toBeInTheDocument()

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    expect(screen.queryByRole('group', { name: '批量操作' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '批量操作' })).toBeDisabled()

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    expect(screen.getByRole('button', { name: '批量操作' })).toBeEnabled()
    expect(screen.queryByRole('group', { name: '批量操作' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
  })

  it('closes the action group on Escape and restores trigger focus', async () => {
    vi.stubGlobal('fetch', listFetch([targetRecord({ target_id: 'tg_001', name: 'Blog' })]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    const trigger = screen.getByRole('button', { name: '批量操作' })
    trigger.focus()
    fireEvent.click(trigger)
    const enterMaintenance = within(screen.getByRole('group', { name: '批量操作' })).getByRole('button', {
      name: '进入维护',
    })
    expect(enterMaintenance).not.toHaveAttribute('tabindex', '-1')

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('group', { name: '批量操作' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('does not treat the action group as an arrow-key menu', async () => {
    vi.stubGlobal('fetch', listFetch([targetRecord({ target_id: 'tg_001', name: 'Blog' })]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    const trigger = screen.getByRole('button', { name: '批量操作' })
    trigger.focus()
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    expect(screen.queryByRole('group', { name: '批量操作' })).not.toBeInTheDocument()
  })

  it('filters 异常, 暂停, 归档, and 覆盖缺口 from the quick-view tabs', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_ok', name: 'Healthy API' }),
      targetRecord({
        target_id: 'tg_alert',
        name: 'Failing API',
        current_health_status: '告警',
      }),
      targetRecord({
        target_id: 'tg_paused',
        name: 'Paused API',
        run_status: '暂停',
      }),
      targetRecord({
        target_id: 'tg_archived',
        name: 'Archived API',
        run_status: '已归档',
      }),
      targetRecord({
        target_id: 'tg_gap',
        name: 'Coverage Gap API',
        execution_monitoring_instance_labels: [],
      }),
    ]))
    renderTargets()

    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    expect(screen.getByText('Failing API')).toBeInTheDocument()
    expect(screen.getByText('Paused API')).toBeInTheDocument()
    expect(screen.getByText('Archived API')).toBeInTheDocument()
    expect(screen.getByText('Coverage Gap API')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /异常/ }))
    await waitFor(() => expect(screen.queryByText('Healthy API')).not.toBeInTheDocument())
    expect(screen.getByText('Failing API')).toBeInTheDocument()
    expect(screen.queryByText('Paused API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /暂停/ }))
    await waitFor(() => expect(screen.getByText('Paused API')).toBeInTheDocument())
    expect(screen.queryByText('Failing API')).not.toBeInTheDocument()
    expect(screen.queryByText('Archived API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /归档/ }))
    await waitFor(() => expect(screen.getByText('Archived API')).toBeInTheDocument())
    expect(screen.queryByText('Paused API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /覆盖缺口/ }))
    await waitFor(() => expect(screen.getByText('Coverage Gap API')).toBeInTheDocument())
    expect(screen.queryByText('Archived API')).not.toBeInTheDocument()
    expect(screen.queryByText('Healthy API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /全部/ }))
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    expect(screen.getByText('Failing API')).toBeInTheDocument()
    expect(screen.getByText('Paused API')).toBeInTheDocument()
    expect(screen.getByText('Archived API')).toBeInTheDocument()
    expect(screen.getByText('Coverage Gap API')).toBeInTheDocument()
  })
})
