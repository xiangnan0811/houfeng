import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SubscriptionsPage } from './SubscriptionsPage'
import type {
  SubscriptionBudgetRecord,
  SubscriptionCostSettings,
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

const defaultSettings: SubscriptionCostSettings = {
  base_currency: 'CNY',
  exchange_rate_provider: 'frankfurter',
  fixer_configured: false,
  fixer_masked_summary: '',
  default_reminder_offsets_days: [14, 7, 1],
  max_reminder_lead_days: 30,
  exchange_rate_stale_after_hours: 36,
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
    budget_risk_count: subscriptions.filter((sub) => sub.budget_status === 'warning' || sub.budget_status === 'over').length,
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
    vps_costs: [],
    missing_subscription_assets: [],
    ...overrides,
  }
}

function statisticsFor(subscriptions: SubscriptionRecord[] = [], overrides: Partial<SubscriptionStatistics> = {}): SubscriptionStatistics {
  const totalMonthly = subscriptions.reduce((sum, sub) => sum + (sub.monthly_price_base ?? 0), 0)
  return {
    window: 'month',
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
    renewal_month_buckets: [],
    budget_statuses: [],
    ...overrides,
  }
}

type SubscriptionFetchOptions = {
  subscriptions?: SubscriptionRecord[]
  vpsRows?: VPSAssetRecord[]
  budgets?: SubscriptionBudgetRecord[]
  settings?: SubscriptionCostSettings
  subscriptionsErrorOnce?: string
}

function setupSubscriptionFetch({
  subscriptions = [],
  vpsRows = [vps],
  budgets = [],
  settings = defaultSettings,
  subscriptionsErrorOnce,
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
    if (url === '/api/subscriptions/statistics?window=month') return Promise.resolve(mockJSONResponse(statisticsFor(currentSubscriptions)))
    if (url === '/api/subscription-budgets') return Promise.resolve(mockJSONResponse(budgets))
    if (url === '/api/subscriptions/settings') return Promise.resolve(mockJSONResponse(settings))
    return Promise.resolve(mockJSONResponse({ error: `unhandled ${method} ${url}` }, 404))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function findCall(fetchMock: ReturnType<typeof vi.fn>, url: string, method = 'GET') {
  return fetchMock.mock.calls.find(([calledUrl, init]) => calledUrl === url && ((init as RequestInit | undefined)?.method ?? 'GET') === method)
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

    expect(fetchMock).toHaveBeenCalledWith('/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
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
    expect(fetchMock).toHaveBeenCalledTimes(12)
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
    expect(fetchMock).toHaveBeenCalledTimes(12)
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
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const editDialog = screen.getByRole('dialog', { name: '编辑订阅表单' })
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

  it('links subscription billing facts back to the VPS owner', async () => {
    setupSubscriptionFetch({
      subscriptions: [{ ...subscription, auto_renew: false, auto_renew_cancelled: true, renewal_mode: 'auto_cancelled' }],
    })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('已取消自动续费')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '回到 VPS' })).toHaveAttribute('href', '/vps/vps_001')
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

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(12))
    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
  })

  it('resets subscription edit draft and errors after drawer cancel', async () => {
    const fetchMock = setupSubscriptionFetch({ subscriptions: [subscription] })

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const firstEditDialog = screen.getByRole('dialog', { name: '编辑订阅表单' })
    fireEvent.change(within(firstEditDialog).getByLabelText('币种'), { target: { value: '__custom' } })
    fireEvent.change(within(firstEditDialog).getByLabelText('自定义币种'), { target: { value: 'US1' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '保存订阅' }))
    await waitFor(() => expect(within(firstEditDialog).getByText('币种必须为 3 位大写代码。')).toBeInTheDocument())
    fireEvent.change(within(firstEditDialog).getByLabelText('支付方式'), { target: { value: '__custom' } })
    fireEvent.change(within(firstEditDialog).getByLabelText('自定义支付方式'), { target: { value: 'draft-pay' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑订阅表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    const editDialog = screen.getByRole('dialog', { name: '编辑订阅表单' })
    expect(within(editDialog).queryByText('币种必须为 3 位大写代码。')).not.toBeInTheDocument()
    expect(within(editDialog).getByLabelText('币种')).toHaveValue('USD')
    expect(within(editDialog).getByLabelText('支付方式')).toHaveValue('__custom')
    expect(within(editDialog).getByLabelText('自定义支付方式')).toHaveValue('card')
    expect(fetchMock).toHaveBeenCalledTimes(6)
  })
})
