import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { NodeDetailPage } from './NodeDetailPage'
import { formatDateTime } from '../lib/format'

class MockIntersectionObserver implements IntersectionObserver {
  private readonly callback: IntersectionObserverCallback
  readonly root = null
  readonly rootMargin = ''
  readonly scrollMargin = ''
  readonly thresholds = []

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback
  }

  observe(target: Element) {
    this.callback([
      {
        isIntersecting: true,
        target,
        intersectionRatio: 1,
        boundingClientRect: target.getBoundingClientRect(),
        intersectionRect: target.getBoundingClientRect(),
        rootBounds: null,
        time: Date.now(),
      } as IntersectionObserverEntry,
    ], this)
  }

  disconnect() {}
  takeRecords() { return [] }
  unobserve() {}
}

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

function nodeRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    node_id: 'nd_conflict',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '指纹变更待确认',
    labels: ['core'],
    note: '',
    current_health_status: '关注',
    last_heartbeat_at: '2026-04-27T09:00:00Z',
    last_sync_at: '2026-04-27T09:05:00Z',
    current_active_incident_count: 1,
    current_primary_issue_summary: '检测到新的指纹接入请求',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-27T09:05:00Z',
    ...overrides,
  }
}

function emptyRuntimeFacts(nodeId = 'nd_conflict') {
  return {
    node_id: nodeId,
    latest_host_sample: null,
  }
}

function onboardingConflictState(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    ...nodeRecord(),
    phase: '绑定冲突待处理',
    has_host_sample: true,
    has_accepted_observation: true,
    enrollment_token_issued_at: '2026-04-26T08:00:00Z',
    current_binding_fingerprint_summary: 'fp-current-1234567890',
    pending_binding: {
      fingerprint: 'fp-pending-abcdefghijklmnopqrstuvwxyz',
      first_seen_at: '2026-04-27T08:55:00Z',
      last_seen_at: '2026-04-27T09:04:00Z',
      attempt_count: 4,
    },
    ...overrides,
  }
}

async function waitForEnabledButton(name: string) {
  await waitFor(() => expect(screen.getByRole('button', { name })).toBeEnabled())
  return screen.getByRole('button', { name })
}

/**
 * Watchtower header puts runtime controls (维护 / 暂停 / 恢复) inside a
 * <details><summary aria-label="运行控制操作">…</summary>...</details>
 * popover. Opening the popover is required before tests can click those buttons.
 */
function openRuntimeMenu() {
  const summary = screen.getByLabelText('运行控制操作')
  fireEvent.click(summary)
}

/**
 * Open one of the secondary collapsible blocks (标签与备注 / 生命周期 / 接入凭证状态).
 * Clicks the trigger regardless of whether the same label appears elsewhere
 * (e.g. NodeLabelsAndNote also renders a "标签与备注" h2 inside the body).
 */
function openSecondary(label: string) {
  const matches = screen.getAllByText(label)
  const trigger =
    matches.find((node) => node.closest('button.collapsible-section__trigger')) ??
    matches.find((node) => node.tagName === 'SUMMARY')
  const clickable = trigger?.tagName === 'BUTTON' ? trigger : trigger?.closest('button, summary')
  if (!clickable) {
    throw new Error(`No collapsible trigger matching "${label}" found`)
  }
  fireEvent.click(clickable)
}

function NodeDetailTestHarness() {
  const navigate = useNavigate()

  return (
    <>
      <button type="button" onClick={() => navigate('/nodes/nd_002')}>
        switch node
      </button>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
      </Routes>
    </>
  )
}

