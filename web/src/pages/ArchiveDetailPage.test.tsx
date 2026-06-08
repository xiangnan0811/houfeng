import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ArchiveDetailPage } from './ArchiveDetailPage'
import type { SubscriptionRecord, VPSAssetRecord, VPSTimeline } from '../lib/types'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const archivedVPS: VPSAssetRecord = {
  vps_id: 'vps_archived',
  display_name: 'Tokyo Retired',
  provider_id: 'pv_001',
  provider_name: 'Hetzner',
  product_name: 'cx22',
  order_ref: 'ord-archived',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: 'nrt',
  ipv4: '192.0.2.44',
  ipv6: '2001:db8::44',
  ssh_host: '192.0.2.44',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: 'Debian',
  virtualization: 'kvm',
  lifecycle_status: 'archived',
  usage_status: 'idle',
  renewal_decision: 'cancel',
  importance: 'normal',
  labels: ['retired'],
  note: 'provider quality evidence',
  active_monitoring_instance_link_count: 0,
  running_monitoring_instance_count: 0,
  running_target_count: 0,
  created_at: '2026-01-01T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: '2026-05-09T08:00:00Z',
}

const subscription: SubscriptionRecord = {
  subscription_id: 'sub_archived',
  vps_id: 'vps_archived',
  price: 24,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 24,
  started_at: '2026-01-01',
  renew_at: '2026-05-01',
  auto_renew: false,
  auto_renew_cancelled: true,
  renewal_mode: 'auto_cancelled',
  status: 'cancelled',
  payment_method: 'card',
  note: 'cancelled after outage',
  created_at: '2026-01-01T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

const archiveReview = {
  vps: archivedVPS,
  subscriptions: [{
    record: subscription,
    role: 'inactive',
    recommended_action: 'read_only_history',
    message: '历史订阅只读保留。',
  }],
  monitoring_instance_links: [{
    monitoring_instance_id: 'mi_archived',
    display_name: 'Tokyo Agent History',
    group: 'legacy',
    region: 'Kanto',
    city: 'Tokyo',
    provider: 'Hetzner',
    lifecycle_status: '已退役',
    monitoring_status: '暂停',
    binding_status: '已绑定',
    current_health_status: '关注',
    last_heartbeat_at: '2026-05-08T08:00:00Z',
    last_sync_at: '2026-05-08T08:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: 'historical packet loss',
    linked_at: '2026-01-02T08:00:00Z',
    note: 'agent history',
  }],
  services: [{
    service_id: 'svc_archived',
    vps_id: 'vps_archived',
    target_id: 'tg_archived',
    name: 'Legacy API',
    service_type: 'api',
    status: 'retired',
    url: 'https://legacy.example.test',
    port: 443,
    labels: ['legacy'],
    note: 'retired endpoint',
    created_at: '2026-01-02T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
  }],
  domains: [{
    domain_id: 'dom_archived',
    vps_id: 'vps_archived',
    service_id: 'svc_archived',
    target_id: 'tg_archived',
    domain_name: 'legacy.example.test',
    purpose: 'legacy api',
    status: 'retired',
    registrar: 'Cloudflare',
    expires_at: '2026-09-01',
    auto_renew: false,
    https_enabled: true,
    labels: ['legacy'],
    note: 'retired domain',
    created_at: '2026-01-02T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
  }],
  target_links: [{
    target_id: 'tg_archived',
    name: 'Legacy API Target',
    run_status: '已归档',
    service_ids: ['svc_archived'],
    domain_ids: ['dom_archived'],
    last_linked_at: '2026-05-09T08:00:00Z',
  }],
  warnings: [],
  blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
  eligible: false,
}

const timeline: VPSTimeline = {
  vps_id: 'vps_archived',
  renewal_decisions: [{
    decision_id: 'rd_001',
    vps_id: 'vps_archived',
    from_decision: 'keep',
    to_decision: 'cancel',
    reason: 'network quality regression',
    decided_at: '2026-05-01T08:00:00Z',
    created_at: '2026-05-01T08:00:00Z',
  }],
  price_histories: [{
    price_history_id: 'ph_001',
    subscription_id: 'sub_archived',
    vps_id: 'vps_archived',
    from_price: 20,
    to_price: 24,
    from_currency: 'USD',
    to_currency: 'USD',
    from_billing_cycle: 'monthly',
    to_billing_cycle: 'monthly',
    from_billing_months: 1,
    to_billing_months: 1,
    from_monthly_price: 20,
    to_monthly_price: 24,
    from_auto_renew: true,
    to_auto_renew: false,
    from_auto_renew_cancelled: false,
    to_auto_renew_cancelled: true,
    from_status: 'active',
    to_status: 'cancelled',
    changed_at: '2026-05-01T08:00:00Z',
    created_at: '2026-05-01T08:00:00Z',
  }],
  ip_histories: [{
    ip_history_id: 'iph_001',
    vps_id: 'vps_archived',
    from_ipv4: '192.0.2.1',
    to_ipv4: '192.0.2.44',
    from_ipv6: '',
    to_ipv6: '2001:db8::44',
    changed_at: '2026-02-01T08:00:00Z',
    created_at: '2026-02-01T08:00:00Z',
  }],
  spec_snapshots: [{
    snapshot_id: 'spec_001',
    vps_id: 'vps_archived',
    product_name: 'cx22',
    ssh_host: '192.0.2.44',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'kvm',
    captured_at: '2026-03-01T08:00:00Z',
    created_at: '2026-03-01T08:00:00Z',
  }],
  experience_logs: [{
    experience_log_id: 'elog_001',
    vps_id: 'vps_archived',
    category: 'network',
    severity: 'warning',
    summary: '晚高峰网络质量明显下降',
    details: '连续三周 TCP probe 抖动，最终决定不再续费。',
    occurred_at: '2026-04-30T08:00:00Z',
    created_at: '2026-04-30T08:00:00Z',
  }],
}

function PathProbe() {
  const location = useLocation()
  return <div data-testid="path-probe">{location.pathname}</div>
}

describe('ArchiveDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders archived VPS read-only detail with user records before monitoring and Target history', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(archiveReview))
      .mockResolvedValueOnce(mockJSONResponse(timeline))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/archive/vps_archived']}>
        <Routes>
          <Route path="/archive/:vpsId" element={<ArchiveDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Retired' })).toBeInTheDocument())
    expect(screen.getByText('只读归档详情')).toBeInTheDocument()
    expect(screen.getByText('已归档资产不会进入 VPS、订阅、监控或资产组合决策主流程。')).toBeInTheDocument()
    for (const heading of ['基础信息', '访问入口', '订阅历史', '月成本', '服务', '域名', '续费判断', '资产历史']) {
      expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument()
    }
    const userRecords = screen.getByRole('region', { name: '用户记录' })
    expect(within(userRecords).getByText('晚高峰网络质量明显下降')).toBeInTheDocument()
    expect(within(userRecords).getByText('连续三周 TCP probe 抖动，最终决定不再续费。')).toBeInTheDocument()
    const userRecordsHeading = screen.getByRole('heading', { name: '用户记录' })
    for (const detailHeading of ['订阅明细', '服务资产', '域名资产']) {
      const heading = screen.getByRole('heading', { name: detailHeading })
      expect(userRecordsHeading.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    expect(screen.getByRole('heading', { name: '续费、价格、规格与 IP 历史' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '监控历史' })).toHaveClass('archive-detail__full-width')
    expect(screen.getByRole('region', { name: 'Target 历史' })).toHaveClass('archive-detail__full-width')
    expect(screen.getByText('Tokyo Agent History')).toBeInTheDocument()
    expect(screen.getByText('Legacy API Target')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复为闲置' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /编辑|新增|创建|关联|取消\/退役工作台/ })).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_archived/archive-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_archived/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions?vps_id=vps_archived&sort=renew_at&order=asc&asset_scope=all', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('does not show restore for cancelled archived-ledger assets', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({
        ...archiveReview,
        vps: { ...archivedVPS, lifecycle_status: 'cancelled', archived_at: null },
        blockers: [],
        eligible: false,
      }))
      .mockResolvedValueOnce(mockJSONResponse(timeline))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/archive/vps_archived']}>
        <Routes>
          <Route path="/archive/:vpsId" element={<ArchiveDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Retired' })).toBeInTheDocument())
    expect(screen.getByText('已取消资产不可恢复，仅保留历史回看。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '恢复为闲置' })).not.toBeInTheDocument()
  })

  it('redirects non-archived assets away from archive detail', async () => {
    const activeReview = {
      ...archiveReview,
      vps: {
        ...archivedVPS,
        vps_id: 'vps_active',
        display_name: 'Tokyo Active',
        lifecycle_status: 'active',
        archived_at: null,
      },
      blockers: [],
      eligible: true,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(activeReview))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/archive/vps_active']}>
        <Routes>
          <Route path="/archive/:vpsId" element={<ArchiveDetailPage />} />
          <Route path="/vps/:vpsId" element={<PathProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByTestId('path-probe')).toHaveTextContent('/vps/vps_active'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_active/archive-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('restores an archived VPS through the controlled restore API and returns to the VPS detail route', async () => {
    const restored = { ...archivedVPS, lifecycle_status: 'idle', archived_at: null }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(archiveReview))
      .mockResolvedValueOnce(mockJSONResponse(timeline))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse(restored))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/archive/vps_archived']}>
        <Routes>
          <Route path="/archive/:vpsId" element={<ArchiveDetailPage />} />
          <Route path="/vps/:vpsId" element={<PathProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Retired' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '恢复为闲置' }))
    const dialog = screen.getByRole('alertdialog', { name: '确认恢复归档 VPS' })
    expect(within(dialog).getByText('恢复后进入闲置状态，关联订阅、监控、服务、域名和历史记录会保留。')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认恢复' }))

    await waitFor(() => expect(screen.getByTestId('path-probe')).toHaveTextContent('/vps/vps_archived'))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_archived/restore-from-archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })
})
