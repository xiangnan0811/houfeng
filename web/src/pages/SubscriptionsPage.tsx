import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Modal, Input, Select } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
  createSubscription,
  createSubscriptionBudget,
  getSubscriptionCostSettings,
  getSubscriptionOverview,
  getSubscriptionStatistics,
  listSubscriptionBudgets,
  listSubscriptions,
  listVPSAssets,
  refreshSubscriptionExchangeRates,
  updateSubscription,
  updateSubscriptionCostSettings,
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
  type CreateSubscriptionBudgetInput,
  type CreateSubscriptionInput,
  type RenewalMode,
  type SubscriptionBudgetStatus,
  type SubscriptionBudgetRecord,
  type SubscriptionCostSettings,
  type SubscriptionListFilter,
  type SubscriptionOverview,
  type SubscriptionRecord,
  type SubscriptionStatistics,
  type VPSAssetRecord,
} from '../lib/types'

type PageState = {
  loading: boolean
  error: string | null
  subscriptions: SubscriptionRecord[]
  vps: VPSAssetRecord[]
  overview: SubscriptionOverview | null
  statistics: SubscriptionStatistics | null
  budgets: SubscriptionBudgetRecord[]
  settings: SubscriptionCostSettings | null
}
type FilterState = {
  vps_id: string | null
  renew_window: string | null
  currency: string | null
  budget_status: string | null
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
type SettingsDraft = {
  baseCurrency: string
  provider: string
  fixerApiKey: string
  reminderOffsets: string
  maxLeadDays: string
  staleHours: string
}
type BudgetDraft = {
  scopeType: string
  scopeID: string
  name: string
  monthlyLimit: string
  yearlyLimit: string
  warningPct: string
  enabled: boolean
  note: string
}

const INITIAL_PAGE: PageState = {
  loading: true,
  error: null,
  subscriptions: [],
  vps: [],
  overview: null,
  statistics: null,
  budgets: [],
  settings: null,
}
const INITIAL_FORM: FormState = {
  vpsID: '', price: '', currency: 'USD', customCurrency: '',
  billingPeriodUnit: 'month', billingPeriodLength: '1',
  startedAt: '', renewAt: '', renewalMode: 'manual',
  displayName: '', costCategory: '', labels: '',
  trialEndsAt: '', endsAt: '',
  paymentMethod: '', customPaymentMethod: '', note: '',
}
const INITIAL_SETTINGS_DRAFT: SettingsDraft = {
  baseCurrency: 'CNY',
  provider: 'frankfurter',
  fixerApiKey: '',
  reminderOffsets: '14,7,1',
  maxLeadDays: '30',
  staleHours: '36',
}
const INITIAL_BUDGET_DRAFT: BudgetDraft = {
  scopeType: 'global',
  scopeID: '',
  name: '',
  monthlyLimit: '',
  yearlyLimit: '',
  warningPct: '80',
  enabled: true,
  note: '',
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
    renew_window: rw && ['30', '60', '90'].includes(rw) ? rw : null,
    currency: sp.get('currency') || null,
    budget_status: sp.get('budget_status') || null,
    label: sp.get('label') || null,
  }
}
function filtersToParams(f: FilterState): URLSearchParams {
  const p = new URLSearchParams()
  if (f.vps_id) p.set('vps_id', f.vps_id)
  if (f.renew_window) p.set('renew_within_days', f.renew_window)
  if (f.currency) p.set('currency', f.currency)
  if (f.budget_status) p.set('budget_status', f.budget_status)
  if (f.label) p.set('label', f.label)
  return p
}

function filtersToAPI(f: FilterState): SubscriptionListFilter {
  return {
    vps_id: f.vps_id,
    renew_within_days: f.renew_window ? Number.parseInt(f.renew_window, 10) : null,
    currency: f.currency,
    budget_status: parseBudgetStatus(f.budget_status),
    label: f.label,
    sort: f.renew_window ? 'renew_at' : '', order: f.renew_window ? 'asc' : '',
  }
}

