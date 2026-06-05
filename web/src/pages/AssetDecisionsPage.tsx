import { type FormEvent, useEffect, useMemo, useState } from 'react'
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
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
  getAssetDecisionGroup,
  getAssetDecisionOverview,
  listAssetDecisionGroups,
  listSubscriptions,
  listVPSAssets,
  updateVPSAsset,
} from '../lib/api'
import { formatDate, formatDateTime, formatMoney, formatOptional } from '../lib/format'
import {
  type AssetDecisionEvidenceChip,
  type AssetDecisionGroupDetail,
  type AssetDecisionGroupMember,
  type AssetDecisionGroupSummary,
  type AssetDecisionOverview,
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

function formatGroupMonthlyCost(group: AssetDecisionGroupSummary): string {
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

function formatGroupYearlyCost(group: AssetDecisionGroupSummary): string {
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

export function AssetDecisionsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeView = parseWorkbenchView(searchParams.get('view'))
  const renewalWindow = parseRenewalWindow(searchParams.get('renew_within_days'))
  const apiView = apiViewForWorkbench(activeView)
  const [queueView, setQueueView] = useState<DecisionQueueView>('all')
  const [portfolioState, setPortfolioState] = useState<PortfolioState>(INITIAL_PORTFOLIO_STATE)
  const [detailState, setDetailState] = useState<DetailState>(INITIAL_DETAIL_STATE)
  const [queueState, setQueueState] = useState<QueueState>(INITIAL_QUEUE_STATE)
  const [selectedGroupID, setSelectedGroupID] = useState<string | null>(null)
  const [selectedVPS, setSelectedVPS] = useState<VPSAssetRecord | null>(null)
  const [decisionDraft, setDecisionDraft] = useState<AssetDecisionDraft>(INITIAL_DECISION_DRAFT)
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)
  const [refreshToken, setRefreshToken] = useState(0)

  useEffect(() => {
    let cancelled = false
    const filter = {
      view: apiView,
      renew_within_days: renewalWindow,
    }

    Promise.all([
      getAssetDecisionOverview(filter),
      activeView === 'single_queue' ? Promise.resolve([]) : listAssetDecisionGroups(filter),
    ])
      .then(([overview, groups]) => {
        if (cancelled) return
        setPortfolioState({
          overviewLoading: false,
          overviewError: null,
          overview,
          groupsLoading: false,
          groupsError: null,
          groups,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = describeError(error, '加载资产组合决策失败')
        setPortfolioState((current) => ({
          ...current,
          overviewLoading: false,
          overviewError: message,
          groupsLoading: false,
          groupsError: message,
          groups: [],
        }))
      })

    return () => { cancelled = true }
  }, [activeView, apiView, renewalWindow, refreshToken])

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

  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(queueState.subscriptions),
    [queueState.subscriptions],
  )
  const vpsByID = useMemo(() => {
    const rows = new Map<string, VPSAssetRecord>()
    for (const vps of [...queueState.unreviewed, ...queueState.migrate, ...queueState.cancel]) {
      rows.set(vps.vps_id, vps)
    }
    for (const member of detailState.detail?.members ?? []) rows.set(member.vps.vps_id, member.vps)
    return rows
  }, [detailState.detail?.members, queueState.cancel, queueState.migrate, queueState.unreviewed])
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
      width: '276px',
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
      width: '168px',
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

  function openGroup(groupID: string) {
    setSelectedVPS(null)
    setDecisionError(null)
    setDetailState({ loading: true, error: null, detail: null })
    setSelectedGroupID(groupID)
  }

  function closeGroupDetail() {
    setSelectedGroupID(null)
    setDetailState(INITIAL_DETAIL_STATE)
    setSelectedVPS(null)
    setDecisionDraft(INITIAL_DECISION_DRAFT)
    setDecisionError(null)
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

      <section className="page-panel asset-decision-command animate-in d2">
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

      <section className="page-panel asset-renewal-evidence animate-in d3">
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

      <section className="page-panel asset-decision-single-queue animate-in d4">
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
                <span>监控</span>
                <strong>{detailState.detail.monitoring_link_count}</strong>
                <small>异常 {detailState.detail.abnormal_monitoring_count}，事件 {detailState.detail.active_incident_count}</small>
              </div>
            </div>
            <div className="asset-decision-detail__evidence">
              {renderEvidenceChips(detailState.detail.evidence_chips, 8)}
              {detailState.detail.primary_issue_summary && (
                <span className="asset-decision-detail__issue">{detailState.detail.primary_issue_summary}</span>
              )}
            </div>
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
    </div>
  )
}
