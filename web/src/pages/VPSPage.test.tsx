import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSPage } from './VPSPage'

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname}{location.search}</span>
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
    expect(screen.getByRole('heading', { name: 'VPS 资产' })).toBeInTheDocument()
    expect(screen.getAllByText('在用').length).toBeGreaterThan(0)
    expect(screen.getAllByText('保留').length).toBeGreaterThan(0)
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

    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
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
      <MemoryRouter initialEntries={['/vps']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    // Subscription error is shown as a status message
    expect(screen.getByRole('status')).toHaveTextContent('订阅不可用，不判定。')

    // Missing subscription tab should not show items when evidence is unavailable
    fireEvent.click(screen.getByRole('tab', { name: '缺订阅' }))
    expect(screen.queryByText('Osaka Missing')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /移除筛选 视图/ }))
    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    expect(screen.queryByText('视图: 缺订阅')).not.toBeInTheDocument()
  })

  it('shows cancellation attention view for inactive subscription and active VPS split', async () => {
    const expiredSubscription = {
      ...subscription,
      status: 'expired',
      auto_renew: false,
      auto_renew_cancelled: true,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([expiredSubscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=cancellation_attention']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getByText('订阅非活跃，VPS 尚未取消')).toBeInTheDocument()
    expect(screen.getByText('视图: 取消待处理')).toBeInTheDocument()
  })

  it('uses running linked assets rather than historical links for cancellation attention', async () => {
    const toCancelWithRetiredLinks = {
      ...vps,
      lifecycle_status: 'to_cancel',
      renewal_decision: 'cancel',
      active_monitoring_instance_link_count: 2,
      running_monitoring_instance_count: 0,
      running_target_count: 0,
    }
    const toCancelWithRunningTarget = {
      ...toCancelWithRetiredLinks,
      vps_id: 'vps_running_target',
      display_name: 'Frankfurt Legacy',
      running_target_count: 1,
    }
    const cancelledWithRunningTarget = {
      ...toCancelWithRunningTarget,
      vps_id: 'vps_cancelled_running_target',
      display_name: 'Cancelled Legacy',
      lifecycle_status: 'cancelled',
    }
    const cancelledSubscription = {
      ...subscription,
      status: 'cancelled',
      auto_renew: false,
      auto_renew_cancelled: true,
    }
    const runningTargetSubscription = {
      ...cancelledSubscription,
      subscription_id: 'sub_running_target',
      vps_id: 'vps_running_target',
    }
    const cancelledRunningTargetSubscription = {
      ...cancelledSubscription,
      subscription_id: 'sub_cancelled_running_target',
      vps_id: 'vps_cancelled_running_target',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([
        toCancelWithRetiredLinks,
        toCancelWithRunningTarget,
        cancelledWithRunningTarget,
      ]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([
        cancelledSubscription,
        runningTargetSubscription,
        cancelledRunningTargetSubscription,
      ]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=cancellation_attention']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Frankfurt Legacy')).toBeInTheDocument())
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.getByText('Cancelled Legacy')).toBeInTheDocument()
    expect(screen.getByText('VPS 待取消，仍有 1 个监控实例/入口探测运行')).toBeInTheDocument()
    expect(screen.getByText('VPS 已取消，仍有 1 个监控实例/入口探测运行')).toBeInTheDocument()
  })

  it('does not apply draft drawer filters when closed by button, Escape, or overlay', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([vps, missingFactsVPS]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=unlinked']}>
        <Routes>
          <Route
            path="/vps"
            element={(
              <>
                <LocationProbe />
                <VPSPage />
              </>
            )}
          />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
    expect(screen.queryByText('Tokyo Edge')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps?view=unlinked')

    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    let drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('生命周期'), { target: { value: 'testing' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'VPS 高级筛选' })).not.toBeInTheDocument())
    expect(screen.getByText('Osaka Missing')).toBeInTheDocument()
    expect(screen.queryByText('生命周期: 测试中')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps?view=unlinked')

    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('用途状态'), { target: { value: 'in_use' } })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'VPS 高级筛选' })).not.toBeInTheDocument())
    expect(screen.queryByText('用途: 承载业务')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps?view=unlinked')

    fireEvent.click(screen.getByRole('button', { name: '筛选' }))
    drawer = await screen.findByRole('dialog', { name: 'VPS 高级筛选' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'keep' } })
    const overlay = document.body.querySelector('.modal-overlay')
    expect(overlay).not.toBeNull()
    fireEvent.click(overlay!)
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'VPS 高级筛选' })).not.toBeInTheDocument())
    expect(screen.queryByText('续费: 保留')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps?view=unlinked')
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('marks missing subscriptions only after subscription evidence is ready', async () => {
    let resolveSubscriptions: (value: Response) => void = () => {}
    const subscriptionsPromise = new Promise<Response>((resolve) => {
      resolveSubscriptions = resolve
    })
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([missingFactsVPS]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockReturnValueOnce(subscriptionsPromise)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps?view=missing_subscription']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
        </Routes>
      </MemoryRouter>,
    )

    // While subscriptions are loading, missing_subscription view should not show items
    await waitFor(() => expect(screen.getByRole('heading', { name: 'VPS 资产' })).toBeInTheDocument())
    expect(screen.queryByText('Osaka Missing')).not.toBeInTheDocument()

    resolveSubscriptions(mockJSONResponse([]))

    await waitFor(() => expect(screen.getByText('Osaka Missing')).toBeInTheDocument())
  })

  it('opens VPS creation modal, creates a VPS, and navigates to the detail route', async () => {
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
    expect(screen.queryByRole('dialog', { name: '添加 VPS' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'VPS 资产' })).toBeInTheDocument()
    expect(screen.getByText('还没有录入 VPS 资产')).toBeInTheDocument()
    expect(screen.getByText('先录入 VPS。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '创建第一台 VPS' }))
    const modal = await screen.findByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByText('核心信息')).toBeInTheDocument()
    expect(within(modal).getByText('网络入口')).toBeInTheDocument()
    expect(within(modal).getByText('创建后进入详情页。')).toBeInTheDocument()
    expect(within(modal).queryByLabelText('生命周期')).not.toBeInTheDocument()
    expect(within(modal).queryByLabelText('用途状态')).not.toBeInTheDocument()
    fireEvent.click(within(modal).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '添加 VPS' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一台 VPS' }))
    const reopenedModal = await screen.findByRole('dialog', { name: '添加 VPS' })
    expect(within(reopenedModal).getByLabelText('VPS 名称')).toHaveValue('')
    fireEvent.change(within(reopenedModal).getByLabelText('VPS 名称'), { target: { value: 'Osaka Standby' } })
    fireEvent.change(within(reopenedModal).getByLabelText('服务商'), { target: { value: 'pv_001' } })
    fireEvent.change(within(reopenedModal).getByLabelText('国家'), { target: { value: 'JP' } })
    fireEvent.click(within(reopenedModal).getByRole('button', { name: /补充信息/ }))
    fireEvent.change(within(reopenedModal).getByLabelText('区域'), { target: { value: 'Kansai' } })
    fireEvent.change(within(reopenedModal).getByLabelText('城市'), { target: { value: 'Osaka' } })
    fireEvent.change(within(reopenedModal).getByLabelText('标签'), { target: { value: 'standby, standby' } })
    fireEvent.click(within(reopenedModal).getByRole('button', { name: '创建 VPS' }))

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