describe('NodeDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads linked VPS summaries when the asset ledger section is visible', async () => {
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['核心', 'edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          latest_host_sample: null,
          recent_host_samples: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            vps_id: 'vps_001',
            display_name: 'Tokyo Edge VPS',
            provider_id: 'pv_001',
            provider_name: 'Hetzner',
            country: 'JP',
            region: 'Kanto',
            city: 'Tokyo',
            lifecycle_status: 'active',
            usage_status: 'in_use',
            renewal_decision: 'keep',
            importance: 'normal',
            labels: ['asset-ledger'],
            archived_at: null,
            linked_at: '2026-04-24T09:06:00Z',
            note: 'primary host',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge VPS')).toBeInTheDocument())
    expect(screen.getByText('primary host')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_001/vps', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('renders node header and latest host sample cards', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['核心', 'edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          latest_host_sample: {
            node_id: 'nd_001',
            observed_at: '2026-04-24T09:05:00Z',
            received_at: '2026-04-24T09:05:02Z',
            agent_version: 'dev',
            fingerprint: 'fp-001',
            cpu_usage_pct: 12.5,
            load_1: 0.3,
            load_5: 0.4,
            load_15: 0.5,
            mem_used_pct: 65,
            mem_available_bytes: 2147483648,
            swap_used_pct: 0,
            disk_used_pct: 52,
            inode_used_pct: 11,
            net_in_bytes_per_sec: 1024,
            net_out_bytes_per_sec: 2048,
            cpu_iowait_pct: 0.4,
            cpu_steal_pct: 0.1,
            disk_read_bytes_per_sec: 3072,
            disk_write_bytes_per_sec: 4096,
            disk_busy_pct: 3,
            uptime_seconds: 7200,
            maintenance_context: false,
            is_backfilled: false,
            sync_batch_id: 'sync-001',
          },
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            incident_id: 'inc_001',
            incident_class: 'node_disk_pressure',
            object_type: 'node',
            object_id: 'nd_001',
            severity: '告警',
            started_at: '2026-04-24T08:50:00Z',
            last_evaluated_at: '2026-04-24T09:05:00Z',
            source_summary: '磁盘使用率持续超过阈值',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            event_id: 'evt_001',
            incident_id: 'inc_001',
            incident_class: 'node_disk_pressure',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'incident_escalated',
            severity: '严重',
            summary: '磁盘压力已升级为严重',
            created_at: '2026-04-24T09:04:00Z',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('正在加载节点详情…')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Watchtower header surfaces uptime in mono "数据新鲜度" and node_id in mono row 2
    expect(screen.getByText('2小时 0分钟')).toBeInTheDocument()
    // Each metric card head renders the current value via MonoDigits
    expect(screen.getAllByText('12.5%').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText(/2.0 GB/i)).toBeInTheDocument()
    expect(screen.queryByText('将在 incidents / events 切片接入后替换为真实内容。')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/incidents?object_type=node&object_id=nd_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/events?object_type=node&object_id=nd_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
  })


  it('renders sparklines and a sample-count meta line when recent host samples are present', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_trend',
            display_name: 'Trend Node',
            region: 'ap-east-1',
            city: 'Hong Kong',
            provider: 'Vultr',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
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
          mockJSONResponse({
            node_id: 'nd_trend',
            latest_host_sample: {
              node_id: 'nd_trend',
              observed_at: '2026-04-24T10:05:00Z',
              received_at: '2026-04-24T10:05:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-trend',
              cpu_usage_pct: 21,
              load_1: 0.8,
              load_5: 1.6,
              load_15: 1.9,
              mem_used_pct: 62,
              mem_available_bytes: 1073741824,
              swap_used_pct: 0,
              disk_used_pct: 41,
              inode_used_pct: 9,
              net_in_bytes_per_sec: 1024,
              net_out_bytes_per_sec: 2048,
              cpu_iowait_pct: 6,
              cpu_steal_pct: 1.4,
              disk_read_bytes_per_sec: 3072,
              disk_write_bytes_per_sec: 4096,
              disk_busy_pct: 8,
              uptime_seconds: 7200,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-trend-latest',
            },
            recent_host_samples: [
              {
                node_id: 'nd_trend',
                observed_at: '2026-04-24T10:05:00Z',
                received_at: '2026-04-24T10:05:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-trend',
                cpu_usage_pct: 21,
                load_1: 0.8,
                load_5: 1.6,
                load_15: 1.9,
                mem_used_pct: 62,
                mem_available_bytes: 1073741824,
                swap_used_pct: 0,
                disk_used_pct: 41,
                inode_used_pct: 9,
                net_in_bytes_per_sec: 1024,
                net_out_bytes_per_sec: 2048,
                cpu_iowait_pct: 6,
                cpu_steal_pct: 1.4,
                disk_read_bytes_per_sec: 3072,
                disk_write_bytes_per_sec: 4096,
                disk_busy_pct: 8,
                uptime_seconds: 7200,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-trend-1',
              },
              {
                node_id: 'nd_trend',
                observed_at: '2026-04-24T09:35:00Z',
                received_at: '2026-04-24T09:35:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-trend',
                cpu_usage_pct: 18,
                load_1: 0.7,
                load_5: 1.2,
                load_15: 1.5,
                mem_used_pct: 58,
                mem_available_bytes: 2147483648,
                swap_used_pct: 0,
                disk_used_pct: 40,
                inode_used_pct: 8,
                net_in_bytes_per_sec: 900,
                net_out_bytes_per_sec: 1800,
                cpu_iowait_pct: 4,
                cpu_steal_pct: 1.1,
                disk_read_bytes_per_sec: 2048,
                disk_write_bytes_per_sec: 3072,
                disk_busy_pct: 6,
                uptime_seconds: 5400,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-trend-2',
              },
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_trend']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Trend Node' })).toBeInTheDocument(),
    )

    // Watchtower main view: 8 metric cards in 4×2 grid; each renders a MetricChart svg
    const cards = container.querySelectorAll('.watchtower-metrics .watchtower-metric-card')
    expect(cards.length).toBe(8)
    // With 2 ascending samples, each card's MetricChart draws a polyline
    expect(container.querySelectorAll('.watchtower-metrics polyline').length).toBe(8)
  })

  it('renders an empty state when no recent host samples are available', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(nodeRecord({ node_id: 'nd_empty', display_name: 'Empty Trend Node', binding_status: '已绑定' })))
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_empty',
            latest_host_sample: null,
            recent_host_samples: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_empty']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Empty Trend Node' })).toBeInTheDocument(),
    )

    // Sample is null → watchtower metrics renders the no-sample empty state.
    expect(screen.getByRole('heading', { name: '尚未收到主机样本' })).toBeInTheDocument()
    expect(container.querySelectorAll('.watchtower-metric-card').length).toBe(0)
  })

  it('renders first-sync, incident, and event empty states when no related records exist yet', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '待接入',
            monitoring_status: '启用',
            binding_status: '未绑定',
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
          mockJSONResponse({
            node_id: 'nd_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_002']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByText('尚未收到主机样本')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('该节点已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。'),
    ).toBeInTheDocument()
  })

  it('keeps node details visible when incidents and events fail to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_003',
            display_name: 'Singapore Edge',
            region: 'ap-southeast-1',
            city: 'Singapore',
            provider: 'AWS',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
            labels: ['sea'],
            note: '',
            current_health_status: '关注',
            last_heartbeat_at: '2026-04-24T09:00:00Z',
            last_sync_at: '2026-04-24T09:05:00Z',
            current_active_incident_count: 1,
            current_primary_issue_summary: '磁盘使用率偏高',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_003',
            latest_host_sample: {
              node_id: 'nd_003',
              observed_at: '2026-04-24T09:05:00Z',
              received_at: '2026-04-24T09:05:02Z',
              agent_version: 'dev',
              fingerprint: 'fp-003',
              cpu_usage_pct: 18,
              load_1: 0.8,
              load_5: 0.6,
              load_15: 0.5,
              mem_used_pct: 71,
              mem_available_bytes: 1073741824,
              swap_used_pct: 2,
              disk_used_pct: 88,
              inode_used_pct: 23,
              net_in_bytes_per_sec: 2048,
              net_out_bytes_per_sec: 1024,
              cpu_iowait_pct: 1.1,
              cpu_steal_pct: 0.2,
              disk_read_bytes_per_sec: 4096,
              disk_write_bytes_per_sec: 8192,
              disk_busy_pct: 15,
              uptime_seconds: 10800,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-003',
            },
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({ error: 'incidents unavailable' }, 503),
        )
        .mockResolvedValueOnce(mockJSONResponse({ error: 'events unavailable' }, 503)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_003']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Singapore Edge' }),
      ).toBeInTheDocument(),
    )

    // Watchtower main view still renders metric cards even when incidents/events fail
    expect(screen.getAllByText('18.0%').length).toBeGreaterThanOrEqual(1)
    expect(
      screen.queryByRole('heading', { name: '节点详情不可用' }),
    ).not.toBeInTheDocument()
  })

  it('renders a high-priority binding conflict card on Node detail', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument(),
    )

    expect(screen.getAllByText('绑定冲突')[0]).toBeInTheDocument()
    expect(screen.getByText('高优先级：绑定冲突待处理')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('fp-current-1234567890')).toBeInTheDocument())
    expect(screen.getByText('fp-pendi…uvwxyz')).toBeInTheDocument()
    expect(screen.getByText(formatDateTime('2026-04-27T08:55:00Z'))).toBeInTheDocument()
    expect(screen.getByText(formatDateTime('2026-04-27T09:04:00Z'))).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(screen.getByText(/同一台机器重装或合法替换/)).toBeInTheDocument()
    // Secondary <details> summaries surface the standard sections (still collapsed)
    expect(screen.getAllByText('标签与备注')[0]).toBeInTheDocument()
    expect(screen.getByText('生命周期')).toBeInTheDocument()
    expect(screen.getByText('接入凭证状态')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开接入工作台' })).toHaveAttribute(
      'href',
      '/nodes/nd_conflict/onboarding',
    )
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps Node detail visible when binding conflict metadata fails to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse({ error: 'onboarding unavailable' }, 503)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByText('onboarding unavailable')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '节点详情不可用' })).not.toBeInTheDocument()
  })

  it('keeps binding actions disabled until conflict metadata is loaded', async () => {
    const onboarding = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => onboarding.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('button', { name: '确认重绑定' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '拒绝新指纹' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeDisabled()

    onboarding.resolve(mockJSONResponse(onboardingConflictState()))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '确认重绑定' })).toBeEnabled(),
    )
    expect(screen.getByRole('button', { name: '拒绝新指纹' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeEnabled()
  })


  it('confirms a pending node rebind from Node detail and hides the conflict card', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
      .mockResolvedValueOnce(
        mockJSONResponse(
          onboardingConflictState({
            binding_status: '已绑定',
            phase: '已绑定，等待稳定观测',
            pending_binding: undefined,
            updated_at: '2026-04-27T09:20:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('确认重绑定'))

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    // "已绑定" StatusBadge surfaces in multiple places (header badge row + 接入凭证状态 summary)
    expect(screen.getAllByText('已绑定').length).toBeGreaterThanOrEqual(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('rejects a pending fingerprint from Node detail', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
      .mockResolvedValueOnce(
        mockJSONResponse(
          onboardingConflictState({ binding_status: '已绑定', pending_binding: undefined }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const rejectButton = await waitForEnabledButton('拒绝新指纹')
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeEnabled()

    fireEvent.click(rejectButton)
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/reject-pending', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
  })

  it('resets node binding from Node detail and returns to the unbound state', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
      .mockResolvedValueOnce(
        mockJSONResponse(
          onboardingConflictState({
            binding_status: '未绑定',
            phase: '未开始接入',
            pending_binding: undefined,
            current_binding_fingerprint_summary: '',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('重置绑定'))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/reset', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    // "未绑定" StatusBadge surfaces in multiple places (header + 接入凭证状态 summary)
    expect(screen.getAllByText('未绑定').length).toBeGreaterThanOrEqual(1)
  })

  it('keeps binding action errors local to the conflict card', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid binding transition' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('重置绑定'))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('invalid binding transition'),
    )
    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
  })

  it('shows the new route core data without stale activity while route-specific requests are still in flight', async () => {
    const nd001Node = deferredResponse()
    const nd001Runtime = deferredResponse()
    const nd001Incidents = deferredResponse()
    const nd001Events = deferredResponse()
    const nd002Node = deferredResponse()
    const nd002Runtime = deferredResponse()
    const nd002Incidents = deferredResponse()
    const nd002Events = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockImplementationOnce(() => nd001Node.promise)
        .mockImplementationOnce(() => nd001Runtime.promise)
        .mockImplementationOnce(() => nd001Incidents.promise)
        .mockImplementationOnce(() => nd001Events.promise)
        .mockImplementationOnce(() => nd002Node.promise)
        .mockImplementationOnce(() => nd002Runtime.promise)
        .mockImplementationOnce(() => nd002Incidents.promise)
        .mockImplementationOnce(() => nd002Events.promise),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <NodeDetailTestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(8))

    nd002Node.resolve(
      mockJSONResponse({
        node_id: 'nd_002',
        display_name: 'Seoul Edge',
        region: 'ap-northeast-2',
        city: 'Seoul',
        provider: 'Hetzner',
        lifecycle_status: '在用',
        monitoring_status: '启用',
        binding_status: '已绑定',
        labels: ['kr'],
        note: '',
        current_health_status: '正常',
        last_heartbeat_at: '2026-04-24T10:00:00Z',
        last_sync_at: '2026-04-24T10:05:00Z',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T10:05:00Z',
      }),
    )
    nd002Runtime.resolve(
      mockJSONResponse({
        node_id: 'nd_002',
        latest_host_sample: {
          node_id: 'nd_002',
          observed_at: '2026-04-24T10:05:00Z',
          received_at: '2026-04-24T10:05:02Z',
          agent_version: 'dev',
          fingerprint: 'fp-002',
          cpu_usage_pct: 9,
          load_1: 0.2,
          load_5: 0.2,
          load_15: 0.2,
          mem_used_pct: 54,
          mem_available_bytes: 3221225472,
          swap_used_pct: 0,
          disk_used_pct: 40,
          inode_used_pct: 8,
          net_in_bytes_per_sec: 1024,
          net_out_bytes_per_sec: 2048,
          cpu_iowait_pct: 0.2,
          cpu_steal_pct: 0.1,
          disk_read_bytes_per_sec: 1024,
          disk_write_bytes_per_sec: 2048,
          disk_busy_pct: 2,
          uptime_seconds: 3600,
          maintenance_context: false,
          is_backfilled: false,
          sync_batch_id: 'sync-002',
        },
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    // Watchtower main view has 8 metric cards (sample is non-null for nd_002)
    expect(
      screen.queryByRole('heading', { name: 'Tokyo Edge' }),
    ).not.toBeInTheDocument()

    nd001Node.resolve(
      mockJSONResponse({
        node_id: 'nd_001',
        display_name: 'Tokyo Edge',
        region: 'ap-northeast-1',
        city: 'Tokyo',
        provider: 'Vultr',
        lifecycle_status: '在用',
        monitoring_status: '启用',
        binding_status: '已绑定',
        labels: ['jp'],
        note: '',
        current_health_status: '告警',
        last_heartbeat_at: '2026-04-24T09:00:00Z',
        last_sync_at: '2026-04-24T09:05:00Z',
        current_active_incident_count: 1,
        current_primary_issue_summary: '旧节点异常',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:05:00Z',
      }),
    )
    nd001Runtime.resolve(
      mockJSONResponse({
        node_id: 'nd_001',
        latest_host_sample: null,
      }),
    )
    nd001Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_old',
          incident_class: 'node_disk_pressure',
          object_type: 'node',
          object_id: 'nd_001',
          severity: '严重',
          started_at: '2026-04-24T09:00:00Z',
          last_evaluated_at: '2026-04-24T09:05:00Z',
          source_summary: '旧节点异常摘要',
        },
      ]),
    )
    nd001Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_old',
          incident_id: 'inc_old',
          incident_class: 'node_disk_pressure',
          object_type: 'node',
          object_id: 'nd_001',
          event_type: 'incident_started',
          severity: '严重',
          summary: '旧节点事件',
          created_at: '2026-04-24T09:05:00Z',
        },
      ]),
    )

    nd002Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_new',
          incident_class: 'node_resource_pressure',
          object_type: 'node',
          object_id: 'nd_002',
          severity: '关注',
          started_at: '2026-04-24T10:00:00Z',
          last_evaluated_at: '2026-04-24T10:05:00Z',
          source_summary: '新节点异常摘要',
        },
      ]),
    )
    nd002Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_new',
          incident_id: 'inc_new',
          incident_class: 'node_resource_pressure',
          object_type: 'node',
          object_id: 'nd_002',
          event_type: 'incident_started',
          severity: '关注',
          summary: '新节点事件',
          created_at: '2026-04-24T10:05:00Z',
        },
      ]),
    )

    expect(screen.queryByText('旧节点异常摘要')).not.toBeInTheDocument()
    expect(screen.queryByText('旧节点事件')).not.toBeInTheDocument()
  })

  it('renders runtime controls and applies light maintenance actions from the detail page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_maintenance',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '维护中',
          binding_status: '已绑定',
          labels: ['core'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_maintenance',
          latest_host_sample: null,
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_maintenance',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['core'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:15:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_maintenance']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '退出维护' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '进入维护' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_maintenance/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('requires strong confirmation before pausing node monitoring from the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['core'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          latest_host_sample: null,
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '暂停',
          binding_status: '已绑定',
          labels: ['core'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-24T09:00:00Z',
          last_sync_at: '2026-04-24T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-24T09:20:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    const pauseButton = screen.getByRole('button', { name: '暂停监控' })
    fireEvent.click(pauseButton)

    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：监控运行状态为启用。')).toBeInTheDocument()
    expect(screen.getByText('操作后：监控运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText(
        '会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 agent 绑定关系。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('heading', { name: '确认暂停节点监控' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)
    await waitFor(() => expect(screen.getByRole('button', { name: '暂停监控' })).toHaveFocus())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复监控' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('shows maintenance-state current copy when pausing from a maintenance node detail view', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_maintenance_pause',
              binding_status: '已绑定',
              monitoring_status: '维护中',
              current_health_status: '正常',
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_maintenance_pause')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_maintenance_pause']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：监控运行状态为维护中。')).toBeInTheDocument()
  })

  it('keeps the pause confirmation visible and reusable after a pause API failure', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_pause_error',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_pause_error')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'pause failed' }, 500))
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_pause_error',
            binding_status: '已绑定',
            monitoring_status: '暂停',
            current_health_status: '正常',
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:40:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_pause_error']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    await waitFor(() => expect(screen.getByText('pause failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认暂停节点监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停监控' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_pause_error/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/nodes/nd_pause_error/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('renders a lifecycle card for retired nodes with restore-to-observing guidance', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_retired',
              lifecycle_status: '已退役',
              monitoring_status: '暂停',
              binding_status: '已绑定',
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_retired')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_retired']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    // The 生命周期 section is collapsed by default; expand to assert
    openSecondary('生命周期')

    expect(
      screen.getByText('已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退役节点' })).not.toBeInTheDocument()
  })

  it('retires a node from Node detail with inline confirmation instead of window.confirm', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_lifecycle',
            binding_status: '已绑定',
            lifecycle_status: '在用',
            current_health_status: '正常',
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_lifecycle')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_lifecycle',
            binding_status: '已绑定',
            lifecycle_status: '已退役',
            monitoring_status: '暂停',
            current_health_status: '正常',
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:30:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_lifecycle']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('生命周期')
    expect(screen.getByRole('button', { name: '退役节点' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '退役节点' }))
    expect(screen.getByRole('button', { name: '确认退役' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeInTheDocument()
    expect(screen.getByText(/这不是删除/)).toBeInTheDocument()
    expect(screen.getByText(/不会清空事件、观测记录或 agent 绑定历史/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '确认退役' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_lifecycle/lifecycle/retire', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('restores a retired node only to observing from Node detail', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_restore',
            binding_status: '已绑定',
            lifecycle_status: '已退役',
            monitoring_status: '暂停',
            current_health_status: '正常',
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_restore')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_restore',
            binding_status: '已绑定',
            lifecycle_status: '观察中',
            monitoring_status: '暂停',
            current_health_status: '正常',
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:40:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_restore']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('生命周期')
    fireEvent.click(screen.getByRole('button', { name: '恢复到观察中' }))

    // "观察中" StatusBadge surfaces in multiple places (lifecycle badge in header)
    await waitFor(() => expect(screen.getAllByText('观察中').length).toBeGreaterThanOrEqual(1))
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      '/api/nodes/nd_restore/lifecycle/restore-to-observing',
      {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
  })

  it('keeps lifecycle action errors local to the lifecycle card', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_lifecycle_error',
            binding_status: '已绑定',
            lifecycle_status: '已退役',
            monitoring_status: '暂停',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_lifecycle_error')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid lifecycle transition' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_lifecycle_error']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('生命周期')
    fireEvent.click(screen.getByRole('button', { name: '恢复到观察中' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('invalid lifecycle transition'),
    )
    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByText('生命周期')).toBeInTheDocument()
  })

  it('ignores a stale runtime-action success after switching to a different node route', async () => {
    const runtimeAction = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_001',
            display_name: 'Tokyo Edge',
            region: 'ap-northeast-1',
            city: 'Tokyo',
            provider: 'Vultr',
            lifecycle_status: '在用',
            monitoring_status: '维护中',
            binding_status: '已绑定',
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
          mockJSONResponse({
            node_id: 'nd_001',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => runtimeAction.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:10:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <NodeDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '退出维护' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    openRuntimeMenu()
    expect(screen.getByRole('button', { name: '进入维护' })).toBeEnabled()

    runtimeAction.resolve(
      mockJSONResponse({
        node_id: 'nd_001',
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
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:20:00Z',
      }),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '进入维护' })).toBeEnabled()
  })

  it('ignores a stale lifecycle-action success after switching to a different node route', async () => {
    const lifecycleAction = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_001',
              binding_status: '已绑定',
              lifecycle_status: '已退役',
              monitoring_status: '暂停',
              current_health_status: '正常',
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => lifecycleAction.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:10:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <NodeDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('生命周期')

    fireEvent.click(screen.getByRole('button', { name: '恢复到观察中' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()

    lifecycleAction.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          lifecycle_status: '观察中',
          monitoring_status: '暂停',
          current_health_status: '正常',
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:45:00Z',
        }),
      ),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.queryByText('观察中')).not.toBeInTheDocument()
  })

  it('ignores a stale binding-action success after switching to a different node route', async () => {
    const confirmRebind = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(nodeRecord({ node_id: 'nd_001' })))
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse(
            onboardingConflictState({
              node_id: 'nd_001',
            }),
          ),
        )
        .mockImplementationOnce(() => confirmRebind.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:10:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <NodeDetailTestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('确认重绑定'))
    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument()

    confirmRebind.resolve(
      mockJSONResponse(
        onboardingConflictState({
          node_id: 'nd_001',
          binding_status: '已绑定',
          phase: '已绑定，等待稳定观测',
          pending_binding: undefined,
          updated_at: '2026-04-27T09:20:00Z',
        }),
      ),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument()
  })



  it('renders empty note copy as 暂无备注 in node detail metadata', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_001',
              binding_status: '已绑定',
              labels: ['edge'],
              note: '',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    expect(screen.getByText('备注：暂无备注')).toBeInTheDocument()
  })

  it('shows and edits node labels and note metadata', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge'],
            note: 'keep me',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge', 'core'],
            note: 'trimmed note',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:10:00Z',
          }),
        ),
      )
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge', 'core'],
            note: 'second note',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:15:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    expect(screen.getByRole('heading', { name: '标签与备注' })).toBeInTheDocument()
    expect(screen.getByText('标签：edge')).toBeInTheDocument()
    expect(screen.getByText('备注：keep me')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core, edge' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: '  trimmed note  ' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_001', {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'If-Match': '"2026-04-27T09:05:00Z"',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ labels: ['edge', 'core'], note: 'trimmed note' }),
      }),
    )
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
    expect(screen.getByText('备注：trimmed note')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'second note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/nodes/nd_001', {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'If-Match': '"2026-04-27T09:10:00Z"',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ labels: ['edge', 'core'], note: 'second note' }),
      }),
    )
    expect(screen.getByText('备注：second note')).toBeInTheDocument()
  })

  it('shows a metadata update failure without replacing the current detail', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_001',
              binding_status: '已绑定',
              labels: ['edge'],
              note: 'keep me',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse({ error: 'metadata write failed' }, 409)),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(screen.getByText('metadata write failed')).toBeInTheDocument(),
    )
    expect(screen.getByRole('textbox', { name: '备注' })).toHaveValue('new note')
    expect(screen.queryByText('备注：new note')).not.toBeInTheDocument()
  })

  it('ignores a stale metadata save after switching to a different node route', async () => {
    const metadataSave = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_001',
              binding_status: '已绑定',
              labels: ['edge'],
              note: 'keep me',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => metadataSave.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            display_name: 'Seoul Edge',
            region: 'ap-northeast-2',
            city: 'Seoul',
            provider: 'Hetzner',
            lifecycle_status: '在用',
            monitoring_status: '启用',
            binding_status: '已绑定',
            labels: ['seoul'],
            note: 'seoul note',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:10:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <NodeDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )

    metadataSave.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge', 'core'],
          note: 'new note',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:45:00Z',
        }),
      ),
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.queryByText('备注：new note')).not.toBeInTheDocument()
    expect(screen.getByText('备注：seoul note')).toBeInTheDocument()
  })


  it('preserves saved metadata when a later runtime response returns stale labels and note', async () => {
    const metadataSave = deferredResponse()
    const runtimeAction = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge'],
            note: 'keep me',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => metadataSave.promise)
      .mockImplementationOnce(() => runtimeAction.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    metadataSave.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge', 'core'],
          note: 'new note',
          monitoring_status: '启用',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:12:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByText('标签：edge · core')).toBeInTheDocument())
    expect(screen.getByText('备注：new note')).toBeInTheDocument()

    runtimeAction.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge'],
          note: 'keep me',
          monitoring_status: '维护中',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:15:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '退出维护' })).toBeInTheDocument())
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
    expect(screen.getByText('备注：new note')).toBeInTheDocument()
  })

  it('preserves saved metadata when a later lifecycle response returns stale labels and note', async () => {
    const metadataSave = deferredResponse()
    const lifecycleAction = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge'],
            note: 'keep me',
            lifecycle_status: '在用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => metadataSave.promise)
      .mockImplementationOnce(() => lifecycleAction.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    openSecondary('生命周期')
    fireEvent.click(screen.getByRole('button', { name: '退役节点' }))
    fireEvent.click(screen.getByRole('button', { name: '确认退役' }))

    metadataSave.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge', 'core'],
          note: 'new note',
          lifecycle_status: '在用',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:12:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByText('标签：edge · core')).toBeInTheDocument())
    expect(screen.getByText('备注：new note')).toBeInTheDocument()

    lifecycleAction.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge'],
          note: 'keep me',
          lifecycle_status: '已退役',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:15:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument())
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
    expect(screen.getByText('备注：new note')).toBeInTheDocument()
  })

  it('preserves saved metadata when a later binding response returns stale labels and note', async () => {
    const metadataSave = deferredResponse()
    const bindingAction = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_conflict',
            binding_status: '指纹变更待确认',
            labels: ['edge'],
            note: 'keep me',
            current_health_status: '关注',
            current_active_incident_count: 1,
            current_primary_issue_summary: '检测到新的指纹接入请求',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState({ labels: ['edge'], note: 'keep me' })))
      .mockImplementationOnce(() => metadataSave.promise)
      .mockImplementationOnce(() => bindingAction.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_conflict']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    await waitForEnabledButton('确认重绑定')
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    fireEvent.click(screen.getByRole('button', { name: '确认重绑定' }))

    metadataSave.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_conflict',
          binding_status: '指纹变更待确认',
          labels: ['edge', 'core'],
          note: 'new note',
          current_health_status: '关注',
          current_active_incident_count: 1,
          current_primary_issue_summary: '检测到新的指纹接入请求',
          updated_at: '2026-04-27T09:12:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByText('标签：edge · core')).toBeInTheDocument())
    expect(screen.getByText('备注：new note')).toBeInTheDocument()

    bindingAction.resolve(
      mockJSONResponse(
        onboardingConflictState({
          binding_status: '已绑定',
          labels: ['edge'],
          note: 'keep me',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:15:00Z',
        }),
      ),
    )

    // "已绑定" StatusBadge surfaces in multiple places (header + 接入凭证状态 summary).
    await waitFor(() => expect(screen.getAllByText('已绑定').length).toBeGreaterThanOrEqual(1))
    expect(screen.getByText('标签：edge · core')).toBeInTheDocument()
    expect(screen.getByText('备注：new note')).toBeInTheDocument()
  })

  it('keeps newer runtime fields when a metadata save resolves with stale non-metadata data', async () => {
    const metadataSave = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge'],
            note: 'keep me',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_001')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => metadataSave.promise)
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_001',
            binding_status: '已绑定',
            labels: ['edge'],
            note: 'keep me',
            monitoring_status: '维护中',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T09:15:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_001']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openSecondary('标签与备注')

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), {
      target: { value: 'edge, core' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '备注' }), {
      target: { value: 'new note' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '正在保存…' })).toBeDisabled(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '退出维护' })).toBeInTheDocument(),
    )

    metadataSave.resolve(
      mockJSONResponse(
        nodeRecord({
          node_id: 'nd_001',
          binding_status: '已绑定',
          labels: ['edge', 'core'],
          note: 'new note',
          monitoring_status: '启用',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          updated_at: '2026-04-27T09:05:00Z',
        }),
      ),
    )

    await waitFor(() => expect(screen.getByText('标签：edge · core')).toBeInTheDocument())
    expect(screen.getByText('备注：new note')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '退出维护' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
  })

  // ────────────────────────────────────────────────────────────
  // Watchtower (PR2) — danger zone, metric grid, secondary collapse
  // ────────────────────────────────────────────────────────────

  it('omits the watchtower danger zone when no active incidents are present', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_calm',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_calm')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_calm']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Danger zone Card not rendered (current_active_incident_count = 0)
    expect(container.querySelector('.watchtower-danger')).toBeNull()
    // No "当前主问题" eyebrow either
    expect(screen.queryByText('当前主问题')).not.toBeInTheDocument()
  })

  it('renders the watchtower danger zone with primary issue summary when incidents are active', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_alert',
              binding_status: '已绑定',
              current_health_status: '严重',
              current_active_incident_count: 3,
              current_primary_issue_summary: '磁盘使用率持续超过阈值',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_alert')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_alert']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    const danger = container.querySelector('.watchtower-danger')
    expect(danger).not.toBeNull()
    expect(screen.getByText('当前主问题')).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: '磁盘使用率持续超过阈值' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /查看完整时间线/ })).toBeInTheDocument()
    // Active incident count surfaces inside the danger meta line
    expect(danger?.textContent ?? '').toContain('3')
  })

  it('renders 8 watchtower metric cards in the main 4×2 grid when a host sample is present', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_metrics',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_metrics',
            latest_host_sample: {
              node_id: 'nd_metrics',
              observed_at: '2026-04-24T10:00:00Z',
              received_at: '2026-04-24T10:00:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-m',
              cpu_usage_pct: 33,
              load_1: 0.4,
              load_5: 0.5,
              load_15: 0.6,
              mem_used_pct: 55,
              mem_available_bytes: 1073741824,
              swap_used_pct: 1,
              disk_used_pct: 60,
              inode_used_pct: 12,
              net_in_bytes_per_sec: 1024,
              net_out_bytes_per_sec: 2048,
              cpu_iowait_pct: 5,
              cpu_steal_pct: 0,
              disk_read_bytes_per_sec: 512,
              disk_write_bytes_per_sec: 768,
              disk_busy_pct: 4,
              uptime_seconds: 3600,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-m',
            },
            recent_host_samples: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_metrics']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    const cards = container.querySelectorAll('.watchtower-metrics .watchtower-metric-card')
    expect(cards.length).toBe(8)
    // Each metric card head renders the canonical 8 labels.
    const headings = Array.from(container.querySelectorAll('.watchtower-metric-card__head h3')).map(
      (n) => (n.textContent ?? '').trim(),
    )
    expect(headings).toHaveLength(8)
    expect(headings).toEqual(expect.arrayContaining([
      'CPU 使用率',
      'Load5',
      '内存使用率',
      '磁盘使用率',
      'Inode 使用率',
      '网络入',
      '网络出',
      'CPU IOWait',
    ]))
  })

  it('keeps the watchtower secondary sections collapsed by default', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_secondary',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_secondary')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/nodes/nd_secondary']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    const secondarySections = Array.from(container.querySelectorAll('.collapsible-section.watchtower-secondary'))
    expect(secondarySections.length).toBe(4)
    secondarySections.forEach((section) => {
      const trigger = section.querySelector('.collapsible-section__trigger')
      expect(trigger).not.toBeNull()
      expect(trigger).toHaveAttribute('aria-expanded', 'false')
    })
    // Page footer surfaces the snapshot meta line
    expect(container.querySelector('.watchtower-snapshot-meta')).not.toBeNull()
  })

  it('renders container list when latest_host_sample contains containers', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_ctr',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_ctr',
            latest_host_sample: {
              node_id: 'nd_ctr',
              observed_at: '2026-04-24T10:05:00Z',
              received_at: '2026-04-24T10:05:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-ctr',
              cpu_usage_pct: 10,
              load_1: 0.1,
              load_5: 0.2,
              load_15: 0.3,
              mem_used_pct: 40,
              mem_available_bytes: 8589934592,
              swap_used_pct: 0,
              disk_used_pct: 30,
              inode_used_pct: 5,
              net_in_bytes_per_sec: 0,
              net_out_bytes_per_sec: 0,
              cpu_iowait_pct: 0,
              cpu_steal_pct: 0,
              disk_read_bytes_per_sec: 0,
              disk_write_bytes_per_sec: 0,
              disk_busy_pct: 0,
              uptime_seconds: 3600,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-ctr',
              containers: [
                {
                  id: 'abc123',
                  name: 'nginx-prod',
                  image: 'nginx:1.25-alpine',
                  status: 'running',
                  cpu_pct: 0.5,
                  mem_pct: 1.2,
                },
                {
                  id: 'def456',
                  name: 'redis-cache',
                  image: 'redis:7-alpine',
                  status: 'exited',
                },
              ],
            },
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_ctr']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Open the 容器列表 section.
    openSecondary('容器列表')

    // Verify container names appear.
    expect(screen.getByText('nginx-prod')).toBeInTheDocument()
    expect(screen.getByText('redis-cache')).toBeInTheDocument()

    // Verify image names.
    expect(screen.getByText('nginx:1.25-alpine')).toBeInTheDocument()
    expect(screen.getByText('redis:7-alpine')).toBeInTheDocument()

    // Verify CPU/Mem percentages (mono digits).
    expect(screen.getByText('0.5%')).toBeInTheDocument()
    expect(screen.getByText('1.2%')).toBeInTheDocument()

    // Exited container should have dash for missing stats.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('shows 暂无容器数据 when latest_host_sample has no containers', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_noctr',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_noctr',
            latest_host_sample: {
              node_id: 'nd_noctr',
              observed_at: '2026-04-24T10:05:00Z',
              received_at: '2026-04-24T10:05:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-noctr',
              cpu_usage_pct: 5,
              load_1: 0.1,
              load_5: 0.1,
              load_15: 0.1,
              mem_used_pct: 30,
              mem_available_bytes: 10737418240,
              swap_used_pct: 0,
              disk_used_pct: 20,
              inode_used_pct: 3,
              net_in_bytes_per_sec: 0,
              net_out_bytes_per_sec: 0,
              cpu_iowait_pct: 0,
              cpu_steal_pct: 0,
              disk_read_bytes_per_sec: 0,
              disk_write_bytes_per_sec: 0,
              disk_busy_pct: 0,
              uptime_seconds: 1800,
              maintenance_context: false,
              is_backfilled: false,
              sync_batch_id: 'sync-noctr',
            },
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_noctr']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openSecondary('容器列表')
    expect(screen.getByText('暂无容器数据')).toBeInTheDocument()
  })

  it('triggers runtime maintenance via the watchtower header operations menu', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_ops',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_ops')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_ops',
            binding_status: '已绑定',
            monitoring_status: '维护中',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            updated_at: '2026-04-27T10:00:00Z',
          }),
        ),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_ops']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Opening the operations <summary> reveals the maintenance/pause buttons
    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_ops/runtime/enter-maintenance', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
  })

  it('opens history drawer and lazy-loads historical incidents on tab switch', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          node_id: 'nd_history',
          display_name: 'History Node',
          region: 'ap-east-1',
          city: 'Hong Kong',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: [],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-27T09:00:00Z',
          last_sync_at: '2026-04-27T09:05:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-27T09:05:00Z',
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_history')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            event_id: 'evt_history_001',
            incident_id: 'inc_h_001',
            incident_class: 'node_disk_pressure',
            object_type: 'node',
            object_id: 'nd_history',
            event_type: 'incident_started',
            severity: '告警',
            summary: '事件抽屉里的事件文案',
            created_at: '2026-04-26T10:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            incident_id: 'inc_h_resolved',
            incident_class: 'node_disk_pressure',
            object_type: 'node',
            object_id: 'nd_history',
            severity: '告警',
            started_at: '2026-04-25T10:00:00Z',
            last_evaluated_at: '2026-04-25T11:00:00Z',
            source_summary: '历史已恢复异常摘要',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_history']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'History Node' })).toBeInTheDocument(),
    )

    // Drawer is closed → no dialog and no historical request yet.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some((call) => String(call[0]).includes('include_resolved=true')),
    ).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: '查看历史' }))

    // Drawer opens; "事件时间线" tab is the default selection.
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // The event text surfaces inside the drawer EventList.
    expect(dialog).toHaveTextContent('事件抽屉里的事件文案')

    // Switching to 历史异常 triggers the incidents?include_resolved=true fetch.
    fireEvent.click(screen.getByRole('tab', { name: '历史异常' }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.find((call) =>
          String(call[0]).includes('/api/incidents?object_type=node&object_id=nd_history&include_resolved=true'),
        ),
      ).toBeDefined(),
    )

    await waitFor(() =>
      expect(screen.getByText('历史已恢复异常摘要')).toBeInTheDocument(),
    )

    // Closing the drawer removes the dialog from the DOM.
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // ── Time window Tabs ──

  it('renders time window Tabs with 24h selected by default', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_calm',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            node_id: 'nd_calm',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_calm']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    const tablist = screen.getByRole('tablist')
    expect(tablist).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '24h' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: '7d' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('tab', { name: '30d' })).toHaveAttribute('aria-selected', 'false')
  })

  it('switches to 7d window and fetches runtime facts with window=7d', async () => {
    const runtimeResponse = mockJSONResponse({
      node_id: 'nd_calm',
      latest_host_sample: null,
    })
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          nodeRecord({
            node_id: 'nd_calm',
            binding_status: '已绑定',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(runtimeResponse)
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(runtimeResponse)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/nodes/nd_calm']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Initial load fetches runtime facts with default 24h window.
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_calm/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })

    // Switch to 7d.
    fireEvent.click(screen.getByRole('tab', { name: '7d' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_calm/runtime-facts?window=7d', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
  })

  // ── Command execution ──

  it('shows 执行命令… button in the watchtower header operations popover', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_cmd',
              binding_status: '已绑定',
              monitoring_status: '启用',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_cmd')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_cmd']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()

    // The 执行命令… button must render inside the operations popover.
    expect(screen.getByRole('button', { name: '执行命令…' })).toBeInTheDocument()
  })

  it('opens command drawer with 8 preset command options', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            nodeRecord({
              node_id: 'nd_cmd2',
              binding_status: '已绑定',
              monitoring_status: '启用',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('nd_cmd2')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/nodes/nd_cmd2']}>
        <Routes>
          <Route path="/nodes/:nodeId" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Drawer is closed by default.
    expect(screen.queryByRole('dialog', { name: '执行命令抽屉' })).not.toBeInTheDocument()

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))

    // Drawer opens.
    const drawer = await screen.findByRole('dialog', { name: '执行命令抽屉' })
    expect(drawer).toBeInTheDocument()

    // 8 preset command buttons render.
    expect(screen.getByRole('button', { name: /df -h/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /free -m/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /uptime/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /top -bn1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /journalctl --lines=50/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /systemctl status/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /dmesg --level=err/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /docker ps/ })).toBeInTheDocument()
  })

})
