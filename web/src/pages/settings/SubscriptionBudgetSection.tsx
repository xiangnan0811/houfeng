import { type FormEvent, useId, useState } from 'react'

import { Badge } from '../../components/atoms'
import { bulkUpsertSubscriptionMonthlyBudgets, upsertSubscriptionMonthlyBudget } from '../../lib/api'
import { formatMoney } from '../../lib/format'
import type { SubscriptionMonthlyBudgetBulkScope, SubscriptionMonthlyBudgetRecord } from '../../lib/types'
import {
  BUDGET_BULK_SCOPE_OPTIONS,
  type BudgetDraft,
  budgetToDraft,
  buildBudgetValues,
  buildMonthlyBudgetInput,
  describeError,
  effectiveBudgetMonth,
  emptyBudgetDraft,
} from './subscriptionSettingsModel'

type BudgetFieldsProps = {
  draft: BudgetDraft
  baseCurrency: string
  onChange: (draft: BudgetDraft) => void
  hideMonth?: boolean
}

/** 月份 / 金额 / 预警 / 备注：新增与行内编辑共用同一组紧凑字段。 */
function BudgetFields({ draft, baseCurrency, onChange, hideMonth = false }: BudgetFieldsProps) {
  return (
    <div className="settings-row-group settings-row-group--4 subscription-budget-fields">
      {hideMonth ? null : (
        <label className="settings-row">
          <span className="sr-label">月份</span>
          <span className="sr-value">
            <input className="input" type="month" aria-label="预算月份" value={draft.month} onChange={(event) => onChange({ ...draft, month: event.target.value })} />
          </span>
        </label>
      )}
      <label className="settings-row">
        <span className="sr-label">月预算</span>
        <span className="sr-value">
          <input className="input input--compact subscription-budget-fields__amount" type="number" min="0" step="0.01" inputMode="decimal" aria-label={`月预算 ${baseCurrency}`} value={draft.monthlyLimit} onChange={(event) => onChange({ ...draft, monthlyLimit: event.target.value })} /> {baseCurrency}
        </span>
      </label>
      <label className="settings-row">
        <span className="sr-label">预警比例</span>
        <span className="sr-value">
          <input className="input input--compact" type="number" min="1" max="100" inputMode="numeric" aria-label="预警比例" value={draft.warningPct} onChange={(event) => onChange({ ...draft, warningPct: event.target.value })} /> %
        </span>
      </label>
      <label className="settings-row">
        <span className="sr-label">备注</span>
        <span className="sr-value">
          <input className="input" aria-label="备注" value={draft.note} onChange={(event) => onChange({ ...draft, note: event.target.value })} />
        </span>
      </label>
    </div>
  )
}

type SubscriptionBudgetSectionProps = {
  budgets: SubscriptionMonthlyBudgetRecord[]
  budgetsError: string | null
  baseCurrency: string
  onSaved: () => void
}

