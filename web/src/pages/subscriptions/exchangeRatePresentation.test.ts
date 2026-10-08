import { describe, expect, it } from 'vitest'

import type { ExchangeRatePairStatus } from '../../lib/types'
import {
  describeExchangeRateStatus,
  exchangeRateRefreshSucceeded,
  knownAmountNote,
  knownMonthlyAmount,
  knownShareCaption,
} from './exchangeRatePresentation'

function pair(patch: Partial<ExchangeRatePairStatus> = {}): ExchangeRatePairStatus {
  return {
    provider: 'frankfurter',
    base_currency: 'CNY',
    quote_currency: 'USD',
    rate_status: 'missing',
    refresh_status: 'idle',
    attempt_count: 0,
    ...patch,
  }
}

describe('exchange rate refresh status', () => {
  it('describes missing, stale, in-flight, and failed pairs without claiming the refresh finished', () => {
    expect(describeExchangeRateStatus([
      pair({ refresh_status: 'running', attempt_count: 1 }),
      pair({ quote_currency: 'EUR', rate_status: 'stale' }),
      pair({ quote_currency: 'JPY', refresh_status: 'failed', attempt_count: 3, error_summary: 'timeout' }),
    ])).toEqual({
      tone: 'error',
      text: '补取中 1 项；补取失败：JPY；汇率过期：EUR',
      diagnostics: [{ label: 'JPY', detail: 'timeout' }],
    })
    expect(describeExchangeRateStatus([
      pair({ quote_currency: 'USD', refresh_status: 'failed', attempt_count: 1 }),
    ])).toEqual({
      tone: 'error',
      text: '补取失败：USD',
    })
  })

  it('treats only an in-flight pair that returns idle as success', () => {
    const running = [pair({ refresh_status: 'queued', attempt_count: 1 })]
    expect(exchangeRateRefreshSucceeded([], running)).toBe(false)
    expect(exchangeRateRefreshSucceeded(running, [pair({ refresh_status: 'running', attempt_count: 1 })])).toBe(false)
    expect(exchangeRateRefreshSucceeded(running, [pair({ refresh_status: 'failed', attempt_count: 1 })])).toBe(false)
    expect(exchangeRateRefreshSucceeded(running, [pair({ rate_status: 'fresh', refresh_status: 'idle', attempt_count: 1 })])).toBe(true)
    expect(exchangeRateRefreshSucceeded([pair({ rate_status: 'fresh' })], [pair({ rate_status: 'fresh' })])).toBe(false)
  })
})

describe('known monthly amounts', () => {
  it('distinguishes a complete zero, a true zero, a known subtotal, and an all-unknown total', () => {
    const completeZero = knownMonthlyAmount({ activeSubscriptionCount: 0, totalMonthlyCost: 0, unknownCount: 0, rows: [] })
    expect(completeZero).toMatchObject({ allUnknown: false, completeZero: true, total: 0 })
    expect(knownAmountNote(completeZero)).toBeNull()

    const trueZero = knownMonthlyAmount({
      activeSubscriptionCount: 1,
      totalMonthlyCost: 0,
      unknownCount: 0,
      rows: [{ monthly_price_base: 0 }],
    })
    expect(trueZero).toMatchObject({ allUnknown: false, completeZero: false, total: 0 })
    expect(knownAmountNote(trueZero)).toBeNull()

    const partial = knownMonthlyAmount({
      activeSubscriptionCount: 2,
      totalMonthlyCost: 40,
      unknownCount: 1,
      rows: [{ monthly_price_base: 40 }, { monthly_price_base: null }],
    })
    expect(partial.allUnknown).toBe(false)
    expect(knownAmountNote(partial)).toBe('已知金额小计（另有 1 项待核对）')
    expect(knownShareCaption(partial)).toBe('已知金额小计（另有 1 项待核对），占比仅指已知金额')

    const allUnknown = knownMonthlyAmount({
      activeSubscriptionCount: 2,
      totalMonthlyCost: 0,
      unknownCount: 2,
      rows: [{ monthly_price_base: null }, { monthly_price_base: null }],
    })
    expect(allUnknown.allUnknown).toBe(true)
    expect(knownAmountNote(allUnknown)).toBeNull()
    expect(knownShareCaption(allUnknown)).toBe('金额待核对')
  })
})
