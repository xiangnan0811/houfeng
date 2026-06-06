import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Modal, Input, Select, StatusGlyph } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
  createSubscription,
  getSubscriptionOverview,
  getSubscriptionStatistics,
  listSubscriptions,
  listVPSAssets,
  refreshSubscriptionExchangeRates,
  updateSubscription,
} from '../lib/api'
import {
  BILLING_PERIOD_UNIT_OPTIONS,
  COMMON_CURRENCY_OPTIONS,
  COMMON_PAYMENT_METHOD_OPTIONS,
  CUSTOM_OPTION_VALUE,
  RENEWAL_MODE_OPTIONS,
  billingCycleFromPeriod,
  billingMonthsFromPeriod,
  displayOption,
  legacyFlagsFromRenewalMode,
  normalizeBillingPeriodUnit,
  normalizeCurrency,
  normalizePaymentMethod,
  normalizeRenewalMode,
  optionSelectValue,
  periodLabel,
  renewalModeFromLegacy,
  renewalModeLabel,
} from '../lib/assetOptions'
import { formatDate, formatMoney } from '../lib/format'
import {
  type BillingPeriodUnit,
  type CreateSubscriptionInput,
  type RenewalMode,
  type SubscriptionListFilter,
  type SubscriptionOverview,
  type SubscriptionRecord,
  type SubscriptionStatistics,
  type VPSAssetRecord,
} from '../lib/types'
import { SubscriptionInsights, type SubscriptionBreakdownKind } from './subscriptions/SubscriptionInsights'

type PageState = {
  loading: boolean
  error: string | null
  subscriptions: SubscriptionRecord[]
  vps: VPSAssetRecord[]
  overview: SubscriptionOverview | null
  statistics: SubscriptionStatistics | null
  statisticsLoading: boolean
  statisticsError: string | null
}
type FilterState = {
  vps_id: string | null
  provider_id: string | null
  renew_window: string | null
  currency: string | null
  label: string | null
}
type FormState = {
  vpsID: string; price: string; currency: string; customCurrency: string
  billingPeriodUnit: BillingPeriodUnit; billingPeriodLength: string
  startedAt: string; renewAt: string; renewalMode: RenewalMode
  displayName: string; costCategory: string; labels: string
  trialEndsAt: string; endsAt: string
  paymentMethod: string; customPaymentMethod: string; note: string
}
const INITIAL_PAGE: PageState = {
  loading: true,
  error: null,
  subscriptions: [],
  vps: [],
  overview: null,
  statistics: null,
  statisticsLoading: true,
  statisticsError: null,
}
const INITIAL_FORM: FormState = {
  vpsID: '', price: '', currency: 'USD', customCurrency: '',
  billingPeriodUnit: 'month', billingPeriodLength: '1',
  startedAt: '', renewAt: '', renewalMode: 'manual',
  displayName: '', costCategory: '', labels: '',
  trialEndsAt: '', endsAt: '',
  paymentMethod: '', customPaymentMethod: '', note: '',
}

function describeError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return fallback
}

function parseFilters(sp: URLSearchParams): FilterState {
  const rw = sp.get('renew_within_days')
  return {
    vps_id: sp.get('vps_id') || null,
    provider_id: sp.get('provider_id') || null,
    renew_window: rw && ['30', '60', '90'].includes(rw) ? rw : null,
    currency: sp.get('currency') || null,
    label: sp.get('label') || null,
  }
}
function filtersToParams(f: FilterState): URLSearchParams {
  const p = new URLSearchParams()
  if (f.vps_id) p.set('vps_id', f.vps_id)
  if (f.provider_id) p.set('provider_id', f.provider_id)
  if (f.renew_window) p.set('renew_within_days', f.renew_window)
  if (f.currency) p.set('currency', f.currency)
  if (f.label) p.set('label', f.label)
  return p
}

function filtersToAPI(f: FilterState): SubscriptionListFilter {
  return {
    vps_id: f.vps_id,
    provider_id: f.provider_id,
    renew_within_days: f.renew_window ? Number.parseInt(f.renew_window, 10) : null,
    currency: f.currency,
    budget_status: null,
    label: f.label,
    sort: f.renew_window ? 'renew_at' : '', order: f.renew_window ? 'asc' : '',
  }
}

