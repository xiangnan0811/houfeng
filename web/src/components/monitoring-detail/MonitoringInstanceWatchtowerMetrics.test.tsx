import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { MetricThresholds } from '../../config/thresholds'
import type { HostSample } from '../../lib/types'
import { MonitoringInstanceWatchtowerMetrics, type HostMetricSeriesPoint } from './MonitoringInstanceWatchtowerMetrics'

function hostSample(overrides: Partial<HostSample> = {}): HostSample {
  return {
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-04-24T10:05:00Z',
    received_at: '2026-04-24T10:05:01Z',
    agent_version: 'dev',
    fingerprint: 'fp-mi',
    cpu_usage_pct: 81,
    load_1: 0.8,
    load_5: 1.6,
    load_15: 1.9,
    mem_used_pct: 62,
    mem_available_bytes: 1073741824,
    mem_total_bytes: 8589934592,
    swap_used_pct: 0,
    disk_used_pct: 41,
    disk_total_bytes: 107374182400,
    inode_used_pct: 9,
    net_in_bytes_per_sec: 1024,
    net_out_bytes_per_sec: 2048,
    cpu_iowait_pct: 6,
    cpu_steal_pct: 1.4,
    disk_read_bytes_per_sec: 3072,
    disk_write_bytes_per_sec: 4096,
    disk_busy_pct: 8,
    uptime_seconds: 7200,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-latest',
    ...overrides,
  }
}

describe('MonitoringInstanceWatchtowerMetrics', () => {
  it('renders warning alert and critical threshold lines from supplied thresholds', () => {
    const thresholds: MetricThresholds = {
      cpu: { warning: 70, alert: 82, critical: 93 },
      mem: { warning: 85, alert: 92, critical: 95 },
      disk: { warning: 85, alert: 92, critical: 97 },
      inode: { warning: 80, alert: 90, critical: 95 },
      iowait: { warning: 20, alert: 35, critical: 50 },
      load5: { warning: 4, alert: 6, critical: 8 },
    }
    const metricPoints: HostMetricSeriesPoint[] = [
      {
        observed_at: '2026-04-24T09:35:00Z',
        cpu_usage_pct: 76,
        mem_used_pct: 58,
        disk_used_pct: 40,
        inode_used_pct: 8,
        load_5: 1.2,
        cpu_iowait_pct: 4,
        net_in_bytes_per_sec: 900,
        net_out_bytes_per_sec: 1800,
      },
      {
        observed_at: '2026-04-24T10:05:00Z',
        cpu_usage_pct: 81,
        mem_used_pct: 62,
        disk_used_pct: 41,
        inode_used_pct: 9,
        load_5: 1.6,
        cpu_iowait_pct: 6,
        net_in_bytes_per_sec: 1024,
        net_out_bytes_per_sec: 2048,
      },
    ]

    const { container } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample()}
        metricPoints={metricPoints}
        timeWindow="24h"
        thresholds={thresholds}
      />,
    )

    const cpuCard = screen.getByRole('heading', { name: 'CPU 使用率' }).closest('article')
    expect(cpuCard).not.toBeNull()
    expect(within(cpuCard!).getByText('70%')).toBeInTheDocument()
    expect(within(cpuCard!).getByText('82%')).toBeInTheDocument()
    expect(within(cpuCard!).getByText('93%')).toBeInTheDocument()
    expect(container.querySelectorAll('.metric-chart__threshold').length).toBeGreaterThanOrEqual(18)
  })
})
