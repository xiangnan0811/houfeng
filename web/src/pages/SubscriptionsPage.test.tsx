import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SubscriptionsPage } from './SubscriptionsPage'
import type {
  SubscriptionOverview,
  SubscriptionRecord,
  SubscriptionStatistics,
  VPSAssetRecord,
} from '../lib/types'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const vps: VPSAssetRecord = {
  vps_id: 'vps_001',
  display_name: 'Tokyo Edge',
  provider_id: 'pv_001',
  provider_name: 'Hetzner',
  product_name: 'cx22',
  order_ref: '',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: '',
  ipv4: '',
  ipv6: '',
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: '',
  virtualization: '',
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
}

const subscription: SubscriptionRecord = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 12,
  monthly_price_base: 84,
  yearly_price_base: 1008,
  base_currency: 'CNY',
  exchange_rate: 7,
  exchange_rate_date: '2026-05-09',
  exchange_rate_status: 'fresh',
  budget_status: 'ok',
  next_reminder_at: '2026-05-18T00:00:00Z',
  started_at: '2026-05-01',
  renew_at: '2026-06-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  renewal_mode: 'auto',
  status: 'active' as const,
  payment_method: 'card',
  note: '',
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

function overviewFor(subscriptions: SubscriptionRecord[] = [], overrides: Partial<SubscriptionOverview> = {}): SubscriptionOverview {
  const totalMonthly = subscriptions.reduce((sum, sub) => sum + (sub.monthly_price_base ?? 0), 0)
  return {
    snapshot_generated_at: '2026-05-09T08:00:00Z',
    base_currency: 'CNY',
    total_monthly_cost: totalMonthly,
    total_yearly_cost: totalMonthly * 12,
    active_subscription_count: subscriptions.length,
    renewal_due_14d_count: 0,
    renewal_due_30d_count: subscriptions.filter((sub) => sub.renew_at).length,
    budget_risk_count: 0,
    current_missing_rate_count: subscriptions.filter((sub) => sub.exchange_rate_status === 'missing').length,
    current_stale_rate_count: subscriptions.filter((sub) => sub.exchange_rate_status === 'stale').length,
    current_unknown_amount_count: subscriptions.filter((sub) => sub.monthly_price_base == null).length,
    decision_attention_count: 0,
    missing_subscription_vps_count: 0,
    upcoming_renewals: subscriptions.filter((sub) => sub.renew_at).map((sub) => ({
      subscription_id: sub.subscription_id,
      vps_id: sub.vps_id,
      vps_display_name: 'Tokyo Edge',
      display_name: sub.display_name ?? '',
      provider_name: 'Hetzner',
      ...(sub.renew_at === undefined ? {} : { renew_at: sub.renew_at }),
      ...(sub.monthly_price_base === undefined
        ? {}
        : { monthly_price_base: sub.monthly_price_base }),
      ...(sub.yearly_price_base === undefined
        ? {}
        : { yearly_price_base: sub.yearly_price_base }),
      base_currency: sub.base_currency ?? 'CNY',
      currency: sub.currency,
      renewal_decision: 'keep',
      lifecycle_status: 'active',
      exchange_rate_status: sub.exchange_rate_status ?? 'fresh',
    })),
    provider_breakdown: subscriptions.length > 0 ? [{
      key: 'pv_001',
      label: 'Hetzner',
      monthly_cost: totalMonthly,
      yearly_cost: totalMonthly * 12,
      subscription_count: subscriptions.length,
    }] : [],
    currency_breakdown: [],
    category_breakdown: [],
    budget_risks: [],
    vps_costs: subscriptions.map((sub) => ({
      subscription_id: sub.subscription_id,
      vps_id: sub.vps_id,
      vps_display_name: 'Tokyo Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      country: 'JP',
      region: 'Kanto',
      display_name: sub.display_name || 'Tokyo Edge',
      cost_category: sub.cost_category ?? '',
      labels: sub.labels ?? [],
      price: sub.price,
      currency: sub.currency,
      monthly_price: sub.monthly_price,
      ...(sub.monthly_price_base === undefined
        ? {}
        : { monthly_price_base: sub.monthly_price_base }),
      ...(sub.yearly_price_base === undefined
        ? {}
        : { yearly_price_base: sub.yearly_price_base }),
      base_currency: sub.base_currency ?? 'CNY',
      ...(sub.exchange_rate === undefined ? {} : { exchange_rate: sub.exchange_rate }),
      ...(sub.exchange_rate_date === undefined
        ? {}
        : { exchange_rate_date: sub.exchange_rate_date }),
      exchange_rate_status: sub.exchange_rate_status ?? 'fresh',
      ...(sub.renew_at === undefined ? {} : { renew_at: sub.renew_at }),
      ...(sub.next_reminder_at === undefined
        ? {}
        : { next_reminder_at: sub.next_reminder_at }),
      status: sub.status,
      payment_method: sub.payment_method,
      lifecycle_status: 'active',
      renewal_decision: 'keep',
      budget_status: sub.budget_status ?? 'unknown',
    })),
    missing_subscription_assets: [],
    ...overrides,
  }
}

function statisticsFor(subscriptions: SubscriptionRecord[] = [], overrides: Partial<SubscriptionStatistics> = {}): SubscriptionStatistics {
  const totalMonthly = subscriptions.reduce((sum, sub) => sum + (sub.monthly_price_base ?? 0), 0)
  return {
    window: 'year',
    base_currency: 'CNY',
    total_monthly_cost: totalMonthly,
    total_yearly_cost: totalMonthly * 12,
    provider_breakdown: subscriptions.length > 0 ? [{
      key: 'pv_001',
      label: 'Hetzner',
      monthly_cost: totalMonthly,
      yearly_cost: totalMonthly * 12,
      subscription_count: subscriptions.length,
    }] : [],
    currency_breakdown: [],
    category_breakdown: [],
    payment_breakdown: [],
    region_breakdown: [],
    cost_month_buckets: [
      { bucket: '2025-07', monthly_cost: Math.max(totalMonthly - 10, 0), renewal_count: 0, data_insufficient: false },
      { bucket: '2025-08', monthly_cost: totalMonthly, renewal_count: 0, data_insufficient: false },
      { bucket: '2026-06', monthly_cost: totalMonthly, renewal_count: 0, data_insufficient: false },
    ],
    renewal_month_buckets: [],
    budget_statuses: [],
    ...overrides,
  }
}

