import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { NodeDetailPage } from './NodeDetailPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
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
})
