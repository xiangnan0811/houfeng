import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TargetDetailPage } from './TargetDetailPage'

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
  let reject!: (error?: unknown) => void
  const promise = new Promise<Response>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function TargetDetailTestHarness() {
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

describe('TargetDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders probe items with latest runtime observations', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
          name: 'Blog',
          target_type: 'service',
          host: 'blog.example.com',
          base_port: 443,
          execution_node_labels: ['edge'],
          run_status: '启用',
          labels: ['公开'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            probe_item_id: 'pb_001',
            target_id: 'tg_001',
            probe_kind: 'http',
            enabled: true,
            frequency_tier: '1m',
            timeout_seconds: 5,
            config: { path: '/healthz', method: 'GET' },
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
          latest_probe_observations: [
            {
              node_id: 'nd_001',
              target_id: 'tg_001',
              probe_item_id: 'pb_001',
              probe_kind: 'http',
              observed_at: '2026-04-24T09:05:00Z',
              received_at: '2026-04-24T09:05:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-001',
              result_kind: 'success',
              latency_ms: 83,
              http_status: 200,
              tls_expiry_days: null,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-001',
            },
          ],
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            incident_id: 'inc_001',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            severity: '严重',
            started_at: '2026-04-24T08:58:00Z',
            last_evaluated_at: '2026-04-24T09:05:00Z',
            source_summary: 'HTTP 探测在多个节点上失败',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            event_id: 'evt_001',
            incident_id: 'inc_001',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'incident_started',
            severity: '严重',
            summary: 'HTTPS 探测连续失败',
            created_at: '2026-04-24T09:05:00Z',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )
    expect(screen.getByText('ProbeItem 列表')).toBeInTheDocument()
    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('83 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.getByText('nd_001')).toBeInTheDocument()
    expect(screen.getByText('当前活跃异常')).toBeInTheDocument()
    expect(screen.getByText('HTTP 探测在多个节点上失败')).toBeInTheDocument()
    expect(screen.getByText('最近相关事件')).toBeInTheDocument()
    expect(screen.getByText('HTTPS 探测连续失败')).toBeInTheDocument()
    expect(screen.queryByText('事件与 incident 仍由后续切片接入，这里先保留版位。')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/targets/tg_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_001/probe-items', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/targets/tg_001/runtime-facts', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/incidents?object_type=target&object_id=tg_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      '/api/events?object_type=target&object_id=tg_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      },
    )
  })

  it('renders probe, incident, and event empty states when the target has no related records', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            name: 'Cache',
            target_type: 'service',
            host: 'cache.example.com',
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            latest_probe_observations: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_002']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('当前还没有 ProbeItem，请为该入口添加至少一种观测方式。'),
    ).toBeInTheDocument()
    expect(screen.getByText('当前没有活跃异常')).toBeInTheDocument()
    expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument()
  })

  it('creates an HTTP ProbeItem from the empty state and appends it to the list', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_002',
          name: 'Cache',
          target_type: 'service',
          host: 'cache.example.com',
          base_port: 443,
          execution_node_labels: ['edge'],
          run_status: '启用',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          {
            probe_item_id: 'pb_new',
            target_id: 'tg_002',
            probe_kind: 'http',
            enabled: true,
            frequency_tier: '1m',
            timeout_seconds: 5,
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
            },
            created_at: '2026-04-27T09:00:00Z',
            updated_at: '2026-04-27T09:00:00Z',
          },
          201,
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_002']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'http' },
    })
    fireEvent.change(screen.getByLabelText('HTTP Scheme'), {
      target: { value: 'https' },
    })
    fireEvent.change(screen.getByLabelText('HTTP Path'), {
      target: { value: '/healthz' },
    })
    fireEvent.change(screen.getByLabelText('HTTP Method'), {
      target: { value: 'GET' },
    })
    fireEvent.change(screen.getByLabelText('期望状态码起点'), {
      target: { value: '200' },
    })
    fireEvent.change(screen.getByLabelText('期望状态码终点'), {
      target: { value: '299' },
    })
    fireEvent.change(screen.getByLabelText('超时秒数'), {
      target: { value: '5' },
    })
    fireEvent.change(screen.getByLabelText('频率档位'), {
      target: { value: '1m' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_002/probe-items', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      body: JSON.stringify({
        probe_kind: 'http',
        enabled: true,
        frequency_tier: '1m',
        timeout_seconds: 5,
        config: {
          scheme: 'https',
          path: '/healthz',
          method: 'GET',
          expected_status_range: [200, 299],
        },
      }),
    })
  })

  it('keeps ProbeItem creation validation errors inside the probe panel', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_002',
          name: 'Cache',
          target_type: 'service',
          host: 'cache.example.com',
          execution_node_labels: ['edge'],
          run_status: '启用',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_002']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: '添加 ProbeItem' }),
      ).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
    fireEvent.change(screen.getByLabelText('端口'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

    expect(screen.getByText('端口必须为正整数。')).toBeInTheDocument()
    expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('keeps target details visible when incidents and events fail to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_003',
            name: 'Payments',
            target_type: 'service',
            host: 'pay.example.com',
            base_port: 443,
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: ['core'],
            note: '',
            current_health_status: '告警',
            current_active_incident_count: 1,
            last_success_at: '2026-04-24T09:00:00Z',
            last_failure_at: '2026-04-24T09:04:00Z',
            current_primary_issue_summary: 'HTTP 探测失败',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              probe_item_id: 'pb_003',
              target_id: 'tg_003',
              probe_kind: 'http',
              enabled: true,
              frequency_tier: '1m',
              timeout_seconds: 5,
              config: { path: '/healthz', method: 'GET' },
              created_at: '2026-04-20T00:00:00Z',
              updated_at: '2026-04-24T09:05:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_003',
            latest_probe_observations: [
              {
                node_id: 'nd_003',
                target_id: 'tg_003',
                probe_item_id: 'pb_003',
                probe_kind: 'http',
                observed_at: '2026-04-24T09:05:00Z',
                received_at: '2026-04-24T09:05:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-003',
                result_kind: 'failure',
                latency_ms: 1200,
                http_status: 503,
                tls_expiry_days: null,
                error_summary: 'origin timeout',
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-003',
              },
            ],
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ error: 'incidents unavailable' }, 503),
        )
        .mockResolvedValueOnce(mockJSONResponse({ error: 'events unavailable' }, 503)),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_003']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Payments' })).toBeInTheDocument(),
    )

    expect(screen.getByText('ProbeItem 列表')).toBeInTheDocument()
    expect(screen.getByText('origin timeout')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '活跃异常暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('incidents unavailable')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '相关事件暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('events unavailable')).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: '目标详情不可用' }),
    ).not.toBeInTheDocument()
  })

  it('shows the new route core data without stale activity while route-specific requests are still in flight', async () => {
    const tg001Target = deferredResponse()
    const tg001ProbeItems = deferredResponse()
    const tg001Runtime = deferredResponse()
    const tg001Incidents = deferredResponse()
    const tg001Events = deferredResponse()
    const tg002Target = deferredResponse()
    const tg002ProbeItems = deferredResponse()
    const tg002Runtime = deferredResponse()
    const tg002Incidents = deferredResponse()
    const tg002Events = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockImplementationOnce(() => tg001Target.promise)
        .mockImplementationOnce(() => tg001ProbeItems.promise)
        .mockImplementationOnce(() => tg001Runtime.promise)
        .mockImplementationOnce(() => tg001Incidents.promise)
        .mockImplementationOnce(() => tg001Events.promise)
        .mockImplementationOnce(() => tg002Target.promise)
        .mockImplementationOnce(() => tg002ProbeItems.promise)
        .mockImplementationOnce(() => tg002Runtime.promise)
        .mockImplementationOnce(() => tg002Incidents.promise)
        .mockImplementationOnce(() => tg002Events.promise),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <TargetDetailTestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(10))

    tg002Target.resolve(
      mockJSONResponse({
        target_id: 'tg_002',
        name: 'Cache',
        target_type: 'service',
        host: 'cache.example.com',
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: ['infra'],
        note: '',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T10:00:00Z',
        last_failure_at: '2026-04-24T08:00:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T10:05:00Z',
      }),
    )
    tg002ProbeItems.resolve(
      mockJSONResponse([
        {
          probe_item_id: 'pb_002',
          target_id: 'tg_002',
          probe_kind: 'tcp',
          enabled: true,
          frequency_tier: '5m',
          timeout_seconds: 3,
          config: { port: 6379 },
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T10:05:00Z',
        },
      ]),
    )
    tg002Runtime.resolve(
      mockJSONResponse({
        target_id: 'tg_002',
        latest_probe_observations: [
          {
            node_id: 'nd_002',
            target_id: 'tg_002',
            probe_item_id: 'pb_002',
            probe_kind: 'tcp',
            observed_at: '2026-04-24T10:05:00Z',
            received_at: '2026-04-24T10:05:01Z',
            agent_version: 'dev',
            fingerprint: 'fp-002',
            result_kind: 'success',
            latency_ms: 32,
            http_status: null,
            tls_expiry_days: null,
            maintenance_context: false,
            is_backfilled: false,
            sync_batch_id: 'sync-002',
          },
        ],
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument(),
    )
    expect(screen.getByText('ProbeItem 列表')).toBeInTheDocument()
    expect(screen.getByText('正在加载活跃异常…')).toBeInTheDocument()
    expect(screen.getByText('正在加载相关事件…')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Blog' })).not.toBeInTheDocument()

    tg001Target.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: ['public'],
        note: '',
        current_health_status: '严重',
        current_active_incident_count: 1,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T09:04:00Z',
        current_primary_issue_summary: '旧目标异常',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:05:00Z',
      }),
    )
    tg001ProbeItems.resolve(
      mockJSONResponse([
        {
          probe_item_id: 'pb_old',
          target_id: 'tg_001',
          probe_kind: 'http',
          enabled: true,
          frequency_tier: '1m',
          timeout_seconds: 5,
          config: { path: '/old' },
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        },
      ]),
    )
    tg001Runtime.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        latest_probe_observations: [],
      }),
    )
    tg001Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_old',
          incident_class: 'target_probe_failure',
          object_type: 'target',
          object_id: 'tg_001',
          severity: '严重',
          started_at: '2026-04-24T09:00:00Z',
          last_evaluated_at: '2026-04-24T09:05:00Z',
          source_summary: '旧目标异常摘要',
        },
      ]),
    )
    tg001Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_old',
          incident_id: 'inc_old',
          incident_class: 'target_probe_failure',
          object_type: 'target',
          object_id: 'tg_001',
          event_type: 'incident_started',
          severity: '严重',
          summary: '旧目标事件',
          created_at: '2026-04-24T09:05:00Z',
        },
      ]),
    )

    tg002Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_new',
          incident_class: 'target_probe_failure',
          object_type: 'target',
          object_id: 'tg_002',
          severity: '关注',
          started_at: '2026-04-24T10:00:00Z',
          last_evaluated_at: '2026-04-24T10:05:00Z',
          source_summary: '新目标异常摘要',
        },
      ]),
    )
    tg002Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_new',
          incident_id: 'inc_new',
          incident_class: 'target_probe_failure',
          object_type: 'target',
          object_id: 'tg_002',
          event_type: 'incident_started',
          severity: '关注',
          summary: '新目标事件',
          created_at: '2026-04-24T10:05:00Z',
        },
      ]),
    )

    await waitFor(() =>
      expect(screen.getByText('新目标异常摘要')).toBeInTheDocument(),
    )
    expect(screen.getByText('新目标事件')).toBeInTheDocument()
    expect(screen.queryByText('旧目标异常摘要')).not.toBeInTheDocument()
    expect(screen.queryByText('旧目标事件')).not.toBeInTheDocument()
  })

  it('renders runtime controls and restores archived targets to paused from the detail page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archived',
          name: 'Legacy API',
          target_type: 'service',
          host: 'legacy.example.com',
          execution_node_labels: ['edge'],
          run_status: '已归档',
          labels: ['legacy'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archived',
          latest_probe_observations: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archived',
          name: 'Legacy API',
          target_type: 'service',
          host: 'legacy.example.com',
          execution_node_labels: ['edge'],
          run_status: '暂停',
          labels: ['legacy'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:15:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_archived']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Legacy API' })).toBeInTheDocument(),
    )

    expect(screen.getByRole('heading', { name: '运行控制' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复到暂停' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument(),
    )
    expect(screen.queryByRole('button', { name: '直接启用' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_archived/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('requires strong confirmation before archiving from the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
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
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
          latest_probe_observations: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
          name: 'Blog',
          target_type: 'service',
          host: 'blog.example.com',
          base_port: 443,
          execution_node_labels: ['edge'],
          run_status: '已归档',
          labels: ['public'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:20:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '归档' }))

    expect(confirmMock).toHaveBeenCalledWith('归档会让目标退出当前工作集，但会保留历史记录，确定继续吗？')
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('ignores a stale runtime-action error after switching to a different target route', async () => {
    const runtimeAction = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_001',
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
            last_success_at: '2026-04-24T09:00:00Z',
            last_failure_at: '2026-04-24T08:30:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_001',
            latest_probe_observations: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => runtimeAction.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            name: 'Cache',
            target_type: 'service',
            host: 'cache.example.com',
            execution_node_labels: ['edge'],
            run_status: '暂停',
            labels: ['infra'],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-24T10:00:00Z',
            last_failure_at: '2026-04-24T08:00:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T10:05:00Z',
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            latest_probe_observations: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <TargetDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('button', { name: '恢复' })).toBeEnabled()

    runtimeAction.resolve(mockJSONResponse({ error: 'runtime action failed' }, 409))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('runtime action failed')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复' })).toBeEnabled()
  })
})
