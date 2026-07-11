import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupListFilter,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupSummary,
  AssetDecisionScenarioTemplateDetail,
  VPSAssetRecord,
} from '../../../lib/types'
import { useAssetDecisionManualGroups } from './useAssetDecisionManualGroups'

const FILTER: AssetDecisionGroupListFilter = {
  view: 'needs_decision',
  renew_within_days: 30,
  provider_id: 'provider_001',
}

const OTHER_FILTER: AssetDecisionGroupListFilter = {
  view: 'cost',
  renew_within_days: 60,
}

const VPS: VPSAssetRecord = {
  vps_id: 'vps_001',
  display_name: 'Frankfurt Primary',
  provider_id: 'provider_001',
  provider_name: 'Provider One',
  product_name: 'Compute',
  order_ref: 'order-001',
  country: 'DE',
  region: 'Hesse',
  city: 'Frankfurt',
  datacenter: 'fra-1',
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
  labels: [],
  note: '',
  active_monitoring_instance_link_count: 1,
  running_monitoring_instance_count: 1,
  running_target_count: 1,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  archived_at: null,
}

const ASSESSMENT = {
  confidence_score: 80,
  pressure_score: 20,
  readiness_score: 70,
  quality_tier: 'strong' as const,
  decision_bias: 'keep' as const,
  support_signal_count: 2,
  risk_signal_count: 0,
  gap_signal_count: 0,
  summary: '证据可用',
}

const SOURCE_AVAILABILITY = {
  subscriptions: true,
  services: true,
  domains: true,
  monitoring: true,
  targets: true,
}

function manualDetail(
  manualGroupID = 'admg_001',
  overrides: Partial<AssetDecisionManualGroupDetail> = {},
): AssetDecisionManualGroupDetail {
  return {
    manual_group_id: manualGroupID,
    status: 'active',
    scenario: 'primary_standby',
    title: '德国主备组合',
    goal: '保留主力',
    note: '人工复核',
    source_type: 'auto_group',
    source_group_id: 'adg_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: 1,
    lifecycle_counts: { active: 1 },
    usage_counts: { in_use: 1 },
    renewal_decision_counts: { unreviewed: 1 },
    renewal_window_count: 1,
    unreviewed_count: 1,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 0,
    in_use_count: 1,
    service_count: 1,
    domain_count: 0,
    target_count: 1,
    running_target_count: 1,
    monitoring_link_count: 1,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [],
    evidence_chips: [],
    evidence_assessment: ASSESSMENT,
    source_availability: SOURCE_AVAILABILITY,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    archived_at: null,
    members: [{
      manual_group_id: manualGroupID,
      vps_id: VPS.vps_id,
      vps: VPS,
      primary_subscription: null,
      subscription_count: 0,
      active_subscription_count: 0,
      inactive_subscription_count: 0,
      service_count: 1,
      domain_count: 0,
      target_count: 1,
      running_target_count: 1,
      monitoring_link_count: 1,
      running_monitoring_count: 1,
      abnormal_monitoring_count: 0,
      active_incident_count: 0,
      primary_issue_summary: '',
      suggested_role: 'primary_candidate',
      suggested_action: 'keep',
      intended_role: 'primary_candidate',
      intended_action: 'keep',
      reason: '主力稳定',
      note: '',
      sort_order: 10,
      evidence_chips: [],
      evidence_assessment: ASSESSMENT,
      renewal_within_window: true,
      source_availability: SOURCE_AVAILABILITY,
      evidence_snapshot: {},
      current_fact_found: true,
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-01T00:00:00Z',
    }],
    ...overrides,
  }
}

function manualSummary(detail: AssetDecisionManualGroupDetail): AssetDecisionManualGroupSummary {
  const { members, ...summary } = detail
  void members
  return summary
}

const AUTO_GROUP = {
  group_id: 'adg_001',
  group_type: 'provider_portfolio',
  title: 'Provider Review',
} as AssetDecisionGroupDetail

const TEMPLATE: AssetDecisionScenarioTemplateDetail = {
  template_id: 'adt_001',
  builtin: true,
  status: 'active',
  scenario: 'provider_review',
  title: '服务商评估',
  goal: '复核服务商组合',
  note: '',
  member_count: 0,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  archived_at: null,
  members: [],
}

