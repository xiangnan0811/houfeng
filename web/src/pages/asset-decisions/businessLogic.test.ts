import { afterEach, describe, expect, it, vi } from 'vitest'

import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupSummary,
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupSummary,
  AssetDecisionOverview,
  AssetDecisionRecordDetail,
  AssetDecisionRecordSummary,
  SubscriptionRecord,
  VPSAssetRecord,
} from '../../lib/types'
import {
  buildAssetDecisionPageModel,
  buildDecisionQueue,
  buildManualGroupProgress,
  buildPortfolioLead,
  buildSecondaryNavItems,
  deriveClosedLoopMetrics,
  deriveNextWorkItems,
  filterDecisionQueue,
  hasCancellationAttention,
  updateDecisionQueues,
} from './businessLogic'
import {
  cancelVPS,
  decisionRecord,
  groupDetail,
  groupSummary,
  manualGroupDetail,
  manualGroupSummary,
  migrateVPS,
  overview,
  subscription,
  vps,
} from './testFixtures'
import type {
  ClosedLoopMetrics,
  DecisionQueueItem,
  ManualGroupsState,
  PortfolioState,
  QueueState,
  RecordsState,
  ScenarioTemplatesState,
} from './types'

function assetVPS(overrides: Partial<VPSAssetRecord> = {}): VPSAssetRecord {
  return { ...vps, ...overrides } as VPSAssetRecord
}

function assetSubscription(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return { ...subscription, ...overrides } as SubscriptionRecord
}

function automaticGroup(overrides: Record<string, unknown> = {}): AssetDecisionGroupSummary {
  return groupSummary(overrides) as AssetDecisionGroupSummary
}

function savedRecord(overrides: Record<string, unknown> = {}): AssetDecisionRecordSummary {
  return decisionRecord(overrides) as AssetDecisionRecordSummary
}

function manualSummary(overrides: Record<string, unknown> = {}): AssetDecisionManualGroupSummary {
  return manualGroupSummary(overrides) as AssetDecisionManualGroupSummary
}

function portfolioOverview(overrides: Record<string, unknown> = {}): AssetDecisionOverview {
  return overview(overrides) as AssetDecisionOverview
}

function queueItem(
  row: VPSAssetRecord,
  linkedSubscription: SubscriptionRecord | null = null,
): DecisionQueueItem {
  return {
    vps: row,
    subscription: linkedSubscription,
    qualityIssues: [],
    renewalDue: false,
    priority: 0,
  }
}

function emptyQueueState(overrides: Partial<QueueState> = {}): QueueState {
  return {
    renewalsLoading: false,
    renewalsError: null,
    queueLoading: false,
    queueError: null,
    renewals: [],
    subscriptions: [],
    unreviewed: [],
    cancel: [],
    ...overrides,
  }
}

function emptyPortfolioState(overrides: Partial<PortfolioState> = {}): PortfolioState {
  return {
    overviewLoading: false,
    overviewError: null,
    overview: null,
    groupsLoading: false,
    groupsError: null,
    groups: [],
    ...overrides,
  }
}

function emptyRecordsState(overrides: Partial<RecordsState> = {}): RecordsState {
  return { loading: false, error: null, records: [], ...overrides }
}

function emptyManualGroupsState(overrides: Partial<ManualGroupsState> = {}): ManualGroupsState {
  return { loading: false, error: null, groups: [], ...overrides }
}

