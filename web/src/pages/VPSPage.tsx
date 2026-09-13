import { Fragment, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import {
  Button,
  Modal,
  SegmentedControl,
  isInteractiveRowTarget,
} from '../components/atoms'
import { FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { PageState as PageStateView } from '../components/PageState'
import { VPSCreateModal } from '../components/VPSCreateModal'
import { ApiError, listProviders, listSubscriptions, listVPSAssets } from '../lib/api'
import { formatDate, formatMoney, formatOptional } from '../lib/format'
import { periodLabel, renewalModeFromLegacy, renewalModeLabel } from '../lib/assetOptions'
import { overviewImportanceLabel } from '../lib/vpsOverviewPresentation'
import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type ProviderRecord,
  type SubscriptionRecord,
  type VPSAssetRecord,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../lib/types'
import {
  IPQualityBadge,
  LifecycleBadge,
  RenewalBadge,
  UsageBadge,
} from './assetPageBadges'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  groupSubscriptionsByVPS,
  hasMissingVPSFacts,
  isSubscriptionInRenewalWindow,
  lifecycleLabel,
  renewalLabel,
  selectPrimarySubscription,
  subscriptionStatusLabel,
  usageLabel,
  vpsLocationLabel,
  type AssetQualityIssue,
} from './assetPageUtils'
import './VPSPage.css'

const WORKSPACE_STORAGE_KEY = 'houfeng.vps.workspace'

type VPSWorkspace = 'workbench' | 'ledger'

type VPSQuickView =
  | 'all'
  | 'renewal'
  | 'unreviewed'
  | 'unlinked'
  | 'cancellation_attention'
  | 'missing_subscription'
  | 'missing_facts'

type SubscriptionEvidenceStatus = 'loading' | 'ready' | 'error'

type InventoryRow = {
  vps: VPSAssetRecord
  subscription: SubscriptionRecord | null
  subscriptionEvidence: SubscriptionEvidenceStatus
  qualityIssues: AssetQualityIssue[]
  renewalDue: boolean
}

type PageState = {
  inventoryLoading: boolean
  inventoryError: string | null
  providersLoading: boolean
  providersError: string | null
  subscriptionsLoading: boolean
  subscriptionsError: string | null
  vps: VPSAssetRecord[]
  providers: ProviderRecord[]
  subscriptions: SubscriptionRecord[]
}

type FilterState = {
  view: VPSQuickView
  provider_id: string | null
  lifecycle_status: VPSLifecycleStatus | null
  usage_status: VPSUsageStatus | null
  renewal_decision: VPSRenewalDecision | null
}

const INITIAL_PAGE_STATE: PageState = {
  inventoryLoading: true,
  inventoryError: null,
  providersLoading: true,
  providersError: null,
  subscriptionsLoading: true,
  subscriptionsError: null,
  vps: [],
  providers: [],
  subscriptions: [],
}

const INITIAL_FILTER_STATE: FilterState = {
  view: 'all',
  provider_id: null,
  lifecycle_status: null,
  usage_status: null,
  renewal_decision: null,
}

function vpsDetailHref(vpsID: string, view: VPSQuickView): string {
  const pathname = `/vps/${encodeURIComponent(vpsID)}`
  return view === 'unlinked' ? `${pathname}?workbench=monitoring` : pathname
}

const LIFECYCLE_OPTIONS = Object.entries(VPS_LIFECYCLE_STATUS_LABELS)
  .filter(([value]) => value !== 'cancelled' && value !== 'archived')
  .map(([value, label]) => ({
    value,
    label,
  }))
const USAGE_OPTIONS = Object.entries(VPS_USAGE_STATUS_LABELS).map(([value, label]) => ({
  value,
  label,
}))
const RENEWAL_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS).map(([value, label]) => ({
  value,
  label,
}))
const QUICK_VIEW_VALUES: VPSQuickView[] = [
  'all',
  'renewal',
  'unreviewed',
  'unlinked',
  'cancellation_attention',
  'missing_subscription',
  'missing_facts',
]
const WORKSPACE_ITEMS = [
  { value: 'workbench' as const, label: '表格视图' },
  { value: 'ledger' as const, label: '目录视图' },
]

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseFilters(searchParams: URLSearchParams): FilterState {
  const lifecycle = searchParams.get('lifecycle_status') as VPSLifecycleStatus | null
  const usage = searchParams.get('usage_status') as VPSUsageStatus | null
  const renewal = searchParams.get('renewal_decision') as VPSRenewalDecision | null
  const view = searchParams.get('view') as VPSQuickView | null
  return {
    view: view && QUICK_VIEW_VALUES.includes(view) ? view : 'all',
    provider_id: searchParams.get('provider_id') || null,
    lifecycle_status: lifecycle && lifecycle in VPS_LIFECYCLE_STATUS_LABELS ? lifecycle : null,
    usage_status: usage && usage in VPS_USAGE_STATUS_LABELS ? usage : null,
    renewal_decision: renewal && renewal in VPS_RENEWAL_DECISION_LABELS ? renewal : null,
  }
}

