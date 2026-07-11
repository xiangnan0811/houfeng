import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupListFilter,
  AssetDecisionGroupSummary,
} from '../../../lib/types'
import { useAssetDecisionGroups } from './useAssetDecisionGroups'

const GROUP_SUMMARY: AssetDecisionGroupSummary = {
  group_id: 'adg_001',
  group_type: 'renewal_attention',
  view: 'renewal',
  title: '续费组合',
  scope_key: 'renewal-window',
  scope_label: '未来 30 天',
  priority: 90,
  member_count: 0,
  lifecycle_counts: {},
  usage_counts: {},
  renewal_decision_counts: {},
  renewal_window_count: 0,
  unreviewed_count: 0,
  migrate_count: 0,
  cancel_count: 0,
  cancellation_attention_count: 0,
  idle_count: 0,
  standby_count: 0,
  in_use_count: 0,
  service_count: 0,
  domain_count: 0,
  target_count: 0,
  running_target_count: 0,
  monitoring_link_count: 0,
  abnormal_monitoring_count: 0,
  active_incident_count: 0,
  primary_issue_summary: '',
  monthly_cost_by_currency: [],
  evidence_chips: [],
  evidence_assessment: {
    confidence_score: 80,
    pressure_score: 20,
    readiness_score: 70,
    quality_tier: 'strong',
    decision_bias: 'keep',
    support_signal_count: 2,
    risk_signal_count: 0,
    gap_signal_count: 0,
    summary: '证据可用',
  },
}

const GROUP_DETAIL: AssetDecisionGroupDetail = {
  ...GROUP_SUMMARY,
  members: [],
}

const FILTER: AssetDecisionGroupListFilter = {
  view: 'renewal',
  renew_within_days: 60,
  provider_id: 'provider_001',
}

