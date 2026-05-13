import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSPage } from './VPSPage'

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
  active_node_link_count: 1,
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
  active_node_link_count: 0,
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
  })

  it('renders inventory quick views, applies drawer filters, and navigates to detail on row click', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([vps, missingFactsVPS]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
          <Route path="/vps/:vpsId" element={<div>vps detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '库存核对' })).toBeInTheDocument()
    expect(screen.getAllByText('在用').length).toBeGreaterThan(0)
    expect(screen.getAllByText('承载业务').length).toBeGreaterThan(0)
    expect(screen.getAllByText('保留').length).toBeGreaterThan(0)
    expect(screen.getByText('USD 12.00/月')).toBeInTheDocument()
    expect(screen.getAllByText('缺订阅').length).toBeGreaterThan(0)
    expect(screen.getAllByText('未关联 Node').length).toBeGreaterThan(0)
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/providers', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions?sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })

    fireEvent.click(screen.getByRole('tab', { name: /未关联/ }))
    expect(screen.getByText('Osaka Missing')).toBeInTheDocument()
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.getByText('视图: 未关联')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))
    const drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('生命周期'), { target: { value: 'testing' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))
    expect(screen.getByText('生命周期: 测试中')).toBeInTheDocument()
    expect(screen.queryByText('Osaka Missing')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(3)

    fireEvent.click(screen.getByRole('button', { name: /移除筛选 生命周期/ }))
    fireEvent.click(screen.getByRole('button', { name: /移除筛选 视图/ }))
    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Tokyo Edge'))
    await waitFor(() => expect(screen.getByText('vps detail route')).toBeInTheDocument())
  })

  it('keeps VPS rows visible and does not mark missing subscriptions when subscription evidence fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([missingFactsVPS]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscription database unavailable' }, 500))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=unknown&renewal_decision=unreviewed']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    expect(screen.getByText('订阅未知')).toBeInTheDocument()
    expect(screen.getByText('证据不可用')).toBeInTheDocument()
    expect(screen.getByText(/订阅证据不可用，缺订阅视图暂不作为事实/)).toBeInTheDocument()
    expect(within(screen.getByRole('table')).queryByText('缺订阅')).not.toBeInTheDocument()
    expect(screen.queryByText('无法核算续费')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '缺订阅' }))
    expect(screen.queryByText('Osaka Missing')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /移除筛选 视图/ }))
    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    expect(screen.queryByText('视图: 缺订阅')).not.toBeInTheDocument()
  })

  it('marks missing subscriptions only after subscription evidence is ready', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([missingFactsVPS]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=missing_subscription']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    expect(screen.getAllByText('缺订阅').length).toBeGreaterThan(0)
    expect(screen.getByText('无法核算续费')).toBeInTheDocument()
  })

  it('creates a VPS and navigates to the created detail route', async () => {
    const created = { ...vps, vps_id: 'vps_new', display_name: 'Osaka Standby' }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse(created, 201))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
          <Route path="/vps/:vpsId" element={<div>created vps detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '创建第一台 VPS' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一台 VPS' }))
    fireEvent.change(screen.getByLabelText('VPS 名称'), { target: { value: 'Osaka Standby' } })
    fireEvent.change(screen.getByLabelText('资产服务商'), { target: { value: 'pv_001' } })
    fireEvent.change(screen.getByLabelText('国家'), { target: { value: 'JP' } })
    fireEvent.change(screen.getByLabelText('区域'), { target: { value: 'Kansai' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Osaka' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'standby, standby' } })
    fireEvent.click(screen.getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(screen.getByText('created vps detail')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        display_name: 'Osaka Standby',
        provider_id: 'pv_001',
        provider_name: 'Hetzner',
        product_name: '',
        order_ref: '',
        country: 'JP',
        region: 'Kansai',
        city: 'Osaka',
        datacenter: '',
        ipv4: '',
        ipv6: '',
        ssh_host: '',
        ssh_port: 22,
        ssh_user: 'root',
        os_name: '',
        virtualization: '',
        lifecycle_status: 'active',
        usage_status: 'unknown',
        renewal_decision: 'unreviewed',
        importance: 'normal',
        labels: ['standby'],
        note: '',
      }),
    })
  })
})
