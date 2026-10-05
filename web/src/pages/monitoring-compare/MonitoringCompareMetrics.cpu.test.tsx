import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { HostMetricSeriesPoint } from '../../components/monitoring-detail/metricSeries'
import type { MonitoringRuntimeWindow } from '../../lib/types'
import { MonitoringCompareMetrics } from './MonitoringCompareMetrics'

function point(overrides: Partial<HostMetricSeriesPoint>): HostMetricSeriesPoint {
  return {
    observed_at: '2026-04-24T10:00:00Z',
    sample_count: 1,
    cpu_usage_pct: null,
    mem_used_pct: 40,
    disk_used_pct: 30,
    inode_used_pct: null,
    load_5: 0.4,
    cpu_iowait_pct: null,
    net_in_bytes_per_sec: null,
    net_out_bytes_per_sec: null,
    load_1: null,
    load_15: null,
    swap_used_pct: null,
    disk_busy_pct: null,
    disk_read_bytes_per_sec: null,
    disk_write_bytes_per_sec: null,
    ...overrides,
  }
}

const runtimeWindow: MonitoringRuntimeWindow = {
  key: '24h',
  started_at: '2026-04-23T10:00:00Z',
  ended_at: '2026-04-24T12:00:00Z',
  bucket_count: 3,
  available_started_at: '2026-04-24T10:00:00Z',
  available_ended_at: '2026-04-24T10:05:00Z',
  sample_count: 2,
}

describe('MonitoringCompareMetrics CPU availability', () => {
  it('shows unavailable at the last host bucket and keeps memory', () => {
    render(
      <MonitoringCompareMetrics
        window={runtimeWindow}
        metricPoints={[
          point({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 20, mem_used_pct: 30 }),
          point({ observed_at: '2026-04-24T10:05:00Z', cpu_usage_pct: null, mem_used_pct: 61 }),
          point({ observed_at: '2026-04-24T10:10:00Z', sample_count: 0, cpu_usage_pct: 0, mem_used_pct: null, disk_used_pct: null }),
        ]}
      />,
    )
    const cpu = screen.getByRole('article', { name: 'CPU 使用率' })
    expect(cpu).toHaveTextContent('CPU 采样不可用')
    expect(cpu).not.toHaveTextContent('20.0%')
    expect(cpu).not.toHaveTextContent('0.0%')
    expect(cpu).not.toHaveClass('monitoring-detail-chart--notice')
    expect(screen.getByRole('article', { name: '内存使用率' })).toHaveTextContent('61.0%')
    expect(screen.queryByText('该窗口没有样本')).not.toBeInTheDocument()
  })

  it('shows a real zero after skipping trailing empty buckets', () => {
    render(
      <MonitoringCompareMetrics
        window={runtimeWindow}
        metricPoints={[
          point({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 20 }),
          point({ observed_at: '2026-04-24T10:05:00Z', cpu_usage_pct: 0 }),
          point({ observed_at: '2026-04-24T10:10:00Z', sample_count: 0, cpu_usage_pct: null, mem_used_pct: null }),
        ]}
      />,
    )
    expect(screen.getByRole('article', { name: 'CPU 使用率' })).toHaveTextContent('0.0%')
    expect(screen.queryByText('CPU 采样不可用')).not.toBeInTheDocument()
  })
})