function deferred<T>() {
  let resolvePromise: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

describe('useAssetDecisionGroups', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the filtered list and keyed detail independently', async () => {
    const listRequest = deferred<AssetDecisionGroupSummary[]>()
    const detailRequest = deferred<AssetDecisionGroupDetail>()
    const listGroups = vi.spyOn(api, 'listAssetDecisionGroups').mockReturnValue(listRequest.promise)
    const getGroup = vi.spyOn(api, 'getAssetDecisionGroup').mockReturnValue(detailRequest.promise)

    const { result } = renderHook(() => useAssetDecisionGroups({
      filter: FILTER,
      renewalWindow: 60,
      selectedGroupID: 'adg_001',
      revision: 0,
    }))

    expect(result.current.state.list).toEqual({ loading: true, error: null, groups: [] })
    expect(result.current.state.detail).toEqual({ loading: true, error: null, detail: null })
    expect(listGroups).toHaveBeenCalledWith(FILTER)
    expect(getGroup).toHaveBeenCalledWith('adg_001', { renew_within_days: 60 })

    act(() => listRequest.resolve([GROUP_SUMMARY]))
    await waitFor(() => expect(result.current.state.list.groups).toEqual([GROUP_SUMMARY]))
    expect(result.current.state.detail.loading).toBe(true)

    act(() => detailRequest.resolve(GROUP_DETAIL))
    await waitFor(() => expect(result.current.state.detail).toEqual({
      loading: false,
      error: null,
      detail: GROUP_DETAIL,
    }))
  })

  it('keeps a list failure separate from a successful detail', async () => {
    vi.spyOn(api, 'listAssetDecisionGroups').mockRejectedValue('offline')
    vi.spyOn(api, 'getAssetDecisionGroup').mockResolvedValue(GROUP_DETAIL)

    const { result } = renderHook(() => useAssetDecisionGroups({
      filter: FILTER,
      renewalWindow: 60,
      selectedGroupID: 'adg_001',
      revision: 0,
    }))

    await waitFor(() => expect(result.current.state.list).toEqual({
      loading: false,
      error: '加载资产决策组失败',
      groups: [],
    }))
    expect(result.current.state.detail).toEqual({
      loading: false,
      error: null,
      detail: GROUP_DETAIL,
    })
  })

  it('keeps a detail failure separate from a successful list', async () => {
    vi.spyOn(api, 'listAssetDecisionGroups').mockResolvedValue([GROUP_SUMMARY])
    vi.spyOn(api, 'getAssetDecisionGroup').mockRejectedValue('offline')

    const { result } = renderHook(() => useAssetDecisionGroups({
      filter: FILTER,
      renewalWindow: 60,
      selectedGroupID: 'adg_001',
      revision: 0,
    }))

    await waitFor(() => expect(result.current.state.detail).toEqual({
      loading: false,
      error: '加载决策组详情失败',
      detail: null,
    }))
    expect(result.current.state.list).toEqual({
      loading: false,
      error: null,
      groups: [GROUP_SUMMARY],
    })
  })

  it('ignores a stale detail response after the selected group changes', async () => {
    const firstRequest = deferred<AssetDecisionGroupDetail>()
    const secondRequest = deferred<AssetDecisionGroupDetail>()
    const nextDetail = {
      ...GROUP_DETAIL,
      group_id: 'adg_002',
      title: '成本组合',
    }
    vi.spyOn(api, 'listAssetDecisionGroups').mockResolvedValue([GROUP_SUMMARY])
    const getGroup = vi.spyOn(api, 'getAssetDecisionGroup')
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(secondRequest.promise)

    const { result, rerender } = renderHook(
      ({ selectedGroupID }) => useAssetDecisionGroups({
        filter: FILTER,
        renewalWindow: 60,
        selectedGroupID,
        revision: 0,
      }),
      { initialProps: { selectedGroupID: 'adg_001' as string | null } },
    )

    rerender({ selectedGroupID: 'adg_002' })
    expect(result.current.state.detail).toEqual({ loading: true, error: null, detail: null })

    act(() => secondRequest.resolve(nextDetail))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(nextDetail))

    await act(async () => {
      firstRequest.resolve(GROUP_DETAIL)
      await firstRequest.promise
    })

    expect(result.current.state.detail.detail).toBe(nextDetail)
    expect(getGroup).toHaveBeenCalledTimes(2)
  })

  it('reloads both reads when the external revision changes', async () => {
    const nextDetail = { ...GROUP_DETAIL, title: '刷新后的组合' }
    const listGroups = vi.spyOn(api, 'listAssetDecisionGroups')
      .mockResolvedValueOnce([GROUP_SUMMARY])
      .mockResolvedValueOnce([{ ...GROUP_SUMMARY, title: '刷新后的组合' }])
    const getGroup = vi.spyOn(api, 'getAssetDecisionGroup')
      .mockResolvedValueOnce(GROUP_DETAIL)
      .mockResolvedValueOnce(nextDetail)

    const { result, rerender } = renderHook(
      ({ revision }) => useAssetDecisionGroups({
        filter: FILTER,
        renewalWindow: 60,
        selectedGroupID: 'adg_001',
        revision,
      }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.detail.detail).toBe(GROUP_DETAIL))

    rerender({ revision: 1 })

    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    await waitFor(() => expect(result.current.state.detail.detail).toBe(nextDetail))
    expect(listGroups).toHaveBeenCalledTimes(2)
    expect(getGroup).toHaveBeenCalledTimes(2)
  })

  it('supports domain-local list and detail retries', async () => {
    const listGroups = vi.spyOn(api, 'listAssetDecisionGroups').mockResolvedValue([GROUP_SUMMARY])
    const getGroup = vi.spyOn(api, 'getAssetDecisionGroup').mockResolvedValue(GROUP_DETAIL)

    const { result } = renderHook(() => useAssetDecisionGroups({
      filter: FILTER,
      renewalWindow: 60,
      selectedGroupID: 'adg_001',
      revision: 0,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(GROUP_DETAIL))

    act(() => result.current.commands.reloadList())
    await waitFor(() => expect(listGroups).toHaveBeenCalledTimes(2))
    expect(getGroup).toHaveBeenCalledTimes(1)

    act(() => result.current.commands.reloadDetail())
    await waitFor(() => expect(getGroup).toHaveBeenCalledTimes(2))
    expect(listGroups).toHaveBeenCalledTimes(2)
  })

  it('resets the panel for a different selection and on explicit detail reset', async () => {
    vi.spyOn(api, 'listAssetDecisionGroups').mockResolvedValue([GROUP_SUMMARY])
    vi.spyOn(api, 'getAssetDecisionGroup').mockResolvedValue(GROUP_DETAIL)

    const { result, rerender } = renderHook(
      ({ selectedGroupID }) => useAssetDecisionGroups({
        filter: FILTER,
        renewalWindow: 60,
        selectedGroupID,
        revision: 0,
      }),
      { initialProps: { selectedGroupID: 'adg_001' as string | null } },
    )
    await waitFor(() => expect(result.current.state.detail.detail).toBe(GROUP_DETAIL))

    act(() => result.current.commands.selectPanel('members'))
    expect(result.current.state.detailPanel).toBe('members')

    rerender({ selectedGroupID: 'adg_002' })
    expect(result.current.state.detailPanel).toBe('overview')

    act(() => result.current.commands.selectPanel('raw'))
    expect(result.current.state.detailPanel).toBe('raw')
    act(() => result.current.commands.resetDetailUI())
    expect(result.current.state.detailPanel).toBe('overview')
  })
})
