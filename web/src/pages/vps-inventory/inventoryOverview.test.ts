import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { SubscriptionRecord } from '../../lib/types'
import { vpsAssetFixture } from '../dashboard/dashboardTestFixtures'
import { buildInventoryOverview, OVERVIEW_RENEWAL_LIMIT, type OverviewRow } from './inventoryOverview'

function row(index: number, renewAt: string | null, overrides: Parameters<typeof vpsAssetFixture>[0] = {}): OverviewRow {
  return {
    vps: vpsAssetFixture({ vps_id: `vps_${index}`, display_name: `VPS ${index}`, ...overrides }),
    subscription: renewAt === null ? null : ({ subscription_id: `sub_${index}`, renew_at: renewAt } as SubscriptionRecord),
  }
}

describe('buildInventoryOverview', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    // 剩余天数按 UTC 日历日：用 UTC 时刻，避免在 UTC+11 以东的机器上落到前一天。
    vi.setSystemTime(new Date('2026-09-29T10:00:00Z'))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('orders renewals by remaining days, overdue first, and bounds the schedule', () => {
    const rows = [
      row(1, '2026-12-01'),
      row(2, '2026-09-20'),
      row(3, '2026-10-01'),
      row(4, null),
      row(5, ''),
      row(6, '2026-10-05'),
      row(7, '2026-11-01'),
      row(8, '2027-01-01'),
      row(9, '2026-09-29'),
    ]
    const overview = buildInventoryOverview(rows, 'ready')
    expect(overview.total).toBe(9)
    expect(overview.renewals).toEqual({
      status: 'ready',
      items: [
        { vpsId: 'vps_2', name: 'VPS 2', date: '2026-09-20', days: -9 },
        { vpsId: 'vps_9', name: 'VPS 9', date: '2026-09-29', days: 0 },
        { vpsId: 'vps_3', name: 'VPS 3', date: '2026-10-01', days: 2 },
        { vpsId: 'vps_6', name: 'VPS 6', date: '2026-10-05', days: 6 },
        { vpsId: 'vps_7', name: 'VPS 7', date: '2026-11-01', days: 33 },
      ],
      hidden: 2,
      undated: 2,
    })
    expect(OVERVIEW_RENEWAL_LIMIT).toBe(5)
  })

  it('reports loading and failed subscription evidence instead of an empty schedule', () => {
    expect(buildInventoryOverview([row(1, null)], 'loading').renewals).toEqual({ status: 'loading' })
    expect(buildInventoryOverview([row(1, null)], 'error').renewals).toEqual({ status: 'error' })
  })

  it('counts renewal intent, usage tags, providers and regions with labelled fallbacks', () => {
    const rows = [
      row(1, null, { renewal_decision: 'keep', usage_tags: ['代理', '构建', '代理'], provider_name: 'Vultr', country: 'jp' }),
      row(2, null, { renewal_decision: 'cancel', usage_tags: ['代理'], provider_name: 'Vultr', country: 'JP' }),
      row(3, null, { renewal_decision: 'keep', usage_tags: [' '], provider_name: '  ', country: '' }),
    ]
    const overview = buildInventoryOverview(rows, 'ready')
    expect(overview.renewalDecisions.map((item) => [item.key, item.count])).toEqual([['value:keep', 2], ['value:cancel', 1]])
    // 多选用途按标签计数，同一台的重复标签只算一次，空标签归入“未标注用途”。
    expect(overview.usages.map((item) => [item.label, item.count])).toEqual([['代理', 2], ['构建', 1], ['未标注用途', 1]])
    expect(overview.providers.map((item) => [item.label, item.count])).toEqual([['Vultr', 2], ['未填写', 1]])
    expect(overview.regions.map((item) => item.count)).toEqual([2, 1])
    expect(overview.regions[1]?.label).toBe('未填写')
  })

  it('collapses a long distribution tail into a counted remainder while keeping every asset counted', () => {
    const providers = ['A', 'A', 'B', 'C', 'D', 'E', 'F']
    const overview = buildInventoryOverview(providers.map((name, index) => row(index, null, { provider_name: name })), 'ready')
    expect(overview.providers.map((item) => [item.label, item.count])).toEqual([['A', 2], ['B', 1], ['C', 1], ['D', 1], ['其余 2 类', 2]])
    expect(overview.providers.reduce((sum, item) => sum + item.count, 0)).toBe(providers.length)
  })

  it('keeps user-entered 其他 and aggregate-like values distinct from the remainder', () => {
    const tags = [['其他'], ['其他'], ['__other'], ['aggregate:rest'], ['B'], ['C'], ['D']]
    const overview = buildInventoryOverview(tags.map((usage_tags, index) => row(index, null, { usage_tags })), 'ready')
    const keys = overview.usages.map((item) => item.key)
    expect(new Set(keys).size).toBe(keys.length)
    expect(overview.usages[0]).toEqual({ key: 'value:其他', label: '其他', count: 2 })
    expect(overview.usages.at(-1)).toEqual({ key: 'aggregate:rest', label: '其余 2 类', count: 2 })
  })

  it('keeps a five-item distribution intact rather than hiding a single item behind a remainder', () => {
    const overview = buildInventoryOverview(['A', 'B', 'C', 'D', 'E'].map((name, index) => row(index, null, { provider_name: name })), 'ready')
    expect(overview.providers.map((item) => item.label)).toEqual(['A', 'B', 'C', 'D', 'E'])
  })
})