function deferred<T>() {
  let resolvePromise: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

function mockSuccessfulReads(detail = manualDetail()) {
  vi.spyOn(api, 'listAssetDecisionManualGroups').mockResolvedValue([manualSummary(detail)])
  vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
  vi.spyOn(api, 'getAssetDecisionManualGroup').mockResolvedValue(detail)
}

describe('useAssetDecisionManualGroups', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads filtered summaries, the keyed detail, and the unfiltered VPS catalog independently', async () => {
    const detail = manualDetail()
    const listGroups = vi.spyOn(api, 'listAssetDecisionManualGroups').mockResolvedValue([manualSummary(detail)])
    const listVPS = vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
    const getGroup = vi.spyOn(api, 'getAssetDecisionManualGroup').mockResolvedValue(detail)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))

    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    expect(result.current.state.catalog.loading).toBe(true)
    expect(listGroups).toHaveBeenCalledWith(FILTER)
    expect(listVPS).toHaveBeenCalledWith()
    expect(getGroup).toHaveBeenCalledWith('admg_001')

    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))
    expect(result.current.state.list.groups).toEqual([manualSummary(detail)])
    expect(result.current.state.catalog.rows).toEqual([VPS])
    expect(result.current.state.candidateRows).toEqual([])
  })

  it('keeps list, detail, and catalog failures local to their read surface', async () => {
    const detail = manualDetail()
    vi.spyOn(api, 'listAssetDecisionManualGroups').mockRejectedValue('list offline')
    vi.spyOn(api, 'listVPSAssets').mockRejectedValue('catalog offline')
    vi.spyOn(api, 'getAssetDecisionManualGroup').mockResolvedValue(detail)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))

    await waitFor(() => expect(result.current.state.list.error).toBe('加载自定义组合失败'))
    expect(result.current.state.catalog.error).toBe('加载 VPS 候选失败')
    expect(result.current.state.detail.detail).toBe(detail)
    expect(result.current.state.detail.error).toBeNull()
  })

  it('ignores a stale detail response after the selected manual group changes', async () => {
    const firstRequest = deferred<AssetDecisionManualGroupDetail>()
    const secondRequest = deferred<AssetDecisionManualGroupDetail>()
    const nextDetail = manualDetail('admg_002', { title: '第二个组合' })
    vi.spyOn(api, 'listAssetDecisionManualGroups').mockResolvedValue([])
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
    const getGroup = vi.spyOn(api, 'getAssetDecisionManualGroup')
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(secondRequest.promise)

    const { result, rerender } = renderHook(
      ({ selectedManualGroupID }) => useAssetDecisionManualGroups({
        filter: FILTER,
        renewalWindow: 30,
        selectedManualGroupID,
        revision: 0,
        onNotice: vi.fn(),
      }),
      { initialProps: { selectedManualGroupID: 'admg_001' as string | null } },
    )

    rerender({ selectedManualGroupID: 'admg_002' })
    expect(result.current.state.detail).toEqual({ loading: true, error: null, detail: null })

    act(() => secondRequest.resolve(nextDetail))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(nextDetail))

    await act(async () => {
      firstRequest.resolve(manualDetail())
      await firstRequest.promise
    })
    expect(result.current.state.detail.detail).toBe(nextDetail)
    expect(getGroup).toHaveBeenCalledTimes(2)
  })

  it('reloads all three read surfaces on an external revision', async () => {
    mockSuccessfulReads()
    const listGroups = vi.mocked(api.listAssetDecisionManualGroups)
    const listVPS = vi.mocked(api.listVPSAssets)
    const getGroup = vi.mocked(api.getAssetDecisionManualGroup)

    const { result, rerender } = renderHook(
      ({ revision }) => useAssetDecisionManualGroups({
        filter: FILTER,
        renewalWindow: 30,
        selectedManualGroupID: 'admg_001',
        revision,
        onNotice: vi.fn(),
      }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    rerender({ revision: 1 })
    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    expect(result.current.state.catalog.loading).toBe(true)
    await waitFor(() => expect(getGroup).toHaveBeenCalledTimes(2))
    expect(listGroups).toHaveBeenCalledTimes(2)
    expect(listVPS).toHaveBeenCalledTimes(2)
  })

  it('creates from an automatic group with the exact input and preserves its summary for only the active filter', async () => {
    vi.spyOn(api, 'listAssetDecisionManualGroups')
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
    const created = manualDetail('admg_created', { title: 'Provider Review' })
    const createGroup = vi.spyOn(api, 'createAssetDecisionManualGroup').mockResolvedValue(created)
    const notice = vi.fn()

    const { result, rerender } = renderHook(
      ({ filter }) => useAssetDecisionManualGroups({
        filter,
        renewalWindow: 60,
        selectedManualGroupID: null,
        revision: 0,
        onNotice: notice,
      }),
      { initialProps: { filter: FILTER } },
    )
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))

    let returned: AssetDecisionManualGroupDetail | null = null
    await act(async () => {
      returned = await result.current.commands.createFromAutomatic(AUTO_GROUP)
    })

    expect(returned).toBe(created)
    expect(createGroup).toHaveBeenCalledWith({
      source_type: 'auto_group',
      source_group_id: 'adg_001',
      renew_within_days: 60,
      scenario: 'provider_review',
      title: 'Provider Review',
      goal: '',
      note: '由自动组 adg_001 创建',
    })
    expect(result.current.state.list.groups[0]?.manual_group_id).toBe('admg_created')
    expect(notice).toHaveBeenCalledWith('已创建自定义组合：Provider Review')

    rerender({ filter: OTHER_FILTER })
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))
    expect(result.current.state.list.groups).toEqual([])

    rerender({ filter: FILTER })
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))
    expect(result.current.state.list.groups[0]?.manual_group_id).toBe('admg_created')
  })

  it.each([
    ['region_portfolio', 'region_review'],
    ['cost_pressure', 'budget_reduction'],
    ['cancellation_attention', 'migration_retirement'],
    ['evidence_gap', 'evidence_cleanup'],
    ['renewal_attention', 'general'],
  ] as const)('maps automatic group %s to scenario %s', async (groupType, scenario) => {
    vi.spyOn(api, 'listAssetDecisionManualGroups').mockResolvedValue([])
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
    const created = manualDetail(`admg_${groupType}`, { scenario })
    const createGroup = vi.spyOn(api, 'createAssetDecisionManualGroup').mockResolvedValue(created)
    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: null,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))

    await act(async () => {
      await result.current.commands.createFromAutomatic({
        ...AUTO_GROUP,
        group_type: groupType,
      })
    })

    expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({ scenario }))
  })

  it('creates from a template with its exact draft and rejects a blank title before the API', async () => {
    mockSuccessfulReads()
    const created = manualDetail('admg_template', { title: '模板组合' })
    const createGroup = vi.spyOn(api, 'createManualGroupFromScenarioTemplate').mockResolvedValue(created)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    await act(async () => {
      const returned = await result.current.commands.createFromTemplate(TEMPLATE, {
        title: '  模板组合  ',
        goal: '  目标  ',
        note: '  备注  ',
        renewWithinDays: 90,
      })
      expect(returned).toBe(created)
    })

    expect(createGroup).toHaveBeenCalledWith('adt_001', {
      title: '模板组合',
      goal: '目标',
      note: '备注',
      scenario: 'provider_review',
      status: 'active',
      renew_within_days: 90,
    })

    await act(async () => {
      const returned = await result.current.commands.createFromTemplate(TEMPLATE, {
        title: '   ',
        goal: '',
        note: '',
        renewWithinDays: 30,
      })
      expect(returned).toBeNull()
    })
    expect(createGroup).toHaveBeenCalledTimes(1)
    expect(result.current.state.error).toBe('请填写要创建的自定义组合标题')
  })

  it('patches the selected group and merges the returned representation locally', async () => {
    const detail = manualDetail()
    mockSuccessfulReads(detail)
    const updated = manualDetail('admg_001', { title: '更新后的组合', status: 'archived' })
    const patchGroup = vi.spyOn(api, 'patchAssetDecisionManualGroup').mockResolvedValue(updated)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    await act(async () => {
      await result.current.commands.patchCurrent({
        title: '更新后的组合',
        goal: '更新目标',
        note: '',
        scenario: 'provider_review',
        status: 'archived',
      })
    })

    expect(patchGroup).toHaveBeenCalledWith('admg_001', {
      title: '更新后的组合',
      goal: '更新目标',
      note: '',
      scenario: 'provider_review',
      status: 'archived',
    })
    expect(result.current.state.detail.detail).toBe(updated)
    expect(result.current.state.list.groups[0]?.title).toBe('更新后的组合')
  })

  it('validates a patch and applies optional-field defaults without clearing a pending member removal', async () => {
    const detail = manualDetail()
    mockSuccessfulReads(detail)
    const updated = manualDetail('admg_001', { title: '精简更新', goal: '', note: '' })
    const patchGroup = vi.spyOn(api, 'patchAssetDecisionManualGroup').mockResolvedValue(updated)
    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    await act(async () => result.current.commands.patchCurrent({ title: '   ' }))
    expect(result.current.state.error).toBe('请填写自定义组合标题')
    expect(patchGroup).not.toHaveBeenCalled()

    await act(async () => result.current.commands.patchCurrent({}))
    expect(result.current.state.error).toBe('请填写自定义组合标题')
    expect(patchGroup).not.toHaveBeenCalled()

    const member = detail.members[0]!
    act(() => result.current.commands.requestMemberRemoval(member))
    await act(async () => result.current.commands.patchCurrent({ title: '  精简更新  ' }))

    expect(patchGroup).toHaveBeenCalledWith('admg_001', {
      title: '精简更新',
      goal: '',
      note: '',
      scenario: detail.scenario,
      status: detail.status,
    })
    expect(result.current.state.pendingMemberRemoval).toBe(member)
  })

  it('adds a member from the owned draft without writing any VPS business object', async () => {
    const detail = manualDetail('admg_001', { members: [] })
    mockSuccessfulReads(detail)
    const updated = manualDetail()
    const addMember = vi.spyOn(api, 'addAssetDecisionManualGroupMember').mockResolvedValue(updated)
    const updateVPS = vi.spyOn(api, 'updateVPSAsset')

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.updateMemberAddDraft({
      vpsID: ' vps_001 ',
      intendedRole: 'primary_candidate',
      intendedAction: 'keep',
      reason: ' 主力 ',
      note: ' 保留 ',
      sortOrder: '12',
    }))
    await act(async () => result.current.commands.addMember())

    expect(addMember).toHaveBeenCalledWith('admg_001', {
      vps_id: 'vps_001',
      intended_role: 'primary_candidate',
      intended_action: 'keep',
      reason: '主力',
      note: '保留',
      sort_order: 12,
    })
    expect(updateVPS).not.toHaveBeenCalled()
    expect(result.current.state.memberAddDraft.vpsID).toBe('')
    expect(result.current.state.memberAddAdvanced).toBe(false)
    expect(result.current.state.detail.detail).toBe(updated)
  })

  it('requires a VPS selection and omits an invalid optional sort order', async () => {
    const detail = manualDetail('admg_001', { members: [] })
    mockSuccessfulReads(detail)
    const updated = manualDetail()
    const addMember = vi.spyOn(api, 'addAssetDecisionManualGroupMember').mockResolvedValue(updated)
    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    await act(async () => result.current.commands.addMember())
    expect(result.current.state.error).toBe('请选择要加入组合的 VPS')
    expect(addMember).not.toHaveBeenCalled()

    act(() => result.current.commands.updateMemberAddDraft({
      vpsID: 'vps_001',
      sortOrder: 'not-a-number',
    }))
    await act(async () => result.current.commands.addMember())

    expect(addMember).toHaveBeenCalledWith('admg_001', {
      vps_id: 'vps_001',
      intended_role: 'observe_candidate',
      intended_action: 'review',
      reason: '',
      note: '',
    })
  })

  it('requires an explicit pending confirmation before deleting a member', async () => {
    const detail = manualDetail()
    mockSuccessfulReads(detail)
    const updated = manualDetail('admg_001', { members: [], member_count: 0 })
    const deleteMember = vi.spyOn(api, 'deleteAssetDecisionManualGroupMember').mockResolvedValue(updated)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))
    const member = detail.members[0]!

    await act(async () => result.current.commands.removeMember(member))
    expect(deleteMember).not.toHaveBeenCalled()

    act(() => result.current.commands.requestMemberRemoval(member))
    expect(result.current.state.pendingMemberRemoval).toBe(member)
    expect(deleteMember).not.toHaveBeenCalled()

    act(() => result.current.commands.cancelMemberRemoval())
    expect(deleteMember).not.toHaveBeenCalled()

    act(() => result.current.commands.requestMemberRemoval(member))
    await act(async () => result.current.commands.removeMember(member))

    expect(deleteMember).toHaveBeenCalledWith('admg_001', 'vps_001')
    expect(result.current.state.pendingMemberRemoval).toBeNull()
    expect(result.current.state.memberSaving.vps_001).toBe(false)
    expect(result.current.state.detail.detail).toBe(updated)
  })

  it('blocks duplicate removal requests and names a missing-fact member by ID', async () => {
    const base = manualDetail()
    const member = { ...base.members[0]!, current_fact_found: false }
    const detail = manualDetail('admg_001', { members: [member] })
    mockSuccessfulReads(detail)
    const removal = deferred<AssetDecisionManualGroupDetail>()
    vi.spyOn(api, 'deleteAssetDecisionManualGroupMember').mockReturnValue(removal.promise)
    const notice = vi.fn()
    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.requestMemberRemoval(member))
    act(() => { void result.current.commands.removeMember(member) })
    await waitFor(() => expect(result.current.state.memberSaving.vps_001).toBe(true))

    act(() => result.current.commands.requestMemberRemoval(member))
    expect(result.current.state.pendingMemberRemoval).toBeNull()

    await act(async () => {
      removal.resolve(manualDetail('admg_001', { members: [], member_count: 0 }))
      await removal.promise
    })
    expect(notice).toHaveBeenCalledWith('成员已移出自定义组合：vps_001')
  })

  it('keeps detail-only commands inert without a selected manual group', async () => {
    vi.spyOn(api, 'listAssetDecisionManualGroups').mockResolvedValue([])
    vi.spyOn(api, 'listVPSAssets').mockResolvedValue([VPS])
    const patchGroup = vi.spyOn(api, 'patchAssetDecisionManualGroup')
    const addMember = vi.spyOn(api, 'addAssetDecisionManualGroupMember')
    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: null,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))

    act(() => {
      result.current.commands.updateMemberAddDraft({ vpsID: 'vps_001' })
      result.current.commands.setMemberAddAdvanced(true)
      result.current.commands.selectPanel('add')
    })
    await act(async () => {
      await result.current.commands.patchCurrent({ title: '不会保存' })
      await result.current.commands.addMember()
    })

    expect(result.current.state.detailPanel).toBe('overview')
    expect(result.current.state.memberAddDraft.vpsID).toBe('')
    expect(result.current.state.memberAddAdvanced).toBe(false)
    expect(patchGroup).not.toHaveBeenCalled()
    expect(addMember).not.toHaveBeenCalled()
  })

  it('owns panel and member-add draft reset semantics', async () => {
    mockSuccessfulReads()
    const getGroup = vi.mocked(api.getAssetDecisionManualGroup)

    const { result } = renderHook(() => useAssetDecisionManualGroups({
      filter: FILTER,
      renewalWindow: 30,
      selectedManualGroupID: 'admg_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    act(() => {
      result.current.commands.updateMemberAddDraft({ reason: '临时理由', note: '临时备注', sortOrder: '5' })
      result.current.commands.setMemberAddAdvanced(true)
    })
    expect(result.current.state.memberAddAdvanced).toBe(true)

    act(() => result.current.commands.selectPanel('add'))
    expect(result.current.state.detailPanel).toBe('add')
    expect(result.current.state.memberAddAdvanced).toBe(false)
    expect(result.current.state.memberAddDraft).toMatchObject({ reason: '', note: '', sortOrder: '' })

    act(() => result.current.commands.resetDetailUI())
    expect(result.current.state.detailPanel).toBe('overview')
    expect(result.current.state.memberAddDraft.vpsID).toBe('')
    expect(result.current.state.error).toBeNull()
    expect(getGroup).toHaveBeenCalledOnce()
  })

  it('does not revive manual-group UI state after the route leaves and returns', async () => {
    mockSuccessfulReads()
    const { result, rerender } = renderHook(
      ({ selectedManualGroupID }: { selectedManualGroupID: string | null }) => useAssetDecisionManualGroups({
        filter: FILTER,
        renewalWindow: 30,
        selectedManualGroupID,
        revision: 0,
        onNotice: vi.fn(),
      }),
      { initialProps: { selectedManualGroupID: 'admg_001' as string | null } },
    )
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))
    act(() => {
      result.current.commands.selectPanel('add')
      result.current.commands.updateMemberAddDraft({ vpsID: 'vps_001', reason: '临时理由' })
    })
    expect(result.current.state.detailPanel).toBe('add')

    rerender({ selectedManualGroupID: null })
    await act(async () => { await Promise.resolve() })
    rerender({ selectedManualGroupID: 'admg_001' })

    expect(result.current.state.detailPanel).toBe('overview')
    expect(result.current.state.memberAddDraft).toMatchObject({ vpsID: '', reason: '' })
  })
})
