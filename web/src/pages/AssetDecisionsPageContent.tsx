import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import {
  type BadgeTone,
} from '../components/atoms'
import { PortfolioWorkbench } from './asset-decisions/components/PortfolioWorkbench'
import { SecondaryWorkbenches } from './asset-decisions/components/SecondaryWorkbenches'
import { useAssetDecisionRouteState } from './asset-decisions/hooks/useAssetDecisionRouteState'
import {
  Badge,
  type DataTableColumn,
} from '../components/atoms'
import { GroupDetailModal } from './asset-decisions/modals/GroupDetailModal'
import { ManualGroupDetailModal } from './asset-decisions/modals/ManualGroupDetailModal'
import { TemplateDetailModal } from './asset-decisions/modals/TemplateDetailModal'
import { RecordDetailModal } from './asset-decisions/modals/RecordDetailModal'
import { RenewalDecisionModal } from './asset-decisions/modals/RenewalDecisionModal'
import {
  addAssetDecisionManualGroupMember,
  createAssetDecisionScenarioTemplate,
  createAssetDecisionManualGroup,
  createManualGroupFromScenarioTemplate,
  createAssetDecisionRecord,
  deleteAssetDecisionManualGroupMember,
  getAssetDecisionGroup,
  getAssetDecisionManualGroup,
  getAssetDecisionOverview,
  getAssetDecisionRecord,
  getAssetDecisionScenarioTemplate,
  listAssetDecisionGroups,
  listAssetDecisionManualGroups,
  listAssetDecisionRecords,
  listAssetDecisionScenarioTemplates,
  listSubscriptions,
  listVPSAssets,
  patchAssetDecisionManualGroup,
  patchAssetDecisionRecord,
  patchAssetDecisionScenarioTemplate,
  updateVPSAsset,
} from '../lib/api'
import { formatDateTime, formatOptional } from '../lib/format'
import {
  type AssetDecisionEvidenceChip,
  type AssetDecisionEvidenceAssessment,
  type AssetDecisionEvidenceDecisionBias,
  type AssetDecisionEvidenceQualityTier,
  type AssetDecisionEvidenceSnapshot,
  type AssetDecisionGroupDetail,
  type AssetDecisionGroupMember,
  type AssetDecisionGroupSummary,
  type AssetDecisionManualGroupDetail,
  type AssetDecisionManualGroupMember,
  type AssetDecisionManualGroupScenario,
  type AssetDecisionManualGroupStatus,
  type AssetDecisionManualGroupSummary,
  type AssetDecisionRecordDetail,
  type AssetDecisionFollowupStatus,
  type AssetDecisionGroupListFilter,
  type AssetDecisionRecordMember,
  type AssetDecisionRecordStatus,
  type AssetDecisionRecordSummary,
  type AssetDecisionScenarioTemplateDetail,
  type AssetDecisionScenarioTemplateStatus,
  type AssetDecisionScenarioTemplateSummary,
  type AssetDecisionSuggestedAction,
  type AssetDecisionSuggestedRole,
  type SubscriptionRecord,
  type VPSAssetRecord,
} from '../lib/types'
import {
  LifecycleBadge,
  RenewalBadge,
  UsageBadge,
} from './assetPageBadges'
import {
  groupSubscriptionsByVPS,
  vpsLocationLabel,
} from './assetPageUtils'
import type {
  MainWorkbenchView,
  DecisionQueueView,
  DecisionQueueItem,
  PortfolioState,
  DetailState,
  ManualGroupsState,
  ManualDetailState,
  ScenarioTemplatesState,
  TemplateDetailState,
  VPSCatalogState,
  RecordsState,
  RecordDetailState,
  RecordMemberDraft,
  RecordFollowupDraft,
  RecordDraft,
  ManualMemberAddDraft,
  TemplateManualGroupDraft,
  FormSubmitEvent,
  GroupDetailPanel,
  ManualDetailPanel,
  RecordDetailPanel,
  TemplateDetailPanel,
  ContextFilterKey,
  OpenStateKey,
  AssetDecisionSecondaryNavItem,
} from './asset-decisions/types'
import {
  INITIAL_DECISION_DRAFT,
  INITIAL_PORTFOLIO_STATE,
  INITIAL_DETAIL_STATE,
  INITIAL_MANUAL_GROUPS_STATE,
  INITIAL_MANUAL_DETAIL_STATE,
  INITIAL_SCENARIO_TEMPLATES_STATE,
  INITIAL_TEMPLATE_DETAIL_STATE,
  INITIAL_VPS_CATALOG_STATE,
  INITIAL_RECORDS_STATE,
  INITIAL_RECORD_DETAIL_STATE,
  INITIAL_QUEUE_STATE,
  ROLE_LABELS,
  ACTION_LABELS,
  SCENARIO_TEMPLATE_STATUS_LABELS,
  RECORD_STATUS_LABELS,
  FOLLOWUP_STATUS_LABELS,
  ROLE_OPTIONS,
  ACTION_OPTIONS,
  FOLLOWUP_STATUS_OPTIONS,
  EXECUTION_PLAN_LANE_ORDER,
} from './asset-decisions/constants'
import { vpsDetailPath, vpsWorkbenchPath } from './asset-decisions/paths'
import {
  completeRecordDraftFromGroupDetail,
  completeRecordDraftFromManualDetail,
} from './asset-decisions/recordDrafts'
import {
  renderReadbackBadge,
  renderExecutionPlanBadge,
  renderEvidenceAssessment,
  renderMemberExecutionPlan,
  previewItems,
} from './asset-decisions/renderHelpers'
import {
  buildDecisionQueue,
  updateDecisionQueues,
  deriveClosedLoopMetrics,
  deriveNextWorkItems,
  buildPortfolioLead,
  buildManualGroupProgress,
} from './asset-decisions/businessLogic'
import {
  describeError,
  parseRenewalWindow,
} from './asset-decisions/utils'
import {
  chipTone,
  roleTone,
  actionTone,
  followupStatusTone,
  renewalQueueLabel,
  compactDecisionText,
  compactMemberReadbackSummary,
  compactMemberPlanSummary,
  currentFactsLabel,
  currentFactsStateLabel,
  ipQualityServiceLabel,
  actionLabelForMember,
} from './asset-decisions/formatters'

// 页面级状态类型
type QueueState = {
  renewalsLoading: boolean
  renewalsError: string | null
  queueLoading: boolean
  queueError: string | null
  renewals: SubscriptionRecord[]
  subscriptions: SubscriptionRecord[]
  unreviewed: VPSAssetRecord[]
  migrate: VPSAssetRecord[]
  cancel: VPSAssetRecord[]
}

type ClosedLoopSourceErrors = {
  overview?: string | null
  groups?: string | null
  records?: string | null
  manualGroups?: string | null
  templates?: string | null
}

type AssetDecisionNextWorkKind =
  | 'record_drift'
  | 'record_blocked'
  | 'record_needs_evidence'
  | 'auto_group'
  | 'manual_group'
  | 'scenario_template'

type AssetDecisionNextWorkTarget =
  | { type: 'record'; id: string }
  | { type: 'group'; id: string }
  | { type: 'manual_group'; id: string }
  | { type: 'template'; id: string }

type AssetDecisionNextWorkItem = {
  id: string
  kind: AssetDecisionNextWorkKind
  tone: BadgeTone
  sourceLabel: string
  kindLabel: string
  title: string
  summary: string
  meta: string
  actionLabel: string
  priority: number
  target: AssetDecisionNextWorkTarget
}

function filterDecisionQueue(
  rows: DecisionQueueItem[],
  view: DecisionQueueView,
): DecisionQueueItem[] {
  if (view === 'all') return rows
  if (view === 'renewal') return rows.filter((row) => row.renewalDue)
  if (view === 'unlinked') return rows.filter((row) => row.vps.active_monitoring_instance_link_count <= 0)
  if (view === 'missing_subscription') return rows.filter((row) => !row.subscription)
  if (view === 'cancellation_attention') return rows.filter((row) => hasCancellationAttention(row))
  return rows.filter((row) => row.vps.renewal_decision === view)
}

function hasCancellationAttention(row: DecisionQueueItem): boolean {
  if (row.vps.renewal_decision === 'cancel' && row.vps.lifecycle_status !== 'to_cancel' && row.vps.lifecycle_status !== 'cancelled') {
    return true
  }
  if (!row.subscription) return false
  const inactiveSubscription = row.subscription.status !== 'active'
  const vpsCancelled = row.vps.lifecycle_status === 'to_cancel' || row.vps.lifecycle_status === 'cancelled'
  return inactiveSubscription && !vpsCancelled
}

function subscriptionCostAttention(subscription: SubscriptionRecord | null): boolean {
  return Boolean(subscription?.exchange_rate_stale)
}

function buildSecondaryNavItems(
  recordsState: RecordsState,
  manualGroupsState: ManualGroupsState,
  templatesState: ScenarioTemplatesState,
  queueState: QueueState,
  visibleDecisionQueueCount: number,
  totalDecisionQueue: number,
): AssetDecisionSecondaryNavItem[] {
  const recordMeta = recordsState.loading
    ? '读取中'
    : recordsState.error
      ? '不可用'
      : `${recordsState.records.length} 条`
  const recordIssues = recordsState.records.reduce((count, record) => (
    count + (record.followup_blocked_count ?? 0) + (record.execution_readback?.needs_evidence_count ?? 0)
  ), 0)
  const scenarioMeta = [
    templatesState.loading ? '模板 ...' : templatesState.error ? '模板不可用' : `模板 ${templatesState.templates.length}`,
    manualGroupsState.loading ? '组合 ...' : manualGroupsState.error ? '组合不可用' : `组合 ${manualGroupsState.groups.length}`,
  ].join(' · ')
  const renewalMeta = queueState.renewalsLoading
    ? '读取中'
    : queueState.renewalsError
      ? '不可用'
      : `${queueState.renewals.length} 条`
  const singleQueueMeta = queueState.queueLoading
    ? '读取中'
    : queueState.queueError
      ? '不可用'
      : `${visibleDecisionQueueCount} / ${totalDecisionQueue}`

  return [
    {
      value: 'records',
      eyebrow: '历史记录',
      title: '保存记录',
      summary: recordIssues > 0 ? `待复核 ${recordIssues}` : '可回看',
      meta: recordMeta,
      actionLabel: '打开记录',
      tone: recordsState.error ? 'alert' : recordIssues > 0 ? 'notice' : 'normal',
    },
    {
      value: 'scenarios',
      eyebrow: '场景',
      title: '场景与组合',
      summary: manualGroupsState.error || templatesState.error ? '部分不可用' : '按需打开',
      meta: scenarioMeta,
      actionLabel: '打开场景',
      tone: manualGroupsState.error || templatesState.error ? 'alert' : 'normal',
    },
    {
      value: 'renewals',
      eyebrow: '续费事实',
      title: '续费窗口',
      summary: queueState.renewals.length > 0 ? '有临近项' : '无临近项',
      meta: renewalMeta,
      actionLabel: '查看续费',
      tone: queueState.renewalsError ? 'alert' : queueState.renewals.length > 0 ? 'notice' : 'normal',
    },
    {
      value: 'single_queue',
      eyebrow: '单台辅助',
      title: '单台队列',
      summary: totalDecisionQueue > 0 ? '可逐台处理' : '暂无待处理',
      meta: singleQueueMeta,
      actionLabel: '查看单台队列',
      tone: queueState.queueError ? 'alert' : totalDecisionQueue > 0 ? 'notice' : 'normal',
    },
  ]
}

