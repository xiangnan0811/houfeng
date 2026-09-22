import { type FormEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Button, Modal, Input, MonoDigits, Select, StatusGlyph, TabPanel, Tabs, tabPanelId } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { FilterBar, FilterChip, FilterSearchSelect, FilterSelect } from '../components/filters'
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
  subscriptionsLoading: boolean
  subscriptionsError: string | null
  subscriptions: SubscriptionRecord[]
  subscriptionsFilterKey: string | null
  vpsLoading: boolean
  vpsError: string | null
  vps: VPSAssetRecord[]
  overviewLoading: boolean
  overviewError: string | null
  overview: SubscriptionOverview | null
  statisticsLoading: boolean
  statisticsError: string | null
  statistics: SubscriptionStatistics | null
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
  subscriptionsLoading: true,
  subscriptionsError: null,
  subscriptions: [],
  subscriptionsFilterKey: null,
  vpsLoading: true,
  vpsError: null,
  vps: [],
  overviewLoading: true,
  overviewError: null,
  overview: null,
  statisticsLoading: true,
  statisticsError: null,
  statistics: null,
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
  if (err instanceof ApiError) {
    if (err.status === 409 && err.code === 'idempotency_key_reused') {
      return '同一幂等键已用于不同的订阅内容，请重新填写后再创建'
    }
    return err.message
  }
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

type PageView = 'details' | 'insights'

const VIEW_TABS = [
  { value: 'details' as const, label: '明细' },
  { value: 'insights' as const, label: '成本洞察' },
]

const RENEW_WINDOW_OPTIONS = [
  { value: '30', label: '未来 30 天' },
  { value: '60', label: '未来 60 天' },
  { value: '90', label: '未来 90 天' },
]

const FILTER_QUERY_KEYS = ['vps_id', 'provider_id', 'renew_within_days', 'currency', 'label'] as const

function parseView(sp: URLSearchParams): PageView {
  return sp.get('view') === 'details' ? 'details' : 'insights'
}


