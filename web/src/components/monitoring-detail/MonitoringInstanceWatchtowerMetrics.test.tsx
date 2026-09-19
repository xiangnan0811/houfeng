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

    const { container } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample()}
        metricPoints={metricPoints}
        timeWindow="24h"
        thresholds={thresholds}
      />,
    )

    const cpuPlot = screen.getByRole('heading', { name: 'CPU 使用率' }).closest('.watchtower-metric-plot')
    expect(cpuPlot).toBeInstanceOf(HTMLElement)
    if (!(cpuPlot instanceof HTMLElement)) throw new Error('expected CPU plot')
    expect(within(cpuPlot).getByText('70%')).toBeInTheDocument()
    expect(within(cpuPlot).getByText('82%')).toBeInTheDocument()
    expect(within(cpuPlot).getByText('93%')).toBeInTheDocument()
    expect(container.querySelectorAll('.metric-chart__threshold').length).toBeGreaterThanOrEqual(18)
  })

  it('keeps four fixed groups and does not reorder them by threshold priority', () => {
    const { container } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ cpu_usage_pct: 10, disk_used_pct: 97, mem_used_pct: 20 })}
        metricPoints={metricPoints}
        timeWindow="24h"
        thresholds={{
          cpu: { warning: 70, alert: 82, critical: 93 },
          mem: { warning: 85, alert: 92, critical: 95 },
          disk: { warning: 85, alert: 92, critical: 97 },
          inode: { warning: 80, alert: 90, critical: 95 },
          iowait: { warning: 20, alert: 35, critical: 50 },
          load5: { warning: 4, alert: 6, critical: 8 },
        }}
      />,
    )

    const groups = Array.from(container.querySelectorAll('.watchtower-metric-group')).map(
      (node) => node.getAttribute('aria-label'),
    )
    expect(groups).toEqual(['CPU 与负载', '内存', '磁盘与 I/O', '网络'])
    expect(screen.queryByText(/已按阈值优先级排序/)).not.toBeInTheDocument()
    expect(screen.getByText('5 分钟负载', { selector: 'summary' }).closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText('CPU I/O 等待', { selector: 'summary' }).closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText('Inode 使用率', { selector: 'summary' }).closest('details')).not.toHaveAttribute('open')
    expect(screen.queryByLabelText('最近一次主机样本')).not.toBeInTheDocument()
  })

  it('labels inline facts as the latest sample and keeps historical lines neutral', () => {
    const { container } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ cpu_usage_pct: 99, mem_used_pct: 99, disk_used_pct: 99 })}
        metricPoints={metricPoints}
        timeWindow="24h"
        thresholds={{
          cpu: { warning: 70, alert: 82, critical: 93 },
          mem: { warning: 85, alert: 92, critical: 95 },
          disk: { warning: 85, alert: 92, critical: 97 },
          inode: { warning: 80, alert: 90, critical: 95 },
          iowait: { warning: 20, alert: 35, critical: 50 },
          load5: { warning: 4, alert: 6, critical: 8 },
        }}
      />,
    )
    expect(screen.getAllByText('最近样本').length).toBeGreaterThanOrEqual(3)
    expect(screen.getByText('CPU 被窃取')).toBeInTheDocument()
    expect(screen.getByText('交换使用率')).toBeInTheDocument()
    expect(container.querySelector('.watchtower-metric-group--critical')).toBeNull()
    const cpuLine = screen.getByLabelText('CPU 使用率近 24h趋势').querySelector('polyline')
    expect(cpuLine?.getAttribute('stroke')).toBe('var(--accent)')
  })

  it('shows genuine network zero on window series and hides invalid or legacy rates', () => {
    const { rerender } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ net_in_bytes_per_sec: 0, net_out_bytes_per_sec: 0, network_rates_valid: true })}
        metricPoints={[{
          observed_at: '2026-04-24T09:00:00Z',
          cpu_usage_pct: 10,
          mem_used_pct: 20,
          disk_used_pct: 30,
          inode_used_pct: 4,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: 0,
          net_out_bytes_per_sec: 0,
        }]}
        timeWindow="24h"
      />,
    )
    expect(screen.getAllByText(/0 B\/s/).length).toBeGreaterThanOrEqual(2)

    rerender(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ net_in_bytes_per_sec: 0, net_out_bytes_per_sec: 4096, network_rates_valid: false })}
        metricPoints={[]}
        timeWindow="24h"
      />,
    )
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)

    const legacy = hostSample({ net_in_bytes_per_sec: 1024, net_out_bytes_per_sec: 2048 })
    delete (legacy as { network_rates_valid?: boolean | null }).network_rates_valid
    rerender(
      <MonitoringInstanceWatchtowerMetrics
        sample={legacy}
        metricPoints={[{
          observed_at: '2026-04-24T09:00:00Z',
          cpu_usage_pct: 10,
          mem_used_pct: 20,
          disk_used_pct: 30,
          inode_used_pct: 4,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: null,
          net_out_bytes_per_sec: null,
        }]}
        timeWindow="24h"
      />,
    )
    expect(screen.getByLabelText('下行近 24h趋势').textContent).not.toContain('1.0 KB/s')
  })

  it('does not invent a latest sample from historical buckets', () => {
    render(
      <MonitoringInstanceWatchtowerMetrics
        sample={null}
        metricPoints={[{
          observed_at: '2026-04-24T09:00:00Z',
          cpu_usage_pct: 77,
          mem_used_pct: 20,
          disk_used_pct: 30,
          inode_used_pct: 4,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: 0,
          net_out_bytes_per_sec: 0,
        }]}
        timeWindow="24h"
      />,
    )
    expect(screen.getByRole('heading', { name: 'CPU 使用率' })).toBeInTheDocument()
    expect(screen.getAllByText('窗口末值').length).toBeGreaterThan(0)
    expect(screen.getByText('77.0%')).toBeInTheDocument()
    expect(screen.queryByLabelText('最近一次主机样本')).not.toBeInTheDocument()
  })

  it('keeps a missing last bucket as an em dash instead of an earlier value', () => {
    render(
      <MonitoringInstanceWatchtowerMetrics
        sample={null}
        metricPoints={[
          {
            observed_at: '2026-04-24T09:00:00Z',
            cpu_usage_pct: 77,
            mem_used_pct: 20,
            disk_used_pct: 30,
            inode_used_pct: 4,
            load_5: 1,
            cpu_iowait_pct: 1,
            net_in_bytes_per_sec: 0,
            net_out_bytes_per_sec: 0,
          },
          {
            observed_at: '2026-04-24T10:00:00Z',
            cpu_usage_pct: null,
            mem_used_pct: 21,
            disk_used_pct: 31,
            inode_used_pct: 4,
            load_5: 1,
            cpu_iowait_pct: 1,
            net_in_bytes_per_sec: 0,
            net_out_bytes_per_sec: 0,
          },
        ]}
        timeWindow="24h"
      />,
    )
    const cpu = screen.getByRole('region', { name: 'CPU 与负载' })
    expect(within(cpu).getByText('窗口末值')).toBeInTheDocument()
    expect(within(cpu).queryByText('77.0%')).not.toBeInTheDocument()
    expect(cpu.querySelector('.watchtower-metric-group__current')).toHaveTextContent('—')
  })

  it('keeps a valid-zero network series on a 0-to-positive B/s axis', () => {
    const { container } = render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ net_in_bytes_per_sec: 0, net_out_bytes_per_sec: 0, network_rates_valid: true })}
        metricPoints={[{
          observed_at: '2026-04-24T09:00:00Z',
          cpu_usage_pct: 10,
          mem_used_pct: 20,
          disk_used_pct: 30,
          inode_used_pct: 4,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: 0,
          net_out_bytes_per_sec: 0,
        }]}
        timeWindow="24h"
      />,
    )
    const net = screen.getByRole('region', { name: '网络' })
    const yLabels = Array.from(net.querySelectorAll('.metric-chart__y-tick text')).map((node) => node.textContent)
    expect(yLabels).toContain('0 B/s')
    expect(yLabels.some((label) => label !== '0 B/s')).toBe(true)
    expect(yLabels.join(' ')).not.toContain('-1')
    expect(container.querySelector('.metric-chart__threshold-leader')).toBeNull()
  })

  it('formats high network rates as GB/s on the shared axis', () => {
    render(
      <MonitoringInstanceWatchtowerMetrics
        sample={hostSample({ net_in_bytes_per_sec: 3221225472, net_out_bytes_per_sec: 1073741824, network_rates_valid: true })}
        metricPoints={[{
          observed_at: '2026-04-24T09:00:00Z',
          cpu_usage_pct: 10,
          mem_used_pct: 20,
          disk_used_pct: 30,
          inode_used_pct: 4,
          load_5: 1,
          cpu_iowait_pct: 1,
          net_in_bytes_per_sec: 3221225472,
          net_out_bytes_per_sec: 1073741824,
        }]}
        timeWindow="24h"
      />,
    )
    const net = screen.getByRole('region', { name: '网络' })
    expect(Array.from(net.querySelectorAll('.metric-chart__y-tick text')).some((node) => /GB\/s$/.test(node.textContent ?? ''))).toBe(true)
  })

  it('uses gutter leaders only for detail presentation with thresholds', () => {
    const props = {
      sample: hostSample(),
      metricPoints,
      timeWindow: '24h' as const,
      thresholds: {
        cpu: { warning: 70, alert: 82, critical: 93 },
        mem: { warning: 85, alert: 92, critical: 95 },
        disk: { warning: 85, alert: 92, critical: 97 },
        inode: { warning: 80, alert: 90, critical: 95 },
        iowait: { warning: 20, alert: 35, critical: 50 },
        load5: { warning: 4, alert: 6, critical: 8 },
      },
    }
    const { container, rerender } = render(<MonitoringInstanceWatchtowerMetrics {...props} />)
    expect(container.querySelector('.metric-chart__threshold-leader')).toBeNull()
    rerender(<MonitoringInstanceWatchtowerMetrics {...props} detailPresentation />)
    expect(container.querySelectorAll('.metric-chart__threshold-leader').length).toBeGreaterThan(0)
    expect(screen.getByRole('region', { name: '网络' }).querySelector('.metric-chart__threshold-leader')).toBeNull()
  })
})
