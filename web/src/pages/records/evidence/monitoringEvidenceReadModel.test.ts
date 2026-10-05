import { describe, expect, it } from 'vitest'

import { decodeMonitoringEvidenceReadModel } from './evidenceReadModels'

const quality = {
  status: 'complete',
  partial: false,
  truncated: false,
  sample_count: 1,
  maintenance_count: 0,
  backfilled_count: 0,
  bucket_count: 1,
  gap_count: 0,
  peak_count: 0,
  data_point_count: 1,
}

function bucket(overrides: Record<string, unknown> = {}) {
  return {
    series_id: 'host',
    series_kind: 'host',
    start: '2026-08-16T00:00:00Z',
    end: '2026-08-16T12:00:00Z',
    source_layer: 'raw',
    source_granularity_seconds: 300,
    sample_count: 4,
    maintenance_count: 1,
    backfilled_count: 0,
    metrics: [{ name: 'cpu_usage_pct', unit: 'percent', average: 20 }],
    ...overrides,
  }
}

function hostModel(overrides: Record<string, unknown> = {}) {
  const buckets = (overrides.buckets as unknown[] | undefined) ?? [bucket()]
  const metricCount = buckets.reduce<number>((count, value) => {
    const metrics = (value as { metrics?: unknown[] }).metrics
    return count + (metrics?.length ?? 0)
  }, 0)
  const sampleCount = buckets.reduce<number>((count, value) => count + Number((value as { sample_count?: number }).sample_count ?? 0), 0)
  const maintenanceCount = buckets.reduce<number>((count, value) => count + Number((value as { maintenance_count?: number }).maintenance_count ?? 0), 0)
  const backfilledCount = buckets.reduce<number>((count, value) => count + Number((value as { backfilled_count?: number }).backfilled_count ?? 0), 0)
  return {
    version: 'monitoring_host_read_model/v1',
    requested_start: '2026-08-16T00:00:00Z',
    requested_end: '2026-08-17T00:00:00Z',
    coverage_start: '2026-08-16T00:00:00Z',
    coverage_end: '2026-08-17T00:00:00Z',
    actual_precision_seconds: 86400,
    buckets,
    gaps: [],
    peaks: [],
    quality: {
      ...quality,
      sample_count: sampleCount,
      maintenance_count: maintenanceCount,
      backfilled_count: backfilledCount,
      data_point_count: metricCount,
      bucket_count: buckets.length,
    },
    ...overrides,
  }
}

function metric(overrides: Record<string, unknown>) {
  return {
    name: 'cpu_usage_pct',
    unit: 'percent',
    average: 30,
    sample_count: 2,
    maintenance_count: 0,
    backfilled_count: 0,
    source_layer: 'raw',
    source_granularity_seconds: 300,
    ...overrides,
  }
}