function buildRecordFollowupDrafts(detail: AssetDecisionRecordDetail | null): Record<string, RecordFollowupDraft> {
  const drafts: Record<string, RecordFollowupDraft> = {}
  for (const member of detail?.members ?? []) {
    drafts[member.vps_id] = {
      status: member.followup_status,
      note: member.followup_note,
    }
  }
  return drafts
}

function parseEvidenceAssessment(snapshot?: AssetDecisionEvidenceSnapshot | null): AssetDecisionEvidenceAssessment | null {
  const raw = snapshot?.evidence_assessment
  if (!raw || typeof raw !== 'object') return null
  const candidate = raw as Partial<AssetDecisionEvidenceAssessment>
  if (
    typeof candidate.confidence_score !== 'number' ||
    typeof candidate.pressure_score !== 'number' ||
    typeof candidate.readiness_score !== 'number' ||
    typeof candidate.quality_tier !== 'string' ||
    typeof candidate.decision_bias !== 'string'
  ) {
    return null
  }
  return {
    confidence_score: candidate.confidence_score,
    pressure_score: candidate.pressure_score,
    readiness_score: candidate.readiness_score,
    quality_tier: candidate.quality_tier as AssetDecisionEvidenceQualityTier,
    decision_bias: candidate.decision_bias as AssetDecisionEvidenceDecisionBias,
    support_signal_count: typeof candidate.support_signal_count === 'number' ? candidate.support_signal_count : 0,
    risk_signal_count: typeof candidate.risk_signal_count === 'number' ? candidate.risk_signal_count : 0,
    gap_signal_count: typeof candidate.gap_signal_count === 'number' ? candidate.gap_signal_count : 0,
    summary: typeof candidate.summary === 'string' ? candidate.summary : '证据评估快照',
  }
}

function manualGroupSummaryFromDetail(detail: AssetDecisionManualGroupDetail): AssetDecisionManualGroupSummary {
  const summary: AssetDecisionManualGroupSummary = {
    manual_group_id: detail.manual_group_id,
    status: detail.status,
    scenario: detail.scenario,
    title: detail.title,
    goal: detail.goal,
    note: detail.note,
    source_type: detail.source_type,
    source_group_id: detail.source_group_id,
    source_group_type: detail.source_group_type,
    source_view: detail.source_view,
    scope_key: detail.scope_key,
    scope_label: detail.scope_label,
    renew_within_days: detail.renew_within_days,
    member_count: detail.member_count,
    lifecycle_counts: detail.lifecycle_counts,
    usage_counts: detail.usage_counts,
    renewal_decision_counts: detail.renewal_decision_counts,
    renewal_window_count: detail.renewal_window_count,
    unreviewed_count: detail.unreviewed_count,
    migrate_count: detail.migrate_count,
    cancel_count: detail.cancel_count,
    cancellation_attention_count: detail.cancellation_attention_count,
    idle_count: detail.idle_count,
    standby_count: detail.standby_count,
    in_use_count: detail.in_use_count,
    service_count: detail.service_count,
    domain_count: detail.domain_count,
    target_count: detail.target_count,
    running_target_count: detail.running_target_count,
    monitoring_link_count: detail.monitoring_link_count,
    abnormal_monitoring_count: detail.abnormal_monitoring_count,
    active_incident_count: detail.active_incident_count,
    primary_issue_summary: detail.primary_issue_summary,
    monthly_cost_by_currency: detail.monthly_cost_by_currency,
    monthly_cost_base: detail.monthly_cost_base,
    yearly_cost_base: detail.yearly_cost_base,
    base_currency: detail.base_currency,
    evidence_chips: detail.evidence_chips,
    evidence_assessment: detail.evidence_assessment,
    decision_recommendation: detail.decision_recommendation,
    comparison_insight: detail.comparison_insight,
    source_availability: detail.source_availability,
    created_at: detail.created_at,
    updated_at: detail.updated_at,
    archived_at: detail.archived_at,
  }
  return summary
}

function mergeManualGroupSummaries(
  rows: AssetDecisionManualGroupSummary[],
  additions: AssetDecisionManualGroupSummary[],
): AssetDecisionManualGroupSummary[] {
  let next = [...rows]
  for (const summary of additions) {
    next = [summary, ...next.filter((row) => row.manual_group_id !== summary.manual_group_id)]
  }
  return next.sort((left, right) => {
    if (left.status !== right.status) return left.status === 'active' ? -1 : 1
    return right.updated_at.localeCompare(left.updated_at)
  })
}

function assetDecisionFilterKey(filter: AssetDecisionGroupListFilter): string {
  return [
    filter.view ?? '',
    filter.renew_within_days ?? '',
    filter.provider_id ?? '',
    filter.vps_id ?? '',
    filter.country ?? '',
    filter.region ?? '',
    filter.city ?? '',
    filter.scenario ?? '',
  ].join('|')
}

function scenarioTemplateSummaryFromDetail(detail: AssetDecisionScenarioTemplateDetail): AssetDecisionScenarioTemplateSummary {
  return {
    template_id: detail.template_id,
    builtin: detail.builtin,
    status: detail.status,
    scenario: detail.scenario,
    title: detail.title,
    goal: detail.goal,
    note: detail.note,
    source_manual_group_id: detail.source_manual_group_id,
    member_count: detail.member_count,
    created_at: detail.created_at,
    updated_at: detail.updated_at,
    archived_at: detail.archived_at,
  }
}

function upsertScenarioTemplateSummary(
  rows: AssetDecisionScenarioTemplateSummary[],
  detail: AssetDecisionScenarioTemplateDetail,
): AssetDecisionScenarioTemplateSummary[] {
  const summary = scenarioTemplateSummaryFromDetail(detail)
  const next = [summary, ...rows.filter((row) => row.template_id !== summary.template_id)]
  return next.sort((left, right) => {
    if (left.builtin !== right.builtin) return left.builtin ? -1 : 1
    if (left.status !== right.status) return left.status === 'active' ? -1 : 1
    return right.updated_at.localeCompare(left.updated_at)
  })
}




function actionHrefForMember(member: AssetDecisionRecordMember): string {
  if (member.execution_plan?.step_kind === 'open_cancellation_workbench') {
    return vpsWorkbenchPath(member.vps_id, 'cancellation')
  }
  if (member.execution_plan?.step_kind === 'open_subscription_context') {
    return `/subscriptions?vps_id=${encodeURIComponent(member.vps_id)}`
  }
  return vpsDetailPath(member.vps_id)
}

function sortedExecutionMembers(members: AssetDecisionRecordMember[]): AssetDecisionRecordMember[] {
  function executionLaneRank(member: AssetDecisionRecordMember): number {
    const lane = member.execution_plan?.lane ?? 'review'
    const rank = EXECUTION_PLAN_LANE_ORDER.indexOf(lane)
    return rank >= 0 ? rank : EXECUTION_PLAN_LANE_ORDER.length
  }
  return members
    .map((member, index) => ({ member, index }))
    .sort((left, right) => executionLaneRank(left.member) - executionLaneRank(right.member) || left.index - right.index)
    .map(({ member }) => member)
}

function scenarioForGroup(group: Pick<AssetDecisionGroupSummary, 'group_type'>): AssetDecisionManualGroupScenario {
  if (group.group_type === 'region_portfolio') return 'region_review'
  if (group.group_type === 'provider_portfolio') return 'provider_review'
  if (group.group_type === 'cost_pressure') return 'budget_reduction'
  if (group.group_type === 'cancellation_attention') return 'migration_retirement'
  if (group.group_type === 'evidence_gap') return 'evidence_cleanup'
  return 'general'
}





