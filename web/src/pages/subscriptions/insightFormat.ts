import { formatMoney } from '../../lib/format'

export function money(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '-'
  return formatMoney(value, currency)
}
