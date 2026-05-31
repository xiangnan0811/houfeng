import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Modal, Input, Select } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createSubscription, listSubscriptions, listVPSAssets, updateSubscription } from '../lib/api'
import { formatDate, formatMoney } from '../lib/format'
import {
  SUBSCRIPTION_STATUS_LABELS,
  type CreateSubscriptionInput,
  type SubscriptionListFilter,
  type SubscriptionRecord,
  type SubscriptionStatus,
  type VPSAssetRecord,
} from '../lib/types'

type PageState = { loading: boolean; error: string | null; subscriptions: SubscriptionRecord[]; vps: VPSAssetRecord[] }
type FilterState = { vps_id: string | null; status: SubscriptionStatus | null; renew_window: string | null }
type FormState = {
  vpsID: string; price: string; currency: string; billingCycle: string; billingMonths: string
  startedAt: string; renewAt: string; autoRenew: boolean; autoRenewCancelled: boolean
  status: SubscriptionStatus; paymentMethod: string; note: string
}

const INITIAL_PAGE: PageState = { loading: true, error: null, subscriptions: [], vps: [] }
const INITIAL_FORM: FormState = {
  vpsID: '', price: '', currency: 'USD', billingCycle: 'monthly', billingMonths: '1',
  startedAt: '', renewAt: '', autoRenew: false, autoRenewCancelled: false,
  status: 'active', paymentMethod: '', note: '',
}
const STATUS_OPTIONS = Object.entries(SUBSCRIPTION_STATUS_LABELS).map(([value, label]) => ({ value, label }))

function describeError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return fallback
}

function parseFilters(sp: URLSearchParams): FilterState {
  const status = sp.get('status') as SubscriptionStatus | null
  const rw = sp.get('renew_within_days')
  return {
    vps_id: sp.get('vps_id') || null,
    status: status && status in SUBSCRIPTION_STATUS_LABELS ? status : null,
    renew_window: rw && ['30', '60', '90'].includes(rw) ? rw : null,
  }
}
function filtersToParams(f: FilterState): URLSearchParams {
  const p = new URLSearchParams()
  if (f.vps_id) p.set('vps_id', f.vps_id)
  if (f.status) p.set('status', f.status)
  if (f.renew_window) p.set('renew_within_days', f.renew_window)
  return p
}

function filtersToAPI(f: FilterState): SubscriptionListFilter {
  return {
    vps_id: f.vps_id, status: f.status,
    renew_within_days: f.renew_window ? Number.parseInt(f.renew_window, 10) : null,
    sort: f.renew_window ? 'renew_at' : '', order: f.renew_window ? 'asc' : '',
  }
}

function buildCreateInput(form: FormState): CreateSubscriptionInput {
  if (!form.vpsID.trim()) throw new Error('VPS 不能为空。')
  const price = Number.parseFloat(form.price.trim())
  if (!Number.isFinite(price) || price < 0) throw new Error('价格必须为非负数字。')
  const billingMonths = Number.parseInt(form.billingMonths.trim(), 10)
  if (!Number.isInteger(billingMonths) || billingMonths <= 0) throw new Error('计费月数必须大于 0。')
  const currency = form.currency.trim().toUpperCase()
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error('币种必须为 3 位大写代码。')
  return {
    vps_id: form.vpsID.trim(), price, currency, billing_cycle: form.billingCycle.trim(),
    billing_months: billingMonths, started_at: form.startedAt || null, renew_at: form.renewAt || null,
    auto_renew: form.autoRenew, auto_renew_cancelled: form.autoRenewCancelled,
    status: form.status, payment_method: form.paymentMethod.trim(), note: form.note.trim(),
  }
}

function subToForm(s: SubscriptionRecord): FormState {
  return {
    vpsID: s.vps_id, price: String(s.price), currency: s.currency,
    billingCycle: s.billing_cycle, billingMonths: String(s.billing_months),
    startedAt: s.started_at ?? '', renewAt: s.renew_at ?? '',
    autoRenew: s.auto_renew, autoRenewCancelled: s.auto_renew_cancelled,
    status: s.status, paymentMethod: s.payment_method, note: s.note,
  }
}

