import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { NodeComparePage } from './NodeComparePage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function nodeRecord(nodeId: string, displayName: string, overrides: Partial<Record<string, unknown>> = {}) {
  return {
    node_id: nodeId,
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

function hostSample(nodeId: string) {
  return {
    node_id: nodeId,
    observed_at: '2026-05-15T08:05:00Z',
    received_at: '2026-05-15T08:05:02Z',
    agent_version: 'dev',
    fingerprint: `${nodeId}-fingerprint`,
    cpu_usage_pct: 22,
    load_1: 0.2,
    load_5: 0.3,
    load_15: 0.4,
    mem_used_pct: 48,
    mem_available_bytes: 2147483648,
    swap_used_pct: 0,
    disk_used_pct: 51,
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
    sync_batch_id: `${nodeId}-sync`,
  }
}

function runtimeFacts(nodeId: string) {
  const sample = hostSample(nodeId)
  return {
    node_id: nodeId,
    latest_host_sample: sample,
    recent_host_samples: [sample],
  }
}

function renderNodeCompare(initialEntry: string) {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/nodes/compare" element={<NodeComparePage />} />
        <Route path="/nodes" element={<div>nodes route</div>} />
        <Route path="/nodes/:nodeId" element={<div>node detail route</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('NodeComparePage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('uses a PageState empty state when two node IDs are not selected', () => {
    renderNodeCompare('/nodes/compare?id=nd_a')

    expect(screen.getByRole('heading', { name: '需要选择 2 个节点' })).toBeInTheDocument()
    expect(screen.getByText('请先在节点列表勾选两个节点，再进入 A / B 指标对比。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回节点列表' })).toHaveAttribute('href', '/nodes')
  })

  it('renders A/B identity composition and metric placeholders from node/runtime facts', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord('nd_a', 'Tokyo Edge')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('nd_a')))
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord('nd_b', 'Osaka Core', { current_health_status: '告警' })))
      .mockResolvedValueOnce(mockJSONResponse({ node_id: 'nd_b', latest_host_sample: null, recent_host_samples: [] }))
    vi.stubGlobal('fetch', fetchMock)

    renderNodeCompare('/nodes/compare?id=nd_a&id=nd_b')

    await waitFor(() => expect(screen.getByRole('link', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(screen.getByText('对比对象 A')).toBeInTheDocument()
    expect(screen.getByText('对比对象 B')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Osaka Core' })).toHaveAttribute('href', '/nodes/nd_b')
    expect(screen.getAllByRole('link', { name: '节点详情' })).toHaveLength(2)
    expect(screen.getByRole('heading', { name: 'CPU 使用率' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '尚未收到主机样本' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_a', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_a/runtime-facts?window=24h', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows a PageState error when one side cannot be loaded', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(nodeRecord('nd_a', 'Tokyo Edge')))
      .mockResolvedValueOnce(mockJSONResponse(runtimeFacts('nd_a')))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'node not found' }, 404))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'node not found' }, 404))
    vi.stubGlobal('fetch', fetchMock)

    renderNodeCompare('/nodes/compare?id=nd_a&id=nd_missing')

    await waitFor(() => expect(screen.getByRole('heading', { name: 'B 节点不可用' })).toBeInTheDocument())
    expect(screen.getAllByText('节点不存在').length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: '返回节点列表重新选择' })).toHaveAttribute('href', '/nodes')
  })
})
