import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { classifyHeartbeatFreshness } from '../monitoring/heartbeatFreshness'
import { MonitoringDetailStatusBand } from './MonitoringDetailStatusBand'

describe('trusted online evidence', () => {
  it.each(['暂停', '已退役'])('shows live evidence independently of %s and old collection timestamps', (status) => {
    const record: MonitoringInstanceRecord = {
      monitoring_instance_id: 'mi_one', display_name: '实例', group: '', region: '', city: '', provider: '',
      lifecycle_status: status === '已退役' ? '已退役' : '已接入', monitoring_status: '暂停', binding_status: '已绑定',
      labels: [], note: '', current_health_status: '正常', current_active_incident_count: 0, current_primary_issue_summary: '',
      created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-26T00:00:00Z', metadata_updated_at: '2026-09-01T00:00:00Z',
      last_heartbeat_at: '2026-09-01T00:00:00Z', last_trusted_online_at: '2026-09-26T00:00:00Z',
    }
    const now = new Date('2026-09-26T00:00:05Z')
    const freshness = classifyHeartbeatFreshness(record.last_trusted_online_at ?? undefined, { heartbeatIntervalMs: 5000, missingThreshold: 12 }, now, true)
    const { container } = render(<MonitoringDetailStatusBand monitoringInstance={record} heartbeatFreshness={freshness} snapshotReadAt={now} sample={null} />)
    expect(freshness.kind).toBe('fresh')
    expect(screen.getByLabelText('心跳与采样')).toHaveTextContent('在线证据')
    expect(container.querySelector('.timestamp')).toHaveAttribute('title', expect.stringContaining('2026/09/26'))
    expect(screen.queryByText('数据陈旧')).not.toBeInTheDocument()
    expect(screen.queryByText('正常')).not.toBeInTheDocument()
  })
})
