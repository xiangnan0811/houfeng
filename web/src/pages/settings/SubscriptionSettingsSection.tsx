import { type FormEvent, useEffect, useState } from 'react'

import { Input, Select } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import {
  ApiError,
  createSubscriptionBudget,
  getSubscriptionCostSettings,
  listSubscriptionBudgets,
  refreshSubscriptionExchangeRates,
  updateSubscriptionBudget,
  updateSubscriptionCostSettings,
} from '../../lib/api'
import { formatMoney } from '../../lib/format'
import type {
  CreateSubscriptionBudgetInput,
  PatchSubscriptionBudgetInput,
  SubscriptionBudgetRecord,
  SubscriptionCostSettings,
} from '../../lib/types'

type LoadState = {
  loading: boolean
  error: string | null
  settings: SubscriptionCostSettings | null
  budgets: SubscriptionBudgetRecord[]
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

const INITIAL_STATE: LoadState = {
  loading: true,
  error: null,
  settings: null,
  budgets: [],
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

function parseIntegerList(value: string): number[] {
  return value
    .split(',')
    .map((item) => Number.parseInt(item.trim(), 10))
    .filter((num) => Number.isInteger(num) && num >= 0)
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

function money(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '-'
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
  return map[status ?? ''] ?? (status || '-')
}

function budgetBadgeClass(status?: string | null): string {
  if (status === 'over') return 'badge badge-err'
  if (status === 'warning') return 'badge badge-warn'
  if (status === 'ok') return 'badge badge-ok'
  return 'badge badge-muted'
}

function scopeLabel(scopeType: string, scopeID: string): string {
  const scopeMap: Record<string, string> = {
    global: '全局',
    provider: '供应商',
    label: '标签',
    category: '分类',
    vps: 'VPS',
  }
  const label = scopeMap[scopeType] ?? scopeType
  return scopeID ? `${label} · ${scopeID}` : label
}

function budgetToDraft(budget: SubscriptionBudgetRecord): BudgetDraft {
  return {
    scopeType: budget.scope_type,
    scopeID: budget.scope_id,
    name: budget.name,
    monthlyLimit: budget.monthly_limit == null ? '' : String(budget.monthly_limit),
    yearlyLimit: budget.yearly_limit == null ? '' : String(budget.yearly_limit),
    warningPct: String(budget.warning_pct),
    enabled: budget.enabled,
    note: budget.note,
  }
}

function parseOptionalLimit(value: string, label: string): number | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  const parsed = Number.parseFloat(trimmed)
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`${label}必须为非负数字。`)
  }
  return parsed
}

function buildBudgetInput(draft: BudgetDraft, baseCurrency: string): CreateSubscriptionBudgetInput {
  const monthlyLimit = parseOptionalLimit(draft.monthlyLimit, '月预算')
  const yearlyLimit = parseOptionalLimit(draft.yearlyLimit, '年预算')
  const warningPct = Number.parseInt(draft.warningPct, 10)
  if (!draft.name.trim()) throw new Error('预算名称不能为空。')
  if (draft.scopeType !== 'global' && !draft.scopeID.trim()) throw new Error('非全局预算需要填写 scope ID。')
  if (monthlyLimit == null && yearlyLimit == null) throw new Error('月预算或年预算至少填写一个。')
  if (!Number.isInteger(warningPct) || warningPct < 1 || warningPct > 100) throw new Error('预警比例必须在 1-100 之间。')
  return {
    scope_type: draft.scopeType,
    scope_id: draft.scopeType === 'global' ? '' : draft.scopeID.trim(),
    name: draft.name.trim(),
    base_currency: baseCurrency,
    monthly_limit: monthlyLimit,
    yearly_limit: yearlyLimit,
    warning_pct: warningPct,
    enabled: draft.enabled,
    note: draft.note.trim(),
  }
}

