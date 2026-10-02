import { describe, expect, it } from 'vitest'

import type { SubscriptionMonthlyBudgetRecord } from '../../lib/types'
import {
  buildBudgetValues,
  buildSettingsInput,
  effectiveBudgetMonth,
  INITIAL_SETTINGS_DRAFT,
  settingsToDraft,
} from './subscriptionSettingsModel'

function budget(month: string): SubscriptionMonthlyBudgetRecord {
  return { budget_month: `${month}-01`, base_currency: 'CNY', monthly_limit: 100, warning_pct: 80, note: '', created_at: '', updated_at: '' }
}

describe('effectiveBudgetMonth', () => {
  it('picks the latest budget that is not later than the current month', () => {
    const budgets = [budget('2026-12'), budget('2026-07'), budget('2026-10'), budget('2025-08')]
    expect(effectiveBudgetMonth(budgets, '2026-10')).toBe('2026-10')
    expect(effectiveBudgetMonth(budgets, '2026-09')).toBe('2026-07')
    expect(effectiveBudgetMonth(budgets, '2025-01')).toBeNull()
    expect(effectiveBudgetMonth([], '2026-10')).toBeNull()
  })
})

describe('buildSettingsInput', () => {
  it('normalizes currency and offsets and keeps an empty Fixer key out of the payload', () => {
    expect(buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, baseCurrency: ' usd ', reminderOffsets: '14, 7,1' })).toEqual({
      base_currency: 'USD',
      exchange_rate_provider: 'frankfurter',
      default_reminder_offsets_days: [14, 7, 1],
      max_reminder_lead_days: 30,
      exchange_rate_stale_after_hours: 36,
    })
    expect(buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, provider: 'fixer', fixerApiKey: ' key ' }).fixer_api_key).toBe('key')
    // 切走 Fixer 后隐藏的 key 草稿不提交。
    expect(buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, provider: 'frankfurter', fixerApiKey: 'secret' })).not.toHaveProperty('fixer_api_key')
  })

  it('rejects invalid currency, lead days and reminder windows', () => {
    expect(() => buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, baseCurrency: 'RMBX' })).toThrow('基准货币必须是 3 位代码。')
    expect(() => buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, maxLeadDays: '0' })).toThrow('最远提前提醒天数必须大于 0。')
    expect(() => buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, staleHours: 'x' })).toThrow('汇率过期小时必须大于 0。')
    expect(() => buildSettingsInput({ ...INITIAL_SETTINGS_DRAFT, reminderOffsets: '45' })).toThrow('提醒窗口不能为空，且不能超过最远提前天数。')
  })

  it('round-trips saved settings into a readable draft', () => {
    expect(settingsToDraft({
      base_currency: 'CNY', exchange_rate_provider: 'fixer', fixer_configured: true,
      default_reminder_offsets_days: [14, 7, 1], max_reminder_lead_days: 30, exchange_rate_stale_after_hours: 36,
    }).reminderOffsets).toBe('14, 7, 1')
  })
})

describe('buildBudgetValues', () => {
  it('validates amount and warning ratio', () => {
    expect(buildBudgetValues({ month: '2026-07', monthlyLimit: '120.5', warningPct: '85', note: ' 增长 ' }, 'CNY'))
      .toEqual({ base_currency: 'CNY', monthly_limit: 120.5, warning_pct: 85, note: '增长' })
    expect(() => buildBudgetValues({ month: '2026-07', monthlyLimit: '', warningPct: '80', note: '' }, 'CNY')).toThrow('月预算不能为空。')
    expect(() => buildBudgetValues({ month: '2026-07', monthlyLimit: '10', warningPct: '0', note: '' }, 'CNY')).toThrow('预警比例必须在 1-100 之间。')
  })
})
