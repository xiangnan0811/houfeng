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

  it('renders readable runtime-control labels without incident-only meta rows', () => {
    render(
      <EventList
        events={[
          {
            event_id: 'evt_runtime_001',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_monitoring_maintenance_entered',
            severity: '',
            summary: '节点已进入维护模式',
            created_at: '2026-04-26T08:10:00Z',
          },
          {
            event_id: 'evt_runtime_002',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_monitoring_maintenance_exited',
            severity: '',
            summary: '节点已退出维护模式',
            created_at: '2026-04-26T08:11:00Z',
          },
          {
            event_id: 'evt_runtime_003',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_monitoring_paused',
            severity: '',
            summary: '节点监控已暂停',
            created_at: '2026-04-26T08:12:00Z',
          },
          {
            event_id: 'evt_runtime_004',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_monitoring_resumed',
            severity: '',
            summary: '节点监控已恢复',
            created_at: '2026-04-26T08:13:00Z',
          },
          {
            event_id: 'evt_runtime_005',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_maintenance_entered',
            severity: '',
            summary: '目标已进入维护模式',
            created_at: '2026-04-26T08:14:00Z',
          },
          {
            event_id: 'evt_runtime_006',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_maintenance_exited',
            severity: '',
            summary: '目标已退出维护模式',
            created_at: '2026-04-26T08:15:00Z',
          },
          {
            event_id: 'evt_runtime_007',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_paused',
            severity: '',
            summary: '目标已暂停',
            created_at: '2026-04-26T08:16:00Z',
          },
          {
            event_id: 'evt_runtime_008',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_resumed',
            severity: '',
            summary: '目标已恢复',
            created_at: '2026-04-26T08:17:00Z',
          },
          {
            event_id: 'evt_runtime_009',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_archived',
            severity: '',
            summary: '目标已归档',
            created_at: '2026-04-26T08:18:00Z',
          },
          {
            event_id: 'evt_runtime_010',
            incident_id: '',
            incident_class: '',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'target_restored_to_paused',
            severity: '',
            summary: '目标已恢复为暂停状态',
            created_at: '2026-04-26T08:19:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByRole('heading', { level: 3, name: '节点进入维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '节点退出维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '节点暂停监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '节点恢复监控' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标进入维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标退出维护' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已暂停' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已恢复' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已归档' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: '目标已恢复为暂停' })).toBeInTheDocument()
    expect(screen.queryByText('异常类型')).not.toBeInTheDocument()
  })
})