export function SubscriptionBudgetSection({ budgets, budgetsError, baseCurrency, onSaved }: SubscriptionBudgetSectionProps) {
  const headingId = useId()
  const [draft, setDraft] = useState<BudgetDraft>(emptyBudgetDraft)
  const [bulkEnabled, setBulkEnabled] = useState(false)
  const [bulkScope, setBulkScope] = useState<SubscriptionMonthlyBudgetBulkScope>('recent_year')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [editingMonth, setEditingMonth] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState<BudgetDraft>(emptyBudgetDraft)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)
  const [listNotice, setListNotice] = useState<string | null>(null)
  const effectiveMonth = effectiveBudgetMonth(budgets)

  function startEdit(budget: SubscriptionMonthlyBudgetRecord) {
    setListNotice(null)
    setEditingMonth(budget.budget_month.slice(0, 7))
    setEditDraft(budgetToDraft(budget))
    setEditError(null)
  }

  function cancelEdit() {
    setEditingMonth(null)
    setEditDraft(emptyBudgetDraft())
    setEditError(null)
  }

  async function handleAdd(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setNotice(null)
    let save: () => Promise<string>
    try {
      if (bulkEnabled) {
        const input = { ...buildBudgetValues(draft, baseCurrency), scope: bulkScope }
        save = async () => {
          const result = await bulkUpsertSubscriptionMonthlyBudgets(input)
          const scopeLabel = BUDGET_BULK_SCOPE_OPTIONS.find((item) => item.value === result.scope)?.label ?? '历史月预算'
          return `${scopeLabel}已保存，共覆盖 ${result.records.length} 个月`
        }
      } else {
        const input = buildMonthlyBudgetInput(draft, baseCurrency)
        save = async () => {
          await upsertSubscriptionMonthlyBudget(draft.month, input)
          return '预算已保存'
        }
      }
    } catch (err: unknown) {
      setError(describeError(err, '预算输入无效'))
      return
    }
    setSubmitting(true)
    try {
      setNotice(await save())
      setDraft(emptyBudgetDraft())
      setBulkEnabled(false)
      onSaved()
    } catch (err: unknown) {
      // 写入失败保留草稿与错误，不刷新列表。
      setError(describeError(err, '保存预算失败'))
    } finally {
      setSubmitting(false)
    }
  }

  // 行内编辑只改这一行：月份是行身份，固定写回原月份，不会改写其它月份。
  function handleUpdate(event: FormEvent<HTMLFormElement>, month: string) {
    event.preventDefault()
    setEditError(null)
    let input
    try {
      input = buildMonthlyBudgetInput({ ...editDraft, month }, baseCurrency)
    } catch (err: unknown) {
      setEditError(describeError(err, '预算输入无效'))
      return
    }
    setEditSubmitting(true)
    upsertSubscriptionMonthlyBudget(month, input)
      .then(() => {
        cancelEdit()
        setListNotice(`${month} 月预算已保存`)
        onSaved()
      })
      .catch((err: unknown) => setEditError(describeError(err, '保存预算失败')))
      .finally(() => setEditSubmitting(false))
  }

  return (
    <section className="settings-section subscription-settings__section" aria-labelledby={headingId}>
      <div className="subscription-settings__head">
        <h2 className="ss-title" id={headingId}>月预算</h2>
        {listNotice ? <span className="subscription-settings__rate-notice subscription-settings__rate-notice--success" role="status">{listNotice}</span> : null}
        {budgetsError ? <span className="subscription-settings__rate-notice subscription-settings__rate-notice--error" role="alert">{budgetsError}</span> : null}
      </div>
      {budgets.length === 0 ? (
        <p className="subscription-budget-empty">尚未配置月预算</p>
      ) : (
        <ul className="subscription-budget-list" aria-label="月预算">
          {budgets.map((budget) => {
            const month = budget.budget_month.slice(0, 7)
            if (editingMonth === month) {
              return (
                <li key={budget.budget_month} className="subscription-budget-item subscription-budget-item--editing">
                  <form className="subscription-budget-edit" onSubmit={(event) => handleUpdate(event, month)} aria-label={`编辑 ${month} 月预算`}>
                    <span className="subscription-budget-item__month mono tnum">{month}</span>
                    <BudgetFields draft={editDraft} baseCurrency={baseCurrency} onChange={setEditDraft} hideMonth />
                    <div className="subscription-budget-edit__actions">
                      {editError ? <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{editError}</p> : null}
                      <button type="button" className="btn sm secondary" onClick={cancelEdit} disabled={editSubmitting}>取消</button>
                      <button type="submit" className="btn sm primary" disabled={editSubmitting}>{editSubmitting ? '保存中…' : '保存预算'}</button>
                    </div>
                  </form>
                </li>
              )
            }
            return (
              <li key={budget.budget_month} className="subscription-budget-item">
                <span className="subscription-budget-item__month">
                  <span className="mono tnum">{month}</span>
                  {month === effectiveMonth ? <Badge variant="state" tone="normal">当前生效</Badge> : null}
                </span>
                <span className="subscription-budget-item__amount mono tnum">{formatMoney(budget.monthly_limit, budget.base_currency)}</span>
                <span className="subscription-budget-item__muted">预警 <span className="tnum">{budget.warning_pct}%</span></span>
                <span className="subscription-budget-item__note">{budget.note}</span>
                <button type="button" className="btn sm secondary" onClick={() => startEdit(budget)} aria-label={`编辑 ${month} 月预算`}>编辑</button>
              </li>
            )
          })}
        </ul>
      )}
      <form className="subscription-budget-add" onSubmit={handleAdd} aria-label="新增月预算">
        <h3 className="subscription-settings__subtitle">新增月预算</h3>
        <BudgetFields draft={draft} baseCurrency={baseCurrency} onChange={setDraft} hideMonth={bulkEnabled} />
        <div className="subscription-budget-add__footer">
          <label className="subscription-budget-add__bulk">
            <input type="checkbox" checked={bulkEnabled} onChange={(event) => setBulkEnabled(event.target.checked)} />
            批量覆盖历史月份
          </label>
          {bulkEnabled ? (
            <select
              className="input subscription-budget-add__scope"
              aria-label="覆盖范围"
              value={bulkScope}
              onChange={(event) => setBulkScope(event.target.value as SubscriptionMonthlyBudgetBulkScope)}
            >
              {BUDGET_BULK_SCOPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          ) : null}
          <div className="subscription-budget-add__messages">
            {error ? <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{error}</p> : null}
            {notice ? <p className="settings-save-footer__message settings-save-footer__message--success" role="status">{notice}</p> : null}
          </div>
          <button type="submit" className="btn md primary" disabled={submitting}>{submitting ? '保存中…' : '添加预算'}</button>
        </div>
      </form>
    </section>
  )
}
