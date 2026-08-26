import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../lib/apiRequest'
import * as api from '../lib/api'
import * as recordsApi from '../lib/recordsApi'
import type {
  ArchiveReview,
  CancellationPreview,
  LifecycleActionResult,
  SubscriptionRecord,
  VPSAssetDetail,
  VPSOverview,
} from '../lib/types'
import { VPSDetailPage } from './VPSDetailPage'

vi.mock('./vps-detail/LegacyVPSDetail', () => ({
  LegacyVPSDetail: () => <div>Legacy VPS detail shell</div>,
}))

function toCancelOverview(): VPSOverview {
  const overview = overviewFixture()
  return {
    ...overview,
    identity: { ...overview.identity, lifecycle_status: 'to_cancel' },
  }
}

function overviewFixture(vpsId = 'vps_001', displayName = '东京边缘'): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
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
      importance: 'high',
      labels: [],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: 'healthy', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: 'low', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: 'keep', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
    },
    recent_activity: {
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      items: [],
    },
    facts: [],
    relations: [],
    capabilities: ['records_v2_read'],
  }
}

function detailFixture(): VPSAssetDetail {
  return {
    vps_id: 'vps_001',
    display_name: '东京边缘',
    provider_id: 'provider_001',
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: 'order_001',
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
    importance: 'high',
    labels: ['edge'],
    note: '',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
  }
}

function subscriptionFixture(): SubscriptionRecord {
  return {
    subscription_id: 'sub_001',
    vps_id: 'vps_001',
    price: 12.5,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    billing_period_unit: 'month',
    billing_period_length: 1,
    monthly_price: 12.5,
    auto_renew: false,
    auto_renew_cancelled: false,
    renewal_mode: 'manual',
    status: 'active',
    payment_method: '',
    note: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
  }
}

function cancellationPreviewFixture(): CancellationPreview {
  return {
    vps: detailFixture(),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    recommended_steps: [],
    warnings: ['请确认上游流量已经迁移。'],
    blockers: ['仍有关联资源，暂时不能执行取消。'],
    preview_digest: 'preview-digest-test',
  }
}

function archiveReviewFixture(): ArchiveReview {
  return {
    vps: detailFixture(),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    warnings: [],
    blockers: [],
    eligible: true,
  }
}

function cancellationResultFixture(): LifecycleActionResult {
  return {
    action: {
      action_id: 'action_001',
      vps_id: 'vps_001',
      action_type: 'cancel_vps',
      status: 'completed',
      reason: '测试退役',
      created_at: '2026-08-23T00:00:00Z',
    },
    steps: [],
  }
}

function deferredOverview() {
  let resolve!: (value: VPSOverview) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<VPSOverview>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function renderDetail({
  initialEntry = '/vps/vps_001',
  nextEntry,
  reactStrictMode = false,
}: {
  initialEntry?: string
  nextEntry?: string
  reactStrictMode?: boolean
} = {}) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      {nextEntry ? <Link to={nextEntry}>切换测试 VPS</Link> : null}
      <Routes>
        <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        <Route path="/archive/:vpsId" element={<div>Archive detail route</div>} />
      </Routes>
    </MemoryRouter>,
    { reactStrictMode },
  )
}

