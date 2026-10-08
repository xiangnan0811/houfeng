import type { ExchangeRatePairStatus, ExchangeRateStatus } from '../../lib/types'

export type KnownMonthlyAmount = {
  allUnknown: boolean
  completeZero: boolean
  unknownCount: number
  total: number
}

export type ExchangeRateDiagnostic = {
  label: string
  detail: string
}

export type ExchangeRateNotice = {
  tone: 'status' | 'error'
  text: string
  diagnostics?: ExchangeRateDiagnostic[]
}

const PENDING_AMOUNT = '金额待核对'

export function exchangeRateRefreshActive(items: readonly ExchangeRatePairStatus[]): boolean {
  return items.some((item) => item.refresh_status === 'queued' || item.refresh_status === 'running')
}

export function exchangeRatePairKey(item: ExchangeRatePairStatus): string {
  return `${item.provider}\0${item.base_currency}\0${item.quote_currency}`
}

/** A refresh succeeded when a pair left the in-flight set and came back idle. */
export function exchangeRateRefreshSucceeded(
  previous: readonly ExchangeRatePairStatus[],
  next: readonly ExchangeRatePairStatus[],
): boolean {
  if (!exchangeRateRefreshActive(previous) || exchangeRateRefreshActive(next)) return false
  const active = new Set(
    previous.filter((item) => item.refresh_status === 'queued' || item.refresh_status === 'running').map(exchangeRatePairKey),
  )
  return next.some((item) => active.has(exchangeRatePairKey(item)) && item.refresh_status === 'idle')
}

export function exchangeRateStatusLabel(status: ExchangeRateStatus | undefined): string | null {
  if (status === 'stale') return '汇率过期'
  if (status === 'missing') return '缺少汇率'
  return null
}

export function exchangeRateNeedsAttention(status: ExchangeRateStatus | undefined): boolean {
  return status === 'stale' || status === 'missing'
}

function quoteList(items: readonly ExchangeRatePairStatus[]): string {
  return items.map((item) => item.quote_currency).filter(Boolean).join('、')
}

/** Page-level status for missing, stale, in-flight, and failed pairs. */
export function describeExchangeRateStatus(items: readonly ExchangeRatePairStatus[]): ExchangeRateNotice | null {
  const running = items.filter((item) => item.refresh_status === 'queued' || item.refresh_status === 'running')
  const failed = items.filter((item) => item.refresh_status === 'failed')
  const missing = items.filter((item) => (
    item.rate_status === 'missing'
    && item.refresh_status !== 'queued'
    && item.refresh_status !== 'running'
    && item.refresh_status !== 'failed'
  ))
  const stale = items.filter((item) => item.rate_status === 'stale')
  const parts: string[] = []
  if (running.length > 0) parts.push(`补取中 ${running.length} 项`)
  const diagnostics: ExchangeRateDiagnostic[] = []
  if (failed.length > 0) {
    const quotes = quoteList(failed)
    parts.push(quotes ? `补取失败：${quotes}` : `补取失败 ${failed.length} 项`)
    for (const item of failed) {
      const detail = item.error_summary?.trim()
      if (!detail) continue
      diagnostics.push({ label: item.quote_currency.trim() || '汇率', detail })
    }
  }
  if (missing.length > 0) {
    const quotes = quoteList(missing)
    parts.push(quotes ? `缺少汇率：${quotes}` : `缺少汇率 ${missing.length} 项`)
  }
  if (stale.length > 0) {
    const quotes = quoteList(stale)
    parts.push(quotes ? `汇率过期：${quotes}` : `汇率过期 ${stale.length} 项`)
  }
  if (parts.length === 0) return null
  return {
    tone: failed.length > 0 ? 'error' : 'status',
    text: parts.join('；'),
    ...(diagnostics.length > 0 ? { diagnostics } : {}),
  }
}

export function knownMonthlyAmount(input: {
  activeSubscriptionCount: number
  totalMonthlyCost: number
  unknownCount: number
  rows?: ReadonlyArray<{ monthly_price_base?: number | null }> | undefined
}): KnownMonthlyAmount {
  const unknownCount = Number.isFinite(input.unknownCount) ? Math.max(0, input.unknownCount) : 0
  const total = Number.isFinite(input.totalMonthlyCost) ? input.totalMonthlyCost : 0
  const rowKnown = input.rows?.some((row) => row.monthly_price_base != null) ?? false
  const hasKnownAmount = rowKnown || total !== 0 || (unknownCount === 0 && input.activeSubscriptionCount > 0)
  const allUnknown = unknownCount > 0 && !hasKnownAmount
  const completeZero = !allUnknown && unknownCount === 0 && input.activeSubscriptionCount === 0 && total === 0 && !rowKnown
  return { allUnknown, completeZero, unknownCount, total }
}

export function knownAmountNote(amount: KnownMonthlyAmount): string | null {
  if (amount.allUnknown || amount.unknownCount === 0) return null
  return `已知金额小计（另有 ${amount.unknownCount} 项待核对）`
}

export function knownShareCaption(amount: KnownMonthlyAmount): string | null {
  if (amount.unknownCount === 0) return null
  if (amount.allUnknown) return PENDING_AMOUNT
  return `${knownAmountNote(amount)}，占比仅指已知金额`
}

export function pendingAmountLabel(): string {
  return PENDING_AMOUNT
}
