import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from './AssetDecisionsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const subscription = {
  subscription_id: 'sub_001',
  vps_id: 'vps_review',
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

const vps = {
  vps_id: 'vps_review',
  display_name: 'Tokyo Review',
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
  renewal_decision: 'unreviewed',
  importance: 'normal',
  labels: ['edge'],
  note: '',
  active_node_link_count: 1,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
}

const migrateVPS = {
  ...vps,
  vps_id: 'vps_migrate',
  display_name: 'Frankfurt Migration',
  country: 'DE',
  region: 'Hesse',
  city: 'Frankfurt',
  renewal_decision: 'migrate',
}

const cancelVPS = {
  ...vps,
  vps_id: 'vps_cancel',
  display_name: 'Seoul Cancel',
  country: 'KR',
  region: 'Seoul',
  city: 'Seoul',
  renewal_decision: 'cancel',
  active_node_link_count: 0,
}

describe('AssetDecisionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders a unified decision queue and reloads subscription renewals when the window changes', async () => {
    const laterSubscription = {
      ...subscription,
      subscription_id: 'sub_002',
      vps_id: 'vps_migrate',
      renew_at: '2026-06-08',
      price: 24,
      monthly_price: 8,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([subscription, laterSubscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([migrateVPS]))
      .mockResolvedValueOnce(mockJSONResponse([cancelVPS]))
      .mockResolvedValueOnce(mockJSONResponse([laterSubscription]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
    expect(screen.getByRole('heading', { name: '资产决策' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '资产决策工作队列' })).toBeInTheDocument()
    expect(screen.getByText('2026-05-20')).toBeInTheDocument()
    expect(screen.getByText('Frankfurt Migration')).toBeInTheDocument()
    expect(screen.getByText('Seoul Cancel')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/subscriptions?sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps?renewal_decision=unreviewed', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps?renewal_decision=migrate', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps?renewal_decision=cancel', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })

    fireEvent.change(screen.getByLabelText('续费窗口'), { target: { value: '60' } })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/subscriptions?renew_within_days=60&sort=renew_at&order=asc', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    expect(screen.getByText('2026-06-08')).toBeInTheDocument()
  })

  it('updates a VPS renewal decision and moves it between decision queues', async () => {
    const updated = {
      ...vps,
      renewal_decision: 'migrate',
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([migrateVPS]))
      .mockResolvedValueOnce(mockJSONResponse([cancelVPS]))
      .mockResolvedValueOnce(mockJSONResponse(updated))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '处理 vps_review' }))
    const drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'migrate' } })
    fireEvent.change(within(drawer).getByLabelText('决策理由'), { target: { value: 'move to Osaka' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText('续费决策已保存：Tokyo Review -> 迁移')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps/vps_review', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        renewal_decision: 'migrate',
        renewal_reason: 'move to Osaka',
      }),
    })
    fireEvent.click(screen.getByRole('tab', { name: /待评估/ }))
    expect(within(screen.getByLabelText('资产决策工作队列')).queryByText('Tokyo Review')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: /迁移/ }))
    expect(within(screen.getByLabelText('资产决策工作队列')).getByText('Tokyo Review')).toBeInTheDocument()
  })

  it('shows next actions when a queue view is empty', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('tab', { name: /未关联/ }))
    await waitFor(() => expect(screen.getByRole('heading', { name: '当前视图暂无待处理 VPS' })).toBeInTheDocument())
    expect(screen.getByText('这组队列暂时没有需要人工决策的资产；可回到全部队列、库存或订阅证据继续核对。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '核对 VPS 库存' })).toHaveAttribute('href', '/vps')
    expect(screen.getByRole('link', { name: '补充订阅证据' })).toHaveAttribute('href', '/subscriptions')
    fireEvent.click(screen.getByRole('button', { name: '查看全部队列' }))
    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
  })

  it('navigates from a queue row while keeping row actions isolated', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions']}>
        <Routes>
          <Route path="/asset-decisions" element={<AssetDecisionsPage />} />
          <Route path="/vps/:vpsId" element={<div>vps detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: '处理 vps_review' }))
    expect(await screen.findByRole('dialog', { name: '续费决策处理' })).toBeInTheDocument()
    expect(screen.queryByText('vps detail route')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '续费决策处理' })).not.toBeInTheDocument())

    const queueRow = screen.getAllByText('Tokyo Review')
      .map((element) => element.closest('li'))
      .find((row) => row?.classList.contains('asset-decision-row'))
    expect(queueRow).not.toBeNull()
    fireEvent.click(queueRow!)
    await waitFor(() => expect(screen.getByText('vps detail route')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('does not submit draft decisions when the drawer is cancelled, escaped, or overlay-closed', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: '处理 vps_review' }))
    let drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'migrate' } })
    fireEvent.change(within(drawer).getByLabelText('决策理由'), { target: { value: 'draft only' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '续费决策处理' })).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(5)

    fireEvent.click(screen.getByRole('button', { name: '处理 vps_review' }))
    drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    expect(within(drawer).getByLabelText('续费决策')).toHaveValue('unreviewed')
    expect(within(drawer).getByLabelText('决策理由')).toHaveValue('')
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '续费决策处理' })).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(5)

    fireEvent.click(screen.getByRole('button', { name: '处理 vps_review' }))
    drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'migrate' } })
    const overlay = document.body.querySelector('.drawer-overlay')
    expect(overlay).not.toBeNull()
    fireEvent.mouseDown(overlay!)
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '续费决策处理' })).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('shows a queue error instead of misreporting missing subscriptions when all subscription evidence fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscription evidence unavailable' }, 500))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([migrateVPS]))
      .mockResolvedValueOnce(mockJSONResponse([cancelVPS]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('subscription evidence unavailable')).toBeInTheDocument())
    expect(screen.queryByText('Tokyo Review')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('资产决策队列列表')).not.toBeInTheDocument()
  })
})
