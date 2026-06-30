import type { BillingPeriodUnit, RenewalMode, SubscriptionRecord } from './types'

export const CUSTOM_OPTION_VALUE = '__custom'

export type LabeledOption = {
  value: string
  label: string
  icon?: string
}

export const COMMON_COUNTRY_OPTIONS: LabeledOption[] = [
  { value: 'US', label: '美国 / United States', icon: 'US' },
  { value: 'HK', label: '香港 / Hong Kong', icon: 'HK' },
  { value: 'SG', label: '新加坡 / Singapore', icon: 'SG' },
  { value: 'JP', label: '日本 / Japan', icon: 'JP' },
  { value: 'TW', label: '台湾 / Taiwan', icon: 'TW' },
  { value: 'KR', label: '韩国 / South Korea', icon: 'KR' },
  { value: 'GB', label: '英国 / United Kingdom', icon: 'GB' },
  { value: 'DE', label: '德国 / Germany', icon: 'DE' },
  { value: 'FR', label: '法国 / France', icon: 'FR' },
  { value: 'NL', label: '荷兰 / Netherlands', icon: 'NL' },
  { value: 'CA', label: '加拿大 / Canada', icon: 'CA' },
  { value: 'AU', label: '澳大利亚 / Australia', icon: 'AU' },
  { value: 'CN', label: '中国大陆 / Mainland China', icon: 'CN' },
  { value: 'MY', label: '马来西亚 / Malaysia', icon: 'MY' },
  { value: 'TH', label: '泰国 / Thailand', icon: 'TH' },
  { value: 'IN', label: '印度 / India', icon: 'IN' },
  { value: 'BR', label: '巴西 / Brazil', icon: 'BR' },
]

export const COMMON_CURRENCY_OPTIONS: LabeledOption[] = [
  { value: 'USD', label: '美元 USD', icon: '$' },
  { value: 'GBP', label: '英镑 GBP', icon: 'GBP' },
  { value: 'CNY', label: '人民币 CNY', icon: '¥' },
  { value: 'AUD', label: '澳元 AUD', icon: 'A$' },
  { value: 'CAD', label: '加拿大元 CAD', icon: 'C$' },
  { value: 'EUR', label: '欧元 EUR', icon: 'EUR' },
  { value: 'HKD', label: '港币 HKD', icon: 'HK$' },
  { value: 'SGD', label: '新加坡元 SGD', icon: 'S$' },
  { value: 'TWD', label: '新台币 TWD', icon: 'NT$' },
  { value: 'NZD', label: '新西兰元 NZD', icon: 'NZ$' },
  { value: 'MYR', label: '马来西亚林吉特 MYR', icon: 'RM' },
]

export const COMMON_PAYMENT_METHOD_OPTIONS: LabeledOption[] = [
  { value: 'PayPal', label: 'PayPal', icon: 'PP' },
  { value: 'Alipay', label: 'Alipay', icon: 'Ali' },
  { value: 'WeChat', label: 'WeChat', icon: 'Wx' },
  { value: 'Credit Card', label: 'Credit Card', icon: 'Card' },
  { value: 'USDT', label: 'USDT', icon: 'USDT' },
  { value: 'Bonus', label: 'Bonus / 余额', icon: 'Bonus' },
]

export const BILLING_PERIOD_UNIT_OPTIONS: Array<LabeledOption & { value: BillingPeriodUnit }> = [
  { value: 'day', label: '天', icon: 'D' },
  { value: 'week', label: '周', icon: 'W' },
  { value: 'month', label: '月', icon: 'M' },
  { value: 'year', label: '年', icon: 'Y' },
]

export const RENEWAL_MODE_OPTIONS: Array<LabeledOption & { value: RenewalMode }> = [
  { value: 'auto', label: '自动续费', icon: 'Auto' },
  { value: 'manual', label: '手动续费', icon: 'Manual' },
  { value: 'auto_cancelled', label: '已取消自动续费', icon: 'Off' },
  { value: 'lottery', label: '抽奖', icon: 'Lottery' },
  { value: 'gift', label: '赠送', icon: 'Gift' },
  { value: 'bonus', label: 'Bonus/余额抵扣', icon: 'Bonus' },
  { value: 'other', label: '其他', icon: 'Other' },
]

export const VALIDITY_EXTENSION_SOURCE_OPTIONS: LabeledOption[] = [
  { value: 'compensation', label: '故障补偿', icon: 'Comp' },
  { value: 'activity', label: '商家活动', icon: 'Act' },
  { value: 'discount_purchase', label: '优惠购买时长', icon: 'Deal' },
  { value: 'manual_adjustment', label: '手动修正', icon: 'Edit' },
  { value: 'other', label: '其他', icon: 'Other' },
]

