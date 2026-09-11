import { describe, expect, it } from 'vitest'

import type { SubscriptionRecord } from '../../lib/types'
import {
  subscriptionCadenceLabel,
  subscriptionDueLabel,
  subscriptionResourceName,
  subscriptionResourceSummary,
} from './vpsDetailResourcePresentation'


function subscription(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return {
    subscription_id: 'sub_001',
    vps_id: 'vps_001',
    price: 12,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    billing_period_unit: 'month',
    billing_period_length: 1,
    monthly_price: 12,
    auto_renew: false,
    auto_renew_cancelled: false,
    renewal_mode: 'manual',
    status: 'active',
    payment_method: 'card',
    note: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    renew_at: null,
    trial_ends_at: null,
    ends_at: null,
    ...overrides,
  }
}

describe('vpsDetailResourcePresentation', () => {
  it.each([
    {
      name: 'non-auto-renewing ends-only subscription',
      overrides: { ends_at: '2026-09-30' },
      expected: '到期 2026-09-30',
    },
    {
      name: 'trial-only subscription',
      overrides: { trial_ends_at: '2026-09-15' },
      expected: '试用到期 2026-09-15',
    },
    {
      name: 'auto-cancelled subscription',
      overrides: { renewal_mode: 'auto_cancelled', ends_at: '2026-09-30', renew_at: '2026-09-01' },
      expected: '到期 2026-09-30',
    },
  ])('keeps the applicable billing date for $name', ({ overrides, expected }) => {
    const item = subscription(overrides)

    expect(subscriptionDueLabel(item)).toBe(expected)
    expect(subscriptionResourceSummary(item)).toContain(expected)
  })

  it('shows both renewal and end dates when a non-auto subscription provides both', () => {
    const item = subscription({ renew_at: '2026-09-01', ends_at: '2026-09-30' })

    expect(subscriptionDueLabel(item)).toBe('续费 2026-09-01 · 到期 2026-09-30')
  })

  it('keeps the true subscription display name instead of genericizing the asset name', () => {
    expect(subscriptionResourceName(subscription({ display_name: '独立账单' }))).toBe('独立账单')
    expect(subscriptionResourceName(subscription({ display_name: '东京边缘' }))).toBe('东京边缘')
    expect(subscriptionResourceName(subscription({ display_name: '东京边缘 订阅' }))).toBe('东京边缘 订阅')
    expect(subscriptionResourceName(subscription({ display_name: '' }))).toBe('未命名订阅')
  })

  it('normalizes cadence to /月 for a single period and /N个月 for longer months', () => {
    expect(subscriptionCadenceLabel(subscription())).toBe('/月')
    const legacy = subscription({ billing_months: 1 })
    delete legacy.billing_period_unit
    delete legacy.billing_period_length
    expect(subscriptionCadenceLabel(legacy)).toBe('/月')
    expect(subscriptionCadenceLabel(subscription({ billing_period_length: 2, billing_months: 2 }))).toBe('/2个月')
    expect(subscriptionCadenceLabel(subscription({ billing_period_unit: 'year', billing_period_length: 1 }))).toBe('/年')
  })

  it('formats planned-cancellation dates structurally for primary and extra subscriptions', () => {
    const primary = subscription({
      renew_at: '2026-10-15',
      ends_at: '2026-11-01',
      trial_ends_at: '2026-09-15',
    })
    const extra = subscription({
      subscription_id: 'sub_extra',
      display_name: '附加账单',
      renew_at: '2026-12-01',
      trial_ends_at: '2026-10-01',
    })
    const planned = { plannedCancellation: true as const }

    expect(subscriptionDueLabel(primary)).toBe('续费 2026-10-15 · 到期 2026-11-01 · 试用到期 2026-09-15')
    expect(subscriptionDueLabel(primary, planned)).toBe('登记续费日 2026-10-15 · 账期到期 2026-11-01 · 试用到期 2026-09-15')
    expect(subscriptionDueLabel(primary, planned)).not.toContain('试用账期到期')
    expect(subscriptionDueLabel(primary, planned)).not.toContain('已取消')
    expect(subscriptionDueLabel(extra, planned)).toBe('登记续费日 2026-12-01 · 试用到期 2026-10-01')
    expect(subscriptionDueLabel(extra, planned)).not.toContain('试用账期到期')
  })

})
