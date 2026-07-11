import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDecisionEvidenceAssessment,
  AssetDecisionGroupDetail,
  AssetDecisionGroupMember,
  AssetDecisionGroupSummary,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupMember,
  AssetDecisionRecordDetail,
  AssetDecisionRecordSummary,
  AssetDecisionSourceAvailability,
  VPSAssetRecord,
} from '../../../lib/types'
import { useAssetDecisionRecords } from './useAssetDecisionRecords'

function recordDetail(
  recordID = 'adr_001',
  overrides: Partial<AssetDecisionRecordDetail> = {},
): AssetDecisionRecordDetail {
  return {
    record_id: recordID,
    title: '德国主备取舍记录',
    goal: '保留主力并补齐备用证据',
    status: 'draft',
    source_type: 'auto_group',
    source_group_id: 'adg_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: 1,
    followup_todo_count: 1,
    followup_in_progress_count: 0,
    followup_blocked_count: 0,
    followup_done_count: 0,
    followup_skipped_count: 0,
    evidence_snapshot: {},
    execution_readback: {
      status: 'open',
      summary: '1 台 VPS 等待跟进',
      open_count: 1,
      aligned_count: 0,
      drift_count: 0,
      blocked_count: 0,
      needs_evidence_count: 0,
    },
    execution_plan: {
      summary: '1 台 VPS 等待执行',
      lane_counts: [{ lane: 'review', count: 1 }],
      actionable_count: 1,
      blocked_count: 0,
    },
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    decided_at: null,
    completed_at: null,
    members: [{
      record_id: recordID,
      vps_id: 'vps_001',
      display_name: 'Germany Primary',
      suggested_role: 'primary_candidate',
      decided_role: 'primary_candidate',
      suggested_action: 'keep',
      decided_action: 'keep',
      reason: '主力保留',
      followup_status: 'todo',
      followup_note: '',
      followup_updated_at: null,
      evidence_snapshot: {},
      execution_readback: {
        status: 'open',
        summary: '等待执行回读',
        issues: [],
        current_facts: {
          found: true,
          lifecycle_status: 'active',
          usage_status: 'in_use',
          renewal_decision: 'keep',
          active_subscription_count: 1,
          service_count: 0,
          domain_count: 0,
          target_count: 0,
          running_target_count: 0,
          monitoring_link_count: 0,
          running_monitoring_count: 0,
          abnormal_monitoring_count: 0,
          active_incident_count: 0,
          source_availability: {
            subscriptions: true,
            services: true,
            domains: true,
            monitoring: true,
            targets: true,
          },
        },
      },
      execution_plan: {
        lane: 'review',
        step_kind: 'review_record',
        tone: 'notice',
        summary: '复核当前记录',
        step_label: '复核记录',
        issue_count: 0,
        blocked: false,
        actionable: true,
      },
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-01T00:00:00Z',
    }],
    ...overrides,
  }
}

function recordSummary(detail: AssetDecisionRecordDetail): AssetDecisionRecordSummary {
  const { members, ...summary } = detail
  void members
  return summary
}

const SOURCE_AVAILABILITY: AssetDecisionSourceAvailability = {
  subscriptions: true,
  services: true,
  domains: true,
  monitoring: true,
  targets: true,
}

const ASSESSMENT: AssetDecisionEvidenceAssessment = {
  confidence_score: 80,
  pressure_score: 20,
  readiness_score: 70,
  quality_tier: 'strong',
  decision_bias: 'review',
  support_signal_count: 2,
  risk_signal_count: 0,
  gap_signal_count: 0,
  summary: '证据可用',
}

function vps(vpsID: string): VPSAssetRecord {
  return {
    vps_id: vpsID,
    display_name: vpsID === 'vps_001' ? 'Germany Primary' : 'Germany Standby',
    provider_id: 'pv_001',
    provider_name: 'Provider A',
    product_name: 'KVM',
    order_ref: 'order-001',
    country: 'DE',
    region: 'EU',
    city: 'Frankfurt',
    datacenter: 'FRA1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'kvm',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 1,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    archived_at: null,
  }
}

