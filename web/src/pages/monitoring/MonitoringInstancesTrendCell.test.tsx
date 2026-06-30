import { render } from '@testing-library/react'
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
})