function patchSearchParams(current: URLSearchParams, patch: Record<string, string | null | undefined>): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const [key, value] of Object.entries(patch)) {
    if (value == null || value === '') next.delete(key)
    else next.set(key, value)
  }
  return next
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
  onChange: (form: FormState) => void
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
  onChange,
  onSubmit,
}: SubscriptionFormProps) {
  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    onChange({ ...form, [key]: value })
  }
  const vpsFieldDisabled = Boolean(vpsDisabled) || submitting
  const showCustomCurrency = form.currency === CUSTOM_OPTION_VALUE
  const showCustomPayment = form.paymentMethod === CUSTOM_OPTION_VALUE

  return (
    <form id={id} className="subscription-form" onSubmit={onSubmit} aria-busy={submitting}>
      {vpsLink ? (
        <p className="subscription-form__owner">
          <Link className="text-link" to={vpsLink}>打开关联 VPS</Link>
        </p>
      ) : null}

      <div className="subscription-form__grid">
        <div className="subscription-form__span-2">
          <Select
            label="VPS"
            value={form.vpsID}
            disabled={vpsFieldDisabled}
            onChange={(event) => update('vpsID', event.target.value)}
            required
          >
            <option value="">选择 VPS</option>
            {vpsOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </Select>
        </div>
        <div className="subscription-form__span-2">
          <Input
            label="展示名"
            value={form.displayName}
            disabled={submitting}
            onChange={(event) => update('displayName', event.target.value)}
            placeholder="例如：Tokyo Edge 年付"
          />
        </div>
        <Input
          label="价格"
          type="number"
          min="0"
          step="0.01"
          value={form.price}
          disabled={submitting}
          onChange={(event) => update('price', event.target.value)}
          required
        />
        <Select
          label="币种"
          value={form.currency}
          disabled={submitting}
          onChange={(event) => update('currency', event.target.value)}
          required
        >
          {COMMON_CURRENCY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义币种</option>
        </Select>
        <Select
          label="计费周期单位"
          value={form.billingPeriodUnit}
          disabled={submitting}
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
          disabled={submitting}
          onChange={(event) => update('billingPeriodLength', event.target.value)}
          required
        />
        {showCustomCurrency ? (
          <div className="subscription-form__span-2">
            <Input
              label="自定义币种"
              value={form.customCurrency}
              disabled={submitting}
              onChange={(event) => update('customCurrency', event.target.value)}
              placeholder="例如：JPY"
              required
            />
          </div>
        ) : null}
        <Input
          label="开始日期"
          type="date"
          value={form.startedAt}
          disabled={submitting}
          onChange={(event) => update('startedAt', event.target.value)}
        />
        <Input
          label="续费日期"
          type="date"
          value={form.renewAt}
          disabled={submitting}
          onChange={(event) => update('renewAt', event.target.value)}
        />
        <Input
          label="试用结束"
          type="date"
          value={form.trialEndsAt}
          disabled={submitting}
          onChange={(event) => update('trialEndsAt', event.target.value)}
        />
        <Input
          label="固定期结束"
          type="date"
          value={form.endsAt}
          disabled={submitting}
          onChange={(event) => update('endsAt', event.target.value)}
        />
        <div className="subscription-form__span-2">
          <Select
            label="支付方式"
            value={form.paymentMethod}
            disabled={submitting}
            onChange={(event) => update('paymentMethod', event.target.value)}
          >
            <option value="">未记录</option>
            {COMMON_PAYMENT_METHOD_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>{displayOption(option)}</option>
            ))}
            <option value={CUSTOM_OPTION_VALUE}>自定义支付方式</option>
          </Select>
        </div>
        {showCustomPayment ? (
          <div className="subscription-form__span-2">
            <Input
              label="自定义支付方式"
              value={form.customPaymentMethod}
              disabled={submitting}
              onChange={(event) => update('customPaymentMethod', event.target.value)}
            />
          </div>
        ) : null}
        <Input
          label="分类"
          value={form.costCategory}
          disabled={submitting}
          onChange={(event) => update('costCategory', event.target.value)}
          placeholder="compute / backup"
        />
        <div className={showCustomPayment ? 'subscription-form__labels-wide' : undefined}>
          <Input
            label="标签"
            value={form.labels}
            disabled={submitting}
            onChange={(event) => update('labels', event.target.value)}
            placeholder="逗号分隔"
          />
        </div>
        <fieldset className="subscription-form__renewal">
          <legend>续费方式</legend>
          <div className="subscription-form__renewal-options" role="radiogroup" aria-label="续费方式">
            {RENEWAL_MODE_OPTIONS.map((option) => (
              <label key={option.value}>
                <input
                  type="radio"
                  name={`${id}-renewal-mode`}
                  value={option.value}
                  aria-label={option.label}
                  checked={form.renewalMode === option.value}
                  disabled={submitting}
                  onChange={() => update('renewalMode', option.value)}
                />
                {option.label}
              </label>
            ))}
          </div>
        </fieldset>
        <div className="subscription-form__note">
          <Input label="备注" value={form.note} disabled={submitting} onChange={(event) => update('note', event.target.value)} />
        </div>
      </div>
      {error && <p className="create-form__error" role="alert">{error}</p>}
    </form>
  )
}

function modalFormFooter({
  formId,
  submitting,
  submitLabel,
  onCancel,
}: {
  formId: string
  submitting: boolean
  submitLabel: string
  onCancel: () => void
}) {
  const submittingLabel = submitLabel.includes('创建') ? '创建中…' : '保存中…'
  return (
    <>
      <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
      <Button type="submit" form={formId} disabled={submitting}>
        {submitting ? submittingLabel : submitLabel}
      </Button>
    </>
  )
}