type SubscriptionFetchOptions = {
  subscriptions?: SubscriptionRecord[]
  vpsRows?: VPSAssetRecord[]
  subscriptionsErrorOnce?: string
  vpsError?: string
  overviewError?: string
  statistics?: SubscriptionStatistics
  statisticsError?: string
  statisticsErrorOnce?: string
  overviewOverrides?: Partial<SubscriptionOverview>
}

function setupSubscriptionFetch({
  subscriptions = [],
  vpsRows = [vps],
  subscriptionsErrorOnce,
  vpsError,
  overviewError,
  statistics,
  statisticsError,
  statisticsErrorOnce,
  overviewOverrides = {},
}: SubscriptionFetchOptions = {}) {
  let currentSubscriptions = subscriptions
  let failNextSubscriptions = subscriptionsErrorOnce
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    if (url.startsWith('/api/subscriptions?') || url === '/api/subscriptions') {
      if (method === 'POST') {
        const body = JSON.parse(String(init?.body)) as SubscriptionRecord
        const created: SubscriptionRecord = {
          ...subscription,
          ...body,
          subscription_id: 'sub_new',
          monthly_price: body.billing_months > 0 ? body.price / body.billing_months : body.price,
          monthly_price_base: body.price * 7,
          yearly_price_base: body.price * 7 * 12,
          base_currency: 'CNY',
          exchange_rate: 7,
          exchange_rate_date: '2026-05-09',
          exchange_rate_status: 'fresh',
          budget_status: 'ok',
          created_at: '2026-05-09T08:00:00Z',
          updated_at: '2026-05-09T08:00:00Z',
        }
        currentSubscriptions = [created]
        return Promise.resolve(mockJSONResponse(created, 201))
      }
      if (failNextSubscriptions) {
        const error = failNextSubscriptions
        failNextSubscriptions = undefined
        return Promise.resolve(mockJSONResponse({ error }, 500))
      }
      return Promise.resolve(mockJSONResponse(currentSubscriptions))
    }
    if (url === '/api/subscriptions/sub_001' && method === 'PATCH') {
      const body = JSON.parse(String(init?.body)) as SubscriptionRecord
      const updated: SubscriptionRecord = {
        ...subscription,
        ...body,
        subscription_id: 'sub_001',
        monthly_price: body.billing_months > 0 ? body.price / body.billing_months : body.price,
        monthly_price_base: body.price * 7,
        yearly_price_base: body.price * 7 * 12,
        base_currency: 'CNY',
        exchange_rate: 7,
        exchange_rate_date: '2026-05-09',
        exchange_rate_status: 'fresh',
        budget_status: 'ok',
        updated_at: '2026-05-09T09:00:00Z',
      }
      currentSubscriptions = [updated]
      return Promise.resolve(mockJSONResponse(updated))
    }
    if (url === '/api/vps') {
      if (vpsError) return Promise.resolve(mockJSONResponse({ error: vpsError }, 500))
      return Promise.resolve(mockJSONResponse(vpsRows))
    }
    if (url === '/api/subscriptions/overview') {
      if (overviewError) return Promise.resolve(mockJSONResponse({ error: overviewError }, 500))
      return Promise.resolve(mockJSONResponse(overviewFor(currentSubscriptions, overviewOverrides)))
    }
    if (url === '/api/subscriptions/statistics?window=year') {
      if (statisticsErrorOnce) {
        const error = statisticsErrorOnce
        statisticsErrorOnce = undefined
        return Promise.resolve(mockJSONResponse({ error }, 500))
      }
      if (statisticsError) return Promise.resolve(mockJSONResponse({ error: statisticsError }, 500))
      return Promise.resolve(mockJSONResponse(statistics ?? statisticsFor(currentSubscriptions)))
    }
    if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
      return Promise.resolve(mockJSONResponse({ items: [] }))
    }
    if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
      return Promise.resolve(mockJSONResponse({ items: [] }, 202))
    }
    return Promise.resolve(mockJSONResponse({ error: `unhandled ${method} ${url}` }, 404))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function findCall(fetchMock: ReturnType<typeof vi.fn>, url: string, method = 'GET') {
  return fetchMock.mock.calls.find(([calledUrl, init]) => calledUrl === url && ((init as RequestInit | undefined)?.method ?? 'GET') === method)
}

function openSubscriptionEditor(name = 'Tokyo Edge') {
  fireEvent.click(screen.getByRole('button', { name }))
  return screen.getByRole('dialog', { name: '编辑订阅表单' })
}

function openInsights() {
  fireEvent.click(screen.getByRole('tab', { name: '成本洞察' }))
}

function selectVpsFilter(optionName: string) {
  fireEvent.click(screen.getByRole('button', { name: /^VPS / }))
  fireEvent.click(screen.getByRole('option', { name: new RegExp(optionName) }))
}

function HistoryControls() {
  const navigate = useNavigate()
  const location = useLocation()
  return (
    <>
      <div data-testid="search">{`${location.pathname}${location.search}`}</div>
      <button type="button" onClick={() => navigate(-1)}>history-back</button>
    </>
  )
}

