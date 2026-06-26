import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { MonitoringDetailPage } from './MonitoringDetailPage'
import { formatDateTime } from '../lib/format'

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

function monitoringInstanceRecord(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    monitoring_instance_id: 'mi_conflict',
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

function emptyRuntimeFacts(monitoringInstanceId = 'mi_conflict') {
  return {
    monitoring_instance_id: monitoringInstanceId,
    latest_host_sample: null,
  }
}

function emptyManagementCounts(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    heartbeat_count: 0,
    host_sample_count: 0,
    probe_observation_count: 0,
    host_sample_daily_aggregate_count: 0,
    ip_quality_report_count: 0,
    active_incident_count: 0,
    state_change_event_count: 0,
    notification_record_count: 0,
    asset_lifecycle_action_step_count: 0,
    active_vps_link_count: 0,
    ...overrides,
  }
}

function managementReview(record: Record<string, unknown>, overrides: Partial<Record<string, unknown>> = {}) {
  return {
    record,
    active_vps_links: [],
    counts: emptyManagementCounts(),
    warnings: [],
    blockers: [],
    actions: {
      can_retire: true,
      can_restore_lifecycle: false,
      can_archive: false,
      can_restore_archive: false,
      can_permanent_cleanup: false,
    },
    empty_mistake_candidate: false,
    ...overrides,
  }
}

function hostSampleRecord(monitoringInstanceId: string, overrides: Partial<Record<string, unknown>> = {}) {
  return {
    monitoring_instance_id: monitoringInstanceId,
    observed_at: '2026-04-24T10:00:00Z',
    received_at: '2026-04-24T10:00:01Z',
    agent_version: 'dev',
    fingerprint: `fp-${monitoringInstanceId}`,
    cpu_usage_pct: 33,
    load_1: 0.4,
    load_5: 0.5,
    load_15: 0.6,
    mem_used_pct: 55,
    mem_available_bytes: 1073741824,
    mem_total_bytes: 8589934592,
    swap_used_pct: 1,
    disk_used_pct: 60,
    disk_total_bytes: 107374182400,
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
    sync_batch_id: `sync-${monitoringInstanceId}`,
    ...overrides,
  }
}

class MockRuntimeWebSocket {
  static instances: MockRuntimeWebSocket[] = []

  readonly url: string
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  close = vi.fn(() => {
    this.onclose?.()
  })

  constructor(url: string) {
    this.url = url
    MockRuntimeWebSocket.instances.push(this)
  }

  emitOpen() {
    this.onopen?.()
  }

  emitMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>)
  }
}

