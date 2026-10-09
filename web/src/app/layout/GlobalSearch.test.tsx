import { act, createEvent, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { GlobalSearch } from './GlobalSearch'
import * as api from '../../lib/api'
import { targetObservationFixture } from '../../lib/targetObservationFixture'
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
    lifecycle_status: 'active',
    run_status: '启用',
    group: 'prod-group',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: 1,
    observation_freshness: targetObservationFixture({
      target_id: 'tg_001',
      run_status: '启用',
      lifecycle_status: 'active',
      evaluated_at: '2026-04-30T08:00:00Z',
      enabled_probe_count: 1,
    }),
    matching_executor_count: 1,
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


function Pathname() {
  const pathname = useLocation().pathname
  return <div data-testid="pathname">{pathname}</div>
}

/** jsdom does not submit a form from Enter; a real field does unless the key is cancelled. */
function pressEnter(input: HTMLElement) {
  const event = createEvent.keyDown(input, { key: 'Enter' })
  fireEvent(input, event)
  if (!event.defaultPrevented) fireEvent.submit(input.closest('form')!)
}

describe('GlobalSearch', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue(mockVPS)
    vi.spyOn(api, 'listMonitoringInstances').mockResolvedValue(mockMonitoringInstances)
    vi.spyOn(api, 'listTargets').mockResolvedValue(mockTargets)
    vi.spyOn(api, 'listProviders').mockResolvedValue(mockProviders)
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue(mockSubscriptions)
    searchRecordsForGlobalSearch.mockReset()
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [], error: null })
    vi.mocked(api.listVPSAssets).mockClear()
    vi.mocked(api.listMonitoringInstances).mockClear()
    vi.mocked(api.listTargets).mockClear()
    vi.mocked(api.listProviders).mockClear()
    vi.mocked(api.listSubscriptions).mockClear()
  })

  it('advertises the keyboard shortcut and focuses the field on Ctrl+K', async () => {
    const { container } = render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    expect(input).toHaveAttribute('aria-keyshortcuts', 'Control+K Meta+K')
    expect(container.querySelector('.global-search__kbd')).toHaveTextContent(/K$/)
    expect(container.querySelector('.global-search__kbd')).toHaveAttribute('aria-hidden', 'true')

    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })
    await waitFor(() => expect(input).toHaveFocus())
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

  it('does not import or describe record search when records are disabled', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch recordsEnabled={false} />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    expect(input).toHaveAttribute('placeholder', '搜索 VPS、IP…')
    fireEvent.focus(input)
    expect(screen.getByText('VPS · 监控实例 · 入口探测 · 服务商 · 订阅')).toBeInTheDocument()
    expect(screen.queryByText(/运维记录/)).not.toBeInTheDocument()
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)
    await waitFor(() => expect(screen.getByText('Tokyo VPS')).toBeInTheDocument())
    expect(searchRecordsForGlobalSearch).not.toHaveBeenCalled()
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
    const vpsLink = screen.getByRole('option', { name: /Tokyo VPS(?! 的订阅)/ })
    expect(vpsLink).toHaveAttribute('href', '/vps/vps_001')
    expect(vpsLink.tagName).toBe('A')
    expect(screen.getByRole('option', { name: /Tokyo Edge/ })).toHaveAttribute('href', '/monitoring/mi_001')
    // 订阅结果以所属 VPS 命名、金额注明月折算，不再以 sub_ 内部 ID 当标题。
    const subscriptionOption = screen.getByRole('option', { name: /Tokyo VPS 的订阅/ })
    expect(subscriptionOption).toHaveAttribute('href', '/subscriptions?vps_id=vps_001&view=details')
    expect(subscriptionOption).toHaveTextContent('/月')
    expect(subscriptionOption).not.toHaveTextContent('sub_001')
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
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [recordHit], error: null })
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
    expect(screen.getByRole('option', { name: /Tokyo VPS(?! 的订阅)/ })).toBeInTheDocument()
  })

  it('offers the way through to the full records result set', async () => {
    searchRecordsForGlobalSearch.mockResolvedValue({
      matches: [
        recordHit,
        { id: '__all__', label: '查看全部匹配记录', hint: '磁盘', to: '/records?q=%E7%A3%81%E7%9B%98' },
      ],
      error: null,
    })
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
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [recordHit], error: null })
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
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [recordHit], error: null })
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument())
    expect(screen.getByRole('alert')).toHaveTextContent('资产搜索暂不可用')
    expect(screen.queryByText('inventory unavailable')).not.toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
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
    await waitFor(() => expect(releaseFirst).toBeTypeOf('function'))
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

    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.submit(input.closest('form')!)
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('请输入搜索关键词')
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
    expect(api.listVPSAssets).not.toHaveBeenCalled()
    expect(searchRecordsForGlobalSearch).not.toHaveBeenCalled()

    fireEvent.change(input, { target: { value: 'tokyo' } })
    expect(screen.queryByText(/请输入搜索关键词/)).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
    expect(api.listVPSAssets).not.toHaveBeenCalled()
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

    const firstOption = screen.getByRole('option', { name: /Tokyo VPS(?! 的订阅)/ })
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

  it('releases focus when the pointer goes down outside the search, but not inside it', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
        <p>页面空白</p>
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    input.focus()
    fireEvent.pointerDown(input)
    expect(input).toHaveFocus()
    // 触屏点空白处未必移走焦点：组件主动失焦，窄屏展开的搜索框随之收回。
    fireEvent.pointerDown(screen.getByText('页面空白'))
    expect(input).not.toHaveFocus()
  })

  it('drops the previous list as soon as the query changes and Enter searches the new text', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
        <Pathname />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)
    const blog = await screen.findByRole('option', { name: /Blog/ })
    expect(blog).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveAttribute('aria-activedescendant', blog.id)

    fireEvent.change(input, { target: { value: '不存在的新词' } })
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
    expect(screen.queryByText('Blog')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()

    pressEnter(input)
    expect(screen.getByTestId('pathname')).toHaveTextContent('/')
    await waitFor(() => expect(screen.getByText('没有匹配项')).toBeInTheDocument())
    expect(screen.queryByText('Blog')).not.toBeInTheDocument()
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/')
  })

  it('submits the edited query instead of activating the focused result from the previous one', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
        <Pathname />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)
    await screen.findByRole('option', { name: /Blog/ })

    fireEvent.change(input, { target: { value: 'hetzner' } })
    pressEnter(input)

    const first = await screen.findByRole('option', { name: /Tokyo VPS(?! 的订阅)/ })
    expect(first).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveAttribute('aria-activedescendant', first.id)
    expect(screen.getAllByRole('option').some((option) => option.getAttribute('href') === '/providers')).toBe(true)
    expect(screen.queryByRole('option', { name: /Blog/ })).not.toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/')
  })

  it('ignores a response for the query that was replaced before it returned', async () => {
    let releaseFirst: ((value: Awaited<ReturnType<typeof api.listVPSAssets>>) => void) | undefined
    vi.spyOn(api, 'listVPSAssets').mockImplementationOnce(
      () => new Promise((resolve) => { releaseFirst = resolve }),
    )
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)
    await waitFor(() => expect(releaseFirst).toEqual(expect.any(Function)))
    fireEvent.change(input, { target: { value: 'hetzner' } })

    expect(screen.queryByRole('option')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
    await act(async () => { releaseFirst?.(mockVPS) })
    expect(screen.queryByText('Blog')).not.toBeInTheDocument()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
  })

  it('shows a source as soon as it succeeds and keeps it when the other fails', async () => {
    let releaseRecords: ((value: { matches: typeof recordHit[]; error: string | null }) => void) | undefined
    searchRecordsForGlobalSearch.mockImplementation(
      () => new Promise((resolve) => { releaseRecords = resolve }),
    )
    vi.spyOn(api, 'listVPSAssets').mockRejectedValue(new Error('inventory unavailable'))
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('资产搜索暂不可用'))
    expect(screen.queryByText('inventory unavailable')).not.toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
    expect(screen.queryByText('东京节点磁盘 IO 抖动')).not.toBeInTheDocument()

    releaseRecords?.({ matches: [recordHit], error: null })
    await waitFor(() => expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument())
    expect(screen.getByRole('alert')).toHaveTextContent('资产搜索暂不可用')
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
  })

  it('keeps asset matches when record search fails later', async () => {
    let releaseRecords: ((reason?: unknown) => void) | undefined
    searchRecordsForGlobalSearch.mockImplementation(
      () => new Promise((_resolve, reject) => { releaseRecords = reject }),
    )
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getByText('Tokyo VPS')).toBeInTheDocument())
    expect(screen.getByText('正在加载…')).toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()

    releaseRecords?.(new Error('index offline'))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('运维记录搜索暂不可用'))
    expect(screen.getByText('Tokyo VPS')).toBeInTheDocument()
    expect(screen.queryByText('index offline')).not.toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
  })

  it('does not treat a failed source with no rows as an empty successful search', async () => {
    vi.spyOn(api, 'listVPSAssets').mockRejectedValue(new Error('inventory unavailable'))
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [], error: 'index offline raw' })
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'zzznever' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(screen.getAllByRole('alert')).toHaveLength(2))
    expect(screen.getByText('资产搜索暂不可用')).toBeInTheDocument()
    expect(screen.getByText('运维记录搜索暂不可用')).toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
    expect(screen.queryByText('inventory unavailable')).not.toBeInTheDocument()
    expect(screen.queryByText('index offline raw')).not.toBeInTheDocument()
  })

  it('distinguishes an unsubmitted query from a finished search that matched nothing', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'zzznever' } })
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
    expect(screen.queryByText('支持检索范围')).not.toBeInTheDocument()
    expect(api.listVPSAssets).not.toHaveBeenCalled()
    expect(searchRecordsForGlobalSearch).not.toHaveBeenCalled()

    fireEvent.submit(input.closest('form')!)
    await waitFor(() => expect(screen.getByText('没有匹配项')).toBeInTheDocument())
    expect(screen.queryByText('按 Enter 搜索')).not.toBeInTheDocument()

    fireEvent.change(input, { target: { value: 'zzznever!' } })
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')
  })

  it('clears a finished search when the field is emptied and does not restore it on focus', async () => {
    searchRecordsForGlobalSearch.mockResolvedValue({ matches: [recordHit], error: null })
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.focus(input)
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)
    await screen.findByText('Tokyo VPS')
    expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument()

    fireEvent.change(input, { target: { value: '' } })
    expect(screen.queryByText('Tokyo VPS')).not.toBeInTheDocument()
    expect(screen.queryByText('东京节点磁盘 IO 抖动')).not.toBeInTheDocument()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
    expect(screen.queryByText('没有匹配项')).not.toBeInTheDocument()

    const container = input.closest('.global-search')!
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    try {
      fireEvent.blur(container, { relatedTarget: outside })
      fireEvent.focus(input)
    } finally {
      document.body.removeChild(outside)
    }
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
    expect(screen.queryByText('Tokyo VPS')).not.toBeInTheDocument()
    expect(input).toHaveValue('')

    let releaseAssets: ((value: Awaited<ReturnType<typeof api.listVPSAssets>>) => void) | undefined
    vi.spyOn(api, 'listVPSAssets').mockImplementationOnce(
      () => new Promise((resolve) => { releaseAssets = resolve }),
    )
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)
    await waitFor(() => expect(releaseAssets).toEqual(expect.any(Function)))
    fireEvent.change(input, { target: { value: '' } })
    await act(async () => { releaseAssets?.(mockVPS) })
    expect(screen.queryByText('Blog')).not.toBeInTheDocument()
    expect(screen.getByText('支持检索范围')).toBeInTheDocument()
  })

  it('invalidates visible results when record search is turned off and ignores the late response', async () => {
    let releaseRecords: ((value: { matches: typeof recordHit[]; error: string | null }) => void) | undefined
    searchRecordsForGlobalSearch.mockImplementation(
      () => new Promise((resolve) => { releaseRecords = resolve }),
    )
    const view = render(
      <MemoryRouter>
        <GlobalSearch recordsEnabled />
      </MemoryRouter>,
    )
    const input = screen.getByRole('combobox', { name: '全局搜索' })
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)
    await screen.findByText('Tokyo VPS')
    expect(screen.getByText('正在加载…')).toBeInTheDocument()

    view.rerender(
      <MemoryRouter>
        <GlobalSearch recordsEnabled={false} />
      </MemoryRouter>,
    )
    expect(input).toHaveAttribute('placeholder', '搜索 VPS、IP…')
    expect(screen.queryByText('Tokyo VPS')).not.toBeInTheDocument()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('按 Enter 搜索')

    expect(releaseRecords).toEqual(expect.any(Function))
    await act(async () => { releaseRecords?.({ matches: [recordHit], error: null }) })
    expect(screen.queryByText('东京节点磁盘 IO 抖动')).not.toBeInTheDocument()
    expect(screen.queryByText('Tokyo VPS')).not.toBeInTheDocument()

    searchRecordsForGlobalSearch.mockClear()
    fireEvent.submit(input.closest('form')!)
    await screen.findByText('Tokyo VPS')
    expect(searchRecordsForGlobalSearch).not.toHaveBeenCalled()
    expect(screen.queryByText(/运维记录/)).not.toBeInTheDocument()
  })
})
