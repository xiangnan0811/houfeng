import { type FormEvent, useEffect, useState } from 'react'

import { Input, Select } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import {
  ApiError,
  bulkUpsertSubscriptionMonthlyBudgets,
  getSubscriptionCostSettings,
  listSubscriptionMonthlyBudgets,
  refreshSubscriptionExchangeRates,
  updateSubscriptionCostSettings,
  upsertSubscriptionMonthlyBudget,
} from '../../lib/api'
import { formatMoney } from '../../lib/format'
import type {
  BulkUpsertSubscriptionMonthlyBudgetInput,
  SubscriptionCostSettings,
  SubscriptionMonthlyBudgetBulkScope,
  SubscriptionMonthlyBudgetRecord,
  UpsertSubscriptionMonthlyBudgetInput,
} from '../../lib/types'

type LoadState = {
  loading: boolean
  error: string | null
  settings: SubscriptionCostSettings | null
  monthlyBudgets: SubscriptionMonthlyBudgetRecord[]
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
  month: string
  monthlyLimit: string
  warningPct: string
  note: string
}

type BudgetValues = Omit<UpsertSubscriptionMonthlyBudgetInput, 'budget_month'>
type PendingBudgetSave =
  | { kind: 'single'; month: string; input: UpsertSubscriptionMonthlyBudgetInput }
  | { kind: 'bulk'; input: BulkUpsertSubscriptionMonthlyBudgetInput }

const INITIAL_STATE: LoadState = {
  loading: true,
  error: null,
  settings: null,
  monthlyBudgets: [],
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
  month: currentMonthValue(),
  monthlyLimit: '',
  warningPct: '80',
  note: '',
}

const BUDGET_BULK_SCOPE_OPTIONS: Array<{ value: SubscriptionMonthlyBudgetBulkScope; label: string }> = [
  { value: 'all_history', label: '所有时间月预算' },
  { value: 'recent_year', label: '最近一年月预算' },
  { value: 'current_year', label: '今年月预算' },
]

function currentMonthValue(): string {
  const now = new Date()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  return `${now.getFullYear()}-${month}`
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
    baseCurrency: settings.base_currency ?? INITIAL_SETTINGS_DRAFT.baseCurrency,
    provider: settings.exchange_rate_provider ?? INITIAL_SETTINGS_DRAFT.provider,
    fixerApiKey: '',
    reminderOffsets: settings.default_reminder_offsets_days?.join(',') ?? INITIAL_SETTINGS_DRAFT.reminderOffsets,
    maxLeadDays: settings.max_reminder_lead_days != null ? String(settings.max_reminder_lead_days) : INITIAL_SETTINGS_DRAFT.maxLeadDays,
    staleHours: settings.exchange_rate_stale_after_hours != null ? String(settings.exchange_rate_stale_after_hours) : INITIAL_SETTINGS_DRAFT.staleHours,
  }
}

function money(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '-'
  return formatMoney(value, currency)
}

function budgetToDraft(budget: SubscriptionMonthlyBudgetRecord): BudgetDraft {
  return {
    month: budget.budget_month?.slice(0, 7) ?? currentMonthValue(),
    monthlyLimit: String(budget.monthly_limit),
    warningPct: String(budget.warning_pct),
    note: budget.note ?? '',
  }
}

function parseRequiredLimit(value: string, label: string): number {
  const trimmed = value.trim()
  if (!trimmed) throw new Error(`${label}不能为空。`)
  const parsed = Number.parseFloat(trimmed)
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`${label}必须为非负数字。`)
  }
  return parsed
}

function buildBudgetValues(draft: BudgetDraft, baseCurrency: string): BudgetValues {
  const monthlyLimit = parseRequiredLimit(draft.monthlyLimit, '月预算')
  const warningPct = Number.parseInt(draft.warningPct, 10)
  if (!Number.isInteger(warningPct) || warningPct < 1 || warningPct > 100) throw new Error('预警比例必须在 1-100 之间。')
  return {
    base_currency: baseCurrency,
    monthly_limit: monthlyLimit,
    warning_pct: warningPct,
    note: draft.note.trim(),
  }
}