function writeFilters(params: URLSearchParams, filters: FilterState) {
  if (filters.view !== 'all') params.set('view', filters.view)
  else params.delete('view')
  if (filters.provider_id) params.set('provider_id', filters.provider_id)
  else params.delete('provider_id')
  if (filters.lifecycle_status) params.set('lifecycle_status', filters.lifecycle_status)
  else params.delete('lifecycle_status')
  if (filters.usage_status) params.set('usage_status', filters.usage_status)
  else params.delete('usage_status')
  if (filters.renewal_decision) params.set('renewal_decision', filters.renewal_decision)
  else params.delete('renewal_decision')
}

function parseWorkspace(value: string | null): VPSWorkspace | null {
  return value === 'workbench' || value === 'ledger' ? value : null
}

function readStoredWorkspace(): VPSWorkspace {
  try {
    return parseWorkspace(window.localStorage.getItem(WORKSPACE_STORAGE_KEY)) ?? 'ledger'
  } catch {
    return 'ledger'
  }
}

function writeStoredWorkspace(value: VPSWorkspace) {
  try {
    window.localStorage.setItem(WORKSPACE_STORAGE_KEY, value)
  } catch {
    // Preference is optional; switching still works from URL/state.
  }
}

function assetDecisionHrefForFilters(filters: FilterState): string {
  const params = new URLSearchParams()
  params.set('view', filters.provider_id ? 'provider' : 'needs_decision')
  params.set('renew_within_days', '30')
  if (filters.provider_id) params.set('provider_id', filters.provider_id)
  if (filters.view === 'renewal') {
    params.set('view', 'renewal')
  }
  if (filters.lifecycle_status === 'to_cancel' || filters.renewal_decision === 'cancel' || filters.view === 'cancellation_attention') {
    params.set('scenario', 'migration_retirement')
  } else if (filters.view === 'missing_subscription' || filters.view === 'unlinked' || filters.view === 'missing_facts') {
    params.set('view', 'evidence')
    params.set('scenario', 'evidence_cleanup')
  }
  return `/asset-decisions?${params.toString()}`
}

function hasActiveFilters(filters: FilterState): boolean {
  return Boolean(
    filters.view !== 'all' ||
      filters.provider_id ||
      filters.lifecycle_status ||
      filters.usage_status ||
      filters.renewal_decision,
  )
}

function providerFilterOptions(providers: ProviderRecord[]): FilterSelectOption[] {
  return providers.map((provider) => ({
    value: provider.provider_id,
    label: provider.name,
  }))
}

function buildInventoryRows(
  vpsRows: VPSAssetRecord[],
  subscriptionsByVPS: Map<string, SubscriptionRecord[]>,
  subscriptionEvidence: SubscriptionEvidenceStatus,
): InventoryRow[] {
  return vpsRows
    .filter((vps) => vps.lifecycle_status !== 'cancelled' && vps.lifecycle_status !== 'archived')
    .map((vps) => {
    const subscription =
      subscriptionEvidence === 'ready'
        ? selectPrimarySubscription(subscriptionsByVPS, vps.vps_id)
        : null
    return {
      vps,
      subscription,
      subscriptionEvidence,
      qualityIssues: buildVPSQualityIssues(vps, subscription, {
        includeMissingSubscription: subscriptionEvidence === 'ready',
      }),
      renewalDue:
        subscriptionEvidence === 'ready' &&
        isSubscriptionInRenewalWindow(subscription, 30),
    }
  })
}