function groupMember(vpsID: string): AssetDecisionGroupMember {
  return {
    vps: vps(vpsID),
    primary_subscription: null,
    subscription_count: 0,
    active_subscription_count: 0,
    inactive_subscription_count: 0,
    service_count: 0,
    domain_count: 0,
    target_count: 0,
    running_target_count: 0,
    monitoring_link_count: 1,
    running_monitoring_count: 1,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    suggested_role: vpsID === 'vps_001' ? 'primary_candidate' : 'standby_candidate',
    suggested_action: vpsID === 'vps_001' ? 'keep' : 'review',
    evidence_chips: [],
    evidence_assessment: ASSESSMENT,
    renewal_within_window: true,
    source_availability: SOURCE_AVAILABILITY,
  }
}

function groupSummary(): AssetDecisionGroupSummary {
  return {
    group_id: 'adg_001',
    group_type: 'renewal_attention',
    view: 'renewal',
    title: '德国主备组合',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    priority: 1,
    member_count: 2,
    lifecycle_counts: { active: 2 },
    usage_counts: { in_use: 2 },
    renewal_decision_counts: { keep: 1, unreviewed: 1 },
    renewal_window_count: 2,
    unreviewed_count: 1,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 1,
    in_use_count: 2,
    service_count: 0,
    domain_count: 0,
    target_count: 0,
    running_target_count: 0,
    monitoring_link_count: 2,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [],
    monthly_cost_base: 100,
    yearly_cost_base: 1200,
    base_currency: 'CNY',
    evidence_chips: [],
    evidence_assessment: ASSESSMENT,
  }
}

function automaticDetail(): AssetDecisionGroupDetail {
  return {
    ...groupSummary(),
    members: [groupMember('vps_001'), groupMember('vps_002')],
  }
}

function manualMember(vpsID: string): AssetDecisionManualGroupMember {
  return {
    ...groupMember(vpsID),
    manual_group_id: 'admg_001',
    vps_id: vpsID,
    intended_role: 'standby_candidate',
    intended_action: 'review',
    reason: '人工复核',
    note: '',
    sort_order: 1,
    evidence_snapshot: {},
    current_fact_found: true,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  }
}

function manualDetail(): AssetDecisionManualGroupDetail {
  const summary = groupSummary()
  return {
    manual_group_id: 'admg_001',
    status: 'active',
    scenario: 'primary_standby',
    title: '人工德国主备组合',
    goal: '人工选择备用机',
    note: '人工复核',
    source_type: 'auto_group',
    source_group_id: summary.group_id,
    source_group_type: summary.group_type,
    source_view: summary.view,
    scope_key: summary.scope_key,
    scope_label: summary.scope_label,
    renew_within_days: 60,
    member_count: 1,
    lifecycle_counts: { active: 1 },
    usage_counts: { in_use: 1 },
    renewal_decision_counts: { keep: 1 },
    renewal_window_count: 1,
    unreviewed_count: 0,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 1,
    in_use_count: 1,
    service_count: 0,
    domain_count: 0,
    target_count: 0,
    running_target_count: 0,
    monitoring_link_count: 1,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [],
    monthly_cost_base: 50,
    yearly_cost_base: 600,
    base_currency: 'CNY',
    evidence_chips: [],
    evidence_assessment: ASSESSMENT,
    source_availability: SOURCE_AVAILABILITY,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    archived_at: null,
    members: [manualMember('vps_002')],
  }
}

function mockSuccessfulReads(detail = recordDetail()) {
  vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([recordSummary(detail)])
  vi.spyOn(api, 'getAssetDecisionRecord').mockResolvedValue(detail)
}

function deferred<T>() {
  let resolvePromise: (value: T) => void = () => undefined
  let rejectPromise: (reason?: unknown) => void = () => undefined
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve
    rejectPromise = reject
  })
  return { promise, resolve: resolvePromise, reject: rejectPromise }
}