export function optionSelectValue(value: string, options: readonly LabeledOption[]): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return options.some((option) => option.value.toLowerCase() === trimmed.toLowerCase())
    ? options.find((option) => option.value.toLowerCase() === trimmed.toLowerCase())?.value ?? trimmed
    : CUSTOM_OPTION_VALUE
}

export function displayOption(option: LabeledOption): string {
  return option.icon ? `${option.icon} · ${option.label}` : option.label
}

export function normalizeCountry(value: string): string {
  return value.trim().toUpperCase()
}

export function countryLabel(value?: string | null): string {
  const trimmed = (value ?? '').trim()
  if (!trimmed) return '未填写'
  const option = COMMON_COUNTRY_OPTIONS.find((item) => item.value.toLowerCase() === trimmed.toLowerCase())
  return option ? option.label : trimmed
}

export function countryOptionsWithExisting(existingCountries: string[]): LabeledOption[] {
  const existing = existingCountries
    .map(normalizeCountry)
    .filter(Boolean)
    .filter((value, index, array) => array.indexOf(value) === index)
    .filter((value) => !COMMON_COUNTRY_OPTIONS.some((option) => option.value === value))
    .map((value) => ({ value, label: `已有：${value}`, icon: value }))
  return [...COMMON_COUNTRY_OPTIONS, ...existing]
}

export function normalizeCurrency(value: string): string {
  return value.trim().toUpperCase()
}

export function normalizePaymentMethod(value: string): string {
  return value.trim()
}

export function normalizeBillingPeriodUnit(value: string | undefined | null): BillingPeriodUnit {
  if (value === 'day' || value === 'week' || value === 'month' || value === 'year') return value
  return 'month'
}

export function normalizeRenewalMode(value: string | undefined | null): RenewalMode {
  if (
    value === 'auto' ||
    value === 'manual' ||
    value === 'auto_cancelled' ||
    value === 'lottery' ||
    value === 'gift' ||
    value === 'bonus' ||
    value === 'other'
  ) {
    return value
  }
  return 'manual'
}

export function renewalModeFromLegacy(subscription: Pick<SubscriptionRecord, 'renewal_mode' | 'auto_renew' | 'auto_renew_cancelled'>): RenewalMode {
  const normalized = normalizeRenewalMode(subscription.renewal_mode)
  if (subscription.renewal_mode) return normalized
  if (subscription.auto_renew_cancelled) return 'auto_cancelled'
  if (subscription.auto_renew) return 'auto'
  return 'manual'
}

export function legacyFlagsFromRenewalMode(mode: RenewalMode): { auto_renew: boolean; auto_renew_cancelled: boolean } {
  if (mode === 'auto') return { auto_renew: true, auto_renew_cancelled: false }
  if (mode === 'auto_cancelled') return { auto_renew: false, auto_renew_cancelled: true }
  return { auto_renew: false, auto_renew_cancelled: false }
}

export function renewalModeLabel(value?: string | null): string {
  const mode = normalizeRenewalMode(value)
  return RENEWAL_MODE_OPTIONS.find((option) => option.value === mode)?.label ?? value ?? '手动续费'
}

export function billingMonthsFromPeriod(unit: BillingPeriodUnit, length: number): number {
  const safeLength = Number.isFinite(length) && length > 0 ? length : 1
  if (unit === 'year') return safeLength * 12
  if (unit === 'month') return safeLength
  if (unit === 'week') return Math.max(1, Math.ceil((safeLength * 7) / 30))
  if (unit === 'day') return Math.max(1, Math.ceil(safeLength / 30))
  return safeLength
}

export function billingCycleFromPeriod(unit: BillingPeriodUnit, length: number): string {
  const safeLength = Number.isFinite(length) && length > 0 ? length : 1
  if (unit === 'day') return safeLength === 1 ? 'daily' : `${safeLength} days`
  if (unit === 'week') return safeLength === 1 ? 'weekly' : `${safeLength} weeks`
  if (unit === 'month') return safeLength === 1 ? 'monthly' : `${safeLength} months`
  if (unit === 'year') return safeLength === 1 ? 'annual' : `${safeLength} years`
  return ''
}

export function periodLabel(unit?: string | null, length?: number | null, fallbackMonths?: number | null): string {
  const normalizedUnit = normalizeBillingPeriodUnit(unit)
  const normalizedLength =
    typeof length === 'number' && Number.isFinite(length) && length > 0
      ? length
      : fallbackMonths && fallbackMonths > 0
        ? fallbackMonths
        : 1
  if (!unit && fallbackMonths && fallbackMonths > 0) {
    return fallbackMonths === 1 ? '每 1 个月' : `每 ${fallbackMonths} 个月`
  }
  const unitLabel = BILLING_PERIOD_UNIT_OPTIONS.find((option) => option.value === normalizedUnit)?.label ?? '月'
  return `每 ${normalizedLength} ${unitLabel}`
}
