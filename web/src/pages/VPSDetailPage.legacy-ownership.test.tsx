import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AppShell } from '../app/layout/AppShell'
import * as authCtx from '../lib/auth-context'
import * as recordsApi from '../lib/recordsApi'
import type { SubscriptionRecord, VPSAssetDetail, VPSOverview } from '../lib/types'
import { VPSDetailPage } from './VPSDetailPage'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function capabilityOffOverview(vpsId: string, displayName: string): VPSOverview {
  return {
    generated_at: '2026-08-27T00:00:00Z',
    identity: {
      vps_id: vpsId,
      display_name: displayName,
      provider_name: 'Example',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.1',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: [],
      updated_at: '2026-08-27T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: 'healthy', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '未接入', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: 'unknown', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: 'keep', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
    },
    recent_activity: {
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      items: [],
    },
    facts: [],
    relations: [],
    capabilities: [],
  }
}

function detailFixture(vpsId: string, displayName: string): VPSAssetDetail {
  return {
    vps_id: vpsId,
    display_name: displayName,
    provider_id: 'provider_001',
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: `order_${vpsId}`,
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: 'TK1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'KVM',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-27T00:00:00Z',
    monitoring_instance_links: [],
  }
}

function subscriptionFixture(vpsId: string): SubscriptionRecord {
  return {
    subscription_id: `subscription_${vpsId}`,
    vps_id: vpsId,
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    billing_period_unit: 'month',
    billing_period_length: 1,
    monthly_price: 12,
    auto_renew: false,
    auto_renew_cancelled: false,
    renewal_mode: 'manual',
    status: 'active',
    payment_method: '',
    note: '',
    created_at: '2026-08-27T00:00:00Z',
    updated_at: '2026-08-27T00:00:00Z',
  }
}

function NavigationHarness() {
  const navigate = useNavigate()
  return (
    <>
      <button type="button" onClick={() => navigate('/vps/vps_b')}>切到 B</button>
      <button type="button" onClick={() => navigate('/vps/vps_a')}>切回 A</button>
      <button type="button" onClick={() => navigate('/vps/vps_a?workbench=subscription')}>同 VPS query reload</button>
      <Routes>
        <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
      </Routes>
    </>
  )
}

const authenticatedUser = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

function installAuthenticatedUser() {
  vi.spyOn(authCtx, 'useAuth').mockReturnValue({
    user: authenticatedUser,
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  })
}

function AppShellNavigationLayout() {
  const navigate = useNavigate()
  return (
    <>
      <button type="button" onClick={() => navigate('/dashboard')}>前往工作台</button>
      <button type="button" onClick={() => navigate('/vps/vps_a')}>返回同一 VPS</button>
      <Outlet />
    </>
  )
}

function DashboardNavigationHarness() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route element={<AppShellNavigationLayout />}>
          <Route path="vps/:vpsId" element={<VPSDetailPage />} />
          <Route path="dashboard" element={<h1>工作台测试页</h1>} />
        </Route>
      </Route>
    </Routes>
  )
}

function openSubscriptionDrawer() {
  const command = screen.getAllByRole('button', { name: '创建/更新订阅' })[0]
  if (!command) throw new Error('subscription command must be present')
  fireEvent.click(command)
  return screen.getByRole('dialog', { name: '创建/更新订阅' })
}

function openLegacyActionDrawer(commandName: string, dialogName: string) {
  const summary = screen.getAllByLabelText('VPS 详情操作')[0]
  const menu = summary?.closest('details')
  if (!summary || !menu) throw new Error('legacy VPS actions menu must be present')
  if (!menu.hasAttribute('open')) fireEvent.click(summary)
  fireEvent.click(within(menu).getByRole('button', { name: commandName }))
  return screen.getByRole('dialog', { name: dialogName })
}

