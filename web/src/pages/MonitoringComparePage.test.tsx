import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, type InitialEntry } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { MonitoringComparePage } from './MonitoringComparePage'
import { MonitoringDetailPage } from './MonitoringDetailPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function monitoringInstanceRecord(monitoringInstanceId: string, displayName: string, overrides: Partial<Record<string, unknown>> = {}) {
  return {
    monitoring_instance_id: monitoringInstanceId,
    display_name: displayName,
    group: 'edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: ['edge'],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-05-15T08:00:00Z',
    updated_at: '2026-05-15T08:05:00Z',
    ...overrides,
  }
}

function hostSample(monitoringInstanceId: string) {
  return {
    monitoring_instance_id: monitoringInstanceId,
    observed_at: '2026-05-15T08:05:00Z',
    received_at: '2026-05-15T08:05:02Z',
    agent_version: 'dev',
    fingerprint: `${monitoringInstanceId}-fingerprint`,
    cpu_usage_pct: 22,
    load_1: 0.2,
    load_5: 0.3,
    load_15: 0.4,
    mem_used_pct: 48,
    mem_available_bytes: 2147483648,
    mem_total_bytes: 8589934592,
    swap_used_pct: 0,
    disk_used_pct: 51,
    disk_total_bytes: 107374182400,
    inode_used_pct: 12,
    net_in_bytes_per_sec: 1024,
    net_out_bytes_per_sec: 2048,
    cpu_iowait_pct: 0.5,
    cpu_steal_pct: 0.1,
    disk_read_bytes_per_sec: 3072,
    disk_write_bytes_per_sec: 4096,
    disk_busy_pct: 3,
    uptime_seconds: 7200,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: `${monitoringInstanceId}-sync`,
  }
}

function runtimeFacts(monitoringInstanceId: string) {
  const sample = hostSample(monitoringInstanceId)
  return {
    monitoring_instance_id: monitoringInstanceId,
    latest_host_sample: sample,
    window: {
      key: '24h',
      started_at: '2026-05-14T08:05:00Z',
      ended_at: '2026-05-15T08:05:00Z',
      bucket_count: 288,
      available_started_at: sample.observed_at,
      available_ended_at: sample.observed_at,
      sample_count: 1,
    },
    host_metric_points: [
      {
        observed_at: sample.observed_at,
        sample_count: 1,
        cpu_usage_pct: sample.cpu_usage_pct,
        mem_used_pct: sample.mem_used_pct,
        disk_used_pct: sample.disk_used_pct,
        inode_used_pct: sample.inode_used_pct,
        load_5: sample.load_5,
        cpu_iowait_pct: sample.cpu_iowait_pct,
        net_in_bytes_per_sec: sample.net_in_bytes_per_sec,
        net_out_bytes_per_sec: sample.net_out_bytes_per_sec,
      },
    ],
  }
}

