import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { HostMetricSeriesPoint } from '../../components/monitoring-detail/metricSeries'
import type { HostSample, MonitoringRuntimeWindow } from '../../lib/types'
import { MonitoringDetailObservations } from './MonitoringDetailObservations'

function point(overrides: Partial<HostMetricSeriesPoint>): HostMetricSeriesPoint {
  return {
    observed_at: '2026-04-24T10:00:00Z',
    sample_count: 1,
    cpu_usage_pct: null,
    cpu_iowait_pct: null,
    mem_used_pct: 40,
    disk_used_pct: 30,
    inode_used_pct: 5,
    load_5: 0.2,
    load_1: 0.1,
    load_15: 0.3,
    swap_used_pct: 1,
    disk_busy_pct: 9,
    net_in_bytes_per_sec: 100,
    net_out_bytes_per_sec: 100,
    disk_read_bytes_per_sec: 10,
    disk_write_bytes_per_sec: 10,
    ...overrides,
  }
}

function hostSample(overrides: Partial<HostSample> = {}): HostSample {
  return {
    monitoring_instance_id: 'mi_001',
    observed_at: '2026-04-24T10:05:00Z',
    received_at: '2026-04-24T10:05:01Z',
    agent_version: 'dev',
    fingerprint: 'fp',
    cpu_usage_pct: 0,
    cpu_rates_valid: false,
    load_1: 0.1,
    load_5: 0.2,
    load_15: 0.3,
    mem_used_pct: 40,
    mem_available_bytes: 1,
    mem_total_bytes: 2,
    swap_used_pct: 0,
    disk_used_pct: 30,
    disk_total_bytes: 4,
    inode_used_pct: 5,
    net_in_bytes_per_sec: 0,
    net_out_bytes_per_sec: 0,
    network_rates_valid: true,
    cpu_iowait_pct: 0,
    cpu_steal_pct: 0,
    disk_read_bytes_per_sec: 0,
    disk_write_bytes_per_sec: 0,
    disk_busy_pct: 0,
    uptime_seconds: 60,
    maintenance_context: false,
    is_backfilled: false,
    sync_batch_id: 'sync-a',
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

function renderObservations(metricPoints: HostMetricSeriesPoint[], sample: HostSample | null = hostSample()) {
  return render(
    <MonitoringDetailObservations
      sample={sample}
      metricPoints={metricPoints}
      timeWindow="24h"
      window={runtimeWindow}
      thresholds={null}
      loading={false}
      error={null}
      onRetryThresholds={() => undefined}
    />,
  )
}

describe('MonitoringDetailObservations CPU availability', () => {
  const unavailablePoints = [
    point({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 20, cpu_iowait_pct: 4, mem_used_pct: 30 }),
    point({ observed_at: '2026-04-24T10:05:00Z', cpu_usage_pct: null, cpu_iowait_pct: null, mem_used_pct: 55 }),
    point({
      observed_at: '2026-04-24T10:10:00Z',
      sample_count: 0,
      cpu_usage_pct: null,
      cpu_iowait_pct: null,
      mem_used_pct: null,
      disk_used_pct: null,
      disk_busy_pct: null,
    }),
  ]

  it('shows the last host row as unavailable instead of an earlier rate or a placeholder zero', () => {
    renderObservations(unavailablePoints)
    const cpu = screen.getByRole('region', { name: 'CPU 使用率' })
    expect(cpu).toHaveTextContent('CPU 采样不可用')
    expect(cpu).not.toHaveTextContent('20.0%')
    expect(cpu).not.toHaveTextContent('0.0%')
    expect(cpu).not.toHaveClass('monitoring-detail-chart--notice')
    expect(cpu).not.toHaveClass('monitoring-detail-chart--alert')
    expect(cpu).not.toHaveClass('monitoring-detail-chart--critical')
    const steal = within(cpu).getByText('被窃取').parentElement
    expect(steal).toHaveTextContent('—')
    expect(steal).not.toHaveTextContent('0.0%')

    const iowait = screen.getByRole('region', { name: 'I/O 等待' })
    expect(iowait).toHaveTextContent('CPU 采样不可用')
    expect(iowait).toHaveTextContent('9.0%')
    expect(screen.getByRole('region', { name: '内存使用率' })).toHaveTextContent('55.0%')
    expect(screen.queryByText('该窗口没有样本')).not.toBeInTheDocument()
  })

  it('hovers the earlier finite CPU bucket without keeping the unavailable label', () => {
    renderObservations(unavailablePoints)
    const cpu = screen.getByRole('region', { name: 'CPU 使用率' })
    const svg = within(cpu).getByRole('img', { name: /CPU 使用率/ })
    svg.getBoundingClientRect = () => ({
      x: 0, y: 0, top: 0, left: 0, right: 360, bottom: 160, width: 360, height: 160, toJSON: () => ({}),
    })
    fireEvent.mouseMove(svg, { clientX: 8, clientY: 40 })
    expect(cpu).toHaveTextContent('20.0%')
    expect(cpu).not.toHaveTextContent('CPU 采样不可用')
  })

  it('shows a real zero and skips trailing empty buckets', () => {
    renderObservations([
      point({ observed_at: '2026-04-24T10:00:00Z', cpu_usage_pct: 20, cpu_iowait_pct: 4 }),
      point({ observed_at: '2026-04-24T10:05:00Z', cpu_usage_pct: 0, cpu_iowait_pct: 0 }),
      point({ observed_at: '2026-04-24T10:10:00Z', sample_count: 0, cpu_usage_pct: null, cpu_iowait_pct: null, mem_used_pct: null }),
    ], hostSample({ cpu_usage_pct: 0, cpu_iowait_pct: 0, cpu_steal_pct: 0, cpu_rates_valid: true }))
    const cpu = screen.getByRole('region', { name: 'CPU 使用率' })
    expect(cpu).toHaveTextContent('0.0%')
    expect(cpu).not.toHaveTextContent('CPU 采样不可用')
    expect(within(cpu).getByText('被窃取').parentElement).toHaveTextContent('0.0%')
  })

  it('uses the chart empty state when no host row exists', () => {
    renderObservations([
      point({
        observed_at: '2026-04-24T10:00:00Z',
        sample_count: 0,
        cpu_usage_pct: null,
        mem_used_pct: null,
        disk_used_pct: null,
        disk_busy_pct: null,
      }),
    ], null)
    expect(screen.queryByText('CPU 采样不可用')).not.toBeInTheDocument()
    expect(screen.getAllByText('暂无观测数据').length).toBeGreaterThan(0)
  })
})