function emptyTemplatesState(overrides: Partial<ScenarioTemplatesState> = {}): ScenarioTemplatesState {
  return { loading: false, error: null, templates: [], ...overrides }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('Asset Decisions decision queue model', () => {
  it('deduplicates, prioritizes, and filters the decision inventory', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-01T08:00:00Z'))

    const dueSubscription = assetSubscription({ renew_at: '2026-06-20', status: 'active' })
    const laterSubscription = assetSubscription({
      subscription_id: 'sub_later',
      vps_id: 'vps_migrate',
      renew_at: '2026-09-20',
      status: 'active',
    })
    const unreviewed = assetVPS()
    const missingFacts = assetVPS({
      vps_id: 'vps_missing',
      display_name: 'Missing Facts',
      renewal_decision: 'keep',
      active_monitoring_instance_link_count: 0,
    })
    const migrating = assetVPS({ ...migrateVPS } as Partial<VPSAssetRecord>)
    const rows = buildDecisionQueue(
      [unreviewed, missingFacts, migrating, { ...unreviewed, display_name: 'Tokyo Review Latest' }],
      new Map<string, SubscriptionRecord[]>([
        [unreviewed.vps_id, [dueSubscription]],
        [migrating.vps_id, [laterSubscription]],
      ]),
      30,
    )

    expect(rows.map((row) => row.vps.vps_id)).toEqual(['vps_review', 'vps_migrate', 'vps_missing'])
    const firstRow = rows[0]
    if (!firstRow) throw new Error('decision queue must include the reviewed VPS')
    expect(firstRow).toMatchObject({ renewalDue: true, qualityIssues: [] })
    expect(firstRow.vps.display_name).toBe('Tokyo Review Latest')
    expect(filterDecisionQueue(rows, 'renewal').map((row) => row.vps.vps_id)).toEqual(['vps_review'])
    expect(filterDecisionQueue(rows, 'unreviewed').map((row) => row.vps.vps_id)).toEqual(['vps_review', 'vps_migrate'])
    expect(filterDecisionQueue(rows, 'missing_subscription').map((row) => row.vps.vps_id)).toEqual(['vps_missing'])
    expect(filterDecisionQueue(rows, 'unlinked').map((row) => row.vps.vps_id)).toEqual(['vps_missing'])
  })

  it('does not count a no-renewal VPS as renewal due even inside the window', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-30T08:00:00Z'))

    const noRenewal = assetVPS({ vps_id: 'vps_no_renewal', renewal_decision: 'cancel', auto_renew_check: 'disabled' })
    const keep = assetVPS({ vps_id: 'vps_keep', renewal_decision: 'keep' })
    const rows = buildDecisionQueue(
      [noRenewal, keep],
      new Map<string, SubscriptionRecord[]>([
        [noRenewal.vps_id, [assetSubscription({ vps_id: noRenewal.vps_id, renew_at: '2026-10-02', status: 'active' })]],
        [keep.vps_id, [assetSubscription({ subscription_id: 'sub_keep', vps_id: keep.vps_id, renew_at: '2026-10-02', status: 'active' })]],
      ]),
      30,
    )

    expect(rows.find((row) => row.vps.vps_id === 'vps_no_renewal')?.renewalDue).toBe(false)
    expect(filterDecisionQueue(rows, 'renewal').map((row) => row.vps.vps_id)).toEqual(['vps_keep'])
    expect(rows[0]?.vps.vps_id).toBe('vps_keep')
  })

  it('uses provider verification independently of lifecycle and subscription', () => {
    const cancellationDecision = queueItem(assetVPS({ ...cancelVPS } as Partial<VPSAssetRecord>))
    const inactiveSubscription = queueItem(
      assetVPS({ vps_id: 'vps_inactive_subscription', renewal_decision: 'keep' }),
      assetSubscription({ vps_id: 'vps_inactive_subscription', status: 'cancelled' }),
    )
    const alreadyCancelling = queueItem(assetVPS({
      vps_id: 'vps_to_cancel',
      lifecycle_status: 'archived',
      renewal_decision: 'cancel',
      auto_renew_check: 'disabled',
    }))

    expect(hasCancellationAttention(cancellationDecision)).toBe(true)
    expect(hasCancellationAttention(inactiveSubscription)).toBe(false)
    expect(hasCancellationAttention(alreadyCancelling)).toBe(false)
    expect(filterDecisionQueue(
      [alreadyCancelling, inactiveSubscription, cancellationDecision],
      'cancellation_attention',
    )).toEqual([cancellationDecision])
  })

  it('moves an updated VPS between decision slices without mutating the source state', () => {
    const original = emptyQueueState({
      unreviewed: [assetVPS()],
      cancel: [assetVPS({ ...cancelVPS } as Partial<VPSAssetRecord>)],
    })
    const updated = assetVPS({ renewal_decision: 'cancel', display_name: 'Tokyo Cancelled' })

    const next = updateDecisionQueues(original, updated)

    expect(next.unreviewed).toEqual([])
    expect(next.cancel.map((row) => row.vps_id)).toEqual(['vps_review', 'vps_cancel'])
    expect(original.unreviewed.map((row) => row.vps_id)).toEqual(['vps_review'])
  })
})

