import { type CSSProperties, type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { AssetDecisionRenewalTable } from '../components/AssetDecisionRenewalTable'
import {
  AssetDecisionWorkPanel,
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import {
  Badge,
  type BadgeTone,
  DataTable,
  type DataTableColumn,
  Modal,
  MonoDigits,
  Tabs,
} from '../components/atoms'
import { FilterChip } from '../components/filters'
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
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
  patchAssetDecisionManualGroupMember,
  patchAssetDecisionRecord,
  patchAssetDecisionScenarioTemplate,
  updateVPSAsset,
} from '../lib/api'
import { formatDate, formatDateTime, formatMoney, formatOptional } from '../lib/format'
import {
  type AssetDecisionEvidenceChip,
  type AssetDecisionEvidenceAssessment,
  type AssetDecisionEvidenceDecisionBias,
  type AssetDecisionEvidenceQualityTier,
  type AssetDecisionEvidenceSnapshot,
  type AssetDecisionExecutionCurrentFacts,
  type AssetDecisionExecutionPlanLane,
  type AssetDecisionExecutionPlanTone,
  type AssetDecisionExecutionReadbackStatus,
  type AssetDecisionGroupDetail,
  type AssetDecisionGroupMember,
  type AssetDecisionGroupSummary,
  type AssetDecisionManualGroupDetail,
  type AssetDecisionManualGroupMember,
  type AssetDecisionManualGroupScenario,
  type AssetDecisionManualGroupStatus,
  type AssetDecisionManualGroupSummary,
  type AssetDecisionMemberExecutionReadback,
  type AssetDecisionMemberExecutionPlan,
  type AssetDecisionRecommendation,
  type AssetDecisionRecordExecutionPlan,
  type AssetDecisionRecordExecutionReadback,
  type AssetDecisionOverview,
  type AssetDecisionRecordDetail,
  type AssetDecisionSourceType,
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
  type AssetDecisionView,
  type SubscriptionRecord,
  type VPSAssetRecord,
  type VPSRenewalDecision,
} from '../lib/types'
import {
  LifecycleBadge,
  RenewalBadge,
  SubscriptionStatusBadge,
  UsageBadge,
} from './assetPageBadges'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  groupSubscriptionsByVPS,
  isSubscriptionInRenewalWindow,
  lifecycleLabel,
  renewalLabel,
  selectPrimarySubscription,
  usageLabel,
  vpsLocationLabel,
  type AssetQualityIssue,
} from './assetPageUtils'

type RenewalWindow = 30 | 60 | 90
type WorkbenchView = AssetDecisionView | 'single_queue'
type MainWorkbenchView = AssetDecisionView
type DecisionQueueView =
  | 'all'
  | 'unreviewed'
  | 'renewal'
  | 'migrate'
  | 'cancel'
  | 'cancellation_attention'
  | 'unlinked'
  | 'missing_subscription'

type DecisionQueueItem = {
  vps: VPSAssetRecord
  subscription: SubscriptionRecord | null
  qualityIssues: AssetQualityIssue[]
  renewalDue: boolean
  priority: number
}

type PortfolioState = {
  overviewLoading: boolean
  overviewError: string | null
  overview: AssetDecisionOverview | null
  groupsLoading: boolean
  groupsError: string | null
  groups: AssetDecisionGroupSummary[]
}

type DetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionGroupDetail | null
}

type ManualGroupsState = {
  loading: boolean
  error: string | null
  groups: AssetDecisionManualGroupSummary[]
}

type ManualDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionManualGroupDetail | null
}

type ScenarioTemplatesState = {
  loading: boolean
  error: string | null
  templates: AssetDecisionScenarioTemplateSummary[]
}

type TemplateDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionScenarioTemplateDetail | null
}

type VPSCatalogState = {
  loading: boolean
  error: string | null
  rows: VPSAssetRecord[]
}

type RecordsState = {
  loading: boolean
  error: string | null
  records: AssetDecisionRecordSummary[]
}

type RecordDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionRecordDetail | null
}

type RecordMemberDraft = {
  decidedRole: AssetDecisionSuggestedRole
  decidedAction: AssetDecisionSuggestedAction
  reason: string
}

type RecordFollowupDraft = {
  status: AssetDecisionFollowupStatus
  note: string
}

type RecordDraft = {
  sourceType: AssetDecisionSourceType
  sourceGroupID: string
  renewWithinDays: number
  title: string
  goal: string
  status: AssetDecisionRecordStatus
  memberOrder: string[]
  members: Record<string, RecordMemberDraft>
}

type ManualMemberDraft = {
  intendedRole: AssetDecisionSuggestedRole
  intendedAction: AssetDecisionSuggestedAction
  reason: string
  note: string
  sortOrder: string
}

type ManualMemberAddDraft = ManualMemberDraft & {
  vpsID: string
}

type TemplateManualGroupDraft = {
  title: string
  goal: string
  note: string
  renewWithinDays: RenewalWindow
}

type ScoreStyle = CSSProperties & {
  '--score': number
}

type CostSummaryLike = {
  monthly_cost_by_currency: AssetDecisionGroupSummary['monthly_cost_by_currency']
  monthly_cost_base?: number | null
  yearly_cost_base?: number | null
  base_currency?: string
}

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