describe('VPSDetailPage gate', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reuses one pending capability probe through StrictMode effect replay', async () => {
    const probe = deferredOverview()
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockReturnValue(probe.promise)

    renderDetail({ reactStrictMode: true })

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
    probe.resolve(overviewFixture())

    expect(await screen.findByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('starts exactly one new gate probe for a StrictMode retry revision', async () => {
    const initial = deferredOverview()
    const retry = deferredOverview()
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockReturnValueOnce(initial.promise)
      .mockReturnValue(retry.promise)

    renderDetail({ reactStrictMode: true })

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
    initial.reject(new TypeError('private initial transport'))
    await screen.findByRole('heading', { name: '无法加载 VPS 概览' })

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    retry.resolve(overviewFixture())
    expect(await screen.findByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('does not let a resolved A gate mount a duplicate B owner during a route transition', async () => {
    const overviewA = overviewFixture('vps_a', 'A 概览')
    const overviewB = overviewFixture('vps_b', 'B 概览')
    const pendingB = deferredOverview()
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation((vpsId) => {
      if (vpsId === 'vps_a') return Promise.resolve(overviewA)
      if (vpsId === 'vps_b') return pendingB.promise
      throw new Error(`unexpected VPS ${vpsId}`)
    })

    renderDetail({ initialEntry: '/vps/vps_a', nextEntry: '/vps/vps_b' })
    expect(await screen.findByRole('heading', { name: 'A 概览' })).toBeInTheDocument()
    expect(get.mock.calls.filter(([vpsId]) => vpsId === 'vps_a')).toHaveLength(1)

    fireEvent.click(screen.getByRole('link', { name: '切换测试 VPS' }))

    expect(screen.queryByRole('heading', { name: 'A 概览' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(get.mock.calls.filter(([vpsId]) => vpsId === 'vps_b')).toHaveLength(1)
    })

    pendingB.resolve(overviewB)
    expect(await screen.findByRole('heading', { name: 'B 概览' })).toBeInTheDocument()
    expect(get.mock.calls.map(([vpsId]) => vpsId)).toEqual(['vps_a', 'vps_b'])
  })

  it('keeps B visible when an earlier pending A probe settles late', async () => {
    const pendingA = deferredOverview()
    const pendingB = deferredOverview()
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation((vpsId) => {
      if (vpsId === 'vps_a') return pendingA.promise
      if (vpsId === 'vps_b') return pendingB.promise
      throw new Error(`unexpected VPS ${vpsId}`)
    })

    renderDetail({ initialEntry: '/vps/vps_a', nextEntry: '/vps/vps_b' })
    await waitFor(() => expect(get).toHaveBeenCalledWith('vps_a'))

    fireEvent.click(screen.getByRole('link', { name: '切换测试 VPS' }))
    await waitFor(() => expect(get).toHaveBeenCalledWith('vps_b'))
    pendingB.resolve(overviewFixture('vps_b', 'B 概览'))
    expect(await screen.findByRole('heading', { name: 'B 概览' })).toBeInTheDocument()

    pendingA.resolve(overviewFixture('vps_a', 'A 概览'))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'B 概览' })).toBeInTheDocument())
    expect(screen.queryByRole('heading', { name: 'A 概览' })).not.toBeInTheDocument()
    expect(get.mock.calls.map(([vpsId]) => vpsId)).toEqual(['vps_a', 'vps_b'])
  })

  it('uses overview composition when records_v2_read is present', async () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())

    renderDetail()

    await waitFor(() => expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '新建记录' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '时间线' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '概览' })).toHaveAttribute('aria-current', 'page')
    expect(screen.queryByText('动作：无')).not.toBeInTheDocument()
    // Gate probe seeds the overview route — no duplicate first-paint fetch.
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('retries a degraded section through the existing full-overview owner', async () => {
    const degraded = overviewFixture()
    degraded.summary.ip_quality.section = {
      state: 'stale',
      observed_at: '2026-08-19T00:00:00Z',
      last_success_at: '2026-08-19T00:00:00Z',
      reason_code: 'ip_quality_stale',
    }
    let resolveRefresh!: (value: VPSOverview) => void
    const refresh = new Promise<VPSOverview>((resolve) => {
      resolveRefresh = resolve
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockResolvedValueOnce(degraded)
      .mockImplementationOnce(() => refresh)

    renderDetail()

    const retry = await screen.findByRole('button', { name: '重试 IP 质量' })
    fireEvent.click(retry)

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    expect(get).toHaveBeenNthCalledWith(2, 'vps_001')
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(retry).toBeDisabled()

    resolveRefresh(overviewFixture())
    await waitFor(() => expect(screen.queryByRole('button', { name: '重试 IP 质量' })).not.toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
  })

  it('keeps the overview mounted and resets retry state when a refresh rejects', async () => {
    const degraded = overviewFixture()
    degraded.summary.ip_quality.section = {
      state: 'unavailable',
      observed_at: '2026-08-19T00:00:00Z',
      last_success_at: '2026-08-19T00:00:00Z',
      reason_code: 'ip_quality_unavailable',
    }
    let rejectRefresh!: (reason: unknown) => void
    const refresh = new Promise<VPSOverview>((_resolve, reject) => {
      rejectRefresh = reject
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockResolvedValueOnce(degraded)
      .mockImplementationOnce(() => refresh)

    renderDetail()

    const retry = await screen.findByRole('button', { name: '重试 IP 质量' })
    fireEvent.click(retry)

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(retry).toBeDisabled()

    rejectRefresh(new ApiError(503, 'refresh unavailable', { code: 'overview_unavailable' }))

    await waitFor(() => expect(retry).toBeEnabled())
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(screen.getByText('IP 质量数据暂不可用，请稍后重试。')).toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('moves focus into the management menu and returns it on Escape', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())

    renderDetail()

    const manage = await screen.findByRole('button', { name: '管理' })
    manage.focus()
    fireEvent.click(manage)

    const firstItem = screen.getByRole('menuitem', { name: '编辑事实' })
    await waitFor(() => expect(firstItem).toHaveFocus())
    fireEvent.keyDown(document, { key: 'Escape' })

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    await waitFor(() => expect(manage).toHaveFocus())
  })

  it('opens the real facts editor from the overview management menu', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    const getDetail = vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listProviders').mockResolvedValue([])

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '编辑事实' }))

    expect(await screen.findByRole('dialog', { name: '编辑 VPS 事实' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('东京边缘')
    expect(screen.queryByText('管理面板')).not.toBeInTheDocument()
    expect(getDetail).toHaveBeenCalledWith('vps_001')
  })

  it('retries a failed action-detail read without leaving the panel', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    const getDetail = vi.spyOn(api, 'getVPSAsset')
      .mockRejectedValueOnce(new ApiError(503, 'detail unavailable'))
      .mockResolvedValueOnce(detailFixture())
    vi.spyOn(api, 'listProviders').mockResolvedValue([])

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '编辑事实' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('detail unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('textbox', { name: 'VPS 名称' })).toHaveValue('东京边缘')
    expect(getDetail).toHaveBeenCalledTimes(2)
  })

  it('keeps the overview visible when a successful write is followed by refresh failure', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview')
      .mockResolvedValueOnce(overviewFixture())
      .mockRejectedValueOnce(new ApiError(500, 'refresh failed', { code: 'internal_error' }))
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(detailFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '编辑事实' }))
    const nameInput = await screen.findByRole('textbox', { name: 'VPS 名称' })
    fireEvent.change(nameInput, { target: { value: '东京边缘 2' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))

    expect(await screen.findByText(/基础信息已更新，但概览刷新失败/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(screen.queryByText('VPS 概览不可用')).not.toBeInTheDocument()
  })

  it('keeps a rejected facts draft open for correction and retry', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    vi.spyOn(api, 'updateVPSAsset').mockRejectedValue(new ApiError(422, '名称已被占用'))

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '编辑事实' }))
    const nameInput = await screen.findByRole('textbox', { name: 'VPS 名称' })
    fireEvent.change(nameInput, { target: { value: '重复名称' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('名称已被占用')
    expect(screen.getByRole('dialog', { name: '编辑 VPS 事实' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('重复名称')
  })

  it('updates the renewal decision from the overview management menu', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    const update = vi.spyOn(api, 'updateVPSAsset').mockResolvedValue({
      ...detailFixture(),
      renewal_subscription_linkage: {
        status: 'no_active_subscription',
        candidate_count: 0,
        updated: false,
        message: '未找到 active 订阅。',
      },
    })

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '续费决策' }))

    const dialog = await screen.findByRole('dialog', { name: '续费决策' })
    fireEvent.change(screen.getByRole('combobox', { name: '续费决策' }), {
      target: { value: 'observe' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '决策理由' }), {
      target: { value: '等待下月价格确认' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(dialog).not.toBeInTheDocument())
    expect(update).toHaveBeenCalledWith('vps_001', {
      renewal_decision: 'observe',
      renewal_reason: '等待下月价格确认',
    }, { expectedUpdatedAt: '2026-08-20T00:00:00Z' })
    expect(screen.getByRole('status')).toHaveTextContent('续费决策已更新，概览已刷新。未找到 active 订阅。')
    const linkageAction = screen.getByRole('link', { name: '创建/更新订阅' })
    expect(linkageAction).toHaveAttribute(
      'href',
      '/vps/vps_001?workbench=subscription',
    )
    fireEvent.click(linkageAction)
    expect(await screen.findByRole('dialog', { name: '订阅事实' })).toBeInTheDocument()
  })

  it('creates a subscription fact from the overview management menu', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    const create = vi.spyOn(api, 'createVPSSubscription').mockResolvedValue(subscriptionFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '订阅事实' }))

    const dialog = await screen.findByRole('dialog', { name: '订阅事实' })
    fireEvent.change(screen.getByRole('spinbutton', { name: '价格' }), {
      target: { value: '12.5' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建/更新订阅' }))

    await waitFor(() => expect(dialog).not.toBeInTheDocument())
    expect(create).toHaveBeenCalledWith(
      'vps_001',
      expect.objectContaining({
        price: 12.5,
        currency: 'USD',
        billing_period_unit: 'month',
        billing_period_length: 1,
        renewal_mode: 'manual',
      }),
      expect.stringMatching(/^[0-9a-f-]{36}$/i),
    )
    expect(screen.getByRole('status')).toHaveTextContent('订阅账单事实已创建，概览已刷新')
  })

  it.each([
    { lifecycle: 'cancelled' as const, capabilities: ['records_v2_read'] },
    { lifecycle: 'archived' as const, capabilities: ['records_v2_read'] },
    { lifecycle: 'cancelled' as const, capabilities: [] as string[] },
    { lifecycle: 'archived' as const, capabilities: [] as string[] },
  ])('redirects $lifecycle VPS to archive when capabilities=$capabilities', async ({ lifecycle, capabilities }) => {
    const overview = overviewFixture()
    overview.identity.lifecycle_status = lifecycle
    overview.capabilities = capabilities
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overview)
    renderDetail()
    expect(await screen.findByText('Archive detail route')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '新建记录' })).not.toBeInTheDocument()
    expect(screen.queryByText('Legacy VPS detail shell')).not.toBeInTheDocument()
  })

  it('loads cancellation preview and preserves server blockers', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview')
      .mockResolvedValue(cancellationPreviewFixture())

    renderDetail({ initialEntry: '/vps/vps_001?workbench=cancellation' })

    expect(await screen.findByRole('dialog', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('仍有关联资源，暂时不能执行取消')
    expect(screen.getByRole('button', { name: '确认取消/退役' })).toBeDisabled()
    expect(getPreview).toHaveBeenCalledWith('vps_001')
  })

  it('retries a failed cancellation preview read in the open workbench', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview')
      .mockRejectedValueOnce(new ApiError(503, 'preview unavailable'))
      .mockResolvedValueOnce(cancellationPreviewFixture())

    renderDetail({ initialEntry: '/vps/vps_001?workbench=cancellation' })

    expect(await screen.findByRole('alert')).toHaveTextContent('preview unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('仍有关联资源，暂时不能执行取消')
    expect(screen.getByRole('dialog', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(getPreview).toHaveBeenCalledTimes(2)
  })

  it('keeps the cancellation audit result and refreshes preview plus overview', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    const safePreview = { ...cancellationPreviewFixture(), warnings: [], blockers: [] }
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview').mockResolvedValue(safePreview)
    const apply = vi.spyOn(api, 'applyVPSCancellation').mockResolvedValue(cancellationResultFixture())

    renderDetail({ initialEntry: '/vps/vps_001?workbench=cancellation' })
    fireEvent.change(await screen.findByRole('textbox', { name: '原因' }), {
      target: { value: '测试退役' },
    })
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))

    expect(await screen.findByText(/已完成生命周期动作 action_001/)).toBeInTheDocument()
    expect(apply).toHaveBeenCalledTimes(1)
    expect(getPreview).toHaveBeenCalledTimes(2)
    expect(recordsApi.getVPSOverview).toHaveBeenCalledTimes(2)
  })

  it('requires a fresh eligible archive review and exact display-name confirmation', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    const review = vi.spyOn(api, 'getVPSArchiveReview').mockResolvedValue(archiveReviewFixture())
    const archive = vi.spyOn(api, 'archiveVPS').mockResolvedValue(archiveReviewFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))

    expect(await screen.findByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    const confirm = screen.getByRole('button', { name: '确认归档' })
    expect(confirm).toBeDisabled()
    fireEvent.change(screen.getByRole('textbox', { name: '输入 VPS 名称确认归档' }), {
      target: { value: '东京边缘' },
    })
    expect(confirm).toBeEnabled()
    fireEvent.click(confirm)

    await waitFor(() => expect(archive).toHaveBeenCalledWith('vps_001', {
      confirmation_name: '东京边缘',
    }))
    expect(await screen.findByText('Archive detail route')).toBeInTheDocument()
    expect(review).toHaveBeenCalledWith('vps_001')
  })

  it('explains an ineligible archive review and keeps confirmation disabled', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    vi.spyOn(api, 'getVPSArchiveReview').mockResolvedValue({
      ...archiveReviewFixture(),
      eligible: false,
    })
    const archive = vi.spyOn(api, 'archiveVPS')

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('服务端判定当前不具备归档资格')
    expect(screen.getByRole('button', { name: '确认归档' })).toBeDisabled()
    expect(archive).not.toHaveBeenCalled()
  })

  it('retries a failed archive review without leaving the confirmation flow', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(toCancelOverview())
    const review = vi.spyOn(api, 'getVPSArchiveReview')
      .mockRejectedValueOnce(new ApiError(503, 'review unavailable'))
      .mockResolvedValueOnce(archiveReviewFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('review unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('textbox', { name: '输入 VPS 名称确认归档' })).toBeInTheDocument()
    expect(screen.getByRole('alertdialog', { name: '确认归档 VPS' })).toBeInTheDocument()
    expect(review).toHaveBeenCalledTimes(2)
  })

  it('shows overview not-found when identity is missing', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(404, 'private lookup detail', { code: 'resource_not_found' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('未找到 VPS')).toBeInTheDocument())
    expect(screen.getByText('该 VPS 不存在，或当前账号无权查看。')).toBeInTheDocument()
    expect(screen.queryByText('private lookup detail')).not.toBeInTheDocument()
    expect(screen.queryByText('Legacy VPS detail shell')).not.toBeInTheDocument()
  })

  it('falls back to legacy only after a valid capability-off overview', async () => {
    const capabilityOff = overviewFixture()
    capabilityOff.capabilities = []
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(capabilityOff)

    renderDetail()

    await waitFor(() => expect(screen.getByText('Legacy VPS detail shell')).toBeInTheDocument())
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('falls back to legacy when overview capability is unavailable', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(503, 'overview unavailable', { code: 'overview_unavailable' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('Legacy VPS detail shell')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
  })

  it('does not silently fall back on overview server errors', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(500, 'boom', { code: 'internal_error' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('无法加载 VPS 概览')).toBeInTheDocument())
  })

  it.each([
    [
      'typed decoder error',
      new recordsApi.InvalidVPSOverviewResponseError('invalid_shape'),
      'Invalid VPS overview response',
    ],
    ['network error', new TypeError('private network endpoint'), 'private network endpoint'],
    [
      'unknown 503',
      new ApiError(503, 'private upstream timeout', { code: 'upstream_timeout' }),
      'private upstream timeout',
    ],
    [
      'other API error',
      new ApiError(500, 'private database failure', { code: 'internal_error' }),
      'private database failure',
    ],
  ])('shows a safe retryable gate for %s and recovers on the next probe', async (
    _caseName,
    failure,
    rawMessage,
  ) => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce(overviewFixture())

    renderDetail()

    await waitFor(() => expect(screen.getByText('无法加载 VPS 概览')).toBeInTheDocument())
    expect(screen.getByText('VPS 概览请求或响应校验失败，请重试。')).toBeInTheDocument()
    expect(screen.queryByText(rawMessage)).not.toBeInTheDocument()
    expect(screen.queryByText('Legacy VPS detail shell')).not.toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument())
  })
})
