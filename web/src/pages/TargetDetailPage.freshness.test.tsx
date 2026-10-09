import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { TargetDetailPage } from './TargetDetailPage'
import type { ProbeObservationFreshness, TargetObservationFreshness, TargetRecord } from '../lib/types'

const hook = vi.hoisted(() => ({
  epoch: 0,
  callback: null as null | ((context: { isCurrent: () => boolean }) => Promise<void>),
  refresh: null as null | (() => Promise<void>),
}))

vi.mock('../lib/useVisibleRefresh', () => ({
  useVisibleRefresh: (callback: (context: { isCurrent: () => boolean }) => Promise<void>) => {
    hook.callback = callback
    const refresh = () => {
      const seen = hook.epoch
      return callback({ isCurrent: () => hook.epoch === seen })
    }
    hook.refresh = refresh
    return {
      refresh,
      invalidate: () => {
        hook.epoch += 1
      },
    }
  },
}))

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

function deferredResponse() {
  let resolve!: (response: Response) => void
  const promise = new Promise<Response>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

function freshness(
  overrides: Partial<TargetObservationFreshness> = {},
  probes: ProbeObservationFreshness[] = [],
): TargetObservationFreshness {
  return {
    state: 'fresh',
    evaluated_at: '2026-04-24T09:05:00Z',
    enabled_probe_count: probes.length,
    fresh_probe_count: probes.filter((probe) => probe.state === 'fresh').length,
    pending_probe_count: probes.filter((probe) => probe.state === 'pending').length,
    stale_probe_count: probes.filter((probe) => probe.state === 'stale').length,
    probes,
    ...overrides,
  }
}

function targetRecord(
  overrides: Partial<TargetRecord> = {},
  observation: TargetObservationFreshness = freshness(),
): TargetRecord {
  return {
    target_id: 'tg_001',
    lifecycle_status: 'active',
    name: 'Blog',
    target_type: 'service',
    host: 'blog.example.com',
    base_port: 443,
    execution_monitoring_instance_labels: ['edge'],
    run_status: '启用',
    group: 'prod',
    labels: ['公开'],
    note: '现网入口',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: observation.enabled_probe_count,
    matching_executor_count: 1,
    last_success_at: '2026-04-24T09:00:00Z',
    last_failure_at: '2026-04-24T08:30:00Z',
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
    observation_freshness: overrides.observation_freshness ?? observation,
  }
}

function probeFreshness(
  overrides: Partial<ProbeObservationFreshness> & Pick<ProbeObservationFreshness, 'probe_item_id' | 'state'>,
): ProbeObservationFreshness {
  return {
    effective_frequency_tier: '1m',
    stale_after_seconds: 185,
    last_observed_at: '2026-04-24T09:00:00Z',
    expected_since: '2026-04-24T08:00:00Z',
    deadline_at: '2026-04-24T09:03:05Z',
    ...overrides,
  }
}

function requestUrl(input: RequestInfo | URL) {
  return typeof input === 'string' ? input : 'url' in input ? input.url : String(input)
}

function emptyStateAddProbeButton() {
  const emptyState = screen.getByRole('heading', { name: '目标尚未配置探测项' }).closest('.page-state')
  if (!(emptyState instanceof HTMLElement)) {
    throw new Error('empty probe state is not rendered')
  }
  return within(emptyState).getByRole('button', { name: '添加探测项' })
}

async function flushVisibleRefresh() {
  await act(async () => {
    await hook.refresh?.()
  })
}

function renderDetail(targetId = 'tg_001') {
  render(
    <MemoryRouter initialEntries={[`/targets/${targetId}`]}>
      <Routes>
        <Route path="/targets/:targetId" element={<TargetDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function SwitchHarness() {
  const navigate = useNavigate()
  return (
    <>
      <button type="button" onClick={() => navigate('/targets/tg_002')}>
        switch target
      </button>
      <Routes>
        <Route path="/targets/:targetId" element={<TargetDetailPage />} />
      </Routes>
    </>
  )
}

describe('TargetDetailPage observation freshness', () => {
  beforeEach(() => {
    hook.epoch = 0
    hook.callback = null
    hook.refresh = null
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows a stale projection beside the active incident without claiming recovery', async () => {
    const stale = freshness({
      state: 'stale',
      enabled_probe_count: 1,
      fresh_probe_count: 0,
      pending_probe_count: 0,
      stale_probe_count: 1,
    }, [
      probeFreshness({ probe_item_id: 'pb_http', state: 'stale' }),
    ])
    const target = targetRecord({
      current_health_status: '告警',
      current_active_incident_count: 1,
      current_primary_issue_summary: 'HTTP 探测失败',
      enabled_probe_count: 1,
      observation_freshness: stale,
    })
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url.includes('/probe-items')) {
        return mockJSONResponse([{
          probe_item_id: 'pb_http',
          target_id: 'tg_001',
          probe_kind: 'http',
          enabled: true,
          frequency_tier: '1m',
          timeout_seconds: 5,
          config: { path: '/healthz', method: 'GET' },
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }])
      }
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents')) {
        return mockJSONResponse([{
          incident_id: 'inc_001',
          incident_class: 'target_probe_failure',
          object_type: 'target',
          object_id: 'tg_001',
          severity: '告警',
          started_at: '2026-04-24T08:58:00Z',
          last_evaluated_at: '2026-04-24T09:05:00Z',
          source_summary: 'HTTP 探测失败',
        }])
      }
      if (url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse(target)
    }))

    renderDetail()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    expect(screen.getByText('HTTP 探测失败')).toBeInTheDocument()
    expect(screen.getAllByText('观测已过期').length).toBeGreaterThan(0)
    expect(screen.getByText('最近已知健康')).toBeInTheDocument()
    expect(screen.getByText('活跃异常仍以最近已知健康为准。')).toBeInTheDocument()
    expect(screen.queryByText('当前正常')).not.toBeInTheDocument()
    expect(screen.queryByText('已恢复')).not.toBeInTheDocument()
    const band = screen.getByLabelText('目标状态')
    expect(band).toHaveTextContent('新鲜')
    expect(band).toHaveTextContent('启用')
    expect(band).toHaveTextContent('评估于')
  })

  it('shows mixed probe deadlines and does not call a historical normal result current', async () => {
    const http = probeFreshness({ probe_item_id: 'pb_http', state: 'stale', effective_frequency_tier: '1m' })
    const tls = probeFreshness({
      probe_item_id: 'pb_tls',
      state: 'fresh',
      effective_frequency_tier: '6h',
      stale_after_seconds: 64805,
      deadline_at: '2026-04-25T04:00:05Z',
    })
    const observation = freshness({ state: 'partial' }, [http, tls])
    const target = targetRecord({
      current_health_status: '正常',
      enabled_probe_count: 2,
      observation_freshness: observation,
    })
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url.includes('/probe-items')) {
        return mockJSONResponse([
          {
            probe_item_id: 'pb_http',
            target_id: 'tg_001',
            probe_kind: 'http',
            enabled: true,
            frequency_tier: '5m',
            timeout_seconds: 5,
            config: { path: '/healthz', method: 'GET' },
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          },
          {
            probe_item_id: 'pb_tls',
            target_id: 'tg_001',
            probe_kind: 'tls',
            enabled: true,
            frequency_tier: '15m',
            timeout_seconds: 5,
            config: { port: 443 },
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          },
          {
            probe_item_id: 'pb_off',
            target_id: 'tg_001',
            probe_kind: 'tcp',
            enabled: false,
            frequency_tier: '5m',
            timeout_seconds: 3,
            config: { port: 80 },
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          },
        ])
      }
      if (url.includes('/runtime-facts')) {
        return mockJSONResponse({
          target_id: 'tg_001',
          latest_probe_observations: [{
            monitoring_instance_id: 'mi_001',
            target_id: 'tg_001',
            probe_item_id: 'pb_http',
            probe_kind: 'http',
            observed_at: '2026-04-24T12:00:00Z',
            received_at: '2026-04-24T12:00:01Z',
            agent_version: 'dev',
            fingerprint: 'fp',
            result_kind: 'success',
            latency_ms: 10,
            http_status: 200,
            tls_expiry_days: null,
            maintenance_context: true,
            is_backfilled: false,
            sync_batch_id: 'sync',
          }],
        })
      }
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse(target)
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    const httpRow = screen.getByText('HTTP').closest('tr') as HTMLElement
    const tlsRow = screen.getByText('TLS').closest('tr') as HTMLElement
    const disabledRow = screen.getByText('TCP').closest('tr') as HTMLElement
    expect(screen.getByText('部分观测过期')).toBeInTheDocument()
    expect(screen.getByText('最近一次正常，当前证据不足')).toBeInTheDocument()
    expect(screen.queryByText('当前正常')).not.toBeInTheDocument()
    expect(within(httpRow).getByText('过期于')).toBeInTheDocument()
    expect(within(httpRow).getByText('维护上下文')).toBeInTheDocument()
    expect(within(httpRow).getByText('成功')).toBeInTheDocument()
    expect(within(tlsRow).getByText('有效至')).toBeInTheDocument()
    expect(within(tlsRow).getByText('6h')).toBeInTheDocument()
    expect(within(disabledRow).getAllByText('已停用').length).toBeGreaterThan(0)
    expect(within(disabledRow).queryByText('等待新观测')).not.toBeInTheDocument()
  })

  it('keeps metadata and probe drafts and focus when the projection refreshes', async () => {
    const initial = targetRecord()
    const refreshed = targetRecord({
      note: '服务器备注',
      updated_at: '2026-04-24T09:06:00Z',
      observation_freshness: freshness({ evaluated_at: '2026-04-24T09:06:00Z' }),
    })
    let targetReads = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') {
        targetReads += 1
        return mockJSONResponse(targetReads === 1 ? initial : refreshed)
      }
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(emptyStateAddProbeButton()).toBeInTheDocument())
    fireEvent.click(emptyStateAddProbeButton())
    fireEvent.change(screen.getByLabelText('端口'), { target: { value: '8443' } })
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    const note = screen.getByLabelText('备注')
    fireEvent.change(note, { target: { value: '草稿备注' } })
    note.focus()

    await flushVisibleRefresh()

    expect(screen.getByLabelText('备注')).toHaveValue('草稿备注')
    expect(screen.getByLabelText('端口')).toHaveValue('8443')
    expect(document.activeElement).toBe(screen.getByLabelText('备注'))
    expect(screen.queryByDisplayValue('服务器备注')).not.toBeInTheDocument()
  })

  it('drops a projection read that resolves after a mutation has invalidated it', async () => {
    const initial = targetRecord()
    const late = targetRecord({ name: '过期读', updated_at: '2026-04-24T09:01:00Z' })
    const saved = targetRecord({
      labels: ['kept'],
      note: '现网入口',
      updated_at: '2026-04-24T09:12:00Z',
    })
    const lateRead = deferredResponse()
    const save = deferredResponse()
    let targetReads = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001' && (!init?.method || init.method === 'GET')) {
        targetReads += 1
        if (targetReads === 1) return mockJSONResponse(initial)
        if (targetReads === 2) return lateRead.promise
        return mockJSONResponse(saved)
      }
      if (init?.method === 'PATCH') return save.promise
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      if (url.includes('/lifecycle-review')) return mockJSONResponse({ dependency_impacts: [], preview_digest: 'tg-digest' })
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    const pendingRead = hook.refresh?.()
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'kept' } })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    await waitFor(() => expect(hook.epoch).toBeGreaterThan(0))

    await act(async () => {
      lateRead.resolve(mockJSONResponse(late))
      await pendingRead
    })
    expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '过期读' })).not.toBeInTheDocument()

    save.resolve(mockJSONResponse(saved))
    await waitFor(() => expect(screen.getByText('标签：kept')).toBeInTheDocument())
    expect(screen.queryByRole('heading', { name: '过期读' })).not.toBeInTheDocument()
  })

  it('keeps the last snapshot and shows the failed refresh time when a later read fails', async () => {
    const initial = targetRecord()
    let targetReads = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') {
        targetReads += 1
        if (targetReads > 1) return mockJSONResponse({ error: 'unavailable' }, 503)
        return mockJSONResponse(initial)
      }
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    await flushVisibleRefresh()

    const notice = screen.getByText('更新失败，显示上次结果').closest('.observability-notice')
    expect(notice).not.toBeNull()
    expect(notice?.querySelector('.timestamp')).not.toBeNull()
    expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument()
    expect(screen.getByText('观测新鲜')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '目标详情不可用' })).not.toBeInTheDocument()
  })

  it('clears the detail on an authorization or missing-target refresh', async () => {
    const initial = targetRecord()
    let status = 200
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') {
        if (status !== 200) return mockJSONResponse({ error: 'gone' }, status)
        return mockJSONResponse(initial)
      }
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    status = 404
    await flushVisibleRefresh()
    expect(screen.getByRole('heading', { name: '目标详情不可用' })).toBeInTheDocument()
    expect(screen.getByText('目标不存在')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Blog' })).not.toBeInTheDocument()
  })

  it('clears the detail when a refresh is unauthorized', async () => {
    const initial = targetRecord()
    let unauthorized = false
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') {
        if (unauthorized) return mockJSONResponse({ error: 'unauthenticated' }, 401)
        return mockJSONResponse(initial)
      }
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    unauthorized = true
    await flushVisibleRefresh()
    expect(screen.getByRole('heading', { name: '目标详情不可用' })).toBeInTheDocument()
    expect(screen.getByText('unauthenticated')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Blog' })).not.toBeInTheDocument()
  })

  it('ignores a late projection from the previous target after switching routes', async () => {
    const first = targetRecord()
    const second = targetRecord({
      target_id: 'tg_002',
      name: 'Cache',
      host: 'cache.example.com',
    })
    const late = deferredResponse()
    let firstReads = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') {
        firstReads += 1
        return firstReads === 1 ? mockJSONResponse(first) : late.promise
      }
      if (url === '/api/targets/tg_002') return mockJSONResponse(second)
      if (url.includes('/probe-items') || url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: url.includes('tg_002') ? 'tg_002' : 'tg_001', latest_probe_observations: [] })
      return mockJSONResponse([])
    }))

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <SwitchHarness />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    const pending = hook.refresh?.()
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    await act(async () => {
      late.resolve(mockJSONResponse(targetRecord({ name: '过期读' })))
      await pending
    })
    expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '过期读' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Blog' })).not.toBeInTheDocument()
  })

  it('keeps the metadata draft token when a projection refresh loads a newer version', async () => {
    const initial = targetRecord({ updated_at: '2026-04-24T09:05:00Z' })
    const external = targetRecord({
      group: 'west',
      labels: ['外部'],
      note: '服务器备注',
      updated_at: '2026-04-24T10:00:00Z',
    })
    let targetReads = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001' && (!init?.method || init.method === 'GET')) {
        targetReads += 1
        return mockJSONResponse(targetReads === 1 ? initial : external)
      }
      if (init?.method === 'PATCH') return mockJSONResponse({ error: 'save failed' }, 409)
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    })
    vi.stubGlobal('fetch', fetchMock)

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: '草稿备注' } })

    await flushVisibleRefresh()
    const groupItem = screen.getByText('分组').closest('.target-detail-identity__item')
    expect(groupItem).toHaveTextContent('west')
    expect(screen.getByLabelText('备注')).toHaveValue('草稿备注')
    expect(screen.getByLabelText('Group')).toHaveValue('prod')
    expect(screen.getByRole('button', { name: '以当前版本为基准' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('save failed'))
    const patch = fetchMock.mock.calls.find(([, init]) => init?.method === 'PATCH')
    expect(patch?.[1]?.headers).toMatchObject({ 'If-Match': '"2026-04-24T09:05:00Z"' })
    expect(screen.getByLabelText('备注')).toHaveValue('草稿备注')
    expect(screen.getByRole('button', { name: '以当前版本为基准' })).toBeInTheDocument()
  })

  it('clears the metadata draft token on cancel and replaces it only on explicit rebase', async () => {
    const initial = targetRecord({ updated_at: '2026-04-24T09:05:00Z', note: '现网入口' })
    const external = targetRecord({
      note: '服务器备注',
      updated_at: '2026-04-24T10:00:00Z',
    })
    const newer = targetRecord({
      note: '更新的服务器备注',
      updated_at: '2026-04-24T11:00:00Z',
    })
    let targetReads = 0
    const matches: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001' && (!init?.method || init.method === 'GET')) {
        targetReads += 1
        if (targetReads === 1) return mockJSONResponse(initial)
        if (targetReads === 2) return mockJSONResponse(external)
        if (targetReads === 3) return mockJSONResponse(newer)
        return mockJSONResponse(targetRecord({ note: '第二次草稿', updated_at: '2026-04-24T11:05:00Z' }))
      }
      if (init?.method === 'PATCH') {
        const headers = init.headers as { 'If-Match'?: string }
        matches.push(headers['If-Match'] ?? '')
        return mockJSONResponse(targetRecord({ note: '第二次草稿', updated_at: '2026-04-24T11:05:00Z' }))
      }
      if (url.includes('/probe-items')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    }))

    renderDetail()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: '草稿备注' } })
    await flushVisibleRefresh()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(screen.queryByLabelText('备注')).not.toBeInTheDocument()
    expect(screen.getByText('备注：服务器备注')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '以当前版本为基准' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: '第二次草稿' } })
    await flushVisibleRefresh()
    expect(screen.getByRole('button', { name: '以当前版本为基准' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '以当前版本为基准' }))
    expect(screen.queryByRole('button', { name: '以当前版本为基准' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('备注')).toHaveValue('第二次草稿')
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    await waitFor(() => expect(screen.getByText('备注：第二次草稿')).toBeInTheDocument())
    expect(matches).toEqual(['"2026-04-24T11:00:00Z"'])
  })

  it('drops the metadata draft token when the detail route changes', async () => {
    const first = targetRecord({ note: '现网入口' })
    const second = targetRecord({
      target_id: 'tg_002',
      name: 'Cache',
      host: 'cache.example.com',
      note: '缓存备注',
      updated_at: '2026-04-24T11:00:00Z',
    })
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001') return mockJSONResponse(first)
      if (url === '/api/targets/tg_002') return mockJSONResponse(second)
      if (url.includes('/probe-items') || url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      if (url.includes('/runtime-facts')) {
        return mockJSONResponse({ target_id: url.includes('tg_002') ? 'tg_002' : 'tg_001', latest_probe_observations: [] })
      }
      return mockJSONResponse([])
    }))

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <SwitchHarness />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: '草稿备注' } })
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    expect(screen.queryByDisplayValue('草稿备注')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '资料维护' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    expect(screen.getByLabelText('备注')).toHaveValue('缓存备注')
  })

  it.each([
    ['resume', 'probe'],
    ['probe', 'resume'],
  ] as const)('refreshes once after overlapping mutations when %s finishes before %s', async (first, second) => {
    const initial = targetRecord({ run_status: '暂停' })
    const refreshed = targetRecord({ name: '刷新后', run_status: '启用', updated_at: '2026-04-24T09:20:00Z' })
    const probe = {
      probe_item_id: 'pb_http',
      target_id: 'tg_001',
      probe_kind: 'http',
      enabled: true,
      frequency_tier: '1m',
      timeout_seconds: 5,
      config: { path: '/healthz', method: 'GET' },
      created_at: '2026-04-20T00:00:00Z',
      updated_at: '2026-04-24T09:05:00Z',
    }
    const resumeGate = deferredResponse()
    const probeGate = deferredResponse()
    let targetReads = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestUrl(input)
      if (url === '/api/targets/tg_001' && (!init?.method || init.method === 'GET')) {
        targetReads += 1
        return mockJSONResponse(targetReads === 1 ? initial : refreshed)
      }
      if (url === '/api/targets/tg_001/runtime/resume') return resumeGate.promise
      if (url === '/api/targets/tg_001/probe-items/pb_http' && init?.method === 'PUT') return probeGate.promise
      if (url.includes('/probe-items')) return mockJSONResponse([probe])
      if (url.includes('/runtime-facts')) return mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] })
      if (url.includes('/incidents') || url.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse([])
    })
    vi.stubGlobal('fetch', fetchMock)

    renderDetail()
    await waitFor(() => expect(screen.getByRole('button', { name: /停用 探测项 pb_http/ })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '恢复' }))
    fireEvent.click(screen.getByRole('button', { name: /停用 探测项 pb_http/ }))
    await waitFor(() => {
      const urls = fetchMock.mock.calls.map(([input]) => requestUrl(input))
      expect(urls).toContain('/api/targets/tg_001/runtime/resume')
      expect(urls).toContain('/api/targets/tg_001/probe-items/pb_http')
    })
    expect(targetReads).toBe(1)

    const resumeResult = mockJSONResponse(targetRecord({ run_status: '启用' }))
    const probeResult = mockJSONResponse({ ...probe, enabled: false })
    if (first === 'resume') resumeGate.resolve(resumeResult)
    else probeGate.resolve(probeResult)
    if (first === 'resume') {
      await waitFor(() => expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument())
    } else {
      await waitFor(() => expect(screen.getByRole('button', { name: /启用 探测项 pb_http/ })).toBeInTheDocument())
    }
    expect(targetReads).toBe(1)
    expect(screen.queryByRole('heading', { name: '刷新后' })).not.toBeInTheDocument()

    if (second === 'resume') resumeGate.resolve(resumeResult)
    else probeGate.resolve(probeResult)
    await waitFor(() => expect(screen.getByRole('heading', { name: '刷新后' })).toBeInTheDocument())
    expect(targetReads).toBe(2)
  })
})
