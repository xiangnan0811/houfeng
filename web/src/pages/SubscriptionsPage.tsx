import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Button, DataTable, Drawer, Input, MonoDigits, type DataTableColumn } from '../components/atoms'
import { FilterBar, FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createSubscription, listSubscriptions, listVPSAssets, updateSubscription } from '../lib/api'
import { formatDate, formatMoney, formatOptional } from '../lib/format'
import {
  SUBSCRIPTION_STATUS_LABELS,
  type CreateSubscriptionInput,
  type SubscriptionListFilter,
  type SubscriptionRecord,
  type SubscriptionStatus,
  type UpdateSubscriptionInput,
  type VPSAssetRecord,
} from '../lib/types'
import { SubscriptionStatusBadge } from './assetPageBadges'
import { subscriptionStatusLabel } from './assetPageUtils'

type PageState = {
  loading: boolean
  error: string | null
  subscriptions: SubscriptionRecord[]
  vps: VPSAssetRecord[]
}

type FilterState = {
  vps_id: string | null
  status: SubscriptionStatus | null
  renew_window: string | null
}

type CreateSubscriptionFormState = {
  vpsID: string
  price: string
  currency: string
  billingCycle: string
  billingMonths: string
  startedAt: string
  renewAt: string
  autoRenew: boolean
  autoRenewCancelled: boolean
  status: SubscriptionStatus
  paymentMethod: string
  note: string
}

const INITIAL_PAGE_STATE: PageState = {
  loading: true,
  error: null,
  subscriptions: [],
  vps: [],
}

const INITIAL_CREATE_FORM: CreateSubscriptionFormState = {
  vpsID: '',
  price: '',
  currency: 'USD',
  billingCycle: 'monthly',
  billingMonths: '1',
  startedAt: '',
  renewAt: '',
  autoRenew: false,
  autoRenewCancelled: false,
  status: 'active',
  paymentMethod: '',
  note: '',
}

const STATUS_OPTIONS = Object.entries(SUBSCRIPTION_STATUS_LABELS).map(([value, label]) => ({
  value,
  label,
}))

const RENEW_WINDOW_OPTIONS: FilterSelectOption[] = [
  { value: '30', label: '未来 30 天' },
  { value: '60', label: '未来 60 天' },
  { value: '90', label: '未来 90 天' },
]

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseFilters(searchParams: URLSearchParams): FilterState {
  const status = searchParams.get('status') as SubscriptionStatus | null
  const renewWindow = searchParams.get('renew_within_days')
  return {
    vps_id: searchParams.get('vps_id') || null,
    status: status && status in SUBSCRIPTION_STATUS_LABELS ? status : null,
    renew_window: renewWindow && ['30', '60', '90'].includes(renewWindow) ? renewWindow : null,
  }
}

function filtersToParams(filters: FilterState): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.vps_id) params.set('vps_id', filters.vps_id)
  if (filters.status) params.set('status', filters.status)
  if (filters.renew_window) params.set('renew_within_days', filters.renew_window)
  return params
}

function filtersToAPI(filters: FilterState): SubscriptionListFilter {
  return {
    vps_id: filters.vps_id,
    status: filters.status,
    renew_within_days: filters.renew_window ? Number.parseInt(filters.renew_window, 10) : null,
    sort: filters.renew_window ? 'renew_at' : '',
    order: filters.renew_window ? 'asc' : '',
  }
}

function hasActiveFilters(filters: FilterState): boolean {
  return Boolean(filters.vps_id || filters.status || filters.renew_window)
}

function buildCreateInput(form: CreateSubscriptionFormState): CreateSubscriptionInput {
  if (form.vpsID.trim() === '') {
    throw new Error('VPS 不能为空。')
  }

  const price = Number.parseFloat(form.price.trim())
  if (!Number.isFinite(price) || price < 0) {
    throw new Error('价格必须为非负数字。')
  }

  const billingMonths = Number.parseInt(form.billingMonths.trim(), 10)
  if (!Number.isInteger(billingMonths) || billingMonths <= 0) {
    throw new Error('计费月数必须大于 0。')
  }

  const currency = form.currency.trim().toUpperCase()
  if (!/^[A-Z]{3}$/.test(currency)) {
    throw new Error('币种必须为 3 位大写代码。')
  }

  return {
    vps_id: form.vpsID.trim(),
    price,
    currency,
    billing_cycle: form.billingCycle.trim(),
    billing_months: billingMonths,
    started_at: form.startedAt || null,
    renew_at: form.renewAt || null,
    auto_renew: form.autoRenew,
    auto_renew_cancelled: form.autoRenewCancelled,
    status: form.status,
    payment_method: form.paymentMethod.trim(),
    note: form.note.trim(),
  }
}

