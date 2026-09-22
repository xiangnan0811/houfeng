import { describe, expect, it } from 'vitest'
import type { SubscriptionSeriesPoint } from '../../lib/types'
import {
  buildContiguousPath,
  buildDifferenceAreas,
  buildTrendPoints,
  computeAdaptiveYDomain,
  computeXTickIndices,
  findNearestMonthIndex,
  formatTickValue,
  getReadoutFacts,
  type TrendPoint,
} from './budgetCostTrend'

describe('budgetCostTrend math and domain logic', () => {
  describe('computeAdaptiveYDomain', () => {
    it('produces non-zero yMin and visible variation for positive clustered series', () => {
      // Clustered positive wave (e.g. 980 to 1320)
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-10', monthly_cost: 980, budget_limit: 1200, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-11', monthly_cost: 1100, budget_limit: 1200, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-12', monthly_cost: 1320, budget_limit: 1200, renewal_count: 0, data_insufficient: false },
        { bucket: '2026-01', monthly_cost: 1050, budget_limit: 1200, renewal_count: 0, data_insufficient: false },
      ]

      const domain = computeAdaptiveYDomain(buckets)
      // Must NOT start at 0
      expect(domain.yMin).toBeGreaterThan(0)
      expect(domain.yMin).toBeLessThanOrEqual(980)
      expect(domain.yMax).toBeGreaterThanOrEqual(1320)
      expect(domain.ticks[0]).toBe(domain.yMin)
      expect(domain.ticks[1]).toBe((domain.yMin + domain.yMax) / 2)
      expect(domain.ticks[2]).toBe(domain.yMax)
    })
    it('provides breathing room on both sides for 110..210 (IR-01)', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 110, budget_limit: null, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 210, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]
      const domain = computeAdaptiveYDomain(buckets)
      // Must NOT snap ceiling exactly to 210 with 0 headroom
      expect(domain.yMin).toBeLessThan(110)
      expect(domain.yMax).toBeGreaterThan(210)
      expect(domain.ticks[0]).toBe(domain.yMin)
      expect(domain.ticks[1]).toBe((domain.yMin + domain.yMax) / 2)
      expect(domain.ticks[2]).toBe(domain.yMax)
    })

    it('produces clean decimal labels without floating-point artifacts for 1.1..2.1 (IR-02)', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 1.1, budget_limit: null, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 2.1, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]
      const domain = computeAdaptiveYDomain(buckets)
      const labels = domain.ticks.map((t) => formatTickValue(t))
      // Must not contain '2.4000000000000004' or similar float noise
      for (const label of labels) {
        expect(label).not.toMatch(/00000000/)
        expect(label.length).toBeLessThanOrEqual(6)
      }
      expect(labels).toEqual(['0.8', '1.6', '2.4'])
    })

    it('handles flat positive series with value-relative padding', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 100, budget_limit: 100, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 100, budget_limit: 100, renewal_count: 0, data_insufficient: false },
      ]

      const domain = computeAdaptiveYDomain(buckets)
      expect(domain.yMin).toBeGreaterThan(0)
      expect(domain.yMin).toBeLessThan(100)
      expect(domain.yMax).toBeGreaterThan(100)
      expect(domain.ticks[1]).toBe(100) // Centered flat line
    })

    it('anchors at zero when data starts at zero or is near zero', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 0, budget_limit: 80, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 60, budget_limit: 80, renewal_count: 0, data_insufficient: false },
      ]

      const domain = computeAdaptiveYDomain(buckets)
      expect(domain.yMin).toBe(0)
      expect(domain.yMax).toBeGreaterThanOrEqual(80)
      expect(domain.ticks[0]).toBe(0)
    })

    it('excludes missing/null budgets from domain computation', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 50, budget_limit: null, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 95, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]

      const domain = computeAdaptiveYDomain(buckets)
      expect(domain.yMax).toBeGreaterThanOrEqual(95)
    })

    it('handles large finite baseline with small delta without stall or precision degradation', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 1000000000006, budget_limit: null, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 1000000000007, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]
      const domain = computeAdaptiveYDomain(buckets)
      expect(domain.yMin).toBeGreaterThan(0)
      expect(domain.yMin).toBeLessThanOrEqual(1000000000006)
      expect(domain.yMax).toBeGreaterThanOrEqual(1000000000007)
      expect(domain.yMax - domain.yMin).toBeGreaterThan(0)
    })

    it('keeps close-thousands ticks distinct and truthful without collapsing', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 1000, budget_limit: null, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 1001, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]
      const domain = computeAdaptiveYDomain(buckets)
      const labels = domain.ticks.map((t) => formatTickValue(t))
      expect(new Set(labels).size).toBe(domain.ticks.length)
    })
  })

  describe('formatTickValue', () => {
    it('preserves truthful fractional values including tiny costs without integer rounding', () => {
      expect(formatTickValue(0.05)).toBe('0.05')
      expect(formatTickValue(0.5)).toBe('0.5')
      expect(formatTickValue(2.5)).toBe('2.5')
      expect(formatTickValue(0)).toBe('0')
    })

    it('formats values under 10000 as exact distinct numbers avoiding collapsing', () => {
      expect(formatTickValue(875)).toBe('875')
      expect(formatTickValue(1000)).toBe('1000')
      expect(formatTickValue(1000.5)).toBe('1000.5')
      expect(formatTickValue(1001)).toBe('1001')
      expect(formatTickValue(1125)).toBe('1125')
      expect(formatTickValue(1375)).toBe('1375')
    })

    it('keeps nearby large totals distinct and numerically accurate', () => {
      const values = [1000000000006, 1000000000007]
      const labels = values.map(formatTickValue)
      expect(labels[0]).not.toBe(labels[1])
      expect(labels.map(Number)).toEqual(values)
    })
    it('returns em dash for non-finite values', () => {
      expect(formatTickValue(Number.NaN)).toBe('—')
      expect(formatTickValue(Number.POSITIVE_INFINITY)).toBe('—')
    })
  })

  describe('buildTrendPoints', () => {
    const dims = {
      width: 760,
      height: 200,
      pad: { left: 48, right: 20, top: 16, bottom: 24 },
    }

    it('preserves cost 0 as a valid finite coordinate using domain bounds', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 0, budget_limit: 50, renewal_count: 0, data_insufficient: false },
        { bucket: '2025-08', monthly_cost: 100, budget_limit: 50, renewal_count: 0, data_insufficient: false },
      ]
      // yMin = 0, yMax = 100
      // chartHeight = 200 - 16 - 24 = 160
      // yFor(0) = 16 + 160 = 176 (bottom)
      // yFor(100) = 16 (top)
      const points = buildTrendPoints(buckets, dims, 0, 100)
      expect(points[0]?.cost).toBe(0)
      expect(points[0]?.yCost).toBe(176)
      expect(points[1]?.yCost).toBe(16)
    })

    it('treats missing budget as null without fake zeros', () => {
      const buckets: SubscriptionSeriesPoint[] = [
        { bucket: '2025-07', monthly_cost: 50, budget_limit: null, renewal_count: 0, data_insufficient: false },
      ]
      const points = buildTrendPoints(buckets, dims, 0, 100)
      expect(points[0]?.budget).toBeNull()
      expect(points[0]?.yBudget).toBeNull()
    })
  })

  describe('buildContiguousPath', () => {
    it('produces contiguous path for uninterrupted points', () => {
      const points = [
        { x: 10, y: 50 },
        { x: 20, y: 40 },
        { x: 30, y: 30 },
      ]
      const path = buildContiguousPath(points)
      expect(path).toBe('M 10.00 50.00 L 20.00 40.00 L 30.00 30.00')
    })

    it('gaps path when middle point has null y and never bridges across', () => {
      const points = [
        { x: 10, y: 50 },
        { x: 20, y: 40 },
        { x: 30, y: null }, // GAP
        { x: 40, y: 20 },
        { x: 50, y: 10 },
      ]
      const path = buildContiguousPath(points)
      expect(path).toBe('M 10.00 50.00 L 20.00 40.00 M 40.00 20.00 L 50.00 10.00')
      expect(path).not.toContain('L 40.00')
    })
  })

  describe('buildDifferenceAreas', () => {
    it('splits crossing segments both ways with exact linear intersection', () => {
      // 1. Cost crosses from over to under
      const pointsOverToUnder: TrendPoint[] = [
        { index: 0, bucket: '2025-07', x: 0, cost: 120, budget: 100, yCost: 0, yBudget: 20 },
        { index: 1, bucket: '2025-08', x: 100, cost: 80, budget: 100, yCost: 40, yBudget: 20 },
      ]

      const areas1 = buildDifferenceAreas(pointsOverToUnder)
      expect(areas1).toHaveLength(2)
      expect(areas1[0]?.tone).toBe('over')
      expect(areas1[1]?.tone).toBe('under')
      expect(areas1[0]?.points).toBe('0.00,0.00 50.00,20.00 0.00,20.00')
      expect(areas1[1]?.points).toBe('50.00,20.00 100.00,40.00 100.00,20.00')

      // 2. Cost crosses from under to over
      const pointsUnderToOver: TrendPoint[] = [
        { index: 0, bucket: '2025-07', x: 0, cost: 70, budget: 100, yCost: 60, yBudget: 30 },
        { index: 1, bucket: '2025-08', x: 100, cost: 110, budget: 100, yCost: 20, yBudget: 30 },
      ]
      const areas2 = buildDifferenceAreas(pointsUnderToOver)
      expect(areas2).toHaveLength(2)
      expect(areas2[0]?.tone).toBe('under')
      expect(areas2[1]?.tone).toBe('over')
      expect(areas2[0]?.points).toBe('0.00,60.00 75.00,30.00 0.00,30.00')
      expect(areas2[1]?.points).toBe('75.00,30.00 100.00,20.00 100.00,30.00')
    })

    it('gaps fill when either endpoint has missing budget', () => {
      const pointsWithGap: TrendPoint[] = [
        { index: 0, bucket: '2025-07', x: 0, cost: 120, budget: 100, yCost: 10, yBudget: 30 },
        { index: 1, bucket: '2025-08', x: 100, cost: 130, budget: null, yCost: 0, yBudget: null },
        { index: 2, bucket: '2025-09', x: 200, cost: 110, budget: 100, yCost: 20, yBudget: 30 },
      ]
      expect(buildDifferenceAreas(pointsWithGap)).toHaveLength(0)
    })

    it('creates no area when cost equals budget across segment', () => {
      const pointsEqual: TrendPoint[] = [
        { index: 0, bucket: '2025-07', x: 0, cost: 100, budget: 100, yCost: 20, yBudget: 20 },
        { index: 1, bucket: '2025-08', x: 100, cost: 100, budget: 100, yCost: 20, yBudget: 20 },
      ]
      expect(buildDifferenceAreas(pointsEqual)).toHaveLength(0)
    })
  })

  describe('getReadoutFacts', () => {
    it('formats over budget with signed delta and percent using real formatMoney', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-03',
        monthly_cost: 120,
        budget_limit: 100,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.costText).toBe('CNY 120.00')
      expect(facts.budgetText).toBe('CNY 100.00')
      expect(facts.deltaText).toBe('+CNY 20.00')
      expect(facts.deltaTone).toBe('over')
      expect(facts.statusText).toBe('超预算')
      expect(facts.percentText).toBe('+20.0%')
    })

    it('formats under budget with signed delta and percent using real formatMoney', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-04',
        monthly_cost: 85,
        budget_limit: 100,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.deltaText).toBe('-CNY 15.00')
      expect(facts.deltaTone).toBe('under')
      expect(facts.statusText).toBe('低于预算')
      expect(facts.percentText).toBe('-15.0%')
    })

    it('formats equal cost and budget', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-05',
        monthly_cost: 100,
        budget_limit: 100,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.deltaText).toBe('CNY 0.00')
      expect(facts.deltaTone).toBe('equal')
      expect(facts.statusText).toBe('持平')
      expect(facts.percentText).toBe('0.0%')
    })

    it('omits percent when budget is 0 to avoid division by zero', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-06',
        monthly_cost: 50,
        budget_limit: 0,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.deltaText).toBe('+CNY 50.00')
      expect(facts.statusText).toBe('超预算')
      expect(facts.percentText).toBeNull()
    })

    it('handles missing budget gracefully without faking cost-0', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-07',
        monthly_cost: 75,
        budget_limit: null,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.costText).toBe('CNY 75.00')
      expect(facts.budgetText).toBe('—')
      expect(facts.hasBudget).toBe(false)
      expect(facts.deltaText).toBeNull()
      expect(facts.deltaTone).toBeNull()
      expect(facts.statusText).toBeNull()
      expect(facts.percentText).toBeNull()
    })

    it('keeps cost 0 as 0 without treating as missing', () => {
      const pt: SubscriptionSeriesPoint = {
        bucket: '2026-08',
        monthly_cost: 0,
        budget_limit: 100,
        renewal_count: 0,
        data_insufficient: false,
      }
      const facts = getReadoutFacts(pt, 'CNY')
      expect(facts.costText).toBe('CNY 0.00')
      expect(facts.deltaText).toBe('-CNY 100.00')
      expect(facts.statusText).toBe('低于预算')
      expect(facts.percentText).toBe('-100.0%')
    })
  })

  describe('findNearestMonthIndex', () => {
    it('finds closest month index along x axis', () => {
      const points = [{ x: 50 }, { x: 150 }, { x: 250 }, { x: 350 }]
      expect(findNearestMonthIndex(points, 40)).toBe(0)
      expect(findNearestMonthIndex(points, 110)).toBe(1)
      expect(findNearestMonthIndex(points, 190)).toBe(1)
      expect(findNearestMonthIndex(points, 210)).toBe(2)
      expect(findNearestMonthIndex(points, 500)).toBe(3)
    })
  })

  describe('computeXTickIndices', () => {
    it('selects non-colliding ticks at narrow plot width and includes endpoints', () => {
      const indices = computeXTickIndices(12, 208, 60)
      expect(indices[0]).toBe(0)
      expect(indices[indices.length - 1]).toBe(11)
      for (let i = 0; i < indices.length - 1; i += 1) {
        expect(indices[i + 1]! - indices[i]!).toBeGreaterThanOrEqual(2)
      }
    })

    it('scales up ticks on wide displays while preserving endpoints and separation', () => {
      const indices = computeXTickIndices(12, 692, 60)
      expect(indices[0]).toBe(0)
      expect(indices[indices.length - 1]).toBe(11)
      expect(indices.length).toBeGreaterThanOrEqual(5)
      for (let i = 0; i < indices.length - 1; i += 1) {
        expect(indices[i + 1]! - indices[i]!).toBeGreaterThanOrEqual(2)
      }
    })

    it('handles small sample counts', () => {
      expect(computeXTickIndices(2, 200)).toEqual([0, 1])
      expect(computeXTickIndices(1, 200)).toEqual([0])
      expect(computeXTickIndices(0, 200)).toEqual([])
    })
  })
})
