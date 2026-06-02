import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Modal, Input, Select } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createSubscription, listSubscriptions, listVPSAssets, updateSubscription } from '../lib/api'
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
  type SubscriptionRecord,
  type VPSAssetRecord,
} from '../lib/types'

type PageState = { loading: boolean; error: string | null; subscriptions: SubscriptionRecord[]; vps: VPSAssetRecord[] }
type FilterState = { vps_id: string | null; renew_window: string | null }
type FormState = {
  vpsID: string; price: string; currency: string; customCurrency: string
  billingPeriodUnit: BillingPeriodUnit; billingPeriodLength: string
  startedAt: string; renewAt: string; renewalMode: RenewalMode
  paymentMethod: string; customPaymentMethod: string; note: string
}

const INITIAL_PAGE: PageState = { loading: true, error: null, subscriptions: [], vps: [] }
const INITIAL_FORM: FormState = {
  vpsID: '', price: '', currency: 'USD', customCurrency: '',
  billingPeriodUnit: 'month', billingPeriodLength: '1',
  startedAt: '', renewAt: '', renewalMode: 'manual',
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
    renew_window: rw && ['30', '60', '90'].includes(rw) ? rw : null,
  }
}
function filtersToParams(f: FilterState): URLSearchParams {
  const p = new URLSearchParams()
  if (f.vps_id) p.set('vps_id', f.vps_id)
  if (f.renew_window) p.set('renew_within_days', f.renew_window)
  return p
}

function filtersToAPI(f: FilterState): SubscriptionListFilter {
  return {
    vps_id: f.vps_id,
    renew_within_days: f.renew_window ? Number.parseInt(f.renew_window, 10) : null,
    sort: f.renew_window ? 'renew_at' : '', order: f.renew_window ? 'asc' : '',
  }
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
  const panelOpen = createOpen || createRequested
  const effectiveForm = createRequested && filters.vps_id && createForm.vpsID === ''
    ? { ...createForm, vpsID: filters.vps_id } : createForm
  const filterVPSID = filters.vps_id
  const filterRenewWindow = filters.renew_window
  const apiFilters = useMemo(
    () => filtersToAPI({ vps_id: filterVPSID, renew_window: filterRenewWindow }),
    [filterRenewWindow, filterVPSID],
  )

  useEffect(() => {
    let cancelled = false
    Promise.all([listSubscriptions(apiFilters), listVPSAssets()])
      .then(([subs, vps]) => { if (!cancelled) setState({ loading: false, error: null, subscriptions: subs, vps }) })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, error: describeError(err, '加载订阅失败'), subscriptions: [], vps: [] }) })
    return () => { cancelled = true }
  }, [apiFilters, reloadKey])

  function setFilter<K extends keyof FilterState>(key: K, val: FilterState[K]) {
    setSearchParams(filtersToParams({ ...filters, [key]: val }), { replace: true })
  }
  function clearFilters() { setSearchParams(new URLSearchParams(), { replace: true }) }
  function clearCreateReq() { if (createRequested) setSearchParams(filtersToParams(filters), { replace: true }) }

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
      .then((s) => { setState((c) => ({ ...c, loading: false, error: null, subscriptions: [s, ...c.subscriptions.filter((x) => x.subscription_id !== s.subscription_id)] })); closeCreate() })
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
      .then((s) => { setState((c) => ({ ...c, subscriptions: c.subscriptions.map((x) => x.subscription_id === s.subscription_id ? s : x) })); cancelEdit() })
      .catch((err: unknown) => setEditError(describeError(err, '更新失败')))
      .finally(() => setEditSubmitting(false))
  }

  function vpsName(id: string | null): string {
    if (!id) return ''; return state.vps.find((v) => v.vps_id === id)?.display_name ?? id
  }

  const vpsOpts = state.vps.map((v) => ({ value: v.vps_id, label: v.display_name }))
  const hasFilters = Boolean(filters.vps_id || filters.renew_window)
  const [now] = useState(Date.now)

  return (
    <div className="page-stack">
      <div className="page-header">
        <div>
          <h1 className="page-title">订阅管理</h1>
          <p className="page-sub">VPS 付费周期跟踪</p>
        </div>
        <div className="header-actions">
          <button className="btn md primary" onClick={openCreate}>
            <svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12" /></svg>
            新建订阅
          </button>
        </div>
      </div>

      {hasFilters && (
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
              </div>
              <button className="filter-bar__clear" onClick={clearFilters}>清除</button>
            </div>
          </div>
        </div>
      )}

      {state.loading ? (
        <PageStateView kind="loading" title="正在加载…" surface="empty" compact />
      ) : state.error ? (
        <PageStateView
          kind="error" title="加载失败" description={state.error}
          action={<button className="btn sm secondary" onClick={() => { setState(INITIAL_PAGE); setReloadKey((k) => k + 1) }}>重试</button>}
          surface="empty" compact
        />
      ) : state.subscriptions.length === 0 ? (
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
        <table className="table animate-in">
          <thead>
            <tr>
              <th>VPS</th>
              <th>服务商</th>
              <th>付费周期</th>
              <th>金额</th>
              <th>下次续费</th>
              <th>续费事实</th>
              <th className="cell-end">操作</th>
            </tr>
          </thead>
          <tbody>
            {state.subscriptions.map((s) => {
              const isUrgent = s.renew_at && (new Date(s.renew_at).getTime() - now) < 30 * 86400000
              return (
                <tr key={s.subscription_id}>
                  <td className="name">{vpsName(s.vps_id)}</td>
                  <td className="sub">{s.billing_cycle}</td>
                  <td>{periodLabel(s.billing_period_unit, s.billing_period_length, s.billing_months)}</td>
                  <td className="mono">{formatMoney(s.price, s.currency)}</td>
                  <td className={`time${isUrgent ? ' text-warn' : ''}`}>{formatDate(s.renew_at)}</td>
                  <td>
                    <span className="asset-context-inline">
                      <span>{renewalModeLabel(s.renewal_mode ?? renewalModeFromLegacy(s))}</span>
                      {s.payment_method ? <span>{s.payment_method}</span> : null}
                      <Link className="text-link" to={`/vps/${s.vps_id}`}>回到 VPS</Link>
                    </span>
                  </td>
                  <td className="cell-end"><button className="btn-text sm secondary" onClick={() => startEdit(s)}>编辑</button></td>
                </tr>
              )
            })}
          </tbody>
        </table>
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