describe('monitoring evidence read model CPU validity', () => {
  it('reads a stored v1 host snapshot and does not treat that shape as a new capture', () => {
    const decoded = decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v1',
    }), 'monitoring_host_read_model/v1')
    expect(decoded?.calculation_version).toBe('monitoring-evidence/v1')
    expect(decoded?.buckets[0]?.metrics[0]).toMatchObject({
      sample_count: 4,
      maintenance_count: 1,
      backfilled_count: 0,
      source_layer: 'raw',
      source_granularity_seconds: 300,
    })
    const withGap = hostModel({
      gaps: [{ series_id: 'host', start: '2026-08-16T12:00:00Z', end: '2026-08-17T00:00:00Z' }],
      quality: { ...quality, sample_count: 4, maintenance_count: 1, gap_count: 1, partial: false },
    })
    expect(decodeMonitoringEvidenceReadModel({
      ...withGap,
      calculation_version: 'monitoring-evidence/v1',
    }, 'monitoring_host_read_model/v1')?.gaps[0]?.metric).toBeUndefined()
    const beforeSummaryField = decodeMonitoringEvidenceReadModel(hostModel(), 'monitoring_host_read_model/v1')
    expect(beforeSummaryField?.calculation_version).toBeUndefined()
    expect(beforeSummaryField?.buckets[0]?.metrics[0]?.sample_count).toBe(4)
  })

  it('rejects an unknown host calculation version and a v2 metric metadata gap', () => {
    expect(decodeMonitoringEvidenceReadModel(
      hostModel({ calculation_version: 'monitoring-evidence/v9' }),
      'monitoring_host_read_model/v1',
    )).toBeNull()
    expect(decodeMonitoringEvidenceReadModel(
      hostModel({ calculation_version: 'monitoring-evidence/v2' }),
      'monitoring_host_read_model/v1',
    )).toBeNull()
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [bucket({ metrics: [{ ...metric({}), source_layer: undefined }] })],
    }), 'monitoring_host_read_model/v1')).toBeNull()
  })

  it('checks v2 reference counts, mixed source, and per-metric gaps', () => {
    const mixed = bucket({
      source_layer: 'mixed',
      source_granularity_seconds: 86400,
      sample_count: 3,
      maintenance_count: 1,
      backfilled_count: 0,
      metrics: [
        metric({ name: 'cpu_usage_pct', sample_count: 2 }),
        metric({
          name: 'mem_used_pct',
          sample_count: 3,
          maintenance_count: 1,
          source_layer: 'daily_aggregate',
          source_granularity_seconds: 86400,
          average: 40,
        }),
      ],
    })
    const decoded = decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [mixed],
      gaps: [
        { series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T12:00:00Z', end: '2026-08-16T18:00:00Z' },
        { series_id: 'host', metric: 'mem_used_pct', start: '2026-08-16T12:00:00Z', end: '2026-08-16T18:00:00Z' },
      ],
      quality: { ...quality, status: 'partial', partial: true, sample_count: 3, maintenance_count: 1, data_point_count: 2, gap_count: 2 },
    }), 'monitoring_host_read_model/v1')
    expect(decoded?.calculation_version).toBe('monitoring-evidence/v2')
    expect(decoded?.buckets[0]?.source_layer).toBe('mixed')
    expect(decoded?.buckets[0]?.metrics.map((item) => item.sample_count)).toEqual([2, 3])
    expect(decoded?.gaps.map((gap) => gap.metric)).toEqual(['cpu_usage_pct', 'mem_used_pct'])

    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [{ ...mixed, sample_count: 2, maintenance_count: 0 }],
    }), 'monitoring_host_read_model/v1')).toBeNull()
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [{ ...mixed, source_layer: 'raw' }],
    }), 'monitoring_host_read_model/v1')).toBeNull()
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [mixed],
      gaps: [{ series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T12:00:00Z', end: '2026-08-17T00:00:00Z' }],
      quality: { ...quality, sample_count: 3, maintenance_count: 1, data_point_count: 2, gap_count: 1, partial: false },
    }), 'monitoring_host_read_model/v1')).toBeNull()
  })

  it('lets a CPU gap overlap a memory-only bucket and rejects a same-metric overlap', () => {
    const memoryOnly = bucket({
      source_layer: 'daily_aggregate',
      source_granularity_seconds: 86400,
      sample_count: 3,
      maintenance_count: 0,
      backfilled_count: 0,
      metrics: [metric({
        name: 'mem_used_pct',
        sample_count: 3,
        source_layer: 'daily_aggregate',
        source_granularity_seconds: 86400,
      })],
    })
    const cpuGap = { series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T00:00:00Z', end: '2026-08-16T06:00:00Z' }
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [memoryOnly],
      gaps: [cpuGap],
      quality: { ...quality, status: 'partial', partial: true, sample_count: 3, gap_count: 1 },
    }), 'monitoring_host_read_model/v1')?.gaps).toHaveLength(1)

    const cpuBucket = bucket({
      sample_count: 2,
      maintenance_count: 0,
      metrics: [metric({ sample_count: 2 })],
    })
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [cpuBucket],
      gaps: [cpuGap],
      quality: { ...quality, status: 'partial', partial: true, sample_count: 2, gap_count: 1 },
    }), 'monitoring_host_read_model/v1')).toBeNull()
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [cpuBucket],
      gaps: [{ series_id: 'host', start: '2026-08-16T12:00:00Z', end: '2026-08-17T00:00:00Z' }],
      quality: { ...quality, status: 'partial', partial: true, sample_count: 2, gap_count: 1 },
    }), 'monitoring_host_read_model/v1')).toBeNull()
  })

  it('uses the lexicographically smallest metric when valid counts tie', () => {
    const tied = bucket({
      sample_count: 3,
      maintenance_count: 0,
      backfilled_count: 1,
      metrics: [
        metric({ name: 'mem_used_pct', sample_count: 3, maintenance_count: 2, backfilled_count: 0 }),
        metric({ name: 'cpu_usage_pct', sample_count: 3, maintenance_count: 0, backfilled_count: 1 }),
      ],
    })
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [tied],
      quality: { ...quality, sample_count: 3, backfilled_count: 1, data_point_count: 2 },
    }), 'monitoring_host_read_model/v1')?.buckets[0]?.sample_count).toBe(3)
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [{ ...tied, maintenance_count: 2, backfilled_count: 0 }],
      quality: { ...quality, sample_count: 3, maintenance_count: 2, data_point_count: 2 },
    }), 'monitoring_host_read_model/v1')).toBeNull()
  })

  it('requires a peak source to match the metric rather than a mixed bucket', () => {
    const mixed = bucket({
      source_layer: 'mixed',
      source_granularity_seconds: 86400,
      sample_count: 3,
      maintenance_count: 0,
      metrics: [
        metric({ sample_count: 2 }),
        metric({
          name: 'mem_used_pct',
          sample_count: 3,
          source_layer: 'daily_aggregate',
          source_granularity_seconds: 86400,
        }),
      ],
    })
    const peak = {
      series_id: 'host',
      metric: 'cpu_usage_pct',
      at: '2026-08-16T03:00:00Z',
      value: 30,
      source_layer: 'raw',
    }
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [mixed],
      peaks: [peak],
      quality: { ...quality, sample_count: 3, data_point_count: 2, peak_count: 1 },
    }), 'monitoring_host_read_model/v1')?.peaks[0]?.source_layer).toBe('raw')
    expect(decodeMonitoringEvidenceReadModel(hostModel({
      calculation_version: 'monitoring-evidence/v2',
      buckets: [mixed],
      peaks: [{ ...peak, source_layer: 'daily_aggregate' }],
      quality: { ...quality, sample_count: 3, data_point_count: 2, peak_count: 1 },
    }), 'monitoring_host_read_model/v1')).toBeNull()
  })

  it('keeps probe snapshots on v1 serialization', () => {
    const probe = {
      version: 'monitoring_probe_read_model/v1',
      requested_start: '2026-08-16T00:00:00Z',
      requested_end: '2026-08-17T00:00:00Z',
      coverage_start: '2026-08-16T00:00:00Z',
      coverage_end: '2026-08-17T00:00:00Z',
      actual_precision_seconds: 86400,
      buckets: [{
        series_id: 'probe-a',
        series_kind: 'http',
        start: '2026-08-16T00:00:00Z',
        end: '2026-08-16T12:00:00Z',
        source_layer: 'raw',
        source_granularity_seconds: 300,
        sample_count: 1,
        maintenance_count: 0,
        backfilled_count: 0,
        metrics: [{ name: 'latency_ms', unit: 'ms', average: 42 }],
      }],
      gaps: [{ series_id: 'probe-a', start: '2026-08-16T12:00:00Z', end: '2026-08-17T00:00:00Z' }],
      peaks: [],
      quality: { ...quality, status: 'partial', partial: true, gap_count: 1 },
    }
    const decoded = decodeMonitoringEvidenceReadModel(probe, 'monitoring_probe_read_model/v1')
    expect(decoded?.calculation_version).toBeUndefined()
    expect(decoded?.gaps[0]?.metric).toBeUndefined()
    expect(decoded?.buckets[0]?.metrics[0]?.sample_count).toBe(1)
    expect(decodeMonitoringEvidenceReadModel({
      ...probe,
      calculation_version: 'monitoring-evidence/v2',
    }, 'monitoring_probe_read_model/v1')?.calculation_version).toBeUndefined()
    expect(decodeMonitoringEvidenceReadModel({
      ...probe,
      gaps: [],
      quality: { ...quality },
      buckets: [{ ...probe.buckets[0], source_layer: 'mixed' }],
    }, 'monitoring_probe_read_model/v1')).toBeNull()
  })
})