function installSingleVPSSubscriptionHarness(
  overviewFor: (vpsId: string) => VPSOverview = (vpsId) => capabilityOffOverview(vpsId, 'Tokyo Edge A'),
) {
  const delayedSubscription = deferred<Response>()
  const initialDetail = detailFixture('vps_a', 'Tokyo Edge A')
  const settledDetail = {
    ...initialDetail,
    display_name: 'Tokyo Edge A converged',
    updated_at: '2026-08-27T01:00:00Z',
  }
  const settledSubscription = subscriptionFixture('vps_a')
  const idempotencyKeys: string[] = []
  let writeSettled = false
  let detailGets = 0

  const getOverview = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation((vpsId) => {
    if (vpsId === 'vps_a') return Promise.resolve(overviewFor(vpsId))
    throw new Error(`unexpected overview ${vpsId}`)
  })
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = (init?.method ?? 'GET').toUpperCase()
    if (url === '/api/dashboard' && method === 'GET') {
      return Promise.resolve(mockJSONResponse({
        snapshot_generated_at: '2026-08-28T00:00:00Z',
        total_monitoring_instance_count: 0,
        total_target_count: 0,
        abnormal_monitoring_instance_count: 0,
        abnormal_target_count: 0,
        severe_monitoring_instance_count: 0,
        severe_target_count: 0,
        maintenance_monitoring_instance_count: 0,
        maintenance_target_count: 0,
        pending_onboarding_monitoring_instance_count: 0,
        paused_monitoring_instance_count: 0,
        retired_monitoring_instance_count: 0,
        paused_target_count: 0,
        archived_target_count: 0,
        recent_new_incident_count: 0,
        recent_recovery_count: 0,
        group_summaries: [],
        notification_status: {
          telegram_configured: false,
          telegram_runtime_managed: false,
          telegram_runtime_apply_active: false,
          feishu_configured: false,
        },
        asset_summary: {
          renewal_due_30d_subscription_count: 0,
          renewal_due_30d_vps_count: 0,
          unreviewed_vps_count: 0,
          to_cancel_vps_count: 0,
          to_migrate_vps_count: 0,
          unlinked_vps_count: 0,
          abnormal_linked_vps_count: 0,
          cost_by_currency: [],
        },
        recent_events: [],
        abnormal_monitoring_instances: [],
        abnormal_targets: [],
      }))
    }
    if (url === '/api/record-notifications/unread-count' && method === 'GET') {
      return Promise.resolve(mockJSONResponse({ unread_count: 0 }))
    }
    if (url === '/api/vps/vps_a' && method === 'GET') {
      detailGets += 1
      return Promise.resolve(mockJSONResponse(writeSettled ? settledDetail : initialDetail))
    }
    if (url === '/api/vps/vps_a/subscriptions' && method === 'POST') {
      idempotencyKeys.push(new Headers(init?.headers).get('Idempotency-Key') ?? '')
      return delayedSubscription.promise
    }
    if (url === '/api/vps/vps_a/timeline') {
      return Promise.resolve(mockJSONResponse({
        vps_id: 'vps_a',
        renewal_decisions: [],
        price_histories: [],
        ip_histories: [],
        spec_snapshots: [],
        experience_logs: [],
      }))
    }
    if (url === '/api/vps/vps_a/services' || url === '/api/vps/vps_a/domains') {
      return Promise.resolve(mockJSONResponse([]))
    }
    if (url.startsWith('/api/subscriptions')) {
      return Promise.resolve(mockJSONResponse(writeSettled ? [settledSubscription] : []))
    }
    throw new Error(`unexpected fetch ${method} ${url}`)
  }))

  return {
    getOverview,
    getDetailGets: () => detailGets,
    idempotencyKeys,
    settle() {
      writeSettled = true
      delayedSubscription.resolve(mockJSONResponse(settledSubscription, 201))
    },
  }
}

