import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { NodeDetailPage } from './NodeDetailPage'
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

    expect(screen.getByText('当前主机指标')).toBeInTheDocument()
    expect(screen.getByText('12.5%')).toBeInTheDocument()
    expect(screen.getByText(/2.0 GB/i)).toBeInTheDocument()
    expect(screen.getByText('2小时 0分钟')).toBeInTheDocument()
    expect(screen.getByText('当前活跃异常')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率持续超过阈值')).toBeInTheDocument()
    expect(screen.getByText('最近相关事件')).toBeInTheDocument()
    expect(screen.getByText('磁盘压力已升级为严重')).toBeInTheDocument()
    expect(screen.queryByText('将在 incidents / events 切片接入后替换为真实内容。')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime-facts', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/incidents?object_type=node&object_id=nd_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/events?object_type=node&object_id=nd_001',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      },
    )
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
      screen.getByText('该节点已存在，但首批 HostSample 还未到达。请等待下一次 agent 同步。'),
    ).toBeInTheDocument()
    expect(screen.getByText('当前没有活跃异常')).toBeInTheDocument()
    expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument()
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

    expect(screen.getByText('当前主机指标')).toBeInTheDocument()
    expect(screen.getByText('18.0%')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '活跃异常暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('incidents unavailable')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '相关事件暂不可用' })).toBeInTheDocument()
    expect(screen.getByText('events unavailable')).toBeInTheDocument()
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

    expect(screen.getByText('高优先级：绑定冲突待处理')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('fp-current-1234567890')).toBeInTheDocument())
    expect(screen.getByText('fp-pendi…uvwxyz')).toBeInTheDocument()
    expect(screen.getByText(formatDateTime('2026-04-27T08:55:00Z'))).toBeInTheDocument()
    expect(screen.getByText(formatDateTime('2026-04-27T09:04:00Z'))).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(screen.getByText(/同一台机器重装或合法替换/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开接入工作台' })).toHaveAttribute(
      'href',
      '/nodes/nd_conflict/onboarding',
    )
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '确认重绑定' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '确认重绑定' }))

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    expect(screen.getByText('已绑定')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '拒绝新指纹' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('button', { name: '重置绑定' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '拒绝新指纹' }))
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/reject-pending', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '重置绑定' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '重置绑定' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_conflict/binding/reset', {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      }),
    )
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: '绑定冲突处置' })).not.toBeInTheDocument(),
    )
    expect(screen.getByText('未绑定')).toBeInTheDocument()
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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '重置绑定' })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: '重置绑定' }))

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
    expect(screen.getByText('当前主机指标')).toBeInTheDocument()
    expect(screen.getByText('正在加载活跃异常…')).toBeInTheDocument()
    expect(screen.getByText('正在加载相关事件…')).toBeInTheDocument()
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

    await waitFor(() =>
      expect(screen.getByText('新节点异常摘要')).toBeInTheDocument(),
    )
    expect(screen.getByText('新节点事件')).toBeInTheDocument()
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

    expect(screen.getByRole('heading', { name: '运行控制' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '退出维护' }))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '进入维护' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_maintenance/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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

    fireEvent.click(screen.getByRole('button', { name: '暂停监控' }))

    expect(confirmMock).toHaveBeenCalledWith('暂停监控会停止采集并产生数据空档，确定继续吗？')
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复监控' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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
      expect(screen.getByRole('heading', { name: '生命周期' })).toBeInTheDocument(),
    )

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
      expect(screen.getByRole('button', { name: '退役节点' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '退役节点' }))
    expect(screen.getByRole('button', { name: '确认退役' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeInTheDocument()
    expect(screen.getByText(/这不是删除/)).toBeInTheDocument()
    expect(screen.getByText(/不会清空事件、 observation 或 agent 绑定历史/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '确认退役' }))

    expect(confirmMock).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/nodes/nd_lifecycle/lifecycle/retire', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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
      expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: '恢复到观察中' }))

    await waitFor(() => expect(screen.getByText('观察中')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      '/api/nodes/nd_restore/lifecycle/restore-to-observing',
      {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
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
      expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: '恢复到观察中' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('invalid lifecycle transition'),
    )
    expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '生命周期' })).toBeInTheDocument()
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

    fireEvent.click(screen.getByRole('button', { name: '退出维护' }))
    fireEvent.click(screen.getByRole('button', { name: 'switch node' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Seoul Edge' })).toBeInTheDocument(),
    )
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
      expect(screen.getByRole('button', { name: '恢复到观察中' })).toBeInTheDocument(),
    )

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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '确认重绑定' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '确认重绑定' }))
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
})
