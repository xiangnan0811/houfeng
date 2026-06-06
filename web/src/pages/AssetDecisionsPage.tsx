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
  type AssetDecisionRecommendation,
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

const WORKBENCH_TABS: ReadonlyArray<{ value: WorkbenchView; label: string }> = [
  { value: 'needs_decision', label: '需要决策' },
  { value: 'renewal', label: '续费取舍' },
  { value: 'region', label: '同区比较' },
  { value: 'provider', label: '服务商组合' },
  { value: 'cost', label: '预算压力' },
  { value: 'evidence', label: '资料缺口' },
  { value: 'single_queue', label: '单台队列' },
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

function apiViewForWorkbench(view: WorkbenchView): AssetDecisionView | undefined {
  return view === 'single_queue' ? undefined : view
}

function buildAssetDecisionFilter(searchParams: URLSearchParams, view: WorkbenchView, renewalWindow: RenewalWindow): AssetDecisionGroupListFilter {
  const scenario = parseScenario(searchParams.get('scenario'))
  return {
    view: apiViewForWorkbench(view),
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

function scenarioTemplateStatusTone(status: AssetDecisionScenarioTemplateStatus): BadgeTone {
  return status === 'active' ? 'maintenance' : 'offline'
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
  if (member.decided_action === 'open_cancellation_workbench' || member.decided_action === 'cancel') {
    return `/vps/${member.vps_id}?workbench=cancellation`
  }
  return `/vps/${member.vps_id}`
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

export function AssetDecisionsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeView = parseWorkbenchView(searchParams.get('view'))
  const renewalWindow = parseRenewalWindow(searchParams.get('renew_within_days'))
  const assetDecisionFilter = useMemo(
    () => buildAssetDecisionFilter(searchParams, activeView, renewalWindow),
    [activeView, renewalWindow, searchParams],
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
      activeView === 'single_queue' ? Promise.resolve([]) : listAssetDecisionGroups(filter),
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
  }, [activeView, assetDecisionFilter, refreshToken])

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
                  : totalDecisionQueue,
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

  const templateColumns: DataTableColumn<AssetDecisionScenarioTemplateSummary>[] = [
    {
      key: 'template',
      label: '场景模板',
      width: '300px',
      render: (template) => (
        <div className="asset-table__identity asset-decision-group-cell">
          <strong>{template.title}</strong>
          <span>{MANUAL_GROUP_SCENARIO_LABELS[template.scenario]} · {template.goal || '场景启动器'}</span>
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={scenarioTemplateStatusTone(template.status)}>
              {SCENARIO_TEMPLATE_STATUS_LABELS[template.status]}
            </Badge>
            <Badge variant="info" tone={template.builtin ? 'notice' : 'neutral'}>
              {template.builtin ? '内置模板' : '自定义模板'}
            </Badge>
          </span>
        </div>
      ),
    },
    {
      key: 'blueprint',
      label: '启动方式',
      width: '248px',
      render: (template) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{template.member_count}</MonoDigits> 个成员蓝图</strong>
          <span>{template.source_manual_group_id ? `来自 ${template.source_manual_group_id}` : '从当前事实重新创建组合'}</span>
          <span>{template.note || '不会直接修改资产状态'}</span>
        </div>
      ),
    },
    {
      key: 'updated',
      label: '更新',
      width: '150px',
      render: (template) => (
        <div className="asset-table__stack">
          <strong>{template.builtin ? '版本内置' : formatDateTime(template.updated_at)}</strong>
          <span>{template.archived_at ? `归档 ${formatDateTime(template.archived_at)}` : '可创建组合'}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '128px',
      render: (template) => (
        <button className="btn sm primary" type="button" onClick={() => openTemplate(template.template_id)}>
          使用模板
        </button>
      ),
    },
  ]

  const groupColumns: DataTableColumn<AssetDecisionGroupSummary>[] = [
    {
      key: 'group',
      label: '决策组',
      width: '286px',
      render: (group) => (
        <div className="asset-table__identity asset-decision-group-cell">
          <strong>{group.title}</strong>
          <span>{VIEW_LABELS[group.view]} · {group.scope_label}</span>
          {renderEvidenceChips(group.evidence_chips, 4)}
          {renderDecisionRecommendation(group.decision_recommendation)}
        </div>
      ),
    },
    {
      key: 'portfolio',
      label: '组合',
      width: '220px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{group.member_count}</MonoDigits> 台 VPS</strong>
          <span>{countSummary(group.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</span>
          <span>{countSummary(group.lifecycle_counts, ['active', 'testing', 'to_migrate', 'to_cancel', 'cancelled'], lifecycleLabel)}</span>
        </div>
      ),
    },
    {
      key: 'evidence',
      label: '证据',
      width: '238px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>
            续费 <MonoDigits>{group.renewal_window_count}</MonoDigits> · 未评估 <MonoDigits>{group.unreviewed_count}</MonoDigits>
          </strong>
          <span>
            服务 {group.service_count} / 域名 {group.domain_count} / Target {group.running_target_count}/{group.target_count}
          </span>
          <span>
            监控 {group.monitoring_link_count} · 异常 {group.abnormal_monitoring_count} · 事件 {group.active_incident_count}
          </span>
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
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '112px',
      render: (group) => (
        <button className="btn sm primary" type="button" onClick={() => openGroup(group.group_id)}>
          查看组
        </button>
      ),
    },
  ]

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
                {member.decided_action === 'open_cancellation_workbench' || member.decided_action === 'cancel' ? '取消/退役' : 'VPS 详情'}
              </Link>
            </div>
          </form>
        )
      },
    },
  ]

  function setWorkbenchView(next: WorkbenchView) {
    setPortfolioState((current) => ({
      ...current,
      overviewLoading: true,
      overviewError: null,
      groupsLoading: next !== 'single_queue',
      groupsError: null,
      ...(next === 'single_queue' ? { groups: [] } : {}),
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
      groupsLoading: activeView !== 'single_queue',
      groupsError: null,
    }))
    setQueueState((current) => ({
      ...current,
      renewalsLoading: true,
      renewalsError: null,
    }))
    const nextParams = new URLSearchParams(searchParams)
    nextParams.set('view', activeView)
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
    const detail = recordDetailState.detail
    if (!detail) return
    const draft = recordFollowupDrafts[member.vps_id] ?? {
      status: member.followup_status,
      note: member.followup_note,
    }
    setRecordPatchError(null)
    setRecordFollowupPatching((current) => ({ ...current, [member.vps_id]: true }))
    patchAssetDecisionRecord(detail.record_id, {
      members: [{
        vps_id: member.vps_id,
        followup_status: draft.status,
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
        setDecisionNotice(`成员跟进已更新：${member.display_name || member.vps_id} -> ${FOLLOWUP_STATUS_LABELS[draft.status]}`)
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
          groupsLoading: activeView !== 'single_queue',
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

      <div className="asset-decision-focus animate-in d1">
        <div className="asset-decision-focus__item asset-decision-focus__item--notice">
          <span>DECISION GROUPS</span>
          <strong>{portfolioState.overviewLoading ? '...' : overview?.group_count ?? 0}</strong>
          <small>涉及 {overview?.member_vps_count ?? 0} 台 VPS</small>
        </div>
        <div className="asset-decision-focus__item asset-decision-focus__item--alert">
          <span>RENEWAL</span>
          <strong>{portfolioState.overviewLoading ? '...' : overview?.renewal_group_count ?? 0}</strong>
          <small>{renewalWindow} 天窗口内的组合取舍</small>
        </div>
        <div className="asset-decision-focus__item asset-decision-focus__item--critical">
          <span>PRESSURE</span>
          <strong>{portfolioState.overviewLoading ? '...' : (overview?.cost_group_count ?? 0) + (overview?.evidence_group_count ?? 0)}</strong>
          <small>预算压力 + 资料缺口</small>
        </div>
        <div className="asset-decision-focus__item asset-decision-focus__item--normal">
          <span>EVIDENCE SOURCES</span>
          <strong>{overview ? '5' : '—'}</strong>
          <small>{overview ? sourceAvailabilityLabel(overview.source_availability) : '等待聚合'}</small>
        </div>
      </div>

      <section className="page-panel asset-decision-closed-loop animate-in d2">
        <div className="asset-decision-closed-loop__header">
          <div>
            <p className="section-heading__eyebrow">CLOSED LOOP</p>
            <h2>下一步导览</h2>
            <p>
              按执行回读、自动组、场景组合和模板生成当前最值得处理的入口；这里只读派生，不会自动修改任何资产状态。
            </p>
          </div>
          <div className="asset-decision-closed-loop__context">
            <Badge variant="state" tone={closedLoopMetrics.partialErrorCount > 0 ? 'alert' : 'normal'}>
              {closedLoopMetrics.partialErrorCount > 0 ? '部分证据不可用' : '证据可导览'}
            </Badge>
            {contextFilterChips.length > 0 && (
              <span>上下文筛选已生效：{contextFilterChips.map((chip) => `${chip.label} ${chip.value}`).join(' · ')}</span>
            )}
          </div>
        </div>

        <div className="asset-decision-closed-loop__grid">
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
                description="当前已加载数据没有漂移、阻塞、缺证据或可启动场景；可以继续查看决策组列表。"
                surface="empty"
                compact
              />
            ) : (
              <ol className="asset-decision-next-work__list">
                {nextWorkItems.map((item, index) => (
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
                    <button
                      className="btn sm primary"
                      type="button"
                      onClick={() => openNextWorkItem(item)}
                    >
                      {item.actionLabel}
                    </button>
                  </li>
                ))}
              </ol>
            )}
          </div>

          <div className="asset-decision-closed-loop__metrics" aria-label="资产决策闭环状态">
            <div>
              <span>AUTO GROUPS</span>
              <strong><MonoDigits>{closedLoopMetrics.autoGroupCount}</MonoDigits></strong>
              <small>自动发现组合</small>
            </div>
            <div>
              <span>SCENARIOS</span>
              <strong><MonoDigits>{closedLoopMetrics.manualActiveCount}</MonoDigits></strong>
              <small>进行中自定义组合</small>
            </div>
            <div>
              <span>RECORDS</span>
              <strong><MonoDigits>{closedLoopMetrics.recordActiveCount}</MonoDigits></strong>
              <small>未关闭决策记录</small>
            </div>
            <div className={closedLoopMetrics.readbackDriftCount > 0 ? 'asset-decision-closed-loop__metric--critical' : ''}>
              <span>DRIFT</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackDriftCount}</MonoDigits></strong>
              <small>事实漂移成员</small>
            </div>
            <div className={closedLoopMetrics.readbackBlockedCount > 0 ? 'asset-decision-closed-loop__metric--critical' : ''}>
              <span>BLOCKED</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackBlockedCount}</MonoDigits></strong>
              <small>回读/跟进阻塞</small>
            </div>
            <div className={closedLoopMetrics.readbackNeedsEvidenceCount > 0 ? 'asset-decision-closed-loop__metric--alert' : ''}>
              <span>EVIDENCE GAP</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackNeedsEvidenceCount + closedLoopMetrics.evidenceGapGroupCount}</MonoDigits></strong>
              <small>回读缺口 + 资料组</small>
            </div>
            <div className={closedLoopMetrics.costPressureGroupCount > 0 ? 'asset-decision-closed-loop__metric--alert' : ''}>
              <span>COST</span>
              <strong><MonoDigits>{closedLoopMetrics.costPressureGroupCount}</MonoDigits></strong>
              <small>预算压力组</small>
            </div>
            <div>
              <span>OPEN</span>
              <strong><MonoDigits>{closedLoopMetrics.readbackOpenCount}</MonoDigits></strong>
              <small>仍待回读成员</small>
            </div>
          </div>
        </div>

        {closedLoopPartialErrors.length > 0 && (
          <div className="inline-alert warn" role="status">
            {closedLoopPartialErrors.join('、')}暂不可用，导览只展示已成功加载的事实。
          </div>
        )}
      </section>

      <section className="page-panel asset-decision-command animate-in d3">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">PORTFOLIO WORKBENCH</p>
            <h2>决策组列表</h2>
            <p>
              当前视图：{VIEW_LABELS[activeView]}。自动组只读派生，不会创建持久化决策记录。
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
          <Tabs items={workbenchTabs} value={activeView} onChange={setWorkbenchView} variant="pill" />
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
        ) : activeView === 'single_queue' ? (
          <PageStateView
            kind="empty"
            title="单台队列在页面底部"
            description="这个 tab 只切换到旧的单台续费处理入口；组合判断仍由其他视图承载。"
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
          <div className="asset-table-scroll" role="region" aria-label="决策组列表" tabIndex={0}>
            <DataTable
              className="asset-table asset-decision-groups-table"
              columns={groupColumns}
              rows={portfolioState.groups}
              rowKey={(group) => group.group_id}
              onRowClick={(group) => openGroup(group.group_id)}
            />
          </div>
        )}
      </section>

      <section className="page-panel asset-decision-templates animate-in d3">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">SCENARIO TEMPLATES</p>
            <h2>场景模板</h2>
            <p>从主备、预算、服务商、同区和资料补齐等场景启动自定义组合；模板只创建比较篮子，不直接生成决策记录。</p>
          </div>
          <span className="section-count">
            {templatesState.loading ? '...' : templatesState.error ? '不可用' : `${templatesState.templates.length} 个`}
          </span>
        </div>
        {templatesState.loading ? (
          <PageStateView
            kind="loading"
            title="正在加载场景模板…"
            surface="empty"
            compact
          />
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
          <div className="asset-table-scroll" role="region" aria-label="资产决策场景模板" tabIndex={0}>
            <DataTable
              className="asset-table asset-decision-templates-table"
              columns={templateColumns}
              rows={templatesState.templates}
              rowKey={(template) => template.template_id}
              onRowClick={(template) => openTemplate(template.template_id)}
            />
          </div>
        )}
      </section>

      <section className="page-panel asset-decision-manual-groups animate-in d4">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">SCENARIO WORKBENCH</p>
            <h2>自定义组合</h2>
            <p>把系统发现的自动组沉淀成真实问题篮子，也可以围绕预算、主备、迁移退役持续调整成员意图。</p>
          </div>
          <span className="section-count">
            {manualGroupsState.loading ? '...' : manualGroupsState.error ? '不可用' : `${manualGroupsState.groups.length} 个`}
          </span>
        </div>
        {manualGroupsState.loading ? (
          <PageStateView
            kind="loading"
            title="正在加载自定义组合…"
            surface="empty"
            compact
          />
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

      <section className="page-panel asset-decision-records animate-in d5">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">DECISION MEMORY</p>
            <h2>已保存组合决策</h2>
            <p>保存组级目标、成员角色/动作/理由和当时证据快照，用于后续回看与推进。</p>
          </div>
          <span className="section-count">
            {recordsState.loading ? '...' : recordsState.error ? '不可用' : `${recordsState.records.length} 条`}
          </span>
        </div>
        {recordsState.loading ? (
          <PageStateView
            kind="loading"
            title="正在加载决策记录…"
            surface="empty"
            compact
          />
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

      <section className="page-panel asset-renewal-evidence animate-in d5">
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

      <section className="page-panel asset-decision-single-queue animate-in d5">
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
        ) : manualDetailState.detail ? (
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
            {recordPatchError && <div className="inline-alert danger">{recordPatchError}</div>}
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
