import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
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

function overviewFixture(): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: '东京边缘',
      provider_name: 'Example',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.1',
      ipv6: '',
      lifecycle_status: '在用',
      usage_status: '生产',
      renewal_decision: '续费',
      importance: '高',
      labels: [],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: '低风险', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: '续费', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
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

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/vps/vps_001']}>
      <Routes>
        <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        <Route path="/archive/:vpsId" element={<div>Archive detail route</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('VPSDetailPage gate', () => {
  afterEach(() => {
    vi.restoreAllMocks()
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

    expect(await screen.findByRole('status')).toHaveTextContent('基础信息已更新，但概览刷新失败')
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
    })
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
    expect(create).toHaveBeenCalledWith('vps_001', expect.objectContaining({
      price: 12.5,
      currency: 'USD',
      billing_period_unit: 'month',
      billing_period_length: 1,
      renewal_mode: 'manual',
    }))
    expect(screen.getByRole('status')).toHaveTextContent('订阅账单事实已创建，概览已刷新')
  })

  it('loads cancellation preview and preserves server blockers', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview')
      .mockResolvedValue(cancellationPreviewFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '取消 / 退役' }))

    expect(await screen.findByRole('dialog', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('仍有关联资源，暂时不能执行取消')
    expect(screen.getByRole('button', { name: '确认取消/退役' })).toBeDisabled()
    expect(getPreview).toHaveBeenCalledWith('vps_001')
  })

  it('retries a failed cancellation preview read in the open workbench', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview')
      .mockRejectedValueOnce(new ApiError(503, 'preview unavailable'))
      .mockResolvedValueOnce(cancellationPreviewFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '取消 / 退役' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('preview unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('仍有关联资源，暂时不能执行取消')
    expect(screen.getByRole('dialog', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(getPreview).toHaveBeenCalledTimes(2)
  })

  it('keeps the cancellation audit result and refreshes preview plus overview', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    const safePreview = { ...cancellationPreviewFixture(), warnings: [], blockers: [] }
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview').mockResolvedValue(safePreview)
    const apply = vi.spyOn(api, 'applyVPSCancellation').mockResolvedValue(cancellationResultFixture())

    renderDetail()

    fireEvent.click(await screen.findByRole('button', { name: '管理' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '取消 / 退役' }))
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
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
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
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
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
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
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
      new ApiError(404, 'vps not found', { code: 'resource_not_found' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('未找到 VPS')).toBeInTheDocument())
    expect(screen.queryByText('Legacy VPS detail shell')).not.toBeInTheDocument()
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
})
