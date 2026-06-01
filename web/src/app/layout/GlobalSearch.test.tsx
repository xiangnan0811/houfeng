import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { GlobalSearch } from './GlobalSearch'
import * as api from '../../lib/api'

const mockVPS = [
  {
    vps_id: 'vps_001',
    display_name: 'Tokyo VPS',
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
    ssh_host: 'tokyo.example.com',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'kvm',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: ['edge'],
    note: '',
    active_monitoring_instance_link_count: 1,
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    archived_at: null,
  },
] as Awaited<ReturnType<typeof api.listVPSAssets>>

const mockMonitoringInstances = [
  {
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'aws',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    group: 'edge-group',
    labels: ['edge'],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-30T08:00:00Z',
    last_sync_at: '2026-04-30T08:00:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-30T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listMonitoringInstances>>

const mockTargets = [
  {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service' as const,
    host: 'blog.example.com',
    base_port: 443,
    execution_monitoring_instance_labels: ['edge'],
    run_status: '启用',
    group: 'prod-group',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: undefined,
    last_failure_at: undefined,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-30T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listTargets>>

const mockProviders = [
  {
    provider_id: 'pv_001',
    name: 'Hetzner',
    website: '',
    panel_url: '',
    account_hint: 'main account',
    country: 'DE',
    note: '',
    rating: null,
    labels: [],
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listProviders>>

const mockSubscriptions = [
  {
    subscription_id: 'sub_001',
    vps_id: 'vps_001',
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 12,
    started_at: '2026-05-01',
    renew_at: '2026-05-20',
    auto_renew: true,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: 'card',
    note: 'tokyo renewal',
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listSubscriptions>>

describe('GlobalSearch', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue(mockVPS)
    vi.spyOn(api, 'listMonitoringInstances').mockResolvedValue(mockMonitoringInstances)
    vi.spyOn(api, 'listTargets').mockResolvedValue(mockTargets)
    vi.spyOn(api, 'listProviders').mockResolvedValue(mockProviders)
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue(mockSubscriptions)
  })

  it('renders the search input', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    expect(screen.getByLabelText('全局搜索')).toBeInTheDocument()
  })

  it('matches records across assets and observation objects with grouped links', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('Tokyo VPS')).toBeInTheDocument()
    })

    expect(api.listSubscriptions).toHaveBeenCalledWith({ sort: 'renew_at', order: 'asc' })
    expect(screen.getAllByText('VPS').length).toBeGreaterThan(0)
    expect(screen.getAllByText('监控实例').length).toBeGreaterThan(0)
    const vpsLink = screen.getByRole('option', { name: /Tokyo VPS/ })
    expect(vpsLink).toHaveAttribute('href', '/vps/vps_001')
    expect(vpsLink.tagName).toBe('A')
    expect(screen.getByRole('option', { name: /Tokyo Edge/ })).toHaveAttribute('href', '/monitoring/mi_001')
    expect(screen.getByRole('option', { name: /sub_001/ })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001')
  })

  it('matches a target by host', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('Blog')).toBeInTheDocument()
    })
    expect(screen.getByRole('option', { name: /Blog/ })).toHaveAttribute('href', '/targets/tg_001')
  })

  it('matches a provider by name', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'hetzner' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expect(
      screen.getAllByRole('option').find((option) => option.getAttribute('href') === '/providers'),
    ).toBeInTheDocument()
  })

  it('shows "没有匹配项" when nothing matches', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'zzznever' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('没有匹配项')).toBeInTheDocument()
    })
  })
})