function statusBadgeClass(status: SubscriptionStatus): string {
  switch (status) {
    case 'active': return 'badge badge-ok'
    case 'paused': return 'badge badge-warn'
    case 'cancelled': case 'expired': return 'badge badge-muted'
    default: return 'badge'
  }
}

function vpsNeedsLifecycleAction(subscription: SubscriptionRecord, vps: VPSAssetRecord | undefined): boolean {
  if (!vps || subscription.status === 'active') return false
  return vps.lifecycle_status !== 'to_cancel' && vps.lifecycle_status !== 'cancelled'
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

  useEffect(() => {
    let cancelled = false
    Promise.all([listSubscriptions(filtersToAPI(filters)), listVPSAssets()])
      .then(([subs, vps]) => { if (!cancelled) setState({ loading: false, error: null, subscriptions: subs, vps }) })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, error: describeError(err, '加载订阅失败'), subscriptions: [], vps: [] }) })
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters.renew_window, filters.status, filters.vps_id, reloadKey])

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

  const vpsByID = useMemo(
    () => new Map(state.vps.map((vps) => [vps.vps_id, vps])),
    [state.vps],
  )
  const vpsOpts = state.vps.map((v) => ({ value: v.vps_id, label: v.display_name }))
  const hasFilters = Boolean(filters.vps_id || filters.status || filters.renew_window)
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
                  <span className="filter-select__label">状态</span>
                  <select className="filter-select__control" value={filters.status ?? ''} onChange={(e) => setFilter('status', (e.target.value || null) as SubscriptionStatus | null)}>
                    <option value="">全部</option>
                    {STATUS_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
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
              <th>状态</th>
              <th className="cell-end">操作</th>
            </tr>
          </thead>
          <tbody>
            {state.subscriptions.map((s) => {
              const isUrgent = s.renew_at && (new Date(s.renew_at).getTime() - now) < 30 * 86400000
              const linkedVPS = vpsByID.get(s.vps_id)
              const needsLifecycleAction = vpsNeedsLifecycleAction(s, linkedVPS)
              return (
                <tr key={s.subscription_id}>
                  <td className="name">{vpsName(s.vps_id)}</td>
                  <td className="sub">{s.billing_cycle}</td>
                  <td>{s.billing_months > 1 ? `${s.billing_months} 个月` : '月付'}</td>
                  <td className="mono">{formatMoney(s.price, s.currency)}</td>
                  <td className={`time${isUrgent ? ' text-warn' : ''}`}>{formatDate(s.renew_at)}</td>
                  <td>
                    <span className={statusBadgeClass(s.status)}>{SUBSCRIPTION_STATUS_LABELS[s.status]}</span>
                    {needsLifecycleAction ? (
                      <span className="asset-context-inline">
                        <span>需要处理资产联动</span>
                        <span aria-hidden="true">·</span>
                        <Link className="text-link" to={`/vps/${s.vps_id}?workbench=cancellation`}>打开工作台</Link>
                      </span>
                    ) : null}
                  </td>
                  <td className="cell-end"><button className="btn-text sm secondary" onClick={() => startEdit(s)}>编辑</button></td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}

      <Modal open={panelOpen} onClose={closeCreate} title="新建订阅" ariaLabel="新建订阅表单">
        <form className="drawer-form" onSubmit={handleCreate}>
          <Select label="VPS" id="sub-create-vps" value={effectiveForm.vpsID} disabled={state.vps.length === 0} onChange={(e) => setCreateForm({ ...createForm, vpsID: e.target.value })} hint={state.vps.length === 0 ? <>无可选 VPS，<Link to="/vps">去创建</Link></> : undefined}>
            <option value="">选择 VPS</option>
            {vpsOpts.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </Select>
          <Input label="价格" type="number" min="0" step="0.01" value={createForm.price} onChange={(e) => setCreateForm({ ...createForm, price: e.target.value })} />
          <Input label="币种" value={createForm.currency} onChange={(e) => setCreateForm({ ...createForm, currency: e.target.value })} />
          <Input label="计费周期" value={createForm.billingCycle} onChange={(e) => setCreateForm({ ...createForm, billingCycle: e.target.value })} />
          <Input label="计费月数" type="number" min="1" value={createForm.billingMonths} onChange={(e) => setCreateForm({ ...createForm, billingMonths: e.target.value })} />
          <Input label="开始日期" type="date" value={createForm.startedAt} onChange={(e) => setCreateForm({ ...createForm, startedAt: e.target.value })} />
          <Input label="续费日期" type="date" value={createForm.renewAt} onChange={(e) => setCreateForm({ ...createForm, renewAt: e.target.value })} />
          <Select label="状态" id="sub-create-status" options={STATUS_OPTIONS} value={createForm.status} onChange={(e) => setCreateForm({ ...createForm, status: e.target.value as SubscriptionStatus })} />
          <Input label="支付方式" value={createForm.paymentMethod} onChange={(e) => setCreateForm({ ...createForm, paymentMethod: e.target.value })} />
          <label className="ck">
            <input type="checkbox" checked={createForm.autoRenew} onChange={(e) => setCreateForm({ ...createForm, autoRenew: e.target.checked })} />
            <span className="ck-box" /> 自动续费
          </label>
          <label className="ck">
            <input type="checkbox" checked={createForm.autoRenewCancelled} onChange={(e) => setCreateForm({ ...createForm, autoRenewCancelled: e.target.checked })} />
            <span className="ck-box" /> 已取消自动续费
          </label>
          <Input label="备注" value={createForm.note} onChange={(e) => setCreateForm({ ...createForm, note: e.target.value })} />
          {createError && <p className="create-form__error" role="alert">{createError}</p>}
          <div className="page-form-actions">
            <button type="button" className="btn md secondary" onClick={closeCreate} disabled={createSubmitting}>取消</button>
            <button type="submit" className="btn md primary" disabled={createSubmitting}>{createSubmitting ? '创建中…' : '创建'}</button>
          </div>
        </form>
      </Modal>

      <Modal open={editingId != null} onClose={cancelEdit} title="编辑订阅" ariaLabel="编辑订阅表单">
        <form className="drawer-form" onSubmit={handleEdit}>
          <Select label="VPS" id="sub-edit-vps" value={editForm.vpsID} onChange={(e) => setEditForm({ ...editForm, vpsID: e.target.value })}>
            <option value="">选择 VPS</option>
            {vpsOpts.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </Select>
          <Input label="价格" type="number" min="0" step="0.01" value={editForm.price} onChange={(e) => setEditForm({ ...editForm, price: e.target.value })} />
          <Input label="币种" value={editForm.currency} onChange={(e) => setEditForm({ ...editForm, currency: e.target.value })} />
          <Input label="计费周期" value={editForm.billingCycle} onChange={(e) => setEditForm({ ...editForm, billingCycle: e.target.value })} />
          <Input label="计费月数" type="number" min="1" value={editForm.billingMonths} onChange={(e) => setEditForm({ ...editForm, billingMonths: e.target.value })} />
          <Input label="开始日期" type="date" value={editForm.startedAt} onChange={(e) => setEditForm({ ...editForm, startedAt: e.target.value })} />
          <Input label="续费日期" type="date" value={editForm.renewAt} onChange={(e) => setEditForm({ ...editForm, renewAt: e.target.value })} />
          <Select label="状态" id="sub-edit-status" options={STATUS_OPTIONS} value={editForm.status} onChange={(e) => setEditForm({ ...editForm, status: e.target.value as SubscriptionStatus })} />
          <Input label="支付方式" value={editForm.paymentMethod} onChange={(e) => setEditForm({ ...editForm, paymentMethod: e.target.value })} />
          <label className="ck">
            <input type="checkbox" checked={editForm.autoRenew} onChange={(e) => setEditForm({ ...editForm, autoRenew: e.target.checked })} />
            <span className="ck-box" /> 自动续费
          </label>
          <label className="ck">
            <input type="checkbox" checked={editForm.autoRenewCancelled} onChange={(e) => setEditForm({ ...editForm, autoRenewCancelled: e.target.checked })} />
            <span className="ck-box" /> 已取消自动续费
          </label>
          <Input label="备注" value={editForm.note} onChange={(e) => setEditForm({ ...editForm, note: e.target.value })} />
          {editError && <p className="create-form__error" role="alert">{editError}</p>}
          <div className="page-form-actions">
            <button type="button" className="btn md secondary" onClick={cancelEdit} disabled={editSubmitting}>取消</button>
            <button type="submit" className="btn md primary" disabled={editSubmitting}>{editSubmitting ? '保存中…' : '保存'}</button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
