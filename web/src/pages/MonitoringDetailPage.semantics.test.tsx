import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getSettings } from '../lib/api'
import type { SettingsRecord } from '../lib/types'
import { MonitoringDetailPage } from './MonitoringDetailPage'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    getSettings: vi.fn().mockResolvedValue({
      incident_defaults: {
        heartbeat_interval_seconds: 30,
        stale_threshold_intervals: 3,
        sweep_interval_seconds: 30,
        notify_on_started: true,
        notify_on_escalated: true,
        notify_on_recovered: true,
        cpu_warning_pct: 80,
        cpu_alert_pct: 90,
        cpu_critical_pct: 95,
        mem_warning_pct: 85,
        mem_alert_pct: 92,
        mem_critical_pct: 95,
        disk_warning_pct: 85,
        disk_alert_pct: 92,
        disk_critical_pct: 97,
        inode_warning_pct: 80,
        inode_alert_pct: 90,
        inode_critical_pct: 95,
        iowait_warning_pct: 20,
        iowait_critical_pct: 50,
        load5_warning: 4,
        load5_critical: 8,
      },
    }),
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

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function record(overrides: Record<string, unknown> = {}) {
  return {
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-24T09:00:00Z',
    last_sync_at: '2026-04-24T09:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

function sample(overrides: Record<string, unknown> = {}) {
  return {
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-04-24T10:00:00Z',
    received_at: '2026-04-24T10:00:01Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    cpu_usage_pct: 12.5,
    load_1: 0.2,
    load_5: 0.3,
    load_15: 0.4,
    mem_used_pct: 40,
    mem_available_bytes: 1,
    mem_total_bytes: 2,
    swap_used_pct: 0,
    disk_used_pct: 30,
    disk_total_bytes: 4,
    inode_used_pct: 5,
    net_in_bytes_per_sec: 0,
    net_out_bytes_per_sec: 0,
    network_rates_valid: true,
    cpu_iowait_pct: 1,
    cpu_steal_pct: 0,
    disk_read_bytes_per_sec: 0,
    disk_write_bytes_per_sec: 0,
    disk_busy_pct: 0,
    uptime_seconds: 3600,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-1',
    ...overrides,
  }
}

function facts(windowKey: string, extra: Record<string, unknown> = {}) {
  return {
    monitoring_instance_id: 'mi_001',
    read_at: '2026-04-24T10:00:05Z',
    latest_host_sample: sample(),
    window: {
      key: windowKey,
      started_at: '2026-04-23T10:00:00Z',
      ended_at: '2026-04-24T10:00:00Z',
      bucket_count: 2,
      available_started_at: '2026-04-23T10:00:00Z',
      available_ended_at: '2026-04-24T10:00:00Z',
      sample_count: 2,
    },
    host_metric_points: [],
    recent_host_samples: windowKey === 'realtime' ? [sample()] : [],
    ...extra,
  }
}

class MockRuntimeWebSocket {
  static instances: MockRuntimeWebSocket[] = []
  readonly url: string
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  close = vi.fn()
  constructor(url: string) {
    this.url = url
    MockRuntimeWebSocket.instances.push(this)
  }
  emitOpen() { this.onopen?.() }
  emitMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>)
  }
  emitClose() { this.onclose?.() }
}

function renderPage(entry = '/monitoring/mi_001') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockRuntimeWebSocket.instances = []
  vi.mocked(getSettings).mockResolvedValue({
    incident_defaults: {
      heartbeat_interval_seconds: 30,
      stale_threshold_intervals: 3,
      sweep_interval_seconds: 30,
      notify_on_started: true,
      notify_on_escalated: true,
      notify_on_recovered: true,
      cpu_warning_pct: 80,
      cpu_alert_pct: 90,
      cpu_critical_pct: 95,
      mem_warning_pct: 85,
      mem_alert_pct: 92,
      mem_critical_pct: 95,
      disk_warning_pct: 85,
      disk_alert_pct: 92,
      disk_critical_pct: 97,
      inode_warning_pct: 80,
      inode_alert_pct: 90,
      inode_critical_pct: 95,
      iowait_warning_pct: 20,
      iowait_critical_pct: 50,
      load5_warning: 4,
      load5_critical: 8,
    },
  } as SettingsRecord)
})

