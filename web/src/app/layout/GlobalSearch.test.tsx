import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { GlobalSearch } from './GlobalSearch'
import * as api from '../../lib/api'
import type { GlobalRecordSearchHit } from '../../pages/records/globalRecordSearch'

const searchRecordsForGlobalSearch = vi.hoisted(() => vi.fn())

vi.mock('../../pages/records/globalRecordSearch', () => ({ searchRecordsForGlobalSearch }))

const recordHit: GlobalRecordSearchHit = {
  id: 'rec_001',
  label: '东京节点磁盘 IO 抖动',
  hint: '排障 · 排查中 · Tokyo Edge',
  to: '/records/rec_001',
}

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
    searchRecordsForGlobalSearch.mockReset()
    searchRecordsForGlobalSearch.mockResolvedValue([])
  })

  it('reveals search capabilities via UI when focused with empty query', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.focus(input)
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
    expect(screen.getByText(/VPS · 监控实例 · 入口探测 · 服务商 · 订阅 · 运维记录/)).toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
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

    expect(screen.getAllByText('VPS').length).toBeGreaterThan(0)
    expect(screen.getAllByText('监控实例').length).toBeGreaterThan(0)
    const vpsLink = screen.getByRole('option', { name: /Tokyo VPS/ })
    expect(vpsLink).toHaveAttribute('href', '/vps/vps_001')
    expect(vpsLink.tagName).toBe('A')
    expect(screen.getByRole('option', { name: /Tokyo Edge/ })).toHaveAttribute('href', '/monitoring/mi_001')
    expect(screen.getByRole('option', { name: /sub_001/ })).toHaveAttribute('href', '/subscriptions?vps_id=vps_001&view=details')
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

  it('groups records beside the assets and links each hit to its record', async () => {
    searchRecordsForGlobalSearch.mockResolvedValue([recordHit])
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: ' Tokyo ' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument())
    // The palette lowercases for its own client-side matching; records are matched
    // by the server, so the typed text has to reach it unchanged.
    expect(searchRecordsForGlobalSearch).toHaveBeenCalledWith('Tokyo', 4)
    expect(screen.getAllByText('运维记录').length).toBeGreaterThan(0)
    expect(screen.getByRole('option', { name: /东京节点磁盘 IO 抖动/ }))
      .toHaveAttribute('href', '/records/rec_001')
    expect(screen.getByRole('option', { name: /Tokyo VPS/ })).toBeInTheDocument()
  })

  it('offers the way through to the full records result set', async () => {
    searchRecordsForGlobalSearch.mockResolvedValue([
      recordHit,
      { id: '__all__', label: '查看全部匹配记录', hint: '磁盘', to: '/records?q=%E7%A3%81%E7%9B%98' },
    ])
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: '磁盘' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('查看全部匹配记录')).toBeInTheDocument())
    expect(screen.getByRole('option', { name: /查看全部匹配记录/ }))
      .toHaveAttribute('href', '/records?q=%E7%A3%81%E7%9B%98')
  })

  it('finds a record when no asset matches the query', async () => {
    searchRecordsForGlobalSearch.mockResolvedValue([recordHit])
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: '磁盘' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument())
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
  })

  it('reports an asset failure without hiding the records that did answer', async () => {
    vi.spyOn(api, 'listVPSAssets').mockRejectedValue(new Error('inventory unavailable'))
    searchRecordsForGlobalSearch.mockResolvedValue([recordHit])
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument())
    expect(screen.getByText('inventory unavailable')).toBeInTheDocument()
  })

  it('ignores an earlier search that resolves after a later one', async () => {
    let releaseFirst: ((value: Awaited<ReturnType<typeof api.listVPSAssets>>) => void) | undefined
    vi.spyOn(api, 'listVPSAssets')
      .mockImplementationOnce(() => new Promise((resolve) => { releaseFirst = resolve }))
      .mockResolvedValue(mockVPS)
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)
    fireEvent.change(input, { target: { value: 'hetzner' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    releaseFirst?.(mockVPS)

    await waitFor(() => expect(screen.queryByText('Blog')).not.toBeInTheDocument())
    expect(screen.getByText('Hetzner')).toBeInTheDocument()
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

  it('exposes an accessible combobox contract and presents empty capabilities outside a listbox', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(input).toHaveAttribute('aria-haspopup', 'listbox')
    expect(input).toHaveAttribute('aria-autocomplete', 'list')
    expect(input).not.toHaveAttribute('aria-controls')
    expect(input).not.toHaveAttribute('aria-activedescendant')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    fireEvent.focus(input)
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(input).not.toHaveAttribute('aria-controls')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    const describedById = input.getAttribute('aria-describedby')
    expect(describedById).toBeTruthy()
    const helpElem = document.getElementById(describedById!)
    expect(helpElem).toHaveTextContent('支持检索范围')
    expect(helpElem).toHaveTextContent(/VPS · 监控实例 · 入口探测 · 服务商 · 订阅 · 运维记录/)
  })

  it('keeps capabilities open with sensible guidance on empty submit instead of dismissing', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.focus(input)
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()

    fireEvent.submit(input.closest('form')!)
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('请输入搜索关键词')

    fireEvent.change(input, { target: { value: 'tokyo' } })
    expect(screen.queryByText(/请输入搜索关键词/)).not.toBeInTheDocument()
  })

  it('closes popup and clears painted help when focus leaves the search widget (focusleave)', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.focus(input)
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()

    const container = input.closest('.global-search')!
    const outsideElement = document.createElement('button')
    document.body.appendChild(outsideElement)
    try {
      fireEvent.blur(container, { relatedTarget: outsideElement })
      expect(input).toHaveAttribute('aria-expanded', 'false')
      expect(screen.queryByText('支持检索范围')).not.toBeInTheDocument()
    } finally {
      document.body.removeChild(outsideElement)
    }
  })

  it('announces active keyboard option via aria-activedescendant and navigates with Arrow keys and Enter', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('Tokyo VPS')).toBeInTheDocument()
    })

    const listbox = screen.getByRole('listbox', { name: '搜索结果' })
    expect(input).toHaveAttribute('aria-controls', listbox.id)

    const firstOption = screen.getByRole('option', { name: /Tokyo VPS/ })
    const secondOption = screen.getByRole('option', { name: /Tokyo Edge/ })

    // Initially first item is selected and announced
    expect(firstOption).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveAttribute('aria-activedescendant', firstOption.id)

    // Navigate down
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(firstOption).toHaveAttribute('aria-selected', 'false')
    expect(secondOption).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveAttribute('aria-activedescendant', secondOption.id)

    // Navigate up returns to first
    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(firstOption).toHaveAttribute('aria-selected', 'true')
    expect(secondOption).toHaveAttribute('aria-selected', 'false')
    expect(input).toHaveAttribute('aria-activedescendant', firstOption.id)

    // Press Enter to activate
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

})
