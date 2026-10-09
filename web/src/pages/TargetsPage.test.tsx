import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type * as ApiModule from '../lib/api'
import { listTargetSparklines } from '../lib/api'
import { targetObservationFixture } from '../lib/targetObservationFixture'
import { VISIBLE_REFRESH_INTERVAL_MS } from '../lib/useVisibleRefresh'
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

function observationFreshness(overrides: Record<string, unknown> = {}) {
  return {
    state: 'fresh',
    evaluated_at: '2026-04-26T09:05:00Z',
    enabled_probe_count: 1,
    fresh_probe_count: 1,
    pending_probe_count: 0,
    stale_probe_count: 0,
    probes: [],
    ...overrides,
  }
}

function targetRecord(overrides: Record<string, unknown> = {}) {
  const { observation_freshness: freshnessOverride, ...rest } = overrides
  const targetId = typeof rest.target_id === 'string' ? rest.target_id : 'tg_001'
  const runStatus = typeof rest.run_status === 'string' ? rest.run_status : '启用'
  const lifecycleStatus = typeof rest.lifecycle_status === 'string' ? rest.lifecycle_status : 'active'
  const enabledProbeCount = typeof rest.enabled_probe_count === 'number' ? rest.enabled_probe_count : 1
  const lastSuccessAt = 'last_success_at' in rest ? rest.last_success_at as string | undefined : '2026-04-26T09:00:00Z'
  const lastFailureAt = 'last_failure_at' in rest ? rest.last_failure_at as string | undefined : '2026-04-26T08:00:00Z'
  return {
    target_id: targetId,
    name: 'Existing API',
    target_type: 'service',
    host: 'api.example.com',
    base_port: 443,
    execution_monitoring_instance_labels: ['edge'],
    lifecycle_status: lifecycleStatus,
    run_status: runStatus,
    labels: ['public'],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: enabledProbeCount,
    matching_executor_count: 1,
    last_success_at: lastSuccessAt,
    last_failure_at: lastFailureAt,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-26T09:05:00Z',
    observation_freshness: freshnessOverride ?? targetObservationFixture({
      target_id: targetId,
      run_status: runStatus,
      lifecycle_status: lifecycleStatus,
      evaluated_at: '2026-04-26T09:05:00Z',
      enabled_probe_count: enabledProbeCount,
      last_success_at: lastSuccessAt,
      last_failure_at: lastFailureAt,
    }),
    ...rest,
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
      return mockJSONResponse(records.filter((record) => record.lifecycle_status !== 'retired'))
    }
    if (url === '/api/targets?scope=retired' && init?.method !== 'POST') {
      return mockJSONResponse(records.filter((record) => record.lifecycle_status === 'retired'))
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
    vi.useRealTimers()
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    })
  })

  it('creates the first target and navigates to its detail page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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
            enabled_probe_count: 1,
            matching_executor_count: 1,
            labels: ['public'],
            note: 'primary blog',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-27T09:00:00Z',
            updated_at: '2026-04-27T09:00:00Z',
            observation_freshness: observationFreshness({
              state: 'pending',
              evaluated_at: '2026-04-27T09:00:00Z',
              fresh_probe_count: 0,
              pending_probe_count: 1,
              stale_probe_count: 0,
            }),
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
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/targets', {
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
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the create form open when Enter is pressed inside a label field', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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
    const executionLabels = within(createDrawer).getByLabelText('执行监控实例标签')
    fireEvent.change(executionLabels, { target: { value: ' edge ，core,, ' } })

    expect(fireEvent.keyDown(executionLabels, { key: 'Enter' })).toBe(false)
    expect(executionLabels).toHaveValue('edge, core')
    const targetLabels = within(createDrawer).getByLabelText('目标标签')
    fireEvent.change(targetLabels, { target: { value: 'public' } })
    expect(fireEvent.keyDown(targetLabels, { key: 'Enter' })).toBe(false)
    expect(screen.getByRole('dialog', { name: '创建目标' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('previews which monitoring instances will run the target and guides unlabeled setups', async () => {
    const instances = [
      { monitoring_instance_id: 'mi_tokyo', display_name: 'tokyo-edge-01', labels: ['jp', 'edge'] },
      { monitoring_instance_id: 'mi_paris', display_name: 'paris-01', labels: ['eu'] },
    ]
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith('/api/monitoring-instances')) return mockJSONResponse(instances)
      return mockJSONResponse([])
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    const executionLabels = within(createDrawer).getByLabelText('执行监控实例标签')
    fireEvent.focus(executionLabels)
    expect(await within(createDrawer).findByRole('button', { name: 'jp' })).toBeInTheDocument()

    fireEvent.change(executionLabels, { target: { value: 'edge, eu' } })
    expect(within(createDrawer).getByText('将由 tokyo-edge-01、paris-01 执行。')).toBeInTheDocument()
    fireEvent.change(executionLabels, { target: { value: 'us' } })
    expect(within(createDrawer).getByText('目前没有监控实例带这些标签，创建后暂时不会被探测。')).toBeInTheDocument()
    // 已有标签但都不匹配时，同样可以显式给某台实例加上第一个执行标签。
    const assign = within(createDrawer).getByRole('group', { name: '给监控实例加标签' })
    expect(within(assign).getAllByRole('button').map((button) => button.textContent)).toEqual([
      '给 tokyo-edge-01 加上「us」',
      '给 paris-01 加上「us」',
    ])
    // 暂停目标不会分配给任何实例。
    fireEvent.change(executionLabels, { target: { value: 'edge' } })
    fireEvent.change(within(createDrawer).getByLabelText('运行状态'), { target: { value: '暂停' } })
    expect(within(createDrawer).getByText('运行状态为暂停，创建后暂时不会被探测。')).toBeInTheDocument()
    fireEvent.change(executionLabels, { target: { value: 'us' } })
    expect(within(createDrawer).queryByRole('group', { name: '给监控实例加标签' })).not.toBeInTheDocument()
  })

  it('does not name paused, retired or archived monitoring instances as executors', async () => {
    const instances = [
      { monitoring_instance_id: 'mi_paused', display_name: 'paused-01', labels: ['edge'], monitoring_status: '暂停', lifecycle_status: '已接入' },
      { monitoring_instance_id: 'mi_retired', display_name: 'retired-01', labels: ['edge'], monitoring_status: '启用', lifecycle_status: '已退役' },
      { monitoring_instance_id: 'mi_archived', display_name: 'archived-01', labels: ['edge'], monitoring_status: '启用', lifecycle_status: '已接入', archived_at: '2026-10-01T00:00:00Z' },
      { monitoring_instance_id: 'mi_live', display_name: 'live-01', labels: ['edge'], monitoring_status: '维护中', lifecycle_status: '已接入' },
    ]
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => (
      String(input).startsWith('/api/monitoring-instances') ? mockJSONResponse(instances) : mockJSONResponse([])
    )))

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    const executionLabels = within(createDrawer).getByLabelText('执行监控实例标签')
    fireEvent.focus(executionLabels)
    await within(createDrawer).findByRole('button', { name: 'edge' })
    fireEvent.change(executionLabels, { target: { value: 'edge' } })
    expect(within(createDrawer).getByText('将由 live-01 执行。')).toBeInTheDocument()
  })

  it('explains that existing monitoring instances still need labels', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith('/api/monitoring-instances')) {
        return mockJSONResponse([{ monitoring_instance_id: 'mi_tokyo', display_name: 'tokyo-edge-01', labels: [] }])
      }
      return mockJSONResponse([])
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    fireEvent.focus(within(createDrawer).getByLabelText('执行监控实例标签'))
    const guidance = await within(createDrawer).findByText(/现有 1 台监控实例都还没有标签/)
    expect(within(guidance).getByRole('link', { name: '监控实例' })).toHaveAttribute('href', '/monitoring')
  })

  it('adds the execution label to a chosen instance only after explicit confirmation', async () => {
    const tokyo = {
      monitoring_instance_id: 'mi_tokyo',
      display_name: 'tokyo-edge-01',
      group: 'apac',
      labels: [] as string[],
      note: 'keep this note',
      updated_at: '2026-10-09T08:00:00Z',
    }
    // 心跳与同步会不断刷新 updated_at：每次重读都拿到更新后的令牌。
    let version = 0
    const reads: string[] = []
    const patches: Array<{ body: unknown; ifMatch: string | null }> = []
    const conflicts = { remaining: 3 }
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/monitoring-instances/mi_tokyo' && init?.method === 'PATCH') {
        patches.push({ body: JSON.parse(String(init.body)), ifMatch: new Headers(init.headers).get('If-Match') })
        if (conflicts.remaining > 0) {
          conflicts.remaining -= 1
          return mockJSONResponse({ error: 'metadata conflict' }, 409)
        }
        return mockJSONResponse({ ...tokyo, labels: ['ops', 'jp'], updated_at: '2026-10-09T08:09:00Z' })
      }
      if (url === '/api/monitoring-instances/mi_tokyo') {
        version += 1
        const updatedAt = `2026-10-09T08:0${version}:00Z`
        reads.push(updatedAt)
        // 读取期间别处给实例加了 ops 标签，追加时必须保留。
        return mockJSONResponse({ ...tokyo, labels: version > 1 ? ['ops'] : [], updated_at: updatedAt })
      }
      if (url.startsWith('/api/monitoring-instances')) return mockJSONResponse([tokyo])
      return mockJSONResponse([])
    }))

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    const executionLabels = within(createDrawer).getByLabelText('执行监控实例标签')
    fireEvent.focus(executionLabels)
    await within(createDrawer).findByText(/现有 1 台监控实例都还没有标签/)
    fireEvent.change(executionLabels, { target: { value: 'jp' } })

    const assign = within(createDrawer).getByRole('group', { name: '给监控实例加标签' })
    const trigger = within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「jp」' })
    fireEvent.click(trigger)
    const confirm = within(assign).getByRole('button', { name: '确认添加' })
    expect(confirm).toHaveFocus()
    expect(confirm).toHaveAccessibleDescription(/加上后，它也会执行其他带这个标签的目标/)
    // 取消不写入，焦点回到发起的实例按钮。
    fireEvent.click(within(assign).getByRole('button', { name: '取消' }))
    expect(patches).toHaveLength(0)
    await waitFor(() => expect(within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「jp」' })).toHaveFocus())

    // 两次尝试都冲突：给出中文说明，可以再确认。
    fireEvent.click(within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「jp」' }))
    fireEvent.click(within(assign).getByRole('button', { name: '确认添加' }))
    expect(await within(assign).findByRole('alert')).toHaveTextContent('给 tokyo-edge-01 加上「jp」失败：实例资料正在更新，没有加上标签，请再确认一次。')
    expect(patches.map((patch) => patch.ifMatch)).toEqual(['"2026-10-09T08:01:00Z"', '"2026-10-09T08:02:00Z"'])

    // 再次确认：第一次仍冲突，重读后用新令牌成功。
    fireEvent.click(within(assign).getByRole('button', { name: '确认添加' }))
    expect(await within(createDrawer).findByText('将由 tokyo-edge-01 执行。')).toBeInTheDocument()
    expect(within(createDrawer).queryByRole('group', { name: '给监控实例加标签' })).not.toBeInTheDocument()
    expect(executionLabels).toHaveFocus()
    expect(reads).toHaveLength(4)
    expect(patches).toHaveLength(4)
    expect(patches[3]).toEqual({
      body: { group: 'apac', labels: ['ops', 'jp'], note: 'keep this note' },
      ifMatch: '"2026-10-09T08:04:00Z"',
    })
  })

  it('keeps a failure visible when the execution label changes while adding', async () => {
    const tokyo = { monitoring_instance_id: 'mi_tokyo', display_name: 'tokyo-edge-01', group: '', labels: [] as string[], note: '', updated_at: '2026-10-09T08:00:00Z' }
    let releasePatch: () => void = () => undefined
    const patchGate = new Promise<void>((resolve) => { releasePatch = resolve })
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/monitoring-instances/mi_tokyo' && init?.method === 'PATCH') {
        await patchGate
        return mockJSONResponse({ error: 'monitoring instance not found' }, 404)
      }
      if (url === '/api/monitoring-instances/mi_tokyo') return mockJSONResponse(tokyo)
      if (url.startsWith('/api/monitoring-instances')) return mockJSONResponse([tokyo])
      return mockJSONResponse([])
    }))

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const createDrawer = screen.getByRole('dialog', { name: '创建目标' })
    const executionLabels = within(createDrawer).getByLabelText('执行监控实例标签')
    fireEvent.focus(executionLabels)
    await within(createDrawer).findByText(/现有 1 台监控实例都还没有标签/)
    fireEvent.change(executionLabels, { target: { value: 'jp' } })
    const assign = within(createDrawer).getByRole('group', { name: '给监控实例加标签' })
    fireEvent.click(within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「jp」' }))
    fireEvent.click(within(assign).getByRole('button', { name: '确认添加' }))
    expect(await within(assign).findByRole('button', { name: '正在添加…' })).toBeDisabled()

    // 请求进行中改了执行标签：确认态让位给新标签的候选，旧请求的失败仍要说清楚。
    fireEvent.change(executionLabels, { target: { value: 'kr' } })
    expect(within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「kr」' })).toBeDisabled()
    await act(async () => { releasePatch() })
    expect(await within(assign).findByRole('alert')).toHaveTextContent('给 tokyo-edge-01 加上「jp」失败：monitoring instance not found')
    expect(within(assign).getByRole('button', { name: '给 tokyo-edge-01 加上「kr」' })).toBeEnabled()
  })

  it('uses Chinese-first validation for base port', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('resets stale create drawer state when cancelled from the drawer', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([targetRecord()])).mockResolvedValueOnce(mockJSONResponse([]))
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
      .mockResolvedValueOnce(mockJSONResponse([targetRecord()])).mockResolvedValueOnce(mockJSONResponse([]))
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
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('does not navigate from a late target creation response after leaving the page', async () => {
    const createResponse = deferred<Response>()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))

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
            enabled_probe_count: 1,
            matching_executor_count: 1,
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
      .mockResolvedValueOnce(mockJSONResponse([])).mockResolvedValueOnce(mockJSONResponse([]))
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

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
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
    ).mockResolvedValueOnce(mockJSONResponse([]))
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
    expect(screen.getByRole('option', { name: '服务' })).toHaveValue('service')
    expect(screen.getByRole('option', { name: '国内参考' })).toHaveValue('china_reference')
    expect(screen.getAllByText('服务').some((node) => node.classList.contains('probe-kind'))).toBe(true)
    expect(screen.getAllByText('国内参考').some((node) => node.classList.contains('probe-kind'))).toBe(true)
    expect(document.querySelector('.probe-kind')?.textContent).not.toBe('service')

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
    ).mockResolvedValueOnce(mockJSONResponse([]))
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

  it('keeps unobserved targets out of the abnormal deep link and opens view=unobserved', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({
        target_id: 'tg_alert',
        name: 'Failing API',
        current_health_status: '告警',
      }),
      targetRecord({
        target_id: 'tg_quiet',
        name: 'Quiet API',
        current_health_status: '数据不可用',
        last_success_at: undefined,
        last_failure_at: undefined,
      }),
      targetRecord({
        target_id: 'tg_paused_unknown',
        name: 'Paused Unknown API',
        run_status: '暂停',
        current_health_status: '数据不可用',
        last_success_at: undefined,
        last_failure_at: undefined,
      }),
    ]))

    const abnormalView = renderTargets('/targets?abnormal=1')
    await waitFor(() => expect(screen.getByText('Failing API')).toBeInTheDocument())
    expect(screen.queryByText('Quiet API')).not.toBeInTheDocument()
    expect(screen.queryByText('Paused Unknown API')).not.toBeInTheDocument()
    abnormalView.unmount()

    renderTargets('/targets?view=unobserved')
    await waitFor(() => expect(screen.getByText('Quiet API')).toBeInTheDocument())
    expect(screen.getByText('数据不可用', { selector: '.badge' }).className).not.toMatch(/tone--/)
    expect(screen.queryByText('Failing API')).not.toBeInTheDocument()
    expect(screen.queryByText('Paused Unknown API')).not.toBeInTheDocument()
    expect(screen.getByText('已匹配实例，尚无样本')).toBeInTheDocument()
  })

  it('filters 数据不可用 without treating it as a known abnormality', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_normal', name: 'Healthy API' }),
      targetRecord({
        target_id: 'tg_unknown',
        name: 'Unknown API',
        current_health_status: '数据不可用',
        last_success_at: undefined,
        last_failure_at: undefined,
      }),
    ]))
    renderTargets()
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('健康'), { target: { value: '数据不可用' } })
    await waitFor(() => expect(screen.queryByText('Healthy API')).not.toBeInTheDocument())
    expect(screen.getByText('Unknown API')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: /异常/ }))
    await waitFor(() => expect(screen.queryByText('Unknown API')).not.toBeInTheDocument())
  })

  it('suggests existing monitoring instance labels and still accepts a future label', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/targets' || url === '/api/targets?scope=retired') return mockJSONResponse([])
      if (url === '/api/monitoring-instances?scope=active') {
        return mockJSONResponse([
          { labels: ['edge', 'core'] },
          { labels: ['edge'] },
        ])
      }
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    fireEvent.click(await screen.findByRole('button', { name: '新建第一个目标' }))
    const dialog = await screen.findByRole('dialog', { name: '创建目标' })
    fireEvent.focus(within(dialog).getByLabelText('执行监控实例标签'))
    fireEvent.click(await within(dialog).findByRole('button', { name: 'edge' }))
    const input = within(dialog).getByLabelText('执行监控实例标签')
    expect(input).toHaveValue('edge')
    fireEvent.change(input, { target: { value: 'edge, not-yet-used' } })
    expect(input).toHaveValue('edge, not-yet-used')
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
          execution_monitoring_instance_labels: ['future-label'],
          matching_executor_count: 0,
        }),
        targetRecord({
          target_id: 'tg_future_labels',
          name: 'Future Labels API',
          execution_monitoring_instance_labels: [],
          enabled_probe_count: 1,
          matching_executor_count: 1,
        }),
      ]),
    ).mockResolvedValueOnce(mockJSONResponse([]))
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
    expect(screen.queryByText('Future Labels API')).not.toBeInTheDocument()
    expect(screen.getByText('没有可接收该任务的实例')).toBeInTheDocument()
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
    ).mockResolvedValueOnce(mockJSONResponse([]))
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

  it('uses lifecycle_status=retired from Dashboard deep links as the initial target filter', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        return mockJSONResponse([
          targetRecord({ target_id: 'tg_enabled', name: 'Enabled API' }),
        ])
      }
      if (url === '/api/targets?scope=retired') {
        return mockJSONResponse([
          targetRecord({
            target_id: 'tg_retired',
            name: 'Archived API',
            lifecycle_status: 'retired',
            run_status: '暂停',
          }),
        ])
      }
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets?lifecycle_status=retired']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Archived API')).toBeInTheDocument())

    expect(screen.queryByText('Enabled API')).not.toBeInTheDocument()
    expect(screen.queryByText('Archived Carrier Only')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: /全部/ }))
    await waitFor(() => expect(screen.getByText('Enabled API')).toBeInTheDocument())
    expect(screen.getByText('Archived API')).toBeInTheDocument()
    expect(screen.queryByText('Archived Carrier Only')).not.toBeInTheDocument()
    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls).toContain('/api/targets')
    expect(urls).toContain('/api/targets?scope=retired')
    expect(urls).not.toContain('/api/targets?scope=all')
  })

  it('does not paint a successful collection when the other bulk list fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/targets') return mockJSONResponse([targetRecord({ name: 'Current API' })])
      if (url === '/api/targets?scope=retired') return mockJSONResponse({ error: 'retired unavailable' }, 503)
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    }))
    renderTargets()

    expect(await screen.findByRole('heading', { name: '目标列表不可用' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '候风尚未配置任何观测目标' })).not.toBeInTheDocument()
    expect(screen.queryByText('Current API')).not.toBeInTheDocument()
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
    ).mockResolvedValueOnce(mockJSONResponse([]))
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
      ).mockResolvedValueOnce(mockJSONResponse([])),
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
      vi.fn().mockResolvedValueOnce(mockJSONResponse([targetRecord()])).mockResolvedValueOnce(mockJSONResponse([])),
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
      ).mockResolvedValueOnce(mockJSONResponse([])),
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
      ).mockResolvedValueOnce(mockJSONResponse([])),
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
    expect(within(actions).getByRole('button', { name: '退役' })).toBeInTheDocument()
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
      if (url === '/api/targets?scope=retired' && init?.method !== 'POST') {
        return mockJSONResponse([])
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
    fireEvent.click(screen.getByRole('button', { name: '退役' }))

    expect(screen.getByRole('alertdialog', { name: '确认批量退役目标' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认批量退役' }))

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

  it('filters 异常, 暂停, 退役, and 覆盖缺口 from the quick-view tabs', async () => {
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
        lifecycle_status: 'retired', run_status: '暂停',
      }),
      targetRecord({
        target_id: 'tg_gap',
        name: 'Coverage Gap API',
        execution_monitoring_instance_labels: ['future-label'],
        enabled_probe_count: 0,
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

    fireEvent.click(screen.getByRole('tab', { name: /退役/ }))
    await waitFor(() => expect(screen.getByText('Archived API')).toBeInTheDocument())
    expect(screen.queryByText('Paused API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /覆盖缺口/ }))
    await waitFor(() => expect(screen.getByText('Coverage Gap API')).toBeInTheDocument())
    expect(screen.getAllByText('未配置启用探测项').length).toBeGreaterThan(0)
    expect(screen.queryByText('Archived API')).not.toBeInTheDocument()
    expect(screen.queryByText('Healthy API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /全部/ }))
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    expect(screen.getByText('Failing API')).toBeInTheDocument()
    expect(screen.getByText('Paused API')).toBeInTheDocument()
    expect(screen.getByText('Archived API')).toBeInTheDocument()
    expect(screen.getByText('Coverage Gap API')).toBeInTheDocument()
  })

  it('keeps stale targets independent of abnormal, unobserved, paused, and retired rows', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_fresh', name: 'Healthy API', group: 'edge' }),
      targetRecord({
        target_id: 'tg_alert_fresh',
        name: 'Failing Fresh',
        group: 'edge',
        current_health_status: '告警',
        current_primary_issue_summary: 'TLS 证书即将过期',
      }),
      targetRecord({
        target_id: 'tg_alert_stale',
        name: 'Failing Stale',
        group: 'edge',
        current_health_status: '告警',
        current_primary_issue_summary: 'HTTP 探测持续失败',
        observation_freshness: observationFreshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_normal_stale',
        name: 'Normal Stale',
        group: 'core',
        observation_freshness: observationFreshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_pending',
        name: 'Pending API',
        group: 'edge',
        observation_freshness: observationFreshness({ state: 'pending', fresh_probe_count: 0, pending_probe_count: 1, stale_probe_count: 0 }),
      }),
      targetRecord({
        target_id: 'tg_partial',
        name: 'Partial Wait',
        group: 'edge',
        observation_freshness: observationFreshness({ state: 'partial', fresh_probe_count: 1, pending_probe_count: 1, stale_probe_count: 0 }),
      }),
      targetRecord({
        target_id: 'tg_never',
        name: 'Never Observed',
        group: 'edge',
        current_health_status: '数据不可用',
        last_success_at: undefined,
        last_failure_at: undefined,
        observation_freshness: observationFreshness({ state: 'unobserved', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_paused',
        name: 'Paused Stale',
        group: 'edge',
        run_status: '暂停',
        observation_freshness: observationFreshness({ state: 'inactive', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_retired',
        name: 'Retired Stale',
        group: 'edge',
        lifecycle_status: 'retired',
        run_status: '暂停',
        observation_freshness: observationFreshness({ state: 'inactive', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
    ]))
    renderTargets()
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    expect(screen.getByRole('tab', { name: '异常 2' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '观测过期 2' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '观测过期 2' }))
    await waitFor(() => expect(screen.getByText('Failing Stale')).toBeInTheDocument())
    expect(screen.getByText('Normal Stale')).toBeInTheDocument()
    expect(screen.getByText('HTTP 探测持续失败')).toBeInTheDocument()
    expect(screen.getByText('最近已知健康')).toBeInTheDocument()
    expect(screen.getAllByText('观测已过期')).toHaveLength(2)
    expect(screen.getByText('最近一次正常，当前证据不足')).toBeInTheDocument()
    expect(screen.queryByText('当前正常')).not.toBeInTheDocument()
    expect(screen.queryByText('Failing Fresh')).not.toBeInTheDocument()
    expect(screen.queryByText('Pending API')).not.toBeInTheDocument()
    expect(screen.queryByText('Partial Wait')).not.toBeInTheDocument()
    expect(screen.queryByText('Never Observed')).not.toBeInTheDocument()
    expect(screen.queryByText('Paused Stale')).not.toBeInTheDocument()
    expect(screen.queryByText('Retired Stale')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '异常 2' }))
    await waitFor(() => expect(screen.getByText('Failing Fresh')).toBeInTheDocument())
    expect(screen.getByText('Failing Stale')).toBeInTheDocument()
    expect(screen.getByText('TLS 证书即将过期')).toBeInTheDocument()
    expect(screen.queryByText('Normal Stale')).not.toBeInTheDocument()
  })

  it('opens view=stale directly, keeps the group filter, and shows an empty stale result', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_fresh', name: 'Healthy API', group: 'edge' }),
      targetRecord({
        target_id: 'tg_stale',
        name: 'Edge Stale',
        group: 'edge',
        observation_freshness: observationFreshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_other',
        name: 'Core Stale',
        group: 'core',
        observation_freshness: observationFreshness({ state: 'partial', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({ target_id: 'tg_paused', name: 'Paused API', group: 'edge', run_status: '暂停' }),
    ]))
    renderTargets('/targets?view=stale&group=edge')
    await waitFor(() => expect(screen.getByText('Edge Stale')).toBeInTheDocument())
    expect(screen.getByText('分组: edge')).toBeInTheDocument()
    expect(screen.queryByText('Healthy API')).not.toBeInTheDocument()
    expect(screen.queryByText('Core Stale')).not.toBeInTheDocument()
    expect(screen.queryByText('Paused API')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '全部 4' }))
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    expect(screen.getByText('分组: edge')).toBeInTheDocument()
    expect(screen.getByText('Edge Stale')).toBeInTheDocument()
    expect(screen.getByText('Paused API')).toBeInTheDocument()
    expect(screen.queryByText('Core Stale')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /观测过期/ }))
    await waitFor(() => expect(screen.queryByText('Healthy API')).not.toBeInTheDocument())
    expect(screen.getByText('Edge Stale')).toBeInTheDocument()
    expect(screen.queryByText('Paused API')).not.toBeInTheDocument()
  })

  it('matches a blank group when the stale link uses 未分组', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({
        target_id: 'tg_blank',
        name: 'Blank Group Stale',
        group: '',
        observation_freshness: observationFreshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({
        target_id: 'tg_named',
        name: 'Named Group Stale',
        group: 'edge',
        observation_freshness: observationFreshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
      }),
      targetRecord({ target_id: 'tg_fresh', name: 'Blank Fresh', group: '   ' }),
    ]))
    renderTargets('/targets?view=stale&group=' + encodeURIComponent('未分组'))
    await waitFor(() => expect(screen.getByText('Blank Group Stale')).toBeInTheDocument())
    expect(screen.queryByText('Named Group Stale')).not.toBeInTheDocument()
    expect(screen.queryByText('Blank Fresh')).not.toBeInTheDocument()
    expect(screen.getByText('分组: 未分组')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '观测过期 2' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByRole('tab', { name: /异常/, selected: true })).not.toBeInTheDocument()
  })

  it('shows the empty stale view without dropping the last list on a later empty filter', async () => {
    vi.stubGlobal('fetch', listFetch([
      targetRecord({ target_id: 'tg_fresh', name: 'Healthy API' }),
    ]))
    renderTargets()
    await waitFor(() => expect(screen.getByText('Healthy API')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('tab', { name: '观测过期' }))
    expect(await screen.findByText('没有匹配当前筛选的目标')).toBeInTheDocument()
    expect(screen.queryByText('Healthy API')).not.toBeInTheDocument()
  })

  it('refreshes only the current list while visible and keeps the snapshot after a network failure', async () => {
    vi.useFakeTimers()
    let currentCalls = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/targets') {
        currentCalls += 1
        if (currentCalls === 1) return mockJSONResponse([targetRecord({ name: 'Blog' })])
        return mockJSONResponse({ error: 'unavailable' }, 503)
      }
      if (url === '/api/targets?scope=retired') return mockJSONResponse([])
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(screen.getByText('Blog')).toBeInTheDocument()
    const retiredCalls = () => fetchMock.mock.calls.filter(([input]) => String(input) === '/api/targets?scope=retired').length
    expect(retiredCalls()).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(currentCalls).toBe(2)
    expect(retiredCalls()).toBe(1)
    expect(screen.getByText('Blog')).toBeInTheDocument()
    const notice = screen.getByText(/更新失败，显示上次结果/)
    expect(notice.closest('p')?.querySelector('.timestamp')).not.toBeNull()
    expect(notice.closest('p')).not.toHaveTextContent('最新')
    expect(screen.getByRole('tab', { name: /全部/ })).toBeInTheDocument()
  })

  it('does not refresh while hidden and refreshes the current list on focus', async () => {
    vi.useFakeTimers()
    let currentCalls = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/targets') {
        currentCalls += 1
        return mockJSONResponse([targetRecord({ name: 'Blog' })])
      }
      if (url === '/api/targets?scope=retired') return mockJSONResponse([])
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(currentCalls).toBe(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'hidden',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('focus'))
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(currentCalls).toBe(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    })
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(currentCalls).toBe(2)
    expect(fetchMock.mock.calls.filter(([input]) => String(input) === '/api/targets?scope=retired')).toHaveLength(1)
  })

  it('clears the list when a visible refresh is unauthorized or not found', async () => {
    vi.useFakeTimers()
    for (const status of [401, 404]) {
      let currentCalls = 0
      const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url === '/api/targets') {
          currentCalls += 1
          if (currentCalls === 1) return mockJSONResponse([targetRecord({ name: 'Blog' })])
          return mockJSONResponse({ error: 'missing' }, status)
        }
        if (url === '/api/targets?scope=retired') return mockJSONResponse([])
        return mockJSONResponse({ error: `unexpected ${url}` }, 500)
      })
      vi.stubGlobal('fetch', fetchMock)
      const view = renderTargets()
      for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(0)
        })
      }
      expect(screen.getByText('Blog')).toBeInTheDocument()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
      })
      expect(screen.getByRole('heading', { name: '目标列表不可用' })).toBeInTheDocument()
      expect(screen.queryByText('Blog')).not.toBeInTheDocument()
      expect(screen.queryByText(/更新失败，显示上次结果/)).not.toBeInTheDocument()
      view.unmount()
    }
  })

  it('does not apply an in-flight periodic read over the post-mutation list', async () => {
    vi.useFakeTimers()
    const gate = deferred<Response>()
    let currentCalls = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        currentCalls += 1
        if (currentCalls === 1) return mockJSONResponse([targetRecord({ target_id: 'tg_001', name: 'Before' })])
        if (currentCalls === 2) return gate.promise
        return mockJSONResponse([targetRecord({ target_id: 'tg_001', name: 'After Mutation' })])
      }
      if (url === '/api/targets?scope=retired') return mockJSONResponse([])
      if (url.includes('/lifecycle-review')) {
        return mockJSONResponse({ dependency_impacts: [], preview_digest: 'target-digest' })
      }
      if (url.includes('/runtime/')) return mockJSONResponse(targetRecord({ target_id: 'tg_001', name: 'After Mutation' }))
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Before'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(currentCalls).toBe(2)

    fireEvent.click(screen.getByLabelText('选择 Before'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))
    for (
      let attempt = 0;
      attempt < 20 && !fetchMock.mock.calls.some(([input]) => String(input).includes('/runtime/enter-maintenance'));
      attempt += 1
    ) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(screen.getByText('Before')).toBeInTheDocument()
    expect(screen.queryByText('After Mutation')).not.toBeInTheDocument()

    await act(async () => {
      gate.resolve(mockJSONResponse([targetRecord({ target_id: 'tg_001', name: 'Obsolete' })]))
      await vi.advanceTimersByTimeAsync(0)
    })
    for (let attempt = 0; attempt < 20 && !screen.queryByText('After Mutation'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(screen.getByText('After Mutation')).toBeInTheDocument()
    expect(screen.queryByText('Obsolete')).not.toBeInTheDocument()
    expect(screen.queryByText('Before')).not.toBeInTheDocument()
  })

  it('keeps the last complete snapshot when the retired read fails after archive', async () => {
    vi.useFakeTimers()
    let currentCalls = 0
    let retiredCalls = 0
    let retiredMode: 'empty' | 'fail' | 'recovered' = 'empty'
    const active = targetRecord({ target_id: 'tg_archive', name: 'Blog' })
    const retired = targetRecord({
      target_id: 'tg_archive',
      name: 'Blog',
      lifecycle_status: 'retired',
      run_status: '暂停',
    })
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        currentCalls += 1
        return mockJSONResponse(currentCalls === 1 ? [active] : [])
      }
      if (url === '/api/targets?scope=retired') {
        retiredCalls += 1
        if (retiredMode === 'fail') return mockJSONResponse({ error: 'retired unavailable' }, 503)
        if (retiredMode === 'recovered') return mockJSONResponse([retired])
        return mockJSONResponse([])
      }
      if (url.includes('/lifecycle-review')) {
        return mockJSONResponse({ dependency_impacts: [], preview_digest: 'target-digest' })
      }
      if (url.includes('/runtime/archive')) {
        retiredMode = 'fail'
        return mockJSONResponse(retired)
      }
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(retiredCalls).toBe(1)

    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '退役' }))
    fireEvent.click(screen.getByRole('button', { name: '确认批量退役' }))
    for (let attempt = 0; attempt < 20 && !screen.queryByText(/更新失败，显示上次结果/); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }

    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.queryByText('已退役')).not.toBeInTheDocument()
    const notice = screen.getByText(/更新失败，显示上次结果/)
    expect(notice.closest('p')?.querySelector('.timestamp')).not.toBeNull()
    expect(currentCalls).toBe(2)
    expect(retiredCalls).toBe(2)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(currentCalls).toBe(3)
    expect(retiredCalls).toBe(2)
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.queryByText('已退役')).not.toBeInTheDocument()
    expect(screen.getByText(/更新失败，显示上次结果/)).toBeInTheDocument()

    retiredMode = 'recovered'
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    for (let attempt = 0; attempt < 20 && !screen.queryByText('已退役'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(retiredCalls).toBe(3)
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('已退役')).toBeInTheDocument()
    expect(screen.queryByText(/更新失败，显示上次结果/)).not.toBeInTheDocument()
  })

  it('clears the list when a paired post-archive read is unauthorized', async () => {
    vi.useFakeTimers()
    let currentCalls = 0
    let retiredMode: 'empty' | 'unauthorized' = 'empty'
    const active = targetRecord({ target_id: 'tg_archive', name: 'Blog' })
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        currentCalls += 1
        return mockJSONResponse(currentCalls === 1 ? [active] : [])
      }
      if (url === '/api/targets?scope=retired') {
        if (retiredMode === 'unauthorized') return mockJSONResponse({ error: 'unauthenticated' }, 401)
        return mockJSONResponse([])
      }
      if (url.includes('/lifecycle-review')) {
        return mockJSONResponse({ dependency_impacts: [], preview_digest: 'target-digest' })
      }
      if (url.includes('/runtime/archive')) {
        retiredMode = 'unauthorized'
        return mockJSONResponse(active)
      }
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '退役' }))
    fireEvent.click(screen.getByRole('button', { name: '确认批量退役' }))
    for (let attempt = 0; attempt < 20 && !screen.queryByRole('heading', { name: '目标列表不可用' }); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(screen.getByRole('heading', { name: '目标列表不可用' })).toBeInTheDocument()
    expect(screen.queryByText('Blog')).not.toBeInTheDocument()
    expect(screen.queryByText(/更新失败，显示上次结果/)).not.toBeInTheDocument()
  })

  it('keeps the last complete snapshot when the current read fails after archive', async () => {
    vi.useFakeTimers()
    let retiredCalls = 0
    let mode: 'initial' | 'failed' | 'recovered' = 'initial'
    const active = targetRecord({ target_id: 'tg_archive', name: 'Blog' })
    const retired = targetRecord({
      target_id: 'tg_archive',
      name: 'Blog',
      lifecycle_status: 'retired',
      run_status: '暂停',
    })
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/targets' && init?.method !== 'POST') {
        if (mode === 'failed') return mockJSONResponse({ error: 'unavailable' }, 503)
        if (mode === 'recovered') return mockJSONResponse([])
        return mockJSONResponse([active])
      }
      if (url === '/api/targets?scope=retired') {
        retiredCalls += 1
        if (mode === 'initial') return mockJSONResponse([])
        return mockJSONResponse([retired])
      }
      if (url.includes('/lifecycle-review')) {
        return mockJSONResponse({ dependency_impacts: [], preview_digest: 'target-digest' })
      }
      if (url.includes('/runtime/archive')) {
        mode = 'failed'
        return mockJSONResponse(retired)
      }
      return mockJSONResponse({ error: `unexpected ${url}` }, 500)
    }))
    renderTargets()
    for (let attempt = 0; attempt < 20 && !screen.queryByText('Blog'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    fireEvent.click(screen.getByLabelText('选择 Blog'))
    fireEvent.click(screen.getByRole('button', { name: '批量操作' }))
    fireEvent.click(screen.getByRole('button', { name: '退役' }))
    fireEvent.click(screen.getByRole('button', { name: '确认批量退役' }))
    for (let attempt = 0; attempt < 20 && !screen.queryByText(/更新失败，显示上次结果/); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.queryByText('已退役')).not.toBeInTheDocument()
    expect(retiredCalls).toBe(2)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(retiredCalls).toBe(2)
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText(/更新失败，显示上次结果/)).toBeInTheDocument()

    mode = 'recovered'
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    for (let attempt = 0; attempt < 20 && !screen.queryByText('已退役'); attempt += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(retiredCalls).toBe(3)
    expect(screen.getByText('已退役')).toBeInTheDocument()
    expect(screen.queryByText(/更新失败，显示上次结果/)).not.toBeInTheDocument()
  })
})
