import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  SubscriptionRecord,
  VPSOverview,
} from '../../../lib/types'
import { useVPSDetailResources } from './useVPSDetailResources'

function overviewFor(vpsId: string, displayName = vpsId): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
    identity: {
      vps_id: vpsId,
      display_name: displayName,
      provider_name: 'Example Cloud',
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

function subscription(subscriptionId: string, vpsId: string): SubscriptionRecord {
  return {
    subscription_id: subscriptionId,
    vps_id: vpsId,
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 12,
    started_at: '2026-07-01',
    renew_at: '2026-08-01',
    auto_renew: true,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: 'card',
    note: '',
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-10T06:00:00Z',
  }
}

function service(serviceId: string, vpsId: string, name: string): AssetServiceRecord {
  return {
    service_id: serviceId,
    vps_id: vpsId,
    name,
    service_type: 'web',
    status: 'active',
    url: `https://${name}.example.invalid`,
    labels: [],
    note: '',
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-10T06:00:00Z',
  }
}

function domain(domainId: string, vpsId: string, name: string): AssetDomainRecord {
  return {
    domain_id: domainId,
    vps_id: vpsId,
    domain_name: name,
    purpose: 'gateway',
    status: 'active',
    registrar: 'Example Registrar',
    auto_renew: true,
    https_enabled: true,
    labels: [],
    note: '',
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-10T06:00:00Z',
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('useVPSDetailResources', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads each detail resource independently with its exact VPS-scoped request', async () => {
    const subscriptions = deferred<SubscriptionRecord[]>()
    const services = deferred<AssetServiceRecord[]>()
    const domains = deferred<AssetDomainRecord[]>()
    const listSubscriptions = vi.spyOn(api, 'listSubscriptions').mockReturnValue(subscriptions.promise)
    const listVPSServices = vi.spyOn(api, 'listVPSServices').mockReturnValue(services.promise)
    const listVPSDomains = vi.spyOn(api, 'listVPSDomains').mockReturnValue(domains.promise)
    const overview = overviewFor('vps_001')

    const { result } = renderHook(() => useVPSDetailResources(overview))

    expect(result.current.subscriptions).toEqual({ status: 'loading', items: [], error: null })
    expect(result.current.services).toEqual({ status: 'loading', items: [], error: null })
    expect(result.current.domains).toEqual({ status: 'loading', items: [], error: null })
    expect(listSubscriptions).toHaveBeenCalledWith({ vps_id: 'vps_001', sort: 'renew_at', order: 'asc' })
    expect(listVPSServices).toHaveBeenCalledWith('vps_001')
    expect(listVPSDomains).toHaveBeenCalledWith('vps_001')

    act(() => subscriptions.resolve([subscription('sub_001', 'vps_001')]))
    await waitFor(() => expect(result.current.subscriptions.status).toBe('ready'))
    expect(result.current.services.status).toBe('loading')
    expect(result.current.domains.status).toBe('loading')

    act(() => services.resolve([service('svc_001', 'vps_001', 'Gateway')]))
    act(() => domains.resolve([domain('dom_001', 'vps_001', 'edge.example.com')]))
    await waitFor(() => expect(result.current.services.status).toBe('ready'))
    await waitFor(() => expect(result.current.domains.status).toBe('ready'))
    expect(result.current.subscriptions.items[0]?.subscription_id).toBe('sub_001')
    expect(result.current.services.items[0]?.name).toBe('Gateway')
    expect(result.current.domains.items[0]?.domain_name).toBe('edge.example.com')
  })

  it('clears prior VPS rows before effects and ignores late responses from the previous route', async () => {
    const firstSubscriptions = deferred<SubscriptionRecord[]>()
    const secondSubscriptions = deferred<SubscriptionRecord[]>()
    const firstServices = deferred<AssetServiceRecord[]>()
    const secondServices = deferred<AssetServiceRecord[]>()
    const firstDomains = deferred<AssetDomainRecord[]>()
    const secondDomains = deferred<AssetDomainRecord[]>()
    const listSubscriptions = vi.spyOn(api, 'listSubscriptions').mockImplementation(({ vps_id } = {}) => (
      vps_id === 'vps_a' ? firstSubscriptions.promise : secondSubscriptions.promise
    ))
    vi.spyOn(api, 'listVPSServices').mockImplementation((vpsId) => (
      vpsId === 'vps_a' ? firstServices.promise : secondServices.promise
    ))
    vi.spyOn(api, 'listVPSDomains').mockImplementation((vpsId) => (
      vpsId === 'vps_a' ? firstDomains.promise : secondDomains.promise
    ))
    const vpsA = overviewFor('vps_a', 'VPS A')
    const vpsB = overviewFor('vps_b', 'VPS B')

    const { result, rerender } = renderHook(
      ({ overview }) => useVPSDetailResources(overview),
      { initialProps: { overview: vpsA } },
    )

    rerender({ overview: vpsB })
    expect(result.current.subscriptions).toEqual({ status: 'loading', items: [], error: null })
    expect(result.current.services).toEqual({ status: 'loading', items: [], error: null })
    expect(result.current.domains).toEqual({ status: 'loading', items: [], error: null })
    expect(listSubscriptions).toHaveBeenCalledTimes(2)

    act(() => {
      secondSubscriptions.resolve([subscription('sub_b', 'vps_b')])
      secondServices.resolve([service('svc_b', 'vps_b', 'B Gateway')])
      secondDomains.resolve([domain('dom_b', 'vps_b', 'b.example.com')])
    })
    await waitFor(() => expect(result.current.subscriptions.status).toBe('ready'))
    await waitFor(() => expect(result.current.services.status).toBe('ready'))
    await waitFor(() => expect(result.current.domains.status).toBe('ready'))
    expect(result.current.subscriptions.items[0]?.vps_id).toBe('vps_b')
    expect(result.current.services.items[0]?.name).toBe('B Gateway')
    expect(result.current.domains.items[0]?.domain_name).toBe('b.example.com')

    await act(async () => {
      firstSubscriptions.resolve([subscription('sub_a', 'vps_a')])
      firstServices.resolve([service('svc_a', 'vps_a', 'A Gateway')])
      firstDomains.resolve([domain('dom_a', 'vps_a', 'a.example.com')])
      await Promise.all([firstSubscriptions.promise, firstServices.promise, firstDomains.promise])
    })

    expect(result.current.subscriptions.items[0]?.vps_id).toBe('vps_b')
    expect(result.current.services.items[0]?.name).toBe('B Gateway')
    expect(result.current.domains.items[0]?.domain_name).toBe('b.example.com')
  })

  it('settles concurrent consumers independently instead of sharing request ownership', async () => {
    vi.spyOn(api, 'listSubscriptions').mockImplementation(({ vps_id } = {}) => (
      Promise.resolve([subscription(`sub_${vps_id}`, vps_id ?? '')])
    ))
    vi.spyOn(api, 'listVPSServices').mockImplementation((vpsId) => (
      Promise.resolve([service(`svc_${vpsId}`, vpsId, `${vpsId} Gateway`)])
    ))
    vi.spyOn(api, 'listVPSDomains').mockImplementation((vpsId) => (
      Promise.resolve([domain(`dom_${vpsId}`, vpsId, `${vpsId}.example.com`)])
    ))

    const firstOverview = overviewFor('vps_a')
    const secondOverview = overviewFor('vps_b')
    const first = renderHook(() => useVPSDetailResources(firstOverview))
    const second = renderHook(() => useVPSDetailResources(secondOverview))
    await waitFor(() => expect(first.result.current.subscriptions.status).toBe('ready'))
    await waitFor(() => expect(first.result.current.services.status).toBe('ready'))
    await waitFor(() => expect(first.result.current.domains.status).toBe('ready'))
    await waitFor(() => expect(second.result.current.subscriptions.status).toBe('ready'))
    await waitFor(() => expect(second.result.current.services.status).toBe('ready'))
    await waitFor(() => expect(second.result.current.domains.status).toBe('ready'))

    expect(first.result.current.subscriptions.items[0]?.vps_id).toBe('vps_a')
    expect(first.result.current.services.items[0]?.name).toBe('vps_a Gateway')
    expect(first.result.current.domains.items[0]?.domain_name).toBe('vps_a.example.com')
    expect(second.result.current.subscriptions.items[0]?.vps_id).toBe('vps_b')
    expect(second.result.current.services.items[0]?.name).toBe('vps_b Gateway')
    expect(second.result.current.domains.items[0]?.domain_name).toBe('vps_b.example.com')
  })

  it('keeps retry ownership local to one resource and ignores the superseded request', async () => {
    const initialServices = deferred<AssetServiceRecord[]>()
    const retriedServices = deferred<AssetServiceRecord[]>()
    const listVPSServices = vi.spyOn(api, 'listVPSServices')
      .mockReturnValueOnce(initialServices.promise)
      .mockReturnValueOnce(retriedServices.promise)
    const listSubscriptions = vi.spyOn(api, 'listSubscriptions').mockResolvedValue([])
    const listVPSDomains = vi.spyOn(api, 'listVPSDomains').mockResolvedValue([])
    const overview = overviewFor('vps_001')

    const { result } = renderHook(() => useVPSDetailResources(overview))
    await waitFor(() => expect(result.current.subscriptions.status).toBe('ready'))
    await waitFor(() => expect(result.current.domains.status).toBe('ready'))

    act(() => result.current.retry('services'))
    expect(result.current.services).toEqual({ status: 'loading', items: [], error: null })
    expect(listVPSServices).toHaveBeenCalledTimes(2)
    expect(listSubscriptions).toHaveBeenCalledTimes(1)
    expect(listVPSDomains).toHaveBeenCalledTimes(1)

    act(() => retriedServices.resolve([service('svc_new', 'vps_001', 'Fresh Gateway')]))
    await waitFor(() => expect(result.current.services.status).toBe('ready'))
    expect(result.current.services.items[0]?.name).toBe('Fresh Gateway')

    await act(async () => {
      initialServices.resolve([service('svc_old', 'vps_001', 'Stale Gateway')])
      await initialServices.promise
    })
    expect(result.current.services.items[0]?.name).toBe('Fresh Gateway')
  })
})
