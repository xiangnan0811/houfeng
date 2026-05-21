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
  active_node_link_count: 0,
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
  monthly_price: 12,
  started_at: '2026-05-01',
  renew_at: '2026-06-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  status: 'active',
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
      <MemoryRouter initialEntries={['/subscriptions']}>
        <SubscriptionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Edge').length).toBeGreaterThan(0))
    expect(screen.getByText('续费与成本证据')).toBeInTheDocument()
    expect(screen.getByText('当前筛选上下文')).toBeInTheDocument()
    expect(screen.getByText('订阅续费证据表')).toBeInTheDocument()
    expect(screen.getByText('当前筛选')).toBeInTheDocument()
    expect(screen.getByText('下一笔续费证据')).toBeInTheDocument()
    expect(screen.getByText(/URL 是订阅证据表的请求真相/)).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '订阅创建表单' })).not.toBeInTheDocument()
    expect(screen.getAllByText('USD 12.00').length).toBeGreaterThan(0)
    expect(screen.getAllByText('生效中').length).toBeGreaterThan(0)

    fireEvent.change(screen.getByLabelText('续费窗口'), { target: { value: '30' } })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    expect(screen.getByText('续费窗口: 未来 30 天')).toBeInTheDocument()
  })

  it('creates subscriptions without sending monthly_price', async () => {
    const created = { ...subscription, subscription_id: 'sub_new', price: 24, monthly_price: 12, billing_months: 2 }
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

    await waitFor(() => expect(screen.getByText('尚未记录订阅续费证据')).toBeInTheDocument())
    expect(screen.getByText(/原始价格、周期和续费日期支撑资产成本判断/)).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '订阅创建表单' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '创建第一条订阅' }))
    const createDialog = screen.getByRole('dialog', { name: '订阅创建表单' })
    expect(createDialog).toBeInTheDocument()
    expect(within(createDialog).getByText('RENEWAL / COST EVIDENCE')).toBeInTheDocument()
    expect(within(createDialog).getByText(/VPS 绑定通过选择器完成/)).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('订阅 VPS'), { target: { value: 'vps_001' } })
    fireEvent.change(screen.getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(screen.getByLabelText('币种'), { target: { value: 'usd' } })
    fireEvent.change(screen.getByLabelText('计费月数'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('续费日期'), { target: { value: '2026-07-01' } })
    fireEvent.click(screen.getByLabelText('自动续费'))
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))

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
        billing_cycle: 'monthly',
        billing_months: 2,
        started_at: null,
        renew_at: '2026-07-01',
        auto_renew: true,
        auto_renew_cancelled: false,
        status: 'active',
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

    await waitFor(() => expect(screen.getByRole('dialog', { name: '订阅创建表单' })).toBeInTheDocument())
    expect(screen.getByText(/关闭抽屉后仍保留当前 VPS 筛选上下文/)).toBeInTheDocument()
    expect(screen.getByLabelText('订阅 VPS')).toHaveValue('vps_001')
    fireEvent.change(screen.getByLabelText('价格'), { target: { value: '99' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '订阅创建表单' })).not.toBeInTheDocument())
    expect(screen.getByText('VPS: Tokyo Edge')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    fireEvent.click(screen.getByRole('button', { name: '为该 VPS 新建订阅' }))
    expect(screen.getByRole('dialog', { name: '订阅创建表单' })).toBeInTheDocument()
    expect(screen.getByLabelText('订阅 VPS')).toHaveValue('vps_001')
    expect(screen.getByLabelText('价格')).toHaveValue(null)
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

    await waitFor(() => expect(screen.getByRole('dialog', { name: '订阅创建表单' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建订阅' }))
    expect(screen.getByText('价格必须为非负数字。')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('价格'), { target: { value: '99' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '订阅创建表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '为该 VPS 新建订阅' }))

    const createDialog = screen.getByRole('dialog', { name: '订阅创建表单' })
    expect(within(createDialog).queryByText('价格必须为非负数字。')).not.toBeInTheDocument()
    expect(within(createDialog).getByLabelText('订阅 VPS')).toHaveValue('vps_001')
    expect(within(createDialog).getByLabelText('价格')).toHaveValue(null)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('updates subscriptions through PATCH and shows backend monthly price', async () => {
    const updated = {
      ...subscription,
      price: 24,
      billing_cycle: 'quarterly',
      billing_months: 3,
      monthly_price: 8,
      renew_at: '2026-08-01',
      auto_renew: false,
      auto_renew_cancelled: true,
      status: 'paused',
      payment_method: 'paypal',
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
    expect(screen.queryByRole('dialog', { name: '订阅编辑表单' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑 sub_001' }))
    const editDialog = screen.getByRole('dialog', { name: '订阅编辑表单' })
    expect(editDialog).toBeInTheDocument()
    expect(within(editDialog).getByText('EDIT RENEWAL EVIDENCE')).toBeInTheDocument()
    expect(within(editDialog).getByText(/不会提交 monthly_price/)).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('价格'), { target: { value: '24' } })
    fireEvent.change(screen.getByLabelText('计费周期'), { target: { value: 'quarterly' } })
    fireEvent.change(screen.getByLabelText('计费月数'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('续费日期'), { target: { value: '2026-08-01' } })
    fireEvent.click(screen.getByLabelText('自动续费'))
    fireEvent.click(screen.getByLabelText('已取消自动续费'))
    fireEvent.change(screen.getByLabelText('订阅状态'), { target: { value: 'paused' } })
    fireEvent.change(screen.getByLabelText('支付方式'), { target: { value: 'paypal' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'review' } })
    fireEvent.click(screen.getByRole('button', { name: '保存订阅' }))

    await waitFor(() => expect(screen.getByText('月付 USD 8.00')).toBeInTheDocument())
    expect(screen.getAllByText('已暂停').length).toBeGreaterThan(0)
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
        billing_cycle: 'quarterly',
        billing_months: 3,
        started_at: '2026-05-01',
        renew_at: '2026-08-01',
        auto_renew: false,
        auto_renew_cancelled: true,
        status: 'paused',
        payment_method: 'paypal',
        note: 'review',
      }),
    })
  })

  it('shows subscription evidence error state without treating it as missing data', async () => {
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

    await waitFor(() => expect(screen.getByText('订阅证据读取失败')).toBeInTheDocument())
    expect(screen.getByText('subscriptions unavailable')).toBeInTheDocument()
    expect(screen.getByText(/不要把读取失败当作真实缺订阅/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新读取订阅' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    await waitFor(() => expect(screen.getByText('尚未记录订阅续费证据')).toBeInTheDocument())
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
    fireEvent.click(screen.getByRole('button', { name: '编辑 sub_001' }))
    const firstEditDialog = screen.getByRole('dialog', { name: '订阅编辑表单' })
    fireEvent.change(within(firstEditDialog).getByLabelText('币种'), { target: { value: 'US1' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '保存订阅' }))
    await waitFor(() => expect(within(firstEditDialog).getByText('币种必须为 3 位大写代码。')).toBeInTheDocument())
    fireEvent.change(within(firstEditDialog).getByLabelText('支付方式'), { target: { value: 'draft-pay' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '取消编辑' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '订阅编辑表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑 sub_001' }))

    const editDialog = screen.getByRole('dialog', { name: '订阅编辑表单' })
    expect(within(editDialog).queryByText('币种必须为 3 位大写代码。')).not.toBeInTheDocument()
    expect(within(editDialog).getByLabelText('币种')).toHaveValue('USD')
    expect(within(editDialog).getByLabelText('支付方式')).toHaveValue('card')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
