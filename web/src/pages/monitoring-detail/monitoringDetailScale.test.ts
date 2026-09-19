import { describe, expect, it } from 'vitest'

import { niceMax, observationChartHeight, OBSERVATION_CHART_HEIGHT } from './monitoringDetailScale'

describe('niceMax', () => {
  it('follows the data instead of an artificial lower bound', () => {
    // I/O wait and load live well below 1; the axis must hug them.
    expect(niceMax(0.5 * 1.15)).toBeCloseTo(0.6, 6)
    expect(niceMax(0.65 * 1.15)).toBeCloseTo(0.8, 6)
    expect(niceMax(1.2 * 1.15)).toBe(1.5)
  })

  it('rounds a degenerate or negative bound up to a small usable axis', () => {
    expect(niceMax(0)).toBe(0.1)
    expect(niceMax(-5)).toBe(0.1)
  })

  it('snaps to the 1–1.5–2–2.5–3–4–5–6–8 ladder', () => {
    expect(niceMax(1)).toBe(1)
    expect(niceMax(1.38)).toBe(1.5)
    expect(niceMax(2)).toBe(2)
    expect(niceMax(2.4)).toBe(2.5)
    expect(niceMax(2.5)).toBe(2.5)
    expect(niceMax(2.9)).toBe(3)
    expect(niceMax(3.68)).toBe(4)
    expect(niceMax(5)).toBe(5)
    expect(niceMax(5.5)).toBe(6)
    expect(niceMax(6.325)).toBe(8)
    expect(niceMax(11)).toBe(15)
    expect(niceMax(45)).toBe(50)
    expect(niceMax(51.75)).toBe(60)
  })

  it('keeps the load thresholds out of a small data-scaled axis', () => {
    // Preview-sized load data stays under the 4/6/8 policy lines.
    expect(niceMax(1.2 * 1.15)).toBe(1.5)
    expect(niceMax(0.65 * 1.15)).toBeCloseTo(0.8, 6)
    // 0.23 raw snaps to 0.25, still far below the first threshold.
    expect(niceMax(0.2 * 1.15)).toBeCloseTo(0.25, 6)
  })
})

describe('observationChartHeight', () => {
  it('is a compact 4×2 tile height and does not grow with the monitor', () => {
    expect(OBSERVATION_CHART_HEIGHT).toBe(200)
    expect(observationChartHeight()).toBe(200)
    expect(observationChartHeight({ layout: 'wide', gridWidth: 3516, viewportHeight: 1923 })).toBe(200)
    expect(observationChartHeight({ layout: 'narrow', gridWidth: 269 })).toBe(200)
  })
})
