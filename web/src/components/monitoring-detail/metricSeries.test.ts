import { describe, expect, it } from 'vitest'

import { currentCPUMetricValue, lastHostSamplePoint, seriesValueAt, type HostMetricSeriesPoint } from './metricSeries'

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

function hostPoint(overrides: Partial<HostMetricSeriesPoint>): HostMetricSeriesPoint {
  return {
    observed_at: '2026-04-24T10:00:00Z',
    sample_count: 1,
    cpu_usage_pct: null,
    cpu_iowait_pct: null,
    mem_used_pct: 10,
    disk_used_pct: 10,
    inode_used_pct: null,
    load_5: null,
    net_in_bytes_per_sec: null,
    net_out_bytes_per_sec: null,
    ...overrides,
  }
}

describe('currentCPUMetricValue', () => {
  const points = [
    hostPoint({ observed_at: '2026-04-24T10:00:00Z', sample_count: 1, cpu_usage_pct: 20, cpu_iowait_pct: 4 }),
    hostPoint({ observed_at: '2026-04-24T10:05:00Z', sample_count: 1, cpu_usage_pct: null, cpu_iowait_pct: null }),
    hostPoint({ observed_at: '2026-04-24T10:10:00Z', sample_count: 0, cpu_usage_pct: 0, cpu_iowait_pct: 0, mem_used_pct: null, disk_used_pct: null }),
  ]

  it('reads the last host row and does not walk back across an unavailable CPU rate', () => {
    expect(lastHostSamplePoint(points)?.observed_at).toBe('2026-04-24T10:05:00Z')
    expect(currentCPUMetricValue(points, 'cpu_usage_pct')).toBeNull()
    expect(currentCPUMetricValue(points, 'cpu_iowait_pct')).toBeNull()
  })

  it('keeps a real zero on the last host row and skips trailing empty buckets', () => {
    const zero = [
      hostPoint({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 20, cpu_iowait_pct: 4 }),
      hostPoint({ observed_at: '2026-04-24T10:05:00Z', cpu_usage_pct: 0, cpu_iowait_pct: 0 }),
      hostPoint({ observed_at: '2026-04-24T10:10:00Z', sample_count: 0, cpu_usage_pct: null }),
    ]
    expect(currentCPUMetricValue(zero, 'cpu_usage_pct')).toBe(0)
    expect(currentCPUMetricValue(zero, 'cpu_iowait_pct')).toBe(0)
  })

  it('returns null when every bucket is empty so callers can show no observation', () => {
    const empty = [hostPoint({ sample_count: 0, cpu_usage_pct: 20 })]
    expect(lastHostSamplePoint(empty)).toBeNull()
    expect(currentCPUMetricValue(empty, 'cpu_usage_pct')).toBeNull()
    expect(currentCPUMetricValue([], 'cpu_iowait_pct')).toBeNull()
  })
})
