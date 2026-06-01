import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

const subscriptionBody = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  monthly_price: 12,
  started_at: '2026-05-01',
  renew_at: '2026-06-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  status: 'active',
  payment_method: 'card',
  note: 'primary subscription',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const vpsDetailBody = {
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
  renewal_decision: 'cancel',
  importance: 'normal',
  labels: ['edge'],
  note: 'primary',
  active_monitoring_instance_link_count: 1,
  running_monitoring_instance_count: 1,
  running_target_count: 1,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
  monitoring_instance_links: [{
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Monitoring Instance',
    group: 'edge',
    region: 'JP',
    city: 'Tokyo',
    provider: 'Monitoring Hint',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    current_health_status: '正常',
    last_heartbeat_at: '2026-05-09T08:10:00Z',
    last_sync_at: '2026-05-09T08:11:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    linked_at: '2026-05-09T08:00:00Z',
    note: 'primary',
  }],
}

function cancellationPreviewBody(overrides: Record<string, unknown> = {}) {
  return {
    vps: vpsDetailBody,
    subscriptions: [{
      record: subscriptionBody,
      role: 'active',
      recommended_action: 'cancel_auto_renew_and_mark_cancelled',
      message: '订阅仍处于 active，需要显式确认取消订阅自动续费并标记为 cancelled。',
    }],
    monitoring_instance_links: vpsDetailBody.monitoring_instance_links,
    services: [serviceBody],
    domains: [domainBody],
    target_links: [{
      target_id: 'tg_001',
      name: 'Blog Target',
      run_status: '启用',
      service_ids: ['svc_001'],
      domain_ids: ['dom_001'],
      last_linked_at: '2026-05-10T08:00:00Z',
    }],
    recommended_steps: [{
      object_type: 'vps',
      object_id: 'vps_001',
      step_type: 'vps_lifecycle',
      from_state: 'active/cancel',
      to_state: 'cancelled/cancel',
      required: true,
      message: '将 VPS 续费决策设为 cancel，并根据订阅到期情况设置生命周期。',
    }],
    warnings: ['仍有 1 个关联监控实例 未标记不续费或已退役。'],
    blockers: [],
    ...overrides,
  }
}

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