function subscriptionToForm(subscription: SubscriptionRecord): CreateSubscriptionFormState {
  return {
    vpsID: subscription.vps_id,
    price: String(subscription.price),
    currency: subscription.currency,
    billingCycle: subscription.billing_cycle,
    billingMonths: String(subscription.billing_months),
    startedAt: subscription.started_at ?? '',
    renewAt: subscription.renew_at ?? '',
    autoRenew: subscription.auto_renew,
    autoRenewCancelled: subscription.auto_renew_cancelled,
    status: subscription.status,
    paymentMethod: subscription.payment_method,
    note: subscription.note,
  }
}

function buildUpdateInput(form: CreateSubscriptionFormState): UpdateSubscriptionInput {
  return buildCreateInput(form)
}

function vpsOptions(vps: VPSAssetRecord[]): FilterSelectOption[] {
  return vps.map((item) => ({
    value: item.vps_id,
    label: item.display_name,
  }))
}

function renewTime(renewAt?: string | null): number | null {
  if (!renewAt) return null
  const time = new Date(`${renewAt}T00:00:00Z`).getTime()
  return Number.isNaN(time) ? null : time
}

function describeUpcomingRenewal(subscriptions: SubscriptionRecord[]): string {
  const upcoming = subscriptions
    .filter((subscription) => subscription.status === 'active' || subscription.status === 'paused')
    .map((subscription) => ({ subscription, time: renewTime(subscription.renew_at) }))
    .filter((item): item is { subscription: SubscriptionRecord; time: number } => item.time != null)
    .sort((left, right) => left.time - right.time)[0]

  if (!upcoming) return '暂无可排序的续费日期'
  return `${formatDate(upcoming.subscription.renew_at)} · ${formatMoney(upcoming.subscription.monthly_price, upcoming.subscription.currency)}/月`
}