export function AssetDecisionsPageContent() {
  const route = useAssetDecisionRouteState()
  const portfolioView = route.state.portfolioView
  const renewalWindow = route.state.renewalWindow
  const assetDecisionFilter = route.state.filter
  const manualGroupListFilterKey = useMemo(
    () => assetDecisionFilterKey(assetDecisionFilter),
    [assetDecisionFilter],
  )
  const contextFilterChips = route.state.contextFilterChips
  const [queueView, setQueueView] = useState<DecisionQueueView>('all')
  const [portfolioState, setPortfolioState] = useState<PortfolioState>(INITIAL_PORTFOLIO_STATE)
  const [detailState, setDetailState] = useState<DetailState>(INITIAL_DETAIL_STATE)
  const [manualGroupsState, setManualGroupsState] = useState<ManualGroupsState>(INITIAL_MANUAL_GROUPS_STATE)
  const [manualDetailState, setManualDetailState] = useState<ManualDetailState>(INITIAL_MANUAL_DETAIL_STATE)
  const [templatesState, setTemplatesState] = useState<ScenarioTemplatesState>(INITIAL_SCENARIO_TEMPLATES_STATE)
  const [templateDetailState, setTemplateDetailState] = useState<TemplateDetailState>(INITIAL_TEMPLATE_DETAIL_STATE)
  const [vpsCatalogState, setVPSCatalogState] = useState<VPSCatalogState>(INITIAL_VPS_CATALOG_STATE)
  const [recordsState, setRecordsState] = useState<RecordsState>(INITIAL_RECORDS_STATE)
  const [recordDetailState, setRecordDetailState] = useState<RecordDetailState>(INITIAL_RECORD_DETAIL_STATE)
  const [queueState, setQueueState] = useState<QueueState>(INITIAL_QUEUE_STATE)
  const [selectedGroupID, setSelectedGroupID] = useState<string | null>(null)
  const [selectedManualGroupID, setSelectedManualGroupID] = useState<string | null>(null)
  const [selectedRecordID, setSelectedRecordID] = useState<string | null>(null)
  const [selectedTemplateID, setSelectedTemplateID] = useState<string | null>(null)
  const [selectedVPS, setSelectedVPS] = useState<VPSAssetRecord | null>(null)
  const [groupDetailPanel, setGroupDetailPanel] = useState<GroupDetailPanel>('overview')
  const [manualDetailPanel, setManualDetailPanel] = useState<ManualDetailPanel>('overview')
  const [recordDetailPanel, setRecordDetailPanel] = useState<RecordDetailPanel>('overview')
  const [templateDetailPanel, setTemplateDetailPanel] = useState<TemplateDetailPanel>('overview')
  const [recordDraft, setRecordDraft] = useState<RecordDraft | null>(null)
  const [recordDraftEditingMemberID, setRecordDraftEditingMemberID] = useState<string | null>(null)
  const [recordSaving, setRecordSaving] = useState(false)
  const [recordSaveError, setRecordSaveError] = useState<string | null>(null)
  const [manualGroupCreating, setManualGroupCreating] = useState(false)
  const [manualGroupSaving, setManualGroupSaving] = useState(false)
  const [manualGroupError, setManualGroupError] = useState<string | null>(null)
  const [manualMemberSaving, setManualMemberSaving] = useState<Record<string, boolean>>({})
  const [pendingManualMemberRemoval, setPendingManualMemberRemoval] = useState<AssetDecisionManualGroupMember | null>(null)
  const [manualMemberAddDraft, setManualMemberAddDraft] = useState<ManualMemberAddDraft>({
    vpsID: '',
    intendedRole: 'observe_candidate',
    intendedAction: 'review',
    reason: '',
    note: '',
    sortOrder: '',
  })
  const [manualMemberAddAdvanced, setManualMemberAddAdvanced] = useState(false)
  const [recordPatchStatus, setRecordPatchStatus] = useState<AssetDecisionRecordStatus>('draft')
  const [recordPatching, setRecordPatching] = useState(false)
  const [recordPatchError, setRecordPatchError] = useState<string | null>(null)
  const [recordFollowupDrafts, setRecordFollowupDrafts] = useState<Record<string, RecordFollowupDraft>>({})
  const [recordFollowupPatching, setRecordFollowupPatching] = useState<Record<string, boolean>>({})
  const [recordFollowupEditingMemberID, setRecordFollowupEditingMemberID] = useState<string | null>(null)
  const [templateSaving, setTemplateSaving] = useState(false)
  const [templateError, setTemplateError] = useState<string | null>(null)
  const [pendingTemplateStatus, setPendingTemplateStatus] = useState<AssetDecisionScenarioTemplateStatus | null>(null)
  const [templateManualDraft, setTemplateManualDraft] = useState<TemplateManualGroupDraft>({
    title: '',
    goal: '',
    note: '',
    renewWithinDays: renewalWindow,
  })
  const [decisionDraft, setDecisionDraft] = useState<AssetDecisionDraft>(INITIAL_DECISION_DRAFT)
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [refreshToken, setRefreshToken] = useState(0)
  const searchParamSignature = route.state.searchSignature
  const secondaryWorkbench = route.state.secondary
  const setSelectedSecondaryWorkbench = route.commands.setSecondary
  const handledOpenStateRef = useRef('')
  const preservedManualGroupSummariesRef = useRef(new Map<string, {
    filterKey: string
    summary: AssetDecisionManualGroupSummary
  }>())

  function applyURLClearedOpenState() {
    setSelectedGroupID(null)
    setSelectedManualGroupID(null)
    setSelectedRecordID(null)
    setSelectedTemplateID(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setDetailState(INITIAL_DETAIL_STATE)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setPendingManualMemberRemoval(null)
    setPendingTemplateStatus(null)
  }

  function applyURLGroupOpenState(groupID: string) {
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setPendingManualMemberRemoval(null)
    setPendingTemplateStatus(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setDetailState({ loading: true, error: null, detail: null })
    setSelectedGroupID(groupID)
  }

  function applyURLManualGroupOpenState(manualGroupID: string) {
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setPendingManualMemberRemoval(null)
    setPendingTemplateStatus(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setManualDetailState({ loading: true, error: null, detail: null })
    setSelectedManualGroupID(manualGroupID)
  }

  function applyURLRecordOpenState(recordID: string) {
    setSelectedGroupID(null)
    setSelectedManualGroupID(null)
    setSelectedTemplateID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setPendingManualMemberRemoval(null)
    setPendingTemplateStatus(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setSelectedRecordID(recordID)
    setRecordDetailState({ loading: true, error: null, detail: null })
    setRecordPatchError(null)
  }

  function applyURLTemplateOpenState(templateID: string) {
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setTemplateDetailState({ loading: true, error: null, detail: null })
    setSelectedTemplateID(templateID)
  }

  useEffect(() => {
    let cancelled = false
    const filter = assetDecisionFilter

    Promise.allSettled([
      getAssetDecisionOverview(filter),
      listAssetDecisionGroups(filter),
    ] as const)
      .then(([overviewResult, groupsResult]) => {
        if (cancelled) return
        const overviewError = overviewResult.status === 'rejected'
          ? describeError(overviewResult.reason, '加载资产组合概览失败')
          : null
        const groupsError = groupsResult.status === 'rejected'
          ? describeError(groupsResult.reason, '加载资产决策组失败')
          : null
        setPortfolioState((current) => ({
          ...current,
          overviewLoading: false,
          overviewError,
          overview: overviewResult.status === 'fulfilled' ? overviewResult.value : null,
          groupsLoading: false,
          groupsError,
          groups: groupsResult.status === 'fulfilled' ? groupsResult.value : [],
        }))
      })

    return () => { cancelled = true }
  }, [assetDecisionFilter, refreshToken])

  useEffect(() => {
    let cancelled = false
    listAssetDecisionRecords(assetDecisionFilter)
      .then((records) => {
        if (cancelled) return
        setRecordsState({ loading: false, error: null, records })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setRecordsState({
          loading: false,
          error: describeError(error, '加载已保存组合决策失败'),
          records: [],
        })
      })
    return () => { cancelled = true }
  }, [assetDecisionFilter, refreshToken])

  useEffect(() => {
    let cancelled = false
    listAssetDecisionManualGroups(assetDecisionFilter)
      .then((groups) => {
        if (cancelled) return
        setManualGroupsState({
          loading: false,
          error: null,
          groups: mergeManualGroupSummaries(
            groups,
            Array.from(preservedManualGroupSummariesRef.current.values())
              .filter((item) => item.filterKey === manualGroupListFilterKey)
              .map((item) => item.summary),
          ),
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setManualGroupsState({
          loading: false,
          error: describeError(error, '加载自定义组合失败'),
          groups: [],
        })
      })
    return () => { cancelled = true }
  }, [assetDecisionFilter, manualGroupListFilterKey, refreshToken])

  useEffect(() => {
    let cancelled = false
    listAssetDecisionScenarioTemplates()
      .then((templates) => {
        if (cancelled) return
        setTemplatesState({ loading: false, error: null, templates })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setTemplatesState({
          loading: false,
          error: describeError(error, '加载场景模板失败'),
          templates: [],
        })
      })
    return () => { cancelled = true }
  }, [refreshToken])

  useEffect(() => {
    let cancelled = false
    listSubscriptions({
      renew_within_days: renewalWindow,
      sort: 'renew_at',
      order: 'asc',
    })
      .then((renewals) => {
        if (cancelled) return
        setQueueState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: null,
          renewals,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setQueueState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: describeError(error, '加载续费 evidence 失败'),
          renewals: [],
        }))
      })
    return () => { cancelled = true }
  }, [renewalWindow, refreshToken])

  useEffect(() => {
    let cancelled = false
    Promise.all([
      listSubscriptions({ sort: 'renew_at', order: 'asc' }),
      listVPSAssets({ renewal_decision: 'unreviewed' }),
      listVPSAssets({ renewal_decision: 'migrate' }),
      listVPSAssets({ renewal_decision: 'cancel' }),
    ])
      .then(([subscriptions, unreviewed, migrate, cancel]) => {
        if (cancelled) return
        setQueueState((current) => ({
          ...current,
          queueLoading: false,
          queueError: null,
          subscriptions,
          unreviewed,
          migrate,
          cancel,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setQueueState((current) => ({
          ...current,
          queueLoading: false,
          queueError: describeError(error, '加载 VPS 单台队列失败'),
          subscriptions: [],
          unreviewed: [],
          migrate: [],
          cancel: [],
        }))
      })
    return () => { cancelled = true }
  }, [refreshToken])

  useEffect(() => {
    let cancelled = false
    listVPSAssets()
      .then((rows) => {
        if (cancelled) return
        setVPSCatalogState({ loading: false, error: null, rows })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setVPSCatalogState({
          loading: false,
          error: describeError(error, '加载 VPS 候选失败'),
          rows: [],
        })
      })
    return () => { cancelled = true }
  }, [refreshToken])

  useEffect(() => {
    if (!selectedGroupID) {
      return
    }
    let cancelled = false
    getAssetDecisionGroup(selectedGroupID, { renew_within_days: renewalWindow })
      .then((detail) => {
        if (cancelled) return
        setDetailState({ loading: false, error: null, detail })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setDetailState({
          loading: false,
          error: describeError(error, '加载决策组详情失败'),
          detail: null,
        })
      })
    return () => { cancelled = true }
  }, [selectedGroupID, renewalWindow, refreshToken])

  useEffect(() => {
    if (!selectedManualGroupID) {
      return
    }
    let cancelled = false
    getAssetDecisionManualGroup(selectedManualGroupID)
      .then((detail) => {
        if (cancelled) return
        setManualDetailState({ loading: false, error: null, detail })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setManualDetailState({
          loading: false,
          error: describeError(error, '加载自定义组合详情失败'),
          detail: null,
        })
      })
    return () => { cancelled = true }
  }, [selectedManualGroupID, refreshToken])

  useEffect(() => {
    if (!selectedRecordID) {
      return
    }
    let cancelled = false
    getAssetDecisionRecord(selectedRecordID)
      .then((detail) => {
        if (cancelled) return
        setRecordDetailState({ loading: false, error: null, detail })
        setRecordPatchStatus(detail.status)
        setRecordFollowupDrafts(buildRecordFollowupDrafts(detail))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setRecordDetailState({
          loading: false,
          error: describeError(error, '加载决策记录失败'),
          detail: null,
        })
        setRecordFollowupDrafts({})
      })
    return () => { cancelled = true }
  }, [selectedRecordID, refreshToken])

  useEffect(() => {
    if (!selectedTemplateID) {
      return
    }
    let cancelled = false
    getAssetDecisionScenarioTemplate(selectedTemplateID)
      .then((detail) => {
        if (cancelled) return
        setTemplateDetailState({ loading: false, error: null, detail })
        setTemplateManualDraft({
          title: detail.title,
          goal: detail.goal,
          note: '',
          renewWithinDays: renewalWindow,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setTemplateDetailState({
          loading: false,
          error: describeError(error, '加载场景模板失败'),
          detail: null,
        })
      })
    return () => { cancelled = true }
  }, [selectedTemplateID, renewalWindow, refreshToken])

  useEffect(() => {
    const openSelection = route.state.open
    const groupID = openSelection?.type === 'group_id' ? openSelection.id : undefined
    const manualGroupID = openSelection?.type === 'manual_group_id' ? openSelection.id : undefined
    const recordID = openSelection?.type === 'record_id' ? openSelection.id : undefined
    const templateID = openSelection?.type === 'template_id' ? openSelection.id : undefined
    const openStateKey = openSelection ? `${openSelection.type}:${openSelection.id}` : ''

    if (openStateKey === handledOpenStateRef.current) return
    // Defer URL-driven drawer/modal selection so deep links do not cascade renders in the effect body.
    const timer = window.setTimeout(() => {
      if (!openStateKey && handledOpenStateRef.current) {
        handledOpenStateRef.current = ''
        applyURLClearedOpenState()
        return
      }
      if (openStateKey === handledOpenStateRef.current) return
      handledOpenStateRef.current = openStateKey
      if (groupID && groupID !== selectedGroupID) {
        applyURLGroupOpenState(groupID)
        return
      }
      if (manualGroupID && manualGroupID !== selectedManualGroupID) {
        applyURLManualGroupOpenState(manualGroupID)
        return
      }
      if (recordID && recordID !== selectedRecordID) {
        applyURLRecordOpenState(recordID)
        return
      }
      if (templateID && templateID !== selectedTemplateID) {
        applyURLTemplateOpenState(templateID)
      }
    }, 0)
    return () => {
      window.clearTimeout(timer)
    }
  }, [route.state.open, searchParamSignature, selectedGroupID, selectedManualGroupID, selectedRecordID, selectedTemplateID])

  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(queueState.subscriptions),
    [queueState.subscriptions],
  )
  const vpsByID = useMemo(() => {
    const rows = new Map<string, VPSAssetRecord>()
    for (const vps of vpsCatalogState.rows) rows.set(vps.vps_id, vps)
    for (const vps of [...queueState.unreviewed, ...queueState.migrate, ...queueState.cancel]) {
      rows.set(vps.vps_id, vps)
    }
    for (const member of detailState.detail?.members ?? []) rows.set(member.vps.vps_id, member.vps)
    for (const member of manualDetailState.detail?.members ?? []) {
      if (member.current_fact_found && member.vps?.vps_id) {
        rows.set(member.vps.vps_id, member.vps)
      }
    }
    return rows
  }, [detailState.detail?.members, manualDetailState.detail?.members, queueState.cancel, queueState.migrate, queueState.unreviewed, vpsCatalogState.rows])
  const manualMemberCandidateRows = useMemo(() => {
    const existing = new Set((manualDetailState.detail?.members ?? []).map((member) => member.vps_id))
    return vpsCatalogState.rows
      .filter((vps) => !existing.has(vps.vps_id))
      .sort((left, right) => left.display_name.localeCompare(right.display_name))
  }, [manualDetailState.detail?.members, vpsCatalogState.rows])
  const decisionQueue = useMemo(
    () =>
      buildDecisionQueue(
        [...queueState.unreviewed, ...queueState.migrate, ...queueState.cancel],
        subscriptionsByVPS,
        renewalWindow,
      ),
    [queueState.cancel, queueState.migrate, queueState.unreviewed, subscriptionsByVPS, renewalWindow],
  )
  const visibleDecisionQueue = useMemo(
    () => filterDecisionQueue(decisionQueue, queueView),
    [decisionQueue, queueView],
  )

  const overview = portfolioState.overview
  const renewalDueQueueCount = decisionQueue.filter((item) => item.renewalDue).length
  const missingSubscriptionCount = decisionQueue.filter((item) => !item.subscription).length
  const unlinkedCount = decisionQueue.filter((item) => item.vps.active_monitoring_instance_link_count <= 0).length
  const cancellationAttentionCount = decisionQueue.filter(hasCancellationAttention).length
  const totalDecisionQueue = decisionQueue.length
  const selectedRecordAssessment = recordDetailState.detail
    ? parseEvidenceAssessment(recordDetailState.detail.evidence_snapshot)
    : null
  const manualGroupProgress = manualDetailState.detail
    ? buildManualGroupProgress(manualDetailState.detail)
    : null
  const closedLoopSourceErrors: ClosedLoopSourceErrors = {
    overview: portfolioState.overviewError,
    groups: portfolioState.groupsError,
    records: recordsState.error,
    manualGroups: manualGroupsState.error,
    templates: templatesState.error,
  }
  const closedLoopMetrics = deriveClosedLoopMetrics(
    portfolioState.groups,
    recordsState.records,
    manualGroupsState.groups,
    closedLoopSourceErrors,
    overview,
  )
  const nextWorkItems = deriveNextWorkItems(
    portfolioState.groups,
    recordsState.records,
    closedLoopSourceErrors,
  )
  const portfolioLead = buildPortfolioLead(
    portfolioView,
    renewalWindow,
    overview,
    portfolioState.groups,
    nextWorkItems,
    closedLoopMetrics,
    contextFilterChips,
  )
  const closedLoopPartialErrors = [
    closedLoopSourceErrors.overview ? '组合概览' : '',
    closedLoopSourceErrors.groups ? '自动组' : '',
    closedLoopSourceErrors.records ? '决策记录' : '',
    closedLoopSourceErrors.manualGroups ? '自定义组合' : '',
    closedLoopSourceErrors.templates ? '场景模板' : '',
  ].filter(Boolean)
  const secondaryNavItems = buildSecondaryNavItems(
    recordsState,
    manualGroupsState,
    templatesState,
    queueState,
    visibleDecisionQueue.length,
    totalDecisionQueue,
  )

  const memberColumns: DataTableColumn<AssetDecisionGroupMember>[] = [
    {
      key: 'vps',
      label: 'VPS',
      width: '280px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={vpsDetailPath(member.vps.vps_id)}>{member.vps.display_name}</Link></strong>
          <span>{formatOptional(member.vps.provider_name)} · {vpsLocationLabel(member.vps)}</span>
        </div>
      ),
    },
    {
      key: 'role',
      label: '角色',
      width: '160px',
      render: (member) => (
        <Badge variant="state" tone={roleTone(member.suggested_role)}>
          {ROLE_LABELS[member.suggested_role]}
        </Badge>
      ),
    },
    {
      key: 'action',
      label: '动作',
      width: '180px',
      render: (member) => (
        <Badge variant="state" tone={actionTone(member.suggested_action)}>
          {ACTION_LABELS[member.suggested_action]}
        </Badge>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '220px',
      render: (member) => (
        <span className="asset-decision-chip-row">
          <LifecycleBadge value={member.vps.lifecycle_status} />
          <UsageBadge value={member.vps.usage_status} />
          <RenewalBadge value={member.vps.renewal_decision} />
        </span>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '112px',
      render: (member) => (
        <button className="btn sm primary" type="button" onClick={() => selectVPS(member.vps)}>
          处理
        </button>
      ),
    },
  ]

  const manualMemberColumns: DataTableColumn<AssetDecisionManualGroupMember>[] = [
    {
      key: 'vps',
      label: 'VPS',
      width: '280px',
      render: (member) => {
        const displayName = member.current_fact_found ? member.vps.display_name : member.vps_id
        return (
          <div className="asset-table__identity">
            <strong>
              {member.current_fact_found ? (
                <Link className="name" to={vpsDetailPath(member.vps_id)}>{displayName}</Link>
              ) : (
                displayName
              )}
            </strong>
            <span>{member.current_fact_found ? `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}` : '当前资产事实缺失'}</span>
          </div>
        )
      },
    },
    {
      key: 'role',
      label: '角色',
      width: '160px',
      render: (member) => (
        <Badge variant="state" tone={roleTone(member.intended_role)}>
          {ROLE_LABELS[member.intended_role]}
        </Badge>
      ),
    },
    {
      key: 'action',
      label: '动作',
      width: '180px',
      render: (member) => (
        <Badge variant="state" tone={actionTone(member.intended_action)}>
          {ACTION_LABELS[member.intended_action]}
        </Badge>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '220px',
      render: (member) => (
        <span className="asset-decision-chip-row">
          {member.current_fact_found ? (
            <>
              <LifecycleBadge value={member.vps.lifecycle_status} />
              <UsageBadge value={member.vps.usage_status} />
              <RenewalBadge value={member.vps.renewal_decision} />
            </>
          ) : (
            <Badge variant="state" tone="critical">事实缺失</Badge>
          )}
        </span>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '112px',
      render: (member) => (
        <button
          className="btn sm secondary"
          type="button"
          disabled={Boolean(manualMemberSaving[member.vps_id])}
          onClick={() => requestManualMemberRemoval(member)}
        >
          移除
        </button>
      ),
    },
  ]

  const recordMemberColumns: DataTableColumn<AssetDecisionRecordMember>[] = [
    {
      key: 'vps',
      label: 'VPS',
      width: '220px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={vpsDetailPath(member.vps_id)}>{member.display_name || member.vps_id}</Link></strong>
          <span>{member.vps_id}</span>
          <span>保存于 {formatDateTime(member.created_at)}</span>
        </div>
      ),
    },
    {
      key: 'decided',
      label: '用户判断',
      width: '220px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.decided_role)}>
              {ROLE_LABELS[member.decided_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.decided_action)}>
              {ACTION_LABELS[member.decided_action]}
            </Badge>
          </span>
          <span>{member.reason || '未填写成员理由'}</span>
        </div>
      ),
    },
    {
      key: 'evidence',
      label: '快照证据',
      width: '308px',
      render: (member) => {
        const assessment = member.evidence_snapshot.evidence_assessment as AssetDecisionEvidenceAssessment | null
        return (
          <div className="asset-table__stack">
            {renderEvidenceAssessment(assessment)}
            <strong>
              服务 {String(member.evidence_snapshot.service_count ?? '—')} · 域名 {String(member.evidence_snapshot.domain_count ?? '—')}
            </strong>
            <span>
              监控 {String(member.evidence_snapshot.running_monitoring_count ?? '—')}/{String(member.evidence_snapshot.monitoring_link_count ?? '—')}
            </span>
            <span>{String(member.evidence_snapshot.primary_issue_summary || '暂无主要问题')}</span>
          </div>
        )
      },
    },
    {
      key: 'readback',
      label: '当前回读',
      width: '360px',
      render: (member) => renderRecordMemberRawDetails(member),
    },
    {
      key: 'plan',
      label: '下一步',
      width: '248px',
      render: (member) => renderMemberExecutionPlan(member),
    },
    {
      key: 'actions',
      label: '跟进',
      align: 'right',
      width: '286px',
      render: (member) => {
        const editing = recordFollowupEditingMemberID === member.vps_id
        return editing ? renderRecordFollowupForm(member) : (
          <button
            className="btn sm primary"
            type="button"
            onClick={() => setRecordFollowupEditingMemberID(member.vps_id)}
          >
            编辑
          </button>
        )
      },
    },
  ]

  function renderRecordFollowupForm(member: AssetDecisionRecordMember) {
    const draft = recordFollowupDrafts[member.vps_id] ?? {
      status: member.followup_status,
      note: member.followup_note,
    }
    const isSaving = Boolean(recordFollowupPatching[member.vps_id])
    const isChanged = draft.status !== member.followup_status || draft.note !== member.followup_note
    return (
      <form className="asset-decision-followup-form" onSubmit={(event) => submitRecordMemberFollowup(event, member)}>
        <div className="asset-decision-followup-form__status">
          <Badge variant="state" tone={followupStatusTone(member.followup_status)}>
            {FOLLOWUP_STATUS_LABELS[member.followup_status]}
          </Badge>
          <span>{member.followup_updated_at ? `更新 ${formatDateTime(member.followup_updated_at)}` : '尚未跟进'}</span>
        </div>
        <label className="visually-hidden" htmlFor={`followup-status-${member.record_id}-${member.vps_id}`}>
          {member.display_name || member.vps_id} 跟进状态
        </label>
        <select
          id={`followup-status-${member.record_id}-${member.vps_id}`}
          aria-label="跟进状态"
          className="input"
          value={draft.status}
          onChange={(event) => updateRecordFollowupDraft(member.vps_id, { status: event.target.value as AssetDecisionFollowupStatus })}
        >
          {FOLLOWUP_STATUS_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </select>
        <label className="visually-hidden" htmlFor={`followup-note-${member.record_id}-${member.vps_id}`}>
          {member.display_name || member.vps_id} 跟进备注
        </label>
        <input
          id={`followup-note-${member.record_id}-${member.vps_id}`}
          aria-label="跟进备注"
          className="input"
          value={draft.note}
          placeholder="备注 / 阻塞原因"
          onChange={(event) => updateRecordFollowupDraft(member.vps_id, { note: event.target.value })}
        />
        <div className="asset-decision-followup-form__actions">
          <button className="btn sm primary" type="submit" disabled={isSaving || !isChanged}>
            {isSaving ? '保存中…' : '保存跟进'}
          </button>
          <button
            className="btn sm secondary"
            type="button"
            onClick={() => setRecordFollowupEditingMemberID(null)}
          >
            收起
          </button>
        </div>
      </form>
    )
  }

  function renderRecordMemberRawDetails(member: AssetDecisionRecordMember) {
    const issues = member.execution_readback?.issues ?? []
    const facts = member.execution_readback?.current_facts
    const ipQuality = facts?.ip_quality_summary
    const blockedServices = facts?.ip_quality_blocked_services ?? []
    return (
      <div className="asset-table__stack">
        <strong>{member.execution_readback?.summary || '等待执行回读'}</strong>
        <span>{currentFactsLabel(facts)}</span>
        <span>{currentFactsStateLabel(facts)}</span>
        {issues.length > 0 && (
          <span>
            问题{' '}
            {issues.map((issue, index) => (
              <span key={`${issue.kind}-${issue.label}-${index}`}>
                {index > 0 ? '；' : ''}
                <span>{issue.label}</span>
                {issue.details ? `：${issue.details}` : ''}
              </span>
            ))}
          </span>
        )}
        {ipQuality && (
          <span>
            IP 质量 {ipQuality.observed_at || '未记录时间'} · {ipQuality.asn || 'ASN 未知'} · {ipQuality.organization || '组织未知'} · provider {ipQuality.provider_count} · 可解锁 {ipQuality.unlockable_count} · {ipQuality.assignment_mode}
          </span>
        )}
        {blockedServices.length > 0 && (
          <span>受阻服务 {blockedServices.map(ipQualityServiceLabel).join('、')}</span>
        )}
      </div>
    )
  }

  function renderRecordMemberFollowupRows(detail: AssetDecisionRecordDetail) {
    const memberPreview = previewItems(detail.members)
    if (detail.members.length === 0) {
      return (
        <section className="asset-decision-record-followups" aria-label="成员跟进列表">
          <div className="asset-decision-member-decisions__empty">
            <strong>暂无成员跟进</strong>
            <span>当前记录没有可展示的成员。</span>
          </div>
        </section>
      )
    }

    return (
      <section className="asset-decision-record-followups" aria-label="成员跟进列表">
        {memberPreview.visible.map((member) => {
          const editing = recordFollowupEditingMemberID === member.vps_id
          return (
            <article key={member.vps_id} className={`asset-decision-record-followup-row${editing ? ' asset-decision-record-followup-row--editing' : ''}`}>
              <div className="asset-decision-record-followup-row__identity">
                <strong>{member.display_name || member.vps_id}</strong>
                <span className="asset-decision-chip-row">
                  <Badge variant="state" tone={roleTone(member.decided_role)}>
                    {ROLE_LABELS[member.decided_role]}
                  </Badge>
                  <Badge variant="state" tone={actionTone(member.decided_action)}>
                    {ACTION_LABELS[member.decided_action]}
                  </Badge>
                </span>
              </div>
              <div className="asset-decision-record-followup-row__state">
                <span className="asset-decision-chip-row">
                  {renderReadbackBadge(member.execution_readback)}
                  <Badge variant="state" tone={followupStatusTone(member.followup_status)}>
                    {FOLLOWUP_STATUS_LABELS[member.followup_status]}
                  </Badge>
                </span>
                <strong>
                  {compactDecisionText(member.followup_note || compactMemberReadbackSummary(member.execution_readback), '尚未跟进')}
                </strong>
              </div>
              <div className="asset-decision-record-followup-row__form">
                {editing ? (
                  renderRecordFollowupForm(member)
                ) : (
                  <button
                    className="btn-text sm secondary"
                    type="button"
                    aria-label="编辑跟进"
                    aria-expanded={false}
                    onClick={() => setRecordFollowupEditingMemberID(member.vps_id)}
                  >
                    编辑跟进
                  </button>
                )}
              </div>
            </article>
          )
        })}
        {memberPreview.hiddenCount > 0 && (
          <div className="asset-decision-preview-more" role="note">
            <span>另有 {memberPreview.hiddenCount} 台在成员底稿中查看</span>
            <button className="btn-text sm secondary" type="button" onClick={() => setRecordDetailPanel('raw')}>
              查看成员底稿
            </button>
          </div>
        )}
      </section>
    )
  }

  function setWorkbenchView(next: MainWorkbenchView) {
    setPortfolioState((current) => ({
      ...current,
      overviewLoading: true,
      overviewError: null,
      groupsLoading: true,
      groupsError: null,
    }))
    route.commands.setWorkbench(next)
  }

  function changeRenewalWindow(value: string) {
    const nextWindow = parseRenewalWindow(value)
    setPortfolioState((current) => ({
      ...current,
      overviewLoading: true,
      overviewError: null,
      groupsLoading: true,
      groupsError: null,
    }))
    setQueueState((current) => ({
      ...current,
      renewalsLoading: true,
      renewalsError: null,
    }))
    route.commands.setRenewalWindow(nextWindow)
  }

  function setOpenState(key: OpenStateKey, value: string) {
    route.commands.openEntity(key, value)
  }

  function clearOpenState(key: OpenStateKey) {
    route.commands.closeEntity(key)
  }

  function clearContextFilter(key: ContextFilterKey) {
    route.commands.clearFilter(key)
  }

  function clearAllContextFilters() {
    route.commands.clearAllFilters()
  }

  function openGroup(groupID: string) {
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setDetailState({ loading: true, error: null, detail: null })
    setSelectedGroupID(groupID)
    setOpenState('group_id', groupID)
  }

  function closeGroupDetail() {
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedVPS(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setDecisionDraft(INITIAL_DECISION_DRAFT)
    setDecisionError(null)
    setGroupDetailPanel('overview')
    clearOpenState('group_id')
  }

  function openManualGroup(manualGroupID: string) {
    setSelectedSecondaryWorkbench('scenarios')
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setManualDetailState({ loading: true, error: null, detail: null })
    setSelectedManualGroupID(manualGroupID)
    setOpenState('manual_group_id', manualGroupID)
  }

  function closeManualGroupDetail() {
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setManualGroupError(null)
    setManualMemberSaving({})
    setPendingManualMemberRemoval(null)
    setManualMemberAddDraft({
      vpsID: '',
      intendedRole: 'observe_candidate',
      intendedAction: 'review',
      reason: '',
      note: '',
      sortOrder: '',
    })
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualDetailPanel('overview')
    clearOpenState('manual_group_id')
  }

  function openTemplate(templateID: string) {
    setSelectedSecondaryWorkbench('scenarios')
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionError(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setTemplateDetailState({ loading: true, error: null, detail: null })
    setSelectedTemplateID(templateID)
    setOpenState('template_id', templateID)
  }

  function closeTemplateDetail() {
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setTemplateError(null)
    setPendingTemplateStatus(null)
    setTemplateManualDraft({
      title: '',
      goal: '',
      note: '',
      renewWithinDays: renewalWindow,
    })
    setTemplateDetailPanel('overview')
    clearOpenState('template_id')
  }

  function openNextWorkItem(item: AssetDecisionNextWorkItem) {
    if (item.target.type === 'record') {
      openRecord(item.target.id)
      return
    }
    if (item.target.type === 'manual_group') {
      openManualGroup(item.target.id)
      return
    }
    if (item.target.type === 'template') {
      openTemplate(item.target.id)
      return
    }
    openGroup(item.target.id)
  }

  function applyManualDetail(detail: AssetDecisionManualGroupDetail) {
    const summary = manualGroupSummaryFromDetail(detail)
    preservedManualGroupSummariesRef.current.set(summary.manual_group_id, {
      filterKey: manualGroupListFilterKey,
      summary,
    })
    setManualDetailState({ loading: false, error: null, detail })
    setPendingManualMemberRemoval((current) =>
      current && detail.members.some((member) => member.vps_id === current.vps_id) ? current : null,
    )
    setManualGroupsState((current) => ({
      loading: false,
      error: null,
      groups: mergeManualGroupSummaries(current.groups, [summary]),
    }))
  }

  function applyTemplateDetail(detail: AssetDecisionScenarioTemplateDetail) {
    setTemplateDetailState({ loading: false, error: null, detail })
    setPendingTemplateStatus(null)
    setTemplateManualDraft({
      title: detail.title,
      goal: detail.goal,
      note: '',
      renewWithinDays: renewalWindow,
    })
    setTemplatesState((current) => ({
      loading: false,
      error: null,
      templates: upsertScenarioTemplateSummary(current.templates, detail),
    }))
  }

  function createManualGroupFromAuto(detail: AssetDecisionGroupDetail) {
    setManualGroupError(null)
    setManualGroupCreating(true)
    createAssetDecisionManualGroup({
      source_type: 'auto_group',
      source_group_id: detail.group_id,
      renew_within_days: renewalWindow,
      scenario: scenarioForGroup(detail),
      title: detail.title,
      goal: '',
      note: `由自动组 ${detail.group_id} 创建`,
    })
      .then((manualDetail) => {
        applyManualDetail(manualDetail)
        setSelectedSecondaryWorkbench('scenarios')
        setSelectedGroupID(null)
        setDetailState(INITIAL_DETAIL_STATE)
        setSelectedVPS(null)
        setDecisionDraft(INITIAL_DECISION_DRAFT)
        setSelectedManualGroupID(manualDetail.manual_group_id)
        setGroupDetailPanel('overview')
        setManualDetailPanel('overview')
        setOpenState('manual_group_id', manualDetail.manual_group_id)
        setDecisionNotice(`已创建自定义组合：${manualDetail.title}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '创建自定义组合失败'))
      })
      .finally(() => setManualGroupCreating(false))
  }

  function startRecordSave(detail: AssetDecisionGroupDetail) {
    const keepsCurrentDraft = recordDraft?.sourceType === 'auto_group' && recordDraft.sourceGroupID === detail.group_id
    setRecordDraft((current) => completeRecordDraftFromGroupDetail(current, detail, renewalWindow))
    if (!keepsCurrentDraft) {
      setRecordDraftEditingMemberID(null)
    }
    setRecordSaveError(null)
    setGroupDetailPanel('save')
  }

  function startManualRecordSave(detail: AssetDecisionManualGroupDetail) {
    const keepsCurrentDraft = recordDraft?.sourceType === 'manual_group' && recordDraft.sourceGroupID === detail.manual_group_id
    setRecordDraft((current) => completeRecordDraftFromManualDetail(current, detail))
    if (!keepsCurrentDraft) {
      setRecordDraftEditingMemberID(null)
    }
    setRecordSaveError(null)
    setManualDetailPanel('save')
  }

  function cancelRecordSave() {
    const sourceType = recordDraft?.sourceType
    setRecordDraft(null)
    setRecordDraftEditingMemberID(null)
    setRecordSaveError(null)
    if (sourceType === 'auto_group') setGroupDetailPanel('overview')
    if (sourceType === 'manual_group') setManualDetailPanel('overview')
  }

  function updateRecordDraftMember(vpsID: string, patch: Partial<RecordMemberDraft>) {
    setRecordDraft((current) => {
      if (!current) return current
      const existing = current.members[vpsID]
      if (!existing) return current
      return {
        ...current,
        members: {
          ...current.members,
          [vpsID]: {
            ...existing,
            ...patch,
          },
        },
      }
    })
  }

  function renderRecordDraftMemberRows(
    members: Array<{
      vpsID: string
      displayName: string
      fallbackRole: AssetDecisionSuggestedRole
      fallbackAction: AssetDecisionSuggestedAction
      meta?: string
      chips?: AssetDecisionEvidenceChip[]
    }>,
  ) {
    if (!recordDraft) return null
    const memberPreview = previewItems(members)
    return (
      <div className="asset-decision-save-members" aria-label="保存记录成员复核">
        {memberPreview.visible.map((member) => {
          const memberDraft = recordDraft.members[member.vpsID]
          const decidedRole = memberDraft?.decidedRole ?? member.fallbackRole
          const decidedAction = memberDraft?.decidedAction ?? member.fallbackAction
          const reason = memberDraft?.reason ?? ''
          const editing = recordDraftEditingMemberID === member.vpsID
          return (
            <article key={member.vpsID} className={`asset-decision-save-member${editing ? ' asset-decision-save-member--editing' : ''}`}>
              <div className="asset-decision-save-member__summary">
                <div className="asset-table__identity">
                  <strong>{member.displayName}</strong>
                </div>
                <span className="asset-decision-chip-row">
                  <Badge variant="state" tone={roleTone(decidedRole)}>
                    {ROLE_LABELS[decidedRole]}
                  </Badge>
                  <Badge variant="state" tone={actionTone(decidedAction)}>
                    {ACTION_LABELS[decidedAction]}
                  </Badge>
                  <Badge variant="state" tone={reason.trim() ? 'normal' : 'maintenance'}>
                    {reason.trim() ? '已写理由' : '理由待补'}
                  </Badge>
                </span>
                <button
                  className="btn-text sm secondary"
                  type="button"
                  aria-expanded={editing}
                  onClick={() => setRecordDraftEditingMemberID((current) => current === member.vpsID ? null : member.vpsID)}
                >
                  {editing ? '收起编辑' : `编辑 ${member.displayName} 成员理由`}
                </button>
              </div>
              {editing && (
                <div className="asset-decision-save-member__editor">
                  <div className="input-field">
                    <span>角色</span>
                    <select
                      aria-label="角色"
                      className="input"
                      value={decidedRole}
                      onChange={(event) => updateRecordDraftMember(member.vpsID, { decidedRole: event.target.value as AssetDecisionSuggestedRole })}
                    >
                      {ROLE_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="input-field">
                    <span>动作</span>
                    <select
                      aria-label="动作"
                      className="input"
                      value={decidedAction}
                      onChange={(event) => updateRecordDraftMember(member.vpsID, { decidedAction: event.target.value as AssetDecisionSuggestedAction })}
                    >
                      {ACTION_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="input-field asset-decision-save-member__reason">
                    <span>理由</span>
                    <input
                      aria-label="理由"
                      className="input"
                      value={reason}
                      onChange={(event) => updateRecordDraftMember(member.vpsID, { reason: event.target.value })}
                    />
                  </div>
                </div>
              )}
            </article>
          )
        })}
        {memberPreview.hiddenCount > 0 && (
          <div className="asset-decision-preview-more" role="note">
            另有 {memberPreview.hiddenCount} 台成员保留在保存底稿中
          </div>
        )}
      </div>
    )
  }

  function submitRecordSave(event: FormSubmitEvent) {
    event.preventDefault()
    const draft = recordDraft
    if (!draft) return
    setRecordSaveError(null)

    const title = draft.title.trim()
    if (!title) {
      setRecordSaveError('请填写决策记录标题')
      return
    }

    setRecordSaving(true)
    createAssetDecisionRecord({
      source_type: draft.sourceType,
      source_group_id: draft.sourceGroupID,
      renew_within_days: draft.renewWithinDays,
      title,
      goal: draft.goal.trim(),
      status: draft.status,
      members: draft.memberOrder.map((vpsID) => {
        const memberDraft = draft.members[vpsID]
        return {
          vps_id: vpsID,
          decided_role: memberDraft?.decidedRole ?? 'observe_candidate',
          decided_action: memberDraft?.decidedAction ?? 'review',
          reason: memberDraft?.reason.trim() ?? '',
        }
      }),
    })
      .then((record) => {
        setRecordsState((current) => ({
          loading: false,
          error: null,
          records: [record, ...current.records.filter((item) => item.record_id !== record.record_id)],
        }))
        setRecordDraft(null)
        setDecisionNotice(`已保存组合决策记录：${record.title}`)
        setSelectedGroupID(null)
        setSelectedManualGroupID(null)
        setSelectedTemplateID(null)
        setDetailState(INITIAL_DETAIL_STATE)
        setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
        setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
        setSelectedVPS(null)
        setSelectedRecordID(record.record_id)
        setOpenState('record_id', record.record_id)
        setRecordDetailState({ loading: false, error: null, detail: record })
        setRecordPatchStatus(record.status)
        setGroupDetailPanel('overview')
        setManualDetailPanel('overview')
        setTemplateDetailPanel('overview')
        setRecordDetailPanel('overview')
      })
      .catch((error: unknown) => {
        setRecordSaveError(describeError(error, '保存组合决策记录失败'))
      })
      .finally(() => setRecordSaving(false))
  }

  function openRecord(recordID: string) {
    setSelectedSecondaryWorkbench('records')
    setSelectedGroupID(null)
    setSelectedManualGroupID(null)
    setSelectedTemplateID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setSelectedVPS(null)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
    setSelectedRecordID(recordID)
    setRecordDetailState({ loading: true, error: null, detail: null })
    setRecordPatchError(null)
    setGroupDetailPanel('overview')
    setManualDetailPanel('overview')
    setRecordDetailPanel('overview')
    setTemplateDetailPanel('overview')
    setRecordFollowupEditingMemberID(null)
    setOpenState('record_id', recordID)
  }

  function closeRecordDetail() {
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setRecordPatchError(null)
    setRecordFollowupDrafts({})
    setRecordFollowupPatching({})
    setRecordFollowupEditingMemberID(null)
    setRecordDetailPanel('overview')
    clearOpenState('record_id')
  }

  function submitTemplateManualGroup(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = templateDetailState.detail
    if (!detail) return
    const title = templateManualDraft.title.trim()
    if (!title) {
      setTemplateError('请填写要创建的自定义组合标题')
      return
    }
    setTemplateError(null)
    setTemplateSaving(true)
    createManualGroupFromScenarioTemplate(detail.template_id, {
      title,
      goal: templateManualDraft.goal.trim(),
      note: templateManualDraft.note.trim(),
      scenario: detail.scenario,
      status: 'active',
      renew_within_days: templateManualDraft.renewWithinDays,
    })
      .then((manualDetail) => {
        applyManualDetail(manualDetail)
        setSelectedTemplateID(null)
        setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
        setSelectedManualGroupID(manualDetail.manual_group_id)
        setTemplateDetailPanel('overview')
        setManualDetailPanel('overview')
        setOpenState('manual_group_id', manualDetail.manual_group_id)
        setDecisionNotice(`已从模板创建自定义组合：${manualDetail.title}`)
      })
      .catch((error: unknown) => {
        setTemplateError(describeError(error, '从模板创建自定义组合失败'))
      })
      .finally(() => setTemplateSaving(false))
  }

  function updateTemplateStatus(status: AssetDecisionScenarioTemplateStatus) {
    const detail = templateDetailState.detail
    if (!detail || detail.builtin) return
    setTemplateError(null)
    setPendingTemplateStatus(null)
    setTemplateSaving(true)
    patchAssetDecisionScenarioTemplate(detail.template_id, { status })
      .then((updated) => {
        applyTemplateDetail(updated)
        setDecisionNotice(`模板状态已更新：${updated.title} -> ${SCENARIO_TEMPLATE_STATUS_LABELS[updated.status]}`)
      })
      .catch((error: unknown) => {
        setTemplateError(describeError(error, '更新模板状态失败'))
      })
      .finally(() => setTemplateSaving(false))
  }

  function requestTemplateStatusUpdate(status: AssetDecisionScenarioTemplateStatus) {
    const detail = templateDetailState.detail
    if (!detail || detail.builtin || templateSaving) return
    setTemplateError(null)
    setPendingTemplateStatus(status)
    setTemplateDetailPanel('status')
  }

  function cancelTemplateStatusUpdate() {
    setPendingTemplateStatus(null)
    setTemplateError(null)
  }

  function saveManualGroupAsTemplate(detail: AssetDecisionManualGroupDetail) {
    setTemplateError(null)
    setManualGroupError(null)
    setTemplateSaving(true)
    createAssetDecisionScenarioTemplate({
      source_manual_group_id: detail.manual_group_id,
      title: `${detail.title} 模板`,
      scenario: detail.scenario,
      goal: detail.goal,
      note: detail.note,
    })
      .then((template) => {
        applyTemplateDetail(template)
        setSelectedManualGroupID(null)
        setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
        setSelectedTemplateID(template.template_id)
        setManualDetailPanel('overview')
        setTemplateDetailPanel('overview')
        setOpenState('template_id', template.template_id)
        setDecisionNotice(`已另存为场景模板：${template.title}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '另存为场景模板失败'))
      })
      .finally(() => setTemplateSaving(false))
  }

  function submitRecordStatus(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = recordDetailState.detail
    if (!detail) return
    setRecordPatchError(null)
    setRecordPatching(true)
    patchAssetDecisionRecord(detail.record_id, { status: recordPatchStatus })
      .then((record) => {
        setRecordDetailState({ loading: false, error: null, detail: record })
        setRecordPatchStatus(record.status)
        setRecordsState((current) => ({
          loading: false,
          error: null,
          records: current.records.map((item) => (item.record_id === record.record_id ? record : item)),
        }))
        setDecisionNotice(`决策记录状态已更新：${record.title} -> ${RECORD_STATUS_LABELS[record.status]}`)
      })
      .catch((error: unknown) => {
        setRecordPatchError(describeError(error, '更新决策记录状态失败'))
      })
      .finally(() => setRecordPatching(false))
  }

  function submitManualGroupPatch(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = manualDetailState.detail
    if (!detail) return
    const form = new FormData(event.currentTarget)
    const title = String(form.get('title') ?? '').trim()
    if (!title) {
      setManualGroupError('请填写自定义组合标题')
      return
    }
    setManualGroupError(null)
    setManualGroupSaving(true)
    patchAssetDecisionManualGroup(detail.manual_group_id, {
      title,
      goal: String(form.get('goal') ?? '').trim(),
      note: String(form.get('note') ?? '').trim(),
      scenario: String(form.get('scenario') ?? detail.scenario) as AssetDecisionManualGroupScenario,
      status: String(form.get('status') ?? detail.status) as AssetDecisionManualGroupStatus,
    })
      .then((updated) => {
        applyManualDetail(updated)
        setDecisionNotice(`自定义组合已更新：${updated.title}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '更新自定义组合失败'))
      })
      .finally(() => setManualGroupSaving(false))
  }

  function setManualMemberAddAdvancedVisible(next: boolean) {
    setManualMemberAddAdvanced(next)
    if (!next) {
      setManualMemberAddDraft((current) => ({
        ...current,
        reason: '',
        note: '',
        sortOrder: '',
      }))
    }
  }

  function selectManualDetailPanel(panel: ManualDetailPanel) {
    if (panel === 'add') {
      setManualMemberAddAdvancedVisible(false)
    }
    setManualDetailPanel(panel)
  }

  function submitManualMemberAdd(event: FormSubmitEvent) {
    event.preventDefault()
    const detail = manualDetailState.detail
    if (!detail) return
    const vpsID = manualMemberAddDraft.vpsID.trim()
    if (!vpsID) {
      setManualGroupError('请选择要加入组合的 VPS')
      return
    }
    const parsedSortOrder = Number.parseInt(manualMemberAddDraft.sortOrder, 10)
    setManualGroupError(null)
    setManualGroupSaving(true)
    addAssetDecisionManualGroupMember(detail.manual_group_id, {
      vps_id: vpsID,
      intended_role: manualMemberAddDraft.intendedRole,
      intended_action: manualMemberAddDraft.intendedAction,
      reason: manualMemberAddDraft.reason.trim(),
      note: manualMemberAddDraft.note.trim(),
      ...(Number.isFinite(parsedSortOrder) ? { sort_order: parsedSortOrder } : {}),
    })
      .then((updated) => {
        applyManualDetail(updated)
        setManualMemberAddDraft({
          vpsID: '',
          intendedRole: 'observe_candidate',
          intendedAction: 'review',
          reason: '',
          note: '',
          sortOrder: '',
        })
        setManualMemberAddAdvanced(false)
        setDecisionNotice('自定义组合成员已加入')
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '新增自定义组合成员失败'))
      })
      .finally(() => setManualGroupSaving(false))
  }

  function deleteManualMember(member: AssetDecisionManualGroupMember) {
    const detail = manualDetailState.detail
    if (!detail) return
    setManualGroupError(null)
    setPendingManualMemberRemoval(null)
    setManualMemberSaving((current) => ({ ...current, [member.vps_id]: true }))
    deleteAssetDecisionManualGroupMember(detail.manual_group_id, member.vps_id)
      .then((updated) => {
        applyManualDetail(updated)
        setDecisionNotice(`成员已移出自定义组合：${member.current_fact_found ? member.vps.display_name : member.vps_id}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '移除成员失败'))
      })
      .finally(() => {
        setManualMemberSaving((current) => ({ ...current, [member.vps_id]: false }))
      })
  }

  function requestManualMemberRemoval(member: AssetDecisionManualGroupMember) {
    if (manualMemberSaving[member.vps_id]) return
    setManualGroupError(null)
    setPendingManualMemberRemoval(member)
  }

  function cancelManualMemberRemoval() {
    setPendingManualMemberRemoval(null)
    setManualGroupError(null)
  }

  function updateRecordFollowupDraft(vpsID: string, patch: Partial<RecordFollowupDraft>) {
    setRecordFollowupDrafts((current) => ({
      ...current,
      [vpsID]: {
        ...current[vpsID],
        ...patch,
      },
    }))
  }

  function submitRecordMemberFollowup(event: FormSubmitEvent, member: AssetDecisionRecordMember) {
    event.preventDefault()
    saveRecordMemberFollowup(member)
  }

  function saveRecordMemberFollowup(member: AssetDecisionRecordMember, nextStatus?: AssetDecisionFollowupStatus) {
    const detail = recordDetailState.detail
    if (!detail) return
    const draft = recordFollowupDrafts[member.vps_id] ?? {
      status: member.followup_status,
      note: member.followup_note,
    }
    const status = nextStatus ?? draft.status
    setRecordPatchError(null)
    setRecordFollowupPatching((current) => ({ ...current, [member.vps_id]: true }))
    patchAssetDecisionRecord(detail.record_id, {
      members: [{
        vps_id: member.vps_id,
        followup_status: status,
        followup_note: draft.note.trim(),
      }],
    })
      .then((record) => {
        setRecordDetailState({ loading: false, error: null, detail: record })
        setRecordPatchStatus(record.status)
        setRecordFollowupDrafts(buildRecordFollowupDrafts(record))
        setRecordsState((current) => ({
          loading: false,
          error: null,
          records: current.records.map((item) => (item.record_id === record.record_id ? record : item)),
        }))
        setDecisionNotice(`成员跟进已更新：${member.display_name || member.vps_id} -> ${FOLLOWUP_STATUS_LABELS[status]}`)
      })
      .catch((error: unknown) => {
        setRecordPatchError(describeError(error, '更新成员跟进失败'))
      })
      .finally(() => {
        setRecordFollowupPatching((current) => ({
          ...current,
          [member.vps_id]: false,
        }))
      })
  }

  function renderRecordExecutionBoard(detail: AssetDecisionRecordDetail) {
    const executionPreview = previewItems(sortedExecutionMembers(detail.members))
    const visibleExecutionMembers = executionPreview.visible

    if (visibleExecutionMembers.length === 0) {
      return (
        <section className="asset-decision-execution-board" aria-label="执行编排">
          <div className="asset-decision-execution-board__empty">
            <strong>暂无执行编排</strong>
          </div>
        </section>
      )
    }

    return (
      <section className="asset-decision-execution-board" aria-label="执行编排">
        <div className="asset-decision-execution-board__header">
          <div>
            <strong>{detail.execution_plan?.summary || '等待执行步骤'}</strong>
          </div>
          <div className="asset-decision-execution-board__counts">
            <span>
              可推进 {detail.execution_plan?.actionable_count ?? 0}
            </span>
            {detail.execution_plan?.blocked_count > 0 && (
              <Badge variant="count" tone="critical">
                阻塞 {detail.execution_plan.blocked_count}
              </Badge>
            )}
          </div>
        </div>
        <div className="asset-decision-execution-board__members">
          {visibleExecutionMembers.map((member) => {
            const isSaving = Boolean(recordFollowupPatching[member.vps_id])
            const canStart = member.followup_status === 'todo'
            const canMarkDone = member.execution_readback?.status === 'aligned' && member.followup_status !== 'done' && member.followup_status !== 'skipped'
            const primaryAction = (() => {
              if (canMarkDone) {
                return (
                  <button className="btn sm primary" type="button" disabled={isSaving} onClick={() => saveRecordMemberFollowup(member, 'done')}>
                    标记完成
                  </button>
                )
              }
              if (member.execution_plan?.step_kind === 'review_record') {
                return (
                  <button className="btn sm secondary" type="button" onClick={() => setDecisionNotice(`请在当前记录中复核：${member.display_name || member.vps_id}`)}>
                    {actionLabelForMember(member)}
                  </button>
                )
              }
              if (member.execution_plan?.step_kind) {
                return (
                  <Link className="btn sm secondary" to={actionHrefForMember(member)}>
                    {actionLabelForMember(member)}
                  </Link>
                )
              }
              if (canStart) {
                return (
                  <button className="btn sm secondary" type="button" disabled={isSaving} onClick={() => saveRecordMemberFollowup(member, 'in_progress')}>
                    开始跟进
                  </button>
                )
              }
              return null
            })()
            return (
              <article key={member.vps_id} className="asset-decision-execution-card">
                <div className="asset-decision-execution-card__head">
                  <strong>{member.display_name || member.vps_id}</strong>
                  <span className="asset-decision-chip-row">
                    {renderExecutionPlanBadge(member.execution_plan)}
                    {renderReadbackBadge(member.execution_readback)}
                  </span>
                </div>
                <p>{compactMemberPlanSummary(member)}</p>
                {member.execution_readback?.issues?.length > 0 && (
                  <span className="asset-decision-chip-row">
                    {member.execution_readback.issues.slice(0, 2).map((issue) => (
                      <Badge key={`${member.vps_id}-${issue.kind}-${issue.label}`} variant="info" tone={chipTone(issue.tone)}>
                        {issue.label}
                      </Badge>
                    ))}
                    {member.execution_readback.issues.length > 2 && (
                      <span>
                        +{member.execution_readback.issues.length - 2}
                      </span>
                    )}
                  </span>
                )}
                <div className="asset-decision-execution-card__actions">
                  {primaryAction}
                </div>
              </article>
            )
          })}
        </div>
        {executionPreview.hiddenCount > 0 && (
          <div className="asset-decision-preview-more" role="note">
            另有 {executionPreview.hiddenCount} 台在成员跟进或底稿中查看
          </div>
        )}
      </section>
    )
  }

  function selectVPS(vps: VPSAssetRecord) {
    setSelectedVPS(vps)
    setDecisionDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setDecisionError(null)
    setDecisionNotice(null)
    if (selectedGroupID) setGroupDetailPanel('vps')
    if (selectedManualGroupID) setManualDetailPanel('add')
  }

  function navigateToVPS(vps: VPSAssetRecord) {
    route.commands.navigateToVPS(vps.vps_id)
  }

  function navigateToVPSSubscription(vpsID: string) {
    route.commands.navigateToVPSSubscription(vpsID)
  }

  function openPortfolioLead() {
    if (portfolioLead.kind === 'stable') return
    if (portfolioLead.primaryItem) {
      openNextWorkItem(portfolioLead.primaryItem)
      return
    }
    if (portfolioLead.primaryGroupID) {
      openGroup(portfolioLead.primaryGroupID)
      return
    }
    setWorkbenchView('needs_decision')
  }

  function openRecordSource(record: AssetDecisionRecordSummary) {
    if (record.source_type === 'manual_group') {
      openManualGroup(record.source_group_id)
      return
    }
    if (record.source_type === 'auto_group') {
      openGroup(record.source_group_id)
      return
    }
    setDecisionNotice(`记录来源仅作历史快照：${record.source_group_id}`)
  }

  function closeDecisionDrawer() {
    setSelectedVPS(null)
    setDecisionDraft(INITIAL_DECISION_DRAFT)
    setDecisionError(null)
    if (selectedGroupID) setGroupDetailPanel('members')
  }

  function handleDecisionSubmit(event: FormSubmitEvent) {
    event.preventDefault()
    if (!selectedVPS) return
    setDecisionError(null)
    setDecisionNotice(null)

    if (decisionDraft.renewalDecision === selectedVPS.renewal_decision) {
      setDecisionError('请选择一个不同的续费决策')
      return
    }

    const reason = decisionDraft.reason.trim()
    setDecisionSubmitting(true)
    updateVPSAsset(selectedVPS.vps_id, {
      renewal_decision: decisionDraft.renewalDecision,
      ...(reason ? { renewal_reason: reason } : {}),
    })
      .then((updated) => {
        setQueueState((current) => ({
          ...current,
          ...updateDecisionQueues(current, updated),
          subscriptions: current.subscriptions.map((subscription) =>
            updated.renewal_subscription_linkage?.updated && subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
              ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
              : subscription,
          ),
          renewals: current.renewals.map((subscription) =>
            updated.renewal_subscription_linkage?.updated && subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
              ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
              : subscription,
          ),
        }))
        closeDecisionDrawer()
        setPortfolioState((current) => ({
          ...current,
          overviewLoading: true,
          overviewError: null,
          groupsLoading: true,
          groupsError: null,
        }))
        setQueueState((current) => ({
          ...current,
          renewalsLoading: true,
          renewalsError: null,
          queueLoading: true,
          queueError: null,
        }))
        if (selectedGroupID) {
          setDetailState((current) => ({
            ...current,
            loading: true,
            error: null,
          }))
        }
        setRefreshToken((current) => current + 1)
        const baseNotice = `续费决策已保存：${updated.display_name} -> ${renewalQueueLabel(updated.renewal_decision)}`
        const linkageMessage = updated.renewal_subscription_linkage?.message
        setDecisionNotice(linkageMessage ? `${baseNotice}。${linkageMessage}` : baseNotice)
      })
      .catch((error: unknown) => {
        setDecisionError(describeError(error, '更新续费决策失败'))
      })
      .finally(() => setDecisionSubmitting(false))
  }

  return (
    <div className="animate-in asset-decision-workbench">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">决策台 · DECISIONS</div>
          <h1 className="page-title">资产组合决策</h1>
        </div>
        <div className="header-actions">
          <Link className="btn md secondary" to="/vps">VPS 库存</Link>
          <Link className="btn md secondary" to="/subscriptions">订阅列表</Link>
        </div>
      </div>

      {decisionNotice && (
        <div className="inline-alert ok" role="status">{decisionNotice}</div>
      )}

      <PortfolioWorkbench
        portfolioView={portfolioView}
        renewalWindow={renewalWindow}
        portfolioState={portfolioState}
        portfolioLead={portfolioLead}
        contextFilterChips={contextFilterChips}
        closedLoopPartialErrors={closedLoopPartialErrors}
        closedLoopAnomalies={closedLoopMetrics.readbackDriftCount + closedLoopMetrics.readbackBlockedCount + closedLoopMetrics.readbackNeedsEvidenceCount}
        partialErrorCount={closedLoopMetrics.partialErrorCount}
        onSetWorkbenchView={setWorkbenchView}
        onChangeRenewalWindow={changeRenewalWindow}
        onOpenGroup={openGroup}
        onOpenPortfolioLead={openPortfolioLead}
        onClearContextFilter={clearContextFilter}
        onClearAllContextFilters={clearAllContextFilters}
      />

      <SecondaryWorkbenches
        secondaryWorkbench={secondaryWorkbench}
        secondaryNavItems={secondaryNavItems}
        queueView={queueView}
        renewalWindow={renewalWindow}
        queueState={queueState}
        visibleDecisionQueue={visibleDecisionQueue}
        totalDecisionQueue={totalDecisionQueue}
        renewalDueQueueCount={renewalDueQueueCount}
        missingSubscriptionCount={missingSubscriptionCount}
        unlinkedCount={unlinkedCount}
        cancellationAttentionCount={cancellationAttentionCount}
        manualGroupsState={manualGroupsState}
        templatesState={templatesState}
        recordsState={recordsState}
        vpsByID={vpsByID}
        onSetSelectedSecondaryWorkbench={setSelectedSecondaryWorkbench}
        onSetQueueView={setQueueView}
        onSelectVPS={selectVPS}
        onNavigateToVPS={navigateToVPS}
        onNavigateToVPSSubscription={navigateToVPSSubscription}
        onOpenManualGroup={openManualGroup}
        onOpenTemplate={openTemplate}
        onOpenRecord={openRecord}
        hasCancellationAttention={hasCancellationAttention}
        subscriptionCostAttention={subscriptionCostAttention}
      />

      <GroupDetailModal
        open={selectedGroupID != null}
        detailState={detailState}
        groupDetailPanel={groupDetailPanel}
        onSetGroupDetailPanel={setGroupDetailPanel}
        recordDraft={recordDraft}
        onSetRecordDraft={setRecordDraft}
        recordSaving={recordSaving}
        recordSaveError={recordSaveError}
        manualGroupCreating={manualGroupCreating}
        manualGroupError={manualGroupError}
        onClose={closeGroupDetail}
        onStartRecordSave={startRecordSave}
        onSubmitRecordSave={submitRecordSave}
        onCancelRecordSave={cancelRecordSave}
        onCreateManualGroupFromAuto={createManualGroupFromAuto}
        selectedVPS={selectedVPS}
        decisionDraft={decisionDraft}
        onSetDecisionDraft={setDecisionDraft}
        decisionSubmitting={decisionSubmitting}
        decisionError={decisionError}
        onSelectVPS={selectVPS}
        onCloseDecisionDrawer={closeDecisionDrawer}
        onHandleDecisionSubmit={handleDecisionSubmit}
        memberColumns={memberColumns}
        renderRecordDraftMemberRows={renderRecordDraftMemberRows}
      />

      <ManualGroupDetailModal
        open={selectedManualGroupID != null}
        manualDetailState={manualDetailState}
        manualDetailPanel={manualDetailPanel}
        manualGroupProgress={manualGroupProgress}
        manualGroupError={manualGroupError}
        manualGroupSaving={manualGroupSaving}
        templateSaving={templateSaving}
        manualMemberSaving={manualMemberSaving}
        pendingManualMemberRemoval={pendingManualMemberRemoval}
        manualMemberAddDraft={manualMemberAddDraft}
        manualMemberAddAdvanced={manualMemberAddAdvanced}
        vpsCatalogState={vpsCatalogState}
        manualMemberCandidateRows={manualMemberCandidateRows}
        recordDraft={recordDraft}
        recordSaving={recordSaving}
        recordSaveError={recordSaveError}
        onClose={closeManualGroupDetail}
        onSelectManualDetailPanel={selectManualDetailPanel}
        onStartManualRecordSave={startManualRecordSave}
        onSubmitRecordSave={submitRecordSave}
        onCancelRecordSave={cancelRecordSave}
        onSubmitManualGroupPatch={submitManualGroupPatch}
        onSaveManualGroupAsTemplate={saveManualGroupAsTemplate}
        onSubmitManualMemberAdd={submitManualMemberAdd}
        onRequestManualMemberRemoval={requestManualMemberRemoval}
        onCancelManualMemberRemoval={cancelManualMemberRemoval}
        onDeleteManualMember={deleteManualMember}
        onSetManualMemberAddDraft={setManualMemberAddDraft}
        onSetManualMemberAddAdvancedVisible={setManualMemberAddAdvancedVisible}
        onSetRecordDraft={setRecordDraft}
        manualMemberColumns={manualMemberColumns}
        renderRecordDraftMemberRows={renderRecordDraftMemberRows}
      />

      <TemplateDetailModal
        open={selectedTemplateID != null}
        templateDetailState={templateDetailState}
        templateDetailPanel={templateDetailPanel}
        templateError={templateError}
        templateSaving={templateSaving}
        pendingTemplateStatus={pendingTemplateStatus}
        templateManualDraft={templateManualDraft}
        onClose={closeTemplateDetail}
        onSetTemplateDetailPanel={setTemplateDetailPanel}
        onRequestTemplateStatusUpdate={requestTemplateStatusUpdate}
        onCancelTemplateStatusUpdate={cancelTemplateStatusUpdate}
        onUpdateTemplateStatus={updateTemplateStatus}
        onSubmitTemplateManualGroup={submitTemplateManualGroup}
        onSetTemplateManualDraft={setTemplateManualDraft}
      />

      <RenewalDecisionModal
        open={selectedVPS != null && selectedGroupID == null}
        selectedVPS={selectedVPS}
        decisionDraft={decisionDraft}
        submitting={decisionSubmitting}
        error={decisionError}
        onDraftChange={setDecisionDraft}
        onSubmit={handleDecisionSubmit}
        onClose={closeDecisionDrawer}
      />

      <RecordDetailModal
        open={selectedRecordID != null}
        recordDetailState={recordDetailState}
        recordDetailPanel={recordDetailPanel}
        recordPatchError={recordPatchError}
        recordPatchStatus={recordPatchStatus}
        recordPatching={recordPatching}
        selectedRecordAssessment={selectedRecordAssessment}
        onClose={closeRecordDetail}
        onSetRecordDetailPanel={setRecordDetailPanel}
        onSubmitRecordStatus={submitRecordStatus}
        onSetRecordPatchStatus={setRecordPatchStatus}
        onOpenRecordSource={openRecordSource}
        recordMemberColumns={recordMemberColumns}
        renderRecordExecutionBoard={renderRecordExecutionBoard}
        renderRecordMemberFollowupRows={renderRecordMemberFollowupRows}
      />
    </div>
  )
}