function parseCSV(value: string): string[] {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function moneyBase(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '—'
  return formatMoney(value, currency)
}

function buildCreateInput(form: FormState): CreateSubscriptionInput {
  if (!form.vpsID.trim()) throw new Error('VPS 不能为空。')
  const price = Number.parseFloat(form.price.trim())
  if (!Number.isFinite(price) || price < 0) throw new Error('价格必须为非负数字。')
  const billingPeriodLength = Number.parseInt(form.billingPeriodLength.trim(), 10)
  if (!Number.isInteger(billingPeriodLength) || billingPeriodLength <= 0) throw new Error('计费周期长度必须大于 0。')
  const billingPeriodUnit = normalizeBillingPeriodUnit(form.billingPeriodUnit)
  const billingMonths = billingMonthsFromPeriod(billingPeriodUnit, billingPeriodLength)
  const currency = normalizeCurrency(form.currency === CUSTOM_OPTION_VALUE ? form.customCurrency : form.currency)
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error('币种必须为 3 位大写代码。')
  const renewalMode = normalizeRenewalMode(form.renewalMode)
  const legacyRenewalFlags = legacyFlagsFromRenewalMode(renewalMode)
  const paymentMethod = normalizePaymentMethod(form.paymentMethod === CUSTOM_OPTION_VALUE ? form.customPaymentMethod : form.paymentMethod)
  return {
    vps_id: form.vpsID.trim(), price, currency,
    billing_cycle: billingCycleFromPeriod(billingPeriodUnit, billingPeriodLength),
    billing_months: billingMonths,
    billing_period_unit: billingPeriodUnit,
    billing_period_length: billingPeriodLength,
    started_at: form.startedAt || null, renew_at: form.renewAt || null,
    auto_renew: legacyRenewalFlags.auto_renew,
    auto_renew_cancelled: legacyRenewalFlags.auto_renew_cancelled,
    renewal_mode: renewalMode,
    display_name: form.displayName.trim(),
    cost_category: form.costCategory.trim(),
    labels: parseCSV(form.labels),
    trial_ends_at: form.trialEndsAt || null,
    ends_at: form.endsAt || null,
    payment_method: paymentMethod, note: form.note.trim(),
  }
}

function subToForm(s: SubscriptionRecord): FormState {
  const currency = optionSelectValue(s.currency, COMMON_CURRENCY_OPTIONS)
  const paymentMethod = optionSelectValue(s.payment_method, COMMON_PAYMENT_METHOD_OPTIONS)
  return {
    vpsID: s.vps_id, price: String(s.price),
    currency, customCurrency: currency === CUSTOM_OPTION_VALUE ? s.currency : '',
    billingPeriodUnit: normalizeBillingPeriodUnit(s.billing_period_unit),
    billingPeriodLength: String(s.billing_period_length && s.billing_period_length > 0 ? s.billing_period_length : s.billing_months || 1),
    startedAt: s.started_at ?? '', renewAt: s.renew_at ?? '',
    renewalMode: renewalModeFromLegacy(s),
    displayName: s.display_name ?? '',
    costCategory: s.cost_category ?? '',
    labels: (s.labels ?? []).join(', '),
    trialEndsAt: s.trial_ends_at ?? '',
    endsAt: s.ends_at ?? '',
    paymentMethod, customPaymentMethod: paymentMethod === CUSTOM_OPTION_VALUE ? s.payment_method : '',
    note: s.note,
  }
}

type SubscriptionFormProps = {
  id: string
  form: FormState
  vpsOptions: Array<{ value: string; label: string }>
  vpsDisabled?: boolean
  vpsLink?: string | null
  error: string | null
  submitting: boolean
  submitLabel: string
  onChange: (form: FormState) => void
  onCancel: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

function SubscriptionForm({
  id,
  form,
  vpsOptions,
  vpsDisabled,
  vpsLink,
  error,
  submitting,
  submitLabel,
  onChange,
  onCancel,
  onSubmit,
}: SubscriptionFormProps) {
  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    onChange({ ...form, [key]: value })
  }

  return (
    <form id={id} className="asset-operation-form" onSubmit={onSubmit}>
      {vpsLink ? (
        <div className="asset-operation-feedback">
          <Link className="text-link" to={vpsLink}>打开关联 VPS</Link>
        </div>
      ) : null}
      <div className="asset-operation-form__grid asset-operation-form__grid--3col">
        <Select
          label="VPS"
          value={form.vpsID}
          disabled={vpsDisabled}
          onChange={(event) => update('vpsID', event.target.value)}
          required
        >
          <option value="">选择 VPS</option>
          {vpsOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </Select>
        <Input
          label="价格"
          type="number"
          min="0"
          step="0.01"
          value={form.price}
          onChange={(event) => update('price', event.target.value)}
          required
        />
        <Select label="币种" value={form.currency} onChange={(event) => update('currency', event.target.value)} required>
          {COMMON_CURRENCY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义币种</option>
        </Select>
        {form.currency === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义币种"
            value={form.customCurrency}
            onChange={(event) => update('customCurrency', event.target.value)}
            placeholder="例如：JPY"
            required
          />
        ) : null}
        <Select
          label="计费周期单位"
          value={form.billingPeriodUnit}
          onChange={(event) => update('billingPeriodUnit', event.target.value as BillingPeriodUnit)}
        >
          {BILLING_PERIOD_UNIT_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
        </Select>
        <Input
          label="计费周期长度"
          type="number"
          min="1"
          value={form.billingPeriodLength}
          onChange={(event) => update('billingPeriodLength', event.target.value)}
          required
        />
        <Input
          label="开始日期"
          type="date"
          value={form.startedAt}
          onChange={(event) => update('startedAt', event.target.value)}
        />
        <Input
          label="续费日期"
          type="date"
          value={form.renewAt}
          onChange={(event) => update('renewAt', event.target.value)}
        />
        <Select label="支付方式" value={form.paymentMethod} onChange={(event) => update('paymentMethod', event.target.value)}>
          <option value="">未记录</option>
          {COMMON_PAYMENT_METHOD_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义支付方式</option>
        </Select>
        {form.paymentMethod === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义支付方式"
            value={form.customPaymentMethod}
            onChange={(event) => update('customPaymentMethod', event.target.value)}
          />
        ) : null}
      </div>

      <div className="asset-operation-form__section">
        <div className="input-field__label">成本事实</div>
        <div className="asset-operation-form__grid asset-operation-form__grid--3col">
          <Input
            label="展示名"
            value={form.displayName}
            onChange={(event) => update('displayName', event.target.value)}
            placeholder="例如：Tokyo Edge 年付"
          />
          <Input
            label="分类"
            value={form.costCategory}
            onChange={(event) => update('costCategory', event.target.value)}
            placeholder="compute / backup"
          />
          <Input
            label="标签"
            value={form.labels}
            onChange={(event) => update('labels', event.target.value)}
            placeholder="逗号分隔"
          />
          <Input
            label="试用结束"
            type="date"
            value={form.trialEndsAt}
            onChange={(event) => update('trialEndsAt', event.target.value)}
          />
          <Input
            label="固定期结束"
            type="date"
            value={form.endsAt}
            onChange={(event) => update('endsAt', event.target.value)}
          />
        </div>
      </div>

      <div className="asset-operation-form__section">
        <div className="input-field__label">续费方式</div>
        <div className="asset-option-grid" role="radiogroup" aria-label="续费方式">
          {RENEWAL_MODE_OPTIONS.map((option) => (
            <label key={option.value} className="asset-option-radio">
              <input
                type="radio"
                name={`${id}-renewal-mode`}
                value={option.value}
                aria-label={option.label}
                checked={form.renewalMode === option.value}
                onChange={() => update('renewalMode', option.value)}
              />
              <span className="asset-option-radio__icon" aria-hidden="true">{option.icon}</span>
              <span className="asset-option-radio__label">{option.label}</span>
            </label>
          ))}
        </div>
      </div>

      <Input label="备注" value={form.note} onChange={(event) => update('note', event.target.value)} />
      {error && <p className="create-form__error" role="alert">{error}</p>}
      <div className="page-form-actions">
        <button type="button" className="btn md secondary" onClick={onCancel} disabled={submitting}>取消</button>
        <button type="submit" className="btn md primary" disabled={submitting}>{submitting ? '保存中…' : submitLabel}</button>
      </div>
    </form>
  )
}

export function SubscriptionsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const createRequested = searchParams.get('create') === '1'
  const [state, setState] = useState<PageState>(INITIAL_PAGE)
  const [reloadKey, setReloadKey] = useState(0)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<FormState>(INITIAL_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<FormState>(INITIAL_FORM)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)
  const [refreshingRates, setRefreshingRates] = useState(false)
  const [rateNotice, setRateNotice] = useState<string | null>(null)
  const [breakdownKind, setBreakdownKind] = useState<SubscriptionBreakdownKind>('provider')
  const panelOpen = createOpen || createRequested
  const effectiveForm = createRequested && filters.vps_id && createForm.vpsID === ''
    ? { ...createForm, vpsID: filters.vps_id } : createForm
  const apiFilters = useMemo(() => filtersToAPI(filters), [filters])

  useEffect(() => {
    let cancelled = false
    Promise.all([
      listSubscriptions(apiFilters),
      listVPSAssets(),
      getSubscriptionOverview(),
    ])
      .then(([subs, vps, overview]) => {
        if (!cancelled) {
          setState((current) => ({
            ...current,
            loading: false,
            error: null,
            subscriptions: subs,
            vps,
            overview,
          }))
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            ...INITIAL_PAGE,
            loading: false,
            statisticsLoading: false,
            error: describeError(err, '加载订阅工作台失败'),
          })
        }
      })
    return () => { cancelled = true }
  }, [apiFilters, reloadKey])

  useEffect(() => {
    let cancelled = false
    getSubscriptionStatistics('year')
      .then((statistics) => {
        if (!cancelled) {
          setState((current) => ({ ...current, statistics, statisticsLoading: false, statisticsError: null }))
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState((current) => ({
            ...current,
            statistics: null,
            statisticsLoading: false,
            statisticsError: describeError(err, '加载年度统计失败'),
          }))
        }
      })
    return () => { cancelled = true }
  }, [reloadKey])

  function setFilter<K extends keyof FilterState>(key: K, val: FilterState[K]) {
    setSearchParams(filtersToParams({ ...filters, [key]: val }), { replace: true })
  }
  function clearFilters() { setSearchParams(new URLSearchParams(), { replace: true }) }
  function clearCreateReq() { if (createRequested) setSearchParams(filtersToParams(filters), { replace: true }) }
  function reloadWorkbench() { setReloadKey((key) => key + 1) }

  function openCreate() {
    setCreateOpen(true); setCreateForm({ ...INITIAL_FORM, vpsID: filters.vps_id ?? '' })
    setCreateError(null); setEditingId(null); setEditError(null)
  }
  function closeCreate() { setCreateOpen(false); setCreateForm(INITIAL_FORM); setCreateError(null); clearCreateReq() }

  function handleCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); setCreateError(null)
    let input: CreateSubscriptionInput
    try { input = buildCreateInput(effectiveForm) } catch (err: unknown) { setCreateError(describeError(err, '输入无效')); return }
    setCreateSubmitting(true)
    createSubscription(input)
      .then(() => { closeCreate(); reloadWorkbench() })
      .catch((err: unknown) => setCreateError(describeError(err, '创建失败')))
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(s: SubscriptionRecord) { closeCreate(); setEditingId(s.subscription_id); setEditForm(subToForm(s)); setEditError(null) }
  function cancelEdit() { setEditingId(null); setEditForm(INITIAL_FORM); setEditError(null) }

  function handleEdit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); if (!editingId) return; setEditError(null)
    let input: CreateSubscriptionInput
    try { input = buildCreateInput(editForm) } catch (err: unknown) { setEditError(describeError(err, '输入无效')); return }
    setEditSubmitting(true)
    updateSubscription(editingId, input)
      .then(() => { cancelEdit(); reloadWorkbench() })
      .catch((err: unknown) => setEditError(describeError(err, '更新失败')))
      .finally(() => setEditSubmitting(false))
  }

  function vpsName(id: string | null): string {
    if (!id) return ''; return state.vps.find((v) => v.vps_id === id)?.display_name ?? id
  }
  function providerName(id: string | null): string {
    if (!id) return ''; return state.vps.find((v) => v.provider_id === id)?.provider_name ?? id
  }

  const vpsOpts = state.vps.map((v) => ({ value: v.vps_id, label: v.display_name }))
  const hasFilters = Boolean(filters.vps_id || filters.provider_id || filters.renew_window || filters.currency || filters.label)
  const [now] = useState(Date.now)
  const baseCurrency = state.overview?.base_currency ?? state.statistics?.base_currency ?? 'CNY'
  const availableCurrencies = Array.from(new Set(state.subscriptions.map((sub) => sub.currency))).sort()
  const filterChips = [
    filters.provider_id ? { key: 'provider', label: `服务商：${providerName(filters.provider_id)}`, clear: () => setFilter('provider_id', null) } : null,
    filters.vps_id ? { key: 'vps', label: `VPS：${vpsName(filters.vps_id)}`, clear: () => setFilter('vps_id', null) } : null,
    filters.renew_window ? { key: 'renew', label: `续费：未来 ${filters.renew_window} 天`, clear: () => setFilter('renew_window', null) } : null,
    filters.currency ? { key: 'currency', label: `币种：${filters.currency}`, clear: () => setFilter('currency', null) } : null,
    filters.label ? { key: 'label', label: `标签：${filters.label}`, clear: () => setFilter('label', null) } : null,
  ].filter((chip): chip is { key: string; label: string; clear: () => void } => chip != null)

  function handleRefreshRates() {
    setRateNotice(null)
    setRefreshingRates(true)
    refreshSubscriptionExchangeRates()
      .then((result) => {
        setRateNotice(`汇率刷新完成：成功 ${result.succeeded.length}，失败 ${result.failed.length}`)
        reloadWorkbench()
      })
      .catch((err: unknown) => setRateNotice(describeError(err, '汇率刷新失败')))
      .finally(() => setRefreshingRates(false))
  }

  return (
    <div className="page-stack">
      <div className="watchtower-header">
        <div className="watchtower-header__row1">
          <div className="watchtower-header__title-block">
            <h1>订阅成本中枢</h1>
            <div className="badge-row">
              <span className="badge badge--state tone--normal"><span className="badge__dot" />{baseCurrency} 基准</span>
              <span className="badge badge--state tone--maintenance"><span className="badge__dot" />年度统计</span>
            </div>
          </div>
          <div className="watchtower-header__actions">
            <button className="btn md secondary" onClick={handleRefreshRates} disabled={refreshingRates}>
              {refreshingRates ? '刷新中…' : '刷新汇率'}
            </button>
            <Link className="btn md secondary" to="/settings?tab=subscriptions">订阅配置</Link>
            <button className="btn md primary" onClick={openCreate}>
              <svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12" /></svg>
              新建订阅
            </button>
          </div>
        </div>
        <div className="watchtower-header__row2">
          <span className="watchtower-header__meta-item">工作台数据按列表、概览、年度统计分层加载</span>
          <span className="watchtower-header__meta-sep">·</span>
          <span className="watchtower-header__meta-item">低频配置在设置页统一管理</span>
          {rateNotice ? <><span className="watchtower-header__meta-sep">·</span><span className="watchtower-header__meta-item">{rateNotice}</span></> : null}
        </div>
      </div>

      {state.loading ? (
        <PageStateView kind="loading" title="正在加载…" surface="empty" compact />
      ) : state.error ? (
        <PageStateView
          kind="error" title="加载失败" description={state.error}
          action={<button className="btn sm secondary" onClick={() => { setState(INITIAL_PAGE); setReloadKey((k) => k + 1) }}>重试</button>}
          surface="empty" compact
        />
      ) : (
        <>
          <div className="subscription-metric-grid animate-in">
            <button className="subscription-metric-card subscription-metric-card--normal" onClick={() => clearFilters()}>
              <span><StatusGlyph state="normal" size="sm" />月均成本</span>
              <strong>{moneyBase(state.overview?.total_monthly_cost, baseCurrency)}</strong>
              <small>年化 {moneyBase(state.overview?.total_yearly_cost, baseCurrency)}</small>
            </button>
            <button className="subscription-metric-card subscription-metric-card--notice" onClick={() => setFilter('renew_window', '30')}>
              <span><StatusGlyph state={(state.overview?.renewal_due_30d_count ?? 0) > 0 ? 'notice' : 'normal'} size="sm" />未来 30 天续费</span>
              <strong>{state.overview?.renewal_due_30d_count ?? 0}</strong>
              <small>14 天内 {state.overview?.renewal_due_14d_count ?? 0}</small>
            </button>
            <Link className="subscription-metric-card subscription-metric-card--alert" to="/settings?tab=subscriptions">
              <span><StatusGlyph state={(state.overview?.budget_risk_count ?? 0) > 0 ? 'alert' : 'normal'} size="sm" />预算风险</span>
              <strong>{state.overview?.budget_risk_count ?? 0}</strong>
              <small>全局月预算</small>
            </Link>
            <div className="subscription-metric-card subscription-metric-card--critical">
              <span><StatusGlyph state={(state.overview?.missing_subscription_vps_count ?? 0) > 0 ? 'critical' : 'normal'} size="sm" />缺订阅资产</span>
              <strong>{state.overview?.missing_subscription_vps_count ?? 0}</strong>
              <small>决策关注 {state.overview?.decision_attention_count ?? 0}</small>
            </div>
          </div>

          <SubscriptionInsights
            overview={state.overview}
            statistics={state.statistics}
            statisticsLoading={state.statisticsLoading}
            statisticsError={state.statisticsError}
            baseCurrency={baseCurrency}
            breakdownKind={breakdownKind}
            onBreakdownKindChange={setBreakdownKind}
            onSelectVPS={(vpsID) => setFilter('vps_id', vpsID)}
          />

          <section className="page-panel page-panel--scroll-x">
            <div className="section-heading section-heading--inline">
              <div>
                <p className="section-heading__eyebrow">Subscriptions</p>
                <h2 className="section-heading__title">订阅明细</h2>
              </div>
              <span className="section-heading__meta">{state.subscriptions.length} 条</span>
            </div>
            <div className="subscription-list-workspace">
              <div className="filter-panel filter-panel--embedded animate-in">
                <div className="filter-bar">
                  <div className="filter-bar__controls">
                    <div className="filter-bar__controls-row">
                      <div className="filter-select">
                        <span className="filter-select__label">VPS</span>
                        <select className="filter-select__control" value={filters.vps_id ?? ''} onChange={(e) => setFilter('vps_id', e.target.value || null)}>
                          <option value="">全部</option>
                          {vpsOpts.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
                        </select>
                      </div>
                      <div className="filter-select">
                        <span className="filter-select__label">续费窗口</span>
                        <select className="filter-select__control" value={filters.renew_window ?? ''} onChange={(e) => setFilter('renew_window', e.target.value || null)}>
                          <option value="">全部</option>
                          <option value="30">未来 30 天</option>
                          <option value="60">未来 60 天</option>
                          <option value="90">未来 90 天</option>
                        </select>
                      </div>
                      <div className="filter-select">
                        <span className="filter-select__label">币种</span>
                        <select className="filter-select__control" value={filters.currency ?? ''} onChange={(e) => setFilter('currency', e.target.value || null)}>
                          <option value="">全部</option>
                          {availableCurrencies.map((currency) => <option key={currency} value={currency}>{currency}</option>)}
                        </select>
                      </div>
                      <div className="filter-select">
                        <span className="filter-select__label">标签</span>
                        <input className="filter-select__control" value={filters.label ?? ''} onChange={(e) => setFilter('label', e.target.value || null)} placeholder="标签" />
                      </div>
                    </div>
                    {hasFilters ? <button className="filter-bar__clear" onClick={clearFilters}>清除</button> : null}
                  </div>
                </div>
              </div>
              {filterChips.length > 0 ? (
                <div className="subscription-filter-chips" aria-label="当前筛选">
                  {filterChips.map((chip) => (
                    <button key={chip.key} type="button" className="subscription-filter-chip" onClick={chip.clear}>
                      {chip.label}
                      <span aria-hidden="true">×</span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            {state.subscriptions.length === 0 ? (
              <PageStateView
                kind="empty"
                title={filters.vps_id ? '当前 VPS 尚无订阅' : '尚未记录订阅'}
                description={filters.vps_id ? '可为当前 VPS 创建订阅记录' : '创建订阅记录以跟踪续费周期'}
                action={state.vps.length > 0
                  ? <button className="btn sm primary" onClick={openCreate}>创建订阅</button>
                  : <Link className="btn sm primary" to="/vps">先创建 VPS</Link>}
                surface="empty" compact
              />
            ) : (
              <table className="data-table data-table--compact asset-table animate-in">
                <thead className="data-table__head">
                  <tr>
                    <th>VPS</th>
                    <th>分类</th>
                    <th>标签</th>
                    <th>周期</th>
                    <th>原价</th>
                    <th>{baseCurrency ? `${baseCurrency} 成本` : '基准货币成本'}</th>
                    <th>续费</th>
                    <th>续费方式</th>
                    <th>资产判断</th>
                  </tr>
                </thead>
                <tbody>
                  {state.subscriptions.map((s) => {
                    const isUrgent = Boolean(s.renew_at && (new Date(s.renew_at).getTime() - now) < 30 * 86400000)
                    return (
                      <tr className="data-table__row" key={s.subscription_id}>
                        <td className="data-table__cell">
                          <div className="asset-table__identity">
                            <button type="button" className="subscription-name-button" onClick={() => startEdit(s)}>
                              {s.display_name || vpsName(s.vps_id)}
                            </button>
                            <small>{vpsName(s.vps_id)}</small>
                          </div>
                        </td>
                        <td className="data-table__cell">{s.cost_category || '未分类'}</td>
                        <td className="data-table__cell">
                          <div className="subscription-tag-list">
                            {(s.labels ?? []).length > 0 ? s.labels?.map((label) => <span key={label} className="asset-context-pill">{label}</span>) : <span className="text-muted">无标签</span>}
                          </div>
                        </td>
                        <td className="data-table__cell">{periodLabel(s.billing_period_unit, s.billing_period_length, s.billing_months)}</td>
                        <td className="data-table__cell mono">{formatMoney(s.price, s.currency)}</td>
                        <td className="data-table__cell mono">
                          <div className="asset-table__stack">
                            <strong>{moneyBase(s.monthly_price_base, s.base_currency ?? baseCurrency)}</strong>
                            <small>{moneyBase(s.yearly_price_base, s.base_currency ?? baseCurrency)}/年</small>
                          </div>
                        </td>
                        <td className={`data-table__cell mono${isUrgent ? ' text-warn' : ''}`}>
                          <span className="subscription-table-signal">
                            <StatusGlyph state={isUrgent ? 'notice' : 'normal'} size="sm" />
                            {formatDate(s.renew_at)}
                          </span>
                        </td>
                        <td className="data-table__cell">
                          <span className="asset-context-inline">
                            <span>{renewalModeLabel(s.renewal_mode ?? renewalModeFromLegacy(s))}</span>
                          </span>
                        </td>
                        <td className="data-table__cell">
                          <Link className="btn-text sm secondary" to={`/asset-decisions?view=renewal&renew_within_days=30&vps_id=${encodeURIComponent(s.vps_id)}`}>
                            需要资产判断
                          </Link>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </section>
        </>
      )}

      <Modal open={panelOpen} onClose={closeCreate} title="新建订阅" ariaLabel="新建订阅表单" size="lg">
        {state.vps.length === 0 ? (
          <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
            无可选 VPS，<Link className="text-link" to="/vps">先去创建 VPS</Link>
          </p>
        ) : (
          <SubscriptionForm
            id="subscription-create-form"
            form={effectiveForm}
            vpsOptions={vpsOpts}
            vpsDisabled={state.vps.length === 0}
            error={createError}
            submitting={createSubmitting}
            submitLabel="创建订阅"
            onChange={setCreateForm}
            onCancel={closeCreate}
            onSubmit={handleCreate}
          />
        )}
      </Modal>

      <Modal open={editingId != null} onClose={cancelEdit} title="编辑订阅" ariaLabel="编辑订阅表单" size="lg">
        <SubscriptionForm
          id="subscription-edit-form"
          form={editForm}
          vpsOptions={vpsOpts}
          vpsLink={editForm.vpsID ? `/vps/${editForm.vpsID}` : null}
          error={editError}
          submitting={editSubmitting}
          submitLabel="保存订阅"
          onChange={setEditForm}
          onCancel={cancelEdit}
          onSubmit={handleEdit}
        />
      </Modal>
    </div>
  )
}
