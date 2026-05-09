import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Button, DataTable, Input, MonoDigits, type DataTableColumn } from '../components/atoms'
import { FilterBar, FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { ApiError, createSubscription, listSubscriptions, listVPSAssets } from '../lib/api'
import { formatDate, formatMoney, formatOptional } from '../lib/format'
import {
  SUBSCRIPTION_STATUS_LABELS,
  type CreateSubscriptionInput,
  type SubscriptionListFilter,
  type SubscriptionRecord,
  type SubscriptionStatus,
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

function vpsOptions(vps: VPSAssetRecord[]): FilterSelectOption[] {
  return vps.map((item) => ({
    value: item.vps_id,
    label: item.display_name,
  }))
}

export function SubscriptionsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateSubscriptionFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    Promise.all([listSubscriptions(filtersToAPI(filters)), listVPSAssets()])
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
  }, [filters])

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    const next = { ...filters, [key]: value }
    setSearchParams(filtersToParams(next), { replace: true })
  }

  function clearFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function toggleCreatePanel() {
    setCreateOpen((open) => {
      const next = !open
      if (!next) {
        setCreateForm(INITIAL_CREATE_FORM)
        setCreateError(null)
      }
      return next
    })
  }

  function handleCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let input: CreateSubscriptionInput
    try {
      input = buildCreateInput(createForm)
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
        setCreateForm(INITIAL_CREATE_FORM)
        setCreateOpen(false)
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建订阅失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  function vpsName(vpsID: string | null): string {
    if (!vpsID) return ''
    return state.vps.find((item) => item.vps_id === vpsID)?.display_name ?? vpsID
  }

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
  ]

  const active = hasActiveFilters(filters)
  const vpsSelectOptions = vpsOptions(state.vps)

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
          <Button variant={createOpen ? 'secondary' : 'primary'} onClick={toggleCreatePanel}>
            {createOpen ? '收起创建' : state.subscriptions.length === 0 ? '创建第一条订阅' : '新建订阅'}
          </Button>
        </div>
      </section>

      {createOpen && (
        <section className="page-panel">
          <div className="page-panel__eyebrow">CREATE</div>
          <h2 className="page-panel__title">订阅创建</h2>
          <form onSubmit={handleCreateSubmit}>
            <label className="input-field">
              <span className="input-field__label">订阅 VPS</span>
              <select className="input" value={createForm.vpsID} onChange={(event) => setCreateForm({ ...createForm, vpsID: event.target.value })}>
                <option value="">选择 VPS</option>
                {vpsSelectOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <Input label="价格" type="number" min="0" step="0.01" value={createForm.price} onChange={(event) => setCreateForm({ ...createForm, price: event.target.value })} />
            <Input label="币种" value={createForm.currency} onChange={(event) => setCreateForm({ ...createForm, currency: event.target.value })} />
            <Input label="计费周期" value={createForm.billingCycle} onChange={(event) => setCreateForm({ ...createForm, billingCycle: event.target.value })} />
            <Input label="计费月数" type="number" min="1" value={createForm.billingMonths} onChange={(event) => setCreateForm({ ...createForm, billingMonths: event.target.value })} />
            <Input label="开始日期" type="date" value={createForm.startedAt} onChange={(event) => setCreateForm({ ...createForm, startedAt: event.target.value })} />
            <Input label="续费日期" type="date" value={createForm.renewAt} onChange={(event) => setCreateForm({ ...createForm, renewAt: event.target.value })} />
            <label className="input-field">
              <span className="input-field__label">状态</span>
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
            <Input label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            {createError && <p className="create-form__error" role="alert">{createError}</p>}
            <div className="page-form-actions">
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建订阅'}
              </Button>
            </div>
          </form>
        </section>
      )}

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
            <MonoDigits>{state.subscriptions.length}</MonoDigits> 条订阅
          </span>
        </div>

        {state.loading ? (
          <div className="empty-state">正在加载订阅…</div>
        ) : state.error ? (
          <div className="empty-state">{state.error}</div>
        ) : (
          <DataTable
            className="asset-table subscriptions-table"
            columns={columns}
            rows={state.subscriptions}
            rowKey={(subscription) => subscription.subscription_id}
            emptyContent={<span className="empty-inline">暂无订阅</span>}
          />
        )}
      </section>

      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">RELATED</div>
          <h2 className="page-panel__title">VPS 资产上下文</h2>
          <p className="page-panel__description">需要确认资产本身、关联 Node 或续费决策时，回到 VPS 列表继续处理。</p>
        </div>
        <div className="page-panel__actions">
          <Link className="btn btn--secondary btn--md" to="/vps">查看 VPS</Link>
        </div>
      </section>
    </div>
  )
}
