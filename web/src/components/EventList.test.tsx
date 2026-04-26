import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { EventList } from './EventList'

describe('EventList', () => {
  it('renders state-change events with shared status badges', () => {
    render(
      <EventList
        events={[
          {
            event_id: 'evt_001',
            incident_id: 'inc_001',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'incident_started',
            severity: '严重',
            summary: 'HTTPS 探测连续失败',
            created_at: '2026-04-25T08:10:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByText('异常开始')).toBeInTheDocument()
    expect(screen.getByText('目标探测失败')).toBeInTheDocument()
    expect(screen.getByText('严重')).toBeInTheDocument()
    expect(screen.getByText('目标')).toBeInTheDocument()
    expect(screen.getByText('tg_001')).toBeInTheDocument()
    expect(screen.getByText('HTTPS 探测连续失败')).toBeInTheDocument()
  })

  it('renders an explicit empty state when there are no events', () => {
    render(<EventList events={[]} />)

    expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument()
    expect(screen.getByText('系统暂时没有新的 incident 变化记录。')).toBeInTheDocument()
  })

  it('renders binding audit events with sane labels and no incident-only meta row', () => {
    render(
      <EventList
        events={[
          {
            event_id: 'evt_bind_001',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_binding_reset',
            severity: '',
            summary: '节点已重置绑定并等待重新接入',
            created_at: '2026-04-26T08:10:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByText('绑定已重置')).toBeInTheDocument()
    expect(screen.getByText('节点')).toBeInTheDocument()
    expect(screen.getByText('nd_001')).toBeInTheDocument()
    expect(screen.getByText('节点已重置绑定并等待重新接入')).toBeInTheDocument()
    expect(screen.queryByText('异常类型')).not.toBeInTheDocument()
  })
})
