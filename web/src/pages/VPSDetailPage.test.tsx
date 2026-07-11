import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSDetailPage } from './VPSDetailPage'

function firstResult<T>(items: readonly T[], description: string): T {
  const item = items[0]
  if (!item) throw new Error(`${description} must expose a first result`)
  return item
}

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

const ipQualitySummaryBody = {
  vps_id: 'vps_001',
  observed_at: '2026-06-08T12:00:00Z',
  ip_address: '192.0.2.1',
  ip_version: 4,
  status: 'success',
  risk_level: 'high',
  use_region_code: 'JP',
  use_region_name: 'Japan',
  asn: 'AS64500',
  organization: 'Example Transit',
  stale: false,
  ambiguous: false,
  assignment_mode: 'link',
  provider_count: 2,
  unlockable_count: 1,
}

const ipQualityReportBody = {
  summary: ipQualitySummaryBody,
  latest_report: {
    report_id: 'ipq_001',
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-06-08T12:00:00Z',
    received_at: '2026-06-08T12:00:05Z',
    agent_version: 'dev',
    fingerprint: 'fp-001',
    sync_batch_id: 'sync_001',
    ip_address: '192.0.2.1',
    ip_version: 4,
    status: 'success',
    asn: 'AS64500',
    organization: 'Example Transit',
    latitude: 35.68,
    longitude: 139.76,
    use_region_code: 'JP',
    use_region_name: 'Japan',
    registered_region_code: 'US',
    registered_region_name: 'United States',
    risk_level: 'high',
    is_backfilled: false,
    created_at: '2026-06-08T12:00:06Z',
  },
  provider_results: [
    {
      provider: 'ipinfo',
      usage_type: 'hosting',
      company_type: 'business',
      risk_level: 'high',
      risk_score: '80',
      region_code: 'JP',
      region_name: 'Japan',
      is_proxy: false,
      is_tor: false,
      is_vpn: true,
      is_server: true,
      is_abuser: false,
      is_robot: false,
    },
  ],
  service_unlocks: [
    { service: 'chatgpt', status: 'unlocked', region: 'JP', unlock_type: 'native' },
    { service: 'netflix', status: 'blocked', region: 'US', unlock_type: 'none' },
  ],
  history: [ipQualitySummaryBody],
}

const subscriptionBody = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 12,
  started_at: '2026-05-01',
  renew_at: '2026-06-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  renewal_mode: 'auto',
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
      message: '订阅账单记录仍显示自动续费有效，需要显式确认取消自动续费。',
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
      message: '将 VPS 调整决策设为 cancel，并根据订阅到期情况设置生命周期。',
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
  const summary = screen.getByLabelText('VPS 详情操作')
  const menu = summary.closest('details')
  if (!menu) throw new Error('VPS actions menu not found')
  fireEvent.click(within(menu).getByRole('button', { name }))
}