describe('VPSDetailPage legacy write ownership', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('revalidates after a current-page write loses view authority when its drawer closes and reopens', async () => {
    const harness = installSingleVPSSubscriptionHarness()

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <NavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const initialDrawer = openSubscriptionDrawer()
    fireEvent.change(within(initialDrawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(initialDrawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    fireEvent.click(within(initialDrawer).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '创建/更新订阅' })).not.toBeInTheDocument())
    const reopenedDrawer = openSubscriptionDrawer()
    expect(within(reopenedDrawer).getByRole('button', { name: '保存中…' })).toBeDisabled()
    expect(screen.getAllByRole('heading', { name: 'Tokyo Edge A' }).length).toBeGreaterThan(0)
    expect(screen.getByText('缺少当前订阅')).toBeInTheDocument()
    expect(harness.getDetailGets()).toBe(1)

    await act(async () => harness.settle())

    await screen.findByRole('heading', { name: 'Tokyo Edge A converged' })
    await waitFor(() => expect(harness.getDetailGets()).toBe(2))
    expect(screen.queryByText('缺少当前订阅')).not.toBeInTheDocument()
    expect(screen.queryByText('订阅账单事实已创建')).not.toBeInTheDocument()
    expect(harness.getOverview).toHaveBeenCalledTimes(2)
    expect(harness.idempotencyKeys).toHaveLength(1)
    expect(harness.idempotencyKeys[0]).not.toBe('')
  })

  it('revalidates after a same-VPS query reload supersedes a pending write view', async () => {
    const harness = installSingleVPSSubscriptionHarness()

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <NavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const initialDrawer = openSubscriptionDrawer()
    fireEvent.change(within(initialDrawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(initialDrawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '同 VPS query reload' }))
    await waitFor(() => expect(harness.getDetailGets()).toBe(2))
    const reloadedDrawer = screen.getByRole('dialog', { name: '创建/更新订阅' })
    expect(within(reloadedDrawer).getByRole('button', { name: '保存中…' })).toBeDisabled()
    expect(screen.getAllByRole('heading', { name: 'Tokyo Edge A' }).length).toBeGreaterThan(0)
    expect(screen.getByText('缺少当前订阅')).toBeInTheDocument()

    await act(async () => harness.settle())

    await waitFor(() => expect(harness.getDetailGets()).toBe(3))
    expect(screen.getAllByRole('heading', { name: 'Tokyo Edge A converged' }).length).toBeGreaterThan(0)
    expect(screen.queryByText('缺少当前订阅')).not.toBeInTheDocument()
    expect(screen.queryByText('订阅账单事实已创建')).not.toBeInTheDocument()
    expect(harness.getOverview).toHaveBeenCalledTimes(2)
    expect(harness.idempotencyKeys).toHaveLength(1)
    expect(harness.idempotencyKeys[0]).not.toBe('')
  })

  it('does not re-probe when a current subscription write commits through its owning view', async () => {
    const harness = installSingleVPSSubscriptionHarness()

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <NavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const drawer = openSubscriptionDrawer()
    fireEvent.change(within(drawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    await act(async () => harness.settle())

    expect(await screen.findByText('订阅账单事实已创建')).toBeInTheDocument()
    expect(screen.queryByText('缺少当前订阅')).not.toBeInTheDocument()
    expect(harness.getOverview).toHaveBeenCalledTimes(1)
    expect(harness.getDetailGets()).toBe(1)
  })

  it('blocks a second same-VPS POST after visiting Dashboard and reloads when the old view settles', async () => {
    installAuthenticatedUser()
    const harness = installSingleVPSSubscriptionHarness()

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <DashboardNavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const initialDrawer = openSubscriptionDrawer()
    fireEvent.change(within(initialDrawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(initialDrawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '前往工作台' }))
    await screen.findByRole('heading', { name: '工作台测试页' })
    fireEvent.click(screen.getByRole('button', { name: '返回同一 VPS' }))
    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    expect(screen.getByText('操作处理中，请等待当前写入完成。')).toBeInTheDocument()

    const blockedExperienceDrawer = openLegacyActionDrawer('记录经验', '记录经验')
    expect(within(blockedExperienceDrawer).getByRole('button', { name: '记录中…' })).toBeDisabled()
    fireEvent.click(within(blockedExperienceDrawer).getByRole('button', { name: '关闭' }))

    const returnedDrawer = openSubscriptionDrawer()
    const blockedSave = within(returnedDrawer).getByRole('button', { name: '保存中…' })
    expect(blockedSave).toBeDisabled()
    fireEvent.click(blockedSave)
    expect(harness.idempotencyKeys).toHaveLength(1)

    await act(async () => harness.settle())

    await screen.findByRole('heading', { name: 'Tokyo Edge A converged' })
    await waitFor(() => expect(harness.getDetailGets()).toBe(3))
    expect(harness.getOverview).toHaveBeenCalledTimes(3)
    expect(harness.idempotencyKeys).toHaveLength(1)
    expect(screen.queryByText('缺少当前订阅')).not.toBeInTheDocument()
    expect(screen.queryByText('订阅账单事实已创建')).not.toBeInTheDocument()
  })

  it('blocks Overview when a pending Legacy write owns the same VPS', async () => {
    installAuthenticatedUser()
    let overviewEnabled = false
    const harness = installSingleVPSSubscriptionHarness((vpsId) => ({
      ...capabilityOffOverview(vpsId, 'Tokyo Edge A'),
      capabilities: overviewEnabled ? ['records_v2_read'] : [],
    }))

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <DashboardNavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const legacyDrawer = openSubscriptionDrawer()
    fireEvent.change(within(legacyDrawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(legacyDrawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '前往工作台' }))
    await screen.findByRole('heading', { name: '工作台测试页' })
    overviewEnabled = true
    fireEvent.click(screen.getByRole('button', { name: '返回同一 VPS' }))

    await screen.findByRole('button', { name: '管理' })
    expect(screen.getByText('操作处理中，请等待当前写入完成。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '订阅事实' }))
    const overviewDrawer = await screen.findByRole('dialog', { name: '订阅事实' })
    const blockedSave = within(overviewDrawer).getByRole('button', { name: '保存中…' })
    expect(blockedSave).toBeDisabled()
    fireEvent.click(blockedSave)
    expect(harness.idempotencyKeys).toHaveLength(1)

    await act(async () => harness.settle())

    await waitFor(() => expect(harness.getOverview).toHaveBeenCalledTimes(3))
    expect(await screen.findByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(harness.idempotencyKeys).toHaveLength(1)
  })

  it('keeps an Overview owner across Dashboard leave/return and reloads the returned view after settle', async () => {
    installAuthenticatedUser()
    const harness = installSingleVPSSubscriptionHarness((vpsId) => ({
      ...capabilityOffOverview(vpsId, 'Tokyo Edge A'),
      capabilities: ['records_v2_read'],
    }))

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <DashboardNavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('button', { name: '管理' })
    fireEvent.click(screen.getByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '订阅事实' }))
    const initialDrawer = await screen.findByRole('dialog', { name: '订阅事实' })
    fireEvent.change(within(initialDrawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(initialDrawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(harness.idempotencyKeys).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '前往工作台' }))
    await screen.findByRole('heading', { name: '工作台测试页' })
    fireEvent.click(screen.getByRole('button', { name: '返回同一 VPS' }))

    await screen.findByRole('button', { name: '管理' })
    expect(screen.getByText('操作处理中，请等待当前写入完成。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '订阅事实' }))
    const returnedDrawer = await screen.findByRole('dialog', { name: '订阅事实' })
    const blockedSave = within(returnedDrawer).getByRole('button', { name: '保存中…' })
    expect(blockedSave).toBeDisabled()
    fireEvent.click(blockedSave)
    expect(harness.idempotencyKeys).toHaveLength(1)

    await act(async () => harness.settle())

    await waitFor(() => expect(harness.getOverview).toHaveBeenCalledTimes(3))
    expect(screen.getByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(harness.idempotencyKeys).toHaveLength(1)
  })

  it('revalidates the current VPS after an inherited write owner settles across the production probe gate remount', async () => {
    const delayedA = deferred<Response>()
    const delayedB = deferred<Response>()
    const detailA = detailFixture('vps_a', 'Tokyo Edge A')
    const detailB = detailFixture('vps_b', 'Osaka Edge B')
    const settledDetailA = {
      ...detailA,
      display_name: 'Tokyo Edge A converged',
      lifecycle_status: 'to_cancel' as const,
      renewal_decision: 'cancel' as const,
      updated_at: '2026-08-27T01:00:00Z',
    }
    const settledSubscriptionA = subscriptionFixture('vps_a')
    const settledServiceA = {
      service_id: 'service_vps_a_settled',
      vps_id: 'vps_a',
      target_id: null,
      name: 'Settled A Service',
      service_type: 'api',
      status: 'active',
      url: 'https://settled-a.example.com',
      port: 443,
      labels: [],
      note: '',
      created_at: '2026-08-27T01:00:00Z',
      updated_at: '2026-08-27T01:00:00Z',
    }
    const subscriptionRequests = new Map<string, string[]>()
    let aWriteSettled = false
    let aDetailGets = 0

    const getOverview = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation((vpsId) => {
      if (vpsId === 'vps_a') return Promise.resolve(capabilityOffOverview('vps_a', 'Tokyo Edge A'))
      if (vpsId === 'vps_b') return Promise.resolve(capabilityOffOverview('vps_b', 'Osaka Edge B'))
      throw new Error(`unexpected overview ${vpsId}`)
    })
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = (init?.method ?? 'GET').toUpperCase()
      if (url === '/api/vps/vps_a' && method === 'GET') {
        aDetailGets += 1
        return Promise.resolve(mockJSONResponse(aWriteSettled ? settledDetailA : detailA))
      }
      if (url === '/api/vps/vps_b' && method === 'GET') return Promise.resolve(mockJSONResponse(detailB))
      if (url === '/api/vps/vps_a/subscriptions' && method === 'POST') {
        const key = new Headers(init?.headers).get('Idempotency-Key') ?? ''
        subscriptionRequests.set('vps_a', [...(subscriptionRequests.get('vps_a') ?? []), key])
        return delayedA.promise
      }
      if (url === '/api/vps/vps_b/subscriptions' && method === 'POST') {
        const key = new Headers(init?.headers).get('Idempotency-Key') ?? ''
        subscriptionRequests.set('vps_b', [...(subscriptionRequests.get('vps_b') ?? []), key])
        return delayedB.promise
      }
      if (url.startsWith('/api/vps/vps_a/timeline') || url.startsWith('/api/vps/vps_b/timeline')) {
        const targetVpsId = url.includes('vps_b') ? 'vps_b' : 'vps_a'
        return Promise.resolve(mockJSONResponse({
          vps_id: targetVpsId,
          renewal_decisions: [],
          price_histories: [],
          ip_histories: [],
          spec_snapshots: [],
          experience_logs: [],
        }))
      }
      if (url === '/api/vps/vps_a/services' && method === 'GET') {
        return Promise.resolve(mockJSONResponse(aWriteSettled ? [settledServiceA] : []))
      }
      if (url.includes('/services') || url.includes('/domains')) return Promise.resolve(mockJSONResponse([]))
      if (url.startsWith('/api/subscriptions')) {
        return Promise.resolve(mockJSONResponse(
          aWriteSettled && url.includes('vps_id=vps_a') ? [settledSubscriptionA] : [],
        ))
      }
      throw new Error(`unexpected fetch ${method} ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <NavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const initialA = openSubscriptionDrawer()
    fireEvent.change(within(initialA).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(initialA).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(subscriptionRequests.get('vps_a')).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '切到 B' }))
    await screen.findByRole('heading', { name: 'Osaka Edge B' })
    const initialB = openSubscriptionDrawer()
    fireEvent.change(within(initialB).getByLabelText('价格'), { target: { value: '18' } })
    fireEvent.click(within(initialB).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(subscriptionRequests.get('vps_b')).toHaveLength(1))

    fireEvent.click(screen.getByRole('button', { name: '切回 A' }))
    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const returnedA = openSubscriptionDrawer()
    fireEvent.change(within(returnedA).getByLabelText('价格'), { target: { value: '13' } })
    const blockedA = within(returnedA).getByRole('button', { name: '保存中…' })
    expect(blockedA).toBeDisabled()
    fireEvent.click(blockedA)
    expect(subscriptionRequests.get('vps_a')).toHaveLength(1)
    expect(subscriptionRequests.get('vps_a')?.[0]).not.toBe('')
    expect(aDetailGets).toBe(2)
    expect(screen.queryByText('Settled A Service')).not.toBeInTheDocument()

    await act(async () => {
      aWriteSettled = true
      delayedA.resolve(mockJSONResponse(settledSubscriptionA, 201))
    })
    await screen.findByRole('heading', { name: 'Tokyo Edge A converged' })
    await waitFor(() => expect(aDetailGets).toBe(3))
    expect(screen.getAllByText('1 个服务').length).toBeGreaterThan(0)
    expect(screen.queryByText('缺少当前订阅')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '处理取消/退役' })).toBeInTheDocument()
    expect(screen.queryByText('订阅账单事实已创建')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '切到 B' }))
    await screen.findByRole('heading', { name: 'Osaka Edge B' })
    const returnedB = openSubscriptionDrawer()
    const stillBlockedB = within(returnedB).getByRole('button', { name: '保存中…' })
    expect(stillBlockedB).toBeDisabled()
    expect(subscriptionRequests.get('vps_b')).toHaveLength(1)

    expect(subscriptionRequests.get('vps_a')).toHaveLength(1)
    expect(subscriptionRequests.get('vps_b')).toHaveLength(1)
    expect(getOverview.mock.calls.map(([vpsId]) => vpsId)).toEqual([
      'vps_a',
      'vps_b',
      'vps_a',
      'vps_a',
      'vps_b',
    ])
  }, 15_000) // Real entry crosses five controlled capability probes and repeated lazy Legacy remounts under coverage.

  it('does not re-probe the current B route when an unmounted A write settles', async () => {
    const delayedA = deferred<Response>()
    const detailA = detailFixture('vps_a', 'Tokyo Edge A')
    const detailB = detailFixture('vps_b', 'Osaka Edge B')
    let subscriptionPosts = 0
    const getOverview = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation((vpsId) => {
      if (vpsId === 'vps_a') return Promise.resolve(capabilityOffOverview('vps_a', 'Tokyo Edge A'))
      if (vpsId === 'vps_b') return Promise.resolve(capabilityOffOverview('vps_b', 'Osaka Edge B'))
      throw new Error(`unexpected overview ${vpsId}`)
    })
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = (init?.method ?? 'GET').toUpperCase()
      if (url === '/api/vps/vps_a' && method === 'GET') return Promise.resolve(mockJSONResponse(detailA))
      if (url === '/api/vps/vps_b' && method === 'GET') return Promise.resolve(mockJSONResponse(detailB))
      if (url === '/api/vps/vps_a/subscriptions' && method === 'POST') {
        subscriptionPosts += 1
        return delayedA.promise
      }
      if (url.startsWith('/api/vps/vps_a/timeline') || url.startsWith('/api/vps/vps_b/timeline')) {
        return Promise.resolve(mockJSONResponse({
          vps_id: url.includes('vps_b') ? 'vps_b' : 'vps_a',
          renewal_decisions: [],
          price_histories: [],
          ip_histories: [],
          spec_snapshots: [],
          experience_logs: [],
        }))
      }
      if (url.includes('/services') || url.includes('/domains') || url.startsWith('/api/subscriptions')) {
        return Promise.resolve(mockJSONResponse([]))
      }
      throw new Error(`unexpected fetch ${method} ${url}`)
    }))

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <NavigationHarness />
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Tokyo Edge A' })
    const drawer = openSubscriptionDrawer()
    fireEvent.change(within(drawer).getByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(subscriptionPosts).toBe(1))

    fireEvent.click(screen.getByRole('button', { name: '切到 B' }))
    await screen.findByRole('heading', { name: 'Osaka Edge B' })
    expect(getOverview.mock.calls.map(([vpsId]) => vpsId)).toEqual(['vps_a', 'vps_b'])

    await act(async () => {
      delayedA.resolve(mockJSONResponse(subscriptionFixture('vps_a'), 201))
    })

    expect(screen.getByRole('heading', { name: 'Osaka Edge B' })).toBeInTheDocument()
    expect(getOverview.mock.calls.map(([vpsId]) => vpsId)).toEqual(['vps_a', 'vps_b'])
  })
})