export function SubscriptionSettingsSection() {
  const [state, setState] = useState<LoadState>(INITIAL_STATE)
  const [reloadKey, setReloadKey] = useState(0)
  const [settingsDraft, setSettingsDraft] = useState<SettingsDraft>(INITIAL_SETTINGS_DRAFT)
  const [settingsSubmitting, setSettingsSubmitting] = useState(false)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [settingsNotice, setSettingsNotice] = useState<string | null>(null)
  const [refreshingRates, setRefreshingRates] = useState(false)
  const [rateNotice, setRateNotice] = useState<string | null>(null)
  const [budgetDraft, setBudgetDraft] = useState<BudgetDraft>(INITIAL_BUDGET_DRAFT)
  const [budgetSubmitting, setBudgetSubmitting] = useState(false)
  const [budgetError, setBudgetError] = useState<string | null>(null)
  const [budgetNotice, setBudgetNotice] = useState<string | null>(null)
  const [editingBudgetID, setEditingBudgetID] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState<BudgetDraft>(INITIAL_BUDGET_DRAFT)
  const [editingSubmitting, setEditingSubmitting] = useState(false)
  const [editingError, setEditingError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    Promise.all([getSubscriptionCostSettings(), listSubscriptionBudgets()])
      .then(([settings, budgets]) => {
        if (cancelled) return
        setState({ loading: false, error: null, settings, budgets })
        setSettingsDraft(settingsToDraft(settings))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState({ ...INITIAL_STATE, loading: false, error: describeError(err, '加载订阅配置失败') })
      })
    return () => { cancelled = true }
  }, [reloadKey])

  const baseCurrency = state.settings?.base_currency ?? settingsDraft.baseCurrency

  function reload() {
    setReloadKey((key) => key + 1)
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
        setState((current) => ({ ...current, settings }))
        setSettingsDraft(settingsToDraft(settings))
        setSettingsNotice('订阅成本设置已保存')
      })
      .catch((err: unknown) => setSettingsError(describeError(err, '保存订阅设置失败')))
      .finally(() => setSettingsSubmitting(false))
  }

  function handleRefreshRates() {
    setRateNotice(null)
    setRefreshingRates(true)
    refreshSubscriptionExchangeRates()
      .then((result) => {
        const failed = result.failed.map((item) => item.quote_currency).filter(Boolean)
        setRateNotice(`汇率刷新完成：成功 ${result.succeeded.length}，失败 ${result.failed.length}${failed.length ? `（${failed.join(', ')}）` : ''}`)
        reload()
      })
      .catch((err: unknown) => setRateNotice(describeError(err, '汇率刷新失败')))
      .finally(() => setRefreshingRates(false))
  }

  function handleCreateBudget(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBudgetError(null)
    setBudgetNotice(null)
    let input: CreateSubscriptionBudgetInput
    try {
      input = buildBudgetInput(budgetDraft, baseCurrency)
    } catch (err: unknown) {
      setBudgetError(describeError(err, '预算输入无效'))
      return
    }
    setBudgetSubmitting(true)
    createSubscriptionBudget(input)
      .then(() => {
        setBudgetDraft(INITIAL_BUDGET_DRAFT)
        setBudgetNotice('预算已创建')
        reload()
      })
      .catch((err: unknown) => setBudgetError(describeError(err, '创建预算失败')))
      .finally(() => setBudgetSubmitting(false))
  }

  function startEditBudget(budget: SubscriptionBudgetRecord) {
    setEditingBudgetID(budget.budget_id)
    setEditDraft(budgetToDraft(budget))
    setEditingError(null)
  }

  function cancelEditBudget() {
    setEditingBudgetID(null)
    setEditDraft(INITIAL_BUDGET_DRAFT)
    setEditingError(null)
  }

  function handleUpdateBudget(event: FormEvent<HTMLFormElement>, budgetID: string) {
    event.preventDefault()
    setEditingError(null)
    let input: CreateSubscriptionBudgetInput
    try {
      input = buildBudgetInput(editDraft, baseCurrency)
    } catch (err: unknown) {
      setEditingError(describeError(err, '预算输入无效'))
      return
    }
    const payload: PatchSubscriptionBudgetInput = { budget_id: budgetID, ...input }
    setEditingSubmitting(true)
    updateSubscriptionBudget(payload)
      .then(() => {
        cancelEditBudget()
        setBudgetNotice('预算已保存')
        reload()
      })
      .catch((err: unknown) => setEditingError(describeError(err, '保存预算失败')))
      .finally(() => setEditingSubmitting(false))
  }

  function toggleBudget(budget: SubscriptionBudgetRecord) {
    setBudgetError(null)
    setBudgetNotice(null)
    updateSubscriptionBudget({ budget_id: budget.budget_id, enabled: !budget.enabled })
      .then(() => {
        setBudgetNotice(budget.enabled ? '预算已停用' : '预算已启用')
        reload()
      })
      .catch((err: unknown) => setBudgetError(describeError(err, '更新预算状态失败')))
  }

  if (state.loading) {
    return <PageState kind="loading" title="正在加载订阅配置…" surface="empty" compact />
  }
  if (state.error) {
    return (
      <PageState
        kind="error"
        title="订阅配置不可用"
        description={state.error}
        action={<button type="button" className="btn sm secondary" onClick={reload}>重试</button>}
        surface="empty"
        compact
      />
    )
  }

  return (
    <div className="subscription-settings animate-in">
      <section className="settings-section subscription-settings__section">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">Cost Settings</p>
            <h2 className="section-heading__title">成本基准与汇率</h2>
          </div>
          <button type="button" className="btn sm secondary" onClick={handleRefreshRates} disabled={refreshingRates}>
            {refreshingRates ? '刷新中…' : '刷新汇率'}
          </button>
        </div>
        <form className="subscription-inline-form subscription-inline-form--settings" onSubmit={handleSettingsSubmit}>
          <Input label="基准货币" value={settingsDraft.baseCurrency} onChange={(event) => setSettingsDraft({ ...settingsDraft, baseCurrency: event.target.value })} />
          <Select label="汇率 Provider" value={settingsDraft.provider} onChange={(event) => setSettingsDraft({ ...settingsDraft, provider: event.target.value })}>
            <option value="frankfurter">Frankfurter</option>
            <option value="fixer">Fixer</option>
          </Select>
          <Input
            label={state.settings?.fixer_configured ? 'Fixer key 已配置' : 'Fixer key'}
            type="password"
            value={settingsDraft.fixerApiKey}
            onChange={(event) => setSettingsDraft({ ...settingsDraft, fixerApiKey: event.target.value })}
            placeholder={state.settings?.fixer_masked_summary || '留空则不修改'}
          />
          <Input label="提醒窗口" value={settingsDraft.reminderOffsets} onChange={(event) => setSettingsDraft({ ...settingsDraft, reminderOffsets: event.target.value })} />
          <Input label="最远提前天数" type="number" min="1" value={settingsDraft.maxLeadDays} onChange={(event) => setSettingsDraft({ ...settingsDraft, maxLeadDays: event.target.value })} />
          <Input label="汇率过期小时" type="number" min="1" value={settingsDraft.staleHours} onChange={(event) => setSettingsDraft({ ...settingsDraft, staleHours: event.target.value })} />
          {settingsError ? <p className="create-form__error" role="alert">{settingsError}</p> : null}
          {settingsNotice ? <p className="asset-operation-feedback" role="status">{settingsNotice}</p> : null}
          {rateNotice ? <p className="asset-operation-feedback" role="status">{rateNotice}</p> : null}
          <button className="btn sm primary" type="submit" disabled={settingsSubmitting}>{settingsSubmitting ? '保存中…' : '保存订阅设置'}</button>
        </form>
      </section>

      <section className="settings-section subscription-settings__section">
        <div className="section-heading">
          <p className="section-heading__eyebrow">Budgets</p>
          <h2 className="section-heading__title">预算规则</h2>
        </div>
        <div className="subscription-budget-list subscription-budget-list--managed">
          {state.budgets.length === 0 ? (
            <p className="asset-table-empty-state">
              <strong>尚未配置预算</strong>
              <span>可以先添加全局月预算，再逐步按供应商、标签或 VPS 收敛。</span>
            </p>
          ) : null}
          {state.budgets.map((budget) => (
            editingBudgetID === budget.budget_id ? (
              <form key={budget.budget_id} className="subscription-budget-edit" onSubmit={(event) => handleUpdateBudget(event, budget.budget_id)}>
                <Select label="范围" value={editDraft.scopeType} onChange={(event) => setEditDraft({ ...editDraft, scopeType: event.target.value })}>
                  <option value="global">全局</option>
                  <option value="provider">供应商</option>
                  <option value="label">标签</option>
                  <option value="category">分类</option>
                  <option value="vps">VPS</option>
                </Select>
                <Input label="Scope ID" value={editDraft.scopeID} disabled={editDraft.scopeType === 'global'} onChange={(event) => setEditDraft({ ...editDraft, scopeID: event.target.value })} />
                <Input label="名称" value={editDraft.name} onChange={(event) => setEditDraft({ ...editDraft, name: event.target.value })} />
                <Input label={`月预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={editDraft.monthlyLimit} onChange={(event) => setEditDraft({ ...editDraft, monthlyLimit: event.target.value })} />
                <Input label={`年预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={editDraft.yearlyLimit} onChange={(event) => setEditDraft({ ...editDraft, yearlyLimit: event.target.value })} />
                <Input label="预警比例" type="number" min="1" max="100" value={editDraft.warningPct} onChange={(event) => setEditDraft({ ...editDraft, warningPct: event.target.value })} />
                <Input label="备注" value={editDraft.note} onChange={(event) => setEditDraft({ ...editDraft, note: event.target.value })} />
                <label className="asset-checkbox-line"><input type="checkbox" checked={editDraft.enabled} onChange={(event) => setEditDraft({ ...editDraft, enabled: event.target.checked })} />启用</label>
                {editingError ? <p className="create-form__error" role="alert">{editingError}</p> : null}
                <div className="page-form-actions">
                  <button type="button" className="btn sm secondary" onClick={cancelEditBudget} disabled={editingSubmitting}>取消</button>
                  <button type="submit" className="btn sm primary" disabled={editingSubmitting}>{editingSubmitting ? '保存中…' : '保存预算'}</button>
                </div>
              </form>
            ) : (
              <div key={budget.budget_id} className="subscription-budget-row">
                <div>
                  <strong>{budget.name}</strong>
                  <small>{scopeLabel(budget.scope_type, budget.scope_id)} · 预警 {budget.warning_pct}%</small>
                </div>
                <span className={budgetBadgeClass(budget.status)}><span className="badge-dot" />{budgetStatusLabel(budget.status)}</span>
                <span className="mono">{money(budget.current_monthly_spend, budget.base_currency)} / {budget.monthly_limit != null ? money(budget.monthly_limit, budget.base_currency) : money((budget.yearly_limit ?? 0) / 12, budget.base_currency)}</span>
                <div className="subscription-budget-row__actions">
                  <button type="button" className="btn-text sm secondary" onClick={() => startEditBudget(budget)}>编辑</button>
                  <button type="button" className="btn-text sm secondary" onClick={() => toggleBudget(budget)}>{budget.enabled ? '停用' : '启用'}</button>
                </div>
              </div>
            )
          ))}
        </div>
        <form className="subscription-inline-form" onSubmit={handleCreateBudget}>
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
          <Input label={`年预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={budgetDraft.yearlyLimit} onChange={(event) => setBudgetDraft({ ...budgetDraft, yearlyLimit: event.target.value })} />
          <Input label="预警比例" type="number" min="1" max="100" value={budgetDraft.warningPct} onChange={(event) => setBudgetDraft({ ...budgetDraft, warningPct: event.target.value })} />
          <Input label="备注" value={budgetDraft.note} onChange={(event) => setBudgetDraft({ ...budgetDraft, note: event.target.value })} />
          <label className="asset-checkbox-line"><input type="checkbox" checked={budgetDraft.enabled} onChange={(event) => setBudgetDraft({ ...budgetDraft, enabled: event.target.checked })} />启用</label>
          {budgetError ? <p className="create-form__error" role="alert">{budgetError}</p> : null}
          {budgetNotice ? <p className="asset-operation-feedback" role="status">{budgetNotice}</p> : null}
          <button className="btn sm primary" type="submit" disabled={budgetSubmitting}>{budgetSubmitting ? '创建中…' : '添加预算'}</button>
        </form>
      </section>
    </div>
  )
}
