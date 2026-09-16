import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ArchivePage } from './ArchivePage'
import type { SubscriptionRecord, VPSAssetRecord } from '../lib/types'

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

describe('ArchivePage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads archived VPS and subscriptions through historical scope and renders a list-only archive ledger', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([archivedVPS]))
      .mockResolvedValueOnce(mockJSONResponse([subscription, eurSubscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <ArchivePage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Retired').length).toBeGreaterThan(0))
    expect(screen.getByRole('heading', { name: '归档资产' })).toBeInTheDocument()
    expect(screen.getByText('USD 24.00/月 + EUR 9.00/月')).toBeInTheDocument()
    const row = screen.getByText('Tokyo Retired').closest('tr')
    expect(row).not.toBeNull()
    expect(within(row!).getByRole('link', { name: '查看归档详情' })).toHaveAttribute('href', '/archive/vps_archived')
    expect(screen.getByRole('link', { name: '返回 VPS' })).toHaveAttribute('href', '/vps')
    expect(screen.queryByRole('button', { name: /编辑|恢复|取消|添加|创建|关联/ })).not.toBeInTheDocument()
    expect(screen.queryByText('Tokyo Agent History')).not.toBeInTheDocument()
    expect(screen.queryByText('Legacy API')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps?asset_scope=historical', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/subscriptions?sort=renew_at&order=asc&asset_scope=historical', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('preserves VPS inventory and allows retry when subscriptions request fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([archivedVPS]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscription backend unavailable' }, 500))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <ArchivePage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Retired').length).toBeGreaterThan(0))
    expect(screen.getByText(/历史订阅数据加载失败/)).toBeInTheDocument()
    expect(screen.queryByText('暂无月成本')).not.toBeInTheDocument()

    fetchMock.mockResolvedValueOnce(mockJSONResponse([subscription]))
    const retryButton = screen.getByRole('button', { name: '重试加载订阅' })
    fireEvent.click(retryButton)

    await waitFor(() => expect(screen.getByText('USD 24.00/月')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions?sort=renew_at&order=asc&asset_scope=historical', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('handles VPS load failure with retry button without clearing page error until recovered', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'database failure' }, 500))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <ArchivePage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: '归档资产加载失败' })).toBeInTheDocument())
    const retryBtn = screen.getByRole('button', { name: '重试加载资产' })
    expect(retryBtn).toBeInTheDocument()

    fetchMock.mockResolvedValueOnce(mockJSONResponse([archivedVPS]))
    fireEvent.click(retryBtn)

    await waitFor(() => expect(screen.getByText('Tokyo Retired')).toBeInTheDocument())
  })
})
