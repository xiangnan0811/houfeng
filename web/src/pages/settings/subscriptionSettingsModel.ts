import { ApiError } from '../../lib/api'
import type {
  SubscriptionCostSettings,
  SubscriptionCostSettingsUpdateInput,
  SubscriptionMonthlyBudgetBulkScope,
  SubscriptionMonthlyBudgetRecord,
  UpsertSubscriptionMonthlyBudgetInput,
} from '../../lib/types'

export type SettingsDraft = {
  baseCurrency: string
  provider: string
  fixerApiKey: string
  reminderOffsets: string
  maxLeadDays: string
  staleHours: string
}

export type BudgetDraft = {
  month: string
  monthlyLimit: string
  warningPct: string
  note: string
}

export type BudgetValues = Omit<UpsertSubscriptionMonthlyBudgetInput, 'budget_month'>

export const INITIAL_SETTINGS_DRAFT: SettingsDraft = {
  baseCurrency: 'CNY',
  provider: 'frankfurter',
  fixerApiKey: '',
  reminderOffsets: '14, 7, 1',
  maxLeadDays: '30',
  staleHours: '36',
}

export const BUDGET_BULK_SCOPE_OPTIONS: Array<{ value: SubscriptionMonthlyBudgetBulkScope; label: string }> = [
  { value: 'all_history', label: '所有时间月预算' },
  { value: 'recent_year', label: '最近一年月预算' },
  { value: 'current_year', label: '今年月预算' },
]

export function currentMonthValue(now: Date = new Date()): string {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

export function emptyBudgetDraft(): BudgetDraft {
  return { month: currentMonthValue(), monthlyLimit: '', warningPct: '80', note: '' }
}

export function describeError(err: unknown, fallback: string): string {
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

export function settingsToDraft(settings: SubscriptionCostSettings | null): SettingsDraft {
  if (!settings) return INITIAL_SETTINGS_DRAFT
  return {
    baseCurrency: settings.base_currency ?? INITIAL_SETTINGS_DRAFT.baseCurrency,
    provider: settings.exchange_rate_provider ?? INITIAL_SETTINGS_DRAFT.provider,
    fixerApiKey: '',
    reminderOffsets: settings.default_reminder_offsets_days?.join(', ') ?? INITIAL_SETTINGS_DRAFT.reminderOffsets,
    maxLeadDays: settings.max_reminder_lead_days != null ? String(settings.max_reminder_lead_days) : INITIAL_SETTINGS_DRAFT.maxLeadDays,
    staleHours: settings.exchange_rate_stale_after_hours != null ? String(settings.exchange_rate_stale_after_hours) : INITIAL_SETTINGS_DRAFT.staleHours,
  }
}

/** 校验并构造成本设置请求；校验失败抛出带中文说明的错误。 */
export function buildSettingsInput(draft: SettingsDraft): SubscriptionCostSettingsUpdateInput {
  const maxLeadDays = Number.parseInt(draft.maxLeadDays, 10)
  const staleHours = Number.parseInt(draft.staleHours, 10)
  const offsets = parseIntegerList(draft.reminderOffsets)
  if (!/^[A-Za-z]{3}$/.test(draft.baseCurrency.trim())) throw new Error('基准货币必须是 3 位代码。')
  if (!Number.isInteger(maxLeadDays) || maxLeadDays <= 0) throw new Error('最远提前提醒天数必须大于 0。')
  if (!Number.isInteger(staleHours) || staleHours <= 0) throw new Error('汇率过期小时必须大于 0。')
  if (offsets.length === 0 || offsets.some((offset) => offset > maxLeadDays)) {
    throw new Error('提醒窗口不能为空，且不能超过最远提前天数。')
  }
  return {
    base_currency: draft.baseCurrency.trim().toUpperCase(),
    exchange_rate_provider: draft.provider,
    // 只有当前选择 Fixer 时才提交 key；切走后隐藏的草稿不能悄悄改写服务端密钥。
    ...(draft.provider === 'fixer' && draft.fixerApiKey.trim() ? { fixer_api_key: draft.fixerApiKey.trim() } : {}),
    default_reminder_offsets_days: offsets,
    max_reminder_lead_days: maxLeadDays,
    exchange_rate_stale_after_hours: staleHours,
  }
}

export function budgetToDraft(budget: SubscriptionMonthlyBudgetRecord): BudgetDraft {
  return {
    month: budget.budget_month?.slice(0, 7) ?? currentMonthValue(),
    monthlyLimit: String(budget.monthly_limit),
    warningPct: String(budget.warning_pct),
    note: budget.note ?? '',
  }
}

export function buildBudgetValues(draft: BudgetDraft, baseCurrency: string): BudgetValues {
  const trimmed = draft.monthlyLimit.trim()
  if (!trimmed) throw new Error('月预算不能为空。')
  const monthlyLimit = Number.parseFloat(trimmed)
  if (!Number.isFinite(monthlyLimit) || monthlyLimit < 0) throw new Error('月预算必须为非负数字。')
  const warningPct = Number.parseInt(draft.warningPct, 10)
  if (!Number.isInteger(warningPct) || warningPct < 1 || warningPct > 100) throw new Error('预警比例必须在 1-100 之间。')
  return { base_currency: baseCurrency, monthly_limit: monthlyLimit, warning_pct: warningPct, note: draft.note.trim() }
}

export function buildMonthlyBudgetInput(draft: BudgetDraft, baseCurrency: string): UpsertSubscriptionMonthlyBudgetInput {
  if (!/^\d{4}-\d{2}$/.test(draft.month.trim())) throw new Error('预算月份必须为 YYYY-MM。')
  return buildBudgetValues(draft, baseCurrency)
}

/** 后续月份沿用最近一次配置：当前生效的是不晚于本月的最新一条。 */
export function effectiveBudgetMonth(budgets: SubscriptionMonthlyBudgetRecord[], month: string = currentMonthValue()): string | null {
  let effective: string | null = null
  for (const budget of budgets) {
    const key = budget.budget_month.slice(0, 7)
    if (key <= month && (effective == null || key > effective)) effective = key
  }
  return effective
}
