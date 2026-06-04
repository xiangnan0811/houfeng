import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ProvidersPage } from './ProvidersPage'
import type { ProviderRecord, SubscriptionRecord, VPSAssetRecord } from '../lib/types'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function provider(overrides: Partial<ProviderRecord> = {}): ProviderRecord {
  return {
    provider_id: 'pv_001',
    name: 'Hetzner',
    website: 'https://hetzner.com',
    panel_url: 'https://console.hetzner.cloud',
    account_hint: 'main',
    country: 'DE',
    note: '',
    rating: 5,
    labels: ['core'],
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    ...overrides,
  }
}

function vps(overrides: Partial<VPSAssetRecord> = {}): VPSAssetRecord {
  return {
    vps_id: 'vps_001',
    display_name: 'Tokyo Edge',
    provider_id: 'pv_001',
    provider_name: 'Hetzner',
    product_name: '',
    order_ref: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: '',
    ipv4: '',
    ipv6: '',
    ssh_host: '',
    ssh_port: 22,
    ssh_user: '',
    os_name: '',
    virtualization: '',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 1,
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    ...overrides,
  }
}

function subscription(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return {
    subscription_id: 'sub_001',
    vps_id: 'vps_001',
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 12,
    started_at: null,
    renew_at: '2026-07-01',
    auto_renew: false,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: '',
    note: '',
    monthly_price_base: 84,
    yearly_price_base: 1008,
    base_currency: 'CNY',
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    ...overrides,
  }
}

function renderProvidersPage() {
  return render(
    <MemoryRouter>
      <ProvidersPage />
    </MemoryRouter>,
  )
}

function expectExactText(text: string) {
  expect(
    screen.getByText((_, element) => {
      if (!element || element.textContent !== text) return false
      return Array.from(element.children).every((child) => child.textContent !== text)
    }),
  ).toBeInTheDocument()
}