describe('Asset Decisions portfolio model', () => {
  it('counts loaded closed-loop facts while marking partial sources explicitly', () => {
    const record = savedRecord({
      status: 'in_progress',
      followup_blocked_count: 2,
      execution_readback: {
        ...decisionRecord().execution_readback,
        status: 'drift',
        drift_count: 1,
        blocked_count: 1,
        needs_evidence_count: 2,
        open_count: 3,
      },
    })
    const metrics = deriveClosedLoopMetrics(
      [automaticGroup({ view: 'cost', group_type: 'cost_pressure' })],
      [record, savedRecord({ record_id: 'adr_completed', status: 'completed' })],
      [manualSummary(), manualSummary({ manual_group_id: 'admg_archived', status: 'archived' })],
      { records: '记录不可用', templates: '模板不可用' },
      portfolioOverview({ cost_group_count: 7, evidence_group_count: 8 }),
    )

    expect(metrics).toEqual({
      autoGroupCount: 1,
      manualActiveCount: 1,
      recordActiveCount: 1,
      readbackDriftCount: 1,
      readbackBlockedCount: 3,
      readbackNeedsEvidenceCount: 3,
      readbackOpenCount: 3,
      costPressureGroupCount: 7,
      evidenceGapGroupCount: 8,
      partialErrorCount: 2,
    })
  })

  it('sorts next work by risk and suppresses facts from failed owners', () => {
    const driftRecord = savedRecord({
      title: '事实漂移记录',
      execution_readback: {
        ...decisionRecord().execution_readback,
        status: 'drift',
        drift_count: 2,
      },
    })
    const group = automaticGroup({ title: '自动组合待办' })

    expect(deriveNextWorkItems([group], [driftRecord], {})[0]).toMatchObject({
      kind: 'record_drift',
      title: '事实漂移记录',
    })
    expect(deriveNextWorkItems([group], [driftRecord], { records: '记录读取失败' }))
      .toEqual([expect.objectContaining({ kind: 'auto_group', title: '自动组合待办' })])
    expect(deriveNextWorkItems([group], [driftRecord], { groups: '自动组读取失败' }))
      .toEqual([expect.objectContaining({ kind: 'record_drift', title: '事实漂移记录' })])
  })

  it('distinguishes a fully loaded scoped-empty portfolio from a partial evidence state', () => {
    const emptyMetrics: ClosedLoopMetrics = {
      autoGroupCount: 0,
      manualActiveCount: 0,
      recordActiveCount: 0,
      readbackDriftCount: 0,
      readbackBlockedCount: 0,
      readbackNeedsEvidenceCount: 0,
      readbackOpenCount: 0,
      costPressureGroupCount: 0,
      evidenceGapGroupCount: 0,
      partialErrorCount: 0,
    }

    const lead = buildPortfolioLead(
      'needs_decision',
      30,
      portfolioOverview({ group_count: 0, top_groups: [] }),
      [],
      [],
      emptyMetrics,
      [],
    )
    expect(lead).toMatchObject({
      kind: 'scoped-empty',
      tone: 'neutral',
      title: '当前视图暂无组合决策',
      contextLabel: '全局资产组合 · 需要决策 · 30 天续费窗口',
      riskLabel: '无待处理决策',
    })
    expect(lead.actionLabel).toBeUndefined()
    expect(lead.title).not.toContain('健康')
    expect(lead.title).not.toContain('稳定')
    expect(lead.riskLabel).not.toBe('执行无偏差')
    expect(lead.riskLabel).not.toBe('暂无异常')
    expect(lead.riskLabel).toBe('无待处理决策')

    const filteredLead = buildPortfolioLead(
      'needs_decision',
      30,
      portfolioOverview({ group_count: 0, top_groups: [] }),
      [],
      [],
      emptyMetrics,
      [{ key: 'provider_id', label: '服务商', value: 'pv_001' }],
    )
    expect(filteredLead.contextLabel).toBe('服务商 pv_001')
    expect(filteredLead.kind).toBe('scoped-empty')

    expect(buildPortfolioLead(
      'needs_decision',
      30,
      null,
      [],
      [],
      { ...emptyMetrics, partialErrorCount: 1 },
      [],
    )).toMatchObject({
      kind: 'work',
      tone: 'alert',
      title: '部分资产决策证据不可用',
      riskLabel: '部分数据未读到',
    })

    const loadingLead = buildPortfolioLead(
      'needs_decision',
      30,
      null,
      [],
      [],
      emptyMetrics,
      [],
      true,
    )
    expect(loadingLead).toMatchObject({
      kind: 'scoped-empty',
      tone: 'neutral',
      title: '正在评估组合决策…',
      summary: '正在汇总决策组、执行回读与场景事实，请稍候…',
      riskLabel: '读取中',
    })
    expect(loadingLead.title).not.toContain('暂无')
    expect(loadingLead.title).not.toContain('健康')
    expect(loadingLead.title).not.toContain('稳定')
  })

  it('guarantees secondary strip visible units and ensures loading/unavailable never collapse to zero', () => {
    const loadingItems = buildSecondaryNavItems(
      emptyRecordsState({ loading: true }),
      emptyManualGroupsState({ loading: true }),
      emptyTemplatesState({ loading: true }),
      emptyQueueState({ renewalsLoading: true, queueLoading: true }),
      0,
      0,
    )
    expect(loadingItems[0]).toMatchObject({ meta: '读取中', summary: '读取中', actionLabel: '查看' })
    expect(loadingItems[1]).toMatchObject({ meta: '读取中', summary: '读取中', actionLabel: '查看' })
    expect(loadingItems[2]).toMatchObject({ meta: '读取中', summary: '读取中', actionLabel: '查看续费' })
    expect(loadingItems[3]).toMatchObject({ meta: '读取中', summary: '读取中', actionLabel: '查看' })
    for (const item of loadingItems) {
      expect(item.meta).not.toContain('0')
      expect(item.summary).not.toContain('无临近项')
      expect(item.summary).not.toContain('暂无待处理')
    }

    const errorItems = buildSecondaryNavItems(
      emptyRecordsState({ error: 'fail' }),
      emptyManualGroupsState({ error: 'fail' }),
      emptyTemplatesState({ error: 'fail' }),
      emptyQueueState({ renewalsError: 'fail', queueError: 'fail' }),
      0,
      0,
    )
    expect(errorItems[0]).toMatchObject({ meta: '不可用', summary: '不可用', tone: 'alert' })
    expect(errorItems[1]).toMatchObject({ meta: '不可用', summary: '部分不可用', tone: 'alert' })
    expect(errorItems[2]).toMatchObject({ meta: '不可用', summary: '不可用', tone: 'alert' })
    expect(errorItems[3]).toMatchObject({ meta: '不可用', summary: '不可用', tone: 'alert' })
    for (const item of errorItems) {
      expect(item.meta).not.toContain('0')
      expect(item.summary).not.toContain('无临近项')
      expect(item.summary).not.toContain('暂无待处理')
    }

    const loadedItems = buildSecondaryNavItems(
      emptyRecordsState({ records: [savedRecord()] }),
      emptyManualGroupsState({ groups: [manualSummary()] }),
      emptyTemplatesState({ templates: [] }),
      emptyQueueState({ renewals: [assetSubscription()] }),
      1,
      4,
    )
    expect(loadedItems.map((item) => item.meta)).toEqual(['1 条', '模板 0 个 · 组合 1 组', '1 条', '1 / 4 台'])
  })
})

