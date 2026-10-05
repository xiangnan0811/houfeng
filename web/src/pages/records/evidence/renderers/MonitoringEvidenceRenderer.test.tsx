import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { MonitoringEvidenceReadModel } from '../evidenceReadModels'
import { decodeMonitoringEvidenceReadModel } from '../evidenceReadModels'
import { MonitoringEvidenceRenderer } from './MonitoringEvidenceRenderer'

const quality = {
  status: 'partial' as const,
  partial: true,
  truncated: false,
  sample_count: 3,
  maintenance_count: 0,
  backfilled_count: 0,
  bucket_count: 2,
  gap_count: 1,
  peak_count: 0,
  data_point_count: 4,
}

function lines(name: string) {
  return screen.getByRole('img', { name }).querySelectorAll('polyline')
}

describe('MonitoringEvidenceRenderer metric sources', () => {
  it('renders an old host snapshot from bucket metadata without a reference count', () => {
    const decoded = decodeMonitoringEvidenceReadModel({
      version: 'monitoring_host_read_model/v1',
      calculation_version: 'monitoring-evidence/v1',
      requested_start: '2026-08-16T00:00:00Z',
      requested_end: '2026-08-17T00:00:00Z',
      coverage_start: '2026-08-16T00:00:00Z',
      coverage_end: '2026-08-17T00:00:00Z',
      actual_precision_seconds: 86400,
      buckets: [{
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
      }],
      gaps: [],
      peaks: [],
      quality: { ...quality, status: 'complete', partial: false, sample_count: 4, maintenance_count: 1, bucket_count: 1, gap_count: 0, data_point_count: 1 },
    }, 'monitoring_host_read_model/v1')
    expect(decoded).not.toBeNull()
    render(<MonitoringEvidenceRenderer title="趋势" model={decoded!} />)
    const cpu = screen.getByText('CPU 使用率').closest('figure')
    expect(cpu).toHaveTextContent('有效样本 4 · 原始')
    expect(screen.queryByText(/覆盖参考样本/)).not.toBeInTheDocument()
    expect(screen.queryByText('混合来源')).not.toBeInTheDocument()
  })

  it('labels mixed buckets and keeps the CPU count apart from the reference count', () => {
    const model: MonitoringEvidenceReadModel = {
      version: 'monitoring_host_read_model/v1',
      calculation_version: 'monitoring-evidence/v2',
      requested_start: '2026-08-16T00:00:00Z',
      requested_end: '2026-08-17T00:00:00Z',
      coverage_start: '2026-08-16T00:00:00Z',
      coverage_end: '2026-08-17T00:00:00Z',
      actual_precision_seconds: 86400,
      buckets: [{
        series_id: 'host',
        series_kind: 'host',
        start: '2026-08-16T00:00:00Z',
        end: '2026-08-16T12:00:00Z',
        source_layer: 'mixed',
        source_granularity_seconds: 86400,
        sample_count: 3,
        maintenance_count: 0,
        backfilled_count: 0,
        metrics: [
          {
            name: 'cpu_usage_pct', unit: 'percent', average: 30,
            sample_count: 2, maintenance_count: 0, backfilled_count: 0,
            source_layer: 'raw', source_granularity_seconds: 300,
          },
          {
            name: 'mem_used_pct', unit: 'percent', average: 40,
            sample_count: 3, maintenance_count: 0, backfilled_count: 0,
            source_layer: 'daily_aggregate', source_granularity_seconds: 86400,
          },
        ],
      }],
      gaps: [{ series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T12:00:00Z', end: '2026-08-17T00:00:00Z' }],
      peaks: [],
      quality,
    }
    render(<MonitoringEvidenceRenderer title="趋势" model={model} />)
    const cpu = screen.getByText('CPU 使用率').closest('figure')
    const memory = screen.getByText('内存使用率').closest('figure')
    expect(cpu).toHaveTextContent('有效样本 2 · 原始')
    expect(cpu).toHaveTextContent('混合来源')
    expect(cpu).toHaveTextContent('覆盖参考样本 3')
    expect(cpu).toHaveTextContent('指标缺口 1')
    expect(cpu).not.toHaveTextContent('有效样本 3')
    expect(memory).toHaveTextContent('有效样本 3 · 日聚合')
    expect(memory).toHaveTextContent('覆盖参考样本 3')
    expect(memory).not.toHaveTextContent('指标缺口')
  })

  it('shows a metric gap when that metric has no samples and does not invent a zero', () => {
    const model: MonitoringEvidenceReadModel = {
      version: 'monitoring_host_read_model/v1',
      calculation_version: 'monitoring-evidence/v2',
      requested_start: '2026-08-16T00:00:00Z',
      requested_end: '2026-08-17T00:00:00Z',
      coverage_start: '2026-08-16T00:00:00Z',
      coverage_end: '2026-08-16T12:00:00Z',
      actual_precision_seconds: 86400,
      buckets: [{
        series_id: 'host',
        series_kind: 'host',
        start: '2026-08-16T00:00:00Z',
        end: '2026-08-16T12:00:00Z',
        source_layer: 'raw',
        source_granularity_seconds: 300,
        sample_count: 3,
        maintenance_count: 0,
        backfilled_count: 0,
        metrics: [{
          name: 'mem_used_pct', unit: 'percent', average: 40,
          sample_count: 3, maintenance_count: 0, backfilled_count: 0,
          source_layer: 'raw', source_granularity_seconds: 300,
        }],
      }],
      gaps: [{ series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T00:00:00Z', end: '2026-08-16T12:00:00Z' }],
      peaks: [],
      quality: { ...quality, sample_count: 3, bucket_count: 1, data_point_count: 1 },
    }
    render(<MonitoringEvidenceRenderer title="趋势" model={model} />)
    const cpu = screen.getByText('CPU 使用率').closest('figure')
    expect(cpu).toHaveTextContent('指标缺口 1')
    expect(cpu).not.toHaveTextContent('%')
    expect(cpu?.querySelector('svg')).toBeNull()
  })

  it('breaks only the metric named by a gap and still breaks every metric for a legacy gap', () => {
    const shared = {
      version: 'monitoring_host_read_model/v1' as const,
      requested_start: '2026-08-16T00:00:00Z',
      requested_end: '2026-08-16T02:00:00Z',
      coverage_start: '2026-08-16T00:00:00Z',
      coverage_end: '2026-08-16T02:00:00Z',
      actual_precision_seconds: 3600,
      buckets: [
        {
          series_id: 'host', series_kind: 'host', start: '2026-08-16T00:00:00Z', end: '2026-08-16T01:00:00Z',
          source_layer: 'raw', source_granularity_seconds: 3600, sample_count: 1, maintenance_count: 0, backfilled_count: 0,
          metrics: [
            { name: 'cpu_usage_pct', unit: 'percent', average: 20 },
            { name: 'mem_used_pct', unit: 'percent', average: 30 },
          ],
        },
        {
          series_id: 'host', series_kind: 'host', start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z',
          source_layer: 'raw', source_granularity_seconds: 3600, sample_count: 1, maintenance_count: 0, backfilled_count: 0,
          metrics: [
            { name: 'cpu_usage_pct', unit: 'percent', average: 40 },
            { name: 'mem_used_pct', unit: 'percent', average: 50 },
          ],
        },
      ],
      peaks: [],
      quality,
    }
    const metricGap: MonitoringEvidenceReadModel = {
      ...shared,
      calculation_version: 'monitoring-evidence/v2',
      gaps: [{ series_id: 'host', metric: 'cpu_usage_pct', start: '2026-08-16T00:30:00Z', end: '2026-08-16T01:30:00Z' }],
    }
    const { unmount } = render(<MonitoringEvidenceRenderer title="趋势" model={metricGap} />)
    expect(lines('CPU 使用率趋势')).toHaveLength(2)
    expect(lines('内存使用率趋势')).toHaveLength(1)
    unmount()

    const legacyGap: MonitoringEvidenceReadModel = {
      ...shared,
      calculation_version: 'monitoring-evidence/v1',
      gaps: [{ series_id: 'host', start: '2026-08-16T00:30:00Z', end: '2026-08-16T01:30:00Z' }],
    }
    render(<MonitoringEvidenceRenderer title="趋势" model={legacyGap} />)
    expect(lines('CPU 使用率趋势')).toHaveLength(2)
    expect(lines('内存使用率趋势')).toHaveLength(2)
  })
})