export function SubscriptionsPage() {
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const view = parseView(searchParams)
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const createRequested = searchParams.get('create') === '1'
  const [state, setState] = useState<PageState>(INITIAL_PAGE)
  const [subscriptionsReloadKey, setSubscriptionsReloadKey] = useState(0)
  const [vpsReloadKey, setVpsReloadKey] = useState(0)
  const [overviewReloadKey, setOverviewReloadKey] = useState(0)
  const [statisticsReloadKey, setStatisticsReloadKey] = useState(0)
  const [statsLatched, setStatsLatched] = useState(() => view === 'insights')
  if (view === 'insights' && !statsLatched) setStatsLatched(true)
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
  const [now] = useState(Date.now)
  const createIdempotencyKeyRef = useRef(crypto.randomUUID())
  const viewRef = useRef(view)
  const skipDetailsScrollRestore = useRef(false)
  const pendingDetailsFocus = useRef(false)
  const scrollMemory = useRef({ details: { main: 0, list: 0 }, insights: { main: 0 } })
  const panelOpen = createOpen || createRequested
  const effectiveForm = createRequested && filters.vps_id && createForm.vpsID === ''
    ? { ...createForm, vpsID: filters.vps_id } : createForm
  const currentFilterKey = useMemo(
    () => JSON.stringify(filtersToAPI(filters)),
    [filters],
  )

  useEffect(() => {
    let cancelled = false
    const requestKey = currentFilterKey
    const requestFilters = JSON.parse(currentFilterKey) as SubscriptionListFilter
    listSubscriptions(requestFilters)
      .then((subs) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          subscriptionsLoading: false,
          subscriptionsError: null,
          subscriptions: subs,
          subscriptionsFilterKey: requestKey,
        }))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          subscriptionsLoading: false,
          subscriptionsError: describeError(err, '加载订阅列表失败'),
          subscriptions: current.subscriptionsFilterKey === requestKey ? current.subscriptions : [],
          subscriptionsFilterKey: requestKey,
        }))
      })
    return () => { cancelled = true }
  }, [currentFilterKey, subscriptionsReloadKey])

  useEffect(() => {
    let cancelled = false
    listVPSAssets()
      .then((vps) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: null,
          vps,
        }))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: describeError(err, '加载 VPS 列表失败'),
        }))
      })
    return () => { cancelled = true }
  }, [vpsReloadKey])

  useEffect(() => {
    let cancelled = false
    getSubscriptionOverview()
      .then((overview) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          overviewLoading: false,
          overviewError: null,
          overview,
        }))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          overviewLoading: false,
          overviewError: describeError(err, '加载成本概览失败'),
        }))
      })
    return () => { cancelled = true }
  }, [overviewReloadKey])
  useEffect(() => {
    if (!statsLatched) return
    let cancelled = false
    getSubscriptionStatistics('year')
      .then((statistics) => {
        if (cancelled) return
        setState((current) => ({ ...current, statistics, statisticsLoading: false, statisticsError: null }))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          statisticsLoading: false,
          statisticsError: describeError(err, '加载年度统计失败'),
        }))
      })
    return () => { cancelled = true }
  }, [statsLatched, statisticsReloadKey])

  useEffect(() => {
    function onScroll(event: Event) {
      const target = event.target
      if (!(target instanceof HTMLElement)) return
      if (target.id === 'main-content') {
        const detailsHidden = document.getElementById(tabPanelId('subscription-view', 'details'))?.closest('[hidden]')
        const insightsHidden = document.getElementById(tabPanelId('subscription-view', 'insights'))?.closest('[hidden]')
        if (!detailsHidden) scrollMemory.current.details.main = target.scrollTop
        else if (!insightsHidden) scrollMemory.current.insights.main = target.scrollTop
        return
      }
      if (target.classList.contains('subscription-list-scroll')) {
        if (target.closest('[hidden]')) return
        scrollMemory.current.details.list = target.scrollLeft
      }
    }
    document.addEventListener('scroll', onScroll, { capture: true, passive: true })
    return () => document.removeEventListener('scroll', onScroll, { capture: true })
  }, [])
  useLayoutEffect(() => {
    const previous = viewRef.current
    if (previous === view) return
    viewRef.current = view
    const main = document.getElementById('main-content')
    const list = document.querySelector('.subscription-list-scroll')
    if (skipDetailsScrollRestore.current && view === 'details') {
      skipDetailsScrollRestore.current = false
      scrollMemory.current.details = { main: 0, list: 0 }
      if (main) main.scrollTop = 0
      if (list instanceof HTMLElement) list.scrollLeft = 0
      if (pendingDetailsFocus.current) {
        pendingDetailsFocus.current = false
        document.getElementById(tabPanelId('subscription-view', 'details'))?.focus()
      }
      return
    }
    const memory = view === 'details' ? scrollMemory.current.details : scrollMemory.current.insights
    if (main) main.scrollTop = memory.main
    if (view === 'details' && list instanceof HTMLElement) list.scrollLeft = scrollMemory.current.details.list
    if (pendingDetailsFocus.current && view === 'details') {
      pendingDetailsFocus.current = false
      document.getElementById(tabPanelId('subscription-view', 'details'))?.focus()
    }
  }, [view])

  useEffect(() => {
    createIdempotencyKeyRef.current = crypto.randomUUID()
  }, [createForm])

  function navigateQuery(next: URLSearchParams, replace: boolean) {
    setSearchParams(next, { replace, state: location.state })
  }
  function setFilter<K extends keyof FilterState>(key: K, val: FilterState[K]) {
    const queryKey = key === 'renew_window' ? 'renew_within_days' : key
    navigateQuery(patchSearchParams(searchParams, { [queryKey]: val }), true)
  }
  function clearFilters() {
    const next = new URLSearchParams(searchParams)
    for (const key of FILTER_QUERY_KEYS) next.delete(key)
    navigateQuery(next, true)
  }
  function clearCreateReq() {
    if (!createRequested) return
    navigateQuery(patchSearchParams(searchParams, { create: null }), true)
  }
  function setView(nextView: PageView) {
    if (nextView === view) return
    if (nextView === 'insights') setStatsLatched(true)
    navigateQuery(patchSearchParams(searchParams, { view: nextView === 'details' ? 'details' : null }), false)
  }
  function handleInsightsSelectVPS(vpsID: string) {
    skipDetailsScrollRestore.current = true
    pendingDetailsFocus.current = true
    scrollMemory.current.details = { main: 0, list: 0 }
    navigateQuery(patchSearchParams(searchParams, { vps_id: vpsID, view: 'details' }), false)
  }

  function retrySubscriptions() {
    setState((current) => ({ ...current, subscriptionsLoading: true, subscriptionsError: null }))
    setSubscriptionsReloadKey((key) => key + 1)
  }
  function retryVPS() {
    setState((current) => ({ ...current, vpsLoading: true, vpsError: null }))
    setVpsReloadKey((key) => key + 1)
  }
  function retryOverview() {
    setState((current) => ({ ...current, overviewLoading: true, overviewError: null }))
    setOverviewReloadKey((key) => key + 1)
  }
  function retryStatistics() {
    setState((current) => ({ ...current, statisticsLoading: true, statisticsError: null }))
    setStatisticsReloadKey((key) => key + 1)
  }
  function reloadWorkbench() {
    retrySubscriptions()
    retryOverview()
    retryStatistics()
  }

  function openCreate() {
    setCreateOpen(true); setCreateForm({ ...INITIAL_FORM, vpsID: filters.vps_id ?? '' })
    setCreateError(null); setEditingId(null); setEditError(null)
  }
  function resetCreate() {
    setCreateOpen(false); setCreateForm(INITIAL_FORM); setCreateError(null); clearCreateReq()
  }
  function closeCreate() {
    if (createSubmitting) return
    resetCreate()
  }

  function handleCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); setCreateError(null)
    let input: CreateSubscriptionInput
    try { input = buildCreateInput(effectiveForm) } catch (err: unknown) { setCreateError(describeError(err, '输入无效')); return }
    setCreateSubmitting(true)
    createSubscription(input, createIdempotencyKeyRef.current)
      .then(() => { resetCreate(); reloadWorkbench() })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 409 && err.code === 'idempotency_key_reused') {
          createIdempotencyKeyRef.current = crypto.randomUUID()
        }
        setCreateError(describeError(err, '创建失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(s: SubscriptionRecord) { closeCreate(); setEditingId(s.subscription_id); setEditForm(subToForm(s)); setEditError(null) }
  function resetEdit() {
    setEditingId(null); setEditForm(INITIAL_FORM); setEditError(null)
  }
  function cancelEdit() {
    if (editSubmitting) return
    resetEdit()
  }

  function handleEdit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); if (!editingId) return; setEditError(null)
    let input: CreateSubscriptionInput
    try { input = buildCreateInput(editForm) } catch (err: unknown) { setEditError(describeError(err, '输入无效')); return }
    setEditSubmitting(true)
    updateSubscription(editingId, input)
      .then(() => { resetEdit(); reloadWorkbench() })
      .catch((err: unknown) => setEditError(describeError(err, '更新失败')))
      .finally(() => setEditSubmitting(false))
  }

  function vpsName(id: string | null): string {
    if (!id) return ''; return state.vps.find((v) => v.vps_id === id)?.display_name ?? id
  }
  function providerName(id: string | null): string {
    if (!id) return ''; return state.vps.find((v) => v.provider_id === id)?.provider_name ?? id
  }

  const listCurrent = state.subscriptionsFilterKey === currentFilterKey
  const visibleSubscriptions = listCurrent ? state.subscriptions : []
  const listError = listCurrent ? state.subscriptionsError : null
  const overviewReady = !state.overviewLoading && state.overviewError == null && state.overview != null
  const vpsCatalogEmpty = !state.vpsLoading && state.vpsError == null && state.vps.length === 0

  const vpsOpts = state.vps.map((v) => ({ value: v.vps_id, label: v.display_name }))
  const vpsFilterOptions = state.vps.map((asset) => ({
    value: asset.vps_id,
    label: asset.display_name,
    hint: [asset.provider_name, asset.city].filter(Boolean).join(' · '),
    keywords: [asset.vps_id, asset.provider_name, asset.city, asset.ipv4, asset.ipv6].filter(Boolean).join(' '),
  }))
  const selectedVpsID = panelOpen ? effectiveForm.vpsID : editForm.vpsID
  if (selectedVpsID && !vpsOpts.some((option) => option.value === selectedVpsID)) {
    vpsOpts.unshift({
      value: selectedVpsID,
      label: state.vps.find((v) => v.vps_id === selectedVpsID)?.display_name ?? selectedVpsID,
    })
  }

  const hasFilters = Boolean(filters.vps_id || filters.provider_id || filters.renew_window || filters.currency || filters.label)
  const baseCurrency = state.overview?.base_currency ?? state.statistics?.base_currency ?? 'CNY'
  const availableCurrencies = Array.from(new Set([
    ...visibleSubscriptions.map((sub) => sub.currency),
    ...(filters.currency ? [filters.currency] : []),
  ])).sort()
  const filterChips = [
    filters.provider_id ? { key: 'provider', label: `服务商: ${providerName(filters.provider_id)}`, clear: () => setFilter('provider_id', null) } : null,
    filters.vps_id ? { key: 'vps', label: `VPS: ${vpsName(filters.vps_id)}`, clear: () => setFilter('vps_id', null) } : null,
    filters.renew_window ? { key: 'renew', label: `续费: 未来 ${filters.renew_window} 天`, clear: () => setFilter('renew_window', null) } : null,
    filters.currency ? { key: 'currency', label: `币种: ${filters.currency}`, clear: () => setFilter('currency', null) } : null,
    filters.label ? { key: 'label', label: `标签: ${filters.label}`, clear: () => setFilter('label', null) } : null,
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

  const overview = state.overview
  const renewal30 = overviewReady ? overview?.renewal_due_30d_count ?? 0 : null
  const budgetRisk = overviewReady ? overview?.budget_risk_count ?? 0 : null
  const missingSubs = overviewReady ? overview?.missing_subscription_vps_count ?? 0 : null

  return (
    <div className="page subscription-page" data-view={view}>
      <header className="page__head subscription-page__head">
        <div className="subscription-page__identity">
          <h1 className="page__title">订阅</h1>
          <div className="subscription-summary" aria-label="订阅摘要">
            <button type="button" onClick={() => clearFilters()}>
              <span className="subscription-summary__label">月均成本</span>
              <span className="subscription-summary__value"><MonoDigits>{overviewReady ? moneyBase(overview?.total_monthly_cost, baseCurrency) : '—'}</MonoDigits></span>
            </button>
            <button
              type="button"
              onClick={() => {
                if (view !== 'details') {
                  navigateQuery(patchSearchParams(searchParams, { renew_within_days: '30', view: 'details' }), false)
                } else {
                  setFilter('renew_window', '30')
                }
              }}
            >
              <span className="subscription-summary__label">30 天续费</span>
              <span className="subscription-summary__value"><MonoDigits>{renewal30 == null ? '—' : renewal30}</MonoDigits></span>
            </button>
            <Link to="/settings?tab=subscriptions">
              <span className="subscription-summary__label">预算风险</span>
              <span className="subscription-summary__value"><MonoDigits>{budgetRisk == null ? '—' : budgetRisk}</MonoDigits></span>
            </Link>
            <span>
              <span className="subscription-summary__label">缺订阅</span>
              <span className="subscription-summary__value"><MonoDigits>{missingSubs == null ? '—' : missingSubs}</MonoDigits></span>
            </span>
          </div>
        </div>
        <div className="page__actions">
          <button type="button" className="btn sm secondary" onClick={handleRefreshRates} disabled={refreshingRates}>
            {refreshingRates ? '刷新中…' : '刷新汇率'}
          </button>
          <Link className="btn sm secondary" to="/settings?tab=subscriptions">订阅配置</Link>
          <button type="button" className="btn sm primary" onClick={openCreate}>新建订阅</button>
        </div>
      </header>

      {rateNotice ? <p className="asset-operation-feedback" role="status">{rateNotice}</p> : null}

      {state.vpsError && !state.vpsLoading ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          VPS 列表不可用：{state.vpsError}
          {' '}
          <button type="button" className="btn sm secondary" onClick={retryVPS}>重试 VPS</button>
        </p>
      ) : null}

      {state.overviewError && !state.overviewLoading ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          成本概览不可用：{state.overviewError}
          {' '}
          <button type="button" className="btn sm secondary" onClick={retryOverview}>重试概览</button>
        </p>
      ) : null}

      <div className="subscription-page__views">
        <Tabs
          label="订阅视图"
          idBase="subscription-view"
          activation="manual"
          value={view}
          onChange={setView}
          items={VIEW_TABS}
        />
      </div>

      <div className="subscription-page__view" hidden={view !== 'details'}>
        <TabPanel idBase="subscription-view" value="details" className="subscription-view-panel">
          <FilterBar
            hasActiveFilters={hasFilters}
            onClearAll={clearFilters}
            activeChips={filterChips.map((chip) => (
              <FilterChip key={chip.key} label={chip.label} onRemove={chip.clear} />
            ))}
          >
            <FilterSearchSelect
              label="VPS"
              value={filters.vps_id}
              options={vpsFilterOptions}
              onChange={(value) => setFilter('vps_id', value)}
              emptyLabel="暂无 VPS"
              noMatchLabel="没有匹配的 VPS"
            />
            <FilterSelect
              label="续费窗口"
              value={filters.renew_window}
              options={RENEW_WINDOW_OPTIONS}
              onChange={(value) => setFilter('renew_window', value)}
            />
            <FilterSelect
              label="币种"
              value={filters.currency}
              options={availableCurrencies.map((currency) => ({ value: currency, label: currency }))}
              onChange={(value) => setFilter('currency', value)}
            />
            <label className="filter-select">
              <span className="filter-select__label">标签</span>
              <input
                className="filter-select__control filter-select__control--text"
                aria-label="标签"
                value={filters.label ?? ''}
                onChange={(event) => setFilter('label', event.target.value || null)}
              />
            </label>
          </FilterBar>
          <section className="subscription-list">
            <div className="subscription-list-toolbar">
              <h2 id="subscription-table-title" className="subscription-list-toolbar__title">订阅明细</h2>
              <span className="subscription-list-toolbar__count">
                {listCurrent ? `${visibleSubscriptions.length} 条` : '加载中'}
              </span>
            </div>
            {!listCurrent || (state.subscriptionsLoading && visibleSubscriptions.length === 0) ? (
              <PageStateView kind="loading" title="正在加载订阅列表…" surface="empty" compact />
            ) : listError && visibleSubscriptions.length === 0 ? (
              <PageStateView
                kind="error"
                title="订阅列表不可用"
                description={listError}
                action={<button type="button" className="btn sm secondary" onClick={retrySubscriptions}>重试</button>}
                surface="empty"
                compact
              />
            ) : visibleSubscriptions.length === 0 ? (
              <PageStateView
                kind="empty"
                title={filters.vps_id ? '当前 VPS 尚无订阅' : '尚未记录订阅'}
                description={filters.vps_id ? '可为当前 VPS 创建订阅记录' : '创建订阅记录以跟踪续费周期'}
                action={vpsCatalogEmpty
                  ? <Link className="btn sm primary" to="/vps">先创建 VPS</Link>
                  : state.vpsError
                    ? <button type="button" className="btn sm secondary" onClick={retryVPS}>重试 VPS</button>
                    : state.vpsLoading
                      ? undefined
                      : <button type="button" className="btn sm primary" onClick={openCreate}>创建订阅</button>}
                surface="empty"
                compact
              />
            ) : (
              <>
                {listError ? (
                  <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                    {listError}
                    {' '}
                    <button type="button" className="btn sm secondary" onClick={retrySubscriptions}>重试</button>
                  </p>
                ) : null}
                <div
                  className="subscription-list-scroll"
                  role="region"
                  aria-labelledby="subscription-table-title"
                  tabIndex={0}
                >
                  <table className="data-table data-table--compact asset-table">
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
                      {visibleSubscriptions.map((s) => {
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
                </div>
              </>
            )}
          </section>
        </TabPanel>
      </div>

      <div className="subscription-page__view" hidden={view !== 'insights'}>
        <TabPanel idBase="subscription-view" value="insights" className="subscription-view-panel">
          {statsLatched ? (
            <SubscriptionInsights
              overview={overviewReady ? overview : null}
              overviewLoading={state.overviewLoading}
              overviewError={state.overviewError}
              statistics={state.statistics}
              statisticsLoading={state.statisticsLoading}
              statisticsError={state.statisticsError}
              onRetryStatistics={retryStatistics}
              baseCurrency={baseCurrency}
              breakdownKind={breakdownKind}
              onBreakdownKindChange={setBreakdownKind}
              onSelectVPS={handleInsightsSelectVPS}
            />
          ) : null}
        </TabPanel>
      </div>


      <Modal
        open={panelOpen}
        onClose={closeCreate}
        title="新建订阅"
        ariaLabel="新建订阅表单"
        contentClassName="subscription-dialog"
        size="lg"
        persistent={createSubmitting}
        footer={vpsCatalogEmpty ? undefined : modalFormFooter({
          formId: 'subscription-create-form',
          submitting: createSubmitting,
          submitLabel: '创建订阅',
          onCancel: closeCreate,
        })}
      >
        {vpsCatalogEmpty ? (
          <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
            无可选 VPS，<Link className="text-link" to="/vps">先去创建 VPS</Link>
          </p>
        ) : (
          <>
            {state.vpsLoading ? <p role="status">正在加载 VPS…</p> : null}
            {state.vpsError ? (
              <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
                VPS 列表不可用：{state.vpsError}
                {' '}
                <button type="button" className="btn sm secondary" onClick={retryVPS}>重试 VPS</button>
              </p>
            ) : null}
            <SubscriptionForm
              id="subscription-create-form"
              form={effectiveForm}
              vpsOptions={vpsOpts}
              vpsDisabled={state.vpsLoading || vpsOpts.length === 0}
              error={createError}
              submitting={createSubmitting}
              onChange={setCreateForm}
              onSubmit={handleCreate}
            />
          </>
        )}
      </Modal>

      <Modal
        open={editingId != null}
        onClose={cancelEdit}
        title="编辑订阅"
        ariaLabel="编辑订阅表单"
        contentClassName="subscription-dialog"
        size="lg"
        persistent={editSubmitting}
        footer={modalFormFooter({
          formId: 'subscription-edit-form',
          submitting: editSubmitting,
          submitLabel: '保存订阅',
          onCancel: cancelEdit,
        })}
      >
        <SubscriptionForm
          id="subscription-edit-form"
          form={editForm}
          vpsOptions={vpsOpts}
          vpsLink={editForm.vpsID ? `/vps/${editForm.vpsID}` : null}
          error={editError}
          submitting={editSubmitting}
          onChange={setEditForm}
          onSubmit={handleEdit}
        />
      </Modal>
    </div>
  )
}
