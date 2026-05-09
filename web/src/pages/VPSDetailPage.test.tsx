import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSDetailPage } from './VPSDetailPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('VPSDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders VPS facts and linked Node monitoring summaries', async () => {
    const responseBody = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: 'ord-1',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: 'nrt',
      ipv4: '192.0.2.1',
      ipv6: '',
      ssh_host: '192.0.2.1',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: 'Debian',
      virtualization: 'kvm',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: ['edge'],
      note: 'primary',
      active_node_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [
        {
          node_id: 'nd_001',
          display_name: 'Tokyo Node',
          group: 'edge',
          region: 'JP',
          city: 'Tokyo',
          provider: 'Node Hint',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          current_health_status: '关注',
          last_heartbeat_at: '2026-05-09T08:10:00Z',
          last_sync_at: '2026-05-09T08:11:00Z',
          current_active_incident_count: 1,
          current_primary_issue_summary: 'latency high',
          linked_at: '2026-05-09T08:00:00Z',
          note: 'primary',
        },
      ],
    }
    const timelineBody = {
      vps_id: 'vps_001',
      renewal_decisions: [
        {
          decision_id: 'rdec_001',
          vps_id: 'vps_001',
          from_decision: 'unreviewed',
          to_decision: 'keep',
          reason: '稳定承载边缘流量',
          decided_at: '2026-05-09T08:12:00Z',
          created_at: '2026-05-09T08:12:00Z',
        },
      ],
      price_histories: [
        {
          price_history_id: 'ph_001',
          subscription_id: 'sub_001',
          vps_id: 'vps_001',
          from_price: 10,
          to_price: 12,
          from_currency: 'USD',
          to_currency: 'USD',
          from_billing_cycle: 'monthly',
          to_billing_cycle: 'monthly',
          from_billing_months: 1,
          to_billing_months: 1,
          from_monthly_price: 10,
          to_monthly_price: 12,
          from_renew_at: '2026-05-01',
          to_renew_at: '2026-06-01',
          from_auto_renew: true,
          to_auto_renew: true,
          from_auto_renew_cancelled: false,
          to_auto_renew_cancelled: false,
          from_status: 'active',
          to_status: 'active',
          changed_at: '2026-05-09T08:13:00Z',
          created_at: '2026-05-09T08:13:00Z',
        },
      ],
      ip_histories: [
        {
          ip_history_id: 'iph_001',
          vps_id: 'vps_001',
          from_ipv4: '192.0.2.10',
          to_ipv4: '192.0.2.1',
          from_ipv6: '',
          to_ipv6: '2001:db8::1',
          changed_at: '2026-05-09T08:14:00Z',
          created_at: '2026-05-09T08:14:00Z',
        },
      ],
      spec_snapshots: [
        {
          snapshot_id: 'vss_001',
          vps_id: 'vps_001',
          product_name: 'cx22',
          ssh_host: '192.0.2.1',
          ssh_port: 22,
          ssh_user: 'root',
          os_name: 'Debian',
          virtualization: 'kvm',
          captured_at: '2026-05-09T08:15:00Z',
          created_at: '2026-05-09T08:15:00Z',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(screen.getByText('基础信息')).toBeInTheDocument()
    expect(screen.getAllByText('192.0.2.1').length).toBeGreaterThan(0)
    expect(screen.getByText('关联 Node 监控')).toBeInTheDocument()
    expect(screen.getByText('Tokyo Node')).toBeInTheDocument()
    expect(screen.getByText('latency high')).toBeInTheDocument()
    expect(screen.getByText('资产历史')).toBeInTheDocument()
    expect(screen.getByText('未评估 -> 保留')).toBeInTheDocument()
    expect(screen.getByText('稳定承载边缘流量')).toBeInTheDocument()
    expect(screen.getAllByText('USD 10.00 -> USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getByText('192.0.2.10 -> 192.0.2.1')).toBeInTheDocument()
    expect(screen.getByText('root@192.0.2.1:22')).toBeInTheDocument()
  })

  it('renders compact empty states when the VPS has no timeline records', async () => {
    const responseBody = {
      vps_id: 'vps_empty',
      display_name: 'Empty Edge',
      provider_id: null,
      provider_name: '',
      product_name: '',
      order_ref: '',
      country: '',
      region: '',
      city: '',
      datacenter: '',
      ipv4: '',
      ipv6: '',
      ssh_host: '',
      ssh_port: 22,
      ssh_user: '',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'idle',
      usage_status: 'unknown',
      renewal_decision: 'unreviewed',
      importance: 'normal',
      labels: [],
      note: '',
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const timelineBody = {
      vps_id: 'vps_empty',
      renewal_decisions: [],
      price_histories: [],
      ip_histories: [],
      spec_snapshots: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_empty']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Empty Edge' })).toBeInTheDocument())
    expect(screen.getByText('暂无续费决策历史')).toBeInTheDocument()
    expect(screen.getByText('暂无价格变化历史')).toBeInTheDocument()
    expect(screen.getByText('暂无 IP 变化历史')).toBeInTheDocument()
    expect(screen.getByText('暂无规格快照')).toBeInTheDocument()
  })
})
