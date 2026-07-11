import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  SubscriptionListFilter,
  SubscriptionRecord,
  VPSAssetRecord,
  VPSAssetListFilter,
  VPSAssetUpdateResult,
} from '../../../lib/types'
import { useAssetDecisionRenewalQueue } from './useAssetDecisionRenewalQueue'

function subscription(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return {
    subscription_id: 'sub_001',
    vps_id: 'vps_review',
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    billing_period_unit: 'month',
    billing_period_length: 1,
    monthly_price: 12,
    started_at: '2026-05-01',
    renew_at: '2026-08-01',
    auto_renew: true,
    auto_renew_cancelled: false,
    renewal_mode: 'auto',
    status: 'active',
    payment_method: 'card',
    display_name: 'Tokyo Review subscription',
    cost_category: 'compute',
    labels: [],
    trial_ends_at: null,
    ends_at: null,
    note: '',
    monthly_price_base: 84,
    yearly_price_base: 1008,
    base_currency: 'CNY',
    exchange_rate: 7,
    exchange_rate_date: '2026-07-10',
    exchange_rate_stale: false,
    budget_status: 'ok',
    next_reminder_at: null,
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    ...overrides,
  }
}

function vps(
  vpsID: string,
  renewalDecision: VPSAssetRecord['renewal_decision'],
): VPSAssetRecord {
  return {
    vps_id: vpsID,
    display_name: vpsID === 'vps_review'
      ? 'Tokyo Review'
      : vpsID === 'vps_migrate'
        ? 'Frankfurt Migration'
        : 'Seoul Cancel',
    provider_id: 'pv_001',
    provider_name: 'Provider A',
    product_name: 'KVM',
    order_ref: 'order-001',
    country: 'JP',
    region: 'Kanto',
    city: 'Tokyo',
    datacenter: 'NRT1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'kvm',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: renewalDecision,
    importance: 'normal',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 1,
    running_monitoring_instance_count: 1,
    running_target_count: 1,
    created_at: '2026-05-09T08:00:00Z',
    updated_at: '2026-05-09T08:00:00Z',
    archived_at: null,
  }
}

const UNREVIEWED = vps('vps_review', 'unreviewed')
const MIGRATE = vps('vps_migrate', 'migrate')
const CANCEL = vps('vps_cancel', 'cancel')

function mockSuccessfulReads() {
  const renewal = subscription()
  vi.spyOn(api, 'listSubscriptions').mockImplementation((filter?: SubscriptionListFilter) => (
    Promise.resolve(filter?.renew_within_days ? [renewal] : [renewal])
  ))
  vi.spyOn(api, 'listVPSAssets').mockImplementation((filter?: VPSAssetListFilter) => {
    if (filter?.renewal_decision === 'migrate') return Promise.resolve([MIGRATE])
    if (filter?.renewal_decision === 'cancel') return Promise.resolve([CANCEL])
    return Promise.resolve([UNREVIEWED])
  })
}