function applyInventoryFilters(rows: InventoryRow[], filters: FilterState): InventoryRow[] {
  return rows
    .filter((row) => {
      if (filters.provider_id && row.vps.provider_id !== filters.provider_id) return false
      if (filters.lifecycle_status && row.vps.lifecycle_status !== filters.lifecycle_status) return false
      if (filters.usage_status && row.vps.usage_status !== filters.usage_status) return false
      if (filters.renewal_decision && row.vps.renewal_decision !== filters.renewal_decision) return false
      return matchesQuickView(row, filters.view)
    })
    .sort((left, right) => {
      const leftRank = inventoryRank(left)
      const rightRank = inventoryRank(right)
      if (leftRank !== rightRank) return rightRank - leftRank
      const leftDays = daysUntilDate(left.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      const rightDays = daysUntilDate(right.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      if (leftDays !== rightDays) return leftDays - rightDays
      return left.vps.display_name.localeCompare(right.vps.display_name)
    })
}

function matchesQuickView(row: InventoryRow, view: VPSQuickView): boolean {
  if (view === 'all') return true
  if (view === 'renewal') return row.renewalDue
  if (view === 'unreviewed') return row.vps.renewal_decision === 'unreviewed'
  if (view === 'unlinked') return row.vps.active_monitoring_instance_link_count <= 0
  if (view === 'cancellation_attention') return hasCancellationAttention(row)
  if (view === 'missing_subscription') return row.subscriptionEvidence === 'ready' && !row.subscription
  if (view === 'missing_facts') return hasMissingVPSFacts(row.vps)
  return true
}

function cancellationAttentionReason(row: InventoryRow): string | null {
  const vpsToCancel = row.vps.lifecycle_status === 'to_cancel'
  const vpsCancelDecision = row.vps.renewal_decision === 'cancel' || row.vps.renewal_decision === 'auto_renew_cancelled'
  const runningLinkedAssetCount = (row.vps.running_monitoring_instance_count ?? 0) + (row.vps.running_target_count ?? 0)
  const subscriptionInactive = row.subscriptionEvidence === 'ready' &&
    row.subscription != null &&
    row.subscription.status !== 'active'
  const subscriptionActive = row.subscriptionEvidence === 'ready' &&
    row.subscription?.status === 'active'

  if (subscriptionInactive && !vpsToCancel) return '订阅非活跃，VPS 尚未取消'
  if (vpsToCancel && subscriptionActive) return 'VPS 待取消，订阅仍生效中'
  if (vpsToCancel && runningLinkedAssetCount > 0) return `VPS 待取消，仍有 ${runningLinkedAssetCount} 个监控实例/入口探测运行`
  if (vpsCancelDecision && !vpsToCancel) return '已决定不续费，生命周期未同步'
  return null
}

function hasCancellationAttention(row: InventoryRow): boolean {
  return cancellationAttentionReason(row) !== null
}

function inventoryRank(row: InventoryRow): number {
  let rank = 0
  if (hasCancellationAttention(row)) rank += 160
  if (row.vps.renewal_decision === 'unreviewed') rank += 120
  if (row.renewalDue) rank += 100
  if (row.subscriptionEvidence === 'ready' && !row.subscription) rank += 80
  if (row.vps.active_monitoring_instance_link_count <= 0) rank += 60
  if (hasMissingVPSFacts(row.vps)) rank += 30
  return rank
}

function quickViewLabel(value: VPSQuickView): string {
  if (value === 'all') return '全部'
  if (value === 'renewal') return '30天续费'
  if (value === 'unreviewed') return '未评估'
  if (value === 'unlinked') return '未关联'
  if (value === 'cancellation_attention') return '取消待处理'
  if (value === 'missing_subscription') return '缺订阅'
  if (value === 'missing_facts') return '缺基础信息'
  return '全部'
}

function renderRenewalDate(row: InventoryRow) {
  if (row.subscriptionEvidence === 'loading') return '订阅加载中'
  if (row.subscriptionEvidence === 'error') return '订阅加载失败'
  if (!row.subscription) return '无订阅'
  if (!row.subscription.renew_at) return '无续费日'
  const days = daysUntilDate(row.subscription.renew_at)
  if (days != null && days <= 30) {
    return <span className="vps-tone-warn">{formatDate(row.subscription.renew_at)}</span>
  }
  return formatDate(row.subscription.renew_at)
}

function providerName(providerID: string | null, providers: ProviderRecord[]): string {
  if (!providerID) return ''
  return providers.find((provider) => provider.provider_id === providerID)?.name ?? providerID
}

function matchesSearch(row: InventoryRow, query: string): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  const vps = row.vps
  return [
    vps.display_name,
    vps.ipv4,
    vps.ipv6,
    vps.ssh_host,
    vps.provider_name,
    vps.country,
    vps.region,
    vps.city,
    vps.datacenter,
  ].some((part) => part.toLowerCase().includes(needle))
}

function compactLine(parts: Array<string | null | undefined>): string {
  return parts.map((part) => (part ?? '').trim()).filter(Boolean).join(' · ')
}

function vpsSpecLabel(vps: VPSAssetRecord): string {
  const parts = [vps.product_name, vps.os_name, vps.virtualization]
    .map((part) => part.trim())
    .filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : '规格未填写'
}

function vpsPlaceLabel(vps: VPSAssetRecord): string {
  const location = vpsLocationLabel(vps)
  const datacenter = vps.datacenter.trim()
  if (!datacenter) return location
  if (location === '位置缺失') return datacenter
  return `${location} · ${datacenter}`
}

function vpsPrimaryAddress(vps: VPSAssetRecord): string {
  return vps.ipv4.trim() || vps.ssh_host.trim() || vps.ipv6.trim() || '—'
}


function subscriptionFact(row: InventoryRow, subscriptionsError: string | null): string {
  if (row.subscriptionEvidence === 'loading') return '加载中…'
  if (row.subscriptionEvidence === 'error') {
    return subscriptionsError
      ? '加载失败：' + subscriptionsError
      : '加载失败'
  }
  if (!row.subscription) return '无订阅'
  const subscription = row.subscription
  return compactLine([
    formatMoney(subscription.price, subscription.currency),
    periodLabel(subscription.billing_period_unit, subscription.billing_period_length, subscription.billing_months),
    subscription.renew_at ? `续费日 ${formatDate(subscription.renew_at)}` : '无续费日',
    subscriptionStatusLabel(subscription.status),
    renewalModeLabel(renewalModeFromLegacy(subscription)),
  ])
}

function inspectorEmptyMessage(selectedID: string | null, visibleCount: number): string {
  if (visibleCount === 0) return '暂无匹配的 VPS'
  if (selectedID) return '选中项不在当前筛选结果中'
  return '选择 VPS'
}

function VPSInspector({
  row,
  detailHref,
  currentInventoryHref,
  subscriptionsError,
  emptyMessage,
}: {
  row: InventoryRow | null
  detailHref: string | null
  currentInventoryHref: string
  subscriptionsError: string | null
  emptyMessage: string
}) {
  return (
    <section className="vps-inspector" aria-label="VPS 检查器">
      <div className="vps-inspector__inner">
        {!row || !detailHref ? (
          <p className="vps-inspector__empty">{emptyMessage}</p>
        ) : (
          <>
            <h2 className="vps-inspector__title">{row.vps.display_name}</h2>
            <dl className="vps-inspector__dl">
              <dt>位置</dt>
              <dd>{vpsPlaceLabel(row.vps)}</dd>
              <dt>服务商</dt>
              <dd>{formatOptional(row.vps.provider_name)}</dd>
              <dt>IPv4</dt>
              <dd className="vps-mono">{row.vps.ipv4.trim() || '—'}</dd>
              {row.vps.ipv6.trim() ? (
                <>
                  <dt>IPv6</dt>
                  <dd className="vps-mono">{row.vps.ipv6.trim()}</dd>
                </>
              ) : null}
              {row.vps.ssh_host.trim() ? (
                <>
                  <dt>SSH</dt>
                  <dd className="vps-mono">
                    {compactLine([
                      row.vps.ssh_user.trim() ? `${row.vps.ssh_user.trim()}@${row.vps.ssh_host.trim()}` : row.vps.ssh_host.trim(),
                      String(row.vps.ssh_port || ''),
                    ])}
                  </dd>
                </>
              ) : null}
              <dt>规格</dt>
              <dd>{vpsSpecLabel(row.vps)}</dd>
              <dt>生命周期</dt>
              <dd><LifecycleBadge value={row.vps.lifecycle_status} /></dd>
              <dt>用途</dt>
              <dd><UsageBadge value={row.vps.usage_status} /></dd>
              <dt>续费</dt>
              <dd><RenewalBadge value={row.vps.renewal_decision} /> · {row.subscriptionEvidence === 'ready' ? (row.subscription?.renew_at ? formatDate(row.subscription.renew_at) : '无续费日') : '续费日未知'}</dd>
              <dt>订阅</dt>
              <dd>{subscriptionFact(row, subscriptionsError)}</dd>
              <dt>关联</dt>
              <dd>
                {`监控实例 ${row.vps.active_monitoring_instance_link_count}`}
                {typeof row.vps.running_monitoring_instance_count === 'number' ? ` · 运行中监控 ${row.vps.running_monitoring_instance_count}` : ''}
                {typeof row.vps.running_target_count === 'number' ? ` · 运行中探测 ${row.vps.running_target_count}` : ''}
              </dd>
              <dt>IP 质量</dt>
              <dd><IPQualityBadge {...(row.vps.ip_quality_summary === undefined ? {} : { summary: row.vps.ip_quality_summary })} /></dd>
              {row.vps.importance.trim() ? (
                <>
                  <dt>重要性</dt>
                  <dd>{overviewImportanceLabel(row.vps.importance)}</dd>
                </>
              ) : null}
              {row.vps.labels.length > 0 ? (
                <>
                  <dt>标签</dt>
                  <dd>{row.vps.labels.join('、')}</dd>
                </>
              ) : null}
            </dl>
            {row.vps.note.trim() ? (
              <>
                <hr className="vps-inspector__rule" />
                <p className="vps-inspector__kicker">备注</p>
                <p className="vps-inspector__note">{row.vps.note}</p>
              </>
            ) : null}
            <Link
              className="vps-inspector__action"
              to={detailHref}
              state={{ vpsInventoryHref: currentInventoryHref }}
            >
              打开 VPS 详情
            </Link>
          </>
        )}
      </div>
    </section>
  )
}

function accordionPanelId(vpsID: string) {
  return `vps-accordion-${vpsID}`
}

function VPSQuickFacts({ row }: { row: InventoryRow }) {
  const attention = cancellationAttentionReason(row)
  return (
    <div className="vps-accordion__facts">
      <div className="vps-accordion__fact">
        <div className="vps-accordion__fact-label">资产身份</div>
        <div className="vps-accordion__fact-value">
          <span className="vps-mono">{vpsPrimaryAddress(row.vps)}</span>
          {row.vps.provider_name.trim() ? ` · ${row.vps.provider_name}` : ''}
          {` · ${vpsPlaceLabel(row.vps)}`}
        </div>
      </div>
      <div className="vps-accordion__fact">
        <div className="vps-accordion__fact-label">经营与续费</div>
        <div className="vps-accordion__fact-value">
          <LifecycleBadge value={row.vps.lifecycle_status} />
          {' · '}
          <UsageBadge value={row.vps.usage_status} />
          {' · '}
          <RenewalBadge value={row.vps.renewal_decision} />
          {' · '}
          {renderRenewalDate(row)}
          {attention ? <span className="vps-tone-warn"> · {attention}</span> : null}
        </div>
      </div>
      <div className="vps-accordion__fact">
        <div className="vps-accordion__fact-label">监控关联</div>
        <div className="vps-accordion__fact-value">
          {row.vps.active_monitoring_instance_link_count > 0
            ? `已关联 ${row.vps.active_monitoring_instance_link_count} 个监控实例`
            : '未关联监控实例'}
          {typeof row.vps.running_monitoring_instance_count === 'number' && row.vps.running_monitoring_instance_count > 0
            ? ` · 运行中 ${row.vps.running_monitoring_instance_count}`
            : ''}
        </div>
      </div>
      <div className="vps-accordion__fact">
        <div className="vps-accordion__fact-label">观察证据</div>
        <div className="vps-accordion__fact-value">
          <IPQualityBadge
            {...(row.vps.ip_quality_summary === undefined
              ? {}
              : { summary: row.vps.ip_quality_summary })}
          />
        </div>
      </div>
    </div>
  )
}

function VPSWorkbenchAccordionRow({
  row,
  detailHref,
  currentInventoryHref,
}: {
  row: InventoryRow
  detailHref: string
  currentInventoryHref: string
}) {
  return (
    <tr className="vps-workbench__accordion">
      <td colSpan={4}>
        <div
          className="vps-accordion"
          id={accordionPanelId(row.vps.vps_id)}
          role="region"
          aria-label="VPS 快速查看"
        >
          <Link
            className="vps-accordion__action"
            to={detailHref}
            state={{ vpsInventoryHref: currentInventoryHref }}
          >
            打开 VPS 详情
          </Link>
          <VPSQuickFacts row={row} />
        </div>
      </td>
    </tr>
  )
}



export function VPSPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const searchQuery = searchParams.get('q') ?? ''
  const selectedID = searchParams.get('selected')
  const urlWorkspace = parseWorkspace(searchParams.get('workspace'))
  const workspace = urlWorkspace ?? readStoredWorkspace()
  const [draftFilters, setDraftFilters] = useState<FilterState>(filters)
  const [filterDrawerOpen, setFilterDrawerOpen] = useState(false)
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [accordionOpen, setAccordionOpen] = useState(false)
  const [inventoryReloadKey, setInventoryReloadKey] = useState(0)
  const [providersReloadKey, setProvidersReloadKey] = useState(0)
  const [subscriptionsReloadKey, setSubscriptionsReloadKey] = useState(0)
  const currentInventoryHref = `/vps${location.search}`
  useEffect(() => {
    if (urlWorkspace) writeStoredWorkspace(urlWorkspace)
  }, [urlWorkspace])

  useEffect(() => {
    let cancelled = false
    listVPSAssets()
      .then((vps) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          inventoryLoading: false,
          inventoryError: null,
          vps,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          inventoryLoading: false,
          inventoryError: describeError(error, '加载 VPS 资产失败'),
          vps: [],
        }))
      })
    return () => {
      cancelled = true
    }
  }, [inventoryReloadKey])

  useEffect(() => {
    let cancelled = false
    listProviders()
      .then((providers) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          providersLoading: false,
          providersError: null,
          providers,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          providersLoading: false,
          providersError: describeError(error, '加载服务商列表失败'),
          providers: [],
        }))
      })
    return () => {
      cancelled = true
    }
  }, [providersReloadKey])

  useEffect(() => {
    let cancelled = false
    listSubscriptions({ sort: 'renew_at', order: 'asc' })
      .then((subscriptions) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          subscriptionsLoading: false,
          subscriptionsError: null,
          subscriptions,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          subscriptionsLoading: false,
          subscriptionsError: describeError(error, '加载订阅证据失败'),
          subscriptions: [],
        }))
      })
    return () => {
      cancelled = true
    }
  }, [subscriptionsReloadKey])

  function retryInventory() {
    setState((current) => ({ ...current, inventoryLoading: true, inventoryError: null }))
    setInventoryReloadKey((key) => key + 1)
  }

  function retryProviders() {
    setState((current) => ({ ...current, providersLoading: true, providersError: null }))
    setProvidersReloadKey((key) => key + 1)
  }

  function retrySubscriptions() {
    setState((current) => ({ ...current, subscriptionsLoading: true, subscriptionsError: null }))
    setSubscriptionsReloadKey((key) => key + 1)
  }

  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(state.subscriptions),
    [state.subscriptions],
  )
  const subscriptionEvidence: SubscriptionEvidenceStatus = state.subscriptionsLoading
    ? 'loading'
    : state.subscriptionsError
      ? 'error'
      : 'ready'
  const inventoryRows = useMemo(
    () => buildInventoryRows(state.vps, subscriptionsByVPS, subscriptionEvidence),
    [state.vps, subscriptionsByVPS, subscriptionEvidence],
  )
  const filteredRows = useMemo(
    () => applyInventoryFilters(inventoryRows, filters),
    [inventoryRows, filters],
  )
  const visibleRows = useMemo(
    () => filteredRows.filter((row) => matchesSearch(row, searchQuery)),
    [filteredRows, searchQuery],
  )
  const selectedRow = visibleRows.find((row) => row.vps.vps_id === selectedID) ?? null
  const providerSelectOptions = providerFilterOptions(state.providers)
  const active = hasActiveFilters(filters)
  const subscriptionDependentView = filters.view === 'renewal' || filters.view === 'missing_subscription'
  const subscriptionViewBlocked = subscriptionDependentView && subscriptionEvidence !== 'ready' && state.vps.length > 0
  const missingSubscriptionCount = subscriptionEvidence === 'ready'
    ? inventoryRows.filter((row) => !row.subscription).length
    : null
  const unreviewedCount = inventoryRows.filter((row) => row.vps.renewal_decision === 'unreviewed').length
  const unlinkedCount = inventoryRows.filter((row) => row.vps.active_monitoring_instance_link_count <= 0).length
  const cancellationAttentionCount = inventoryRows.filter(hasCancellationAttention).length
  const missingFactsCount = inventoryRows.filter((row) => hasMissingVPSFacts(row.vps)).length
  const renewalDueCount = subscriptionEvidence === 'ready'
    ? inventoryRows.filter((row) => row.renewalDue).length
    : null
  const quickViews = [
    { value: 'all', label: '全部', count: inventoryRows.length },
    { value: 'renewal', label: '30天续费', ...(renewalDueCount == null ? {} : { count: renewalDueCount }) },
    { value: 'unreviewed', label: '未评估', count: unreviewedCount },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'cancellation_attention', label: '取消待处理', count: cancellationAttentionCount },
    { value: 'missing_subscription', label: '缺订阅', ...(missingSubscriptionCount == null ? {} : { count: missingSubscriptionCount }) },
    { value: 'missing_facts', label: '缺信息', count: missingFactsCount },
  ] satisfies Array<{ value: VPSQuickView; label: string; count?: number }>

  function patchSearchParams(patch: (params: URLSearchParams) => void, flushSync = false) {
    const next = new URLSearchParams(searchParams)
    patch(next)
    setSearchParams(next, { replace: true, flushSync })
  }

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    const nextFilters = { ...filters, [key]: value }
    patchSearchParams((params) => writeFilters(params, nextFilters))
  }

  function setWorkspace(next: VPSWorkspace) {
    writeStoredWorkspace(next)
    patchSearchParams((params) => {
      params.set('workspace', next)
    })
  }

  function setSelected(vpsID: string) {
    patchSearchParams((params) => {
      params.set('selected', vpsID)
    })
  }

  function selectOrToggle(vpsID: string) {
    if (selectedID === vpsID) {
      setAccordionOpen((open) => !open)
      return
    }
    setSelected(vpsID)
    setAccordionOpen(true)
  }

  function setSearchQuery(value: string) {
    patchSearchParams((params) => {
      const next = value.trim()
      if (next) params.set('q', value)
      else params.delete('q')
    }, true)
  }

  function clearFilters() {
    patchSearchParams((params) => writeFilters(params, INITIAL_FILTER_STATE))
  }

  function openFilterDrawer() {
    setDraftFilters(filters)
    setFilterDrawerOpen(true)
  }

  function applyDrawerFilters() {
    patchSearchParams((params) => writeFilters(params, draftFilters))
    setFilterDrawerOpen(false)
  }

  const listEmptyMessage = state.vps.length === 0
    ? { title: '还没有录入 VPS 资产', detail: '先录入 VPS。' }
    : { title: '当前筛选没有匹配 VPS', detail: searchQuery.trim() ? '改搜索或清空筛选。' : '清空筛选或新建 VPS。' }
  const inspectorEmpty = inspectorEmptyMessage(selectedID, visibleRows.length)
  const selectedDetailHref = selectedRow ? vpsDetailHref(selectedRow.vps.vps_id, filters.view) : null

  return (
    <div className="page vps-page">
      <header className="page__head">
        <h1 className="page__title">VPS 资产</h1>
        <div className="page__actions">
          <Link className="btn sm secondary" to={assetDecisionHrefForFilters(filters)}>进入组合决策</Link>
          <Link className="btn sm secondary" to="/archive">查看归档</Link>
          <button type="button" className="btn sm secondary" onClick={openFilterDrawer}>筛选</button>
          <button type="button" className="btn sm primary" onClick={() => setCreateOpen(true)}>
            {state.vps.length === 0 ? '创建第一台 VPS' : '添加 VPS'}
          </button>
        </div>
      </header>

      <div className="vps-page__tools">
        <SegmentedControl
          label="VPS 工作视图"
          items={WORKSPACE_ITEMS}
          value={workspace}
          onChange={setWorkspace}
        />
        <input
          className="vps-page__search"
          type="search"
          aria-label="搜索 VPS"
          placeholder="搜索名称、IP、服务商、位置"
          value={searchQuery}
          autoComplete="off"
          onChange={(event) => setSearchQuery(event.target.value)}
        />
        <p className="vps-page__stats">
          <span className="vps-mono">{inventoryRows.length}</span> 台
          {!subscriptionViewBlocked && visibleRows.length !== inventoryRows.length ? (
            <> · 显示 <span className="vps-mono">{visibleRows.length}</span></>
          ) : null}
        </p>
      </div>

      <div className="page-filters">
        <SegmentedControl
          label="VPS 快速视图"
          items={quickViews}
          value={filters.view}
          onChange={(view) => setFilter('view', view)}
        />
      </div>

      {active && (
        <div className="filter-bar">
          {filters.view !== 'all' && <FilterChip label={`视图: ${quickViewLabel(filters.view)}`} onRemove={() => setFilter('view', 'all')} />}
          {filters.provider_id && <FilterChip label={`服务商: ${providerName(filters.provider_id, state.providers)}`} onRemove={() => setFilter('provider_id', null)} />}
          {filters.lifecycle_status && <FilterChip label={`生命周期: ${lifecycleLabel(filters.lifecycle_status)}`} onRemove={() => setFilter('lifecycle_status', null)} />}
          {filters.usage_status && <FilterChip label={`用途: ${usageLabel(filters.usage_status)}`} onRemove={() => setFilter('usage_status', null)} />}
          {filters.renewal_decision && <FilterChip label={`续费: ${renewalLabel(filters.renewal_decision)}`} onRemove={() => setFilter('renewal_decision', null)} />}
          <button type="button" className="filter-clear" onClick={clearFilters}>清除全部</button>
        </div>
      )}

      {state.providersError && !state.providersLoading ? (
        <div className="vps-page__notice" role="status">
          <span>服务商列表加载失败。{state.providersError}</span>
          <button type="button" className="btn sm secondary" onClick={retryProviders}>重试</button>
        </div>
      ) : null}

      {subscriptionEvidence === 'error' && !subscriptionViewBlocked ? (
        <div className="vps-page__notice" role="status">
          <span>订阅加载失败。{state.subscriptionsError}</span>
          <button type="button" className="btn sm secondary" onClick={retrySubscriptions}>重试</button>
        </div>
      ) : null}

      <div className="vps-canvas" data-workspace={workspace}>
        {state.inventoryLoading ? (
          <div className="vps-canvas__state">
            <PageStateView kind="loading" title="正在加载 VPS…" surface="empty" compact />
          </div>
        ) : state.inventoryError ? (
          <div className="vps-canvas__state">
            <PageStateView
              kind="error"
              title="VPS 库存不可用"
              description={state.inventoryError}
              technicalSummary={state.inventoryError}
              action={<button type="button" className="btn sm secondary" onClick={retryInventory}>重试</button>}
              surface="empty"
              compact
            />
          </div>
        ) : subscriptionViewBlocked && subscriptionEvidence === 'loading' ? (
          <div className="vps-canvas__state">
            <PageStateView kind="loading" title="正在加载订阅证据…" surface="empty" compact />
          </div>
        ) : subscriptionViewBlocked && subscriptionEvidence === 'error' ? (
          <div className="vps-canvas__state">
            <PageStateView
              kind="error"
              title="订阅证据不可用"
              description={state.subscriptionsError ?? '加载订阅证据失败'}
              technicalSummary={state.subscriptionsError}
              action={<button type="button" className="btn sm secondary" onClick={retrySubscriptions}>重试</button>}
              surface="empty"
              compact
            />
          </div>
        ) : workspace === 'workbench' ? (
          <div className="vps-workbench">
            <div className="vps-workbench__list" role="region" aria-label="VPS 清单">
              {visibleRows.length === 0 ? (
                <div className="vps-canvas__empty">
                  <strong>{listEmptyMessage.title}</strong>
                  <div>{listEmptyMessage.detail}</div>
                </div>
              ) : (
                <table className="vps-workbench__table">
                  <thead>
                    <tr>
                      <th scope="col">机器</th>
                      <th scope="col">位置与规格</th>
                      <th scope="col">经营与续费</th>
                      <th scope="col">证据</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visibleRows.map((row) => {
                      const selected = row.vps.vps_id === selectedID
                      const expanded = selected && accordionOpen
                      const attention = cancellationAttentionReason(row)
                      const detailHref = vpsDetailHref(row.vps.vps_id, filters.view)
                      return (
                        <Fragment key={row.vps.vps_id}>
                          {/* a11y-allow-nonsemantic-click: primary-link-row-enhancement */}
                          <tr
                            className="vps-workbench__row row-clickable"
                            aria-selected={selected}
                            onClick={(event) => {
                              if (isInteractiveRowTarget(event.target)) return
                              selectOrToggle(row.vps.vps_id)
                            }}
                          >
                            <td>
                              <div className="vps-workbench__identity-head">
                                <button
                                  type="button"
                                  className="vps-workbench__name"
                                  aria-label={`选择 ${row.vps.display_name}`}
                                  aria-pressed={selected}
                                  aria-expanded={expanded}
                                  aria-controls={expanded ? accordionPanelId(row.vps.vps_id) : undefined}
                                  onClick={() => selectOrToggle(row.vps.vps_id)}
                                >
                                  {row.vps.display_name}
                                </button>
                              </div>
                              <div className="vps-workbench__meta">
                                <span className="vps-mono">{vpsPrimaryAddress(row.vps)}</span>
                                {row.vps.provider_name.trim() ? ` · ${row.vps.provider_name}` : ''}
                              </div>
                            </td>
                            <td>
                              <div>{vpsPlaceLabel(row.vps)}</div>
                              <div className="vps-workbench__meta">{vpsSpecLabel(row.vps)}</div>
                            </td>
                            <td>
                              <div className="badge-row">
                                <LifecycleBadge value={row.vps.lifecycle_status} />
                                <UsageBadge value={row.vps.usage_status} />
                              </div>
                              <div className="vps-workbench__meta">
                                <RenewalBadge value={row.vps.renewal_decision} />
                                {' · '}
                                {renderRenewalDate(row)}
                              </div>
                            </td>
                            <td>
                              <IPQualityBadge
                                {...(row.vps.ip_quality_summary === undefined
                                  ? {}
                                  : { summary: row.vps.ip_quality_summary })}
                              />
                              <div className={attention ? 'vps-workbench__meta vps-tone-warn' : 'vps-workbench__meta'}>
                                {row.vps.active_monitoring_instance_link_count > 0
                                  ? `监控 ${row.vps.active_monitoring_instance_link_count}`
                                  : '未关联监控'}
                                {attention ? ` · ${attention}` : ''}
                              </div>
                            </td>
                          </tr>
                          {expanded ? (
                            <VPSWorkbenchAccordionRow
                              row={row}
                              detailHref={detailHref}
                              currentInventoryHref={currentInventoryHref}
                            />
                          ) : null}
                        </Fragment>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        ) : (
          <div className="vps-ledger">
            <aside className="vps-ledger__dir">
              <div className="vps-ledger__head">
                <h2 className="vps-ledger__title">{visibleRows.length} 台机器</h2>
              </div>
              <div className="vps-ledger__items" role="region" aria-label="VPS 目录">
                {visibleRows.length === 0 ? (
                  <p className="vps-canvas__empty">{listEmptyMessage.title} {listEmptyMessage.detail}</p>
                ) : (
                  visibleRows.map((row) => {
                    const selected = row.vps.vps_id === selectedID
                    const attention = cancellationAttentionReason(row)
                    return (
                      <button
                        key={row.vps.vps_id}
                        type="button"
                        className="vps-ledger__item"
                        aria-label={`选择 ${row.vps.display_name}`}
                        aria-pressed={selected}
                        onClick={() => setSelected(row.vps.vps_id)}
                      >
                        <span className="vps-ledger__item-name">{row.vps.display_name}</span>
                        <span className="vps-ledger__item-line">
                          <span className="vps-mono">{vpsPrimaryAddress(row.vps)}</span>
                          {compactLine(['', row.vps.provider_name, vpsPlaceLabel(row.vps)]) ? ` · ${compactLine([row.vps.provider_name, vpsPlaceLabel(row.vps)])}` : ''}
                        </span>
                        <span className={attention ? 'vps-ledger__item-st vps-tone-warn' : 'vps-ledger__item-st'}>
                          <LifecycleBadge value={row.vps.lifecycle_status} />
                          {' · '}
                          <RenewalBadge value={row.vps.renewal_decision} />
                          {attention ? ` · ${attention}` : ''}
                        </span>
                      </button>
                    )
                  })
                )}
              </div>
            </aside>
            <VPSInspector
              row={selectedRow}
              detailHref={selectedDetailHref}
              currentInventoryHref={currentInventoryHref}
              subscriptionsError={state.subscriptionsError}
              emptyMessage={inspectorEmpty}
            />
          </div>
        )}
      </div>

      <VPSCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        providers={state.providers}
        providersLoading={state.providersLoading}
        providersError={state.providersError}
        onCreated={(vps) => navigate(`/vps/${vps.vps_id}`, { state: { vpsInventoryHref: currentInventoryHref } })}
        onProviderCreated={(p) => setState((s) => ({ ...s, providers: [...s.providers, p] }))}
      />

      <Modal
        open={filterDrawerOpen}
        onClose={() => setFilterDrawerOpen(false)}
        title="高级筛选"
        ariaLabel="VPS 高级筛选"
      >
        <div className="asset-filter-drawer">
          <FilterSelect
            label="服务商"
            value={draftFilters.provider_id}
            options={providerSelectOptions}
            onChange={(value) => setDraftFilters({ ...draftFilters, provider_id: value })}
          />
          <FilterSelect
            label="生命周期"
            value={draftFilters.lifecycle_status}
            options={LIFECYCLE_OPTIONS}
            onChange={(value) => setDraftFilters({ ...draftFilters, lifecycle_status: value as VPSLifecycleStatus | null })}
          />
          <FilterSelect
            label="用途状态"
            value={draftFilters.usage_status}
            options={USAGE_OPTIONS}
            onChange={(value) => setDraftFilters({ ...draftFilters, usage_status: value as VPSUsageStatus | null })}
          />
          <FilterSelect
            label="续费决策"
            value={draftFilters.renewal_decision}
            options={RENEWAL_OPTIONS}
            onChange={(value) => setDraftFilters({ ...draftFilters, renewal_decision: value as VPSRenewalDecision | null })}
          />
          <div className="asset-filter-drawer__actions">
            <Button
              variant="secondary"
              onClick={() => {
                setDraftFilters(INITIAL_FILTER_STATE)
              }}
            >
              重置
            </Button>
            <Button onClick={applyDrawerFilters}>应用筛选</Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