describe('MonitoringDetailPage source semantics', () => {
  it('keeps the record available and retries when runtime facts fail', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return mockJSONResponse(record())
      if (path.includes('/runtime-facts')) return mockJSONResponse({ error: 'facts down' }, 500)
      if (path.includes('/incidents') || path.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse({ error: 'unhandled' }, 404)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '重试运行指标' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试运行指标' }))
    await waitFor(() => expect(fetchMock.mock.calls.filter((call) => String(call[0]).includes('/runtime-facts')).length).toBeGreaterThan(1))
  })

  it('does not label a late 24h payload as 7d after a failed rapid window switch', async () => {
    const facts24 = deferred<Response>()
    const facts7 = deferred<Response>()
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return Promise.resolve(mockJSONResponse(record()))
      if (path.endsWith('runtime-facts?window=24h')) return facts24.promise
      if (path.endsWith('runtime-facts?window=7d')) return facts7.promise
      if (path.includes('/incidents') || path.includes('/events')) return Promise.resolve(mockJSONResponse([]))
      return Promise.resolve(mockJSONResponse({ error: 'unhandled' }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { container } = renderPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '7d' }))
    await waitFor(() => expect(fetchMock.mock.calls.some((call) => String(call[0]).includes('window=7d'))).toBe(true))
    await act(async () => {
      facts7.reject(new Error('7d failed'))
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '重试运行指标' })).toBeInTheDocument())
    await act(async () => {
      facts24.resolve(mockJSONResponse(facts('24h', {
        host_metric_points: [{
          observed_at: '2026-04-24T00:00:00Z',
          sample_count: 4,
          cpu_usage_pct: 66,
          mem_used_pct: 40,
          disk_used_pct: 30,
          inode_used_pct: 5,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: 0,
          net_out_bytes_per_sec: 0,
        }],
      })))
    })
    expect(screen.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true')
    // The late 24h payload is not relabelled as the requested 7d window.
    expect(screen.queryByText('66.0%')).not.toBeInTheDocument()
    expect(screen.queryByText(/近 24h/)).not.toBeInTheDocument()
    expect(screen.queryByText(/近 7d · 0 个原始样本/)).not.toBeInTheDocument()
    expect(screen.getByText('运行指标不可用。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试运行指标' })).toBeInTheDocument()
    // No retained window means no fabricated sample either.
    expect(container.querySelectorAll('.metric-chart__line--secondary').length).toBe(0)
  })

  it('keeps latest sample time after a successful 24h window then a failed 7d read', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return mockJSONResponse(record())
      if (path.endsWith('runtime-facts?window=24h')) return mockJSONResponse(facts('24h'))
      if (path.endsWith('runtime-facts?window=7d')) return mockJSONResponse({ error: '7d down' }, 500)
      if (path.includes('/incidents') || path.includes('/events')) return mockJSONResponse([])
      return mockJSONResponse({ error: 'unhandled' }, 404)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await waitFor(() => expect(screen.getByText(/已运行 1小时 0分钟/)).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '7d' }))
    await waitFor(() => expect(screen.getByText('运行指标不可用。')).toBeInTheDocument())
    // The sample itself is independent of the failed window and stays readable.
    expect(screen.getByText(/已运行 1小时 0分钟/)).toBeInTheDocument()
    expect(screen.getByText(/当前样本/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试运行指标' })).toBeInTheDocument()
  })

  it('clears prior-epoch latest when a newer HTTP snapshot returns null', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return mockJSONResponse(record())
      if (path.endsWith('runtime-facts?window=24h')) return mockJSONResponse(facts('24h'))
      if (path.endsWith('runtime-facts?window=7d')) {
        return mockJSONResponse(facts('7d', {
          read_at: '2026-04-24T10:05:00Z',
          latest_host_sample: null,
          host_metric_points: [],
          window: {
            key: '7d',
            started_at: '2026-04-17T10:00:00Z',
            ended_at: '2026-04-24T10:00:00Z',
            bucket_count: 2,
            available_started_at: null,
            available_ended_at: null,
            sample_count: 0,
          },
        }))
      }
      return mockJSONResponse([])
    })
    vi.stubGlobal('fetch', fetchMock)
    const { container } = renderPage()
    await waitFor(() => expect(screen.getByText(/已运行 1小时 0分钟/)).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '7d' }))
    await waitFor(() => expect(screen.getByText('当前样本 尚无')).toBeInTheDocument())
    expect(screen.queryByText(/已运行 1小时 0分钟/)).not.toBeInTheDocument()
    // The heartbeat is its own evidence and is never replaced by the sample time.
    const heartbeat = container.querySelector('.monitoring-detail-status__heartbeat')?.textContent ?? ''
    expect(heartbeat).toContain('心跳')
    expect(heartbeat).not.toContain('—')
  })

  it('does not let a late older HTTP snapshot overwrite a live sample received after read_at', async () => {
    MockRuntimeWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockRuntimeWebSocket)
    let factsCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return mockJSONResponse(record())
      if (path.includes('/runtime-facts')) {
        factsCalls += 1
        if (factsCalls === 1) {
          return mockJSONResponse(facts('realtime', {
            read_at: '2026-04-24T10:00:05Z',
            latest_host_sample: sample({ observed_at: '2026-04-24T10:00:00Z', received_at: '2026-04-24T10:00:01Z', cpu_usage_pct: 12.5 }),
          }))
        }
        return mockJSONResponse(facts('realtime', {
          read_at: '2026-04-24T10:00:10Z',
          latest_host_sample: sample({ observed_at: '2026-04-24T10:00:00Z', received_at: '2026-04-24T10:00:01Z', cpu_usage_pct: 9, sync_batch_id: 'http-old' }),
        }))
      }
      return mockJSONResponse([])
    }))
    renderPage('/monitoring/mi_001?window=realtime')
    await waitFor(() => expect(MockRuntimeWebSocket.instances.length).toBe(1))
    const socket = MockRuntimeWebSocket.instances[0]
    if (!socket) throw new Error('expected websocket')
    act(() => socket.emitOpen())
    act(() => {
      socket.emitMessage({
        type: 'host_sample',
        monitoring_instance_id: 'mi_001',
        sample: sample({ observed_at: '2026-04-24T10:00:20Z', received_at: '2026-04-24T10:00:21Z', cpu_usage_pct: 41, sync_batch_id: 'live' }),
        received_at: '2026-04-24T10:00:21Z',
      })
    })
    await waitFor(() => expect(screen.getAllByText('41.0%').length).toBeGreaterThan(0))
    act(() => socket.emitClose())
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 2100))
    })
    await waitFor(() => expect(MockRuntimeWebSocket.instances.length).toBe(2))
    act(() => MockRuntimeWebSocket.instances[1]?.emitOpen())
    await waitFor(() => expect(factsCalls).toBeGreaterThan(1))
    expect(screen.getAllByText('41.0%').length).toBeGreaterThan(0)
    expect(screen.queryByText('9.0%')).not.toBeInTheDocument()
  })

  it('surfaces policy failure without inventing heartbeat freshness', async () => {
    vi.mocked(getSettings).mockRejectedValue(new Error('settings down'))
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') return mockJSONResponse(record({ last_heartbeat_at: '2026-04-24T09:00:00Z' }))
      if (path.includes('/runtime-facts')) return mockJSONResponse(facts('24h'))
      return mockJSONResponse([])
    }))
    const { container } = renderPage()
    await waitFor(() => expect(screen.getByRole('button', { name: '重试策略' })).toBeInTheDocument())
    expect(screen.getByText(/阈值策略不可用/)).toBeInTheDocument()
    // Without a policy nothing is coloured by an invented threshold.
    expect(container.querySelectorAll('.metric-chart__alert-band').length).toBe(0)
    const heartbeat = container.querySelector('.monitoring-detail-status__heartbeat')?.textContent ?? ''
    expect(heartbeat).toContain('心跳')
    expect(heartbeat).not.toContain('数据陈旧')
  })

  it('treats missing heartbeat as unknown health and does not copy sample time onto heartbeat', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') {
        return mockJSONResponse(record({ last_heartbeat_at: undefined, current_health_status: '正常' }))
      }
      if (path.includes('/runtime-facts')) return mockJSONResponse(facts('24h'))
      return mockJSONResponse([])
    }))
    const { container } = renderPage()
    await waitFor(() => expect(container.querySelector('.monitoring-detail-status__health--unknown')).not.toBeNull())
    expect(container.querySelector('.monitoring-detail-status__health--unknown')).toHaveTextContent('未知')
    expect(screen.getByText(/未收到心跳/)).toBeInTheDocument()
    // The sample time is not copied onto the heartbeat.
    const heartbeat = container.querySelector('.monitoring-detail-status__heartbeat')?.textContent ?? ''
    expect(heartbeat).toContain('未收到心跳')
    expect(heartbeat).not.toContain('10:00')
  })

  it('rejects an out-of-order stream sample and never overwrites record heartbeat', async () => {
    MockRuntimeWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockRuntimeWebSocket)
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_001') {
        return mockJSONResponse(record({ last_heartbeat_at: '2026-04-24T09:00:00Z' }))
      }
      if (path.includes('/runtime-facts')) return mockJSONResponse(facts('realtime'))
      return mockJSONResponse([])
    }))
    const { container } = renderPage('/monitoring/mi_001?window=realtime')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    await waitFor(() => expect(MockRuntimeWebSocket.instances.length).toBe(1))
    const socket = MockRuntimeWebSocket.instances[0]
    if (!socket) throw new Error('expected websocket')
    act(() => socket.emitOpen())
    act(() => {
      socket.emitMessage({
        type: 'host_sample',
        monitoring_instance_id: 'mi_001',
        sample: sample({ observed_at: '2026-04-24T10:00:10Z', cpu_usage_pct: 41, sync_batch_id: 'live' }),
        received_at: '2026-04-24T10:00:11Z',
      })
      socket.emitMessage({
        type: 'host_sample',
        monitoring_instance_id: 'mi_001',
        sample: sample({ observed_at: '2026-04-24T09:50:00Z', cpu_usage_pct: 9, sync_batch_id: 'old' }),
        received_at: '2026-04-24T10:00:12Z',
      })
    })
    await waitFor(() => expect(screen.getAllByText('41.0%').length).toBeGreaterThan(0))
    expect(screen.queryByText('9.0%')).not.toBeInTheDocument()
    expect(container.querySelector('.monitoring-detail-status__health--unknown')).toBeNull()
    expect(container.querySelector('.monitoring-detail-status__health--normal')).toHaveTextContent('正常')
    expect(screen.queryByText(/未收到心跳/)).not.toBeInTheDocument()
  })
})
