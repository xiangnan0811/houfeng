import { type FormEvent, useEffect, useId, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import {
  Button,
  Drawer,
  Input,
  MonoDigits,
  Tabs,
} from '../components/atoms'
import { FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createVPSAsset, listProviders, listSubscriptions, listVPSAssets } from '../lib/api'
import { formatDate, formatOptional } from '../lib/format'
import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type CreateVPSAssetInput,
  type ProviderRecord,
  type SubscriptionRecord,
  type VPSAssetRecord,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../lib/types'
import {
  LifecycleBadge,
  RenewalBadge,
} from './assetPageBadges'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  groupSubscriptionsByVPS,
  hasMissingVPSFacts,
  isSubscriptionInRenewalWindow,
  lifecycleLabel,
  parseLabels,
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
  | 'missing_subscription'
  | 'missing_facts'
  | 'archived'

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

type CreateVPSFormState = {
  displayName: string
  providerID: string
  providerName: string
  productName: string
  orderRef: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  sshHost: string
  sshPort: string
  sshUser: string
  osName: string
  virtualization: string
  lifecycleStatus: VPSLifecycleStatus
  usageStatus: VPSUsageStatus
  renewalDecision: VPSRenewalDecision
  importance: string
  labels: string
  note: string
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

const INITIAL_CREATE_FORM: CreateVPSFormState = {
  displayName: '',
  providerID: '',
  providerName: '',
  productName: '',
  orderRef: '',
  country: '',
  region: '',
  city: '',
  datacenter: '',
  ipv4: '',
  ipv6: '',
  sshHost: '',
  sshPort: '22',
  sshUser: 'root',
  osName: '',
  virtualization: '',
  lifecycleStatus: 'active',
  usageStatus: 'unknown',
  renewalDecision: 'unreviewed',
  importance: 'normal',
  labels: '',
  note: '',
}

const INITIAL_FILTER_STATE: FilterState = {
  view: 'all',
  provider_id: null,
  lifecycle_status: null,
  usage_status: null,
  renewal_decision: null,
}

const LIFECYCLE_OPTIONS = Object.entries(VPS_LIFECYCLE_STATUS_LABELS).map(([value, label]) => ({
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
  'missing_subscription',
  'missing_facts',
  'archived',
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

function hasActiveFilters(filters: FilterState): boolean {
  return Boolean(
    filters.view !== 'all' ||
      filters.provider_id ||
      filters.lifecycle_status ||
      filters.usage_status ||
      filters.renewal_decision,
  )
}

function buildCreateInput(form: CreateVPSFormState, providers: ProviderRecord[]): CreateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
  }
  const selectedProvider = providers.find((provider) => provider.provider_id === form.providerID)
  const sshPort = form.sshPort.trim() === '' ? undefined : Number.parseInt(form.sshPort.trim(), 10)
  if (sshPort != null && (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535)) {
    throw new Error('SSH 端口必须为 1 到 65535。')
  }

  return {
    display_name: form.displayName.trim(),
    provider_id: form.providerID || null,
    provider_name: selectedProvider?.name ?? form.providerName.trim(),
    product_name: form.productName.trim(),
    order_ref: form.orderRef.trim(),
    country: form.country.trim(),
    region: form.region.trim(),
    city: form.city.trim(),
    datacenter: form.datacenter.trim(),
    ipv4: form.ipv4.trim(),
    ipv6: form.ipv6.trim(),
    ssh_host: form.sshHost.trim(),
    ...(sshPort == null ? {} : { ssh_port: sshPort }),
    ssh_user: form.sshUser.trim(),
    os_name: form.osName.trim(),
    virtualization: form.virtualization.trim(),
    lifecycle_status: form.lifecycleStatus,
    usage_status: form.usageStatus,
    renewal_decision: form.renewalDecision,
    importance: form.importance.trim() || 'normal',
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function providerOptions(providers: ProviderRecord[]): FilterSelectOption[] {
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
  return vpsRows.map((vps) => {
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
  if (view === 'unlinked') return row.vps.active_node_link_count <= 0
  if (view === 'missing_subscription') return row.subscriptionEvidence === 'ready' && !row.subscription
  if (view === 'missing_facts') return hasMissingVPSFacts(row.vps)
  if (view === 'archived') return row.vps.lifecycle_status === 'archived'
  return true
}

function inventoryRank(row: InventoryRow): number {
  let rank = 0
  if (row.vps.renewal_decision === 'unreviewed') rank += 120
  if (row.renewalDue) rank += 100
  if (row.subscriptionEvidence === 'ready' && !row.subscription) rank += 80
  if (row.vps.active_node_link_count <= 0) rank += 60
  if (hasMissingVPSFacts(row.vps)) rank += 30
  return rank
}

function quickViewLabel(value: VPSQuickView): string {
  if (value === 'all') return '全部'
  if (value === 'renewal') return '30天续费'
  if (value === 'unreviewed') return '未评估'
  if (value === 'unlinked') return '未关联'
  if (value === 'missing_subscription') return '缺订阅'
  if (value === 'missing_facts') return '缺基础信息'
  return '已归档'
}

function renderRenewalDate(row: InventoryRow) {
  if (row.subscriptionEvidence !== 'ready') return '—'
  if (!row.subscription?.renew_at) return '—'
  const days = daysUntilDate(row.subscription.renew_at)
  if (days != null && days <= 30) {
    return <span style={{ color: 'var(--warn)' }}>{formatDate(row.subscription.renew_at)}</span>
  }
  return formatDate(row.subscription.renew_at)
}

function providerName(providerID: string | null, providers: ProviderRecord[]): string {
  if (!providerID) return ''
  return providers.find((provider) => provider.provider_id === providerID)?.name ?? providerID
}

export function VPSPage() {
  const navigate = useNavigate()
  const createProviderSelectId = useId()
  const createLifecycleSelectId = useId()
  const createUsageSelectId = useId()
  const createRenewalSelectId = useId()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const [draftFilters, setDraftFilters] = useState<FilterState>(filters)
  const [filterDrawerOpen, setFilterDrawerOpen] = useState(false)
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateVPSFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

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
  const providerSelectOptions = providerOptions(state.providers)
  const active = hasActiveFilters(filters)
  const missingSubscriptionCount = subscriptionEvidence === 'ready'
    ? inventoryRows.filter((row) => !row.subscription).length
    : 0
  const unreviewedCount = inventoryRows.filter((row) => row.vps.renewal_decision === 'unreviewed').length
  const unlinkedCount = inventoryRows.filter((row) => row.vps.active_node_link_count <= 0).length
  const missingFactsCount = inventoryRows.filter((row) => hasMissingVPSFacts(row.vps)).length
  const renewalDueCount = inventoryRows.filter((row) => row.renewalDue).length
  const quickViews = [
    { value: 'all', label: '全部', count: inventoryRows.length },
    { value: 'renewal', label: '30天续费', count: renewalDueCount },
    { value: 'unreviewed', label: '未评估', count: unreviewedCount },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'missing_subscription', label: '缺订阅', count: missingSubscriptionCount },
    { value: 'missing_facts', label: '缺信息', count: missingFactsCount },
    { value: 'archived', label: '已归档', count: inventoryRows.filter((row) => row.vps.lifecycle_status === 'archived').length },
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

  function openCreateDrawer() {
    setCreateOpen(true)
  }

  function closeCreateDrawer() {
    setCreateOpen(false)
    setCreateForm(INITIAL_CREATE_FORM)
    setCreateError(null)
  }

  function handleCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let input: CreateVPSAssetInput
    try {
      input = buildCreateInput(createForm, state.providers)
    } catch (error: unknown) {
      setCreateError(describeError(error, 'VPS 输入无效'))
      return
    }

    setCreateSubmitting(true)
    createVPSAsset(input)
      .then((vps) => {
        navigate(`/vps/${vps.vps_id}`)
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建 VPS 失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  return (
    <div className="animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">VPS 资产</h1>
        </div>
        <div className="header-actions">
          <button type="button" className="btn sm secondary" onClick={openFilterDrawer}>筛选</button>
          <button type="button" className="btn sm primary" onClick={openCreateDrawer}>
            {state.vps.length === 0 ? '创建第一台 VPS' : '导入'}
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
        <p style={{ fontSize: '11px', color: 'var(--warn)', marginTop: '8px' }} role="status">
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
                <th>生命周期</th>
                <th>续费决策</th>
                <th>到期</th>
                <th>关联节点</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((row) => (
                <tr key={row.vps.vps_id} onClick={() => navigate(`/vps/${row.vps.vps_id}`)} style={{ cursor: 'pointer' }}>
                  <td className="name">{row.vps.display_name}</td>
                  <td>{formatOptional(row.vps.provider_name)}</td>
                  <td className="mono">{row.vps.ipv4 || row.vps.ssh_host || '—'}</td>
                  <td><LifecycleBadge value={row.vps.lifecycle_status} /></td>
                  <td><RenewalBadge value={row.vps.renewal_decision} /></td>
                  <td className="time">{renderRenewalDate(row)}</td>
                  <td>{row.vps.active_node_link_count > 0 ? <MonoDigits>{row.vps.active_node_link_count}</MonoDigits> : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <Drawer
        open={createOpen}
        onClose={closeCreateDrawer}
        title="VPS 创建"
        ariaLabel="VPS 创建表单"
      >
        <div className="asset-create-drawer">
          <form className="asset-create-form" onSubmit={handleCreateSubmit}>
            <fieldset className="asset-create-form__group">
              <legend>基础识别</legend>
              <Input label="VPS 名称" value={createForm.displayName} onChange={(event) => setCreateForm({ ...createForm, displayName: event.target.value })} />
              <label className="input-field" htmlFor={createProviderSelectId}>
                <span className="input-field__label">资产服务商</span>
                <select id={createProviderSelectId} aria-label="资产服务商" className="input" value={createForm.providerID} onChange={(event) => setCreateForm({ ...createForm, providerID: event.target.value })}>
                  <option value="">未关联服务商</option>
                  {providerSelectOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
                <span className="input-field__hint">
                  {state.providers.length === 0
                    ? '无服务商主数据；可保留名称快照。'
                    : '优先选择主数据；快照用于展示。'}
                  {' '}
                  <Link className="text-link" to="/providers">服务商列表</Link>
                </span>
              </label>
              <Input label="服务商名称快照" value={createForm.providerName} onChange={(event) => setCreateForm({ ...createForm, providerName: event.target.value })} />
              <Input label="产品名" value={createForm.productName} onChange={(event) => setCreateForm({ ...createForm, productName: event.target.value })} />
              <Input label="订单号" value={createForm.orderRef} onChange={(event) => setCreateForm({ ...createForm, orderRef: event.target.value })} />
            </fieldset>

            <fieldset className="asset-create-form__group">
              <legend>访问入口</legend>
              <Input label="国家" value={createForm.country} onChange={(event) => setCreateForm({ ...createForm, country: event.target.value })} />
              <Input label="区域" value={createForm.region} onChange={(event) => setCreateForm({ ...createForm, region: event.target.value })} />
              <Input label="城市" value={createForm.city} onChange={(event) => setCreateForm({ ...createForm, city: event.target.value })} />
              <Input label="数据中心" value={createForm.datacenter} onChange={(event) => setCreateForm({ ...createForm, datacenter: event.target.value })} />
              <Input label="IPv4" value={createForm.ipv4} onChange={(event) => setCreateForm({ ...createForm, ipv4: event.target.value })} />
              <Input label="IPv6" value={createForm.ipv6} onChange={(event) => setCreateForm({ ...createForm, ipv6: event.target.value })} />
              <Input label="SSH Host" value={createForm.sshHost} onChange={(event) => setCreateForm({ ...createForm, sshHost: event.target.value })} />
              <Input label="SSH 端口" type="number" value={createForm.sshPort} onChange={(event) => setCreateForm({ ...createForm, sshPort: event.target.value })} />
              <Input label="SSH 用户" value={createForm.sshUser} onChange={(event) => setCreateForm({ ...createForm, sshUser: event.target.value })} />
            </fieldset>

            <fieldset className="asset-create-form__group">
              <legend>运行与决策</legend>
              <Input label="操作系统" value={createForm.osName} onChange={(event) => setCreateForm({ ...createForm, osName: event.target.value })} />
              <Input label="虚拟化" value={createForm.virtualization} onChange={(event) => setCreateForm({ ...createForm, virtualization: event.target.value })} />
              <label className="input-field" htmlFor={createLifecycleSelectId}>
                <span className="input-field__label">生命周期</span>
                <select id={createLifecycleSelectId} aria-label="生命周期" className="input" value={createForm.lifecycleStatus} onChange={(event) => setCreateForm({ ...createForm, lifecycleStatus: event.target.value as VPSLifecycleStatus })}>
                  {LIFECYCLE_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <label className="input-field" htmlFor={createUsageSelectId}>
                <span className="input-field__label">用途状态</span>
                <select id={createUsageSelectId} aria-label="用途状态" className="input" value={createForm.usageStatus} onChange={(event) => setCreateForm({ ...createForm, usageStatus: event.target.value as VPSUsageStatus })}>
                  {USAGE_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <label className="input-field" htmlFor={createRenewalSelectId}>
                <span className="input-field__label">续费决策</span>
                <select id={createRenewalSelectId} aria-label="续费决策" className="input" value={createForm.renewalDecision} onChange={(event) => setCreateForm({ ...createForm, renewalDecision: event.target.value as VPSRenewalDecision })}>
                  {RENEWAL_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <Input label="重要性" value={createForm.importance} onChange={(event) => setCreateForm({ ...createForm, importance: event.target.value })} />
            </fieldset>

            <fieldset className="asset-create-form__group asset-create-form__group--wide">
              <legend>备注标签</legend>
              <Input label="标签" hint="用逗号分隔" value={createForm.labels} onChange={(event) => setCreateForm({ ...createForm, labels: event.target.value })} />
              <Input name="note" label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            </fieldset>

            {createError && <p className="create-form__error" role="alert">{createError}</p>}
            <div className="page-form-actions asset-create-form__actions">
              <span className="asset-create-form__hint">创建后进入详情页。</span>
              <Button variant="secondary" type="button" onClick={closeCreateDrawer}>
                取消
              </Button>
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建 VPS'}
              </Button>
            </div>
          </form>
        </div>
      </Drawer>

      <Drawer
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
      </Drawer>
    </div>
  )
}
