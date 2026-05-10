import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSDetailPage } from './VPSDetailPage'

const timelineEmptyBody = {
  vps_id: 'vps_001',
  renewal_decisions: [],
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [],
}

const servicesEmptyBody: unknown[] = []
const domainsEmptyBody: unknown[] = []

const serviceBody = {
  service_id: 'svc_001',
  vps_id: 'vps_001',
  target_id: 'tg_001',
  name: 'Blog',
  service_type: 'web',
  status: 'active',
  url: 'https://blog.example.com',
  port: 443,
  labels: ['prod'],
  note: 'primary service',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const domainBody = {
  domain_id: 'dom_001',
  vps_id: 'vps_001',
  service_id: 'svc_001',
  target_id: 'tg_001',
  domain_name: 'www.example.com',
  purpose: 'site',
  status: 'active',
  registrar: 'NameSilo',
  expires_at: '2026-07-01',
  auto_renew: true,
  https_enabled: true,
  labels: ['prod'],
  note: 'primary domain',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

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
      experience_logs: [
        {
          experience_log_id: 'elog_001',
          vps_id: 'vps_001',
          category: 'network',
          severity: 'warning',
          summary: '晚高峰丢包',
          details: '已向服务商提交工单',
          occurred_at: '2026-05-09T08:16:00Z',
          created_at: '2026-05-09T08:16:30Z',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineBody))
      .mockResolvedValueOnce(mockJSONResponse([serviceBody]))
      .mockResolvedValueOnce(mockJSONResponse([domainBody]))
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
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/domains', {
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
    expect(screen.getByText('晚高峰丢包')).toBeInTheDocument()
    expect(screen.getByText('已向服务商提交工单')).toBeInTheDocument()
    expect(screen.getByText('服务资产')).toBeInTheDocument()
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('https://blog.example.com')).toBeInTheDocument()
    expect(screen.getByText('tg_001')).toBeInTheDocument()
    expect(screen.getByText('域名资产')).toBeInTheDocument()
    expect(screen.getByText('www.example.com')).toBeInTheDocument()
    expect(screen.getByText('NameSilo')).toBeInTheDocument()
    expect(screen.getByText('2026-07-01')).toBeInTheDocument()
  })

  it('updates the renewal decision and refreshes asset history', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const updatedRecord = {
      ...detailBody,
      renewal_decision: 'cancel',
      updated_at: '2026-05-09T09:00:00Z',
      active_node_link_count: 0,
    }
    const refreshedDetail = {
      ...updatedRecord,
      node_links: [],
    }
    const refreshedTimeline = {
      vps_id: 'vps_001',
      renewal_decisions: [
        {
          decision_id: 'rdec_002',
          vps_id: 'vps_001',
          from_decision: 'keep',
          to_decision: 'cancel',
          reason: 'too expensive',
          decided_at: '2026-05-09T09:01:00Z',
          created_at: '2026-05-09T09:01:00Z',
        },
      ],
      price_histories: [],
      ip_histories: [],
      spec_snapshots: [],
      experience_logs: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(updatedRecord))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(screen.getByLabelText('决策理由'), { target: { value: 'too expensive' } })
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText('续费决策已更新，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getByText('保留 -> 取消')).toBeInTheDocument()
    expect(screen.getByText('too expensive')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        renewal_decision: 'cancel',
        renewal_reason: 'too expensive',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('updates VPS facts and refreshes detail plus timeline', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const updatedRecord = {
      ...detailBody,
      display_name: 'Tokyo Edge 2',
      product_name: 'cx32',
      ipv4: '198.51.100.5',
      ssh_host: 'edge.example.com',
      ssh_port: 2222,
      ssh_user: 'deploy',
      os_name: 'Ubuntu 24.04',
      usage_status: 'standby',
      labels: ['edge', 'backup'],
      note: 'updated',
      updated_at: '2026-05-09T09:00:00Z',
      active_node_link_count: 0,
    }
    const refreshedDetail = {
      ...updatedRecord,
      node_links: [],
    }
    const refreshedTimeline = {
      vps_id: 'vps_001',
      renewal_decisions: [],
      price_histories: [],
      ip_histories: [
        {
          ip_history_id: 'iph_002',
          vps_id: 'vps_001',
          from_ipv4: '192.0.2.1',
          to_ipv4: '198.51.100.5',
          from_ipv6: '',
          to_ipv6: '',
          changed_at: '2026-05-09T09:01:00Z',
          created_at: '2026-05-09T09:01:00Z',
        },
      ],
      spec_snapshots: [
        {
          snapshot_id: 'vss_002',
          vps_id: 'vps_001',
          product_name: 'cx32',
          ssh_host: 'edge.example.com',
          ssh_port: 2222,
          ssh_user: 'deploy',
          os_name: 'Ubuntu 24.04',
          virtualization: 'kvm',
          captured_at: '2026-05-09T09:02:00Z',
          created_at: '2026-05-09T09:02:00Z',
        },
      ],
      experience_logs: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(updatedRecord))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '编辑基础信息' }))
    fireEvent.change(screen.getByLabelText('VPS 名称'), { target: { value: 'Tokyo Edge 2' } })
    fireEvent.change(screen.getByLabelText('产品名'), { target: { value: 'cx32' } })
    fireEvent.change(screen.getByLabelText('IPv4'), { target: { value: '198.51.100.5' } })
    fireEvent.change(screen.getByLabelText('SSH Host'), { target: { value: 'edge.example.com' } })
    fireEvent.change(screen.getByLabelText('SSH 端口'), { target: { value: '2222' } })
    fireEvent.change(screen.getByLabelText('SSH 用户'), { target: { value: 'deploy' } })
    fireEvent.change(screen.getByLabelText('操作系统'), { target: { value: 'Ubuntu 24.04' } })
    fireEvent.change(screen.getByLabelText('用途状态'), { target: { value: 'standby' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'edge, backup' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'updated' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))

    await waitFor(() => expect(screen.getByText('基础信息已更新，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Tokyo Edge 2' })).toBeInTheDocument()
    expect(screen.getByText('192.0.2.1 -> 198.51.100.5')).toBeInTheDocument()
    expect(screen.getByText('deploy@edge.example.com:2222')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        display_name: 'Tokyo Edge 2',
        provider_id: 'pv_001',
        provider_name: 'Hetzner',
        product_name: 'cx32',
        order_ref: 'ord-1',
        country: 'JP',
        region: 'Kanto',
        city: 'Tokyo',
        datacenter: 'nrt',
        ipv4: '198.51.100.5',
        ipv6: '',
        ssh_host: 'edge.example.com',
        ssh_port: 2222,
        ssh_user: 'deploy',
        os_name: 'Ubuntu 24.04',
        virtualization: 'kvm',
        lifecycle_status: 'active',
        usage_status: 'standby',
        importance: 'normal',
        labels: ['edge', 'backup'],
        note: 'updated',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('links and unlinks Node monitoring from a VPS asset', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const linkedDetail = {
      ...detailBody,
      active_node_link_count: 1,
      node_links: [
        {
          node_id: 'nd_002',
          display_name: 'Seoul Node',
          group: 'edge',
          region: 'KR',
          city: 'Seoul',
          provider: 'Node Hint',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          current_health_status: '正常',
          last_heartbeat_at: '2026-05-09T08:10:00Z',
          last_sync_at: '2026-05-09T08:11:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          linked_at: '2026-05-09T09:02:00Z',
          note: 'secondary',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse({
        link_id: 'vpn_001',
        vps_id: 'vps_001',
        node_id: 'nd_002',
        linked_at: '2026-05-09T09:02:00Z',
        unlinked_at: null,
        note: 'secondary',
      }, 201))
      .mockResolvedValueOnce(mockJSONResponse(linkedDetail))
      .mockResolvedValueOnce(mockJSONResponse({
        link_id: 'vpn_001',
        vps_id: 'vps_001',
        node_id: 'nd_002',
        linked_at: '2026-05-09T09:02:00Z',
        unlinked_at: '2026-05-09T09:04:00Z',
        note: 'secondary',
      }))
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('Node ID'), { target: { value: 'nd_002' } })
    fireEvent.change(screen.getByLabelText('关联备注'), { target: { value: 'secondary' } })
    fireEvent.click(screen.getByRole('button', { name: '关联 Node' }))

    await waitFor(() => expect(screen.getByText('Seoul Node')).toBeInTheDocument())
    expect(screen.getByText('Node 关联已更新')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001/link-node', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ node_id: 'nd_002', note: 'secondary' }),
    })

    fireEvent.click(screen.getByRole('button', { name: '解除关联' }))
    await waitFor(() => expect(screen.queryByText('Seoul Node')).not.toBeInTheDocument())
    expect(screen.getByText('Node 关联已解除')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/unlink-node', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ node_id: 'nd_002', note: 'secondary' }),
    })
  })

  it('creates an experience log and refreshes asset history', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const experienceLog = {
      experience_log_id: 'elog_001',
      vps_id: 'vps_001',
      category: 'network',
      severity: 'warning',
      summary: '晚高峰丢包',
      details: '连续三天 tcp probe 抖动',
      occurred_at: '2026-05-10T09:30:00.000Z',
      created_at: '2026-05-10T09:31:00Z',
    }
    const refreshedTimeline = {
      vps_id: 'vps_001',
      renewal_decisions: [],
      price_histories: [],
      ip_histories: [],
      spec_snapshots: [],
      experience_logs: [experienceLog],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(experienceLog, 201))
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('分类'), { target: { value: 'network' } })
    fireEvent.change(screen.getByLabelText('级别'), { target: { value: 'warning' } })
    fireEvent.change(screen.getByLabelText('摘要'), { target: { value: '晚高峰丢包' } })
    fireEvent.change(screen.getByLabelText('发生时间'), { target: { value: '2026-05-10T09:30' } })
    fireEvent.change(screen.getByLabelText('详情'), { target: { value: '连续三天 tcp probe 抖动' } })
    fireEvent.click(screen.getByRole('button', { name: '写入经验记录' }))

    await waitFor(() => expect(screen.getByText('经验记录已写入资产历史')).toBeInTheDocument())
    expect(screen.getAllByText('晚高峰丢包').length).toBeGreaterThan(0)
    expect(screen.getByText('连续三天 tcp probe 抖动')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001/experience-logs', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        category: 'network',
        severity: 'warning',
        summary: '晚高峰丢包',
        details: '连续三天 tcp probe 抖动',
        occurred_at: new Date('2026-05-10T09:30').toISOString(),
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('archives a VPS through the lifecycle card and refreshes detail plus timeline', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const archivedRecord = {
      ...detailBody,
      lifecycle_status: 'archived',
      updated_at: '2026-05-09T10:00:00Z',
      archived_at: '2026-05-09T10:00:00Z',
    }
    const refreshedTimeline = {
      vps_id: 'vps_001',
      renewal_decisions: [],
      price_histories: [],
      ip_histories: [],
      spec_snapshots: [],
      experience_logs: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(archivedRecord))
      .mockResolvedValueOnce(mockJSONResponse({ ...archivedRecord, node_links: [] }))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档 VPS' }))

    expect(screen.getByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    expect(screen.getByText('不会删除 VPS、订阅、Node 关联或资产历史。后续可恢复为闲置。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(screen.getByText('VPS 已归档，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getAllByText('已归档').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: '恢复为闲置' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ lifecycle_status: 'archived' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('keeps the archive confirmation visible and shows a lifecycle-local error when archive fails', async () => {
    const detailBody = {
      vps_id: 'vps_archive_fail',
      display_name: 'Archive Fail Edge',
      provider_id: null,
      provider_name: 'Unknown',
      product_name: 'cx22',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
      ipv4: '192.0.2.2',
      ipv6: '',
      ssh_host: '192.0.2.2',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: 'Debian',
      virtualization: 'kvm',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: [],
      note: '',
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'archive failed' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_archive_fail']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Archive Fail Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档 VPS' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(screen.getByText('archive failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存续费决策' })).toBeDisabled()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_archive_fail', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ lifecycle_status: 'archived' }),
    })
  })

  it('restores an archived VPS to idle through the lifecycle card', async () => {
    const archivedDetail = {
      vps_id: 'vps_archived',
      display_name: 'Archived Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: 'ord-1',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: 'nrt',
      ipv4: '192.0.2.3',
      ipv6: '',
      ssh_host: '192.0.2.3',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: 'Debian',
      virtualization: 'kvm',
      lifecycle_status: 'archived',
      usage_status: 'idle',
      renewal_decision: 'cancel',
      importance: 'normal',
      labels: ['legacy'],
      note: 'archived',
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: '2026-05-09T08:30:00Z',
      node_links: [],
    }
    const restoredRecord = {
      ...archivedDetail,
      lifecycle_status: 'idle',
      updated_at: '2026-05-09T11:00:00Z',
      archived_at: null,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(archivedDetail))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(restoredRecord))
      .mockResolvedValueOnce(mockJSONResponse({ ...restoredRecord, node_links: [] }))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_archived']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Archived Edge' })).toBeInTheDocument())
    expect(screen.getByText(/已归档时间：/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '恢复为闲置' }))

    await waitFor(() => expect(screen.getByText('VPS 已恢复为闲置，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '归档 VPS' })).toBeInTheDocument()
    expect(screen.getAllByText('闲置').length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_archived', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ lifecycle_status: 'idle' }),
    })
  })

  it('creates a service record for the current VPS and refreshes services', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const createdService = {
      ...serviceBody,
      service_type: 'api',
      labels: ['prod', 'public'],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(createdService, 201))
      .mockResolvedValueOnce(mockJSONResponse([createdService]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('服务名称'), { target: { value: 'Blog' } })
    fireEvent.change(screen.getByLabelText('服务类型'), { target: { value: 'api' } })
    fireEvent.change(screen.getByLabelText('入口 URL'), { target: { value: 'https://blog.example.com' } })
    fireEvent.change(screen.getByLabelText('端口'), { target: { value: '443' } })
    fireEvent.change(screen.getAllByLabelText('Target ID')[0], { target: { value: 'tg_001' } })
    fireEvent.change(screen.getByLabelText('服务标签'), { target: { value: 'prod, public' } })
    fireEvent.change(screen.getByLabelText('服务备注'), { target: { value: 'primary service' } })
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))

    await waitFor(() => expect(screen.getByText('服务记录已创建')).toBeInTheDocument())
    expect(screen.getByText('Blog')).toBeInTheDocument()
    expect(screen.getByText('https://blog.example.com')).toBeInTheDocument()
    expect(screen.getByText('端口 443')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001/services', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        target_id: 'tg_001',
        name: 'Blog',
        service_type: 'api',
        status: 'active',
        url: 'https://blog.example.com',
        port: 443,
        labels: ['prod', 'public'],
        note: 'primary service',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows a service-local validation error before creating services', async () => {
    const detailBody = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: null,
      provider_name: '',
      product_name: '',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
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
      labels: [],
      note: '',
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))

    expect(screen.getByText('服务名称不能为空。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('creates a domain record for the current VPS and refreshes domains', async () => {
    const detailBody = {
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
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const createdDomain = {
      ...domainBody,
      domain_name: 'api.example.com',
      purpose: 'api',
      service_id: 'svc_001',
      target_id: 'tg_001',
      registrar: 'NameSilo',
      labels: ['prod', 'public'],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([serviceBody]))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(createdDomain, 201))
      .mockResolvedValueOnce(mockJSONResponse([createdDomain]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('域名'), { target: { value: 'API.Example.COM.' } })
    fireEvent.change(screen.getByLabelText('域名状态'), { target: { value: 'active' } })
    fireEvent.change(screen.getByLabelText('用途'), { target: { value: 'api' } })
    fireEvent.change(screen.getByLabelText('Service ID'), { target: { value: 'svc_001' } })
    fireEvent.change(screen.getAllByLabelText('Target ID')[1], { target: { value: 'tg_001' } })
    fireEvent.change(screen.getByLabelText('注册商'), { target: { value: 'NameSilo' } })
    fireEvent.change(screen.getByLabelText('过期日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(screen.getByLabelText('自动续费'))
    fireEvent.change(screen.getByLabelText('域名标签'), { target: { value: 'prod, public' } })
    fireEvent.change(screen.getByLabelText('域名备注'), { target: { value: 'primary domain' } })
    fireEvent.click(screen.getByRole('button', { name: '创建域名记录' }))

    await waitFor(() => expect(screen.getByText('域名记录已创建')).toBeInTheDocument())
    expect(screen.getByText('api.example.com')).toBeInTheDocument()
    expect(screen.getByText('NameSilo')).toBeInTheDocument()
    expect(screen.getByText('2026-07-01')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001/domains', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        service_id: 'svc_001',
        target_id: 'tg_001',
        domain_name: 'api.example.com',
        purpose: 'api',
        status: 'active',
        registrar: 'NameSilo',
        expires_at: '2026-07-01',
        auto_renew: true,
        https_enabled: true,
        labels: ['prod', 'public'],
        note: 'primary domain',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows a domain-local validation error before creating domains', async () => {
    const detailBody = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: null,
      provider_name: '',
      product_name: '',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
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
      labels: [],
      note: '',
      active_node_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      node_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('域名'), { target: { value: 'https://example.com/path' } })
    fireEvent.click(screen.getByRole('button', { name: '创建域名记录' }))

    expect(screen.getByText('域名必须是不带协议、路径和空格的完整域名。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(4)
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
      experience_logs: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
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
    expect(screen.getByText('暂无经验记录')).toBeInTheDocument()
    expect(screen.getByText('尚未记录服务')).toBeInTheDocument()
    expect(screen.getByText('尚未记录域名')).toBeInTheDocument()
  })
})
