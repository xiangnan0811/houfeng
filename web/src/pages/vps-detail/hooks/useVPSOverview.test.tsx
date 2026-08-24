import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type { VPSOverview } from '../../../lib/types'
import { useVPSOverview } from './useVPSOverview'

function overview(): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: 'Edge',
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

function overviewFor(vpsId: string, displayName: string): VPSOverview {
  const value = overview()
  return {
    ...value,
    identity: {
      ...value.identity,
      vps_id: vpsId,
      display_name: displayName,
    },
  }
}

describe('useVPSOverview', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads overview and ignores stale responses', async () => {
    let resolveFirst!: (value: VPSOverview) => void
    const first = new Promise<VPSOverview>((resolve) => {
      resolveFirst = resolve
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockImplementationOnce(() => first)
      .mockResolvedValueOnce({
        ...overview(),
        identity: { ...overview().identity, vps_id: 'vps_002', display_name: 'Second' },
      })

    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id),
      { initialProps: { id: 'vps_001' } },
    )
    rerender({ id: 'vps_002' })
    resolveFirst(overview())

    await waitFor(() => expect(result.current.state.overview?.identity.display_name).toBe('Second'))
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('maps missing VPS to not_found without falling back', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(404, 'missing', { code: 'resource_not_found' }),
    )
    const { result } = renderHook(() => useVPSOverview('vps_missing'))
    await waitFor(() => expect(result.current.state.status).toBe('not_found'))
  })

  it('seeds from an initial overview without an immediate refetch', async () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
    const seeded = overview()
    const { result } = renderHook(() => useVPSOverview('vps_001', seeded))
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.overview?.identity.display_name).toBe('Edge')
    await waitFor(() => expect(get).not.toHaveBeenCalled())
  })

  it('issues one unseeded first-paint request under StrictMode', () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(
      () => new Promise<VPSOverview>(() => undefined),
    )

    renderHook(() => useVPSOverview('vps_001'), { reactStrictMode: true })

    expect(get).toHaveBeenCalledTimes(1)
  })

  it('does not refetch a seeded first paint under StrictMode', () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview')

    const { result } = renderHook(
      () => useVPSOverview('vps_001', overview()),
      { reactStrictMode: true },
    )

    expect(result.current.state.status).toBe('ready')
    expect(get).not.toHaveBeenCalled()
  })

  it('keeps the prior overview and releases duplicate suppression after a rejected refresh', async () => {
    let rejectRefresh!: (reason: unknown) => void
    const refresh = new Promise<VPSOverview>((_resolve, reject) => {
      rejectRefresh = reject
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(() => refresh)
    const seeded = overview()
    const recovered = {
      ...seeded,
      identity: { ...seeded.identity, display_name: 'Recovered' },
    }
    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id, seeded),
      { initialProps: { id: 'vps_001' } },
    )

    let first!: Promise<boolean>
    let duplicate!: Promise<boolean>
    act(() => {
      first = result.current.commands.refresh()
      duplicate = result.current.commands.refresh()
    })
    rerender({ id: '  vps_001  ' })

    expect(get).toHaveBeenCalledTimes(1)
    expect(result.current.state.status).toBe('loading')
    expect(result.current.state.overview).toBe(seeded)

    await act(async () => {
      rejectRefresh(new ApiError(503, 'refresh unavailable', { code: 'overview_unavailable' }))
      await Promise.all([first, duplicate])
    })

    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.overview).toBe(seeded)
    expect(result.current.state.errorMessage).toBe('VPS 概览不可用。')

    get.mockResolvedValueOnce(recovered)
    await act(async () => {
      await expect(result.current.commands.refresh()).resolves.toBe(true)
    })
    expect(get).toHaveBeenCalledTimes(2)
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.overview?.identity.display_name).toBe('Recovered')
  })

  it.each([
    [
      'typed decoder failure',
      new recordsApi.InvalidVPSOverviewResponseError('invalid_shape'),
    ],
    ['transport failure', new TypeError('private transport URL')],
    [
      'unknown 503',
      new ApiError(503, 'private upstream detail', { code: 'upstream_timeout' }),
    ],
  ])('retains a valid overview with safe refresh copy after %s', async (_caseName, failure) => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValueOnce(failure)
    const seeded = overview()
    const { result } = renderHook(() => useVPSOverview('vps_001', seeded))

    await act(async () => {
      await expect(result.current.commands.refresh()).resolves.toBe(false)
    })

    expect(get).toHaveBeenCalledTimes(1)
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.overview).toBe(seeded)
    expect(result.current.state.errorMessage).toBe('VPS 概览请求或响应校验失败，请重试。')
    expect(result.current.state.errorCode).toBeNull()
    expect(JSON.stringify(result.current.state)).not.toContain('private')
    expect(JSON.stringify(result.current.state)).not.toContain('upstream_timeout')
  })

  it('does not classify an unidentified 503 as overview unavailable', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(503, 'private upstream detail', { code: 'upstream_timeout' }),
    )
    const { result } = renderHook(() => useVPSOverview('vps_001'))

    await waitFor(() => expect(result.current.state.status).toBe('error'))
    expect(result.current.state.errorMessage).toBe('VPS 概览请求或响应校验失败，请重试。')
    expect(result.current.state.errorCode).toBeNull()
  })

  it('clears VPS A immediately while VPS B is pending', async () => {
    let resolveB!: (value: VPSOverview) => void
    const pendingB = new Promise<VPSOverview>((resolve) => {
      resolveB = resolve
    })
    vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(() => pendingB)
    const vpsA = overviewFor('vps_a', 'VPS A')
    const vpsB = overviewFor('vps_b', 'VPS B')
    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id, vpsA),
      { initialProps: { id: 'vps_a' } },
    )

    rerender({ id: 'vps_b' })

    expect(result.current.state.status).toBe('loading')
    expect(result.current.state.overview).toBeNull()

    await act(async () => {
      resolveB(vpsB)
      await pendingB
    })
  })

  it('does not restore VPS A as ready when VPS B fails', async () => {
    let rejectB!: (reason: unknown) => void
    const pendingB = new Promise<VPSOverview>((_resolve, reject) => {
      rejectB = reject
    })
    vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(() => pendingB)
    const vpsA = overviewFor('vps_a', 'VPS A')
    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id, vpsA),
      { initialProps: { id: 'vps_a' } },
    )

    rerender({ id: 'vps_b' })
    await act(async () => {
      rejectB(new ApiError(503, 'B unavailable', { code: 'overview_unavailable' }))
      await pendingB.catch(() => undefined)
    })

    expect(result.current.state.status).toBe('unavailable')
    expect(result.current.state.overview).toBeNull()
  })

  it('keeps a missing route not_found when the prior VPS resolves late', async () => {
    let resolveA!: (value: VPSOverview) => void
    const pendingA = new Promise<VPSOverview>((resolve) => {
      resolveA = resolve
    })
    vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(() => pendingA)
    const vpsA = overviewFor('vps_a', 'VPS A')
    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id),
      { initialProps: { id: 'vps_a' as string | undefined } },
    )

    rerender({ id: '   ' })
    expect(result.current.state.status).toBe('not_found')
    expect(result.current.state.overview).toBeNull()

    await act(async () => {
      resolveA(vpsA)
      await pendingA
    })

    expect(result.current.state.status).toBe('not_found')
    expect(result.current.state.overview).toBeNull()
  })

  it('invalidates a pending refresh when the hook unmounts', async () => {
    let resolveA!: (value: VPSOverview) => void
    const pendingA = new Promise<VPSOverview>((resolve) => {
      resolveA = resolve
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockImplementation(() => pendingA)
    const vpsA = overviewFor('vps_a', 'VPS A')
    const { result, unmount } = renderHook(() => useVPSOverview('vps_a', vpsA))

    let refresh!: Promise<boolean>
    act(() => {
      refresh = result.current.commands.refresh()
    })
    unmount()

    resolveA(vpsA)
    await expect(refresh).resolves.toBe(false)
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('does not seed an overview whose identity belongs to another VPS', async () => {
    const vpsA = overviewFor('vps_a', 'VPS A')
    const vpsB = overviewFor('vps_b', 'VPS B')
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(vpsB)
    const { result } = renderHook(() => useVPSOverview('vps_b', vpsA))

    expect(result.current.state.status).toBe('loading')
    expect(result.current.state.overview).toBeNull()
    await waitFor(() => expect(result.current.state.overview?.identity.vps_id).toBe('vps_b'))
    expect(get).toHaveBeenCalledTimes(1)
  })
})
