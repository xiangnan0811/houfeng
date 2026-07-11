import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDecisionGroupListFilter,
  AssetDecisionOverview,
} from '../../../lib/types'
import { useAssetDecisionPortfolio } from './useAssetDecisionPortfolio'

const OVERVIEW: AssetDecisionOverview = {
  snapshot_generated_at: '2026-07-11T06:00:00Z',
  renew_within_days: 30,
  group_count: 3,
  member_vps_count: 4,
  needs_decision_count: 1,
  renewal_group_count: 1,
  region_group_count: 1,
  provider_group_count: 1,
  cost_group_count: 1,
  evidence_group_count: 1,
  top_groups: [],
  type_counts: { renewal_attention: 1 },
  view_counts: { renewal: 1 },
  source_availability: {
    subscriptions: true,
    services: true,
    domains: true,
    monitoring: true,
    targets: true,
  },
}

function deferred<T>() {
  let resolvePromise: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

describe('useAssetDecisionPortfolio', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the overview for the complete asset-decision filter', async () => {
    const request = deferred<AssetDecisionOverview>()
    const getOverview = vi.spyOn(api, 'getAssetDecisionOverview').mockReturnValue(request.promise)
    const filter: AssetDecisionGroupListFilter = {
      view: 'provider',
      renew_within_days: 60,
      provider_id: 'provider_001',
      country: 'DE',
    }

    const { result } = renderHook(() => useAssetDecisionPortfolio({ filter, revision: 0 }))

    expect(result.current.state).toEqual({
      loading: true,
      error: null,
      overview: null,
    })
    expect(getOverview).toHaveBeenCalledOnce()
    expect(getOverview).toHaveBeenCalledWith(filter)

    act(() => request.resolve(OVERVIEW))

    await waitFor(() => expect(result.current.state).toEqual({
      loading: false,
      error: null,
      overview: OVERVIEW,
    }))
  })

  it('keeps the settled overview visible while an equivalent filter identity revalidates', async () => {
    const revalidation = deferred<AssetDecisionOverview>()
    const getOverview = vi.spyOn(api, 'getAssetDecisionOverview')
      .mockResolvedValueOnce(OVERVIEW)
      .mockReturnValueOnce(revalidation.promise)
    const filter: AssetDecisionGroupListFilter = {
      view: 'needs_decision',
      renew_within_days: 30,
      provider_id: 'provider_001',
    }

    const { result, rerender } = renderHook(
      ({ activeFilter }) => useAssetDecisionPortfolio({ filter: activeFilter, revision: 0 }),
      { initialProps: { activeFilter: filter } },
    )
    await waitFor(() => expect(result.current.state.overview).toBe(OVERVIEW))

    const equivalentFilter = { ...filter }
    rerender({ activeFilter: equivalentFilter })

    expect(getOverview).toHaveBeenNthCalledWith(2, equivalentFilter)
    expect(result.current.state).toEqual({
      loading: false,
      error: null,
      overview: OVERVIEW,
    })
  })

  it('keeps an overview failure local and uses the existing fallback message', async () => {
    vi.spyOn(api, 'getAssetDecisionOverview').mockRejectedValue('offline')
    const filter: AssetDecisionGroupListFilter = {
      view: 'needs_decision',
      renew_within_days: 30,
    }

    const { result } = renderHook(() => useAssetDecisionPortfolio({
      filter,
      revision: 0,
    }))

    await waitFor(() => expect(result.current.state).toEqual({
      loading: false,
      error: '加载资产组合概览失败',
      overview: null,
    }))
  })

  it('ignores a stale response after the filter changes', async () => {
    const firstRequest = deferred<AssetDecisionOverview>()
    const secondRequest = deferred<AssetDecisionOverview>()
    const nextOverview = { ...OVERVIEW, renew_within_days: 90, group_count: 5 }
    const getOverview = vi.spyOn(api, 'getAssetDecisionOverview')
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(secondRequest.promise)
    const firstFilter: AssetDecisionGroupListFilter = {
      view: 'needs_decision',
      renew_within_days: 30,
    }
    const secondFilter: AssetDecisionGroupListFilter = {
      view: 'cost',
      renew_within_days: 90,
    }

    const { result, rerender } = renderHook(
      ({ filter }) => useAssetDecisionPortfolio({ filter, revision: 0 }),
      { initialProps: { filter: firstFilter } },
    )

    rerender({ filter: secondFilter })
    expect(result.current.state.loading).toBe(true)
    expect(getOverview).toHaveBeenNthCalledWith(2, secondFilter)

    act(() => secondRequest.resolve(nextOverview))
    await waitFor(() => expect(result.current.state.overview).toBe(nextOverview))

    await act(async () => {
      firstRequest.resolve(OVERVIEW)
      await firstRequest.promise
    })

    expect(result.current.state).toEqual({
      loading: false,
      error: null,
      overview: nextOverview,
    })
  })

  it('reloads only its own overview when requested locally', async () => {
    const nextOverview = { ...OVERVIEW, group_count: 7 }
    const getOverview = vi.spyOn(api, 'getAssetDecisionOverview')
      .mockResolvedValueOnce(OVERVIEW)
      .mockResolvedValueOnce(nextOverview)
    const filter: AssetDecisionGroupListFilter = {
      view: 'needs_decision',
      renew_within_days: 30,
    }

    const { result } = renderHook(() => useAssetDecisionPortfolio({ filter, revision: 0 }))
    await waitFor(() => expect(result.current.state.overview).toBe(OVERVIEW))

    act(() => result.current.commands.reload())

    expect(result.current.state.loading).toBe(true)
    await waitFor(() => expect(result.current.state.overview).toBe(nextOverview))
    expect(getOverview).toHaveBeenCalledTimes(2)
  })

  it('reloads when the external revision changes', async () => {
    const nextOverview = { ...OVERVIEW, group_count: 9 }
    const getOverview = vi.spyOn(api, 'getAssetDecisionOverview')
      .mockResolvedValueOnce(OVERVIEW)
      .mockResolvedValueOnce(nextOverview)
    const filter: AssetDecisionGroupListFilter = {
      view: 'needs_decision',
      renew_within_days: 30,
    }

    const { result, rerender } = renderHook(
      ({ revision }) => useAssetDecisionPortfolio({ filter, revision }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.overview).toBe(OVERVIEW))

    rerender({ revision: 1 })

    expect(result.current.state.loading).toBe(true)
    await waitFor(() => expect(result.current.state.overview).toBe(nextOverview))
    expect(getOverview).toHaveBeenCalledTimes(2)
  })
})
