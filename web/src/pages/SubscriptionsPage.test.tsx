import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SubscriptionsPage } from './SubscriptionsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const vps = {
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

const subscription = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 12,
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

describe('SubscriptionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders subscriptions and applies renew-window filters', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/subscriptions?renew_within_days=30']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.queryByRole('dialog', { name: '新建订阅表单' })).not.toBeInTheDocument()
    expect(screen.getAllByText('USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getByText('自动续费')).toBeInTheDocument()

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('shows no-VPS prerequisite with link to VPS page', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '先创建 VPS' })).toHaveAttribute('href', '/vps')
  })

  it('creates subscriptions without sending monthly_price', async () => {
    const created = {
      ...subscription,
      subscription_id: 'sub_new',
      price: 24,
      monthly_price: 12,
      billing_cycle: '2 months',
      billing_months: 2,
      billing_period_length: 2,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse(created, 201))
    vi.stubGlobal('fetch', fetchMock)

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
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions', {
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
        payment_method: '',
        note: '',
      }),
    })
  })

  it('closes URL-requested create drawer without dropping filters', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

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
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('resets URL-requested create draft and errors after drawer cancel', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

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
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('updates subscriptions through PATCH and shows updated billing facts', async () => {
    const updated = {
      ...subscription,
      price: 24,
      billing_cycle: '3 months',
      billing_months: 3,
      billing_period_unit: 'month',
      billing_period_length: 3,
      monthly_price: 8,
      renew_at: '2026-08-01',
      auto_renew: false,
      auto_renew_cancelled: true,
      renewal_mode: 'auto_cancelled',
      status: 'paused' as const,
      payment_method: 'PayPal',
      note: 'review',
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse(updated))
    vi.stubGlobal('fetch', fetchMock)

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
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions/sub_001', {
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
        payment_method: 'PayPal',
        note: 'review',
      }),
    })
  })

  it('links subscription billing facts back to the VPS owner', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([{ ...subscription, auto_renew: false, auto_renew_cancelled: true, renewal_mode: 'auto_cancelled' }]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('已取消自动续费')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '回到 VPS' })).toHaveAttribute('href', '/vps/vps_001')
  })

  it('shows subscription error state with retry', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscriptions unavailable' }, 500))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('加载失败')).toBeInTheDocument())
    expect(screen.getByText('subscriptions unavailable')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    await waitFor(() => expect(screen.getByText('尚未记录订阅')).toBeInTheDocument())
  })

  it('resets subscription edit draft and errors after drawer cancel', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
    vi.stubGlobal('fetch', fetchMock)

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
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