const targetBody = {
  target_id: 'tg_001',
  name: 'Blog Target',
  target_type: 'service',
  host: 'blog.example.com',
  base_port: 443,
  execution_monitoring_instance_labels: ['edge'],
  run_status: '启用',
  group: 'edge',
  labels: ['prod'],
  note: '',
  current_health_status: '正常',
  current_active_incident_count: 0,
  current_primary_issue_summary: '',
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

function openVPSActionsMenu() {
  const summary = screen.getByLabelText('VPS 详情操作')
  const menu = summary.closest('details')
  if (!menu?.hasAttribute('open')) {
    fireEvent.click(summary)
  }
}

function clickVPSAction(name: string) {
  openVPSActionsMenu()
  fireEvent.click(screen.getByRole('button', { name }))
}

describe('VPSDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders VPS facts and linked monitoring instance summaries', async () => {
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
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [
        {
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Monitoring Instance',
          group: 'edge',
          region: 'JP',
          city: 'Tokyo',
          provider: 'Monitoring Hint',
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
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
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
    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?vps_id=vps_001&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(screen.getByRole('button', { name: '处理决策' })).toBeInTheDocument()
    expect(screen.getByLabelText('VPS 详情操作')).toBeInTheDocument()
    openVPSActionsMenu()
    expect(screen.getByRole('button', { name: '编辑基础信息' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '记录经验' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '关联监控实例' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新增服务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新增域名' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '归档 VPS' })).toBeInTheDocument()
    expect(screen.getByText('资产判断')).toBeInTheDocument()
    expect(screen.getByText('下一步动作')).toBeInTheDocument()
    expect(screen.getByText('先核对运行异常')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看监控实例' })).toHaveAttribute('href', '/monitoring/mi_001')
    expect(screen.getByLabelText('资产判断证据状态')).toBeInTheDocument()
    expect(screen.getByText('续费与成本')).toBeInTheDocument()
    expect(screen.getAllByText('USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/续费日 2026-06-01/).length).toBeGreaterThan(0)
    expect(screen.getByText('续费与成本')).toBeInTheDocument()
    expect(screen.getByText('USD 12.00')).toBeInTheDocument()
    expect(screen.getByText(/续费日 2026-06-01/)).toBeInTheDocument()
    expect(screen.getAllByText('监控实例证据').length).toBeGreaterThan(0)
    expect(screen.getByText('服务与域名')).toBeInTheDocument()
    expect(screen.getByText('最近历史')).toBeInTheDocument()
    expect(screen.getByText('资料摘要')).toBeInTheDocument()
    expect(screen.getByText(/1 服务 · 1 域名/)).toBeInTheDocument()
    expect(screen.getAllByText('Tokyo Monitoring Instance').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/latency high/).length).toBeGreaterThan(0)
    expect(screen.getByText(/晚高峰丢包/)).toBeInTheDocument()
    expect(screen.getByText('Blog；www.example.com')).toBeInTheDocument()
    expect(screen.getByText(/cx22 · nrt/)).toBeInTheDocument()
    expect(screen.getAllByText('192.0.2.1').length).toBeGreaterThan(0)
    expect(screen.queryByRole('heading', { name: '续费与成本证据' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '决策依据与经验记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '监控实例证据' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '基础信息' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '资产历史' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '服务资产' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '域名资产' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '访问摘要' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看监控实例详情' }))
    const nodeDrawer = screen.getByRole('dialog', { name: '监控实例证据' })
    expect(within(nodeDrawer).getAllByRole('heading', { name: '监控实例证据' }).length).toBeGreaterThan(0)
    expect(within(nodeDrawer).getByText(/关联监控实例用于解释续费决策/)).toBeInTheDocument()
    expect(within(nodeDrawer).getAllByText('Tokyo Monitoring Instance').length).toBeGreaterThan(0)
    fireEvent.click(within(nodeDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '服务详情' }))
    const servicesDrawer = screen.getByRole('dialog', { name: '服务资产详情' })
    expect(within(servicesDrawer).getByRole('heading', { name: '服务资产' })).toBeInTheDocument()
    expect(within(servicesDrawer).getByText('https://blog.example.com')).toBeInTheDocument()
    expect(within(servicesDrawer).getByText('tg_001')).toBeInTheDocument()
    fireEvent.click(within(servicesDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '域名详情' }))
    const domainsDrawer = screen.getByRole('dialog', { name: '域名资产详情' })
    expect(within(domainsDrawer).getByRole('heading', { name: '域名资产' })).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('www.example.com')).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('NameSilo')).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('2026-07-01')).toBeInTheDocument()
    fireEvent.click(within(domainsDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '查看资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史详情' })
    expect(within(timelineDrawer).getByRole('heading', { name: '资产历史' })).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('未评估 -> 保留')).toBeInTheDocument()
    expect(within(timelineDrawer).getAllByText('稳定承载边缘流量').length).toBeGreaterThan(0)
    expect(within(timelineDrawer).getAllByText('USD 10.00 -> USD 12.00').length).toBeGreaterThan(0)
    expect(within(timelineDrawer).getByText('192.0.2.10 -> 192.0.2.1')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('root@192.0.2.1:22')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('已向服务商提交工单')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '查看基础资料' }))
    const factsDrawer = screen.getByRole('dialog', { name: '基础资料详情' })
    expect(within(factsDrawer).getByRole('heading', { name: '基础信息' })).toBeInTheDocument()
    expect(within(factsDrawer).getByText('vps_001')).toBeInTheDocument()
  })

  it('does not treat subscription load failures as missing subscription facts', async () => {
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
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [
        {
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Monitoring Instance',
          group: 'edge',
          region: 'JP',
          city: 'Tokyo',
          provider: 'Monitoring Hint',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          current_health_status: '正常',
          last_heartbeat_at: '2026-05-09T08:10:00Z',
          last_sync_at: '2026-05-09T08:11:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          linked_at: '2026-05-09T08:00:00Z',
          note: 'primary',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscription backend down' }, 500))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(screen.getAllByText('订阅读取失败').length).toBeGreaterThan(0)
    expect(screen.getByText('先恢复订阅证据')).toBeInTheDocument()
    expect(screen.getByText('读取失败')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '核对订阅' })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001')
    expect(screen.getAllByText('subscription backend down').length).toBeGreaterThan(0)
    expect(screen.getAllByText('订阅未知').length).toBeGreaterThan(0)
    expect(screen.queryByText('缺订阅')).not.toBeInTheDocument()
  })

  it('treats an empty successful subscription response as a true missing-subscription issue', async () => {
    const responseBody = {
      vps_id: 'vps_missing_subscription',
      display_name: 'Missing Subscription Edge',
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
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [
        {
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Monitoring Instance',
          group: 'edge',
          region: 'JP',
          city: 'Tokyo',
          provider: 'Monitoring Hint',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          current_health_status: '正常',
          last_heartbeat_at: '2026-05-09T08:10:00Z',
          last_sync_at: '2026-05-09T08:11:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          linked_at: '2026-05-09T08:00:00Z',
          note: 'primary',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_missing_subscription']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Missing Subscription Edge' })).toBeInTheDocument())
    expect(screen.getByText('补录续费成本')).toBeInTheDocument()
    expect(screen.getAllByText('缺订阅').length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: '补订阅' })).toHaveAttribute('href', '/subscriptions?vps_id=vps_missing_subscription&create=1')
    expect(screen.queryByText('订阅读取失败')).not.toBeInTheDocument()
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const updatedRecord = {
      ...detailBody,
      renewal_decision: 'cancel',
      updated_at: '2026-05-09T09:00:00Z',
      active_monitoring_instance_link_count: 0,
    }
    const refreshedDetail = {
      ...updatedRecord,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(updatedRecord))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '调整决策' }))
    const decisionDrawer = screen.getByRole('dialog', { name: '续费决策' })
    fireEvent.change(within(decisionDrawer).getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(within(decisionDrawer).getByLabelText('决策理由'), { target: { value: 'too expensive' } })
    fireEvent.click(within(decisionDrawer).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText('续费决策已更新，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getAllByText('too expensive').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: '查看资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史详情' })
    expect(within(timelineDrawer).getByText('保留 -> 取消')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
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
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('lets users cancel routine VPS detail drawers without submitting', async () => {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '调整决策' }))
    let decisionDialog = screen.getByRole('dialog', { name: '续费决策' })
    fireEvent.change(within(decisionDialog).getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(within(decisionDialog).getByLabelText('决策理由'), { target: { value: 'stale decision' } })
    fireEvent.click(within(decisionDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '续费决策' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '调整决策' }))
    decisionDialog = screen.getByRole('dialog', { name: '续费决策' })
    expect(within(decisionDialog).getByLabelText('续费决策')).toHaveValue('keep')
    expect(within(decisionDialog).getByLabelText('决策理由')).toHaveValue('')
    fireEvent.click(within(decisionDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('编辑基础信息')
    let factsDialog = screen.getByRole('dialog', { name: '编辑基础信息' })
    fireEvent.change(within(factsDialog).getByLabelText('VPS 名称'), { target: { value: 'Stale VPS' } })
    fireEvent.click(within(factsDialog).getByRole('button', { name: '取消编辑' }))
    expect(screen.queryByRole('dialog', { name: '编辑基础信息' })).not.toBeInTheDocument()

    clickVPSAction('编辑基础信息')
    factsDialog = screen.getByRole('dialog', { name: '编辑基础信息' })
    expect(within(factsDialog).getByLabelText('VPS 名称')).toHaveValue('Tokyo Edge')
    fireEvent.click(within(factsDialog).getByRole('button', { name: '取消编辑' }))

    clickVPSAction('关联监控实例')
    let nodeDialog = screen.getByRole('dialog', { name: '关联监控实例' })
    expect(within(nodeDialog).getByLabelText('选择监控实例')).toBeDisabled()
    fireEvent.change(within(nodeDialog).getByLabelText('关联备注'), { target: { value: 'stale note' } })
    fireEvent.click(within(nodeDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '关联监控实例' })).not.toBeInTheDocument()

    clickVPSAction('关联监控实例')
    nodeDialog = screen.getByRole('dialog', { name: '关联监控实例' })
    expect(within(nodeDialog).getByLabelText('选择监控实例')).toHaveValue('')
    expect(within(nodeDialog).getByLabelText('关联备注')).toHaveValue('')
    fireEvent.click(within(nodeDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('记录经验')
    let experienceDialog = screen.getByRole('dialog', { name: '经验记录' })
    fireEvent.change(within(experienceDialog).getByLabelText('摘要'), { target: { value: 'stale experience' } })
    fireEvent.click(within(experienceDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '经验记录' })).not.toBeInTheDocument()

    clickVPSAction('记录经验')
    experienceDialog = screen.getByRole('dialog', { name: '经验记录' })
    expect(within(experienceDialog).getByLabelText('摘要')).toHaveValue('')
    fireEvent.click(within(experienceDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('新增服务')
    let serviceDialog = screen.getByRole('dialog', { name: '新增服务' })
    fireEvent.change(within(serviceDialog).getByLabelText('服务名称'), { target: { value: 'stale service' } })
    fireEvent.click(within(serviceDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '新增服务' })).not.toBeInTheDocument()

    clickVPSAction('新增服务')
    serviceDialog = screen.getByRole('dialog', { name: '新增服务' })
    expect(within(serviceDialog).getByLabelText('服务名称')).toHaveValue('')
    fireEvent.click(within(serviceDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('新增域名')
    let domainDialog = screen.getByRole('dialog', { name: '新增域名' })
    fireEvent.change(within(domainDialog).getByLabelText('域名'), { target: { value: 'stale.example.com' } })
    fireEvent.click(within(domainDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '新增域名' })).not.toBeInTheDocument()

    clickVPSAction('新增域名')
    domainDialog = screen.getByRole('dialog', { name: '新增域名' })
    expect(within(domainDialog).getByLabelText('域名')).toHaveValue('')
    fireEvent.click(within(domainDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(8))
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      active_monitoring_instance_link_count: 0,
    }
    const refreshedDetail = {
      ...updatedRecord,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(updatedRecord))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('编辑基础信息')
    const factsDrawer = screen.getByRole('dialog', { name: '编辑基础信息' })
    fireEvent.change(within(factsDrawer).getByLabelText('VPS 名称'), { target: { value: 'Tokyo Edge 2' } })
    fireEvent.change(within(factsDrawer).getByLabelText('产品名'), { target: { value: 'cx32' } })
    fireEvent.change(within(factsDrawer).getByLabelText('IPv4'), { target: { value: '198.51.100.5' } })
    fireEvent.change(within(factsDrawer).getByLabelText('SSH Host'), { target: { value: 'edge.example.com' } })
    fireEvent.change(within(factsDrawer).getByLabelText('SSH 端口'), { target: { value: '2222' } })
    fireEvent.change(within(factsDrawer).getByLabelText('SSH 用户'), { target: { value: 'deploy' } })
    fireEvent.change(within(factsDrawer).getByLabelText('操作系统'), { target: { value: 'Ubuntu 24.04' } })
    fireEvent.change(within(factsDrawer).getByLabelText('用途状态'), { target: { value: 'standby' } })
    fireEvent.change(within(factsDrawer).getByLabelText('标签'), { target: { value: 'edge, backup' } })
    fireEvent.change(within(factsDrawer).getByLabelText('备注'), { target: { value: 'updated' } })
    fireEvent.click(within(factsDrawer).getByRole('button', { name: '保存基础信息' }))

    await waitFor(() => expect(screen.getByText('基础信息已更新，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Tokyo Edge 2' })).toBeInTheDocument()
    expect(screen.getAllByText('edge.example.com').length).toBeGreaterThan(0)
    expect(screen.getAllByText('2222').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: '查看资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史详情' })
    expect(within(timelineDrawer).getByText('192.0.2.1 -> 198.51.100.5')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('deploy@edge.example.com:2222')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/providers', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001', {
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
        usage_status: 'standby',
        importance: 'normal',
        labels: ['edge', 'backup'],
        note: 'updated',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(11, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('links and unlinks monitoring instance evidence from a VPS asset', async () => {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const linkedDetail = {
      ...detailBody,
      active_monitoring_instance_link_count: 1,
      monitoring_instance_links: [
        {
          monitoring_instance_id: 'mi_002',
          display_name: 'Seoul Monitoring Instance',
          group: 'edge',
          region: 'KR',
          city: 'Seoul',
          provider: 'Monitoring Hint',
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
    const nodeOption = {
      monitoring_instance_id: 'mi_002',
      display_name: 'Seoul Monitoring Instance',
      group: 'edge',
      region: 'KR',
      city: 'Seoul',
      provider: 'Monitoring Hint',
      lifecycle_status: '在用',
      monitoring_status: '启用',
      binding_status: '已绑定',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([nodeOption]))
      .mockResolvedValueOnce(mockJSONResponse({
        link_id: 'vpn_001',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mi_002',
        linked_at: '2026-05-09T09:02:00Z',
        unlinked_at: null,
        note: 'secondary',
      }, 201))
      .mockResolvedValueOnce(mockJSONResponse(linkedDetail))
      .mockResolvedValueOnce(mockJSONResponse({
        link_id: 'vpn_001',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mi_002',
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

    clickVPSAction('关联监控实例')
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    }))
    fireEvent.change(within(screen.getByRole('dialog', { name: '关联监控实例' })).getByLabelText('选择监控实例'), { target: { value: 'mi_002' } })
    fireEvent.change(within(screen.getByRole('dialog', { name: '关联监控实例' })).getByLabelText('关联备注'), { target: { value: 'secondary' } })
    fireEvent.click(within(screen.getByRole('dialog', { name: '关联监控实例' })).getByRole('button', { name: '关联监控实例' }))

    await waitFor(() => expect(screen.getAllByText('Seoul Monitoring Instance').length).toBeGreaterThan(0))
    expect(screen.getByText('监控实例关联已更新')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/link-monitoring-instance', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ monitoring_instance_id: 'mi_002', note: 'secondary' }),
    })

    fireEvent.click(screen.getByRole('button', { name: '查看监控实例详情' }))
    const nodeEvidenceDrawer = screen.getByRole('dialog', { name: '监控实例证据' })
    fireEvent.click(within(nodeEvidenceDrawer).getByRole('button', { name: '解除关联' }))
    await waitFor(() => expect(screen.queryByText('Seoul Monitoring Instance')).not.toBeInTheDocument())
    expect(screen.getByText('监控实例关联已解除')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/unlink-monitoring-instance', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ monitoring_instance_id: 'mi_002', note: 'secondary' }),
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(experienceLog, 201))
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('记录经验')
    const experienceDrawer = screen.getByRole('dialog', { name: '经验记录' })
    fireEvent.change(within(experienceDrawer).getByLabelText('分类'), { target: { value: 'network' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('级别'), { target: { value: 'warning' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('摘要'), { target: { value: '晚高峰丢包' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('发生时间'), { target: { value: '2026-05-10T09:30' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('详情'), { target: { value: '连续三天 tcp probe 抖动' } })
    fireEvent.click(within(experienceDrawer).getByRole('button', { name: '写入经验记录' }))

    await waitFor(() => expect(screen.getAllByText('经验记录已写入资产历史').length).toBeGreaterThan(0))
    expect(screen.getByText(/晚高峰丢包/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '查看资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史详情' })
    expect(within(timelineDrawer).getByText('连续三天 tcp probe 抖动')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/experience-logs', {
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
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/vps/vps_001/domains', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(archivedRecord))
      .mockResolvedValueOnce(mockJSONResponse({ ...archivedRecord, monitoring_instance_links: [] }))
      .mockResolvedValueOnce(mockJSONResponse(refreshedTimeline))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('归档 VPS')

    expect(screen.getByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    expect(screen.getByText('不会删除 VPS、订阅、监控实例关联或资产历史。后续可恢复为闲置。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(screen.getByText('VPS 已归档，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getAllByText('已归档').length).toBeGreaterThan(0)
    openVPSActionsMenu()
    expect(screen.getByRole('button', { name: '恢复为闲置' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ lifecycle_status: 'archived' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/vps/vps_001/domains', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
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

    clickVPSAction('归档 VPS')
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(screen.getByText('archive failed')).toBeInTheDocument())
    expect(screen.getByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_archive_fail', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: '2026-05-09T08:30:00Z',
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(restoredRecord))
      .mockResolvedValueOnce(mockJSONResponse({ ...restoredRecord, monitoring_instance_links: [] }))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_archived']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Archived Edge' })).toBeInTheDocument())
    expect(screen.getAllByText('已归档').length).toBeGreaterThan(0)
    expect(screen.queryByText(/已归档时间：/)).not.toBeInTheDocument()

    clickVPSAction('恢复为闲置')

    expect(screen.getByRole('alertdialog', { name: '确认恢复 VPS' })).toBeInTheDocument()
    expect(screen.getByText('不会删除或重建 VPS、订阅、监控实例关联或资产历史。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认恢复' }))

    await waitFor(() => expect(screen.getByText('VPS 已恢复为闲置，资产历史已刷新')).toBeInTheDocument())
    openVPSActionsMenu()
    expect(screen.getByRole('button', { name: '归档 VPS' })).toBeInTheDocument()
    expect(screen.getAllByText('闲置').length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_archived', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([targetBody]))
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

    clickVPSAction('新增服务')
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    }))
    const serviceDrawer = screen.getByRole('dialog', { name: '新增服务' })
    fireEvent.change(within(serviceDrawer).getByLabelText('服务名称'), { target: { value: 'Blog' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('服务类型'), { target: { value: 'api' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('入口 URL'), { target: { value: 'https://blog.example.com' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('端口'), { target: { value: '443' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('关联 Target'), { target: { value: 'tg_001' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('服务标签'), { target: { value: 'prod, public' } })
    fireEvent.change(within(serviceDrawer).getByLabelText('服务备注'), { target: { value: 'primary service' } })
    fireEvent.click(within(serviceDrawer).getByRole('button', { name: '创建服务记录' }))

    await waitFor(() => expect(screen.getByText('服务记录已创建')).toBeInTheDocument())
    expect(screen.getByText('Blog；域名上下文待补录')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '服务详情' }))
    const serviceDetailDrawer = screen.getByRole('dialog', { name: '服务资产详情' })
    expect(within(serviceDetailDrawer).getByText('Blog')).toBeInTheDocument()
    expect(within(serviceDetailDrawer).getByText('https://blog.example.com')).toBeInTheDocument()
    expect(within(serviceDetailDrawer).getByText('端口 443')).toBeInTheDocument()
    fireEvent.click(within(serviceDetailDrawer).getByLabelText('关闭'))
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/services', {
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
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/services', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('新增服务')
    const invalidServiceDrawer = screen.getByRole('dialog', { name: '新增服务' })
    fireEvent.click(within(invalidServiceDrawer).getByRole('button', { name: '创建服务记录' }))

    expect(screen.getAllByText('服务名称不能为空。').length).toBeGreaterThan(0)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([targetBody]))
      .mockResolvedValueOnce(mockJSONResponse(createdDomain, 201))
      .mockResolvedValueOnce(mockJSONResponse([createdDomain]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('新增域名')
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    }))
    const domainDrawer = screen.getByRole('dialog', { name: '新增域名' })
    fireEvent.change(within(domainDrawer).getByLabelText('域名'), { target: { value: 'API.Example.COM.' } })
    fireEvent.change(within(domainDrawer).getByLabelText('域名状态'), { target: { value: 'active' } })
    fireEvent.change(within(domainDrawer).getByLabelText('用途'), { target: { value: 'api' } })
    fireEvent.change(within(domainDrawer).getByLabelText('关联服务'), { target: { value: 'svc_001' } })
    fireEvent.change(within(domainDrawer).getByLabelText('关联 Target'), { target: { value: 'tg_001' } })
    fireEvent.change(within(domainDrawer).getByLabelText('注册商'), { target: { value: 'NameSilo' } })
    fireEvent.change(within(domainDrawer).getByLabelText('过期日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(domainDrawer).getByLabelText('自动续费'))
    fireEvent.change(within(domainDrawer).getByLabelText('域名标签'), { target: { value: 'prod, public' } })
    fireEvent.change(within(domainDrawer).getByLabelText('域名备注'), { target: { value: 'primary domain' } })
    fireEvent.click(within(domainDrawer).getByRole('button', { name: '创建域名记录' }))

    await waitFor(() => expect(screen.getByText('域名记录已创建')).toBeInTheDocument())
    expect(screen.getByText('Blog；api.example.com')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '域名详情' }))
    const domainDetailDrawer = screen.getByRole('dialog', { name: '域名资产详情' })
    expect(within(domainDetailDrawer).getByText('api.example.com')).toBeInTheDocument()
    expect(within(domainDetailDrawer).getByText('NameSilo')).toBeInTheDocument()
    expect(within(domainDetailDrawer).getByText('2026-07-01')).toBeInTheDocument()
    fireEvent.click(within(domainDetailDrawer).getByLabelText('关闭'))
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/domains', {
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
    expect(fetchMock).toHaveBeenNthCalledWith(8, '/api/vps/vps_001/domains', {
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('新增域名')
    const invalidDomainDrawer = screen.getByRole('dialog', { name: '新增域名' })
    fireEvent.change(within(invalidDomainDrawer).getByLabelText('域名'), { target: { value: 'https://example.com/path' } })
    fireEvent.click(within(invalidDomainDrawer).getByRole('button', { name: '创建域名记录' }))

    expect(screen.getAllByText('域名必须是不带协议、路径和空格的完整域名。').length).toBeGreaterThan(0)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))
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
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
      monitoring_instance_links: [],
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
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_empty']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Empty Edge' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '资产判断' })).toBeInTheDocument()
    expect(screen.getByText('服务上下文待补录；域名上下文待补录')).toBeInTheDocument()
    expect(screen.queryByText('暂无续费决策历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无价格变化历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无 IP 变化历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无规格快照')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无经验记录')).not.toBeInTheDocument()
    expect(screen.queryByText('尚未记录服务')).not.toBeInTheDocument()
    expect(screen.queryByText('尚未记录域名')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史详情' })
    expect(within(timelineDrawer).getByText('暂无续费决策历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无价格变化历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无 IP 变化历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无规格快照')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无经验记录')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '服务详情' }))
    const servicesDrawer = screen.getByRole('dialog', { name: '服务资产详情' })
    expect(within(servicesDrawer).getByText('尚未记录服务')).toBeInTheDocument()
    fireEvent.click(within(servicesDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '域名详情' }))
    const domainsDrawer = screen.getByRole('dialog', { name: '域名资产详情' })
    expect(within(domainsDrawer).getByText('尚未记录域名')).toBeInTheDocument()
  })

  it('refreshes cancellation preview after applying lifecycle actions', async () => {
    const refreshedDetail = {
      ...vpsDetailBody,
      lifecycle_status: 'cancelled',
      running_monitoring_instance_count: 0,
      running_target_count: 0,
      monitoring_instance_links: [{
        ...vpsDetailBody.monitoring_instance_links[0],
        lifecycle_status: '已退役',
        monitoring_status: '暂停',
      }],
    }
    const applyResult = {
      action: {
        action_id: 'alca_001',
        vps_id: 'vps_001',
        action_type: 'cancel_vps',
        status: 'completed',
        reason: '已过期且不准备续费',
        effective_date: '2026-05-30',
        created_at: '2026-05-30T08:00:00Z',
        confirmed_at: '2026-05-30T08:00:00Z',
        completed_at: '2026-05-30T08:00:00Z',
        summary: {},
      },
      steps: [{
        step_id: 'alcs_001',
        action_id: 'alca_001',
        object_type: 'vps',
        object_id: 'vps_001',
        step_type: 'vps_lifecycle',
        status: 'completed',
        before_state: { lifecycle_status: 'active' },
        after_state: { lifecycle_status: 'cancelled' },
        message: 'VPS 生命周期已确认。',
        executed_at: '2026-05-30T08:00:00Z',
        created_at: '2026-05-30T08:00:00Z',
      }],
    }
    const refreshedPreview = cancellationPreviewBody({
      vps: refreshedDetail,
      subscriptions: [{
        record: {
          ...subscriptionBody,
          auto_renew: false,
          auto_renew_cancelled: true,
          status: 'cancelled',
        },
        role: 'inactive',
        recommended_action: 'keep_inactive',
        message: '订阅已处于非活跃状态，仍需处理 VPS、监控实例与入口探测状态。',
      }],
      monitoring_instance_links: refreshedDetail.monitoring_instance_links,
      target_links: [{
        target_id: 'tg_001',
        name: 'Blog Target',
        run_status: '已归档',
        service_ids: ['svc_001'],
        domain_ids: ['dom_001'],
        last_linked_at: '2026-05-10T08:00:00Z',
      }],
      warnings: [],
      recommended_steps: [],
    })
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([serviceBody]))
      .mockResolvedValueOnce(mockJSONResponse([domainBody]))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(cancellationPreviewBody()))
      .mockResolvedValueOnce(mockJSONResponse(applyResult))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([serviceBody]))
      .mockResolvedValueOnce(mockJSONResponse([domainBody]))
      .mockResolvedValueOnce(mockJSONResponse([{ ...subscriptionBody, status: 'cancelled' }]))
      .mockResolvedValueOnce(mockJSONResponse(refreshedPreview))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?workbench=cancellation']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const workbench = await screen.findByRole('dialog', { name: '取消/退役工作台' })
    fireEvent.change(within(workbench).getByLabelText('原因'), {
      target: { value: '已过期且不准备续费' },
    })
    fireEvent.click(within(within(workbench).getByText('sub_001').closest('.asset-cancel-workbench__row')!).getByRole('checkbox'))
    fireEvent.click(within(within(workbench).getByText('Tokyo Monitoring Instance').closest('.asset-checkbox-line')!).getByRole('checkbox'))
    fireEvent.click(within(within(workbench).getByText('Blog Target').closest('.asset-checkbox-line')!).getByRole('checkbox'))
    fireEvent.click(within(workbench).getByRole('button', { name: '确认取消/退役' }))

    await waitFor(() => expect(screen.getByText('取消/退役动作已完成，写入 1 个审计步骤')).toBeInTheDocument())
    expect(screen.getByText('已完成生命周期动作 alca_001，写入 1 个步骤。')).toBeInTheDocument()
    expect(screen.getByText('active 0 · 非活跃 1')).toBeInTheDocument()
    expect(screen.queryByText('仍有 1 个关联监控实例 未标记不续费或已退役。')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(13, '/api/vps/vps_001/cancellation-preview', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })
})
