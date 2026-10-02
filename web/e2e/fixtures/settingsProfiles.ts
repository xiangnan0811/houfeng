import type { SubscriptionCostSettings, SubscriptionMonthlyBudgetRecord } from '../../src/lib/types'
import { apiRouteKey, type ApiFixtureProfile } from './contracts'
import { authenticatedProfile } from './profiles'

const SUBSCRIPTION_COST_SETTINGS: SubscriptionCostSettings = {
  base_currency: 'CNY',
  exchange_rate_provider: 'fixer',
  fixer_configured: true,
  fixer_masked_summary: 'fx_****8a2c',
  default_reminder_offsets_days: [14, 7, 1],
  max_reminder_lead_days: 30,
  exchange_rate_stale_after_hours: 36,
}

// 月预算按月份倒序，覆盖有备注、无备注与不同预警比例。
const MONTHLY_BUDGETS: SubscriptionMonthlyBudgetRecord[] = [
  ['2026-10', 360, 85, '新增法兰克福镜像'],
  ['2026-07', 320, 80, ''],
  ['2026-04', 300, 80, '季度复核后下调'],
  ['2026-01', 340, 90, ''],
  ['2025-10', 280, 80, ''],
  ['2025-08', 260, 75, '首次配置'],
].map(([month, limit, warning, note]) => ({
  budget_month: `${month}-01`,
  base_currency: 'CNY',
  monthly_limit: limit as number,
  warning_pct: warning as number,
  note: note as string,
  created_at: '2026-09-20T08:00:00Z',
  updated_at: '2026-09-20T08:00:00Z',
}))

// 浏览器时钟固定在上海 2026-10-02，“当前生效”不随运行日期漂移；用例须 page.clock.setFixedTime(SETTINGS_NOW)。
export const SETTINGS_NOW = new Date('2026-10-02T04:00:00Z')

/** 设置 › 订阅：已配置 Fixer、提醒窗口与六个月预算。 */
export function settingsSubscriptionsProfile(options: { budgets?: SubscriptionMonthlyBudgetRecord[] } = {}): ApiFixtureProfile {
  return authenticatedProfile({
    [apiRouteKey('GET', '/api/subscriptions/settings')]: { status: 200, body: SUBSCRIPTION_COST_SETTINGS },
    [apiRouteKey('GET', '/api/subscription-monthly-budgets')]: { status: 200, body: options.budgets ?? MONTHLY_BUDGETS },
  })
}
