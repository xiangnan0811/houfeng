import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { MetricThresholds } from '../../config/thresholds'
import type { MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'
import { MonitoringInstancesTrendCell } from './MonitoringInstancesTrendCell'

function monitoringInstanceRecord(): MonitoringInstanceRecord {
  return {
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Edge',
    group: 'edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-26T09:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
  }
}

describe('MonitoringInstancesTrendCell', () => {
  it('uses the supplied alert threshold before marking a sparkline critical', () => {
    const thresholds: MetricThresholds = {
      cpu: { warning: 80, alert: 90, critical: 95 },
      mem: { warning: 85, alert: 92, critical: 95 },
      disk: { warning: 85, alert: 92, critical: 97 },
      inode: { warning: 80, alert: 90, critical: 95 },
      iowait: { warning: 20, alert: 35, critical: 50 },
      load5: { warning: 4, alert: 6, critical: 8 },
    }
    const sparklines: MonitoringInstanceSparklinesResponse = {
      monitoring_instances: {
        mi_001: {
          cpu_usage_pct: [40, 50],
          mem_used_pct: [50, 60],
          disk_used_pct: [88, 92],
        },
      },
    }

    const { container } = render(
      <MonitoringInstancesTrendCell
        monitoringInstance={monitoringInstanceRecord()}
        sparklines={sparklines}
        thresholds={thresholds}
      />,
    )

    const sparklinesNodes = container.querySelectorAll('svg.sparkline')
    expect(sparklinesNodes).toHaveLength(3)
    expect(sparklinesNodes[2]).toHaveClass('sparkline--alert')
    expect(sparklinesNodes[2]).not.toHaveClass('sparkline--critical')
  })

  it('keeps null buckets as visual gaps instead of joining values', () => {
    const sparklines: MonitoringInstanceSparklinesResponse = {
      monitoring_instances: {
        mi_001: {
          cpu_usage_pct: [10, null, 20],
          mem_used_pct: [null, null],
          disk_used_pct: [40],
        },
      },
    }
    const { container } = render(
      <MonitoringInstancesTrendCell
        monitoringInstance={monitoringInstanceRecord()}
        sparklines={sparklines}
        thresholds={null}
      />,
    )
    // One sparkline per metric, no positioned segment wrappers, no inline styles.
    const tracks = [...container.querySelectorAll('.monitoring-table__trend-track')]
    expect(tracks).toHaveLength(2)
    expect(container.querySelectorAll('.monitoring-table__trend-segment')).toHaveLength(0)
    expect(container.querySelectorAll('.monitoring-table__trend-track [style]')).toHaveLength(0)

    // [10, null, 20] keeps the gap: two separate polylines, never one joined line.
    const cpuPolylines = [...tracks[0]!.querySelectorAll('polyline')]
    expect(cpuPolylines).toHaveLength(2)
    // [40] is a single bucket: no line, just the point marker.
    expect(tracks[1]!.querySelectorAll('polyline')).toHaveLength(0)
    expect(tracks[1]!.querySelectorAll('circle')).toHaveLength(1)
    expect(container.querySelectorAll('svg.sparkline--alert')).toHaveLength(0)
  })

  it('projects gapped segments onto one full-series domain and keeps bucket placement', () => {
    const sparklines: MonitoringInstanceSparklinesResponse = {
      monitoring_instances: {
        mi_001: {
          cpu_usage_pct: [10, null, 90],
          mem_used_pct: [0, null, 80],
          disk_used_pct: [12, 48, null],
        },
      },
    }
    const { container } = render(
      <MonitoringInstancesTrendCell
        monitoringInstance={monitoringInstanceRecord()}
        sparklines={sparklines}
        thresholds={null}
      />,
    )
    const items = [...container.querySelectorAll('.monitoring-table__trend-item')]
    expect(items.length).toBeGreaterThanOrEqual(2)

    // Gapped buckets keep their own x position: bucket 0 at 0, bucket 2 at the far edge.
    const cpuTrack = items[0]!.querySelector('.monitoring-table__trend-track') as HTMLElement
    const cpuPolylines = [...cpuTrack.querySelectorAll('polyline')]
    expect(cpuPolylines).toHaveLength(2)
    const cpuXs = cpuPolylines.map((line) => Number((line.getAttribute('points') ?? '').split(' ')[0]?.split(',')[0]))
    expect(cpuXs[0]).toBe(0)
    expect(cpuXs[1]).toBeCloseTo(120, 0)
    const cpuYs = cpuPolylines.map((line) => Number((line.getAttribute('points') ?? '').split(' ')[0]?.split(',')[1]))
    // Both metrics share the full-series domain, so the higher value sits higher.
    expect(cpuYs[1]).toBeLessThan(cpuYs[0]! - 4)

    const memTrack = items[1]!.querySelector('.monitoring-table__trend-track') as HTMLElement
    const memYs = [...memTrack.querySelectorAll('polyline')].map(
      (line) => Number((line.getAttribute('points') ?? '').split(' ')[0]?.split(',')[1]),
    )
    expect(memYs).toHaveLength(2)
    expect(memYs[1]).toBeLessThan(memYs[0]! - 4)

    expect(screen.getByLabelText('磁盘 24小时历史 48.0%')).toBeInTheDocument()
  })
})