describe('Asset Decisions composed page model', () => {
  it('reports manual-group readiness without treating review intent or missing facts as complete', () => {
    const ready = manualGroupDetail() as AssetDecisionManualGroupDetail
    const blocked = {
      ...ready,
      title: '',
      goal: '',
      evidence_assessment: {
        ...ready.evidence_assessment,
        quality_tier: 'blocked',
        gap_signal_count: 2,
      },
      members: ready.members.map((member) => ({
        ...member,
        intended_action: 'review',
        current_fact_found: false,
      })),
    } as AssetDecisionManualGroupDetail

    expect(buildManualGroupProgress(ready)).toMatchObject({
      readinessLabel: '可保存决定',
      readyToRecord: true,
      doneCount: 5,
      totalCount: 5,
    })
    expect(buildManualGroupProgress(blocked)).toMatchObject({
      readinessLabel: '继续整理',
      readyToRecord: false,
      doneCount: 1,
      totalCount: 5,
    })
  })

  it('builds the VPS index, partial errors, and secondary navigation from their real owners', () => {
    const automaticDetail = groupDetail() as AssetDecisionGroupDetail
    const baseManualDetail = manualGroupDetail() as AssetDecisionManualGroupDetail
    const manualDetail = {
      ...baseManualDetail,
      members: baseManualDetail.members.map((member) => ({
        ...member,
        vps: member.vps
          ? { ...member.vps, display_name: 'Manual Current Fact' }
          : member.vps,
      })),
    } as AssetDecisionManualGroupDetail
    const recordDetail = decisionRecord() as AssetDecisionRecordDetail
    const model = buildAssetDecisionPageModel({
      portfolioView: 'needs_decision',
      renewalWindow: 30,
      portfolioState: emptyPortfolioState({
        overviewError: '概览失败',
        overview: portfolioOverview(),
        groupsError: '自动组失败',
        groups: [automaticGroup()],
      }),
      recordsState: emptyRecordsState({ error: '记录失败', records: [recordDetail] }),
      manualGroupsState: emptyManualGroupsState({ groups: [manualSummary()] }),
      templatesState: emptyTemplatesState({ error: '模板失败' }),
      queueState: emptyQueueState({
        renewals: [assetSubscription()],
        unreviewed: [assetVPS({ ...migrateVPS } as Partial<VPSAssetRecord>)],
        cancel: [assetVPS({ ...cancelVPS } as Partial<VPSAssetRecord>)],
      }),
      recordDetail,
      manualDetail,
      automaticDetail,
      vpsCatalogRows: [assetVPS({ vps_id: 'vps_catalog', display_name: 'Catalog VPS' })],
      contextFilterChips: [{ key: 'provider_id', label: '服务商', value: 'pv_001' }],
      visibleDecisionQueueCount: 2,
      totalDecisionQueue: 3,
    })

    expect([...model.vpsByID.keys()]).toEqual(expect.arrayContaining([
      'vps_catalog',
      'vps_migrate',
      'vps_cancel',
      'vps_primary',
      'vps_standby',
    ]))
    expect(model.vpsByID.get('vps_primary')?.display_name).toBe('Manual Current Fact')
    expect(model.selectedRecordAssessment?.quality_tier).toBe('usable')
    expect(model.manualGroupProgress?.readyToRecord).toBe(true)
    expect(model.closedLoopPartialErrors).toEqual(['组合概览', '自动组', '决策记录', '场景模板'])
    expect(model.secondaryNavItems.map((item) => item.value)).toEqual([
      'records',
      'scenarios',
      'renewals',
      'single_queue',
    ])
    expect(model.secondaryNavItems[0]).toMatchObject({ meta: '不可用', summary: '不可用', tone: 'alert' })
    expect(model.secondaryNavItems[1]).toMatchObject({ summary: '部分不可用', tone: 'alert' })
    expect(model.secondaryNavItems[2]).toMatchObject({ meta: '1 条', summary: '有临近项', tone: 'notice' })
    expect(model.secondaryNavItems[3]).toMatchObject({ meta: '2 / 3 台', summary: '可逐台处理', tone: 'notice' })
  })
})