export function SubscriptionsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const createRequested = searchParams.get('create') === '1'
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateSubscriptionFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [editingSubscriptionId, setEditingSubscriptionId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<CreateSubscriptionFormState>(INITIAL_CREATE_FORM)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)
  const createPanelOpen = createOpen || createRequested
  const effectiveCreateForm =
    createRequested && filters.vps_id && createForm.vpsID === ''
      ? { ...createForm, vpsID: filters.vps_id }
      : createForm

  useEffect(() => {
    let cancelled = false
    const apiFilters = filtersToAPI({
      vps_id: filters.vps_id,
      status: filters.status,
      renew_window: filters.renew_window,
    })

    Promise.all([listSubscriptions(apiFilters), listVPSAssets()])
      .then(([subscriptions, vps]) => {
        if (cancelled) return
        setState({ loading: false, error: null, subscriptions, vps })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          error: describeError(error, '加载订阅失败'),
          subscriptions: [],
          vps: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [filters.renew_window, filters.status, filters.vps_id])

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    const next = { ...filters, [key]: value }
    setSearchParams(filtersToParams(next), { replace: true })
  }

  function clearFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function clearCreateRequest() {
    if (!createRequested) return
    const params = filtersToParams(filters)
    setSearchParams(params, { replace: true })
  }

  function openCreatePanel() {
    setCreateOpen(true)
    setCreateForm({ ...INITIAL_CREATE_FORM, vpsID: filters.vps_id ?? '' })
    setCreateError(null)
    setEditingSubscriptionId(null)
    setEditForm(INITIAL_CREATE_FORM)
    setEditError(null)
  }

  function closeCreatePanel() {
    setCreateOpen(false)
    setCreateForm(INITIAL_CREATE_FORM)
    setCreateError(null)
    clearCreateRequest()
  }

  function handleCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let input: CreateSubscriptionInput
    try {
      input = buildCreateInput(effectiveCreateForm)
    } catch (error: unknown) {
      setCreateError(describeError(error, '订阅输入无效'))
      return
    }

    setCreateSubmitting(true)
    createSubscription(input)
      .then((subscription) => {
        setState((current) => ({
          loading: false,
          error: null,
          subscriptions: [
            subscription,
            ...current.subscriptions.filter((item) => item.subscription_id !== subscription.subscription_id),
          ],
          vps: current.vps,
        }))
        closeCreatePanel()
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建订阅失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(subscription: SubscriptionRecord) {
    setCreateOpen(false)
    setCreateError(null)
    clearCreateRequest()
    setEditingSubscriptionId(subscription.subscription_id)
    setEditForm(subscriptionToForm(subscription))
    setEditError(null)
  }

  function cancelEdit() {
    setEditingSubscriptionId(null)
    setEditForm(INITIAL_CREATE_FORM)
    setEditError(null)
  }

  function handleEditSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!editingSubscriptionId) return
    setEditError(null)

    let input: UpdateSubscriptionInput
    try {
      input = buildUpdateInput(editForm)
    } catch (error: unknown) {
      setEditError(describeError(error, '订阅输入无效'))
      return
    }

    setEditSubmitting(true)
    updateSubscription(editingSubscriptionId, input)
      .then((subscription) => {
        setState((current) => ({
          loading: false,
          error: null,
          subscriptions: current.subscriptions.map((item) =>
            item.subscription_id === subscription.subscription_id ? subscription : item,
          ),
          vps: current.vps,
        }))
        cancelEdit()
      })
      .catch((error: unknown) => {
        setEditError(describeError(error, '更新订阅失败'))
      })
      .finally(() => setEditSubmitting(false))
  }

  function vpsName(vpsID: string | null): string {
    if (!vpsID) return ''
    return state.vps.find((item) => item.vps_id === vpsID)?.display_name ?? vpsID
  }

  const selectedContextVPS = filters.vps_id ? state.vps.find((item) => item.vps_id === filters.vps_id) : null

  const columns: DataTableColumn<SubscriptionRecord>[] = [
    {
      key: 'subscription',
      label: '订阅',
      render: (subscription) => (
        <div className="asset-table__identity">
          <strong>{vpsName(subscription.vps_id) || subscription.vps_id}</strong>
          <span>{subscription.subscription_id}</span>
        </div>
      ),
    },
    {
      key: 'price',
      label: '金额 / 月付折算',
      render: (subscription) => (
        <div className="asset-table__stack">
          <strong>{formatMoney(subscription.price, subscription.currency)}</strong>
          <span>月付 {formatMoney(subscription.monthly_price, subscription.currency)}</span>
        </div>
      ),
    },
    {
      key: 'billing',
      label: '周期',
      render: (subscription) => (
        <div className="asset-table__stack">
          <strong>{subscription.billing_months} 个月</strong>
          <span>{formatOptional(subscription.billing_cycle)}</span>
        </div>
      ),
    },
    {
      key: 'renew',
      label: '续费',
      render: (subscription) => (
        <div className="asset-table__stack">
          <strong>{formatDate(subscription.renew_at)}</strong>
          <span>{subscription.auto_renew ? '自动续费' : '手动续费'} · {subscription.auto_renew_cancelled ? '已取消自动续费' : '自动续费未取消'}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (subscription) => <SubscriptionStatusBadge value={subscription.status} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (subscription) => (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`编辑 ${subscription.subscription_id}`}
          onClick={() => startEdit(subscription)}
        >
          编辑
        </Button>
      ),
    },
  ]

  const active = hasActiveFilters(filters)
  const vpsSelectOptions = vpsOptions(state.vps)
  const filteredCount = state.subscriptions.length
  const activeCount = state.subscriptions.filter((subscription) => subscription.status === 'active').length
  const manualRenewCount = state.subscriptions.filter(
    (subscription) => subscription.status === 'active' && !subscription.auto_renew,
  ).length
  const autoRenewCount = state.subscriptions.filter(
    (subscription) => subscription.status === 'active' && subscription.auto_renew && !subscription.auto_renew_cancelled,
  ).length
  const inactiveCount = state.subscriptions.filter(
    (subscription) => subscription.status === 'cancelled' || subscription.status === 'expired',
  ).length
  const upcomingRenewal = describeUpcomingRenewal(state.subscriptions)

  return (
    <div className="page-stack asset-page subscriptions-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">订阅</h1>
          <p className="page-panel__description">
            记录 VPS 的价格、计费周期、续费日期与自动续费状态。月付折算由后端计算，前端只展示结果。
          </p>
        </div>
        <div className="page-panel__actions">
          <Button onClick={openCreatePanel}>
            {state.subscriptions.length === 0 ? '创建第一条订阅' : '新建订阅'}
          </Button>
        </div>
      </section>

      {filters.vps_id ? (
        <section className="page-panel page-panel--inline">
          <div>
            <div className="page-panel__eyebrow">CONTEXT</div>
            <h2 className="page-panel__title">当前 VPS 上下文</h2>
            <p className="page-panel__description">
              正在查看 {selectedContextVPS ? `${selectedContextVPS.display_name}（${selectedContextVPS.vps_id}）` : filters.vps_id} 的订阅记录；创建表单会默认带入该 VPS。
            </p>
          </div>
          <div className="page-panel__actions">
            <Link className="btn btn--ghost btn--md" to={`/vps/${filters.vps_id}`}>返回 VPS 详情</Link>
            <Button onClick={openCreatePanel}>
              为该 VPS 新建订阅
            </Button>
          </div>
        </section>
      ) : null}

      {!state.loading && !state.error && state.vps.length === 0 ? (
        <section className="page-panel page-panel--inline">
          <div>
            <div className="page-panel__eyebrow">PREREQUISITE</div>
            <h2 className="page-panel__title">需要先录入 VPS</h2>
            <p className="page-panel__description">订阅必须绑定到一台 VPS。当前没有 VPS 资产，请先建立资产记录再补订阅。</p>
          </div>
          <div className="page-panel__actions">
            <Link className="btn btn--primary btn--md" to="/vps">去创建 VPS</Link>
          </div>
        </section>
      ) : null}


      <section className="page-panel subscriptions-command-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">RENEWAL / COST EVIDENCE</p>
            <h2>续费与成本证据</h2>
            <p className="section-heading__description">
              当前摘要只基于已应用筛选后的订阅记录，帮助判断下一笔续费、自动续费责任和失效订阅噪音。
            </p>
          </div>
        </div>
        <dl className="asset-workbench-summary">
          <div className="asset-workbench-summary__item">
            <dt>当前筛选</dt>
            <dd>{state.loading ? '正在读取' : state.error ? '不可用' : <><MonoDigits>{filteredCount}</MonoDigits> 条订阅</>}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>最近续费</dt>
            <dd>{state.loading || state.error ? '—' : upcomingRenewal}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>生效 / 续费方式</dt>
            <dd>{state.loading || state.error ? '—' : <>生效 <MonoDigits>{activeCount}</MonoDigits> · 手动 <MonoDigits>{manualRenewCount}</MonoDigits> · 自动 <MonoDigits>{autoRenewCount}</MonoDigits></>}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>取消 / 过期</dt>
            <dd>{state.loading || state.error ? '—' : <><MonoDigits>{inactiveCount}</MonoDigits> 条保留作历史证据</>}</dd>
          </div>
        </dl>
      </section>

      <section className="page-panel">
        <FilterBar
          hasActiveFilters={active}
          onClearAll={clearFilters}
          activeChips={
            <>
              {filters.vps_id && <FilterChip label={`VPS: ${vpsName(filters.vps_id)}`} onRemove={() => setFilter('vps_id', null)} />}
              {filters.status && <FilterChip label={`状态: ${subscriptionStatusLabel(filters.status)}`} onRemove={() => setFilter('status', null)} />}
              {filters.renew_window && <FilterChip label={`续费窗口: 未来 ${filters.renew_window} 天`} onRemove={() => setFilter('renew_window', null)} />}
            </>
          }
        >
          <FilterSelect label="VPS" value={filters.vps_id} options={vpsSelectOptions} onChange={(value) => setFilter('vps_id', value)} />
          <FilterSelect label="状态" value={filters.status} options={STATUS_OPTIONS} onChange={(value) => setFilter('status', value as SubscriptionStatus | null)} />
          <FilterSelect label="续费窗口" value={filters.renew_window} options={RENEW_WINDOW_OPTIONS} onChange={(value) => setFilter('renew_window', value)} />
        </FilterBar>
      </section>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">SUBSCRIPTIONS</p>
            <h2>订阅列表</h2>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{filteredCount}</MonoDigits> 条订阅
          </span>
        </div>

        {state.loading ? (
          <PageStateView
            kind="loading"
            title="正在加载订阅…"
            surface="empty"
            compact
          />
        ) : state.error ? (
          <PageStateView
            kind="error"
            title="订阅列表不可用"
            description={state.error}
            technicalSummary={state.error}
            surface="empty"
            compact
          />
        ) : (
          <DataTable
            className="asset-table subscriptions-table"
            columns={columns}
            rows={state.subscriptions}
            rowKey={(subscription) => subscription.subscription_id}
            emptyContent={
              <span className="empty-inline">
                {filters.vps_id ? '当前 VPS 暂无订阅，可用上方按钮创建。' : '暂无订阅；请先选择 VPS 或创建订阅记录。'}
              </span>
            }
          />
        )}
      </section>

      <Drawer
        open={createPanelOpen}
        onClose={closeCreatePanel}
        title="订阅创建"
        ariaLabel="订阅创建表单"
      >
        <div className="asset-create-drawer subscriptions-drawer">
          <p className="page-panel__description">
            创建订阅只提交原始价格、周期和续费状态；月付折算继续以后端返回值为准。
          </p>
          <form className="asset-create-form" onSubmit={handleCreateSubmit}>
            <fieldset className="asset-create-form__group">
              <legend>绑定与价格</legend>
              <div className="input-field">
                <label className="input-field__label" htmlFor="subscription-create-vps">订阅 VPS</label>
                <select id="subscription-create-vps" className="input" value={effectiveCreateForm.vpsID} disabled={state.vps.length === 0} onChange={(event) => setCreateForm({ ...createForm, vpsID: event.target.value })}>
                  <option value="">选择 VPS</option>
                  {vpsSelectOptions.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
                {state.vps.length === 0 ? (
                  <span className="input-field__hint">还没有 VPS 可选。<Link className="text-link" to="/vps">去创建 VPS</Link></span>
                ) : filters.vps_id ? (
                  <span className="input-field__hint">已从 URL 上下文预填当前 VPS，可切换为其他 VPS。</span>
                ) : null}
              </div>
              <Input label="价格" type="number" min="0" step="0.01" value={createForm.price} onChange={(event) => setCreateForm({ ...createForm, price: event.target.value })} />
              <Input label="币种" value={createForm.currency} onChange={(event) => setCreateForm({ ...createForm, currency: event.target.value })} />
              <Input label="计费周期" value={createForm.billingCycle} onChange={(event) => setCreateForm({ ...createForm, billingCycle: event.target.value })} />
              <Input label="计费月数" type="number" min="1" value={createForm.billingMonths} onChange={(event) => setCreateForm({ ...createForm, billingMonths: event.target.value })} />
            </fieldset>
            <fieldset className="asset-create-form__group">
              <legend>续费状态</legend>
              <Input label="开始日期" type="date" value={createForm.startedAt} onChange={(event) => setCreateForm({ ...createForm, startedAt: event.target.value })} />
              <Input label="续费日期" type="date" value={createForm.renewAt} onChange={(event) => setCreateForm({ ...createForm, renewAt: event.target.value })} />
              <label className="input-field">
                <span className="input-field__label">订阅状态</span>
                <select className="input" value={createForm.status} onChange={(event) => setCreateForm({ ...createForm, status: event.target.value as SubscriptionStatus })}>
                  {STATUS_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <Input label="支付方式" value={createForm.paymentMethod} onChange={(event) => setCreateForm({ ...createForm, paymentMethod: event.target.value })} />
              <label className="asset-checkbox">
                <input type="checkbox" checked={createForm.autoRenew} onChange={(event) => setCreateForm({ ...createForm, autoRenew: event.target.checked })} />
                <span>自动续费</span>
              </label>
              <label className="asset-checkbox">
                <input type="checkbox" checked={createForm.autoRenewCancelled} onChange={(event) => setCreateForm({ ...createForm, autoRenewCancelled: event.target.checked })} />
                <span>已取消自动续费</span>
              </label>
            </fieldset>
            <fieldset className="asset-create-form__group asset-create-form__group--wide">
              <legend>备注</legend>
              <Input name="note" label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            </fieldset>
            {createError && <p className="create-form__error" role="alert">{createError}</p>}
            <div className="page-form-actions asset-create-form__actions">
              <span className="asset-create-form__hint">关闭抽屉会移除 create=1 并丢弃未提交草稿。</span>
              <Button type="button" variant="secondary" onClick={closeCreatePanel} disabled={createSubmitting}>
                取消
              </Button>
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建订阅'}
              </Button>
            </div>
          </form>
        </div>
      </Drawer>

      <Drawer
        open={editingSubscriptionId != null}
        onClose={cancelEdit}
        title="订阅编辑"
        ariaLabel="订阅编辑表单"
      >
        <div className="asset-create-drawer subscriptions-drawer">
          <p className="page-panel__description">
            编辑保持原始订阅字段语义不变；保存后列表展示后端重新计算的月付折算。
          </p>
          <form className="asset-create-form" onSubmit={handleEditSubmit}>
            <fieldset className="asset-create-form__group">
              <legend>绑定与价格</legend>
              <div className="input-field">
                <label className="input-field__label" htmlFor="subscription-edit-vps">订阅 VPS</label>
                <select id="subscription-edit-vps" className="input" value={editForm.vpsID} onChange={(event) => setEditForm({ ...editForm, vpsID: event.target.value })}>
                  <option value="">选择 VPS</option>
                  {vpsSelectOptions.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </div>
              <Input label="价格" type="number" min="0" step="0.01" value={editForm.price} onChange={(event) => setEditForm({ ...editForm, price: event.target.value })} />
              <Input label="币种" value={editForm.currency} onChange={(event) => setEditForm({ ...editForm, currency: event.target.value })} />
              <Input label="计费周期" value={editForm.billingCycle} onChange={(event) => setEditForm({ ...editForm, billingCycle: event.target.value })} />
              <Input label="计费月数" type="number" min="1" value={editForm.billingMonths} onChange={(event) => setEditForm({ ...editForm, billingMonths: event.target.value })} />
            </fieldset>
            <fieldset className="asset-create-form__group">
              <legend>续费状态</legend>
              <Input label="开始日期" type="date" value={editForm.startedAt} onChange={(event) => setEditForm({ ...editForm, startedAt: event.target.value })} />
              <Input label="续费日期" type="date" value={editForm.renewAt} onChange={(event) => setEditForm({ ...editForm, renewAt: event.target.value })} />
              <label className="input-field">
                <span className="input-field__label">订阅状态</span>
                <select className="input" value={editForm.status} onChange={(event) => setEditForm({ ...editForm, status: event.target.value as SubscriptionStatus })}>
                  {STATUS_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <Input label="支付方式" value={editForm.paymentMethod} onChange={(event) => setEditForm({ ...editForm, paymentMethod: event.target.value })} />
              <label className="asset-checkbox">
                <input type="checkbox" checked={editForm.autoRenew} onChange={(event) => setEditForm({ ...editForm, autoRenew: event.target.checked })} />
                <span>自动续费</span>
              </label>
              <label className="asset-checkbox">
                <input type="checkbox" checked={editForm.autoRenewCancelled} onChange={(event) => setEditForm({ ...editForm, autoRenewCancelled: event.target.checked })} />
                <span>已取消自动续费</span>
              </label>
            </fieldset>
            <fieldset className="asset-create-form__group asset-create-form__group--wide">
              <legend>备注</legend>
              <Input name="note" label="备注" value={editForm.note} onChange={(event) => setEditForm({ ...editForm, note: event.target.value })} />
            </fieldset>
            {editError && <p className="create-form__error" role="alert">{editError}</p>}
            <div className="page-form-actions asset-create-form__actions">
              <span className="asset-create-form__hint">取消会恢复为当前已保存的订阅资料。</span>
              <Button type="button" variant="secondary" onClick={cancelEdit} disabled={editSubmitting}>
                取消编辑
              </Button>
              <Button type="submit" disabled={editSubmitting}>
                {editSubmitting ? '保存中…' : '保存订阅'}
              </Button>
            </div>
          </form>
        </div>
      </Drawer>
    </div>
  )
}