describe('useAssetDecisionRecords', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the filtered record list and keyed detail independently', async () => {
    const detail = recordDetail()
    const filter = { view: 'renewal' as const, renew_within_days: 30 }
    const listRecords = vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([recordSummary(detail)])
    const getRecord = vi.spyOn(api, 'getAssetDecisionRecord').mockResolvedValue(detail)

    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))

    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    expect(listRecords).toHaveBeenCalledWith(filter)
    expect(getRecord).toHaveBeenCalledWith('adr_001')

    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))
    expect(result.current.state.list.records).toEqual([recordSummary(detail)])
    expect(result.current.state.patchStatus).toBe('draft')
    expect(result.current.state.followupDrafts).toEqual({
      vps_001: { status: 'todo', note: '' },
    })
  })

  it('keeps list/detail failures independent and never exposes a prior keyed detail', async () => {
    const firstRequest = deferred<AssetDecisionRecordDetail>()
    const secondRequest = deferred<AssetDecisionRecordDetail>()
    vi.spyOn(api, 'listAssetDecisionRecords').mockRejectedValue(new Error('records offline'))
    vi.spyOn(api, 'getAssetDecisionRecord')
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(secondRequest.promise)
    const filter = {}
    const { result, rerender } = renderHook(
      ({ selectedRecordID }) => useAssetDecisionRecords({
        filter,
        selectedRecordID,
        revision: 0,
        onNotice: vi.fn(),
      }),
      { initialProps: { selectedRecordID: 'adr_001' } },
    )

    await waitFor(() => expect(result.current.state.list.error).toBe('records offline'))
    rerender({ selectedRecordID: 'adr_002' })
    expect(result.current.state.detail).toEqual({ loading: true, error: null, detail: null })
    expect(result.current.state.followupDrafts).toEqual({})

    const secondDetail = recordDetail('adr_002', { title: '第二条记录' })
    act(() => secondRequest.resolve(secondDetail))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(secondDetail))
    await act(async () => {
      firstRequest.resolve(recordDetail())
      await firstRequest.promise
    })
    expect(result.current.state.detail.detail).toBe(secondDetail)
  })

  it('keeps a detail failure separate from a successful record list', async () => {
    const detail = recordDetail()
    const filter = {}
    vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([recordSummary(detail)])
    vi.spyOn(api, 'getAssetDecisionRecord').mockRejectedValue(new Error('detail offline'))
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))

    await waitFor(() => expect(result.current.state.detail.error).toBe('detail offline'))

    expect(result.current.state.list.records).toEqual([recordSummary(detail)])
  })

  it('reloads the list and selected detail on an external revision', async () => {
    mockSuccessfulReads()
    const listRecords = vi.mocked(api.listAssetDecisionRecords)
    const getRecord = vi.mocked(api.getAssetDecisionRecord)
    const filter = {}
    const { result, rerender } = renderHook(
      ({ revision }) => useAssetDecisionRecords({
        filter,
        selectedRecordID: 'adr_001',
        revision,
        onNotice: vi.fn(),
      }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    rerender({ revision: 1 })
    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    await waitFor(() => expect(getRecord).toHaveBeenCalledTimes(2))
    expect(listRecords).toHaveBeenCalledTimes(2)
  })

  it('resets detail UI without reloading the selected record', async () => {
    mockSuccessfulReads()
    const getRecord = vi.mocked(api.getAssetDecisionRecord)
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    act(() => result.current.commands.selectPanel('members'))
    expect(result.current.state.detailPanel).toBe('members')
    act(() => result.current.commands.resetDetailUI())

    expect(result.current.state.detailPanel).toBe('overview')
    expect(getRecord).toHaveBeenCalledOnce()
  })

  it('constructs and preserves keyed automatic/manual drafts without exposing setters', async () => {
    mockSuccessfulReads()
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: null,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))

    act(() => result.current.commands.startFromAutomatic(automaticDetail(), 30))
    expect(result.current.state.draft).toMatchObject({
      sourceType: 'auto_group',
      sourceGroupID: 'adg_001',
      renewWithinDays: 30,
      title: '德国主备组合',
      memberOrder: ['vps_001', 'vps_002'],
    })

    act(() => {
      result.current.commands.updateDraft({ title: '保留的标题' })
      result.current.commands.updateDraftMember('vps_001', { reason: '保留的理由' })
      result.current.commands.editDraftMember('vps_001')
      result.current.commands.startFromAutomatic(automaticDetail(), 60)
    })
    expect(result.current.state.draft).toMatchObject({
      title: '保留的标题',
      renewWithinDays: 60,
      members: { vps_001: { reason: '保留的理由' } },
    })
    expect(result.current.state.draftEditingMemberID).toBe('vps_001')

    act(() => result.current.commands.startFromManual(manualDetail()))
    expect(result.current.state.draft).toMatchObject({
      sourceType: 'manual_group',
      sourceGroupID: 'admg_001',
      renewWithinDays: 60,
      title: '人工德国主备组合',
      goal: '人工选择备用机',
      members: {
        vps_002: {
          decidedRole: 'standby_candidate',
          decidedAction: 'review',
          reason: '人工复核',
        },
      },
    })
    expect(result.current.state.draftEditingMemberID).toBeNull()
    expect(Object.keys(result.current)).toEqual(['state', 'commands'])
  })

  it('keeps draft and detail commands inert when their owning state is absent', async () => {
    const filter = {}
    vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([])
    const createRecord = vi.spyOn(api, 'createAssetDecisionRecord')
    const patchRecord = vi.spyOn(api, 'patchAssetDecisionRecord')
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: null,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))
    const member = recordDetail().members[0]!

    act(() => {
      result.current.commands.updateDraft({ title: '不会保存' })
      result.current.commands.updateDraftMember('missing-vps', { reason: '不会保存' })
      result.current.commands.setPatchStatus('decided')
      result.current.commands.updateFollowupDraft(member.vps_id, { note: '不会保存' })
      result.current.commands.selectPanel('members')
    })
    let saved: AssetDecisionRecordDetail | null = recordDetail()
    await act(async () => {
      saved = await result.current.commands.saveDraft()
      await result.current.commands.patchStatus()
      await result.current.commands.saveFollowup(member)
    })

    expect(saved).toBeNull()
    expect(result.current.state.draft).toBeNull()
    expect(result.current.state.detailPanel).toBe('overview')
    expect(result.current.state.patchStatus).toBe('draft')
    expect(result.current.state.followupDrafts).toEqual({})
    expect(createRecord).not.toHaveBeenCalled()
    expect(patchRecord).not.toHaveBeenCalled()
  })

  it('owns keyed detail UI commands while the selected detail is still loading', async () => {
    const detailRequest = deferred<AssetDecisionRecordDetail>()
    const filter = {}
    vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([])
    vi.spyOn(api, 'getAssetDecisionRecord').mockReturnValue(detailRequest.promise)
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_waiting',
      revision: 0,
      onNotice: vi.fn(),
    }))

    act(() => {
      result.current.commands.setPatchStatus('decided')
      result.current.commands.updateFollowupDraft('vps_waiting', { note: '等待详情' })
      result.current.commands.selectPanel('members')
    })

    expect(result.current.state.patchStatus).toBe('decided')
    expect(result.current.state.followupDrafts.vps_waiting).toEqual({
      status: 'todo',
      note: '等待详情',
    })
    expect(result.current.state.detailPanel).toBe('members')
  })

  it('validates and saves the exact record POST, then locally merges the returned detail', async () => {
    mockSuccessfulReads()
    const created = recordDetail('adr_created', {
      title: '批准后的标题',
      goal: '批准后的目标',
      status: 'decided',
      updated_at: '2026-07-02T00:00:00Z',
    })
    const createRecord = vi.spyOn(api, 'createAssetDecisionRecord').mockResolvedValue(created)
    const updateVPS = vi.spyOn(api, 'updateVPSAsset')
    const notice = vi.fn()
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: null,
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.list.loading).toBe(false))

    act(() => result.current.commands.startFromAutomatic(automaticDetail(), 30))
    act(() => result.current.commands.updateDraft({ title: '   ' }))
    let returned: AssetDecisionRecordDetail | null = created
    await act(async () => {
      returned = await result.current.commands.saveDraft()
    })
    expect(returned).toBeNull()
    expect(result.current.state.saveError).toBe('请填写决策记录标题')
    expect(createRecord).not.toHaveBeenCalled()

    act(() => {
      result.current.commands.updateDraft({
        title: '  批准后的标题  ',
        goal: '  批准后的目标  ',
        status: 'decided',
      })
      result.current.commands.updateDraftMember('vps_001', {
        decidedAction: 'migrate',
        reason: '  成本过高  ',
      })
    })
    await act(async () => {
      returned = await result.current.commands.saveDraft()
    })

    expect(returned).toBe(created)
    expect(createRecord).toHaveBeenCalledWith({
      source_type: 'auto_group',
      source_group_id: 'adg_001',
      renew_within_days: 30,
      title: '批准后的标题',
      goal: '批准后的目标',
      status: 'decided',
      members: [
        {
          vps_id: 'vps_001',
          decided_role: 'primary_candidate',
          decided_action: 'migrate',
          reason: '成本过高',
        },
        {
          vps_id: 'vps_002',
          decided_role: 'standby_candidate',
          decided_action: 'review',
          reason: '',
        },
      ],
    })
    expect(result.current.state.draft).toBeNull()
    expect(result.current.state.list.records[0]?.record_id).toBe('adr_created')
    expect(result.current.state.detail.detail).toBeNull()
    expect(notice).toHaveBeenCalledWith('已保存组合决策记录：批准后的标题')
    expect(updateVPS).not.toHaveBeenCalled()
  })

  it('can save a draft before the background record list settles', async () => {
    const listRequest = deferred<AssetDecisionRecordSummary[]>()
    const filter = {}
    vi.spyOn(api, 'listAssetDecisionRecords').mockReturnValue(listRequest.promise)
    const created = recordDetail('adr_early', { title: '先保存的记录' })
    vi.spyOn(api, 'createAssetDecisionRecord').mockResolvedValue(created)
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: null,
      revision: 0,
      onNotice: vi.fn(),
    }))

    act(() => result.current.commands.startFromAutomatic(automaticDetail(), 30))
    await act(async () => result.current.commands.saveDraft())

    expect(result.current.state.list.records[0]?.record_id).toBe('adr_early')
  })

  it('patches record status and updates the keyed detail and list summary locally', async () => {
    const detail = recordDetail()
    mockSuccessfulReads(detail)
    const updated = recordDetail('adr_001', {
      status: 'in_progress',
      updated_at: '2026-07-02T00:00:00Z',
    })
    const patchRecord = vi.spyOn(api, 'patchAssetDecisionRecord').mockResolvedValue(updated)
    const notice = vi.fn()
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.setPatchStatus('in_progress'))
    await act(async () => result.current.commands.patchStatus())

    expect(patchRecord).toHaveBeenCalledWith('adr_001', { status: 'in_progress' })
    expect(result.current.state.detail.detail).toBe(updated)
    expect(result.current.state.list.records[0]?.status).toBe('in_progress')
    expect(result.current.state.patchStatus).toBe('in_progress')
    expect(notice).toHaveBeenCalledWith('决策记录状态已更新：德国主备取舍记录 -> 推进中')
  })

  it('appends a selected detail missing from the list and later updates it without dropping siblings', async () => {
    const detail = recordDetail()
    const sibling = recordDetail('adr_sibling', { title: '另一条记录' })
    const filter = {}
    vi.spyOn(api, 'listAssetDecisionRecords').mockResolvedValue([recordSummary(sibling)])
    vi.spyOn(api, 'getAssetDecisionRecord').mockResolvedValue(detail)
    const firstUpdate = recordDetail('adr_001', { status: 'in_progress' })
    const secondUpdate = recordDetail('adr_001', { status: 'decided' })
    vi.spyOn(api, 'patchAssetDecisionRecord')
      .mockResolvedValueOnce(firstUpdate)
      .mockResolvedValueOnce(secondUpdate)
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.setPatchStatus('in_progress'))
    await act(async () => result.current.commands.patchStatus())
    expect(result.current.state.list.records.map((record) => record.record_id)).toEqual([
      'adr_001',
      'adr_sibling',
    ])

    act(() => result.current.commands.setPatchStatus('decided'))
    await act(async () => result.current.commands.patchStatus())
    expect(result.current.state.list.records).toEqual([
      recordSummary(secondUpdate),
      recordSummary(sibling),
    ])
  })

  it('updates a loaded detail before its background list has settled', async () => {
    const listRequest = deferred<AssetDecisionRecordSummary[]>()
    const filter = {}
    const detail = recordDetail()
    const updated = recordDetail('adr_001', { status: 'in_progress' })
    vi.spyOn(api, 'listAssetDecisionRecords').mockReturnValue(listRequest.promise)
    vi.spyOn(api, 'getAssetDecisionRecord').mockResolvedValue(detail)
    vi.spyOn(api, 'patchAssetDecisionRecord').mockResolvedValue(updated)
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.setPatchStatus('in_progress'))
    await act(async () => result.current.commands.patchStatus())

    expect(result.current.state.list.records).toEqual([recordSummary(updated)])
  })

  it('rebuilds each keyed detail editor after its local UI state is reset', async () => {
    const filter = {}
    const detail = recordDetail()
    mockSuccessfulReads(detail)
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => result.current.commands.resetDetailUI())
    act(() => result.current.commands.updateFollowupDraft('vps_001', { note: '重建跟进草稿' }))
    expect(result.current.state.followupDrafts.vps_001?.note).toBe('重建跟进草稿')

    act(() => result.current.commands.resetDetailUI())
    act(() => result.current.commands.selectPanel('members'))
    expect(result.current.state.detailPanel).toBe('members')
  })

  it('saves one member follow-up with an explicit empty note and synchronizes returned counters', async () => {
    const detail = recordDetail()
    mockSuccessfulReads(detail)
    const member = detail.members[0]!
    const updatedMember = {
      ...member,
      followup_status: 'blocked' as const,
      followup_note: '',
      followup_updated_at: '2026-07-02T00:00:00Z',
    }
    const updated = recordDetail('adr_001', {
      followup_todo_count: 0,
      followup_blocked_count: 1,
      members: [updatedMember],
    })
    const patchRequest = deferred<AssetDecisionRecordDetail>()
    const patchRecord = vi.spyOn(api, 'patchAssetDecisionRecord').mockReturnValue(patchRequest.promise)
    const notice = vi.fn()
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    act(() => {
      result.current.commands.updateFollowupDraft('vps_001', { note: '' })
      result.current.commands.editFollowupMember('vps_001')
    })
    let savePromise: Promise<void> | undefined
    act(() => {
      savePromise = result.current.commands.saveFollowup(member, 'blocked')
    })
    expect(result.current.state.followupSaving).toEqual({ vps_001: true })
    expect(result.current.state.followupEditingMemberID).toBe('vps_001')
    expect(patchRecord).toHaveBeenCalledWith('adr_001', {
      members: [{
        vps_id: 'vps_001',
        followup_status: 'blocked',
        followup_note: '',
      }],
    })

    await act(async () => {
      patchRequest.resolve(updated)
      await savePromise
    })
    expect(result.current.state.followupSaving).toEqual({ vps_001: false })
    expect(result.current.state.followupErrors.vps_001).toBeNull()
    expect(result.current.state.followupDrafts.vps_001).toEqual({ status: 'blocked', note: '' })
    expect(result.current.state.list.records[0]).toMatchObject({
      followup_todo_count: 0,
      followup_blocked_count: 1,
    })
    expect(notice).toHaveBeenCalledWith('成员跟进已更新：Germany Primary -> 阻塞')
  })

  it('falls back to member facts when no keyed follow-up draft exists', async () => {
    const detail = recordDetail()
    const filter = {}
    mockSuccessfulReads(detail)
    vi.spyOn(api, 'patchAssetDecisionRecord').mockResolvedValue(detail)
    const notice = vi.fn()
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))
    const syntheticMember = {
      ...detail.members[0]!,
      vps_id: 'vps_missing_draft',
      display_name: '',
      followup_status: 'in_progress' as const,
      followup_note: '  继续核对  ',
    }

    await act(async () => result.current.commands.saveFollowup(syntheticMember))

    expect(api.patchAssetDecisionRecord).toHaveBeenCalledWith('adr_001', {
      members: [{
        vps_id: 'vps_missing_draft',
        followup_status: 'in_progress',
        followup_note: '继续核对',
      }],
    })
    expect(notice).toHaveBeenCalledWith('成员跟进已更新：vps_missing_draft -> 处理中')
  })

  it('retains the current detail and assigns a follow-up failure to that member', async () => {
    const detail = recordDetail()
    mockSuccessfulReads(detail)
    vi.spyOn(api, 'patchAssetDecisionRecord').mockRejectedValue(new Error('offline'))
    const filter = {}
    const { result } = renderHook(() => useAssetDecisionRecords({
      filter,
      selectedRecordID: 'adr_001',
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    await act(async () => result.current.commands.saveFollowup(detail.members[0]!))

    expect(result.current.state.detail.detail).toBe(detail)
    expect(result.current.state.followupErrors.vps_001).toBe('offline')
    expect(result.current.state.patchError).toBe('offline')
    expect(result.current.state.followupSaving.vps_001).toBe(false)
  })
})
