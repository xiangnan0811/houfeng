import { describe, expect, it } from 'vitest'

import { seriesValueAt } from './metricSeries'

function sample(value: number | null, observedAt: string) {
  return { value, observedAt }
}

describe('seriesValueAt', () => {
  it('walks back trailing gaps for the current readout', () => {
    const series = [
      sample(12, '2026-04-24T09:00:00Z'),
      sample(18, '2026-04-24T09:30:00Z'),
      sample(null, '2026-04-24T10:00:00Z'),
      sample(null, '2026-04-24T10:30:00Z'),
    ]
    expect(seriesValueAt(series, null)).toBe(18)
  })

  it('returns null when every point is a gap', () => {
    expect(seriesValueAt([sample(null, '2026-04-24T10:00:00Z')], null)).toBeNull()
    expect(seriesValueAt([], null)).toBeNull()
  })
})
