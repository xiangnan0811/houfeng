import { afterEach, describe, expect, it } from 'vitest'

import type { SubscriptionRecord, VPSTimeline } from '../../lib/types'
import { archiveClosingSubscriptions, archiveServiceSpanLabel, archiveTimelineEntries } from './archivePageHelpers'

const originalTZ = process.env.TZ

afterEach(() => {
  process.env.TZ = originalTZ
})

describe('archiveServiceSpanLabel', () => {
  it('counts calendar months and remaining days between billing start and archive', () => {
    expect(archiveServiceSpanLabel('2025-11-02', '2026-09-28')).toBe('10 个月 26 天')
    expect(archiveServiceSpanLabel('2026-03-15', '2026-04-15')).toBe('1 个月')
    expect(archiveServiceSpanLabel('2026-03-15', '2026-03-27')).toBe('12 天')
  })

  it('clamps month-end starts instead of overflowing into the following month', () => {
    expect(archiveServiceSpanLabel('2026-01-31', '2026-03-01')).toBe('1 个月 1 天')
    expect(archiveServiceSpanLabel('2026-01-31', '2026-02-28')).toBe('28 天')
  })

  it('switches to years for long-lived servers', () => {
    expect(archiveServiceSpanLabel('2023-06-01', '2026-06-01')).toBe('3 年')
    expect(archiveServiceSpanLabel('2024-06-01', '2026-09-20')).toBe('2 年 3 个月')
  })

  it('counts calendar days across a DST-shortened day and ignores time of day', () => {
    process.env.TZ = 'America/New_York'
    expect(archiveServiceSpanLabel('2026-03-08', '2026-03-09')).toBe('1 天')
    expect(archiveServiceSpanLabel('2026-03-01T23:30:00-05:00', '2026-03-02T00:10:00-05:00')).toBe('1 天')
  })

  it('returns null for missing, invalid or reversed ranges', () => {
    expect(archiveServiceSpanLabel(null, '2026-01-01')).toBeNull()
    expect(archiveServiceSpanLabel('not-a-date', '2026-01-01')).toBeNull()
    expect(archiveServiceSpanLabel('2026-02-01', '2026-01-01')).toBeNull()
  })
})

function subscription(id: string, currency: string, monthly: number, started: string, renew: string): SubscriptionRecord {
  return {
    subscription_id: id, vps_id: 'v', price: monthly, currency, billing_cycle: 'monthly', billing_months: 1,
    billing_period_unit: 'month', billing_period_length: 1, monthly_price: monthly, started_at: started, renew_at: renew,
    auto_renew: false, auto_renew_cancelled: true, renewal_mode: 'auto_cancelled', status: 'cancelled', payment_method: '',
    note: '', created_at: started, updated_at: started,
  }
}

describe('archiveClosingSubscriptions', () => {
  it('keeps every currency whose billing period covers the archive day', () => {
    const plan = subscription('plan', 'USD', 24, '2026-01-01', '2026-06-01')
    const addon = subscription('addon', 'CNY', 10, '2026-04-01', '2026-07-01')
    const promo = subscription('promo', 'USD', 3, '2025-12-01', '2026-01-01')
    expect(archiveClosingSubscriptions([promo, plan, addon], '2026-05-09').map((row) => row.subscription_id)).toEqual(['plan', 'addon'])
  })

  it('falls back to the most recent row per currency when nothing covers the archive day', () => {
    const early = subscription('early', 'USD', 30, '2025-01-01', '2025-06-01')
    const late = subscription('late', 'USD', 5, '2025-06-01', '2025-12-01')
    const cny = subscription('cny', 'CNY', 10, '2025-03-01', '2025-09-01')
    expect(archiveClosingSubscriptions([early, late, cny], '2026-05-09').map((row) => row.subscription_id)).toEqual(['late', 'cny'])
  })
})

describe('archiveTimelineEntries', () => {
  function priceTimeline(overrides: Partial<VPSTimeline['price_histories'][number]>): VPSTimeline {
    return {
      vps_id: 'v', renewal_decisions: [], ip_histories: [], spec_snapshots: [], experience_logs: [],
      price_histories: [{
        price_history_id: 'ph', subscription_id: 'sub', vps_id: 'v', from_price: 120, to_price: 144, from_currency: 'USD', to_currency: 'USD',
        from_billing_cycle: 'yearly', to_billing_cycle: 'yearly', from_billing_months: 12, to_billing_months: 12,
        from_billing_period_unit: 'year', to_billing_period_unit: 'year', from_billing_period_length: 1, to_billing_period_length: 1,
        from_monthly_price: 10, to_monthly_price: 12, from_auto_renew: true, to_auto_renew: true, from_auto_renew_cancelled: false,
        to_auto_renew_cancelled: false, from_status: 'active', to_status: 'active', changed_at: '2026-02-01T00:00:00Z', created_at: '2026-02-01T00:00:00Z',
        ...overrides,
      }],
    }
  }

  it('keeps the billed period price and the normalized monthly price for yearly plans', () => {
    const [entry] = archiveTimelineEntries(priceTimeline({}))
    expect(entry?.title).toBe('USD 120.00 每 1 年 → USD 144.00 每 1 年')
    expect(entry?.detail).toContain('折合 USD 10.00 → USD 12.00 /月')
  })

  it('labels weekly plans by their real period instead of the legacy one-month bucket', () => {
    const [entry] = archiveTimelineEntries(priceTimeline({
      from_price: 2, to_price: 3, from_billing_cycle: 'weekly', to_billing_cycle: 'weekly', from_billing_months: 1, to_billing_months: 1,
      from_billing_period_unit: 'week', to_billing_period_unit: 'week', from_monthly_price: 8.67, to_monthly_price: 13,
    }))
    expect(entry?.title).toBe('USD 2.00 每 1 周 → USD 3.00 每 1 周')
    expect(entry?.detail).toContain('折合 USD 8.67 → USD 13.00 /月')
  })
})
