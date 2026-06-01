import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { IncidentList } from './IncidentList'

describe('IncidentList', () => {
  it('renders active incidents with shared status badges', () => {
    render(
      <IncidentList
        incidents={[
          {
            incident_id: 'inc_001',
            incident_class: 'monitoring_instance_disk_pressure',
            object_type: 'monitoring_instance',
            object_id: 'mi_001',
            severity: '告警',
            started_at: '2026-04-25T08:00:00Z',
            last_evaluated_at: '2026-04-25T08:05:00Z',
            source_summary: '磁盘使用率持续超过阈值',
          },
        ]}
      />,
    )

    expect(screen.getByText('监控实例磁盘压力')).toBeInTheDocument()
    expect(screen.getByText('告警')).toBeInTheDocument()
    expect(screen.getByText('监控实例')).toBeInTheDocument()
    expect(screen.getByText('mi_001')).toBeInTheDocument()
    expect(screen.getByText('磁盘使用率持续超过阈值')).toBeInTheDocument()
  })

  it('renders an explicit empty state when there are no incidents', () => {
    render(<IncidentList incidents={[]} />)

    expect(screen.getByText('当前没有活跃异常')).toBeInTheDocument()
    expect(screen.getByText('一切正常，暂未发现需要处理的活跃异常。')).toBeInTheDocument()
  })
})