function LocationProbe() {
  const location = useLocation()
  return (
    <>
      <div data-testid="location-path">{location.pathname}</div>
      <div data-testid="location-search">{location.search}</div>
    </>
  )
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
    expect(screen.getByRole('region', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '关联概览' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '单机台账' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'IP 质量概况' })).toBeInTheDocument()
    const relatedOverviewLayout = screen.getByRole('region', { name: '关联概览' })
    expect(within(relatedOverviewLayout).getByRole('list')).toHaveClass('vps-related-overview__list')
    expect(within(relatedOverviewLayout).queryAllByRole('article')).toHaveLength(0)
    const singleLedgerLayout = screen.getByRole('region', { name: '单机台账' })
    expect(within(singleLedgerLayout).queryAllByRole('article')).toHaveLength(0)
    expect(within(singleLedgerLayout).getByRole('list', { name: '近期记录' })).toBeInTheDocument()
    expect(within(singleLedgerLayout).getByRole('list', { name: '承载清单' })).toBeInTheDocument()
    expect(within(singleLedgerLayout).queryByRole('button', { name: '资产历史' })).not.toBeInTheDocument()
    expect(within(singleLedgerLayout).queryByRole('button', { name: '服务' })).not.toBeInTheDocument()
    expect(within(singleLedgerLayout).queryByRole('button', { name: '域名' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('VPS 综合基础信息')).toBeInTheDocument()
    const currentJudgement = screen.getByLabelText('当前判断')
    expect(within(currentJudgement).getByText('运行观测需要核对')).toBeInTheDocument()
    expect(within(currentJudgement).getByText('Tokyo Monitoring Instance · 1 个活跃异常')).toBeInTheDocument()
    expect(within(currentJudgement).getByRole('link', { name: '查看监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(within(currentJudgement).getByRole('button', { name: '监控观测' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '需要处理的状态' })).not.toBeInTheDocument()
    const overviewActions = screen.getByRole('region', { name: 'Tokyo Edge' }).querySelector('.vps-detail-overview__actions')
    expect(overviewActions).toBeInstanceOf(HTMLElement)
    expect(within(overviewActions as HTMLElement).getByRole('button', { name: '调整决策' })).toBeInTheDocument()
    expect(within(overviewActions as HTMLElement).getByRole('button', { name: '基础资料' })).toBeInTheDocument()
    const overviewActionNames = within(overviewActions as HTMLElement).getAllByRole('button').map((button) => button.textContent)
    expect(overviewActionNames.slice(0, 4)).toEqual(['资产历史', '服务', '域名', '调整决策'])
    expect(screen.getByLabelText('VPS 详情操作')).toBeInTheDocument()
    openVPSActionsMenu()
    expect(screen.getByRole('button', { name: '编辑基础资料' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '记录经验' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '创建/更新订阅' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '接入/升级 agent' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: '关联已有监控实例' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '新增服务' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '新增域名' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: '归档 VPS' })).toBeInTheDocument()
    expect(screen.getByText('JP · Kanto · Tokyo · nrt')).toBeInTheDocument()
    expect(screen.getAllByText('cx22').length).toBeGreaterThan(0)
    expect(screen.getAllByText('192.0.2.1:22').length).toBeGreaterThan(0)
    expect(screen.getAllByText('USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getAllByText('监控观测').length).toBeGreaterThan(0)
    expect(screen.getAllByText('1 个实例 · 关注').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/晚高峰丢包/).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Blog').length).toBeGreaterThan(0)
    expect(screen.getAllByText('www.example.com').length).toBeGreaterThan(0)
    expect(screen.queryByText('资产判断')).not.toBeInTheDocument()
    expect(screen.queryByText('下一步动作')).not.toBeInTheDocument()
    expect(screen.queryByText('成本卡片')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '续费与成本证据' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '决策依据与记录经验' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '监控观测' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '基础信息' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '资产历史' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '服务资产' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '域名资产' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '访问摘要' })).not.toBeInTheDocument()

    fireEvent.click(within(overviewActions as HTMLElement).getByRole('button', { name: '资产历史' }))
    const topTimelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
    expect(within(topTimelineDrawer).getAllByRole('heading', { name: '资产历史' }).length).toBeGreaterThan(0)
    fireEvent.click(within(topTimelineDrawer).getByLabelText('关闭'))

    fireEvent.click(within(overviewActions as HTMLElement).getByRole('button', { name: '服务' }))
    const topServicesDrawer = screen.getByRole('dialog', { name: '服务详情' })
    expect(within(topServicesDrawer).getByRole('heading', { name: '服务资产' })).toBeInTheDocument()
    fireEvent.click(within(topServicesDrawer).getByLabelText('关闭'))

    fireEvent.click(within(overviewActions as HTMLElement).getByRole('button', { name: '域名' }))
    const topDomainsDrawer = screen.getByRole('dialog', { name: '域名详情' })
    expect(within(topDomainsDrawer).getByRole('heading', { name: '域名资产' })).toBeInTheDocument()
    fireEvent.click(within(topDomainsDrawer).getByLabelText('关闭'))

    fireEvent.click(within(currentJudgement).getByRole('button', { name: '监控观测' }))
    const nodeDrawer = screen.getByRole('dialog', { name: '监控观测' })
    expect(within(nodeDrawer).getAllByRole('heading', { name: '监控观测' }).length).toBeGreaterThan(0)
    expect(within(nodeDrawer).getAllByText('Tokyo Monitoring Instance').length).toBeGreaterThan(0)
    fireEvent.click(within(nodeDrawer).getByLabelText('关闭'))

    const relatedOverview = screen.getByRole('region', { name: '关联概览' })
    fireEvent.click(within(relatedOverview).getByRole('button', { name: '服务' }))
    const servicesDrawer = screen.getByRole('dialog', { name: '服务详情' })
    expect(within(servicesDrawer).getByRole('heading', { name: '服务资产' })).toBeInTheDocument()
    expect(within(servicesDrawer).getByText('https://blog.example.com')).toBeInTheDocument()
    expect(within(servicesDrawer).getByText('tg_001')).toBeInTheDocument()
    fireEvent.click(within(servicesDrawer).getByLabelText('关闭'))

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '域名' }))
    const domainsDrawer = screen.getByRole('dialog', { name: '域名详情' })
    expect(within(domainsDrawer).getByRole('heading', { name: '域名资产' })).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('www.example.com')).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('NameSilo')).toBeInTheDocument()
    expect(within(domainsDrawer).getByText('2026-07-01')).toBeInTheDocument()
    fireEvent.click(within(domainsDrawer).getByLabelText('关闭'))

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
    expect(within(timelineDrawer).getAllByRole('heading', { name: '资产历史' }).length).toBeGreaterThan(0)
    expect(within(timelineDrawer).getByText('未评估 -> 保留')).toBeInTheDocument()
    expect(within(timelineDrawer).getAllByText('稳定承载边缘流量').length).toBeGreaterThan(0)
    expect(within(timelineDrawer).getAllByText('USD 10.00 -> USD 12.00').length).toBeGreaterThan(0)
    expect(within(timelineDrawer).getByText('192.0.2.10 -> 192.0.2.1')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('root@192.0.2.1:22')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('已向服务商提交工单')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))

    fireEvent.click(screen.getByRole('button', { name: '基础资料' }))
    const factsDrawer = screen.getByRole('dialog', { name: '基础资料' })
    expect(within(factsDrawer).getByRole('heading', { name: '基础信息' })).toBeInTheDocument()
    expect(within(factsDrawer).getByText('vps_001')).toBeInTheDocument()
  })

  it('loads and renders an IP quality summary with a link to the full report', async () => {
    const responseBody = {
      ...vpsDetailBody,
      ip_quality_summary: ipQualitySummaryBody,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(ipQualityReportBody))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量概况' })).toBeInTheDocument())
    expect(screen.queryByText('取消/退役待处理')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/ip-quality', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    const ipQualitySummary = screen.getByRole('region', { name: 'IP 质量概况' })
    expect(ipQualitySummary.querySelector('.vps-ip-quality-summary__facts')).not.toBeNull()
    expect(ipQualitySummary.querySelector('.vps-ip-quality-summary__score')).toBeNull()
    expect(screen.getByRole('link', { name: '查看完整 IP 质量报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality')
    expect(screen.getByText('风险信号')).toBeInTheDocument()
    expect(screen.getByText('解锁概览')).toBeInTheDocument()
    expect(screen.getAllByText('1 可用').length).toBeGreaterThan(0)
    expect(screen.getByText('1 受阻')).toBeInTheDocument()
    expect(screen.getByText('0 部分')).toBeInTheDocument()
    expect(screen.getByText('0 未知')).toBeInTheDocument()
    expect(screen.queryByText('IP 高风险 · JP')).not.toBeInTheDocument()
    expect(screen.queryByText('AS64500')).not.toBeInTheDocument()
    expect(screen.queryByText('Example Transit')).not.toBeInTheDocument()
    expect(screen.getAllByText('VPN').length).toBeGreaterThan(0)
    expect(screen.queryByText('ChatGPT')).not.toBeInTheDocument()
    expect(screen.queryByText('解锁 · JP')).not.toBeInTheDocument()
    expect(screen.queryByText('Netflix')).not.toBeInTheDocument()
    expect(screen.queryByText('受阻 · US')).not.toBeInTheDocument()
    expect(screen.queryByText('Provider 判断')).not.toBeInTheDocument()
  })

  it('closes the top actions menu when the user clicks elsewhere on the page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
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
    const summary = screen.getByLabelText('VPS 详情操作')
    const menu = summary.closest('details')
    expect(menu).not.toBeNull()

    fireEvent.click(summary)
    expect(menu).toHaveAttribute('open')

    fireEvent.pointerDown(screen.getByLabelText('VPS 综合基础信息'))
    expect(menu).not.toHaveAttribute('open')
  })

  it('keeps related overview titles and quick actions wired to their target routes or modals', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({
        ...vpsDetailBody,
        ip_quality_summary: ipQualitySummaryBody,
      }))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([serviceBody]))
      .mockResolvedValueOnce(mockJSONResponse([domainBody]))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(ipQualityReportBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route
            path="/vps/:vpsId"
            element={(
              <>
                <LocationProbe />
                <VPSDetailPage />
              </>
            )}
          />
          <Route path="/subscriptions" element={<LocationProbe />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<LocationProbe />} />
          <Route path="/vps/:vpsId/ip-quality" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    const relatedOverview = screen.getByRole('region', { name: '关联概览' })

    expect(within(relatedOverview).getByRole('link', { name: '订阅' })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001')
    expect(within(relatedOverview).getByRole('link', { name: '监控观测' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(within(relatedOverview).getByRole('link', { name: 'IP 质量' })).toHaveAttribute('href', '/vps/vps_001/ip-quality')
    expect(within(relatedOverview).getByRole('button', { name: '接入/升级 agent' })).toBeInTheDocument()
    expect(within(relatedOverview).queryByRole('button', { name: '接入/升级' })).not.toBeInTheDocument()

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '创建/更新订阅' }))
    expect(screen.getByRole('dialog', { name: '创建/更新订阅' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '创建/更新订阅' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '延长' }))
    expect(screen.getByRole('dialog', { name: '延长有效期' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '延长有效期' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '关联' }))
    expect(await screen.findByRole('dialog', { name: '关联已有监控实例' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '关联已有监控实例' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '服务' }))
    expect(screen.getByRole('dialog', { name: '服务详情' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '服务详情' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '新增服务' }))
    expect(screen.getByRole('dialog', { name: '新增服务' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '新增服务' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '域名' }))
    expect(screen.getByRole('dialog', { name: '域名详情' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '域名详情' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '新增域名' }))
    expect(screen.getByRole('dialog', { name: '新增域名' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '新增域名' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '资产历史' }))
    expect(screen.getByRole('dialog', { name: '资产历史' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('dialog', { name: '资产历史' }).querySelector('.modal-close') as HTMLElement)

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '记录' }))
    expect(screen.getByRole('dialog', { name: '记录经验' })).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/targets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows IP quality load failures without mixing in the no-report empty state', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({
        ...vpsDetailBody,
        ip_quality_summary: ipQualitySummaryBody,
      }))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'ip quality backend down' }, 503))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const ipQualitySummary = await screen.findByRole('region', { name: 'IP 质量概况' })
    expect(within(ipQualitySummary).getByRole('alert')).toHaveTextContent('ip quality backend down')
    expect(within(ipQualitySummary).getByText('报告暂不可用')).toBeInTheDocument()
    expect(within(ipQualitySummary).getByRole('link', { name: '查看完整 IP 质量报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality')
    expect(within(ipQualitySummary).queryByText('尚无 IP 质量报告')).not.toBeInTheDocument()
    expect(within(ipQualitySummary).queryByText('尚未收到可用质量结论。')).not.toBeInTheDocument()
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
    const currentJudgement = screen.getByLabelText('当前判断')
    expect(within(currentJudgement).getByText('订阅证据暂不可用')).toBeInTheDocument()
    expect(within(currentJudgement).getByRole('link', { name: '核对订阅' })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001')
    expect(within(currentJudgement).getByText('subscription backend down')).toBeInTheDocument()
    const operationFeedback = screen.queryByLabelText('VPS 操作反馈')
    if (operationFeedback) {
      expect(operationFeedback).not.toHaveTextContent('订阅证据暂不可用')
      expect(operationFeedback).not.toHaveTextContent('subscription backend down')
    }
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
    expect(screen.getByText('缺少当前订阅')).toBeInTheDocument()
    expect(screen.getAllByText('缺订阅').length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '创建/更新订阅' }).length).toBeGreaterThan(0)
    expect(screen.queryByText('订阅证据暂不可用')).not.toBeInTheDocument()
  })

  it('creates subscription facts from the VPS detail page without asking for subscription status', async () => {
    const responseBody = {
      ...vpsDetailBody,
      vps_id: 'vps_missing_subscription',
      display_name: 'Missing Subscription Edge',
      renewal_decision: 'keep',
    }
    const createdSubscription = {
      ...subscriptionBody,
      subscription_id: 'sub_scoped_001',
      vps_id: 'vps_missing_subscription',
      price: 18,
      monthly_price: 18,
      renew_at: '2026-07-01',
      auto_renew: true,
      renewal_mode: 'auto',
      payment_method: 'visa',
      note: 'created from vps detail',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(responseBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(createdSubscription, 201))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_missing_subscription']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Missing Subscription Edge' })).toBeInTheDocument())
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '创建/更新订阅' }), 'subscription command'))
    const drawer = screen.getByRole('dialog', { name: '创建/更新订阅' })
    expect(within(drawer).queryByLabelText('订阅状态')).not.toBeInTheDocument()
    fireEvent.change(within(drawer).getByLabelText('价格'), { target: { value: '18' } })
    fireEvent.change(within(drawer).getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(drawer).getByLabelText('自动续费'))
    fireEvent.change(within(drawer).getByLabelText('支付方式'), { target: { value: '__custom' } })
    fireEvent.change(within(drawer).getByLabelText('自定义支付方式'), { target: { value: 'visa' } })
    fireEvent.change(within(drawer).getByLabelText('备注'), { target: { value: 'created from vps detail' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '创建/更新订阅' }))

    await waitFor(() => expect(screen.getByText('订阅账单事实已创建')).toBeInTheDocument())
    expect(screen.getAllByText('USD 18.00').length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_missing_subscription/subscriptions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        price: 18,
        currency: 'USD',
        billing_cycle: 'monthly',
        billing_months: 1,
        billing_period_unit: 'month',
        billing_period_length: 1,
        started_at: null,
        renew_at: '2026-07-01',
        auto_renew: true,
        auto_renew_cancelled: false,
        renewal_mode: 'auto',
        payment_method: 'visa',
        note: 'created from vps detail',
      }),
    })
  })

  it('opens quick subscription creation from the VPS workbench deep link', async () => {
    const responseBody = {
      ...vpsDetailBody,
      vps_id: 'vps_missing_subscription',
      display_name: 'Missing Subscription Edge',
      renewal_decision: 'keep',
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
      <MemoryRouter initialEntries={['/vps/vps_missing_subscription?workbench=subscription']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const drawer = await screen.findByRole('dialog', { name: '创建/更新订阅' })
    expect(within(drawer).getByText('只补录账单事实；生命周期、用途和续费决策继续归 VPS 管理。')).toBeInTheDocument()
    expect(within(drawer).queryByLabelText('订阅状态')).not.toBeInTheDocument()
  })

  it('opens monitoring instance creation from the VPS workbench deep link', async () => {
    const detailBody = {
      ...vpsDetailBody,
      renewal_decision: 'keep',
      active_monitoring_instance_link_count: 0,
      monitoring_instance_links: [],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?workbench=monitoring']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const drawer = await screen.findByRole('dialog', { name: '接入/升级 agent' })
    expect(within(drawer).getByLabelText('监控实例名称')).toHaveValue('Tokyo Edge')
    expect(within(drawer).getByLabelText('服务商')).toHaveValue('Hetzner')
    expect(within(drawer).getByLabelText('区域')).toHaveValue('Kanto')
    expect(within(drawer).getByLabelText('城市')).toHaveValue('Tokyo')
    expect(within(drawer).getByText('已按 VPS 资料预填，必要时微调后直接创建并进入 agent 接入。')).toBeInTheDocument()
    expect(within(drawer).queryByLabelText('继承字段')).not.toBeInTheDocument()
  })

  it('reuses the existing active monitoring instance when opening the monitoring workbench deep link', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?workbench=monitoring']}>
        <Routes>
          <Route
            path="/vps/:vpsId"
            element={(
              <>
                <LocationProbe />
                <VPSDetailPage />
              </>
            )}
          />
          <Route path="/monitoring/:monitoringInstanceId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByTestId('location-path')).toHaveTextContent('/monitoring/mi_001'))
    expect(screen.getByTestId('location-search')).toHaveTextContent('onboarding=1')
    expect(screen.getByTestId('location-search')).toHaveTextContent('return_vps=vps_001')
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/vps/vps_001/monitoring-instances', expect.anything())
  })

  it('cleans VPS workbench query params when a deep-linked drawer is cancelled', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?workbench=subscription']}>
        <Routes>
          <Route
            path="/vps/:vpsId"
            element={(
              <>
                <LocationProbe />
                <VPSDetailPage />
              </>
            )}
          />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByTestId('location-search')).toHaveTextContent('workbench=subscription')
    const drawer = await screen.findByRole('dialog', { name: '创建/更新订阅' })
    fireEvent.click(within(drawer).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '创建/更新订阅' })).not.toBeInTheDocument())
    expect(screen.getByTestId('location-search')).not.toHaveTextContent('workbench=')
  })

  it('rejects validity extension dates before the active subscription renewal date', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const validityButtons = await screen.findAllByRole('button', { name: '延长有效期' })
    fireEvent.click(firstResult(validityButtons, 'validity command'))
    const drawer = await screen.findByRole('dialog', { name: '延长有效期' })
    fireEvent.change(within(drawer).getByLabelText('延长至日期'), { target: { value: '2026-05-15' } })
    fireEvent.change(within(drawer).getByLabelText('延长原因'), { target: { value: '故障补偿' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '保存延长记录' }))

    expect(await within(drawer).findByRole('alert')).toHaveTextContent('延长至日期不能早于当前 active 订阅续费日。')
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('creates a monitoring instance from VPS identity and navigates to onboarding', async () => {
    const detailBody = {
      ...vpsDetailBody,
      renewal_decision: 'keep',
      active_monitoring_instance_link_count: 0,
      monitoring_instance_links: [],
    }
    const refreshedDetail = {
      ...detailBody,
      active_monitoring_instance_link_count: 1,
      monitoring_instance_links: [{
        monitoring_instance_id: 'mi_scoped_001',
        display_name: 'Tokyo Edge',
        group: 'edge',
        region: 'Kanto',
        city: 'Tokyo',
        provider: 'Hetzner',
        lifecycle_status: '待接入',
        monitoring_status: '启用',
        binding_status: '待绑定',
        current_health_status: '正常',
        last_heartbeat_at: null,
        last_sync_at: null,
        current_active_incident_count: 0,
        current_primary_issue_summary: '',
        linked_at: '2026-05-09T09:02:00Z',
        note: 'primary',
      }],
    }
    const createdMonitoringInstance = {
      monitoring_instance_id: 'mi_scoped_001',
      display_name: 'Tokyo Edge',
      group: 'edge',
      region: 'Kanto',
      city: 'Tokyo',
      provider: 'Hetzner',
      lifecycle_status: '待接入',
      monitoring_status: '启用',
      binding_status: '待绑定',
      labels: ['edge'],
      note: 'primary',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-05-09T09:02:00Z',
      updated_at: '2026-05-09T09:02:00Z',
      link: {
        link_id: 'vnl_scoped_001',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mi_scoped_001',
        linked_at: '2026-05-09T09:02:00Z',
        unlinked_at: null,
        note: 'created from vps detail',
      },
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(createdMonitoringInstance, 201))
      .mockResolvedValueOnce(mockJSONResponse(refreshedDetail))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<div>monitoring onboarding route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '接入/升级 agent' }), 'agent onboarding command'))
    const drawer = screen.getByRole('dialog', { name: '接入/升级 agent' })
    expect(within(drawer).getByLabelText('监控实例名称')).toHaveValue('Tokyo Edge')
    expect(within(drawer).getByLabelText('标签')).toHaveValue('edge')
    fireEvent.click(within(drawer).getByRole('button', { name: '接入/升级 agent' }))

    await waitFor(() => expect(screen.getByText('monitoring onboarding route')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/monitoring-instances', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        display_name: 'Tokyo Edge',
        group: '',
        region: 'Kanto',
        city: 'Tokyo',
        provider: 'Hetzner',
        labels: ['edge'],
        note: 'primary',
        link_note: 'created from vps detail',
      }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('upgrades through the existing active monitoring instance instead of creating another one', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route
            path="/vps/:vpsId"
            element={(
              <>
                <LocationProbe />
                <VPSDetailPage />
              </>
            )}
          />
          <Route path="/monitoring/:monitoringInstanceId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '接入/升级 agent' }), 'agent upgrade command'))

    await waitFor(() => expect(screen.getByTestId('location-path')).toHaveTextContent('/monitoring/mi_001'))
    expect(screen.getByTestId('location-search')).toHaveTextContent('onboarding=1')
    expect(screen.getByTestId('location-search')).toHaveTextContent('return_vps=vps_001')
    expect(fetchMock).not.toHaveBeenCalledWith('/api/vps/vps_001/monitoring-instances', expect.anything())
  })

  it('surfaces duplicate active monitoring links for manual review without showing create entry', async () => {
    const duplicateDetail = {
      ...vpsDetailBody,
      active_monitoring_instance_link_count: 2,
      monitoring_instance_links: [
        vpsDetailBody.monitoring_instance_links[0],
        {
          ...vpsDetailBody.monitoring_instance_links[0],
          monitoring_instance_id: 'mi_002',
          display_name: 'Tokyo Monitoring Instance Duplicate',
          current_health_status: '关注',
          linked_at: '2026-05-09T09:00:00Z',
        },
      ],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(duplicateDetail))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
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
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '监控观测' }), 'monitoring evidence command'))
    const evidenceDrawer = await screen.findByRole('dialog', { name: '监控观测' })

    expect(within(evidenceDrawer).queryByRole('button', { name: '关联已有监控实例' })).not.toBeInTheDocument()
    expect(within(evidenceDrawer).getByRole('alert')).toHaveTextContent('检测到 2 个 active 监控实例关联')
    expect(within(evidenceDrawer).getAllByRole('button', { name: '接入/升级 agent' })).toHaveLength(2)
    expect(within(evidenceDrawer).getAllByRole('button', { name: '解除关联' })).toHaveLength(2)
  })

  it('does not promote the cancellation workbench for a fresh active VPS', async () => {
    const detailBody = {
      ...vpsDetailBody,
      renewal_decision: 'unreviewed',
      active_monitoring_instance_link_count: 0,
      running_monitoring_instance_count: 0,
      running_target_count: 0,
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
    expect(screen.getByRole('button', { name: '调整决策' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '创建/更新订阅' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '接入/升级 agent' }).length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: '打开取消/退役' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '处理取消/退役' })).not.toBeInTheDocument()
    openVPSActionsMenu()
    expect(screen.queryByRole('button', { name: '取消/退役' })).not.toBeInTheDocument()
    expect(screen.queryByText('LIFECYCLE COORDINATION')).not.toBeInTheDocument()
  })

  it('opens cancellation handling from the top current judgement instead of the more menu', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(vpsDetailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(cancellationPreviewBody()))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    const currentJudgement = screen.getByLabelText('当前判断')
    expect(within(currentJudgement).getAllByText('取消/退役').length).toBeGreaterThan(0)
    const cancellationButton = within(currentJudgement).getByRole('button', { name: '处理取消/退役' })
    expect(cancellationButton).toBeInTheDocument()
    openVPSActionsMenu()
    expect(screen.queryByRole('button', { name: '取消/退役' })).not.toBeInTheDocument()
    fireEvent.pointerDown(screen.getByLabelText('VPS 综合基础信息'))

    fireEvent.click(cancellationButton)

    const drawer = await screen.findByRole('dialog', { name: '取消/退役' })
    expect(within(drawer).getByLabelText('取消/退役影响范围摘要')).toBeInTheDocument()
    expect(screen.queryByText('取消/退役待处理')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/cancellation-preview', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows multiple persistent attention states together in the top current judgement', async () => {
    const detailBody = {
      ...vpsDetailBody,
      lifecycle_status: 'to_cancel',
      renewal_decision: 'cancel',
      monitoring_instance_links: [{
        ...vpsDetailBody.monitoring_instance_links[0],
        current_health_status: '告警',
        current_active_incident_count: 2,
        current_primary_issue_summary: 'packet loss',
      }],
    }
    const cancelledAutoRenewSubscription = {
      ...subscriptionBody,
      auto_renew_cancelled: true,
      renew_at: '2026-08-15',
      ends_at: '2026-08-15',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([cancelledAutoRenewSubscription]))
      .mockResolvedValueOnce(mockJSONResponse(cancellationPreviewBody({ vps: detailBody })))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?workbench=cancellation']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    const currentJudgement = screen.getByLabelText('当前判断')
    expect(within(currentJudgement).getAllByText('取消/退役').length).toBeGreaterThan(0)
    expect(within(currentJudgement).getByText('运行观测需要核对')).toBeInTheDocument()
    expect(within(currentJudgement).getByText('Tokyo Monitoring Instance · 2 个活跃异常')).toBeInTheDocument()
    expect(within(currentJudgement).getByText('自动续费已取消')).toBeInTheDocument()
    expect(within(currentJudgement).getByRole('button', { name: '处理取消/退役' })).toBeInTheDocument()
    expect(within(currentJudgement).getByRole('link', { name: '查看监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(within(currentJudgement).getByRole('button', { name: '调整决策' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '需要处理的状态' })).not.toBeInTheDocument()
    const operationFeedback = screen.queryByLabelText('VPS 操作反馈')
    if (operationFeedback) {
      expect(operationFeedback).not.toHaveTextContent('运行观测需要核对')
      expect(operationFeedback).not.toHaveTextContent('自动续费已取消')
    }
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
    const decisionDrawer = screen.getByRole('dialog', { name: '调整决策' })
    fireEvent.change(within(decisionDrawer).getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(within(decisionDrawer).getByLabelText('决策理由'), { target: { value: 'too expensive' } })
    fireEvent.click(within(decisionDrawer).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText('续费决策已更新，资产历史已刷新')).toBeInTheDocument())
    expect(screen.getAllByText('too expensive').length).toBeGreaterThan(0)
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '资产历史' }), 'asset history command'))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
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
    let decisionDialog = screen.getByRole('dialog', { name: '调整决策' })
    fireEvent.change(within(decisionDialog).getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(within(decisionDialog).getByLabelText('决策理由'), { target: { value: 'stale decision' } })
    fireEvent.click(within(decisionDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '调整决策' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '调整决策' }))
    decisionDialog = screen.getByRole('dialog', { name: '调整决策' })
    expect(within(decisionDialog).getByLabelText('续费决策')).toHaveValue('keep')
    expect(within(decisionDialog).getByLabelText('决策理由')).toHaveValue('')
    fireEvent.click(within(decisionDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('编辑基础资料')
    let factsDialog = screen.getByRole('dialog', { name: '编辑基础资料' })
    fireEvent.change(within(factsDialog).getByLabelText('VPS 名称'), { target: { value: 'Stale VPS' } })
    fireEvent.click(within(factsDialog).getByRole('button', { name: '取消编辑' }))
    expect(screen.queryByRole('dialog', { name: '编辑基础资料' })).not.toBeInTheDocument()

    clickVPSAction('编辑基础资料')
    factsDialog = screen.getByRole('dialog', { name: '编辑基础资料' })
    expect(within(factsDialog).getByLabelText('VPS 名称')).toHaveValue('Tokyo Edge')
    fireEvent.click(within(factsDialog).getByRole('button', { name: '取消编辑' }))

    clickVPSAction('关联已有监控实例')
    let nodeDialog = screen.getByRole('dialog', { name: '关联已有监控实例' })
    expect(within(nodeDialog).getByLabelText('选择监控实例')).toBeDisabled()
    fireEvent.change(within(nodeDialog).getByLabelText('关联备注'), { target: { value: 'stale note' } })
    fireEvent.click(within(nodeDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '关联已有监控实例' })).not.toBeInTheDocument()

    clickVPSAction('关联已有监控实例')
    nodeDialog = screen.getByRole('dialog', { name: '关联已有监控实例' })
    expect(within(nodeDialog).getByLabelText('选择监控实例')).toHaveValue('')
    expect(within(nodeDialog).getByLabelText('关联备注')).toHaveValue('')
    fireEvent.click(within(nodeDialog).getByRole('button', { name: '取消' }))

    clickVPSAction('记录经验')
    let experienceDialog = screen.getByRole('dialog', { name: '记录经验' })
    fireEvent.change(within(experienceDialog).getByLabelText('摘要'), { target: { value: 'stale experience' } })
    fireEvent.click(within(experienceDialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '记录经验' })).not.toBeInTheDocument()

    clickVPSAction('记录经验')
    experienceDialog = screen.getByRole('dialog', { name: '记录经验' })
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

    clickVPSAction('编辑基础资料')
    const factsDrawer = screen.getByRole('dialog', { name: '编辑基础资料' })
    fireEvent.change(within(factsDrawer).getByLabelText('VPS 名称'), { target: { value: 'Tokyo Edge 2' } })
    fireEvent.change(within(factsDrawer).getByLabelText('产品名'), { target: { value: 'cx32' } })
    fireEvent.change(within(factsDrawer).getByLabelText('IPv4 / 主入口'), { target: { value: '198.51.100.5' } })
    fireEvent.click(within(factsDrawer).getByLabelText(/SSH Host 与 IP 不一致/))
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
    expect(screen.getAllByText('edge.example.com:2222').length).toBeGreaterThan(0)
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '资产历史' }), 'asset history command'))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
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

    clickVPSAction('关联已有监控实例')
    await waitFor(() => expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    }))
    fireEvent.change(within(screen.getByRole('dialog', { name: '关联已有监控实例' })).getByLabelText('选择监控实例'), { target: { value: 'mi_002' } })
    fireEvent.change(within(screen.getByRole('dialog', { name: '关联已有监控实例' })).getByLabelText('关联备注'), { target: { value: 'secondary' } })
    fireEvent.click(within(screen.getByRole('dialog', { name: '关联已有监控实例' })).getByRole('button', { name: '关联监控实例' }))

    await waitFor(() => expect(screen.getAllByText('1 个实例 · 正常').length).toBeGreaterThan(0))
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

    clickVPSAction('监控观测')
    const nodeEvidenceDrawer = screen.getByRole('dialog', { name: '监控观测' })
    expect(within(nodeEvidenceDrawer).queryByRole('button', { name: '关联已有监控实例' })).not.toBeInTheDocument()
    expect(within(nodeEvidenceDrawer).getAllByRole('button', { name: '接入/升级 agent' }).length).toBeGreaterThan(0)
    fireEvent.click(within(nodeEvidenceDrawer).getByLabelText('关闭'))
    clickVPSAction('监控观测')
    const reopenedEvidenceDrawer = screen.getByRole('dialog', { name: '监控观测' })
    expect(within(reopenedEvidenceDrawer).getAllByRole('button', { name: '接入/升级 agent' }).length).toBeGreaterThan(0)
    fireEvent.click(within(reopenedEvidenceDrawer).getByLabelText('关闭'))
    clickVPSAction('监控观测')
    const unlinkEvidenceDrawer = screen.getByRole('dialog', { name: '监控观测' })
    fireEvent.click(within(unlinkEvidenceDrawer).getByRole('button', { name: '解除关联' }))
    const unlinkConfirmation = within(unlinkEvidenceDrawer).getByRole('alertdialog', { name: '确认解除监控实例关联' })
    expect(fetchMock).not.toHaveBeenCalledWith('/api/vps/vps_001/unlink-monitoring-instance', expect.anything())
    fireEvent.click(within(unlinkConfirmation).getByRole('button', { name: '确认解除关联' }))
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
    const experienceDrawer = screen.getByRole('dialog', { name: '记录经验' })
    fireEvent.change(within(experienceDrawer).getByLabelText('分类'), { target: { value: 'network' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('级别'), { target: { value: 'warning' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('摘要'), { target: { value: '晚高峰丢包' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('发生时间'), { target: { value: '2026-05-10T09:30' } })
    fireEvent.change(within(experienceDrawer).getByLabelText('详情'), { target: { value: '连续三天 tcp probe 抖动' } })
    fireEvent.click(within(experienceDrawer).getByRole('button', { name: '写入经验记录' }))

    await waitFor(() => expect(screen.getAllByText('经验记录已写入资产历史').length).toBeGreaterThan(0))
    expect(screen.getAllByText(/晚高峰丢包/).length).toBeGreaterThan(0)
    fireEvent.click(firstResult(screen.getAllByRole('button', { name: '资产历史' }), 'asset history command'))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
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

  it.each(['archived', 'cancelled'] as const)('redirects %s VPS detail requests to archive detail', async (lifecycleStatus) => {
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
      lifecycle_status: lifecycleStatus,
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: ['edge'],
      note: 'primary',
      active_monitoring_instance_link_count: 0,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: lifecycleStatus === 'archived' ? '2026-05-09T10:00:00Z' : null,
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
          <Route path="/archive/:vpsId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByTestId('location-path')).toHaveTextContent('/archive/vps_001'))
    expect(screen.queryByRole('heading', { name: 'Tokyo Edge' })).not.toBeInTheDocument()
  })

  it('loads archive review and blocks archive when active subscriptions or runtime evidence remain', async () => {
    const detailBody = {
      ...vpsDetailBody,
      monitoring_instance_links: [],
      active_monitoring_instance_link_count: 0,
      running_monitoring_instance_count: 0,
      running_target_count: 0,
    }
    const archiveReview = {
      vps: detailBody,
      subscriptions: [{
        record: subscriptionBody,
        role: 'active',
        recommended_action: 'cancel_subscription_first',
        message: '订阅仍为 active。',
      }],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: ['存在 1 条 active 订阅，必须先取消或结束订阅后才能归档。'],
      eligible: false,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([subscriptionBody]))
      .mockResolvedValueOnce(mockJSONResponse(archiveReview))
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

    const dialog = await screen.findByRole('alertdialog', { name: '确认归档 VPS' })
    expect(document.querySelector('.asset-lifecycle-card')).toBeNull()
    expect(within(dialog).getByText('存在 1 条 active 订阅，必须先取消或结束订阅后才能归档。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '确认归档' })).toBeDisabled()
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/archive-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledTimes(6)
  })

  it('requires typed display name, archives through controlled API, and navigates to archive detail', async () => {
    const detailBody = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
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
    const archiveReview = {
      vps: detailBody,
      subscriptions: [{
        record: { ...subscriptionBody, status: 'cancelled' },
        role: 'inactive',
        recommended_action: 'read_only_history',
        message: '历史订阅只读保留。',
      }],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: [],
      eligible: true,
    }
    const archivedReview = {
      ...archiveReview,
      vps: {
        ...detailBody,
        lifecycle_status: 'archived',
        archived_at: '2026-05-09T10:00:00Z',
      },
      blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      eligible: false,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(detailBody))
      .mockResolvedValueOnce(mockJSONResponse(timelineEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(servicesEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse(domainsEmptyBody))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(archiveReview))
      .mockResolvedValueOnce(mockJSONResponse(archivedReview))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
          <Route path="/archive/:vpsId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())

    clickVPSAction('归档 VPS')
    const dialog = await screen.findByRole('alertdialog', { name: '确认归档 VPS' })
    expect(document.querySelector('.asset-lifecycle-card')).toBeNull()
    expect(within(dialog).getByText('输入 VPS 展示名后才能归档，服务端会再次校验资格。')).toBeInTheDocument()
    const confirmButton = within(dialog).getByRole('button', { name: '确认归档' })
    expect(confirmButton).toBeDisabled()

    fireEvent.change(within(dialog).getByLabelText('输入 VPS 名称确认归档'), { target: { value: 'Tokyo' } })
    expect(confirmButton).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('输入 VPS 名称确认归档'), { target: { value: 'Tokyo Edge' } })
    expect(confirmButton).toBeEnabled()
    fireEvent.click(confirmButton)

    await waitFor(() => expect(screen.getByTestId('location-path')).toHaveTextContent('/archive/vps_001'))
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_001/archive-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/vps/vps_001/archive', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ confirmation_name: 'Tokyo Edge' }),
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
    expect(screen.getAllByText('1 个服务').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Blog').length).toBeGreaterThan(0)
    const relatedOverview = screen.getByRole('region', { name: '关联概览' })
    fireEvent.click(within(relatedOverview).getByRole('button', { name: '服务' }))
    const serviceDetailDrawer = screen.getByRole('dialog', { name: '服务详情' })
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
    expect(screen.getAllByText('1 个域名').length).toBeGreaterThan(0)
    expect(screen.getAllByText('api.example.com').length).toBeGreaterThan(0)
    const relatedOverview = screen.getByRole('region', { name: '关联概览' })
    fireEvent.click(within(relatedOverview).getByRole('button', { name: '域名' }))
    const domainDetailDrawer = screen.getByRole('dialog', { name: '域名详情' })
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
    expect(screen.getByRole('region', { name: '关联概览' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '单机台账' })).toBeInTheDocument()
    expect(screen.getByText('未记录服务')).toBeInTheDocument()
    expect(screen.getByText('未记录域名')).toBeInTheDocument()
    expect(screen.queryByText('暂无续费决策历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无价格变化历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无 IP 变化历史')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无规格快照')).not.toBeInTheDocument()
    expect(screen.queryByText('暂无经验记录')).not.toBeInTheDocument()
    expect(screen.queryByText('尚未记录服务')).not.toBeInTheDocument()
    expect(screen.queryByText('尚未记录域名')).not.toBeInTheDocument()

    const relatedOverview = screen.getByRole('region', { name: '关联概览' })
    fireEvent.click(within(relatedOverview).getByRole('button', { name: '资产历史' }))
    const timelineDrawer = screen.getByRole('dialog', { name: '资产历史' })
    expect(within(timelineDrawer).getByText('暂无续费决策历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无价格变化历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无 IP 变化历史')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无规格快照')).toBeInTheDocument()
    expect(within(timelineDrawer).getByText('暂无经验记录')).toBeInTheDocument()
    fireEvent.click(within(timelineDrawer).getByLabelText('关闭'))

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '服务' }))
    const servicesDrawer = screen.getByRole('dialog', { name: '服务详情' })
    expect(within(servicesDrawer).getByText('尚未记录服务')).toBeInTheDocument()
    fireEvent.click(within(servicesDrawer).getByLabelText('关闭'))

    fireEvent.click(within(relatedOverview).getByRole('button', { name: '域名' }))
    const domainsDrawer = screen.getByRole('dialog', { name: '域名详情' })
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
        message: '订阅账单记录已无续费动作，仍需处理 VPS、监控实例与入口探测状态。',
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

    const workbench = await screen.findByRole('dialog', { name: '取消/退役' })
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