describe('useAssetDecisionRenewalQueue', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('owns the renewal read and the three decision slices', async () => {
    mockSuccessfulReads()
    const listSubscriptions = vi.mocked(api.listSubscriptions)
    const listVPSAssets = vi.mocked(api.listVPSAssets)
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))

    expect(result.current.state.queue.renewalsLoading).toBe(true)
    expect(result.current.state.queue.queueLoading).toBe(true)
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))

    expect(listSubscriptions).toHaveBeenCalledWith({
      renew_within_days: 30,
      sort: 'renew_at',
      order: 'asc',
    })
    expect(listSubscriptions).toHaveBeenCalledWith({ sort: 'renew_at', order: 'asc' })
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'unreviewed' })
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'migrate' })
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'cancel' })
    expect(result.current.state.queue).toMatchObject({
      renewals: [subscription()],
      subscriptions: [subscription()],
      unreviewed: [UNREVIEWED],
      migrate: [MIGRATE],
      cancel: [CANCEL],
    })
    expect(result.current.state.decisionQueue.map((row) => row.vps.vps_id)).toEqual([
      'vps_review',
      'vps_migrate',
      'vps_cancel',
    ])
  })

  it('keeps renewal evidence and the single-VPS queue failures independent', async () => {
    const renewal = subscription()
    vi.spyOn(api, 'listSubscriptions').mockImplementation((filter?: SubscriptionListFilter) => (
      filter?.renew_within_days
        ? Promise.reject(new Error('renewals offline'))
        : Promise.resolve([renewal])
    ))
    vi.spyOn(api, 'listVPSAssets').mockImplementation((filter?: VPSAssetListFilter) => {
      if (filter?.renewal_decision === 'migrate') return Promise.resolve([MIGRATE])
      if (filter?.renewal_decision === 'cancel') return Promise.resolve([CANCEL])
      return Promise.resolve([UNREVIEWED])
    })
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))

    await waitFor(() => expect(result.current.state.queue.renewalsError).toBe('renewals offline'))
    expect(result.current.state.queue.queueError).toBeNull()
    expect(result.current.state.decisionQueue).toHaveLength(3)
  })

  it('keeps a single-VPS queue failure separate from renewal evidence', async () => {
    const renewal = subscription()
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([renewal])
    vi.spyOn(api, 'listVPSAssets').mockRejectedValue(new Error('queue offline'))
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))

    await waitFor(() => expect(result.current.state.queue.queueError).toBe('queue offline'))

    expect(result.current.state.queue.renewalsError).toBeNull()
    expect(result.current.state.queue.renewals).toEqual([renewal])
    expect(result.current.state.decisionQueue).toEqual([])
  })

  it('replays both renewal and queue reads when the external revision changes', async () => {
    mockSuccessfulReads()
    const listSubscriptions = vi.mocked(api.listSubscriptions)
    const listVPSAssets = vi.mocked(api.listVPSAssets)
    const { result, rerender } = renderHook(
      ({ revision }: { revision: number }) => useAssetDecisionRenewalQueue({
        renewalWindow: 30,
        revision,
        onNotice: vi.fn(),
        onInvalidate: vi.fn(),
      }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))
    listSubscriptions.mockClear()
    listVPSAssets.mockClear()

    rerender({ revision: 1 })

    expect(result.current.state.queue.renewalsLoading).toBe(true)
    expect(result.current.state.queue.queueLoading).toBe(true)
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))
    expect(listSubscriptions).toHaveBeenCalledTimes(2)
    expect(listSubscriptions).toHaveBeenCalledWith({
      renew_within_days: 30,
      sort: 'renew_at',
      order: 'asc',
    })
    expect(listSubscriptions).toHaveBeenCalledWith({ sort: 'renew_at', order: 'asc' })
    expect(listVPSAssets).toHaveBeenCalledTimes(3)
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'unreviewed' })
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'migrate' })
    expect(listVPSAssets).toHaveBeenCalledWith({ renewal_decision: 'cancel' })
  })

  it('owns queue view, selected VPS, and draft reset commands', async () => {
    mockSuccessfulReads()
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))

    act(() => {
      result.current.commands.selectQueueView('migrate')
      result.current.commands.selectVPS(UNREVIEWED)
    })
    expect(result.current.state.queueView).toBe('migrate')
    expect(result.current.state.visibleDecisionQueue.map((row) => row.vps.vps_id)).toEqual(['vps_migrate'])
    expect(result.current.state.selectedVPS).toBe(UNREVIEWED)
    expect(result.current.state.draft).toEqual({ renewalDecision: 'unreviewed', reason: '' })

    act(() => result.current.commands.updateDraft({ renewalDecision: 'cancel', reason: '不再续费' }))
    expect(result.current.state.draft).toEqual({ renewalDecision: 'cancel', reason: '不再续费' })
    act(() => result.current.commands.closeVPS())
    expect(result.current.state.selectedVPS).toBeNull()
    expect(result.current.state.draft).toEqual({ renewalDecision: 'unreviewed', reason: '' })
  })

  it('discards a selected VPS when its route context changes', async () => {
    mockSuccessfulReads()
    const { result, rerender } = renderHook(
      ({ contextKey }: { contextKey: string }) => useAssetDecisionRenewalQueue({
        renewalWindow: 30,
        revision: 0,
        contextKey,
        onNotice: vi.fn(),
        onInvalidate: vi.fn(),
      }),
      { initialProps: { contextKey: 'group_id:adg_001' } },
    )
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))
    act(() => {
      result.current.commands.selectVPS(UNREVIEWED)
      result.current.commands.updateDraft({ renewalDecision: 'cancel', reason: '离开前草稿' })
    })
    expect(result.current.state.selectedVPS).toBe(UNREVIEWED)

    rerender({ contextKey: '' })
    expect(result.current.state.selectedVPS).toBeNull()
    expect(result.current.state.draft).toEqual({ renewalDecision: 'unreviewed', reason: '' })
    await act(async () => { await Promise.resolve() })
    rerender({ contextKey: 'group_id:adg_001' })

    expect(result.current.state.selectedVPS).toBeNull()
    expect(result.current.state.error).toBeNull()
  })

  it('submits the exact PATCH, merges linkage locally, and emits one semantic invalidation', async () => {
    mockSuccessfulReads()
    const updated: VPSAssetUpdateResult = {
      ...UNREVIEWED,
      renewal_decision: 'cancel',
      renewal_subscription_linkage: {
        status: 'subscription_updated',
        candidate_count: 1,
        subscription_id: 'sub_001',
        updated: true,
        message: '关联订阅已取消自动续费',
      },
    }
    const updateVPS = vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(updated)
    const notice = vi.fn()
    const invalidate = vi.fn()
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: notice,
      onInvalidate: invalidate,
    }))
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))

    act(() => {
      result.current.commands.selectVPS(UNREVIEWED)
      result.current.commands.updateDraft({ renewalDecision: 'cancel', reason: '   ' })
    })
    let returned: VPSAssetUpdateResult | null = null
    await act(async () => {
      returned = await result.current.commands.submitRenewal()
    })

    expect(returned).toBe(updated)
    expect(updateVPS).toHaveBeenCalledWith('vps_review', { renewal_decision: 'cancel' })
    expect(result.current.state.queue.unreviewed).toEqual([])
    expect(result.current.state.queue.cancel[0]).toBe(updated)
    expect(result.current.state.queue.subscriptions[0]).toMatchObject({
      subscription_id: 'sub_001',
      auto_renew: false,
      auto_renew_cancelled: true,
    })
    expect(result.current.state.queue.renewals[0]).toMatchObject({
      subscription_id: 'sub_001',
      auto_renew: false,
      auto_renew_cancelled: true,
    })
    expect(result.current.state.selectedVPS).toBeNull()
    expect(invalidate).toHaveBeenCalledOnce()
    expect(invalidate).toHaveBeenCalledWith({ type: 'renewal-decision-saved', vpsID: 'vps_review' })
    expect(notice).toHaveBeenCalledWith('续费决策已保存：Tokyo Review -> 取消。关联订阅已取消自动续费')
  })

  it('completes a selected mutation before its background queue reads settle', async () => {
    const pendingSubscriptions = new Promise<SubscriptionRecord[]>(() => undefined)
    vi.spyOn(api, 'listSubscriptions').mockReturnValue(pendingSubscriptions)
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue([])
    const updated: VPSAssetUpdateResult = { ...UNREVIEWED, renewal_decision: 'migrate' }
    vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(updated)
    const notice = vi.fn()
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: notice,
      onInvalidate: vi.fn(),
    }))

    act(() => {
      result.current.commands.selectVPS(UNREVIEWED)
      result.current.commands.updateDraft({ renewalDecision: 'migrate' })
    })
    await act(async () => result.current.commands.submitRenewal())

    expect(result.current.state.selectedVPS).toBeNull()
    expect(notice).toHaveBeenCalledWith('续费决策已保存：Tokyo Review -> 迁移')
  })

  it('rejects an unchanged decision without sending a PATCH', async () => {
    mockSuccessfulReads()
    const updateVPS = vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(UNREVIEWED)
    const notice = vi.fn()
    const invalidate = vi.fn()
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: notice,
      onInvalidate: invalidate,
    }))
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))
    act(() => result.current.commands.selectVPS(UNREVIEWED))

    let returned: VPSAssetUpdateResult | null = UNREVIEWED
    await act(async () => {
      returned = await result.current.commands.submitRenewal()
    })

    expect(returned).toBeNull()
    expect(result.current.state.error).toBe('请选择一个不同的续费决策')
    expect(result.current.state.selectedVPS).toBe(UNREVIEWED)
    expect(updateVPS).not.toHaveBeenCalled()
    expect(notice).not.toHaveBeenCalled()
    expect(invalidate).not.toHaveBeenCalled()
  })

  it('does not submit when no VPS is selected', async () => {
    mockSuccessfulReads()
    const updateVPS = vi.spyOn(api, 'updateVPSAsset')
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))

    await expect(result.current.commands.submitRenewal()).resolves.toBeNull()

    expect(updateVPS).not.toHaveBeenCalled()
    expect(result.current.state.error).toBeNull()
  })

  it('keeps the selected VPS open and exposes the mutation error on failure', async () => {
    mockSuccessfulReads()
    vi.spyOn(api, 'updateVPSAsset').mockRejectedValue(new Error('write offline'))
    const { result } = renderHook(() => useAssetDecisionRenewalQueue({
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
      onInvalidate: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.queue.queueLoading).toBe(false))
    act(() => {
      result.current.commands.selectVPS(UNREVIEWED)
      result.current.commands.updateDraft({ renewalDecision: 'migrate', reason: 'move' })
    })

    await act(async () => result.current.commands.submitRenewal())
    expect(result.current.state.selectedVPS).toBe(UNREVIEWED)
    expect(result.current.state.error).toBe('write offline')
    expect(result.current.state.submitting).toBe(false)
  })
})
