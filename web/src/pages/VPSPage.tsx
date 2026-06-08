import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import {
  Button,
  Modal,
  MonoDigits,
  Tabs,
} from '../components/atoms'
import { FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { PageState as PageStateView } from '../components/PageState'
import { VPSCreateModal } from '../components/VPSCreateModal'
import { ApiError, listProviders, listSubscriptions, listVPSAssets } from '../lib/api'
import { formatDate, formatOptional } from '../lib/format'
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
  LifecycleBadge,
  IPQualityBadge,
  RenewalBadge,
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
  usageLabel,
  type AssetQualityIssue,
} from './assetPageUtils'

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

function filterToQuery(filters: FilterState): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.view !== 'all') params.set('view', filters.view)
  if (filters.provider_id) params.set('provider_id', filters.provider_id)
  if (filters.lifecycle_status) params.set('lifecycle_status', filters.lifecycle_status)
  if (filters.usage_status) params.set('usage_status', filters.usage_status)
  if (filters.renewal_decision) params.set('renewal_decision', filters.renewal_decision)
  return params
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
  if (vpsToCancel && subscriptionActive) return 'VPS 待取消，订阅仍 active'
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
  if (row.subscriptionEvidence !== 'ready') return '—'
  if (!row.subscription?.renew_at) return '—'
  const days = daysUntilDate(row.subscription.renew_at)
  if (days != null && days <= 30) {
    return <span className="text-warn">{formatDate(row.subscription.renew_at)}</span>
  }
  return formatDate(row.subscription.renew_at)
}

function providerName(providerID: string | null, providers: ProviderRecord[]): string {
  if (!providerID) return ''
  return providers.find((provider) => provider.provider_id === providerID)?.name ?? providerID
}

