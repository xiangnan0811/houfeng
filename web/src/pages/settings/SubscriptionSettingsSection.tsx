import { type FormEvent, useEffect, useId, useRef, useState } from 'react'

import { PageState } from '../../components/PageState'
import {
  getSubscriptionCostSettings,
  listSubscriptionMonthlyBudgets,
  updateSubscriptionCostSettings,
} from '../../lib/api'
import type { SubscriptionCostSettings, SubscriptionMonthlyBudgetRecord } from '../../lib/types'
import { SubscriptionBudgetSection } from './SubscriptionBudgetSection'
import { ExchangeRateNoticeBody } from '../subscriptions/ExchangeRateNoticeBody'
import { useExchangeRateRefresh } from '../subscriptions/useExchangeRateRefresh'
import {
  buildSettingsInput,
  describeError,
  INITIAL_SETTINGS_DRAFT,
  type SettingsDraft,
  settingsToDraft,
} from './subscriptionSettingsModel'

type LoadState = {
  loading: boolean
  error: string | null
  settings: SubscriptionCostSettings | null
  monthlyBudgets: SubscriptionMonthlyBudgetRecord[]
}

const INITIAL_STATE: LoadState = { loading: true, error: null, settings: null, monthlyBudgets: [] }

export function SubscriptionSettingsSection() {
  const headingId = useId()
  const fixerHintId = useId()
  const [state, setState] = useState<LoadState>(INITIAL_STATE)
  const [reloadKey, setReloadKey] = useState(0)
  const [budgetsError, setBudgetsError] = useState<string | null>(null)
  // 预算列表刷新可能重叠：只采纳最新一次请求的成功或失败，卸载后丢弃。
  const budgetsRequestRef = useRef(0)

  useEffect(() => () => { budgetsRequestRef.current += 1 }, [])
  const [draft, setDraft] = useState<SettingsDraft>(INITIAL_SETTINGS_DRAFT)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const rateRefresh = useExchangeRateRefresh(() => {})

  useEffect(() => {
    let cancelled = false
    Promise.all([getSubscriptionCostSettings(), listSubscriptionMonthlyBudgets()])
      .then(([settings, monthlyBudgets]) => {
        if (cancelled) return
        setState({ loading: false, error: null, settings, monthlyBudgets })
        setDraft(settingsToDraft(settings))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setState({ ...INITIAL_STATE, loading: false, error: describeError(err, '加载订阅配置失败') })
      })
    return () => { cancelled = true }
  }, [reloadKey])

  const baseCurrency = state.settings?.base_currency ?? draft.baseCurrency

  // 整页重载只用于首次加载失败后的重试；草稿只在加载与成本设置保存成功时重建。
  function reload() {
    setReloadKey((key) => key + 1)
  }

  // 预算写入后只刷新预算列表，不触碰成本卡的未保存草稿；刷新失败保留现有列表并局部提示。
  function reloadBudgets() {
    budgetsRequestRef.current += 1
    const request = budgetsRequestRef.current
    listSubscriptionMonthlyBudgets()
      .then((monthlyBudgets) => {
        if (request !== budgetsRequestRef.current) return
        setBudgetsError(null)
        setState((current) => ({ ...current, monthlyBudgets }))
      })
      .catch((err: unknown) => {
        if (request !== budgetsRequestRef.current) return
        setBudgetsError(describeError(err, '刷新月预算失败'))
      })
  }

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setNotice(null)
    let input
    try {
      input = buildSettingsInput(draft)
    } catch (err: unknown) {
      setError(describeError(err, '订阅设置无效'))
      return
    }
    setSubmitting(true)
    try {
      const updated = await updateSubscriptionCostSettings(input)
      setState((current) => ({ ...current, settings: updated }))
      setDraft(settingsToDraft(updated))
      setNotice('订阅成本设置已保存')
      rateRefresh.readStatus()
    } catch (err: unknown) {
      setError(describeError(err, '保存订阅设置失败'))
    } finally {
      setSubmitting(false)
    }
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

  const fixerSelected = draft.provider === 'fixer'
  const fixerConfigured = Boolean(state.settings?.fixer_configured)

  return (
    <div className="subscription-settings">
      <section className="settings-section subscription-settings__section" aria-labelledby={headingId}>
        <form onSubmit={handleSave} aria-labelledby={headingId}>
          <div className="subscription-settings__head">
            <h2 className="ss-title" id={headingId}>成本基准与汇率</h2>
            <div className="subscription-settings__head-actions">
              {rateRefresh.notice ? (
                <ExchangeRateNoticeBody
                  notice={rateRefresh.notice}
                  className={`subscription-settings__rate-notice subscription-settings__rate-notice--${rateRefresh.notice.tone === 'error' ? 'error' : 'success'}`}
                />
              ) : null}
              {rateRefresh.statusUnavailable ? (
                <button type="button" className="btn sm secondary" onClick={rateRefresh.readStatus}>重试读取</button>
              ) : null}
              <button type="button" className="btn sm secondary" onClick={rateRefresh.refresh} disabled={rateRefresh.refreshing}>
                {rateRefresh.buttonLabel}
              </button>
            </div>
          </div>
          <div className="settings-row-group settings-row-group--3">
            <label className="settings-row">
              <span className="sr-label">基准货币</span>
              <span className="sr-value">
                <input className="input input--compact subscription-settings__currency" aria-label="基准货币" maxLength={3} autoComplete="off" value={draft.baseCurrency} onChange={(event) => setDraft({ ...draft, baseCurrency: event.target.value })} />
              </span>
            </label>
            <label className="settings-row">
              <span className="sr-label">汇率来源</span>
              <span className="sr-value">
                <select className="input input--compact" aria-label="汇率来源" value={draft.provider} onChange={(event) => setDraft({ ...draft, provider: event.target.value })}>
                  <option value="frankfurter">Frankfurter</option>
                  <option value="fixer">Fixer</option>
                </select>
              </span>
            </label>
            {/* Fixer key 只在选择 Fixer 时需要；留空不修改已配置的 key。 */}
            {fixerSelected ? (
              <label className="settings-row">
                <span className="sr-label">Fixer key{fixerConfigured ? <span className="subscription-settings__configured" id={fixerHintId}>已配置</span> : null}</span>
                <span className="sr-value">
                  <input
                    className="input"
                    aria-label="Fixer key"
                    aria-describedby={fixerConfigured ? fixerHintId : undefined}
                    type="password"
                    autoComplete="off"
                    value={draft.fixerApiKey}
                    onChange={(event) => setDraft({ ...draft, fixerApiKey: event.target.value })}
                    placeholder={fixerConfigured ? (state.settings?.fixer_masked_summary || '留空则不修改') : '输入 Fixer API key'}
                  />
                </span>
              </label>
            ) : <span aria-hidden="true" />}
          </div>
          <div className="settings-row-group settings-row-group--3">
            <label className="settings-row">
              <span className="sr-label">提前提醒</span>
              <span className="sr-value">
                <input className="input input--compact subscription-settings__offsets" aria-label="提前提醒天数" inputMode="numeric" value={draft.reminderOffsets} onChange={(event) => setDraft({ ...draft, reminderOffsets: event.target.value })} /> 天
              </span>
            </label>
            <label className="settings-row">
              <span className="sr-label">最远提前</span>
              <span className="sr-value">
                <input className="input input--compact" type="number" min="1" inputMode="numeric" aria-label="最远提前天数" value={draft.maxLeadDays} onChange={(event) => setDraft({ ...draft, maxLeadDays: event.target.value })} /> 天
              </span>
            </label>
            <label className="settings-row">
              <span className="sr-label">汇率过期</span>
              <span className="sr-value">
                <input className="input input--compact" type="number" min="1" inputMode="numeric" aria-label="汇率过期小时" value={draft.staleHours} onChange={(event) => setDraft({ ...draft, staleHours: event.target.value })} /> 小时
              </span>
            </label>
          </div>
          <div className="settings-save-footer subscription-settings__footer">
            <div>
              {error ? <p className="settings-save-footer__message settings-save-footer__message--error" role="alert">{error}</p> : null}
              {notice ? <p className="settings-save-footer__message settings-save-footer__message--success" role="status">{notice}</p> : null}
            </div>
            <button className="btn md primary" type="submit" disabled={submitting}>
              {submitting ? '保存中…' : '保存订阅配置'}
            </button>
          </div>
        </form>
      </section>

      <SubscriptionBudgetSection budgets={state.monthlyBudgets} budgetsError={budgetsError} baseCurrency={baseCurrency} onSaved={reloadBudgets} />
    </div>
  )
}
