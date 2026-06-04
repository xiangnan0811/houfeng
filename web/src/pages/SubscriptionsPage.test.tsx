import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
  exchange_rate_stale: false,
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
    exchange_rate_stale_count: subscriptions.filter((sub) => sub.exchange_rate_stale).length,
    decision_attention_count: 0,
    missing_subscription_vps_count: 0,
    upcoming_renewals: subscriptions.filter((sub) => sub.renew_at).map((sub) => ({
      subscription_id: sub.subscription_id,
      vps_id: sub.vps_id,
      vps_display_name: 'Tokyo Edge',
      display_name: sub.display_name ?? '',
      provider_name: 'Hetzner',
      renew_at: sub.renew_at,
      monthly_price_base: sub.monthly_price_base,
      yearly_price_base: sub.yearly_price_base,
      base_currency: sub.base_currency ?? 'CNY',
      currency: sub.currency,
      renewal_decision: 'keep',
      lifecycle_status: 'active',
      exchange_rate_stale: Boolean(sub.exchange_rate_stale),
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
      monthly_price_base: sub.monthly_price_base,
      yearly_price_base: sub.yearly_price_base,
      base_currency: sub.base_currency ?? 'CNY',
      exchange_rate: sub.exchange_rate,
      exchange_rate_date: sub.exchange_rate_date,
      exchange_rate_stale: Boolean(sub.exchange_rate_stale),
      renew_at: sub.renew_at,
      next_reminder_at: sub.next_reminder_at,
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
  statistics?: SubscriptionStatistics
  statisticsError?: string
}

function setupSubscriptionFetch({
  subscriptions = [],
  vpsRows = [vps],
  subscriptionsErrorOnce,
  statistics,
  statisticsError,
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
          exchange_rate_stale: false,
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
        exchange_rate_stale: false,
        budget_status: 'ok',
        updated_at: '2026-05-09T09:00:00Z',
      }
      currentSubscriptions = [updated]
      return Promise.resolve(mockJSONResponse(updated))
    }
    if (url === '/api/vps') return Promise.resolve(mockJSONResponse(vpsRows))
    if (url === '/api/subscriptions/overview') return Promise.resolve(mockJSONResponse(overviewFor(currentSubscriptions)))
    if (url === '/api/subscriptions/statistics?window=year') {
      if (statisticsError) return Promise.resolve(mockJSONResponse({ error: statisticsError }, 500))
      return Promise.resolve(mockJSONResponse(statistics ?? statisticsFor(currentSubscriptions)))
    }
    if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
      return Promise.resolve(mockJSONResponse({ provider: 'frankfurter', base_currency: 'CNY', fetched_at: '2026-05-09T08:00:00Z', succeeded: [], failed: [] }))
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

describe('SubscriptionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders subscriptions and applies renew-window filters', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?renew_within_days=30']}>
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

    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('applies provider filters from provider directory links', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?provider_id=pv_001']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByRole('button', { name: /服务商：Hetzner/ })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?provider_id=pv_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows no-VPS prerequisite with link to VPS page', async () => {
    setupSubscriptionFetch({ subscriptions: [], vpsRows: [] })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '先创建 VPS' })).toHaveAttribute('href', '/vps')
  })

  it('creates subscriptions without sending monthly_price', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [], vpsRows: [vps] })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))
    const createDialog = screen.getByRole('dialog', { name: '新建订阅表单' })
    expect(createDialog).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('VPS'), { target: { value: 'vps_001' } })
    fireEvent.change(within(createDialog).getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(within(createDialog).getByLabelText('币种'), { target: { value: 'USD' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期单位'), { target: { value: 'month' } })
    fireEvent.change(within(createDialog).getByLabelText('计费周期长度'), { target: { value: '2' } })
    fireEvent.change(within(createDialog).getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(within(createDialog).getByLabelText('自动续费'))
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建订阅' }))

    await waitFor(() => expect(screen.getByText('USD 24.00')).toBeInTheDocument())
    expect(findCall(fetchMock, '/api/subscriptions', 'POST')).toEqual(['/api/subscriptions', {
      method: 'POST',
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
      }),
    }])
  })

  it('closes URL-requested create drawer without dropping filters', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions?vps_id=vps_001&create=1']}>
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
      <MemoryRouter initialEntries={['/subscriptions?vps_id=vps_001&create=1']}>
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
      <MemoryRouter initialEntries={['/subscriptions']}>
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
      <MemoryRouter initialEntries={['/subscriptions']}>
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
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('加载失败')).toBeInTheDocument())
    expect(screen.getByText('subscriptions unavailable')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
    expect(fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/subscriptions')).length).toBeGreaterThanOrEqual(3)
  })

  it('keeps list usable when statistics panel fails', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription], statisticsError: 'statistics unavailable' })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByText('statistics unavailable')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '订阅明细' })).toBeInTheDocument()
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

    await waitFor(() => expect(screen.getByRole('heading', { name: '成本洞察' })).toBeInTheDocument())
    const insights = screen.getByRole('region', { name: '订阅成本洞察' })
    const headings = within(insights).getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
    expect(headings).toEqual(['月成本', '年度趋势与风险', '成本构成', '续费队列'])
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
  })

  it('renders monthly labels for the annual trend axis', async () => {
    setupSubscriptionFetch({ subscriptions: [subscription] })
    const { container } = render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByText('25/07')).toBeInTheDocument()
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

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByText('历史成本数据不足')).toBeInTheDocument()
    expect(screen.getByText('部分历史月份缺少可用汇率或预算币种不一致，暂不绘制可能误导的趋势曲线。')).toBeInTheDocument()
    expect(container.querySelector('.subscription-insight-panel--trend polyline')).toBeNull()
  })

  it('resets subscription edit draft and errors after drawer cancel', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
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
})