export function VPSPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const [draftFilters, setDraftFilters] = useState<FilterState>(filters)
  const [filterDrawerOpen, setFilterDrawerOpen] = useState(false)
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)

  useEffect(() => {
    let cancelled = false

    Promise.all([listVPSAssets(), listProviders()])
      .then(([vps, providers]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          inventoryLoading: false,
          inventoryError: null,
          vps,
          providers,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          inventoryLoading: false,
          inventoryError: describeError(error, '加载 VPS 资产失败'),
          vps: [],
          providers: [],
        }))
      })

    return () => {
      cancelled = true
    }
  }, [])

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
  }, [])

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
  const providerSelectOptions = providerFilterOptions(state.providers)
  const active = hasActiveFilters(filters)
  const missingSubscriptionCount = subscriptionEvidence === 'ready'
    ? inventoryRows.filter((row) => !row.subscription).length
    : 0
  const unreviewedCount = inventoryRows.filter((row) => row.vps.renewal_decision === 'unreviewed').length
  const unlinkedCount = inventoryRows.filter((row) => row.vps.active_monitoring_instance_link_count <= 0).length
  const cancellationAttentionCount = inventoryRows.filter(hasCancellationAttention).length
  const missingFactsCount = inventoryRows.filter((row) => hasMissingVPSFacts(row.vps)).length
  const renewalDueCount = inventoryRows.filter((row) => row.renewalDue).length
  const quickViews = [
    { value: 'all', label: '全部', count: inventoryRows.length },
    { value: 'renewal', label: '30天续费', count: renewalDueCount },
    { value: 'unreviewed', label: '未评估', count: unreviewedCount },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'cancellation_attention', label: '取消待处理', count: cancellationAttentionCount },
    { value: 'missing_subscription', label: '缺订阅', count: missingSubscriptionCount },
    { value: 'missing_facts', label: '缺信息', count: missingFactsCount },
  ] satisfies Array<{ value: VPSQuickView; label: string; count: number }>

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    const next = { ...filters, [key]: value }
    setSearchParams(filterToQuery(next), { replace: true })
  }

  function clearFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function openFilterDrawer() {
    setDraftFilters(filters)
    setFilterDrawerOpen(true)
  }

  function applyDrawerFilters() {
    setSearchParams(filterToQuery(draftFilters), { replace: true })
    setFilterDrawerOpen(false)
  }

  return (
    <div className="animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">VPS 资产</h1>
        </div>
        <div className="header-actions">
          <Link className="btn sm secondary" to={assetDecisionHrefForFilters(filters)}>进入组合决策</Link>
          <Link className="btn sm secondary" to="/archive">查看归档</Link>
          <button type="button" className="btn sm secondary" onClick={openFilterDrawer}>筛选</button>
          <button type="button" className="btn sm primary" onClick={() => setCreateOpen(true)}>
            {state.vps.length === 0 ? '创建第一台 VPS' : '添加 VPS'}
          </button>
        </div>
      </div>

      <div className="tabs animate-in">
        <Tabs
          items={quickViews}
          value={filters.view}
          onChange={(view) => setFilter('view', view)}
          variant="pill"
        />
      </div>

      {active && (
        <div className="filter-bar animate-in d1">
          {filters.view !== 'all' && <FilterChip label={`视图: ${quickViewLabel(filters.view)}`} onRemove={() => setFilter('view', 'all')} />}
          {filters.provider_id && <FilterChip label={`服务商: ${providerName(filters.provider_id, state.providers)}`} onRemove={() => setFilter('provider_id', null)} />}
          {filters.lifecycle_status && <FilterChip label={`生命周期: ${lifecycleLabel(filters.lifecycle_status)}`} onRemove={() => setFilter('lifecycle_status', null)} />}
          {filters.usage_status && <FilterChip label={`用途: ${usageLabel(filters.usage_status)}`} onRemove={() => setFilter('usage_status', null)} />}
          {filters.renewal_decision && <FilterChip label={`续费: ${renewalLabel(filters.renewal_decision)}`} onRemove={() => setFilter('renewal_decision', null)} />}
          <button type="button" className="filter-clear" onClick={clearFilters}>清除全部</button>
        </div>
      )}

      {subscriptionEvidence === 'error' && (
        <p className="text-sm text-warn" role="status">
          订阅不可用，不判定。{state.subscriptionsError}
        </p>
      )}

      <div className="animate-in d2">
        {state.inventoryLoading ? (
          <PageStateView kind="loading" title="正在加载 VPS…" surface="empty" compact />
        ) : state.inventoryError ? (
          <PageStateView kind="error" title="VPS 库存不可用" description={state.inventoryError} technicalSummary={state.inventoryError} surface="empty" compact />
        ) : filteredRows.length === 0 ? (
          <div className="empty-state">
            <strong>{active ? '当前筛选没有匹配 VPS' : '还没有录入 VPS 资产'}</strong>
            <span>{active ? '清空筛选或新建 VPS。' : '先录入 VPS。'}</span>
          </div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>VPS</th>
                <th>服务商</th>
                <th>IP</th>
                <th>IP 质量</th>
                <th>生命周期</th>
                <th>续费决策</th>
                <th>到期</th>
                <th>关联监控实例</th>
                <th>资产联动</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((row) => {
                const cancellationReason = cancellationAttentionReason(row)
                return (
                  <tr key={row.vps.vps_id} onClick={() => navigate(`/vps/${row.vps.vps_id}`)} className="row-clickable">
                    <td className="name">{row.vps.display_name}</td>
                    <td>{formatOptional(row.vps.provider_name)}</td>
                    <td className="mono">{row.vps.ipv4 || row.vps.ssh_host || '—'}</td>
                    <td><IPQualityBadge summary={row.vps.ip_quality_summary} /></td>
                    <td><LifecycleBadge value={row.vps.lifecycle_status} /></td>
                    <td><RenewalBadge value={row.vps.renewal_decision} /></td>
                    <td className="time">{renderRenewalDate(row)}</td>
                    <td>{row.vps.active_monitoring_instance_link_count > 0 ? <MonoDigits>{row.vps.active_monitoring_instance_link_count}</MonoDigits> : '—'}</td>
                    <td>
                      {cancellationReason ? (
                        <span className="asset-context-pill asset-context-pill--attention">{cancellationReason}</span>
                      ) : (
                        <span className="asset-context-pill">已同步</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      <VPSCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        providers={state.providers}
        existingCountries={state.vps.map((vps) => vps.country)}
        onCreated={(vps) => navigate(`/vps/${vps.vps_id}`)}
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
