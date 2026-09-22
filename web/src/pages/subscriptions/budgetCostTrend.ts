import { formatMoney } from '../../lib/format'
import type { SubscriptionSeriesPoint } from '../../lib/types'

export type TrendPoint = {
  index: number
  bucket: string
  x: number
  cost: number | null
  budget: number | null
  yCost: number | null
  yBudget: number | null
}

export type DifferenceTone = 'over' | 'under'

export type DifferenceArea = {
  tone: DifferenceTone
  points: string
}

export type ReadoutFacts = {
  bucket: string
  monthLabel: string
  cost: number
  budget: number | null
  costText: string
  budgetText: string
  hasBudget: boolean
  deltaText: string | null
  deltaTone: 'over' | 'under' | 'equal' | null
  statusText: '超预算' | '低于预算' | '持平' | null
  percentText: string | null
}

export type ChartDimensions = {
  width: number
  height: number
  pad: {
    left: number
    right: number
    top: number
    bottom: number
  }
}

export type AdaptiveYDomain = {
  yMin: number
  yMax: number
  ticks: [number, number, number]
}

const NICE_FACTORS = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]

function getStepDecimals(step: number): number {
  if (!Number.isFinite(step) || step <= 0) return 0
  const str = String(step)
  if (str.includes('e-')) {
    const parts = str.split('e-')
    return (parts[0]?.includes('.') ? parts[0].split('.')[1]!.length : 0) + Number(parts[1])
  }
  if (str.includes('.')) {
    return str.split('.')[1]!.length
  }
  return 0
}

function canonicalizeValue(val: number, step: number): number {
  if (!Number.isFinite(val)) return 0
  const decimals = Math.min(100, Math.max(0, getStepDecimals(step / 2)))
  return Number(val.toFixed(decimals))
}

export function findNiceStep(minStep: number): number {
  if (!Number.isFinite(minStep) || minStep <= 0) return 1
  const exp = Math.pow(10, Math.floor(Math.log10(minStep)))
  const frac = minStep / exp
  for (const factor of NICE_FACTORS) {
    if (factor >= frac - 1e-9) {
      return Number((factor * exp).toPrecision(12))
    }
  }
  return Number((10 * exp).toPrecision(12))
}

function fitZeroBasedDomain(dataMax: number): AdaptiveYDomain {
  if (dataMax <= 0) {
    return { yMin: 0, yMax: 10, ticks: [0, 5, 10] }
  }
  const targetMax = dataMax * 1.1
  const minStep = targetMax / 2
  const step = findNiceStep(minStep)
  const yMax = canonicalizeValue(2 * step, step)
  const mid = canonicalizeValue(step, step)
  return { yMin: 0, yMax, ticks: [0, mid, yMax] }
}

function fitNiceDomain(
  targetMin: number,
  targetMax: number,
): AdaptiveYDomain {
  const allowedMin = Math.max(0, targetMin)
  const minSpan = Math.max(1e-9, targetMax - allowedMin)
  const minStep = minSpan / 2
  const baseExp = Math.floor(Math.log10(minStep))

  // Direct bounded search across nice steps without unbounded decrement loops
  for (let expPower = baseExp; expPower <= baseExp + 3; expPower += 1) {
    const exp = Math.pow(10, expPower)
    for (const factor of NICE_FACTORS) {
      const step = Number((factor * exp).toPrecision(12))
      if (step < minStep * 0.9999) continue

      const snapBases = [step, step / 2]
      for (const snap of snapBases) {
        // Direct bounded arithmetic: snap targetMin down to multiple of snap
        const rawMin = Math.max(0, Math.floor(allowedMin / snap) * snap)
        const yMinCandidate = canonicalizeValue(rawMin, step)
        const yMaxCandidate = canonicalizeValue(yMinCandidate + 2 * step, step)

        // Require covering targetMin and targetMax to provide breathing room on both bounds
        if (yMinCandidate <= allowedMin && yMaxCandidate >= targetMax && yMinCandidate >= 0) {
          const mid = canonicalizeValue((yMinCandidate + yMaxCandidate) / 2, step)
          return {
            yMin: yMinCandidate,
            yMax: yMaxCandidate,
            ticks: [yMinCandidate, mid, yMaxCandidate],
          }
        }
      }
    }
  }

  // Direct bounded arithmetic fallback: guarantees covering targetMin and targetMax
  const step = findNiceStep(minSpan / 2)
  const rawMin = Math.max(0, Math.min(allowedMin, targetMax - 2 * step))
  const yMin = canonicalizeValue(rawMin, step)
  const yMax = canonicalizeValue(yMin + 2 * step, step)
  const mid = canonicalizeValue((yMin + yMax) / 2, step)
  return { yMin, yMax, ticks: [yMin, mid, yMax] }
}