describe('SubscriptionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders subscriptions and applies renew-window filters', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?renew_within_days=30&view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.queryByRole('dialog', { name: '新建订阅表单' })).not.toBeInTheDocument()
    expect(screen.getAllByText('USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getByText('自动续费')).toBeInTheDocument()
    expect(screen.queryByText('预算状态')).not.toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: '预算/汇率' })).not.toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'CNY 成本' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /预算风险/ })).toHaveAttribute('href', '/settings?tab=subscriptions')
    expect(screen.getByRole('link', { name: '需要资产判断' })).toHaveAttribute('href', '/asset-decisions?view=renewal&renew_within_days=30&vps_id=vps_001')
    const tableTitle = screen.getByRole('heading', { name: '订阅明细' })
    const tableRegion = screen.getByRole('region', { name: '订阅明细' })
    expect(tableTitle).toHaveAttribute('id', 'subscription-table-title')
    expect(tableTitle.closest('section')).not.toHaveClass('page-panel--scroll-x')
    expect(tableRegion).toHaveAttribute('tabindex', '0')
    expect(tableRegion).toHaveAttribute('aria-labelledby', tableTitle.id)
    expect(tableRegion).not.toHaveAttribute('aria-describedby')
    expect(screen.queryByText('横向滚动查看完整列')).not.toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('applies provider filters from provider directory links', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?provider_id=pv_001&view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByRole('button', { name: '移除筛选 服务商: Hetzner' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?provider_id=pv_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('highlights renewals within 30 calendar days, including day 30', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-05T12:00:00Z'))
    try {
      setupSubscriptionFetch({
        subscriptions: [
          { ...subscription, subscription_id: 'sub_d29', renew_at: '2026-10-31' },
          { ...subscription, subscription_id: 'sub_d30', renew_at: '2026-11-01' },
          { ...subscription, subscription_id: 'sub_d31', renew_at: '2026-11-02' },
          { ...subscription, subscription_id: 'sub_late', renew_at: '2026-09-24' },
        ],
        // 明细与摘要取同一个“今天”：摘要生成于 UTC 10-02，页面时钟已是 10-05。
        overviewOverrides: { snapshot_generated_at: '2026-10-02T04:00:00Z' },
      })
      render(
        <MemoryRouter initialEntries={['/subscriptions?view=details']}>
          <SubscriptionsPage />
        </MemoryRouter>,
      )
      await waitFor(() => expect(screen.getByText('2026-11-02')).toBeInTheDocument())
      const cell = (date: string) => screen.getByText(date).closest('td')
      expect(cell('2026-10-31')).toHaveClass('text-warn')
      expect(cell('2026-11-01')).toHaveClass('text-warn')
      // 按页面时钟（10-05）11-02 只剩 28 天会被高亮；不高亮证明明细用的是摘要的 10-02。
      expect(cell('2026-11-02')).not.toHaveClass('text-warn')
      // 逾期在单元格里直接写出，不只靠颜色或悬停提示。
      expect(cell('2026-09-24')).toHaveTextContent('已逾期 8 天')
      expect(cell('2026-10-31')).not.toHaveTextContent('已逾期')
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps the summary day while the overview reloads and drops it once the refresh fails', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-05T12:00:00Z'))
    try {
      let overviewRequests = 0
      let overviewResponder: (() => Promise<Response>) | null = null
      let failReload!: () => void
      const reload = new Promise<Response>((resolve) => { failReload = () => resolve(mockJSONResponse({ error: 'overview unavailable' }, 500)) })
      const fetchMock = setupSubscriptionFetch({
        subscriptions: [
          { ...subscription, subscription_id: 'sub_d31', renew_at: '2026-11-02' },
          { ...subscription, subscription_id: 'sub_late', renew_at: '2026-10-01', display_name: '逾期账单' },
        ],
        overviewOverrides: { snapshot_generated_at: '2026-10-02T04:00:00Z' },
      })
      const original = fetchMock.getMockImplementation()!
      fetchMock.mockImplementation((url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        if (url === '/api/subscriptions/overview') {
          overviewRequests += 1
          if (overviewResponder) return overviewResponder()
          if (overviewRequests > 1) return reload
        }
        if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
          return Promise.resolve(mockJSONResponse({
            items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'fresh', refresh_status: 'idle', attempt_count: 1 }],
          }, 202))
        }
        if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
          return Promise.resolve(mockJSONResponse({ items: [] }))
        }
        return original(url, init)
      })
      render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
      await waitFor(() => expect(screen.getByText('2026-11-02')).toBeInTheDocument())
      const cell = () => screen.getByText('2026-11-02').closest('td')
      // 相对摘要日 10-02 是 31 天，不高亮。
      expect(cell()).not.toHaveClass('text-warn')

      await waitFor(() => expect(screen.getByRole('button', { name: '刷新汇率' })).toBeEnabled())
      fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
      await waitFor(() => expect(overviewRequests).toBe(2))
      // 重新加载中仍按旧摘要日计算，不跳动。
      expect(cell()).not.toHaveClass('text-warn')

      failReload()
      // 刷新失败后旧摘要不可用：退回页面打开日 10-05，只剩 28 天，进入 30 天高亮。
      await waitFor(() => expect(cell()).toHaveClass('text-warn'))

      // 失败后重试：请求进行中仍按页面日，不提前恢复旧摘要；成功后才切回新摘要。
      let finishRetry!: () => void
      overviewResponder = () => new Promise<Response>((resolve) => {
        finishRetry = () => resolve(mockJSONResponse(overviewFor([{ ...subscription, renew_at: '2026-11-02' }], { snapshot_generated_at: '2026-10-04T04:00:00Z' })))
      })
      const lateCell = () => screen.getByText('2026-10-01').closest('td')
      // 逾期天数能区分三个候选“今天”：旧摘要 10-02 为 1 天，页面日 10-05 为 4 天，新摘要 10-04 为 3 天。
      expect(lateCell()).toHaveTextContent('已逾期 4 天')
      fireEvent.click(await screen.findByRole('button', { name: '重试概览' }))
      await waitFor(() => expect(overviewRequests).toBe(3))
      expect(lateCell()).toHaveTextContent('已逾期 4 天')
      finishRetry()
      await waitFor(() => expect(lateCell()).toHaveTextContent('已逾期 3 天'))
    } finally {
      vi.useRealTimers()
    }
  })

  it('falls back to the page-open day when the subscription overview is unavailable', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-05T12:00:00Z'))
    try {
      setupSubscriptionFetch({
        subscriptions: [{ ...subscription, subscription_id: 'sub_d28', renew_at: '2026-11-02' }],
        overviewError: 'overview unavailable',
      })
      render(
        <MemoryRouter initialEntries={['/subscriptions?view=details']}>
          <SubscriptionsPage />
        </MemoryRouter>,
      )
      await waitFor(() => expect(screen.getByText('2026-11-02')).toBeInTheDocument())
      expect(screen.getByText('2026-11-02').closest('td')).toHaveClass('text-warn')
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows the VPS name once for an unnamed subscription and keeps both lines for a named one', async () => {
    setupSubscriptionFetch({
      subscriptions: [
        { ...subscription, subscription_id: 'sub_unnamed', display_name: '' },
        { ...subscription, subscription_id: 'sub_named', display_name: '主账单', renew_at: '2026-12-01' },
      ],
    })
    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    const named = await screen.findByRole('button', { name: '主账单' })
    expect(named.closest('tr')).toHaveTextContent('Tokyo Edge')
    const unnamedRow = screen.getByRole('button', { name: 'Tokyo Edge' }).closest('tr')!
    expect(within(unnamedRow).getAllByText('Tokyo Edge')).toHaveLength(1)
  })

  it('shows no-VPS prerequisite with link to VPS page', async () => {
    setupSubscriptionFetch({ subscriptions: [], vpsRows: [] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('link', { name: '先创建 VPS' })).toHaveAttribute('href', '/vps'))
    expect(screen.getByText('尚未记录订阅')).toBeInTheDocument()
  })

  it('creates subscriptions without sending monthly_price', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [], vpsRows: [vps] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '创建订阅' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    expect(createDialog).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('价格')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('币种')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('计费周期单位')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('开始日期')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('支付方式')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('展示名')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('分类')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('标签')).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('备注')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('VPS'), { target: { value: 'vps_001' } })
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(within(createDialog).getByLabelText('币种'), { target: { value: 'USD' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期单位'), { target: { value: 'month' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期长度'), { target: { value: '2' } })
    fireEvent.change(within(createDialog).getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(createDialog).getByLabelText('自动续费'))
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))

    await waitFor(() => expect(screen.getByText('USD 24.00')).toBeInTheDocument())
    const createCall = findCall(fetchMock, '/api/subscriptions', 'POST')
    expect(createCall?.[0]).toBe('/api/subscriptions')
    const createInit = createCall?.[1] as RequestInit
    expect(createInit.method).toBe('POST')
    expect(createInit.headers).toEqual({
      Accept: 'application/json',
      'Content-Type': 'application/json',
      'Idempotency-Key': expect.stringMatching(/^[0-9a-f-]{36}$/i),
    })
    expect(createInit.cache).toBe('no-store')
    expect(createInit.credentials).toBe('include')
    expect(JSON.parse(String(createInit.body))).toEqual({
        vps_id: 'vps_001',
        price: 24,
        currency: 'USD',
        billing_cycle: '2 months',
        billing_months: 2,
        billing_period_unit: 'month',
        billing_period_length: 2,
        started_at: null,
        renew_at: '2026-07-01',
        auto_renew: true,
        auto_renew_cancelled: false,
        renewal_mode: 'auto',
        display_name: '',
        cost_category: '',
        labels: [],
        trial_ends_at: null,
        ends_at: null,
        payment_method: '',
        note: '',
    })
  })

  it('reuses the create idempotency key after a lost response', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [], vpsRows: [vps] })
    let posts = 0
    const original = fetchMock.getMockImplementation()
    if (!original) throw new Error('subscription fetch mock missing implementation')
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions' && method === 'POST') {
        posts += 1
        if (posts === 1) return Promise.reject(new TypeError('Failed to fetch'))
      }
      return original(url, init)
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '创建订阅' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    fireEvent.change(within(createDialog).getByLabelText('VPS'), { target: { value: 'vps_001' } })
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(within(createDialog).getByLabelText('币种'), { target: { value: 'USD' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期单位'), { target: { value: 'month' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期长度'), { target: { value: '2' } })
    fireEvent.change(within(createDialog).getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))
    expect(await screen.findByText('Failed to fetch')).toBeInTheDocument()
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))
    await waitFor(() => expect(screen.getByText('USD 24.00')).toBeInTheDocument())

    const postCalls = fetchMock.mock.calls.filter(([url, init]) => (
      url === '/api/subscriptions' && ((init as RequestInit | undefined)?.method ?? 'GET') === 'POST'
    ))
    expect(postCalls).toHaveLength(2)
    const firstKey = (postCalls[0]?.[1] as RequestInit).headers
    const secondKey = (postCalls[1]?.[1] as RequestInit).headers
    expect(firstKey).toEqual(expect.objectContaining({
      'Idempotency-Key': expect.stringMatching(/^[0-9a-f-]{36}$/i),
    }))
    expect(secondKey).toEqual(firstKey)
  })

  it('closes URL-requested create drawer without dropping filters', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?vps_id=vps_001&create=1&view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('dialog', { name: '新建订阅表单' })).toBeInTheDocument())
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    expect(within(createDialog).getByLabelText('VPS')).toHaveValue('vps_001')
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '99' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '新建订阅表单' })).not.toBeInTheDocument())
    expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/subscriptions?vps_id=vps_001'))).toBe(true)
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes('create=1'))).toBe(false)
  })

  it('resets URL-requested create draft and errors after drawer cancel', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?vps_id=vps_001&create=1&view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('dialog', { name: '新建订阅表单' })).toBeInTheDocument())
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '9' } })
    fireEvent.change(within(createDialog).getByLabelText('币种'), { target: { value: '__custom' } })
    fireEvent.change(within(createDialog).getByLabelText('自定义币种'), { target: { value: 'US1' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))
    expect(screen.getByText('币种必须为 3 位大写代码。')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '99' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '新建订阅表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /新建订阅/ }))

    const reopened = screen.getByRole('dialog', { name: '新建订阅表单' })
    expect(within(reopened).queryByText('币种必须为 3 位大写代码。')).not.toBeInTheDocument()
    expect(within(reopened).getByLabelText('价格')).toHaveValue(null)
    expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/subscriptions?vps_id=vps_001'))).toBe(true)
  })

  it('updates subscriptions through PATCH and shows updated billing facts', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.queryByRole('dialog', { name: '编辑订阅表单' })).not.toBeInTheDocument()
    const editDialog = openSubscriptionEditor()
    expect(editDialog).toBeInTheDocument()
    fireEvent.change(within(editDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(within(editDialog).getByLabelText('计费周期单位'), { target: { value: 'month' } })
    fireEvent.change(within(editDialog).getByLabelText('计费周期长度'), { target: { value: '3' } })
    fireEvent.change(within(editDialog).getByLabelText('续费日期'), { target: { value: '2026-08-01' } })
    fireEvent.click(within(editDialog).getByLabelText('已取消自动续费'))
    fireEvent.change(within(editDialog).getByLabelText('支付方式'), { target: { value: 'PayPal' } })
    fireEvent.change(within(editDialog).getByLabelText('备注'), { target: { value: 'review' } })
    fireEvent.click(within(editDialog).getByRole('button', { name: '保存订阅' }))

    await waitFor(() => expect(screen.getAllByText('USD 24.00').length).toBeGreaterThan(0))
    expect(screen.getByText('已取消自动续费')).toBeInTheDocument()
    expect(findCall(fetchMock, '/api/subscriptions/sub_001', 'PATCH')).toEqual(['/api/subscriptions/sub_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        vps_id: 'vps_001',
        price: 24,
        currency: 'USD',
        billing_cycle: '3 months',
        billing_months: 3,
        billing_period_unit: 'month',
        billing_period_length: 3,
        started_at: '2026-05-01',
        renew_at: '2026-08-01',
        auto_renew: false,
        auto_renew_cancelled: true,
        renewal_mode: 'auto_cancelled',
        display_name: '',
        cost_category: '',
        labels: [],
        trial_ends_at: null,
        ends_at: null,
        payment_method: 'PayPal',
        note: 'review',
      }),
    }])
  })

  it('links subscription billing facts back to the VPS owner from the edit dialog', async () => {
    setupSubscriptionFetch({
      subscriptions: [{ ...subscription, auto_renew: false, auto_renew_cancelled: true, renewal_mode: 'auto_cancelled' }],
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('已取消自动续费')).toBeInTheDocument())
    const editDialog = openSubscriptionEditor()
    expect(within(editDialog).getByRole('link', { name: '打开关联 VPS' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.queryByRole('link', { name: '回到 VPS' })).not.toBeInTheDocument()
  })

  it('shows subscription error state with retry', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [], subscriptionsErrorOnce: 'subscriptions unavailable' })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('订阅列表不可用')).toBeInTheDocument())
    expect(screen.getByText('subscriptions unavailable')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
    expect(fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions')).length).toBeGreaterThanOrEqual(3)
  })

  it('keeps list usable when statistics panel fails', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription], statisticsError: 'statistics unavailable' })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument()
    expect(screen.queryByText('statistics unavailable')).not.toBeInTheDocument()
    openInsights()
    await waitFor(() => expect(screen.getByText('statistics unavailable')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '重试统计' })).toBeInTheDocument()
  })

  it('organizes cost insights as a 2 by 2 workbench with contextual donut details and a composition select', async () => {
    setupSubscriptionFetch({
      subscriptions: [
        subscription,
        {
          ...subscription,
          subscription_id: 'sub_002',
          vps_id: 'vps_002',
          display_name: 'Osaka Backup',
          price: 6,
          monthly_price: 6,
          monthly_price_base: 42,
          yearly_price_base: 504,
          payment_method: 'paypal',
        },
      ],
      statistics: statisticsFor([subscription], {
        provider_breakdown: [
          { key: 'pv_001', label: 'Hetzner', monthly_cost: 84, yearly_cost: 1008, subscription_count: 1 },
        ],
        payment_breakdown: [
          { key: 'card', label: 'card', monthly_cost: 84, yearly_cost: 1008, subscription_count: 1 },
        ],
        region_breakdown: [
          { key: 'JP / Kanto', label: 'JP / Kanto', monthly_cost: 84, yearly_cost: 1008, subscription_count: 1 },
        ],
        cost_month_buckets: [
          { bucket: '2025-07', monthly_cost: 70, renewal_count: 0, budget_limit: 100, budget_currency: 'CNY', budget_warning_pct: 80, data_insufficient: false },
          { bucket: '2025-08', monthly_cost: 84, renewal_count: 0, budget_limit: 80, budget_currency: 'CNY', budget_warning_pct: 80, data_insufficient: false },
          { bucket: '2026-06', monthly_cost: 84, renewal_count: 0, budget_limit: 120, budget_currency: 'CNY', budget_warning_pct: 80, data_insufficient: false },
        ],
      }),
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('progressbar', { name: 'Hetzner 月成本' })).toBeInTheDocument())
    const insights = screen.getByRole('region', { name: '订阅成本洞察' })
    const monthTabs = within(insights).getByRole('tablist', { name: '月成本展示' })
    const activeMonthTab = within(monthTabs).getByRole('tab', { selected: true })
    const monthPanel = within(insights).getByRole('tabpanel')
    expect(activeMonthTab).toHaveAttribute('aria-controls', monthPanel.id)
    expect(monthPanel).toHaveAttribute('aria-labelledby', activeMonthTab.id)
    expect(within(insights).getByRole('heading', { name: '月成本与月预算' })).toBeInTheDocument()
    expect(within(insights).getByRole('progressbar', { name: 'Hetzner 月成本' })).toHaveAttribute('value', '84')
    expect(screen.getByLabelText('构成维度')).toHaveValue('provider')
    fireEvent.change(screen.getByLabelText('构成维度'), { target: { value: 'payment' } })
    expect(screen.getByText('card')).toBeInTheDocument()
    expect(screen.queryByText('划过扇区查看明细')).not.toBeInTheDocument()
    expect(screen.queryByText(/原始付费：/)).not.toBeInTheDocument()

    const donutSegment = within(insights).getByRole('button', { name: /筛选 Tokyo Edge/ })
    fireEvent.mouseEnter(donutSegment)
    expect(screen.getByText('原始付费：USD 12.00')).toBeInTheDocument()
    expect(screen.getByText('基准月成本：CNY 84.00')).toBeInTheDocument()
    fireEvent.mouseLeave(donutSegment)
    expect(screen.queryByText('原始付费：USD 12.00')).not.toBeInTheDocument()

    fireEvent.click(within(insights).getByRole('tab', { name: '排行' }))
    expect(within(insights).getByRole('progressbar', { name: 'Tokyo Edge 月成本' })).toHaveAttribute('max', '84')
    expect(within(insights).getByRole('progressbar', { name: 'Osaka Backup 月成本' })).toHaveAttribute('value', '42')
  })

  it('renders monthly labels for the annual trend axis', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription] })
    const { container } = render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('25/07')).toBeInTheDocument())
    expect(screen.getByText('25/08')).toBeInTheDocument()
    expect(screen.getByText('26/06')).toBeInTheDocument()
    expect(container).not.toHaveTextContent('00:00')
  })

  it('does not draw annual trend when any bucket is marked data-insufficient', async () => {
    setupSubscriptionFetch({
      subscriptions: [subscription],
      statistics: statisticsFor([subscription], {
        cost_month_buckets: [
          { bucket: '2025-07', monthly_cost: 0, renewal_count: 0, data_insufficient: true },
          { bucket: '2025-08', monthly_cost: 84, renewal_count: 0, data_insufficient: false },
          { bucket: '2026-06', monthly_cost: 84, renewal_count: 0, data_insufficient: false },
        ],
      }),
    })

    const { container } = render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    await waitFor(() => expect(screen.getByText('历史成本数据不足')).toBeInTheDocument())
    expect(screen.getByText('部分历史月份缺少可用汇率或预算币种不一致，暂不绘制可能误导的趋势曲线。')).toBeInTheDocument()
    expect(container.querySelector('.subscription-insight-panel--trend polyline')).toBeNull()
  })

  it('resets subscription edit draft and errors after drawer cancel', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    const firstEditDialog = openSubscriptionEditor()
    fireEvent.change(within(firstEditDialog).getByLabelText('币种'), { target: { value: '__custom' } })
    fireEvent.change(within(firstEditDialog).getByLabelText('自定义币种'), { target: { value: 'US1' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '保存订阅' }))
    await waitFor(() => expect(within(firstEditDialog).getByText('币种必须为 3 位大写代码。')).toBeInTheDocument())
    fireEvent.change(within(firstEditDialog).getByLabelText('支付方式'), { target: { value: '__custom' } })
    fireEvent.change(within(firstEditDialog).getByLabelText('自定义支付方式'), { target: { value: 'draft-pay' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑订阅表单' })).not.toBeInTheDocument())
    const editDialog = openSubscriptionEditor()
    expect(within(editDialog).queryByText('币种必须为 3 位大写代码。')).not.toBeInTheDocument()
    expect(within(editDialog).getByLabelText('币种')).toHaveValue('USD')
    expect(within(editDialog).getByLabelText('支付方式')).toHaveValue('__custom')
    expect(within(editDialog).getByLabelText('自定义支付方式')).toHaveValue('card')
    expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions').length).toBeGreaterThanOrEqual(1)
  })

  it('keeps subscription list when VPS or overview sources fail', async () => {
    setupSubscriptionFetch({
      subscriptions: [subscription],
      vpsError: 'vps unavailable',
      overviewError: 'overview unavailable',
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('vps_001').length).toBeGreaterThan(0))
    expect(screen.getByText(/VPS 列表不可用：vps unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/成本概览不可用：overview unavailable/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试 VPS' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试概览' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '需要资产判断' })).toBeInTheDocument()
    expect(screen.queryByText('订阅列表不可用')).not.toBeInTheDocument()
    openInsights()
    await waitFor(() => expect(screen.getByText('月成本不可用')).toBeInTheDocument())
  })

  it('reloads latched cost statistics only after an in-flight rate refresh succeeds', async () => {
    let refreshCount = 0
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
        refreshCount += 1
        return Promise.resolve(mockJSONResponse({
          items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'missing', refresh_status: 'queued', attempt_count: 1 }],
        }, 202))
      }
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return Promise.resolve(mockJSONResponse({
          items: refreshCount > 0
            ? [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'fresh', refresh_status: 'idle', attempt_count: 1 }]
            : [],
        }))
      }
      return original(url, init)
    })
    render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
    const statsCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/statistics?window=year').length
    const overviewCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/overview').length
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新汇率' })).toBeEnabled())
    const overviewBefore = overviewCalls()
    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    expect(await screen.findByText('汇率已更新')).toBeInTheDocument()
    await waitFor(() => expect(overviewCalls()).toBeGreaterThan(overviewBefore))
    expect(statsCalls()).toBe(0)
    openInsights()
    await waitFor(() => expect(statsCalls()).toBe(1))
    fireEvent.click(screen.getByRole('tab', { name: '明细' }))
    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    await waitFor(() => expect(statsCalls()).toBe(2))
  })

  it('reloads insights opened while a rate refresh is still in flight once that refresh succeeds', async () => {
    let finishRefresh!: (response: Response) => void
    const pending = new Promise<Response>((resolve) => { finishRefresh = resolve })
    let posted = false
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
        posted = true
        return pending
      }
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return Promise.resolve(mockJSONResponse({
          items: posted
            ? [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'fresh', refresh_status: 'idle', attempt_count: 1 }]
            : [],
        }))
      }
      return original(url, init)
    })
    render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新汇率' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    openInsights()
    const statsCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/statistics?window=year').length
    await waitFor(() => expect(statsCalls()).toBe(1))
    finishRefresh(mockJSONResponse({
      items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'stale', refresh_status: 'queued', attempt_count: 1 }],
    }, 202))
    await waitFor(() => expect(statsCalls()).toBe(2))
  })

  it('rechecks rate status after a subscription write and polls queued work without posting a refresh', async () => {
    let patched = false
    let queuedDelivered = false
    let releaseIdle!: (response: Response) => void
    const idlePending = new Promise<Response>((resolve) => { releaseIdle = resolve })
    const pair = (refreshStatus: 'queued' | 'idle', rateStatus: 'missing' | 'fresh') => ({
      provider: 'frankfurter',
      base_currency: 'CNY',
      quote_currency: 'USD',
      rate_status: rateStatus,
      refresh_status: refreshStatus,
      attempt_count: 1,
    })
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/sub_001' && method === 'PATCH') patched = true
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        if (!patched) return Promise.resolve(mockJSONResponse({ items: [] }))
        if (!queuedDelivered) {
          queuedDelivered = true
          return Promise.resolve(mockJSONResponse({ items: [pair('queued', 'missing')] }))
        }
        return idlePending
      }
      return original(url, init)
    })
    render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
    await waitFor(() => expect(screen.getAllByText('CNY 84.00').length).toBeGreaterThan(0))
    const statusGets = () => fetchMock.mock.calls.filter(([url, init]) => url === '/api/subscriptions/exchange-rates/status' && ((init as RequestInit | undefined)?.method ?? 'GET') === 'GET').length
    const refreshPosts = () => fetchMock.mock.calls.filter(([url, init]) => url === '/api/subscriptions/exchange-rates/refresh' && (init as RequestInit | undefined)?.method === 'POST').length
    const overviewCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/overview').length
    const statusBefore = statusGets()
    const overviewBefore = overviewCalls()
    const editDialog = openSubscriptionEditor()
    fireEvent.change(within(editDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.click(within(editDialog).getByRole('button', { name: '保存订阅' }))
    expect(await screen.findByText('补取中 1 项')).toBeInTheDocument()
    expect(statusGets()).toBeGreaterThan(statusBefore)
    expect(refreshPosts()).toBe(0)
    await waitFor(() => expect(overviewCalls()).toBeGreaterThan(overviewBefore))
    const overviewAfterWrite = overviewCalls()
    releaseIdle(mockJSONResponse({ items: [pair('idle', 'fresh')] }))
    expect(await screen.findByText('汇率已更新')).toBeInTheDocument()
    await waitFor(() => expect(overviewCalls()).toBeGreaterThan(overviewAfterWrite))
    expect(refreshPosts()).toBe(0)
  })

  it('reloads costs when a manual refresh is already idle fresh without calling HTTP 202 a success', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
        return Promise.resolve(mockJSONResponse({
          items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'fresh', refresh_status: 'idle', attempt_count: 0 }],
        }, 202))
      }
      return original(url, init)
    })
    render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
    const overviewCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/overview').length
    const statsCalls = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/statistics?window=year').length
    await waitFor(() => expect(screen.getAllByText('CNY 84.00').length).toBeGreaterThan(0))
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新汇率' })).toBeEnabled())
    const overviewBefore = overviewCalls()
    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    await waitFor(() => expect(overviewCalls()).toBeGreaterThan(overviewBefore))
    expect(screen.queryByText('汇率已更新')).not.toBeInTheDocument()
    expect(statsCalls()).toBe(0)
  })

  it('keeps cost amounts and shows a scoped status error when the status read fails', async () => {
    let statusReadable = false
    const secret = 'https://fx.example/latest?access_key=super-secret-token'
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        if (!statusReadable) return Promise.resolve(mockJSONResponse({ error: secret }, 500))
        return Promise.resolve(mockJSONResponse({ items: [] }))
      }
      return original(url, init)
    })
    render(<MemoryRouter initialEntries={['/subscriptions?view=details']}><SubscriptionsPage /></MemoryRouter>)
    expect(await screen.findByRole('alert')).toHaveTextContent('汇率状态暂不可读')
    await waitFor(() => expect(screen.getAllByText('CNY 84.00').length).toBeGreaterThan(0))
    expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('super-secret-token')
    expect(document.body.textContent).not.toContain('fx.example')
    const statusGets = () => fetchMock.mock.calls.filter(([url, init]) => url === '/api/subscriptions/exchange-rates/status' && ((init as RequestInit | undefined)?.method ?? 'GET') === 'GET').length
    const beforeRetry = statusGets()
    statusReadable = true
    fireEvent.click(screen.getByRole('button', { name: '重试读取' }))
    await waitFor(() => expect(screen.queryByText('汇率状态暂不可读')).not.toBeInTheDocument())
    expect(statusGets()).toBeGreaterThan(beforeRetry)
    expect(screen.getAllByText('CNY 84.00').length).toBeGreaterThan(0)
    expect(document.body.textContent).not.toContain('super-secret-token')
  })

  it('retries statistics without reloading the subscription list', async () => {
    const fetchMock = setupSubscriptionFetch({
      subscriptions: [subscription],
      statisticsErrorOnce: 'statistics unavailable',
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('tab', { name: '成本洞察' })).toBeInTheDocument())
    expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/subscriptions/statistics?window=year')).toBe(false)
    openInsights()
    await waitFor(() => expect(screen.getByText('statistics unavailable')).toBeInTheDocument())
    const listCallsBefore = fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions?') || String(url) === '/api/subscriptions').length
    fireEvent.click(screen.getByRole('button', { name: '重试统计' }))
    await waitFor(() => expect(screen.getByText('25/07')).toBeInTheDocument())
    const listCallsAfter = fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions?') || String(url) === '/api/subscriptions').length
    expect(listCallsAfter).toBe(listCallsBefore)
    expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/subscriptions/statistics?window=year').length).toBe(2)
  })

  it('does not present old-filter records while a newer filter request is in flight', async () => {
    let resolveFirst!: (value: Response) => void
    const first = new Promise<Response>((resolve) => { resolveFirst = resolve })
    let listCalls = 0
    const fetchMock = setupSubscriptionFetch({
      subscriptions: [subscription],
      vpsRows: [vps, { ...vps, vps_id: 'vps_002', display_name: 'Osaka Edge' }],
    })
    const original = fetchMock.getMockImplementation()
    if (!original) throw new Error('subscription fetch mock missing implementation')
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if ((url.startsWith('/api/subscriptions?') || url === '/api/subscriptions') && method === 'GET') {
        listCalls += 1
        if (listCalls === 1) return first
        if (String(url).includes('vps_id=vps_002')) return Promise.resolve(mockJSONResponse([]))
      }
      return original(url, init)
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: 'VPS 全部' })).toBeInTheDocument())
    selectVpsFilter('Osaka Edge')
    resolveFirst(mockJSONResponse([subscription]))
    await waitFor(() => expect(screen.getByText('当前 VPS 尚无订阅')).toBeInTheDocument())
    expect(within(screen.getByRole('heading', { name: '订阅明细' }).closest('section') as HTMLElement).queryByRole('button', { name: 'Tokyo Edge' })).not.toBeInTheDocument()
  })

  it('keeps create modal open while submit is pending', async () => {
    let resolveCreate!: (value: Response) => void
    const pendingCreate = new Promise<Response>((resolve) => { resolveCreate = resolve })
    const fetchMock = setupSubscriptionFetch({ subscriptions: [], vpsRows: [vps] })
    const original = fetchMock.getMockImplementation()
    if (!original) throw new Error('subscription fetch mock missing implementation')
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/subscriptions' && (init?.method ?? 'GET') === 'POST') return pendingCreate
      return original(url, init)
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '创建订阅' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    fireEvent.change(within(createDialog).getByLabelText('VPS'), { target: { value: 'vps_001' } })
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(within(createDialog).getByLabelText('币种'), { target: { value: 'USD' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期单位'), { target: { value: 'month' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期长度'), { target: { value: '2' } })
    fireEvent.change(within(createDialog).getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))
    expect(within(createDialog).getByRole('button', { name: '创建中…' })).toBeDisabled()
    fireEvent.click(within(createDialog).getByRole('button', { name: '关闭' }))
    expect(screen.getByRole('dialog', { name: '新建订阅表单' })).toBeInTheDocument()
    resolveCreate(mockJSONResponse({
      ...subscription,
      subscription_id: 'sub_new',
      price: 24,
      billing_months: 2,
      billing_period_length: 2,
    }, 201))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '新建订阅表单' })).not.toBeInTheDocument())
  })


  it('keeps list fetches stable across tab switches and restores details on Back', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })
    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <HistoryControls />
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    const listCallsBefore = fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions?') || String(url) === '/api/subscriptions').length
    openInsights()
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions')
    expect(screen.queryByRole('button', { name: 'VPS 全部' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: '明细' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    const listCallsAfter = fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions?') || String(url) === '/api/subscriptions').length
    expect(listCallsAfter).toBe(listCallsBefore)
    openInsights()
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions')
    fireEvent.click(screen.getByRole('button', { name: 'history-back' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions?view=details')
  })

  it('selects a VPS from insights and returns to the filtered details list', async () => {
    setupSubscriptionFetch({
      subscriptions: [subscription],
      vpsRows: [vps, { ...vps, vps_id: 'vps_002', display_name: 'Osaka Edge' }],
    })
    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <HistoryControls />
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /筛选 Tokyo Edge/ }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '移除筛选 VPS: Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions?vps_id=vps_001&view=details')
    expect(screen.getByRole('tabpanel', { name: '明细' })).toHaveFocus()
  })

  it('searches a long VPS catalog without a native select', async () => {
    const vpsRows = Array.from({ length: 120 }, (_, index) => ({
      ...vps,
      vps_id: `vps_${String(index).padStart(3, '0')}`,
      display_name: index === 87 ? 'Unique Osaka Node' : `Host ${index}`,
      ipv4: index === 87 ? '10.8.7.1' : `10.0.0.${index % 250}`,
    }))
    setupSubscriptionFetch({ subscriptions: [subscription], vpsRows })
    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('button', { name: /^VPS / })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /^VPS / }))
    fireEvent.change(screen.getByRole('combobox', { name: '搜索VPS' }), { target: { value: 'Unique Osaka' } })
    expect(screen.getByRole('option', { name: /Unique Osaka Node/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /^Host 1$/ })).not.toBeInTheDocument()
  })

  it('keeps the insights view when filters are cleared', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription] })
    render(
      <MemoryRouter initialEntries={['/subscriptions?view=insights&vps_id=vps_001']}>
        <HistoryControls />
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions?view=insights&vps_id=vps_001')
    fireEvent.click(screen.getByRole('button', { name: /当前预计月成本/ }))
    await waitFor(() => expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions?view=insights'))
    expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument()
  })

  // Renders 240 rows and walks two view switches plus Back: ~2.2s under coverage in
  // isolation, so the default 5s budget has no headroom under full-suite contention.
  it('restores details main and list scroll after insights, including Back', async () => {
    const rows = Array.from({ length: 120 }, (_, index) => ({
      ...subscription,
      subscription_id: `sub_${index}`,
      vps_id: `vps_${index}`,
      display_name: `Host ${index}`,
    }))
    const vpsRows = Array.from({ length: 120 }, (_, index) => ({
      ...vps,
      vps_id: `vps_${index}`,
      display_name: `Host ${index}`,
    }))
    setupSubscriptionFetch({ subscriptions: rows, vpsRows })
    render(
      <MemoryRouter initialEntries={['/subscriptions?view=details']}>
        <HistoryControls />
        <div id="main-content">
          <SubscriptionsPage />
        </div>
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    const main = document.getElementById('main-content')
    const list = document.querySelector('.subscription-list-scroll')
    if (!main || !(list instanceof HTMLElement)) throw new Error('scroll containers missing')
    main.scrollTop = 2000
    fireEvent.scroll(main)
    list.scrollLeft = 200
    fireEvent.scroll(list)
    openInsights()
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    main.scrollTop = 237
    fireEvent.scroll(main)
    fireEvent.click(screen.getByRole('tab', { name: '明细' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    expect(main.scrollTop).toBe(2000)
    expect(list.scrollLeft).toBe(200)
    openInsights()
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'history-back' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    expect(main.scrollTop).toBe(2000)
    expect(list.scrollLeft).toBe(200)
  }, 15_000)

  it('defaults naked /subscriptions to insights and supports tab navigation', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription] })
    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <HistoryControls />
        <SubscriptionsPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions')
    expect(document.querySelector('.subscription-page')).toHaveAttribute('data-view', 'insights')

    fireEvent.click(screen.getByRole('tab', { name: '明细' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions?view=details')
    expect(document.querySelector('.subscription-page')).toHaveAttribute('data-view', 'details')

    openInsights()
    await waitFor(() => expect(screen.getByRole('region', { name: '订阅成本洞察' })).toBeInTheDocument())
    expect(screen.getByTestId('search')).toHaveTextContent('/subscriptions')
    expect(document.querySelector('.subscription-page')).toHaveAttribute('data-view', 'insights')
  })
})
