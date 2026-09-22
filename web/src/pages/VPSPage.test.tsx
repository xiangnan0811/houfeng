import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSPage } from './VPSPage'

function LocationProbe() {
  const location = useLocation()
  return (
    <span data-testid="location" data-state={JSON.stringify(location.state)}>
      {location.pathname}{location.search}
    </span>
  )
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const provider = {
  provider_id: 'pv_001',
  name: 'Hetzner',
  website: '',
  panel_url: '',
  account_hint: '',
  country: 'DE',
  note: '',
  rating: null,
  labels: [],
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

const vps = {
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
  note: '',
  active_monitoring_instance_link_count: 1,
  running_monitoring_instance_count: 0,
  running_target_count: 0,
  ip_quality_summary: {
    vps_id: 'vps_001',
    observed_at: '2026-06-08T12:00:00Z',
    ip_address: '192.0.2.1',
    ip_version: 4,
    status: 'success',
    risk_level: 'low',
    use_region_code: 'JP',
    use_region_name: 'Japan',
    asn: 'AS64500',
    organization: 'Example Transit',
    stale: false,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: 2,
    unlockable_count: 1,
  },
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
}

const missingFactsVPS = {
  ...vps,
  vps_id: 'vps_missing',
  display_name: 'Osaka Missing',
  provider_id: null,
  provider_name: '',
  product_name: '',
  country: '',
  region: '',
  city: '',
  ipv4: '',
  ssh_host: '',
  usage_status: 'unknown',
  renewal_decision: 'unreviewed',
  active_monitoring_instance_link_count: 0,
  running_monitoring_instance_count: 0,
  running_target_count: 0,
  ip_quality_summary: null,
}

const subscription = {
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
  note: '',
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

describe('VPSPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  function mockInventory(rows: unknown[] = [vps, missingFactsVPS], subscriptions: unknown[] = [subscription]) {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/vps' && !init?.method) return mockJSONResponse(rows)
      if (url === '/api/providers') return mockJSONResponse([provider])
      if (url.startsWith('/api/subscriptions?')) return mockJSONResponse(subscriptions)
      throw new Error('Unexpected request: ' + url)
    })
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  function mount(entry = '/vps') {
    return render(
      <MemoryRouter initialEntries={[entry]}>
        <LocationProbe />
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
          <Route path="/vps/:vpsId" element={<h1>VPS 管理详情</h1>} />
        </Routes>
      </MemoryRouter>,
    )
  }

  function currentQuery() {
    const location = screen.getByTestId('location').textContent ?? ''
    return new URLSearchParams(location.split('?')[1])
  }

  it.each([
    { name: 'missing subscription', body: [], status: 200, fact: '无订阅', missing: true },
    { name: 'subscription without a renewal date', body: [{ ...subscription, renew_at: '' }], status: 200, fact: '无续费日', missing: false },
    { name: 'subscription request failure', body: { error: 'subscription backend unavailable' }, status: 503, fact: '加载失败', missing: false },
  ])('distinguishes pending renewal evidence from $name in the row and accordion', async ({ body, status, fact, missing }) => {
    let resolveSubscriptions!: (response: Response) => void
    const pending = new Promise<Response>((resolve) => { resolveSubscriptions = resolve })
    const fetchMock = mockInventory([vps], [])
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((input, init) => String(input).startsWith('/api/subscriptions?')
      ? pending
      : original(input, init))
    mount('/vps?workspace=workbench')
    fireEvent.click(await screen.findByRole('button', { name: '选择 Tokyo Edge' }))
    const row = screen.getByRole('row', { name: /Tokyo Edge/ })
    const accordion = screen.getByRole('region', { name: 'VPS 快速查看' })
    expect(row).toHaveTextContent(/加载/)
    expect(accordion).toHaveTextContent(/加载/)
    expect(screen.getByRole('button', { name: '缺订阅' })).toBeInTheDocument()

    resolveSubscriptions(mockJSONResponse(body, status))
    await waitFor(() => expect(row).toHaveTextContent(fact))
    expect(accordion).toHaveTextContent(fact)
    expect(screen.getByRole('button', { name: missing ? '缺订阅 1' : '缺订阅' })).toBeInTheDocument()
  })

  it('keeps the selected asset, search, filters and unrelated URL context when switching workspaces', async () => {
    mockInventory()
    mount('/vps?workspace=workbench&provider_id=pv_001&q=Tokyo&selected=vps_001&source=renewals')
    await screen.findByRole('button', { name: '选择 Tokyo Edge' })
    fireEvent.click(screen.getByRole('button', { name: '目录视图' }))
    const inspector = screen.getByRole('region', { name: 'VPS 检查器' })
    expect(within(inspector).getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('Tokyo')
    expect(screen.queryByRole('button', { name: '选择 Osaka Missing' })).not.toBeInTheDocument()
    expect(currentQuery().get('workspace')).toBe('ledger')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('source')).toBe('renewals')
    expect(currentQuery().get('provider_id')).toBe('pv_001')
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    const drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('用途状态'), { target: { value: 'in_use' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))
    expect(currentQuery().get('q')).toBe('Tokyo')
    expect(currentQuery().get('workspace')).toBe('ledger')
    expect(currentQuery().get('source')).toBe('renewals')
    fireEvent.click(screen.getByRole('button', { name: '表格视图' }))
    expect(screen.getByRole('button', { name: '选择 Tokyo Edge' })).toHaveAttribute('aria-pressed', 'true')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('usage_status')).toBe('in_use')
  })

  it('remembers the workspace on a later visit while an explicit URL wins over the preference', async () => {
    mockInventory()
    const first = mount()
    expect(await screen.findByRole('button', { name: '目录视图' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: '表格视图' }))
    first.unmount()
    const second = mount()
    expect(await screen.findByRole('button', { name: '表格视图' })).toHaveAttribute('aria-pressed', 'true')
    second.unmount()
    mount('/vps?workspace=ledger')
    expect(await screen.findByRole('button', { name: '目录视图' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('selects an asset without navigating and keeps canonical monitoring onboarding available in both workspaces', async () => {
    mockInventory()
    mount('/vps?workspace=ledger')
    fireEvent.click(await screen.findByRole('button', { name: '选择 Tokyo Edge' }))
    expect(currentQuery().get('selected')).toBe('vps_001')
    fireEvent.click(screen.getByRole('button', { name: /未关联/ }))
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '选择 Osaka Missing' }))
    expect(screen.getByRole('link', { name: '打开 VPS 详情' })).toHaveAttribute('href', '/vps/vps_missing?workbench=monitoring')
    fireEvent.click(screen.getByRole('button', { name: '表格视图' }))
    fireEvent.click(screen.getByRole('button', { name: '选择 Osaka Missing' }))
    const workbenchDetail = screen.getByRole('link', { name: '打开 VPS 详情' })
    expect(workbenchDetail).toHaveAttribute('href', '/vps/vps_missing?workbench=monitoring')
    fireEvent.click(workbenchDetail)
    expect(await screen.findByRole('heading', { name: 'VPS 管理详情' })).toBeInTheDocument()
    expect(currentQuery().get('workbench')).toBe('monitoring')
  })

  it('does not show a stale selected asset when a search has no matches', async () => {
    mockInventory()
    mount('/vps?workspace=ledger&selected=vps_001')
    await screen.findByRole('button', { name: '选择 Tokyo Edge' })
    fireEvent.change(screen.getByRole('searchbox', { name: '搜索 VPS' }), { target: { value: 'no-matching-host' } })
    expect(screen.queryByRole('link', { name: '打开 VPS 详情' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Tokyo Edge' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '表格视图' }))
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('no-matching-host')
  })

  it('keeps inventory usable without treating unavailable subscription evidence as missing', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/api/vps') return mockJSONResponse([missingFactsVPS])
      if (String(input) === '/api/providers') return mockJSONResponse([provider])
      return mockJSONResponse({ error: 'subscription database unavailable' }, 500)
    }))
    mount('/vps?workspace=ledger')
    await screen.findByRole('button', { name: '选择 Osaka Missing' })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('subscription database unavailable'))
    fireEvent.click(screen.getByRole('button', { name: '缺订阅' }))
    expect(screen.queryByRole('button', { name: '选择 Osaka Missing' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '订阅证据不可用' })).toBeInTheDocument()
    expect(screen.queryByText('当前筛选没有匹配 VPS')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /移除筛选 视图/ }))
    expect(await screen.findByRole('button', { name: '选择 Osaka Missing' })).toBeInTheDocument()
  })

  it('waits for subscription evidence before including assets in the missing-subscription view', async () => {
    let resolveSubscriptions!: (value: Response) => void
    const pending = new Promise<Response>((resolve) => { resolveSubscriptions = resolve })
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input) === '/api/vps') return Promise.resolve(mockJSONResponse([missingFactsVPS]))
      if (String(input) === '/api/providers') return Promise.resolve(mockJSONResponse([provider]))
      return pending
    }))
    mount('/vps?workspace=ledger&view=missing_subscription')
    expect(await screen.findByRole('heading', { name: '正在加载订阅证据…' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Osaka Missing' })).not.toBeInTheDocument()
    expect(screen.queryByText('当前筛选没有匹配 VPS')).not.toBeInTheDocument()
    expect(screen.queryByText('还没有录入 VPS 资产')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '缺订阅' })).toBeInTheDocument()
    resolveSubscriptions(mockJSONResponse([]))
    expect(await screen.findByRole('button', { name: '选择 Osaka Missing' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '缺订阅 1' })).toBeInTheDocument()
  })

  it('keeps cancellation attention based on running links and excludes final lifecycle assets', async () => {
    const retiring = { ...vps, lifecycle_status: 'to_cancel', renewal_decision: 'cancel', active_monitoring_instance_link_count: 2 }
    const running = { ...retiring, vps_id: 'running', display_name: 'Running Target', running_target_count: 1 }
    const cancelled = { ...running, vps_id: 'cancelled', display_name: 'Cancelled Target', lifecycle_status: 'cancelled' }
    const archived = { ...running, vps_id: 'archived', display_name: 'Archived Target', lifecycle_status: 'archived' }
    mockInventory([retiring, running, cancelled, archived], [
      { ...subscription, status: 'cancelled', auto_renew: false, auto_renew_cancelled: true },
      { ...subscription, subscription_id: 'sub_running', vps_id: 'running', status: 'cancelled', auto_renew: false, auto_renew_cancelled: true },
    ])
    mount('/vps?workspace=ledger&view=cancellation_attention')
    expect(await screen.findByRole('button', { name: '选择 Running Target' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Cancelled Target' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Archived Target' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '表格视图' }))
    expect(screen.getByRole('button', { name: '选择 Running Target' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Cancelled Target' })).not.toBeInTheDocument()
  })

  it('does not apply draft filters when the dialog is dismissed', async () => {
    mockInventory()
    mount('/vps?workspace=ledger&view=unlinked&source=onboarding')
    await screen.findByRole('button', { name: '选择 Osaka Missing' })
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    const drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('生命周期'), { target: { value: 'testing' } })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'VPS 高级筛选' })).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '选择 Osaka Missing' })).toBeInTheDocument()
    expect(currentQuery().get('lifecycle_status')).toBeNull()
    expect(currentQuery().get('source')).toBe('onboarding')
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    const reopened = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    expect(within(reopened).getByLabelText('生命周期')).not.toHaveValue('testing')
  })

  it('creates a VPS through the shared modal and opens its canonical detail with authored inputs', async () => {
    const fetchMock = mockInventory([], [])
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/vps' && init?.method === 'POST') return mockJSONResponse({ ...vps, vps_id: 'vps_new' }, 201)
      if (String(input) === '/api/providers') return mockJSONResponse([provider])
      return mockJSONResponse([])
    })
    mount('/vps?workspace=ledger')
    fireEvent.click(await screen.findByRole('button', { name: '创建第一台 VPS' }))
    const modal = await screen.findByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Osaka Standby' } })
    fireEvent.change(within(modal).getByLabelText('资产服务商'), { target: { value: 'pv_001' } })
    fireEvent.change(within(modal).getByRole('combobox', { name: '国家 / 地区' }), { target: { value: 'JP' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))
    expect(await screen.findByRole('heading', { name: 'VPS 管理详情' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_new')
    expect(screen.getByTestId('location')).toHaveAttribute(
      'data-state',
      JSON.stringify({ vpsInventoryHref: '/vps?workspace=ledger' }),
    )
    const postCall = fetchMock.mock.calls.find(
      ([input, init]) => String(input) === '/api/vps' && init?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const payload = JSON.parse(String(postCall?.[1]?.body))
    expect(payload).toMatchObject({
      display_name: 'Osaka Standby',
      provider_id: 'pv_001',
      country: 'JP',
      ipv4: '203.0.113.8',
    })
  })

  it('shows honest provider catalog unavailability in create without blocking unassociated', async () => {
    const fetchMock = mockInventory()
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input) === '/api/providers') return mockJSONResponse({ error: 'provider catalog down' }, 503)
      return original(input, init)
    })
    mount('/vps?workspace=ledger')
    fireEvent.click(await screen.findByRole('button', { name: '添加 VPS' }))
    const modal = await screen.findByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByText(/服务商不可用：/)).toBeInTheDocument()
    expect(within(modal).getByLabelText('资产服务商')).toHaveValue('')
    expect(within(modal).getByRole('combobox', { name: '资产服务商' })).not.toBeDisabled()
    expect(within(modal).getByRole('button', { name: '新建服务商' })).toBeInTheDocument()
  })

  it('disables the provider catalog in create while providers are still loading', async () => {
    let resolveProviders!: (response: Response) => void
    const pending = new Promise<Response>((resolve) => { resolveProviders = resolve })
    const fetchMock = mockInventory()
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((input, init) => (
      String(input) === '/api/providers' ? pending : original(input, init)
    ))
    mount('/vps?workspace=ledger')
    fireEvent.click(await screen.findByRole('button', { name: '添加 VPS' }))
    const modal = await screen.findByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByRole('combobox', { name: '资产服务商' })).toBeDisabled()
    expect(within(modal).getByText('正在读取服务商…')).toBeInTheDocument()
    resolveProviders(mockJSONResponse([provider]))
    await waitFor(() => expect(within(modal).getByRole('combobox', { name: '资产服务商' })).not.toBeDisabled())
  })

  it('does not infer healthy observations from stable business facts', async () => {
    mockInventory([
      { ...vps, ip_quality_summary: null },
      { ...vps, vps_id: 'vps_risk', display_name: 'Risky Edge', ip_quality_summary: { ...vps.ip_quality_summary, risk_level: 'high' } },
    ], [])
    mount('/vps?workspace=workbench')
    fireEvent.click(await screen.findByRole('button', { name: '选择 Tokyo Edge' }))
    const first = screen.getByRole('region', { name: 'VPS 快速查看' })
    const firstEvidence = within(first).getByText('观察证据').parentElement!
    expect(firstEvidence).toHaveTextContent('未采集')
    expect(firstEvidence).not.toHaveTextContent('正常')

    fireEvent.click(screen.getByRole('button', { name: '选择 Risky Edge' }))
    const second = screen.getByRole('region', { name: 'VPS 快速查看' })
    const secondEvidence = within(second).getByText('观察证据').parentElement!
    expect(secondEvidence).toHaveTextContent('高风险')
    expect(secondEvidence).not.toHaveTextContent('正常')
    expect(screen.getAllByRole('region', { name: 'VPS 快速查看' })).toHaveLength(1)
  })

  it('opens one inline accordion after the selected workbench row and keeps the selected URL when closed', async () => {
    mockInventory()
    mount('/vps?workspace=workbench&selected=vps_001&source=inventory-flow')
    const pick = await screen.findByRole('button', { name: '选择 Tokyo Edge' })
    expect(pick).toHaveAttribute('aria-pressed', 'true')
    expect(pick).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('region', { name: 'VPS 快速查看' })).not.toBeInTheDocument()

    fireEvent.click(pick)
    const accordion = screen.getByRole('region', { name: 'VPS 快速查看' })
    expect(pick).toHaveAttribute('aria-expanded', 'true')
    expect(pick).toHaveAttribute('aria-controls', 'vps-accordion-vps_001')
    expect(accordion).toHaveAttribute('id', 'vps-accordion-vps_001')
    expect(accordion.compareDocumentPosition(pick.closest('tr')!)).toBe(Node.DOCUMENT_POSITION_PRECEDING)
    expect(within(accordion).getByText('资产身份')).toBeInTheDocument()
    expect(within(accordion).getByText('经营与续费')).toBeInTheDocument()
    expect(within(accordion).getByText('监控关联')).toBeInTheDocument()
    expect(within(accordion).getByText('观察证据')).toBeInTheDocument()

    fireEvent.click(pick)
    expect(screen.queryByRole('region', { name: 'VPS 快速查看' })).not.toBeInTheDocument()
    expect(pick).toHaveAttribute('aria-pressed', 'true')
    expect(pick).toHaveAttribute('aria-expanded', 'false')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('source')).toBe('inventory-flow')

    fireEvent.click(screen.getByRole('button', { name: '选择 Osaka Missing' }))
    const moved = screen.getByRole('region', { name: 'VPS 快速查看' })
    expect(screen.getAllByRole('region', { name: 'VPS 快速查看' })).toHaveLength(1)
    expect(currentQuery().get('selected')).toBe('vps_missing')
    const detailLink = within(moved).getByRole('link', { name: '打开 VPS 详情' })
    expect(detailLink).toHaveAttribute('href', '/vps/vps_missing')
    fireEvent.click(detailLink)
    expect(await screen.findByRole('heading', { name: 'VPS 管理详情' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveAttribute(
      'data-state',
      JSON.stringify({ vpsInventoryHref: '/vps?workspace=workbench&selected=vps_missing&source=inventory-flow' }),
    )
  })

  it('lets the workbench row toggle the accordion without becoming a tab stop', async () => {
    mockInventory()
    mount('/vps?workspace=workbench')
    const name = await screen.findByRole('button', { name: '选择 Tokyo Edge' })
    expect(name).toHaveTextContent('Tokyo Edge')
    expect(name.tagName).toBe('BUTTON')
    const row = name.closest('tr')!
    expect(row).not.toHaveAttribute('tabindex')
    fireEvent.click(within(row).getAllByRole('cell')[1]!)
    expect(screen.getByRole('region', { name: 'VPS 快速查看' })).toBeInTheDocument()
    expect(currentQuery().get('selected')).toBe('vps_001')
    fireEvent.click(within(row).getAllByRole('cell')[1]!)
    expect(screen.queryByRole('region', { name: 'VPS 快速查看' })).not.toBeInTheDocument()
    expect(currentQuery().get('selected')).toBe('vps_001')
  })

  it('keeps in-progress CJK search text and unknown query params', async () => {
    mockInventory()
    mount('/vps?workspace=workbench&selected=vps_001&source=review')
    const search = await screen.findByRole('searchbox', { name: '搜索 VPS' })
    fireEvent.change(search, { target: { value: '东' } })
    expect(search).toHaveValue('东')
    expect(currentQuery().get('q')).toBe('东')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('source')).toBe('review')
    fireEvent.change(search, { target: { value: '东京 Tokyo' } })
    expect(search).toHaveValue('东京 Tokyo')
    expect(currentQuery().get('q')).toBe('东京 Tokyo')
    expect(currentQuery().get('source')).toBe('review')
  })

  it('respects hidden selection state in workbench without displaying stale facts', async () => {
    mockInventory()
    mount('/vps?workspace=workbench&selected=vps_001')
    fireEvent.click(await screen.findByRole('button', { name: '选择 Tokyo Edge' }))
    expect(screen.getByRole('region', { name: 'VPS 快速查看' })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('searchbox', { name: '搜索 VPS' }), { target: { value: 'no-match' } })
    expect(screen.queryByRole('region', { name: 'VPS 快速查看' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '打开 VPS 详情' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    expect(currentQuery().get('selected')).toBe('vps_001')
  })
  it('includes an active VPS with inactive billing in cancellation attention', async () => {
    mockInventory([vps], [{ ...subscription, status: 'expired', auto_renew: false, auto_renew_cancelled: true }])
    mount('/vps?workspace=ledger&view=cancellation_attention')
    expect(await screen.findByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
  })

  it('keeps VPS inventory usable when providers fail and retries without inventing providers', async () => {
    let providerCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/vps') return mockJSONResponse([vps])
      if (url === '/api/providers') {
        providerCalls += 1
        if (providerCalls === 1) return mockJSONResponse({ error: 'provider catalog unavailable' }, 503)
        return mockJSONResponse([provider])
      }
      if (url.startsWith('/api/subscriptions?')) return mockJSONResponse([subscription])
      throw new Error('Unexpected request: ' + url)
    }))
    mount('/vps?workspace=ledger&q=Tokyo&selected=vps_001&source=providers')
    expect(await screen.findByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('provider catalog unavailable'))
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    const drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    expect(within(drawer).queryByRole('option', { name: 'Hetzner' })).not.toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'VPS 高级筛选' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.queryByText('provider catalog unavailable')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
    expect(currentQuery().get('q')).toBe('Tokyo')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('source')).toBe('providers')
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    const reopened = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    expect(within(reopened).getByRole('option', { name: 'Hetzner' })).toBeInTheDocument()
  })

  it('retries inventory errors without dropping query, view or selection', async () => {
    let vpsCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/vps') {
        vpsCalls += 1
        if (vpsCalls === 1) return mockJSONResponse({ error: 'vps store unavailable' }, 500)
        return mockJSONResponse([missingFactsVPS])
      }
      if (url === '/api/providers') return mockJSONResponse([provider])
      if (url.startsWith('/api/subscriptions?')) return mockJSONResponse([])
      throw new Error('Unexpected request: ' + url)
    }))
    mount('/vps?workspace=workbench&view=unlinked&q=Osaka&selected=vps_missing&source=review')
    expect(await screen.findByRole('heading', { name: 'VPS 库存不可用' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('Osaka')
    fireEvent.click(within(screen.getByRole('alert')).getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('button', { name: '选择 Osaka Missing' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('Osaka')
    expect(currentQuery().get('workspace')).toBe('workbench')
    expect(currentQuery().get('view')).toBe('unlinked')
    expect(currentQuery().get('q')).toBe('Osaka')
    expect(currentQuery().get('selected')).toBe('vps_missing')
    expect(currentQuery().get('source')).toBe('review')
  })

  it('retries subscription errors without dropping query, view or selection', async () => {
    let subscriptionCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/vps') return mockJSONResponse([vps])
      if (url === '/api/providers') return mockJSONResponse([provider])
      if (url.startsWith('/api/subscriptions?')) {
        subscriptionCalls += 1
        if (subscriptionCalls === 1) return mockJSONResponse({ error: 'subscription database unavailable' }, 500)
        return mockJSONResponse([subscription])
      }
      throw new Error('Unexpected request: ' + url)
    }))
    mount('/vps?workspace=ledger&q=Tokyo&selected=vps_001&source=keep')
    expect(await screen.findByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('subscription database unavailable'))
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.queryByText('subscription database unavailable')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '搜索 VPS' })).toHaveValue('Tokyo')
    expect(currentQuery().get('q')).toBe('Tokyo')
    expect(currentQuery().get('selected')).toBe('vps_001')
    expect(currentQuery().get('source')).toBe('keep')
    expect(currentQuery().get('workspace')).toBe('ledger')
  })

  it('does not treat renewal views as empty while subscription evidence is unavailable', async () => {
    let resolveSubscriptions!: (response: Response) => void
    const pending = new Promise<Response>((resolve) => { resolveSubscriptions = resolve })
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input) === '/api/vps') return Promise.resolve(mockJSONResponse([vps]))
      if (String(input) === '/api/providers') return Promise.resolve(mockJSONResponse([provider]))
      return pending
    }))
    mount('/vps?workspace=ledger&view=renewal')
    expect(await screen.findByRole('heading', { name: '正在加载订阅证据…' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '30天续费' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '30天续费 0' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '缺订阅 0' })).not.toBeInTheDocument()
    expect(document.querySelector('.vps-page__stats')).toHaveTextContent('1')
    expect(document.querySelector('.vps-page__stats')).not.toHaveTextContent('显示')
    expect(screen.queryByText('当前筛选没有匹配 VPS')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    resolveSubscriptions(mockJSONResponse({ error: 'subscription backend unavailable' }, 503))
    expect(await screen.findByRole('heading', { name: '订阅证据不可用' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '30天续费' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '30天续费 0' })).not.toBeInTheDocument()
    expect(document.querySelector('.vps-page__stats')).not.toHaveTextContent('显示')
    expect(screen.queryByText('当前筛选没有匹配 VPS')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '选择 Tokyo Edge' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '全部 1' }))
    expect(await screen.findByRole('button', { name: '选择 Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('subscription backend unavailable')
  })

})