type ClosedLoopMetrics = {
  autoGroupCount: number
  manualActiveCount: number
  recordActiveCount: number
  readbackDriftCount: number
  readbackBlockedCount: number
  readbackNeedsEvidenceCount: number
  readbackOpenCount: number
  costPressureGroupCount: number
  evidenceGapGroupCount: number
  partialErrorCount: number
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

type AssetDecisionPortfolioLead = {
  tone: BadgeTone
  eyebrow: string
  title: string
  summary: string
  actionLabel: string
  contextLabel: string
  riskLabel: string
  evidenceLabel: string
  renewalLabel: string
  primaryItem?: AssetDecisionNextWorkItem
  primaryGroupID?: string
}

type AssetDecisionProgressStage =
  | 'discover'
  | 'compare'
  | 'decide'
  | 'execute'

type AssetDecisionPathStep = {
  stage: AssetDecisionProgressStage
  eyebrow: string
  title: string
  summary: string
  metric: string
  meta: string
  tone: BadgeTone
  actionLabel: string
  target?: AssetDecisionNextWorkTarget
  unavailable?: boolean
}

type ManualGroupProgressItem = {
  key: string
  label: string
  summary: string
  tone: BadgeTone
  done: boolean
}

type ManualGroupProgress = {
  readinessLabel: string
  readinessTone: BadgeTone
  readyToRecord: boolean
  doneCount: number
  totalCount: number
  items: ManualGroupProgressItem[]
}

const RENEWAL_WINDOWS: readonly RenewalWindow[] = [30, 60, 90]
const DECISION_QUEUE_VALUES: VPSRenewalDecision[] = ['unreviewed', 'migrate', 'cancel']
const INITIAL_DECISION_DRAFT: AssetDecisionDraft = {
  renewalDecision: 'unreviewed',
  reason: '',
}
const INITIAL_PORTFOLIO_STATE: PortfolioState = {
  overviewLoading: true,
  overviewError: null,
  overview: null,
  groupsLoading: true,
  groupsError: null,
  groups: [],
}
const INITIAL_DETAIL_STATE: DetailState = {
  loading: false,
  error: null,
  detail: null,
}
const INITIAL_MANUAL_GROUPS_STATE: ManualGroupsState = {
  loading: true,
  error: null,
  groups: [],
}
const INITIAL_MANUAL_DETAIL_STATE: ManualDetailState = {
  loading: false,
  error: null,
  detail: null,
}
const INITIAL_SCENARIO_TEMPLATES_STATE: ScenarioTemplatesState = {
  loading: true,
  error: null,
  templates: [],
}
const INITIAL_TEMPLATE_DETAIL_STATE: TemplateDetailState = {
  loading: false,
  error: null,
  detail: null,
}
const INITIAL_VPS_CATALOG_STATE: VPSCatalogState = {
  loading: true,
  error: null,
  rows: [],
}
const INITIAL_RECORDS_STATE: RecordsState = {
  loading: true,
  error: null,
  records: [],
}
const INITIAL_RECORD_DETAIL_STATE: RecordDetailState = {
  loading: false,
  error: null,
  detail: null,
}
const INITIAL_QUEUE_STATE: QueueState = {
  renewalsLoading: true,
  renewalsError: null,
  queueLoading: true,
  queueError: null,
  renewals: [],
  subscriptions: [],
  unreviewed: [],
  migrate: [],
  cancel: [],
}

const VIEW_LABELS: Record<WorkbenchView, string> = {
  needs_decision: '需要决策',
  renewal: '续费取舍',
  region: '同区比较',
  provider: '服务商组合',
  cost: '预算压力',
  evidence: '资料缺口',
  single_queue: '单台队列',
}

const ROLE_LABELS: Record<AssetDecisionSuggestedRole, string> = {
  primary_candidate: '主力候选',
  standby_candidate: '备用候选',
  observe_candidate: '观察候选',
  retire_candidate: '退役候选',
  evidence_needed: '补证据',
}

const ACTION_LABELS: Record<AssetDecisionSuggestedAction, string> = {
  review: '复核',
  keep: '保留',
  observe: '观察',
  migrate: '迁移',
  cancel: '取消',
  open_cancellation_workbench: '进入取消台',
  complete_evidence: '补齐资料',
}

const MANUAL_GROUP_STATUS_LABELS: Record<AssetDecisionManualGroupStatus, string> = {
  active: '进行中',
  archived: '已归档',
}

const SCENARIO_TEMPLATE_STATUS_LABELS: Record<AssetDecisionScenarioTemplateStatus, string> = {
  active: '启用',
  archived: '已归档',
}

const MANUAL_GROUP_SCENARIO_LABELS: Record<AssetDecisionManualGroupScenario, string> = {
  general: '通用组合',
  primary_standby: '主备取舍',
  budget_reduction: '预算压缩',
  provider_review: '服务商评估',
  region_review: '同区比较',
  migration_retirement: '迁移退役',
  evidence_cleanup: '资料清理',
}

const RECORD_STATUS_LABELS: Record<AssetDecisionRecordStatus, string> = {
  draft: '草稿',
  decided: '已决策',
  in_progress: '推进中',
  completed: '已完成',
  abandoned: '已放弃',
}

const FOLLOWUP_STATUS_LABELS: Record<AssetDecisionFollowupStatus, string> = {
  todo: '待处理',
  in_progress: '处理中',
  blocked: '阻塞',
  done: '已完成',
  skipped: '跳过',
}

const READBACK_STATUS_LABELS: Record<AssetDecisionExecutionReadbackStatus, string> = {
  open: '待回读',
  aligned: '已对齐',
  drift: '有漂移',
  blocked: '阻塞',
  needs_evidence: '需补证据',
  inactive: '不活跃',
}

const EXECUTION_PLAN_LANE_LABELS: Record<AssetDecisionExecutionPlanLane, string> = {
  cancel_retire: '取消退役',
  migration: '迁移',
  keep_observe: '保留观察',
  evidence: '补证据',
  review: '复核',
}

const EXECUTION_PLAN_LANE_ORDER: AssetDecisionExecutionPlanLane[] = [
  'cancel_retire',
  'migration',
  'keep_observe',
  'evidence',
  'review',
]

const EVIDENCE_TIER_LABELS: Record<AssetDecisionEvidenceQualityTier, string> = {
  strong: '证据强',
  usable: '可决策',
  weak: '证据弱',
  blocked: '先补证据',
}

const EVIDENCE_BIAS_LABELS: Record<AssetDecisionEvidenceDecisionBias, string> = {
  keep: '偏保留',
  observe: '偏观察',
  complete_evidence: '补证据',
  retire: '偏退役',
  migrate: '偏迁移',
  review: '待复核',
}

const ROLE_OPTIONS: ReadonlyArray<{ value: AssetDecisionSuggestedRole; label: string }> = [
  { value: 'primary_candidate', label: ROLE_LABELS.primary_candidate },
  { value: 'standby_candidate', label: ROLE_LABELS.standby_candidate },
  { value: 'observe_candidate', label: ROLE_LABELS.observe_candidate },
  { value: 'retire_candidate', label: ROLE_LABELS.retire_candidate },
  { value: 'evidence_needed', label: ROLE_LABELS.evidence_needed },
]

const ACTION_OPTIONS: ReadonlyArray<{ value: AssetDecisionSuggestedAction; label: string }> = [
  { value: 'review', label: ACTION_LABELS.review },
  { value: 'keep', label: ACTION_LABELS.keep },
  { value: 'observe', label: ACTION_LABELS.observe },
  { value: 'migrate', label: ACTION_LABELS.migrate },
  { value: 'cancel', label: ACTION_LABELS.cancel },
  { value: 'open_cancellation_workbench', label: ACTION_LABELS.open_cancellation_workbench },
  { value: 'complete_evidence', label: ACTION_LABELS.complete_evidence },
]

const MANUAL_GROUP_SCENARIO_OPTIONS: ReadonlyArray<{ value: AssetDecisionManualGroupScenario; label: string }> = [
  { value: 'general', label: MANUAL_GROUP_SCENARIO_LABELS.general },
  { value: 'primary_standby', label: MANUAL_GROUP_SCENARIO_LABELS.primary_standby },
  { value: 'budget_reduction', label: MANUAL_GROUP_SCENARIO_LABELS.budget_reduction },
  { value: 'provider_review', label: MANUAL_GROUP_SCENARIO_LABELS.provider_review },
  { value: 'region_review', label: MANUAL_GROUP_SCENARIO_LABELS.region_review },
  { value: 'migration_retirement', label: MANUAL_GROUP_SCENARIO_LABELS.migration_retirement },
  { value: 'evidence_cleanup', label: MANUAL_GROUP_SCENARIO_LABELS.evidence_cleanup },
]

const RECORD_STATUS_OPTIONS: ReadonlyArray<{ value: AssetDecisionRecordStatus; label: string }> = [
  { value: 'draft', label: RECORD_STATUS_LABELS.draft },
  { value: 'decided', label: RECORD_STATUS_LABELS.decided },
  { value: 'in_progress', label: RECORD_STATUS_LABELS.in_progress },
  { value: 'completed', label: RECORD_STATUS_LABELS.completed },
  { value: 'abandoned', label: RECORD_STATUS_LABELS.abandoned },
]

const FOLLOWUP_STATUS_OPTIONS: ReadonlyArray<{ value: AssetDecisionFollowupStatus; label: string }> = [
  { value: 'todo', label: FOLLOWUP_STATUS_LABELS.todo },
  { value: 'in_progress', label: FOLLOWUP_STATUS_LABELS.in_progress },
  { value: 'blocked', label: FOLLOWUP_STATUS_LABELS.blocked },
  { value: 'done', label: FOLLOWUP_STATUS_LABELS.done },
  { value: 'skipped', label: FOLLOWUP_STATUS_LABELS.skipped },
]

const CONTEXT_FILTER_KEYS = ['provider_id', 'vps_id', 'country', 'region', 'city', 'scenario'] as const
const OPEN_STATE_KEYS = ['group_id', 'manual_group_id', 'record_id', 'template_id'] as const

type ContextFilterKey = typeof CONTEXT_FILTER_KEYS[number]
type OpenStateKey = typeof OPEN_STATE_KEYS[number]

type ContextFilterChip = {
  key: ContextFilterKey
  label: string
  value: string
}

const WORKBENCH_TABS: ReadonlyArray<{ value: MainWorkbenchView; label: string }> = [
  { value: 'needs_decision', label: '需要决策' },
  { value: 'renewal', label: '续费取舍' },
  { value: 'region', label: '同区比较' },
  { value: 'provider', label: '服务商组合' },
  { value: 'cost', label: '预算压力' },
  { value: 'evidence', label: '资料缺口' },
]

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseRenewalWindow(value?: string | null): RenewalWindow {
  const parsed = Number.parseInt(value ?? '', 10)
  return RENEWAL_WINDOWS.includes(parsed as RenewalWindow) ? (parsed as RenewalWindow) : 30
}

function parseWorkbenchView(value?: string | null): WorkbenchView {
  switch (value) {
    case 'renewal_attention':
    case 'renewal':
      return 'renewal'
    case 'region_portfolio':
    case 'region':
      return 'region'
    case 'provider_portfolio':
    case 'provider':
      return 'provider'
    case 'cost_pressure':
    case 'cost':
      return 'cost'
    case 'evidence_gap':
    case 'evidence':
      return 'evidence'
    case 'single_queue':
      return 'single_queue'
    case 'needs_decision':
    default:
      return 'needs_decision'
  }
}

function portfolioViewForWorkbench(view: WorkbenchView): MainWorkbenchView {
  return view === 'single_queue' ? 'needs_decision' : view
}

function buildAssetDecisionFilter(searchParams: URLSearchParams, view: MainWorkbenchView, renewalWindow: RenewalWindow): AssetDecisionGroupListFilter {
  const scenario = parseScenario(searchParams.get('scenario'))
  return {
    view,
    renew_within_days: renewalWindow,
    provider_id: trimParam(searchParams.get('provider_id')),
    vps_id: trimParam(searchParams.get('vps_id')),
    country: trimParam(searchParams.get('country')),
    region: trimParam(searchParams.get('region')),
    city: trimParam(searchParams.get('city')),
    scenario,
  }
}

function parseScenario(value: string | null): AssetDecisionManualGroupScenario | undefined {
  const normalized = trimParam(value)
  if (!normalized) return undefined
  return MANUAL_GROUP_SCENARIO_OPTIONS.some((option) => option.value === normalized)
    ? normalized as AssetDecisionManualGroupScenario
    : undefined
}

function trimParam(value: string | null): string | undefined {
  const trimmed = value?.trim()
  return trimmed || undefined
}

function buildContextFilterChips(filter: AssetDecisionGroupListFilter): ContextFilterChip[] {
  const chips: ContextFilterChip[] = []
  if (filter.provider_id) chips.push({ key: 'provider_id', label: '服务商', value: filter.provider_id })
  if (filter.vps_id) chips.push({ key: 'vps_id', label: 'VPS', value: filter.vps_id })
  if (filter.country) chips.push({ key: 'country', label: '国家', value: filter.country })
  if (filter.region) chips.push({ key: 'region', label: '区域', value: filter.region })
  if (filter.city) chips.push({ key: 'city', label: '城市', value: filter.city })
  if (filter.scenario) chips.push({ key: 'scenario', label: '场景', value: MANUAL_GROUP_SCENARIO_LABELS[filter.scenario] })
  return chips
}

function updateDecisionQueues(
  state: QueueState,
  updated: VPSAssetRecord,
): Pick<QueueState, 'unreviewed' | 'migrate' | 'cancel'> {
  const next = {
    unreviewed: state.unreviewed.filter((vps) => vps.vps_id !== updated.vps_id),
    migrate: state.migrate.filter((vps) => vps.vps_id !== updated.vps_id),
    cancel: state.cancel.filter((vps) => vps.vps_id !== updated.vps_id),
  }

  if (updated.renewal_decision === 'unreviewed') next.unreviewed = [updated, ...next.unreviewed]
  if (updated.renewal_decision === 'migrate') next.migrate = [updated, ...next.migrate]
  if (updated.renewal_decision === 'cancel') next.cancel = [updated, ...next.cancel]

  return next
}

function renewalQueueLabel(value: VPSRenewalDecision): string {
  return DECISION_QUEUE_VALUES.includes(value) ? renewalLabel(value) : '已处理'
}

function buildDecisionQueue(
  vpsRows: VPSAssetRecord[],
  subscriptionsByVPS: Map<string, SubscriptionRecord[]>,
  renewalWindow: RenewalWindow,
): DecisionQueueItem[] {
  const uniqueRows = new Map<string, VPSAssetRecord>()
  for (const vps of vpsRows) uniqueRows.set(vps.vps_id, vps)

  return [...uniqueRows.values()]
    .map((vps) => {
      const subscription = selectPrimarySubscription(subscriptionsByVPS, vps.vps_id)
      const qualityIssues = buildVPSQualityIssues(vps, subscription)
      const renewalDue = isSubscriptionInRenewalWindow(subscription, renewalWindow)
      return {
        vps,
        subscription,
        qualityIssues,
        renewalDue,
        priority: queuePriority(vps, subscription, qualityIssues, renewalDue),
      }
    })
    .sort((left, right) => {
      if (left.priority !== right.priority) return right.priority - left.priority
      const leftDays = daysUntilDate(left.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      const rightDays = daysUntilDate(right.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      if (leftDays !== rightDays) return leftDays - rightDays
      return left.vps.display_name.localeCompare(right.vps.display_name)
    })
}

function queuePriority(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
  qualityIssues: AssetQualityIssue[],
  renewalDue: boolean,
): number {
  let priority = 0
  if (vps.renewal_decision === 'unreviewed') priority += 500
  if (renewalDue) priority += 300
  if (vps.renewal_decision === 'migrate' || vps.renewal_decision === 'cancel') priority += 180
  if (subscription?.exchange_rate_stale) priority += 60
  if (vps.active_monitoring_instance_link_count <= 0) priority += 90
  if (!subscription) priority += 80
  return priority + qualityIssues.length * 8
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

function baseMoney(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '—'
  return formatMoney(value, currency)
}

function subscriptionCostAttention(subscription: SubscriptionRecord | null): boolean {
  return Boolean(subscription?.exchange_rate_stale)
}

function chipTone(tone?: string): BadgeTone {
  if (tone === 'normal' || tone === 'notice' || tone === 'alert' || tone === 'critical' || tone === 'maintenance' || tone === 'offline') {
    return tone
  }
  return 'neutral'
}

function roleTone(role: AssetDecisionSuggestedRole): BadgeTone {
  if (role === 'primary_candidate') return 'normal'
  if (role === 'standby_candidate' || role === 'observe_candidate') return 'maintenance'
  if (role === 'retire_candidate') return 'critical'
  return 'notice'
}

function actionTone(action: AssetDecisionSuggestedAction): BadgeTone {
  if (action === 'keep') return 'normal'
  if (action === 'open_cancellation_workbench' || action === 'cancel') return 'critical'
  if (action === 'migrate' || action === 'observe') return 'maintenance'
  return 'notice'
}

function manualGroupStatusTone(status: AssetDecisionManualGroupStatus): BadgeTone {
  return status === 'active' ? 'maintenance' : 'offline'
}

function scenarioForGroup(group: Pick<AssetDecisionGroupSummary, 'group_type'>): AssetDecisionManualGroupScenario {
  if (group.group_type === 'region_portfolio') return 'region_review'
  if (group.group_type === 'provider_portfolio') return 'provider_review'
  if (group.group_type === 'cost_pressure') return 'budget_reduction'
  if (group.group_type === 'cancellation_attention') return 'migration_retirement'
  if (group.group_type === 'evidence_gap') return 'evidence_cleanup'
  return 'general'
}

function recordStatusTone(status: AssetDecisionRecordStatus): BadgeTone {
  if (status === 'completed') return 'normal'
  if (status === 'in_progress') return 'maintenance'
  if (status === 'decided') return 'notice'
  if (status === 'abandoned') return 'offline'
  return 'neutral'
}

function followupStatusTone(status: AssetDecisionFollowupStatus): BadgeTone {
  if (status === 'done' || status === 'skipped') return 'normal'
  if (status === 'blocked') return 'critical'
  if (status === 'in_progress') return 'maintenance'
  return 'notice'
}

function readbackStatusTone(status?: AssetDecisionExecutionReadbackStatus): BadgeTone {
  if (status === 'aligned') return 'normal'
  if (status === 'drift') return 'critical'
  if (status === 'blocked') return 'critical'
  if (status === 'needs_evidence') return 'alert'
  if (status === 'inactive') return 'offline'
  if (status === 'open') return 'maintenance'
  return 'neutral'
}

function executionPlanTone(tone?: AssetDecisionExecutionPlanTone): BadgeTone {
  if (tone === 'normal') return 'normal'
  if (tone === 'critical') return 'critical'
  if (tone === 'alert') return 'alert'
  if (tone === 'notice') return 'maintenance'
  return 'neutral'
}

function recordFollowupDoneCount(record: AssetDecisionRecordSummary): number {
  return (record.followup_done_count ?? 0) + (record.followup_skipped_count ?? 0)
}

function recordFollowupOpenCount(record: AssetDecisionRecordSummary): number {
  return (record.followup_todo_count ?? 0) + (record.followup_in_progress_count ?? 0) + (record.followup_blocked_count ?? 0)
}

function readbackCountSummary(readback?: AssetDecisionRecordExecutionReadback): string {
  if (!readback) return '等待回读'
  const parts = [
    readback.drift_count > 0 ? `漂移 ${readback.drift_count}` : '',
    readback.blocked_count > 0 ? `阻塞 ${readback.blocked_count}` : '',
    readback.needs_evidence_count > 0 ? `缺口 ${readback.needs_evidence_count}` : '',
    readback.open_count > 0 ? `待处理 ${readback.open_count}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : `对齐 ${readback.aligned_count ?? 0}`
}

function executionPlanCountSummary(plan?: AssetDecisionRecordExecutionPlan): string {
  if (!plan) return '等待编排'
  const laneSummary = (plan.lane_counts ?? [])
    .filter((item) => item.count > 0)
    .map((item) => `${EXECUTION_PLAN_LANE_LABELS[item.lane] ?? item.lane} ${item.count}`)
  const actionSummary = plan.actionable_count > 0 ? `可推进 ${plan.actionable_count}` : '无待推进'
  const blockedSummary = plan.blocked_count > 0 ? `阻塞 ${plan.blocked_count}` : ''
  return [actionSummary, blockedSummary, ...laneSummary].filter(Boolean).join(' · ')
}

function scenarioTemplateStatusTone(status: AssetDecisionScenarioTemplateStatus): BadgeTone {
  return status === 'active' ? 'maintenance' : 'offline'
}

function countActiveTemplates(templates: AssetDecisionScenarioTemplateSummary[]): number {
  return templates.filter((template) => template.status === 'active').length
}

function countRecordExecutionRisk(records: AssetDecisionRecordSummary[]): number {
  return records.reduce((total, record) => (
    total
    + (record.execution_readback?.drift_count ?? 0)
    + (record.execution_readback?.blocked_count ?? 0)
    + (record.execution_readback?.needs_evidence_count ?? 0)
  ), 0)
}

function deriveClosedLoopMetrics(
  groups: AssetDecisionGroupSummary[],
  records: AssetDecisionRecordSummary[],
  manualGroups: AssetDecisionManualGroupSummary[],
  sourceErrors: ClosedLoopSourceErrors,
  overview?: AssetDecisionOverview | null,
): ClosedLoopMetrics {
  return {
    autoGroupCount: groups.length,
    manualActiveCount: manualGroups.filter((group) => group.status === 'active').length,
    recordActiveCount: records.filter((record) => record.status !== 'completed' && record.status !== 'abandoned').length,
    readbackDriftCount: records.reduce((total, record) => total + (record.execution_readback?.drift_count ?? 0), 0),
    readbackBlockedCount: records.reduce(
      (total, record) => total + (record.execution_readback?.blocked_count ?? 0) + (record.followup_blocked_count ?? 0),
      0,
    ),
    readbackNeedsEvidenceCount: records.reduce((total, record) => total + (record.execution_readback?.needs_evidence_count ?? 0), 0),
    readbackOpenCount: records.reduce((total, record) => total + (record.execution_readback?.open_count ?? 0), 0),
    costPressureGroupCount: overview?.cost_group_count ?? groups.filter((group) => group.view === 'cost' || group.group_type === 'cost_pressure').length,
    evidenceGapGroupCount: overview?.evidence_group_count ?? groups.filter((group) => group.view === 'evidence' || group.group_type === 'evidence_gap').length,
    partialErrorCount: Object.values(sourceErrors).filter(Boolean).length,
  }
}

function buildDecisionPathSteps(
  groups: AssetDecisionGroupSummary[],
  records: AssetDecisionRecordSummary[],
  manualGroups: AssetDecisionManualGroupSummary[],
  templates: AssetDecisionScenarioTemplateSummary[],
  metrics: ClosedLoopMetrics,
  sourceErrors: ClosedLoopSourceErrors,
): AssetDecisionPathStep[] {
  const discoverGroup = groups[0]
  const compareGroup = manualGroups.find((group) => group.status === 'active') ?? manualGroups[0]
  const decideRecord = records.find((record) => record.status !== 'completed' && record.status !== 'abandoned') ?? records[0]
  const executeRecord = records.find((record) => (record.execution_readback?.drift_count ?? 0) > 0)
    ?? records.find((record) => (record.execution_readback?.blocked_count ?? 0) > 0 || (record.followup_blocked_count ?? 0) > 0)
    ?? records.find((record) => (record.execution_readback?.needs_evidence_count ?? 0) > 0)
    ?? decideRecord
  const activeTemplate = templates.find((template) => template.status === 'active')
  const activeTemplateCount = countActiveTemplates(templates)
  const executionRiskCount = countRecordExecutionRisk(records)

  return [
    {
      stage: 'discover',
      eyebrow: '01 DISCOVER',
      title: '发现组合压力',
      summary: sourceErrors.groups
        ? '自动组暂不可用，不能把缺失解释为无风险。'
        : discoverGroup?.primary_issue_summary || discoverGroup?.decision_recommendation?.summary || '自动组负责发现同区、服务商、续费、预算和资料缺口。',
      metric: sourceErrors.groups ? '不可用' : `${groups.length} 组`,
      meta: sourceErrors.groups ? sourceErrors.groups : discoverGroup ? `${discoverGroup.scope_label} · ${groupPressureLabel(discoverGroup)}` : '当前视图暂无自动组',
      tone: sourceErrors.groups ? 'alert' : groups.length > 0 ? 'notice' : 'normal',
      actionLabel: discoverGroup ? '打开自动组' : '查看需要决策',
      target: discoverGroup ? { type: 'group', id: discoverGroup.group_id } : undefined,
      unavailable: Boolean(sourceErrors.groups),
    },
    {
      stage: 'compare',
      eyebrow: '02 COMPARE',
      title: '形成真实场景',
      summary: sourceErrors.manualGroups || sourceErrors.templates
        ? '场景数据部分不可用，只展示已加载的组合和模板。'
        : compareGroup?.decision_recommendation?.next_step || compareGroup?.goal || '从模板或自动组创建自定义组合，沉淀真实比较篮子。',
      metric: sourceErrors.manualGroups ? '不可用' : `${metrics.manualActiveCount} 个`,
      meta: sourceErrors.manualGroups
        ? sourceErrors.manualGroups
        : compareGroup
          ? `${MANUAL_GROUP_SCENARIO_LABELS[compareGroup.scenario]} · 成员 ${compareGroup.member_count} · 缺口 ${compareGroup.evidence_assessment.gap_signal_count}`
          : `可用模板 ${sourceErrors.templates ? '不可用' : activeTemplateCount}`,
      tone: sourceErrors.manualGroups ? 'alert' : metrics.manualActiveCount > 0 ? 'maintenance' : activeTemplateCount > 0 ? 'notice' : 'neutral',
      actionLabel: compareGroup ? '继续组合' : '使用模板',
      target: compareGroup
        ? { type: 'manual_group', id: compareGroup.manual_group_id }
        : activeTemplate
          ? { type: 'template', id: activeTemplate.template_id }
          : undefined,
      unavailable: Boolean(sourceErrors.manualGroups),
    },
    {
      stage: 'decide',
      eyebrow: '03 DECIDE',
      title: '保存一次判断',
      summary: sourceErrors.records
        ? '决策记录暂不可用，不能确认历史判断。'
        : decideRecord?.goal || decideRecord?.execution_readback?.summary || '保存记录后才能把组合判断和证据快照纳入长期跟进。',
      metric: sourceErrors.records ? '不可用' : `${records.length} 条`,
      meta: sourceErrors.records
        ? sourceErrors.records
        : decideRecord
          ? `${RECORD_STATUS_LABELS[decideRecord.status]} · 成员 ${decideRecord.member_count} · ${readbackCountSummary(decideRecord.execution_readback)}`
          : '尚未保存组合决策',
      tone: sourceErrors.records ? 'alert' : records.length > 0 ? 'notice' : 'neutral',
      actionLabel: decideRecord ? '打开记录' : '先选组合',
      target: decideRecord ? { type: 'record', id: decideRecord.record_id } : discoverGroup ? { type: 'group', id: discoverGroup.group_id } : undefined,
      unavailable: Boolean(sourceErrors.records),
    },
    {
      stage: 'execute',
      eyebrow: '04 EXECUTE',
      title: '回读执行闭环',
      summary: sourceErrors.records
        ? '记录回读不可用，执行闭环状态未知。'
        : executeRecord?.execution_plan?.summary || executeRecord?.execution_readback?.summary || '执行回读只校验当前事实，不会替用户自动完成动作。',
      metric: sourceErrors.records ? '未知' : executionRiskCount > 0 ? `${executionRiskCount} 项` : '稳定',
      meta: sourceErrors.records
        ? sourceErrors.records
        : executeRecord
          ? `${READBACK_STATUS_LABELS[executeRecord.execution_readback?.status ?? 'open']} · ${executionPlanCountSummary(executeRecord.execution_plan)}`
          : '暂无记录需要回读',
      tone: sourceErrors.records ? 'alert' : metrics.readbackDriftCount > 0 || metrics.readbackBlockedCount > 0 ? 'critical' : metrics.readbackNeedsEvidenceCount > 0 ? 'alert' : 'normal',
      actionLabel: executeRecord ? '查看回读' : '查看记录',
      target: executeRecord ? { type: 'record', id: executeRecord.record_id } : undefined,
      unavailable: Boolean(sourceErrors.records),
    },
  ]
}

function deriveNextWorkItems(
  groups: AssetDecisionGroupSummary[],
  records: AssetDecisionRecordSummary[],
  manualGroups: AssetDecisionManualGroupSummary[],
  templates: AssetDecisionScenarioTemplateSummary[],
  sourceErrors: ClosedLoopSourceErrors,
): AssetDecisionNextWorkItem[] {
  const items: AssetDecisionNextWorkItem[] = []

  if (!sourceErrors.records) {
    for (const record of records) {
      const readback = record.execution_readback
      if (!readback) continue
      const scope = record.scope_label || record.source_group_id
      if (readback.drift_count > 0 || readback.status === 'drift') {
        items.push({
          id: `record-drift-${record.record_id}`,
          kind: 'record_drift',
          tone: 'critical',
          sourceLabel: 'DECISION MEMORY',
          kindLabel: '事实漂移',
          title: record.title,
          summary: readback.summary || '当前事实与已保存判断不一致，需要复核执行闭环。',
          meta: `${scope} · ${readbackCountSummary(readback)} · 跟进未关闭 ${recordFollowupOpenCount(record)}`,
          actionLabel: '复核记录',
          priority: 1000 + readback.drift_count * 16 + recordFollowupOpenCount(record),
          target: { type: 'record', id: record.record_id },
        })
      } else if (readback.blocked_count > 0 || record.followup_blocked_count > 0 || readback.status === 'blocked') {
        items.push({
          id: `record-blocked-${record.record_id}`,
          kind: 'record_blocked',
          tone: 'critical',
          sourceLabel: 'DECISION MEMORY',
          kindLabel: '跟进阻塞',
          title: record.title,
          summary: readback.summary || '记录中仍有成员阻塞，需要人工解除或调整跟进路径。',
          meta: `${scope} · 阻塞 ${(readback.blocked_count ?? 0) + (record.followup_blocked_count ?? 0)} · 未关闭 ${recordFollowupOpenCount(record)}`,
          actionLabel: '处理阻塞',
          priority: 900 + (readback.blocked_count ?? 0) * 12 + (record.followup_blocked_count ?? 0) * 12,
          target: { type: 'record', id: record.record_id },
        })
      } else if (readback.needs_evidence_count > 0 || readback.status === 'needs_evidence') {
        items.push({
          id: `record-needs-evidence-${record.record_id}`,
          kind: 'record_needs_evidence',
          tone: 'alert',
          sourceLabel: 'DECISION MEMORY',
          kindLabel: '回读缺证据',
          title: record.title,
          summary: readback.summary || '当前记录仍有证据缺口，先补齐资料再推进判断。',
          meta: `${scope} · ${readbackCountSummary(readback)} · 成员 ${record.member_count}`,
          actionLabel: '补证据',
          priority: 800 + readback.needs_evidence_count * 10,
          target: { type: 'record', id: record.record_id },
        })
      }
    }
  }

  if (!sourceErrors.groups) {
    for (const group of groups) {
      const pressure = group.evidence_assessment.pressure_score + group.evidence_assessment.gap_signal_count * 4
      items.push({
        id: `auto-group-${group.group_id}`,
        kind: 'auto_group',
        tone: group.evidence_assessment.quality_tier === 'blocked' ? 'alert' : 'notice',
        sourceLabel: 'AUTO GROUP',
        kindLabel: VIEW_LABELS[group.view],
        title: group.title,
        summary: group.decision_recommendation?.summary || group.primary_issue_summary || '打开自动组比较成员、成本、服务承载和证据缺口。',
        meta: `${group.scope_label} · ${group.member_count} 台 VPS · 续费 ${group.renewal_window_count} · 缺口 ${group.evidence_assessment.gap_signal_count}`,
        actionLabel: '打开决策组',
        priority: 620 + group.priority + pressure,
        target: { type: 'group', id: group.group_id },
      })
    }
  }

  if (!sourceErrors.manualGroups) {
    for (const group of manualGroups.filter((item) => item.status === 'active')) {
      items.push({
        id: `manual-group-${group.manual_group_id}`,
        kind: 'manual_group',
        tone: 'maintenance',
        sourceLabel: 'SCENARIO WORKBENCH',
        kindLabel: MANUAL_GROUP_SCENARIO_LABELS[group.scenario],
        title: group.title,
        summary: group.decision_recommendation?.next_step || group.goal || '继续维护成员意图，必要时保存为一次组合决策记录。',
        meta: `${group.member_count} 台 VPS · 资料缺口 ${group.evidence_assessment.gap_signal_count} · 更新 ${formatDateTime(group.updated_at)}`,
        actionLabel: '继续组合',
        priority: 520 + group.member_count * 4 + group.evidence_assessment.gap_signal_count * 8,
        target: { type: 'manual_group', id: group.manual_group_id },
      })
    }
  }

  if (!sourceErrors.templates) {
    for (const template of templates.filter((item) => item.status === 'active')) {
      items.push({
        id: `scenario-template-${template.template_id}`,
        kind: 'scenario_template',
        tone: template.builtin ? 'notice' : 'maintenance',
        sourceLabel: 'SCENARIO TEMPLATES',
        kindLabel: MANUAL_GROUP_SCENARIO_LABELS[template.scenario],
        title: template.title,
        summary: template.goal || template.note || '从模板启动自定义组合，再根据当前事实补齐成员。',
        meta: `${template.builtin ? '内置模板' : '自定义模板'} · 蓝图成员 ${template.member_count}`,
        actionLabel: '使用模板',
        priority: 320 + (template.builtin ? 8 : 16) + template.member_count,
        target: { type: 'template', id: template.template_id },
      })
    }
  }

  return items
    .sort((left, right) => {
      if (left.priority !== right.priority) return right.priority - left.priority
      return left.title.localeCompare(right.title)
    })
    .slice(0, 6)
}

function nextWorkTargetLabel(target: AssetDecisionNextWorkTarget): string {
  if (target.type === 'record') return target.id
  if (target.type === 'manual_group') return target.id
  if (target.type === 'template') return target.id
  return target.id
}

function portfolioContextLabel(chips: ContextFilterChip[], view: MainWorkbenchView, renewalWindow: RenewalWindow): string {
  if (chips.length === 0) {
    return `全局资产组合 · ${VIEW_LABELS[view]} · ${renewalWindow} 天续费窗口`
  }
  return chips.map((chip) => `${chip.label} ${chip.value}`).join(' / ')
}

function portfolioRiskLabel(metrics: ClosedLoopMetrics): string {
  if (metrics.readbackDriftCount > 0) return `事实漂移 ${metrics.readbackDriftCount}`
  if (metrics.readbackBlockedCount > 0) return `阻塞 ${metrics.readbackBlockedCount}`
  const gapCount = metrics.readbackNeedsEvidenceCount + metrics.evidenceGapGroupCount
  if (gapCount > 0) return `资料缺口 ${gapCount}`
  if (metrics.readbackOpenCount > 0) return `待回读 ${metrics.readbackOpenCount}`
  if (metrics.recordActiveCount > 0) return `跟进中 ${metrics.recordActiveCount}`
  return '闭环稳定'
}

function portfolioEvidenceLabel(overview?: AssetDecisionOverview | null): string {
  if (!overview) return '等待资产证据聚合'
  return sourceAvailabilityLabel(overview.source_availability)
}

function portfolioRenewalLabel(overview: AssetDecisionOverview | null | undefined, renewalWindow: RenewalWindow): string {
  if (!overview) return `${renewalWindow} 天窗口等待聚合`
  return `${renewalWindow} 天窗口 · 续费组 ${overview.renewal_group_count} · 待决策 ${overview.needs_decision_count}`
}

function buildPortfolioLead(
  view: MainWorkbenchView,
  renewalWindow: RenewalWindow,
  overview: AssetDecisionOverview | null,
  groups: AssetDecisionGroupSummary[],
  nextWorkItems: AssetDecisionNextWorkItem[],
  metrics: ClosedLoopMetrics,
  contextChips: ContextFilterChip[],
): AssetDecisionPortfolioLead {
  const first = nextWorkItems[0]
  const contextLabel = portfolioContextLabel(contextChips, view, renewalWindow)
  const riskLabel = portfolioRiskLabel(metrics)
  const evidenceLabel = portfolioEvidenceLabel(overview)
  const renewalLabelText = portfolioRenewalLabel(overview, renewalWindow)
  const fallbackGroup = groups[0]
  if (first) {
    return {
      tone: first.tone,
      eyebrow: first.sourceLabel,
      title: `${first.kindLabel} · ${first.title}`,
      summary: first.summary,
      actionLabel: first.actionLabel,
      contextLabel,
      riskLabel,
      evidenceLabel,
      renewalLabel: renewalLabelText,
      primaryItem: first,
    }
  }
  if (fallbackGroup) {
    return {
      tone: fallbackGroup.evidence_assessment.gap_signal_count > 0 ? 'alert' : 'notice',
      eyebrow: 'AUTO GROUP',
      title: `先比较 ${fallbackGroup.title}`,
      summary: fallbackGroup.decision_recommendation?.summary || fallbackGroup.primary_issue_summary || '当前视图有可比较的自动决策组，先打开组详情核对成员事实。',
      actionLabel: '打开决策组',
      contextLabel,
      riskLabel,
      evidenceLabel,
      renewalLabel: renewalLabelText,
      primaryGroupID: fallbackGroup.group_id,
    }
  }
  return {
    tone: metrics.partialErrorCount > 0 ? 'alert' : 'normal',
    eyebrow: 'PORTFOLIO STATUS',
    title: metrics.partialErrorCount > 0 ? '部分资产决策证据不可用' : '当前视图暂无置顶组合工作',
    summary: metrics.partialErrorCount > 0
      ? '请先查看局部错误边界；已加载的自动组、记录和单台辅助队列仍可继续处理。'
      : '当前已加载数据没有漂移、阻塞或可启动场景，可切换视图继续检查其它组合压力。',
    actionLabel: '查看需要决策',
    contextLabel,
    riskLabel,
    evidenceLabel,
    renewalLabel: renewalLabelText,
  }
}

function renderDecisionRecommendation(
  recommendation?: AssetDecisionRecommendation | null,
  mode: 'compact' | 'detail' = 'compact',
) {
  if (!recommendation) return <span className="empty-inline">等待建议</span>
  const signalLimit = mode === 'detail' ? 5 : 3
  const reasons = recommendation.reasons ?? []
  const blockers = recommendation.blockers ?? []
  return (
    <div className={`asset-decision-recommendation asset-decision-recommendation--${mode}`}>
      <span className="asset-decision-chip-row">
        <Badge variant="state" tone={blockers.length > 0 ? 'critical' : 'notice'}>
          {recommendation.confidence_label || '建议'}
        </Badge>
        {blockers.length > 0 && (
          <Badge variant="count" tone="critical">
            阻塞 {blockers.length}
          </Badge>
        )}
      </span>
      <strong>{recommendation.summary || '等待系统建议'}</strong>
      <span>{recommendation.next_step || '继续比较同组成员后记录判断'}</span>
      {mode === 'detail' && recommendation.priority_vps_ids.length > 0 && (
        <small>优先核对 {recommendation.priority_vps_ids.join(' / ')}</small>
      )}
      {(reasons.length > 0 || blockers.length > 0) && (
        <span className="asset-decision-chip-row">
          {[...blockers, ...reasons].slice(0, signalLimit).map((reason) => (
            <Badge key={`${reason.kind}-${reason.label}`} variant="info" tone={chipTone(reason.tone)}>
              {reason.label}
            </Badge>
          ))}
          {blockers.length + reasons.length > signalLimit && (
            <Badge variant="count" tone="neutral">
              +{blockers.length + reasons.length - signalLimit}
            </Badge>
          )}
        </span>
      )}
    </div>
  )
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

function buildManualMemberDrafts(detail: AssetDecisionManualGroupDetail | null): Record<string, ManualMemberDraft> {
  const drafts: Record<string, ManualMemberDraft> = {}
  for (const member of detail?.members ?? []) {
    drafts[member.vps_id] = {
      intendedRole: member.intended_role,
      intendedAction: member.intended_action,
      reason: member.reason,
      note: member.note,
      sortOrder: String(member.sort_order),
    }
  }
  return drafts
}

function evidenceTierTone(tier: AssetDecisionEvidenceQualityTier): BadgeTone {
  if (tier === 'strong') return 'normal'
  if (tier === 'usable') return 'notice'
  if (tier === 'blocked') return 'critical'
  return 'maintenance'
}

function evidenceBiasTone(bias: AssetDecisionEvidenceDecisionBias): BadgeTone {
  if (bias === 'keep') return 'normal'
  if (bias === 'retire') return 'critical'
  if (bias === 'migrate' || bias === 'observe') return 'maintenance'
  if (bias === 'complete_evidence') return 'alert'
  return 'notice'
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

function buildRecordDraft(detail: AssetDecisionGroupDetail, renewWithinDays: number): RecordDraft {
  const members: Record<string, RecordMemberDraft> = {}
  const memberOrder: string[] = []
  for (const member of detail.members) {
    const vpsID = member.vps.vps_id
    memberOrder.push(vpsID)
    members[vpsID] = {
      decidedRole: member.suggested_role,
      decidedAction: member.suggested_action,
      reason: '',
    }
  }
  return {
    sourceType: 'auto_group',
    sourceGroupID: detail.group_id,
    renewWithinDays,
    title: detail.title,
    goal: '',
    status: 'draft',
    memberOrder,
    members,
  }
}

function buildManualRecordDraft(detail: AssetDecisionManualGroupDetail): RecordDraft {
  const members: Record<string, RecordMemberDraft> = {}
  const memberOrder: string[] = []
  for (const member of detail.members) {
    memberOrder.push(member.vps_id)
    members[member.vps_id] = {
      decidedRole: member.intended_role,
      decidedAction: member.intended_action,
      reason: member.reason,
    }
  }
  return {
    sourceType: 'manual_group',
    sourceGroupID: detail.manual_group_id,
    renewWithinDays: detail.renew_within_days,
    title: detail.title,
    goal: detail.goal,
    status: 'draft',
    memberOrder,
    members,
  }
}

function memberIntentIsSet(member: AssetDecisionManualGroupMember): boolean {
  return Boolean(member.intended_role && member.intended_action && member.intended_action !== 'review')
}

function buildManualGroupProgress(detail: AssetDecisionManualGroupDetail): ManualGroupProgress {
  const hasGoal = Boolean(detail.goal.trim() || detail.title.trim())
  const hasMembers = detail.members.length > 0
  const intentReadyCount = detail.members.filter(memberIntentIsSet).length
  const intentReady = hasMembers && intentReadyCount === detail.members.length
  const evidenceReady = detail.evidence_assessment.quality_tier !== 'blocked' && detail.evidence_assessment.gap_signal_count === 0
  const currentFactMissingCount = detail.members.filter((member) => !member.current_fact_found).length
  const factReady = currentFactMissingCount === 0
  const readyToRecord = hasGoal && hasMembers && intentReady && evidenceReady && factReady
  const items: ManualGroupProgressItem[] = [
    {
      key: 'goal',
      label: '目标',
      summary: hasGoal ? detail.goal || detail.title : '补齐组合目标后再沉淀判断',
      tone: hasGoal ? 'normal' : 'alert',
      done: hasGoal,
    },
    {
      key: 'members',
      label: '成员',
      summary: hasMembers ? `${detail.members.length} 台 VPS 已加入比较` : '至少加入一台 VPS',
      tone: hasMembers ? 'normal' : 'alert',
      done: hasMembers,
    },
    {
      key: 'intent',
      label: '意图',
      summary: hasMembers ? `已设置 ${intentReadyCount}/${detail.members.length} 个成员动作` : '等待成员后设置角色和动作',
      tone: intentReady ? 'normal' : hasMembers ? 'notice' : 'neutral',
      done: intentReady,
    },
    {
      key: 'evidence',
      label: '证据',
      summary: evidenceReady ? detail.evidence_assessment.summary : `仍有 ${detail.evidence_assessment.gap_signal_count} 个资料缺口`,
      tone: evidenceReady ? 'normal' : detail.evidence_assessment.quality_tier === 'blocked' ? 'critical' : 'alert',
      done: evidenceReady,
    },
    {
      key: 'facts',
      label: '当前事实',
      summary: factReady ? '成员当前事实可回读' : `${currentFactMissingCount} 个成员缺少当前事实`,
      tone: factReady ? 'normal' : 'critical',
      done: factReady,
    },
  ]
  const doneCount = items.filter((item) => item.done).length
  return {
    readinessLabel: readyToRecord ? '可保存记录' : doneCount >= 3 ? '接近可保存' : '继续整理',
    readinessTone: readyToRecord ? 'normal' : doneCount >= 3 ? 'maintenance' : 'alert',
    readyToRecord,
    doneCount,
    totalCount: items.length,
    items,
  }
}

function recordSourceLabel(record: Pick<AssetDecisionRecordSummary, 'source_type' | 'source_group_id' | 'source_group_type' | 'scope_label'>): string {
  const scope = record.scope_label || record.source_group_id
  if (record.source_type === 'manual_group') return `来自自定义组合 ${scope}`
  if (record.source_type === 'auto_group') return `来自自动组 ${scope}`
  return `来源 ${scope}`
}

function recordSourceDetail(record: AssetDecisionRecordSummary): string {
  const sourceType = record.source_type === 'manual_group' ? '自定义组合' : record.source_type === 'auto_group' ? '自动组' : record.source_type
  return `${sourceType} · ${record.source_group_type} · ${VIEW_LABELS[record.source_view] ?? record.source_view} · ${record.source_group_id}`
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
    source_availability: detail.source_availability,
    created_at: detail.created_at,
    updated_at: detail.updated_at,
    archived_at: detail.archived_at,
  }
  return summary
}

function upsertManualGroupSummary(
  rows: AssetDecisionManualGroupSummary[],
  detail: AssetDecisionManualGroupDetail,
): AssetDecisionManualGroupSummary[] {
  const summary = manualGroupSummaryFromDetail(detail)
  const next = [summary, ...rows.filter((row) => row.manual_group_id !== summary.manual_group_id)]
  return next.sort((left, right) => {
    if (left.status !== right.status) return left.status === 'active' ? -1 : 1
    return right.updated_at.localeCompare(left.updated_at)
  })
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
    return `/vps/${member.vps_id}?workbench=cancellation`
  }
  if (member.execution_plan?.step_kind === 'open_subscription_context') {
    return `/subscriptions?vps_id=${encodeURIComponent(member.vps_id)}`
  }
  return `/vps/${member.vps_id}`
}

function actionLabelForMember(member: AssetDecisionRecordMember): string {
  if (member.execution_plan?.step_label) return member.execution_plan.step_label
  if (member.decided_action === 'open_cancellation_workbench' || member.decided_action === 'cancel') return '取消/退役'
  return 'VPS 详情'
}

function renderEvidenceChips(chips: AssetDecisionEvidenceChip[], limit = 5) {
  if (chips.length === 0) return <span className="empty-inline">证据稳定</span>
  const visible = chips.slice(0, limit)
  return (
    <span className="asset-decision-chip-row">
      {visible.map((chip) => (
        <Badge key={chip.kind} variant="info" tone={chipTone(chip.tone)}>
          {chip.label}
        </Badge>
      ))}
      {chips.length > visible.length && (
        <Badge variant="count" tone="neutral">
          +{chips.length - visible.length}
        </Badge>
      )}
    </span>
  )
}

function renderEvidenceAssessment(assessment: AssetDecisionEvidenceAssessment | null, mode: 'compact' | 'detail' = 'compact') {
  if (!assessment) return <span className="empty-inline">无证据评估</span>
  return (
    <div className={`asset-decision-assessment asset-decision-assessment--${mode}`}>
      <div className="asset-decision-assessment__head">
        <Badge variant="state" tone={evidenceTierTone(assessment.quality_tier)}>
          {EVIDENCE_TIER_LABELS[assessment.quality_tier] ?? assessment.quality_tier}
        </Badge>
        <Badge variant="state" tone={evidenceBiasTone(assessment.decision_bias)}>
          {EVIDENCE_BIAS_LABELS[assessment.decision_bias] ?? assessment.decision_bias}
        </Badge>
      </div>
      <div className="asset-decision-assessment__bars" aria-label="证据评估刻度">
        <span style={{ '--score': assessment.confidence_score } as ScoreStyle}>
          可信 <MonoDigits>{assessment.confidence_score}</MonoDigits>
        </span>
        <span style={{ '--score': assessment.pressure_score } as ScoreStyle}>
          压力 <MonoDigits>{assessment.pressure_score}</MonoDigits>
        </span>
        <span style={{ '--score': assessment.readiness_score } as ScoreStyle}>
          准备 <MonoDigits>{assessment.readiness_score}</MonoDigits>
        </span>
      </div>
      {mode === 'detail' && (
        <div className="asset-decision-assessment__meta">
          <strong>{assessment.summary}</strong>
          <span>支撑 {assessment.support_signal_count} · 风险 {assessment.risk_signal_count} · 缺口 {assessment.gap_signal_count}</span>
        </div>
      )}
    </div>
  )
}

function formatGroupMonthlyCost(group: CostSummaryLike): string {
  if (group.monthly_cost_base != null) {
    return `${formatMoney(group.monthly_cost_base, group.base_currency ?? 'CNY')}/月`
  }
  if (group.monthly_cost_by_currency.length > 0) {
    return group.monthly_cost_by_currency
      .map((item) => `${formatMoney(item.monthly_total, item.currency)}/月`)
      .join(' / ')
  }
  return '暂无成本'
}

function formatGroupYearlyCost(group: CostSummaryLike): string {
  if (group.yearly_cost_base != null) {
    return `${formatMoney(group.yearly_cost_base, group.base_currency ?? 'CNY')}/年`
  }
  if (group.monthly_cost_by_currency.length > 0) {
    return group.monthly_cost_by_currency
      .map((item) => `${formatMoney(item.yearly_total, item.currency)}/年`)
      .join(' / ')
  }
  return '成本证据不足'
}

function countSummary<T extends string>(
  counts: Partial<Record<T, number>>,
  order: T[],
  labeler: (value: T) => string,
): string {
  const parts = order
    .map((key) => {
      const count = counts[key] ?? 0
      return count > 0 ? `${labeler(key)} ${count}` : ''
    })
    .filter(Boolean)
  return parts.length > 0 ? parts.join(' / ') : '暂无分布'
}

function sourceAvailabilityLabel(source: AssetDecisionOverview['source_availability'] | AssetDecisionGroupMember['source_availability']): string {
  const missing = [
    !source.subscriptions && '订阅',
    !source.services && '服务',
    !source.domains && '域名',
    !source.monitoring && '监控',
    !source.targets && 'Target',
  ].filter(Boolean)
  return missing.length > 0 ? `${missing.join('、')}证据不可用` : '证据源正常'
}

function memberContextLabel(member: AssetDecisionGroupMember): string {
  return [
    `服务 ${member.service_count}`,
    `域名 ${member.domain_count}`,
    `Target ${member.running_target_count}/${member.target_count}`,
    `监控 ${member.running_monitoring_count}/${member.monitoring_link_count}`,
  ].join(' · ')
}

function currentFactsLabel(facts?: AssetDecisionExecutionCurrentFacts): string {
  if (!facts) return '当前事实尚未返回'
  if (!facts.found) return '当前事实缺失'
  return [
    `订阅 ${facts.active_subscription_count}`,
    `服务 ${facts.service_count}`,
    `域名 ${facts.domain_count}`,
    `Target ${facts.running_target_count}/${facts.target_count}`,
    `监控 ${facts.running_monitoring_count}/${facts.monitoring_link_count}`,
  ].join(' · ')
}

function currentFactsStateLabel(facts?: AssetDecisionExecutionCurrentFacts): string {
  if (!facts) return '等待资产聚合事实'
  if (!facts.found) return '资产聚合中未找到当前 VPS'
  return [
    facts.lifecycle_status ? lifecycleLabel(facts.lifecycle_status) : '',
    facts.usage_status ? usageLabel(facts.usage_status) : '',
    facts.renewal_decision ? renewalLabel(facts.renewal_decision) : '',
    facts.abnormal_monitoring_count > 0 ? `异常监控 ${facts.abnormal_monitoring_count}` : '',
    facts.active_incident_count > 0 ? `事件 ${facts.active_incident_count}` : '',
  ].filter(Boolean).join(' · ') || '基础状态正常'
}

function renderReadbackBadge(readback?: { status: AssetDecisionExecutionReadbackStatus }) {
  const status = readback?.status
  if (!status) {
    return (
      <Badge variant="state" tone="neutral">
        等待回读
      </Badge>
    )
  }
  return (
    <Badge variant="state" tone={readbackStatusTone(status)}>
      {READBACK_STATUS_LABELS[status] ?? status}
    </Badge>
  )
}

function renderExecutionPlanBadge(plan?: AssetDecisionMemberExecutionPlan) {
  if (!plan) {
    return (
      <Badge variant="state" tone="neutral">
        等待编排
      </Badge>
    )
  }
  return (
    <Badge variant="state" tone={executionPlanTone(plan.tone)}>
      {EXECUTION_PLAN_LANE_LABELS[plan.lane] ?? plan.lane}
    </Badge>
  )
}

function renderMemberExecutionPlan(member: AssetDecisionRecordMember) {
  const plan = member.execution_plan
  return (
    <div className="asset-table__stack asset-decision-plan-cell">
      <span className="asset-decision-chip-row">
        {renderExecutionPlanBadge(plan)}
        {plan?.actionable && (
          <Badge variant="count" tone={executionPlanTone(plan.tone)}>
            可推进
          </Badge>
        )}
        {plan?.blocked && (
          <Badge variant="count" tone="critical">
            阻塞
          </Badge>
        )}
      </span>
      <strong>{plan?.summary || '等待执行编排'}</strong>
      <span>{plan?.step_label || actionLabelForMember(member)}</span>
      {plan?.issue_count ? <span>关联问题 {plan.issue_count} 项</span> : <span>无额外问题</span>}
    </div>
  )
}

function renderMemberReadback(readback?: AssetDecisionMemberExecutionReadback) {
  if (!readback) {
    return (
      <div className="asset-table__stack asset-decision-readback-cell">
        <span className="asset-decision-chip-row">{renderReadbackBadge()}</span>
        <strong>等待执行证据回读</strong>
        <span>当前事实尚未返回</span>
      </div>
    )
  }
  const issues = readback.issues ?? []
  return (
    <div className="asset-table__stack asset-decision-readback-cell">
      <span className="asset-decision-chip-row">
        {renderReadbackBadge(readback)}
        {issues.length > 0 && (
          <Badge variant="count" tone={readbackStatusTone(readback.status)}>
            {issues.length} 项
          </Badge>
        )}
      </span>
      <strong>{readback.summary || '等待执行回读'}</strong>
      <span>{currentFactsLabel(readback.current_facts)}</span>
      <span>{currentFactsStateLabel(readback.current_facts)}</span>
      {issues.length > 0 && (
        <span className="asset-decision-chip-row">
          {issues.slice(0, 3).map((issue) => (
            <Badge key={`${issue.kind}-${issue.label}`} variant="info" tone={chipTone(issue.tone)}>
              {issue.label}
            </Badge>
          ))}
          {issues.length > 3 && (
            <Badge variant="count" tone="neutral">
              +{issues.length - 3}
            </Badge>
          )}
        </span>
      )}
    </div>
  )
}

function membersForExecutionLane(members: AssetDecisionRecordMember[], lane: AssetDecisionExecutionPlanLane): AssetDecisionRecordMember[] {
  return members.filter((member) => (member.execution_plan?.lane ?? 'review') === lane)
}

function laneIssueCount(members: AssetDecisionRecordMember[]): number {
  return members.reduce((total, member) => total + (member.execution_plan?.issue_count ?? member.execution_readback?.issues?.length ?? 0), 0)
}

function laneActionableCount(members: AssetDecisionRecordMember[]): number {
  return members.filter((member) => member.execution_plan?.actionable).length
}

function laneBlockedCount(members: AssetDecisionRecordMember[]): number {
  return members.filter((member) => member.execution_plan?.blocked || member.followup_status === 'blocked').length
}

function groupPressureLabel(group: AssetDecisionGroupSummary): string {
  const parts = [
    group.renewal_window_count > 0 ? `续费窗口 ${group.renewal_window_count}` : '',
    group.unreviewed_count > 0 ? `未评估 ${group.unreviewed_count}` : '',
    group.cancellation_attention_count > 0 ? `取消联动 ${group.cancellation_attention_count}` : '',
    group.evidence_assessment.gap_signal_count > 0 ? `缺口 ${group.evidence_assessment.gap_signal_count}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '暂无高压信号'
}

function groupServiceLabel(group: AssetDecisionGroupSummary): string {
  return `服务 ${group.service_count} · 域名 ${group.domain_count} · Target ${group.running_target_count}/${group.target_count}`
}

function groupMonitoringLabel(group: AssetDecisionGroupSummary): string {
  const abnormal = group.abnormal_monitoring_count > 0 ? `异常 ${group.abnormal_monitoring_count}` : '异常 0'
  const incidents = group.active_incident_count > 0 ? `事件 ${group.active_incident_count}` : '事件 0'
  return `监控 ${group.monitoring_link_count} · ${abnormal} · ${incidents}`
}

export function AssetDecisionsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeView = parseWorkbenchView(searchParams.get('view'))
  const portfolioView = portfolioViewForWorkbench(activeView)
  const isSingleQueueDeepLink = activeView === 'single_queue'
  const renewalWindow = parseRenewalWindow(searchParams.get('renew_within_days'))
  const assetDecisionFilter = useMemo(
    () => buildAssetDecisionFilter(searchParams, portfolioView, renewalWindow),
    [portfolioView, renewalWindow, searchParams],
  )
  const contextFilterChips = useMemo(
    () => buildContextFilterChips(assetDecisionFilter),
    [assetDecisionFilter],
  )
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
  const [recordDraft, setRecordDraft] = useState<RecordDraft | null>(null)
  const [recordSaving, setRecordSaving] = useState(false)
  const [recordSaveError, setRecordSaveError] = useState<string | null>(null)
  const [manualGroupCreating, setManualGroupCreating] = useState(false)
  const [manualGroupSaving, setManualGroupSaving] = useState(false)
  const [manualGroupError, setManualGroupError] = useState<string | null>(null)
  const [manualMemberDrafts, setManualMemberDrafts] = useState<Record<string, ManualMemberDraft>>({})
  const [manualMemberSaving, setManualMemberSaving] = useState<Record<string, boolean>>({})
  const [manualMemberAddDraft, setManualMemberAddDraft] = useState<ManualMemberAddDraft>({
    vpsID: '',
    intendedRole: 'observe_candidate',
    intendedAction: 'review',
    reason: '',
    note: '',
    sortOrder: '',
  })
  const [recordPatchStatus, setRecordPatchStatus] = useState<AssetDecisionRecordStatus>('draft')
  const [recordPatching, setRecordPatching] = useState(false)
  const [recordPatchError, setRecordPatchError] = useState<string | null>(null)
  const [recordFollowupDrafts, setRecordFollowupDrafts] = useState<Record<string, RecordFollowupDraft>>({})
  const [recordFollowupPatching, setRecordFollowupPatching] = useState<Record<string, boolean>>({})
  const [templateSaving, setTemplateSaving] = useState(false)
  const [templateError, setTemplateError] = useState<string | null>(null)
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
  const handledOpenStateRef = useRef('')

  function applyURLClearedOpenState() {
    setSelectedGroupID(null)
    setSelectedManualGroupID(null)
    setSelectedRecordID(null)
    setSelectedTemplateID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setRecordDraft(null)
    setRecordSaveError(null)
    setManualGroupError(null)
    setTemplateError(null)
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
        setManualGroupsState({ loading: false, error: null, groups })
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
  }, [assetDecisionFilter, refreshToken])

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
        setManualMemberDrafts(buildManualMemberDrafts(detail))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setManualDetailState({
          loading: false,
          error: describeError(error, '加载自定义组合详情失败'),
          detail: null,
        })
        setManualMemberDrafts({})
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
          note: detail.note,
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
    const groupID = trimParam(searchParams.get('group_id'))
    const manualGroupID = trimParam(searchParams.get('manual_group_id'))
    const recordID = trimParam(searchParams.get('record_id'))
    const templateID = trimParam(searchParams.get('template_id'))
    const openStateKey = groupID ? `group:${groupID}`
      : manualGroupID ? `manual:${manualGroupID}`
        : recordID ? `record:${recordID}`
          : templateID ? `template:${templateID}`
            : ''

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
  }, [searchParams, selectedGroupID, selectedManualGroupID, selectedRecordID, selectedTemplateID])

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
    manualGroupsState.groups,
    templatesState.templates,
    closedLoopSourceErrors,
  )
  const decisionPathSteps = buildDecisionPathSteps(
    portfolioState.groups,
    recordsState.records,
    manualGroupsState.groups,
    templatesState.templates,
    closedLoopMetrics,
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

  const workbenchTabs = WORKBENCH_TABS.map((item) => ({
    ...item,
    count:
      item.value === 'needs_decision' ? overview?.needs_decision_count
        : item.value === 'renewal' ? overview?.renewal_group_count
          : item.value === 'region' ? overview?.region_group_count
            : item.value === 'provider' ? overview?.provider_group_count
              : item.value === 'cost' ? overview?.cost_group_count
                : item.value === 'evidence' ? overview?.evidence_group_count
                  : undefined,
  }))
  const queueTabs = [
    { value: 'all', label: '全部', count: totalDecisionQueue },
    { value: 'unreviewed', label: '待评估', count: queueState.unreviewed.length },
    { value: 'renewal', label: `${renewalWindow}天续费`, count: renewalDueQueueCount },
    { value: 'migrate', label: '迁移', count: queueState.migrate.length },
    { value: 'cancel', label: '取消', count: queueState.cancel.length },
    { value: 'cancellation_attention', label: '取消联动', count: cancellationAttentionCount },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'missing_subscription', label: '缺订阅', count: missingSubscriptionCount },
  ] satisfies Array<{ value: DecisionQueueView; label: string; count: number }>

  const manualGroupColumns: DataTableColumn<AssetDecisionManualGroupSummary>[] = [
    {
      key: 'group',
      label: '自定义组合',
      width: '286px',
      render: (group) => (
        <div className="asset-table__identity asset-decision-group-cell">
          <strong>{group.title}</strong>
          <span>{MANUAL_GROUP_SCENARIO_LABELS[group.scenario]} · {group.goal || group.scope_label || '用户场景'}</span>
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={manualGroupStatusTone(group.status)}>
              {MANUAL_GROUP_STATUS_LABELS[group.status]}
            </Badge>
            <Badge variant="info" tone="neutral">
              {group.source_type === 'auto_group' ? '来自自动组' : '手工创建'}
            </Badge>
          </span>
          {renderEvidenceChips(group.evidence_chips, 3)}
          {renderDecisionRecommendation(group.decision_recommendation)}
        </div>
      ),
    },
    {
      key: 'portfolio',
      label: '组合事实',
      width: '220px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{group.member_count}</MonoDigits> 台 VPS</strong>
          <span>{countSummary(group.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</span>
          <span>{countSummary(group.renewal_decision_counts, ['unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced'], renewalLabel)}</span>
        </div>
      ),
    },
    {
      key: 'evidence',
      label: '证据',
      width: '236px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>
            服务 {group.service_count} · 域名 {group.domain_count}
          </strong>
          <span>Target {group.running_target_count}/{group.target_count} · 监控 {group.monitoring_link_count}</span>
          <span>资料缺口 {group.evidence_assessment.gap_signal_count} · 风险 {group.evidence_assessment.risk_signal_count}</span>
        </div>
      ),
    },
    {
      key: 'assessment',
      label: '判断尺度',
      width: '238px',
      render: (group) => renderEvidenceAssessment(group.evidence_assessment),
    },
    {
      key: 'cost',
      label: '成本',
      width: '176px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>{formatGroupMonthlyCost(group)}</strong>
          <span>{formatGroupYearlyCost(group)}</span>
        </div>
      ),
    },
    {
      key: 'updated',
      label: '更新',
      width: '160px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>{formatDateTime(group.updated_at)}</strong>
          <span>{group.archived_at ? `归档 ${formatDateTime(group.archived_at)}` : '持续跟进'}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '128px',
      render: (group) => (
        <button className="btn sm primary" type="button" onClick={() => openManualGroup(group.manual_group_id)}>
          查看组合
        </button>
      ),
    },
  ]

  const recordColumns: DataTableColumn<AssetDecisionRecordSummary>[] = [
    {
      key: 'record',
      label: '决策记录',
      width: '286px',
      render: (record) => (
        <div className="asset-table__identity asset-decision-record-cell">
          <strong>{record.title}</strong>
          <span>{VIEW_LABELS[record.source_view]} · {record.scope_label || record.source_group_id}</span>
          <span>{record.source_group_type} · {record.source_group_id}</span>
          <span>{record.goal || '暂无目标备注'}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '136px',
      render: (record) => (
        <div className="asset-table__stack">
          <Badge variant="state" tone={recordStatusTone(record.status)}>
            {RECORD_STATUS_LABELS[record.status]}
          </Badge>
          <span>{record.renew_within_days} 天窗口</span>
        </div>
      ),
    },
    {
      key: 'scope',
      label: '推进',
      width: '220px',
      render: (record) => (
        <div className="asset-table__stack">
          <strong>
            推进 <MonoDigits>{recordFollowupDoneCount(record)}</MonoDigits>/<MonoDigits>{record.member_count}</MonoDigits>
          </strong>
          <span>
            待处理 {record.followup_todo_count ?? 0} · 处理中 {record.followup_in_progress_count ?? 0}
          </span>
          <span>
            阻塞 {record.followup_blocked_count ?? 0} · 未关闭 {recordFollowupOpenCount(record)}
          </span>
        </div>
      ),
    },
    {
      key: 'readback',
      label: '执行回读',
      width: '228px',
      render: (record) => (
        <div className="asset-table__stack asset-decision-readback-cell">
          <span className="asset-decision-chip-row">
            {renderReadbackBadge(record.execution_readback)}
          </span>
          <strong>{record.execution_readback?.summary || '等待执行证据回读'}</strong>
          <span>{readbackCountSummary(record.execution_readback)}</span>
        </div>
      ),
    },
    {
      key: 'plan',
      label: '执行编排',
      width: '260px',
      render: (record) => (
        <div className="asset-table__stack asset-decision-plan-cell">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={record.execution_plan?.blocked_count > 0 ? 'critical' : record.execution_plan?.actionable_count > 0 ? 'maintenance' : 'normal'}>
              下一步导览
            </Badge>
            {record.execution_plan?.actionable_count > 0 && (
              <Badge variant="count" tone="maintenance">
                {record.execution_plan.actionable_count} 项
              </Badge>
            )}
          </span>
          <strong>{record.execution_plan?.summary || '等待执行编排'}</strong>
          <span>{executionPlanCountSummary(record.execution_plan)}</span>
        </div>
      ),
    },
    {
      key: 'updated',
      label: '更新时间',
      width: '168px',
      render: (record) => (
        <div className="asset-table__stack">
          <strong>{formatDateTime(record.updated_at)}</strong>
          <span>{record.completed_at ? `完成 ${formatDateTime(record.completed_at)}` : record.decided_at ? `决策 ${formatDateTime(record.decided_at)}` : '尚未决策'}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '112px',
      render: (record) => (
        <button className="btn sm primary" type="button" onClick={() => openRecord(record.record_id)}>
          查看记录
        </button>
      ),
    },
  ]

  const memberColumns: DataTableColumn<AssetDecisionGroupMember>[] = [
    {
      key: 'vps',
      label: 'VPS',
      width: '240px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={`/vps/${member.vps.vps_id}`}>{member.vps.display_name}</Link></strong>
          <span>{formatOptional(member.vps.provider_name)} · {vpsLocationLabel(member.vps)}</span>
          <span>{member.vps.product_name || member.vps.vps_id}</span>
          <span className="asset-decision-chip-row">
            <LifecycleBadge value={member.vps.lifecycle_status} />
            <UsageBadge value={member.vps.usage_status} />
            <RenewalBadge value={member.vps.renewal_decision} />
          </span>
        </div>
      ),
    },
    {
      key: 'subscription',
      label: '订阅',
      width: '188px',
      render: (member) => {
        const sub = member.primary_subscription
        if (!member.source_availability.subscriptions) {
          return (
            <div className="asset-subscription-cell asset-subscription-cell--unknown">
              <strong>订阅证据不可用</strong>
              <span>不会按缺订阅误判</span>
            </div>
          )
        }
        if (!sub) {
          return (
            <div className="asset-subscription-cell asset-subscription-cell--missing">
              <strong>缺订阅</strong>
              <span>需回 VPS 详情补齐</span>
            </div>
          )
        }
        const daysLeft = daysUntilDate(sub.renew_at)
        return (
          <div className="asset-subscription-cell">
            <strong>{formatMoney(sub.monthly_price, sub.currency)}/月</strong>
            <span>{formatDate(sub.renew_at)} {daysLeft != null ? `· ${daysLeft}天` : ''}</span>
            <SubscriptionStatusBadge value={sub.status} />
          </div>
        )
      },
    },
    {
      key: 'context',
      label: '上下文',
      width: '248px',
      render: (member) => (
        <div className="asset-table__stack">
          <strong>{memberContextLabel(member)}</strong>
          <span>{sourceAvailabilityLabel(member.source_availability)}</span>
          <span>{member.primary_issue_summary || '暂无主要问题'}</span>
        </div>
      ),
    },
    {
      key: 'suggestion',
      label: '建议',
      width: '244px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.suggested_role)}>
              {ROLE_LABELS[member.suggested_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.suggested_action)}>
              {ACTION_LABELS[member.suggested_action]}
            </Badge>
          </span>
          {renderDecisionRecommendation(member.decision_recommendation)}
          {renderEvidenceAssessment(member.evidence_assessment)}
          {member.cancellation_attention_reason && <span>{member.cancellation_attention_reason}</span>}
          {renderEvidenceChips(member.evidence_chips, 4)}
        </div>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '172px',
      render: (member) => (
        <div className="asset-decision-member-actions">
          <button className="btn sm primary" type="button" onClick={() => selectVPS(member.vps)}>
            处理
          </button>
          {member.suggested_action === 'open_cancellation_workbench' ? (
            <Link className="btn sm secondary" to={`/vps/${member.vps.vps_id}?workbench=cancellation`}>
              取消/退役
            </Link>
          ) : (
            <Link className="btn sm secondary" to={`/vps/${member.vps.vps_id}`}>
              VPS
            </Link>
          )}
        </div>
      ),
    },
  ]

  const manualMemberColumns: DataTableColumn<AssetDecisionManualGroupMember>[] = [
    {
      key: 'vps',
      label: 'VPS',
      width: '236px',
      render: (member) => {
        const displayName = member.current_fact_found ? member.vps.display_name : member.vps_id
        return (
          <div className="asset-table__identity">
            <strong>
              {member.current_fact_found ? (
                <Link className="name" to={`/vps/${member.vps_id}`}>{displayName}</Link>
              ) : (
                displayName
              )}
            </strong>
            <span>{member.current_fact_found ? `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}` : '当前资产事实缺失'}</span>
            <span>{member.current_fact_found ? member.vps.product_name || member.vps_id : member.vps_id}</span>
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
          </div>
        )
      },
    },
    {
      key: 'context',
      label: '当前上下文',
      width: '248px',
      render: (member) => (
        <div className="asset-table__stack">
          <strong>{member.current_fact_found ? memberContextLabel(member) : '无法回读当前事实'}</strong>
          <span>{member.current_fact_found ? sourceAvailabilityLabel(member.source_availability) : '手工组合成员仍在，但当前 facts 未返回该 VPS'}</span>
          <span>{member.primary_issue_summary || '暂无主要问题'}</span>
        </div>
      ),
    },
    {
      key: 'intent',
      label: '组合意图',
      width: '336px',
      render: (member) => {
        const draft = manualMemberDrafts[member.vps_id] ?? {
          intendedRole: member.intended_role,
          intendedAction: member.intended_action,
          reason: member.reason,
          note: member.note,
          sortOrder: String(member.sort_order),
        }
        const isSaving = Boolean(manualMemberSaving[member.vps_id])
        return (
          <form className="asset-decision-manual-member-form" onSubmit={(event) => submitManualMemberPatch(event, member)}>
            <label className="visually-hidden" htmlFor={`manual-role-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 组合角色
            </label>
            <select
              id={`manual-role-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 组合角色`}
              className="input"
              value={draft.intendedRole}
              onChange={(event) => updateManualMemberDraft(member.vps_id, { intendedRole: event.target.value as AssetDecisionSuggestedRole })}
            >
              {ROLE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <label className="visually-hidden" htmlFor={`manual-action-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 组合动作
            </label>
            <select
              id={`manual-action-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 组合动作`}
              className="input"
              value={draft.intendedAction}
              onChange={(event) => updateManualMemberDraft(member.vps_id, { intendedAction: event.target.value as AssetDecisionSuggestedAction })}
            >
              {ACTION_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <label className="visually-hidden" htmlFor={`manual-reason-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 意图理由
            </label>
            <input
              id={`manual-reason-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 意图理由`}
              className="input"
              value={draft.reason}
              placeholder="理由"
              onChange={(event) => updateManualMemberDraft(member.vps_id, { reason: event.target.value })}
            />
            <label className="visually-hidden" htmlFor={`manual-note-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 备注
            </label>
            <input
              id={`manual-note-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 备注`}
              className="input"
              value={draft.note}
              placeholder="备注"
              onChange={(event) => updateManualMemberDraft(member.vps_id, { note: event.target.value })}
            />
            <div className="asset-decision-manual-member-form__actions">
              <button className="btn sm primary" type="submit" disabled={isSaving}>
                {isSaving ? '保存中…' : '保存意图'}
              </button>
              <button className="btn sm secondary" type="button" onClick={() => deleteManualMember(member)} disabled={isSaving}>
                移除
              </button>
            </div>
          </form>
        )
      },
    },
    {
      key: 'evidence',
      label: '证据',
      width: '250px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.intended_role)}>
              {ROLE_LABELS[member.intended_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.intended_action)}>
              {ACTION_LABELS[member.intended_action]}
            </Badge>
          </span>
          {renderDecisionRecommendation(member.decision_recommendation)}
          {renderEvidenceAssessment(member.evidence_assessment)}
          {renderEvidenceChips(member.evidence_chips, 3)}
        </div>
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
          <strong><Link className="name" to={`/vps/${member.vps_id}`}>{member.display_name || member.vps_id}</Link></strong>
          <span>{member.vps_id}</span>
          <span>保存于 {formatDateTime(member.created_at)}</span>
        </div>
      ),
    },
    {
      key: 'suggested',
      label: '系统建议',
      width: '180px',
      render: (member) => (
        <span className="asset-decision-chip-row">
          <Badge variant="state" tone={roleTone(member.suggested_role)}>
            {ROLE_LABELS[member.suggested_role]}
          </Badge>
          <Badge variant="state" tone={actionTone(member.suggested_action)}>
            {ACTION_LABELS[member.suggested_action]}
          </Badge>
        </span>
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
        const assessment = parseEvidenceAssessment(member.evidence_snapshot)
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
      width: '312px',
      render: (member) => renderMemberReadback(member.execution_readback),
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
              aria-label={`${member.display_name || member.vps_id} 跟进状态`}
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
              aria-label={`${member.display_name || member.vps_id} 跟进备注`}
              className="input"
              value={draft.note}
              placeholder="备注 / 阻塞原因"
              onChange={(event) => updateRecordFollowupDraft(member.vps_id, { note: event.target.value })}
            />
            <div className="asset-decision-followup-form__actions">
              <button className="btn sm primary" type="submit" disabled={isSaving || !isChanged}>
                {isSaving ? '保存中…' : '保存跟进'}
              </button>
              <Link className="btn sm secondary" to={actionHrefForMember(member)}>
                {actionLabelForMember(member)}
              </Link>
            </div>
          </form>
        )
      },
    },
  ]

  function setWorkbenchView(next: MainWorkbenchView) {
    setPortfolioState((current) => ({
      ...current,
      overviewLoading: true,
      overviewError: null,
      groupsLoading: true,
      groupsError: null,
    }))
    const nextParams = new URLSearchParams(searchParams)
    nextParams.set('view', next)
    nextParams.set('renew_within_days', String(renewalWindow))
    setSearchParams(nextParams)
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
    const nextParams = new URLSearchParams(searchParams)
    nextParams.set('view', portfolioView)
    nextParams.set('renew_within_days', String(nextWindow))
    setSearchParams(nextParams)
  }

  function updateAssetDecisionSearchParams(mutator: (next: URLSearchParams) => void) {
    const nextParams = new URLSearchParams(searchParams)
    mutator(nextParams)
    if (nextParams.toString() !== searchParams.toString()) {
      setSearchParams(nextParams)
    }
  }

  function setOpenState(key: OpenStateKey, value: string) {
    updateAssetDecisionSearchParams((nextParams) => {
      for (const openKey of OPEN_STATE_KEYS) {
        if (openKey !== key) nextParams.delete(openKey)
      }
      nextParams.set(key, value)
    })
  }

  function clearOpenState(key: OpenStateKey) {
    updateAssetDecisionSearchParams((nextParams) => {
      nextParams.delete(key)
    })
  }

  function clearContextFilter(key: ContextFilterKey) {
    updateAssetDecisionSearchParams((nextParams) => {
      nextParams.delete(key)
    })
  }

  function clearAllContextFilters() {
    updateAssetDecisionSearchParams((nextParams) => {
      for (const key of CONTEXT_FILTER_KEYS) nextParams.delete(key)
    })
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
    clearOpenState('group_id')
  }

  function openManualGroup(manualGroupID: string) {
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
    setManualDetailState({ loading: true, error: null, detail: null })
    setSelectedManualGroupID(manualGroupID)
    setOpenState('manual_group_id', manualGroupID)
  }

  function closeManualGroupDetail() {
    setSelectedManualGroupID(null)
    setManualDetailState(INITIAL_MANUAL_DETAIL_STATE)
    setManualGroupError(null)
    setManualMemberDrafts({})
    setManualMemberSaving({})
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
    clearOpenState('manual_group_id')
  }

  function openTemplate(templateID: string) {
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
    setTemplateDetailState({ loading: true, error: null, detail: null })
    setSelectedTemplateID(templateID)
    setOpenState('template_id', templateID)
  }

  function closeTemplateDetail() {
    setSelectedTemplateID(null)
    setTemplateDetailState(INITIAL_TEMPLATE_DETAIL_STATE)
    setTemplateError(null)
    setTemplateManualDraft({
      title: '',
      goal: '',
      note: '',
      renewWithinDays: renewalWindow,
    })
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
    setManualDetailState({ loading: false, error: null, detail })
    setManualMemberDrafts(buildManualMemberDrafts(detail))
    setManualGroupsState((current) => ({
      loading: false,
      error: null,
      groups: upsertManualGroupSummary(current.groups, detail),
    }))
  }

  function applyTemplateDetail(detail: AssetDecisionScenarioTemplateDetail) {
    setTemplateDetailState({ loading: false, error: null, detail })
    setTemplateManualDraft({
      title: detail.title,
      goal: detail.goal,
      note: detail.note,
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
        setSelectedGroupID(null)
        setDetailState(INITIAL_DETAIL_STATE)
        setSelectedManualGroupID(manualDetail.manual_group_id)
        setOpenState('manual_group_id', manualDetail.manual_group_id)
        setDecisionNotice(`已创建自定义组合：${manualDetail.title}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '创建自定义组合失败'))
      })
      .finally(() => setManualGroupCreating(false))
  }

  function startRecordSave(detail: AssetDecisionGroupDetail) {
    setRecordDraft(buildRecordDraft(detail, renewalWindow))
    setRecordSaveError(null)
  }

  function startManualRecordSave(detail: AssetDecisionManualGroupDetail) {
    setRecordDraft(buildManualRecordDraft(detail))
    setRecordSaveError(null)
  }

  function cancelRecordSave() {
    setRecordDraft(null)
    setRecordSaveError(null)
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

  function submitRecordSave(event: FormEvent<HTMLFormElement>) {
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
      })
      .catch((error: unknown) => {
        setRecordSaveError(describeError(error, '保存组合决策记录失败'))
      })
      .finally(() => setRecordSaving(false))
  }

  function openRecord(recordID: string) {
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
    setOpenState('record_id', recordID)
  }

  function closeRecordDetail() {
    setSelectedRecordID(null)
    setRecordDetailState(INITIAL_RECORD_DETAIL_STATE)
    setRecordPatchError(null)
    setRecordFollowupDrafts({})
    setRecordFollowupPatching({})
    clearOpenState('record_id')
  }

  function submitTemplateManualGroup(event: FormEvent<HTMLFormElement>) {
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
        setOpenState('template_id', template.template_id)
        setDecisionNotice(`已另存为场景模板：${template.title}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '另存为场景模板失败'))
      })
      .finally(() => setTemplateSaving(false))
  }

  function submitRecordStatus(event: FormEvent<HTMLFormElement>) {
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

  function updateManualMemberDraft(vpsID: string, patch: Partial<ManualMemberDraft>) {
    setManualMemberDrafts((current) => ({
      ...current,
      [vpsID]: {
        ...current[vpsID],
        ...patch,
      },
    }))
  }

  function submitManualGroupPatch(event: FormEvent<HTMLFormElement>) {
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

  function submitManualMemberAdd(event: FormEvent<HTMLFormElement>) {
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
        setDecisionNotice('自定义组合成员已加入')
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '新增自定义组合成员失败'))
      })
      .finally(() => setManualGroupSaving(false))
  }

  function submitManualMemberPatch(event: FormEvent<HTMLFormElement>, member: AssetDecisionManualGroupMember) {
    event.preventDefault()
    const detail = manualDetailState.detail
    if (!detail) return
    const draft = manualMemberDrafts[member.vps_id] ?? {
      intendedRole: member.intended_role,
      intendedAction: member.intended_action,
      reason: member.reason,
      note: member.note,
      sortOrder: String(member.sort_order),
    }
    const parsedSortOrder = Number.parseInt(draft.sortOrder, 10)
    setManualGroupError(null)
    setManualMemberSaving((current) => ({ ...current, [member.vps_id]: true }))
    patchAssetDecisionManualGroupMember(detail.manual_group_id, member.vps_id, {
      intended_role: draft.intendedRole,
      intended_action: draft.intendedAction,
      reason: draft.reason.trim(),
      note: draft.note.trim(),
      ...(Number.isFinite(parsedSortOrder) ? { sort_order: parsedSortOrder } : {}),
    })
      .then((updated) => {
        applyManualDetail(updated)
        setDecisionNotice(`成员意图已更新：${member.current_fact_found ? member.vps.display_name : member.vps_id}`)
      })
      .catch((error: unknown) => {
        setManualGroupError(describeError(error, '更新成员意图失败'))
      })
      .finally(() => {
        setManualMemberSaving((current) => ({ ...current, [member.vps_id]: false }))
      })
  }

  function deleteManualMember(member: AssetDecisionManualGroupMember) {
    const detail = manualDetailState.detail
    if (!detail) return
    setManualGroupError(null)
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

  function updateRecordFollowupDraft(vpsID: string, patch: Partial<RecordFollowupDraft>) {
    setRecordFollowupDrafts((current) => ({
      ...current,
      [vpsID]: {
        ...current[vpsID],
        ...patch,
      },
    }))
  }

  function submitRecordMemberFollowup(event: FormEvent<HTMLFormElement>, member: AssetDecisionRecordMember) {
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
    const lanes = EXECUTION_PLAN_LANE_ORDER
      .map((lane) => ({ lane, members: membersForExecutionLane(detail.members, lane) }))
      .filter((section) => section.members.length > 0)

    if (lanes.length === 0) {
      return (
        <section className="asset-decision-execution-board" aria-label="执行编排">
          <div className="asset-decision-execution-board__empty">
            <strong>暂无执行编排</strong>
            <span>当前记录没有可展示的成员步骤。</span>
          </div>
        </section>
      )
    }

    return (
      <section className="asset-decision-execution-board" aria-label="执行编排">
        <div className="asset-decision-execution-board__header">
          <div>
            <p className="section-heading__eyebrow">EXECUTION PLAN</p>
            <h3>执行编排</h3>
            <span>{detail.execution_plan?.summary || '按成员当前事实生成下一步导览。'}</span>
          </div>
          <div className="asset-decision-execution-board__counts">
            <Badge variant="count" tone={detail.execution_plan?.actionable_count > 0 ? 'maintenance' : 'normal'}>
              可推进 {detail.execution_plan?.actionable_count ?? 0}
            </Badge>
            {detail.execution_plan?.blocked_count > 0 && (
              <Badge variant="count" tone="critical">
                阻塞 {detail.execution_plan.blocked_count}
              </Badge>
            )}
          </div>
        </div>
        <div className="asset-decision-execution-board__lanes">
          {lanes.map(({ lane, members }) => (
            <section key={lane} className={`asset-decision-execution-lane asset-decision-execution-lane--${lane}`}>
              <div className="asset-decision-execution-lane__header">
                <div>
                  <strong>{EXECUTION_PLAN_LANE_LABELS[lane]}</strong>
                  <span><MonoDigits>{members.length}</MonoDigits> 台 · 可推进 {laneActionableCount(members)} · 问题 {laneIssueCount(members)}</span>
                </div>
                {laneBlockedCount(members) > 0 && (
                  <Badge variant="count" tone="critical">
                    阻塞 {laneBlockedCount(members)}
                  </Badge>
                )}
              </div>
              <div className="asset-decision-execution-lane__members">
                {members.map((member) => {
                  const isSaving = Boolean(recordFollowupPatching[member.vps_id])
                  const canStart = member.followup_status === 'todo'
                  const canMarkDone = member.execution_readback?.status === 'aligned' && member.followup_status !== 'done' && member.followup_status !== 'skipped'
                  return (
                    <article key={member.vps_id} className="asset-decision-execution-card">
                      <div className="asset-decision-execution-card__head">
                        <strong><Link className="name" to={`/vps/${member.vps_id}`}>{member.display_name || member.vps_id}</Link></strong>
                        <span className="asset-decision-chip-row">
                          {renderExecutionPlanBadge(member.execution_plan)}
                          {renderReadbackBadge(member.execution_readback)}
                        </span>
                      </div>
                      <p>{member.execution_plan?.summary || '等待执行编排'}</p>
                      <div className="asset-decision-execution-card__facts">
                        <span>当前事实</span>
                        <strong>{currentFactsLabel(member.execution_readback?.current_facts)}</strong>
                        <small>{currentFactsStateLabel(member.execution_readback?.current_facts)}</small>
                      </div>
                      {member.execution_readback?.issues?.length > 0 && (
                        <span className="asset-decision-chip-row">
                          {member.execution_readback.issues.slice(0, 3).map((issue) => (
                            <Badge key={`${member.vps_id}-${issue.kind}-${issue.label}`} variant="info" tone={chipTone(issue.tone)}>
                              {issue.label}
                            </Badge>
                          ))}
                          {member.execution_readback.issues.length > 3 && (
                            <Badge variant="count" tone="neutral">
                              +{member.execution_readback.issues.length - 3}
                            </Badge>
                          )}
                        </span>
                      )}
                      <div className="asset-decision-execution-card__actions">
                        {member.execution_plan?.step_kind === 'review_record' ? (
                          <button className="btn sm secondary" type="button" onClick={() => setDecisionNotice(`请在当前记录中复核：${member.display_name || member.vps_id}`)}>
                            {actionLabelForMember(member)}
                          </button>
                        ) : (
                          <Link className="btn sm secondary" to={actionHrefForMember(member)}>
                            {actionLabelForMember(member)}
                          </Link>
                        )}
                        {canStart && (
                          <button className="btn sm secondary" type="button" disabled={isSaving} onClick={() => saveRecordMemberFollowup(member, 'in_progress')}>
                            开始跟进
                          </button>
                        )}
                        {canMarkDone && (
                          <button className="btn sm primary" type="button" disabled={isSaving} onClick={() => saveRecordMemberFollowup(member, 'done')}>
                            标记完成
                          </button>
                        )}
                      </div>
                    </article>
                  )
                })}
              </div>
            </section>
          ))}
        </div>
      </section>
    )
  }

  function renderDecisionGroupCards(groups: AssetDecisionGroupSummary[]) {
    return (
      <div className="asset-decision-group-cards" aria-label="决策组扫描列表">
        {groups.map((group, index) => {
          const assessment = group.evidence_assessment
          const recommendation = group.decision_recommendation
          const hasOperationalRisk = group.cancellation_attention_count > 0
            || group.active_incident_count > 0
            || group.abnormal_monitoring_count > 0
            || group.evidence_chips.some((chip) => chip.tone === 'critical' || chip.tone === 'alert')
          const tone = hasOperationalRisk || assessment.gap_signal_count > 0 ? 'alert' : 'normal'
          return (
            <article key={group.group_id} className={`asset-decision-group-card asset-decision-group-card--${tone}`}>
              <div className="asset-decision-group-card__rank">
                <strong>P{index + 1}</strong>
                <span>{VIEW_LABELS[group.view]}</span>
              </div>
              <div className="asset-decision-group-card__body">
                <div className="asset-decision-group-card__head">
                  <div>
                    <strong>{group.title}</strong>
                    <span>{group.scope_label} · {group.primary_issue_summary || '暂无主要问题'}</span>
                  </div>
                  <span className="asset-decision-chip-row">
                    <Badge variant="info" tone={tone}>
                      {groupPressureLabel(group)}
                    </Badge>
                    <Badge variant="state" tone={evidenceTierTone(assessment.quality_tier)}>
                      {EVIDENCE_TIER_LABELS[assessment.quality_tier] ?? assessment.quality_tier}
                    </Badge>
                    <Badge variant="state" tone={evidenceBiasTone(assessment.decision_bias)}>
                      {EVIDENCE_BIAS_LABELS[assessment.decision_bias] ?? assessment.decision_bias}
                    </Badge>
                  </span>
                </div>

                <div className="asset-decision-group-card__brief">
                  <div className="asset-decision-group-card__recommendation">
                    <span>NEXT STEP</span>
                    <strong>{recommendation?.summary || '打开组详情继续比较成员'}</strong>
                    <small>{recommendation?.next_step || group.primary_issue_summary || '核对成本、承载和证据缺口后保存判断。'}</small>
                  </div>
                  <div className="asset-decision-group-card__assessment">
                    {renderEvidenceAssessment(assessment)}
                  </div>
                </div>

                <div className="asset-decision-group-card__metrics" aria-label={`${group.title} 关键证据`}>
                  <div>
                    <span>组合范围</span>
                    <strong><MonoDigits>{group.member_count}</MonoDigits> 台 VPS</strong>
                    <small>{countSummary(group.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</small>
                  </div>
                  <div>
                    <span>承载证据</span>
                    <strong>{groupServiceLabel(group)}</strong>
                    <small>{groupMonitoringLabel(group)}</small>
                  </div>
                  <div>
                    <span>成本证据</span>
                    <strong>{formatGroupMonthlyCost(group)}</strong>
                    <small>{formatGroupYearlyCost(group)}</small>
                  </div>
                </div>

                <div className="asset-decision-group-card__evidence">
                  {renderEvidenceChips(group.evidence_chips, 5)}
                  <small>{assessment.summary}</small>
                </div>
              </div>
              <div className="asset-decision-group-card__actions">
                <button className="btn sm primary" type="button" onClick={() => openGroup(group.group_id)}>
                  查看组
                </button>
              </div>
            </article>
          )
        })}
      </div>
    )
  }

  function selectVPS(vps: VPSAssetRecord) {
    setSelectedVPS(vps)
    setDecisionDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setDecisionError(null)
    setDecisionNotice(null)
  }

  function navigateToVPS(vps: VPSAssetRecord) {
    navigate(`/vps/${vps.vps_id}`)
  }

  function navigateToVPSSubscription(vpsID: string) {
    navigate(`/vps/${vpsID}?workbench=subscription`)
  }

  function openPortfolioLead() {
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

  function openDecisionPathStep(step: AssetDecisionPathStep) {
    if (step.unavailable) return
    if (step.target?.type === 'record') {
      openRecord(step.target.id)
      return
    }
    if (step.target?.type === 'manual_group') {
      openManualGroup(step.target.id)
      return
    }
    if (step.target?.type === 'template') {
      openTemplate(step.target.id)
      return
    }
    if (step.target?.type === 'group') {
      openGroup(step.target.id)
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
  }

  function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
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
          <h1 className="page-title">资产组合决策</h1>
          <p className="page-sub">从 VPS、订阅、服务、域名和监控证据中派生组合取舍。</p>
        </div>
        <div className="header-actions">
          <Link className="btn md secondary" to="/vps">VPS 库存</Link>
          <Link className="btn md secondary" to="/subscriptions">订阅列表</Link>
        </div>
      </div>

      {decisionNotice && (
        <div className="inline-alert ok" role="status">{decisionNotice}</div>
      )}

      <section className={`asset-decision-focus asset-decision-command-summary asset-decision-command-summary--${portfolioLead.tone} animate-in d1`} aria-label="资产组合决策当前判断">
        <div className="asset-decision-command-summary__lead">
          <span>{portfolioLead.eyebrow}</span>
          <h2>{portfolioLead.title}</h2>
          <p>{portfolioLead.summary}</p>
          <div className="asset-decision-command-summary__actions">
            <button className="btn md primary" type="button" onClick={openPortfolioLead}>
              {portfolioLead.actionLabel}
            </button>
            <Link className="btn md secondary" to={`/asset-decisions?view=evidence&renew_within_days=${renewalWindow}&scenario=evidence_cleanup`}>
              资料缺口
            </Link>
          </div>
        </div>
        <div className="asset-decision-command-summary__facts" aria-label="资产组合决策当前事实">
          <div className="asset-decision-focus__item asset-decision-focus__item--notice">
            <span>PORTFOLIO</span>
            <strong>{portfolioState.overviewLoading ? '...' : overview?.group_count ?? portfolioState.groups.length}</strong>
            <small>{portfolioLead.contextLabel}</small>
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--alert">
            <span>RENEWAL</span>
            <strong>{portfolioState.overviewLoading ? '...' : overview?.renewal_group_count ?? 0}</strong>
            <small>{portfolioLead.renewalLabel}</small>
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--critical">
            <span>CLOSED LOOP</span>
            <strong>{closedLoopMetrics.readbackDriftCount + closedLoopMetrics.readbackBlockedCount + closedLoopMetrics.readbackNeedsEvidenceCount}</strong>
            <small>{portfolioLead.riskLabel}</small>
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--normal">
            <span>EVIDENCE</span>
            <strong>{overview ? '5' : '—'}</strong>
            <small>{portfolioLead.evidenceLabel}</small>
          </div>
        </div>
      </section>

      <section className="page-panel asset-decision-path animate-in d2" aria-label="资产组合决策推进路径">
        <div className="asset-decision-path__header">
          <div>
            <p className="section-heading__eyebrow">DECISION PATH</p>
            <h2>决策路径</h2>
            <p>从自动发现、场景比较、保存判断到执行回读，按当前已加载事实推进。</p>
          </div>
          <Badge variant="state" tone={closedLoopMetrics.partialErrorCount > 0 ? 'alert' : 'normal'}>
            {closedLoopMetrics.partialErrorCount > 0 ? '局部证据缺失' : '路径可用'}
          </Badge>
        </div>
        <ol className="asset-decision-path__rail">
          {decisionPathSteps.map((step, index) => (
            <li key={step.stage} className={`asset-decision-path__step asset-decision-path__step--${step.tone}`}>
              <div className="asset-decision-path__index">
                <strong><MonoDigits>{index + 1}</MonoDigits></strong>
                <span>{step.eyebrow}</span>
              </div>
              <div className="asset-decision-path__body">
                <div className="asset-decision-path__title">
                  <strong>{step.title}</strong>
                  <Badge variant="state" tone={step.tone}>{step.metric}</Badge>
                </div>
                <p>{step.summary}</p>
                <small>{step.meta}</small>
              </div>
              <button
                className="btn sm secondary"
                type="button"
                onClick={() => openDecisionPathStep(step)}
                disabled={step.unavailable}
              >
                {step.actionLabel}
              </button>
            </li>
          ))}
        </ol>
      </section>

      {isSingleQueueDeepLink && (
        <div className="inline-alert info asset-decision-deeplink-notice" role="status">
          旧链接已承接到单台辅助队列；组合判断仍以决策组列表为主。
          <a className="alert-action" href="#single-vps-queue">查看单台队列</a>
        </div>
      )}

      <div className="asset-decision-primary-grid animate-in d2">
        <section className="page-panel asset-decision-command">
          <div className="asset-decision-board__header">
            <div>
              <p className="section-heading__eyebrow">PORTFOLIO WORKBENCH</p>
              <h2>决策组列表</h2>
              <p>
                当前视图：{VIEW_LABELS[portfolioView]}。自动组只读派生，不会创建持久化决策记录。
                {overview?.snapshot_generated_at ? `快照 ${formatDateTime(overview.snapshot_generated_at)}。` : ''}
              </p>
            </div>
            <div className="asset-decision-board__tools">
              <div className="asset-decision-window">
                <span>续费窗口</span>
                <select
                  className="input filter-select--inline"
                  aria-label="续费窗口"
                  value={String(renewalWindow)}
                  onChange={(event) => changeRenewalWindow(event.target.value)}
                >
                  {RENEWAL_WINDOWS.map((value) => (
                    <option key={value} value={value}>未来 {value} 天</option>
                  ))}
                </select>
              </div>
              <p>{portfolioState.overviewError ? '组合概览不可用' : `当前显示 ${portfolioState.groups.length} 个组`}</p>
            </div>
          </div>
          <div className="asset-decision-tabs">
            <Tabs items={workbenchTabs} value={portfolioView} onChange={setWorkbenchView} variant="pill" />
            {contextFilterChips.length > 0 && (
              <div className="asset-decision-filter-chips" aria-label="资产决策上下文筛选">
                {contextFilterChips.map((chip) => (
                  <FilterChip
                    key={chip.key}
                    label={`${chip.label}: ${chip.value}`}
                    onRemove={() => clearContextFilter(chip.key)}
                  />
                ))}
                <button className="filter-clear" type="button" onClick={clearAllContextFilters}>清除上下文</button>
              </div>
            )}
          </div>

          {portfolioState.groupsLoading ? (
            <PageStateView
              kind="loading"
              title="正在加载决策组…"
              surface="empty"
              compact
            />
          ) : portfolioState.groupsError ? (
            <PageStateView
              kind="error"
              title="决策组不可用"
              description={<>{portfolioState.groupsError}</>}
              technicalSummary={portfolioState.groupsError}
              surface="empty"
              compact
            />
          ) : portfolioState.groups.length === 0 ? (
            <PageStateView
              kind="empty"
              title="当前视图暂无决策组"
              description="可切换到需要决策或资料缺口；单台队列仍在页面底部保留。"
              action={<button className="btn sm secondary" onClick={() => setWorkbenchView('needs_decision')}>查看需要决策</button>}
              surface="empty"
              compact
            />
          ) : (
            renderDecisionGroupCards(portfolioState.groups)
          )}
        </section>

        <aside className="page-panel asset-decision-closed-loop" aria-label="资产决策闭环导览">
          <div className="asset-decision-closed-loop__header">
            <div>
              <p className="section-heading__eyebrow">CLOSED LOOP</p>
              <h2>下一步导览</h2>
              <p>只读收敛当前最值得处理的组合工作，不自动修改任何资产状态。</p>
            </div>
            <Badge variant="state" tone={closedLoopMetrics.partialErrorCount > 0 ? 'alert' : 'normal'}>
              {closedLoopMetrics.partialErrorCount > 0 ? '部分证据不可用' : '证据可导览'}
            </Badge>
          </div>

          <div className="asset-decision-closed-loop__metrics" aria-label="资产决策闭环状态">
            <div>
              <span>AUTO</span>
              <strong><MonoDigits>{closedLoopMetrics.autoGroupCount}</MonoDigits></strong>
              <small>自动组</small>
            </div>
            <div>
              <span>DRIFT</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackDriftCount}</MonoDigits></strong>
              <small>事实漂移</small>
            </div>
            <div>
              <span>BLOCKED</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackBlockedCount}</MonoDigits></strong>
              <small>阻塞</small>
            </div>
            <div>
              <span>GAP</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackNeedsEvidenceCount + closedLoopMetrics.evidenceGapGroupCount}</MonoDigits></strong>
              <small>资料缺口</small>
            </div>
          </div>

          <div className="asset-decision-next-work" aria-label="资产决策下一步工作项">
            {portfolioState.groupsLoading || recordsState.loading || manualGroupsState.loading || templatesState.loading ? (
              <PageStateView
                kind="loading"
                title="正在生成下一步导览…"
                surface="empty"
                compact
              />
            ) : nextWorkItems.length === 0 ? (
              <PageStateView
                kind="empty"
                title="暂无需要置顶的组合工作"
                description="当前已加载数据没有漂移、阻塞、缺证据或可启动场景。"
                surface="empty"
                compact
              />
            ) : (
              <ol className="asset-decision-next-work__list">
                {nextWorkItems.slice(0, 5).map((item, index) => (
                  <li
                    key={item.id}
                    className={`asset-decision-next-work__item asset-decision-next-work__item--${item.tone}`}
                  >
                    <div className="asset-decision-next-work__rank">
                      <strong>P{index + 1}</strong>
                      <span>{item.sourceLabel}</span>
                    </div>
                    <div className="asset-decision-next-work__body">
                      <span className="asset-decision-chip-row">
                        <Badge variant="state" tone={item.tone}>{item.kindLabel}</Badge>
                        <Badge variant="info" tone="neutral">{nextWorkTargetLabel(item.target)}</Badge>
                      </span>
                      <strong>{item.title}</strong>
                      <span>{item.summary}</span>
                      <small>{item.meta}</small>
                    </div>
                    <div className="asset-decision-next-work__action">
                      <span>{item.target.type === 'record' ? '回读记录' : item.target.type === 'group' ? '自动组' : item.target.type === 'manual_group' ? '自定义组合' : '场景模板'}</span>
                      <button
                        className="btn sm secondary"
                        type="button"
                        onClick={() => openNextWorkItem(item)}
                      >
                        {item.actionLabel}
                      </button>
                    </div>
                  </li>
                ))}
              </ol>
            )}
          </div>

          {closedLoopPartialErrors.length > 0 && (
            <div className="inline-alert warn" role="status">
              {closedLoopPartialErrors.join('、')}暂不可用，导览只展示已成功加载的事实。
            </div>
          )}
        </aside>
      </div>

      <section className="page-panel asset-decision-scenario-records animate-in d3">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">SCENARIOS & MEMORY</p>
            <h2>场景与记录</h2>
            <p>模板负责启动场景，自定义组合承接真实比较，决策记录保存判断与执行回读。</p>
          </div>
          <div className="asset-decision-board__tools">
            <span className="section-count">
              模板 {templatesState.loading ? '...' : templatesState.error ? '不可用' : templatesState.templates.length}
            </span>
            <span className="section-count">
              组合 {manualGroupsState.loading ? '...' : manualGroupsState.error ? '不可用' : manualGroupsState.groups.length}
            </span>
            <span className="section-count">
              记录 {recordsState.loading ? '...' : recordsState.error ? '不可用' : recordsState.records.length}
            </span>
          </div>
        </div>

        <div className="asset-decision-scenario-records__grid">
          <section className="asset-decision-scenario-card asset-decision-templates" aria-label="资产决策场景模板">
            <div className="asset-decision-scenario-card__head">
              <div>
                <p className="section-heading__eyebrow">SCENARIO TEMPLATES</p>
                <h3>场景模板</h3>
                <span>作为场景启动器使用，不直接保存决策记录。</span>
              </div>
            </div>
            {templatesState.loading ? (
              <PageStateView kind="loading" title="正在加载场景模板…" surface="empty" compact />
            ) : templatesState.error ? (
              <PageStateView
                kind="error"
                title="场景模板不可用"
                description={<>{templatesState.error}</>}
                technicalSummary={templatesState.error}
                surface="empty"
                compact
              />
            ) : templatesState.templates.length === 0 ? (
              <PageStateView
                kind="empty"
                title="暂无场景模板"
                description="可以先从自动组创建自定义组合，再另存为模板。"
                surface="empty"
                compact
              />
            ) : (
              <div className="asset-decision-template-launchers">
                {templatesState.templates.slice(0, 6).map((template) => (
                  <article key={template.template_id} className="asset-decision-template-launcher">
                    <div>
                      <span className="asset-decision-chip-row">
                        <Badge variant="state" tone={scenarioTemplateStatusTone(template.status)}>
                          {SCENARIO_TEMPLATE_STATUS_LABELS[template.status]}
                        </Badge>
                        <Badge variant="info" tone={template.builtin ? 'notice' : 'neutral'}>
                          {template.builtin ? '内置' : '自定义'}
                        </Badge>
                      </span>
                      <strong>{template.title}</strong>
                      <span>{MANUAL_GROUP_SCENARIO_LABELS[template.scenario]} · {template.goal || template.note || '场景启动器'}</span>
                      <small>蓝图成员 <MonoDigits>{template.member_count}</MonoDigits></small>
                    </div>
                    <button className="btn sm secondary" type="button" onClick={() => openTemplate(template.template_id)}>
                      使用模板
                    </button>
                  </article>
                ))}
              </div>
            )}
          </section>

          <section className="asset-decision-scenario-card asset-decision-manual-groups" aria-label="自定义资产组合">
            <div className="asset-decision-scenario-card__head">
              <div>
                <p className="section-heading__eyebrow">SCENARIO WORKBENCH</p>
                <h3>自定义组合</h3>
                <span>沉淀真实比较篮子，可继续补成员和保存记录。</span>
              </div>
            </div>
            {manualGroupsState.loading ? (
              <PageStateView kind="loading" title="正在加载自定义组合…" surface="empty" compact />
            ) : manualGroupsState.error ? (
              <PageStateView
                kind="error"
                title="自定义组合不可用"
                description={<>{manualGroupsState.error}</>}
                technicalSummary={manualGroupsState.error}
                surface="empty"
                compact
              />
            ) : manualGroupsState.groups.length === 0 ? (
              <PageStateView
                kind="empty"
                title="尚未创建自定义组合"
                description="打开上方自动决策组后，可以把它创建为可长期编辑的手工组合。"
                surface="empty"
                compact
              />
            ) : (
              <div className="asset-table-scroll" role="region" aria-label="自定义资产组合" tabIndex={0}>
                <DataTable
                  className="asset-table asset-decision-manual-groups-table"
                  columns={manualGroupColumns}
                  rows={manualGroupsState.groups}
                  rowKey={(group) => group.manual_group_id}
                  onRowClick={(group) => openManualGroup(group.manual_group_id)}
                />
              </div>
            )}
          </section>
        </div>

        <section className="asset-decision-scenario-card asset-decision-records" aria-label="已保存组合决策">
          <div className="asset-decision-scenario-card__head">
            <div>
              <p className="section-heading__eyebrow">DECISION MEMORY</p>
              <h3>已保存组合决策</h3>
              <span>保存过的判断、跟进状态、执行回读和下一步编排。</span>
            </div>
          </div>
          {recordsState.loading ? (
            <PageStateView kind="loading" title="正在加载决策记录…" surface="empty" compact />
          ) : recordsState.error ? (
            <PageStateView
              kind="error"
              title="决策记录不可用"
              description={<>{recordsState.error}</>}
              technicalSummary={recordsState.error}
              surface="empty"
              compact
            />
          ) : recordsState.records.length === 0 ? (
            <PageStateView
              kind="empty"
              title="尚未保存组合决策"
              description="打开上方决策组后，可以把当前组合判断保存成长期记录。"
              surface="empty"
              compact
            />
          ) : (
            <div className="asset-table-scroll" role="region" aria-label="已保存组合决策" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-records-table"
                columns={recordColumns}
                rows={recordsState.records}
                rowKey={(record) => record.record_id}
                onRowClick={(record) => openRecord(record.record_id)}
              />
            </div>
          )}
        </section>
      </section>

      <section className="page-panel asset-renewal-evidence asset-decision-support-surface animate-in d4">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">RENEWAL EVIDENCE</p>
            <h2 className="section-heading__title">续费证据区</h2>
            <p className="section-heading__description">
              只展示订阅续费事实，不在这里替代组合判断。
            </p>
          </div>
          <span className={`section-count${queueState.renewals.length > 0 ? ' section-count--warn' : ''}`}>
            {queueState.renewalsLoading ? '...' : queueState.renewalsError ? '不可用' : `${queueState.renewals.length} 条`}
          </span>
        </div>
        <AssetDecisionRenewalTable
          loading={queueState.renewalsLoading}
          error={queueState.renewalsError}
          renewals={queueState.renewals}
          vpsByID={vpsByID}
          renderVPSReference={(subscription, vps) => (
            <Link className="name" to={`/vps/${subscription.vps_id}`}>
              {vps?.display_name ?? subscription.vps_id}
            </Link>
          )}
          renderActions={(subscription) => (
            <>
              <Link className="btn-text sm secondary" to={`/asset-decisions?view=renewal&renew_within_days=${renewalWindow}`}>组合判断</Link>
              <Link className="btn-text sm secondary" to={`/vps/${subscription.vps_id}`}>VPS 详情</Link>
            </>
          )}
        />
      </section>

      <section id="single-vps-queue" className="page-panel asset-decision-single-queue asset-decision-support-surface animate-in d5">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">SINGLE VPS QUEUE</p>
            <h2>单台待处理队列</h2>
            <p>保留已有单台续费决策能力；取消/退役仍从 VPS 详情的 lifecycle workbench 进入。</p>
          </div>
          <span className="section-count">
            {queueState.queueLoading ? '...' : `${visibleDecisionQueue.length} / ${totalDecisionQueue}`}
          </span>
        </div>
        <div className="asset-decision-tabs">
          <Tabs items={queueTabs} value={queueView} onChange={setQueueView} variant="pill" />
        </div>
        {queueState.queueLoading ? (
          <PageStateView
            kind="loading"
            title="正在加载单台队列…"
            surface="empty"
            compact
          />
        ) : queueState.queueError ? (
          <PageStateView
            kind="error"
            title="单台队列不可用"
            description={<>{queueState.queueError}</>}
            technicalSummary={queueState.queueError}
            surface="empty"
            compact
          />
        ) : visibleDecisionQueue.length === 0 ? (
          <PageStateView
            kind="empty"
            title="当前视图暂无待处理 VPS"
            description="可回到全部或 VPS 库存；订阅和接入都从 VPS 详情页补齐。"
            action={
              <div className="asset-empty-actions">
                {queueView !== 'all' && (
                  <button className="btn sm secondary" onClick={() => setQueueView('all')}>查看全部</button>
                )}
                <Link className="btn sm ghost" to="/vps">VPS 库存</Link>
                <Link className="btn sm ghost" to="/vps?view=missing_subscription">缺订阅 VPS</Link>
              </div>
            }
            surface="empty"
            compact
          />
        ) : (
          <div className="asset-table-scroll" role="region" aria-label="单台待处理队列" tabIndex={0}>
            <DataTable
              className="asset-table asset-decision-queue-table"
              columns={[
              {
                key: 'vps',
                label: 'VPS',
                width: '236px',
                render: (item) => (
                  <div className="asset-table__identity">
                    <strong>{item.vps.display_name}</strong>
                    <span>{formatOptional(item.vps.provider_name)} · {vpsLocationLabel(item.vps)}</span>
                  </div>
                ),
              },
              {
                key: 'decision',
                label: '决策',
                width: '112px',
                render: (item) => <RenewalBadge value={item.vps.renewal_decision} />,
              },
              {
                key: 'subscription',
                label: '订阅',
                width: '176px',
                render: (item) => {
                  const sub = item.subscription
                  const daysLeft = sub ? daysUntilDate(sub.renew_at) : null
                  return sub ? (
                    <div className="asset-table__stack">
                      <strong>{formatMoney(sub.monthly_price, sub.currency)}/月</strong>
                      <span className={daysLeft != null && daysLeft <= renewalWindow ? 'days-urgent' : 'days-normal'}>
                        {daysLeft != null ? `${daysLeft}天` : formatDate(sub.renew_at)}
                      </span>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="text-link"
                      onClick={() => navigateToVPSSubscription(item.vps.vps_id)}
                    >
                      缺订阅
                    </button>
                  )
                },
              },
              {
                key: 'cost',
                label: '成本信号',
                width: '220px',
                render: (item) => {
                  const sub = item.subscription
                  return sub ? (
                    <div className="asset-context-cell asset-cost-signal">
                      <span className={sub.exchange_rate_stale ? 'badge badge-warn' : 'badge badge-ok'}>
                        <span className="badge-dot" />{sub.exchange_rate_stale ? '汇率过期' : '成本已换算'}
                      </span>
                      <small>
                        {baseMoney(sub.monthly_price_base, sub.base_currency ?? 'CNY')}/月 · {baseMoney(sub.yearly_price_base, sub.base_currency ?? 'CNY')}/年
                      </small>
                      {subscriptionCostAttention(sub) ? (
                        <span className="asset-context-pill asset-context-pill--attention">
                          汇率过期
                        </span>
                      ) : (
                        <span className="asset-context-pill">成本正常</span>
                      )}
                    </div>
                  ) : (
                    <div className="asset-context-cell asset-cost-signal">
                      <span className="asset-context-pill asset-context-pill--attention">缺订阅成本</span>
                      <small>无法参与续费判断</small>
                    </div>
                  )
                },
              },
              {
                key: 'monitoring',
                label: '监控',
                width: '112px',
                render: (item) => (
                  item.vps.active_monitoring_instance_link_count > 0 ? (
                    <span><MonoDigits>{item.vps.active_monitoring_instance_link_count}</MonoDigits> 关联</span>
                  ) : (
                    <span className="text-muted">未关联</span>
                  )
                ),
              },
              {
                key: 'actions',
                label: '操作',
                align: 'right',
                width: '172px',
                render: (item) => (
                  <div className="asset-decision-member-actions">
                    <button className="btn sm primary" onClick={() => selectVPS(item.vps)}>
                      处理
                    </button>
                    {item.vps.renewal_decision === 'cancel' || hasCancellationAttention(item) ? (
                      <Link className="btn sm secondary" to={`/vps/${item.vps.vps_id}?workbench=cancellation`}>
                        取消/退役
                      </Link>
                    ) : null}
                  </div>
                ),
              },
            ]}
              rows={visibleDecisionQueue}
              rowKey={(item) => item.vps.vps_id}
              onRowClick={(item) => navigateToVPS(item.vps)}
            />
          </div>
        )}
      </section>

      <Modal
        open={selectedGroupID != null}
        onClose={closeGroupDetail}
        title={detailState.detail?.title ?? '决策组详情'}
        ariaLabel="资产决策组详情"
        size="xl"
        contentClassName="asset-decision-group-modal"
      >
        {detailState.loading ? (
          <PageStateView kind="loading" title="正在加载决策组详情…" surface="empty" compact />
        ) : detailState.error ? (
          <PageStateView
            kind="error"
            title="决策组详情不可用"
            description={<>{detailState.error}</>}
            technicalSummary={detailState.error}
            surface="empty"
            compact
          />
        ) : detailState.detail ? (
          <div className="asset-decision-detail">
            <div className="asset-decision-detail__summary">
              <div>
                <span>VPS</span>
                <strong><MonoDigits>{detailState.detail.member_count}</MonoDigits></strong>
                <small>{countSummary(detailState.detail.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</small>
              </div>
              <div>
                <span>成本</span>
                <strong>{formatGroupMonthlyCost(detailState.detail)}</strong>
                <small>{formatGroupYearlyCost(detailState.detail)}</small>
              </div>
              <div>
                <span>业务上下文</span>
                <strong>{detailState.detail.service_count} / {detailState.detail.domain_count}</strong>
                <small>服务 / 域名，Target {detailState.detail.running_target_count}/{detailState.detail.target_count}</small>
              </div>
              <div>
                <span>证据质量</span>
                <strong>{EVIDENCE_TIER_LABELS[detailState.detail.evidence_assessment.quality_tier]}</strong>
                <small>可信 {detailState.detail.evidence_assessment.confidence_score}，压力 {detailState.detail.evidence_assessment.pressure_score}，准备 {detailState.detail.evidence_assessment.readiness_score}</small>
              </div>
            </div>
            <section className="asset-decision-progression-branch" aria-label="自动组场景推进建议">
              <div className="asset-decision-progression-branch__head">
                <div>
                  <p className="section-heading__eyebrow">GROUP TO SCENARIO</p>
                  <h3>场景推进建议</h3>
                  <span>{detailState.detail.decision_recommendation?.next_step || '先判断当前自动组是否就是你的真实决策范围。'}</span>
                </div>
                <Badge variant="state" tone={evidenceTierTone(detailState.detail.evidence_assessment.quality_tier)}>
                  {EVIDENCE_TIER_LABELS[detailState.detail.evidence_assessment.quality_tier]}
                </Badge>
              </div>
              <div className="asset-decision-progression-branch__options">
                <article>
                  <span>直接保存记录</span>
                  <strong>组成员已经就是本次判断范围</strong>
                  <small>保存后会进入执行回读和成员跟进，适合证据足够、无需再增删 VPS 的场景。</small>
                </article>
                <article>
                  <span>先创建自定义组合</span>
                  <strong>还需要补成员、目标或人工语境</strong>
                  <small>自定义组合只保存比较篮子和成员意图，不修改 VPS、订阅、监控或 Target。</small>
                </article>
              </div>
            </section>
            <div className="asset-decision-detail__evidence">
              {renderDecisionRecommendation(detailState.detail.decision_recommendation, 'detail')}
              {renderEvidenceAssessment(detailState.detail.evidence_assessment, 'detail')}
              {renderEvidenceChips(detailState.detail.evidence_chips, 8)}
              <div className="asset-decision-member-actions">
                <button
                  className="btn sm primary"
                  type="button"
                  onClick={() => createManualGroupFromAuto(detailState.detail!)}
                  disabled={manualGroupCreating}
                >
                  {manualGroupCreating ? '创建中…' : '创建自定义组合'}
                </button>
                <button className="btn sm secondary" type="button" onClick={() => startRecordSave(detailState.detail!)}>
                  保存为决策记录
                </button>
              </div>
              {detailState.detail.primary_issue_summary && (
                <span className="asset-decision-detail__issue">{detailState.detail.primary_issue_summary}</span>
              )}
            </div>
            {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
            {recordDraft && (
              <form className="asset-decision-record-form" onSubmit={submitRecordSave}>
                <div className="asset-decision-record-form__header">
                  <div>
                    <p className="section-heading__eyebrow">SAVE DECISION</p>
                    <h3>保存组合决策记录</h3>
                    <p>记录当前组级目标、成员判断和这一次聚合出的证据快照。</p>
                  </div>
                  <div className="asset-decision-member-actions">
                    <button className="btn sm primary" type="submit" disabled={recordSaving}>
                      {recordSaving ? '保存中…' : '保存记录'}
                    </button>
                    <button className="btn sm secondary" type="button" onClick={cancelRecordSave} disabled={recordSaving}>
                      取消
                    </button>
                  </div>
                </div>
                {recordSaveError && <div className="inline-alert danger">{recordSaveError}</div>}
                <div className="asset-decision-record-form__grid">
                  <label className="input-field">
                    <span>标题</span>
                    <input
                      className="input"
                      value={recordDraft.title}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, title: event.target.value } : current)}
                    />
                  </label>
                  <label className="input-field">
                    <span>状态</span>
                    <select
                      className="input"
                      value={recordDraft.status}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, status: event.target.value as AssetDecisionRecordStatus } : current)}
                    >
                      {RECORD_STATUS_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </label>
                  <label className="input-field asset-decision-record-form__goal">
                    <span>组合目标</span>
                    <textarea
                      className="input"
                      value={recordDraft.goal}
                      rows={2}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, goal: event.target.value } : current)}
                    />
                  </label>
                </div>
                <div className="asset-decision-record-form__members">
                  {detailState.detail.members.map((member) => {
                    const memberDraft = recordDraft.members[member.vps.vps_id]
                    return (
                      <div className="asset-decision-record-form__member" key={member.vps.vps_id}>
                        <div className="asset-table__identity">
                          <strong>{member.vps.display_name}</strong>
                          <span>{member.vps.vps_id}</span>
                          {renderEvidenceChips(member.evidence_chips, 2)}
                        </div>
                        <label className="input-field">
                          <span>角色</span>
                          <select
                            className="input"
                            value={memberDraft?.decidedRole ?? member.suggested_role}
                            onChange={(event) => updateRecordDraftMember(member.vps.vps_id, { decidedRole: event.target.value as AssetDecisionSuggestedRole })}
                          >
                            {ROLE_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>{option.label}</option>
                            ))}
                          </select>
                        </label>
                        <label className="input-field">
                          <span>动作</span>
                          <select
                            className="input"
                            value={memberDraft?.decidedAction ?? member.suggested_action}
                            onChange={(event) => updateRecordDraftMember(member.vps.vps_id, { decidedAction: event.target.value as AssetDecisionSuggestedAction })}
                          >
                            {ACTION_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>{option.label}</option>
                            ))}
                          </select>
                        </label>
                        <label className="input-field">
                          <span>理由</span>
                          <input
                            className="input"
                            value={memberDraft?.reason ?? ''}
                            onChange={(event) => updateRecordDraftMember(member.vps.vps_id, { reason: event.target.value })}
                          />
                        </label>
                      </div>
                    )
                  })}
                </div>
              </form>
            )}
            <div className="asset-table-scroll" role="region" aria-label="决策组成员对比" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-members-table"
                columns={memberColumns}
                rows={detailState.detail.members}
                rowKey={(member) => member.vps.vps_id}
              />
            </div>
            {selectedVPS && (
              <div className="asset-decision-detail__work-panel">
                <AssetDecisionWorkPanel
                  surface="drawer"
                  selectedVPS={selectedVPS}
                  decisionDraft={decisionDraft}
                  submitting={decisionSubmitting}
                  error={decisionError}
                  notice={null}
                  onDraftChange={setDecisionDraft}
                  onSubmit={handleDecisionSubmit}
                  onCancel={closeDecisionDrawer}
                />
              </div>
            )}
          </div>
        ) : null}
      </Modal>

      <Modal
        open={selectedManualGroupID != null}
        onClose={closeManualGroupDetail}
        title={manualDetailState.detail?.title ?? '自定义组合详情'}
        ariaLabel="自定义资产组合详情"
        size="xl"
        contentClassName="asset-decision-manual-modal"
      >
        {manualDetailState.loading ? (
          <PageStateView kind="loading" title="正在加载自定义组合详情…" surface="empty" compact />
        ) : manualDetailState.error ? (
          <PageStateView
            kind="error"
            title="自定义组合详情不可用"
            description={<>{manualDetailState.error}</>}
            technicalSummary={manualDetailState.error}
            surface="empty"
            compact
          />
        ) : manualDetailState.detail && manualGroupProgress ? (
          <div className="asset-decision-detail asset-decision-manual-detail">
            <div className="asset-decision-detail__summary">
              <div>
                <span>场景</span>
                <strong>{MANUAL_GROUP_SCENARIO_LABELS[manualDetailState.detail.scenario]}</strong>
                <small>{MANUAL_GROUP_STATUS_LABELS[manualDetailState.detail.status]}</small>
              </div>
              <div>
                <span>VPS</span>
                <strong><MonoDigits>{manualDetailState.detail.member_count}</MonoDigits></strong>
                <small>{countSummary(manualDetailState.detail.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</small>
              </div>
              <div>
                <span>成本</span>
                <strong>{formatGroupMonthlyCost(manualDetailState.detail)}</strong>
                <small>{formatGroupYearlyCost(manualDetailState.detail)}</small>
              </div>
              <div>
                <span>证据质量</span>
                <strong>{EVIDENCE_TIER_LABELS[manualDetailState.detail.evidence_assessment.quality_tier]}</strong>
                <small>可信 {manualDetailState.detail.evidence_assessment.confidence_score} · 缺口 {manualDetailState.detail.evidence_assessment.gap_signal_count}</small>
              </div>
            </div>

            <section className="asset-decision-progress-panel" aria-label="自定义组合推进状态">
              <div className="asset-decision-progress-panel__head">
                <div>
                  <p className="section-heading__eyebrow">SCENARIO PROGRESS</p>
                  <h3>组合推进状态</h3>
                  <span>
                    {manualGroupProgress.readyToRecord
                      ? '当前组合已经具备保存为决策记录的基本条件。'
                      : '先补齐目标、成员意图、证据缺口或当前事实，再保存长期记录。'}
                  </span>
                </div>
                <Badge variant="state" tone={manualGroupProgress.readinessTone}>
                  {manualGroupProgress.readinessLabel} {manualGroupProgress.doneCount}/{manualGroupProgress.totalCount}
                </Badge>
              </div>
              <div className="asset-decision-progress-panel__items">
                {manualGroupProgress.items.map((item) => (
                  <article key={item.key} className={`asset-decision-progress-item asset-decision-progress-item--${item.tone}`}>
                    <Badge variant="state" tone={item.tone}>{item.done ? '完成' : '待补'}</Badge>
                    <strong>{item.label}</strong>
                    <span>{item.summary}</span>
                  </article>
                ))}
              </div>
            </section>

            <form className="asset-decision-manual-group-form" onSubmit={submitManualGroupPatch}>
              <div className="asset-decision-record-form__header">
                <div>
                  <p className="section-heading__eyebrow">SCENARIO</p>
                  <h3>组合场景</h3>
                  <p>{manualDetailState.detail.source_type === 'auto_group' ? `来自自动组 ${manualDetailState.detail.source_group_id}` : '手工维护的资产对比篮子'}</p>
                </div>
                <div className="asset-decision-member-actions">
                  <button className="btn sm primary" type="submit" disabled={manualGroupSaving}>
                    {manualGroupSaving ? '保存中…' : '保存组合'}
                  </button>
                  <button
                    className="btn sm secondary"
                    type="button"
                    onClick={() => startManualRecordSave(manualDetailState.detail!)}
                    disabled={manualDetailState.detail.members.length === 0}
                  >
                    保存为决策记录
                  </button>
                  <button
                    className="btn sm secondary"
                    type="button"
                    onClick={() => saveManualGroupAsTemplate(manualDetailState.detail!)}
                    disabled={templateSaving}
                  >
                    {templateSaving ? '保存中…' : '另存为模板'}
                  </button>
                </div>
              </div>
              {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
              <div className="asset-decision-detail__evidence">
                {renderDecisionRecommendation(manualDetailState.detail.decision_recommendation, 'detail')}
                {renderEvidenceAssessment(manualDetailState.detail.evidence_assessment, 'detail')}
                {renderEvidenceChips(manualDetailState.detail.evidence_chips, 8)}
              </div>
              <div className="asset-decision-manual-group-form__grid">
                <label className="input-field">
                  <span>标题</span>
                  <input className="input" name="title" defaultValue={manualDetailState.detail.title} />
                </label>
                <label className="input-field">
                  <span>场景</span>
                  <select className="input" name="scenario" defaultValue={manualDetailState.detail.scenario}>
                    {MANUAL_GROUP_SCENARIO_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <label className="input-field">
                  <span>状态</span>
                  <select className="input" name="status" defaultValue={manualDetailState.detail.status}>
                    <option value="active">进行中</option>
                    <option value="archived">已归档</option>
                  </select>
                </label>
                <label className="input-field asset-decision-record-form__goal">
                  <span>组合目标</span>
                  <textarea className="input" name="goal" rows={2} defaultValue={manualDetailState.detail.goal} />
                </label>
                <label className="input-field asset-decision-record-form__goal">
                  <span>备注</span>
                  <textarea className="input" name="note" rows={2} defaultValue={manualDetailState.detail.note} />
                </label>
              </div>
            </form>

            <form className="asset-decision-manual-add-form" onSubmit={submitManualMemberAdd}>
              <div className="asset-decision-record-form__header">
                <div>
                  <p className="section-heading__eyebrow">MEMBER</p>
                  <h3>加入 VPS</h3>
                  <p>从当前 VPS 资产目录选择成员，组合只保存意图，不修改 VPS 状态。</p>
                </div>
                <button className="btn sm primary" type="submit" disabled={manualGroupSaving || vpsCatalogState.loading || !manualMemberAddDraft.vpsID}>
                  {manualGroupSaving ? '加入中…' : '加入组合'}
                </button>
              </div>
              {vpsCatalogState.error && <div className="inline-alert danger">{vpsCatalogState.error}</div>}
              <div className="asset-decision-manual-add-form__grid">
                <label className="input-field">
                  <span>VPS</span>
                  <select
                    className="input"
                    value={manualMemberAddDraft.vpsID}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, vpsID: event.target.value }))}
                    disabled={vpsCatalogState.loading || Boolean(vpsCatalogState.error)}
                  >
                    <option value="">{vpsCatalogState.loading ? '正在加载 VPS…' : manualMemberCandidateRows.length === 0 ? '暂无可加入 VPS' : '选择 VPS'}</option>
                    {manualMemberCandidateRows.map((vps) => (
                      <option key={vps.vps_id} value={vps.vps_id}>
                        {vps.display_name} · {formatOptional(vps.provider_name)} · {vpsLocationLabel(vps)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="input-field">
                  <span>角色</span>
                  <select
                    className="input"
                    value={manualMemberAddDraft.intendedRole}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, intendedRole: event.target.value as AssetDecisionSuggestedRole }))}
                  >
                    {ROLE_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <label className="input-field">
                  <span>动作</span>
                  <select
                    className="input"
                    value={manualMemberAddDraft.intendedAction}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, intendedAction: event.target.value as AssetDecisionSuggestedAction }))}
                  >
                    {ACTION_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <label className="input-field">
                  <span>排序</span>
                  <input
                    className="input"
                    inputMode="numeric"
                    value={manualMemberAddDraft.sortOrder}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, sortOrder: event.target.value }))}
                    placeholder="自动"
                  />
                </label>
                <label className="input-field">
                  <span>理由</span>
                  <input
                    className="input"
                    value={manualMemberAddDraft.reason}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, reason: event.target.value }))}
                    placeholder="加入组合的原因"
                  />
                </label>
                <label className="input-field">
                  <span>备注</span>
                  <input
                    className="input"
                    value={manualMemberAddDraft.note}
                    onChange={(event) => setManualMemberAddDraft((current) => ({ ...current, note: event.target.value }))}
                    placeholder="可选"
                  />
                </label>
              </div>
            </form>

            {recordDraft && recordDraft.sourceType === 'manual_group' && (
              <form className="asset-decision-record-form" onSubmit={submitRecordSave}>
                <div className="asset-decision-record-form__header">
                  <div>
                    <p className="section-heading__eyebrow">SAVE DECISION</p>
                    <h3>保存自定义组合决策</h3>
                    <p>使用当前自定义组合成员意图生成一次可跟进、可回读的决策记录。</p>
                  </div>
                  <div className="asset-decision-member-actions">
                    <button className="btn sm primary" type="submit" disabled={recordSaving}>
                      {recordSaving ? '保存中…' : '保存记录'}
                    </button>
                    <button className="btn sm secondary" type="button" onClick={cancelRecordSave} disabled={recordSaving}>
                      取消
                    </button>
                  </div>
                </div>
                {recordSaveError && <div className="inline-alert danger">{recordSaveError}</div>}
                <div className="asset-decision-record-form__grid">
                  <label className="input-field">
                    <span>标题</span>
                    <input
                      className="input"
                      value={recordDraft.title}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, title: event.target.value } : current)}
                    />
                  </label>
                  <label className="input-field">
                    <span>状态</span>
                    <select
                      className="input"
                      value={recordDraft.status}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, status: event.target.value as AssetDecisionRecordStatus } : current)}
                    >
                      {RECORD_STATUS_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </label>
                  <label className="input-field asset-decision-record-form__goal">
                    <span>组合目标</span>
                    <textarea
                      className="input"
                      value={recordDraft.goal}
                      rows={2}
                      onChange={(event) => setRecordDraft((current) => current ? { ...current, goal: event.target.value } : current)}
                    />
                  </label>
                </div>
                <div className="asset-decision-record-form__members">
                  {manualDetailState.detail.members.map((member) => {
                    const memberDraft = recordDraft.members[member.vps_id]
                    return (
                      <div className="asset-decision-record-form__member" key={member.vps_id}>
                        <div className="asset-table__identity">
                          <strong>{member.current_fact_found ? member.vps.display_name : member.vps_id}</strong>
                          <span>{member.vps_id}</span>
                          {renderEvidenceChips(member.evidence_chips, 2)}
                        </div>
                        <label className="input-field">
                          <span>角色</span>
                          <select
                            className="input"
                            value={memberDraft?.decidedRole ?? member.intended_role}
                            onChange={(event) => updateRecordDraftMember(member.vps_id, { decidedRole: event.target.value as AssetDecisionSuggestedRole })}
                          >
                            {ROLE_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>{option.label}</option>
                            ))}
                          </select>
                        </label>
                        <label className="input-field">
                          <span>动作</span>
                          <select
                            className="input"
                            value={memberDraft?.decidedAction ?? member.intended_action}
                            onChange={(event) => updateRecordDraftMember(member.vps_id, { decidedAction: event.target.value as AssetDecisionSuggestedAction })}
                          >
                            {ACTION_OPTIONS.map((option) => (
                              <option key={option.value} value={option.value}>{option.label}</option>
                            ))}
                          </select>
                        </label>
                        <label className="input-field">
                          <span>理由</span>
                          <input
                            className="input"
                            value={memberDraft?.reason ?? ''}
                            onChange={(event) => updateRecordDraftMember(member.vps_id, { reason: event.target.value })}
                          />
                        </label>
                      </div>
                    )
                  })}
                </div>
              </form>
            )}

            <div className="asset-table-scroll" role="region" aria-label="自定义组合成员对比" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-manual-members-table"
                columns={manualMemberColumns}
                rows={manualDetailState.detail.members}
                rowKey={(member) => member.vps_id}
              />
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={selectedTemplateID != null}
        onClose={closeTemplateDetail}
        title={templateDetailState.detail?.title ?? '场景模板'}
        ariaLabel="资产决策场景模板详情"
        size="xl"
        contentClassName="asset-decision-template-modal"
      >
        {templateDetailState.loading ? (
          <PageStateView kind="loading" title="正在加载场景模板…" surface="empty" compact />
        ) : templateDetailState.error ? (
          <PageStateView
            kind="error"
            title="场景模板不可用"
            description={<>{templateDetailState.error}</>}
            technicalSummary={templateDetailState.error}
            surface="empty"
            compact
          />
        ) : templateDetailState.detail ? (
          <div className="asset-decision-detail asset-decision-template-detail">
            <div className="asset-decision-detail__summary">
              <div>
                <span>场景</span>
                <strong>{MANUAL_GROUP_SCENARIO_LABELS[templateDetailState.detail.scenario]}</strong>
                <small>{templateDetailState.detail.template_id}</small>
              </div>
              <div>
                <span>类型</span>
                <strong>{templateDetailState.detail.builtin ? '内置模板' : '自定义模板'}</strong>
                <small>{SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}</small>
              </div>
              <div>
                <span>蓝图成员</span>
                <strong><MonoDigits>{templateDetailState.detail.member_count}</MonoDigits></strong>
                <small>{templateDetailState.detail.source_manual_group_id ? `来自 ${templateDetailState.detail.source_manual_group_id}` : '创建时读取当前事实'}</small>
              </div>
              <div>
                <span>更新</span>
                <strong>{templateDetailState.detail.builtin ? '版本内置' : formatDateTime(templateDetailState.detail.updated_at)}</strong>
                <small>{templateDetailState.detail.archived_at ? `归档 ${formatDateTime(templateDetailState.detail.archived_at)}` : '可用于创建组合'}</small>
              </div>
            </div>

            <div className="asset-decision-record-detail__lead">
              <div>
                <p className="section-heading__eyebrow">TEMPLATE GOAL</p>
                <strong>{templateDetailState.detail.goal || '从该场景创建自定义组合后再细化目标'}</strong>
                <span>{templateDetailState.detail.note || '模板只保存场景 blueprint，不保存当前成本、订阅、监控或服务事实。'}</span>
              </div>
              {!templateDetailState.detail.builtin && (
                <div className="asset-decision-template-status-actions">
                  <Badge variant="state" tone={scenarioTemplateStatusTone(templateDetailState.detail.status)}>
                    {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}
                  </Badge>
                  <button
                    className="btn sm secondary"
                    type="button"
                    onClick={() => updateTemplateStatus(templateDetailState.detail!.status === 'active' ? 'archived' : 'active')}
                    disabled={templateSaving}
                  >
                    {templateSaving ? '更新中…' : templateDetailState.detail.status === 'active' ? '归档模板' : '重新启用'}
                  </button>
                </div>
              )}
            </div>

            <form className="asset-decision-template-form" onSubmit={submitTemplateManualGroup}>
              <div className="asset-decision-record-form__header">
                <div>
                  <p className="section-heading__eyebrow">CREATE SCENARIO</p>
                  <h3>从模板创建自定义组合</h3>
                  <p>创建时后端会重新读取当前 VPS、订阅、服务、域名、Target 和监控关联事实。</p>
                </div>
                <button className="btn sm primary" type="submit" disabled={templateSaving || templateDetailState.detail.status !== 'active'}>
                  {templateSaving ? '创建中…' : '创建组合'}
                </button>
              </div>
              {templateError && <div className="inline-alert danger">{templateError}</div>}
              <div className="asset-decision-template-form__grid">
                <label className="input-field">
                  <span>标题</span>
                  <input
                    className="input"
                    value={templateManualDraft.title}
                    onChange={(event) => setTemplateManualDraft((current) => ({ ...current, title: event.target.value }))}
                  />
                </label>
                <label className="input-field">
                  <span>续费窗口</span>
                  <select
                    className="input"
                    value={String(templateManualDraft.renewWithinDays)}
                    onChange={(event) => setTemplateManualDraft((current) => ({ ...current, renewWithinDays: parseRenewalWindow(event.target.value) }))}
                  >
                    {RENEWAL_WINDOWS.map((value) => (
                      <option key={value} value={value}>未来 {value} 天</option>
                    ))}
                  </select>
                </label>
                <label className="input-field asset-decision-record-form__goal">
                  <span>组合目标</span>
                  <textarea
                    className="input"
                    rows={2}
                    value={templateManualDraft.goal}
                    onChange={(event) => setTemplateManualDraft((current) => ({ ...current, goal: event.target.value }))}
                  />
                </label>
                <label className="input-field asset-decision-record-form__goal">
                  <span>备注</span>
                  <textarea
                    className="input"
                    rows={2}
                    value={templateManualDraft.note}
                    onChange={(event) => setTemplateManualDraft((current) => ({ ...current, note: event.target.value }))}
                  />
                </label>
              </div>
            </form>

            <div className="asset-decision-template-members">
              <div className="asset-decision-record-form__header">
                <div>
                  <p className="section-heading__eyebrow">BLUEPRINT</p>
                  <h3>成员蓝图</h3>
                  <p>{templateDetailState.detail.member_count > 0 ? '自定义模板会按蓝图成员重新读取当前事实。' : '内置模板不固定成员，适合从当前筛选场景启动。'}</p>
                </div>
              </div>
              {templateDetailState.detail.members.length === 0 ? (
                <PageStateView
                  kind="empty"
                  title="模板未固定成员"
                  description="创建自定义组合后可在组合详情中加入 VPS 并保存成员意图。"
                  surface="empty"
                  compact
                />
              ) : (
                <div className="asset-decision-template-member-list">
                  {templateDetailState.detail.members.map((member) => (
                    <div className="asset-decision-template-member" key={member.member_id || `${member.vps_id}-${member.sort_order}`}>
                      <div className="asset-table__identity">
                        <strong>{member.vps_id || '待补 VPS'}</strong>
                        <span>{member.reason || '未填写理由'}</span>
                      </div>
                      <span className="asset-decision-chip-row">
                        <Badge variant="state" tone={roleTone(member.intended_role ?? 'observe_candidate')}>
                          {ROLE_LABELS[member.intended_role ?? 'observe_candidate']}
                        </Badge>
                        <Badge variant="state" tone={actionTone(member.intended_action ?? 'review')}>
                          {ACTION_LABELS[member.intended_action ?? 'review']}
                        </Badge>
                      </span>
                      <span>{member.note || '无备注'}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={selectedVPS != null && selectedGroupID == null}
        onClose={closeDecisionDrawer}
        title={selectedVPS ? `处理 ${selectedVPS.display_name}` : '处理续费决策'}
        ariaLabel="续费决策处理"
      >
        <AssetDecisionWorkPanel
          surface="drawer"
          selectedVPS={selectedVPS}
          decisionDraft={decisionDraft}
          submitting={decisionSubmitting}
          error={decisionError}
          notice={null}
          onDraftChange={setDecisionDraft}
          onSubmit={handleDecisionSubmit}
          onCancel={closeDecisionDrawer}
        />
      </Modal>

      <Modal
        open={selectedRecordID != null}
        onClose={closeRecordDetail}
        title={recordDetailState.detail?.title ?? '组合决策记录'}
        ariaLabel="资产组合决策记录详情"
        size="xl"
        contentClassName="asset-decision-record-modal"
      >
        {recordDetailState.loading ? (
          <PageStateView kind="loading" title="正在加载决策记录…" surface="empty" compact />
        ) : recordDetailState.error ? (
          <PageStateView
            kind="error"
            title="决策记录不可用"
            description={<>{recordDetailState.error}</>}
            technicalSummary={recordDetailState.error}
            surface="empty"
            compact
          />
        ) : recordDetailState.detail ? (
          <div className="asset-decision-detail asset-decision-record-detail">
            <div className="asset-decision-detail__summary">
              <div>
                <span>状态</span>
                <strong>{RECORD_STATUS_LABELS[recordDetailState.detail.status]}</strong>
                <small>{recordDetailState.detail.record_id}</small>
              </div>
              <div>
                <span>成员</span>
                <strong><MonoDigits>{recordDetailState.detail.member_count}</MonoDigits></strong>
                <small>{recordDetailState.detail.scope_label || recordDetailState.detail.source_group_id}</small>
              </div>
              <div>
                <span>跟进</span>
                <strong>
                  <MonoDigits>{recordFollowupDoneCount(recordDetailState.detail)}</MonoDigits>/<MonoDigits>{recordDetailState.detail.member_count}</MonoDigits>
                </strong>
                <small>阻塞 {recordDetailState.detail.followup_blocked_count ?? 0} · 待处理 {recordDetailState.detail.followup_todo_count ?? 0}</small>
              </div>
              <div>
                <span>执行回读</span>
                <strong>{recordDetailState.detail.execution_readback?.status ? READBACK_STATUS_LABELS[recordDetailState.detail.execution_readback.status] : '等待回读'}</strong>
                <small>{readbackCountSummary(recordDetailState.detail.execution_readback)}</small>
              </div>
              <div>
                <span>执行计划</span>
                <strong><MonoDigits>{recordDetailState.detail.execution_plan?.actionable_count ?? 0}</MonoDigits> 项</strong>
                <small>{executionPlanCountSummary(recordDetailState.detail.execution_plan)}</small>
              </div>
              <div>
                <span>证据快照</span>
                <strong>{selectedRecordAssessment ? EVIDENCE_TIER_LABELS[selectedRecordAssessment.quality_tier] : '未记录'}</strong>
                <small>{selectedRecordAssessment ? `可信 ${selectedRecordAssessment.confidence_score}，压力 ${selectedRecordAssessment.pressure_score}` : formatDateTime(recordDetailState.detail.updated_at)}</small>
              </div>
            </div>
            <div className="asset-decision-record-detail__lead">
              <div>
                <p className="section-heading__eyebrow">GOAL</p>
                <strong>{recordDetailState.detail.goal || '暂无组合目标'}</strong>
                <span>
                  快照成本 {recordDetailState.detail.evidence_snapshot.monthly_cost_base != null
                    ? formatMoney(Number(recordDetailState.detail.evidence_snapshot.monthly_cost_base), String(recordDetailState.detail.evidence_snapshot.base_currency || 'CNY'))
                    : '暂无 base currency 成本'}
                </span>
                {selectedRecordAssessment && renderEvidenceAssessment(selectedRecordAssessment, 'detail')}
              </div>
              <form className="asset-decision-record-status-form" onSubmit={submitRecordStatus}>
                <label className="input-field">
                  <span>推进状态</span>
                  <select
                    aria-label="推进状态"
                    className="input"
                    value={recordPatchStatus}
                    onChange={(event) => setRecordPatchStatus(event.target.value as AssetDecisionRecordStatus)}
                  >
                    {RECORD_STATUS_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <button className="btn sm primary" type="submit" disabled={recordPatching || recordPatchStatus === recordDetailState.detail.status}>
                  {recordPatching ? '保存中…' : '更新状态'}
                </button>
              </form>
            </div>
            <section className="asset-decision-record-continuity" aria-label="决策记录来源连续性">
              <div>
                <p className="section-heading__eyebrow">SOURCE CONTINUITY</p>
                <h3>来源与当前闭环</h3>
                <strong>{recordSourceLabel(recordDetailState.detail)}</strong>
                <span>{recordSourceDetail(recordDetailState.detail)}</span>
              </div>
              <div className="asset-decision-record-continuity__state">
                <Badge variant="state" tone={readbackStatusTone(recordDetailState.detail.execution_readback?.status)}>
                  {recordDetailState.detail.execution_readback?.status ? READBACK_STATUS_LABELS[recordDetailState.detail.execution_readback.status] : '等待回读'}
                </Badge>
                <Badge variant="count" tone={recordDetailState.detail.execution_plan?.actionable_count > 0 ? 'maintenance' : 'normal'}>
                  可推进 {recordDetailState.detail.execution_plan?.actionable_count ?? 0}
                </Badge>
                <button className="btn sm secondary" type="button" onClick={() => openRecordSource(recordDetailState.detail!)}>
                  复核来源
                </button>
              </div>
            </section>
            {recordPatchError && <div className="inline-alert danger">{recordPatchError}</div>}
            {renderRecordExecutionBoard(recordDetailState.detail)}
            <div className="asset-table-scroll" role="region" aria-label="决策记录成员" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-record-members-table"
                columns={recordMemberColumns}
                rows={recordDetailState.detail.members}
                rowKey={(member) => member.vps_id}
              />
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