function renderMonitoringCompare(initialEntry: InitialEntry = '/monitoring/compare') {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/monitoring/compare" element={<MonitoringComparePage />} />
        <Route path="/monitoring" element={<div>monitoring route</div>} />
        <Route path="/monitoring/:monitoringInstanceId" element={<div>monitoring detail route</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('MonitoringComparePage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('uses a PageState empty state when two monitoring instance IDs are not selected', () => {
    renderMonitoringCompare('/monitoring/compare?id=mi_a')

    expect(screen.getByRole('heading', { name: '需要选择 2 个监控实例' })).toBeInTheDocument()
    expect(screen.getByText('请先在监控实例列表勾选两个监控实例，再进入 A / B 指标对比。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute('href', '/monitoring')
  })

  it('renders command context, A/B summary, and metric placeholders from monitoring instance/runtime facts', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_a', 'Tokyo Edge')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('mi_a')))
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_b', 'Osaka Core', {
        current_health_status: '告警',
        group: 'core',
        region: 'ap-northeast-3',
        city: 'Osaka',
        provider: 'AWS',
      })))
      .mockResolvedValueOnce(mockJSONResponse({
        monitoring_instance_id: 'mi_b',
        latest_host_sample: null,
        window: {
          key: '24h',
          started_at: '2026-05-14T08:05:00Z',
          ended_at: '2026-05-15T08:05:00Z',
          bucket_count: 288,
          available_started_at: null,
          available_ended_at: null,
          sample_count: 0,
        },
        host_metric_points: [],
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderMonitoringCompare('/monitoring/compare?id=mi_a&id=mi_b')

    await waitFor(() => expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '判断两个监控实例是否需要深入排查' })).toBeInTheDocument()
    expect(screen.getByText('监控实例对比 · 24 小时运行事实')).toBeInTheDocument()
    expect(screen.getByText('先对齐 A/B 的身份、健康、运行态、绑定态、位置与样本可用性；只有差异明显时再下钻详细主机指标。')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'A/B 摘要判断' })).toBeInTheDocument()
    expect(screen.getByText('默认先看状态与样本是否可比；详细图表保留在下方。')).toBeInTheDocument()
    expect(screen.getByText('对比对象 A')).toBeInTheDocument()
    expect(screen.getByText('对比对象 B')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Osaka Core' })).toHaveAttribute('href', '/monitoring/mi_b')
    expect(screen.getAllByRole('link', { name: '监控实例详情' })).toHaveLength(2)
    expect(screen.getAllByText('健康状态')).toHaveLength(2)
    expect(screen.getAllByText('接入阶段')).toHaveLength(2)
    expect(screen.getAllByText('运行 / 绑定')).toHaveLength(2)
    expect(screen.getAllByText('位置上下文')).toHaveLength(2)
    expect(screen.getAllByText('样本可用性')).toHaveLength(2)
    expect(screen.getAllByText('edge · Vultr · ap-northeast-1 · Tokyo')).toHaveLength(2)
    expect(screen.getAllByText('core · AWS · ap-northeast-3 · Osaka')).toHaveLength(2)
    expect(screen.getByText('有样本')).toBeInTheDocument()
    expect(screen.getByText('无样本')).toBeInTheDocument()
    expect(screen.getByText(/窗口样本/)).toBeInTheDocument()
    expect(screen.getByText('24 小时运行事实暂无主机样本')).toBeInTheDocument()
    expect(screen.getByText('详细趋势仍使用 MonitoringInstanceWatchtowerMetrics')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'CPU 使用率' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '尚未收到主机样本' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_a', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_a/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances/mi_b', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/monitoring-instances/mi_b/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows a PageState error when one side cannot be loaded', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_a', 'Tokyo Edge')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('mi_a')))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'monitoringInstance not found' }, 404))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'monitoringInstance not found' }, 404))
    vi.stubGlobal('fetch', fetchMock)

    renderMonitoringCompare('/monitoring/compare?id=mi_a&id=mi_missing')

    await waitFor(() => expect(screen.getByRole('heading', { name: 'B 监控实例不可用' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '判断两个监控实例是否需要深入排查' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'B 摘要不可用' })).toBeInTheDocument()
    expect(screen.getAllByText('监控实例不存在').length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: '返回监控实例列表重新选择' })).toHaveAttribute('href', '/monitoring')
  })
  it('preserves listHref and location.state through compare return link', async () => {
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_a&selected=mi_b&q=Tokyo&sort=name_asc',
      vpsInventoryHref: '/vps?workspace=workbench&selected=vps_001',
      return_vps: 'vps_001',
    }

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_a', 'Tokyo Edge')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('mi_a')))
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_b', 'Osaka Core')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('mi_b')))
    vi.stubGlobal('fetch', fetchMock)

    function ListProbe() {
      const loc = useLocation()
      return (
        <div data-testid="list-probe" data-state={JSON.stringify(loc.state)}>
          {loc.pathname}{loc.search}
        </div>
      )
    }

    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/monitoring/compare',
          search: '?id=mi_a&id=mi_b',
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/monitoring/compare" element={<MonitoringComparePage />} />
          <Route path="/monitoring" element={<ListProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toBeInTheDocument())
    const returnLink = screen.getByRole('link', { name: '返回监控实例列表' })
    expect(returnLink).toHaveAttribute(
      'href',
      '/monitoring?view=abnormal&selected=mi_a&selected=mi_b&q=Tokyo&sort=name_asc',
    )
    fireEvent.click(returnLink)

    expect(screen.getByTestId('list-probe')).toHaveTextContent(
      '/monitoring?view=abnormal&selected=mi_a&selected=mi_b&q=Tokyo&sort=name_asc',
    )
    expect(JSON.parse(screen.getByTestId('list-probe').getAttribute('data-state')!)).toEqual(navState)
  })

  it('carries validated return_vps from compare query onto detail links and restores header source VPS', async () => {
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_a&selected=mi_b&q=Tokyo&sort=name_asc',
      vpsInventoryHref: '/vps?workspace=workbench&selected=vps_001',
      return_vps: 'vps_SHOULD_NOT_USE',
    }

    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_a') return mockJSONResponse(monitoringInstanceRecord('mi_a', 'Tokyo Edge'))
      if (path === '/api/monitoring-instances/mi_b') return mockJSONResponse(monitoringInstanceRecord('mi_b', 'Osaka Core'))
      if (path === '/api/monitoring-instances/mi_a/runtime-facts?window=24h') return mockJSONResponse(runtimeFacts('mi_a'))
      if (path === '/api/monitoring-instances/mi_b/runtime-facts?window=24h') return mockJSONResponse(runtimeFacts('mi_b'))
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_a') return mockJSONResponse([])
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_a') return mockJSONResponse([])
      if (path === '/api/monitoring-instances/mi_a/vps') return mockJSONResponse([])
      if (path === '/api/settings') {
        return mockJSONResponse({
          incident_defaults: {
            heartbeat_interval_seconds: 30,
            stale_threshold_intervals: 3,
          },
        })
      }
      return mockJSONResponse({ error: `unexpected ${path}` }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)

    function LocationProbe() {
      const loc = useLocation()
      return (
        <div data-testid="detail-probe" data-state={JSON.stringify(loc.state)}>
          {loc.pathname}{loc.search}
        </div>
      )
    }

    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/monitoring/compare',
          search: '?id=mi_a&id=mi_b&return_vps=vps_tokyo_origin',
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/monitoring/compare" element={<MonitoringComparePage />} />
          <Route
            path="/monitoring/:monitoringInstanceId"
            element={(
              <>
                <LocationProbe />
                <MonitoringDetailPage />
              </>
            )}
          />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toBeInTheDocument())

    const detailLinks = screen.getAllByRole('link', { name: '监控实例详情' })
    expect(detailLinks[0]).toHaveAttribute('href', '/monitoring/mi_a?return_vps=vps_tokyo_origin')
    expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toHaveAttribute(
      'href',
      '/monitoring/mi_a?return_vps=vps_tokyo_origin',
    )
    expect(detailLinks[0]?.getAttribute('href')).not.toContain('id=')
    expect(detailLinks[0]?.getAttribute('href')).not.toContain('window=')

    fireEvent.click(screen.getByRole('link', { name: 'Tokyo Edge' }))

    await waitFor(() => expect(screen.getByRole('link', { name: '返回来源 VPS' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '返回来源 VPS' })).toHaveAttribute('href', '/vps/vps_tokyo_origin')
    expect(screen.getByTestId('detail-probe')).toHaveTextContent('/monitoring/mi_a?return_vps=vps_tokyo_origin')
    expect(screen.getByTestId('detail-probe')).not.toHaveTextContent('id=')
    expect(JSON.parse(screen.getByTestId('detail-probe').getAttribute('data-state')!)).toEqual(navState)
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_a/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('keeps local chart tooltips on compare hover', async () => {
    const factsA = runtimeFacts('mi_a')
    const first = factsA.host_metric_points[0]!
    factsA.host_metric_points = [
      { ...first, observed_at: '2026-05-15T08:00:00Z', cpu_usage_pct: 20 },
      { ...first, observed_at: '2026-05-15T08:05:00Z', cpu_usage_pct: 22 },
    ]
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_a', 'Tokyo Edge')))
        .mockResolvedValueOnce(mockJSONResponse(factsA))
        .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord('mi_b', 'Osaka Core')))
        .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('mi_b'))),
    )
    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/compare?id=mi_a&id=mi_b']}>
        <Routes>
          <Route path="/monitoring/compare" element={<MonitoringComparePage />} />
        </Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getAllByRole('heading', { name: 'CPU 使用率' }).length).toBeGreaterThan(0))
    const svg = container.querySelector('.compare-metrics svg')
    expect(svg).toBeTruthy()
    svg!.getBoundingClientRect = () => ({
      x: 0, y: 0, top: 0, left: 0, right: 360, bottom: 160, width: 360, height: 160, toJSON: () => ({}),
    })
    fireEvent.mouseMove(svg!, { clientX: 180 })
    expect(container.querySelector('.compare-metrics .metric-chart__tooltip')).not.toBeNull()
  })
})
