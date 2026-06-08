import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ArchivePage } from './ArchivePage'
import type { AssetDomainRecord, AssetServiceRecord, SubscriptionRecord, VPSAssetDetail, VPSAssetRecord, VPSTimeline } from '../lib/types'

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
  ipv6: '',
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

const archivedDetail: VPSAssetDetail = {
  ...archivedVPS,
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
}

const service: AssetServiceRecord = {
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
}

const domain: AssetDomainRecord = {
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

const eurSubscription: SubscriptionRecord = {
  ...subscription,
  subscription_id: 'sub_archived_eur',
  price: 9,
  currency: 'EUR',
  monthly_price: 9,
  started_at: '2026-02-01',
  renew_at: '2026-04-01',
  note: 'second historical contract',
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
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [],
}

describe('ArchivePage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads archived VPS and subscriptions through archived scope and renders read-only history', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([archivedVPS]))
      .mockResolvedValueOnce(mockJSONResponse([subscription, eurSubscription]))
      .mockResolvedValueOnce(mockJSONResponse(archivedDetail))
      .mockResolvedValueOnce(mockJSONResponse([service]))
      .mockResolvedValueOnce(mockJSONResponse([domain]))
      .mockResolvedValueOnce(mockJSONResponse(timeline))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <ArchivePage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Retired').length).toBeGreaterThan(0))
    expect(screen.getByRole('heading', { name: '归档资产' })).toBeInTheDocument()
    expect(screen.getAllByText('历史订阅').length).toBeGreaterThan(0)
    expect(screen.getByText('USD 24.00/月 + EUR 9.00/月')).toBeInTheDocument()
    expect(screen.getAllByText('USD 24.00/月').length).toBeGreaterThan(0)
    expect(screen.getAllByText('EUR 9.00/月').length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.getByText('Tokyo Agent History')).toBeInTheDocument())
    expect(screen.getByText('Legacy API')).toBeInTheDocument()
    expect(screen.getByText('legacy.example.test')).toBeInTheDocument()
    expect(screen.getByText('network quality regression')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回 VPS' })).toHaveAttribute('href', '/vps')
    expect(screen.queryByRole('button', { name: /编辑|恢复|取消|添加|创建|关联/ })).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps?asset_scope=archived', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/subscriptions?sort=renew_at&order=asc&asset_scope=archived', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_archived', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_archived/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_archived/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_archived/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })
})