function buildMonthlyBudgetInput(draft: BudgetDraft, baseCurrency: string): UpsertSubscriptionMonthlyBudgetInput {
  if (!/^\d{4}-\d{2}$/.test(draft.month.trim())) throw new Error('预算月份必须为 YYYY-MM。')
  return buildBudgetValues(draft, baseCurrency)
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
  const [budgetBulkEnabled, setBudgetBulkEnabled] = useState(false)
  const [budgetBulkScope, setBudgetBulkScope] = useState<SubscriptionMonthlyBudgetBulkScope>('recent_year')
  const [budgetError, setBudgetError] = useState<string | null>(null)
  const [budgetNotice, setBudgetNotice] = useState<string | null>(null)
  const [editingBudgetMonth, setEditingBudgetMonth] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState<BudgetDraft>(INITIAL_BUDGET_DRAFT)
  const [editingSubmitting, setEditingSubmitting] = useState(false)
  const [editingError, setEditingError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    Promise.all([getSubscriptionCostSettings(), listSubscriptionMonthlyBudgets()])
      .then(([settings, monthlyBudgets]) => {
        if (cancelled) return
        setState({ loading: false, error: null, settings, monthlyBudgets })
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

  async function handleSaveAll(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault()
    setSettingsError(null)
    setBudgetError(null)
    setSettingsNotice(null)
    setBudgetNotice(null)
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
    const nextBaseCurrency = settingsDraft.baseCurrency.trim().toUpperCase()
    let pendingBudget: PendingBudgetSave | null = null
    if (budgetDraft.monthlyLimit.trim() !== '') {
      try {
        pendingBudget = budgetBulkEnabled
          ? { kind: 'bulk', input: { ...buildBudgetValues(budgetDraft, nextBaseCurrency), scope: budgetBulkScope } }
          : { kind: 'single', month: budgetDraft.month, input: buildMonthlyBudgetInput(budgetDraft, nextBaseCurrency) }
      } catch (err: unknown) {
        setBudgetError(describeError(err, '预算输入无效'))
        return
      }
    }
    setSettingsSubmitting(true)
    let updated: SubscriptionCostSettings
    try {
      updated = await updateSubscriptionCostSettings({
        base_currency: nextBaseCurrency,
        exchange_rate_provider: settingsDraft.provider,
        ...(settingsDraft.fixerApiKey.trim()
          ? { fixer_api_key: settingsDraft.fixerApiKey.trim() }
          : {}),
        default_reminder_offsets_days: offsets,
        max_reminder_lead_days: maxLeadDays,
        exchange_rate_stale_after_hours: staleHours,
      })
      setState((current) => ({ ...current, settings: updated }))
      setSettingsDraft(settingsToDraft(updated))
      setSettingsNotice('订阅成本设置已保存')
    } catch (err: unknown) {
      setSettingsSubmitting(false)
      setSettingsError(describeError(err, '保存订阅设置失败'))
      return
    }

    if (pendingBudget) {
      try {
        if (pendingBudget.kind === 'bulk') {
          const result = await bulkUpsertSubscriptionMonthlyBudgets(pendingBudget.input)
          const scopeLabel = BUDGET_BULK_SCOPE_OPTIONS.find((item) => item.value === result.scope)?.label ?? '历史月预算'
          setBudgetNotice(`${scopeLabel}已保存，共覆盖 ${result.records.length} 个月`)
        } else {
          await upsertSubscriptionMonthlyBudget(pendingBudget.month, pendingBudget.input)
          setBudgetNotice('预算已保存')
        }
        setBudgetDraft({ ...INITIAL_BUDGET_DRAFT, month: currentMonthValue() })
        setBudgetBulkEnabled(false)
      } catch (err: unknown) {
        setSettingsSubmitting(false)
        setBudgetError(describeError(err, '保存预算失败'))
        reload()
        return
      }
    }
    reload()
    setSettingsSubmitting(false)
  }

  function handleRefreshRates() {
    setRateNotice(null)
    setRefreshingRates(true)
    refreshSubscriptionExchangeRates()
      .then((result) => {
        const failedItems = result.failed ?? []
        const failed = failedItems.map((item) => item?.quote_currency).filter(Boolean)
        setRateNotice(`汇率刷新完成：成功 ${result.succeeded?.length ?? 0}，失败 ${failedItems.length}${failed.length ? `（${failed.join(', ')}）` : ''}`)
        reload()
      })
      .catch((err: unknown) => setRateNotice(describeError(err, '汇率刷新失败')))
      .finally(() => setRefreshingRates(false))
  }

  function startEditBudget(budget: SubscriptionMonthlyBudgetRecord) {
    setEditingBudgetMonth(budget.budget_month.slice(0, 7))
    setEditDraft(budgetToDraft(budget))
    setEditingError(null)
  }

  function cancelEditBudget() {
    setEditingBudgetMonth(null)
    setEditDraft(INITIAL_BUDGET_DRAFT)
    setEditingError(null)
  }

  function handleUpdateBudget(event: FormEvent<HTMLFormElement>, month: string) {
    event.preventDefault()
    setEditingError(null)
    let input: UpsertSubscriptionMonthlyBudgetInput
    try {
      input = buildMonthlyBudgetInput(editDraft, baseCurrency)
    } catch (err: unknown) {
      setEditingError(describeError(err, '预算输入无效'))
      return
    }
    setEditingSubmitting(true)
    upsertSubscriptionMonthlyBudget(month, input)
      .then(() => {
        cancelEditBudget()
        setBudgetNotice('预算已保存')
        reload()
      })
      .catch((err: unknown) => setEditingError(describeError(err, '保存预算失败')))
      .finally(() => setEditingSubmitting(false))
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
        <form className="subscription-inline-form subscription-inline-form--settings" onSubmit={handleSaveAll}>
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
        </form>
      </section>

      <section className="settings-section subscription-settings__section">
        <div className="section-heading">
          <p className="section-heading__eyebrow">Budgets</p>
          <h2 className="section-heading__title">月预算时间线</h2>
        </div>
        <div className="subscription-budget-list subscription-budget-list--managed">
          {state.monthlyBudgets.length === 0 ? (
            <p className="asset-table-empty-state">
              <strong>尚未配置月预算</strong>
              <span>添加一个月份的全局月预算后，后续月份会沿用最近一次配置，直到再次调整。</span>
            </p>
          ) : null}
          {state.monthlyBudgets.map((budget) => (
            editingBudgetMonth === budget.budget_month.slice(0, 7) ? (
              <form key={budget.budget_month} className="subscription-budget-edit" onSubmit={(event) => handleUpdateBudget(event, editDraft.month)}>
                <Input label="预算月份" type="month" value={editDraft.month} onChange={(event) => setEditDraft({ ...editDraft, month: event.target.value })} />
                <Input label={`月预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={editDraft.monthlyLimit} onChange={(event) => setEditDraft({ ...editDraft, monthlyLimit: event.target.value })} />
                <Input label="预警比例" type="number" min="1" max="100" value={editDraft.warningPct} onChange={(event) => setEditDraft({ ...editDraft, warningPct: event.target.value })} />
                <Input label="备注" value={editDraft.note} onChange={(event) => setEditDraft({ ...editDraft, note: event.target.value })} />
                {editingError ? <p className="create-form__error" role="alert">{editingError}</p> : null}
                <div className="page-form-actions">
                  <button type="button" className="btn sm secondary" onClick={cancelEditBudget} disabled={editingSubmitting}>取消</button>
                  <button type="submit" className="btn sm primary" disabled={editingSubmitting}>{editingSubmitting ? '保存中…' : '保存预算'}</button>
                </div>
              </form>
            ) : (
              <div key={budget.budget_month} className="subscription-budget-row">
                <div>
                  <strong>{budget.budget_month.slice(0, 7)}</strong>
                  <small>全局月预算 · 预警 {budget.warning_pct}%{budget.note ? ` · ${budget.note}` : ''}</small>
                </div>
                <span className="badge badge-muted"><span className="badge-dot" />{budget.base_currency}</span>
                <span className="mono">{money(budget.monthly_limit, budget.base_currency)}</span>
                <div className="subscription-budget-row__actions">
                  <button type="button" className="btn-text sm secondary" onClick={() => startEditBudget(budget)}>编辑</button>
                </div>
              </div>
            )
          ))}
        </div>
        <form className="subscription-inline-form" onSubmit={handleSaveAll}>
          <Input label="预算月份" type="month" value={budgetDraft.month} onChange={(event) => setBudgetDraft({ ...budgetDraft, month: event.target.value })} />
          <Input label={`月预算 ${baseCurrency}`} type="number" min="0" step="0.01" value={budgetDraft.monthlyLimit} onChange={(event) => setBudgetDraft({ ...budgetDraft, monthlyLimit: event.target.value })} />
          <Input label="预警比例" type="number" min="1" max="100" value={budgetDraft.warningPct} onChange={(event) => setBudgetDraft({ ...budgetDraft, warningPct: event.target.value })} />
          <Input label="备注" value={budgetDraft.note} onChange={(event) => setBudgetDraft({ ...budgetDraft, note: event.target.value })} />
          <label className="asset-checkbox-line subscription-budget-coverage">
            <input
              type="checkbox"
              checked={budgetBulkEnabled}
              onChange={(event) => setBudgetBulkEnabled(event.target.checked)}
            />
            <span>
              <strong>批量覆盖历史月预算</strong>
              <small>首次配置时可一次写入多个历史月份；默认关闭，只保存所选月份。</small>
            </span>
          </label>
          {budgetBulkEnabled ? (
            <Select
              label="覆盖范围"
              value={budgetBulkScope}
              onChange={(event) => setBudgetBulkScope(event.target.value as SubscriptionMonthlyBudgetBulkScope)}
            >
              {BUDGET_BULK_SCOPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </Select>
          ) : null}
        </form>
      </section>

      <div className="settings-save-footer">
        <div>
          {settingsError ? <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{settingsError}</p> : null}
          {settingsNotice ? <p className="settings-save-footer__message settings-save-footer__message--success">{settingsNotice}</p> : null}
          {budgetError ? <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{budgetError}</p> : null}
          {budgetNotice ? <p className="settings-save-footer__message settings-save-footer__message--success">{budgetNotice}</p> : null}
          {rateNotice ? <p className="settings-save-footer__message settings-save-footer__message--success">{rateNotice}</p> : null}
        </div>
        <button className="btn md primary" type="button" onClick={() => handleSaveAll()} disabled={settingsSubmitting}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" />
            <path d="M17 21v-8H7v8M7 3v5h8" />
          </svg>
          {settingsSubmitting ? '保存中…' : '保存订阅配置'}
        </button>
      </div>
    </div>
  )
}