function onboardingConflictState(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    ...monitoringInstanceRecord(),
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

function MonitoringDetailTestHarness() {
  const navigate = useNavigate()

  return (
    <>
      <button type="button" onClick={() => navigate('/monitoring/mi_002')}>
        switch monitoring instance
      </button>
      <Routes>
        <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
      </Routes>
    </>
  )
}

describe('MonitoringDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads linked VPS summary into the watchtower header', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_001',
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
          monitoring_instance_id: 'mi_001',
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
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge VPS')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Tokyo Edge VPS' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.queryByText('primary host')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '关联 VPS' })).not.toBeInTheDocument()
    expect(screen.queryByText('VPS 是资产账本里的购买、续费与归属对象；监控实例是 agent 接入后的运行实例。')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_001/vps', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('settles linked VPS loading after a delayed response', async () => {
    const linkedVPSResponse = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord({
        monitoring_instance_id: 'mi_slow',
        display_name: 'Slow Linked VPS Monitoring Instance',
        binding_status: '已绑定',
        current_health_status: '正常',
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
      })))
      .mockResolvedValueOnce(mockJSONResponse({
        monitoring_instance_id: 'mi_slow',
        latest_host_sample: null,
        recent_host_samples: [],
      }))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockReturnValueOnce(linkedVPSResponse.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_slow']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('VPS 关联加载中')).toBeInTheDocument())

    linkedVPSResponse.resolve(mockJSONResponse([
      {
        vps_id: 'vps_slow',
        display_name: 'Slow Response VPS',
        provider_id: 'pv_001',
        provider_name: 'Hetzner',
        country: 'DE',
        region: 'Bavaria',
        city: 'Nuremberg',
        lifecycle_status: 'active',
        usage_status: 'in_use',
        renewal_decision: 'keep',
        importance: 'normal',
        labels: ['asset-ledger'],
        archived_at: null,
        linked_at: '2026-04-24T09:06:00Z',
        note: 'delayed response',
      },
    ]))

    await waitFor(() => expect(screen.getByText('Slow Response VPS')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Slow Response VPS' })).toHaveAttribute('href', '/vps/vps_slow')
    expect(screen.queryByText('delayed response')).not.toBeInTheDocument()
    expect(screen.queryByText('VPS 关联加载中')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('edits monitoring instance group labels and note from the detail metadata section', async () => {
    const initialRecord = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_metadata',
      display_name: 'Tokyo Metadata Edge',
      binding_status: '已绑定',
      group: 'prod',
      labels: ['edge'],
      note: 'keep visible',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      updated_at: '2026-04-27T09:05:00Z',
    })
    const updatedRecord = {
      ...initialRecord,
      group: 'core',
      labels: ['edge', 'db'],
      note: 'new note',
      updated_at: '2026-04-27T09:08:00Z',
    }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_metadata' && init?.method === 'PATCH') {
        return Promise.resolve(mockJSONResponse(updatedRecord))
      }
      if (path === '/api/monitoring-instances/mi_metadata') {
        return Promise.resolve(mockJSONResponse(initialRecord))
      }
      if (path === '/api/monitoring-instances/mi_metadata/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_metadata')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_metadata') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_metadata') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_metadata/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_metadata']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Metadata Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByText('标签与备注', { selector: 'summary' }))
    expect(screen.getByText('Group：prod')).toBeInTheDocument()
    expect(screen.getByText('标签：edge')).toBeInTheDocument()
    expect(screen.getByText('备注：keep visible')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    fireEvent.change(screen.getByLabelText('Group'), { target: { value: 'draft' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'draft' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'discarded' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    fireEvent.click(screen.getByRole('button', { name: '编辑标签与备注' }))
    expect(screen.getByLabelText('Group')).toHaveValue('prod')
    expect(screen.getByLabelText('标签')).toHaveValue('edge')
    expect(screen.getByLabelText('备注')).toHaveValue('keep visible')
    fireEvent.change(screen.getByLabelText('Group'), { target: { value: 'core' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'edge, db, edge' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'new note' } })
    fireEvent.click(screen.getByRole('button', { name: '保存标签与备注' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_metadata', {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'If-Match': '"2026-04-27T09:05:00Z"',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ group: 'core', labels: ['edge', 'db'], note: 'new note' }),
      }),
    )
    expect(screen.queryByRole('button', { name: '保存标签与备注' })).not.toBeInTheDocument()
    expect(screen.getByText('Group：core')).toBeInTheDocument()
    expect(screen.getByText('标签：edge · db')).toBeInTheDocument()
    expect(screen.getByText('备注：new note')).toBeInTheDocument()
  })

  it('keeps detail metadata when runtime updates return stale metadata fields', async () => {
    const currentRecord = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_metadata_runtime',
      display_name: 'Tokyo Metadata Runtime',
      binding_status: '已绑定',
      group: 'core',
      labels: ['edge'],
      note: 'fresh note',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      updated_at: '2026-04-27T09:05:00Z',
    })
    const runtimeUpdatedRecord = {
      ...currentRecord,
      group: 'stale',
      labels: ['stale'],
      note: 'stale note',
      monitoring_status: '维护中',
      updated_at: '2026-04-27T09:09:00Z',
    }
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_metadata_runtime/runtime/enter-maintenance') {
        return Promise.resolve(mockJSONResponse(runtimeUpdatedRecord))
      }
      if (path === '/api/monitoring-instances/mi_metadata_runtime') {
        return Promise.resolve(mockJSONResponse(currentRecord))
      }
      if (path === '/api/monitoring-instances/mi_metadata_runtime/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_metadata_runtime')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_metadata_runtime') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_metadata_runtime') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_metadata_runtime/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_metadata_runtime']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Metadata Runtime' })).toBeInTheDocument())

    fireEvent.click(screen.getByText('标签与备注', { selector: 'summary' }))
    expect(screen.getByText('Group：core')).toBeInTheDocument()

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '进入维护' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_metadata_runtime/runtime/enter-maintenance', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    expect(screen.getByText('Group：core')).toBeInTheDocument()
    expect(screen.getByText('标签：edge')).toBeInTheDocument()
    expect(screen.getByText('备注：fresh note')).toBeInTheDocument()
    expect(screen.queryByText('Group：stale')).not.toBeInTheDocument()
  })

  it('loads monitoring instance management review from the unified detail entry', async () => {
    const record = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_manage',
      display_name: 'Tokyo Managed Edge',
      binding_status: '已绑定',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
    })
    const review = managementReview(record, {
      active_vps_links: [
        {
          link_id: 'lnk_001',
          vps_id: 'vps_001',
          display_name: 'Tokyo VPS',
          lifecycle_status: 'active',
          usage_status: 'in_use',
          linked_at: '2026-04-27T09:00:00Z',
          note: 'primary host',
        },
      ],
      counts: emptyManagementCounts({
        host_sample_count: 3,
        ip_quality_report_count: 1,
        active_vps_link_count: 1,
      }),
      warnings: ['存在历史观测数据'],
      blockers: ['归档前需要先退役实例'],
      actions: {
        can_retire: true,
        can_restore_lifecycle: false,
        can_archive: false,
        can_restore_archive: false,
        can_permanent_cleanup: false,
      },
    })
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_manage/management-review') {
        return Promise.resolve(mockJSONResponse(review))
      }
      if (path === '/api/monitoring-instances/mi_manage') {
        return Promise.resolve(mockJSONResponse(record))
      }
      if (path === '/api/monitoring-instances/mi_manage/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_manage')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_manage') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_manage') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_manage/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_manage']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Managed Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByText('管理实例', { selector: 'summary' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_manage/management-review', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    expect(screen.getByText('主机样本')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Tokyo VPS' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByText('存在历史观测数据')).toBeInTheDocument()
    expect(screen.getByText('归档前需要先退役实例')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '退役实例' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '归档实例' })).toBeDisabled()
  })

  it('retires a monitoring instance from the management panel and refreshes review', async () => {
    const initialRecord = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_retire',
      display_name: 'Tokyo Retire Edge',
      binding_status: '已绑定',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
    })
    const retiredRecord = {
      ...initialRecord,
      lifecycle_status: '已退役',
      monitoring_status: '暂停',
      updated_at: '2026-04-27T09:30:00Z',
    }
    let currentReviewRecord = initialRecord
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_retire/lifecycle/retire' && init?.method === 'POST') {
        currentReviewRecord = retiredRecord
        return Promise.resolve(mockJSONResponse(retiredRecord))
      }
      if (path === '/api/monitoring-instances/mi_retire/management-review') {
        return Promise.resolve(mockJSONResponse(managementReview(currentReviewRecord, {
          actions: {
            can_retire: currentReviewRecord.lifecycle_status !== '已退役',
            can_restore_lifecycle: currentReviewRecord.lifecycle_status === '已退役',
            can_archive: currentReviewRecord.lifecycle_status === '已退役',
            can_restore_archive: false,
            can_permanent_cleanup: false,
          },
        })))
      }
      if (path === '/api/monitoring-instances/mi_retire') {
        return Promise.resolve(mockJSONResponse(initialRecord))
      }
      if (path === '/api/monitoring-instances/mi_retire/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_retire')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_retire') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_retire') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_retire/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_retire']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Retire Edge' })).toBeInTheDocument())
    fireEvent.click(screen.getByText('管理实例', { selector: 'summary' }))
    fireEvent.click(await screen.findByRole('button', { name: '退役实例' }))

    const dialog = await screen.findByRole('alertdialog', { name: '退役监控实例' })
    fireEvent.change(within(dialog).getByLabelText('原因'), { target: { value: '已不再需要观测' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认退役' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_retire/lifecycle/retire', {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ reason: '已不再需要观测' }),
      }),
    )
    expect(screen.getAllByText('已退役').length).toBeGreaterThan(0)
    expect(screen.getAllByText('暂停').length).toBeGreaterThan(0)
  })

  it('archives a retired monitoring instance with display-name confirmation', async () => {
    const retiredRecord = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_archive',
      display_name: 'Tokyo Archive Edge',
      lifecycle_status: '已退役',
      monitoring_status: '暂停',
      binding_status: '已绑定',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
    })
    const archivedRecord = {
      ...retiredRecord,
      archived_at: '2026-04-27T09:35:00Z',
      archived_reason: '重复创建',
      updated_at: '2026-04-27T09:35:00Z',
    }
    let currentReviewRecord: Record<string, unknown> = retiredRecord
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_archive/archive' && init?.method === 'POST') {
        currentReviewRecord = archivedRecord
        return Promise.resolve(mockJSONResponse(archivedRecord))
      }
      if (path === '/api/monitoring-instances/mi_archive/management-review') {
        return Promise.resolve(mockJSONResponse(managementReview(currentReviewRecord, {
          actions: {
            can_retire: false,
            can_restore_lifecycle: !currentReviewRecord.archived_at,
            can_archive: !currentReviewRecord.archived_at,
            can_restore_archive: Boolean(currentReviewRecord.archived_at),
            can_permanent_cleanup: true,
          },
          empty_mistake_candidate: true,
        })))
      }
      if (path === '/api/monitoring-instances/mi_archive') {
        return Promise.resolve(mockJSONResponse(retiredRecord))
      }
      if (path === '/api/monitoring-instances/mi_archive/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_archive')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_archive') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_archive') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_archive/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_archive']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Archive Edge' })).toBeInTheDocument())
    fireEvent.click(screen.getByText('管理实例', { selector: 'summary' }))
    fireEvent.click(await screen.findByRole('button', { name: '归档实例' }))

    const dialog = await screen.findByRole('alertdialog', { name: '归档监控实例' })
    expect(within(dialog).getByRole('button', { name: '确认归档' })).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('原因'), { target: { value: '重复创建' } })
    fireEvent.change(within(dialog).getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Archive Edge' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认归档' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_archive/archive', {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ reason: '重复创建', confirmation_name: 'Tokyo Archive Edge' }),
      }),
    )
    expect(screen.getByText('已归档')).toBeInTheDocument()
  })

  it('keeps archived details read-only and permanently cleans up from management', async () => {
    const archivedRecord = monitoringInstanceRecord({
      monitoring_instance_id: 'mi_cleanup',
      display_name: 'Tokyo Cleanup Edge',
      lifecycle_status: '已退役',
      monitoring_status: '暂停',
      binding_status: '已绑定',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      archived_at: '2026-04-27T09:35:00Z',
      archived_reason: '重复创建',
    })
    const cleanupResult = {
      monitoring_instance_id: 'mi_cleanup',
      counts: emptyManagementCounts(),
      deleted_reference_count: 0,
      deleted: true,
    }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/monitoring-instances/mi_cleanup/permanent-cleanup' && init?.method === 'POST') {
        return Promise.resolve(mockJSONResponse(cleanupResult))
      }
      if (path === '/api/monitoring-instances/mi_cleanup/management-review') {
        return Promise.resolve(mockJSONResponse(managementReview(archivedRecord, {
          actions: {
            can_retire: false,
            can_restore_lifecycle: false,
            can_archive: false,
            can_restore_archive: true,
            can_permanent_cleanup: true,
          },
          empty_mistake_candidate: true,
        })))
      }
      if (path === '/api/monitoring-instances/mi_cleanup') {
        return Promise.resolve(mockJSONResponse(archivedRecord))
      }
      if (path === '/api/monitoring-instances/mi_cleanup/runtime-facts?window=realtime') {
        return Promise.resolve(mockJSONResponse(emptyRuntimeFacts('mi_cleanup')))
      }
      if (path === '/api/incidents?object_type=monitoring_instance&object_id=mi_cleanup') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/events?object_type=monitoring_instance&object_id=mi_cleanup') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (path === '/api/monitoring-instances/mi_cleanup/vps') {
        return Promise.resolve(mockJSONResponse([]))
      }
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${path}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cleanup']}>
        <Routes>
          <Route path="/monitoring" element={<div>monitoring list</div>} />
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Cleanup Edge' })).toBeInTheDocument())

    openRuntimeMenu()
    expect(screen.queryByRole('button', { name: '升级/重新接入 agent…' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '执行命令…' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '恢复监控' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByText('标签与备注', { selector: 'summary' }))
    expect(screen.queryByRole('button', { name: '编辑标签与备注' })).not.toBeInTheDocument()
    expect(screen.getByText('已归档实例资料只读')).toBeInTheDocument()

    fireEvent.click(screen.getByText('管理实例', { selector: 'summary' }))
    fireEvent.click(await screen.findByRole('button', { name: '永久清理' }))
    const dialog = await screen.findByRole('alertdialog', { name: '永久清理监控实例' })
    fireEvent.change(within(dialog).getByLabelText('原因'), { target: { value: '误创建空实例' } })
    fireEvent.change(within(dialog).getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Cleanup Edge' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认永久清理' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_cleanup/permanent-cleanup', {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ reason: '误创建空实例', confirmation_name: 'Tokyo Cleanup Edge' }),
      }),
    )
    await waitFor(() => expect(screen.getByText('monitoring list')).toBeInTheDocument())
  })

  it('renders monitoringInstance header and latest host sample cards', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_001',
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
          monitoring_instance_id: 'mi_001',
          latest_host_sample: {
            monitoring_instance_id: 'mi_001',
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
            mem_total_bytes: 8589934592,
            swap_used_pct: 0,
            disk_used_pct: 52,
            disk_total_bytes: 107374182400,
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
            incident_class: 'monitoring_instance_disk_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
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
            incident_class: 'monitoring_instance_disk_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            event_type: 'incident_escalated',
            severity: '严重',
            summary: '磁盘压力已升级为严重',
            created_at: '2026-04-24T09:04:00Z',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('正在加载监控实例详情…')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Watchtower header surfaces uptime in mono "数据新鲜度" and monitoring_instance_id in mono row 2
    expect(screen.getByText('2小时 0分钟')).toBeInTheDocument()
    // Each metric card head renders the current value via MonoDigits
    expect(screen.getAllByText('12.5%').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText(/2.0 GB/i)).toBeInTheDocument()
    expect(screen.getByText('总内存')).toBeInTheDocument()
    expect(screen.getByText(/8.0 GB/i)).toBeInTheDocument()
    expect(screen.getByText('总磁盘')).toBeInTheDocument()
    expect(screen.getByText(/100.0 GB/i)).toBeInTheDocument()
    expect(screen.queryByText('将在 incidents / events 切片接入后替换为真实内容。')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/runtime-facts?window=realtime', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/incidents?object_type=monitoring_instance&object_id=mi_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/events?object_type=monitoring_instance&object_id=mi_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
  })


  it('renders unknown memory and disk capacity as dash for older samples', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_unknown_capacity',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_unknown_capacity',
            latest_host_sample: {
              monitoring_instance_id: 'mi_unknown_capacity',
              observed_at: '2026-04-24T10:05:00Z',
              received_at: '2026-04-24T10:05:01Z',
              agent_version: 'dev',
              fingerprint: 'fp-unknown-capacity',
              cpu_usage_pct: 10,
              load_1: 0.1,
              load_5: 0.2,
              load_15: 0.3,
              mem_used_pct: 40,
              mem_available_bytes: 2147483648,
              mem_total_bytes: 0,
              swap_used_pct: 0,
              disk_used_pct: 30,
              disk_total_bytes: 0,
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
              sync_batch_id: 'sync-unknown-capacity',
            },
            recent_host_samples: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_unknown_capacity']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('总内存')).toBeInTheDocument()
    expect(screen.getByText('总磁盘')).toBeInTheDocument()
    expect(screen.queryByText('0 B')).not.toBeInTheDocument()
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('renders sparklines and a sample-count meta line when host metric points are present', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_trend',
            display_name: 'Trend Monitoring Instance',
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
            monitoring_instance_id: 'mi_trend',
            latest_host_sample: {
              monitoring_instance_id: 'mi_trend',
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
              mem_total_bytes: 8589934592,
              swap_used_pct: 0,
              disk_used_pct: 41,
              disk_total_bytes: 107374182400,
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
            window: {
              key: 'realtime',
              started_at: '2026-04-24T09:05:00Z',
              ended_at: '2026-04-24T10:05:00Z',
              bucket_count: 720,
              available_started_at: '2026-04-24T09:35:00Z',
              available_ended_at: '2026-04-24T10:05:00Z',
              sample_count: 2,
            },
            recent_host_samples: [
              {
                monitoring_instance_id: 'mi_trend',
                observed_at: '2026-04-24T09:35:00Z',
                received_at: '2026-04-24T09:35:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-trend',
                cpu_usage_pct: 18,
                load_1: 0.7,
                load_5: 1.2,
                load_15: 1.5,
                mem_used_pct: 58,
                mem_available_bytes: 1610612736,
                mem_total_bytes: 8589934592,
                swap_used_pct: 0,
                disk_used_pct: 40,
                disk_total_bytes: 107374182400,
                inode_used_pct: 8,
                net_in_bytes_per_sec: 900,
                net_out_bytes_per_sec: 1800,
                cpu_iowait_pct: 4,
                cpu_steal_pct: 1,
                disk_read_bytes_per_sec: 2048,
                disk_write_bytes_per_sec: 3072,
                disk_busy_pct: 7,
                uptime_seconds: 6900,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-trend-prev',
              },
              {
                monitoring_instance_id: 'mi_trend',
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
                mem_total_bytes: 8589934592,
                swap_used_pct: 0,
                disk_used_pct: 41,
                disk_total_bytes: 107374182400,
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
            ],
            host_metric_points: [
              {
                observed_at: '2026-04-24T09:35:00Z',
                sample_count: 1,
                cpu_usage_pct: 18,
                mem_used_pct: 58,
                disk_used_pct: 40,
                inode_used_pct: 8,
                load_5: 1.2,
                cpu_iowait_pct: 4,
                net_in_bytes_per_sec: 900,
                net_out_bytes_per_sec: 1800,
              },
              {
                observed_at: '2026-04-24T10:05:00Z',
                sample_count: 1,
                cpu_usage_pct: 21,
                mem_used_pct: 62,
                disk_used_pct: 41,
                inode_used_pct: 9,
                load_5: 1.6,
                cpu_iowait_pct: 6,
                net_in_bytes_per_sec: 1024,
                net_out_bytes_per_sec: 2048,
              },
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_trend']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Trend Monitoring Instance' })).toBeInTheDocument(),
    )

    // Watchtower main view: 8 metric cards in 4×2 grid; each renders a MetricChart svg
    const cards = container.querySelectorAll('.watchtower-metrics .watchtower-metric-card')
    expect(cards.length).toBe(8)
    // With 2 ascending metric points, each card's MetricChart draws a polyline
    expect(container.querySelectorAll('.watchtower-metrics polyline').length).toBe(8)
  })

  it('renders realtime trends from recent host samples before websocket messages arrive', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(monitoringInstanceRecord({
            monitoring_instance_id: 'mi_seed',
            display_name: 'Seeded Realtime Monitoring Instance',
            binding_status: '已绑定',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          })),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_seed',
            latest_host_sample: hostSampleRecord('mi_seed', {
              observed_at: '2026-04-24T10:00:05Z',
              cpu_usage_pct: 46,
              sync_batch_id: 'sync-seed-2',
            }),
            window: {
              key: 'realtime',
              started_at: '2026-04-24T09:00:05Z',
              ended_at: '2026-04-24T10:00:05Z',
              bucket_count: 720,
              available_started_at: '2026-04-24T10:00:00Z',
              available_ended_at: '2026-04-24T10:00:05Z',
              sample_count: 2,
            },
            recent_host_samples: [
              hostSampleRecord('mi_seed', {
                observed_at: '2026-04-24T10:00:00Z',
                cpu_usage_pct: 44,
                sync_batch_id: 'sync-seed-1',
              }),
              hostSampleRecord('mi_seed', {
                observed_at: '2026-04-24T10:00:05Z',
                cpu_usage_pct: 46,
                sync_batch_id: 'sync-seed-2',
              }),
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_seed']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seeded Realtime Monitoring Instance' })).toBeInTheDocument(),
    )

    expect(screen.getByText('实时滚动 2 点 · 已按阈值优先级排序')).toBeInTheDocument()
    expect(container.querySelectorAll('.watchtower-metrics polyline').length).toBe(8)
  })

  it('shows inline metric values on each chart at the shared hover time', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(monitoringInstanceRecord({
            monitoring_instance_id: 'mi_hover',
            display_name: 'Hover Monitoring Instance',
            binding_status: '已绑定',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          })),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_hover',
            latest_host_sample: hostSampleRecord('mi_hover', {
              observed_at: '2026-04-24T10:00:05Z',
              cpu_usage_pct: 42,
              mem_used_pct: 63,
              disk_used_pct: 61,
              inode_used_pct: 13,
              load_5: 0.9,
              cpu_iowait_pct: 7,
              net_in_bytes_per_sec: 4096,
              net_out_bytes_per_sec: 8192,
              sync_batch_id: 'sync-hover-2',
            }),
            window: {
              key: 'realtime',
              started_at: '2026-04-24T09:00:05Z',
              ended_at: '2026-04-24T10:00:05Z',
              bucket_count: 720,
              available_started_at: '2026-04-24T10:00:00Z',
              available_ended_at: '2026-04-24T10:00:05Z',
              sample_count: 2,
            },
            recent_host_samples: [
              hostSampleRecord('mi_hover', {
                observed_at: '2026-04-24T10:00:00Z',
                cpu_usage_pct: 40,
                mem_used_pct: 62,
                disk_used_pct: 60,
                inode_used_pct: 12,
                load_5: 0.8,
                cpu_iowait_pct: 6,
                net_in_bytes_per_sec: 2048,
                net_out_bytes_per_sec: 4096,
                sync_batch_id: 'sync-hover-1',
              }),
              hostSampleRecord('mi_hover', {
                observed_at: '2026-04-24T10:00:05Z',
                cpu_usage_pct: 42,
                mem_used_pct: 63,
                disk_used_pct: 61,
                inode_used_pct: 13,
                load_5: 0.9,
                cpu_iowait_pct: 7,
                net_in_bytes_per_sec: 4096,
                net_out_bytes_per_sec: 8192,
                sync_batch_id: 'sync-hover-2',
              }),
            ],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_hover']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Hover Monitoring Instance' })).toBeInTheDocument(),
    )

    const svg = container.querySelector('.watchtower-metrics svg')!
    svg.getBoundingClientRect = () => ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 360,
      bottom: 160,
      width: 360,
      height: 160,
      toJSON: () => ({}),
    })
    fireEvent.mouseMove(svg, { clientX: 360 })

    await waitFor(() =>
      expect(container.querySelectorAll('.watchtower-metrics .metric-chart__tooltip').length).toBe(8),
    )
    expect(container.querySelector('.watchtower-metrics-hover')).toBeNull()
    expect(container.querySelectorAll('.watchtower-metrics .metric-chart__cursor').length).toBe(8)

    const tooltipFor = (heading: string) => {
      const card = screen.getByRole('heading', { name: heading }).closest('.watchtower-metric-card')
      expect(card).toBeTruthy()
      const tooltip = card!.querySelector('.metric-chart__tooltip')
      expect(tooltip).toBeTruthy()
      expect((tooltip as HTMLElement).style.top).not.toBe('')
      return tooltip!
    }

    expect(tooltipFor('CPU 使用率')).toHaveTextContent('42.0%')
    expect(tooltipFor('内存使用率')).toHaveTextContent('63.0%')
    expect(tooltipFor('磁盘使用率')).toHaveTextContent('61.0%')
    expect(tooltipFor('Inode 使用率')).toHaveTextContent('13.0%')
    expect(tooltipFor('Load5')).toHaveTextContent('0.9')
    expect(tooltipFor('CPU IOWait')).toHaveTextContent('7.0%')
    expect(tooltipFor('网络入')).toHaveTextContent('4.0 KB/s')
    expect(tooltipFor('网络出')).toHaveTextContent('8.0 KB/s')
  })

  it('renders an empty state when no host metric points are available', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord({ monitoring_instance_id: 'mi_empty', display_name: 'Empty Trend Monitoring Instance', binding_status: '已绑定' })))
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_empty',
            latest_host_sample: null,
            recent_host_samples: [],
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_empty']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Empty Trend Monitoring Instance' })).toBeInTheDocument(),
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
            monitoring_instance_id: 'mi_002',
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
            monitoring_instance_id: 'mi_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_002']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByText('尚未收到主机样本')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('该监控实例已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。'),
    ).toBeInTheDocument()
  })

  it('keeps monitoring details visible when incidents and events fail to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_003',
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
            monitoring_instance_id: 'mi_003',
            latest_host_sample: {
              monitoring_instance_id: 'mi_003',
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
              mem_total_bytes: 8589934592,
              swap_used_pct: 2,
              disk_used_pct: 88,
              disk_total_bytes: 107374182400,
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
      <MemoryRouter initialEntries={['/monitoring/mi_003']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
      screen.queryByRole('heading', { name: '监控实例详情不可用' }),
    ).not.toBeInTheDocument()
  })

  it('renders a high-priority binding conflict card on monitoring instance detail', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
    expect(screen.getByText('标签与备注', { selector: 'summary' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '生命周期' })).not.toBeInTheDocument()
    expect(screen.queryByText('接入凭证状态')).not.toBeInTheDocument()
    openRuntimeMenu()
    expect(screen.getByRole('button', { name: '升级/重新接入 agent…' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_conflict/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps monitoring instance detail visible when binding conflict metadata fails to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse({ error: 'onboarding unavailable' }, 503)),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByText('onboarding unavailable')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '监控实例详情不可用' })).not.toBeInTheDocument()
  })

  it('keeps binding actions disabled until conflict metadata is loaded', async () => {
    const onboarding = deferredResponse()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockImplementationOnce(() => onboarding.promise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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


  it('confirms a pending monitoring instance rebind from monitoring instance detail and hides the conflict card', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
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
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('确认重绑定'))
    const rebindDialog = screen.getByRole('alertdialog', { name: '确认重绑定' })
    expect(fetchMock).toHaveBeenCalledTimes(5)
    fireEvent.click(within(rebindDialog).getByRole('button', { name: '确认重绑定' }))

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    // "已绑定" StatusBadge remains visible in the header badge row
    expect(screen.getAllByText('已绑定').length).toBeGreaterThanOrEqual(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_conflict/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('rejects a pending fingerprint from monitoring instance detail', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
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
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const rejectButton = await waitForEnabledButton('拒绝新指纹')
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeEnabled()

    fireEvent.click(rejectButton)
    const rejectDialog = screen.getByRole('alertdialog', { name: '拒绝新指纹' })
    expect(fetchMock).toHaveBeenCalledTimes(5)
    fireEvent.click(within(rejectDialog).getByRole('button', { name: '拒绝新指纹' }))
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_conflict/binding/reject-pending', {
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

  it('resets monitoring instance binding from monitoring instance detail and returns to the unbound state', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
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
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('重置绑定'))
    const resetDialog = screen.getByRole('alertdialog', { name: '重置绑定' })
    expect(fetchMock).toHaveBeenCalledTimes(5)
    fireEvent.click(within(resetDialog).getByRole('button', { name: '重置绑定' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_conflict/binding/reset', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    // "未绑定" StatusBadge remains visible in the header badge row
    expect(screen.getAllByText('未绑定').length).toBeGreaterThanOrEqual(1)
  })

  it('keeps binding action errors inside the confirmation modal and preserves the conflict card', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord()))
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts()))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(onboardingConflictState()))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid binding transition' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_conflict']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('重置绑定'))
    const resetDialog = screen.getByRole('alertdialog', { name: '重置绑定' })
    expect(fetchMock).toHaveBeenCalledTimes(5)
    fireEvent.click(within(resetDialog).getByRole('button', { name: '重置绑定' }))

    await waitFor(() =>
      expect(within(screen.getByRole('alertdialog', { name: '重置绑定' })).getByRole('alert')).toHaveTextContent('invalid binding transition'),
    )
    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '绑定冲突处置' })).toBeInTheDocument()
  })

  it('shows the new route core data without stale activity while route-specific requests are still in flight', async () => {
    const mi001Response = deferredResponse()
    const nd001Runtime = deferredResponse()
    const nd001Incidents = deferredResponse()
    const nd001Events = deferredResponse()
    const mi002Response = deferredResponse()
    const nd002Runtime = deferredResponse()
    const nd002Incidents = deferredResponse()
    const nd002Events = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockImplementationOnce(() => mi001Response.promise)
        .mockImplementationOnce(() => nd001Runtime.promise)
        .mockImplementationOnce(() => nd001Incidents.promise)
        .mockImplementationOnce(() => nd001Events.promise)
        .mockImplementationOnce(() => mi002Response.promise)
        .mockImplementationOnce(() => nd002Runtime.promise)
        .mockImplementationOnce(() => nd002Incidents.promise)
        .mockImplementationOnce(() => nd002Events.promise),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <MonitoringDetailTestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'switch monitoring instance' }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(8))

    mi002Response.resolve(
      mockJSONResponse({
        monitoring_instance_id: 'mi_002',
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
        monitoring_instance_id: 'mi_002',
        latest_host_sample: {
          monitoring_instance_id: 'mi_002',
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
          mem_total_bytes: 8589934592,
          swap_used_pct: 0,
          disk_used_pct: 40,
          disk_total_bytes: 107374182400,
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
    // Watchtower main view has 8 metric cards (sample is non-null for mi_002)
    expect(
      screen.queryByRole('heading', { name: 'Tokyo Edge' }),
    ).not.toBeInTheDocument()

    mi001Response.resolve(
      mockJSONResponse({
        monitoring_instance_id: 'mi_001',
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
        current_primary_issue_summary: '旧监控实例异常',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-24T09:05:00Z',
      }),
    )
    nd001Runtime.resolve(
      mockJSONResponse({
        monitoring_instance_id: 'mi_001',
        latest_host_sample: null,
      }),
    )
    nd001Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_old',
          incident_class: 'monitoring_instance_disk_pressure',
          object_type: 'monitoring_instance',
          object_id: 'mi_001',
          severity: '严重',
          started_at: '2026-04-24T09:00:00Z',
          last_evaluated_at: '2026-04-24T09:05:00Z',
          source_summary: '旧监控实例异常摘要',
        },
      ]),
    )
    nd001Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_old',
          incident_id: 'inc_old',
          incident_class: 'monitoring_instance_disk_pressure',
          object_type: 'monitoring_instance',
          object_id: 'mi_001',
          event_type: 'incident_started',
          severity: '严重',
          summary: '旧监控实例事件',
          created_at: '2026-04-24T09:05:00Z',
        },
      ]),
    )

    nd002Incidents.resolve(
      mockJSONResponse([
        {
          incident_id: 'inc_new',
          incident_class: 'monitoring_instance_resource_pressure',
          object_type: 'monitoring_instance',
          object_id: 'mi_002',
          severity: '关注',
          started_at: '2026-04-24T10:00:00Z',
          last_evaluated_at: '2026-04-24T10:05:00Z',
          source_summary: '新监控实例异常摘要',
        },
      ]),
    )
    nd002Events.resolve(
      mockJSONResponse([
        {
          event_id: 'evt_new',
          incident_id: 'inc_new',
          incident_class: 'monitoring_instance_resource_pressure',
          object_type: 'monitoring_instance',
          object_id: 'mi_002',
          event_type: 'incident_started',
          severity: '关注',
          summary: '新监控实例事件',
          created_at: '2026-04-24T10:05:00Z',
        },
      ]),
    )

    expect(screen.queryByText('旧监控实例异常摘要')).not.toBeInTheDocument()
    expect(screen.queryByText('旧监控实例事件')).not.toBeInTheDocument()
  })

  it('renders runtime controls and applies light maintenance actions from the detail page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_maintenance',
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
          monitoring_instance_id: 'mi_maintenance',
          latest_host_sample: null,
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_maintenance',
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
      <MemoryRouter initialEntries={['/monitoring/mi_maintenance']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_maintenance/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('requires strong confirmation before pausing monitoringInstance monitoring from the detail page', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_001',
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
          monitoring_instance_id: 'mi_001',
          latest_host_sample: null,
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_001',
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
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    const pauseButton = screen.getByRole('button', { name: '暂停监控' })
    fireEvent.click(pauseButton)

    expect(screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：监控运行状态为启用。')).toBeInTheDocument()
    expect(screen.getByText('操作后：监控运行状态变为暂停。')).toBeInTheDocument()
    expect(
      screen.getByText(
        '会停止主机指标采集，并停止该监控实例承担的探针执行。趋势图会从此开始出现数据空档。',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('不会删除历史事件、观测记录或 agent 绑定关系。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('heading', { name: '确认暂停监控实例监控' })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)
    await waitFor(() => expect(screen.getByRole('button', { name: '暂停监控' })).toHaveFocus())

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))
    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复监控' })).toHaveFocus())
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('shows maintenance-state current copy when pausing from a maintenance monitoring detail view', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_maintenance_pause',
              binding_status: '已绑定',
              monitoring_status: '维护中',
              current_health_status: '正常',
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_maintenance_pause')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_maintenance_pause']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })).toBeInTheDocument()
    expect(screen.getByText('当前：监控运行状态为维护中。')).toBeInTheDocument()
  })

  it('keeps the pause confirmation visible and reusable after a pause API failure', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_pause_error',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_pause_error')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'pause failed' }, 500))
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_pause_error',
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
      <MemoryRouter initialEntries={['/monitoring/mi_pause_error']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
    expect(screen.getByRole('alertdialog', { name: '确认暂停监控实例监控' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认暂停监控' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '确认暂停监控' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_pause_error/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/monitoring-instances/mi_pause_error/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
        credentials: 'include',
    })
  })

  it('keeps monitoring lifecycle actions out of the watchtower actions menu', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_retired',
              lifecycle_status: '已退役',
              monitoring_status: '暂停',
              binding_status: '已绑定',
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_retired')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_retired']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )
    openRuntimeMenu()

    expect(screen.queryByRole('button', { name: '恢复到观察中' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退役监控实例' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '生命周期' })).not.toBeInTheDocument()
  })

  it('ignores a stale runtime-action success after switching to a different monitoringInstance route', async () => {
    const runtimeAction = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_001',
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
            monitoring_instance_id: 'mi_001',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockImplementationOnce(() => runtimeAction.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_002',
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
            monitoring_instance_id: 'mi_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <MonitoringDetailTestHarness />
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '退出维护' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch monitoring instance' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    openRuntimeMenu()
    expect(screen.getByRole('button', { name: '进入维护' })).toBeEnabled()

    runtimeAction.resolve(
      mockJSONResponse({
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

  it('ignores a stale binding-action success after switching to a different monitoringInstance route', async () => {
    const confirmRebind = deferredResponse()

    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(mockJSONResponse(monitoringInstanceRecord({ monitoring_instance_id: 'mi_001' })))
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_001')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse(
            onboardingConflictState({
              monitoring_instance_id: 'mi_001',
            }),
          ),
        )
        .mockImplementationOnce(() => confirmRebind.promise)
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_002',
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
            monitoring_instance_id: 'mi_002',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_001']}>
        <MonitoringDetailTestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(await waitForEnabledButton('确认重绑定'))
    const rebindDialog = screen.getByRole('alertdialog', { name: '确认重绑定' })
    fireEvent.click(within(rebindDialog).getByRole('button', { name: '确认重绑定' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch monitoring instance' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
    expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument()

    confirmRebind.resolve(
      mockJSONResponse(
        onboardingConflictState({
          monitoring_instance_id: 'mi_001',
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
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_calm',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_calm')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_calm']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_alert',
              binding_status: '已绑定',
              current_health_status: '严重',
              current_active_incident_count: 3,
              current_primary_issue_summary: '磁盘使用率持续超过阈值',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_alert')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_alert']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_metrics',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_metrics',
            latest_host_sample: {
              monitoring_instance_id: 'mi_metrics',
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
              mem_total_bytes: 8589934592,
              swap_used_pct: 1,
              disk_used_pct: 60,
              disk_total_bytes: 107374182400,
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
      <MemoryRouter initialEntries={['/monitoring/mi_metrics']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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

  it('removes the old folded property sections while keeping standalone data sections', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_secondary',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_secondary')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_secondary']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(container.querySelectorAll('.collapsible-section.watchtower-secondary').length).toBe(0)
    expect(screen.getByText('标签与备注', { selector: 'summary' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '生命周期' })).not.toBeInTheDocument()
    expect(screen.queryByText('接入凭证状态')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '关联 VPS' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '容器列表' })).not.toBeInTheDocument()
    // Page footer surfaces the snapshot meta line
    expect(container.querySelector('.watchtower-snapshot-meta')).not.toBeNull()
  })

  it('keeps container inventory out of the monitoring detail body', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_ctr',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_ctr',
            latest_host_sample: {
              monitoring_instance_id: 'mi_ctr',
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
              mem_total_bytes: 8589934592,
              swap_used_pct: 0,
              disk_used_pct: 30,
              disk_total_bytes: 107374182400,
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
      <MemoryRouter initialEntries={['/monitoring/mi_ctr']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.queryByRole('heading', { name: '容器列表' })).not.toBeInTheDocument()
    expect(screen.queryByText('nginx-prod')).not.toBeInTheDocument()
    expect(screen.queryByText('redis-cache')).not.toBeInTheDocument()
    expect(screen.queryByText('nginx:1.25-alpine')).not.toBeInTheDocument()
    expect(screen.queryByText('redis:7-alpine')).not.toBeInTheDocument()
  })

  it('does not render empty container messaging when host samples omit containers', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_noctr',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_noctr',
            latest_host_sample: {
              monitoring_instance_id: 'mi_noctr',
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
              mem_total_bytes: 8589934592,
              swap_used_pct: 0,
              disk_used_pct: 20,
              disk_total_bytes: 107374182400,
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
      <MemoryRouter initialEntries={['/monitoring/mi_noctr']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.queryByRole('heading', { name: '容器列表' })).not.toBeInTheDocument()
    expect(screen.queryByText('暂无容器数据')).not.toBeInTheDocument()
  })

  it('triggers runtime maintenance via the watchtower header operations menu', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_ops',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_ops')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_ops',
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
      <MemoryRouter initialEntries={['/monitoring/mi_ops']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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
      expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_ops/runtime/enter-maintenance', {
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
          monitoring_instance_id: 'mi_history',
          display_name: 'History Monitoring Instance',
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
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_history')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            event_id: 'evt_history_001',
            incident_id: 'inc_h_001',
            incident_class: 'monitoring_instance_disk_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_history',
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
            incident_class: 'monitoring_instance_disk_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_history',
            severity: '告警',
            started_at: '2026-04-25T10:00:00Z',
            last_evaluated_at: '2026-04-25T11:00:00Z',
            source_summary: '历史已恢复异常摘要',
          },
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_history']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'History Monitoring Instance' })).toBeInTheDocument(),
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
          String(call[0]).includes('/api/incidents?object_type=monitoring_instance&object_id=mi_history&include_resolved=true'),
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

  it('renders time window Tabs with realtime selected by default', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_calm',
              binding_status: '已绑定',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            monitoring_instance_id: 'mi_calm',
            latest_host_sample: null,
          }),
        )
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_calm']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    const tablist = screen.getByRole('tablist')
    expect(tablist).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '实时' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: '24h' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('tab', { name: '7d' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('tab', { name: '30d' })).toHaveAttribute('aria-selected', 'false')
  })

  it('switches to 7d window and fetches runtime facts with window=7d', async () => {
    const runtimeResponse = mockJSONResponse({
      monitoring_instance_id: 'mi_calm',
      latest_host_sample: null,
    })
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_calm',
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
      <MemoryRouter initialEntries={['/monitoring/mi_calm']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    // Initial load fetches runtime facts with the default realtime window.
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_calm/runtime-facts?window=realtime', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })

    // Switch to 7d.
    fireEvent.click(screen.getByRole('tab', { name: '7d' }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((call) =>
          call[0] === '/api/monitoring-instances/mi_calm/runtime-facts?window=7d',
        ),
      ).toBe(true),
    )
  })

  it('opens a realtime runtime stream, renders host samples, and closes it when leaving realtime', async () => {
    MockRuntimeWebSocket.instances = []
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_realtime',
            binding_status: '已绑定',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_realtime',
          latest_host_sample: null,
          window: {
            key: 'realtime',
            started_at: '2026-04-24T09:00:00Z',
            ended_at: '2026-04-24T10:00:00Z',
            bucket_count: 720,
            available_started_at: null,
            available_ended_at: null,
            sample_count: 0,
          },
          recent_host_samples: [],
        }),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          monitoring_instance_id: 'mi_realtime',
          latest_host_sample: null,
          window: {
            key: '24h',
            started_at: '2026-04-23T10:00:00Z',
            ended_at: '2026-04-24T10:00:00Z',
            bucket_count: 288,
            available_started_at: null,
            available_ended_at: null,
            sample_count: 0,
          },
          host_metric_points: [],
        }),
      )
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('WebSocket', MockRuntimeWebSocket)

    const { container } = render(
      <MemoryRouter initialEntries={['/monitoring/mi_realtime']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    await waitFor(() => expect(MockRuntimeWebSocket.instances).toHaveLength(1))
    const socket = MockRuntimeWebSocket.instances[0]
    expect(new URL(socket.url).protocol).toBe('ws:')
    expect(new URL(socket.url).pathname).toBe('/api/monitoring-instances/mi_realtime/runtime-stream')

    act(() => {
      socket.emitOpen()
    })
    await waitFor(() => expect(screen.getByText('已连接')).toBeInTheDocument())

    const firstSample = hostSampleRecord('mi_realtime', {
      observed_at: '2026-04-24T10:00:00Z',
      received_at: '2026-04-24T10:00:01Z',
      cpu_usage_pct: 40,
      mem_used_pct: 62,
      disk_used_pct: 60,
      inode_used_pct: 12,
      load_5: 0.8,
      cpu_iowait_pct: 6,
      net_in_bytes_per_sec: 2048,
      net_out_bytes_per_sec: 4096,
    })
    const secondSample = hostSampleRecord('mi_realtime', {
      observed_at: '2026-04-24T10:00:05Z',
      received_at: '2026-04-24T10:00:06Z',
      cpu_usage_pct: 42,
      mem_used_pct: 63,
      disk_used_pct: 61,
      inode_used_pct: 13,
      load_5: 0.9,
      cpu_iowait_pct: 7,
      net_in_bytes_per_sec: 4096,
      net_out_bytes_per_sec: 8192,
    })
    act(() => {
      socket.emitMessage({
        type: 'host_sample',
        monitoring_instance_id: 'mi_realtime',
        sample: firstSample,
        received_at: '2026-04-24T10:00:01Z',
      })
      socket.emitMessage({
        type: 'host_sample',
        monitoring_instance_id: 'mi_realtime',
        sample: secondSample,
        received_at: '2026-04-24T10:00:06Z',
      })
    })

    await waitFor(() => expect(screen.getByText('实时滚动 2 点 · 已按阈值优先级排序')).toBeInTheDocument())
    expect(screen.getByText('42.0%')).toBeInTheDocument()
    expect(container.querySelectorAll('.watchtower-metrics polyline').length).toBe(8)

    fireEvent.click(screen.getByRole('tab', { name: '24h' }))
    await waitFor(() => expect(socket.close).toHaveBeenCalledTimes(1))
    expect(screen.queryByText('已连接')).not.toBeInTheDocument()
  })

  // ── Command execution ──

  it('shows onboarding, command, and lifecycle actions in the watchtower header operations popover', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_cmd',
              binding_status: '已绑定',
              monitoring_status: '启用',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()

    expect(screen.getByRole('button', { name: '升级/重新接入 agent…' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '执行命令…' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退役监控实例' })).not.toBeInTheDocument()
  })

  it('uses upgrade wording when a bound monitoring instance opens onboarding', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_upgrade',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_upgrade')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_upgrade']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '升级/重新接入 agent…' }))

    const drawer = await screen.findByRole('dialog', { name: '监控实例接入抽屉' })
    expect(within(drawer).getByRole('heading', { name: '升级/重新接入 agent' })).toBeInTheDocument()
    expect(within(drawer).getByRole('button', { name: '生成升级/重新接入命令' })).toBeInTheDocument()
  })

  it('opens command drawer with 8 preset command options', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse(
            monitoringInstanceRecord({
              monitoring_instance_id: 'mi_cmd2',
              binding_status: '已绑定',
              monitoring_status: '启用',
              current_health_status: '正常',
              current_active_incident_count: 0,
              current_primary_issue_summary: '',
            }),
          ),
        )
        .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd2')))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd2']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
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

  it('shows pending command identity immediately after command dispatch', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_cmd3',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd3')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          action_id: 'act_001',
          command_id: 'uptime',
          status: 'pending',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd3']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))
    const uptimeButton = await screen.findByRole('button', { name: /uptime/ })
    fireEvent.click(uptimeButton)

    await waitFor(() =>
      expect(screen.getByText('uptime · 等待 agent 执行…')).toBeInTheDocument(),
    )
    expect(screen.getByText(/已下发，等待 agent 执行/)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_cmd3/actions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ command_id: 'uptime' }),
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('requires confirmation before posting sensitive monitoring instance commands', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_cmd_sensitive',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd_sensitive')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(
        mockJSONResponse({
          action_id: 'act_sensitive',
          command_id: 'systemctl_status',
          status: 'pending',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd_sensitive']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))
    fireEvent.click(await screen.findByRole('button', { name: /systemctl status/ }))

    const dialog = await screen.findByRole('alertdialog', { name: '确认执行敏感命令' })
    expect(within(dialog).getByText(/systemctl status/)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)

    fireEvent.click(within(dialog).getByRole('button', { name: '确认执行' }))

    await waitFor(() =>
      expect(screen.getByText('systemctl status · 等待 agent 执行…')).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_cmd_sensitive/actions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ command_id: 'systemctl_status', confirmed_sensitive: true }),
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('does not post a sensitive command when confirmation is cancelled', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_cmd_cancel',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd_cancel')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd_cancel']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))
    fireEvent.click(await screen.findByRole('button', { name: /systemctl status/ }))

    const dialog = await screen.findByRole('alertdialog', { name: '确认执行敏感命令' })
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))

    await waitFor(() =>
      expect(screen.queryByRole('alertdialog', { name: '确认执行敏感命令' })).not.toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('renders expired command output without stale stdout or stderr', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse(
          monitoringInstanceRecord({
            monitoring_instance_id: 'mi_cmd_expired',
            binding_status: '已绑定',
            monitoring_status: '启用',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            last_action: {
              action_id: 'act_expired',
              command_id: 'uptime',
              status: 'done',
              exit_code: 0,
              completed_at: '2026-06-25T12:00:00Z',
              output_expires_at: '2026-06-26T12:00:00Z',
              output_expired: true,
            },
          }),
        ),
      )
      .mockResolvedValueOnce(mockJSONResponse(emptyRuntimeFacts('mi_cmd_expired')))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_cmd_expired']}>
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<MonitoringDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    openRuntimeMenu()
    fireEvent.click(screen.getByRole('button', { name: '执行命令…' }))
    const drawer = await screen.findByRole('dialog', { name: '执行命令抽屉' })

    expect(within(drawer).getByText(/命令输出已过期/)).toBeInTheDocument()
    expect(within(drawer).queryByText('stdout')).not.toBeInTheDocument()
    expect(within(drawer).queryByText('stderr')).not.toBeInTheDocument()
  })

})