/**
 * Computes an adaptive nice Y DOMAIN using all finite known costs + budgets.
 * Padded above and below min/max (roughly 10% span).
 * Clustered positive series do not start at 0 so fluctuations remain visible.
 * Lower bound is non-negative; real zeros are included; missing budgets are excluded.
 */
export function computeAdaptiveYDomain(buckets: SubscriptionSeriesPoint[]): AdaptiveYDomain {
  const knownValues: number[] = []
  for (const b of buckets) {
    if (typeof b.monthly_cost === 'number' && Number.isFinite(b.monthly_cost) && b.monthly_cost >= 0) {
      knownValues.push(b.monthly_cost)
    }
    if (b.budget_limit != null && Number.isFinite(b.budget_limit) && b.budget_limit >= 0) {
      knownValues.push(b.budget_limit)
    }
  }

  if (knownValues.length === 0) {
    return { yMin: 0, yMax: 10, ticks: [0, 5, 10] }
  }

  const dataMin = Math.min(...knownValues)
  const dataMax = Math.max(...knownValues)

  // Flat series (min === max)
  if (dataMin === dataMax) {
    if (dataMax === 0) {
      return { yMin: 0, yMax: 10, ticks: [0, 5, 10] }
    }
    // Reasonable value-relative padding (10%)
    const pad = dataMax * 0.1
    const targetMin = Math.max(0, dataMax - pad)
    const targetMax = dataMax + pad
    return fitNiceDomain(targetMin, targetMax)
  }

  const dataSpan = dataMax - dataMin
  const pad = dataSpan * 0.1
  const targetMin = Math.max(0, dataMin - pad)
  const targetMax = dataMax + pad

  // If data is at or close to zero, anchor at zero
  if (targetMin <= 0 || dataMin <= pad) {
    return fitZeroBasedDomain(dataMax)
  }
  return fitNiceDomain(targetMin, targetMax)
}

/**
 * Formats tick values truthfully without forced rounding or scaling.
 * Canonicalization belongs to domain generation; invalid numbers emit em dash.
 */
export function formatTickValue(value: number): string {
  return Number.isFinite(value) ? String(value) : '—'
}

/**
 * Builds coordinate points for each bucket using the adaptive domain (yMin to yMax).
 * Cost 0 is a real value and gets plotted at yFor(0).
 * Missing budget limits are null and get yBudget = null.
 */
export function buildTrendPoints(
  buckets: SubscriptionSeriesPoint[],
  dims: ChartDimensions,
  yMin: number,
  yMax: number,
): TrendPoint[] {
  const { width, height, pad } = dims
  const chartWidth = Math.max(1, width - pad.left - pad.right)
  const chartHeight = Math.max(1, height - pad.top - pad.bottom)
  const domainSpan = Math.max(1e-9, yMax - yMin)

  const xFor = (index: number) =>
    pad.left + (buckets.length <= 1 ? chartWidth / 2 : (index / (buckets.length - 1)) * chartWidth)

  const yFor = (value: number) =>
    pad.top + chartHeight - ((value - yMin) / domainSpan) * chartHeight

  return buckets.map((bucket, index) => {
    const cost =
      typeof bucket.monthly_cost === 'number' && Number.isFinite(bucket.monthly_cost) && bucket.monthly_cost >= 0
        ? bucket.monthly_cost
        : null

    const budget =
      bucket.budget_limit != null && Number.isFinite(bucket.budget_limit) && bucket.budget_limit >= 0
        ? bucket.budget_limit
        : null

    return {
      index,
      bucket: bucket.bucket,
      x: xFor(index),
      cost,
      budget,
      yCost: cost != null ? yFor(cost) : null,
      yBudget: budget != null ? yFor(budget) : null,
    }
  })
}

