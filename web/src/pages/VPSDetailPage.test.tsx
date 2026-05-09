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
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse(responseBody))
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
    expect(screen.getByText('基础信息')).toBeInTheDocument()
    expect(screen.getAllByText('192.0.2.1').length).toBeGreaterThan(0)
    expect(screen.getByText('关联 Node 监控')).toBeInTheDocument()
    expect(screen.getByText('Tokyo Node')).toBeInTheDocument()
    expect(screen.getByText('latency high')).toBeInTheDocument()
  })
})
