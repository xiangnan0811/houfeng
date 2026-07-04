import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import {
  AssetDecisionWorkPanel,
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import {
  type BadgeTone,
} from '../components/atoms'
import { PortfolioWorkbench } from './asset-decisions/components/PortfolioWorkbench'
import { SecondaryWorkbenches } from './asset-decisions/components/SecondaryWorkbenches'
import {
  Badge,
  DataTable,
  type DataTableColumn,
  Modal,
  Tabs,
} from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
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
import { formatDateTime, formatMoney, formatOptional } from '../lib/format'
import {
  type AssetDecisionEvidenceChip,
  type AssetDecisionEvidenceAssessment,
  type AssetDecisionEvidenceDecisionBias,
  type AssetDecisionEvidenceQualityTier,
  type AssetDecisionEvidenceSnapshot,
  type AssetDecisionComparisonInsight,
  type AssetDecisionComparisonLane,
  type AssetDecisionMemberComparisonInsight,
  type AssetDecisionGroupDetail,
  type AssetDecisionGroupMember,
  type AssetDecisionGroupSummary,
  type AssetDecisionManualGroupDetail,
  type AssetDecisionManualGroupMember,
  type AssetDecisionManualGroupScenario,
  type AssetDecisionManualGroupStatus,
  type AssetDecisionManualGroupSummary,
  type AssetDecisionRecommendation,
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
  lifecycleLabel,
  renewalLabel,
  usageLabel,
  vpsLocationLabel,
} from './assetPageUtils'
import type {
  WorkbenchView,
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
  GroupDetailPanel,
  ManualDetailPanel,
  RecordDetailPanel,
  TemplateDetailPanel,
  ContextFilterKey,
  OpenStateKey,
  ContextFilterChip,
  AssetDecisionSecondaryNavItem,
  SecondaryWorkbench,
} from './asset-decisions/types'
import {
  RENEWAL_WINDOWS,
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
  MANUAL_GROUP_STATUS_LABELS,
  SCENARIO_TEMPLATE_STATUS_LABELS,
  MANUAL_GROUP_SCENARIO_LABELS,
  RECORD_STATUS_LABELS,
  FOLLOWUP_STATUS_LABELS,
  READBACK_STATUS_LABELS,
  ASSET_DECISION_DETAIL_PREVIEW_LIMIT,
  COMPARISON_LANE_LABELS,
  ROLE_OPTIONS,
  ACTION_OPTIONS,
  MANUAL_GROUP_SCENARIO_OPTIONS,
  RECORD_STATUS_OPTIONS,
  FOLLOWUP_STATUS_OPTIONS,
  CONTEXT_FILTER_KEYS,
  OPEN_STATE_KEYS,
} from './asset-decisions/constants'
import {
  renderCompactRiskChips,
  renderComparisonSignals,
  renderReadbackBadge,
  renderExecutionPlanBadge,
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
  parseWorkbenchView,
  trimParam,
  buildAssetDecisionFilter,
  parseComparisonInsight,
} from './asset-decisions/utils'
import {
  chipTone,
  roleTone,
  actionTone,
  followupStatusTone,
  readbackStatusTone,
  comparisonLaneTone,
  scenarioTemplateStatusTone,
  renewalQueueLabel,
  recordSourceLabel,
  recordSourceDetail,
  compactDecisionText,
  sourceAvailabilityLabel,
  memberContextLabel,
  compactMemberReadbackSummary,
  compactMemberPlanSummary,
  actionLabelForMember,
  compactVPSOptionLabel,
  manualCoverMeta,
  recordCoverSummary,
  recordCoverMeta,
  compactGroupJudgement,
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

type ComparisonMatrixMember = {
  key: string
  displayName: string
  href?: string
  meta: string
  product: string
  facts: string
  statusFacts: string
  sourceLabel: string
  role: AssetDecisionSuggestedRole
  action: AssetDecisionSuggestedAction
  intendedRole?: AssetDecisionSuggestedRole
  intendedAction?: AssetDecisionSuggestedAction
  comparison?: AssetDecisionMemberComparisonInsight | null
  evidenceChips?: AssetDecisionEvidenceChip[]
  currentFactFound?: boolean
}

type DetailCommandOptions = {
  ariaLabel: string
  title: string
  summary: string
  assessment?: AssetDecisionEvidenceAssessment | null
  recommendation?: AssetDecisionRecommendation | null
  insight?: AssetDecisionComparisonInsight | null
  chips?: AssetDecisionEvidenceChip[]
  badge?: ReactNode
  actions?: ReactNode
  footer?: ReactNode
}

type MemberDecisionCardsOptions = {
  title: string
  ariaLabel: string
  summary?: string
  showIntent?: boolean
  action?: (member: ComparisonMatrixMember) => ReactNode
}

function portfolioViewForWorkbench(view: WorkbenchView): MainWorkbenchView {
  return view === 'single_queue' ? 'needs_decision' : view
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




function memberComparisonTitle(member: ComparisonMatrixMember) {
  return <strong>{member.displayName}</strong>
}

function renderDetailCommand(options: DetailCommandOptions) {
  return (
    <section className="asset-decision-detail-command" aria-label={options.ariaLabel}>
      <div className="asset-decision-detail-command__main">
        <div>
          <strong>{options.summary}</strong>
        </div>
        <div className="asset-decision-detail-command__meta">
          {options.badge ?? null}
          {renderCompactRiskChips(options.chips, options.assessment)}
        </div>
      </div>
      {(options.actions || options.footer) && (
        <div className="asset-decision-detail-command__footer">
          {options.footer}
          {options.actions && <div className="asset-decision-detail-command__actions">{options.actions}</div>}
        </div>
      )}
    </section>
  )
}


function renderMemberDecisionRows(members: ComparisonMatrixMember[], options: MemberDecisionCardsOptions) {
  const sortedMembers = [...members].sort((left, right) => {
    const leftRank = left.comparison?.rank ?? Number.POSITIVE_INFINITY
    const rightRank = right.comparison?.rank ?? Number.POSITIVE_INFINITY
    if (leftRank !== rightRank) return leftRank - rightRank
    return left.displayName.localeCompare(right.displayName)
  })
  const memberPreview = previewItems(sortedMembers)
  return (
    <section className="asset-decision-member-decisions">
      {renderCompactTaskHeader(options.title, `成员 ${sortedMembers.length}`)}
      {sortedMembers.length > 0 ? (
        <div className="asset-decision-member-rows" aria-label={options.ariaLabel}>
          {memberPreview.visible.map((member) => {
            const comparison = member.comparison
            const lane: AssetDecisionComparisonLane = comparison?.lane ?? 'review'
            const laneTone = comparisonLaneTone(lane)
            const intentMismatch = options.showIntent
              && comparison
              && (
                (member.intendedAction === 'cancel' || member.intendedAction === 'open_cancellation_workbench') !== (lane === 'retire')
                || (member.intendedAction === 'complete_evidence') !== (lane === 'evidence')
                || (member.intendedRole === 'primary_candidate') !== (lane === 'primary')
              )
            return (
              <article key={member.key} className={`asset-decision-member-row asset-decision-member-row--${laneTone}`}>
                <div className="asset-decision-member-row__identity">
                  <span>{COMPARISON_LANE_LABELS[lane] ?? lane}</span>
                  {memberComparisonTitle(member)}
                </div>
                <div className="asset-decision-member-row__decision">
                  <div>
                    <span className="asset-decision-chip-row">
                    <Badge variant="state" tone={roleTone(member.intendedRole ?? member.role)}>
                      {ROLE_LABELS[member.intendedRole ?? member.role]}
                    </Badge>
                    <Badge variant="state" tone={actionTone(member.intendedAction ?? member.action)}>
                      {ACTION_LABELS[member.intendedAction ?? member.action]}
                    </Badge>
                    {options.showIntent && (
                      <Badge variant="state" tone={intentMismatch ? 'alert' : 'normal'}>
                        {intentMismatch ? '需复核意图' : '意图匹配'}
                      </Badge>
                    )}
                    </span>
                    <strong>{compactDecisionText(comparison?.summary, '等待复核')}</strong>
                  </div>
                </div>
                {((comparison?.risks?.length ?? 0) + (comparison?.gaps?.length ?? 0)) > 0 && (
                  <div className="asset-decision-member-row__signals">
                    {renderComparisonSignals([...(comparison?.risks ?? []), ...(comparison?.gaps ?? [])], 2)}
                  </div>
                )}
                {options.action && <div className="asset-decision-member-row__actions">{options.action(member)}</div>}
              </article>
            )
          })}
          {memberPreview.hiddenCount > 0 && (
            <div className="asset-decision-preview-more" role="note">
              另有 {memberPreview.hiddenCount} 台在底稿中查看
            </div>
          )}
        </div>
      ) : (
        <div className="asset-decision-member-decisions__empty">
          <strong>暂无可取舍成员</strong>
          <span>当前组合没有可展示的成员判断。</span>
        </div>
      )}
    </section>
  )
}


function renderDetailPanel(title: string, children: ReactNode, includeHiddenHeading = false) {
  return (
    <section className="asset-decision-detail-panel" aria-label={title}>
      {includeHiddenHeading ? <h4 className="visually-hidden">{title}</h4> : null}
      {children}
    </section>
  )
}

function renderCompactTaskHeader(title: string, meta?: ReactNode) {
  return (
    <div className="asset-decision-task-header" role="heading" aria-level={4} aria-label={title}>
      <strong>{title}</strong>
      {meta ? <span aria-hidden="true">{meta}</span> : null}
    </div>
  )
}

function groupMemberComparisonMatrixMember(member: AssetDecisionGroupMember): ComparisonMatrixMember {
  const monthlyCost = member.primary_subscription
    ? `${formatMoney(member.primary_subscription.monthly_price, member.primary_subscription.currency)}/月`
    : member.source_availability.subscriptions ? '缺订阅成本' : '订阅证据不可用'
  return {
    key: member.vps.vps_id,
    displayName: member.vps.display_name || member.vps.vps_id,
    href: `/vps/${member.vps.vps_id}`,
    meta: `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}`,
    product: `${monthlyCost} · ${member.vps.product_name || member.vps.vps_id}`,
    facts: memberContextLabel(member),
    statusFacts: `${lifecycleLabel(member.vps.lifecycle_status)} · ${usageLabel(member.vps.usage_status)} · ${renewalLabel(member.vps.renewal_decision)}`,
    sourceLabel: sourceAvailabilityLabel(member.source_availability),
    role: member.suggested_role,
    action: member.suggested_action,
    comparison: member.comparison_insight,
    evidenceChips: member.evidence_chips,
    currentFactFound: true,
  }
}

function manualMemberComparisonMatrixMember(member: AssetDecisionManualGroupMember): ComparisonMatrixMember {
  if (!member.current_fact_found) {
    return {
      key: member.vps_id,
      displayName: member.vps_id,
      meta: '当前资产事实缺失',
      product: '无法回读成本和产品',
      facts: '无法回读承载和监控',
      statusFacts: '当前 facts 未返回',
      sourceLabel: '当前事实缺失',
      role: member.suggested_role,
      action: member.suggested_action,
      intendedRole: member.intended_role,
      intendedAction: member.intended_action,
      comparison: member.comparison_insight,
      evidenceChips: member.evidence_chips,
      currentFactFound: false,
    }
  }
  return {
    ...groupMemberComparisonMatrixMember(member),
    key: member.vps_id,
    href: `/vps/${member.vps_id}`,
    intendedRole: member.intended_role,
    intendedAction: member.intended_action,
    currentFactFound: member.current_fact_found,
  }
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

function sortedExecutionMembers(members: AssetDecisionRecordMember[]): AssetDecisionRecordMember[] {
  const EXECUTION_PLAN_LANE_ORDER = ['retirement', 'migration', 'evidence', 'primary', 'standby', 'observe', 'review']
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

function previewItems<T>(items: T[]): { visible: T[]; hiddenCount: number } {
  return {
    visible: items.slice(0, ASSET_DECISION_DETAIL_PREVIEW_LIMIT),
    hiddenCount: Math.max(0, items.length - ASSET_DECISION_DETAIL_PREVIEW_LIMIT),
  }
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
  const manualGroupListFilterKey = useMemo(
    () => assetDecisionFilterKey(assetDecisionFilter),
    [assetDecisionFilter],
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
  const urlSecondaryWorkbench: SecondaryWorkbench | null = (() => {
    if (activeView === 'single_queue') return 'single_queue'
    if (portfolioView === 'renewal') return 'renewals'
    if (trimParam(searchParams.get('record_id'))) return 'records'
    if (trimParam(searchParams.get('manual_group_id')) || trimParam(searchParams.get('template_id'))) return 'scenarios'
    return null
  })()
  const [selectedSecondaryWorkbench, setSelectedSecondaryWorkbench] = useState<SecondaryWorkbench | null>(null)
  const secondaryWorkbench = urlSecondaryWorkbench ?? selectedSecondaryWorkbench
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
          <strong><Link className="name" to={`/vps/${member.vps.vps_id}`}>{member.vps.display_name}</Link></strong>
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
                <Link className="name" to={`/vps/${member.vps_id}`}>{displayName}</Link>
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
      width: '240px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={`/vps/${member.vps_id}`}>{member.display_name || member.vps_id}</Link></strong>
          <span>{member.vps_id}</span>
        </div>
      ),
    },
    {
      key: 'role',
      label: '角色',
      width: '160px',
      render: (member) => (
        <Badge variant="state" tone={roleTone(member.decided_role)}>
          {ROLE_LABELS[member.decided_role]}
        </Badge>
      ),
    },
    {
      key: 'action',
      label: '动作',
      width: '180px',
      render: (member) => (
        <Badge variant="state" tone={actionTone(member.decided_action)}>
          {ACTION_LABELS[member.decided_action]}
        </Badge>
      ),
    },
    {
      key: 'followup',
      label: '跟进状态',
      width: '160px',
      render: (member) => (
        <Badge variant="state" tone={followupStatusTone(member.followup_status)}>
          {FOLLOWUP_STATUS_LABELS[member.followup_status]}
        </Badge>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '112px',
      render: (member) => (
        <button
          className="btn sm primary"
          type="button"
          onClick={() => setRecordFollowupEditingMemberID(member.vps_id)}
        >
          编辑
        </button>
      ),
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
            另有 {memberPreview.hiddenCount} 台在成员底稿中查看
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
    setRecordDraft(buildRecordDraft(detail, renewalWindow))
    setRecordDraftEditingMemberID(null)
    setRecordSaveError(null)
    setGroupDetailPanel('save')
  }

  function startManualRecordSave(detail: AssetDecisionManualGroupDetail) {
    setRecordDraft(buildManualRecordDraft(detail))
    setRecordDraftEditingMemberID(null)
    setRecordSaveError(null)
    setManualDetailPanel('save')
  }

  function cancelRecordSave() {
    setRecordDraft(null)
    setRecordDraftEditingMemberID(null)
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
    const executionPreview = previewItems(detail.members)
    const visibleExecutionMembers = sortedExecutionMembers(executionPreview.visible)

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
  }

  function navigateToVPS(vps: VPSAssetRecord) {
    navigate(`/vps/${vps.vps_id}`)
  }

  function navigateToVPSSubscription(vpsID: string) {
    navigate(`/vps/${vpsID}?workbench=subscription`)
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

      {isSingleQueueDeepLink && (
        <div className="inline-alert info asset-decision-deeplink-notice" role="status">
          旧链接已承接到单台辅助队列；组合判断仍以决策组扫描为主。
          <button className="alert-action" type="button" onClick={() => setSelectedSecondaryWorkbench('single_queue')}>查看单台队列</button>
        </div>
      )}

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
            surface="empty"
            compact
          />
        ) : detailState.detail ? (
          <div className="asset-decision-detail">
            <Tabs
              items={[
                { value: 'overview', label: '概览' },
                { value: 'members', label: '成员', count: detailState.detail.members.length },
                { value: 'save', label: '保存' },
              ]}
              value={groupDetailPanel}
              onChange={(value) => {
                if (value === 'save' && recordDraft?.sourceType !== 'auto_group') {
                  startRecordSave(detailState.detail!)
                  return
                }
                setGroupDetailPanel(value as GroupDetailPanel)
              }}
            />
            {groupDetailPanel === 'overview' && renderDetailCommand({
              ariaLabel: '决策组当前判断',
              title: '',
              summary: compactGroupJudgement(detailState.detail),
              assessment: detailState.detail.evidence_assessment,
              recommendation: detailState.detail.decision_recommendation,
              insight: detailState.detail.comparison_insight,
              chips: detailState.detail.evidence_chips,
              actions: (
                <button
                  className="btn md primary"
                  type="button"
                  onClick={() => createManualGroupFromAuto(detailState.detail!)}
                  disabled={manualGroupCreating}
                >
                  {manualGroupCreating ? '创建中…' : '创建组合'}
                </button>
              ),
            })}
            {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
            {groupDetailPanel === 'members' && renderDetailPanel('成员明细',
              renderMemberDecisionRows(
                detailState.detail.members.map(groupMemberComparisonMatrixMember),
                {
                  title: '成员取舍',
                  ariaLabel: '成员取舍列表',
                  action: (member) => {
                    const sourceMember = detailState.detail?.members.find((item) => item.vps.vps_id === member.key)
                    if (!sourceMember) return null
                    if (sourceMember.suggested_action === 'open_cancellation_workbench') {
                      return (
                        <Link className="btn sm primary" to={`/vps/${sourceMember.vps.vps_id}?workbench=cancellation`}>
                          取消/退役
                        </Link>
                      )
                    }
                    return (
                      <button className="btn sm primary" type="button" onClick={() => selectVPS(sourceMember.vps)}>
                        处理
                      </button>
                    )
                  },
                },
              ),
            )}
            {groupDetailPanel === 'save' && recordDraft && renderDetailPanel('保存记录',
              <form className="asset-decision-record-form" onSubmit={submitRecordSave}>
                <div className="asset-decision-record-form__header">
                  {renderCompactTaskHeader('保存组合决策记录', `成员 ${detailState.detail.members.length}`)}
                  <div className="asset-decision-member-actions">
                    <button className="btn md primary" type="submit" disabled={recordSaving}>
                      {recordSaving ? '保存中…' : '保存记录'}
                    </button>
                    <button className="btn md secondary" type="button" onClick={cancelRecordSave} disabled={recordSaving}>
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
                {renderRecordDraftMemberRows(detailState.detail.members.map((member) => ({
                  vpsID: member.vps.vps_id,
                  displayName: member.vps.display_name || member.vps.vps_id,
                  fallbackRole: member.suggested_role,
                  fallbackAction: member.suggested_action,
                  meta: `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}`,
                })))}
              </form>,
            )}
            {groupDetailPanel === 'raw' && renderDetailPanel('数据底稿',
              <div className="asset-table-scroll" role="region" aria-label="决策组成员对比" tabIndex={0}>
                <DataTable
                  className="asset-table asset-decision-members-table"
                  columns={memberColumns}
                  rows={detailState.detail.members}
                  rowKey={(member) => member.vps.vps_id}
                />
              </div>,
              true,
            )}
            {groupDetailPanel === 'vps' && selectedVPS && (
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
            surface="empty"
            compact
          />
        ) : manualDetailState.detail && manualGroupProgress ? (
          <div className="asset-decision-detail asset-decision-manual-detail">
            <Tabs
              items={[
                { value: 'overview', label: '概览' },
                { value: 'members', label: '成员', count: manualDetailState.detail.members.length },
                { value: 'edit', label: '编辑' },
                { value: 'save', label: '保存' },
              ]}
              value={manualDetailPanel}
              onChange={(value) => {
                if (value === 'save' && recordDraft?.sourceType !== 'manual_group') {
                  startManualRecordSave(manualDetailState.detail!)
                  return
                }
                selectManualDetailPanel(value as ManualDetailPanel)
              }}
            />
            {manualDetailPanel === 'overview' && renderDetailCommand({
              ariaLabel: '自定义组合当前判断',
              title: '当前判断',
              summary: compactDecisionText(
                manualDetailState.detail.comparison_insight?.summary
                  || manualDetailState.detail.decision_recommendation?.summary
                  || manualDetailState.detail.goal,
                '继续整理组合',
              ),
              footer: <span className="asset-decision-detail-command__context">{manualCoverMeta(manualDetailState.detail, manualGroupProgress)}</span>,
              assessment: manualDetailState.detail.evidence_assessment,
              recommendation: manualDetailState.detail.decision_recommendation,
              insight: manualDetailState.detail.comparison_insight,
              chips: manualDetailState.detail.evidence_chips,
              badge: (
                <Badge variant="state" tone={manualGroupProgress.readinessTone}>
                  {manualGroupProgress.readinessLabel} {manualGroupProgress.doneCount}/{manualGroupProgress.totalCount}
                </Badge>
              ),
            })}
            {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
            {manualDetailPanel === 'members' && renderDetailPanel('成员维护',
              renderMemberDecisionRows(
                manualDetailState.detail.members.map(manualMemberComparisonMatrixMember),
                {
                  title: '成员取舍',
                  ariaLabel: '自定义组合成员取舍',
                  showIntent: true,
                  action: (member) => {
                    const sourceMember = manualDetailState.detail?.members.find((item) => item.vps_id === member.key)
                    if (!sourceMember) return null
                    return (
                      <button
                        className="btn sm secondary"
                        type="button"
                        disabled={Boolean(manualMemberSaving[sourceMember.vps_id])}
                        onClick={() => requestManualMemberRemoval(sourceMember)}
                      >
                        移除
                      </button>
                    )
                  },
                },
              ),
            )}

            {manualDetailPanel === 'edit' && renderDetailPanel('编辑组合',
              <form id="asset-decision-manual-group-form" className="asset-decision-manual-group-form" onSubmit={submitManualGroupPatch}>
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('组合场景', MANUAL_GROUP_STATUS_LABELS[manualDetailState.detail.status])}
              </div>
              {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
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
              <div className="asset-operation-actions">
                <button className="btn md primary" type="submit" disabled={manualGroupSaving}>
                  {manualGroupSaving ? '保存中…' : '保存组合'}
                </button>
                <button
                  className="btn md secondary"
                  type="button"
                  onClick={() => saveManualGroupAsTemplate(manualDetailState.detail!)}
                  disabled={templateSaving}
                >
                  {templateSaving ? '保存中…' : '另存为模板'}
                </button>
              </div>
            </form>,
            )}

            {manualDetailPanel === 'add' && renderDetailPanel('添加成员',
              <form className="asset-decision-manual-add-form" onSubmit={submitManualMemberAdd}>
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('加入 VPS', `候选 ${manualMemberCandidateRows.length}`)}
                <div className="asset-decision-member-actions">
                  <button
                    className="btn sm secondary"
                    type="button"
                    aria-expanded={manualMemberAddAdvanced}
                    onClick={() => setManualMemberAddAdvancedVisible(!manualMemberAddAdvanced)}
                  >
                    高级选项
                  </button>
                  <button className="btn md primary" type="submit" disabled={manualGroupSaving || vpsCatalogState.loading || !manualMemberAddDraft.vpsID}>
                    {manualGroupSaving ? '加入中…' : '加入组合'}
                  </button>
                </div>
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
                        {compactVPSOptionLabel(vps)}
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
                {manualMemberAddAdvanced && (
                  <>
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
                  </>
                )}
              </div>
            </form>,
            )}

            {manualDetailPanel === 'save' && recordDraft && recordDraft.sourceType === 'manual_group' && renderDetailPanel('保存记录',
              <form className="asset-decision-record-form" onSubmit={submitRecordSave}>
                <div className="asset-decision-record-form__header">
                  {renderCompactTaskHeader('保存自定义组合决策', `成员 ${manualDetailState.detail.members.length}`)}
                  <div className="asset-decision-member-actions">
                    <button className="btn md primary" type="submit" disabled={recordSaving}>
                      {recordSaving ? '保存中…' : '保存记录'}
                    </button>
                    <button className="btn md secondary" type="button" onClick={cancelRecordSave} disabled={recordSaving}>
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
                {renderRecordDraftMemberRows(manualDetailState.detail.members.map((member) => ({
                  vpsID: member.vps_id,
                  displayName: member.current_fact_found ? member.vps.display_name || member.vps_id : member.vps_id,
                  fallbackRole: member.intended_role,
                  fallbackAction: member.intended_action,
                  meta: member.current_fact_found ? `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}` : '当前事实缺失',
                })))}
              </form>,
            )}

            {manualDetailPanel === 'members' && pendingManualMemberRemoval ? (
              <section className="asset-lifecycle-confirm" role="alertdialog" aria-label="确认移除组合成员">
                <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
                <h3>确认移除组合成员</h3>
                <div className="asset-lifecycle-confirm__flow">
                  <span>
                    当前：{pendingManualMemberRemoval.current_fact_found ? pendingManualMemberRemoval.vps.display_name : pendingManualMemberRemoval.vps_id} 在这个自定义组合中。
                  </span>
                  <span>操作后：该 VPS 会从当前自定义组合中移除。</span>
                </div>
                <div className="asset-lifecycle-confirm__callouts">
                  <p>会删除这个组合里的成员意图、理由、备注和排序。</p>
                  <p>不会修改 VPS、订阅、监控实例、Target 或已保存决策记录。</p>
                </div>
                <div className="asset-operation-actions">
                  <button
                    className="btn sm secondary"
                    type="button"
                    disabled={Boolean(manualMemberSaving[pendingManualMemberRemoval.vps_id])}
                    onClick={cancelManualMemberRemoval}
                  >
                    取消
                  </button>
                  <button
                    className="btn sm danger"
                    type="button"
                    disabled={Boolean(manualMemberSaving[pendingManualMemberRemoval.vps_id])}
                    onClick={() => deleteManualMember(pendingManualMemberRemoval)}
                  >
                    {manualMemberSaving[pendingManualMemberRemoval.vps_id] ? '移除中…' : '确认移除'}
                  </button>
                </div>
              </section>
            ) : null}

            {manualDetailPanel === 'raw' && renderDetailPanel('成员数据',
              <div className="asset-table-scroll" role="region" aria-label="自定义组合成员对比" tabIndex={0}>
                <DataTable
                  className="asset-table asset-decision-manual-members-table"
                  columns={manualMemberColumns}
                  rows={manualDetailState.detail.members}
                  rowKey={(member) => member.vps_id}
                />
              </div>,
              true,
            )}
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
            surface="empty"
            compact
          />
        ) : templateDetailState.detail ? (
          <div className="asset-decision-detail asset-decision-template-detail">
            <Tabs
              items={[
                { value: 'overview', label: '概览' },
                { value: 'members', label: '成员', count: templateDetailState.detail.member_count },
                ...(!templateDetailState.detail.builtin ? [{ value: 'status' as const, label: '状态' }] : []),
              ]}
              value={templateDetailPanel}
              onChange={(value) => setTemplateDetailPanel(value as TemplateDetailPanel)}
            />
            {templateDetailPanel === 'overview' && renderDetailCommand({
              ariaLabel: '场景模板当前判断',
              title: '当前模板',
              summary: compactDecisionText(templateDetailState.detail.goal, '创建组合后细化'),
              footer: <span className="asset-decision-detail-command__context">
                {MANUAL_GROUP_SCENARIO_LABELS[templateDetailState.detail.scenario]} · {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]} · 蓝图 {templateDetailState.detail.member_count}
              </span>,
              badge: (
                <Badge variant="state" tone={scenarioTemplateStatusTone(templateDetailState.detail.status)}>
                  {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}
                </Badge>
              ),
            })}
            {templateError && <div className="inline-alert danger">{templateError}</div>}

            {templateDetailPanel === 'status' && !templateDetailState.detail.builtin && renderDetailPanel('状态维护',
              <>
                <div className="asset-decision-template-status-actions">
                  <Badge variant="state" tone={scenarioTemplateStatusTone(templateDetailState.detail.status)}>
                    {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}
                  </Badge>
                  <button
                    className="btn sm secondary"
                    type="button"
                    onClick={() => requestTemplateStatusUpdate(templateDetailState.detail!.status === 'active' ? 'archived' : 'active')}
                    disabled={templateSaving}
                  >
                    {templateSaving ? '更新中…' : templateDetailState.detail.status === 'active' ? '归档模板' : '重新启用'}
                  </button>
                </div>
                {pendingTemplateStatus ? (
              <section className="asset-lifecycle-confirm" role="alertdialog" aria-label={pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用模板'}>
                <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
                <h3>{pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用模板'}</h3>
                <div className="asset-lifecycle-confirm__flow">
                  <span>当前：模板状态为 {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}。</span>
                  <span>操作后：模板状态变为 {SCENARIO_TEMPLATE_STATUS_LABELS[pendingTemplateStatus]}。</span>
                </div>
                <div className="asset-lifecycle-confirm__callouts">
                  <p>{pendingTemplateStatus === 'archived' ? '归档后不能直接从该模板创建新组合。' : '重新启用后可继续从该模板创建自定义组合。'}</p>
                  <p>不会修改已经创建的自定义组合、决策记录或任何 VPS 资产事实。</p>
                </div>
                <div className="asset-operation-actions">
                  <button className="btn sm secondary" type="button" disabled={templateSaving} onClick={cancelTemplateStatusUpdate}>
                    取消
                  </button>
                  <button className="btn sm primary" type="button" disabled={templateSaving} onClick={() => updateTemplateStatus(pendingTemplateStatus)}>
                    {templateSaving ? '更新中…' : pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用'}
                  </button>
                </div>
              </section>
                ) : null}
              </>,
            )}

            {templateDetailPanel === 'create' && renderDetailPanel('创建组合',
              <form className="asset-decision-template-form" onSubmit={submitTemplateManualGroup}>
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('从模板创建自定义组合', SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status])}
                <button className="btn md primary" type="submit" disabled={templateSaving || templateDetailState.detail.status !== 'active'}>
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
            </form>,
            )}

            {templateDetailPanel === 'members' && renderDetailPanel('成员蓝图',
              <div className="asset-decision-template-members">
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('成员蓝图', `成员 ${templateDetailState.detail.members.length}`)}
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
                  {templateDetailState.detail.members.map((member, index) => (
                    <div className="asset-decision-template-member" key={member.member_id || `${member.vps_id}-${member.sort_order}`}>
                      <div className="asset-table__identity">
                        <strong>{member.vps_id ? `成员 ${index + 1}` : '待补成员'}</strong>
                        <span>{ROLE_LABELS[member.intended_role ?? 'observe_candidate']} · {ACTION_LABELS[member.intended_action ?? 'review']}</span>
                      </div>
                      <span className="asset-decision-chip-row">
                        <Badge variant="state" tone={roleTone(member.intended_role ?? 'observe_candidate')}>
                          {ROLE_LABELS[member.intended_role ?? 'observe_candidate']}
                        </Badge>
                        <Badge variant="state" tone={actionTone(member.intended_action ?? 'review')}>
                          {ACTION_LABELS[member.intended_action ?? 'review']}
                        </Badge>
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>,
            )}
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
            surface="empty"
            compact
          />
        ) : recordDetailState.detail ? (
          <div className="asset-decision-detail asset-decision-record-detail">
            <Tabs
              items={[
                { value: 'overview', label: '概览' },
                { value: 'execution', label: '执行', count: recordDetailState.detail.execution_plan?.actionable_count ?? 0 },
                { value: 'members', label: '成员', count: recordDetailState.detail.members.length },
                { value: 'source', label: '来源' },
              ]}
              value={recordDetailPanel}
              onChange={(value) => setRecordDetailPanel(value as RecordDetailPanel)}
            />
            {recordDetailPanel === 'overview' && renderDetailCommand({
              ariaLabel: '保存记录当前判断',
              title: '当前记录',
              summary: recordCoverSummary(recordDetailState.detail),
              footer: <span className="asset-decision-detail-command__context">{recordCoverMeta(recordDetailState.detail)}</span>,
              assessment: selectedRecordAssessment,
              insight: parseComparisonInsight(recordDetailState.detail.evidence_snapshot),
              chips: [],
            })}
            {recordPatchError && <div className="inline-alert danger">{recordPatchError}</div>}
            {recordDetailPanel === 'execution' && renderDetailPanel('执行跟进',
              <>
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
                {renderRecordExecutionBoard(recordDetailState.detail)}
              </>,
            )}
            {recordDetailPanel === 'source' && renderDetailPanel('来源复核',
              <section className="asset-decision-record-continuity" aria-label="决策记录来源连续性">
                <div>
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
              </section>,
            )}
            {recordDetailPanel === 'members' && renderDetailPanel('成员跟进',
              renderRecordMemberFollowupRows(recordDetailState.detail),
            )}
            {recordDetailPanel === 'raw' && renderDetailPanel('成员底稿',
              <div className="asset-table-scroll" role="region" aria-label="决策记录成员" tabIndex={0}>
                <DataTable
                  className="asset-table asset-decision-record-members-table"
                  columns={recordMemberColumns}
                  rows={recordDetailState.detail.members}
                  rowKey={(member) => member.vps_id}
                />
              </div>,
              true,
            )}
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