function stubInitialLoad({
  providers = [provider()],
  vpsAssets = [vps()],
  subscriptions = [subscription()],
  vpsStatus = 200,
  subscriptionsStatus = 200,
}: {
  providers?: unknown[]
  vpsAssets?: unknown[]
  subscriptions?: unknown[]
  vpsStatus?: number
  subscriptionsStatus?: number
} = {}) {
  const fetchMock = vi
    .fn()
    .mockImplementation((url: string) => {
      if (url === '/api/providers') return Promise.resolve(mockJSONResponse(providers))
      if (url === '/api/vps') {
        return Promise.resolve(
          mockJSONResponse(vpsStatus >= 400 ? { error: 'vps unavailable' } : vpsAssets, vpsStatus),
        )
      }
      if (url === '/api/subscriptions') {
        return Promise.resolve(
          mockJSONResponse(
            subscriptionsStatus >= 400 ? { error: 'subscriptions unavailable' } : subscriptions,
            subscriptionsStatus,
          ),
        )
      }
      return Promise.reject(new Error(`unhandled ${url}`))
    })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('ProvidersPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders provider directory context from providers, VPS, and subscriptions', async () => {
    const providerNote = '关键生产服务商，账单入口跟实际续费日期需要人工核对。'
    const providers = [
      provider({ note: providerNote }),
      provider({
        provider_id: 'pv_002',
        name: 'Vultr',
        panel_url: '',
        account_hint: '',
        country: '',
        rating: null,
        labels: ['edge'],
      }),
    ]
    const fetchMock = stubInitialLoad({
      providers,
      vpsAssets: [
        vps(),
        vps({ vps_id: 'vps_002', display_name: 'Backup Edge', provider_id: 'pv_001' }),
      ],
      subscriptions: [
        subscription(),
        subscription({ subscription_id: 'sub_002', vps_id: 'vps_002', monthly_price_base: 21, yearly_price_base: 252, base_currency: 'CNY' }),
      ],
    })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '服务商目录' })).toBeInTheDocument())
    expect(screen.getByText('供 VPS 与订阅引用的低频资产事实')).toBeInTheDocument()
    expect(screen.getByText('我的评分与外部口碑入口分离')).toBeInTheDocument()
    expect(screen.getByText('2 个服务商')).toBeInTheDocument()
    expect(screen.getByText('1 个待补事实')).toBeInTheDocument()
    expect(screen.getByLabelText('服务商目录摘要')).toHaveTextContent('4 外部口碑源入口')
    expect(screen.getByRole('heading', { name: '服务商与入口' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '目录与入口' })).not.toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '服务入口' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '标签 / 备注' })).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: '操作' })).not.toBeInTheDocument()
    expectExactText('2 VPS · 2 订阅')
    expect(document.querySelector('col[style="width: 160px;"]')).toBeInTheDocument()
    expect(document.querySelector('col[style="width: 122px;"]')).toBeInTheDocument()
    expect(document.querySelector('col[style="width: 154px;"]')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '我的评分' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '外部口碑' })).toBeInTheDocument()
    expect(screen.queryByText('缺面板入口')).not.toBeInTheDocument()
    expect(screen.queryByText('网站已记录')).not.toBeInTheDocument()
    expect(screen.queryByText('网站未记录')).not.toBeInTheDocument()
    expect(screen.queryByText('账号未记录')).not.toBeInTheDocument()
    expect(screen.queryByText('未标记')).not.toBeInTheDocument()
    expect(screen.queryByText('入口，不代表我的评分')).not.toBeInTheDocument()
    expect(screen.queryByText('成本未折算')).not.toBeInTheDocument()
    expect(screen.queryByText(/CNY .*\/月/)).not.toBeInTheDocument()
    expect(screen.getByText(providerNote)).toHaveClass('provider-directory-note')
    expect(screen.getByText(providerNote)).toHaveAttribute('title', providerNote)
    expect(screen.getByRole('button', { name: '编辑服务商 Hetzner' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '在 LET 搜索 Hetzner 外部口碑' })).toHaveAttribute('href', 'https://lowendtalk.com/search?Search=Hetzner')
    expect(screen.getByRole('link', { name: '在 Trustpilot 搜索 Hetzner 外部口碑' })).toHaveAttribute('href', 'https://www.trustpilot.com/search?query=Hetzner')
    expect(screen.getByRole('link', { name: '在 VPSBenchmarks 搜索 Hetzner 外部口碑' })).toHaveAttribute('href', 'https://www.vpsbenchmarks.com/search?search=Hetzner')
    expect(screen.getByRole('link', { name: '查看 Hetzner VPS' })).toHaveAttribute('href', '/vps?provider_id=pv_001')
    expect(screen.getByRole('link', { name: '查看 Hetzner 订阅' })).toHaveAttribute('href', '/subscriptions?provider_id=pv_001')
    expect(screen.getByRole('link', { name: '打开官网 Hetzner' })).toHaveAttribute('href', 'https://hetzner.com')
    expect(screen.getByRole('link', { name: '打开面板 Hetzner' })).toHaveAttribute('href', 'https://console.hetzner.cloud')
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('keeps asset context to counts when subscription costs are not safely folded', async () => {
    stubInitialLoad({
      subscriptions: [subscription({ monthly_price_base: null, base_currency: 'CNY' })],
    })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expectExactText('1 VPS · 1 订阅')
    expect(screen.queryByText('1 项订阅')).not.toBeInTheDocument()
    expect(screen.queryByText('成本未折算')).not.toBeInTheDocument()
    expect(screen.queryByText(/CNY .*\/月/)).not.toBeInTheDocument()
  })

  it('keeps providers visible when asset context loading fails', async () => {
    stubInitialLoad({
      vpsStatus: 500,
      subscriptionsStatus: 500,
    })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expect(screen.getByText(/VPS 上下文不可用：vps unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/订阅上下文不可用：subscriptions unavailable/)).toBeInTheDocument()
    expect(screen.queryByText('资产上下文不可用')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '查看 Hetzner VPS' })).not.toBeInTheDocument()
  })

  it('filters providers by asset, account, metadata, rating, and search text', async () => {
    stubInitialLoad({
      providers: [
        provider({ labels: ['core'] }),
        provider({
          provider_id: 'pv_002',
          name: 'Vultr',
          panel_url: '',
          account_hint: 'backup, finance, lab',
          country: 'US',
          rating: 2,
          labels: ['edge'],
        }),
        provider({
          provider_id: 'pv_003',
          name: 'Linode',
          panel_url: 'https://cloud.linode.com',
          account_hint: 'lab',
          country: 'SG',
          rating: null,
          labels: ['lab'],
        }),
      ],
      vpsAssets: [
        vps(),
        vps({ vps_id: 'vps_002', provider_id: 'pv_002', provider_name: 'Vultr' }),
      ],
      subscriptions: [],
    })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '有资产' }))
    expect(screen.getByText('Hetzner')).toBeInTheDocument()
    expect(screen.getByText('Vultr')).toBeInTheDocument()
    expect(screen.queryByText('Linode')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '多账号' }))
    expect(screen.getByText('Vultr')).toBeInTheDocument()
    expect(screen.getByText('+1')).toBeInTheDocument()
    expect(screen.queryByText('Hetzner')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '缺资料' }))
    expect(screen.queryByText('Hetzner')).not.toBeInTheDocument()
    expect(screen.getByText('Vultr')).toBeInTheDocument()
    expect(screen.getByText('Linode')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '低评分' }))
    expect(screen.getByText('Vultr')).toBeInTheDocument()
    expect(screen.queryByText('Linode')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '未评分' }))
    expect(screen.getByText('Linode')).toBeInTheDocument()
    expect(screen.queryByText('Vultr')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    fireEvent.change(screen.getByLabelText('搜索服务商'), { target: { value: 'lab' } })
    expect(screen.getByText('Vultr')).toBeInTheDocument()
    expect(screen.getByText('Linode')).toBeInTheDocument()
    expect(screen.queryByText('Hetzner')).not.toBeInTheDocument()
  })

  it('renders providers and creates a new provider through the API helper path', async () => {
    const created = provider({ provider_id: 'pv_002', name: 'Vultr', rating: 4 })
    const fetchMock = vi
      .fn()
      .mockImplementation((url: string, init?: RequestInit) => {
        if (url === '/api/providers' && init?.method === 'POST') return Promise.resolve(mockJSONResponse(created, 201))
        if (url === '/api/providers') return Promise.resolve(mockJSONResponse([provider()]))
        if (url === '/api/vps') return Promise.resolve(mockJSONResponse([]))
        if (url === '/api/subscriptions') return Promise.resolve(mockJSONResponse([]))
        return Promise.reject(new Error(`unhandled ${url}`))
      })
    vi.stubGlobal('fetch', fetchMock)

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '新建服务商表单' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /新建服务商/ }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    expect(createDialog).toBeInTheDocument()
    expect(within(createDialog).getByText('身份')).toBeInTheDocument()
    expect(within(createDialog).getByText('入口')).toBeInTheDocument()
    expect(within(createDialog).getByText('复盘')).toBeInTheDocument()
    expect(within(createDialog).getByText('多个账号用逗号或换行分隔')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Vultr' } })
    fireEvent.change(within(createDialog).getByLabelText('网站'), { target: { value: 'https://vultr.com' } })
    fireEvent.change(within(createDialog).getByLabelText('面板地址'), { target: { value: 'https://my.vultr.com' } })
    fireEvent.change(within(createDialog).getByLabelText('账号提示'), { target: { value: 'backup' } })
    fireEvent.change(within(createDialog).getByLabelText('国家 / 地区'), { target: { value: 'US' } })
    fireEvent.change(within(createDialog).getByLabelText('评分 (1-5)'), { target: { value: '4' } })
    fireEvent.change(within(createDialog).getByLabelText('标签'), { target: { value: 'edge, edge' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(screen.getByText('Vultr')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/providers', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        name: 'Vultr',
        website: 'https://vultr.com',
        panel_url: 'https://my.vultr.com',
        account_hint: 'backup',
        country: 'US',
        rating: 4,
        labels: ['edge'],
        note: '',
      }),
    })
  })

  it('shows provider empty state and resets create draft/errors after drawer cancel', async () => {
    const fetchMock = stubInitialLoad({ providers: [], vpsAssets: [], subscriptions: [] })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))
    expect(screen.getByText('服务商名称不能为空。')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Draft Provider' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '新建服务商表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))

    const reopened = screen.getByRole('dialog', { name: '新建服务商表单' })
    expect(within(reopened).queryByText('服务商名称不能为空。')).not.toBeInTheDocument()
    expect(within(reopened).getByLabelText('服务商名称')).toHaveValue('')
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('keeps invalid provider input local', async () => {
    const fetchMock = stubInitialLoad({ providers: [], vpsAssets: [], subscriptions: [] })

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))

    expect(screen.getByText('服务商名称不能为空。')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Invalid Rating' } })
    fireEvent.change(within(createDialog).getByLabelText('评分 (1-5)'), { target: { value: '1.5' } })
    fireEvent.submit(within(createDialog).getByRole('button', { name: '创建' }).closest('form')!)

    expect(within(createDialog).getByText('评分必须为 1 到 5。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('updates an existing provider through PATCH and refreshes the row', async () => {
    const updated = provider({
      name: 'Hetzner Cloud',
      rating: null,
      labels: ['core', 'backup'],
      updated_at: '2026-05-09T09:00:00Z',
    })
    const fetchMock = vi
      .fn()
      .mockImplementation((url: string, init?: RequestInit) => {
        if (url === '/api/providers/pv_001' && init?.method === 'PATCH') return Promise.resolve(mockJSONResponse(updated))
        if (url === '/api/providers') return Promise.resolve(mockJSONResponse([provider()]))
        if (url === '/api/vps') return Promise.resolve(mockJSONResponse([]))
        if (url === '/api/subscriptions') return Promise.resolve(mockJSONResponse([]))
        return Promise.reject(new Error(`unhandled ${url}`))
      })
    vi.stubGlobal('fetch', fetchMock)

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '编辑服务商表单' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑服务商 Hetzner' }))
    const editDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    expect(editDialog).toBeInTheDocument()
    fireEvent.change(within(editDialog).getByLabelText('服务商名称'), { target: { value: 'Hetzner Cloud' } })
    fireEvent.change(within(editDialog).getByLabelText('评分 (1-5)'), { target: { value: '' } })
    fireEvent.change(within(editDialog).getByLabelText('标签'), { target: { value: 'core, backup, core' } })
    fireEvent.click(within(editDialog).getByRole('button', { name: '保存' }))

    await waitFor(() => expect(screen.getByText('Hetzner Cloud')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith('/api/providers/pv_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        name: 'Hetzner Cloud',
        website: 'https://hetzner.com',
        panel_url: 'https://console.hetzner.cloud',
        account_hint: 'main',
        country: 'DE',
        rating: null,
        labels: ['core', 'backup'],
        note: '',
      }),
    })
  })

  it('shows provider error state with retry action', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce((url: string) => {
        if (url === '/api/providers') return Promise.resolve(mockJSONResponse({ error: 'database unavailable' }, 500))
        if (url === '/api/vps') return Promise.resolve(mockJSONResponse([]))
        if (url === '/api/subscriptions') return Promise.resolve(mockJSONResponse([]))
        return Promise.reject(new Error(`unhandled ${url}`))
      })
      .mockImplementation((url: string) => {
        if (url === '/api/providers') return Promise.resolve(mockJSONResponse([]))
        if (url === '/api/vps') return Promise.resolve(mockJSONResponse([]))
        if (url === '/api/subscriptions') return Promise.resolve(mockJSONResponse([]))
        return Promise.reject(new Error(`unhandled ${url}`))
      })
    vi.stubGlobal('fetch', fetchMock)

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('加载失败')).toBeInTheDocument())
    expect(screen.getByText('database unavailable')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6))
    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
  })

  it('resets provider edit draft and errors after drawer cancel', async () => {
    const fetchMock = stubInitialLoad()

    renderProvidersPage()

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑服务商 Hetzner' }))
    const firstEditDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    fireEvent.change(within(firstEditDialog).getByLabelText('服务商名称'), { target: { value: '' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(within(firstEditDialog).getByText('服务商名称不能为空。')).toBeInTheDocument())
    fireEvent.change(within(firstEditDialog).getByLabelText('服务商名称'), { target: { value: 'Draft Hetzner' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑服务商表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑服务商 Hetzner' }))

    const editDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    expect(within(editDialog).queryByText('服务商名称不能为空。')).not.toBeInTheDocument()
    expect(within(editDialog).getByLabelText('服务商名称')).toHaveValue('Hetzner')
    expect(within(editDialog).getByLabelText('评分 (1-5)')).toHaveValue(5)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})
