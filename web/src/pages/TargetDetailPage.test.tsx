import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

function probeActionButton(action: string, probeItemId = 'pb_001') {
  return screen.getByRole('button', {
    name: new RegExp(`^${action} ProbeItem ${probeItemId}\\b`),
  })
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
    expect(screen.getAllByText('标签与备注')[0]).toBeInTheDocument()
    expect(screen.getAllByText('ProbeItem 列表')[0]).toBeInTheDocument()
    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('83 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.getByText('nd_001')).toBeInTheDocument()
    expect(screen.queryByText('当前还没有 ProbeItem')).not.toBeInTheDocument()
    expect(screen.queryByText('事件与 incident 仍由后续切片接入，这里先保留版位。')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/targets/tg_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_001/probe-items', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/targets/tg_001/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/incidents?object_type=target&object_id=tg_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      '/api/events?object_type=target&object_id=tg_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
  })


  it('renders recent latency trends grouped by probe item', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_trend',
            name: 'Trend Target',
            target_type: 'service',
            host: 'trend.example.com',
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
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              probe_item_id: 'pb_http',
              target_id: 'tg_trend',
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
            target_id: 'tg_trend',
            latest_probe_observations: [],
            recent_probe_observations: [
              {
                node_id: 'nd_001',
                target_id: 'tg_trend',
                probe_item_id: 'pb_http',
                probe_kind: 'http',
                observed_at: '2026-04-24T09:05:00Z',
                received_at: '2026-04-24T09:05:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-001',
                result_kind: 'success',
                latency_ms: 80,
                http_status: 200,
                tls_expiry_days: null,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-001',
              },
              {
                node_id: 'nd_002',
                target_id: 'tg_trend',
                probe_item_id: 'pb_http',
                probe_kind: 'http',
                observed_at: '2026-04-24T09:10:00Z',
                received_at: '2026-04-24T09:10:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-002',
                result_kind: 'success',
                latency_ms: 120,
                http_status: 200,
                tls_expiry_days: null,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-002',
              },
              {
                node_id: 'nd_002',
                target_id: 'tg_trend',
                probe_item_id: 'pb_http',
                probe_kind: 'http',
                observed_at: '2026-04-24T09:11:00Z',
                received_at: '2026-04-24T09:11:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-002',
                result_kind: 'timeout',
                latency_ms: null,
                http_status: null,
                tls_expiry_days: null,
                error_code: 'timeout',
                error_summary: 'timeout',
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-003',
              },
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_trend']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Trend Target' })).toBeInTheDocument(),
    )

    // watchtower metric-card heading shows kind label "HTTP · <config summary>"
    const trendCardHeading = screen
      .getAllByRole('heading')
      .find((heading) => /^HTTP · /.test(heading.textContent ?? ''))
    expect(trendCardHeading).toBeDefined()
    // latest latency 120 ms appears in metric-card head; mean 100 ms appears in stats
    expect(screen.getAllByText('120 ms').length).toBeGreaterThan(0)
    expect(screen.getByText('100 ms')).toBeInTheDocument()
    expect(screen.getByText('平均')).toBeInTheDocument()
    expect(screen.getByText('最大')).toBeInTheDocument()
    expect(screen.getByText('样本数')).toBeInTheDocument()
    expect(screen.getByText('覆盖节点')).toBeInTheDocument()
  })

  it('renders an empty state when no recent latency samples are available', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_empty',
            name: 'Empty Trend Target',
            target_type: 'service',
            host: 'empty.example.com',
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
            target_id: 'tg_empty',
            latest_probe_observations: [],
            recent_probe_observations: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_empty']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Empty Trend Target' })).toBeInTheDocument(),
    )

    expect(
      screen.getByRole('heading', { name: '近 24h 暂无可用延迟样本' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '该目标尚未收到带有 latency_ms 的成功观测，或所有 ProbeItem 当前均处于停用状态。',
      ),
    ).toBeInTheDocument()
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
    fireEvent.change(screen.getByLabelText('HTTP 协议'), {
      target: { value: 'https' },
    })
    fireEvent.change(screen.getByLabelText('HTTP 路径'), {
      target: { value: '/healthz' },
    })
    fireEvent.change(screen.getByLabelText('HTTP 方法'), {
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
        credentials: 'include',
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

  it('defaults a created TLS ProbeItem frequency to 6h after switching the probe kind', async () => {
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
            probe_item_id: 'pb_tls',
            target_id: 'tg_002',
            probe_kind: 'tls',
            enabled: true,
            frequency_tier: '6h',
            timeout_seconds: 5,
            config: {
              port: 443,
              expiry_warning_days: 14,
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
      target: { value: 'tls' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('6h')
    fireEvent.change(screen.getByLabelText('端口'), {
      target: { value: '443' },
    })
    fireEvent.change(screen.getByLabelText('证书预警天数'), {
      target: { value: '14' },
    })
    fireEvent.change(screen.getByLabelText('超时秒数'), {
      target: { value: '5' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建 ProbeItem' }))

    await waitFor(() => expect(screen.getByText('TLS')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_002/probe-items', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        probe_kind: 'tls',
        enabled: true,
        frequency_tier: '6h',
        timeout_seconds: 5,
        config: {
          port: 443,
          expiry_warning_days: 14,
        },
      }),
    })
  })

  it('keeps HTTP and TCP create-mode frequency defaults at 5m when switching probe kinds', async () => {
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

    fireEvent.click(screen.getByRole('button', { name: '添加 ProbeItem' }))
    expect(screen.getByLabelText('频率档位')).toHaveValue('5m')

    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'tls' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('6h')

    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'http' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('5m')

    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'tcp' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('5m')
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

  it('edits an existing ProbeItem and replaces the row after save', async () => {
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
            },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          probe_item_id: 'pb_001',
          target_id: 'tg_001',
          probe_kind: 'http',
          enabled: true,
          frequency_tier: '5m',
          timeout_seconds: 8,
          config: {
            scheme: 'https',
            path: '/ready',
            method: 'HEAD',
            expected_status_range: [200, 204],
          },
          created_at: '2026-04-21T00:00:00Z',
          updated_at: '2026-04-27T10:00:00Z',
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

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('编辑'))
    expect(screen.getAllByText('ProbeItem 编辑')[0]).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '编辑 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByLabelText('HTTP 路径')).toHaveValue('/healthz')

    fireEvent.change(screen.getByLabelText('HTTP 路径'), { target: { value: '/ready' } })
    fireEvent.change(screen.getByLabelText('HTTP 方法'), { target: { value: 'HEAD' } })
    fireEvent.change(screen.getByLabelText('期望状态码终点'), { target: { value: '204' } })
    fireEvent.change(screen.getByLabelText('超时秒数'), { target: { value: '8' } })
    fireEvent.change(screen.getByLabelText('频率档位'), { target: { value: '5m' } })
    fireEvent.click(screen.getByRole('button', { name: '保存 ProbeItem' }))

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '编辑 ProbeItem' })).not.toBeInTheDocument(),
    )
    expect(screen.getAllByText(/path: \/ready/).length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/probe-items/pb_001', {
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        probe_kind: 'http',
        enabled: true,
        frequency_tier: '5m',
        timeout_seconds: 8,
        config: {
          scheme: 'https',
          path: '/ready',
          method: 'HEAD',
          expected_status_range: [200, 204],
        },
      }),
    })
  })

  it('preserves the stored frequency tier in edit mode when the probe kind changes', async () => {
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
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
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
              frequency_tier: '15m',
              timeout_seconds: 5,
              config: {
                scheme: 'https',
                path: '/healthz',
                method: 'GET',
                expected_status_range: [200, 299],
              },
              created_at: '2026-04-21T00:00:00Z',
              updated_at: '2026-04-21T00:00:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('编辑'))
    expect(screen.getByRole('heading', { name: '编辑 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByLabelText('频率档位')).toHaveValue('15m')

    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'tls' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('15m')

    fireEvent.change(screen.getByLabelText('Probe 类型'), {
      target: { value: 'tcp' },
    })
    expect(screen.getByLabelText('频率档位')).toHaveValue('15m')
  })

  it('blocks edit when an existing ProbeItem contains unsupported config fields', async () => {
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
              headers: { 'X-Probe': 'houfeng' },
            },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('编辑'))

    expect(
      screen.getByText('ProbeItem 包含当前 V1 表单不支持的配置字段，不能安全编辑。'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '编辑 ProbeItem' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('keeps row ProbeItem actions disabled while a form save is pending', async () => {
    const saveResponse = deferredResponse()
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
            },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => saveResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('编辑'))
    fireEvent.change(screen.getByLabelText('HTTP 路径'), { target: { value: '/ready' } })
    fireEvent.click(screen.getByRole('button', { name: '保存 ProbeItem' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))

    expect(screen.getByRole('button', { name: '正在保存…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '添加 ProbeItem' })).toBeDisabled()
    expect(probeActionButton('编辑')).toBeDisabled()
    expect(probeActionButton('停用')).toBeDisabled()
    expect(probeActionButton('删除')).toBeDisabled()

    saveResponse.resolve(
      mockJSONResponse({
        probe_item_id: 'pb_001',
        target_id: 'tg_001',
        probe_kind: 'http',
        enabled: true,
        frequency_tier: '1m',
        timeout_seconds: 5,
        config: {
          scheme: 'https',
          path: '/ready',
          method: 'GET',
          expected_status_range: [200, 299],
        },
        created_at: '2026-04-21T00:00:00Z',
        updated_at: '2026-04-27T10:00:00Z',
      }),
    )

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '编辑 ProbeItem' })).not.toBeInTheDocument(),
    )
  })

  it('serializes row ProbeItem mutations across multiple rows', async () => {
    const rowUpdate = deferredResponse()
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
            },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
          {
            probe_item_id: 'pb_002',
            target_id: 'tg_001',
            probe_kind: 'tcp',
            enabled: true,
            frequency_tier: '5m',
            timeout_seconds: 3,
            config: { port: 443 },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => rowUpdate.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())
    expect(screen.getByText('TCP')).toBeInTheDocument()

    fireEvent.click(probeActionButton('停用', 'pb_001'))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))
    expect(probeActionButton('编辑', 'pb_001')).toBeDisabled()
    expect(probeActionButton('停用', 'pb_001')).toBeDisabled()
    expect(probeActionButton('删除', 'pb_001')).toBeDisabled()
    expect(probeActionButton('编辑', 'pb_002')).toBeDisabled()
    expect(probeActionButton('停用', 'pb_002')).toBeDisabled()
    expect(probeActionButton('删除', 'pb_002')).toBeDisabled()

    fireEvent.click(probeActionButton('停用', 'pb_002'))
    expect(fetchMock).toHaveBeenCalledTimes(6)

    rowUpdate.resolve(
      mockJSONResponse({
        probe_item_id: 'pb_001',
        target_id: 'tg_001',
        probe_kind: 'http',
        enabled: false,
        frequency_tier: '1m',
        timeout_seconds: 5,
        config: {
          scheme: 'https',
          path: '/healthz',
          method: 'GET',
          expected_status_range: [200, 299],
        },
        created_at: '2026-04-21T00:00:00Z',
        updated_at: '2026-04-27T10:00:00Z',
      }),
    )

    await waitFor(() => expect(probeActionButton('启用', 'pb_001')).toBeEnabled())
    expect(probeActionButton('停用', 'pb_002')).toBeEnabled()
    expect(fetchMock).toHaveBeenCalledTimes(6)
  })

  it('disables a ProbeItem with a full update and preserves the existing config', async () => {
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            config: {
              scheme: 'https',
              path: '/healthz',
              method: 'GET',
              expected_status_range: [200, 299],
            },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          probe_item_id: 'pb_001',
          target_id: 'tg_001',
          probe_kind: 'http',
          enabled: false,
          frequency_tier: '1m',
          timeout_seconds: 5,
          config: {
            scheme: 'https',
            path: '/healthz',
            method: 'GET',
            expected_status_range: [200, 299],
          },
          created_at: '2026-04-21T00:00:00Z',
          updated_at: '2026-04-27T10:00:00Z',
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

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('停用'))

    await waitFor(() => expect(screen.getByText('停用')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/probe-items/pb_001', {
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        probe_kind: 'http',
        enabled: false,
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

  it('uses an inline stateful confirmation before deleting a ProbeItem', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            probe_kind: 'tcp',
            enabled: true,
            frequency_tier: '5m',
            timeout_seconds: 3,
            config: { port: 443 },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({}, 204))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('TCP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('删除'))

    const confirmation = screen.getByRole('alertdialog', { name: '确认删除 ProbeItem' })
    expect(confirmation).toBeInTheDocument()
    expect(screen.getByText('当前：这条 ProbeItem 仍属于当前目标。')).toBeInTheDocument()
    expect(screen.getByText('操作后：这条观测方式会被移除。')).toBeInTheDocument()
    expect(
      screen.getByText('仅用于误建场景。删除后该 ProbeItem 不再产生新的观测记录。'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('不会删除目标，也不会删除既有事件或历史观测记录。'),
    ).toBeInTheDocument()
    expect(within(confirmation).getByText('port: 443')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(probeActionButton('删除')).toHaveFocus())
    expect(fetchMock).toHaveBeenCalledTimes(5)

    fireEvent.click(probeActionButton('删除'))
    fireEvent.click(screen.getByRole('button', { name: '确认删除 ProbeItem' }))

    expect(confirmSpy).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument(),
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '添加 ProbeItem' })).toHaveFocus(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/probe-items/pb_001', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps ProbeItem errors local and leaves delete confirmation visible when delete fails', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
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
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
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
            probe_kind: 'tcp',
            enabled: true,
            frequency_tier: '5m',
            timeout_seconds: 3,
            config: { port: 443 },
            created_at: '2026-04-21T00:00:00Z',
            updated_at: '2026-04-21T00:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'update failed' }, 503))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'delete failed' }, 503))
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

    fireEvent.click(probeActionButton('停用'))
    await waitFor(() => expect(screen.getByText('update failed')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument()

    fireEvent.click(probeActionButton('删除'))
    fireEvent.click(screen.getByRole('button', { name: '确认删除 ProbeItem' }))

    expect(confirmSpy).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByText('delete failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument()
    expect(screen.getAllByText('ProbeItem 列表')[0]).toBeInTheDocument()
  })

  it('prevents opening a ProbeItem delete confirmation while a runtime confirmation is active', async () => {
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
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
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
              probe_kind: 'tcp',
              enabled: true,
              frequency_tier: '5m',
              timeout_seconds: 3,
              config: { port: 443 },
              created_at: '2026-04-21T00:00:00Z',
              updated_at: '2026-04-21T00:00:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('TCP')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(probeActionButton('删除')).toBeDisabled()
    fireEvent.click(probeActionButton('删除'))
    expect(screen.queryByRole('alertdialog', { name: '确认删除 ProbeItem' })).not.toBeInTheDocument()
  })

  it('prevents opening a runtime confirmation while a ProbeItem delete confirmation is active', async () => {
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
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
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
              probe_kind: 'tcp',
              enabled: true,
              frequency_tier: '5m',
              timeout_seconds: 3,
              config: { port: 443 },
              created_at: '2026-04-21T00:00:00Z',
              updated_at: '2026-04-21T00:00:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('TCP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('删除'))

    expect(screen.getByRole('alertdialog', { name: '确认删除 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '暂停' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '归档' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    expect(screen.queryByRole('alertdialog', { name: '确认暂停目标监控' })).not.toBeInTheDocument()
  })

  it('ignores stale ProbeItem save results after switching to another target route', async () => {
    const tg001Target = deferredResponse()
    const tg001ProbeItems = deferredResponse()
    const tg001Runtime = deferredResponse()
    const tg001Incidents = deferredResponse()
    const tg001Events = deferredResponse()
    const staleSave = deferredResponse()
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
        .mockImplementationOnce(() => staleSave.promise)
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

    tg001Target.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
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
    tg001ProbeItems.resolve(
      mockJSONResponse([
        {
          probe_item_id: 'pb_001',
          target_id: 'tg_001',
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
          created_at: '2026-04-21T00:00:00Z',
          updated_at: '2026-04-21T00:00:00Z',
        },
      ]),
    )
    tg001Runtime.resolve(
      mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
    )
    tg001Incidents.resolve(mockJSONResponse([]))
    tg001Events.resolve(mockJSONResponse([]))

    await waitFor(() => expect(screen.getByText('HTTP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('编辑'))
    fireEvent.change(screen.getByLabelText('HTTP 路径'), { target: { value: '/stale' } })
    fireEvent.click(screen.getByRole('button', { name: '保存 ProbeItem' }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(6))

    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(11))

    tg002Target.resolve(
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
          created_at: '2026-04-21T00:00:00Z',
          updated_at: '2026-04-24T10:05:00Z',
        },
      ]),
    )
    tg002Runtime.resolve(
      mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }),
    )
    tg002Incidents.resolve(mockJSONResponse([]))
    tg002Events.resolve(mockJSONResponse([]))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument(),
    )

    staleSave.resolve(
      mockJSONResponse({
        probe_item_id: 'pb_001',
        target_id: 'tg_001',
        probe_kind: 'http',
        enabled: true,
        frequency_tier: '1m',
        timeout_seconds: 5,
        config: {
          scheme: 'https',
          path: '/stale',
          method: 'GET',
          expected_status_range: [200, 299],
        },
        created_at: '2026-04-21T00:00:00Z',
        updated_at: '2026-04-27T10:00:00Z',
      }),
    )

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '编辑 ProbeItem' })).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Cache')).toBeInTheDocument()
    expect(screen.getByText('TCP')).toBeInTheDocument()
    expect(screen.queryByText('/stale')).not.toBeInTheDocument()
    expect(screen.queryByText('update failed')).not.toBeInTheDocument()
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

    expect(screen.getAllByText('ProbeItem 列表')[0]).toBeInTheDocument()
    expect(screen.getByText('origin timeout')).toBeInTheDocument()
    // Danger zone is rendered because current_active_incident_count > 0
    expect(screen.getByText('HTTP 探测失败')).toBeInTheDocument()
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
    expect(screen.getAllByText('ProbeItem 列表')[0]).toBeInTheDocument()
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
      expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument(),
    )
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

    fireEvent.click(screen.getByRole('button', { name: '恢复到暂停' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_archived/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('uses an inline stateful confirmation before pausing a target from the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_pause',
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
          target_id: 'tg_pause',
          latest_probe_observations: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_pause',
          name: 'Blog',
          target_type: 'service',
          host: 'blog.example.com',
          base_port: 443,
          execution_node_labels: ['edge'],
          run_status: '暂停',
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
      <MemoryRouter initialEntries={['/targets/tg_pause']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：目标运行状态为启用。')).toBeInTheDocument()
    expect(screen.getByText('操作后：目标运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText('会停止该目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。'),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 ProbeItem 配置。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '暂停' })).toHaveFocus())
    expect(fetchMock).toHaveBeenCalledTimes(5)

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_pause/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps the pause confirmation visible and local error when pause fails on the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_pause_fail',
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
      .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_pause_fail', latest_probe_observations: [] }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'pause failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_pause_fail']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByText('pause failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认暂停目标监控' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_pause_fail/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('uses an inline stateful confirmation before archiving from the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archive',
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
          target_id: 'tg_archive',
          latest_probe_observations: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archive',
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
      <MemoryRouter initialEntries={['/targets/tg_archive']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )

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
    expect(fetchMock).toHaveBeenCalledTimes(5)

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复到暂停' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_archive/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps the archive confirmation visible and local error when archive fails on the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archive_fail',
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
      .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_archive_fail', latest_probe_observations: [] }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'archive failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_archive_fail']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByText('archive failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认归档目标' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_archive_fail/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('ignores a stale confirmed pause action after switching to a different target route', async () => {
    const pauseAction = deferredResponse()

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
        .mockImplementationOnce(() => pauseAction.promise)
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

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停目标' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())

    pauseAction.resolve(mockJSONResponse({ error: 'pause failed' }, 409))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    expect(screen.queryByText('pause failed')).not.toBeInTheDocument()
    expect(screen.queryByRole('alertdialog', { name: '确认暂停目标监控' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复' })).not.toHaveFocus()
    expect(screen.getByRole('button', { name: '归档' })).not.toHaveFocus()
  })

  it('ignores a stale confirmed archive action after switching to a different target route', async () => {
    const archiveAction = deferredResponse()

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
        .mockImplementationOnce(() => archiveAction.promise)
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

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())

    archiveAction.resolve(mockJSONResponse({ error: 'archive failed' }, 409))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    expect(screen.queryByText('archive failed')).not.toBeInTheDocument()
    expect(screen.queryByRole('alertdialog', { name: '确认归档目标' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复' })).not.toHaveFocus()
    expect(screen.getByRole('button', { name: '归档' })).not.toHaveFocus()
  })

  it('ignores a stale confirmed ProbeItem delete after switching to a different target route', async () => {
    const deleteAction = deferredResponse()

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
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
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
              probe_kind: 'tcp',
              enabled: true,
              frequency_tier: '5m',
              timeout_seconds: 3,
              config: { port: 443 },
              created_at: '2026-04-21T00:00:00Z',
              updated_at: '2026-04-21T00:00:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => deleteAction.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            name: 'Cache',
            target_type: 'service',
            host: 'cache.example.com',
            execution_node_labels: ['edge'],
            run_status: '暂停',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T10:05:00Z',
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <TargetDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('TCP')).toBeInTheDocument())

    fireEvent.click(probeActionButton('删除'))
    fireEvent.click(screen.getByRole('button', { name: '确认删除 ProbeItem' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())

    deleteAction.resolve(mockJSONResponse({ error: 'delete failed' }, 503))

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    expect(screen.queryByText('delete failed')).not.toBeInTheDocument()
    expect(screen.queryByRole('alertdialog', { name: '确认删除 ProbeItem' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复' })).not.toHaveFocus()
    expect(screen.getByRole('button', { name: '归档' })).not.toHaveFocus()
    expect(screen.getByRole('button', { name: '添加 ProbeItem' })).not.toHaveFocus()
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



  it('renders empty note copy with the 备注 label in target detail metadata', async () => {
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
            labels: ['公开'],
            note: '   ',
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
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '标签与备注' })).toBeInTheDocument(),
    )

    expect(screen.getByText('备注：暂无备注')).toBeInTheDocument()
  })

  it('edits target labels and note from the detail page', async () => {
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
          note: '现网入口',
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
          run_status: '启用',
          labels: ['alpha', 'beta'],
          note: '新的备注',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-24T09:00:00Z',
          last_failure_at: '2026-04-24T08:30:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:30:00Z',
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

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument())

    expect(screen.getByRole('heading', { name: '标签与备注' })).toBeInTheDocument()
    expect(screen.getByText('公开')).toBeInTheDocument()
    expect(screen.getByText('备注：现网入口')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.change(screen.getByLabelText('备注'), {
      target: { value: '  新的备注  ' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() => expect(screen.getByText('alpha · beta')).toBeInTheDocument())
    expect(screen.getByText('备注：新的备注')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': '"2026-04-24T09:05:00Z"',
      },
      cache: 'no-store',
        credentials: 'include',
      body: JSON.stringify({
        labels: ['alpha', 'beta'],
        note: '新的备注',
      }),
    })
  })

  it('shows a metadata error when target detail label or note update fails', async () => {
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
          note: '现网入口',
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
      .mockResolvedValueOnce(mockJSONResponse({ error: 'save failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('save failed'),
    )
    expect(screen.getByDisplayValue('alpha, beta')).toBeInTheDocument()
  })

  it('ignores a stale metadata save response after switching to a different target route', async () => {
    const metadataAction = deferredResponse()
    const targetTwoResponse = deferredResponse()

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
            labels: ['公开'],
            note: '现网入口',
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
          mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => metadataAction.promise)
        .mockImplementationOnce(() => targetTwoResponse.promise)
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_002', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <TargetDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.change(screen.getByLabelText('备注'), {
      target: { value: '新的备注' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch target' }))

    metadataAction.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: ['alpha', 'beta'],
        note: '新的备注',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T08:30:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:30:00Z',
      }),
    )

    targetTwoResponse.resolve(
      mockJSONResponse({
        target_id: 'tg_002',
        name: 'Cache',
        target_type: 'service',
        host: 'cache.example.com',
        execution_node_labels: ['edge'],
        run_status: '暂停',
        labels: ['内部'],
        note: '缓存入口',
        current_health_status: '正常',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T10:05:00Z',
      }),
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Cache' })).toBeInTheDocument())
    expect(screen.queryByText('备注：新的备注')).not.toBeInTheDocument()
    expect(screen.getByText('备注：缓存入口')).toBeInTheDocument()
  })

  it('preserves a successful metadata save across an unrelated target refresh', async () => {
    const metadataAction = deferredResponse()
    const runtimeAction = deferredResponse()
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
          note: '现网入口',
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
      .mockImplementationOnce(() => metadataAction.promise)
      .mockImplementationOnce(() => runtimeAction.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.change(screen.getByLabelText('备注'), {
      target: { value: '新的备注' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))

    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(7))

    runtimeAction.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '维护中',
        labels: ['公开'],
        note: '现网入口',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T08:30:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:10:00Z',
      }),
    )

    await waitFor(() => expect(screen.getByText('维护中')).toBeInTheDocument())

    metadataAction.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: ['alpha', 'beta'],
        note: '新的备注',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T08:30:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:12:00Z',
      }),
    )

    await waitFor(() => expect(screen.getByText('alpha · beta')).toBeInTheDocument())
    expect(screen.getByText('备注：新的备注')).toBeInTheDocument()
    expect(screen.getByText('维护中')).toBeInTheDocument()
    expect(screen.queryByText('备注：现网入口')).not.toBeInTheDocument()
  })

  it('preserves saved metadata when a later runtime response returns stale labels and note', async () => {
    const metadataAction = deferredResponse()
    const runtimeAction = deferredResponse()
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
          note: '现网入口',
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
      .mockImplementationOnce(() => metadataAction.promise)
      .mockImplementationOnce(() => runtimeAction.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '编辑标签与备注' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('标签'), {
      target: { value: 'alpha, beta' },
    })
    fireEvent.change(screen.getByLabelText('备注'), {
      target: { value: '新的备注' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))

    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(7))

    metadataAction.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '启用',
        labels: ['alpha', 'beta'],
        note: '新的备注',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T08:30:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:12:00Z',
      }),
    )

    await waitFor(() => expect(screen.getByText('alpha · beta')).toBeInTheDocument())
    expect(screen.getByText('备注：新的备注')).toBeInTheDocument()

    runtimeAction.resolve(
      mockJSONResponse({
        target_id: 'tg_001',
        name: 'Blog',
        target_type: 'service',
        host: 'blog.example.com',
        base_port: 443,
        execution_node_labels: ['edge'],
        run_status: '维护中',
        labels: ['公开'],
        note: '现网入口',
        current_health_status: '正常',
        current_active_incident_count: 0,
        last_success_at: '2026-04-24T09:00:00Z',
        last_failure_at: '2026-04-24T08:30:00Z',
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:15:00Z',
      }),
    )

    await waitFor(() => expect(screen.getByText('维护中')).toBeInTheDocument())
    expect(screen.getByText('alpha · beta')).toBeInTheDocument()
    expect(screen.queryByText('备注：现网入口')).not.toBeInTheDocument()
  })

  it('does not render the danger zone when current_active_incident_count is 0', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_dz_no',
            name: 'No Issues',
            target_type: 'service',
            host: 'no-issues.example.com',
            execution_node_labels: [],
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
          mockJSONResponse({ target_id: 'tg_dz_no', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_dz_no']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'No Issues' })).toBeInTheDocument(),
    )

    expect(screen.queryByText('当前主问题')).not.toBeInTheDocument()
    expect(
      document.querySelector('.watchtower-danger'),
    ).not.toBeInTheDocument()
  })

  it('renders the danger zone with summary and status badge when active incidents exist', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_dz_yes',
            name: 'Has Issues',
            target_type: 'service',
            host: 'has-issues.example.com',
            execution_node_labels: [],
            run_status: '启用',
            labels: [],
            note: '',
            current_health_status: '告警',
            current_active_incident_count: 3,
            current_primary_issue_summary: 'HTTP 探测持续失败',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({ target_id: 'tg_dz_yes', latest_probe_observations: [] }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              incident_id: 'inc_dz',
              incident_class: 'target_probe_failure',
              object_type: 'target',
              object_id: 'tg_dz_yes',
              severity: '告警',
              started_at: '2026-04-24T08:00:00Z',
              last_evaluated_at: '2026-04-24T09:00:00Z',
              source_summary: 'HTTP 探测持续失败',
            },
          ]),
        )
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_dz_yes']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Has Issues' })).toBeInTheDocument(),
    )

    const dangerZone = document.querySelector('.watchtower-danger')
    expect(dangerZone).toBeInTheDocument()
    expect(
      within(dangerZone as HTMLElement).getByText('当前主问题'),
    ).toBeInTheDocument()
    expect(
      within(dangerZone as HTMLElement).getByText('HTTP 探测持续失败'),
    ).toBeInTheDocument()
    expect(within(dangerZone as HTMLElement).getByText('3')).toBeInTheDocument()
    expect(within(dangerZone as HTMLElement).getByText('告警')).toBeInTheDocument()
  })

  it('renders secondary details sections collapsed by default', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_collapsed',
            name: 'Collapsed Target',
            target_type: 'service',
            host: 'collapsed.example.com',
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: ['test'],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              probe_item_id: 'pb_collapsed',
              target_id: 'tg_collapsed',
              probe_kind: 'tcp',
              enabled: true,
              frequency_tier: '5m',
              timeout_seconds: 3,
              config: { port: 443 },
              created_at: '2026-04-20T00:00:00Z',
              updated_at: '2026-04-24T09:05:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_collapsed',
            latest_probe_observations: [
              {
                node_id: 'nd_col',
                target_id: 'tg_collapsed',
                probe_item_id: 'pb_collapsed',
                probe_kind: 'tcp',
                observed_at: '2026-04-24T09:05:00Z',
                received_at: '2026-04-24T09:05:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-col',
                result_kind: 'success',
                latency_ms: 10,
                http_status: null,
                tls_expiry_days: null,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-col',
              },
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_collapsed']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Collapsed Target' })).toBeInTheDocument(),
    )

    const secondaryDetails = document.querySelectorAll('.watchtower-secondary')
    expect(secondaryDetails.length).toBeGreaterThanOrEqual(3)

    for (const details of secondaryDetails) {
      expect(details).not.toHaveAttribute('open')
    }
  })

  // ── Time window Tabs ──

  it('renders time window Tabs with 24h / 7d / 30d options', async () => {
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
          last_failure_at: null,
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
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_001',
          latest_probe_observations: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
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

    const tablist = screen.getByRole('tablist')
    expect(tablist).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '24h' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: '7d' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('tab', { name: '30d' })).toHaveAttribute('aria-selected', 'false')
  })

})