function parseCSV(value: string): string[] {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function parseIntegerList(value: string): number[] {
  return value.split(',').map((item) => Number.parseInt(item.trim(), 10)).filter((num) => Number.isInteger(num) && num >= 0)
}

function parseBudgetStatus(value: string | null): SubscriptionBudgetStatus | null {
  if (value === 'disabled' || value === 'ok' || value === 'warning' || value === 'over' || value === 'unknown') return value
  return null
}

function moneyBase(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '—'
  return formatMoney(value, currency)
}

function budgetStatusLabel(status?: string | null): string {
  const map: Record<string, string> = {
    disabled: '已停用',
    ok: '预算内',
    warning: '接近上限',
    over: '已超预算',
    unknown: '未匹配',
  }
  return map[status ?? ''] ?? (status || '—')
}

function budgetTone(status?: string | null): string {
  if (status === 'over') return 'critical'
  if (status === 'warning') return 'notice'
  if (status === 'ok') return 'normal'
  return 'maintenance'
}

function settingsToDraft(settings: SubscriptionCostSettings | null): SettingsDraft {
  if (!settings) return INITIAL_SETTINGS_DRAFT
  return {
    baseCurrency: settings.base_currency,
    provider: settings.exchange_rate_provider,
    fixerApiKey: '',
    reminderOffsets: settings.default_reminder_offsets_days.join(','),
    maxLeadDays: String(settings.max_reminder_lead_days),
    staleHours: String(settings.exchange_rate_stale_after_hours),
  }
}

function budgetBadgeClass(status?: string | null): string {
  if (status === 'over') return 'badge badge-err'
  if (status === 'warning') return 'badge badge-warn'
  if (status === 'ok') return 'badge badge-ok'
  return 'badge badge-muted'
}

function barWidth(value: number, max: number): string {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0) return '0%'
  return `${Math.max(4, Math.min(100, (value / max) * 100))}%`
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
  const [settingsDraft, setSettingsDraft] = useState<SettingsDraft>(INITIAL_SETTINGS_DRAFT)
  const [settingsSubmitting, setSettingsSubmitting] = useState(false)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [settingsNotice, setSettingsNotice] = useState<string | null>(null)
  const [budgetDraft, setBudgetDraft] = useState<BudgetDraft>(INITIAL_BUDGET_DRAFT)
  const [budgetSubmitting, setBudgetSubmitting] = useState(false)
  const [budgetError, setBudgetError] = useState<string | null>(null)
  const [refreshingRates, setRefreshingRates] = useState(false)
  const [rateNotice, setRateNotice] = useState<string | null>(null)
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
      getSubscriptionStatistics('month'),
      listSubscriptionBudgets(),
      getSubscriptionCostSettings(),
    ])
      .then(([subs, vps, overview, statistics, budgets, settings]) => {
        if (!cancelled) {
          setState({ loading: false, error: null, subscriptions: subs, vps, overview, statistics, budgets, settings })
          setSettingsDraft(settingsToDraft(settings))
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            ...INITIAL_PAGE,
            loading: false,
            error: describeError(err, '加载订阅工作台失败'),
          })
        }
      })
    return () => { cancelled = true }
  }, [apiFilters, reloadKey])

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

  const vpsOpts = state.vps.map((v) => ({ value: v.vps_id, label: v.display_name }))
  const hasFilters = Boolean(filters.vps_id || filters.renew_window || filters.currency || filters.budget_status || filters.label)
  const [now] = useState(Date.now)
  const baseCurrency = state.overview?.base_currency ?? state.settings?.base_currency ?? 'CNY'
  const availableCurrencies = Array.from(new Set(state.subscriptions.map((sub) => sub.currency))).sort()

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

  function handleSettingsSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSettingsError(null)
    setSettingsNotice(null)
    const maxLeadDays = Number.parseInt(settingsDraft.maxLeadDays, 10)
    const staleHours = Number.parseInt(settingsDraft.staleHours, 10)
    const offsets = parseIntegerList(settingsDraft.reminderOffsets)
    if (!/^[A-Za-z]{3}$/.test(settingsDraft.baseCurrency.trim())) {
      setSettingsError('基准货币必须是 3 位代码。')
      return
    }
    if (!Number.isInteger(maxLeadDays) || maxLeadDays <= 0) {
      setSettingsError('最远提前提醒天数必须大于 0。')
      return
    }
    if (!Number.isInteger(staleHours) || staleHours <= 0) {
      setSettingsError('汇率过期小时必须大于 0。')
      return
    }
    if (offsets.length === 0 || offsets.some((offset) => offset > maxLeadDays)) {
      setSettingsError('提醒窗口不能为空，且不能超过最远提前天数。')
      return
    }
    setSettingsSubmitting(true)
    updateSubscriptionCostSettings({
      base_currency: settingsDraft.baseCurrency.trim().toUpperCase(),
      exchange_rate_provider: settingsDraft.provider,
      fixer_api_key: settingsDraft.fixerApiKey.trim() || undefined,
      default_reminder_offsets_days: offsets,
      max_reminder_lead_days: maxLeadDays,
      exchange_rate_stale_after_hours: staleHours,
    })
      .then((settings) => {
        setSettingsNotice('订阅成本设置已保存')
        setSettingsDraft(settingsToDraft(settings))
        reloadWorkbench()
      })
      .catch((err: unknown) => setSettingsError(describeError(err, '保存订阅设置失败')))
      .finally(() => setSettingsSubmitting(false))
  }

  function handleBudgetSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBudgetError(null)
    const monthlyLimit = budgetDraft.monthlyLimit.trim() ? Number.parseFloat(budgetDraft.monthlyLimit) : null
    const yearlyLimit = budgetDraft.yearlyLimit.trim() ? Number.parseFloat(budgetDraft.yearlyLimit) : null
    const warningPct = Number.parseInt(budgetDraft.warningPct, 10)
    if (!budgetDraft.name.trim()) {
      setBudgetError('预算名称不能为空。')
      return
    }
    if (budgetDraft.scopeType !== 'global' && !budgetDraft.scopeID.trim()) {
      setBudgetError('非全局预算需要填写 scope ID。')
      return
    }
    if (monthlyLimit == null && yearlyLimit == null) {
      setBudgetError('月预算或年预算至少填写一个。')
      return
    }
    if ((monthlyLimit != null && (!Number.isFinite(monthlyLimit) || monthlyLimit < 0)) || (yearlyLimit != null && (!Number.isFinite(yearlyLimit) || yearlyLimit < 0))) {
      setBudgetError('预算金额必须为非负数字。')
      return
    }
    if (!Number.isInteger(warningPct) || warningPct < 1 || warningPct > 100) {
      setBudgetError('预警比例必须在 1-100 之间。')
      return
    }
    const input: CreateSubscriptionBudgetInput = {
      scope_type: budgetDraft.scopeType,
      scope_id: budgetDraft.scopeType === 'global' ? '' : budgetDraft.scopeID.trim(),
      name: budgetDraft.name.trim(),
      base_currency: baseCurrency,
      monthly_limit: monthlyLimit,
      yearly_limit: yearlyLimit,
      warning_pct: warningPct,
      enabled: budgetDraft.enabled,
      note: budgetDraft.note.trim(),
    }
    setBudgetSubmitting(true)
    createSubscriptionBudget(input)
      .then(() => {
        setBudgetDraft(INITIAL_BUDGET_DRAFT)
        reloadWorkbench()
      })
      .catch((err: unknown) => setBudgetError(describeError(err, '创建预算失败')))
      .finally(() => setBudgetSubmitting(false))
  }

  return (
    <div className="page-stack">
      <div className="watchtower-header">
        <div className="watchtower-header__row1">
          <div className="watchtower-header__title-block">
            <h1>订阅成本中枢</h1>
            <div className="badge-row">
              <span className="badge badge--state tone--normal"><span className="badge__dot" />{baseCurrency} 基准</span>
              <span className="badge badge--state tone--maintenance"><span className="badge__dot" />{state.settings?.exchange_rate_provider ?? 'frankfurter'}</span>
            </div>
          </div>
          <div className="watchtower-header__actions">
            <button className="btn md secondary" onClick={handleRefreshRates} disabled={refreshingRates}>
              {refreshingRates ? '刷新中…' : '刷新汇率'}
            </button>
            <button className="btn md primary" onClick={openCreate}>
              <svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12" /></svg>
              新建订阅
            </button>
          </div>
        </div>
        <div className="watchtower-header__row2">
          <span className="watchtower-header__meta-item">续费提醒 {state.settings?.default_reminder_offsets_days.join('/') ?? '14/7/1'} 天</span>
          <span className="watchtower-header__meta-sep">·</span>
          <span className="watchtower-header__meta-item">汇率过期阈值 {state.settings?.exchange_rate_stale_after_hours ?? 36} 小时</span>
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
          <div className="asset-decision-focus subscription-cost-focus animate-in">
            <button className="asset-decision-focus__item asset-decision-focus__item--normal" onClick={() => clearFilters()}>
              <span>月均成本</span>
              <strong>{moneyBase(state.overview?.total_monthly_cost, baseCurrency)}</strong>
              <small>年化 {moneyBase(state.overview?.total_yearly_cost, baseCurrency)}</small>
            </button>
            <button className="asset-decision-focus__item asset-decision-focus__item--notice" onClick={() => setFilter('renew_window', '30')}>
              <span>未来 30 天续费</span>
              <strong>{state.overview?.renewal_due_30d_count ?? 0}</strong>
              <small>14 天内 {state.overview?.renewal_due_14d_count ?? 0}</small>
            </button>
            <button className="asset-decision-focus__item asset-decision-focus__item--alert" onClick={() => setFilter('budget_status', 'warning')}>
              <span>预算风险</span>
              <strong>{state.overview?.budget_risk_count ?? 0}</strong>
              <small>超额或接近预算</small>
            </button>
            <div className="asset-decision-focus__item asset-decision-focus__item--critical">
              <span>缺订阅资产</span>
              <strong>{state.overview?.missing_subscription_vps_count ?? 0}</strong>
              <small>决策关注 {state.overview?.decision_attention_count ?? 0}</small>
            </div>
          </div>

          <div className="filter-panel animate-in">
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
                    <span className="filter-select__label">预算状态</span>
                    <select className="filter-select__control" value={filters.budget_status ?? ''} onChange={(e) => setFilter('budget_status', e.target.value || null)}>
                      <option value="">全部</option>
                      <option value="ok">预算内</option>
                      <option value="warning">接近上限</option>
                      <option value="over">已超预算</option>
                      <option value="unknown">未匹配</option>
                    </select>
                  </div>
                </div>
                {hasFilters ? <button className="filter-bar__clear" onClick={clearFilters}>清除</button> : null}
              </div>
            </div>
          </div>

          <div className="subscription-workbench-grid">
            <section className="page-panel subscription-workbench-panel">
              <div className="section-heading section-heading--inline">
                <div>
                  <p className="section-heading__eyebrow">Renewal Queue</p>
                  <h2 className="section-heading__title">续费队列</h2>
                </div>
                <Link className="btn sm secondary" to="/asset-decisions">资产决策</Link>
              </div>
              <div className="subscription-renewal-queue">
                {(state.overview?.upcoming_renewals ?? []).length === 0 ? (
                  <p className="asset-table-empty-state"><strong>暂无临近续费</strong><span>未来 90 天没有需要处理的订阅续费。</span></p>
                ) : state.overview?.upcoming_renewals.map((item) => (
                  <Link key={item.subscription_id} className={`subscription-renewal-row ${item.exchange_rate_stale ? 'subscription-renewal-row--stale' : ''}`} to={`/vps/${item.vps_id}`}>
                    <span>
                      <strong>{item.display_name || item.vps_display_name}</strong>
                      <small>{item.provider_name || '未记录服务商'} · {item.currency}</small>
                    </span>
                    <span className="mono">{formatDate(item.renew_at)}</span>
                    <span className="mono">{moneyBase(item.monthly_price_base, item.base_currency)}/月</span>
                  </Link>
                ))}
              </div>
            </section>

            <section className="page-panel subscription-workbench-panel">
              <div className="section-heading">
                <p className="section-heading__eyebrow">Breakdown</p>
                <h2 className="section-heading__title">成本拆分</h2>
              </div>
              <div className="subscription-breakdown-list">
                {(state.statistics?.provider_breakdown ?? []).slice(0, 6).map((item) => (
                  <div key={item.key} className="subscription-breakdown-row">
                    <div>
                      <strong>{item.label}</strong>
                      <small>{item.subscription_count} 项订阅</small>
                    </div>
                    <div className="subscription-breakdown-bar">
                      <span style={{ width: barWidth(item.monthly_cost, state.statistics?.total_monthly_cost ?? 0) }} />
                    </div>
                    <span className="mono">{moneyBase(item.monthly_cost, baseCurrency)}</span>
                  </div>
                ))}
              </div>
            </section>
          </div>

          <section className="page-panel page-panel--scroll-x">
            <div className="section-heading section-heading--inline">
              <div>
                <p className="section-heading__eyebrow">Subscriptions</p>
                <h2 className="section-heading__title">订阅明细</h2>
              </div>
              <span className="section-heading__meta">{state.subscriptions.length} 条</span>
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
                    <th>周期</th>
                    <th>原币种</th>
                    <th>{baseCurrency} 成本</th>
                    <th>续费</th>
                    <th>预算/汇率</th>
                    <th className="data-table__th--right">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {state.subscriptions.map((s) => {
                    const isUrgent = s.renew_at && (new Date(s.renew_at).getTime() - now) < 30 * 86400000
                    return (
                      <tr className="data-table__row" key={s.subscription_id}>
                        <td className="data-table__cell">
                          <div className="asset-table__identity">
                            <strong>{s.display_name || vpsName(s.vps_id)}</strong>
                            <small>{s.cost_category || '未分类'} · {(s.labels ?? []).join(' / ') || '无标签'}</small>
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
                        <td className={`data-table__cell mono${isUrgent ? ' text-warn' : ''}`}>{formatDate(s.renew_at)}</td>
                        <td className="data-table__cell">
                          <span className={budgetBadgeClass(s.budget_status)}>
                            <span className="badge-dot" />{budgetStatusLabel(s.budget_status)}
                          </span>
                          <span className="asset-context-inline">
                            {s.exchange_rate_stale ? <span className="asset-context-pill asset-context-pill--attention">汇率过期</span> : null}
                            <span>{renewalModeLabel(s.renewal_mode ?? renewalModeFromLegacy(s))}</span>
                            <Link className="text-link" to={`/vps/${s.vps_id}`}>回到 VPS</Link>
                          </span>
                        </td>
                        <td className="data-table__cell data-table__cell--right"><button className="btn-text sm secondary" onClick={() => startEdit(s)}>编辑</button></td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </section>

          <div className="subscription-workbench-grid subscription-workbench-grid--settings">
            <section className="page-panel">
              <div className="section-heading">
                <p className="section-heading__eyebrow">Budgets</p>
                <h2 className="section-heading__title">预算</h2>
              </div>
              <div className="subscription-budget-list">
                {state.budgets.length === 0 ? <p className="asset-table-empty-state"><strong>尚未配置预算</strong><span>可以先添加全局月预算，再逐步按供应商、标签或 VPS 收敛。</span></p> : null}
                {state.budgets.map((budget) => (
                  <div key={budget.budget_id} className={`subscription-budget-row subscription-budget-row--${budgetTone(budget.status)}`}>
                    <div>
                      <strong>{budget.name}</strong>
                      <small>{budget.scope_type}{budget.scope_id ? ` · ${budget.scope_id}` : ''}</small>
                    </div>
                    <span className={budgetBadgeClass(budget.status)}><span className="badge-dot" />{budgetStatusLabel(budget.status)}</span>
                    <span className="mono">{moneyBase(budget.current_monthly_spend, budget.base_currency)} / {budget.monthly_limit != null ? moneyBase(budget.monthly_limit, budget.base_currency) : moneyBase((budget.yearly_limit ?? 0) / 12, budget.base_currency)}</span>
                  </div>
                ))}
              </div>
              <form className="subscription-inline-form" onSubmit={handleBudgetSubmit}>
                <Select label="范围" value={budgetDraft.scopeType} onChange={(event) => setBudgetDraft({ ...budgetDraft, scopeType: event.target.value })}>
                  <option value="global">全局</option>
                  <option value="provider">供应商</option>
                  <option value="label">标签</option>
                  <option value="category">分类</option>
                  <option value="vps">VPS</option>
                </Select>
                <Input label="Scope ID" value={budgetDraft.scopeID} disabled={budgetDraft.scopeType === 'global'} onChange={(event) => setBudgetDraft({ ...budgetDraft, scopeID: event.target.value })} />
                <Input label="名称" value={budgetDraft.name} onChange={(event) => setBudgetDraft({ ...budgetDraft, name: event.target.value })} />
                <Input label={`月预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={budgetDraft.monthlyLimit} onChange={(event) => setBudgetDraft({ ...budgetDraft, monthlyLimit: event.target.value })} />
                <Input label="预警比例" type="number" min="1" max="100" value={budgetDraft.warningPct} onChange={(event) => setBudgetDraft({ ...budgetDraft, warningPct: event.target.value })} />
                <label className="asset-checkbox-line"><input type="checkbox" checked={budgetDraft.enabled} onChange={(event) => setBudgetDraft({ ...budgetDraft, enabled: event.target.checked })} />启用</label>
                {budgetError ? <p className="create-form__error" role="alert">{budgetError}</p> : null}
                <button className="btn sm primary" type="submit" disabled={budgetSubmitting}>{budgetSubmitting ? '创建中…' : '添加预算'}</button>
              </form>
            </section>

            <section className="page-panel">
              <div className="section-heading">
                <p className="section-heading__eyebrow">Settings</p>
                <h2 className="section-heading__title">汇率与提醒</h2>
              </div>
              <form className="subscription-inline-form subscription-inline-form--settings" onSubmit={handleSettingsSubmit}>
                <Input label="基准货币" value={settingsDraft.baseCurrency} onChange={(event) => setSettingsDraft({ ...settingsDraft, baseCurrency: event.target.value })} />
                <Select label="汇率 Provider" value={settingsDraft.provider} onChange={(event) => setSettingsDraft({ ...settingsDraft, provider: event.target.value })}>
                  <option value="frankfurter">Frankfurter</option>
                  <option value="fixer">Fixer</option>
                </Select>
                <Input label={state.settings?.fixer_configured ? 'Fixer key 已配置' : 'Fixer key'} type="password" value={settingsDraft.fixerApiKey} onChange={(event) => setSettingsDraft({ ...settingsDraft, fixerApiKey: event.target.value })} placeholder={state.settings?.fixer_masked_summary || '留空则不修改'} />
                <Input label="提醒窗口" value={settingsDraft.reminderOffsets} onChange={(event) => setSettingsDraft({ ...settingsDraft, reminderOffsets: event.target.value })} />
                <Input label="最远提前天数" type="number" min="1" value={settingsDraft.maxLeadDays} onChange={(event) => setSettingsDraft({ ...settingsDraft, maxLeadDays: event.target.value })} />
                <Input label="汇率过期小时" type="number" min="1" value={settingsDraft.staleHours} onChange={(event) => setSettingsDraft({ ...settingsDraft, staleHours: event.target.value })} />
                {settingsError ? <p className="create-form__error" role="alert">{settingsError}</p> : null}
                {settingsNotice ? <p className="asset-operation-feedback" role="status">{settingsNotice}</p> : null}
                <button className="btn sm primary" type="submit" disabled={settingsSubmitting}>{settingsSubmitting ? '保存中…' : '保存设置'}</button>
              </form>
            </section>
          </div>
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