/**
 * Builds SVG path 'd' string for contiguous segments with non-null Y coordinates.
 * Never bridges across missing/null endpoints (gaps the path).
 * Disconnected segments each start with 'M' and continue with 'L'.
 */
export function buildContiguousPath(points: Array<{ x: number; y: number | null }>): string {
  const subpaths: string[] = []
  let currentRun: Array<{ x: number; y: number }> = []

  for (const pt of points) {
    if (pt.y != null && Number.isFinite(pt.y)) {
      currentRun.push({ x: pt.x, y: pt.y })
    } else {
      if (currentRun.length >= 2) {
        subpaths.push(
          currentRun.map((p, idx) => `${idx === 0 ? 'M' : 'L'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' '),
        )
      }
      currentRun = []
    }
  }

  if (currentRun.length >= 2) {
    subpaths.push(
      currentRun.map((p, idx) => `${idx === 0 ? 'M' : 'L'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' '),
    )
  }

  return subpaths.join(' ')
}

function polygon(points: Array<{ x: number; y: number }>): string {
  return points.map((p) => `${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' ')
}

/**
 * Builds difference areas (red for over budget, green for under budget).
 * Handles piecewise linear crossing splits at exact intersection.
 * Skips any segment where either endpoint lacks a known budget or cost (gaps fill).
 */
export function buildDifferenceAreas(points: TrendPoint[]): DifferenceArea[] {
  const areas: DifferenceArea[] = []

  for (let i = 0; i < points.length - 1; i += 1) {
    const left = points[i]
    const right = points[i + 1]
    if (!left || !right) continue

    if (
      left.cost == null ||
      right.cost == null ||
      left.budget == null ||
      right.budget == null ||
      left.yCost == null ||
      right.yCost == null ||
      left.yBudget == null ||
      right.yBudget == null
    ) {
      continue
    }

    const leftDelta = left.cost - left.budget
    const rightDelta = right.cost - right.budget

    if (leftDelta === 0 && rightDelta === 0) continue

    if (leftDelta === 0 || rightDelta === 0 || Math.sign(leftDelta) === Math.sign(rightDelta)) {
      const tone: DifferenceTone = (leftDelta || rightDelta) > 0 ? 'over' : 'under'
      areas.push({
        tone,
        points: polygon([
          { x: left.x, y: left.yCost },
          { x: right.x, y: right.yCost },
          { x: right.x, y: right.yBudget },
          { x: left.x, y: left.yBudget },
        ]),
      })
      continue
    }

    const t = Math.abs(leftDelta) / (Math.abs(leftDelta) + Math.abs(rightDelta))
    const intersectionX = left.x + (right.x - left.x) * t
    const intersectionY = left.yCost + (right.yCost - left.yCost) * t

    areas.push({
      tone: leftDelta > 0 ? 'over' : 'under',
      points: polygon([
        { x: left.x, y: left.yCost },
        { x: intersectionX, y: intersectionY },
        { x: left.x, y: left.yBudget },
      ]),
    })

    areas.push({
      tone: rightDelta > 0 ? 'over' : 'under',
      points: polygon([
        { x: intersectionX, y: intersectionY },
        { x: right.x, y: right.yCost },
        { x: right.x, y: right.yBudget },
      ]),
    })
  }

  return areas
}

/**
 * Computes X-axis tick indices based on available plot width.
 * Ensures labels have adequate spacing and endpoints are included
 * without adjacent or colliding labels.
 */
export function computeXTickIndices(
  sampleCount: number,
  plotWidth: number,
  minSpacing = 60,
): number[] {
  if (sampleCount <= 0) return []
  if (sampleCount === 1) return [0]

  const maxLabels = Math.max(2, Math.min(6, Math.floor(plotWidth / minSpacing) + 1))
  const targetCount = Math.min(sampleCount, maxLabels)

  if (targetCount <= 2) {
    return [0, sampleCount - 1]
  }

  const step = (sampleCount - 1) / (targetCount - 1)
  const indices: number[] = []

  for (let i = 0; i < targetCount; i += 1) {
    const idx = Math.round(i * step)
    if (indices.length === 0 || idx > indices[indices.length - 1]!) {
      indices.push(idx)
    }
  }

  if (indices[0] !== 0) {
    indices[0] = 0
  }
  const lastIdx = sampleCount - 1
  if (indices[indices.length - 1] !== lastIdx) {
    if (indices.length > 1 && lastIdx - indices[indices.length - 2]! >= 2) {
      indices[indices.length - 1] = lastIdx
    } else if (indices.length > 1) {
      indices.pop()
      indices[indices.length - 1] = lastIdx
    } else {
      indices.push(lastIdx)
    }
  }

  return indices
}

export function formatMonthLabel(bucket: string): string {
  const [, month] = bucket.split('-')
  return `${bucket.slice(2, 4)}/${month ?? '01'}`
}

/**
 * Calculates truthful in-flow readout facts for a bucket.
 * Reuses existing formatMoney from lib/format.
 * - Missing budget: budgetText = '—', deltaText = null, percentText = null (never cost - 0).
 * - Cost 0 stays 0.
 * - Percent only displayed when finite budget > 0.
 */
export function getReadoutFacts(
  point: SubscriptionSeriesPoint,
  baseCurrency: string,
): ReadoutFacts {
  const bucket = point.bucket
  const month = formatMonthLabel(bucket)
  const cost = typeof point.monthly_cost === 'number' && Number.isFinite(point.monthly_cost)
    ? Math.max(0, point.monthly_cost)
    : 0
  const costText = formatMoney(cost, baseCurrency)

  const hasBudget = point.budget_limit != null && Number.isFinite(point.budget_limit) && point.budget_limit >= 0
  const budget = hasBudget ? point.budget_limit! : null
  const budgetCurrency = point.budget_currency || baseCurrency
  const budgetText = hasBudget && budget != null ? formatMoney(budget, budgetCurrency) : '—'

  if (!hasBudget || budget == null) {
    return {
      bucket,
      monthLabel: month,
      cost,
      budget: null,
      costText,
      budgetText,
      hasBudget: false,
      deltaText: null,
      deltaTone: null,
      statusText: null,
      percentText: null,
    }
  }

  const delta = cost - budget

  if (delta > 0) {
    const pct = budget > 0 ? (delta / budget) * 100 : null
    return {
      bucket,
      monthLabel: month,
      cost,
      budget,
      costText,
      budgetText,
      hasBudget: true,
      deltaText: `+${formatMoney(delta, baseCurrency)}`,
      deltaTone: 'over',
      statusText: '超预算',
      percentText: pct != null ? `+${pct.toFixed(1)}%` : null,
    }
  }

  if (delta < 0) {
    const pct = budget > 0 ? (delta / budget) * 100 : null
    return {
      bucket,
      monthLabel: month,
      cost,
      budget,
      costText,
      budgetText,
      hasBudget: true,
      deltaText: `-${formatMoney(Math.abs(delta), baseCurrency)}`,
      deltaTone: 'under',
      statusText: '低于预算',
      percentText: pct != null ? `${pct.toFixed(1)}%` : null,
    }
  }

  // delta === 0
  const pct = budget > 0 ? '0.0%' : null
  return {
    bucket,
    monthLabel: month,
    cost,
    budget,
    costText,
    budgetText,
    hasBudget: true,
    deltaText: formatMoney(0, baseCurrency),
    deltaTone: 'equal',
    statusText: '持平',
    percentText: pct,
  }
}

/**
 * Finds index of point closest to the given X coordinate.
 */
export function findNearestMonthIndex(points: Array<{ x: number }>, targetX: number): number {
  if (points.length === 0) return -1
  let closestIndex = 0
  let minDistance = Math.abs(points[0]!.x - targetX)

  for (let i = 1; i < points.length; i += 1) {
    const distance = Math.abs(points[i]!.x - targetX)
    if (distance < minDistance) {
      minDistance = distance
      closestIndex = i
    }
  }

  return closestIndex
}
